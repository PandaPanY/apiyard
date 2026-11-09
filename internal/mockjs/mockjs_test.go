package mockjs

import (
	"strings"
	"testing"

	"github.com/PandaPanY/apiyard/internal/jsjson"
)

func mockText(t *testing.T, tpl string) string {
	t.Helper()
	v, err := jsjson.ParseJSON5(tpl)
	if err != nil {
		t.Fatal(err)
	}
	if v, err = Mock(v); err != nil {
		t.Fatal(err)
	}
	out, err := jsjson.Marshal(v, "")
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestDeterministic(t *testing.T) {
	for _, c := range []struct{ tpl, want string }{
		{`{"list|5": [{"id|+2": 10}]}`, `{"list":[{"id":10},{"id":12},{"id":14},{"id":16},{"id":18}]}`},
		{`{"list|5": ["@order('a', 'b', 'c')"]}`, `{"list":["a","b","c","a","b"]}`},
		{`{"list|4": [{"v|+1": ["x", "y", "z"]}]}`, `{"list":[{"v":"x"},{"v":"y"},{"v":"z"},{"v":"x"}]}`},
		{`{"a|+1xyz": 5}`, `{"axyz":5}`}, // 规则后面多出的字符留在属性名里
		{`{"escaped": "\\@email", "unknown": "@nothing(1)", "upper": "@UPPER('ab')"}`, `{"escaped":"\\@email","unknown":"@nothing(1)","upper":"AB"}`},
		{`{"name": "tom", "hello": "hi @name"}`, `{"name":"tom","hello":"hi tom"}`},
		{`{"msg": "@name hi", "name": "@upper('x')"}`, `{"msg":"X hi","name":"X"}`}, // 引用后面的属性：先计算它
		{`{"msg": "@name!", "name": "x"}`, `{"msg":"@name!","name":"x"}`},           // ! 也是占位符名的一部分
		{`{"a": {"b": "deep"}, "c": "@/a/b", "d": {"e": "@../a/b"}}`, `{"a":{"b":"deep"},"c":"deep","d":{"e":"deep"}}`},
		{`{"n": "@integer(3, 3)", "s": "x@integer(3, 3)", "b": "@boolean(1, 0, true)"}`, `{"n":3,"s":"x3","b":true}`},
		{`{"$": "@pick(['$&'])", "q": "@lower('A$$B')"}`, `{"$":"@pick(['$&'])","q":"a$b"}`}, // replace 的 $ 规则
		{`{"2": 1, "1": 2, "b|1": [null]}`, `{"1":2,"2":1,"b":null}`},
		{`{"u": "@/missing/x"}`, `{}`}, // 引用不到时为 undefined，输出时省略
		{`{"c|2-2": "ab", "e|0": "x"}`, `{"c":"abab","e":""}`},
	} {
		if got := mockText(t, c.tpl); got != c.want {
			t.Errorf("%s\n得到 %s\n应为 %s", c.tpl, got, c.want)
		}
	}
}

// 原版用 eval 解析参数，这里只认字面量：括号里的 JS 代码不会执行，按逗号拆成普通字符串。
func TestParamsAreNotEvaluated(t *testing.T) {
	for _, tpl := range []string{
		`{"a": "@pick([process.version])"}`,
		`{"a": "@string(this.constructor.constructor('return process')().exit())"}`,
		`{"a": "@pick(require('child_process').execSync('id').toString())"}`,
	} {
		got := mockText(t, tpl)
		if strings.Contains(got, "v1") || strings.Contains(got, "uid=") {
			t.Errorf("%s 不应执行参数里的代码: %s", tpl, got)
		}
	}
	if got := mockText(t, `{"a": "@pick([process.version])"}`); got != `{"a":"[process.version]"}` {
		t.Errorf("解析不了的参数应按字符串处理: %s", got)
	}
	if got := mockText(t, `{"a": "@date(yyyy)"}`); len(got) != len(`{"a":"2024"}`) {
		t.Errorf("没加引号的参数应当作字符串: %s", got)
	}
}

func TestDecimalDoesNotHang(t *testing.T) {
	// 原版对 'h|10.0' 会一直补小数位直到内存耗尽，这里最多 17 位
	got := mockText(t, `{"h|10.0": 1}`)
	if !strings.HasPrefix(got, `{"h":10.`) {
		t.Errorf("得到 %s", got)
	}
}

func TestErrors(t *testing.T) {
	for _, tpl := range []string{`"@dataImage"`, `{"a": "@color('nope')"}`, `{"a": "@range(0, 1e9)"}`, `{"a": "@string(1e9)"}`} {
		v, _ := jsjson.ParseJSON5(tpl)
		if _, err := Mock(v); err == nil {
			t.Errorf("%s 应返回错误", tpl)
		}
	}
}

func TestExtra(t *testing.T) {
	vars := jsjson.NewObject("query", jsjson.NewObject("id", "7"), "body", "text")
	tpl, _ := jsjson.ParseJSON5(`{"a": "${query.id}", "b|regexp": "x{2}", "c": "${query.none}", "d|RegExp": "y", "e": 1}`)
	v, err := Extra(tpl, vars)
	if err != nil {
		t.Fatal(err)
	}
	if v, err = Mock(v); err != nil {
		t.Fatal(err)
	}
	out, _ := jsjson.Marshal(v, "")
	// |regexp 的属性在处理到它时移到当时的末尾；写成 |RegExp 时属性名不变，Mock.js 生成的属性名就成了 dRegExp
	if string(out) != `{"a":"7","b":"xx","c":"","dRegExp":"y","e":1}` {
		t.Errorf("得到 %s", out)
	}
	tpl, _ = jsjson.ParseJSON5(`{"a": "${body.x}"}`)
	if _, err := Extra(tpl, vars); err == nil || !strings.Contains(err.Error(), "Cannot use 'in' operator") {
		t.Errorf("对字符串取属性应与原版一样出错: %v", err)
	}
}
