# 架构设计

## 设计原则

1. **兼容优先**：凡是外部可见的东西（开放 API、token、数据结构、Mock 地址）都和 YApi 保持一致；内部实现可以自由设计。
2. **零依赖部署**：默认情况下只有一个可执行文件和一个 SQLite 数据库文件。
3. **安全默认**：默认不执行任何用户提交的代码，需要时显式开启。
4. **范围克制**：只做 README 中列出的功能，没列出的一律延后。

## 目录结构

```
cmd/apiyard/            程序入口：解析配置，启动服务
internal/config/        配置加载（命令行参数 > 环境变量 > 默认值）
internal/server/        HTTP 路由、中间件、优雅退出
internal/model/         领域模型：用户、分组、项目、分类、接口          (M1)
internal/store/         存储接口；sqlite/ 为默认实现，表结构见 docs/schema.md (M1)
internal/auth/          登录、会话、角色权限                            (M1)
internal/api/           与 YApi 路径一致的 /api/* 处理器，按资源分文件  (M1–M3)
internal/yapitoken/     开放 API 的 token 编解码                        (M3)
internal/openapi/       OpenAPI 3 / Swagger 2 导入导出                  (M3)
internal/migrate/       从 YApi 的 MongoDB 导入数据                      (M4)
internal/mock/          Mock 数据生成与 Mock 路由                        (M5)
web/                    前端构建产物，通过 embed 打进可执行文件          (M6)
internal/mcp/           MCP Server                                      (M7)
```

依赖方向：`server → api/auth/mock → store → model`，`store` 不依赖 HTTP 层。

## 关键决策

### D1：用 Go 实现

- 可以编译成单个可执行文件，跨平台交叉编译简单，内存占用低。
- `embed` 能把前端打进可执行文件；`modernc.org/sqlite` 是纯 Go 实现，不需要 CGO，交叉编译不受影响。
- goja 是纯 Go 的 JS 引擎，可以在没有系统访问能力的环境里执行 Mock 脚本。

### D2：内部数据模型以 YApi 结构为准，OpenAPI 只作为交换格式

> 这一条修正了调研报告中"内部用 OpenAPI 3 表示接口"的建议。

- 开放 API 和 YApi 前端读写的都是 YApi 的接口结构（`req_query`、`req_body_other`、`res_body` 等字段）。如果内部改用 OpenAPI，每次请求都要来回转换，而且有些字段转换后会丢失信息。
- 所以存储层保留 YApi 的字段语义：常用于查询的字段（项目、分类、路径、方法、状态）单独建列，其余的结构化字段以 JSON 存储。
- 只有在导入导出 OpenAPI / Swagger 时才做格式转换，转换逻辑集中放在 `internal/openapi`。

### D3：数据库 ID 沿用 YApi 的自增整数

- YApi 使用自增整数作为 `_id`，并在 URL 和开放 API 里直接暴露这些 ID，例如 `/project/11/interface/api/25`、`/mock/11/...`。
- 迁移时原样保留 ID，这样旧链接、书签和 Mock 地址都不会失效。

### D4：前端第一阶段沿用 YApi 原有前端

- 原有前端是 Apache-2.0 许可，可以使用，保留出处说明即可。这样能把工作量集中在后端。
- `yapi-vendor@1.12.0` 自带构建好的前端（`static/prd`），**不需要用老旧的 Node 14 + node-sass 重新构建**。
- **代价**：前端直接调用 YApi 的内部接口，共 105 个路径（见 [yapi-routes.md](yapi-routes.md)）：v0.1 要实现 68 个、占位 4 个，其余 33 个延后。所以从 M1 开始，后端就按 YApi 的路径和响应格式来实现，而不是另起一套。
- 等后端稳定后，再评估是否重写前端。
- **状态：基本确认（2026-11-04）。** 由 Go 托管预构建前端、`/api` 转发给原版 YApi 时，登录、建项目、加接口、编辑保存、WebSocket 冲突检测、Mock 都能正常工作。延后功能的隐藏方式见 D6。
- `-yapi-upstream` 是**全量转发**模式：全部 `/api`、`/mock` 交给原版 YApi，不使用 ApiYard 自己的实现，只用于验证前端托管和对比行为。M0.5 时曾设想"只转发未实现的接口"，但 ApiYard 有了自己的用户表和登录令牌后，两边数据不互通（原版无法识别 ApiYard 签发的 Cookie），混用会导致界面失效，所以 M1 改为全量转发。

