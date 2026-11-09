package compat

import (
	"encoding/json"
	"regexp"
	"sort"
)

// shapeClasses 把字符串归入大致的类别，用于比对随机生成的数据（Mock）：两边生成的值不同，但类别应当相同。
// 按顺序匹配，第一个匹配的就是类别。
var shapeClasses = []struct {
	name string
	re   *regexp.Regexp
}{
	{"empty", regexp.MustCompile(`^$`)},
	{"email", regexp.MustCompile(`^[a-z]\.[a-z]{3,10}@\S+$`)},
	{"url", regexp.MustCompile(`^[a-z0-9]+://\S+$`)},
	{"datetime", regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`)},
	{"date", regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)},
	{"time", regexp.MustCompile(`^\d{2}:\d{2}:\d{2}$`)},
	{"ip", regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`)},
	{"guid", regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)},
	{"color", regexp.MustCompile(`^(#[0-9a-f]{6}|(rgba?|hsl)\(.*\))$`)},
	{"digits", regexp.MustCompile(`^\d+$`)},
	{"han", regexp.MustCompile(`^[\x{4e00}-\x{9fa5}\x{3002} ]+$`)},
	{"lower", regexp.MustCompile(`^[a-z]+$`)},
	{"upper", regexp.MustCompile(`^[A-Z]+$`)},
	{"words", regexp.MustCompile(`^[A-Za-z][a-z]*( [A-Za-z][a-z]*)*\.?$`)},
}

// Shape 把值换成它的“形状”：字符串为 <string:类别>，数字为 <number>，布尔为 <boolean>，null 不变；
// 对象保留键，数组换成去重、排序后的元素形状（长度随机的数组也能比对）。
func Shape(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			out[k] = Shape(item)
		}
		return out
	case []any:
		seen := map[string]any{}
		for _, item := range x {
			s := Shape(item)
			key, _ := json.Marshal(s)
			seen[string(key)] = s
		}
		keys := make([]string, 0, len(seen))
		for k := range seen {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([]any, len(keys))
		for i, k := range keys {
			out[i] = seen[k]
		}
		return out
	case string:
		for _, c := range shapeClasses {
			if c.re.MatchString(x) {
				return "<string:" + c.name + ">"
			}
		}
		return "<string>"
	case json.Number, float64, int:
		return "<number>"
	case bool:
		return "<boolean>"
	}
	return v
}
