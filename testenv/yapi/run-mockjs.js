// 在 YApi 容器里用原版的 Mock.js（加上 YApi 的 mock-extra）处理 testdata/mockjs/cases.json 里的每个模板，
// 每个模板生成 SAMPLES 次，输出各字段的统计特征，作为 internal/mockjs 交叉核对的标准答案。
// 统计和分类的算法与 internal/mockjs/crosscheck_test.go 完全相同，改一处要同时改另一处。
// 用法见 testenv/yapi/README.md。
const Mock = require('mockjs');
const mockExtra = require('./common/mock-extra.js');

const SAMPLES = 400;
const query = { id: '7', tags: ['a', 'b'] };
const body = { name: 'tom', nested: { x: 1 } };
const vars = { query, body, params: Object.assign({}, query, body) };

const classes = [
  ['empty', /^$/],
  ['email', /^[a-z]\.[a-z]{3,10}@\S+$/],
  ['image', /^http:\/\/dummyimage\.com\//],
  ['url', /^[a-z0-9]+:\/\/\S+\/[a-z]+$/],
  ['datetime', /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/],
  ['date', /^\d{4}-\d{2}-\d{2}$/],
  ['time', /^\d{2}:\d{2}:\d{2}$/],
  ['ip', /^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$/],
  ['guid', /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/],
  ['hex', /^#[0-9a-f]{6}$/],
  ['color', /^(rgba?|hsl)\(/],
  ['idcard', /^\d{17}[\dX]$/],
  ['digits', /^\d+$/],
  ['han', /^[一-龥。 ]+$/],
  ['lower', /^[a-z]+$/],
  ['upper', /^[A-Z]+$/],
  ['capital', /^[A-Z][a-z]+$/],
  ['words', /^[A-Z][a-z]*( [A-Z][a-z]*)+$/],
  ['sentence', /^[A-Z][a-z ]*\.( [A-Z][a-z ]*\.)*$/],
  ['alnum', /^[A-Za-z0-9]+$/],
];

function classify(s) {
  for (const [name, re] of classes) if (re.test(s)) return name;
  return 'other';
}

function typeOf(v) {
  if (v === null) return 'null';
  if (Array.isArray(v)) return 'array';
  return typeof v;
}

// 范围用 5% 和 95% 分位数（按排名取），不用最小、最大值：长尾的字段（例如区县名的长度）极值很不稳定
function quantiles(list) {
  if (!list.length) return undefined;
  const s = list.slice().sort((a, b) => a - b);
  const at = q => s[Math.min(s.length - 1, Math.floor(q * (s.length - 1)))];
  return [at(0.05), at(0.95)];
}

function add(st, key, x) {
  (st[key] || (st[key] = [])).push(x);
}

function walk(paths, v, path) {
  const st = paths[path] || (paths[path] = { n: 0, types: {}, classes: {}, numCount: 0, intCount: 0, keys: {} });
  st.n++;
  const t = typeOf(v);
  st.types[t] = (st.types[t] || 0) + 1;
  if (t === 'number') {
    st.numCount++;
    if (Number.isInteger(v)) st.intCount++;
    add(st, 'num', v);
  } else if (t === 'string') {
    const c = classify(v);
    st.classes[c] = (st.classes[c] || 0) + 1;
    add(st, 'len', [...v].length);
  } else if (t === 'array') {
    add(st, 'arr', v.length);
    v.forEach(item => walk(paths, item, path + '[]'));
  } else if (t === 'object') {
    for (const k of Object.keys(v)) {
      st.keys[k] = 1;
      walk(paths, v[k], path + '.' + k);
    }
  }
}

let input = '';
process.stdin.on('data', c => (input += c));
process.stdin.on('end', () => {
  const out = {};
  for (const c of JSON.parse(input)) {
    const paths = {};
    try {
      for (let i = 0; i < SAMPLES; i++) {
        const tpl = mockExtra(JSON.parse(JSON.stringify(c.tpl)), vars);
        walk(paths, Mock.mock(tpl), '$');
      }
      for (const st of Object.values(paths)) {
        for (const k of ['num', 'len', 'arr']) if (st[k]) st[k] = quantiles(st[k]);
      }
      out[c.name] = { paths };
    } catch (e) {
      out[c.name] = { error: e.message };
    }
  }
  process.stdout.write(JSON.stringify(out, null, 1) + '\n');
});
