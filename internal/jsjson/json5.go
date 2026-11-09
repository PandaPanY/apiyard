package jsjson

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ParseJSON5 按 json5@0.5.1（YApi 解析 Mock 数据用的版本）的规则解析：支持注释、单引号字符串、不加引号的键、
// 结尾逗号、十六进制数、Infinity / NaN 等。结果与 Parse 相同：对象为 *Object，数组为 []any，数字为 json.Number
// （按 JS 的 Number 格式化，例如 0x10 为 16）；NaN、±Infinity 无法放进 json.Number，用 float64 表示。
func ParseJSON5(text string) (any, error) {
	p := &json5Parser{s: text}
	p.white()
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	p.white()
	if p.i < len(p.s) {
		return nil, p.errorf("多余的内容")
	}
	return v, nil
}

type json5Parser struct {
	s string
	i int
}

func (p *json5Parser) errorf(format string, args ...any) error {
	return fmt.Errorf("JSON5 第 %d 个字符: %s", p.i+1, fmt.Sprintf(format, args...))
}

func (p *json5Parser) peek() rune {
	if p.i >= len(p.s) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(p.s[p.i:])
	return r
}

func (p *json5Parser) next() rune {
	r, size := utf8.DecodeRuneInString(p.s[p.i:])
	p.i += size
	return r
}

// white 跳过空白和注释。
func (p *json5Parser) white() {
	for p.i < len(p.s) {
		switch r := p.peek(); {
		case r == '/' && strings.HasPrefix(p.s[p.i:], "//"):
			if end := strings.IndexAny(p.s[p.i:], "\n\r"); end >= 0 {
				p.i += end
			} else {
				p.i = len(p.s)
			}
		case r == '/' && strings.HasPrefix(p.s[p.i:], "/*"):
			end := strings.Index(p.s[p.i+2:], "*/")
			if end < 0 {
				p.i = len(p.s) // 未闭合的注释：后面没有内容，value() 会报错
				return
			}
			p.i += end + 4
		case r == 0x20 || r == 0x09 || r == 0x0D || r == 0x0A || r == 0x0B || r == 0x0C || r == 0xA0 || r == 0xFEFF: // json5 认可的空白字符
			p.next()
		default:
			return
		}
	}
}

func (p *json5Parser) value() (any, error) {
	switch r := p.peek(); {
	case p.i >= len(p.s):
		return nil, p.errorf("内容不完整")
	case r == '{':
		return p.object()
	case r == '[':
		return p.array()
	case r == '"' || r == '\'':
		return p.str()
	case r == '-' || r == '+' || r == '.' || (r >= '0' && r <= '9'):
		return p.number()
	default:
		return p.word()
	}
}

func (p *json5Parser) word() (any, error) {
	for _, w := range []struct {
		text string
		val  any
	}{{"true", true}, {"false", false}, {"null", nil}, {"Infinity", math.Inf(1)}, {"NaN", math.NaN()}} {
		if strings.HasPrefix(p.s[p.i:], w.text) {
			p.i += len(w.text)
			return w.val, nil
		}
	}
	return nil, p.errorf("无法识别的字符 %q", p.peek())
}

func (p *json5Parser) number() (any, error) {
	start := p.i
	sign := 1.0
	if r := p.peek(); r == '-' || r == '+' {
		p.next()
		if r == '-' {
			sign = -1
		}
	}
	if strings.HasPrefix(p.s[p.i:], "Infinity") {
		p.i += len("Infinity")
		return math.Inf(int(sign)), nil
	}
	if strings.HasPrefix(p.s[p.i:], "NaN") {
		p.i += len("NaN")
		return math.NaN(), nil
	}
	if strings.HasPrefix(p.s[p.i:], "0x") || strings.HasPrefix(p.s[p.i:], "0X") {
		p.i += 2
		digits := p.i
		for p.i < len(p.s) && strings.ContainsRune("0123456789abcdefABCDEF", p.peek()) {
			p.next()
		}
		n, err := strconv.ParseUint(p.s[digits:p.i], 16, 64)
		if err != nil {
			return nil, p.errorf("十六进制数格式不对")
		}
		return jsNumber(sign * float64(n)), nil
	}
	digits := func() {
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
		}
	}
	digits()
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		digits()
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		digits()
	}
	text := strings.TrimPrefix(strings.TrimPrefix(p.s[start:p.i], "+"), "-")
	f, err := strconv.ParseFloat(text, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return nil, p.errorf("数字格式不对")
	}
	return jsNumber(sign * f), nil
}

// jsNumber 把数值转成 json.Number；无穷大和 NaN 保留为 float64。
func jsNumber(f float64) any {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return f
	}
	return json.Number(FormatNumber(f))
}

func (p *json5Parser) str() (string, error) {
	quote := p.next()
	var b strings.Builder
	for {
		if p.i >= len(p.s) {
			return "", p.errorf("字符串没有结束")
		}
		r := p.next()
		switch {
		case r == quote:
			return b.String(), nil
		case r == '\n' || r == '\r':
			return "", p.errorf("字符串里不能直接换行")
		case r != '\\':
			b.WriteRune(r)
			continue
		}
		if p.i >= len(p.s) {
			return "", p.errorf("字符串没有结束")
		}
		switch e := p.next(); e {
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'u':
			if p.i+4 > len(p.s) {
				return "", p.errorf("\\u 后面应有 4 位十六进制数")
			}
			n, err := strconv.ParseUint(p.s[p.i:p.i+4], 16, 16)
			if err != nil {
				return "", p.errorf("\\u 后面应有 4 位十六进制数")
			}
			p.i += 4
			b.WriteRune(rune(n)) // #nosec G115 -- 只有 4 位十六进制，最大 0xFFFF
		case '\r': // 行尾的反斜杠表示续行
			if p.peek() == '\n' {
				p.next()
			}
		case '\n':
		default:
			b.WriteRune(e)
		}
	}
}

// key 读取对象的键：字符串，或者不加引号的标识符。
func (p *json5Parser) key() (string, error) {
	if r := p.peek(); r == '"' || r == '\'' {
		return p.str()
	}
	start := p.i
	for p.i < len(p.s) {
		r := p.peek()
		ok := r == '_' || r == '$' || unicode.IsLetter(r) || (p.i > start && unicode.IsDigit(r))
		if !ok {
			break
		}
		p.next()
	}
	if p.i == start {
		return "", p.errorf("对象的键格式不对")
	}
	return p.s[start:p.i], nil
}

func (p *json5Parser) object() (any, error) {
	p.next() // {
	obj := NewObject()
	for {
		p.white()
		if p.peek() == '}' {
			p.next()
			return obj, nil
		}
		k, err := p.key()
		if err != nil {
			return nil, err
		}
		p.white()
		if p.peek() != ':' {
			return nil, p.errorf("键 %q 后面应为冒号", k)
		}
		p.next()
		p.white()
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		obj.Set(k, v) // 重复的键：位置取第一次出现，值取最后一次，与 JS 相同
		p.white()
		switch p.peek() {
		case ',':
			p.next()
		case '}':
			p.next()
			return obj, nil
		default:
			return nil, p.errorf("对象里应为逗号或 }")
		}
	}
}

func (p *json5Parser) array() (any, error) {
	p.next() // [
	list := []any{}
	for {
		p.white()
		if p.peek() == ']' {
			p.next()
			return list, nil
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		list = append(list, v)
		p.white()
		switch p.peek() {
		case ',':
			p.next()
		case ']':
			p.next()
			return list, nil
		default:
			return nil, p.errorf("数组里应为逗号或 ]")
		}
	}
}
