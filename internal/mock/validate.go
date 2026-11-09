package mock

import (
	"encoding/json"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/PandaPanY/apiyard/internal/jsjson"
	"github.com/PandaPanY/apiyard/internal/store"
)

// 各请求方法是否有请求体，与 YApi 前端的 variable.HTTP_METHOD 相同。
var methodHasBody = map[string]bool{"GET": false, "POST": true, "PUT": true, "DELETE": true, "HEAD": false, "OPTIONS": false, "PATCH": true}

// validate 对应原版的 mockValidator（项目开启了“严格模式”时）：检查必填的查询参数和表单字段，
// 请求体是 JSON Schema 时按 schema 校验。返回 false 时 msg 是原版的错误信息。
func validate(it *store.Interface, req *request) (ok bool, msg string, err error) {
	method := strings.ToUpper(it.Method)
	if method == "" {
		method = "GET"
	}
	hasBody, known := methodHasBody[method]
	if !known { // 原版在这里抛出 TypeError，Mock 返回 409
		return false, "", jsError("Cannot read property 'request_body' of undefined")
	}

	var missing []string
	for _, q := range requiredItems(it.ReqQuery) {
		if v, _ := req.query.Get(q); !jsTruthy(v) {
			missing = append(missing, q)
		}
	}
	if hasBody && deref(it.ReqBodyType) == "form" {
		body, _ := req.body.(*jsjson.Object)
		for _, name := range requiredItems(it.ReqBodyForm) {
			if !hasField(body, name) {
				missing = append(missing, name)
			}
		}
	}
	schemaInvalid, schemaMsg := false, ""
	if hasBody && deref(it.ReqBodyType) == "json" && it.ReqBodyIsJSONSchema {
		var schema any = undefinedSchema
		if it.ReqBodyOther != nil {
			schema = parseJSON5(*it.ReqBodyOther)
		}
		data := req.body
		if s, isStr := data.(string); isStr {
			data = parseJSON5(s)
		}
		valid, m := schemaValidator(schema, data)
		schemaInvalid, schemaMsg = !valid, m
	}
	if len(missing) == 0 && !schemaInvalid {
		return true, "", nil
	}
	msg = "错误信息："
	if len(missing) > 0 {
		msg += "缺少必须字段 " + strings.Join(missing, ",") + "  "
	}
	if schemaInvalid {
		msg += "schema 验证请求参数 " + schemaMsg
	}
	return false, msg, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// requiredItems 返回参数列表（req_query、req_body_form 的 JSON）里 required 为 "1" 的参数名。
func requiredItems(list string) []string {
	var items []map[string]any
	_ = json.Unmarshal([]byte(list), &items)
	var out []string
	for _, item := range items {
		if item != nil && item["required"] == "1" {
			name, ok := item["name"].(string)
			if !ok {
				name = "undefined"
			}
			out = append(out, name)
		}
	}
	return out
}

// hasField 对应 body[name] || body.fields[name] || body.files[name]。
func hasField(body *jsjson.Object, name string) bool {
	if body == nil {
		return false
	}
	if v, _ := body.Get(name); jsTruthy(v) {
		return true
	}
	for _, k := range []string{"fields", "files"} {
		if sub, ok := body.Get(k); ok {
			if o, ok := sub.(*jsjson.Object); ok {
				if v, _ := o.Get(name); jsTruthy(v) {
					return true
				}
			}
		}
	}
	return false
}

// jsTruthy 对应 JS 的布尔转换。
func jsTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case json.Number:
		f, err := x.Float64()
		return err == nil && f != 0
	case float64:
		return x != 0 && !math.IsNaN(x)
	}
	return true
}

// ---- JSON Schema 校验（ajv 5 + ajv-i18n 的中文信息）----

type undefinedType struct{}

var undefinedSchema = undefinedType{}

var schemaTypes = map[string]bool{"array": true, "boolean": true, "integer": true, "null": true, "number": true, "object": true, "string": true}

