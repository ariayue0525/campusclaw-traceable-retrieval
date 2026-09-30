# Delta Spec: 认证凭证（JWT Bearer）

## ADDED Requirements

### Requirement: 登录签发 token 对

系统 SHALL 在 `POST /api/login` 口令校验通过后返回 JSON：
`token_type`（固定 `"Bearer"`）、`access_token`（HS256 签名的 JWT）、
`expires_in`（access 剩余秒数）、`refresh_token`（随机串）与 `user` 对象。
响应 SHALL NOT 再设置任何认证 Cookie。

#### Scenario: 登录返回可用凭证

- **WHEN** 使用正确口令调用 `POST /api/login`
- **THEN** 响应 200，`access_token` 为三段 base64url 的 JWT，
  `refresh_token` 非空，`user` 含实时角色与班级

### Requirement: 受保护接口接受 Bearer 头

所有受保护接口 SHALL 从 `Authorization: Bearer <token>` 读取凭证，
校验 JWT 签名（HS256，密钥为 `SESSION_SECRET`）与 `exp`（允许 30 秒时钟偏移），
再按 `sub` 从数据库读取实时用户身份；缺失/伪造/过期凭证 SHALL 返回 401。
授权（角色、班级）SHALL 以数据库实时结果为准， SHALL NOT 信任 payload 中的
`class_id`/`role` 之外的自报值。

#### Scenario: 伪造签名被拒绝

- **WHEN** 携带签名与密钥不匹配的 JWT 请求 `GET /api/me`
- **THEN** 返回 401

#### Scenario: 过期 token 被拒绝

- **WHEN** 携带 `exp` 早于当前时间的合法签名 JWT 请求受保护接口
- **THEN** 返回 401

#### Scenario: 无凭证访问

- **WHEN** 不带 Authorization 头请求受保护接口
- **THEN** 返回 401「请先登录」

### Requirement: refresh token 轮换

系统 SHALL 提供 `POST /api/token/refresh`：请求体携带 `refresh_token`，
校验其 SHA-256 摘要存在于 `refresh_tokens` 且未过期，在同一事务中
删除旧记录并写入新记录（轮换），返回与登录相同结构的 token 对。
无效、过期或已轮换的 refresh_token SHALL 返回 401。

#### Scenario: 续期成功

- **WHEN** 用有效的 refresh_token 调用 `/api/token/refresh`
- **THEN** 返回新的 access_token 与 refresh_token，旧 refresh_token 立即失效

#### Scenario: 重放旧 refresh_token 被拒绝

- **WHEN** 同一 refresh_token 第二次用于续期
- **THEN** 返回 401

### Requirement: 登出吊销 refresh token

`POST /api/logout` SHALL 删除请求体所携带 refresh_token 的库记录，
使后续续期失败；access_token SHALL 依靠短时效自然失效。

#### Scenario: 登出后无法续期

- **WHEN** 登出后用同一 refresh_token 调用 `/api/token/refresh`
- **THEN** 返回 401

### Requirement: 配置

token 时效 SHALL 可由环境变量配置：`TOKEN_TTL`（access，默认 30m）、
`REFRESH_TTL`（refresh，默认 168h）。签名密钥复用 `SESSION_SECRET`。

#### Scenario: 缺少必需配置启动失败

- **WHEN** `SESSION_SECRET` 缺失或短于 32 字符时启动
- **THEN** 进程报错退出
