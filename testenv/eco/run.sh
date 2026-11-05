#!/usr/bin/env bash
# 生态实测：分别对原版 YApi 和 ApiYard 灌入同样的数据，运行同样的工具，比较结果。
# 在测试服务器上执行：bash run.sh（前提见 README.md）。结果在 out/ 下，有差异时退出码为 1。
set -u
cd "$(dirname "$0")"
. ./env.sh

YAPI=http://127.0.0.1:13000
YARD=http://127.0.0.1:13100
OUT=$PWD/out
rm -rf "$OUT"
mkdir -p "$OUT"

# 启动一个全新的 ApiYard
pkill -f '/data/apiyard-testenv/apiyard/apiyard' 2>/dev/null
rm -f /data/apiyard-testenv/apiyard/eco.db*
/data/apiyard-testenv/apiyard/apiyard -addr 127.0.0.1:13100 -db /data/apiyard-testenv/apiyard/eco.db \
  -admin-email admin@admin.com -admin-password ymfe.org >"$OUT/apiyard.log" 2>&1 &
YARD_PID=$!
trap 'kill $YARD_PID 2>/dev/null' EXIT
for _ in $(seq 1 50); do curl -sf "$YARD/healthz" >/dev/null && break; sleep 0.2; done

# 原版也要从干净的状态开始
/data/apiyard-testenv/yapi/reset.sh >/dev/null 2>&1

fail=0
step() { # step 名称 命令...
  local name=$1; shift
  if "$@" >"$OUT/$name.log" 2>&1; then echo "  ✓ $name"; else echo "  ✗ $name（见 out/$name.log）"; fail=1; fi
}

for target in yapi yard; do
  [ "$target" = yapi ] && base=$YAPI || base=$YARD
  echo "== $target"
  dir=$OUT/$target
  mkdir -p "$dir"

  node seed.js "$base" "$dir/seed.json" || { echo "灌数据失败"; exit 1; }

  # yapi-to-typescript：生成代码并做类型检查
  ytt=$dir/ytt
  mkdir -p "$ytt"
  node -e '
    const s = require(process.argv[1]);
    require("fs").writeFileSync(process.argv[2], `
      const { defineConfig } = require("yapi-to-typescript");
      module.exports = { default: defineConfig([{
        serverUrl: ${JSON.stringify(s.base)}, typesOnly: false, target: "typescript", reactHooks: { enabled: false },
        prodEnvName: "production", outputFilePath: "api/index.ts", requestFunctionFilePath: "api/request.ts", dataKey: "data",
        projects: [{ token: ${JSON.stringify(s.token)}, categories: [{ id: 0, getRequestFunctionName(i, changeCase) { return changeCase.camelCase(i.parsedPath.dir + "_" + i.parsedPath.name + "_" + i.method); } }] }],
      }]) };`);
  ' "$dir/seed.json" "$ytt/ytt.config.js"
  ln -sfn "$PWD/node_modules" "$ytt/node_modules"
  step "$target-ytt" bash -c "cd '$ytt' && NODE_PATH='$PWD/node_modules' '$PWD/node_modules/.bin/ytt'"
  # 只看生成的代码（api/）有没有类型错误。yapi-to-typescript 自带的旧版 TypeScript 解析不了新版 @types/node，
  # 那部分报错与生成的代码无关，所以不计入
  echo '{"compilerOptions":{"noEmit":true,"target":"es2018","module":"commonjs","esModuleInterop":true,"skipLibCheck":true,"strict":true},"files":["api/index.ts"]}' >"$ytt/tsconfig.json"
  step "$target-ytt-tsc" bash -c "cd '$ytt' && ! '$PWD/node_modules/.bin/tsc' -p tsconfig.json | grep '^api/'"

  # easy-yapi：先于 MCP 跑，MCP 会改接口
  step "$target-easy" node easy-yapi-replay.js "$dir/seed.json" "$dir/easy.txt"
  # Yapi-MCP
  step "$target-mcp" node mcp-test.js "$dir/seed.json" "$dir/mcp.txt"
done

echo "== 比较"
norm_ytt() { [ -f "$1" ] && node -e 'const {normalize}=require("./lib");process.stdout.write(normalize(require("fs").readFileSync(process.argv[1],"utf8")))' "$1"; }
compare() { # compare 名称 文件A 文件B；文件不存在或为空时算失败，避免两个空结果被判为一致
  if [ ! -s "$2" ] || [ ! -s "$3" ]; then echo "  ✗ $1 没有结果"; fail=1; return; fi
  if diff -u "$2" "$3" >"$OUT/$1.diff"; then echo "  ✓ $1 一致"; else echo "  ✗ $1 有差异（见 out/$1.diff，$(grep -c '^[-+][^-+]' "$OUT/$1.diff") 行）"; fail=1; fi
}
norm_ytt "$OUT/yapi/ytt/api/index.ts" >"$OUT/yapi/ytt.txt"
norm_ytt "$OUT/yard/ytt/api/index.ts" >"$OUT/yard/ytt.txt"
compare ytt "$OUT/yapi/ytt.txt" "$OUT/yard/ytt.txt"
compare easy "$OUT/yapi/easy.txt" "$OUT/yard/easy.txt"
compare mcp "$OUT/yapi/mcp.txt" "$OUT/yard/mcp.txt"
exit $fail
