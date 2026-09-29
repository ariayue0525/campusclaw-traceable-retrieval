## Why

第 3 课已经把材料正文放进 `knowledge_entries`，但它只能整篇读取：师生想知道「这句话出自哪一份材料的哪一段」时，只能靠人眼通读。教研场景中，同一件事在不同材料里往往用不同措辞表述，只靠 `LIKE` 匹配会漏掉语义相同而用词不同的片段，也无法给出可核对的出处。

本变更在同一套系统上追加检索能力：以一句自然语言在本班材料中定位相关片段，并把命中结果回溯到具体材料的切片序号与字符区间。当本班材料中不存在依据时，必须明确回答「资料中未找到相关内容」，不得以弱相关切片凑数，也不得让生成模型凭参数记忆作答。

## What Changes

- 教师上传 `.txt` / `.md` 成功后，正文在服务端被切分为切片并做向量嵌入；切片正文写入 MySQL `knowledge_chunks`，浮点向量写入 Qdrant，向量主键等于 `knowledge_chunks.id`。
- 提供 `auto`（约 800 字窗口、重叠 80 字、优先在空行/换行/句号处断开）、`custom`（长度 100–2000、重叠 0%–50%、自定义分隔符、可选移除 URL/邮箱与折叠空白）、`hierarchy`（按 Markdown `#` / `##` / `###` 分章，过长再按窗口切）三种切分策略。
- 提供 `keyword`（仅 MySQL ngram 全文索引）、`vector`（问句嵌入后在 Qdrant 按班级过滤，余弦低于阈值丢弃）、`hybrid`（两路各自过滤后按名次执行 RRF，k = 60）三种检索模式，默认 `hybrid`。
- 每条命中都给出材料标题、切片序号、字符区间与摘录，并支持打开对应材料；摘录取自 MySQL 的 `chunk_text`，不取自向量库。
- `POST /api/ask` 先以混合模式取本班前 4 条切片；有切片才调用对话网关生成带 `[1]`、`[2]` 标注的简短回答；无切片直接返回固定文案，`citations` 为空且不调用对话网关。
- 班级标识仅从登录会话读取；查询参数、JSON 正文与请求头中携带的班级编号一律丢弃；MySQL 与 Qdrant 两条路径都按会话班级过滤，回表时再次核对。
- 教师可按新策略重建索引：先删除旧切片与旧向量，再按本次请求策略重新写入；原文 `body_text` 不被改写。

## Capabilities

### New Capabilities

- `knowledge-retrieval`: 切分与重建索引、向量嵌入与入库、三类检索模式、班级隔离下的检索边界、可回溯出处、有依据才生成的问答。

### Modified Capabilities

- `auth-upload`: 上传成功后追加建索引步骤，响应增加 `chunk_count` 与 `chunk_strategy`；材料详情接口增加切片对照数据。上传鉴权、类型校验与事务语义不变。

## Impact

- 新增 `backend/internal/chunk/`、`backend/internal/embed/`、`backend/internal/vector/`、`backend/internal/retrieval/` 四个包，以及 `knowledge_chunks` 表与 ngram 全文索引。
- `docker-compose.yml` 新增 `qdrant` 服务（仅接入 internal 网络，不映射宿主端口）；MySQL 启动时固定 `ngram_token_size=2`。
- Nginx 增加 `/week04` 前缀：静态资源与 `/week04/api` 同源代理，原 `/api` 路径保留。
- 前端新增检索页与问答页，材料详情增加切片与向量对照面板，教师可见重建索引入口。
- 新增环境变量：`QDRANT_URL`、`QDRANT_COLLECTION`、`EMBED_PROVIDER`、`LLM_GATEWAY_BASE_URL`、`LLM_GATEWAY_API_KEY`、`EMBEDDING_MODEL`、`EMBED_DIM`、`CHAT_MODEL`、`VECTOR_MIN_SCORE`、`RRF_K`。

## Non-goals

- 不做流式输出、多轮会话状态存储与 Agent 编排（第 5 课）。
- 不做交叉编码器重排序，不引入 LangChain / LlamaIndex 一类编排框架。
- 不做 OCR、PDF/DOCX 解析；仍然只接受 `.txt` / `.md`。
- 不做跨班级检索、全班合并视图或平台级超级管理员。
- 不做作业提交、批改、成绩与错题本。
- 不把 Qdrant 端口或两类模型网关暴露给浏览器，不把密钥下发到页面。
- 不以隐藏按钮代替服务端班级过滤，不以 403/404 暗示他班资料存在。
