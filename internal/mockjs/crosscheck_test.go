package mockjs

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/PandaPanY/apiyard/internal/jsjson"
)

// 交叉核对：testdata/mockjs/cases.json 里的每个模板，原版 Mock.js（testenv/yapi/run-mockjs.js）和这里各生成 400 次，
// 比较每个字段的统计特征。随机数据不能逐字比较，但类型分布、字符串的类别、数值和长度的范围、对象的键都应当一致。
// 统计和分类的算法与 run-mockjs.js 完全相同。

const samples = 400

type pathStats struct {
	N        int            `json:"n"`
	Types    map[string]int `json:"types"`
	Classes  map[string]int `json:"classes"`
	NumCount int            `json:"numCount"`
	IntCount int            `json:"intCount"`
	Keys     map[string]int `json:"keys"`
	Num      []float64      `json:"num"`
	Len      []float64      `json:"len"`
	Arr      []float64      `json:"arr"`
}

type caseStats struct {
	Error string                `json:"error"`
	Paths map[string]*pathStats `json:"paths"`
}

var strClasses = []struct {
	name string
	re   *regexp.Regexp
}{
	{"empty", regexp.MustCompile(`^$`)},
	{"email", regexp.MustCompile(`^[a-z]\.[a-z]{3,10}@\S+$`)},
	{"image", regexp.MustCompile(`^http://dummyimage\.com/`)},
	{"url", regexp.MustCompile(`^[a-z0-9]+://\S+/[a-z]+$`)},
	{"datetime", regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`)},
	{"date", regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)},
	{"time", regexp.MustCompile(`^\d{2}:\d{2}:\d{2}$`)},
	{"ip", regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`)},
	{"guid", regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)},
	{"hex", regexp.MustCompile(`^#[0-9a-f]{6}$`)},
	{"color", regexp.MustCompile(`^(rgba?|hsl)\(`)},
	{"idcard", regexp.MustCompile(`^\d{17}[\dX]$`)},
	{"digits", regexp.MustCompile(`^\d+$`)},
	{"han", regexp.MustCompile(`^[\x{4e00}-\x{9fa5}\x{3002} ]+$`)},
	{"lower", regexp.MustCompile(`^[a-z]+$`)},
	{"upper", regexp.MustCompile(`^[A-Z]+$`)},
	{"capital", regexp.MustCompile(`^[A-Z][a-z]+$`)},
	{"words", regexp.MustCompile(`^[A-Z][a-z]*( [A-Z][a-z]*)+$`)},
	{"sentence", regexp.MustCompile(`^[A-Z][a-z ]*\.( [A-Z][a-z ]*\.)*$`)},
	{"alnum", regexp.MustCompile(`^[A-Za-z0-9]+$`)},
}

func classify(s string) string {
	for _, c := range strClasses {
		if c.re.MatchString(s) {
			return c.name
		}
	}
	return "other"
}

// quantiles 取 5% 和 95% 分位数（按排名取），与 run-mockjs.js 相同。
func quantiles(list []float64) []float64 {
	if len(list) == 0 {
		return nil
	}
	s := slices.Clone(list)
	slices.Sort(s)
	at := func(q float64) float64 { return s[min(len(s)-1, int(math.Floor(q*float64(len(s)-1))))] }
	return []float64{at(0.05), at(0.95)}
}

func walk(paths map[string]*pathStats, v any, path string) {
	st := paths[path]
	if st == nil {
		st = &pathStats{Types: map[string]int{}, Classes: map[string]int{}, Keys: map[string]int{}}
		paths[path] = st
	}
	st.N++
	switch x := v.(type) {
	case nil:
		st.Types["null"]++
	case json.Number:
		st.Types["number"]++
		st.NumCount++
		f, _ := x.Float64()
		if f == math.Trunc(f) {
			st.IntCount++
		}
		st.Num = append(st.Num, f)
	case string:
		st.Types["string"]++
		st.Classes[classify(x)]++
		st.Len = append(st.Len, float64(utf8.RuneCountInString(x)))
	case bool:
		st.Types["boolean"]++
	case []any:
		st.Types["array"]++
		st.Arr = append(st.Arr, float64(len(x)))
		for _, item := range x {
			walk(paths, item, path+"[]")
		}
	case *jsjson.Object:
		st.Types["object"]++
		for _, k := range x.Keys() {
			st.Keys[k] = 1
			val, _ := x.Get(k)
			walk(paths, val, path+"."+k)
		}
	default:
		st.Types[fmt.Sprintf("%T", v)]++
	}
}

