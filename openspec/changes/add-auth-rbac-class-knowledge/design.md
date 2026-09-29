## Context

按第二阶段课件实现 CampusClaw 迭代 1。浏览器只访问 Nginx；Go 是唯一信任边界；MySQL 与上传目录不直接暴露。

## Goals / Non-Goals

Goals：登录、服务端会话、角色授权、班级隔离、原子上传入库、鉴权下载、可复现 Compose 与可复核证据。Non-goals 见 proposal。

## Decisions

### D1 前后端与请求链路

React 18 + TypeScript + Vite 构建静态 SPA；Nginx 提供静态资源并同源代理 `/api` 和 `/health`；Go `net/http` 访问 MySQL 8。浏览器不直连 API 容器或数据库。

### D2 服务端会话

登录生成 256-bit 随机 token，只将 token 放入 HttpOnly、SameSite=Lax Cookie；数据库只存 SHA-256 摘要。每次请求由 session 关联 user 表得到实时 `role` 与 `class_id`，登出删除服务端记录。

### D3 班级来源与跨班语义

所有班级信息只取自服务端会话关联的用户记录，不采信 query/body/header 中的班级。集合 SQL 强制 `WHERE class_id=?`；对象先取行再比较班级，跨班统一 404，避免泄露对象存在性。

### D4 口令与登录防护

口令 bcrypt 哈希；账号不存在也执行 dummy hash 比较；失败响应同形。按来源 IP + 用户名做进程内滑动窗口限流（演示单实例边界）。

### D5 上传白名单

仅 `.txt`、`.md`，扩展名不区分大小写；限制请求体与文件大小，要求 UTF-8；用随机存储名与 `filepath.Base`，从不使用用户路径。

### D6 上传失败无脏数据

先写同目录临时文件并解析，再开启事务写 `materials` 与 `knowledge_entries`，原子 rename 后提交；任何一步失败都会回滚两表并删除临时/最终文件。

### D7 下载必须鉴权

Nginx 不配置 `/uploads` alias。`GET /api/materials/{id}/file` 经会话和班级校验后由 Go 返回 `Content-Disposition: attachment`。

### D8 Compose 与持久化

web 同时接入可发布端口的 edge 网络与后端 internal 网络；仅 web 映射宿主端口，api、db 只在 internal 网络。MySQL 数据与上传目录使用 named volumes；db healthcheck + api 连接重试保证就绪。

### D9 健康检查

`GET /health` 只返回进程存活，不查询数据库。数据库异常由业务 API 返回 503，避免被误判为登录失效。

## Data model

- `classes(id,name)`
- `users(id,class_id,username,display_name,role,password_hash)`
- `sessions(token_hash,user_id,expires_at)`
- `materials(id,class_id,uploader_id,title,original_name,stored_name,media_type,size_bytes,created_at)`
- `knowledge_entries(id,material_id,class_id,content,created_at)`

## API

| 方法与路径 | 鉴权 | 语义 |
|---|---|---|
| POST `/api/login` | 无 | 登录并签发 Cookie |
| POST `/api/logout` | 会话 | 删除会话并清 Cookie |
| GET `/api/me` | 会话 | 返回身份、角色与班级 |
| GET `/api/materials?q=` | 会话 | 仅查本班列表/搜索 |
| GET `/api/materials/{id}` | 会话+班级 | 详情与正文；跨班 404 |
| GET `/api/materials/{id}/file` | 会话+班级 | 鉴权下载；跨班 404 |
| POST `/api/materials` | 教师 | multipart 上传入库 |
| GET `/health` | 无 | 存活探针 |
