"""生成兼容场景 testdata/compat/scenarios/10-mock.json（Mock 服务）。

场景里有 multipart 请求体和正则，需要 CRLF 和反斜杠；手写 JSON 容易转义出错，所以用这个脚本生成。
修改场景请改这里，再运行：python testenv/yapi/gen-mock-scenario.py
"""
import json
import os

CRLF = chr(13) + chr(10)
BS = chr(92)  # 反斜杠

steps = []


def step(name, method, path, **kw):
    s = {'name': name, 'method': method, 'path': path}
    s.update(kw)
    steps.append(s)


def add_iface(var, title, path, method='GET', **fields):
    body = {'project_id': '{{pid}}', 'catid': '{{cat}}', 'title': title, 'path': path, 'method': method,
            'req_headers': [], 'req_body_form': []}  # 原版新建表单接口时缺少这两项会出错
    body.update(fields)
    step('新建接口：' + title, 'POST', '/api/interface/add', body=body, capture={var: 'data._id'})


def mock(name, method, path, **kw):
    step(name, method, '/mock/{{pid}}' + path, **kw)


step('管理员登录', 'POST', '/api/user/login', body={'email': 'admin@admin.com', 'password': 'ymfe.org'})
step('新建分组', 'POST', '/api/group/add', body={'group_name': 'Mock 组', 'owner_uids': []}, capture={'gid': 'data._id'})
step('新建项目', 'POST', '/api/project/add',
     body={'name': 'Mock 项目', 'basepath': '/api', 'group_id': '{{gid}}', 'desc': 'Mock 测试'},
     capture={'pid': 'data._id'})
step('默认分类', 'GET', '/api/project/get', query={'id': '{{pid}}'}, capture={'cat': 'data.cat.0._id'})

user_tpl = {
    'code': 0,
    'data|2-4': [{
        'id|+1': 1, 'name': '@cname', 'email': '@email', 'age|18-60': 1, 'active|1': True,
        'created': '@datetime', 'tags|1-3': ['@word'], 'score|1-100.1-2': 1, 'city': '@city(true)',
        'ip': '@ip', 'guid': '@guid', 'color': '@color', 'url': '@url',
    }],
    'phone|regexp': '1[3-9]' + BS + 'd{9}',
}
add_iface('i1', '用户列表', '/users', res_body_type='json', res_body=json.dumps(user_tpl, ensure_ascii=False))
add_iface('i2', '用户详情', '/users/{id}', res_body_type='json',
          res_body='{"id": "${query.id}", "q": "${query.q}", "params": "${params.id}", "none": "${query.none}"}')
add_iface('i3', '新建用户', '/users', method='POST', req_body_type='json', res_body_type='json',
          res_body="{name: '${body.name}', // JSON5 注释\n tag: '${body.tags}', all: '${params.name}',}")
add_iface('i4', '纯文本', '/plain', res_body_type='raw', res_body='pong @email')
add_iface('i5', 'HTML', '/html', res_body_type='raw', res_body='  <p>hi</p>')
add_iface('i6', '空响应体', '/empty', res_body_type='json', res_body='')
add_iface('i7', '数字', '/number', res_body_type='json', res_body='123')
add_iface('i8', 'null', '/null', res_body_type='json', res_body='null')
add_iface('i9', '不是 JSON', '/text', res_body_type='json', res_body='hello @upper("x")')
# 原版的 json-schema-faker 0.5.0-rc16 有缺陷：非必填字段随机缺失，没有 maxItems 的数组总是空的。
# 这里全部字段必填、数组长度固定，原版生成的结构才是确定的（ApiYard 总是生成全部字段，见 docs/architecture.md）
schema = {
    'type': 'object', 'required': ['id', 'name', 'email', 'ok', 'list'],
    'properties': {
        'id': {'type': 'integer', 'minimum': 1, 'maximum': 9},
        'name': {'type': 'string', 'mock': {'mock': '@cname'}},
        'email': {'type': 'string', 'mock': {'mock': '@email'}},
        'ok': {'type': 'boolean'},
        'list': {'type': 'array', 'minItems': 2, 'maxItems': 2,
                 'items': {'type': 'object', 'required': ['n'], 'properties': {'n': {'type': 'number', 'mock': {'mock': '@integer(1, 5)'}}}}},
    },
}
add_iface('i10', 'JSON Schema', '/schema', res_body_type='json', res_body_is_json_schema=True,
          res_body=json.dumps(schema, ensure_ascii=False))
