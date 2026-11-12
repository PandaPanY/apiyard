// Package schemafake 按 JSON Schema 生成示例数据，对应 YApi 用的 json-schema-faker。
// 只实现 YApi 界面里常用的部分：type、properties、required、items、enum、const、format、长度和数值范围、
// allOf/anyOf/oneOf 以及本文档内的 $ref。生成的值是随机的，与原版不会逐字相同。
package schemafake

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/PandaPanY/apiyard/internal/genbudget"
	"github.com/PandaPanY/apiyard/internal/mockjs"
)

// Options 控制生成方式。
type Options struct {
	// AlwaysFakeOptionals 为 true 时非必填字段也生成（YApi 默认如此）；否则随机决定。
	AlwaysFakeOptionals bool
}

// ErrInvalidSchema 表示 schema 不是 JSON 对象。
var ErrInvalidSchema = errors.New("invalid schema")

// maxDepth 防止循环引用导致无限递归。
const maxDepth = 20

// Generate 按 schema 生成一个示例值。
func Generate(schema any, opts Options) (any, error) {
	return GenerateContext(context.Background(), schema, opts)
}

// GenerateContext generates data with bounded resources and cancellation.
func GenerateContext(ctx context.Context, schema any, opts Options) (any, error) {
	root, ok := schema.(map[string]any)
	if !ok {
		return nil, ErrInvalidSchema
	}
	g := &generator{root: root, opts: opts, budget: genbudget.New(ctx)}
	v := g.value(root, 0)
	g.account(v, 0)
	if err := g.budget.Err(); err != nil {
		return nil, err
	}
	return v, g.err
}

func (g *generator) account(v any, depth int) {
	if !g.budget.Step(depth) {
		return
	}
	switch x := v.(type) {
	case string:
		g.budget.Size(len(x))
	case map[string]any:
		for k, val := range x {
			if !g.budget.Size(len(k)) {
				return
			}
			g.account(val, depth+1)
			if g.budget.Err() != nil {
				return
			}
		}
	case []any:
		for _, val := range x {
			g.account(val, depth+1)
			if g.budget.Err() != nil {
				return
			}
		}
	}
}

type generator struct {
	budget *genbudget.Budget
	root   map[string]any
	opts   Options
	err    error // Mock 规则出错（与原版一样，整个生成失败）
}

// #nosec G404 -- 示例数据不需要密码学安全的随机数
func randInt(lo, hi int64) int64 {
	if hi <= lo {
		return lo
	}
	return lo + rand.Int64N(hi-lo+1)
}

func (g *generator) value(s map[string]any, depth int) (result any) {
	if !g.budget.Step(depth) {
		return nil
	}
	defer func() {
		if s, ok := result.(string); ok {
			g.budget.Size(len(s))
		}
	}()
	if depth > maxDepth {
		return nil
	}
	s = g.resolve(s)
	if all, ok := s["allOf"].([]any); ok {
		s = mergeAll(s, all, g)
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		if list, ok := s[key].([]any); ok && len(list) > 0 {
			if sub, ok := list[randInt(0, int64(len(list)-1))].(map[string]any); ok {
				return g.value(sub, depth+1)
			}
		}
	}
	if v, ok := s["const"]; ok {
		return v
	}
	if list, ok := s["enum"].([]any); ok && len(list) > 0 {
		return list[randInt(0, int64(len(list)-1))]
	}
	// YApi 的 JSON Schema 编辑器把 Mock 规则写在 mock.mock 中，YApi 给 json-schema-faker 注册了 mock 扩展，
	// 值是 Mock.mock(规则) 的结果（不按 type 转换，例如 @integer 得到数字）
	if m, ok := s["mock"].(map[string]any); ok {
		if text, ok := m["mock"].(string); ok && text != "" {
			v, err := mockjs.MockWithBudget(text, g.budget)
			if err != nil {
				g.err = err
				return nil
			}
			return v
		}
	}

	switch typeOf(s) {
	case "object":
		return g.object(s, depth)
	case "array":
		return g.array(s, depth)
	case "string":
		for _, key := range []string{"minLength", "maxLength"} {
			if n, ok := intOf(s[key]); ok && (n < 0 || n > genbudget.MaxLength) {
				g.budget.Fail()
				return nil
			}
		}
		return fakeString(s)
	case "integer":
		return fakeInteger(s)
	case "number":
		return fakeNumber(s)
	case "boolean":
		return randInt(0, 1) == 1
	default:
		return nil
	}
}

