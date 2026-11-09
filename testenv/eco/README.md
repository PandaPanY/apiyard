# 生态实测

验证"现有 YApi 周边工具只改服务器地址就能接入 ApiYard"：分别对原版 YApi 和 ApiYard 灌入同样的数据，运行同样的工具，归一化（ID、时间、token、键顺序）后比较结果，要求完全一致。

| 工具 | 版本 | 方式 | 用到的接口 |
|---|---|---|---|
| [yapi-to-typescript](https://github.com/fjc0k/yapi-to-typescript) | 3.39.0 | 真实运行 CLI，生成 TypeScript 并做类型检查 | `project/get`、`getCatMenu`、`plugin/export` |
| [Yapi-MCP](https://github.com/lzsheng/Yapi-MCP)（npm 包 `yapi-auto-mcp`） | 0.1.3 | 真实运行，通过 MCP 协议调用全部 5 个工具 | `project/get`、`getCatMenu`、`interface/{get,add,up,list,list_cat}` |
| [easy-yapi](https://github.com/tangcent/easy-yapi) | master（2026-11-05） | **回放**：它是 IDEA 插件，不能在命令行运行，`easy-yapi-replay.js` 按 `DefaultYapiApiClient.kt`、`YapiFormatter.kt` 复现请求和响应解析 | `project/get`、`list_menu`、`getCatMenu`、`add_cat`、`list_cat`、`save`、`get` |

## 运行

在测试服务器上执行（需要 Node，脚本借用 `/data/tools/node`；ApiYard 监听 13100，对照实例监听 13000）：

```bash
# 本机：构建 ApiYard 并上传，同步脚本
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o apiyard-linux ./cmd/apiyard
scp apiyard-linux testenv:/data/apiyard-testenv/apiyard/apiyard
tar cf - -C testenv/eco . | ssh testenv 'mkdir -p /data/apiyard-testenv/eco && tar xf - -C /data/apiyard-testenv/eco'

# 服务器：首次安装工具，然后运行
cd /data/apiyard-testenv/eco && . ./env.sh
npm init -y && npm i yapi-to-typescript@3.39.0 yapi-auto-mcp@0.1.3
bash run.sh
```

`run.sh` 会重置对照实例，结果在 `out/`，有差异时退出码为 1。

### 迁移后的实测

`migrated.sh` 不重置对照实例：它用 YApi 里原有的项目 token，让 yapi-to-typescript 分别对原版和迁移后的 ApiYard 生成代码，要求逐字一致。先把对照实例迁移到 `/data/apiyard-testenv/apiyard/migrated.db`（见 [docs/testing.md](../../docs/testing.md) 的“迁移核对”），再运行 `bash migrated.sh`。

## 注意

- 三个工具依次使用同一个项目，顺序固定（YTT → easy-yapi → MCP），两边相同。
- `yapi_get_categories` 读的是工具启动后在后台加载的缓存，测试里要等它加载完。
- easy-yapi 只做了回放，没有运行真实插件，插件本身的逻辑（如何生成文档）不在验证范围内。
