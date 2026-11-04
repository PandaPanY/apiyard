package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/PandaPanY/apiyard/internal/store"
)

// testEnv 是一个带全新数据库的 ApiYard 接口服务，管理员为 admin@admin.com / ymfe.org。
type testEnv struct {
	t   *testing.T
	srv *httptest.Server
	st  *store.Store
}

func newEnv(t *testing.T, opts Options) *testEnv {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, _, err := Bootstrap(ctx, st, "admin@admin.com", "ymfe.org"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(st, opts, slog.New(slog.NewTextHandler(io.Discard, nil))).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &testEnv{t: t, srv: srv, st: st}
}

// client 是带独立 Cookie 的调用方，相当于一个浏览器。
type client struct {
	env  *testEnv
	http *http.Client
}

func (e *testEnv) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{env: e, http: &http.Client{Jar: jar}}
}

// call 发送请求并解析 JSON 响应。
func (c *client) call(method, path string, body any) map[string]any {
	c.env.t.Helper()
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, c.env.srv.URL+path, reader)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		c.env.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		c.env.t.Fatalf("%s %s 的响应不是 JSON: %v", method, path, err)
	}
	return out
}

func (c *client) login(email, password string) map[string]any {
	return c.call("POST", "/api/user/login", map[string]any{"email": email, "password": password})
}

// register 注册新用户并返回其 uid；注册后该客户端即以新用户身份登录。
func (c *client) register(email string) float64 {
	c.env.t.Helper()
	r := c.call("POST", "/api/user/reg", map[string]any{"email": email, "password": "pass"})
	expectCode(c.env.t, r, 0)
	return r["data"].(map[string]any)["uid"].(float64)
}

func expectCode(t *testing.T, resp map[string]any, want float64) {
	t.Helper()
	if resp["errcode"] != want {
		t.Fatalf("errcode = %v（%v），期望 %v", resp["errcode"], resp["errmsg"], want)
	}
}

func TestValidationRunsBeforeLoginCheck(t *testing.T) {
	anon := newEnv(t, Options{}).client()

	// 与 YApi 的 createAction 一致：参数不合法时先返回 400，而不是“请登录”
	r := anon.call("POST", "/api/group/add", map[string]any{})
	expectCode(t, r, 400)
	if msg := r["errmsg"].(string); !strings.Contains(msg, "group_name") {
		t.Errorf("校验提示应指出缺少的字段: %s", msg)
	}
	expectCode(t, anon.call("POST", "/api/group/add", map[string]any{"group_name": "g"}), 40011)
	expectCode(t, anon.call("GET", "/api/group/get?id=abc", nil), 400)
}

func TestLogin(t *testing.T) {
	env := newEnv(t, Options{})
	c := env.client()

	expectCode(t, c.login("", "x"), 400)
	expectCode(t, c.login("nobody@x.com", "x"), 404)
	expectCode(t, c.login("admin@admin.com", "wrong"), 405)
	expectCode(t, c.call("GET", "/api/user/status", nil), 40011)

	r := c.login(" admin@admin.com ", "ymfe.org") // 邮箱首尾空白会被去掉
	expectCode(t, r, 0)
	if r["errmsg"] != "logout success..." {
		t.Errorf("登录成功的提示应保留 YApi 原文, 实际 %v", r["errmsg"])
	}
	expectCode(t, c.call("GET", "/api/user/status", nil), 0)

	expectCode(t, c.call("GET", "/api/user/logout", nil), 0)
	expectCode(t, c.call("GET", "/api/user/status", nil), 40011)
}

