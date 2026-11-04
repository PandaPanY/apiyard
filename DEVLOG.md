# 开发日志

> 按日期倒序记录，最新的在最上面。每条只写要点。

## 2026-11-04 M2-4：其余接口（M2 接口全部完成）

- 完成：
  - `log/list_by_update`（数据导入时查各接口最近一次修改）；新场景 `06-misc`（15 次调用），场景 01–06 共 191 次调用与原版一致。
  - 占位接口：LDAP 登录返回明确提示；第三方登录与原版一样跳回首页；`swagger_url` 暂不提供；`download_crx` 直接提供前端目录里的插件包。
  - 浏览器实测：用第二个用户占用编辑状态，界面正确提示“第二编辑者正在编辑该接口”；界面上新建分类、数据管理页、JSON Schema 预览正常。
- 问题 / 决定：
  - `project/download` 在 YApi 1.12 中根本没有实现（前端按钮是隐藏的），原版返回 404，ApiYard 保持一致，不再算作待办。
  - `log/list` 的项目动态和 `list_by_update` 对私有项目增加查看权限检查（动态里带有接口改动的完整内容）。
  - M2 验收里“公开仓库”还没做：需要先清理文档中的内网地址和主机名。
- 下一步：清理文档后公开仓库，M2 收尾；然后进入 M3。

## 2026-11-04 M2-3：分类与接口

- 完成：
  - 分类与接口 16 个接口：`add`、`save`、`up`、`get`、`list`、`list_cat`、`list_menu`、`del`、`add_cat`、`up_cat`、`del_cat`、`getCatMenu`、`up_index`、`up_cat_index`、`schema2json`，以及 WebSocket `solve_conflict`（编辑冲突检测，依赖 `coder/websocket`）。
  - `project/copy`（连同分类、接口、成员一起复制）；`user/project` 的接口类型。
  - `internal/schemafake`：JSON Schema 生成示例数据，替代 json-schema-faker。
  - 新场景 `05-interface`（50 次调用），自检稳定；场景 01–05 共 176 次调用与原版完全一致，变异测试确认比对有效。
  - 浏览器实测：新建接口、编辑保存（进入编辑页占用、离开后释放）、动态里的“改动详情”、复制项目都正常。
- 问题 / 决定：
  - 原版新建 form 接口时不传 `req_headers` 会出错，场景里补上；ApiYard 按空数组处理。
  - 参数解析补齐 ajv 的行为：请求体可以是数组（`up_index`），同名查询参数得到数组（前端按多个状态筛选），字符串字段接受布尔，新增 boolean 规则。
  - 与原版的刻意差异新增 11 处，多为权限和数据一致性修正（分类归属、删除权限、排序权限、复制源项目权限、编辑冲突释放等），见 architecture.md 的差异表。
  - `nModified` 忽略 `up_time`，否则同一秒内和跨秒保存的结果不同。
- 下一步：M2-4（`log/list_by_update`、4 个占位接口、`project/download`），完成后 M2 收尾。

## 2026-11-04 M2-2：项目成员

- 完成：
  - 项目成员 5 个接口：`get_member_list`、`add_member`、`del_member`、`change_member_role`、`change_member_email_notice`。
  - 新场景 `04-project-member`（28 次调用），首次录制就能稳定复现；场景 01–04 共 126 次调用与原版完全一致。
  - 浏览器实测：成员管理页面能正确显示项目成员、角色和邮件通知开关。
- 问题 / 决定：
  - "复制项目"会连同接口一起复制，移到 M2-3 接口完成之后再做，才能真正测到。
  - 安全修正：`change_member_email_notice` 只允许本人或组长修改（原版谁都能改别人的），`get_member_list` 对私有项目要求查看权限。
  - 前端在个人空间里的项目上不显示"成员管理"，这是原版的设计，不是 bug。
- 下一步：M2-3 分类与接口。

## 2026-11-04 M2-1：项目、关注与动态

- 完成：
  - M2 按依赖拆成四部分（见 roadmap.md）。本次完成 M2-1：项目 13 个接口、关注 3 个、`log/list`，以及 `user/project` 的项目类型。
  - `internal/yapitoken`：开放 API token 的编解码（EVP_BytesToKey + AES-192-CBC），用对照实例的真实 token 做测试，**与 YApi 逐字节一致**。原计划在 M3 做，因为 `project/token` 需要而提前。
  - 新场景 `03-project`（45 次调用）；场景 01–03 共 98 次调用与原版完全一致，变异测试确认比对有效。
  - 浏览器实测：新建项目、项目页、设置页、token、环境配置正常。