add_iface('i11', '查询参数一', '/search?type=1', res_body_type='json', res_body='{"type": 1}')
add_iface('i12', '查询参数二', '/search?type=2&page=1', res_body_type='json', res_body='{"type": 2}')
add_iface('i13', '段内参数', '/files/{name}.json', res_body_type='json', res_body='{"file": "${query.name}"}')
add_iface('i14', '冒号参数', '/orders/:oid/items', res_body_type='json', res_body='{"oid": "${query.oid}"}')
add_iface('i15', 'Mock.js 出错', '/broken', res_body_type='json', res_body='{"img": "@dataImage", "n|1-3": 1}')
add_iface('i16', '取不到的变量', '/bad-var', method='POST', res_body_type='json', res_body='{"v": "${body.x.y}"}')
add_iface('i17', '表单', '/form', method='POST', req_body_type='form', res_body_type='json',
          res_body='{"a": "${body.a}", "f": "${body.f}", "all": "${body}"}')
add_iface('i18', 'XML', '/xml', res_body_type='xml', res_body='<a>@email</a>')

# ---- 基本 ----
mock('Mock 规则（形状）', 'GET', '/api/users', shape=True, responseHeaders=['Content-Type'])
mock('路径参数和查询参数', 'GET', '/api/users/42', query={'q': '搜索'})
mock('请求体变量（JSON5 模板）', 'POST', '/api/users', body={'name': 'tom', 'tags': ['a', 'b']})
mock('纯文本不处理占位符', 'GET', '/api/plain', responseHeaders=['Content-Type'])
mock('以 < 开头为 HTML', 'GET', '/api/html', responseHeaders=['Content-Type'])
mock('空响应体', 'GET', '/api/empty', responseHeaders=['Content-Type'])
mock('数字', 'GET', '/api/number', responseHeaders=['Content-Type'])
mock('null 返回 204', 'GET', '/api/null')
mock('不是 JSON 的模板按字符串处理', 'GET', '/api/text', responseHeaders=['Content-Type'])
mock('JSON Schema（形状）', 'GET', '/api/schema', shape=True)
mock('XML 原样返回', 'GET', '/api/xml', responseHeaders=['Content-Type'])

# ---- 匹配 ----
mock('query_path：type=1', 'GET', '/api/search', query={'type': '1'})
mock('query_path：只看最后一个参数', 'GET', '/api/search', query={'type': '9', 'page': '1'})
mock('query_path：都不匹配', 'GET', '/api/search', query={'type': '3'}, errcode=404)
mock('段内的 {name}', 'GET', '/api/files/report.json')
mock('冒号参数', 'GET', '/api/orders/A01/items')
mock('段数不同', 'GET', '/api/orders/A01', errcode=404)
mock('方法不对', 'POST', '/api/plain', errcode=404)
mock('不存在的接口', 'GET', '/api/nope', errcode=404)
mock('基础路径只按长度截掉', 'GET', '/xyz/plain')
mock('路径编码', 'GET', '/api/users/%E4%B8%AD')

# ---- 项目 ID ----
step('项目 ID 不是数字', 'GET', '/mock/abc/x', errcode=403)
step('项目不存在', 'GET', '/mock/987654/x', errcode=403)
step('项目 ID 是小数', 'GET', '/mock/1.5/x', errcode=403)
step('没有项目 ID', 'GET', '/mock/', errcode=400)

# ---- 出错 ----
mock('Mock.js 出错时返回原模板', 'GET', '/api/broken')
mock('变量取不到时报解析出错', 'POST', '/api/bad-var', body={'x': 5}, errcode=400)

# ---- 请求体格式 ----
mock('urlencoded 表单', 'POST', '/api/form', rawBody='a=1&a=2&f=%E4%B8%AD+x', contentType='application/x-www-form-urlencoded')
boundary = 'XBOUNDARY'
multipart = ('--' + boundary + CRLF + 'Content-Disposition: form-data; name="a"' + CRLF + CRLF + 'va' + CRLF +
             '--' + boundary + CRLF + 'Content-Disposition: form-data; name="f"; filename="t.txt"' + CRLF +
             'Content-Type: text/plain' + CRLF + CRLF + 'file content' + CRLF + '--' + boundary + '--' + CRLF)
