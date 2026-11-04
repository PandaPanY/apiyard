# YApi 1.12.0 对照实例，仅用于 ApiYard 的兼容性测试。
# 警告：YApi 存在未修复的远程代码执行漏洞，切勿暴露到局域网或公网。
FROM node:14-bullseye-slim

ARG YAPI_VERSION=1.12.0
ARG NPM_REGISTRY=https://registry.npmmirror.com

# YApi 的目录约定：代码在 /app/vendors，配置文件、init.lock 和日志放在它的上一级 /app
WORKDIR /app
RUN npm config set registry "${NPM_REGISTRY}" \
 && npm pack "yapi-vendor@${YAPI_VERSION}" --silent \
 && mkdir vendors \
 && tar -xzf yapi-vendor-*.tgz -C vendors --strip-components=1 \
 && rm yapi-vendor-*.tgz \
 && cd vendors \
 && npm install --production --no-audit --no-fund \
 && npm cache clean --force

COPY config.json /app/config.json
COPY --chmod=755 entrypoint.sh /app/entrypoint.sh

# init.lock 标记数据库已初始化，必须和数据库一起持久化，所以把它链接到挂载目录里
RUN mkdir -p /app/state /app/log \
 && ln -s /app/state/init.lock /app/init.lock \
 && chown -R node:node /app

USER node
EXPOSE 3000
ENTRYPOINT ["/app/entrypoint.sh"]