// schemaValidator 对应 YApi common/utils.js 的 schemaValidator：draft-04，遇到第一个错误就停止（ajv 默认的 allErrors: false），
// 不校验 format。返回的信息与 ajv.errorsText 相同，例如 "data.name 应当是 string 类型"。
func schemaValidator(schema, data any) (bool, string) {
	switch s := schema.(type) {
	case undefinedType, nil:
		return true, "" // 原版用一个空对象 schema 代替
	case string:
		if s == "" {
			return true, ""
		}
		return false, "schema should be object or boolean"
	case *jsjson.Object:
		if msg := metaCheck(s, "data"); msg != "" {
			return false, msg
		}
		v := &schemaCheck{root: s}
		if v.check(s, data, "") {
			return true, ""
		}
		return false, strings.Join(v.errs, "\n")
	}
	return false, "schema should be object or boolean"
}

// metaCheck 只检查最常见的 schema 错误：type 写成了不存在的类型。原版这时报 schema is invalid。
func metaCheck(s *jsjson.Object, path string) string {
	if t, ok := s.Get("type"); ok {
		valid := false
		switch x := t.(type) {
		case string:
			valid = schemaTypes[x]
		case []any:
			valid = true
			for _, item := range x {
				if name, ok := item.(string); !ok || !schemaTypes[name] {
					valid = false
				}
			}
		}
		if !valid {
			p := path + ".type"
			return "schema is invalid: " + p + " should be equal to one of the allowed values, " + p + " should be array, " + p + " should match some schema in anyOf"
		}
	}
	if props, ok := s.Get("properties"); ok {
		if po, ok := props.(*jsjson.Object); ok {
			for _, k := range po.Keys() {
				sub, _ := po.Get(k)
				if so, ok := sub.(*jsjson.Object); ok {
					if msg := metaCheck(so, path+".properties"+propertyPath(k)); msg != "" {
						return msg
					}
				}
			}
		}
	}
	return ""
}

type schemaCheck struct {
	root *jsjson.Object
	errs []string
}

func (v *schemaCheck) fail(path, msg string) bool {
	v.errs = append(v.errs, "data"+path+" "+msg)
	return false
}

var reIdentifier = regexp.MustCompile(`(?i)^[a-z$_][a-z$_0-9]*$`)

// propertyPath 对应 ajv 的 getProperty：标识符写成 .name，其他写成 ['a-b']。
func propertyPath(key string) string {
	if reIdentifier.MatchString(key) {
		return "." + key
	}
	return "['" + escapeQuotes(key) + "']"
}

// ruleKeywords 是 ajv 认为“有规则”的关键字；只有 title、description 这类关键字的 schema 视为没有规则。
var ruleKeywords = []string{"type", "$ref", "enum", "const", "not", "anyOf", "oneOf", "allOf", "maximum", "minimum",
	"exclusiveMaximum", "exclusiveMinimum", "multipleOf", "maxLength", "minLength", "pattern", "format", "maxItems",
	"minItems", "uniqueItems", "contains", "items", "additionalItems", "maxProperties", "minProperties", "required",
	"dependencies", "propertyNames", "properties", "additionalProperties", "patternProperties"}

func hasRules(s any) bool {
	o, ok := s.(*jsjson.Object)
	if !ok {
		return false
	}
	for _, k := range ruleKeywords {
		if _, ok := o.Get(k); ok {
			return true
		}
	}
	return false
}

func get(s *jsjson.Object, key string) (any, bool) { return s.Get(key) }

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case float64:
		return x, true
	}
	return 0, false
}

func dataType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case json.Number, float64:
		return "number"
	case []any:
		return "array"
	case *jsjson.Object:
		return "object"
	}
	return "undefined"
}

func matchesType(v any, t string) bool {
	dt := dataType(v)
	switch t {
	case "integer":
		f, ok := number(v)
		return ok && f == math.Trunc(f) && !math.IsInf(f, 0)
	case "number":
		return dt == "number"
	}
	return dt == t
}

func fmtNum(v any) string {
	if f, ok := number(v); ok {
		return jsjson.FormatNumber(f)
	}
	return jsjson.String(v)
}

