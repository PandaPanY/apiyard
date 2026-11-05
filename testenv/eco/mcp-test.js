// 通过 MCP 协议（stdio）调用 Yapi-MCP 的全部工具，把结果写到输出文件。
// 用法：node mcp-test.js <seed.json> <输出文件>
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');
const { normalize, canonical, readSeed } = require('./lib');

const seed = readSeed(process.argv[2]);
const child = spawn(path.join(__dirname, 'node_modules/.bin/yapi-mcp'), ['--stdio'], {
  env: { ...process.env, YAPI_BASE_URL: seed.base, YAPI_TOKEN: `${seed.pid}:${seed.token}`, YAPI_LOG_LEVEL: 'none', YAPI_CACHE_TTL: '0', HOME: path.join(__dirname, 'home') }, // 工具的缓存留在实测目录里
  stdio: ['pipe', 'pipe', 'inherit'],
});

let buffer = '';
let nextId = 1;
const pending = new Map();
child.stdout.on('data', chunk => {
  buffer += chunk;
  let end;
  while ((end = buffer.indexOf('\n')) >= 0) {
    const line = buffer.slice(0, end).trim();
    buffer = buffer.slice(end + 1);
    if (!line) continue;
    let msg;
    try { msg = JSON.parse(line); } catch { continue; } // 工具自己打印的日志
    if (pending.has(msg.id)) {
      pending.get(msg.id)(msg);
      pending.delete(msg.id);
    }
  }
});

function rpc(method, params) {
  const id = nextId++;
  child.stdin.write(JSON.stringify({ jsonrpc: '2.0', id, method, params }) + '\n');
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(`${method} 超时`)), 30000);
    pending.set(id, msg => { clearTimeout(timer); resolve(msg); });
  });
}

// canonicalText 把工具输出里的 JSON（整段或结尾的一段）解析出来，排除键顺序的差异。
function canonicalText(text) {
  const m = text.match(/^([^[{]*)([[{][\s\S]*)$/);
  if (!m) return text;
  try {
    return m[1] + JSON.stringify(canonical(JSON.parse(m[2])), null, 1);
  } catch {
    return text;
  }
}

async function main() {
  const out = [];
  const projectId = String(seed.pid);
  const init = await rpc('initialize', { protocolVersion: '2024-11-05', capabilities: {}, clientInfo: { name: 'apiyard-eco', version: '1' } });
  child.stdin.write(JSON.stringify({ jsonrpc: '2.0', method: 'notifications/initialized' }) + '\n');
  out.push(['initialize', init.result?.serverInfo]);
  const tools = await rpc('tools/list', {});
  out.push(['tools', tools.result.tools.map(t => t.name)]);

  const call = async (name, args) => {
    const r = await rpc('tools/call', { name, arguments: args });
    const text = r.result?.content?.map(c => c.text).join('\n') ?? JSON.stringify(r.error);
    out.push([`${name} ${JSON.stringify(args)}`, { isError: !!r.result?.isError, text: canonicalText(text) }]);
    return text;
  };

  // 工具启动后在后台加载项目和分类缓存，yapi_get_categories 读的是这份缓存，要等它加载完
  await new Promise(r => setTimeout(r, 3000));
  await call('yapi_list_projects', {});
  await call('yapi_get_categories', { projectId });
  await call('yapi_get_api_desc', { projectId, apiId: String(seed.getUser) });
  await call('yapi_search_apis', { nameKeyword: '用户' });
  await call('yapi_search_apis', { pathKeyword: 'users' });
  await call('yapi_search_apis', { projectKeyword: '生态' });
  const added = await call('yapi_save_api', {
    projectId, catid: String(seed.userCat), title: 'MCP 新建', path: '/mcp/{id}', method: 'GET', status: 'undone',
    req_params: '[{"name":"id","desc":"编号"}]', req_query: '[{"name":"page","desc":"页码","required":"1"}]',
    res_body_type: 'json', res_body_is_json_schema: true, res_body: '{"type":"object","properties":{"ok":{"type":"boolean"}}}',
    desc: 'MCP 写入的接口',
  });
  await call('yapi_save_api', {
    projectId, catid: String(seed.userCat), id: String(seed.createUser), title: 'MCP 修改', path: '/users', method: 'POST', status: 'done',
  });
  await call('yapi_get_api_desc', { projectId, apiId: String(seed.createUser) });
  await call('yapi_save_api', { projectId, catid: String(seed.userCat), title: '重复路径', path: '/users', method: 'POST' });
  fs.writeFileSync(process.argv[3], normalize(JSON.stringify(out, null, 2)) + '\n');
  if (!added) throw new Error('没有结果');
}

main()
  .catch(e => { console.error(e); process.exitCode = 1; })
  .finally(() => child.kill());
