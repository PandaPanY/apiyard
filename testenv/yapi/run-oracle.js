// 在 YApi 容器里运行原版的 Swagger 转换（run.js），补上 Node 中没有的 File/Blob，输出转换结果
global.File = class File {};
global.Blob = class Blob {};
const run = require('/app/vendors/exts/yapi-plugin-import-swagger/run.js');
let input = '';
process.stdin.on('data', d => (input += d));
process.stdin.on('end', async () => {
  const res = await run(input);
  process.stdout.write(JSON.stringify(res, null, 2));
});
