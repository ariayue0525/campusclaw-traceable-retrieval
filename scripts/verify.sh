#!/usr/bin/env sh
# 端到端验收：把关键场景的真实 HTTP 报文写入 docs/evidence，并对关键结论做断言。
# 检索相关的场景覆盖三条路径、跨班隔离、无依据不作答，以及 MySQL 与向量库的对照。
set -eu

BASE_URL=${BASE_URL:-http://localhost:8080}
: "${TEACHER_PASSWORD:?请设置 TEACHER_PASSWORD（须与 .env 的 SEED_TEACHER_PASSWORD 一致）}"
: "${STUDENT_A_PASSWORD:?请设置 STUDENT_A_PASSWORD（须与 .env 的 SEED_STUDENT_A_PASSWORD 一致）}"
: "${STUDENT_B_PASSWORD:?请设置 STUDENT_B_PASSWORD（须与 .env 的 SEED_STUDENT_B_PASSWORD 一致）}"

EVIDENCE_DIR=${EVIDENCE_DIR:-docs/evidence}
mkdir -p "$EVIDENCE_DIR"
TEACHER_JAR=$(mktemp)
STUDENT_JAR=$(mktemp)
STUDENT_B_JAR=$(mktemp)
UPLOAD_FILE=$(mktemp -t campusclaw-verify.XXXXXX.md)
trap 'rm -f "$TEACHER_JAR" "$STUDENT_JAR" "$STUDENT_B_JAR" "$UPLOAD_FILE"' EXIT
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

post() { curl -sS -i -b "$1" -H 'Content-Type: application/json' -d "$2" "${BASE_URL}$3"; }

# ---- 身份与权限（第 2、3 次课的回归）----
curl -sS -i "$BASE_URL/health" | tee "$EVIDENCE_DIR/01-health.txt"
curl -sS -i -c "$TEACHER_JAR" -H 'Content-Type: application/json' -d "{\"username\":\"teacher-a\",\"password\":\"$TEACHER_PASSWORD\"}" "$BASE_URL/api/login" | tee "$EVIDENCE_DIR/02-teacher-login-http.txt"
curl -sS -i -b "$TEACHER_JAR" "$BASE_URL/api/me" | tee "$EVIDENCE_DIR/03-teacher-me-http.txt"
curl -sS -i -b "$TEACHER_JAR" -F "file=@$UPLOAD_FILE;filename=verify.md;type=text/markdown" -F 'title=课程验收材料' "$BASE_URL/api/materials" | tee "$EVIDENCE_DIR/04-teacher-upload-http.txt"
curl -sS -i -b "$TEACHER_JAR" "$BASE_URL/api/materials?q=%E9%AA%8C%E6%94%B6" | tee "$EVIDENCE_DIR/05-materials-http.txt"
curl -sS -i -c "$STUDENT_JAR" -H 'Content-Type: application/json' -d "{\"username\":\"student-a1\",\"password\":\"$STUDENT_A_PASSWORD\"}" "$BASE_URL/api/login" | tee "$EVIDENCE_DIR/06-student-login-http.txt"
curl -sS -i -b "$STUDENT_JAR" -F "file=@$UPLOAD_FILE;filename=student.md;type=text/markdown" "$BASE_URL/api/materials" | tee "$EVIDENCE_DIR/07-student-upload-403.txt"
curl -sS -i "$BASE_URL/api/materials" | tee "$EVIDENCE_DIR/08-anonymous-401.txt"

# ---- 三条检索路径 ----
post "$TEACHER_JAR" '{"q":"磁铁能吸住什么","mode":"keyword"}' /api/search | tee "$EVIDENCE_DIR/09-search-keyword.txt"
post "$TEACHER_JAR" '{"q":"哪些东西会被磁铁吸引","mode":"vector"}' /api/search | tee "$EVIDENCE_DIR/10-search-vector.txt"
post "$TEACHER_JAR" '{"q":"磁铁能吸住什么","mode":"hybrid"}' /api/search | tee "$EVIDENCE_DIR/11-search-hybrid.txt"

# ---- 跨班级隔离：B 班账号检索 A 班的磁铁内容应当无命中 ----
curl -sS -i -c "$STUDENT_B_JAR" -H 'Content-Type: application/json' -d "{\"username\":\"student-b1\",\"password\":\"$STUDENT_B_PASSWORD\"}" "$BASE_URL/api/login" > "$EVIDENCE_DIR/12-student-b-login.txt"
post "$STUDENT_B_JAR" '{"q":"磁铁能吸住什么","mode":"hybrid"}' /api/search | tee "$EVIDENCE_DIR/13-search-cross-class.txt"

# ---- 无依据时不调用生成模型：问一句资料里没有的事 ----
post "$TEACHER_JAR" '{"q":"请告诉我火星移民的票价是多少"}' /api/ask | tee "$EVIDENCE_DIR/14-ask-no-evidence.txt"

# ---- 请求体里伪造班级编号，结果仍应限定在本班 ----
post "$TEACHER_JAR" '{"q":"磁铁能吸住什么","mode":"hybrid","class_id":999}' /api/search | tee "$EVIDENCE_DIR/15-search-spoofed-class.txt"

# ---- MySQL 与向量库对照、按策略重建索引 ----
MATERIAL_ID=$(grep -o '"id":[0-9]*' "$EVIDENCE_DIR/05-materials-http.txt" | head -1 | grep -o '[0-9]*')
curl -sS -i -b "$TEACHER_JAR" "$BASE_URL/api/materials/$MATERIAL_ID/vectors" | tee "$EVIDENCE_DIR/16-chunk-vectors.txt"
post "$TEACHER_JAR" '{"chunk_strategy":"custom","chunk_separator":"blankline","chunk_max_chars":600,"chunk_overlap_percent":10}' "/api/materials/$MATERIAL_ID/reindex" | tee "$EVIDENCE_DIR/17-reindex-custom.txt"

# ---- 前端挂在 /week04 前缀下 ----
curl -sS -i "$BASE_URL/week04/" | tee "$EVIDENCE_DIR/18-week04-spa.txt"

grep -q '200 OK' "$EVIDENCE_DIR/01-health.txt"
grep -q 'HttpOnly' "$EVIDENCE_DIR/02-teacher-login-http.txt"
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

echo "关键 Scenario 已通过；完整 HTTP 证据位于 $EVIDENCE_DIR"
