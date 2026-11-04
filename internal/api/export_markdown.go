package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/PandaPanY/apiyard/internal/store"
)

// 本文件逐行对应 YApi 的 common/markdown.js 和 common/schema-transformTo-table.js，输出与原版逐字一致
// （由兼容场景 08-export 保证）。保留了原版的各种细节，例如 "<p key=N>" 的 N 会把值为 undefined 的字段也计算在内。

// exportCat 是导出时的一个分类及其接口（已按 YApi 的顺序排好）。
type exportCat struct {
	cat  *store.Cat
	list []*store.Interface
}

// ---- 有序记录：模拟 JS 对象（键有顺序，值可以是 undefined） ----

// undefined 表示 JS 中值为 undefined 但键存在的字段，它会占用 Object.keys 的位置。
type undefinedValue struct{}

var undefined = undefinedValue{}

type rec struct {
	keys []string
	vals map[string]any
}

func newRec(pairs ...any) *rec {
	r := &rec{vals: map[string]any{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		r.set(pairs[i].(string), pairs[i+1])
	}
	return r
}

func (r *rec) set(key string, v any) {
	if _, ok := r.vals[key]; !ok {
		r.keys = append(r.keys, key)
	}
	r.vals[key] = v
}

func (r *rec) get(key string) any {
	if v, ok := r.vals[key]; ok {
		return v
	}
	return undefined
}

func (r *rec) del(key string) {
	if _, ok := r.vals[key]; !ok {
		return
	}
	delete(r.vals, key)
	for i, k := range r.keys {
		if k == key {
			r.keys = append(r.keys[:i], r.keys[i+1:]...)
			break
		}
	}
}

// assign 对应 Object.assign(r, src)：已有的键原地覆盖，新键追加到末尾。
func (r *rec) assign(src *rec) {
	for _, k := range src.keys {
		r.set(k, src.vals[k])
	}
}

// jsField 读取 JSON 对象的字段，不存在时返回 undefined。
func jsField(o any, key string) any {
	if obj, ok := o.(*jsObject); ok {
		if v, ok := obj.get(key); ok {
			return v
		}
	}
	return undefined
}

func isUndefined(v any) bool { _, ok := v.(undefinedValue); return ok }

// truthy 对应 JS 的真值判断。
func truthy(v any) bool {
	switch x := v.(type) {
	case undefinedValue, nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case json.Number:
		f, err := x.Float64()
		return err == nil && f != 0
	default:
		return true
	}
}

// templateString 对应模板字符串中的 `${v}`。
func templateString(v any) string {
	if isUndefined(v) {
		return "undefined"
	}
	return jsString(v)
}

// ---- schema-transformTo-table.js ----

type schemaTable struct {
	arraySeq *atomic.Int64 // 原版的模块级计数器 fieldNum，进程内一直递增
}

// checkJSONSchema：没有 type 但有 properties 时补上 type: object（复制一份，不修改原对象）。
func checkJSONSchema(v any) *jsObject {
	src, _ := v.(*jsObject)
	out := &jsObject{vals: map[string]any{}}
	if arr, ok := v.([]any); ok { // Object.assign({}, 数组) 得到以下标为键的对象
		for i, item := range arr {
			k := strconv.Itoa(i)
			out.keys, out.vals[k] = append(out.keys, k), item
		}
		return out
	}
	if src != nil {
		out.keys = append(out.keys, src.keys...)
		for k, val := range src.vals {
			out.vals[k] = val
		}
	}
	props := jsField(out, "properties")
	if isUndefined(jsField(out, "type")) && isJSObjectLike(props) {
		out.keys, out.vals["type"] = append(out.keys, "type"), "object"
	}
	return out
}

func isJSObjectLike(v any) bool {
	switch v.(type) {
	case *jsObject, []any:
		return true
	}
	return false
}

func concatDesc(title, desc any) string {
	part := func(v any) string {
		if isUndefined(v) || v == nil {
			return ""
		}
		return jsString(v)
	}
	return strings.TrimSpace(part(title) + "\n" + part(desc))
}

// mockOf 对应 data.mock && data.mock.mock。
func mockOf(data *jsObject) any {
	m := jsField(data, "mock")
	if !truthy(m) {
		return m
	}
	return jsField(m, "mock")
}

func (t *schemaTable) mapping(data *jsObject, index string) any {
	typ, _ := jsField(data, "type").(string)
	desc := concatDesc(jsField(data, "title"), jsField(data, "description"))
	switch typ {
	case "string":
		return newRec("desc", desc, "default", jsField(data, "default"), "maxLength", jsField(data, "maxLength"),
			"minLength", jsField(data, "minLength"), "enum", jsField(data, "enum"), "enumDesc", jsField(data, "enumDesc"),
			"format", jsField(data, "format"), "mock", mockOf(data))
	case "number", "integer":
		return newRec("desc", desc, "maximum", jsField(data, "maximum"), "minimum", jsField(data, "minimum"),
			"default", jsField(data, "default"), "format", jsField(data, "format"), "enum", jsField(data, "enum"),
			"enumDesc", jsField(data, "enumDesc"), "mock", mockOf(data))
	case "array":
		return t.schemaArray(data, index)
	case "object":
		return t.schemaObject(data, index)
	case "boolean":
		return newRec("desc", desc, "default", jsField(data, "default"), "enum", jsField(data, "enum"), "mock", mockOf(data))
	default:
		return newRec("desc", desc, "default", jsField(data, "default"), "mock", mockOf(data))
	}
}

func (t *schemaTable) schemaArray(data *jsObject, index string) *rec {
	rawItems := jsField(data, "items")
	if !truthy(rawItems) {
		rawItems = &jsObject{keys: []string{"type"}, vals: map[string]any{"type": "string"}}
	}
	items := checkJSONSchema(rawItems)
	optionForm := t.mapping(items, index)
	children := any(optionForm)
	if r, ok := optionForm.(*rec); ok {
		r.set("key", "array-"+strconv.FormatInt(t.arraySeq.Add(1), 10))
		children = []*rec{r}
	}
	itemType := jsField(items, "type")
	item := newRec("desc", concatDesc(jsField(data, "title"), jsField(data, "description")),
		"default", jsField(data, "default"), "minItems", jsField(data, "minItems"),
		"uniqueItems", jsField(data, "uniqueItems"), "maxItems", jsField(data, "maxItems"),
		"itemType", itemType, "children", children)
	if itemType == "string" {
		item.set("itemFormat", jsField(items, "format"))
	}
	return item
}

func (t *schemaTable) schemaObject(data *jsObject, key string) []*rec {
	props, _ := jsField(data, "properties").(*jsObject)
	required := jsField(data, "required")
	result := []*rec{}
	if props == nil {
		return result
	}
	for index, name := range props.orderedKeys() {
		value := props.vals[name]
		copied := checkJSONSchema(value)
		childKey := key + "-" + strconv.Itoa(index)
		optionForm := t.schema(copied, childKey)
		item := newRec("name", name, "key", childKey,
			"desc", concatDesc(jsField(copied, "title"), jsField(copied, "description")),
			"required", containsName(required, name))
		valueType := jsField(value, "type")
		children, isList := optionForm.([]*rec)
		if valueType == "object" || (isUndefined(valueType) && isList) {
			item.set("type", "object")
			item.set("children", children)
		} else if r, ok := optionForm.(*rec); ok {
			item.assign(r)
		}
		result = append(result, item)
	}
	return result
}

// containsName 对应 required.indexOf(name) != -1（required 是字符串时 JS 按子串查找）。
func containsName(required any, name string) bool {
	switch r := required.(type) {
	case []any:
		for _, v := range r {
			if v == name {
				return true
			}
		}
	case string:
		return strings.Contains(r, name)
	}
	return false
}

// schema 对应 Schema(data, key)：非对象类型包一层 {type, key, desc, default, sub, children}。
func (t *schemaTable) schema(data *jsObject, key string) any {
	result := t.mapping(data, key)
	if jsField(data, "type") == "object" {
		return result
	}
	r, ok := result.(*rec)
	if !ok { // 只有 object 类型会得到数组
		return result
	}
	desc, def, children := r.get("desc"), r.get("default"), r.get("children")
	r.del("desc")
	r.del("default")
	r.del("children")
	item := newRec("type", jsField(data, "type"), "key", key, "desc", desc, "default", def, "sub", r)
	if list, ok := children.([]*rec); ok {
		item.set("children", list)
	}
	return item
}

// transform 对应 schemaTransformToTable：根节点的 key 是数字 0。
func (t *schemaTable) transform(schema any) ([]*rec, error) {
	if schema == nil {
		return nil, errors.New("schema 为 null") // 原版这里会抛异常，导致整个导出失败
	}
	root := checkJSONSchema(schema)
	switch v := t.schema(root, "0").(type) {
	case []*rec:
		return v, nil
	case *rec:
		return []*rec{v}, nil
	}
	return []*rec{}, nil
}

// ---- markdown.js ----

var schemaSubNames = map[string]string{
	"desc": "备注", "default": "实例", "maximum": "最大值", "minimum": "最小值", "maxItems": "最大数量",
	"minItems": "最小数量", "maxLength": "最大长度", "minLength": "最小长度", "uniqueItems": "元素是否都不同",
	"itemType": "item 类型", "format": "format", "enum": "枚举", "enumDesc": "枚举备注", "mock": "mock",
}

var schemaColumns = [][2]string{{"name", "名称"}, {"type", "类型"}, {"required", "是否必须"},
	{"default", "默认值"}, {"desc", "备注"}, {"sub", "其他信息"}}

func handleSubObject(sub any) string {
	r, ok := sub.(*rec)
	if !ok {
		return ""
	}
	var b strings.Builder
	for index, key := range r.keys {
		v := r.vals[key]
		if isUndefined(v) {
			continue
		}
		name, known := schemaSubNames[key]
		if !known {
			name = "undefined"
		}
		fmt.Fprintf(&b, `<p key=%d><span style="font-weight: '700'">%s: </span><span>%s</span></p>`, index, name, jsString(v))
	}
	return b.String()
}

func tableCol(col *rec, level int) string {
	var b strings.Builder
	for index, c := range schemaColumns {
		value := col.get(c[0])
		if isUndefined(value) {
			value = ""
		}
		var text string
		switch c[0] {
		case "sub":
			text = handleSubObject(value)
		case "type":
			if value == "array" {
				itemType := "array"
				if sub, ok := col.get("sub").(*rec); ok {
					itemType = ""
					if v := sub.get("itemType"); truthy(v) {
						itemType = jsString(v)
					}
				}
				text = "<span>" + itemType + " []</span>"
			} else {
				text = "<span>" + jsString(value) + "</span>"
			}
		case "required":
			text = "非必须"
			if truthy(value) {
				text = "必须"
			}
		case "desc":
			text = `<span style="white-space: pre-wrap">` + jsString(value) + "</span>"
		case "name":
			branch := ""
			if level > 0 {
				branch = "├─"
			}
			text = fmt.Sprintf(`<span style="padding-left: %dpx"><span style="color: #8c8a8a">%s</span> %s</span>`,
				20*level, branch, jsString(value))
		default:
			text = jsString(value)
		}
		fmt.Fprintf(&b, "<td key=%d>%s</td>", index, text)
	}
	return b.String()
}

func tableBody(rows []*rec, level int) string {
	var b strings.Builder
	for _, col := range rows {
		b.WriteString("<tr key=" + templateString(col.get("key")) + ">" + tableCol(col, level) + "</tr>")
		if children, ok := col.get("children").([]*rec); ok {
			b.WriteString(tableBody(children, level+1))
		}
	}
	return b.String()
}

// schemaTableHTML 对应 createSchemaTable：body 不是合法 JSON 时按空对象处理。
func (t *schemaTable) schemaTableHTML(body string) (string, error) {
	parsed, err := parseJSOrdered(body)
	if err != nil {
		parsed = &jsObject{vals: map[string]any{}}
	}
	rows, err := t.transform(parsed)
	if err != nil {
		return "", err
	}
	var header strings.Builder
	for _, c := range schemaColumns {
		fmt.Fprintf(&header, "<th key=%s>%s</th>", c[0], c[1])
	}
	return "<table>\n  <thead class=\"ant-table-thead\">\n    <tr>\n      " + header.String() +
		"\n    </tr>\n  </thead>" + "<tbody className=\"ant-table-tbody\">" + tableBody(rows, 0) +
		"\n               </tbody>\n              </table>\n            ", nil
}

// paramItems 解析接口里以 JSON 保存的参数数组。
func paramItems(s string) []map[string]any {
	var items []map[string]any
	_ = json.Unmarshal([]byte(s), &items)
	return items
}

// itemText 对应 `${x || ”}`。
func itemText(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

// wrapText 对应 handleWrap(x) || ”：换行换成 <br/>。
func wrapText(m map[string]any, key string) string {
	return strings.ReplaceAll(itemText(m, key), "\n", "<br/>")
}

func requiredText(m map[string]any) string {
	if m["required"] == "1" {
		return "是"
	}
	return "否"
}

func mdHeaders(items []map[string]any) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("**Headers**\n\n| 参数名称  | 参数值  |  是否必须 | 示例  | 备注  |\n| ------------ | ------------ | ------------ | ------------ | ------------ |\n")
	for _, h := range items {
		fmt.Fprintf(&b, "| %s  |  %s | %s  |  %s |  %s |\n", itemText(h, "name"), itemText(h, "value"),
			requiredText(h), wrapText(h, "example"), wrapText(h, "desc"))
	}
	return b.String()
}

func mdPathParams(items []map[string]any) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("**路径参数**\n\n| 参数名称 | 示例  | 备注  |\n| ------------ | ------------ | ------------ |\n")
	for _, p := range items {
		fmt.Fprintf(&b, "| %s |  %s |  %s |\n", itemText(p, "name"), wrapText(p, "example"), wrapText(p, "desc"))
	}
	return b.String()
}

