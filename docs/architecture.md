# 架构设计

2026-11 安全审计后的 token 身份、资源预算和迁移替换设计见 [安全修复与升级说明](security-hardening.md)。本文中的旧 token 行为记录用于说明历史兼容性，当前权限契约以该说明为准。

## 设计原则

1. **兼容优先**：凡是外部可见的东西（开放 API、token、数据结构、Mock 地址）都和 YApi 保持一致；内部实现可以自由设计。
2. **零依赖部署**：默认情况下只有一个可执行文件和一个 SQLite 数据库文件。
3. **安全默认**：默认不执行任何用户提交的代码，需要时显式开启。
4. **范围克制**：只做 README 中列出的功能，没列出的一律延后。

## 目录结构

```
cmd/apiyard/            程序入口：解析配置，启动服务
internal/config/        配置加载（命令行参数 > 环境变量 > 默认值）
internal/server/        HTTP 路由、前端托管、全量转发模式、优雅退出
internal/store/         SQLite 存储（领域结构体也定义在这里），表结构见 docs/schema.md (M1–M2)
internal/auth/          密码哈希、登录令牌（与 YApi 一致）                (M1)
internal/api/           与 YApi 路径一致的 /api/* 处理器，按资源分文件   (M1–M3)
internal/yapitoken/     开放 API 的 token 编解码                         (M2，原计划 M3)
internal/schemafake/    按 JSON Schema 生成示例数据（schema2json，Mock 复用）(M2)
internal/web/           托管预构建前端，注入前端补丁层（D6）             (M0.5)
internal/compat/        兼容测试框架：执行场景、归一化、比对 golden      (M0.5)
internal/openapi/       Swagger 2 / OpenAPI 3 转换为 YApi 接口（导入）   (M3)
internal/jsjson/        按 JS 规则解析和输出 JSON（保持键顺序），导入导出共用 (M3)
internal/migrate/       从 YApi 的 MongoDB 或 mongodump 目录迁移数据     (M4)
internal/mock/          Mock 服务：地址匹配、请求变量、严格模式的参数校验 (M5)
internal/mockjs/        Mock.js 的 Go 实现，以及 YApi 对它的扩展          (M5)
internal/mcp/           MCP Server（/mcp，工具调用 api 的同一套逻辑）     (M7，计划中)
web/                    用 embed 把 web/yapi 打进可执行文件                  (M6)
web/yapi/               预构建前端（由 tools/fetch-yapi-web 下载，不入库，只有占位文件 .gitkeep）
tools/                  compat（录制 / 比对兼容场景）、migcheck（核对迁移结果）、fetch-yapi-web（下载前端）、dist（交叉编译和打包）
```

依赖方向：`server → api/web/mock/mcp`，`mcp → api`（M7），`server → web`（内嵌的前端文件），`api → auth/store/yapitoken/schemafake/openapi/jsjson`，`mock → store/mockjs/schemafake/jsjson`，`schemafake → mockjs → jsjson`，`openapi → jsjson`，`migrate → store/jsjson`；`store`、`auth` 等底层包互不依赖，也不依赖 HTTP 层。

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
- **代价**：前端直接调用 YApi 的内部接口，共 104 个路径（见 [yapi-routes.md](yapi-routes.md)）：v0.1 要实现 66 个、占位 4 个，其余 33 个延后；另有 `project/download` 一个，原版也没有实现。所以从 M1 开始，后端就按 YApi 的路径和响应格式来实现，而不是另起一套。
- 等后端稳定后，再评估是否重写前端。
- **打包（M6）**：前端不随仓库提交，构建前由 `tools/fetch-yapi-web` 从 npm 下载并校验，再由 `web` 包 embed 进可执行文件，不指定 `-web-dir` 时使用内嵌的前端。下载时删掉已有 `.gz` 的原文件（可执行文件小 6MB），不支持 gzip 的客户端由服务端解压。yapi-vendor 里的 `.gz` 和原文件是两次混淆的结果，变量名不同、功能相同；浏览器在原版里拿到的也一直是 `.gz`。
- **状态：基本确认（2026-11-04）。** 由 Go 托管预构建前端、`/api` 转发给原版 YApi 时，登录、建项目、加接口、编辑保存、WebSocket 冲突检测、Mock 都能正常工作。延后功能的隐藏方式见 D6。
- `-yapi-upstream` 是**全量转发**模式：全部 `/api`、`/mock` 交给原版 YApi，不使用 ApiYard 自己的实现，只用于验证前端托管和对比行为。M0.5 时曾设想"只转发未实现的接口"，但 ApiYard 有了自己的用户表和登录令牌后，两边数据不互通（原版无法识别 ApiYard 签发的 Cookie），混用会导致界面失效，所以 M1 改为全量转发。

