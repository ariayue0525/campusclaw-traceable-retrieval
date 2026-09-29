# 项目规则

1. 无规约不写代码：先读 `openspec/changes/add-auth-rbac-class-knowledge/` 四件套。
2. 范围由 proposal 的 Non-goals 决定；新增需求先修改 delta spec，再实现。
3. 每完成一个 task 立即运行该项 verify；只在验证通过后勾选。
4. 浏览器与前端不可信；认证、授权、班级隔离和文件校验必须在服务端完成。
5. 禁止提交 `.env`、真实口令、会话密钥、数据库数据与上传内容。

