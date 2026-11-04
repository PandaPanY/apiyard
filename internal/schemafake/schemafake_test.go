package schemafake

import (
	"encoding/json"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func generate(t *testing.T, schema string, opts Options) any {
	t.Helper()
	var s any
	dec := json.NewDecoder(strings.NewReader(schema))
	dec.UseNumber() // 与接口层解析请求体的方式一致
	if err := dec.Decode(&s); err != nil {
		t.Fatal(err)
	}
	v, err := Generate(s, opts)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestGenerateObject(t *testing.T) {
	v := generate(t, `{
		"type": "object",
		"required": ["id", "email"],
		"properties": {
			"id": {"type": "integer", "minimum": 10, "maximum": 20},
			"email": {"type": "string", "format": "email"},
			"status": {"type": "string", "enum": ["on"]},
			"price": {"type": ["number", "null"], "minimum": 1, "maximum": 2},
			"when": {"type": "string", "format": "date-time"},
			"name": {"type": "string", "minLength": 3, "maxLength": 5},
			"fixed": {"type": "string", "mock": {"mock": "固定值"}}
		}
	}`, Options{AlwaysFakeOptionals: true}).(map[string]any)

	if id := v["id"].(int64); id < 10 || id > 20 {
		t.Errorf("id 超出范围: %d", id)
	}
	if _, err := mail.ParseAddress(v["email"].(string)); err != nil {
		t.Errorf("email 格式不对: %v", v["email"])
	}
	if v["status"] != "on" || v["fixed"] != "固定值" {
		t.Errorf("enum 或 mock 文本没有生效: %v", v)
	}
	if p := v["price"].(float64); p < 1 || p > 2 {
		t.Errorf("price 超出范围: %v", p)
	}
	if _, err := time.Parse(time.RFC3339, v["when"].(string)); err != nil {
		t.Errorf("date-time 格式不对: %v", v["when"])
	}
	if n := len(v["name"].(string)); n < 3 || n > 5 {
		t.Errorf("name 长度不对: %q", v["name"])
	}
}

func TestGenerateOptionalFields(t *testing.T) {
	schema := `{"type": "object", "required": ["a"], "properties": {"a": {"const": 1}, "b": {"const": 2}}}`
	for range 20 {
		v := generate(t, schema, Options{}).(map[string]any)
		if _, ok := v["a"]; !ok {
			t.Fatal("必填字段必须生成")
		}
	}
	if v := generate(t, schema, Options{AlwaysFakeOptionals: true}).(map[string]any); len(v) != 2 {
		t.Errorf("AlwaysFakeOptionals 时应生成全部字段: %v", v)
	}
}

func TestGenerateArrayAndRefs(t *testing.T) {
	v := generate(t, `{
		"type": "array", "minItems": 3, "maxItems": 3,
		"items": {"$ref": "#/definitions/node"},
		"definitions": {"node": {"allOf": [
			{"type": "object", "required": ["x"], "properties": {"x": {"const": "x"}}},
			{"required": ["y"], "properties": {"y": {"const": "y"}}}
		]}}
	}`, Options{}).([]any)
	if len(v) != 3 {
		t.Fatalf("数组长度应为 3: %v", v)
	}
	if item := v[0].(map[string]any); item["x"] != "x" || item["y"] != "y" {
		t.Errorf("allOf 应合并两个 schema 的字段: %v", item)
	}
}

func TestGenerateRecursiveRef(t *testing.T) {
	// 自引用的 schema 不能无限递归
	v := generate(t, `{"type": "object", "required": ["child"], "properties": {"child": {"$ref": "#"}}}`, Options{})
	if v == nil {
		t.Fatal("应生成结果")
	}
}

func TestGenerateInvalid(t *testing.T) {
	if _, err := Generate("not a schema", Options{}); err == nil {
		t.Error("schema 不是对象时应报错")
	}
}