mock('multipart：请求体换成 fields', 'POST', '/api/form', rawBody=multipart, contentType='multipart/form-data; boundary=' + boundary)
mock('纯文本请求体', 'POST', '/api/form', rawBody='hello', contentType='text/plain', errcode=400)

# ---- 跨域 ----
mock('带 Origin 的请求', 'GET', '/api/plain', headers={'Origin': 'http://example.com'},
     responseHeaders=['Access-Control-Allow-Origin', 'Access-Control-Allow-Credentials'])
mock('跨域预检', 'OPTIONS', '/api/plain',
     headers={'Origin': 'http://example.com', 'Access-Control-Request-Method': 'POST', 'Access-Control-Request-Headers': 'x-token'},
     responseHeaders=['Access-Control-Allow-Origin', 'Access-Control-Allow-Methods', 'Access-Control-Allow-Headers',
                      'Access-Control-Max-Age', 'Content-Type'])
mock('不是预检的 OPTIONS', 'OPTIONS', '/api/plain', errcode=404)

# ---- 严格模式 ----
step('开启严格模式', 'POST', '/api/project/up', body={'id': '{{pid}}', 'strice': True})
add_iface('s1', '严格：查询参数', '/strict/query',
          req_query=[{'name': 'page', 'required': '1'}, {'name': 'size', 'required': '1'}, {'name': 'opt', 'required': '0'}],
          res_body_type='json', res_body='{"ok": true}')
add_iface('s2', '严格：表单', '/strict/form', method='POST', req_body_type='form',
          req_body_form=[{'name': 'a', 'type': 'text', 'required': '1'}, {'name': 'file', 'type': 'file', 'required': '1'}],
          res_body_type='json', res_body='{"ok": true}')
body_schema = {'type': 'object', 'required': ['name', 'age'],
               'properties': {'name': {'type': 'string', 'minLength': 2}, 'age': {'type': 'integer', 'minimum': 1},
                              'tags': {'type': 'array', 'items': {'type': 'string'}}}}
add_iface('s3', '严格：JSON Schema', '/strict/json', method='POST', req_body_type='json', req_body_is_json_schema=True,
          req_body_other=json.dumps(body_schema), res_body_type='json', res_body='{"ok": true}')
mock('缺少查询参数', 'GET', '/api/strict/query', query={'size': '1'}, errcode=404)
mock('查询参数为空字符串', 'GET', '/api/strict/query', query={'page': '', 'size': '1'}, errcode=404)
mock('查询参数齐全', 'GET', '/api/strict/query', query={'page': '1', 'size': '2'})
mock('缺少表单字段', 'POST', '/api/strict/form', rawBody='a=1', contentType='application/x-www-form-urlencoded', errcode=404)
mock('multipart 表单齐全', 'POST', '/api/strict/form', rawBody=multipart.replace('name="f"', 'name="file"'),
     contentType='multipart/form-data; boundary=' + boundary)
mock('JSON 缺少必需属性', 'POST', '/api/strict/json', body={'name': 'tom'}, errcode=404)
mock('JSON 类型不对', 'POST', '/api/strict/json', body={'name': 'tom', 'age': 1.5}, errcode=404)
mock('JSON 数组元素不对', 'POST', '/api/strict/json', body={'name': 'tom', 'age': 2, 'tags': ['a', 1]}, errcode=404)
mock('JSON 长度不够', 'POST', '/api/strict/json', body={'name': 't', 'age': 2}, errcode=404)
mock('JSON 通过', 'POST', '/api/strict/json', body={'name': 'tom', 'age': 2})
mock('GET 不校验请求体', 'GET', '/api/strict/query', query={'page': '1', 'size': '1'})

scenario = {
    'description': 'M5：Mock 服务。地址匹配（完整路径、query_path、路径参数）、Mock.js 规则和占位符、${query.x} 变量、'
                   'JSON Schema、各种响应类型、跨域、严格模式校验。随机生成的响应只比对形状',
    'steps': steps,
}
out = os.path.join(os.path.dirname(__file__), '..', '..', 'testdata', 'compat', 'scenarios', '10-mock.json')
with open(out, 'w', encoding='utf-8', newline='\n') as f:
    json.dump(scenario, f, ensure_ascii=False, indent=2)
    f.write('\n')
print('已生成', os.path.normpath(out), len(steps), '步')