// check 按 ajv 的顺序校验：$ref（忽略同级的其他关键字）→ type → 数值 → 字符串 → 数组 → 对象 → enum/not/anyOf/oneOf/allOf。
func (v *schemaCheck) check(schema any, data any, path string) bool {
	s, ok := schema.(*jsjson.Object)
	if !ok {
		return true
	}
	if ref, ok := get(s, "$ref"); ok {
		target := v.resolve(jsjson.String(ref))
		if target == nil {
			return true
		}
		return v.check(target, data, path)
	}
	if t, ok := get(s, "type"); ok {
		var types []string
		switch x := t.(type) {
		case string:
			types = []string{x}
		case []any:
			for _, item := range x {
				types = append(types, jsjson.String(item))
			}
		}
		matched := false
		for _, name := range types {
			if matchesType(data, name) {
				matched = true
			}
		}
		if !matched {
			return v.fail(path, "应当是 "+strings.Join(types, ",")+" 类型")
		}
	}
	switch dataType(data) {
	case "number":
		if !v.checkNumber(s, data, path) {
			return false
		}
	case "string":
		if !v.checkString(s, data.(string), path) {
			return false
		}
	case "array":
		if !v.checkArray(s, data.([]any), path) {
			return false
		}
	case "object":
		if !v.checkObject(s, data.(*jsjson.Object), path) {
			return false
		}
	}
	return v.checkGeneric(s, data, path)
}

func (v *schemaCheck) resolve(ref string) *jsjson.Object {
	if ref == "#" {
		return v.root
	}
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	var cur any = v.root
	for _, part := range strings.Split(ref[2:], "/") {
		part = strings.NewReplacer("~1", "/", "~0", "~").Replace(part)
		o, ok := cur.(*jsjson.Object)
		if !ok {
			return nil
		}
		if cur, ok = o.Get(part); !ok {
			return nil
		}
	}
	o, _ := cur.(*jsjson.Object)
	return o
}

func (v *schemaCheck) checkNumber(s *jsjson.Object, data any, path string) bool {
	f, _ := number(data)
	limit := func(key, exclusiveKey, lt, le string, ok func(f, l float64, exclusive bool) bool) bool {
		l, has := get(s, key)
		if !has {
			return true
		}
		lf, isNum := number(l)
		if !isNum {
			return true
		}
		ex, _ := get(s, exclusiveKey)
		exclusive := ex == true
		if ok(f, lf, exclusive) {
			return true
		}
		cmp := le
		if exclusive {
			cmp = lt
		}
		return v.fail(path, "应当为 "+cmp+" "+fmtNum(l))
	}
	if !limit("maximum", "exclusiveMaximum", "<", "<=", func(f, l float64, ex bool) bool { return f < l || (!ex && f == l) }) {
		return false
	}
	if !limit("minimum", "exclusiveMinimum", ">", ">=", func(f, l float64, ex bool) bool { return f > l || (!ex && f == l) }) {
		return false
	}
	if m, ok := get(s, "multipleOf"); ok {
		if mf, isNum := number(m); isNum && mf != 0 {
			if div := f / mf; div != math.Trunc(div) {
				return v.fail(path, "应当是 "+fmtNum(m)+" 的整数倍")
			}
		}
	}
	return true
}

func intLimit(s *jsjson.Object, key string) (float64, bool) {
	l, ok := get(s, key)
	if !ok {
		return 0, false
	}
	return number(l)
}

func (v *schemaCheck) checkString(s *jsjson.Object, data, path string) bool {
	n := float64(utf8.RuneCountInString(data))
	if l, ok := intLimit(s, "maxLength"); ok && n > l {
		return v.fail(path, "不应多于 "+jsjson.FormatNumber(l)+" 个字符")
	}
	if l, ok := intLimit(s, "minLength"); ok && n < l {
		return v.fail(path, "不应少于 "+jsjson.FormatNumber(l)+" 个字符")
	}
	if p, ok := get(s, "pattern"); ok {
		if re, err := regexp.Compile(jsjson.String(p)); err == nil && !re.MatchString(data) {
			return v.fail(path, `应当匹配模式 "`+jsjson.String(p)+`"`)
		}
	}
	return true
}