func mdQuery(items []map[string]any) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("**Query**\n\n| 参数名称  |  是否必须 | 示例  | 备注  |\n| ------------ | ------------ | ------------ | ------------ |\n")
	for _, q := range items {
		fmt.Fprintf(&b, "| %s | %s  |  %s |  %s |\n", itemText(q, "name"), requiredText(q), wrapText(q, "example"), wrapText(q, "desc"))
	}
	return b.String()
}

func (t *schemaTable) mdReqBody(it *store.Interface) (string, error) {
	form := paramItems(it.ReqBodyForm)
	if deref(it.ReqBodyType) == "form" && len(form) > 0 {
		var b strings.Builder
		b.WriteString("**Body**\n\n| 参数名称  | 参数类型  |  是否必须 | 示例  | 备注  |\n| ------------ | ------------ | ------------ | ------------ | ------------ |\n")
		for _, f := range form {
			fmt.Fprintf(&b, "| %s | %s  |  %s |  %s  |  %s |\n", itemText(f, "name"), itemText(f, "type"),
				requiredText(f), itemText(f, "example"), itemText(f, "desc"))
		}
		return b.String() + "\n\n", nil
	}
	other := deref(it.ReqBodyOther)
	if other == "" {
		return "", nil
	}
	if it.ReqBodyIsJSONSchema {
		table, err := t.schemaTableHTML(other)
		return "**Body**\n\n" + table, err
	}
	return "**Body**\n\n```javascript\n" + other + "\n```", nil
}