func TestSessionCookieAttributes(t *testing.T) {
	env := newEnv(t, Options{})
	login := func(proto string) []*http.Cookie {
		req, _ := http.NewRequest("POST", env.srv.URL+"/api/user/login",
			strings.NewReader(`{"email":"admin@admin.com","password":"ymfe.org"}`))
		req.Header.Set("Content-Type", "application/json")
		if proto != "" {
			req.Header.Set("X-Forwarded-Proto", proto)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.Cookies()
	}

	cookies := login("")
	if len(cookies) != 2 {
		t.Fatalf("应设置 2 个登录 Cookie, 实际 %d 个", len(cookies))
	}
	for _, ck := range cookies {
		if !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Secure {
			t.Errorf("HTTP 访问时 Cookie 应为 HttpOnly + SameSite=Lax、不带 Secure: %+v", ck)
		}
	}
	for _, ck := range login("https") {
		if !ck.Secure {
			t.Errorf("经 HTTPS 反向代理访问时 Cookie 应带 Secure: %+v", ck)
		}
	}
}

func TestTamperedCookieIsRejected(t *testing.T) {
	env := newEnv(t, Options{})
	c := env.client()
	expectCode(t, c.login("admin@admin.com", "ymfe.org"), 0)

	// 把 _yapi_uid 改成别的用户，令牌里的 uid 对不上，应视为未登录
	base, _ := url.Parse(env.srv.URL)
	c.http.Jar.SetCookies(base, []*http.Cookie{{Name: uidCookie, Value: "12", Path: "/"}})
	expectCode(t, c.call("GET", "/api/user/status", nil), 40011)
}

func TestRegister(t *testing.T) {
	env := newEnv(t, Options{})
	c := env.client()
	c.register("dev@apiyard.test")

	r := c.call("GET", "/api/user/status", nil)
	if r["data"].(map[string]any)["username"] != "dev" {
		t.Errorf("未填用户名时应取邮箱 @ 前的部分: %v", r["data"])
	}
	expectCode(t, c.call("GET", "/api/group/get_mygroup", nil), 0)
	expectCode(t, env.client().call("POST", "/api/user/reg", map[string]any{"email": "dev@apiyard.test", "password": "x"}), 401)
	expectCode(t, env.client().call("POST", "/api/user/reg", map[string]any{"email": "a@b.c"}), 400)

	closed := newEnv(t, Options{CloseRegister: true}).client()
	expectCode(t, closed.call("POST", "/api/user/reg", map[string]any{"email": "a@b.c", "password": "x"}), 400)
	if closed.call("GET", "/api/user/status", nil)["canRegister"] != false {
		t.Error("关闭注册后 canRegister 应为 false")
	}
}

func TestUserSearch(t *testing.T) {
	c := newEnv(t, Options{}).client()
	c.login("admin@admin.com", "ymfe.org")

	r := c.call("GET", "/api/user/search", nil)
	expectCode(t, r, 400)
	if _, ok := r["data"]; ok {
		t.Error("没有关键词时 YApi 的响应不含 data 字段")
	}
	expectCode(t, c.call("GET", "/api/user/search?q=a%2B", nil), 400) // 含 +
	if list := c.call("GET", "/api/user/search?q=ADMIN", nil)["data"].([]any); len(list) != 1 {
		t.Errorf("搜索应不区分大小写, 结果 %v", list)
	}
}

func TestGroupPermissions(t *testing.T) {
	env := newEnv(t, Options{})
	admin := env.client()
	admin.login("admin@admin.com", "ymfe.org")
	dev := env.client()
	devUID := dev.register("dev@apiyard.test")

	r := admin.call("POST", "/api/group/add", map[string]any{"group_name": "后端组"}) // 不传 owner_uids 也能创建
	expectCode(t, r, 0)
	gid := r["data"].(map[string]any)["_id"]
	expectCode(t, admin.call("POST", "/api/group/add", map[string]any{"group_name": "后端组", "owner_uids": []any{}}), 401)
	expectCode(t, admin.call("POST", "/api/group/add_member",
		map[string]any{"id": gid, "member_uids": []any{devUID}, "role": "dev"}), 0)

	// 开发者不能修改、删除分组，也不能加人（加人限制是 ApiYard 新增的，YApi 原版没有检查）
	expectCode(t, dev.call("POST", "/api/group/up", map[string]any{"id": gid, "group_name": "x"}), 405)
	expectCode(t, dev.call("POST", "/api/group/del", map[string]any{"id": gid}), 401)
	expectCode(t, dev.call("POST", "/api/group/add_member", map[string]any{"id": gid, "member_uids": []any{11}}), 405)
	if role := dev.call("GET", "/api/group/get?id="+num(gid), nil)["data"].(map[string]any)["role"]; role != "dev" {
		t.Errorf("开发者查看分组时 role 应为 dev, 实际 %v", role)
	}

	// 已是成员、用户不存在的情况分别归类
	r = admin.call("POST", "/api/group/add_member", map[string]any{"id": gid, "member_uids": []any{devUID, 999}})
	data := r["data"].(map[string]any)
	if len(data["exist_members"].([]any)) != 1 || len(data["no_members"].([]any)) != 1 || len(data["add_members"].([]any)) != 0 {
		t.Errorf("成员归类不对: %v", data)
	}

	// 角色没有变化时 nModified 为 0，与 MongoDB 一致
	r = admin.call("POST", "/api/group/change_member_role", map[string]any{"id": gid, "member_uid": devUID, "role": "dev"})
	if r["data"].(map[string]any)["nModified"] != float64(0) {
		t.Errorf("角色未变时 nModified 应为 0: %v", r["data"])
	}
	expectCode(t, admin.call("POST", "/api/group/change_member_role", map[string]any{"id": gid, "member_uid": 999}), 400)
	expectCode(t, admin.call("POST", "/api/group/del_member", map[string]any{"id": gid, "member_uid": 999}), 400)
}

func TestGroupGetNotFound(t *testing.T) {
	env := newEnv(t, Options{})
	c := env.client()
	c.login("admin@admin.com", "ymfe.org")
	resp, err := c.http.Get(env.srv.URL + "/api/group/get?id=999")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("分组不存在时 YApi 返回 404, 实际 %d", resp.StatusCode)
	}
	expectCode(t, c.call("GET", "/api/group/get_member_list?id=999", nil), 40011) // YApi 在这里抛异常
}

