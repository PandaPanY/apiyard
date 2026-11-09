# ApiYard 的 Docker 镜像：运行环境是 distroless（没有 shell 和包管理器），以非 root 用户运行。
#
#   docker build -t apiyard .
#   docker run -d -p 3000:3000 -v apiyard-data:/data apiyard
#
# 国内网络可以加 --build-arg GOPROXY=https://goproxy.cn,direct
# 和 --build-arg NPM_REGISTRY=https://registry.npmmirror.com。

# 构建阶段在构建机自己的平台上运行，交叉编译出目标平台的可执行文件，构建多平台镜像不需要 QEMU 模拟。
# BuildKit（docker buildx）会自动设置 BUILDPLATFORM；没有 buildx 的旧版构建器要加 --build-arg BUILDPLATFORM=linux/amd64
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM golang:1.27-bookworm AS build
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=
ARG GOPROXY=https://proxy.golang.org,direct
ARG NPM_REGISTRY=https://registry.npmjs.org
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# 已经用 tools/fetch-yapi-web 下载过前端就直接用，否则从 npm 下载并校验
RUN test -f web/yapi/index.html || go run ./tools/fetch-yapi-web -registry "$NPM_REGISTRY"
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X main.buildVersion=$VERSION" -o /out/apiyard ./cmd/apiyard \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/apiyard /usr/local/bin/apiyard
# 数据目录归镜像里的 nonroot 用户（65532）所有；挂载宿主机目录时，要让这个用户有写权限
COPY --from=build --chown=65532:65532 /out/data /data
ENV APIYARD_ADDR=:3000 \
    APIYARD_DB=/data/apiyard.db
VOLUME /data
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD ["/usr/local/bin/apiyard", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/apiyard"]
