package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// interfaceEnv 在 projectEnv 的私有项目里准备好默认分类。
type interfaceEnv struct {
	*projectEnv
	cat float64
}

func newInterfaceEnv(t *testing.T) *interfaceEnv {
	t.Helper()
	e := &interfaceEnv{projectEnv: newProjectEnv(t)}
	r := e.admin.call("GET", "/api/project/get?id="+q(e.pid), nil)
	e.cat = r["data"].(map[string]any)["cat"].([]any)[0].(map[string]any)["_id"].(float64)
	return e
}

// addInterface 新建接口并返回其 ID。
func (e *interfaceEnv) addInterface(c *client, title, path string, extra map[string]any) float64 {
	e.env.t.Helper()
	body := map[string]any{"project_id": e.pid, "catid": e.cat, "title": title, "path": path, "method": "GET"}
	for k, v := range extra {
		body[k] = v
	}
	r := c.call("POST", "/api/interface/add", body)
	expectCode(e.env.t, r, 0)
	return r["data"].(map[string]any)["_id"].(float64)
}

func TestInterfaceCatMustBelongToProject(t *testing.T) {
	e := newInterfaceEnv(t)
	r := e.admin.call("POST", "/api/project/add", map[string]any{"name": "另一个", "group_id": e.gid})
	other := r["data"].(map[string]any)["_id"].(float64)
	otherCat := e.admin.call("GET", "/api/project/get?id="+q(other), nil)["data"].(map[string]any)["cat"].([]any)[0].(map[string]any)["_id"]

	// 与 YApi 不同：分类必须存在且属于同一个项目
	body := map[string]any{"project_id": e.pid, "catid": otherCat, "title": "t", "path": "/t", "method": "GET"}
	expectCode(t, e.admin.call("POST", "/api/interface/add", body), 400)
	id := e.addInterface(e.admin, "t", "/t", nil)
	expectCode(t, e.admin.call("POST", "/api/interface/up", map[string]any{"id": id, "catid": otherCat}), 400)
	// project_id 参数被忽略，接口不会被移到别的项目
	expectCode(t, e.admin.call("POST", "/api/interface/up", map[string]any{"id": id, "project_id": other, "title": "t2"}), 0)
	got := e.admin.call("GET", "/api/interface/get?id="+q(id), nil)["data"].(map[string]any)
	if got["project_id"] != e.pid || got["title"] != "t2" {
		t.Errorf("修改后的接口不对: %v", got)
	}
}

func TestInterfacePermissions(t *testing.T) {
	e := newInterfaceEnv(t)
	outsideUID := e.outside.call("GET", "/api/user/status", nil)["data"].(map[string]any)["_id"]

	// 分组外的用户不能新建、查看私有项目的接口
	body := map[string]any{"project_id": e.pid, "catid": e.cat, "title": "x", "path": "/x", "method": "GET"}
	expectCode(t, e.outside.call("POST", "/api/interface/add", body), 40033)
	id := e.addInterface(e.dev, "dev 的接口", "/dev", nil)
	expectCode(t, e.outside.call("GET", "/api/interface/get?id="+q(id), nil), 406)
	expectCode(t, e.outside.call("GET", "/api/interface/list_menu?project_id="+q(e.pid), nil), 406)
	// 与 YApi 不同：按接口查角色时同样检查私有项目的查看权限
	expectCode(t, e.outside.call("GET", "/api/user/project?type=interface&id="+q(id), nil), 406)

	// 项目成员可以删除自己建的接口，被移出项目后就不行了（YApi 只看是不是创建者）
	expectCode(t, e.admin.call("POST", "/api/project/add_member",
		map[string]any{"id": e.pid, "member_uids": []any{outsideUID}, "role": "dev"}), 0)
	own := e.addInterface(e.outside, "外部成员的接口", "/outside", nil)
	other := e.addInterface(e.outside, "另一个", "/outside2", nil)
	expectCode(t, e.outside.call("POST", "/api/interface/del", map[string]any{"id": own}), 0)
	expectCode(t, e.admin.call("POST", "/api/project/del_member", map[string]any{"id": e.pid, "member_uid": outsideUID}), 0)
	expectCode(t, e.outside.call("POST", "/api/interface/del", map[string]any{"id": other}), 400)
	expectCode(t, e.admin.call("POST", "/api/interface/del", map[string]any{"id": 99999}), 402)
	expectCode(t, e.admin.call("POST", "/api/interface/del_cat", map[string]any{"catid": 99999}), 400)
}

