package mockjs

import (
	"regexp"
	"strings"

	"github.com/PandaPanY/apiyard/internal/jsjson"
)

// 以下对应 YApi 的 common/mock-extra.js：在交给 Mock.js 之前先处理一遍模板。

// reVar 匹配 ${query.name}、${body.a.b} 这样的请求变量。
var reVar = regexp.MustCompile(`(?i)\$\{([a-zA-Z]+)\.?([a-zA-Z0-9_.]*)\}`)

// Extra 对应 YApi 的 mockExtra(template, context)：
//   - 字符串值里有 ${query.x} 这样的变量时，整个值换成请求里对应的数据（没有时为空字符串）；
//   - 属性名带 |regexp 的，值当作正则，交给 Mock.js 生成匹配的字符串（属性移到对象末尾，与原版相同）。
//
// vars 是请求的数据，通常有 query、body、params 三项。模板不是对象或数组时原样返回。
func Extra(template any, vars *jsjson.Object) (any, error) {
	switch template.(type) {
	case *jsjson.Object, []any:
		return extraParse(template, vars)
	}
	return template, nil
}

func extraParse(p any, vars *jsjson.Object) (any, error) {
	if list, ok := p.([]any); ok {
		out := make([]any, len(list))
		for i, item := range list {
			v, err := extraValue(item, vars)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	}
	obj := p.(*jsjson.Object)
	out := jsjson.NewObject()
	for _, k := range obj.Keys() {
		item, _ := obj.Get(k)
		s, isStr := item.(string)
		if !isStr || s == "" {
			v, err := extraValue(item, vars)
			if err != nil {
				return nil, err
			}
			out.Set(k, v)
			continue
		}
		v, err := handleVar(s, vars)
		if err != nil {
			return nil, err
		}
		out.Set(k, v)
		filters := strings.Split(k, "|")
		kept := append([]string(nil), filters...)
		for _, f := range filters[1:] {
			if strings.ToLower(f) != "regexp" {
				continue
			}
			// 原版用 newFilters.indexOf(小写名) 查找，所以只有写成小写的 |regexp 会从属性名里去掉；
			// 写成 |RegExp 时属性名保持不变，Mock.js 生成的属性名会变成 aRegExp
			for j, kf := range kept {
				if j > 0 && kf == "regexp" {
					kept = append(kept[:j], kept[j+1:]...)
					break
				}
			}
			out.Delete(k)
			out.Set(strings.Join(kept, "|"), RegExp{Source: jsString(fromJS(v))})
		}
	}
	return out, nil
}

// extraValue 处理不需要替换变量的值：对象和数组递归处理，其余原样保留。
func extraValue(v any, vars *jsjson.Object) (any, error) {
	switch v.(type) {
	case *jsjson.Object, []any:
		return extraParse(v, vars)
	}
	return v, nil
}

// handleVar 对应 mock-extra 的 handleStr。
func handleVar(s string, vars *jsjson.Object) (any, error) {
	if !strings.Contains(s, "{") || !strings.Contains(s, "}") || !strings.Contains(s, "$") {
		return s, nil
	}
	m := reVar.FindStringSubmatch(s)
	if m == nil {
		return s, nil
	}
	name := m[1]
	if m[2] != "" {
		name += "." + m[2]
	}
	var data any = vars
	for _, n := range strings.Split(name, ".") {
		if data == "" {
			break
		}
		switch d := data.(type) {
		case *jsjson.Object:
			if v, ok := d.Get(n); ok {
				data = v
			} else {
				data = ""
			}
		case []any:
			if v, ok := arrayLookup(d, n); ok {
				data = v
			} else {
				data = ""
			}
		default:
			// 原版对数字、字符串、null 用 in 运算符会抛出 TypeError
			return nil, jsErrorf("Cannot use 'in' operator to search for '%s' in %s", n, jsString(fromJS(data)))
		}
	}
	return data, nil
}
