#!/bin/sh
# 重置对照实例：删除全部数据，回到只有管理员账号的初始状态。录制 golden 文件前必须先执行。
# 用法（本机）：ssh testenv /data/apiyard-testenv/yapi/reset.sh
set -e
cd "$(dirname "$0")"

docker compose down -v
docker compose up -d

# 首次启动要初始化数据库，等服务真正开始响应
for _ in $(seq 1 60); do
  if wget -qO- http://127.0.0.1:13000/api/user/status >/dev/null 2>&1; then
    echo "对照实例已重置并就绪"
    exit 0
  fi
  sleep 2
done
echo "等待对照实例启动超时" >&2
exit 1
