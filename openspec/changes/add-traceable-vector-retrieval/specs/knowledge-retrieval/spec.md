## ADDED Requirements

### Requirement: R1 上传后自动切分并写入切片

系统 MUST 在教师上传材料并提交事务之后，把 `knowledge_entries.body_text` 切分为若干切片写入 `knowledge_chunks`，MUST NOT 改写原始 `body_text` 与原文件。每个切片 MUST 记录 `chunk_index`、`start_offset`、`end_offset`、`class_id`、`material_id`、`knowledge_entry_id` 与所用 `chunk_strategy`。

#### Scenario: 上传后立即可检索
- **WHEN** 教师上传一份超过单个切片长度的 `.md` 材料
- **THEN** 返回 201，响应包含 `chunk_count` 大于 1 与 `chunk_strategy`，且本班混合检索材料中的原词可以命中其中切片

#### Scenario: 原文不被切分改写
- **WHEN** 教师使用带预处理的策略重建索引后再次查看材料详情
- **THEN** `body_text` 仍为上传时的原文，切片偏移量对应预处理后的文本

#### Scenario: 未指定策略时按自动窗口
- **WHEN** 上传或重建索引请求未携带 `chunk_strategy`
- **THEN** 按自动窗口处理（约 800 字，重叠约 80 字），`chunk_strategy` 记为 `auto`

### Requirement: R2 三种切分策略

系统 MUST 支持 `auto`、`custom`、`hierarchy` 三种切分策略，且 MUST 在参数越界时返回 400。

#### Scenario: 自动窗口优先在自然断点断开
- **WHEN** 以 `auto` 切分含空行与句号的正文
- **THEN** 切片长度不超过约 800 字，相邻切片存在重叠，断点落在空行、换行或句号附近

#### Scenario: 自定义分隔符与长度范围
- **WHEN** 以 `custom` 且 `chunk_max_chars=300`、`chunk_overlap_percent=10`、分隔符为句号切分
- **THEN** 切片不超过 300 字、重叠约 30 字，无断点处按长度强制截断

#### Scenario: 自定义参数越界被拒
- **WHEN** `chunk_max_chars` 小于 100 或大于 2000，或 `chunk_overlap_percent` 大于 50
- **THEN** 返回 400，已入库切片与向量不发生任何变化

#### Scenario: 按 Markdown 标题分章
- **WHEN** 以 `hierarchy` 切分含 `#`、`##`、`###` 的正文
- **THEN** 每个标题层级形成独立切片且标题保留在该切片内，超长章节再按自动窗口细分

#### Scenario: 自动策略忽略另行填写的长度参数
- **WHEN** 请求指定 `chunk_strategy=auto` 同时填写 `chunk_max_chars`
- **THEN** 按 800/80 的默认窗口切分，填入的长度不生效

### Requirement: R3 切片正文存 MySQL，向量存 Qdrant

系统 MUST 把切片正文保存在 `knowledge_chunks.chunk_text`，MUST NOT 把正文写入 Qdrant payload。Qdrant 中的向量主键 MUST 等于 `knowledge_chunks.id`，且等于 `payload.chunk_id`；payload 只含 `class_id`、`material_id`、`knowledge_entry_id`、`chunk_id`、`chunk_index` 若干标识。

#### Scenario: 主键三者一致
- **WHEN** 读取某材料的切片与向量对照
- **THEN** 每条记录的 `knowledge_chunks.id`、向量主键与 `payload.chunk_id` 数值相同，且向量库一侧不含 `chunk_text`

#### Scenario: 嵌入失败保留切片并标记
- **WHEN** 嵌入网关不可用导致某条切片嵌入失败
- **THEN** 该切片行仍然存在且 `index_status` 为 `failed`，向量库中没有对应记录，材料记录与原文不受影响

#### Scenario: 仅就绪切片参与检索
- **WHEN** 材料中同时存在 `ready` 与 `failed` 切片
- **THEN** 任何检索模式的结果只来自 `index_status='ready'` 的切片