// resolve 展开本文档内的 $ref（如 #/definitions/User）；无法解析的引用视为空 schema。
func (g *generator) resolve(s map[string]any) map[string]any {
	for range maxDepth {
		ref, ok := s["$ref"].(string)
		if !ok {
			return s
		}
		var cur any = g.root
		for part := range strings.SplitSeq(strings.TrimPrefix(ref, "#"), "/") {
			if part == "" {
				continue
			}
			part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
			m, _ := cur.(map[string]any)
			cur = m[part]
		}
		next, ok := cur.(map[string]any)
		if !ok {
			return map[string]any{}
		}
		s = next
	}
	return s
}

// mergeAll 合并 allOf 中各个 schema 的 properties 和 required，其余字段以后出现的为准。
func mergeAll(s map[string]any, all []any, g *generator) map[string]any {
	out := map[string]any{}
	props := map[string]any{}
	var required []any
	for _, item := range append([]any{s}, all...) {
		sub, ok := item.(map[string]any)
		if !ok {
			continue
		}
		sub = g.resolve(sub)
		for k, v := range sub {
			if k != "allOf" {
				out[k] = v
			}
		}
		if p, ok := sub["properties"].(map[string]any); ok {
			for k, v := range p {
				props[k] = v
			}
		}
		if r, ok := sub["required"].([]any); ok {
			required = append(required, r...)
		}
	}
	if len(props) > 0 {
		out["properties"] = props
		if _, ok := out["type"]; !ok {
			out["type"] = "object"
		}
	}
	out["required"] = required
	return out
}

// typeOf 返回 schema 的类型；type 是数组时取第一个非 null 的类型，没有 type 时按其他字段推断。
func typeOf(s map[string]any) string {
	switch t := s["type"].(type) {
	case string:
		return t
	case []any:
		for _, item := range t {
			if name, ok := item.(string); ok && name != "null" {
				return name
			}
		}
		return "null"
	}
	switch {
	case s["properties"] != nil:
		return "object"
	case s["items"] != nil:
		return "array"
	}
	return ""
}

func (g *generator) object(s map[string]any, depth int) map[string]any {
	out := map[string]any{}
	props, _ := s["properties"].(map[string]any)
	required := map[string]bool{}
	if list, ok := s["required"].([]any); ok {
		for _, r := range list {
			if name, ok := r.(string); ok {
				required[name] = true
			}
		}
	}
	for name, sub := range props {
		if !required[name] && !g.opts.AlwaysFakeOptionals && randInt(0, 1) == 0 {
			continue
		}
		if m, ok := sub.(map[string]any); ok {
			out[name] = g.value(m, depth+1)
		}
	}
	return out
}

func (g *generator) array(s map[string]any, depth int) []any {
	lo, hasMin := intOf(s["minItems"])
	hi, hasMax := intOf(s["maxItems"])
	if lo < 0 || hi < 0 || lo > genbudget.MaxLength || hi > genbudget.MaxLength {
		g.budget.Fail()
		return nil
	}
	switch {
	case !hasMin && !hasMax:
		lo, hi = 1, 3
	case !hasMax:
		hi = lo + 3
	case !hasMin:
		lo = min(1, hi)
	}
	// 元组形式：items 是数组时按位置生成
	if tuple, ok := s["items"].([]any); ok {
		if len(tuple) > genbudget.MaxLength {
			g.budget.Fail()
			return nil
		}
		out := make([]any, 0, len(tuple))
		for _, item := range tuple {
			if m, ok := item.(map[string]any); ok {
				out = append(out, g.value(m, depth+1))
			}
		}
		return out
	}
	items, _ := s["items"].(map[string]any)
	n := randInt(lo, hi)
	if n < 0 || n > genbudget.MaxLength {
		g.budget.Fail()
		return nil
	}
	out := make([]any, 0, n)
	for range n {
		if g.budget.Err() != nil {
			return nil
		}
		if items == nil {
			if !g.budget.Step(depth + 1) {
				return nil
			}
			out = append(out, nil)
			continue
		}
		out = append(out, g.value(items, depth+1))
	}
	return out
}

