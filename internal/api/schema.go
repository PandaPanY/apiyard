package api

import (
	"net/http"

	"github.com/PandaPanY/apiyard/internal/genbudget"
	"github.com/PandaPanY/apiyard/internal/jsjson"
	"github.com/PandaPanY/apiyard/internal/schemafake"
)

// interfaceSchema2JSON 按 JSON Schema 生成示例数据，直接把结果作为响应体（不包 errcode），与 YApi 一致。
func (a *API) interfaceSchema2JSON(c *call) error {
	release, ok := genbudget.Acquire(c.r.Context())
	if !ok {
		http.Error(c.w, "Mock generation busy", http.StatusTooManyRequests)
		c.replied = true
		return nil
	}
	defer release()
	opts := schemafake.Options{AlwaysFakeOptionals: true}
	if v, ok := c.params["required"]; ok {
		b, _ := castBool(v)
		opts.AlwaysFakeOptionals = b
	}
	value, err := schemafake.GenerateContext(c.r.Context(), c.params["schema"], opts)
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
	return jsjson.Marshal(v, indent)
}

// mergeResBodySchema 对应 YApi save 接口的 dataSync=good：把新的响应 JSON Schema 合并到原有的上面，
// 保留原有字段上的描述、Mock 规则等。与原版一样先用 json_parse（解析失败时保留原字符串）解析两边，
// 合并后按 JSON.stringify(v, null, 2) 输出，键的顺序与原版相同。
func mergeResBodySchema(old *string, newBody any) any {
	text, ok := newBody.(string)
	if !ok {
		return newBody
	}
	var src any // 原来没有响应体时相当于 undefined
	if old != nil {
		src = jsonParseOrString(*old)
	}
	data, err := jsjson.Marshal(jsjson.NormalizeNumbers(mergeSchema(src, jsonParseOrString(text))), "  ")
	if err != nil {
		return newBody
	}
	return string(data)
}

// jsonParseOrString 对应 YApi 的 json_parse：能解析就返回解析结果，否则返回原字符串。
func jsonParseOrString(s string) any {
	if v, err := jsjson.Parse(s); err == nil {
		return v
	}
	return s
}

// mergeSchema 与 YApi common/mergeJsonSchema.js 相同：以 merge 为准，source 中有、merge 中没有的字段保留。
func mergeSchema(source, merge any) any {
	src, ok1 := source.(*jsjson.Object)
	m, ok2 := merge.(*jsjson.Object)
	if !ok1 || !ok2 {
		return merge
	}
	result := src.Clone() // Object.assign({}, source, merge)
	for _, k := range m.Keys() {
		v, _ := m.Get(k)
		result.Set(k, v)
	}
	// merge 没有 properties/items 时 JS 里结果为 undefined，序列化时该字段消失
	setOrDrop := func(key string, v any) {
		if _, ok := m.Get(key); ok {
			result.Set(key, v)
		} else {
			result.Delete(key)
		}
	}
	srcProps, _ := src.Get("properties")
	mProps, _ := m.Get("properties")
	srcItems, _ := src.Get("items")
	mItems, _ := m.Get("items")
	switch t, _ := m.Get("type"); t {
	case "object":
		setOrDrop("properties", mergeProperties(srcProps, mProps))
	case "array":
		setOrDrop("items", mergeSchema(srcItems, mItems))
	}
	return result
}

func mergeProperties(source, merge any) any {
	src, ok1 := source.(*jsjson.Object)
	m, ok2 := merge.(*jsjson.Object)
	if !ok1 || !ok2 {
		return merge
	}
	for _, k := range m.Keys() {
		sv, _ := src.Get(k)
		mv, _ := m.Get(k)
		m.Set(k, mergeSchema(sv, mv))
	}
	return m
}
