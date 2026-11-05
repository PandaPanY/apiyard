// 按 easy-yapi 的实际请求序列调用接口。easy-yapi 是 IDEA 插件，不能在命令行运行，
// 这里照 src/main/kotlin/com/itangcent/easyapi/channel/yapi/ 下的 DefaultYapiApiClient.kt 和 YapiFormatter.kt 复现：
// 请求的路径、参数类型和请求体结构都与插件相同，并按插件 parseResponse 的规则判断成败、读取响应字段。
// 用法：node easy-yapi-replay.js <seed.json> <输出文件>
const fs = require('fs');
const { normalize, canonical, readSeed } = require('./lib');

const seed = readSeed(process.argv[2]);
const out = [];
const enc = encodeURIComponent;

async function request(method, path, body) {
  const res = await fetch(seed.base + path, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  let json = null;
  try { json = JSON.parse(text); } catch { /* 下面按失败处理 */ }
  // 插件的判断：HTTP 200 且 errcode 为 0/200，或 errmsg 为“成功”“成功！”
  const ok = res.status === 200 && json !== null && (['成功', '成功！'].includes(json.errmsg) || [0, 200].includes(json.errcode));
  out.push([`${method} ${path.replace(/token=[^&]*/, 'token=<token>')}`, { status: res.status, ok, body: json ?? text }]);
  if (!ok) throw new Error(`${method} ${path} 失败: ${text.slice(0, 200)}`);
  return json;
}

// YapiFormatter.buildApiDocBody：字段齐全，catid 是字符串，required 来自文档模型。
function apiDoc(catId, existingId, doc) {
  const body = {
    token: seed.token, catid: catId, title: doc.title, path: doc.path, method: doc.method, desc: doc.desc, markdown: doc.markdown,
    status: doc.status ?? 'done',
    req_body_is_json_schema: doc.reqBodyIsJsonSchema ?? false,
    res_body_is_json_schema: doc.resBodyIsJsonSchema ?? false,
    req_headers: doc.reqHeaders ?? [], req_query: doc.reqQuery ?? [], req_params: doc.reqParams ?? [],
    req_body_other: doc.reqBodyOther ?? '', res_body: doc.resBody ?? '', req_body_form: doc.reqBodyForm ?? [],
  };
  if (doc.reqBodyType) body.req_body_type = doc.reqBodyType;
  if (doc.resBodyType) body.res_body_type = doc.resBodyType;
  if (doc.open !== undefined) body.api_opened = doc.open;
  if (doc.tag) body.tag = doc.tag;
  if (existingId) body.id = existingId;
  return body;
}

async function main() {
  const q = `token=${enc(seed.token)}`;
  // getProjectId：先 project/get（不带 id），data._id 为空时再试 list_menu 的第一项
  const project = await request('GET', `/api/project/get?${q}`);
  const projectId = String(project.data?._id ?? '');
  if (!projectId || projectId !== String(seed.pid)) throw new Error(`解析项目 ID 失败: ${projectId}`);
  await request('GET', `/api/interface/list_menu?${q}`);

  // findOrCreateCart：列分类，找不到就新建；add_cat 的 project_id 是字符串
  const carts = (await request('GET', `/api/interface/getCatMenu?project_id=${enc(projectId)}&${q}`)).data;
  if (!Array.isArray(carts) || carts.some(c => c._id === undefined || c.name === undefined)) throw new Error('分类格式不对');
  const created = await request('POST', '/api/interface/add_cat', { desc: '由 easy-yapi 创建', project_id: projectId, name: 'easy-yapi 分类', token: seed.token });
  const catId = String(created.data._id);

  const doc = {
    title: '查询订单', path: '/orders/{id}', method: 'GET', desc: '<p>查询订单</p>', markdown: '查询订单',
    reqParams: [{ name: 'id', example: '', desc: '订单 ID' }],
    reqQuery: [{ name: 'page', value: '', example: '1', desc: '页码', required: 1 }],
    reqHeaders: [{ name: 'X-Trace', value: 'abc', desc: '', example: 'abc', required: 0 }],
    resBodyType: 'json', resBodyIsJsonSchema: true,
    resBody: '{"type":"object","properties":{"id":{"type":"integer"}}}',
    reqBodyType: 'raw', open: false,
  };

  // saveApi：先在分类里查是否已存在，再 save；第一次新建，第二次更新
  const list = async () => (await request('GET', `/api/interface/list_cat?${q}&catid=${enc(catId)}&limit=1000`)).data.list;
  if ((await list()).length !== 0) throw new Error('新分类应为空');
  await request('POST', '/api/interface/save', apiDoc(catId, null, doc));
  const saved = (await list()).find(a => a.path === doc.path && a.method.toLowerCase() === doc.method.toLowerCase());
  if (!saved) throw new Error('save 后在分类里找不到接口');
  await request('POST', '/api/interface/save', apiDoc(catId, String(saved._id), { ...doc, title: '查询订单V2', desc: '<p>改过的描述</p>' }));

  // 表单请求体和 JSON 请求体各上传一个
  await request('POST', '/api/interface/save', apiDoc(catId, null, {
    title: '上传附件', path: '/orders/{id}/files', method: 'POST', reqBodyType: 'form',
    reqParams: [{ name: 'id', example: '', desc: '' }],
    reqBodyForm: [{ name: 'file', example: '', type: 'file', required: 1, desc: '附件' }],
  }));
  await request('POST', '/api/interface/save', apiDoc(catId, null, {
    title: '创建订单', path: '/orders', method: 'POST', reqBodyType: 'json', reqBodyIsJsonSchema: true,
    reqBodyOther: '{"type":"object","properties":{"amount":{"type":"number"}}}',
  }));

  // listProjectApis：list_menu 的每一项都要有 _id、path、method
  const menu = (await request('GET', `/api/interface/list_menu?project_id=${enc(projectId)}&${q}`)).data;
  for (const cart of menu) {
    for (const a of cart.list) {
      if (a._id === undefined || !a.path || !a.method) throw new Error(`菜单里的接口缺字段: ${JSON.stringify(a)}`);
    }
  }
  // 接口详情（用浏览器不会用到的 token 方式读回，核对写入内容）
  const detail = await request('GET', `/api/interface/get?id=${saved._id}&${q}`);
  if (detail.data.title !== '查询订单V2') throw new Error(`更新没有生效: ${detail.data.title}`);
}

main()
  .then(() => out.push(['结果', '全部成功']))
  .catch(e => { out.push(['结果', e.message]); process.exitCode = 1; })
  .finally(() => fs.writeFileSync(process.argv[3], normalize(JSON.stringify(canonical(out), null, 2)) + '\n'));