### D5：Mock 默认只用 JSON Schema 生成数据

- YApi 的漏洞根源是用 `vm2`/`safeify` 在未鉴权的情况下执行用户脚本。
- M4 只支持静态响应、JSON Schema 和 Mock.js 占位符，不执行任何脚本。
- 以后如果要支持脚本 Mock，使用 goja：不注入任何宿主对象，设置执行超时，并且只有项目管理员才能开启。

### D6：用"前端补丁层"调整沿用的 YApi 前端

YApi 前端的插件在构建时就编译进了产物，无法在运行时关闭；重新构建又要维护 webpack 2 + node-sass + Node 14 这套早已停止维护的工具链。所以不改动预构建文件，而是由 Go 在返回 `index.html` 时做两件事：

1. 在 `<head>` 最前面注入 `/apiyard/patch.js`（源码在 `internal/web/patch/`，用 embed 打进可执行文件）：
   - 拦截写往 `google-analytics.com` 的图片请求。原版会把服务器主机名发给 Google 统计，内网部署同样会泄露。
   - 用 MutationObserver 隐藏暂不支持的功能入口：测试集合、高级 Mock、Wiki、系统信息、Swagger 自动同步、全局 mock 脚本。
2. 返回 `Content-Security-Policy: connect-src 'self'` 响应头，禁止前端向其他站点发起 XHR 请求（原版会请求 fastmock.site 检查新版本）。WebSocket 不受影响，已实测。

**取舍**：补丁按 DOM 结构和文字匹配，依赖前端版本；但预构建前端固定为 1.12.0，不会再变化，所以这种脆弱性是可控的。直接输入地址（如 `/project/1/wiki`）仍能打开对应页面，此时由后端对延后接口返回"未开放"。

## 与 YApi 刻意不同的地方

兼容优先，但以下几处出于安全或健壮性考虑与原版不同。它们只影响异常请求或未授权请求，正常使用时的响应与原版一致（由兼容测试保证）。

| 位置 | YApi 原版 | ApiYard | 原因 |
|---|---|---|---|
| `/api/group/add_member` | 没有任何权限检查，任何登录用户都能往任意分组加人 | 只有组长和管理员可以，否则返回 405 | 越权 |
| 动态内容（log） | 用户名、分组名等原样拼进 HTML，前端按 HTML 渲染 | 做 HTML 转义 | 存储型 XSS |
| 登录 Cookie | 只有 HttpOnly | 增加 `SameSite=Lax`；经 HTTPS 访问时再加 `Secure` | 防 CSRF；内网 HTTP 部署仍能登录 |
| `/api/group/add` | 不传 `owner_uids` 会抛异常 | 按空数组处理 | 健壮性 |
| 首次启动的管理员 | 固定密码 `ymfe.org` | 不配置密码时随机生成，只在日志里出现一次 | 避免公开的默认密码 |
| `/api/user/search` | 关键词被当作正则表达式 | 按普通文本做不区分大小写的匹配 | 避免正则注入；常见输入结果相同 |
| 参数校验的错误提示 | ajv-i18n 生成 | 模仿其格式，个别措辞可能不同 | 不值得完整复刻 ajv |
| 修改用户名、邮箱 | 只同步第一个包含该成员的项目里的副本（漏了 multi） | 同步全部分组和项目 | 数据一致 |
| 上传头像 | base64 字符串原样保存 | 解码后保存，非法数据返回 400 | 健壮性 |

## YApi 兼容要点

### 登录状态（摘自 `server/controllers/base.js`、`user.js`）

