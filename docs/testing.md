# 兼容性测试

ApiYard 的接口必须与原版 YApi 行为一致。做法是：用同一组"场景"分别调用原版 YApi 和 ApiYard，把原版的响应录制成 golden 文件，再逐字段比对 ApiYard 的响应。

## 目录

```
testdata/compat/
├── scenarios/   场景：按文件名顺序执行，每个场景使用独立的 Cookie
└── golden/      原版 YApi 的录制结果（由工具生成，不要手改）
internal/compat/ 框架：执行场景、归一化、比对
tools/compat/    命令行工具
```

## 自动运行

`go test ./...` 会自动执行兼容比对（`internal/server/compat_test.go`）：在进程内启动一个使用全新数据库的 ApiYard，按顺序执行已实现的场景，并与 golden 文件逐字段比对，**不需要连接对照实例**。每完成一部分功能，就把对应场景加进 `implementedScenarios`。

录制时所有场景**按文件名顺序在同一个实例上**执行，后面的场景可以依赖前面留下的数据（例如 02 用到 01 注册的 dev1）。比对也照此进行，所以已实现的场景必须是从第一个开始的连续一段，测试会检查这一点。

golden 文件里的 `url` 和 `request` 记录的是场景中的模板（保留 `{{变量}}`），不是替换后的值，因此重新录制时 golden 文件不会因为 ID 变化而改动。

## 常用命令

前提：已建立到对照实例的 SSH 隧道（`ssh -N -L 13000:127.0.0.1:13000 testenv`，`testenv` 是测试服务器的 SSH 别名），对照实例的说明见 [`testenv/yapi`](../testenv/yapi/README.md)。

```bash
# 新增或修改场景后，重新录制 golden（必须先重置对照实例）
ssh testenv /data/apiyard-testenv/yapi/reset.sh
go run ./tools/compat record -target http://localhost:13000

# 比对 ApiYard（ApiYard 也要从全新状态启动）
go run ./tools/compat check -target http://localhost:3000

# 只执行部分场景
go run ./tools/compat check -target http://localhost:3000 -run user
```

`check` 发现差异时退出码为 1，可以直接用于 CI。

## 场景格式

```json
{
  "description": "场景说明",
  "steps": [
    {
      "name": "新建分组",
      "method": "POST",
      "path": "/api/group/add",
      "body": { "group_name": "后端组", "owner_uids": [] },
      "capture": { "gid": "data._id" }
    },
    { "name": "查询分组", "method": "GET", "path": "/api/group/get", "query": { "id": "{{gid}}" } },
    { "name": "批量新建", "method": "POST", "path": "/api/interface/add", "repeat": 12,
      "body": { "title": "接口 {{i}}", "path": "/demo/{{i}}" } },
    { "name": "未登录", "method": "GET", "path": "/api/user/status", "errcode": 40011 }
  ]
}
```

| 字段 | 说明 |
|---|---|
| `capture` | 按点分路径从响应中取值，保存为变量，例如 `data.list.0._id` |
| `{{变量}}` | 在 `path`、`query`、`body` 中引用变量；整个字符串只有一个变量时保留原类型（数字仍是数字） |
| `repeat` | 重复执行该步骤，变量 `{{i}}` 依次为 1..N |
| `errcode` | 期望的业务错误码，默认 0；不符时场景立即失败 |

## 归一化规则

比对前，下面这些每次运行都会变化的值会被替换成占位符：

| 内容 | 占位符 | 原因 |
|---|---|---|
| `add_time`、`up_time`、`addTime`、`upTime` | `<time>` | 时间戳 |
| `token` | `<token>` | 随机生成 |
| 24 位十六进制字符串 | `<objectid>` | MongoDB 自动生成的 ObjectId |
| `_id`、`id`、`uid`、`catid`、`*_id`、`*_uid`、`*_ids`、`*_uids` 中的数字 | `<id>` | YApi 每次启动都会随机选自增步长，ID 无法复现 |

ID 之间的引用关系由场景本身保证：后续步骤用捕获的 ID 继续调用，引用错了会得到错误的数据或错误码。

## 约定

- **录制前必须重置对照实例**，场景之间有状态依赖（例如列表接口会返回前面场景创建的数据）。
- 新场景先在原版上 `record`，再重置一次并对原版执行 `check`，确认结果可以重复。如果出现差异，说明还有未处理的随机值，需要补充归一化规则。
- `90-seed-demo` 最后执行，执行后对照实例保留一份示例数据（2 个分组、3 个项目、38 个接口），供界面验证和 M4 迁移测试使用。
