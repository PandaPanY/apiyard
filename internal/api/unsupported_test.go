package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
)

func TestUnsupportedEndpoints(t *testing.T) {
	zip := []byte("PK\x03\x04fake")
	e := newEnv(t, Options{WebFS: fstest.MapFS{"attachment/cross-request.zip": {Data: zip}}})
	anon, admin := e.client(), e.client()
	admin.login("admin@admin.com", "ymfe.org")

	r := anon.call("POST", "/api/user/login_by_ldap", map[string]any{"email": "a", "password": "b"})
	if r["errcode"] != float64(401) || !strings.Contains(r["errmsg"].(string), "LDAP") {
		t.Errorf("LDAP 登录应提示不支持: %v", r)
	}
	expectCode(t, admin.call("GET", "/api/project/swagger_url?url=http://127.0.0.1/", nil), 402)
	expectCode(t, anon.call("GET", "/api/project/swagger_url?url=http://127.0.0.1/", nil), 40011)

	// 第三方登录跳回首页
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noFollow.Get(e.srv.URL + "/api/user/login_by_token")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Errorf("login_by_token 应跳转到首页: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}

	// 浏览器插件需要登录后下载
	expectCode(t, anon.call("GET", "/api/interface/download_crx", nil), 40011)
	resp, err = admin.http.Get(e.srv.URL + "/api/interface/download_crx")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	if string(data) != string(zip) || resp.Header.Get("Content-Type") != "application/zip" ||
		!strings.Contains(resp.Header.Get("Content-Disposition"), "crossRequest.zip") {
		t.Errorf("插件下载不对: %s %v", resp.Header, data)
	}
}

func TestListByUpdate(t *testing.T) {
	e := newInterfaceEnv(t)
	expectCode(t, e.admin.call("POST", "/api/project/up", map[string]any{"id": e.pid, "basepath": "/v1"}), 0)
	id := e.addInterface(e.admin, "x", "/x", nil)
	expectCode(t, e.admin.call("POST", "/api/interface/up", map[string]any{"id": id, "title": "x2"}), 0)

	body := map[string]any{"type": "project", "typeid": e.pid, "apis": []any{
		map[string]any{"method": "GET", "path": "/v1/x"},
		map[string]any{"path": "/x"}, // 没有 method 时匹配任意方法
	}}
	r := e.dev.call("POST", "/api/log/list_by_update", body)
	expectCode(t, r, 0)
	if list := r["data"].([]any); len(list) != 2 {
		t.Errorf("应找到两次同一条修改动态: %v", list)
	} else if _, ok := list[0].(map[string]any)["data"]; ok {
		t.Errorf("结果不应包含改动数据: %v", list[0])
	}
	// 与 YApi 不同：私有项目要求查看权限
	expectCode(t, e.outside.call("POST", "/api/log/list_by_update", body), 406)
}
