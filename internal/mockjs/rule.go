package mockjs

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// 与 Mock.js 的 Constant 相同。
var (
	// reKey 匹配属性名里的生成规则：'name|min-max'、'name|count'、'name|min-max.dmin-dmax'、'name|+step' 等。
	// 1 name, 2 step, 3 range, 4 decimal
	reKey = regexp.MustCompile(`(.+)\|(?:\+(\d+)|([\+\-]?\d+-?[\+\-]?\d*)?(?:\.(\d+-?\d*))?)`)
	// reRange 拆出范围的两端。
	reRange = regexp.MustCompile(`([\+\-]?\d+)-?([\+\-]?\d+)?`)
	// rePlaceholder 匹配占位符 @name 或 @name(参数)。
	rePlaceholder = regexp.MustCompile(`\\*@([^@#%&()\?\s]+)(?:\((.*?)\))?`)
)

// rule 是解析后的生成规则，对应 Mock.js Parser.parse 的结果；没有的值为 nil。
type rule struct {
	parameters []string // 0 整体, 1 name, 2 step, 3 range, 4 decimal；不匹配时为 nil
	hasRange   bool
	min, max   *float64
	count      *float64
	hasDecimal bool
	dcount     *float64
}

func num(f float64) *float64 { return &f }

// parseRule 解析属性名里的生成规则。
func parseRule(name string) rule {
	var r rule
	m := reKey.FindStringSubmatch(name)
	if m == nil {
		return r
	}
	r.parameters = m
	if m[3] != "" {
		if rg := reRange.FindStringSubmatch(m[3]); rg != nil {
			r.hasRange = true
			r.min = num(parseInt(rg[1]))
			if rg[2] != "" {
				r.max = num(parseInt(rg[2]))
				r.count = num(randomInteger(*r.min, *r.max))
			} else {
				r.count = num(parseInt(rg[1]))
			}
		}
	}
	if m[4] != "" {
		if dg := reRange.FindStringSubmatch(m[4]); dg != nil {
			r.hasDecimal = true
			dmin := parseInt(dg[1])
			// 原版是 !decimal[2] && parseInt(decimal[1]) || Random.integer(dmin, dmax)：
			// 只写了 dmin 且为 0 时（如 '|1.0'）也会走到随机分支，dmax 为 undefined 即 2^53。为了不在补位时死循环，这里限制为 17 位
			dmax := math.NaN()
			if dg[2] != "" {
				dmax = parseInt(dg[2])
			}
			if dg[2] == "" && dmin != 0 {
				r.dcount = num(dmin)
			} else {
				if math.IsNaN(dmax) {
					dmax = maxSafe
				}
				r.dcount = num(randomInteger(dmin, dmax))
			}
			if *r.dcount > 17 {
				*r.dcount = 17
			}
		}
	}
	return r
}

// parsedName 去掉属性名里的生成规则（name.replace(RE_KEY, '$1')，只替换第一处）。
func parsedName(name string) string {
	loc := reKey.FindStringSubmatchIndex(name)
	if loc == nil {
		return name
	}
	return name[:loc[0]] + name[loc[2]:loc[3]] + name[loc[1]:]
}

// stringToNumber 对应 JS 的 Number(string)：去掉首尾空白，空串为 0，支持 0x/0o/0b 前缀和 Infinity，其余格式为 NaN。
func stringToNumber(s string) float64 {
	s = trimJSSpace(s)
	if s == "" {
		return 0
	}
	switch s {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	for prefix, base := range map[string]int{"0x": 16, "0X": 16, "0o": 8, "0O": 8, "0b": 2, "0B": 2} {
		if strings.HasPrefix(s, prefix) {
			n, err := strconv.ParseUint(s[2:], base, 64)
			if err != nil {
				return math.NaN()
			}
			return float64(n)
		}
	}
	if strings.ContainsAny(s, "xXoObBpP_") || strings.HasPrefix(strings.ToLower(strings.TrimLeft(s, "+-")), "inf") || strings.EqualFold(s, "nan") {
		return math.NaN() // Go 的 ParseFloat 认可而 JS 不认可的写法
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) { // 超出范围时 f 为 ±Inf 或 0，与 JS 相同
		return math.NaN()
	}
	return f
}

func trimJSSpace(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '\r', 0x0B, 0x0C, 0xA0, 0xFEFF, 0x2028, 0x2029:
			return true
		}
		return r >= 0x2000 && r <= 0x200A || r == 0x1680 || r == 0x202F || r == 0x205F || r == 0x3000
	})
}