- 问题 / 决定：
  - 自检又发现两类变化值：驼峰 ID 字段（`groupId`）和动态内容链接里的 ID，已补充归一化规则；YApi 的动态在同一秒内顺序不固定，兼容框架新增 `unordered`，随机 token 用新增的 `volatile` 处理。
  - 前端补丁原来用 requestAnimationFrame 批处理，页面不渲染时 rAF 暂停会导致入口再也藏不住。改为在 MutationObserver 回调里直接处理。
  - 安全修正：`project/token` 增加权限检查（原版任何登录用户都能拿到任意项目的 token），`update_token` 要求组长并修正漏传 uid 的 bug。
  - 新增配置 `-token-salt`（对应 YApi 的 passsalt）。
- 下一步：M2-2 项目成员与复制项目。

## 2026-11-04 M1（下）：个人中心与用户管理（M1 完成）

- 完成：
  - 6 个接口：`user/find`、`update`、`change_password`、`del`、`upload_avatar`、`project`。M1 的 24 个接口全部完成。
  - 新场景 `02-user-profile`（32 次调用）：权限限制、改名后成员副本同步、改密码后旧登录状态失效、头像的新建与覆盖等。场景 01、02 共 53 次调用与原版完全一致。
  - 浏览器实测：个人中心页面、修改用户名、用户管理列表都正常。
- 问题 / 决定：
  - 兼容测试改为所有场景按顺序在同一个实例上执行，与录制方式一致（场景 02 依赖 01 注册的用户）。
  - golden 文件改为记录请求模板，ID 不同不再导致文件变动；两次独立录制的文件完全相同。
  - `user/project` 中项目、接口两种类型目前只会查不到（项目还不能创建），响应与 YApi 查不到时相同，M2 补全。
- 下一步：M2 项目与接口。

## 2026-11-04 M1（上）：存储层、登录与分组

- 完成：
  - `internal/store`：SQLite（modernc，纯 Go）、按 `user_version` 执行的版本化迁移，用户 / 分组 / 成员 / 动态的数据访问；覆盖率 78%。
  - `internal/auth`：YApi 兼容的密码哈希和 JWT 登录令牌。用对照实例的真实数据做测试向量，**生成的令牌与 YApi 逐字节一致**。
  - `internal/api`：处理顺序与 YApi 的 createAction 相同（参数校验 → 登录检查 → 业务处理）；实现 18 个接口：用户 8 个（status、login、reg、logout、avatar、search、list、up_study）、分组 10 个。
  - 首次启动自动创建管理员（不配置密码时随机生成）。
  - `go test` 自动执行兼容比对：场景 `01-user-and-group` 的 21 次调用与原版 YApi 完全一致；用变异测试确认比对确实有效。
  - 浏览器实测：登录、退出指引、新建分组、查看分组和成员列表都正常。
- 问题 / 决定：
  - 从源码确认登录令牌是用户自己的 passsalt 签名的 JWT，照此实现后迁移来的用户无需重新登录。
  - 改了 3 处表结构：`custom_field1` 允许 NULL（YApi 修改分组后会变成 null）、ID 从 11 开始、迁移文件移到 `internal/store/migrations`。
  - `-yapi-upstream` 改为全量转发：ApiYard 的登录令牌原版无法识别，两边无法混用。
  - 出于安全考虑与原版刻意不同的地方，统一记录在 architecture.md：`add_member` 增加权限检查、动态内容做 HTML 转义、Cookie 增加 SameSite、管理员默认密码随机生成等。
  - 关注（follow）的 3 个接口依赖项目数据，移到 M2。
- 下一步：M1（下）——用户剩下的接口（find、update、change_password、del、upload_avatar、project），并补充对应的兼容场景。

## 2026-11-04 建立仓库

- 完成：创建私有仓库 `PandaPanY/apiyard`，模块路径改为 `github.com/PandaPanY/apiyard`，提交 M0 和 M0.5 的全部成果。
- 问题 / 决定：
  - 先保持私有，M2 完成、界面的核心流程可用后再公开。
  - 提交使用 GitHub 匿名邮箱（只在本仓库设置），公开后不会暴露真实邮箱。
  - 联系 ApiMind 作者（M0.5 第 5 项）推迟到公开时再做：对方的帖子发布 5 个多月没有回复，也没有公开仓库。
- 下一步：M1 账号与分组。

## 2026-11-04 M0.5：表结构草案（M0.5 完成）

