# CampusClaw · 教研材料库

价值：用可审计的身份、角色与班级边界，集中管理班级教研材料，并支持**限定班级范围、可溯源的知识库检索**。

场景：教师上传 `.txt` / `.md` 材料，服务端切分、嵌入并写入 Qdrant；同班师生可用关键字、向量、混合三种方式检索，或用带 `[1][2]` 出处标注的问答查看原文段落。

不做：本迭代不实现作业提交/批改、注册找回密码、跨班共享或生产高可用。

## 一键启动

1. 复制环境变量并填写所有 `CHANGE_ME`：

   ```bash
   cp .env.example .env
   ```

2. 启动四服务（唯一发布形态：MySQL + Go API + Qdrant + Nginx）：

   ```bash
   docker compose up --build -d
   docker compose ps
   ```

   > 若所在网络无法直连 Docker Hub，可先 `docker pull docker.m.daocloud.io/qdrant/qdrant:v1.12.1`
   > 再 `docker tag` 成 `qdrant/qdrant:v1.12.1`；构建后端镜像时可用
   > `DOCKER_BUILDKIT=0 docker compose build --build-arg GOPROXY=https://goproxy.cn,direct api`。

3. 浏览器打开 <http://localhost:8080>（根目录会跳转到 `/week04/`）。健康检查：

   ```bash
   curl -i http://localhost:8080/health
   ```

预置账号由 `.env` 决定，默认用户名是 `teacher-a`、`student-a1`、`student-b1`，口令必须由使用者自行填写。首次启动会写入 A/B 两班、三个用户和两条可区分材料；种子过程幂等。

停止服务但保留数据库与上传材料：`docker compose down`。只有明确要删除所有课程演示数据时才使用 `docker compose down -v`。

## 开发与验收

```bash
# 前端
cd frontend && npm ci && npm test && npm run build

# 后端（本机需要 Go 1.24；没有 Go 可在容器中运行）
docker build --target test -f backend/Dockerfile backend

# OpenSpec
openspec validate add-auth-rbac-class-knowledge --strict
openspec validate add-traceable-vector-retrieval --strict
openspec validate add-jwt-bearer-auth --strict

# 启动后逐项产生作业证据
./scripts/verify.sh
```

完整接口、测试账号、作业问题答案与截图方法见 [docs/作业提交说明.md](docs/作业提交说明.md)。

## 检索是怎么走的

| 模式 | 路径 | 说明 |
| --- | --- | --- |
| `keyword` | MySQL ngram 全文索引 → 回表取正文 | 不嵌入、不访问向量库，字面完全一致的片段排前 |
| `vector` | 问句嵌入 → Qdrant 按班过滤 → 回表取正文 | 低于相似度阈值的候选丢弃，同义改写也能召回 |
| `hybrid` | 两条路径各取前 N → RRF 融合（k=60） | 默认模式，字面匹配与语义相近同时生效 |

三条路径的正文都取自 MySQL 的 `knowledge_chunks.chunk_text`；Qdrant 只保存浮点向量与标识
（`chunk_id` / `class_id` / `material_id`），向量主键等于 `knowledge_chunks.id`，两库由此对齐。
班级编号只来自登录会话，请求体里的 `class_id` 一律丢弃；没有命中切片时问答直接返回
「资料中未找到相关内容」，不调用生成模型。

## 安全边界

- 浏览器只访问 Nginx 暴露的 `8080`；MySQL、Qdrant 与上传目录都在 internal 网络内，不映射宿主端口。
- 认证、角色授权、班级过滤、上传校验和下载鉴权都在 Go API 中完成。
- 账号不存在与口令错误统一返回 `401 {"error":"用户名或密码错误"}`。
- `/health` 只表示进程存活，不查询数据库；数据库不可用时业务接口返回 `503`。
- 密码只以 bcrypt 哈希存储；`.env`、数据库文件与上传文件均不提交 Git。

