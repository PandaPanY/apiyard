# 部署与配置

升级前请阅读 [安全修复与升级说明](security-hardening.md)，其中列出了 token 权限变化、数据库迁移和恢复步骤。

ApiYard 只有一个可执行文件，前端已经打在里面，数据存在一个 SQLite 文件里，不需要 Node、MongoDB 或其他服务。

## 安装

### 可执行文件

从 GitHub Releases 下载对应平台的压缩包：Linux、macOS、Windows，各有 amd64 和 arm64 两种架构。解压后直接运行：

```bash
tar -xzf apiyard_0.1.0_linux_amd64.tar.gz
cd apiyard_0.1.0_linux_amd64
./apiyard -admin-password '初始密码'
```

浏览器打开 `http://服务器地址:3000`，用 `admin@admin.com` 和上面的密码登录。数据库默认是当前目录下的 `apiyard.db`，用 `-db` 指定其他位置。

压缩包可以用同一页的 `checksums.txt` 校验：`sha256sum -c checksums.txt --ignore-missing`。

Linux 上长期运行可以交给 systemd，例如 `/etc/systemd/system/apiyard.service`：

```ini
[Unit]
Description=ApiYard
After=network.target

[Service]
User=apiyard
WorkingDirectory=/var/lib/apiyard
ExecStart=/usr/local/bin/apiyard -db /var/lib/apiyard/apiyard.db -close-register
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

### Docker

镜像支持 amd64 和 arm64：

```bash
docker run -d --name apiyard -p 3000:3000 -v apiyard-data:/data \
  -e APIYARD_ADMIN_PASSWORD='初始密码' ghcr.io/pandapany/apiyard:0.1
```

- 镜像基于 distroless（没有 shell），以 UID 65532 运行，数据库在 `/data/apiyard.db`。压缩后约 10MB。
- 挂载宿主机目录代替数据卷时，先执行 `chown 65532:65532 目录`，否则启动时会报“打开数据库……有写权限”。
- 镜像自带 `HEALTHCHECK`，`docker ps` 能看到 `healthy` 状态。

docker compose：

```yaml
services:
  apiyard:
    image: ghcr.io/pandapany/apiyard:0.1
    ports:
      - "3000:3000"
    volumes:
      - apiyard-data:/data
    environment:
      APIYARD_ADMIN_PASSWORD: 初始密码
      APIYARD_CLOSE_REGISTER: "true"
    restart: unless-stopped
volumes:
  apiyard-data:
```

自己构建镜像：`docker build -t apiyard .`。国内网络可以加 `--build-arg GOPROXY=https://goproxy.cn,direct --build-arg NPM_REGISTRY=https://registry.npmmirror.com`。

### 从源码编译

需要 Go 1.26 及以上版本：

```bash
go run ./tools/fetch-yapi-web          # 下载 YApi 的预构建前端到 web/yapi
go build -o apiyard ./cmd/apiyard
```

没有下载前端也能编译，但可执行文件里没有界面，启动时日志会提示。各平台的发布包用 `go run ./tools/dist -version v0.1.0` 生成，见 [release.md](release.md)。

## 首次启动

数据库里还没有用户时，ApiYard 会创建管理员账号（默认 `admin@admin.com`）。没有用 `-admin-password` 指定密码的，会随机生成一个，**只在启动日志里出现这一次**（Docker 用 `docker logs apiyard` 查看）。登录后请尽快修改。

从 YApi 迁移过来的数据库里已经有用户，不会再创建管理员，用原来的账号登录。迁移步骤见 [migration.md](migration.md)。

## 配置

每个参数都可以用命令行参数或环境变量设置，命令行参数优先。

| 参数 | 环境变量 | 默认值 | 说明 |
|---|---|---|---|
| `-addr` | `APIYARD_ADDR` | `:3000` | 监听地址 |
| `-db` | `APIYARD_DB` | `apiyard.db` | SQLite 数据库文件，不存在时自动创建 |
| `-admin-email` | `APIYARD_ADMIN_EMAIL` | `admin@admin.com` | 首次启动（数据库里还没有用户）时创建的管理员邮箱 |
| `-admin-password` | `APIYARD_ADMIN_PASSWORD` | 空 | 首次启动时的管理员密码；为空则随机生成，并在日志里打印一次 |
| `-close-register` | `APIYARD_CLOSE_REGISTER` | `false` | 关闭用户注册 |
| `-mcp` | `APIYARD_MCP` | `off` | MCP 模式：`on`、`readonly`、`off`；v0.1.1 默认关闭，工具尚在开发 |
| `-token-salt` | `APIYARD_TOKEN_SALT` | `abcde` | 开放 API token 的加密口令；从 YApi 迁移时填原配置文件里的 `passsalt`，已有的 token 才能继续使用 |
| `-web-dir` | `APIYARD_WEB_DIR` | 空 | 前端目录；为空时使用打进可执行文件的前端，一般不需要设置 |
| `-yapi-upstream` | `APIYARD_YAPI_UPSTREAM` | 空 | 开发用：把**全部** `/api`、`/mock` 请求转发给原版 YApi，用于对比行为 |

子命令：

| 命令 | 说明 |
|---|---|
| `apiyard migrate` | 从 YApi 迁移数据，见 [migration.md](migration.md) |
| `apiyard version` | 显示版本 |
| `apiyard healthcheck` | 请求本机服务的 `/healthz`，正常时退出码为 0；`-addr` 与启动服务时相同 |

### 与 YApi 的 config.json 对照

| YApi | ApiYard |
|---|---|
| `port` | `-addr`（如 `:3000`） |
| `adminAccount` | `-admin-email`；原版的初始密码固定是 `ymfe.org`，ApiYard 随机生成或用 `-admin-password` 指定 |
| `closeRegister` | `-close-register` |
| `passsalt` | `-token-salt` |
| `db` | 不需要，数据在 `-db` 指定的 SQLite 文件里 |
| `mail` | 暂不支持，不发送邮件通知 |
| `ldapLogin` | 暂不支持 |
| `plugins` | 不支持插件，常用的插件功能（导出 Swagger 等）已内置 |

## 反向代理

接口编辑页的冲突检测用 WebSocket（`/api/interface/solve_conflict`），反向代理要转发 `Upgrade` 请求头。经 HTTPS 访问时，代理要传 `X-Forwarded-Proto`，ApiYard 才会给登录 Cookie 加上 `Secure`。nginx 示例：

```nginx
server {
    listen 443 ssl;
    server_name yapi.example.com;
    client_max_body_size 10m;   # ApiYard 自己的上限：接口请求体 2MB，Mock 的 multipart 请求 10MB

    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

## 备份与升级

数据库使用 WAL 模式，运行时由 `apiyard.db`、`apiyard.db-wal`、`apiyard.db-shm` 三个文件组成。备份可以任选一种：

- 停止 ApiYard，复制这三个文件；
- 不停服务，用 SQLite 命令行工具做在线备份：`sqlite3 apiyard.db ".backup apiyard-backup.db"`。

升级时先备份，再替换可执行文件（或拉取新镜像）后重新启动。数据库结构的变更在启动时自动执行。