- 完成：
  - `internal/store/sqlite/migrations/0001_init.sql`：v0.1 范围内的 11 张表；设计说明见 `docs/schema.md`。
  - 用 SQLite 3.53 校验通过：18 项约束、ID 分配（迁移后续号、按 YApi 计数器抬高、删除后不复用）、级联删除、5 个关键查询都命中索引。
  - M0.5 的验收标准全部满足；第 5 项（联系 ApiMind 作者）需要你来做。
- 问题 / 决定：
  - 唯一约束和枚举 CHECK 只加在 YApi 本身就会保证的字段上，避免运行多年的真实数据因重复或异常值导致迁移失败。
  - 发现响应中普遍带 `__v: 0`，嵌套数组元素带 ObjectId，都需要接口层处理，已记入 schema.md。
  - 兼容测试的 ID 归一化补上了关注列表的 `projectid` 字段。
- 下一步：M1 账号与分组——存储层落地、登录会话、27 个接口，用 `01-user-and-group` 场景验收。

## 2026-11-04 M0.5：兼容测试框架

- 完成：
  - `internal/compat` + `tools/compat`：场景执行（变量捕获、模板、重复步骤、期望错误码）、归一化、golden 文件读写、逐字段比对。用法见 `docs/testing.md`。
  - 场景 `01-user-and-group`（21 次调用，M1 用）和 `90-seed-demo`（50 次调用，生成示例数据）；`testenv/yapi/reset.sh` 用于重置对照实例。
  - 三项验证：原版 YApi 两次独立运行的结果完全一致（录制可重复）；经 ApiYard 转发后 71 次调用全部一致（转发层透明）；状态不符时报错清楚、退出码为 1。
  - 对照实例现在保留了示例数据（2 个分组、3 个项目、38 个接口）。
- 问题 / 决定：
  - 自检发现 YApi 每次启动都会给每个集合随机选 1~10 的自增步长，ID 无法复现，因此把 ID 归一化为 `<id>`，引用关系由场景流程本身保证。ApiYard 不复刻随机步长（已写入 architecture.md）。
  - 原版的 bug：新建分组不传 `owner_uids` 会抛异常。场景按前端的实际行为传 `[]`，ApiYard 应当兼容不传的情况。
- 下一步：M0.5 剩下第 5 项（联系 ApiMind 作者，需要你来做）和第 6 项（SQLite 表结构草案）。

## 2026-11-04 M0.5：前端补丁层

- 完成：
  - 决定用"前端补丁层"调整沿用的 YApi 前端（architecture.md D6）：Go 在 index.html 里注入 `/apiyard/patch.js`，并返回 CSP 响应头 `connect-src 'self'`。
  - 浏览器实测：6 个延后功能的入口都已隐藏；GA 图片请求被拦截，普通图片不受影响；fastmock 版本检查被 CSP 拦截，没有真正发出网络请求；WebSocket 和其他功能正常。
- 问题 / 决定：
  - 没有选择重新构建前端：插件是构建时编译进去的，但重新构建要维护一套早已停止维护的工具链，代价太大。
  - 验证时发现"全局mock脚本"（原版 RCE 漏洞的源头）也应该隐藏，已加入隐藏列表。
- 下一步：M0.5 第 4 项，兼容测试框架（录制 golden 文件 + 比对工具），以及预置示例数据。

## 2026-11-04 M0.5：Go 托管 YApi 前端

- 完成：
  - `tools/fetch-yapi-web`：从 npm 下载 yapi-vendor，按 SHA-512 校验后只解压 `static/` 到 `web/yapi`（已加入 gitignore）。
  - `internal/web`：按 YApi `app.js` 的规则托管前端（前端路由回退到 index.html；浏览器支持 gzip 时返回预压缩的 .gz 文件；带哈希的资源长期缓存）。
  - `-yapi-upstream`：把未实现的 `/api`、`/mock` 转发给原版 YApi，支持 WebSocket。
  - 浏览器实测通过：登录、新建项目、添加和编辑接口、保存、WebSocket 冲突检测、Mock 都正常。D4（沿用原有前端）基本确认。
- 问题 / 决定：
  - 常见扩展名的 MIME 类型写死在代码里：Go 默认参考 Windows 注册表，可能把 .js 识别成 text/plain。
  - 原版前端会请求 Google Analytics 和 fastmock.site，留到第 3 项处理（考虑用 CSP 禁止外连）。
  - Claude 的 shell 运行在沙箱里，写入 AppData 的内容不会生效到真实环境：之前的 `go env -w` 只在沙箱里生效，已改在真实终端重新执行。真实文件确认都在 D 盘。
- 下一步：M0.5 第 3 项（怎么隐藏延后的功能、禁止外连），然后是第 4 项兼容测试框架。

## 2026-11-04 M0.5：YApi 对照实例

