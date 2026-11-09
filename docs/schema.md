# 数据库表结构

> 状态：M0.5 草案（2026-11-04），已用 SQLite 3.53 校验。M1 落地时作为第一个迁移执行。
> DDL：[`internal/store/migrations/0001_init.sql`](../internal/store/migrations/0001_init.sql)

## 集合与表的对应

| YApi 集合 | ApiYard 表 | 说明 |
|---|---|---|
| `user` | `users` | |
| `group` | `groups` + `group_members` | `members` 数组拆成独立表，"我参与的分组"需要按 uid 查询 |
| `project` | `projects` + `project_members` | 同上；`env`、`tag` 整体读写，存为 JSON |
| `interface_cat` | `interface_cats` | |
| `interface` | `interfaces` | 请求参数、请求头等嵌套数组整体读写，存为 JSON |
| `token` | `tokens` | 每个项目一行 |
| `follow` | `follows` | |
| `log` | `logs` | |
| `avatar` | `avatars` | base64 字符串解码为二进制 |
| `interface_col`、`interface_case` | — | 自动化测试，v0.1 不做 |
| `storage`、各插件的集合 | — | 插件数据，v0.1 不做 |
| `identitycounters` | `sqlite_sequence` | 迁移时用它抬高各表的 ID 序列 |

表名用复数，是为了避开 SQL 关键字（`group`）。

## 字段改名

| YApi 字段 | 所在集合 | ApiYard 列 | 原因 |
|---|---|---|---|
| `_id` | 全部 | `id` | |
| `desc` | project、interface_cat、interface | `description` | SQL 关键字 |
| `index` | interface_cat、interface | `position` | SQL 关键字 |
| `projectid` | follow | `project_id` | 统一命名（YApi 在这个字段上的索引甚至建在了不存在的 `project_id` 上） |

接口层负责把列名转换回 YApi 的字段名，对外的字段名不变。

## 设计原则

1. **ID 沿用 YApi 的整数**：迁移时原样保留，旧链接和 Mock 地址不失效。用 `AUTOINCREMENT` 保证删除后 ID 不被复用，迁移时再按 YApi 的 `identitycounters` 抬高序列，原版里已删除数据的 ID 也不会被复用。
2. **无损迁移**：
   - 可选文本字段允许 `NULL`，表示原文档里没有这个字段，响应中也不输出该字段；
   - YApi 文档里没有单独建列的字段（包括插件写入的字段）存进 `extra`（JSON），写回时原样合并。
3. **约束跟着 YApi 走**：
   - 唯一约束只加在 YApi 本身就有唯一索引的字段上（`users.email`）。分组名、项目名、接口路径的查重由应用层处理。运行多年的真实数据里可能有重复记录，数据库层强制唯一会导致迁移失败。
   - 枚举 `CHECK` 只加在 YApi 本身会校验的字段上；例如 `users.role` YApi 没有校验，这里也不加。
4. **嵌套结构存为 JSON**：只整体读写、不单独查询的结构（环境配置、请求参数、请求头等）存为 JSON 文本，并用 `json_valid` 约束格式。
5. **外键与级联删除**：分组 → 项目 → 分类 → 接口，以及 token、关注、成员，都设置 `ON DELETE CASCADE`，与 YApi 删除时的清理逻辑一致。创建者 `uid` 不加外键：YApi 删除用户时不会清理该用户创建的数据。
6. **使用 `STRICT` 表**：防止 SQLite 的宽松类型把错误类型的数据静默写进去。

## 对接口层的要求

这些是建表时发现的、需要接口层处理的兼容细节：

| 细节 | 处理方式 |
|---|---|
| YApi 的很多响应带 `"__v": 0`（Mongoose 的版本字段），golden 文件里出现了 47 次 | 接口层固定输出 `__v: 0`，不建列 |
| 嵌套数组的每个元素都有自己的 ObjectId（成员、环境配置、请求头等），响应中会返回，前端可能把它当作列表的 key | 迁移时原样保留；ApiYard 新建元素时生成同样格式的 24 位十六进制 ID |
| 成员、关注里保存了用户名、项目名等信息的副本 | 保留副本，并按 YApi 的逻辑同步更新，保证响应一致 |

## 迁移时需要注意（M4，已实现，见 [migration.md](migration.md)）

- **孤儿数据**：YApi 的删除操作不是原子的，删除项目时也不清理分类和 token，所以会有记录指向已删除的数据。迁移时跳过这些记录并写进报告；外键约束保证这类数据不会被写进来。
- **重复成员**：`group_members`、`project_members` 以 `(分组/项目, uid)` 为主键，迁移时只保留第一个，并写进报告。
- **头像**：base64 字符串按 Node 的宽松规则解码后写入 `avatars.image`。
- **缺少的字段**：按 Mongoose schema 的默认值补齐（例如 `req_query` 里每项的 `required` 默认为 `"1"`）；schema 里没有默认值的可选字段存为 `NULL`，响应中同样不输出。

## 校验结果

用 SQLite 3.53 执行 DDL 后验证了 18 项约束（唯一邮箱、枚举、JSON 格式、外键、允许重复接口等）、ID 分配（迁移后续号、按计数器抬高、删除后不复用）、级联删除，以及 5 个关键查询的索引命中，全部通过。M1 会把这些检查改写成 Go 测试。
