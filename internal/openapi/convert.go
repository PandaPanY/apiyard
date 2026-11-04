// Package openapi 把 Swagger 2 / OpenAPI 3 文档转换成 YApi 的接口数据。
//
// 转换规则逐条对应 YApi 的 exts/yapi-plugin-import-swagger/run.js，以及它调用的 swagger-client 的引用解析：
//   - $ref 被展开，展开后的对象末尾加上 "$$ref": "原引用"；循环引用里的内层引用保持 {"$ref": ...} 不展开；找不到的引用原样保留；
//   - allOf 的各个成员按顺序深度合并（数组拼接去重），对象自己的字段最后合并；
//   - 路径级的 parameters 追加到每个操作的参数后面，与操作里同名、同位置的参数重复时以操作为准；
//   - OpenAPI 3 先按 run.js 的 openapi2swagger 改写响应和 requestBody，再按 Swagger 2 处理。
//
// 测试用例 testdata/import/*.yapi.json 是在 YApi 容器里直接运行原版 run.js 得到的结果，Convert 的输出与之逐字段一致。
package openapi

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/PandaPanY/apiyard/internal/jsjson"
)

// Cat 是导入时要用到的分类。
type Cat struct {
	Name string
	Desc any // 字符串，或不存在时为 nil
}

// Result 对应 run.js 返回的 { apis, cats, basePath }。
type Result struct {
	BasePath string
	Cats     []Cat
	// APIs 中的每一项是一个接口，字段与 YApi 接口参数同名（title、path、method、req_query……），另有 catname 表示所属分类。
	APIs []*jsjson.Object
}

// ErrNotObject 表示文档不是 JSON 对象。
var ErrNotObject = errors.New("文档不是 JSON 对象")

// Convert 解析并转换一份 Swagger 2 或 OpenAPI 3 的 JSON 文档。
func Convert(text string) (*Result, error) {
	parsed, err := jsjson.Parse(text)
	if err != nil {
		return nil, err
	}
	root, ok := parsed.(*jsjson.Object)
	if !ok {
		return nil, ErrNotObject
	}
	if isOAS3(root) {
		openapiToSwagger(root)
	}
	r := &resolver{root: root}
	paths, _ := get(root, "paths").(*jsjson.Object)
	if paths != nil {
		paths = r.resolve(paths, nil).(*jsjson.Object)
		normalizePathParameters(paths)
	}

	res := &Result{}
	if bp, ok := get(root, "basePath").(string); ok {
		res.BasePath = bp
	}
	var originTags []any
	if tags, ok := get(root, "tags").([]any); ok {
		originTags = tags
		for _, t := range tags {
			name, _ := get(t, "name").(string)
			res.Cats = append(res.Cats, Cat{Name: name, Desc: get(t, "description")})
		}
	}

	if paths != nil {
		for _, path := range paths.Keys() {
			apis, ok := get(paths, path).(*jsjson.Object)
			if !ok {
				continue
			}
			apis.Delete("parameters") // 路径级的公共参数不是一个操作
			for _, method := range apis.Keys() {
				op, ok := get(apis, method).(*jsjson.Object)
				if !ok { // 字符串等非对象的值在原版里会抛异常，被跳过
					continue
				}
				api, err := handleOperation(op, path, method, originTags, root)
				if err != nil {
					continue
				}
				if name, ok := get(api, "catname").(string); ok && len(originTags) == 0 && !hasCat(res.Cats, name) {
					res.Cats = append(res.Cats, Cat{Name: name, Desc: name})
				}
				res.APIs = append(res.APIs, api)
			}
		}
	}

	// 只保留有接口的分类
	used := res.Cats[:0]
	for _, c := range res.Cats {
		for _, api := range res.APIs {
			if get(api, "catname") == c.Name {
				used = append(used, c)
				break
			}
		}
	}
	res.Cats = used
	return res, nil
}

func hasCat(cats []Cat, name string) bool {
	for _, c := range cats {
		if c.Name == name {
			return true
		}
	}
	return false
}

// get 读取对象字段，不是对象或字段不存在时返回 nil。
func get(v any, key string) any {
	if o, ok := v.(*jsjson.Object); ok {
		val, _ := o.Get(key)
		return val
	}
	return nil
}

