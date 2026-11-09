# YApi 对照实例

运行原版 YApi 1.12.0，作为 ApiYard 兼容性测试的"标准答案"。

> **安全警告**：YApi 1.12.0 存在未修复的远程代码执行漏洞。服务只绑定服务器的 `127.0.0.1`，必须通过 SSH 隧道访问。不要改动端口绑定，也不要用它存放真实数据。

## 组成

| 服务 | 镜像 | 说明 |
|---|---|---|
| `yapi` | 自建 `apiyard/yapi:1.12.0` | `node:14` + npm 包 `yapi-vendor@1.12.0`，使用包里预构建好的前端 |
| `mongo` | 官方 `mongo:4.4` | 不对外开放端口 |

## 部署

在 ApiYard 仓库根目录执行，把部署文件同步到测试服务器并启动。下面的命令用 `testenv` 指代测试服务器，先在 `~/.ssh/config` 里给它配置同名的 Host 别名：

```bash
tar -C testenv -cf - yapi | ssh testenv "mkdir -p /data/apiyard-testenv && tar -C /data/apiyard-testenv -xf -"
ssh testenv "cd /data/apiyard-testenv/yapi && docker compose up -d --build"
```

首次启动会自动初始化数据库，并创建管理员账号 `admin@admin.com`，密码是 YApi 的默认值 `ymfe.org`。

## 访问

```bash
ssh -N -L 13000:127.0.0.1:13000 testenv
```

保持这个命令运行，然后在本机浏览器打开 `http://localhost:13000`。

MongoDB 同样只绑定服务器的 `127.0.0.1:13017`，迁移测试时在隧道里加上 `-L 13017:127.0.0.1:13017`。

## 常用操作

```bash
# 查看日志
ssh testenv "docker logs --tail 50 apiyard-yapi"

# 停止（保留数据）
ssh testenv "cd /data/apiyard-testenv/yapi && docker compose down"

# 彻底重置（删除所有数据）
ssh testenv "cd /data/apiyard-testenv/yapi && docker compose down -v"
```

## 生成 Swagger 导入的标准答案

原版 1.12 的服务端 Swagger 导入在 Node 里加载就出错（swagger-client 依赖浏览器的 `File`），没法通过接口录制。
`run-oracle.js` 给 Node 补上 `File` 后直接运行原版的 `run.js`，输出转换结果，用作 `internal/openapi` 的对照答案：

```bash
scp testenv/yapi/run-oracle.js testenv:/data/tmp/
ssh testenv "sudo docker cp /data/tmp/run-oracle.js apiyard-yapi:/tmp/run-oracle.js"
# 对 testdata/import 下的每个文档生成 *.yapi.json
ssh testenv "sudo docker exec -i -w /app/vendors apiyard-yapi node /tmp/run-oracle.js"   < testdata/import/swagger2.json > testdata/import/swagger2.yapi.json
```
