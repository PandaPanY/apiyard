package compat

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var varPattern = regexp.MustCompile(`\{\{(\w+)\}\}`)

// render 把 v 中所有字符串里的 {{变量}} 替换为变量值。
// 整个字符串恰好是一个 "{{变量}}" 时保留变量原类型，这样 "{{gid}}" 在请求体里仍是数字。
func render(v any, vars map[string]any) (any, error) {
	switch x := v.(type) {
	case string:
		return renderString(x, vars)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			r, err := render(item, vars)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			r, err := render(item, vars)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	default:
		return v, nil
	}
}

func renderString(s string, vars map[string]any) (any, error) {
	if m := varPattern.FindStringSubmatch(s); m != nil && m[0] == s {
		val, ok := vars[m[1]]
		if !ok {
			return nil, fmt.Errorf("未定义的变量 {{%s}}", m[1])
		}
		return val, nil
	}
	return renderText(s, vars)
}

// renderText 总是返回字符串，用于路径和查询参数。
func renderText(s string, vars map[string]any) (string, error) {
	var missing string
	out := varPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := varPattern.FindStringSubmatch(match)[1]
		val, ok := vars[name]
		if !ok {
			missing = name
			return match
		}
		return fmt.Sprint(val)
	})
	if missing != "" {
		return "", fmt.Errorf("未定义的变量 {{%s}}", missing)
	}
	return out, nil
}

// lookup 按点分路径从 JSON 值中取值，数字段表示数组下标，如 "data.list.0._id"。
func lookup(v any, path string) (any, error) {
	cur := v
	for _, key := range strings.Split(path, ".") {
		switch x := cur.(type) {
		case map[string]any:
			next, ok := x[key]
			if !ok {
				return nil, fmt.Errorf("路径 %s 中找不到字段 %q", path, key)
			}
			cur = next
		case []any:
			idx, err := strconv.Atoi(key)
			if err != nil || idx < 0 || idx >= len(x) {
				return nil, fmt.Errorf("路径 %s 中的下标 %q 无效", path, key)
			}
			cur = x[idx]
		default:
			return nil, fmt.Errorf("路径 %s 在 %q 处不是对象或数组", path, key)
		}
	}
	return cur, nil
}