// isOAS3 对应 res.openapi && compareVersions(res.openapi, '3.0.0') >= 0。
func isOAS3(root *jsjson.Object) bool {
	version, ok := get(root, "openapi").(string)
	if !ok || version == "" {
		return false
	}
	major, _, _ := strings.Cut(version, ".")
	n, err := strconv.Atoi(major)
	return err == nil && n >= 3
}

// openapiToSwagger 对应 run.js 的 openapi2swagger：响应的 content 里取 application/json、application/hal+json、*/* 合并到响应上，
// requestBody 改成一个 in: body 的参数。
func openapiToSwagger(root *jsjson.Object) {
	root.Set("swagger", "2.0")
	paths, _ := get(root, "paths").(*jsjson.Object)
	if paths == nil {
		return
	}
	for _, path := range paths.Keys() {
		apis, _ := get(paths, path).(*jsjson.Object)
		if apis == nil {
			continue
		}
		for _, method := range apis.Keys() {
			op, _ := get(apis, method).(*jsjson.Object)
			if op == nil {
				continue
			}
			if responses, ok := get(op, "responses").(*jsjson.Object); ok {
				for _, code := range responses.Keys() {
					res, _ := get(responses, code).(*jsjson.Object)
					if res == nil {
						continue
					}
					for _, mediaType := range []string{"application/json", "application/hal+json", "*/*"} {
						media, ok := get(get(res, "content"), mediaType).(*jsjson.Object)
						if !ok {
							continue
						}
						for _, k := range media.Keys() {
							v, _ := media.Get(k)
							res.Set(k, v)
						}
						res.Delete("content")
					}
				}
			}
			if body, ok := op.Get("requestBody"); ok && body != nil && body != false {
				params, _ := get(op, "parameters").([]any)
				param := jsjson.NewObject("type", "object", "name", "body", "in", "body")
				content, hasContent := get(body, "content").(*jsjson.Object)
				media, hasMedia := get(content, "application/json").(*jsjson.Object)
				switch {
				case !hasContent || !hasMedia: // 原版在这里抛异常，被 catch 后使用空对象
					param.Set("schema", jsjson.NewObject())
				default:
					if schema, ok := media.Get("schema"); ok {
						param.Set("schema", schema)
					}
				}
				op.Set("parameters", append(params, param))
			}
		}
	}
}

// ---- 引用解析（swagger-client） ----

type resolver struct {
	root *jsjson.Object
}

// resolve 返回展开引用后的副本，stack 是当前展开路径上的引用，用于识别循环引用。
func (r *resolver) resolve(v any, stack []string) any {
	switch x := v.(type) {
	case *jsjson.Object:
		if ref, ok := get(x, "$ref").(string); ok {
			return r.resolveRef(x, ref, stack)
		}
		out := jsjson.NewObject()
		for _, k := range x.Keys() {
			val, _ := x.Get(k)
			out.Set(k, r.resolve(val, stack))
		}
		if all, ok := get(out, "allOf").([]any); ok {
			return mergeAllOf(out, all)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = r.resolve(item, stack)
		}
		return out
	default:
		return v
	}
}

func (r *resolver) resolveRef(node *jsjson.Object, ref string, stack []string) any {
	for _, s := range stack {
		if s == ref { // 循环引用：内层保持不展开
			return node
		}
	}
	target, ok := r.lookup(ref)
	if !ok { // 找不到的引用原样保留
		return node
	}
	resolved := r.resolve(target, append(stack, ref))
	if obj, ok := resolved.(*jsjson.Object); ok {
		obj.Delete("$$ref") // 引用指向另一个引用时，记录最外层的引用
		obj.Set("$$ref", ref)
	}
	return resolved
}

