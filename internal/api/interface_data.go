package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/PandaPanY/apiyard/internal/store"
)

// ---- 文档格式 ----

// interfaceDoc 是完整的接口文档（YApi 的 Model.get）；可选字段为 nil 时不输出。
func interfaceDoc(it *store.Interface) map[string]any {
	m := map[string]any{
		"__v": 0, "_id": it.ID, "uid": it.UID, "edit_uid": it.EditUID, "project_id": it.ProjectID, "catid": it.CatID,
		"title": it.Title, "path": it.Path, "method": it.Method, "status": it.Status, "type": it.Type,
		"api_opened": it.APIOpened, "index": it.Position, "query_path": json.RawMessage(it.QueryPath),
		"req_query": json.RawMessage(it.ReqQuery), "req_headers": json.RawMessage(it.ReqHeaders),
		"req_params": json.RawMessage(it.ReqParams), "req_body_form": json.RawMessage(it.ReqBodyForm),
		"req_body_is_json_schema": it.ReqBodyIsJSONSchema, "res_body_is_json_schema": it.ResBodyIsJSONSchema,
		"tag": json.RawMessage(it.Tag), "add_time": it.AddTime, "up_time": it.UpTime,
	}
	setOptional(m, "desc", it.Desc)
	setOptional(m, "markdown", it.Markdown)
	setOptional(m, "req_body_type", it.ReqBodyType)
	setOptional(m, "req_body_other", it.ReqBodyOther)
	setOptional(m, "res_body_type", it.ResBodyType)
	setOptional(m, "res_body", it.ResBody)
	setOptional(m, "custom_field_value", it.CustomFieldValue)
	return m
}

// 列表接口只返回部分字段，与 YApi 各查询的 select 一致。
// YApi 的 listByOptionWithPage 把 select 写成 "up_time, index, tag"，逗号让 up_time 和 index 失效，所以分页列表里没有这两个字段。
var (
	interfacePageFields = []string{"_id", "title", "uid", "path", "method", "project_id", "catid", "edit_uid",
		"api_opened", "status", "add_time", "tag"}
	interfaceAllFields = []string{"_id", "title", "uid", "path", "method", "project_id", "catid", "edit_uid",
		"status", "add_time", "up_time"}
	interfaceMenuFields = []string{"_id", "title", "uid", "path", "method", "project_id", "catid", "edit_uid",
		"status", "add_time", "up_time", "index", "tag"}
)

func pickFields(doc map[string]any, keys []string) map[string]any {
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		if v, ok := doc[k]; ok {
			out[k] = v
		}
	}
	return out
}

func interfaceDocs(list []*store.Interface, keys []string) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, it := range list {
		out = append(out, pickFields(interfaceDoc(it), keys))
	}
	return out
}

// ---- 路径 ----

var (
	interfacePathPattern = regexp.MustCompile(`^/[a-zA-Z0-9\-/_:!.{}=]*$`)
	pathVarPattern       = regexp.MustCompile(`\{(.+?)\}`)
)

const badPathMsg = "path第一位必需为 /, 只允许由 字母数字-/_:.! 组成"

// splitPath 与 Node 的 url.parse(path, true) 一样拆出路径和查询参数，查询参数保持出现顺序。
func splitPath(raw string) (pathname string, query [][2]string) {
	if i := strings.IndexByte(raw, '#'); i >= 0 {
		raw = raw[:i]
	}
	pathname, rawQuery, _ := strings.Cut(raw, "?")
	index := map[string]int{}
	for pair := range strings.SplitSeq(rawQuery, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		k, v = unescapeQuery(k), unescapeQuery(v)
		if i, ok := index[k]; ok {
			query[i][1] += "," + v // 同名参数在 YApi 中是数组，存入字符串字段时会出错；这里合并成一个值
			continue
		}
		index[k] = len(query)
		query = append(query, [2]string{k, v})
	}
	return pathname, query
}

// unescapeQuery 与 Node querystring.unescape 一样解码，格式不对时保留原文。
func unescapeQuery(s string) string {
	if u, err := url.QueryUnescape(s); err == nil {
		return u
	}
	return s
}

// queryPathJSON 生成 query_path 子文档。
func queryPathJSON(pathname string, query [][2]string) (string, error) {
	params := make([]map[string]any, 0, len(query))
	for _, q := range query {
		params = append(params, map[string]any{"_id": store.NewObjectID(), "name": q[0], "value": q[1]})
	}
	return marshalCompact(map[string]any{"path": pathname, "params": params})
}

// addPathParams 与 YApi handleVarPath 相同：把路径中的 :name 段和 {name} 加入 req_params（已有同名的不加）。
func addPathParams(path string, params []any) []any {
	insert := func(name string) {
		for _, p := range params {
			if m, ok := p.(map[string]any); ok && m["name"] == name {
				return
			}
		}
		params = append(params, map[string]any{"name": name, "desc": ""})
	}
	if strings.Contains(path, ":") {
		segs := strings.Split(path, "/")
		for _, seg := range segs[1:] {
			if strings.HasPrefix(seg, ":") {
				insert(seg[1:])
			}
		}
	}
	for _, m := range pathVarPattern.FindAllStringSubmatch(path, -1) {
		insert(m[1])
	}
	return params
}