func (v *schemaCheck) checkArray(s *jsjson.Object, data []any, path string) bool {
	n := float64(len(data))
	if l, ok := intLimit(s, "maxItems"); ok && n > l {
		return v.fail(path, "不应多于 "+jsjson.FormatNumber(l)+" 个项")
	}
	if l, ok := intLimit(s, "minItems"); ok && n < l {
		return v.fail(path, "不应少于 "+jsjson.FormatNumber(l)+" 个项")
	}
	if u, _ := get(s, "uniqueItems"); u == true {
		for i := len(data) - 1; i > 0; i-- {
			for j := i - 1; j >= 0; j-- {
				if deepEqual(data[i], data[j]) {
					return v.fail(path, "不应当含有重复项 (第 "+strconv.Itoa(j)+" 项与第 "+strconv.Itoa(i)+" 项是重复的)")
				}
			}
		}
	}
	items, ok := get(s, "items")
	if !ok {
		return true
	}
	if list, isList := items.([]any); isList {
		if add, _ := get(s, "additionalItems"); add == false && len(data) > len(list) {
			return v.fail(path, "不允许超过"+strconv.Itoa(len(list))+"个元素")
		}
		for i := 0; i < len(list) && i < len(data); i++ {
			if !v.check(list[i], data[i], path+"["+strconv.Itoa(i)+"]") {
				return false
			}
		}
		if add, ok := get(s, "additionalItems"); ok && add != false {
			for i := len(list); i < len(data); i++ {
				if !v.check(add, data[i], path+"["+strconv.Itoa(i)+"]") {
					return false
				}
			}
		}
		return true
	}
	for i, item := range data {
		if !v.check(items, item, path+"["+strconv.Itoa(i)+"]") {
			return false
		}
	}
	return true
}

func (v *schemaCheck) checkObject(s *jsjson.Object, data *jsjson.Object, path string) bool {
	n := float64(data.Len())
	if l, ok := intLimit(s, "maxProperties"); ok && n > l {
		return v.fail(path, "不应有多于 "+jsjson.FormatNumber(l)+" 个属性")
	}
	if l, ok := intLimit(s, "minProperties"); ok && n < l {
		return v.fail(path, "不应有少于 "+jsjson.FormatNumber(l)+" 个属性")
	}
	props, _ := get(s, "properties")
	propObj, _ := props.(*jsjson.Object)
	var required []string
	if r, ok := get(s, "required"); ok {
		if list, ok := r.([]any); ok {
			for _, item := range list {
				required = append(required, jsjson.String(item))
			}
		}
	}
	// 在 properties 里有规则的必需属性，留到校验 properties 时再检查（ajv 5 的做法），其余的在这里按顺序检查
	for _, name := range required {
		var ps any
		if propObj != nil {
			ps, _ = propObj.Get(name)
		}
		if propObj != nil && hasRules(ps) {
			continue
		}
		if _, ok := data.Get(name); !ok {
			return v.fail(path, "应当有必需属性 "+propertyPath(name))
		}
	}

	// 额外的属性
	add, hasAdd := get(s, "additionalProperties")
	patterns, _ := get(s, "patternProperties")
	patObj, _ := patterns.(*jsjson.Object)
	if (hasAdd && add != true) || patObj != nil {
		for _, key := range data.Keys() {
			if propObj != nil {
				if _, ok := propObj.Get(key); ok {
					continue
				}
			}
			if matchesPattern(patObj, key) {
				continue
			}
			if !hasAdd || add == true {
				continue
			}
			if add == false {
				return v.fail(path, "不允许有额外的属性")
			}
			val, _ := data.Get(key)
			if !v.check(add, val, path+"['"+key+"']") {
				return false
			}
		}
	}
	if propObj != nil {
		for _, name := range propObj.Keys() {
			ps, _ := propObj.Get(name)
			if !hasRules(ps) {
				continue
			}
			val, present := data.Get(name)
			if !present {
				for _, r := range required {
					if r == name { // 这里报的是属性名本身，不像 required 关键字那样写成 .name
						return v.fail(path, "应当有必需属性 "+escapeQuotes(name))
					}
				}
				continue
			}
			if !v.check(ps, val, path+propertyPath(name)) {
				return false
			}
		}
	}
	if patObj != nil {
		for _, pattern := range patObj.Keys() {
			re, err := regexp.Compile(pattern)
			if err != nil {
				continue
			}
			ps, _ := patObj.Get(pattern)
			for _, key := range data.Keys() {
				if re.MatchString(key) {
					val, _ := data.Get(key)
					if !v.check(ps, val, path+"['"+key+"']") {
						return false
					}
				}
			}
		}
	}
	return true
}

