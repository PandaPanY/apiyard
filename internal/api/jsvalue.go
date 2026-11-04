package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
)

// 导出 Markdown 时要和 YApi（JavaScript）逐字一致，所以这里按 JS 的规则解析和格式化 JSON：
// 对象保持键的顺序（JSON.parse 后 Object.keys 的顺序），值转字符串时与模板字符串 `${v}` 相同。

// jsObject 是保持键顺序的 JSON 对象。
type jsObject struct {
	keys []string
	vals map[string]any
}

func (o *jsObject) get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.vals[key]
	return v, ok
}

// orderedKeys 按 JS 的规则返回键：像数组下标的整数键按数值升序排在前面，其余按出现顺序。
func (o *jsObject) orderedKeys() []string {
	var index, other []string
	for _, k := range o.keys {
		if isArrayIndex(k) {
			index = append(index, k)
		} else {
			other = append(other, k)
		}
	}
	sort.Slice(index, func(i, j int) bool {
		a, _ := strconv.ParseUint(index[i], 10, 32)
		b, _ := strconv.ParseUint(index[j], 10, 32)
		return a < b
	})
	return append(index, other...)
}

func isArrayIndex(k string) bool {
	n, err := strconv.ParseUint(k, 10, 32)
	return err == nil && n < math.MaxUint32 && strconv.FormatUint(n, 10) == k
}

// parseJSOrdered 解析 JSON，对象解析为 *jsObject，数字保留为 json.Number。
func parseJSOrdered(text string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	v, err := decodeOrdered(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err == nil {
		return nil, errors.New("JSON 后面还有多余内容")
	}
	return v, nil
}

func decodeOrdered(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		if t == '[' {
			list := []any{}
			for dec.More() {
				v, err := decodeOrdered(dec)
				if err != nil {
					return nil, err
				}
				list = append(list, v)
			}
			_, err := dec.Token()
			return list, err
		}
		obj := &jsObject{vals: map[string]any{}}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, _ := keyTok.(string)
			v, err := decodeOrdered(dec)
			if err != nil {
				return nil, err
			}
			if _, dup := obj.vals[key]; !dup { // 重复的键：位置取第一次出现，值取最后一次
				obj.keys = append(obj.keys, key)
			}
			obj.vals[key] = v
		}
		_, err := dec.Token()
		return obj, err
	default:
		return tok, nil
	}
}

// MarshalJSON 按 JS 的键顺序输出。
func (o *jsObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.orderedKeys() {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := encodeJSON(k, "")
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		val, err := encodeJSON(o.vals[k], "")
		if err != nil {
			return nil, err
		}
		buf.Write(val)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// jsString 与 JavaScript 的 String(v) 相同；undefined 用 Go 的 nil 以外的方式表示，不会走到这里。
func jsString(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return x.String()
		}
		return jsNumber(f)
	case float64:
		return jsNumber(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case []any:
		parts := make([]string, len(x))
		for i, item := range x {
			if item != nil { // 数组里的 null 和 undefined 转成空字符串
				parts[i] = jsString(item)
			}
		}
		return strings.Join(parts, ",")
	default:
		return "[object Object]"
	}
}

// jsNumber 按 JS 的 Number#toString 格式化：1e-6 ≤ |f| < 1e21 用普通写法，否则用指数写法（如 1e+21、1.5e-7）。
func jsNumber(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		return "0"
	}
	if abs := math.Abs(f); abs >= 1e-6 && abs < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	s := strconv.FormatFloat(f, 'e', -1, 64) // 例如 1.5e-07
	mant, exp, _ := strings.Cut(s, "e")
	sign := exp[0]
	exp = strings.TrimLeft(exp[1:], "0")
	return mant + "e" + string(sign) + exp
}