### D5：Mock 默认只用 JSON Schema 生成数据

- YApi 的漏洞根源是用 `vm2`/`safeify` 在未鉴权的情况下执行用户脚本。
- M5（Mock 服务）只支持静态响应、JSON Schema 和 Mock.js 占位符，不执行任何脚本。
- 以后如果要支持脚本 Mock，使用 goja：不注入任何宿主对象，设置执行超时，并且只有项目管理员才能开启。
- **状态：已实现（2026-11-09）。** 实现时又发现两处：Mock.js 用 `eval` 解析占位符括号里的参数，在原版里不写脚本、只写模板也能执行代码（实测 `@pick([process.version])` 返回 `v14.21.3`），ApiYard 只按字面量解析；Mock 地址与界面同源，返回的 HTML 能在界面的源里运行脚本，ApiYard 给 Mock 响应加上 `Content-Security-Policy: sandbox`。

### D6：用"前端补丁层"调整沿用的 YApi 前端

YApi 前端的插件在构建时就编译进了产物，无法在运行时关闭；重新构建又要维护 webpack 2 + node-sass + Node 14 这套早已停止维护的工具链。所以不改动预构建文件，而是由 Go 在返回 `index.html` 时做两件事：

1. 在 `<head>` 最前面注入 `/apiyard/patch.js`（源码在 `internal/web/patch/`，用 embed 打进可执行文件）：
   - 拦截写往 `google-analytics.com` 的图片请求。原版会把服务器主机名发给 Google 统计，内网部署同样会泄露。
   - 用 MutationObserver 隐藏暂不支持的功能入口：测试集合、高级 Mock、Wiki、系统信息、Swagger 自动同步、全局 mock 脚本。
2. 返回 `Content-Security-Policy: connect-src 'self'` 响应头，禁止前端向其他站点发起 XHR 请求（原版会请求 fastmock.site 检查新版本）。WebSocket 不受影响，已实测。

此外把界面上的 YApi 名称、logo 和链接换成 ApiYard（`internal/web/brand.go`）：启动时读取 `index.html` 和前端主包，按固定的字符串替换标题、logo 组件、页脚（源码、反馈、版本号，保留对 YApi 的致谢）以及“使用文档”入口，结果保存在内存里。`assets.js` 里的主包和样式表地址加上内容哈希，浏览器不会继续用缓存里的原版文件。

界面外观也换成 ApiYard 自己的（2026-11-09）。只换名字时，布局、配色和首页宣传页都和 YApi 一模一样，一眼就能认出来。完全重写前端要 4–8 周，所以分三层处理，内部页面的结构和操作方式保持不变，从 YApi 迁移过来的用户不用重新学：

- **配色**：在原版样式表里按字符串替换颜色值（`cssRewrites`）：蓝色主色及其悬停、按下、浅底各档换成青绿色，文字和边框换成偏冷的中性色，圆角从 4px 加大到 6px，antd 的字体栈去掉两个别名字体。直接替换颜色值，所有控件的各种状态都能一并换掉，比逐个写覆盖样式可靠。请求方法标签的颜色写在主包里（`METHOD_COLOR`），一并替换。
- **布局**：`patch/theme.css` 插在 `</head>` 之前（排在原版样式表之后），把深色顶栏、深色分组侧栏改成白底，卡片式标签页改成下划线样式，项目卡片、接口表格、页脚重新排版。少数白底和外边距是行内样式，只能用 `!important` 覆盖。
- **登录页**：原版的首页（宣传落地页）和登录页由服务端直接换成 `patch/login.html`，调用与原版相同的 `/api/user/login`、`/api/user/reg`；已登录时跳到分组页，关闭注册时隐藏注册入口。原版在未登录、退出登录时用 `history.push('/')` 做站内跳转，补丁脚本拦截后改为整页打开 `/login?redirect=原页面`，登录后回到原页面；`redirect` 只接受站内路径。

