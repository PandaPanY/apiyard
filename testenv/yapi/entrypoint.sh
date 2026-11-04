#!/bin/sh
# 首次启动时初始化数据库并创建管理员账号，之后直接启动服务。
set -e
cd /app/vendors

if [ ! -f /app/state/init.lock ]; then
  # install.js 失败时只抛出未处理的 Promise 异常，进程不会退出，所以加超时兜底；
  # 超时或失败后容器退出，由 restart 策略重试。
  timeout 120 node server/install.js
fi

exec node server/app.js
