package mock

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PandaPanY/apiyard/internal/store"
)

// 行为与原版一致的部分由兼容场景 10-mock 覆盖，这里测与原版刻意不同的地方和兼容场景测不到的边界。

type env struct {
	t   *testing.T
	st  *store.Store
	srv *httptest.Server
	p   *store.Project
	cat *store.Cat
}

func newEnv(t *testing.T, script string) *env {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "mock.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	g := &store.Group{UID: 11, Name: "g", Type: "public"}
	if err := st.CreateGroup(ctx, g, nil); err != nil {
		t.Fatal(err)
	}
	basepath := "/api"
	p := &store.Project{UID: 11, GroupID: g.ID, Name: "p", Basepath: &basepath, ProjectType: "public", Env: "[]", Tag: "[]",
		IsMockOpen: script != "", ProjectMockScript: &script}
	cat := &store.Cat{UID: 11, Name: "公共分类"}
	if err := st.CreateProject(ctx, p, cat, nil); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(st, slog.New(slog.DiscardHandler)))
	t.Cleanup(srv.Close)
	return &env{t: t, st: st, srv: srv, p: p, cat: cat}
}

func (e *env) add(path, resType, resBody string, schema bool) {
	e.t.Helper()
	typ := "static"
	if strings.Contains(path, "{") {
		typ = "var"
	}
	it := &store.Interface{UID: 11, ProjectID: e.p.ID, CatID: e.cat.ID, Title: path, Path: path, Method: "GET",
		Status: "done", Type: typ, QueryPath: `{"path":"` + path + `","params":[]}`, ReqQuery: "[]", ReqHeaders: "[]",
		ReqParams: "[]", ReqBodyForm: "[]", Tag: "[]", ResBodyType: &resType, ResBody: &resBody, ResBodyIsJSONSchema: schema}
	if err := e.st.CreateInterface(context.Background(), it); err != nil {
		e.t.Fatal(err)
	}
}

// reply 是一次 Mock 请求的结果。
type reply struct {
	status int
	header http.Header
}

func (e *env) get(path string) (reply, string) {
	e.t.Helper()
	resp, err := http.Get(e.srv.URL + "/mock/" + jsonID(e.p.ID) + path)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	return reply{resp.StatusCode, resp.Header}, string(data)
}

func jsonID(id int64) string {
	data, _ := json.Marshal(id)
	return string(data)
}

// YApi 已公开的远程代码执行利用方式：在项目的全局 Mock 脚本里逃逸沙箱。ApiYard 不执行脚本，只在响应头里提示。
func TestMockScriptIsNotExecuted(t *testing.T) {
	script := `const sandbox = this; const ObjectConstructor = this.constructor; const FunctionConstructor = ObjectConstructor.constructor;
const myfun = FunctionConstructor('return process'); const process = myfun();
mockJson = process.mainModule.require("child_process").execSync("id && touch /tmp/pwned").toString()`
	e := newEnv(t, script)
	e.add("/users", "json", `{"name": "tom"}`, false)
	resp, body := e.get("/api/users")
	if body != `{"name":"tom"}` {
		t.Errorf("脚本不应改变 Mock 数据: %s", body)
	}
	if resp.header.Get("X-ApiYard-Mock-Script") != "ignored" {
		t.Error("应在响应头里提示脚本被忽略")
	}
}

// 原版用 eval 解析 Mock.js 占位符的参数，模板本身就能执行代码，不需要 Mock 脚本。
func TestPlaceholderParamsAreNotEvaluated(t *testing.T) {
	e := newEnv(t, "")
	e.add("/v", "json", `{"v": "@pick([process.version])", "x": "@pick(require('child_process').execSync('id').toString())"}`, false)
	_, body := e.get("/api/v")
	if strings.Contains(body, "v1") || strings.Contains(body, "uid=") {
		t.Errorf("不应执行参数里的代码: %s", body)
	}
}

// 原版的 json-schema-faker 有缺陷：非必填字段随机缺失、没有 maxItems 的数组总是空的。ApiYard 按原版的设置生成全部字段。
func TestSchemaMockFillsEverything(t *testing.T) {
	e := newEnv(t, "")
	e.add("/s", "json", `{"type":"object","properties":{"a":{"type":"string"},"list":{"type":"array","items":{"type":"integer"}}}}`, true)
	for range 20 {
		_, body := e.get("/api/s")
		var v struct {
			A    *string `json:"a"`
			List []int   `json:"list"`
		}
		if err := json.Unmarshal([]byte(body), &v); err != nil || v.A == nil || len(v.List) == 0 {
			t.Fatalf("应生成全部字段、数组不为空: %s", body)
		}
	}
}

func TestResponses(t *testing.T) {
	e := newEnv(t, "")
	e.add("/n", "json", "null", false)
	e.add("/u/{id}", "json", `{"id": "${query.id}"}`, false)
	if resp, _ := e.get("/api/n"); resp.status != http.StatusNoContent {
		t.Errorf("null 应返回 204: %d", resp.status)
	}
	if _, body := e.get("/api/u/42?x=1"); body != `{"id":"42"}` {
		t.Errorf("路径参数: %s", body)
	}
	// 跨域：没有 Origin 时不输出（原版会输出字符串 "undefined"）
	if resp, _ := e.get("/api/n"); resp.header.Get("Access-Control-Allow-Origin") != "" ||
		resp.header.Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("跨域响应头: %v", resp.header)
	}
	// 直接在浏览器里打开 Mock 地址时，返回的 HTML 与界面隔离
	if resp, _ := e.get("/api/n"); resp.header.Get("Content-Security-Policy") != "sandbox" ||
		resp.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("Mock 响应应带 CSP sandbox: %v", resp.header)
	}

	big := strings.Repeat("a", jsonLimit+1)
	resp, err := http.Post(e.srv.URL+"/mock/"+jsonID(e.p.ID)+"/api/n", "application/json", strings.NewReader(`"`+big+`"`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("请求体超过 2MB 应返回 413: %d", resp.StatusCode)
	}
}

func TestMatchAPI(t *testing.T) {
	for _, c := range []struct {
		path, rule string
		want       string // 匹配到的参数，不匹配时为 "-"
	}{
		{"/users/42", "/users/{id}", `{"id":"42"}`},
		{"/users/42/x", "/users/:id/x", `{"id":"42"}`},
		{"/files/a.json", "/files/{name}.json", `{"name":"a"}`},
		{"/files/a.txt", "/files/{name}.json", "-"},
		{"/a/b", "/a//b", "-"},
		{"/a/x/b", "/a//b", `{}`}, // 空的段不比较
		{"/users", "/users/{id}", "-"},
	} {
		got, ok := matchAPI(c.path, c.rule)
		s := "-"
		if ok {
			data, _ := json.Marshal(got)
			s = string(data)
		}
		if s != c.want {
			t.Errorf("matchAPI(%q, %q) = %s，应为 %s", c.path, c.rule, s, c.want)
		}
	}
}
