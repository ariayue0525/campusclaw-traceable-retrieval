# Proposal: add-jwt-bearer-auth

## Why

当前登录认证是「服务端有状态会话 + HttpOnly Cookie 承载的不透明随机 token」：
登录后生成 32 字节随机 token 存入 `sessions` 表，浏览器凭 `campusclaw_session` Cookie 访问，
每次请求都要查库换取用户身份。课程示例（第 4 课参考实现）要求浏览器请求携带
`Authorization: Bearer <JWT>`，前端可见并可控凭证，与现有 Cookie 方案不兼容。

## What Changes

- 登录成功改为签发一对 token：
  - **access_token**：HS256 签名的 JWT，payload 含 `sub`（user_id）、`username`、`class_id`、`role`、`iat`、`exp`，短时效（默认 30 分钟），服务端无状态校验签名与过期；
  - **refresh_token**：32 字节随机串，服务端存 SHA-256 摘要到新表 `refresh_tokens`，长时效（默认 7 天），支持吊销与轮换。
- 新增 `POST /api/token/refresh`：用 refresh_token 换取新的一对 token（轮换：旧 refresh 立即作废）。
- 所有受保护接口从「读 Cookie 查 sessions 表」改为「解析 `Authorization: Bearer` → 校验 JWT 签名与 exp → 按 `sub` 回库读取实时角色与班级」。
- `POST /api/logout` 吊销 refresh_token；access_token 靠短时效自然失效。
- 移除 Cookie 下发与 `sessions` 表读取路径（表保留，不再使用）。

## Capabilities

### 修改
- `auth-session`（隐含于既有行为，本次首次显式成文）：认证凭证从 Cookie 会话改为 JWT Bearer。

## Impact

- 后端：`internal/auth`（新增）、`internal/server`、`internal/store`、`internal/config`。
- 前端：token 存 localStorage、请求带头、401 静默续期重试、文件下载改为 fetch+blob。
- 验收脚本与文档同步。

## Non-goals

- 不做 OAuth2 / 第三方登录、不做多设备会话管理界面。
- 不做 JWT 黑名单（access_token 短时效 + refresh 可吊销已覆盖撤销语义）。
- 不改检索、班级隔离与溯源逻辑：班级边界仍然只来自服务端回库结果，不信任 JWT payload 中的 `class_id` 之外的自报值。
