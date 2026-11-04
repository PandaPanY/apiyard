# ApiYard

兼容 YApi 的轻量接口管理平台：一个可执行文件就能部署，现有的 YApi 周边工具不用改就能接入。

## 背景

[YApi](https://github.com/YMFE/yapi) 曾是国内最流行的自托管接口管理平台（27k+ Star），但 2022 年后已停止维护：

- 依赖 Node + MongoDB，版本陈旧，部署和升级都很麻烦；
- Mock 脚本沙箱存在**未修复的未授权远程代码执行漏洞**；
- 仍有大量团队在用，IDEA 插件、TypeScript 代码生成、MCP 等周边工具也仍在活跃更新。

ApiYard 用 Go 重新实现 YApi 的核心能力，让这些团队可以低成本、安全地迁移过来。立项调研见 [`../_research/yapi.md`](../_research/yapi.md)，架构设计见 [`docs/architecture.md`](docs/architecture.md)。

## 核心特性（规划）

- **单文件部署**：Go 编译，前端内嵌，默认使用 SQLite，不需要 MongoDB
- **兼容 YApi 开放 API**：15 个开放接口与 YApi 保持一致，easy-yapi、yapi-to-typescript、Yapi-MCP 等工具无需修改
- **一键迁移**：直接从 YApi 的 MongoDB 导入用户、分组、项目和接口，原有 token 继续可用
- **安全的 Mock**：默认只按 JSON Schema 生成数据；脚本 Mock 运行在没有系统访问能力的引擎里
- **内网友好**：去掉原版前端的 Google Analytics 统计和外部版本检查，不向外部站点发送任何数据
- **标准格式**：支持导入导出 OpenAPI 3 / Swagger 2
- **AI 接入**：内置 MCP Server

## 开发目标

- [x] M0 立项：调研、架构设计、工程骨架
- [x] M0.5 前置验证：搭建 YApi 对照环境，验证能否沿用原有前端，搭好兼容测试框架
- [ ] M1 账号与分组：SQLite 存储、登录会话、角色权限、用户 / 分组 / 关注接口（27 个）
- [ ] M2 项目与接口：项目 / 接口 / 动态接口（38 个），界面核心流程可用，公开仓库
- [ ] M3 开放 API 与导入导出：token 兼容，OpenAPI / Swagger 导入导出，周边工具实测
- [ ] M4 数据迁移：一条命令从 YApi 的 MongoDB 迁移，原密码、token、链接都继续有效
- [ ] M5 Mock 服务：基于 JSON Schema 和 Mock.js 语法生成数据，不执行脚本
- [ ] M6 打包发布 v0.1：单文件、Docker 镜像、CI、文档
- [ ] M7 MCP Server（v0.2）

**v0.1 发布标准**：完成 M0.5–M6，一个真实的 YApi 实例可以完整迁移过来并正常使用。每个里程碑的任务、验收标准和工时预估见 [`docs/roadmap.md`](docs/roadmap.md)。

**暂不做**：自动化测试集合、Wiki、统计、Swagger 自动同步、高级 Mock 脚本。

## 技术栈

- 语言：Go（标准库 `net/http`）
- 存储：SQLite（`modernc.org/sqlite`，纯 Go 实现，不需要 CGO），后续支持 PostgreSQL / MySQL
- 前端：第一阶段沿用 YApi 原有前端（React），之后逐步替换

## 快速开始

需要 Go 1.26 及以上版本。

```bash
# 获取 YApi 的预构建前端（从 npm 下载 yapi-vendor 并校验，解压到 web/yapi）
go run ./tools/fetch-yapi-web

# 运行
go run ./cmd/apiyard

# 测试
go test ./...

# 代码检查（需要先安装 golangci-lint）
golangci-lint run
```

启动后访问 `http://localhost:3000/healthz` 检查服务状态。

| 参数 | 环境变量 | 默认值 | 说明 |
|---|---|---|---|
| `-addr` | `APIYARD_ADDR` | `:3000` | 监听地址 |
| `-db` | `APIYARD_DB` | `apiyard.db` | SQLite 数据库文件，不存在时自动创建 |
| `-web-dir` | `APIYARD_WEB_DIR` | `web/yapi` | 前端静态文件目录 |
| `-admin-email` | `APIYARD_ADMIN_EMAIL` | `admin@admin.com` | 首次启动（数据库里还没有用户）时创建的管理员邮箱 |
| `-admin-password` | `APIYARD_ADMIN_PASSWORD` | 空 | 首次启动时的管理员密码；为空则随机生成，并在日志里打印一次 |
| `-close-register` | `APIYARD_CLOSE_REGISTER` | `false` | 关闭用户注册 |
| `-yapi-upstream` | `APIYARD_YAPI_UPSTREAM` | 空 | 开发用：把**全部** `/api`、`/mock` 请求转发给原版 YApi，用于对比行为（对照实例见 [`testenv/yapi`](testenv/yapi/README.md)） |

本地开发时可以这样运行：`go run ./cmd/apiyard -db apiyard-dev.db -admin-password ymfe.org`。

接口兼容性测试（与原版 YApi 逐字段比对）见 [`docs/testing.md`](docs/testing.md)。

## 许可证

[Apache-2.0](LICENSE)。本项目不隶属于 YApi 及其原作者；沿用的 YApi 代码见 [NOTICE](NOTICE)。