func intOf(v any) (int64, bool) {
	f, ok := numberOf(v)
	return int64(f), ok
}

func numberOf(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case interface{ Float64() (float64, error) }:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

// bounds 读取数值范围；只给出一端时另一端取相距 1000 的值。
func bounds(s map[string]any) (lo, hi float64) {
	lo, hasLo := numberOf(s["minimum"])
	hi, hasHi := numberOf(s["maximum"])
	if v, ok := numberOf(s["exclusiveMinimum"]); ok {
		lo, hasLo = v+1, true
	} else if s["exclusiveMinimum"] == true && hasLo {
		lo++
	}
	if v, ok := numberOf(s["exclusiveMaximum"]); ok {
		hi, hasHi = v-1, true
	} else if s["exclusiveMaximum"] == true && hasHi {
		hi--
	}
	switch {
	case !hasLo && !hasHi:
		return 0, 1000
	case !hasLo:
		return hi - 1000, hi
	case !hasHi:
		return lo, lo + 1000
	}
	return lo, hi
}

func fakeInteger(s map[string]any) int64 {
	lo, hi := bounds(s)
	n := randInt(int64(math.Ceil(lo)), int64(math.Floor(hi)))
	if step, ok := intOf(s["multipleOf"]); ok && step > 0 {
		n -= n % step
	}
	return n
}

// #nosec G404 -- 示例数据不需要密码学安全的随机数
func fakeNumber(s map[string]any) float64 {
	lo, hi := bounds(s)
	return math.Round((lo+rand.Float64()*(hi-lo))*100) / 100
}

var words = strings.Fields("lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor " +
	"incididunt ut labore et dolore magna aliqua enim ad minim veniam quis nostrud exercitation ullamco")

func fakeString(s map[string]any) string {
	switch s["format"] {
	case "email":
		return fmt.Sprintf("%s.%s@example.com", word(), word())
	case "date-time":
		return randomTime().Format(time.RFC3339)
	case "date":
		return randomTime().Format(time.DateOnly)
	case "time":
		return randomTime().Format(time.TimeOnly)
	case "uri", "url", "uri-reference":
		return fmt.Sprintf("https://%s.example.com/%s", word(), word())
	case "hostname":
		return word() + ".example.com"
	case "ipv4":
		return fmt.Sprintf("%d.%d.%d.%d", randInt(1, 254), randInt(0, 255), randInt(0, 255), randInt(1, 254))
	case "ipv6":
		return fmt.Sprintf("2001:db8::%x:%x", randInt(0, 0xffff), randInt(0, 0xffff))
	case "uuid":
		return fmt.Sprintf("%08x-%04x-4%03x-a%03x-%012x", randInt(0, 0xffffffff), randInt(0, 0xffff),
			randInt(0, 0xfff), randInt(0, 0xfff), randInt(0, 0xffffffffffff))
	}
	minLen, _ := intOf(s["minLength"])
	maxLen, hasMax := intOf(s["maxLength"])
	if !hasMax {
		maxLen = max(minLen, 24)
	}
	text := word()
	for int64(len(text)) < minLen || (randInt(0, 2) > 0 && int64(len(text)) < maxLen) {
		text += " " + word()
	}
	if int64(len(text)) > maxLen {
		text = strings.TrimSpace(text[:maxLen])
	}
	for int64(len(text)) < minLen {
		text += "x"
	}
	return text
}

func word() string { return words[randInt(0, int64(len(words)-1))] }

func randomTime() time.Time {
	return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(randInt(0, 5*365*24*3600)) * time.Second)
}