func TestAvatar(t *testing.T) {
	png := []byte("\x89PNG-default")
	env := newEnv(t, Options{WebFS: fstest.MapFS{"image/avatar.png": {Data: png}}})
	resp, err := http.Get(env.srv.URL + "/api/user/avatar?uid=11")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !bytes.Equal(body, png) || resp.Header.Get("Content-Type") != "image/png" {
		t.Errorf("没有上传头像时应返回默认头像: %q %s", body, resp.Header.Get("Content-Type"))
	}

	noWeb := newEnv(t, Options{})
	resp, err = http.Get(noWeb.srv.URL + "/api/user/avatar?uid=11")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("没有前端文件时默认头像应返回 404, 实际 %d", resp.StatusCode)
	}
}

func TestBootstrap(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	created, password, err := Bootstrap(ctx, st, "root@example.com", "")
	if err != nil || !created || len(password) < 8 {
		t.Fatalf("首次启动应创建管理员并生成随机密码: %v %q %v", created, password, err)
	}
	if created, _, _ := Bootstrap(ctx, st, "root@example.com", ""); created {
		t.Error("已有用户时不应再次创建管理员")
	}
	u, _ := st.UserByEmail(ctx, "root@example.com")
	if u.Role != "admin" {
		t.Errorf("管理员角色不对: %+v", u)
	}
	if _, err := st.PrivateGroupOf(ctx, u.ID); err == nil {
		t.Error("与 YApi 一样，创建管理员时不应创建个人空间")
	}
}

// num 把 JSON 解码得到的数字 ID 转成查询参数用的文本。
func num(v any) string {
	return strconv.FormatFloat(v.(float64), 'f', -1, 64)
}