// lookup 按 JSON Pointer 在文档内查找 "#/a/b"，支持 ~0、~1 和 %XX 转义。
func (r *resolver) lookup(ref string) (any, bool) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, false
	}
	var cur any = r.root
	for _, part := range strings.Split(ref[2:], "/") {
		if unescaped, err := unescapePercent(part); err == nil {
			part = unescaped
		}
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch c := cur.(type) {
		case *jsjson.Object:
			v, ok := c.Get(part)
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(c) {
				return nil, false
			}
			cur = c[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

func unescapePercent(s string) (string, error) {
	if !strings.Contains(s, "%") {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			n, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
			if err != nil {
				return "", err
			}
			b.WriteByte(byte(n))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String(), nil
}

// mergeAllOf 把 allOf 的成员依次深度合并，最后合并对象自身的其他字段；成员上的 $$ref 不保留。
func mergeAllOf(obj *jsjson.Object, members []any) *jsjson.Object {
	out := jsjson.NewObject()
	for _, m := range members {
		if mo, ok := m.(*jsjson.Object); ok {
			mo = mo.Clone()
			mo.Delete("$$ref")
			deepMerge(out, mo)
		}
	}
	own := obj.Clone()
	own.Delete("allOf")
	deepMerge(out, own)
	return out
}

func deepMerge(dst, src *jsjson.Object) {
	for _, k := range src.Keys() {
		sv, _ := src.Get(k)
		dv, exists := dst.Get(k)
		switch {
		case !exists:
			dst.Set(k, sv)
		case isObject(dv) && isObject(sv):
			merged := dv.(*jsjson.Object).Clone()
			deepMerge(merged, sv.(*jsjson.Object))
			dst.Set(k, merged)
		case isArray(dv) && isArray(sv):
			dst.Set(k, appendUnique(dv.([]any), sv.([]any)))
		default:
			dst.Set(k, sv)
		}
	}
}

func isObject(v any) bool { _, ok := v.(*jsjson.Object); return ok }
func isArray(v any) bool  { _, ok := v.([]any); return ok }

func appendUnique(a, b []any) []any {
	out := append([]any(nil), a...)
	for _, item := range b {
		dup := false
		for _, existing := range out {
			if existing == item {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, item)
		}
	}
	return out
}

// normalizePathParameters 把路径级参数追加到每个操作的参数后面（同名同位置的以操作为准）。
func normalizePathParameters(paths *jsjson.Object) {
	for _, path := range paths.Keys() {
		item, _ := get(paths, path).(*jsjson.Object)
		pathParams, _ := get(item, "parameters").([]any)
		if len(pathParams) == 0 {
			continue
		}
		for _, method := range item.Keys() {
			op, ok := get(item, method).(*jsjson.Object)
			if !ok || method == "parameters" {
				continue
			}
			params, _ := get(op, "parameters").([]any)
			for _, p := range pathParams {
				exists := false
				for _, q := range params {
					if get(q, "name") == get(p, "name") && get(q, "in") == get(p, "in") {
						exists = true
						break
					}
				}
				if !exists {
					params = append(params, p)
				}
			}
			op.Set("parameters", params)
		}
	}
}

// ---- run.js 的 handleSwagger ----

var versionTag = regexp.MustCompile(`v[0-9.]+`)

func handleOperation(data *jsjson.Object, path, method string, originTags []any, root *jsjson.Object) (*jsjson.Object, error) {
	api := jsjson.NewObject("method", strings.ToUpper(method))
	title := get(data, "summary")
	if !truthy(title) {
		title = path
	}
	api.Set("title", title)
	if desc, ok := data.Get("description"); ok {
		api.Set("desc", desc)
	}
	api.Set("catname", nil)
	if tags, ok := get(data, "tags").([]any); ok {
		api.Set("tag", tags)
		for _, tag := range tags {
			if versionTag.MatchString(jsjson.String(tag)) {
				continue
			}
			if len(originTags) > 0 && tagDeclared(originTags, tag) {
				api.Set("catname", tag)
				break
			}
			if len(originTags) == 0 {
				api.Set("catname", tag)
				break
			}
		}
	}
	api.Set("path", handlePath(path))
	reqParams, bodyForm, headers, query := []any{}, []any{}, []any{}, []any{}
	// 先按原版的字段顺序占位，参数处理完后再写入（Set 会原地替换）
	for _, k := range []string{"req_params", "req_body_form", "req_headers", "req_query"} {
		api.Set(k, []any{})
	}
	api.Set("req_body_type", "raw")
	api.Set("res_body_type", "raw")

	if containsValue(get(data, "produces"), "application/json") {
		api.Set("res_body_type", "json")
		api.Set("res_body_is_json_schema", true)
	}
	if consumes, ok := get(data, "consumes").([]any); ok {
		if containsValue(consumes, "application/x-www-form-urlencoded") || containsValue(consumes, "multipart/form-data") {
			api.Set("req_body_type", "form")
		} else if containsValue(consumes, "application/json") {
			api.Set("req_body_type", "json")
			api.Set("req_body_is_json_schema", true)
		}
	}

	resBody, err := handleResponse(get(data, "responses"))
	if err != nil {
		return nil, err
	}
	api.Set("res_body", resBody)
	if _, err := jsjson.Parse(resBody); err == nil {
		api.Set("res_body_type", "json")
		api.Set("res_body_is_json_schema", true)
	} else {
		api.Set("res_body_type", "raw")
	}

	if params, ok := get(data, "parameters").([]any); ok {
		for _, param := range params {
			if p, ok := param.(*jsjson.Object); ok {
				if ref, ok := get(p, "$ref").(string); ok { // 没能展开的参数引用，原版再按 #/parameters/... 找一次
					param = lookupParameter(ref, root)
				}
			}
			p, ok := param.(*jsjson.Object)
			if !ok { // 原版读取 param.name 时抛异常，整个接口被跳过
				return nil, errors.New("参数无效")
			}
			item := jsjson.NewObject()
			if name, ok := p.Get("name"); ok {
				item.Set("name", name)
			}
			if desc, ok := p.Get("description"); ok {
				item.Set("desc", desc)
			}
			required := "0"
			if truthy(get(p, "required")) {
				required = "1"
			}
			item.Set("required", required)

			in := get(p, "in")
			switch {
			case !truthy(in):
				query = append(query, item)
			case in == "path":
				reqParams = append(reqParams, item)
			case in == "query":
				query = append(query, item)
			case in == "header":
				headers = append(headers, item)
			case in == "formData":
				typ := "text"
				if get(p, "type") == "file" {
					typ = "file"
				}
				item.Set("type", typ)
				if ex := get(p, "example"); truthy(ex) {
					item.Set("example", ex)
				}
				bodyForm = append(bodyForm, item)
			case in == "body":
				if schema, ok := p.Get("schema"); ok {
					text, err := jsjson.Marshal(jsjson.NormalizeNumbers(schema), "  ")
					if err != nil {
						return nil, err
					}
					api.Set("req_body_other", string(text))
					api.Set("req_body_type", "json")
					api.Set("req_body_is_json_schema", true)
				}
			}
		}
	}
	api.Set("req_params", reqParams)
	api.Set("req_body_form", bodyForm)
	api.Set("req_headers", headers)
	api.Set("req_query", query)
	return api, nil
}

func tagDeclared(originTags []any, tag any) bool {
	for _, t := range originTags {
		if get(t, "name") == tag {
			return true
		}
	}
	return false
}

// lookupParameter 对应 run.js 的 simpleJsonPathParse(ref, {parameters: SwaggerData.parameters})。
func lookupParameter(ref string, root *jsjson.Object) any {
	if !strings.HasPrefix(ref, "#/") || len(ref) <= 2 {
		return nil
	}
	var cur any = jsjson.NewObject("parameters", get(root, "parameters"))
	for _, k := range strings.Split(ref[2:], "/") {
		if k != "" {
			cur = get(cur, k)
		}
	}
	return cur
}

// handlePath 补上开头的斜杠、去掉结尾的斜杠，"/" 保持不变。
func handlePath(path string) string {
	if path == "/" {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimSuffix(path, "/")
}

// handleResponse 取 200 响应（没有时取第一个）的 schema，没有 schema 时取描述。
func handleResponse(v any) (string, error) {
	responses, ok := v.(*jsjson.Object)
	if !ok || responses.Len() == 0 {
		return "", nil
	}
	codes := responses.Keys()
	code := codes[0]
	if _, ok := responses.Get("200"); ok {
		code = "200"
	}
	res, _ := responses.Get(code)
	switch r := res.(type) {
	case *jsjson.Object:
		if schema, ok := r.Get("schema"); ok && truthy(schema) {
			text, err := jsjson.Marshal(jsjson.NormalizeNumbers(schema), "  ")
			return string(text), err
		}
		if desc := get(r, "description"); truthy(desc) {
			return jsjson.String(desc), nil
		}
		return "", nil
	case string:
		return r, nil
	default:
		return "", nil
	}
}

// containsValue 对应 x.indexOf(value) > -1：数组按元素查找，字符串按子串查找。
func containsValue(v any, value string) bool {
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			if item == value {
				return true
			}
		}
	case string:
		return strings.Contains(x, value)
	}
	return false
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	default:
		return jsjson.String(v) != "0"
	}
}
