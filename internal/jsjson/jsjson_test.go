package jsjson

import (
	"encoding/json"
	"testing"
)

func TestFormatNumber(t *testing.T) {
	cases := map[float64]string{
		0: "0", 1: "1", -1.5: "-1.5", 100: "100", 0.1: "0.1", 1e21: "1e+21", 1.5e-7: "1.5e-7", 9.9e-7: "9.9e-7",
		123456789012345680000: "123456789012345680000", 0.000001: "0.000001",
	}
	for in, want := range cases {
		if got := FormatNumber(in); got != want {
			t.Errorf("FormatNumber(%v) = %s, 期望 %s", in, got, want)
		}
	}
	if got := String([]any{"a", nil, json.Number("2"), true}); got != "a,,2,true" {
		t.Errorf("数组转字符串: %s", got)
	}
}

func TestParseKeepsOrder(t *testing.T) {
	v, err := Parse(`{"b":1,"a":{"y":true,"x":[1.0,"s"]},"2":null,"1":2,"b":3}`)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := Marshal(NormalizeNumbers(v), "")
	// 整数键排在前面；重复的 b 保留第一次的位置、最后一次的值；1.0 按 JS 规则输出为 1
	if string(out) != `{"1":2,"2":null,"b":3,"a":{"y":true,"x":[1,"s"]}}` {
		t.Errorf("输出不对: %s", out)
	}
	if _, err := Parse(`{"a":1} x`); err == nil {
		t.Error("多余内容应报错")
	}
}