- 完成：
  - 编写 `testenv/yapi/`（Dockerfile、compose、配置、入口脚本、README），在测试服务器上运行 YApi 1.12.0 + MongoDB 4.4，数据放在 `/data`。
  - 验证：端口只绑定 127.0.0.1，局域网无法访问；重启后不会重复初始化；接口登录和通过 SSH 隧道访问界面都正常。
  - 新增 `.gitattributes`，保证 `.sh`、`Dockerfile`、`.yml` 始终用 LF 换行。
- 问题 / 决定：
  - YApi 的 `install.js` 失败时进程不会退出，入口脚本加了 `timeout` 兜底。
  - 记录了几个必须原样兼容的细节：密码哈希算法、Cookie 名称、登录成功时 `errmsg` 为 `"logout success..."`、自增 ID 从 11 开始。已写入 `architecture.md`。
- 下一步：M0.5 第 2 项，让 Go 托管预构建的前端，并把 `/api` 转发给对照实例，验证界面能否正常工作。

## 2026-11-04 测试服务器

- 完成：
  - 确定局域网测试服务器（Ubuntu 24.04），配置了免密登录。
  - 清空两块闲置 SSD，格式化为 ext4：128G 挂到 `/data`，64G 挂到 `/data2`，开机自动挂载（`nofail`），归 `pan` 用户所有。
  - 安装 Docker 29.1.3 + Compose 2.40 + buildx 0.30.1；Docker 和 containerd 的数据目录都放在 `/data`，系统盘零占用；apt 源换成中科大镜像。
- 问题 / 决定：YApi 对照实例放在这台服务器上，用 Docker 部署。本机 PowerShell 5.1 会按 GBK 读取 `.ps1` 文件、`ssh` 实际调用的是 Git 的 MSYS 版，因此给终端运行的脚本只写 ASCII。
- 下一步：开始 M0.5，先在 `testenv/yapi/` 写 YApi 对照实例的部署文件。

## 2026-11-03 路线图

- 完成：
  - 编写 `docs/roadmap.md`：M0.5–M6 每个里程碑的任务、验收标准和工时预估（到 v0.1 约 16 周），以及测试策略和风险决策点。
  - 静态扫描 YApi 前端源码，整理出 105 个接口并按里程碑归类，见 `docs/yapi-routes.md`。
- 问题 / 决定：
  - 沿用 YApi 前端就意味着从 M1 起要直接按 YApi 的接口来实现，因此里程碑改为按"界面能用起来的顺序"划分，并新增 M0.5 前置验证阶段。
  - 发现 `yapi-vendor` 自带构建好的前端，不需要重新构建，前端方面的风险大幅降低。
  - 本机没有 Docker，WSL 也没有装发行版；对照实例怎么搭（nvm + 免安装版 MongoDB，还是装 Docker Desktop）待定。
- 下一步：开始 M0.5。

## 2026-11-03 立项

- 完成：
  - 调研 YApi 现状、生态和竞品，报告见 `../_research/yapi.md`；确定定位为"兼容 YApi 的轻量继任者"。
  - 确定项目名 ApiYard（GitHub 上基本没有重名），许可证 Apache-2.0。
  - 编写 README（目标与里程碑 M0–M6）和 `docs/architecture.md`（5 条关键决策和 YApi 兼容要点）。
  - 搭好 Go 工程骨架：配置加载、HTTP 服务（`/healthz`）、优雅退出、单元测试、golangci-lint 配置。
- 问题 / 决定：
  - 内部数据模型改为以 YApi 结构为准，OpenAPI 只作为交换格式（D2），避免每次请求都来回转换。
  - 读源码确认：开放 API token 是用 Node `createCipher('aes192', passsalt)` 加密的 `uid|项目token`，迁移时需要用户提供原来的 `passsalt`。
  - Go 1.24 已经停止支持，最低版本改为 1.26，开发时使用 1.27.3。工具链由 `GOTOOLCHAIN` 自动下载，GOPATH 和各类缓存都放在 `D:\Dev_Env\Go_Env`，不占用 C 盘。
  - golangci-lint 必须用和项目相同的 Go 版本编译，否则分析标准库时会崩溃。
  - 骨架已验证：gofmt、go vet、5 个单元测试、构建、golangci-lint（0 个问题）全部通过。
  - `go.mod` 的模块路径暂定为 `apiyard`，创建 GitHub 仓库后改为完整路径。
- 下一步：
  - 开工前联系 ApiMind 作者（YApi issue #2806），了解对方的进展。
  - 开始 M1：设计 SQLite 表结构，实现核心模型的增删改查。
