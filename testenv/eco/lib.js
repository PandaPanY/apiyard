// 生态实测的公共函数：调用接口、归一化结果。
const fs = require('fs');

// api 发送一次请求。cookie 保存在 session 上，模拟浏览器的登录状态。
async function api(session, method, path, body) {
  const res = await fetch(session.base + path, {
    method,
    headers: { 'Content-Type': 'application/json', Cookie: session.cookie || '' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const setCookies = res.headers.getSetCookie();
  if (setCookies.length) {
    session.cookie = setCookies.map(c => c.split(';')[0]).join('; ');
  }
  const text = await res.text();
  try {
    return JSON.parse(text);
  } catch {
    return { status: res.status, text };
  }
}

// must 要求接口成功，否则中止测试。
function must(r, what) {
  if (r.errcode !== 0) {
    throw new Error(`${what} 失败: ${JSON.stringify(r)}`);
  }
  return r.data;
}

// canonical 递归地按键名排序，排除两个服务器在键顺序上的差异（键顺序对工具没有影响）。
function canonical(v) {
  if (Array.isArray(v)) return v.map(canonical);
  if (v && typeof v === 'object') {
    return Object.fromEntries(Object.keys(v).sort().map(k => [k, canonical(v[k])]));
  }
  return v;
}

// normalize 替换每次运行都不同的值（ID、时间、ObjectId、token），便于比较两个服务器的输出。
function normalize(text) {
  const idKeys = '_id|id|uid|edit_uid|catid|cat_id|project_id|group_id|projectId|catId|apiId|interface_id|add_time|up_time|addTime|upTime|接口ID|分类ID';
  return text
    .replace(/\b[0-9a-f]{64,}\b/g, '<token>')
    .replace(/\b[0-9a-f]{24}\b/g, '<objectid>') // 要在数字 ID 之前替换，ObjectId 可能以数字开头
    .replace(new RegExp('(\\\\*"(?:' + idKeys + ')\\\\*":\\s*\\\\*"?)\\d+', 'g'), '$1<id>')
    .replace(/(接口ID|分类ID): \d+/g, '$1: <id>')
    .replace(/\(ID: \d+\)/g, '(ID: <id>)')
    .replace(/([?&](?:id|catid|project_id)=)\d+/g, '$1<id>')
    .replace(/\/project\/\d+/g, '/project/<id>')
    .replace(/\/interface\/api\/\d+/g, '/interface/api/<id>')
    .replace(/cat_\d+/g, 'cat_<id>')
    .replace(/\/user\/profile\/\d+/g, '/user/profile/<id>')
    .replace(/127\.0\.0\.1:\d+/g, '<server>')
    .replace(/\d{1,2}\/\d{1,2}\/\d{4}, \d{1,2}:\d{2}:\d{2} [AP]M/g, '<datetime>')
    .replace(/\d{4}[-/]\d{1,2}[-/]\d{1,2}[ T]\d{1,2}:\d{2}:\d{2}/g, '<datetime>');
}

function readSeed(name) {
  return JSON.parse(fs.readFileSync(name, 'utf8'));
}

module.exports = { api, must, normalize, canonical, readSeed };