### Requirement: R4 关键字检索只查 MySQL 全文索引

系统 MUST 在 `mode=keyword` 时仅查询 MySQL 全文索引，MUST NOT 调用嵌入服务，也 MUST NOT 访问 Qdrant。

#### Scenario: 原词命中
- **WHEN** 以材料中出现的原词发起关键字检索
- **THEN** 返回 200，命中的切片按全文相关度降序排列，且响应中 `query_vector` 为 null

#### Scenario: 同义改写落空
- **WHEN** 以材料中未出现、仅语义相近的改写句发起关键字检索
- **THEN** 返回 200 且 `hits` 为空，`status` 为 `no_evidence`

### Requirement: R5 向量检索按班级过滤并设阈值

系统 MUST 在 `mode=vector` 时先把问句转换为向量，再在 Qdrant 中以会话班级过滤，MUST 丢弃余弦相似度低于阈值的候选（阈值随嵌入实现而定：网关语义嵌入 0.35，本地哈希嵌入 0.15，可由 VECTOR_MIN_SCORE 覆盖），再用向量主键回 MySQL 取回正文。

#### Scenario: 语义相近命中
- **WHEN** 以表述改变但语义保留的问句发起向量检索
- **THEN** 返回 200，命中切片按余弦相似度降序排列，响应包含问句向量

#### Scenario: 低于阈值不保留
- **WHEN** 所有候选切片的余弦相似度均低于阈值
- **THEN** 返回 200 且 `hits` 为空，`status` 为 `no_evidence`，不得以弱相关切片凑数

#### Scenario: 回表再次核对班级
- **WHEN** 向量库返回若干主键
- **THEN** 回表 SQL 仍带 `class_id = 会话班级` 条件，不属于本班的主键不出现在结果中

### Requirement: R6 混合检索按名次融合

系统 MUST 在 `mode=hybrid` 时分别执行关键字与向量两条路径，各自按绝对分数过滤后，以 RRF（k = 60）按名次融合为最终次序，MUST NOT 把两路分数相加。缺席的一路 MUST NOT 贡献分数。

#### Scenario: 两路都命中时按名次融合
- **WHEN** 两条路径都返回候选
- **THEN** 结果同时包含两路切片，两路均命中的切片排序更靠前，每条结果携带两路的 score 与 rank 供核对

#### Scenario: 单路缺席
- **WHEN** 关键字路径为空而向量路径有候选
- **THEN** 最终结果只含向量路径切片，缺席一路不贡献分数

#### Scenario: 默认采用混合模式
- **WHEN** 请求未指定 `mode` 或指定非法值
- **THEN** 按 `hybrid` 处理；显式指定非法值时返回 400

### Requirement: R7 每种模式命中结果可回溯到原文

系统 MUST 为每条命中给出材料标题、切片序号、字符区间与一段摘录，摘录 MUST 取自 MySQL 的 `chunk_text`，并 MUST 提供打开对应材料的标识。

#### Scenario: 命中带完整出处
- **WHEN** 任一模式检索到切片
- **THEN** 每条命中包含 `material_id`、`material_title`、`chunk_id`、`chunk_index`、`start_offset`、`end_offset` 与 `snippet`，`snippet` 内容来自 `chunk_text`

#### Scenario: 摘录不来自向量库
- **WHEN** 清空 Qdrant 中某材料的向量记录但保留 MySQL 切片
- **THEN** 关键字检索仍能返回该材料切片的摘录

### Requirement: R8 班级隔离在检索侧生效

系统 MUST 只从登录会话读取班级用于检索过滤，MUST NOT 采信查询参数、JSON 正文或请求头中的班级编号。跨班级检索 MUST 表现为无命中，MUST NOT 以 403 或 404 暗示资料归属其他班级。

#### Scenario: 请求体中的班级编号被丢弃
- **WHEN** A 班会话在检索请求体中写入 B 班的 `class_id`
- **THEN** 过滤条件仍是 A 班，结果中不出现 B 班切片

