## Why

教研材料分散在群文件和网盘中，难以复用，也缺少班级数据边界。本变更先建立身份、角色、班级隔离与材料入库底座。

## What Changes

- 教师和学生使用账号密码登录，未登录 API 返回 401，页面回登录页。
- 教师可上传；学生只读，调用上传接口返回 403。
- 服务端以会话中的班级为数据边界，跨班详情与文件返回 404。
- 教师上传 `.txt` / `.md` 后，原文件落盘，正文与材料记录同事务入库，列表立即可查。
- React + TypeScript 前端提供登录、材料列表/详情/下载、搜索、主题、双视图、命令面板、进度与通知。
- Nginx + Go + MySQL 由 Docker Compose 一键启动，提供 `GET /health`。

## Capabilities

### New Capabilities

- `auth-upload`: 登录会话、角色授权、班级隔离、上传入库、材料浏览下载、前端体验、Compose 与健康检查。

### Modified Capabilities

无。

## Impact

新增 `frontend/`、`backend/`、`nginx/`、Compose、环境变量示例、测试和验收脚本。

## Non-goals

- 不做 RAG、向量化、全文检索或智能问答。
- 不做聊天助手、Agent 编排或技能运行时。
- 不做作业提交、批改、成绩与错题本。
- 不做注册、找回密码、OAuth/OIDC/SSO、平台超级管理员与多校租户。
- 不做 Kubernetes、多副本 HA 或完整 CI/CD。
- 不以隐藏按钮代替服务端权限判断。

