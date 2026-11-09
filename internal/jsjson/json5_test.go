package jsjson

import (
	"math"
	"testing"
)

func TestParseJSON5(t *testing.T) {
	text := `// 注释
	{
		unquoted: 'single',  /* 块注释 */
		"list|1-3": [1, .5, 5., +2, -0x10, 1e3,],
		$id: "a中\
b",
		"2": true, "1": null,
		dup: 1, dup: 2,
	}`
	v, err := ParseJSON5(text)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Marshal(v, "")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"1":null,"2":true,"unquoted":"single","list|1-3":[1,0.5,5,2,-16,1000],"$id":"a中b","dup":2}`
	if string(got) != want {
		t.Errorf("得到 %s\n应为 %s", got, want)
	}

	if v, _ := ParseJSON5("-Infinity"); v != math.Inf(-1) {
		t.Errorf("-Infinity: %v", v)
	}
	for _, bad := range []string{"", "{a:1", "[1 2]", "'abc", "{1a: 2}", "undefined", "@email", "{a:1} x"} {
		if _, err := ParseJSON5(bad); err == nil {
			t.Errorf("%q 应解析失败", bad)
		}
	}
}