// handleHeaders 与 YApi 相同：form 和 json 请求体自动设置 Content-Type 请求头。
// YApi 在 form 请求缺少 req_headers 或 req_body_form 时会出错，这里按空数组处理。
func handleHeaders(p map[string]any) {
	var value string
	switch p["req_body_type"] {
	case "form":
		value = "application/x-www-form-urlencoded"
		for _, item := range arrayOr(p["req_body_form"]) {
			if m, ok := item.(map[string]any); ok && m["type"] == "file" {
				value = "multipart/form-data"
			}
		}
	case "json":
		value = "application/json"
	default:
		return
	}
	headers := arrayOr(p["req_headers"])
	found := false
	for _, h := range headers {
		if m, ok := h.(map[string]any); ok && m["name"] == "Content-Type" {
			m["value"] = value
			found = true
		}
	}
	if !found {
		headers = append([]any{map[string]any{"name": "Content-Type", "value": value}}, headers...)
	}
	p["req_headers"] = headers
}

// ---- 子文档整理 ----

// itemSpec 描述一种参数子文档：可保存的字段，以及是否有 required（默认 "1"）和 type（text/file）。
type itemSpec struct {
	keys     []string
	required bool
}

var (
	paramItem  = itemSpec{keys: []string{"name", "value", "example", "desc"}, required: true}         // req_query、req_headers
	pathItem   = itemSpec{keys: []string{"name", "desc", "example"}}                                  // req_params
	formItem   = itemSpec{keys: []string{"name", "type", "example", "value", "desc"}, required: true} // req_body_form
	errBadItem = errors.New("参数子项的字段类型不正确")
)

// normalizeItems 按 Mongoose 子文档的规则整理数组：只保留定义过的字段并转为字符串，补上 _id 和 required 默认值。
func normalizeItems(items []any, spec itemSpec) (string, error) {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		src, ok := item.(map[string]any)
		if !ok {
			return "", errBadItem
		}
		doc := map[string]any{"_id": subdocID(src["_id"])}
		for _, k := range spec.keys {
			s, present, err := castString(src[k])
			if err != nil {
				return "", err
			}
			if !present || (k == "type" && s != "text" && s != "file") {
				continue
			}
			doc[k] = s
		}
		if spec.required {
			doc["required"] = "1"
			if s, _, _ := castString(src["required"]); s == "0" {
				doc["required"] = "0"
			}
		}
		out = append(out, doc)
	}
	return marshalCompact(out)
}

var objectIDPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)

// subdocID 沿用合法的 ObjectId，否则新生成一个。
func subdocID(v any) string {
	if s, ok := v.(string); ok && objectIDPattern.MatchString(s) {
		return s
	}
	return store.NewObjectID()
}

// castString 与 Mongoose 的 String 类型转换一致：数字和布尔转为字符串，null/缺失视为没有该字段，其他类型报错。
func castString(v any) (s string, present bool, err error) {
	switch x := v.(type) {
	case nil:
		return "", false, nil
	case string:
		return x, true, nil
	case json.Number:
		return x.String(), true, nil
	case bool:
		return strconv.FormatBool(x), true, nil
	default:
		return "", false, errBadItem
	}
}

// castBool 与 Mongoose 的 Boolean 类型转换一致。
func castBool(v any) (bool, error) {
	switch x := v.(type) {
	case bool:
		return x, nil
	case string:
		switch x {
		case "true", "1", "yes":
			return true, nil
		case "false", "0", "no", "":
			return false, nil
		}
	case json.Number:
		switch x.String() {
		case "1":
			return true, nil
		case "0":
			return false, nil
		}
	}
	return false, fmt.Errorf("无法转换为布尔值: %v", v)
}

// applyInterfaceParams 把参数中出现的字段写入接口（参数已经过校验）。
// 不处理 project_id：YApi 允许借修改接口把它移到别的项目，这里不允许。
func applyInterfaceParams(it *store.Interface, p map[string]any) error {
	strFields := map[string]*string{"title": &it.Title, "path": &it.Path, "method": &it.Method, "status": &it.Status}
	for k, dst := range strFields {
		if s, ok := p[k].(string); ok {
			*dst = s
		}
	}
	optFields := map[string]**string{
		"desc": &it.Desc, "markdown": &it.Markdown, "req_body_type": &it.ReqBodyType, "req_body_other": &it.ReqBodyOther,
		"res_body_type": &it.ResBodyType, "res_body": &it.ResBody, "custom_field_value": &it.CustomFieldValue,
	}
	for k, dst := range optFields {
		if s, ok := p[k].(string); ok {
			*dst = &s
		}
	}
	if n, ok := toInt(p["catid"]); ok {
		it.CatID = n
	}
	boolFields := map[string]*bool{
		"api_opened": &it.APIOpened, "req_body_is_json_schema": &it.ReqBodyIsJSONSchema,
		"res_body_is_json_schema": &it.ResBodyIsJSONSchema,
	}
	for k, dst := range boolFields {
		if v, ok := p[k]; ok {
			b, err := castBool(v)
			if err != nil {
				return err
			}
			*dst = b
		}
	}
	itemFields := []struct {
		key  string
		spec itemSpec
		dst  *string
	}{
		{"req_query", paramItem, &it.ReqQuery}, {"req_headers", paramItem, &it.ReqHeaders},
		{"req_params", pathItem, &it.ReqParams}, {"req_body_form", formItem, &it.ReqBodyForm},
	}
	for _, f := range itemFields {
		items, ok := p[f.key].([]any)
		if !ok {
			continue
		}
		s, err := normalizeItems(items, f.spec)
		if err != nil {
			return err
		}
		*f.dst = s
	}
	if tags, ok := p["tag"].([]any); ok {
		s, err := marshalCompact(tags)
		if err != nil {
			return err
		}
		it.Tag = s
	}
	return nil
}