- `_yapi_token` 是 HS256 签名的 JWT，载荷 `{uid, iat, exp}`，**密钥是该用户自己的 `passsalt`**，有效期 7 天；`_yapi_uid` 是用户 ID。校验时用 uid 找到用户，再用其 passsalt 验签。
- ApiYard 生成的令牌与 YApi 逐字节一致（有单元测试），迁移后用户无需重新登录；用户改密码时 passsalt 更换，旧令牌自动失效。
- 接口处理顺序：解析登录状态 → 合并参数 → 参数校验（400）→ 登录检查（40011）→ 业务处理；业务处理抛异常时返回 `40011 服务器出错...`。

### 开放 API 白名单（共 15 个，摘自 `server/controllers/base.js`）

```
/api/open/run_auto_test     /api/open/import_data
/api/interface/add          /api/interface/save        /api/interface/up
/api/interface/get          /api/interface/list        /api/interface/list_menu
/api/interface/add_cat      /api/interface/getCatMenu  /api/interface/list_cat
/api/project/get            /api/project/up
/api/plugin/export          /api/plugin/exportSwagger
```

响应格式统一为 `{"errcode": 0, "errmsg": "成功！", "data": ...}`。

### token 编解码（摘自 `server/utils/token.js`）

- 每个项目有一个原始 token，存在 `token` 集合中。
- 对外提供的 token 是把 `"<uid>|<项目 token>"` 用 Node 的 `crypto.createCipher('aes192', passsalt)` 加密后得到的十六进制字符串。`passsalt` 来自 YApi 配置文件，默认值是 `abcde`。
- `createCipher` 的密钥派生方式是 OpenSSL 的 `EVP_BytesToKey`：MD5、迭代 1 次、不加盐。算法是 AES-192-CBC，使用 PKCS#7 填充。Go 需要严格按这个方式实现。
- 如果解密失败，就把整个 token 当作原始项目 token 处理，此时 uid 视为 `999999`。
- **迁移时必须由用户提供原 YApi 的 `passsalt`**，否则已经配置在各种工具里的旧 token 都会失效。

### 已在对照实例上确认的细节

| 项目 | YApi 的行为 | 来源 |
|---|---|---|
| 密码哈希 | `sha1(password + sha1(passsalt))`，每个用户有自己随机的 `passsalt` | `server/utils/commons.js` |
| 登录 Cookie | `_yapi_token`（httponly）和 `_yapi_uid`，有效期 7 天 | 实测 `/api/user/login` |
| 登录成功的提示 | `errmsg` 是 `"logout success..."`（原版的 bug，需要原样保留） | 实测 |
| 自增 ID | 从 11 开始，但**每次进程启动都给每个集合随机选 1~10 的步长**（防止遍历），同样的操作每次得到的 ID 不同。计数器存在 `identitycounters` 集合里。ApiYard 不需要复刻随机步长：迁移时保留原 ID，新 ID 从现有最大值往上分配即可 | `server/models/base.js` + 实测 |
| 新建分组 | 参数校验里 `owner_uids` 是可选的，但代码直接读取 `owner_uids.length`，不传就会抛异常（前端总是传 `[]`）。ApiYard 应当兼容不传的情况 | 实测 |
| 服务器异常 | 统一返回 `errcode 40011`、`errmsg "服务器出错..."`，与"未登录"共用同一个错误码 | 实测 |
| 未登录时 | `/api/user/status` 返回 `errcode 40011`，并附带 `ladp`、`canRegister` 字段（`ladp` 是原版拼写） | 实测 |
| 初始化 | 首次运行创建管理员 `adminAccount`，密码 `ymfe.org`，并写入 `init.lock` | `server/install.js` |

### 数据集合（M3 迁移范围）

| YApi 集合 | 是否迁移 | 说明 |
|---|---|---|
| user、group、project、token | 是 | 核心数据 |
| interface_cat、interface | 是 | 核心数据 |
| interface_col、interface_case | 延后 | 自动化测试相关，不在 v0.1 范围内 |
| follow | 是 | 数据量小，方便用户保留关注关系 |
| log、avatar、storage | 否 | 日志、头像、插件存储 |
