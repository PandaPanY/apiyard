# 从 YApi 迁移

`apiyard migrate` 把 YApi 的数据从 MongoDB 读出来，写进 ApiYard 的 SQLite 数据库。所有 ID 原样保留，所以迁移后：

- 原来的账号密码可以直接登录（密码哈希算法与 YApi 相同）；
- 原来的项目 token 继续可用，yapi-to-typescript、easy-yapi、Yapi-MCP 等工具只需改服务器地址；
- 原来的接口链接（如 `/project/11/interface/api/25`）、书签和 Mock 地址都不失效。

## 迁移哪些数据

| YApi 集合 | 迁移 | 说明 |
|---|---|---|
| `user`、`group`、`project`、`token` | 是 | 分组和项目的成员一起迁移 |
| `interface_cat`、`interface` | 是 | 正在编辑的标记（`edit_uid`）不迁移，ApiYard 启动时会清空 |
| `follow`、`log`、`avatar` | 是 | 关注、操作动态（含“查看改动”用的数据）、头像 |
| `identitycounters` | 用于抬高 ID 序列 | 迁移后新建的数据不会复用 YApi 里已删除数据的 ID |
| `interface_col`、`interface_case` | 否 | 自动化测试，ApiYard 暂不支持 |
| `wiki`、`adv_mock`、`statis_mock`、`storage` 等 | 否 | 插件数据，ApiYard 暂不支持 |

不迁移的集合如果有数据，报告里会列出来。YApi 文档里 ApiYard 没有单独建列的字段（包括插件写进去的字段）保存在 `extra` 列，数据不会丢失。

## 步骤

### 1. 准备连接信息

数据库连接信息在 YApi 部署目录的 `config.json` 里：

```json
"db": { "servername": "127.0.0.1", "DATABASE": "yapi", "port": 27017, "user": "yapi", "pass": "...", "authSource": "admin" }
```

对应的连接串是 `mongodb://yapi:密码@127.0.0.1:27017/yapi?authSource=admin`；没有配置用户名时写成 `mongodb://127.0.0.1:27017/yapi`。配置了 `connectString` 的，直接用它。

同时记下 `config.json` 里的 `passsalt`。没有这一项的，就是 YApi 的默认值 `abcde`。

### 2. 预检

```bash
apiyard migrate -mongo "mongodb://127.0.0.1:27017/yapi" -dry-run
```

预检会完整执行一遍迁移和核对，但不写入目标数据库。报告列出每个集合的源数据、已迁移和跳过的条数，以及跳过和修正的原因，例如：

```
跳过的记录：
  interface_cat：所属项目不存在 × 13（_id 11、18、25、32、46 等）
  token：所属项目不存在 × 4（_id 11、16、21、26）
```

YApi 删除项目时不会清理它的分类和 token，删除分类也不是原子操作，运行久了的实例里常有这种指向已删除数据的记录。它们在 YApi 的界面上本来就看不到，跳过不影响使用。

### 3. 正式迁移

迁移前先停止 YApi，或者至少让大家暂停编辑，避免迁移过程中又有新数据写入。

```bash
apiyard migrate -mongo "mongodb://127.0.0.1:27017/yapi" -db apiyard.db
```

数据先写进临时文件 `apiyard.db.migrating`，全部写完并核对通过后才改名为 `apiyard.db`；中途失败不会留下不完整的数据库。

### 4. 启动 ApiYard

```bash
apiyard -db apiyard.db -token-salt 原来的passsalt
```

`-token-salt` 必须与 YApi 的 `passsalt` 一致，否则原来的项目 token 都会失效。数据库里已经有用户，所以启动时不会再创建管理员账号，用原来的管理员登录即可。

### 5. 切换地址

确认无误后，把原来指向 YApi 的域名或反向代理改为指向 ApiYard。ApiYard 的默认端口与 YApi 相同（3000）。

## MongoDB 版本较老时

直连使用的 MongoDB 驱动支持 3.6 及以上版本。更早的版本（YApi 早期文档要求的是 2.6 以上），或者 ApiYard 所在的机器连不上 MongoDB 时，先用 `mongodump` 导出，再从导出目录迁移：

```bash
mongodump --db yapi --out ./dump        # 也可以加 --gzip
apiyard migrate -dump ./dump -db apiyard.db
```

`-dump` 可以指定 `--out` 的目录，也可以直接指定里面的 `yapi` 目录。数据库名不是 `yapi` 时，用 `-mongo-db` 指定。两种方式迁移出来的数据完全相同。

## 用 Docker 迁移

镜像里同样有 `migrate` 子命令。迁移到数据卷里，再用同一个数据卷启动：

```bash
docker run --rm -v apiyard-data:/data ghcr.io/pandapany/apiyard:0.1 \
  migrate -mongo "mongodb://yapi:密码@mongo-host:27017/yapi?authSource=admin" -db /data/apiyard.db
docker run -d --name apiyard -p 3000:3000 -v apiyard-data:/data \
  -e APIYARD_TOKEN_SALT=原来的passsalt ghcr.io/pandapany/apiyard:0.1
```

MongoDB 在本机时，Linux 上加 `--network host` 并用 `127.0.0.1`；Docker Desktop 上用 `host.docker.internal`。用 `-dump` 时把导出目录也挂载进容器。

## 重新迁移

目标数据库已经存在时，迁移会拒绝执行；加 `-overwrite` 会用新迁移的数据**整个替换**目标数据库，ApiYard 里在此期间新增的数据会丢失。替换前要先停止 ApiYard。

## 报告里的“修正”

数据类型不对的字段，按 YApi（Mongoose）读取时的转换规则处理。例如字符串 `"12"` 读作数字 12，`"true"` 读作布尔值。转换不了的字段改成默认值，记在报告的“修正过的记录”里，记录本身照常迁移。常见的有：

| 修正 | 处理 |
|---|---|
| 成员重复 | 只保留第一个，YApi 的权限判断也只看第一个 |
| 成员角色无效 | 改为 `guest`（只读） |
| `req_body_type` 等取值不在 YApi 的枚举里 | 清空，界面按默认方式显示 |
| 同一项目有多个 token、同一用户有多个头像 | 只保留第一个，与 YApi 查询到的相同 |

以下几种记录会被跳过：邮箱为空或重复的用户、`_id` 不是正整数的记录，以及类型无效的动态。

## 迁移速度

1 万个接口加 3 万条动态，经过 SSH 隧道迁移约 2 秒。