// sameInterface 判断两份接口除 up_time 外是否完全相同，用于计算 nModified。
// MongoDB 在 up_time 变化时也算修改；YApi 的时间精确到秒，这里忽略它，使结果不随调用时机变化。
func sameInterface(a, b *store.Interface) bool {
	x, y := *a, *b
	x.UpTime, y.UpTime = 0, 0
	da, errA := marshalCompact(interfaceDoc(&x))
	db, errB := marshalCompact(interfaceDoc(&y))
	return errA == nil && errB == nil && da == db
}

// ---- 改动检测（对应 YApi common/diff-view.js：只有这些字段有变化时才写动态） ----

// interfaceChanged 判断修改前后在 YApi 改动日志关心的字段上是否有差异。
func interfaceChanged(old, cur *store.Interface) bool {
	if old.Path != cur.Path || old.Title != cur.Title || old.Method != cur.Method ||
		old.CatID != cur.CatID || old.Status != cur.Status || !jsonEqual(old.Tag, cur.Tag) {
		return true
	}
	if !paramsEqual(old.ReqParams, cur.ReqParams) || !paramsEqual(old.ReqQuery, cur.ReqQuery) ||
		!paramsEqual(old.ReqHeaders, cur.ReqHeaders) {
		return true
	}

	reqType := deref(cur.ReqBodyType)
	oldForm, oldOther := old.ReqBodyForm, old.ReqBodyOther
	if !samePtr(old.ReqBodyType, cur.ReqBodyType) {
		if deref(old.ReqBodyType) != reqType {
			return true
		}
		oldForm, oldOther = "[]", nil
	}
	switch reqType {
	case "json":
		if !json5Equal(oldOther, cur.ReqBodyOther) {
			return true
		}
	case "form":
		if !paramsEqual(oldForm, cur.ReqBodyForm) {
			return true
		}
	default:
		if deref(oldOther) != deref(cur.ReqBodyOther) {
			return true
		}
	}

	oldRes := old.ResBody
	if !samePtr(old.ResBodyType, cur.ResBodyType) {
		if deref(old.ResBodyType) != deref(cur.ResBodyType) {
			return true
		}
		empty := ""
		oldRes = &empty
	}
	if deref(cur.ResBodyType) == "json" {
		return !json5Equal(oldRes, cur.ResBody)
	}
	return deref(oldRes) != deref(cur.ResBody)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func samePtr(a, b *string) bool {
	return (a == nil) == (b == nil) && deref(a) == deref(b)
}

func jsonEqual(a, b string) bool {
	var x, y any
	return json.Unmarshal([]byte(a), &x) == nil && json.Unmarshal([]byte(b), &y) == nil && reflect.DeepEqual(x, y)
}

// diffValueNames 是 diff-view 比较参数前做的值替换；不在表中的值都变成 undefined。
var diffValueNames = map[string]string{"1": "必需", "0": "非必需", "text": "文本", "file": "文件"}

// paramsEqual 比较两个参数数组：忽略 _id，required 和 type 按 diff-view 的方式替换后再比较。
func paramsEqual(a, b string) bool {
	norm := func(s string) []map[string]any {
		var items []map[string]any
		_ = json.Unmarshal([]byte(s), &items)
		for _, m := range items {
			delete(m, "_id")
			for _, k := range []string{"required", "type"} {
				if v, ok := m[k]; ok {
					if name, known := diffValueNames[fmt.Sprint(v)]; known {
						m[k] = name
					} else {
						delete(m, k)
					}
				}
			}
		}
		return items
	}
	x, y := norm(a), norm(b)
	if len(x) == 0 && len(y) == 0 {
		return true
	}
	return reflect.DeepEqual(x, y)
}

// json5Equal 按 diff-view 的 json5_parse 比较两段请求/响应体：能解析的比较解析结果，不能解析的比较原文。
// 这里只按标准 JSON 解析，JSON5 特有写法会退回到原文比较，最多多写一条动态。
func json5Equal(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	parse := func(s string) any {
		var v any
		if json.Unmarshal([]byte(s), &v) != nil {
			return s
		}
		return v
	}
	return reflect.DeepEqual(parse(*a), parse(*b))
}