func matchesPattern(patterns *jsjson.Object, key string) bool {
	if patterns == nil {
		return false
	}
	for _, p := range patterns.Keys() {
		if re, err := regexp.Compile(p); err == nil && re.MatchString(key) {
			return true
		}
	}
	return false
}

func (v *schemaCheck) checkGeneric(s *jsjson.Object, data any, path string) bool {
	if c, ok := get(s, "const"); ok && !deepEqual(c, data) {
		return v.fail(path, "应当等于常量")
	}
	if e, ok := get(s, "enum"); ok {
		if list, ok := e.([]any); ok {
			found := false
			for _, item := range list {
				if deepEqual(item, data) {
					found = true
					break
				}
			}
			if !found {
				return v.fail(path, "应当是预设定的枚举值之一")
			}
		}
	}
	if n, ok := get(s, "not"); ok {
		sub := &schemaCheck{root: v.root}
		if sub.check(n, data, path) {
			return v.fail(path, `不应当匹配 "not" schema`)
		}
	}
	// anyOf、oneOf：每个不通过的分支都会留下它的第一个错误，最后再加上 anyOf / oneOf 本身的错误
	if list, ok := schemaList(s, "anyOf"); ok && v.passing(list, data, path) == 0 {
		return v.fail(path, "数据应为 anyOf 所指定的其中一个")
	}
	if list, ok := schemaList(s, "oneOf"); ok && v.passing(list, data, path) != 1 {
		return v.fail(path, `只能匹配一个 "oneOf" 中的 schema`)
	}
	if a, ok := get(s, "allOf"); ok {
		if list, ok := a.([]any); ok {
			for _, sub := range list {
				if !v.check(sub, data, path) {
					return false
				}
			}
		}
	}
	return true
}

func schemaList(s *jsjson.Object, key string) ([]any, bool) {
	v, ok := get(s, key)
	if !ok {
		return nil, false
	}
	list, ok := v.([]any)
	return list, ok
}

// passing 返回 list 里有几个 schema 通过校验。只要有一个通过，就去掉不通过的分支留下的错误。
func (v *schemaCheck) passing(list []any, data any, path string) int {
	before := len(v.errs)
	n := 0
	for _, sub := range list {
		if v.check(sub, data, path) {
			n++
		}
	}
	if n > 0 {
		v.errs = v.errs[:before]
	}
	return n
}

// deepEqual 按 JSON 的值比较（数字按数值比较，对象不看键的顺序）。
func deepEqual(a, b any) bool {
	if fa, ok := number(a); ok {
		fb, ok := number(b)
		return ok && fa == fb
	}
	switch x := a.(type) {
	case *jsjson.Object:
		y, ok := b.(*jsjson.Object)
		if !ok || x.Len() != y.Len() {
			return false
		}
		for _, k := range x.Keys() {
			va, _ := x.Get(k)
			vb, ok := y.Get(k)
			if !ok || !deepEqual(va, vb) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !deepEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}

// jsError 是与原版 JS 异常信息相同的错误，会原样出现在响应里。
type jsError string

func (e jsError) Error() string { return string(e) }

// escapeQuotes 对应 ajv 的 escapeQuotes：转义反斜杠和单引号（\x5c 是反斜杠）。
func escapeQuotes(s string) string {
	return quoteEscaper.Replace(s)
}

var quoteEscaper = strings.NewReplacer("\x5c", "\x5c\x5c", "'", "\x5c'")
