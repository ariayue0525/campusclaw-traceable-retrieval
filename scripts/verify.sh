#!/usr/bin/env sh
# 端到端验收：把关键场景的真实 HTTP 报文写入 docs/evidence，并对关键结论做断言。
# 认证为 JWT Bearer 方案：登录取 token 对，请求带 Authorization 头；
# 覆盖签发、续期轮换、吊销、伪造拒绝，以及三条检索路径与班级隔离。
set -eu

BASE_URL=${BASE_URL:-http://localhost:8080}
: "${TEACHER_PASSWORD:?请设置 TEACHER_PASSWORD（须与 .env 的 SEED_TEACHER_PASSWORD 一致）}"
: "${STUDENT_A_PASSWORD:?请设置 STUDENT_A_PASSWORD（须与 .env 的 SEED_STUDENT_A_PASSWORD 一致）}"
: "${STUDENT_B_PASSWORD:?请设置 STUDENT_B_PASSWORD（须与 .env 的 SEED_STUDENT_B_PASSWORD 一致）}"

EVIDENCE_DIR=${EVIDENCE_DIR:-docs/evidence}
mkdir -p "$EVIDENCE_DIR"
UPLOAD_FILE=$(mktemp -t campusclaw-verify.XXXXXX.md)
trap 'rm -f "$UPLOAD_FILE"' EXIT
# 自带一份材料再检索：结论只依赖本次上传的内容，不受既有种子数据影响。
cat > "$UPLOAD_FILE" <<EOF
# 验收材料

由 verify.sh 在 $(date '+%F %T') 上传。

## 考试安排

期末考试安排在第十七周的周三上午，补考安排在第十八周的周五下午。

## 磁铁实验

科学角准备了条形磁铁、铁钉、铜片和木块。把磁铁靠近这些物品时，铁钉会被磁铁吸住，
木块、塑料和铜片不会被吸住。隔着一张纸仍然可以吸住铁钉，只是力量会弱一些。
同名磁极相对会互相排斥，异名磁极相对会互相吸引。

## 种植记录

种植箱里种了绿豆和向日葵。播种前先把土壤浇湿，挖约两厘米深的小坑，每个小坑放两到三粒种子。
绿豆大约三天出芽，向日葵大约五到七天出芽。
EOF

# post TOKEN JSON PATH：携带 Bearer 凭证的 JSON 请求。
post() { curl -sS -i -H "Authorization: Bearer $1" -H 'Content-Type: application/json' -d "$2" "${BASE_URL}$3"; }
# login USER PASS：登录并把完整响应（含 token 对）写到 stdout。
login() { curl -sS -H 'Content-Type: application/json' -d "{\"username\":\"$1\",\"password\":\"$2\"}" "$BASE_URL/api/login"; }
# 从登录响应中提取字段（access/refresh 都是 base64url 字符，双引号定界）。
field() { printf '%s' "$1" | sed -n "s/.*\"$2\":\"\([^\"]*\)\".*/\1/p" | head -1; }

# ---- 身份与权限（第 2、3 次课的回归 + JWT 签发）----
curl -sS -i "$BASE_URL/health" | tee "$EVIDENCE_DIR/01-health.txt"
LOGIN_BODY=$(login teacher-a "$TEACHER_PASSWORD")
printf 'POST /api/login\n\n%s\n' "$LOGIN_BODY" > "$EVIDENCE_DIR/02-teacher-login-http.txt"
TEACHER_TOKEN=$(field "$LOGIN_BODY" access_token)
TEACHER_REFRESH=$(field "$LOGIN_BODY" refresh_token)
[ -n "$TEACHER_TOKEN" ] && [ -n "$TEACHER_REFRESH" ] || { echo "登录响应缺少 token 对" >&2; exit 1; }
curl -sS -i -H "Authorization: Bearer $TEACHER_TOKEN" "$BASE_URL/api/me" | tee "$EVIDENCE_DIR/03-teacher-me-http.txt"
curl -sS -i -H "Authorization: Bearer $TEACHER_TOKEN" -F "file=@$UPLOAD_FILE;filename=verify.md;type=text/markdown" -F 'title=课程验收材料' "$BASE_URL/api/materials" | tee "$EVIDENCE_DIR/04-teacher-upload-http.txt"
curl -sS -i -H "Authorization: Bearer $TEACHER_TOKEN" "$BASE_URL/api/materials?q=%E9%AA%8C%E6%94%B6" | tee "$EVIDENCE_DIR/05-materials-http.txt"
STUDENT_BODY=$(login student-a1 "$STUDENT_A_PASSWORD")
printf 'POST /api/login\n\n%s\n' "$STUDENT_BODY" > "$EVIDENCE_DIR/06-student-login-http.txt"
STUDENT_TOKEN=$(field "$STUDENT_BODY" access_token)
curl -sS -i -H "Authorization: Bearer $STUDENT_TOKEN" -F "file=@$UPLOAD_FILE;filename=student.md;type=text/markdown" "$BASE_URL/api/materials" | tee "$EVIDENCE_DIR/07-student-upload-403.txt"
curl -sS -i "$BASE_URL/api/materials" | tee "$EVIDENCE_DIR/08-anonymous-401.txt"

# ---- 三条检索路径 ----
post "$TEACHER_TOKEN" '{"q":"磁铁能吸住什么","mode":"keyword"}' /api/search | tee "$EVIDENCE_DIR/09-search-keyword.txt"
post "$TEACHER_TOKEN" '{"q":"哪些东西会被磁铁吸引","mode":"vector"}' /api/search | tee "$EVIDENCE_DIR/10-search-vector.txt"
post "$TEACHER_TOKEN" '{"q":"磁铁能吸住什么","mode":"hybrid"}' /api/search | tee "$EVIDENCE_DIR/11-search-hybrid.txt"

# ---- 跨班级隔离：B 班账号检索 A 班的磁铁内容应当无命中 ----
B_BODY=$(login student-b1 "$STUDENT_B_PASSWORD")
printf 'POST /api/login\n\n%s\n' "$B_BODY" > "$EVIDENCE_DIR/12-student-b-login.txt"
post "$(field "$B_BODY" access_token)" '{"q":"磁铁能吸住什么","mode":"hybrid"}' /api/search | tee "$EVIDENCE_DIR/13-search-cross-class.txt"

# ---- 无依据时不调用生成模型：问一句资料里没有的事 ----
post "$TEACHER_TOKEN" '{"q":"请告诉我火星移民的票价是多少"}' /api/ask | tee "$EVIDENCE_DIR/14-ask-no-evidence.txt"

# ---- 请求体里伪造班级编号，结果仍应限定在本班 ----
post "$TEACHER_TOKEN" '{"q":"磁铁能吸住什么","mode":"hybrid","class_id":999}' /api/search | tee "$EVIDENCE_DIR/15-search-spoofed-class.txt"

# ---- MySQL 与向量库对照、按策略重建索引 ----
MATERIAL_ID=$(grep -o '"id":[0-9]*' "$EVIDENCE_DIR/05-materials-http.txt" | head -1 | grep -o '[0-9]*')
curl -sS -i -H "Authorization: Bearer $TEACHER_TOKEN" "$BASE_URL/api/materials/$MATERIAL_ID/vectors" | tee "$EVIDENCE_DIR/16-chunk-vectors.txt"
post "$TEACHER_TOKEN" '{"chunk_strategy":"custom","chunk_separator":"blankline","chunk_max_chars":600,"chunk_overlap_percent":10}' "/api/materials/$MATERIAL_ID/reindex" | tee "$EVIDENCE_DIR/17-reindex-custom.txt"

# ---- 前端挂在 /week04 前缀下 ----
curl -sS -i "$BASE_URL/week04/" | tee "$EVIDENCE_DIR/18-week04-spa.txt"

# ---- JWT Bearer：续期轮换、伪造拒绝、登出吊销 ----
# 用 refresh_token 换新 token 对；随后重放旧串，必须 401（轮换生效）。
REFRESH_BODY=$(curl -sS -i -H 'Content-Type: application/json' -d "{\"refresh_token\":\"$TEACHER_REFRESH\"}" "$BASE_URL/api/token/refresh")
printf 'POST /api/token/refresh\n\n%s\n' "$REFRESH_BODY" > "$EVIDENCE_DIR/19-refresh-rotation.txt"
NEW_REFRESH=$(field "$(printf '%s' "$REFRESH_BODY" | tail -1)" refresh_token)
curl -sS -i -H 'Content-Type: application/json' -d "{\"refresh_token\":\"$TEACHER_REFRESH\"}" "$BASE_URL/api/token/refresh" > "$EVIDENCE_DIR/19b-replayed-refresh-401.txt" || true
# 篡改 payload 的 token（签名不匹配）必须 401。
FORGED="${TEACHER_TOKEN%.*}.eyJzdWIiOjk5OSwiY2xhc3NfaWQiOjk5OX0.c2lnbmF0dXJlLWZvcmdlZA"
curl -sS -i -H "Authorization: Bearer $FORGED" "$BASE_URL/api/me" > "$EVIDENCE_DIR/20-forged-token-401.txt" || true
# 登出吊销 refresh：登出后再续期必须 401。
curl -sS -i -H "Authorization: Bearer $TEACHER_TOKEN" -H 'Content-Type: application/json' -d "{\"refresh_token\":\"$NEW_REFRESH\"}" "$BASE_URL/api/logout" > "$EVIDENCE_DIR/21-logout-revokes-refresh.txt" || true
curl -sS -i -H 'Content-Type: application/json' -d "{\"refresh_token\":\"$NEW_REFRESH\"}" "$BASE_URL/api/token/refresh" > "$EVIDENCE_DIR/21b-refresh-after-logout-401.txt" || true

# ---- 断言 ----
grep -q '200 OK' "$EVIDENCE_DIR/01-health.txt"
grep -q '"token_type":"Bearer"' "$EVIDENCE_DIR/02-teacher-login-http.txt"
grep -q '"access_token":"eyJ' "$EVIDENCE_DIR/02-teacher-login-http.txt"
if grep -q 'Set-Cookie' "$EVIDENCE_DIR/02-teacher-login-http.txt"; then echo "登录不应再下发 Cookie" >&2; exit 1; fi
grep -q '"role":"teacher"' "$EVIDENCE_DIR/03-teacher-me-http.txt"
grep -q '201 Created' "$EVIDENCE_DIR/04-teacher-upload-http.txt"
grep -q '课程验收材料' "$EVIDENCE_DIR/05-materials-http.txt"
grep -q '403 Forbidden' "$EVIDENCE_DIR/07-student-upload-403.txt"
grep -q '401 Unauthorized' "$EVIDENCE_DIR/08-anonymous-401.txt"

# 三条路径都应命中，并带上可回溯的段落位置。
for f in 09-search-keyword 10-search-vector 11-search-hybrid; do
  grep -q '200 OK' "$EVIDENCE_DIR/$f.txt"
  grep -q '"status":"ok"' "$EVIDENCE_DIR/$f.txt"
  grep -q '"start_offset"' "$EVIDENCE_DIR/$f.txt"
done
# 向量与混合模式会回显检索用到的问句向量。
grep -q '"query_vector":\[' "$EVIDENCE_DIR/10-search-vector.txt"
grep -q '"query_vector":\[' "$EVIDENCE_DIR/11-search-hybrid.txt"

# 跨班级：B 班检索 A 班内容为无依据，且不带任何命中。
grep -q '"status":"no_evidence"' "$EVIDENCE_DIR/13-search-cross-class.txt"
grep -q '资料中未找到相关内容' "$EVIDENCE_DIR/13-search-cross-class.txt"

# 无依据：不调用生成模型，直接给固定文案，出处为空。
grep -q '200 OK' "$EVIDENCE_DIR/14-ask-no-evidence.txt"
grep -q '资料中未找到相关内容' "$EVIDENCE_DIR/14-ask-no-evidence.txt"
grep -q '"citations":\[\]' "$EVIDENCE_DIR/14-ask-no-evidence.txt"

# 伪造的班级编号不改变检索范围：仍然只命中本班，且响应里不出现 999。
grep -q '"status":"ok"' "$EVIDENCE_DIR/15-search-spoofed-class.txt"
if grep -q '"class_id":999' "$EVIDENCE_DIR/15-search-spoofed-class.txt"; then echo "出现了伪造班级的数据" >&2; exit 1; fi

# 两库对照：至少有一个切片同时存在于 MySQL 与 Qdrant，且主键对齐。
grep -q '"linked":true' "$EVIDENCE_DIR/16-chunk-vectors.txt"
grep -q '200 OK' "$EVIDENCE_DIR/17-reindex-custom.txt"
grep -q '"chunk_strategy":"custom"' "$EVIDENCE_DIR/17-reindex-custom.txt"
grep -q '200 OK' "$EVIDENCE_DIR/18-week04-spa.txt"

# JWT 生命周期：续期返回新 token 对；旧串重放、伪造签名、登出后续期都是 401。
grep -q '200 OK' "$EVIDENCE_DIR/19-refresh-rotation.txt"
grep -q '"access_token":"eyJ' "$EVIDENCE_DIR/19-refresh-rotation.txt"
grep -q '401' "$EVIDENCE_DIR/19b-replayed-refresh-401.txt"
grep -q '401' "$EVIDENCE_DIR/20-forged-token-401.txt"
grep -q '401' "$EVIDENCE_DIR/21b-refresh-after-logout-401.txt"

echo "关键 Scenario 已通过；完整 HTTP 证据位于 $EVIDENCE_DIR"
