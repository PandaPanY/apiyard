// 在目标服务器上灌入实测数据，把项目 ID、分类 ID、接口 ID 和 token 写到 seed.json。
// 用法：node seed.js <服务器地址> <输出文件>
const fs = require('fs');
const { api, must } = require('./lib');

const userSchema = {
  type: 'object', title: '用户', required: ['id', 'name'],
  properties: {
    id: { type: 'integer', description: '用户 ID' },
    name: { type: 'string', description: '用户名' },
    tags: { type: 'array', items: { type: 'string' } },
    roles: { type: 'array', items: { type: 'object', properties: { code: { type: 'string', enum: ['admin', 'member'] } } } },
    profile: { type: 'object', properties: { age: { type: 'integer' }, active: { type: 'boolean' } } },
  },
};
const createSchema = { type: 'object', required: ['name'], properties: { name: { type: 'string' }, age: { type: 'integer' } } };

async function main() {
  const [base, out] = process.argv.slice(2);
  const s = { base };
  must(await api(s, 'POST', '/api/user/login', { email: 'admin@admin.com', password: 'ymfe.org' }), '登录');
  const group = must(await api(s, 'POST', '/api/group/add', { group_name: '生态实测', owner_uids: [] }), '新建分组');
  const project = must(await api(s, 'POST', '/api/project/add', { name: '生态项目', basepath: '/eco', group_id: group._id, desc: '周边工具实测' }), '新建项目');
  const pid = project._id;
  const cat0 = must(await api(s, 'GET', `/api/project/get?id=${pid}`), '读取项目').cat[0]._id;
  const userCat = must(await api(s, 'POST', '/api/interface/add_cat', { name: '用户', project_id: pid, desc: '用户相关' }), '新建分类')._id;

  const add = async (catid, body) => must(await api(s, 'POST', '/api/interface/add', { project_id: pid, catid, ...body }), `新建接口 ${body.path}`)._id;
  const getUser = await add(userCat, {
    title: '获取用户', path: '/users/{id}', method: 'GET', status: 'done', desc: '<p>按 ID 获取用户</p>', markdown: '按 ID 获取用户', tag: ['v1'],
    req_params: [{ name: 'id', desc: '用户 ID', example: '1' }],
    req_query: [{ name: 'fields', required: '0', example: 'a,b', desc: '返回字段' }],
    req_headers: [{ name: 'X-Trace', value: 'abc', required: '0', desc: '跟踪号' }],
    res_body_type: 'json', res_body_is_json_schema: true, res_body: JSON.stringify(userSchema),
  });
  const createUser = await add(userCat, {
    title: '新建用户', path: '/users', method: 'POST',
    req_body_type: 'json', req_body_is_json_schema: true, req_body_other: JSON.stringify(createSchema),
    res_body_type: 'json', res_body_is_json_schema: true, res_body: JSON.stringify(userSchema),
  });
  await add(userCat, {
    title: '上传头像', path: '/users/{id}/avatar', method: 'POST', req_body_type: 'form', req_headers: [],
    req_body_form: [{ name: 'file', type: 'file', required: '1', desc: '头像' }, { name: 'note', type: 'text', required: '0' }],
    res_body_type: 'json', res_body: '{"code":0}',
  });
  await add(cat0, { title: '健康检查', path: '/ping', method: 'GET', res_body_type: 'raw', res_body: 'pong' });

  const token = must(await api(s, 'GET', `/api/project/token?project_id=${pid}`), '获取 token');
  fs.writeFileSync(out, JSON.stringify({ base, pid, cat0, userCat, getUser, createUser, token }, null, 2));
}

main().catch(e => { console.error(e); process.exit(1); });