界面只用系统字体，不请求外部字体。具体功能的帮助链接（Mock 规则、数据导入、开放 API 等）用法与 YApi 相同，仍指向 YApi 文档；已隐藏功能里的文字不改。每条替换都核对出现次数，与 1.12.0 不一致时整体放弃，按原样返回前端并在日志里警告。

**取舍**：补丁按 DOM 结构和文字匹配，品牌和配色替换按字符串匹配，主题样式按原版的类名覆盖，都依赖前端版本；但预构建前端固定为 1.12.0，不会再变化，所以这种脆弱性是可控的，测试会用打包的真实前端核对每条替换。直接输入地址（如 `/project/1/wiki`）仍能打开对应页面，但后端没有延后的接口（返回 404），页面加载不出数据。

### D7：MCP Server 内置在服务端（M7，计划中）

- 地址 `/mcp`，Streamable HTTP，用官方 Go SDK 以无状态模式运行，同时支持 `2025-11-25` 和 `2026-07-28` 两个协议版本。
- 鉴权沿用开放 API 的项目 token（`Authorization: Bearer`，多个项目用逗号分隔），权限按 token 所属用户计算，MCP 能做的事不超过开放 API 已经允许的范围。
- 工具直接调用 `internal/api` 的逻辑，权限、校验、动态记录与界面一致；不提供删除工具，可用 `-mcp off|readonly` 关闭写入或整个服务。
- 工具清单、任务和验收标准见 [roadmap.md](roadmap.md) 的 M7。

## 与 YApi 刻意不同的地方

兼容优先，但下面这些地方与原版不同，分两类列出。

### 只影响异常请求或未授权请求

这些差异出于安全或健壮性考虑。正常使用时，响应与原版一致，由兼容测试保证；只有在越权访问、参数异常、名称里含有 HTML 特殊字符，或者走到原版出错的分支时，才看得出区别。