func TestInterfaceListFilters(t *testing.T) {
	e := newInterfaceEnv(t)
	e.addInterface(e.admin, "c", "/c", map[string]any{"tag": []any{"v1"}, "status": "done"})
	e.addInterface(e.admin, "a", "/a", map[string]any{"tag": []any{"v2"}})
	e.addInterface(e.admin, "b", "/b", nil)

	list := func(query string) map[string]any {
		t.Helper()
		r := e.admin.call("GET", "/api/interface/list?project_id="+q(e.pid)+query, nil)
		expectCode(t, r, 0)
		return r["data"].(map[string]any)
	}
	titles := func(d map[string]any) string {
		var out []string
		for _, item := range d["list"].([]any) {
			out = append(out, item.(map[string]any)["title"].(string))
		}
		return strings.Join(out, ",")
	}

	// limit=all 按标题排序，不分页，total 与 YApi 一样是 null
	all := list("&limit=all")
	if titles(all) != "a,b,c" || all["total"] != nil || all["count"] != float64(3) {
		t.Errorf("limit=all 的结果不对: %v", all)
	}
	if got := titles(list("&page=2&limit=2")); got != "b" {
		t.Errorf("第二页应只有按创建顺序的第三个接口, 实际 %s", got)
	}
	if got := titles(list("&tag=v1&tag=v2")); got != "c,a" {
		t.Errorf("按多个 tag 筛选的结果不对: %s", got)
	}
	if got := titles(list("&status=done")); got != "c" {
		t.Errorf("按状态筛选的结果不对: %s", got)
	}
	expectCode(t, e.admin.call("GET", "/api/interface/list?project_id=99999", nil), 407)
	expectCode(t, e.admin.call("GET", "/api/interface/list", nil), 400)
	expectCode(t, e.admin.call("GET", "/api/interface/list_cat?catid=99999", nil), 402)
}

func TestUpIndexChecksPermission(t *testing.T) {
	e := newInterfaceEnv(t)
	id := e.addInterface(e.admin, "x", "/x", nil)

	// 与 YApi 不同：没有编辑权限的接口被忽略，但响应仍是成功
	expectCode(t, e.outside.call("POST", "/api/interface/up_index", []any{map[string]any{"id": id, "index": 5}}), 0)
	expectCode(t, e.outside.call("POST", "/api/interface/up_cat_index", []any{map[string]any{"id": e.cat, "index": 5}}), 0)
	if idx := e.admin.call("GET", "/api/interface/get?id="+q(id), nil)["data"].(map[string]any)["index"]; idx != float64(0) {
		t.Errorf("无权限的用户不应改动接口顺序, index = %v", idx)
	}
	expectCode(t, e.dev.call("POST", "/api/interface/up_index", []any{map[string]any{"id": id, "index": 5}}), 0)
	if idx := e.admin.call("GET", "/api/interface/get?id="+q(id), nil)["data"].(map[string]any)["index"]; idx != float64(5) {
		t.Errorf("开发者应能调整接口顺序, index = %v", idx)
	}
	expectCode(t, e.admin.call("POST", "/api/interface/up_index", map[string]any{"id": id}), 400)
}

