## Context

现有认证使用 Bearer JWT，但每次请求按 `sub` 回库取得实时用户、角色和班级。`sessions` 是旧认证历史表，教学对话必须使用新的 `conversations` / `messages`。

## Decisions

### D1 幂等迁移

沿用 `Store.Migrate` 的 `CREATE TABLE IF NOT EXISTS` 机制追加六张表。外键只指向既有主键并使用明确的删除语义；迁移不执行 DROP、TRUNCATE 或切片重建。

### D2 班级与角色边界

所有智能体查询都带实时 `class_id`。写操作要求实时角色为 teacher。学生查询额外带 `enabled=TRUE`；他班、停用或不存在对象统一返回 404。

### D3 原子绑定

智能体本体与两组绑定在单个事务内写入。更新时锁定智能体行；每个技能和材料以 `class_id` 校验并加共享锁，随后删除旧绑定并写入新集合。任一步失败整体回滚。

### D4 重复与唯一性

API 在进入事务前拒绝重复或非正整数绑定 ID；两张绑定表以复合主键作最终防线。智能体使用 `(class_id,name)` 唯一约束，重复名称返回 400。
