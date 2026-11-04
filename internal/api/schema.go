package api

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/PandaPanY/apiyard/internal/schemafake"
)

// interfaceSchema2JSON 按 JSON Schema 生成示例数据，直接把结果作为响应体（不包 errcode），与 YApi 一致。
func (a *API) interfaceSchema2JSON(c *call) error {
	opts := schemafake.Options{AlwaysFakeOptionals: true}
	if v, ok := c.params["required"]; ok {
		b, _ := castBool(v)
		opts.AlwaysFakeOptionals = b
	}
	value, err := schemafake.Generate(c.params["schema"], opts)
	if err != nil {
		// json-schema-faker 出错时 YApi 把错误信息作为纯文本返回
		c.replied = true
		c.w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, err = c.w.Write([]byte(err.Error()))
		return err
	}
	return c.replyValue(value)
}

// replyValue 把任意值作为 JSON 响应体输出（不转义 HTML）。
func (c *call) replyValue(v any) error {
	data, err := encodeJSON(v, "")
	if err != nil {
		return err
	}
	c.replied = true
	c.w.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.w.WriteHeader(http.StatusOK)
	_, err = c.w.Write(data)
	return err
}

// encodeJSON 序列化为 JSON 且不转义 <>&；indent 非空时按其缩进（相当于 JSON.stringify(v, null, indent)）。
func encodeJSON(v any, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent("", indent)
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// mergeResBodySchema 对应 YApi save 接口的 dataSync=good：把新的响应 JSON Schema 合并到原有的上面，
// 保留原有字段上的描述、Mock 规则等。任何一边不是 JSON 时保持新内容不变。
// 合并结果按键名排序输出，与 YApi 保留原有键顺序的输出在文本上可能不同。
func mergeResBodySchema(old *string, newBody any) any {
	text, ok := newBody.(string)
	if !ok || old == nil {
		return newBody
	}
	var src, merge any
	if json.Unmarshal([]byte(*old), &src) != nil || json.Unmarshal([]byte(text), &merge) != nil {
		return newBody
	}
	data, err := encodeJSON(mergeSchema(src, merge), "  ")
	if err != nil {
		return newBody
	}
	return string(data)
}

// mergeSchema 与 YApi common/mergeJsonSchema.js 相同：以 merge 为准，source 中有、merge 中没有的字段保留。
func mergeSchema(source, merge any) any {
	src, ok1 := source.(map[string]any)
	m, ok2 := merge.(map[string]any)
	if !ok1 || !ok2 {
		return merge
	}
	result := make(map[string]any, len(src)+len(m))
	for k, v := range src {
		result[k] = v
	}
	for k, v := range m {
		result[k] = v
	}
	// merge 没有 properties/items 时 JS 里结果为 undefined，序列化时该字段消失
	setOrDrop := func(key string, v any) {
		if _, ok := m[key]; ok {
			result[key] = v
		} else {
			delete(result, key)
		}
	}
	switch m["type"] {
	case "object":
		setOrDrop("properties", mergeProperties(src["properties"], m["properties"]))
	case "array":
		setOrDrop("items", mergeSchema(src["items"], m["items"]))
	}
	return result
}

func mergeProperties(source, merge any) any {
	src, ok1 := source.(map[string]any)
	m, ok2 := merge.(map[string]any)
	if !ok1 || !ok2 {
		return merge
	}
	for k, v := range m {
		m[k] = mergeSchema(src[k], v)
	}
	return m
}
