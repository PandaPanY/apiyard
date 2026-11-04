package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// download 请求导出接口，返回状态码、Content-Type 和响应体。
func (c *client) download(path string) (string, string) {
	c.env.t.Helper()
	resp, err := c.http.Get(c.env.srv.URL + path)
	if err != nil {
		c.env.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	return resp.Header.Get("Content-Type"), string(data)
}

func TestExportHTML(t *testing.T) {
	e := newInterfaceEnv(t)
	e.addInterface(e.admin, "获取<用户>", "/users", map[string]any{"desc": "<p>说明</p>", "res_body": "{\"a\":1}"})

	ct, page := e.admin.download("/api/plugin/export?type=html&status=all&pid=" + q(e.pid))
	if ct != "application/octet-stream" {
		t.Errorf("Content-Type = %s", ct)
	}
	for _, want := range []string{
		"<!DOCTYPE html>", `<div class="table-of-contents">`, `href="#获取&lt;用户&gt;` + q(e.cat) + `"`,
		"<p>说明</p>", "<pre><code class=\"language-javascript\">{&quot;a&quot;:1}", ".table-of-contents",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("HTML 中缺少 %q", want)
		}
	}
	// 不传 type 时默认导出 HTML
	if _, page := e.admin.download("/api/plugin/export?pid=" + q(e.pid)); !strings.HasPrefix(page, "<!DOCTYPE html>") {
		t.Error("默认应导出 HTML")
	}
}

func TestExportPermissions(t *testing.T) {
	e := newOpenEnv(t)
	e.addInterface(e.admin, "x", "/x", nil)

	// 与 YApi 不同：私有项目要求查看权限（原版任何登录用户都能导出）
	expectCode(t, e.outside.call("GET", "/api/plugin/export?type=json&pid="+q(e.pid), nil), 406)
	expectCode(t, e.outside.call("GET", "/api/plugin/exportSwagger?type=OpenAPIV2&pid="+q(e.pid), nil), 406)
	if _, body := e.dev.download("/api/plugin/export?type=json&pid=" + q(e.pid)); !strings.HasPrefix(body, "[") {
		t.Errorf("分组开发者应能导出: %s", body)
	}

	// token 的 pid 被覆盖为 token 所属项目，导不出其他项目
	adminUID := e.admin.call("GET", "/api/user/status", nil)["data"].(map[string]any)["_id"]
	tok := tokenFor(t, adminUID, e.rawQ)
	_, body := e.anon.download("/api/plugin/export?type=json&pid=" + q(e.pid) + "&token=" + tok)
	var cats []map[string]any
	if err := json.Unmarshal([]byte(body), &cats); err != nil || len(cats) != 1 ||
		cats[0]["list"].([]any)[0].(map[string]any)["title"] != "q" {
		t.Errorf("用 Q 的 token 应只导出 Q: %s", body)
	}
}

func TestExportInvalidBodies(t *testing.T) {
	e := newInterfaceEnv(t)
	// 请求体声明为 JSON 但内容不是合法 JSON：原版 Swagger 导出整体失败（502），Markdown 按原文放进代码块
	e.addInterface(e.admin, "坏的", "/bad", map[string]any{"req_body_type": "json", "req_body_other": "{bad"})
	expectCode(t, e.admin.call("GET", "/api/plugin/exportSwagger?type=OpenAPIV2&pid="+q(e.pid), nil), 502)
	if _, md := e.admin.download("/api/plugin/export?type=markdown&pid=" + q(e.pid)); !strings.Contains(md, "```javascript\n{bad\n```") {
		t.Errorf("Markdown 应原样输出请求体: %s", md)
	}
	expectCode(t, e.admin.call("GET", "/api/plugin/export?type=json&pid=99999", nil), 502)
}

func TestSchemaTableEdgeCases(t *testing.T) {
	table := &schemaTable{arraySeq: new(atomic.Int64)}
	// 不是合法 JSON 时按空对象处理；根节点为 null 时原版会出错
	if html, err := table.schemaTableHTML("{bad"); err != nil || !strings.Contains(html, "<tr key=0>") {
		t.Errorf("非法 JSON 应按空对象生成一行: %v %s", err, html)
	}
	if _, err := table.schemaTableHTML("null"); err == nil {
		t.Error("根节点为 null 时应返回错误")
	}
	// 数字形式的属性名按 JS 的规则排在前面
	html, _ := table.schemaTableHTML(`{"type":"object","properties":{"b":{"type":"string"},"2":{"type":"string"},"1":{"type":"string"}}}`)
	if i1, i2, ib := strings.Index(html, "> 1</span>"), strings.Index(html, "> 2</span>"), strings.Index(html, "> b</span>"); i1 > i2 || i2 > ib {
		t.Errorf("属性顺序不对: %s", html)
	}
}

func TestJSNumber(t *testing.T) {
	cases := map[float64]string{
		0: "0", 1: "1", -1.5: "-1.5", 100: "100", 0.1: "0.1", 1e21: "1e+21", 1.5e-7: "1.5e-7", 9.9e-7: "9.9e-7",
		123456789012345680000: "123456789012345680000", 0.000001: "0.000001",
	}
	for in, want := range cases {
		if got := jsNumber(in); got != want {
			t.Errorf("jsNumber(%v) = %s, 期望 %s", in, got, want)
		}
	}
	if got := jsString([]any{"a", nil, json.Number("2"), true}); got != "a,,2,true" {
		t.Errorf("数组转字符串: %s", got)
	}
}

func TestExportNeedsLogin(t *testing.T) {
	e := newEnv(t, Options{})
	resp, err := http.Get(e.srv.URL + "/api/plugin/export?type=json&pid=11")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(data), "40011") {
		t.Errorf("未登录时应要求登录: %s", data)
	}
}
