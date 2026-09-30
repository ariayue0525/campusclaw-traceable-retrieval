# Tasks: add-jwt-bearer-auth

## 1. 后端

- [x] 1.1 `internal/auth/jwt.go`：HS256 签发/校验（header 校验、exp + 30s leeway），单元测试覆盖正常、伪造签名、过期、错算法
- [x] 1.2 `internal/config`：新增 `TOKEN_TTL`（默认 30m）、`REFRESH_TTL`（默认 168h），测试更新
- [x] 1.3 `internal/store`：`refresh_tokens` 表（token_hash UNIQUE、user_id、expires_at）、创建/事务轮换/删除/按摘要查用户/按 ID 查用户
- [x] 1.4 `internal/server`：login 返回 token 对（无 Cookie）、`requireAuth` 解析 Bearer + 回库取实时身份、新增 `POST /api/token/refresh`、logout 吊销 refresh、移除 Cookie 路径

## 2. 前端

- [x] 2.1 `api.ts`：localStorage 存取 token、请求附加 Authorization、401 静默续期一次并重放、上传 XHR 带头、下载改 fetch+blob
- [x] 2.2 `App.tsx`：下载入口从 `<a href>` 改为按钮调用带头下载
- [x] 2.3 `npm test` 与 `npm run build` 通过

## 3. 验收与文档

- [x] 3.1 `verify.sh` 改为 Bearer 流程（登录取 token、续期、吊销、伪造/过期 401），18 项证据重跑通过
- [x] 3.2 README、作业提交说明、.env.example 同步 token 方案
- [x] 3.3 `openspec validate add-jwt-bearer-auth --strict` 通过
