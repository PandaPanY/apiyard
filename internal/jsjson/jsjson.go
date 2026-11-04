// Package jsjson 按 JavaScript 的规则解析和输出 JSON，用于需要与 YApi（Node.js）逐字一致的地方：
// 对象保持键的顺序（与 JSON.parse 之后 Object.keys 的顺序相同），数字和其他值转字符串时与 JS 的 String(v) 相同。
package jsjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Object 是保持键顺序的 JSON 对象。
type Object struct {
	keys []string
	vals map[string]any
}

// NewObject 按顺序用键值对创建对象。
func NewObject(pairs ...any) *Object {
	o := &Object{vals: map[string]any{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		o.Set(pairs[i].(string), pairs[i+1])
	}
	return o
}

// Get 读取字段。
func (o *Object) Get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.vals[key]
	return v, ok
}

// Set 写入字段：已有的键原地替换，新键追加到末尾（与给 JS 对象赋值相同）。
func (o *Object) Set(key string, v any) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

// Delete 删除字段。
func (o *Object) Delete(key string) {
	if _, ok := o.vals[key]; !ok {
		return
	}
	delete(o.vals, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			return
		}
	}
}

// Len 返回字段数。
func (o *Object) Len() int { return len(o.keys) }

// Clone 浅复制。
func (o *Object) Clone() *Object {
	c := &Object{keys: append([]string(nil), o.keys...), vals: make(map[string]any, len(o.vals))}
	for k, v := range o.vals {
		c.vals[k] = v
	}
	return c
}

// Keys 按 JS 的规则返回键：像数组下标的整数键按数值升序排在前面，其余按出现顺序。
func (o *Object) Keys() []string {
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

// MarshalJSON 按 Keys 的顺序输出。
func (o *Object) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.Keys() {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := Marshal(k, "")
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		val, err := Marshal(o.vals[k], "")
		if err != nil {
			return nil, err
		}
		buf.Write(val)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// Parse 解析 JSON：对象解析为 *Object，数组为 []any，数字保留为 json.Number。
func Parse(text string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	v, err := decode(dec)
	if err != nil {
		return nil, err
	}
	if rest := strings.TrimLeft(text[dec.InputOffset():], " \t\r\n"); rest != "" {
		return nil, errors.New("JSON 后面还有多余内容")
	}
	return v, nil
}

func decode(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	if delim == '[' {
		list := []any{}
		for dec.More() {
			v, err := decode(dec)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		_, err := dec.Token()
		return list, err
	}
	obj := NewObject()
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := keyTok.(string)
		v, err := decode(dec)
		if err != nil {
			return nil, err
		}
		obj.Set(key, v) // 重复的键：位置取第一次出现，值取最后一次，与 JSON.parse 相同
	}
	_, err = dec.Token()
	return obj, err
}

// Marshal 序列化为 JSON 且不转义 <>&；indent 非空时缩进，相当于 JSON.stringify(v, null, indent)。
func Marshal(v any, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent("", indent)
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// NormalizeNumbers 把值中的数字改写成 JS 的格式（例如 1.0 → 1、1e2 → 100），
// 用于模拟 JSON.parse 之后再 JSON.stringify 的结果。原值不会被修改。
func NormalizeNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		if f, err := x.Float64(); err == nil && !math.IsInf(f, 0) {
			return json.Number(FormatNumber(f))
		}
		return x
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = NormalizeNumbers(item)
		}
		return out
	case *Object:
		out := NewObject()
		for _, k := range x.keys {
			out.Set(k, NormalizeNumbers(x.vals[k]))
		}
		return out
	default:
		return v
	}
}

// String 与 JavaScript 的 String(v) 相同（null 为 "null"，对象为 "[object Object]"）。
func String(v any) string {
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
		return FormatNumber(f)
	case float64:
		return FormatNumber(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case []any:
		parts := make([]string, len(x))
		for i, item := range x {
			if item != nil { // 数组里的 null 转成空字符串
				parts[i] = String(item)
			}
		}
		return strings.Join(parts, ",")
	default:
		return "[object Object]"
	}
}

// FormatNumber 按 JS 的 Number#toString 格式化：1e-6 ≤ |f| < 1e21 用普通写法，否则用指数写法（如 1e+21、1.5e-7）。
func FormatNumber(f float64) string {
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
	return mant + "e" + exp[:1] + strings.TrimLeft(exp[1:], "0")
}