func (t *schemaTable) mdResponse(it *store.Interface) (string, error) {
	body := deref(it.ResBody)
	if body == "" {
		return "", nil
	}
	if it.ResBodyIsJSONSchema && deref(it.ResBodyType) == "json" {
		table, err := t.schemaTableHTML(body)
		return "\n### 返回数据\n\n" + table, err
	}
	return "\n### 返回数据\n\n```javascript\n" + body + "\n```", nil
}

// interfaceMarkdown 对应 createInterMarkdown（isToc 为 false）。
func (t *schemaTable) interfaceMarkdown(basepath string, it *store.Interface) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "\n## %s\n<a id=%s%d> </a>\n", it.Title, it.Title, it.CatID)
	fmt.Fprintf(&b, "### 基本信息\n\n**Path：** %s\n\n**Method：** %s\n\n**接口描述：**\n%s\n",
		basepath+it.Path, it.Method, deref(it.Desc))
	b.WriteString("\n### 请求参数\n")
	b.WriteString(mdHeaders(paramItems(it.ReqHeaders)))
	b.WriteString(mdPathParams(paramItems(it.ReqParams)))
	b.WriteString(mdQuery(paramItems(it.ReqQuery)))
	body, err := t.mdReqBody(it)
	if err != nil {
		return "", err
	}
	b.WriteString(body)
	res, err := t.mdResponse(it)
	if err != nil {
		return "", err
	}
	b.WriteString(res)
	return b.String(), nil
}

// projectMarkdown 生成整个项目的 Markdown（createProjectMarkdown + createClassMarkdown）。
func (t *schemaTable) projectMarkdown(p *store.Project, cats []exportCat) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "\n <h1 class=\"curproject-name\"> %s </h1> \n %s\n\n", p.Name, deref(p.Desc))
	basepath := "undefined" // 与 `${undefined + path}` 一致；正常项目都有 basepath
	if p.Basepath != nil {
		basepath = *p.Basepath
	}
	for _, c := range cats {
		fmt.Fprintf(&b, "\n# %s\n", c.cat.Name)
		for _, it := range c.list {
			md, err := t.interfaceMarkdown(basepath, it)
			if err != nil {
				return "", err
			}
			b.WriteString(md)
		}
	}
	return b.String(), nil
}
