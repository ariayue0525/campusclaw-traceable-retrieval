## 1. 仓库与配置
- [x] 1.1 建立 README 三行说明、AGENTS.md、OpenSpec 四件套（verify: `openspec validate add-auth-rbac-class-knowledge --strict`）
- [x] 1.2 建立 `.env.example`、忽略规则与失败即停配置（verify: 配置测试）

## 2. 数据与种子
- [x] 2.1 建立五张核心表及班级索引（verify: schema 初始化测试）
- [x] 2.2 幂等写入两班、三用户、两材料，口令 bcrypt（verify: 重复初始化行数不变）

## 3. 登录与会话
- [x] 3.1 实现登录、登出、`/api/me`、统一 401 与限流（verify: 单元测试 + HTTP 验收）
- [x] 3.2 服务端 session 摘要、过期与安全 Cookie（verify: 完整 HTTP Header 验收）

## 4. 班级隔离
- [x] 4.1 列表 SQL 强制班级过滤（verify: A 班列表无 B 班标题）
- [x] 4.2 详情与文件 fetch-then-check，跨班 404（verify: 跨班负向测试）

## 5. 上传入库
- [x] 5.1 教师角色门禁、类型/大小/UTF-8 校验与随机存储名（verify: 学生与非法文件测试）
- [x] 5.2 两表事务与文件回滚（verify: 故障注入后无脏数据）

## 6. 读取与下载
- [x] 6.1 实现列表/搜索、详情、鉴权下载（verify: 同班主路径与跨班失败路径）

## 7. 前端
- [x] 7.1 登录页、身份恢复、材料列表/详情/下载与教师上传（verify: Vitest + production build）
- [x] 7.2 主题、响应式、双视图、搜索、命令面板、进度、toast、GFM（verify: UI 测试与人工截图）

## 8. Compose 与文档
- [x] 8.1 建立 web/api/db、内部网络、健康检查与持久卷（verify: `docker compose config` + `docker compose ps`）
- [x] 8.2 README 从零启动、停止与验收说明（verify: `down`/`up` 后上传记录仍可查询）

## 9. 验收
- [x] 9.1 后端单元/集成测试、前端测试与构建通过
- [x] 9.2 登录、身份、跨班、学生上传、教师上传、下载与 `/health` 证据可复核
- [x] 9.3 `openspec validate add-auth-rbac-class-knowledge --strict` 退出码为 0
