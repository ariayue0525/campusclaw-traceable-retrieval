## ADDED Requirements

### Requirement: R1 第 5 课数据底座

系统 MUST 幂等建立 `tutor_agents`、`skills`、两张绑定表、`conversations` 和 `messages`，且不得复用认证 `sessions` 或破坏已有材料/切片。

#### Scenario: 重复迁移
- **WHEN** 同一数据库重复执行迁移
- **THEN** 不报错、不重复种子、不删除已有 materials 与 knowledge_chunks 数据

### Requirement: R2 教师管理智能体

系统 MUST 仅允许教师创建、修改、启停本班智能体，并限制名称 64 字、提示词 2000 字及班内名称唯一。

#### Scenario: 学生写入
- **WHEN** 学生调用创建或修改接口
- **THEN** 返回 403 且数据库不变化

### Requirement: R3 原子且同班的绑定

系统 MUST 在事务内校验技能/材料属于教师当前班级并原子替换绑定，重复 ID 必须明确拒绝。

#### Scenario: 跨班绑定
- **WHEN** A 班教师提交 B 班技能或材料 ID
- **THEN** 返回与不存在绑定对象相同的 404，智能体及原绑定均不发生部分更新

### Requirement: R4 学生可见性

学生 MUST 只能列出或读取本班已启用的智能体。

#### Scenario: 停用与他班对象
- **WHEN** 学生读取本班停用、他班或不存在智能体
- **THEN** 三者均返回相同的 404 响应形状
