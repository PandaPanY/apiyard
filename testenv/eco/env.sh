# 实测环境：Node 借用 /data/tools/node，npm 缓存放在本目录，不写系统盘
export PATH=/data/tools/node/bin:$PATH
export npm_config_cache=/data/apiyard-testenv/eco/.npm-cache
export npm_config_registry=https://registry.npmmirror.com