#### Scenario: 跨班检索表现为无命中
- **WHEN** A 班会话检索一个只出现在 B 班正文中的词
- **THEN** 返回 200 且 `hits` 为空，`status` 为 `no_evidence`

#### Scenario: 跨班打开材料仍为 404
- **WHEN** A 班会话请求 B 班材料详情、文件或切片对照
- **THEN** 返回 404，响应不含 B 班标题、正文或切片内容

### Requirement: R9 无依据时不调用生成模型

系统 MUST 在 `POST /api/ask` 中先执行本班混合检索并取前 4 条切片；无命中时 MUST 返回固定文案「资料中未找到相关内容」且 `citations` 为空，MUST NOT 调用对话网关。有命中时回答 MUST 以 `[1]`、`[2]` 标注出处，编号顺序与 `citations` 一致。

#### Scenario: 有依据生成带标注回答
- **WHEN** 提问内容在本班材料中存在依据
- **THEN** 返回 200，回答正文中出现 `[1]` 等标注，且 `citations` 顺序与标注一一对应，每条含材料标题、切片序号、字符区间与摘录

#### Scenario: 无依据返回固定文案
- **WHEN** 提问内容在本班材料中不存在依据（如天气、比分）
- **THEN** 返回 200，回答为固定文案，`citations` 为空数组，`status` 为 `no_evidence`，且对话网关未被调用

#### Scenario: 客户端注入的 system 消息被丢弃
- **WHEN** 请求 `history` 中包含 `role` 为 `system` 的消息
- **THEN** 该消息在解析时被丢弃，不进入对话模块的输入

#### Scenario: 只用最新一句发起检索
- **WHEN** 请求携带多轮 `history`
- **THEN** 检索仅使用最新一句提问，历史对话只作为上下文附于其后

### Requirement: R10 重建索引与依赖降级

系统 MUST 允许教师按请求中的策略重建索引：先删除旧切片与旧向量记录，再按本次策略重新写入。Qdrant 不可用时 MUST 让关键字检索继续返回结果，向量与混合模式返回 503。空查询 MUST 返回 400。

#### Scenario: 重建索引先删后写
- **WHEN** 教师以 `hierarchy` 重建一份原为 `auto` 的材料
- **THEN** 旧切片行与旧向量记录被删除，新切片按 `hierarchy` 写入且数量与策略相符，向量主键重排后与新的 `knowledge_chunks.id` 一致

#### Scenario: 学生不能重建索引
- **WHEN** 学生会话调用重建索引接口
- **THEN** 返回 403，切片与向量均不变化

#### Scenario: Qdrant 故障时关键字仍可用
- **WHEN** Qdrant 不可用时发起 `keyword` 检索
- **THEN** 返回 200 与按全文相关度排序的结果

#### Scenario: Qdrant 故障时向量与混合返回 503
- **WHEN** Qdrant 不可用时发起 `vector` 或 `hybrid` 检索
- **THEN** 返回 503，不编造相似度分数也不伪造命中

#### Scenario: 空查询被拒
- **WHEN** 检索或问答请求的问句为空白字符串
- **THEN** 返回 400，`hits` 与 `citations` 均不返回内容

### Requirement: R11 密钥与依赖不暴露给浏览器

系统 MUST 只在服务端调用嵌入网关、对话网关与 Qdrant，MUST NOT 把网关密钥下发到页面，MUST NOT 在页面上展示向量原始分量之外的敏感配置。Qdrant MUST NOT 映射宿主端口，也 MUST NOT 接入可发布端口的网络。

#### Scenario: 浏览器无法直连 Qdrant
- **WHEN** 从宿主或浏览器访问 Qdrant 的 HTTP/GRPC 端口
- **THEN** 连接失败；只有 Go 服务可以通过内部网络访问

#### Scenario: 响应中不出现密钥
- **WHEN** 调用检索、问答或切片对照接口
- **THEN** 响应正文不包含网关密钥、DSN 或会话密钥
