## ADDED Requirements

### Requirement: R1 账号密码登录与会话

系统 MUST 以服务端会话 Cookie 维持教师和学生登录态，MUST NOT 使用 JWT/Bearer token。

#### Scenario: 正确凭据登录
- **WHEN** 预置用户提交正确用户名和口令
- **THEN** 返回 200、统一用户结构与 HttpOnly/SameSite=Lax 会话 Cookie

#### Scenario: 错误凭据同形失败
- **WHEN** 用户名不存在或口令错误
- **THEN** 两者均返回 401 和相同错误正文，不透露账号是否存在

#### Scenario: 未登录调用受保护 API
- **WHEN** 无有效 Cookie 请求 `/api/me` 或 `/api/materials`
- **THEN** 返回 401 且响应中不包含任何材料数据

#### Scenario: 登出使旧 Cookie 失效
- **WHEN** 已登录用户登出后再次使用旧 Cookie
- **THEN** 受保护 API 返回 401

### Requirement: R2 角色授权

系统 MUST 只允许教师上传，学生只能查看和下载本班材料；权限 MUST 在 API 执行。

#### Scenario: 学生上传被拒
- **WHEN** 学生会话提交一份合法 `.txt` 文件
- **THEN** 返回 403，材料表、知识库表和上传目录均不变化

### Requirement: R3 班级隔离

系统 MUST 从服务端身份取得班级并用于所有列表、详情和文件访问，MUST NOT 采信客户端班级标识。

#### Scenario: 跨班对象不可见
- **WHEN** A 班用户请求 B 班材料详情或文件 ID
- **THEN** 返回 404，响应不含 B 班标题、正文、文件名或存储路径

#### Scenario: 列表仅含本班数据
- **WHEN** A 班用户请求材料列表或搜索
- **THEN** 结果只来自数据库中的 A 班记录

### Requirement: R4 教师上传并知识库入库

系统 MUST 接受教师上传的 UTF-8 `.txt`/`.md`，并把材料元数据与正文归属到教师班级。

#### Scenario: 上传后列表与知识库可查
- **WHEN** 教师上传合法材料
- **THEN** 返回 201，本班列表出现新条目，`materials` 与 `knowledge_entries` 各新增一条对应记录

#### Scenario: 非法类型或超限被拒
- **WHEN** 上传非 `.txt`/`.md`、非法 UTF-8 或超过配置上限的文件
- **THEN** 返回 400/413，数据库与磁盘无残留

#### Scenario: 任一步失败均回滚
- **WHEN** 落盘、解析、任一表写入或提交失败
- **THEN** 两表与上传目录均不留下部分结果

### Requirement: R5 鉴权查看与下载

系统 MUST 通过 API 提供材料正文与原文件，MUST NOT 由 Nginx 静态暴露上传目录。

#### Scenario: 同班用户查看下载
- **WHEN** 同班教师或学生请求详情与文件
- **THEN** 详情返回正文，文件响应包含安全的 `Content-Disposition: attachment`

### Requirement: R6 可验证种子数据

系统 MUST 幂等写入 A/B 两班、教师 A、学生 A1/B1 和两班可区分材料，种子口令来自环境变量且只以 bcrypt 哈希落库。

#### Scenario: 重启不重复种子
- **WHEN** 应用使用同一数据库重复启动
- **THEN** 种子行不重复且预置账号仍可登录

### Requirement: R7 凭据与安全配置

系统 MUST 从环境变量读取数据库口令、会话密钥和种子口令；缺少必需项 MUST 启动失败；仓库不得含真实密钥。

#### Scenario: 缺失密钥失败即停
- **WHEN** 未设置 `SESSION_SECRET` 或数据库凭据
- **THEN** API 启动失败且日志明确指出缺少的变量

### Requirement: R8 Compose、持久化与健康检查

系统 MUST 通过 Docker Compose 启动 web/api/db，只暴露 web 端口，持久化数据库和上传目录，并提供无鉴权 `/health`。

#### Scenario: 一键启动且数据持久
- **WHEN** 第三方按 README 启动、上传后执行 `docker compose down` 再 `up`
- **THEN** 登录页可访问且上传记录和文件仍存在

#### Scenario: 健康检查不依赖数据库
- **WHEN** API 存活但数据库暂停
- **THEN** `/health` 仍返回 200，业务 API 返回 503

### Requirement: R9 Web 行为与安全

系统 MUST 提供登录页和材料页，刷新后以 `/api/me` 恢复身份，401 清理前端身份并回到登录页；材料正文必须按文本/安全 Markdown 渲染，不能执行脚本。

#### Scenario: 页面恢复身份
- **WHEN** 有效会话用户刷新页面
- **THEN** 前端依据 `/api/me` 恢复姓名、角色和班级，教师看到上传入口、学生看不到

### Requirement: R10 Web 视觉与交互

系统 MUST 提供北大红品牌、浅/深色主题、响应式布局、列表/网格切换、本班搜索、⌘/Ctrl+K 命令面板、上传进度、toast 和 Markdown GFM 展示。

#### Scenario: 交互可用
- **WHEN** 用户切换主题/视图、搜索、打开命令面板或上传
- **THEN** 页面即时反馈且不改变服务端授权边界