func TestCopyProjectRequiresViewOnSource(t *testing.T) {
	e := newInterfaceEnv(t)
	e.addInterface(e.admin, "x", "/x", nil)
	mine := e.outside.call("GET", "/api/group/get_mygroup", nil)["data"].(map[string]any)["_id"]
	cats := []any{map[string]any{"_id": e.cat, "name": "公共分类"}}

	// 与 YApi 不同：复制别人的私有项目需要有查看权限
	body := map[string]any{"_id": e.pid, "name": "偷来的", "group_id": mine, "cat": cats}
	expectCode(t, e.outside.call("POST", "/api/project/copy", body), 405)

	// 开发者可以把项目复制到自己的个人空间，复制者自动成为组长
	devMine := e.dev.call("GET", "/api/group/get_mygroup", nil)["data"].(map[string]any)["_id"]
	body = map[string]any{"_id": e.pid, "name": "副本", "group_id": devMine, "cat": cats}
	r := e.dev.call("POST", "/api/project/copy", body)
	expectCode(t, r, 0)
	copied := r["data"].(map[string]any)["_id"].(float64)
	menu := e.dev.call("GET", "/api/interface/list_menu?project_id="+q(copied), nil)["data"].([]any)
	if len(menu) != 1 || len(menu[0].(map[string]any)["list"].([]any)) != 1 {
		t.Errorf("副本应包含一个分类和其中的接口: %v", menu)
	}
	if role := e.dev.call("GET", "/api/project/get?id="+q(copied), nil)["data"].(map[string]any)["role"]; role != "owner" {
		t.Errorf("复制者应成为副本的组长, 实际 %v", role)
	}
	expectCode(t, e.dev.call("POST", "/api/project/copy", map[string]any{"_id": e.pid, "name": "无分类", "group_id": devMine}), 402)
}

func TestSchema2JSON(t *testing.T) {
	e := newInterfaceEnv(t)
	schema := map[string]any{
		"type": "object", "required": []any{"id"},
		"properties": map[string]any{
			"id":   map[string]any{"type": "integer", "minimum": 5, "maximum": 5},
			"tags": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/definitions/tag"}, "minItems": 2, "maxItems": 2},
		},
		"definitions": map[string]any{"tag": map[string]any{"const": "t"}},
	}
	r := e.admin.call("POST", "/api/interface/schema2json", map[string]any{"schema": schema})
	if r["id"] != float64(5) || len(r["tags"].([]any)) != 2 || r["tags"].([]any)[0] != "t" {
		t.Errorf("生成的示例不对: %v", r)
	}

	// schema 不是对象时与 YApi 一样返回纯文本的错误信息
	resp, err := e.admin.http.Post(e.env.srv.URL+"/api/interface/schema2json", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("出错时应返回纯文本, Content-Type = %s", ct)
	}
}

func TestCatMenuMissingProject(t *testing.T) {
	e := newInterfaceEnv(t)
	resp, err := e.admin.http.Get(e.env.srv.URL + "/api/interface/getCatMenu?project_id=99999")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("项目不存在时 YApi 返回 404, 实际 %d", resp.StatusCode)
	}
	// 私有项目的分类列表要求编辑权限
	guest := e.env.client()
	guest.register("guest@apiyard.test")
	guestUID := guest.call("GET", "/api/user/status", nil)["data"].(map[string]any)["_id"]
	expectCode(t, e.admin.call("POST", "/api/project/add_member",
		map[string]any{"id": e.pid, "member_uids": []any{guestUID}, "role": "guest"}), 0)
	expectCode(t, guest.call("GET", "/api/interface/getCatMenu?project_id="+q(e.pid), nil), 406)
	expectCode(t, guest.call("GET", "/api/interface/list_menu?project_id="+q(e.pid), nil), 0)
}

// dialConflict 以 c 的身份连接编辑冲突检测，返回连接和收到的第一条消息。
func dialConflict(t *testing.T, c *client, id string) (*websocket.Conn, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(c.env.srv.URL, "http") + "/api/interface/solve_conflict?id=" + id
	conn, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPClient: c.http})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return conn, string(data)
}

