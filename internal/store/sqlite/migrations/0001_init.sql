-- ApiYard 数据库第 1 版：v0.1 范围内的表（M0.5 草案，M1 落地时作为第一个迁移执行）。
-- 设计说明见 docs/schema.md，要点：
--   * 主键沿用 YApi 的整数 ID；AUTOINCREMENT 保证删除后 ID 不被复用（YApi 也从不复用，旧链接不会指向新数据）。
--   * 时间为 Unix 秒，布尔值为 0/1。
--   * 可选文本字段允许 NULL：NULL 表示 YApi 原文档里没有这个字段，响应中也不输出，保证迁移无损。
--   * 整体读写、不单独查询的嵌套结构存为 JSON 文本，并用 json_valid 约束。
--   * extra 保存 YApi 文档中未单独建列的字段（包括插件写入的字段），迁移不丢数据。
--   * 唯一约束只用于 YApi 本身就在数据库层保证唯一的字段（邮箱），其余由应用层检查，
--     以免运行多年的真实数据里的重复记录导致迁移失败。
--   * 枚举 CHECK 只用于 YApi 本身就会校验的字段。

CREATE TABLE users (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    username  TEXT    NOT NULL,
    email     TEXT    NOT NULL,
    password  TEXT    NOT NULL,                -- sha1(明文 + sha1(passsalt))，与 YApi 相同
    passsalt  TEXT    NOT NULL,
    role      TEXT    NOT NULL DEFAULT 'member', -- admin / member；YApi 未校验，不加 CHECK
    type      TEXT    NOT NULL DEFAULT 'site' CHECK (type IN ('site', 'third')),
    study     INTEGER NOT NULL DEFAULT 0 CHECK (study IN (0, 1)),
    add_time  INTEGER NOT NULL,
    up_time   INTEGER NOT NULL,
    extra     TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(extra))
) STRICT;
CREATE UNIQUE INDEX users_email ON users (email);
CREATE INDEX users_username ON users (username);

-- type = 'private' 表示用户的个人空间
CREATE TABLE groups (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    uid           INTEGER NOT NULL,          -- 创建者；不建外键，YApi 删除用户时不清理其分组
    group_name    TEXT    NOT NULL,
    group_desc    TEXT,
    type          TEXT    NOT NULL DEFAULT 'public' CHECK (type IN ('public', 'private')),
    custom_field1 TEXT    NOT NULL DEFAULT '{"enable":false}' CHECK (json_valid(custom_field1)),
    add_time      INTEGER NOT NULL,
    up_time       INTEGER NOT NULL,
    extra         TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(extra))
) STRICT;
CREATE INDEX groups_uid ON groups (uid);
CREATE INDEX groups_name ON groups (group_name);

-- 对应 YApi groups.members 数组。username/email 是 YApi 保存的副本，保留以保证响应一致
CREATE TABLE group_members (
    group_id  INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    uid       INTEGER NOT NULL,
    role      TEXT    NOT NULL CHECK (role IN ('owner', 'dev', 'guest')),
    username  TEXT,
    email     TEXT,
    sub_id    TEXT    NOT NULL,              -- 成员子文档的 ObjectId，接口响应中会返回
    position  INTEGER NOT NULL,              -- 在原数组中的顺序
    PRIMARY KEY (group_id, uid)
) STRICT;
CREATE INDEX group_members_uid ON group_members (uid);

CREATE TABLE projects (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    uid                 INTEGER NOT NULL,
    group_id            INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    name                TEXT    NOT NULL,
    basepath            TEXT,
    description         TEXT,                -- YApi 字段名 desc
    project_type        TEXT    NOT NULL CHECK (project_type IN ('public', 'private')),
    switch_notice       INTEGER NOT NULL DEFAULT 1 CHECK (switch_notice IN (0, 1)),
    icon                TEXT,
    color               TEXT,
    env                 TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(env)),
    tag                 TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(tag)),
    pre_script          TEXT,                -- 前端“运行”时在浏览器里执行，服务端不执行
    after_script        TEXT,
    project_mock_script TEXT,                -- 迁移时保留，但 ApiYard 不执行（D5）
    is_mock_open        INTEGER NOT NULL DEFAULT 0 CHECK (is_mock_open IN (0, 1)),
    strice              INTEGER NOT NULL DEFAULT 0 CHECK (strice IN (0, 1)), -- YApi 原拼写，开启后 Mock 校验请求参数
    is_json5            INTEGER NOT NULL DEFAULT 1 CHECK (is_json5 IN (0, 1)),
    add_time            INTEGER NOT NULL,
    up_time             INTEGER NOT NULL,
    extra               TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(extra))
) STRICT;
CREATE INDEX projects_group ON projects (group_id, name);
CREATE INDEX projects_uid ON projects (uid);

CREATE TABLE project_members (
    project_id   INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    uid          INTEGER NOT NULL,
    role         TEXT    NOT NULL CHECK (role IN ('owner', 'dev', 'guest')),
    username     TEXT,
    email        TEXT,
    email_notice INTEGER NOT NULL DEFAULT 1 CHECK (email_notice IN (0, 1)),
    sub_id       TEXT    NOT NULL,
    position     INTEGER NOT NULL,
    PRIMARY KEY (project_id, uid)
) STRICT;
CREATE INDEX project_members_uid ON project_members (uid);

