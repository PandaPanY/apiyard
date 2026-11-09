// Package mockjs 是 Mock.js 1.0.1-beta3（YApi 用的版本）在 Go 中的实现：按数据模板生成随机数据，
// 支持生成规则（'list|1-10'、'id|+1'、'price|1-100.2' 等）和占位符（@email、@integer(1, 10)、@cname 等）。
//
// 移植自 Mock.js（https://github.com/nuysoft/Mock，MIT License，Copyright (c) 2015 nuysoft），
// 规则和随机函数的行为与原版一致，包括一些不直观的地方（见各函数的注释）。
// 与原版刻意不同的一点：原版用 eval 解析占位符的参数（@pick(...) 括号里的内容会被当作 JS 代码执行），
// 在 YApi 里这是一条不需要 Mock 脚本的代码执行途径；这里只按字面量（数字、字符串、数组、对象等）解析，
// 解析不了时与原版的出错分支一样按逗号拆成字符串。
package mockjs

import (
	"encoding/json"
	"math"
	"sync/atomic"

	"github.com/PandaPanY/apiyard/internal/jsjson"
)

// RegExp 是正则模板，生成与之匹配的随机字符串（Mock.js 的 'name': /regexp/）。
// JSON 里没有正则，YApi 用 'name|regexp': '模式' 的写法产生它，见 internal/mock。
type RegExp struct {
	Source string
}

// Mock 按 Mock.js 的规则处理数据模板，相当于 Mock.mock(template)。
// template 是 jsjson 的值（*jsjson.Object、[]any、json.Number、string、bool、nil），可以含 RegExp；
// 结果也是 jsjson 的值，可以直接用 jsjson.Marshal 输出（NaN、Infinity 输出为 null，与 JSON.stringify 相同）。
// 返回错误相当于原版抛出异常（例如 @dataImage 在 Node 里没有 canvas）。
func Mock(template any) (any, error) {
	m := &mocker{guid: guid.Add(1)}
	v, err := m.gen(fromJS(template), "", nil)
	if err != nil {
		return nil, err
	}
	return toJS(v), nil
}

// guid 对应 Mock.js 的 Constant.GUID：每次生成从新的编号开始，@order 按它区分不同的模板。
var guid atomic.Int64

// ---- 内部的值 ----
//
// 模板在生成过程中会被修改（'id|+1' 会累加，被占位符引用的属性会先计算并写回模板），
// 所以先转成可修改的 tObject / tArray；数字一律是 float64。生成结果用 *jsjson.Object 和 *rArray。

type tObject struct {
	keys []string
	vals map[string]any
}

func (o *tObject) get(k string) (any, bool) {
	v, ok := o.vals[k]
	return v, ok
}

type tArray struct {
	items      []any
	orderIndex int // 'list|+1': [...] 的当前位置，记在模板上（与原版的 __order_index 相同）
}

// rArray 是正在生成的数组：占位符可能在数组生成到一半时引用它（例如 @length）。
type rArray struct {
	items []any
}

// undefined 对应 JS 的 undefined（例如没有传的参数）。
type undefinedType struct{}

var undefined = undefinedType{}

// fromJS 把 jsjson 的值转成可修改的模板。
func fromJS(v any) any {
	switch x := v.(type) {
	case *jsjson.Object:
		o := &tObject{vals: map[string]any{}}
		for _, k := range x.Keys() {
			val, _ := x.Get(k)
			o.keys = append(o.keys, k)
			o.vals[k] = fromJS(val)
		}
		return o
	case []any:
		a := &tArray{items: make([]any, len(x))}
		for i, item := range x {
			a.items[i] = fromJS(item)
		}
		return a
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return math.NaN()
		}
		return f
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case *rArray:
		return fromJS(toJS(x))
	}
	return v
}

// toJS 把生成结果转成 jsjson 的值。
func toJS(v any) any {
	switch x := v.(type) {
	case *jsjson.Object:
		out := jsjson.NewObject()
		for _, k := range x.Keys() {
			if val, _ := x.Get(k); val != any(undefined) { // 与 JSON.stringify 一样省略值为 undefined 的属性
				out.Set(k, toJS(val))
			}
		}
		return out
	case *rArray:
		out := make([]any, len(x.items))
		for i, item := range x.items {
			out[i] = toJS(item)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = toJS(item)
		}
		return out
	case *tObject:
		out := jsjson.NewObject()
		for _, k := range x.keys {
			if x.vals[k] != any(undefined) {
				out.Set(k, toJS(x.vals[k]))
			}
		}
		return out
	case *tArray:
		return toJS(&rArray{items: x.items})
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil
		}
		return json.Number(jsjson.FormatNumber(x))
	case undefinedType:
		return nil
	case RegExp:
		return jsjson.NewObject() // JSON.stringify(/x/) 为 {}
	}
	return v
}

// jsType 对应 Mock.js 的 Util.type。
func jsType(v any) string {
	switch v.(type) {
	case *tObject, *jsjson.Object:
		return "object"
	case *tArray, *rArray, []any:
		return "array"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case string:
		return "string"
	case RegExp:
		return "regexp"
	case nil:
		return "null"
	}
	return "undefined"
}

// typeOf 对应 JS 的 typeof。
func typeOf(v any) string {
	switch v.(type) {
	case float64:
		return "number"
	case bool:
		return "boolean"
	case string:
		return "string"
	case undefinedType:
		return "undefined"
	}
	return "object"
}

// jsString 对应 JS 的 String(v)。
func jsString(v any) string {
	switch x := v.(type) {
	case undefinedType:
		return "undefined"
	case *rArray:
		return jsjson.String(toJS(x))
	case *tArray:
		return jsjson.String(toJS(x))
	case *tObject, *jsjson.Object, RegExp:
		if r, ok := x.(RegExp); ok {
			return "/" + r.Source + "/"
		}
		return "[object Object]"
	}
	return jsjson.String(v)
}

// toNumber 对应 JS 的 Number(v)（比较大小、算术时的隐式转换）。
func toNumber(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case bool:
		if x {
			return 1
		}
		return 0
	case nil:
		return 0
	case string:
		return stringToNumber(x)
	case *rArray, *tArray:
		return stringToNumber(jsString(x))
	}
	return math.NaN()
}

// parseInt 对应 JS 的 parseInt(v, 10)：取开头的整数部分，没有时为 NaN。
func parseInt(v any) float64 {
	s := trimJSSpace(jsString(v))
	end := 0
	if end < len(s) && (s[end] == '+' || s[end] == '-') {
		end++
	}
	digits := end
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == digits {
		return math.NaN()
	}
	f := stringToNumber(s[:end])
	return f
}