func TestCrossCheck(t *testing.T) {
	var cases []struct {
		Name      string          `json:"name"`
		Tpl       json.RawMessage `json:"tpl"`
		SkipRange []string        `json:"skipRange"`
	}
	readJSON(t, "../../testdata/mockjs/cases.json", &cases)
	var want map[string]caseStats
	readJSON(t, "../../testdata/mockjs/mockjs.stats.json", &want)

	query := jsjson.NewObject("id", "7", "tags", []any{"a", "b"})
	body := jsjson.NewObject("name", "tom", "nested", jsjson.NewObject("x", json.Number("1")))
	params := jsjson.NewObject("id", "7", "tags", []any{"a", "b"}, "name", "tom", "nested", jsjson.NewObject("x", json.Number("1")))
	vars := jsjson.NewObject("query", query, "body", body, "params", params)

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			got := caseStats{Paths: map[string]*pathStats{}}
			for range samples {
				tpl, err := jsjson.Parse(string(c.Tpl))
				if err != nil {
					t.Fatal(err)
				}
				v, err := Extra(tpl, vars)
				if err == nil {
					v, err = Mock(v)
				}
				if err != nil {
					got.Error = err.Error()
					break
				}
				walk(got.Paths, v, "$")
			}
			for _, st := range got.Paths {
				st.Num, st.Len, st.Arr = quantiles(st.Num), quantiles(st.Len), quantiles(st.Arr)
			}
			w := want[c.Name]
			if (w.Error != "") != (got.Error != "") {
				t.Fatalf("原版出错 %q，这里出错 %q", w.Error, got.Error)
			}
			if w.Error != "" {
				return
			}
			for _, d := range compareStats(w.Paths, got.Paths, c.SkipRange) {
				t.Error(d)
			}
		})
	}
}

func readJSON(t *testing.T, file string, v any) {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
}

// compareStats 比较两份统计，返回差异。频率 ≥ 10% 的类型或类别在另一边至少要有 2%；范围的两端相差不超过跨度的 20%。
func compareStats(want, got map[string]*pathStats, skipRange []string) []string {
	var diffs []string
	for _, p := range unionKeys(want, got) {
		w, g := want[p], got[p]
		switch {
		case w == nil:
			diffs = append(diffs, p+"：原版没有这个字段")
			continue
		case g == nil:
			diffs = append(diffs, p+"：缺少这个字段")
			continue
		}
		if math.Abs(float64(w.N-g.N)) > 0.2*float64(max(w.N, g.N))+5 {
			diffs = append(diffs, fmt.Sprintf("%s：出现次数 原版 %d，这里 %d", p, w.N, g.N))
		}
		diffs = append(diffs, compareFreq(p+" 类型", w.Types, w.N, g.Types, g.N)...)
		strW, strG := w.Types["string"], g.Types["string"]
		diffs = append(diffs, compareFreq(p+" 字符串类别", w.Classes, strW, g.Classes, strG)...)
		// skipRange 列出不比较范围的字段：随时间变化的值（@timestamp），以及分布在分位数附近有跳变的值（区县名的长度）
		if !slices.Contains(skipRange, p) {
			diffs = append(diffs, compareRange(p+" 数值", w.Num, g.Num)...)
			diffs = append(diffs, compareRange(p+" 字符串长度", w.Len, g.Len)...)
		}
		diffs = append(diffs, compareRange(p+" 数组长度", w.Arr, g.Arr)...)
		if (w.NumCount > 0 && w.IntCount == w.NumCount) != (g.NumCount > 0 && g.IntCount == g.NumCount) {
			diffs = append(diffs, fmt.Sprintf("%s：整数 原版 %d/%d，这里 %d/%d", p, w.IntCount, w.NumCount, g.IntCount, g.NumCount))
		}
		if wk, gk := sortedKeys(w.Keys), sortedKeys(g.Keys); !slices.Equal(wk, gk) {
			diffs = append(diffs, fmt.Sprintf("%s：键 原版 %v，这里 %v", p, wk, gk))
		}
	}
	return diffs
}

func compareFreq(what string, w map[string]int, wn int, g map[string]int, gn int) []string {
	var diffs []string
	freq := func(m map[string]int, k string, n int) float64 {
		if n == 0 {
			return 0
		}
		return float64(m[k]) / float64(n)
	}
	for _, k := range unionKeys(w, g) {
		fw, fg := freq(w, k, wn), freq(g, k, gn)
		if (fw >= 0.1 && fg < 0.02) || (fg >= 0.1 && fw < 0.02) {
			diffs = append(diffs, fmt.Sprintf("%s %s：原版 %.0f%%，这里 %.0f%%（原版 %v，这里 %v）", what, k, fw*100, fg*100, w, g))
		}
	}
	return diffs
}

func compareRange(what string, w, g []float64) []string {
	if (w == nil) != (g == nil) {
		return []string{fmt.Sprintf("%s：原版 %v，这里 %v", what, w, g)}
	}
	if w == nil {
		return nil
	}
	span := math.Max(math.Max(w[1]-w[0], g[1]-g[0]), 1)
	if math.Abs(w[0]-g[0]) > 0.2*span+1 || math.Abs(w[1]-g[1]) > 0.2*span+1 {
		return []string{fmt.Sprintf("%s：原版 %v，这里 %v", what, w, g)}
	}
	return nil
}

func unionKeys[V any](a, b map[string]V) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range []map[string]V{a, b} {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Fields(strings.Join(out, " "))
}
