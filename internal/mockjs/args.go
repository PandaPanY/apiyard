package mockjs

import (
	"regexp"
	"strings"

	"github.com/PandaPanY/apiyard/internal/jsjson"
)

var reArgSplit = regexp.MustCompile(`,\s*`)

// parseParams 解析占位符括号里的参数。原版用 eval 求值，这里只接受字面量：数字、字符串（单引号或双引号）、
// true / false / null、数组和对象（JSON5 的写法）。解析不了时（例如 @date(yyyy-MM-dd) 这种没加引号的写法，
// 或者任何 JS 表达式）与原版 eval 出错时的处理相同：按逗号拆成字符串。
func parseParams(text string) []any {
	if strings.TrimSpace(text) == "" {
		return []any{}
	}
	if v, err := jsjson.ParseJSON5("[" + text + "]"); err == nil {
		if list, ok := v.([]any); ok {
			out := make([]any, len(list))
			for i, item := range list {
				out[i] = fromJS(item)
			}
			return out
		}
	}
	parts := reArgSplit.Split(text, -1)
	out := make([]any, len(parts))
	for i, p := range parts {
		out[i] = p
	}
	return out
}
