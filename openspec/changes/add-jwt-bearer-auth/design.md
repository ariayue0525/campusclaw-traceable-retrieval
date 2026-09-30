# Design: add-jwt-bearer-auth

## Context

现状：登录 → 随机 token 存 `sessions` 表 → HttpOnly Cookie → 每请求查库。
目标：与课程示例一致的 `Authorization: Bearer <JWT>`，前端持有凭证。

## 关键决策

### 1. HS256 JWT 自实现（不引第三方库）

签名验证就是 `HMAC-SHA256(base64url(header) + "." + base64url(payload), SESSION_SECRET)`，
加上 base64url 解码与 exp 检查，约 80 行、零新依赖、可单测。
复用现有 `SESSION_SECRET`（≥32 字符）作为签名密钥，不新增密钥配置。

### 2. 班级与角色仍然回库读取（安全边界不动）

JWT payload 里虽然带 `class_id`/`role`（与课程示例一致），但 `requireAuth`
校验签名与过期后，**按 `sub` 重新从 users/classes 表读取实时身份**。
理由：令牌签发后的角色变更、班级调整要立即生效；payload 只用于证明
「这个 user_id 在签发时刻通过了口令校验」，不作为授权依据。
因此每次请求仍有一次用户表查询——这点与旧方案相同，换取的是
「改角色即刻生效 + 服务端可审计」。

### 3. refresh_token 轮换

- 存储与旧 sessions 一致：库里只存 `SHA-256(随机串)`，泄露库不泄露凭证。
- `POST /api/token/refresh` 校验通过后在同一事务里删除旧行、写入新行（轮换）；
  已被使用的旧 refresh_token 再次出现直接 401。
- `logout` 删除 refresh_token 行；access_token 靠 30 分钟短时效自然过期。

### 4. 前端存储

access_token 与 refresh_token 存 localStorage（课程演示环境无严格 CSP 要求，
实现简单、刷新页面可直接恢复；XSS 风险以短时效缓解并在文档注明）。
请求统一经过 `request()` 包装附加 `Authorization` 头；收到 401 且尚未重试过时，
先用 refresh_token 静默续期一次再重放原请求，失败则清空凭证回登录页。

### 5. 文件下载不能走 `<a href>`

`<a>` 标签无法携带 Authorization 头。下载改为 `fetch` + `blob` +
`Content-Disposition` 解析文件名后触发保存。

## Risks / Trade-offs

- localStorage 存 token 有 XSS 面 → access 30 分钟过期 + refresh 可服务端吊销；
- 每请求一次回库（同旧方案）→ 换取实时授权，暂不做缓存；
- 时钟偏移 → 验证允许 `leeway = 30s`。

## Migration

一次切换，不做双轨：旧的 Cookie 读取路径删除，`sessions` 表保留但不再读写
（避免迁移脚本；部署后旧 Cookie 自然失效，用户重新登录即可）。
