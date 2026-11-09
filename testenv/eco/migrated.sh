#!/usr/bin/env bash
# 迁移实测：用 YApi 原来的项目 token，让 yapi-to-typescript 分别对原版 YApi 和迁移后的 ApiYard 生成代码，比较结果。
# 前提：已按 docs/migration.md 把对照实例迁移到 $DB（默认 /data/apiyard-testenv/apiyard/migrated.db）。
# 用法（测试服务器上）：bash migrated.sh；有差异时退出码为 1。
set -u
cd "$(dirname "$0")"
. ./env.sh

YAPI=http://127.0.0.1:13000
YARD=http://127.0.0.1:13101
DB=${DB:-/data/apiyard-testenv/apiyard/migrated.db}
OUT=$PWD/out-migrated
rm -rf "$OUT"
mkdir -p "$OUT"

/data/apiyard-testenv/apiyard/apiyard -addr 127.0.0.1:13101 -db "$DB" -token-salt abcde >"$OUT/apiyard.log" 2>&1 &
YARD_PID=$!
trap 'kill $YARD_PID 2>/dev/null' EXIT
for _ in $(seq 1 50); do curl -sf "$YARD/healthz" >/dev/null && break; sleep 0.2; done

# 从原版 YApi 取每个项目的 token（迁移前就存在的 token）
node -e '
  const { api, must } = require("./lib");
  (async () => {
    const s = { base: process.argv[1] };
    must(await api(s, "POST", "/api/user/login", { email: "admin@admin.com", password: "ymfe.org" }), "登录");
    const out = [];
    for (const g of must(await api(s, "GET", "/api/group/list"), "分组")) {
      for (const p of must(await api(s, "GET", `/api/project/list?group_id=${g._id}`), "项目").list) {
        out.push({ pid: p._id, token: must(await api(s, "GET", `/api/project/token?project_id=${p._id}`), "token") });
      }
    }
    console.log(JSON.stringify(out));
  })().catch(e => { console.error(e.message); process.exit(1); });
' "$YAPI" >"$OUT/tokens.json" || { echo "读取 token 失败"; exit 1; }

fail=0
for target in yapi yard; do
  [ "$target" = yapi ] && base=$YAPI || base=$YARD
  dir=$OUT/$target
  mkdir -p "$dir"
  node -e '
    const tokens = require(process.argv[1]);
    require("fs").writeFileSync(process.argv[2], `
      const { defineConfig } = require("yapi-to-typescript");
      module.exports = { default: defineConfig([{
        serverUrl: ${JSON.stringify(process.argv[3])}, typesOnly: true, target: "typescript", reactHooks: { enabled: false },
        prodEnvName: "production", outputFilePath: "api/index.ts", dataKey: "data",
        // 按接口 ID 命名：示例数据里有 /1 这样的路径，默认按路径命名会得到非法的类型名
        projects: ${JSON.stringify(tokens.map(t => t.token))}
          .map(token => ({ token, categories: [{ id: 0, getRequestFunctionName: i => "api" + i._id }] })),
      }]) };`);
  ' "$OUT/tokens.json" "$dir/ytt.config.js" "$base"
  ln -sfn "$PWD/node_modules" "$dir/node_modules"
  if (cd "$dir" && NODE_PATH="$PWD/node_modules" "$PWD/node_modules/.bin/ytt") >"$OUT/$target-ytt.log" 2>&1; then
    echo "  ✓ $target 生成代码"
  else
    echo "  ✗ $target 生成代码失败（见 out-migrated/$target-ytt.log）"; fail=1
  fi
done

# 两边的 ID 相同，生成的代码应当逐字一致（只有注释里的服务器地址不同）
norm() { [ -s "$1" ] && sed -E 's#127\.0\.0\.1:13[0-9]{3}#<server>#g' "$1"; }
norm "$OUT/yapi/api/index.ts" >"$OUT/yapi.ts"
norm "$OUT/yard/api/index.ts" >"$OUT/yard.ts"
if [ -s "$OUT/yapi.ts" ] && diff -u "$OUT/yapi.ts" "$OUT/yard.ts" >"$OUT/ytt.diff"; then
  echo "  ✓ 生成的代码一致（$(grep -c '^export interface' "$OUT/yapi.ts") 个类型）"
else
  echo "  ✗ 生成的代码有差异（见 out-migrated/ytt.diff）"; fail=1
fi
exit $fail