| 位置 | YApi 原版 | ApiYard | 原因 |
|---|---|---|---|
| `/api/group/add_member` | 没有任何权限检查，任何登录用户都能往任意分组加人 | 只有组长和管理员可以，否则返回 405 | 越权 |
| 动态内容（log） | 用户名、分组名等原样拼进 HTML，前端按 HTML 渲染 | 做 HTML 转义 | 存储型 XSS |
| 登录 Cookie | 只有 HttpOnly | 增加 `SameSite=Lax`；经 HTTPS 访问时再加 `Secure` | 防 CSRF；内网 HTTP 部署仍能登录 |
| `/api/group/add` | 不传 `owner_uids` 会抛异常 | 按空数组处理 | 健壮性 |
| `/api/user/search` | 关键词被当作正则表达式 | 按普通文本做不区分大小写的匹配 | 避免正则注入；常见输入结果相同 |
| 参数校验的错误提示 | ajv-i18n 生成 | 模仿其格式，个别措辞可能不同 | 不值得完整复刻 ajv |
| 上传头像 | base64 字符串原样保存 | 解码后保存，非法数据返回 400 | 健壮性 |
| `/api/project/token` | 不检查权限，任何登录用户都能拿到任意项目的 token | 要求开发者及以上 | token 能通过开放 API 读写项目 |
| `/api/project/update_token` | 不检查权限；生成的 token 漏了 uid（加密的是 `undefined|...`），开放 API 无法对应到用户 | 要求组长或管理员；使用当前用户的 uid | 越权；修正 bug |
| `/api/project/change_member_email_notice` | 不检查权限，任何登录用户都能改别人的邮件通知设置 | 只允许本人，或组长、管理员 | 越权 |
| `/api/project/get_member_list` | 不检查权限，任何登录用户都能看私有项目的成员 | 私有项目要求有查看权限，否则 406（与 `project/get` 一致） | 信息泄露 |
| 接口所属分类 | 不检查分类是否存在、是否属于该项目；`interface/up` 还能借 `project_id` 把接口移到别的项目 | 分类必须属于该项目，否则 400；忽略 `project_id` | 越权、数据一致 |
| `/api/interface/del`、`del_cat` | 创建者可以直接删除 | 创建者也要仍有编辑权限（被移出项目后不能再删） | 越权 |
| `/api/interface/up_index`、`up_cat_index` | 不检查权限 | 只改有编辑权限的项目中的接口、分类，其余忽略，响应不变 | 越权 |
| `/api/user/project` 的接口类型 | 不检查权限，能读到私有项目的接口定义 | 私有项目要求查看权限，否则 406 | 信息泄露 |
| `/api/project/copy` | 不检查源项目的权限；参数 `cat` 可以指向任意项目的分类 | 要求对源项目有查看权限，只复制属于源项目的分类下的接口；整个复制在一个事务里完成 | 信息泄露、数据一致 |
| `/api/interface/solve_conflict` | 不检查权限；被挡住的连接关闭时也会清除编辑者，第三个人就能同时进入编辑 | 要求编辑权限；只有占用成功的连接关闭时才清除；进程重启时清除全部编辑状态 | 修正 bug |
| 修改说明 `message` | 原样拼进动态的 HTML | 转义后再把换行换成 `<br>` | 存储型 XSS |
| form 请求体缺少 `req_headers` | 抛异常 | 按空数组处理 | 健壮性 |
| `/api/log/list`（项目动态）、`list_by_update` | 不检查权限，能读到私有项目的动态和改动内容 | 私有项目要求查看权限，否则 406 | 信息泄露 |
| `/api/user/login_by_ldap` | 没配置 LDAP 时返回 401 和一条 JS 异常信息 | 返回 401 和“暂不支持 LDAP 登录” | 提示更清楚 |
| 开放 API token 越权 | 通过 token 访问时，新建、修改接口和新建分类完全不检查权限，任何项目的 token 都能改其他项目的接口和分类 | token 只能操作所属项目；对其他项目按"没有权限"处理（各接口原有的错误码） | 越权 |
| 伪造的开放 API token | 能解密但项目 token 不存在时，只写了 42014 就继续执行，把请求当成 token 里 uid 对应的用户；默认 passsalt 下等于可以伪造任意用户 | 返回 42014 `token 无效` | 身份伪造 |
| 用户已删除的 token | 当前用户为空，部分接口能用、部分接口出错 | 返回 42014 `token 无效` | 健壮性 |
| `/api/plugin/export`、`exportSwagger` | 不检查任何权限，任何登录用户都能导出任意项目 | 私有项目要求查看权限，否则 406；通过 token 访问时只能导出 token 所属项目 | 信息泄露 |
| `/api/open/import_data` 的 token | token 无效时继续执行，拿这个 token 调用其他接口，再逐个失败 | 直接返回 40011 | 健壮性 |
| Mock.js 占位符的参数 | 用 `eval` 求值，括号里可以写任意 JS 代码 | 只接受字面量（数字、字符串、数组、对象），其余与原版出错时一样按逗号拆成字符串；正常写法结果相同 | 代码执行 |
| Mock 的跨域响应头 | 请求没有 `Origin` 时输出 `Access-Control-Allow-Origin: undefined` | 没有 `Origin` 时不输出 | 健壮性 |
| Mock 的 multipart 请求体 | 不限大小，文件写到临时目录 | 最多 10MB，文件内容不保存，只记录文件名、类型和大小 | 资源占用 |
| Mock.js 的 `'x\|10.0': 1` | 小数位数取 0 到 2^53 之间的随机数，一直补位直到内存耗尽 | 小数位最多 17 位 | 拒绝服务 |

### 正常使用中也能看到的差异

这些差异在正常操作中就能察觉。每一项都是权衡后的选择：修正原版的 bug、让结果稳定，或者暂不提供有风险的功能。兼容测试的场景避开了这些情况，或者用 `volatile` 处理随机结果。

