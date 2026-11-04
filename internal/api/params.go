package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// maxBodySize 限制请求体大小，与 YApi 的 jsonLimit（2MB）一致。
const maxBodySize = 2 << 20

// readParams 合并查询参数和请求体，请求体中的同名字段优先，与 YApi 的 Object.assign(query, body) 一致。
// 同名查询参数出现多次时与 Koa 一样得到数组。body 是解析后的请求体本身（对象、数组或表单字段），
// 供只读请求体的接口使用（如 up_index 的数组、add_cat 只认请求体里的字段）。
func readParams(r *http.Request) (params map[string]any, body any, err error) {
	params = map[string]any{}
	for k, v := range r.URL.Query() {
		if len(v) == 1 {
			params[k] = v[0]
			continue
		}
		list := make([]any, len(v))
		for i, s := range v {
			list[i] = s
		}
		params[k] = list
	}
	if r.Body == nil {
		return params, nil, nil
	}

	data, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return params, nil, err
	}
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch mediaType {
	case "application/x-www-form-urlencoded":
		r.Body = io.NopCloser(bytes.NewReader(data))
		if err := r.ParseForm(); err != nil {
			return nil, nil, err
		}
		form := map[string]any{}
		for k, v := range r.PostForm {
			params[k] = v[0]
			form[k] = v[0]
		}
		return params, form, nil
	default:
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := dec.Decode(&body); err != nil {
			return nil, nil, err
		}
		if obj, ok := body.(map[string]any); ok {
			for k, v := range obj {
				params[k] = v
			}
		}
		return params, body, nil
	}
}

// toInt 按 YApi（ajv coerceTypes）的规则把参数转换为整数：数字或数字字符串都可以。
func toInt(v any) (int64, bool) {
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n, true
		}
		f, err := x.Float64()
		if err != nil || f != math.Trunc(f) {
			return 0, false
		}
		return int64(f), true
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n, err == nil
	case float64:
		return int64(x), x == math.Trunc(x)
	case int64:
		return x, true
	case int:
		return int64(x), true
	default:
		return 0, false
	}
}

// int 读取整数参数。
func (c *call) int(key string) (int64, bool) {
	return toInt(c.params[key])
}

// str 读取字符串参数；数字会转换为字符串（YApi 的 handleParams 也是这样处理的）。
func (c *call) str(key string) (string, bool) {
	switch x := c.params[key].(type) {
	case string:
		return x, true
	case json.Number:
		return x.String(), true
	default:
		return "", false
	}
}

// trimmed 读取字符串参数并去掉首尾空白，与 YApi 的 handleParams(params, {key: 'string'}) 一致。
func (c *call) trimmed(key string) string {
	s, _ := c.str(key)
	return strings.TrimSpace(s)
}

// field 是一条参数校验规则，对应 YApi schemaMap 中的一个字段。
type field struct {
	name     string
	kind     string // number / string / boolean / array / object
	required bool
	min      int      // 字符串最小长度或数组最少项数
	enum     []string // 字符串的可选值
}

// validate 按规则校验并转换参数，返回只保留规则内字段的新参数表（YApi 用 ajv removeAdditional 去掉多余字段）。
// 校验失败时返回的提示文字模仿 ajv-i18n 的中文格式。
func validate(params map[string]any, fields []field) (map[string]any, string) {
	out := map[string]any{}
	var errs []string
	for _, f := range fields {
		v, ok := params[f.name]
		if !ok || v == nil {
			if f.required {
				errs = append(errs, "data 应当有必需属性 "+f.name)
			}
			continue
		}
		converted, msg := checkField(f, v)
		if msg != "" {
			errs = append(errs, "data."+f.name+" "+msg)
			continue
		}
		out[f.name] = converted
	}
	if len(errs) > 0 {
		return nil, "请求参数 " + strings.Join(errs, "\n")
	}
	return out, ""
}

func checkField(f field, v any) (any, string) {
	switch f.kind {
	case "number":
		n, ok := toInt(v)
		if !ok {
			return nil, "应当是 number 类型"
		}
		return n, ""
	case "string":
		// ajv 的 coerceTypes 会把数字和布尔转为字符串
		var s string
		switch x := v.(type) {
		case string:
			s = x
		case json.Number:
			s = x.String()
		case bool:
			s = strconv.FormatBool(x)
		default:
			return nil, "应当是 string 类型"
		}
		if len([]rune(s)) < f.min {
			return nil, fmt.Sprintf("不应少于 %d 个字符", f.min)
		}
		if f.enum != nil && !slices.Contains(f.enum, s) {
			return nil, "应当是预设定的枚举值之一"
		}
		return s, ""
	case "array":
		items, ok := v.([]any)
		if !ok {
			return nil, "应当是 array 类型"
		}
		if len(items) < f.min {
			return nil, fmt.Sprintf("不应少于 %d 个项", f.min)
		}
		return items, ""
	case "boolean":
		// ajv 的 coerceTypes 接受 "true"/"false"、1/0
		switch x := v.(type) {
		case bool:
			return x, ""
		case string:
			if x == "true" || x == "false" {
				return x == "true", ""
			}
		case json.Number:
			if x == "1" || x == "0" {
				return x == "1", ""
			}
		}
		return nil, "应当是 boolean 类型"
	case "object":
		if _, ok := v.(map[string]any); !ok {
			return nil, "应当是 object 类型"
		}
		return v, ""
	default:
		return v, ""
	}
}

// intList 把数组参数中的每一项转换为整数，无法转换的项被忽略。
func intList(v any) []int64 {
	items, _ := v.([]any)
	out := make([]int64, 0, len(items))
	for _, item := range items {
		if n, ok := toInt(item); ok {
			out = append(out, n)
		}
	}
	return out
}
