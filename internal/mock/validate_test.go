package mock

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/PandaPanY/apiyard/internal/jsjson"
)

// testdata/ajv.json 是在 YApi 容器里用原版 schemaValidator（ajv 5 + ajv-i18n）得到的结果。
func TestSchemaValidator(t *testing.T) {
	data, err := os.ReadFile("testdata/ajv.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Schema  json.RawMessage `json:"schema"`
		Data    json.RawMessage `json:"data"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	// 再加上几条：在 properties 里有规则的必需属性留到后面检查，额外属性在 properties 之前检查，type 无效
	extra := `[
		{"schema": {"type":"object","required":["name","a b"],"properties":{"name":{"type":"string"}}}, "data": {}, "message": "data 应当有必需属性 ['a b']"},
		{"schema": {"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}, "data": {}, "message": "data 应当有必需属性 name"},
		{"schema": {"type":"object","properties":{"name":{"type":"string"}},"additionalProperties":false}, "data": {"x":1,"name":2}, "message": "data 不允许有额外的属性"},
		{"schema": {"type":"bogus"}, "data": {}, "message": "schema is invalid: data.type should be equal to one of the allowed values, data.type should be array, data.type should match some schema in anyOf"},
		{"schema": {"type":"object"}, "data": "str", "message": "data 应当是 object 类型"}
	]`
	var more []struct {
		Schema  json.RawMessage `json:"schema"`
		Data    json.RawMessage `json:"data"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal([]byte(extra), &more); err != nil {
		t.Fatal(err)
	}
	for _, c := range append(cases, more...) {
		schema, err := jsjson.Parse(string(c.Schema))
		if err != nil {
			t.Fatal(err)
		}
		d, err := jsjson.Parse(string(c.Data))
		if err != nil {
			t.Fatal(err)
		}
		valid, msg := schemaValidator(schema, d)
		if msg != c.Message || valid != (c.Message == "") {
			t.Errorf("schema %s\n数据 %s\n得到 %q\n应为 %q", c.Schema, c.Data, msg, c.Message)
		}
	}
}
