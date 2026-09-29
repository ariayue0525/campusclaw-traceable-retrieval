## 1. 规约与配置
- [x] 1.1 建立 `add-traceable-vector-retrieval` 四件套（verify: `openspec validate add-traceable-vector-retrieval --strict`）
- [x] 1.2 扩展配置：Qdrant、嵌入网关、对话网关、阈值与 RRF 参数（verify: 配置测试）

## 2. 数据与切分
- [x] 2.1 建立 `knowledge_chunks` 表、ngram 全文索引与就绪状态字段（verify: schema 初始化测试）
- [x] 2.2 实现 `auto` / `custom` / `hierarchy` 三种切分策略与预处理（verify: 切分单元测试）
- [x] 2.3 种子材料扩为多切片正文，启动时按 `auto` 补齐索引（verify: 重启后切片数与向量数一致）

## 3. 嵌入与向量库
- [x] 3.1 实现 OpenAI 兼容嵌入客户端与本地确定性回退（verify: 嵌入单元测试与维度一致性）
- [x] 3.2 实现 Qdrant 集合创建、upsert、按班级过滤检索与删除（verify: 客户端单元测试 + 容器内集成）
- [x] 3.3 编排入库链路：切分 → 嵌入 → 写 MySQL → 写向量；失败标记 `failed`（verify: 故障注入测试）

## 4. 检索编排
- [x] 4.1 关键字路径：仅查 MySQL 全文索引并带班级与就绪条件（verify: 原词命中 / 改写落空）
- [x] 4.2 向量路径：问句嵌入 → Qdrant 班级过滤 → 阈值 → 回表核对班级（verify: 阈值与回表测试）
- [x] 4.3 混合路径：两路独立过滤后按名次 RRF（k = 60）融合，缺席不贡献（verify: RRF 单元测试）
- [x] 4.4 无候选统一返回 `no_evidence`，Qdrant 故障时vector/hybrid 返回 503（verify: 降级测试）

## 5. HTTP 接口
- [x] 5.1 `POST /api/search`：三模式、limit/offset、丢弃请求中的班级（verify: 接口测试 + 跨班负向测试）
- [x] 5.2 `POST /api/ask`：先混合检索取前 4 条，有切片才调用对话网关，无切片返回固定文案（verify: 无依据不调用网关的测试）
- [x] 5.3 `POST /api/materials/{id}/reindex`（教师）与 `GET /api/materials/{id}/vectors`（同班）（verify: 权限与跨班测试）
- [x] 5.4 上传成功后按请求切分参数自动建索引，响应带 `chunk_count` / `chunk_strategy`（verify: HTTP 验收）

## 6. 部署
- [x] 6.1 Compose 增加 qdrant 服务（仅 internal 网络），MySQL 固定 `ngram_token_size=2`（verify: `docker compose config` + `docker compose ps`）
- [x] 6.2 Nginx 增加 `/week04` 静态前缀与 `/week04/api` 代理，保留 `/api`（verify: 两条前缀均可访问）
- [x] 6.3 补充 `.env.example` 与 README 启动说明（verify: 空 `.env` 缺失项启动失败）

## 7. 前端
- [x] 7.1 Vite base 改为 `/week04/`，API 前缀同步（verify: production build）
- [x] 7.2 检索页：模式选择、示例问句、每条结果的路径与 score/rank 说明（verify: Vitest）
- [x] 7.3 问答页：[1] 标注与出处列表一致；材料详情的切片/向量对照与重建索引（verify: Vitest + build）

## 8. 验收与提交
- [x] 8.1 后端单测、前端测试与构建全部通过（verify: `docker build --target test` + `npm test`）
- [x] 8.2 `scripts/verify.sh` 产出三模式、跨班、无依据、503 与重建索引证据（verify: 脚本退出码 0）
- [x] 8.3 推送到公开仓库，`openspec validate add-traceable-vector-retrieval --strict` 退出码为 0（verify: 仓库可访问）