| 位置 | YApi 原版 | ApiYard | 原因 |
|---|---|---|---|
| 首次启动的管理员 | 固定密码 `ymfe.org` | 不配置密码时随机生成，只在日志里出现一次 | 避免公开的默认密码 |
| 修改用户名、邮箱 | 只同步第一个包含该成员的项目里的副本（漏了 multi） | 同步全部分组和项目 | 数据一致 |
| 修改项目图标颜色 | 只更新当前用户自己的关注记录 | 更新所有关注该项目的记录 | 数据一致 |
| 动态列表的顺序 | 只按秒级时间排序，同一秒内 MongoDB 按插入顺序返回；分组动态（`$or` 查询）同一秒内的先后取决于执行计划 | 时间相同时按 ID 升序：单个项目、分组本身的动态与原版相同，分组里多个项目的动态同一秒内可能先后不同 | 结果稳定 |
| `/api/interface/up` 的 `nModified` | `up_time` 变了也算修改，同一秒内重复保存是 0，否则是 1 | 忽略 `up_time`，内容没变就是 0 | 结果稳定 |
| `/api/interface/schema2json` | json-schema-faker | 自己实现的生成器（`internal/schemafake`），覆盖常用关键字；结果本来就是随机的 | 不依赖 Node |
| `/api/project/swagger_url` | 服务端去请求用户给的任意 URL | 暂不提供，返回 402 和提示；“从 URL 导入”在 M3 做导入功能时再决定是否提供（加地址限制） | SSRF |
| HTML 导出 | markdown-it 渲染，左侧是 markdown-it-table-of-contents 生成的目录 | goldmark 渲染，目录按"分类 → 接口"生成；样式表沿用原版，页眉页脚文字不同 | HTML 渲染器无法逐字一致；JSON、Markdown、Swagger 导出与原版一致 |
| 项目的全局 Mock 脚本 | 开启后在 vm 沙箱里执行，可以改返回数据、状态码、响应头、延迟 | 不执行（D5），响应头 `X-ApiYard-Mock-Script: ignored` 提示脚本被忽略；迁移报告列出开启了脚本的项目；界面上的入口已隐藏 | 远程代码执行 |
| 高级 Mock（期望、接口级脚本） | 插件提供 | 暂不提供，界面入口已隐藏 | 延后功能；脚本部分同上 |
| JSON Schema 的 Mock 数据 | 代码里设置了 `alwaysFakeOptionals: true`，但 json-schema-faker 0.5.0-rc16 有缺陷：非必填字段随机缺失，没有 `maxItems` 的数组总是空的 | 按原版的设置生成全部字段，数组默认 1–3 项（`interface/schema2json` 相同） | 修正 bug |
| Mock 响应头 | 没有安全相关的响应头 | 加上 `Content-Security-Policy: sandbox`、`X-Content-Type-Options: nosniff`：在浏览器里直接打开 Mock 地址时，返回的 HTML 与界面隔离；fetch、XHR 等调用不受影响 | 存储型 XSS |
| `/api/open/import_data` | 1.12 的服务端 Swagger 转换在 Node 里加载就出错（swagger-client 依赖浏览器的 File），总是返回 40011，接口实际不可用 | 按代码本意实现，转换结果与原版 run.js 一致；不支持 `url` 参数和以 http 开头的 `json` | 修正 bug；URL 导入有 SSRF 风险 |

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
| 接口路径 | `path` 原样保存（含查询串），查重也按原样比较；`query_path` 存拆开的路径和参数；`:name`、`{name}` 自动加入 `req_params` | `controllers/interface.js` + 实测 |
| 接口列表的字段 | `list`、`list_cat` 的 select 写成 `"up_time, index, tag"`，逗号让 `up_time`、`index` 失效，所以分页结果里没有这两个字段；`limit=all` 时按标题排序且 `total` 为 null | `models/interface.js` + 实测 |
| 修改接口写动态的条件 | 只有 `common/diff-view.js` 比较的字段（路径、名称、方法、分类、状态、tag、各类参数、请求体、响应体）有变化才写；只改备注不写。动态内容里有固定的换行和 20 个空格的缩进 | 实测 |
| `/api/interface/save` | 响应只是按路径查到的 `[{_id, res_body}]`，新建时是 `[]`，不反映修改或新建是否成功 | 实测 |
| `/api/interface/getCatMenu` | 私有项目要求编辑权限（访客看不到）；项目不存在时异常没有被处理，Koa 返回 404 | 源码 |
| 复制项目 | 不建默认分类；分类按参数 `cat` 的顺序新建（index 都是 0），接口按标题排序复制并保留原 index；`is_json5` 取模型默认值 true | 实测 |
| 开放 API token | 只对 15 个白名单接口和 `/api/open/*` 生效；`project_id` 被强制改成 token 所属项目（参数校验之前）。新格式 token 代表生成它的用户，按其角色判断读权限；旧格式（原始 token）代表 uid 999999 的 system 普通会员，读不了私有项目，但写接口不检查角色。解不开也找不到的 token 被忽略 | `controllers/base.js` + 实测 |
| token 访问的特殊情况 | `add_cat` 只读请求体，token 不会补上 `project_id`；`project/get` 只给 token 不给 `id` 时，`cat` 为空数组，`role` 对管理员是 admin、其他用户是 false；`interface/get` 的接口不属于 token 项目时返回 400 `token有误` | 实测 |
| 导出 | 分类按 index 排序，分类内按 index、标题排序，没有接口的分类不导出；`status=open` 只导出公开接口。JSON 导出中分类去掉了 `_id` 等字段，接口却保留完整字段（原版删的是 Mongoose 文档对象上的属性，没有生效）。项目不存在、请求体或响应体不是合法 JSON（Swagger）时返回 502 `下载出错` | `exts/yapi-plugin-export-*` + 实测 |
| Markdown 导出 | 字符串拼接，可以逐字复现：JSON Schema 转成 HTML 表格，`<p key=N>` 的 N 把值为 undefined 的字段也算在内；数组行的 key 是 `array-N`，N 来自进程内一直递增的计数器 | `common/markdown.js`、`common/schema-transformTo-table.js` |
| 数据导入 | 界面导入在**浏览器里解析**文件（Swagger、Postman、HAR、YApi JSON），再按"按名称复用或新建分类 → 有 basePath 时修改项目 → 逐个 `interface/add`（普通模式）或 `interface/save`（智能合并 good、完全覆盖 merge）"写入；`interUpload` 不会被请求。开放 API 的 `import_data` 在服务端走同样的流程，没有标签的接口放进第一个分类；参数 `merge` 默认 normal，旧参数名 `dataSync` 会被参数校验去掉，不起作用 | `common/HandleImportData.js` + 实测 |
| Mock 地址 | `/mock/{项目 ID}` 后面的部分按项目基础路径的**长度**截掉开头（不检查前缀是否相同），再依次按完整路径、`query_path`、路径参数模板查找；`query_path` 匹配时只看最后一个参数；路径参数模板有多个匹配时最后一个胜出（原版的权重比较写错了），路径参数并入 `query`。项目 ID 转不成数字时返回 403 CastError，项目不存在时也是 403（`toObject` of null），走不到“不存在的项目”那个分支 | `server/middleware/mockServer.js` + 实测 |
| Mock 返回数据 | `res_body` 用 JSON5 解析（失败时当作字符串模板），先由 `common/mock-extra.js` 替换 `${query.x}` 这类变量（整个值替换成请求里的数据）、把 `name\|regexp` 转成正则，再交给 Mock.js；Mock.js 出错时返回处理前的数据。Koa 的输出规则：null 为 204，字符串为 text/plain（以 `<` 开头为 text/html），其余为 JSON | 源码 + 实测 |
| 严格模式 | 检查 `required` 为 `"1"` 的查询参数（空字符串也算缺少）和表单字段，JSON Schema 请求体用 ajv 5（draft-04、遇到第一个错误就停、不校验 format）加 ajv-i18n 中文信息。在 `properties` 里有规则的必需属性要等校验 `properties` 时才检查，报错是 `name` 而不是 `.name`；`anyOf`、`oneOf` 会把每个分支的第一个错误一起列出 | `common/utils.js` + 容器内运行原版代码对照 |
| Swagger 转换 | 由 swagger-client 展开 `$ref`，展开后的对象末尾加 `"$$ref": "原引用"`；循环引用的内层不展开；`allOf` 深度合并；路径级参数追加到操作参数后面。版本号形如 `v1` 的 tag 不作为分类 | `exts/yapi-plugin-import-swagger/run.js` + 容器内运行原版代码对照 |

### 数据集合（M4 迁移范围）

| YApi 集合 | 是否迁移 | 说明 |
|---|---|---|
| user、group、project、token | 是 | 核心数据 |
| interface_cat、interface | 是 | 核心数据 |
| follow、log、avatar | 是 | 关注、操作动态、头像；数据量不大，迁移后界面与原来一样 |
| interface_col、interface_case | 延后 | 自动化测试相关，不在 v0.1 范围内 |
| wiki、adv_mock、statis_mock、storage 等 | 否 | 插件数据 |

迁移的细节（类型转换、孤儿数据、重复成员）见 [migration.md](migration.md)。另外两点是迁移实测时才发现的：

- 动态只按秒级时间排序，同一秒内的记录 MongoDB 按插入顺序返回，所以 ApiYard 同一秒内按 ID 升序。分组动态是 `$or` 查询，同一秒内分组和各项目的动态谁先谁后取决于 MongoDB 的执行计划（与组内项目数有关），不去复现。
- YApi 删除项目时不清理它的分类和 token，迁移时作为孤儿数据跳过。
