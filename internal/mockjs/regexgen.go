package mockjs

import (
	"regexp/syntax"
	"strings"

	"github.com/PandaPanY/apiyard/internal/genbudget"
)

// genRegexp 生成一个与正则匹配的随机字符串，规则与 Mock.js 的 RegExp Handler 相同：
// 没有上限的量词（*、+、{n,}）重复 min 到 min+3~7 次，字符集里随机取一项，. 取任意可打印字符，
// 取反的字符集只在可打印 ASCII 字符里取，^ $ \b 这类断言不生成字符。
// 正则用 Go 的语法解析，Go 不支持的写法（反向引用、先行断言）原样返回模式文本。
func genRegexpWithBudget(source string, budget *genbudget.Budget) (string, error) {
	re, err := syntax.Parse(source, syntax.Perl)
	if err != nil {
		return source, nil
	}
	var b strings.Builder
	genNode(re, &b, budget)
	return b.String(), budget.Err()
}

const printable = " !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~"

func repeatCount(minCount, maxCount int) int {
	lo := float64(max(minCount, 0))
	hi := float64(maxCount)
	if maxCount < 0 { // 没有上限
		hi = lo + randomInteger(3, 7)
	}
	return int(randomInteger(lo, hi))
}

func genNode(re *syntax.Regexp, b *strings.Builder, budget *genbudget.Budget) {
	if !budget.Step(0) || b.Len() > genbudget.MaxBytes {
		budget.Fail()
		return
	}
	switch re.Op {
	case syntax.OpLiteral:
		b.WriteString(string(re.Rune))
	case syntax.OpCharClass:
		b.WriteRune(pickFromClass(re.Rune))
	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		b.WriteString(randomCharacter(undefined))
	case syntax.OpCapture:
		genNode(re.Sub[0], b, budget)
	case syntax.OpStar:
		repeatNode(re.Sub[0], repeatCount(0, -1), b, budget)
	case syntax.OpPlus:
		repeatNode(re.Sub[0], repeatCount(1, -1), b, budget)
	case syntax.OpQuest:
		repeatNode(re.Sub[0], repeatCount(0, 1), b, budget)
	case syntax.OpRepeat:
		repeatNode(re.Sub[0], repeatCount(re.Min, re.Max), b, budget)
	case syntax.OpConcat:
		for _, sub := range re.Sub {
			genNode(sub, b, budget)
		}
	case syntax.OpAlternate:
		// 原版的选择是二叉的（a|b|c 为 a 或 (b|c)），各取一半的概率
		for i, sub := range re.Sub {
			if i == len(re.Sub)-1 || random() >= 0.5 {
				genNode(sub, b, budget)
				return
			}
		}
	}
	// 其余是 ^ $ \b 等断言和空匹配，不生成字符
}

func repeatNode(re *syntax.Regexp, n int, b *strings.Builder, budget *genbudget.Budget) {
	for i := 0; i < n; i++ {
		if budget.Err() != nil {
			return
		}
		genNode(re, b, budget)
	}
}

// pickFromClass 从字符集（成对的区间）里随机取一个字符：先随机取一个区间，再在区间里取。
// 区间覆盖了可打印 ASCII 以外的大片字符时（取反的字符集），只在可打印 ASCII 里取。
func pickFromClass(ranges []rune) rune {
	if len(ranges) == 0 {
		return 0
	}
	if ranges[len(ranges)-1] > 0xFFFF {
		var pool []rune
		for _, c := range printable {
			for i := 0; i+1 < len(ranges); i += 2 {
				if c >= ranges[i] && c <= ranges[i+1] {
					pool = append(pool, c)
					break
				}
			}
		}
		if len(pool) > 0 {
			return pool[int(randomNatural(0, float64(len(pool)-1)))]
		}
	}
	i := int(randomNatural(0, float64(len(ranges)/2-1))) * 2
	return rune(randomInteger(float64(ranges[i]), float64(ranges[i+1])))
}