func TestSolveConflict(t *testing.T) {
	e := newInterfaceEnv(t)
	id := e.addInterface(e.admin, "x", "/x", nil)
	devUID := e.dev.call("GET", "/api/user/status", nil)["data"].(map[string]any)["_id"].(float64)

	editor := func() any {
		return e.admin.call("GET", "/api/interface/get?id="+q(id), nil)["data"].(map[string]any)["edit_uid"]
	}
	waitEditor := func(want float64) {
		t.Helper()
		for range 50 {
			if editor() == want {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("edit_uid = %v, 期望 %v", editor(), want)
	}

	devConn, msg := dialConflict(t, e.dev, q(id))
	if !strings.Contains(msg, `"errno":0}`) || !strings.Contains(msg, `"title":"x"`) {
		t.Fatalf("第一个编辑者应占用成功: %s", msg)
	}
	waitEditor(devUID)

	adminConn, msg := dialConflict(t, e.admin, q(id))
	if !strings.Contains(msg, `"errno":`+q(devUID)) || !strings.Contains(msg, `"uid":`+q(devUID)) {
		t.Fatalf("第二个人应收到正在编辑的提示: %s", msg)
	}
	// 与 YApi 不同：被挡住的连接关闭时不会清除别人的编辑状态
	_ = adminConn.Close(websocket.StatusNormalClosure, "")
	time.Sleep(100 * time.Millisecond)
	waitEditor(devUID)

	_ = devConn.Close(websocket.StatusNormalClosure, "")
	waitEditor(0)

	badConn, msg := dialConflict(t, e.admin, "abc")
	defer func() { _ = badConn.CloseNow() }()
	if msg != "id 参数有误" {
		t.Errorf("id 不合法时的提示不对: %s", msg)
	}
}

func TestMergeResBodySchema(t *testing.T) {
	old := `{"type":"object","properties":{"a":{"type":"string","description":"旧描述","mock":{"mock":"@name"}}},"title":"旧"}`
	merged := mergeResBodySchema(&old, `{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"number"}}}`)
	s, _ := merged.(string)
	for _, want := range []string{`"description": "旧描述"`, `"mock": "@name"`, `"b": {`, `"title": "旧"`} {
		if !strings.Contains(s, want) {
			t.Errorf("合并结果缺少 %s:\n%s", want, s)
		}
	}
	if got := mergeResBodySchema(&old, "not json"); got != "not json" {
		t.Errorf("新内容不是 JSON 时应原样保留: %v", got)
	}
}

func TestInterfaceSaveDataSync(t *testing.T) {
	e := newInterfaceEnv(t)
	oldBody := `{"type":"object","properties":{"a":{"type":"string","description":"说明"}}}`
	id := e.addInterface(e.admin, "x", "/x", map[string]any{"res_body": oldBody, "res_body_is_json_schema": true})

	r := e.admin.call("POST", "/api/interface/save", map[string]any{
		"project_id": e.pid, "catid": e.cat, "title": "x", "path": "/x", "method": "GET", "dataSync": "good",
		"res_body_is_json_schema": true, "res_body": `{"type":"object","properties":{"a":{"type":"string"}}}`,
	})
	expectCode(t, r, 0)
	body := e.admin.call("GET", "/api/interface/get?id="+q(id), nil)["data"].(map[string]any)["res_body"].(string)
	if !strings.Contains(body, "说明") {
		t.Errorf("dataSync=good 应保留原有字段描述: %s", body)
	}
}

func TestSchema2JSONNeedsLogin(t *testing.T) {
	e := newEnv(t, Options{})
	resp, err := http.Post(e.srv.URL+"/api/interface/schema2json", "application/json", strings.NewReader(`{"schema":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(data), "40011") {
		t.Errorf("未登录时应要求登录: %s", data)
	}
}