CREATE TABLE interface_cats (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    uid         INTEGER NOT NULL,
    project_id  INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name        TEXT    NOT NULL,
    description TEXT,                        -- YApi 字段名 desc
    position    INTEGER NOT NULL DEFAULT 0,  -- YApi 字段名 index
    add_time    INTEGER NOT NULL,
    up_time     INTEGER NOT NULL,
    extra       TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(extra))
) STRICT;
CREATE INDEX interface_cats_project ON interface_cats (project_id, position);

CREATE TABLE interfaces (
    id                      INTEGER PRIMARY KEY AUTOINCREMENT,
    uid                     INTEGER NOT NULL,
    edit_uid                INTEGER NOT NULL DEFAULT 0,  -- 正在编辑的用户，0 表示无人编辑
    project_id              INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    catid                   INTEGER NOT NULL REFERENCES interface_cats (id) ON DELETE CASCADE,
    title                   TEXT    NOT NULL,
    path                    TEXT    NOT NULL,
    method                  TEXT    NOT NULL,
    status                  TEXT    NOT NULL DEFAULT 'undone' CHECK (status IN ('undone', 'done')),
    type                    TEXT    NOT NULL DEFAULT 'static' CHECK (type IN ('static', 'var')), -- var：路径含参数
    api_opened              INTEGER NOT NULL DEFAULT 0 CHECK (api_opened IN (0, 1)),
    position                INTEGER NOT NULL DEFAULT 0,  -- YApi 字段名 index
    description             TEXT,                        -- YApi 字段名 desc（HTML）
    markdown                TEXT,
    query_path              TEXT    NOT NULL DEFAULT '{"path":"","params":[]}' CHECK (json_valid(query_path)),
    req_query               TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(req_query)),
    req_headers             TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(req_headers)),
    req_params              TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(req_params)),
    req_body_form           TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(req_body_form)),
    req_body_type           TEXT    CHECK (req_body_type IN ('form', 'json', 'text', 'file', 'raw')),
    req_body_is_json_schema INTEGER NOT NULL DEFAULT 0 CHECK (req_body_is_json_schema IN (0, 1)),
    req_body_other          TEXT,                        -- 原样保存的字符串（JSON / JSON Schema / 文本）
    res_body_type           TEXT    CHECK (res_body_type IN ('json', 'text', 'xml', 'raw', 'json-schema')),
    res_body_is_json_schema INTEGER NOT NULL DEFAULT 0 CHECK (res_body_is_json_schema IN (0, 1)),
    res_body                TEXT,
    custom_field_value      TEXT,
    tag                     TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(tag)),
    add_time                INTEGER NOT NULL,
    up_time                 INTEGER NOT NULL,
    extra                   TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(extra))
) STRICT;
CREATE INDEX interfaces_project ON interfaces (project_id, catid, position);
CREATE INDEX interfaces_route ON interfaces (project_id, method, path);  -- 新建时查重、Mock 按路径匹配
CREATE INDEX interfaces_uid ON interfaces (uid);

-- 每个项目一个原始 token；开放 API 的 token 是用 passsalt 加密后的“uid|原始 token”
CREATE TABLE tokens (
    project_id INTEGER PRIMARY KEY REFERENCES projects (id) ON DELETE CASCADE,
    token      TEXT    NOT NULL
) STRICT;
CREATE UNIQUE INDEX tokens_token ON tokens (token);

-- projectname/icon/color 是 YApi 保存的项目信息副本
CREATE TABLE follows (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    uid         INTEGER NOT NULL,
    project_id  INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE, -- YApi 字段名 projectid
    projectname TEXT    NOT NULL,
    icon        TEXT,
    color       TEXT
) STRICT;
CREATE INDEX follows_uid ON follows (uid);
CREATE INDEX follows_project ON follows (project_id);

-- 操作动态。typeid 按 type 指向不同的表，因此不建外键
CREATE TABLE logs (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    uid      INTEGER NOT NULL,
    username TEXT    NOT NULL,
    type     TEXT    NOT NULL CHECK (type IN ('user', 'group', 'interface', 'project', 'other', 'interface_col')),
    typeid   INTEGER NOT NULL,
    content  TEXT    NOT NULL,               -- HTML 片段
    data     TEXT    CHECK (json_valid(data)), -- 变更前后的原始数据，用于“查看改动”
    add_time INTEGER NOT NULL
) STRICT;
CREATE INDEX logs_target ON logs (type, typeid, id);
CREATE INDEX logs_uid ON logs (uid);

-- YApi 以 base64 字符串保存头像，迁移时解码为二进制
CREATE TABLE avatars (
    uid   INTEGER PRIMARY KEY,
    image BLOB    NOT NULL,
    type  TEXT    NOT NULL                   -- MIME 类型，如 image/png
) STRICT;
