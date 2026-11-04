package api

import (
	"strconv"
	"testing"
)

// projectEnv 准备一个分组和一个私有项目：admin 是管理员，dev 是分组开发者，outsider 与分组无关。
type projectEnv struct {
	env                 *testEnv
	admin, dev, outside *client
	gid, pid            float64
}

func newProjectEnv(t *testing.T) *projectEnv {
	t.Helper()
	e := &projectEnv{env: newEnv(t, Options{})}
	e.admin = e.env.client()
	e.admin.login("admin@admin.com", "ymfe.org")
	e.dev, e.outside = e.env.client(), e.env.client()
	devUID := e.dev.register("dev@apiyard.test")
	e.outside.register("outside@apiyard.test")

	r := e.admin.call("POST", "/api/group/add", map[string]any{"group_name": "项目组"})
	e.gid = r["data"].(map[string]any)["_id"].(float64)
	expectCode(t, e.admin.call("POST", "/api/group/add_member", map[string]any{"id": e.gid, "member_uids": []any{devUID}}), 0)
	r = e.admin.call("POST", "/api/project/add", map[string]any{"name": "私有项目", "group_id": e.gid})
	expectCode(t, r, 0)
	e.pid = r["data"].(map[string]any)["_id"].(float64)
	return e
}

func q(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func TestPrivateProjectVisibility(t *testing.T) {
	e := newProjectEnv(t)

	// 分组外的用户看不到私有项目，也打不开
	list := e.outside.call("GET", "/api/project/list?group_id="+q(e.gid), nil)["data"].(map[string]any)["list"].([]any)
	if len(list) != 0 {
		t.Errorf("分组外的用户不应看到私有项目: %v", list)
	}
	expectCode(t, e.outside.call("GET", "/api/project/get?id="+q(e.pid), nil), 406)

	// 分组开发者能看到
	if list := e.dev.call("GET", "/api/project/list?group_id="+q(e.gid), nil)["data"].(map[string]any)["list"].([]any); len(list) != 1 {
		t.Errorf("分组开发者应能看到私有项目: %v", list)
	}
	if role := e.dev.call("GET", "/api/project/get?id="+q(e.pid), nil)["data"].(map[string]any)["role"]; role != "dev" {
		t.Errorf("分组开发者在项目中的角色应为 dev, 实际 %v", role)
	}
}

func TestProjectTokenPermissions(t *testing.T) {
	e := newProjectEnv(t)

	// 与 YApi 不同：分组外的用户不能拿到项目 token
	expectCode(t, e.outside.call("GET", "/api/project/token?project_id="+q(e.pid), nil), 405)
	// 开发者可以查看 token，但只有组长和管理员能重置
	r := e.dev.call("GET", "/api/project/token?project_id="+q(e.pid), nil)
	expectCode(t, r, 0)
	if tok, _ := r["data"].(string); len(tok) != 64 {
		t.Errorf("token 应为 64 位十六进制: %q", tok)
	}
	expectCode(t, e.dev.call("GET", "/api/project/update_token?project_id="+q(e.pid), nil), 405)

	first := e.admin.call("GET", "/api/project/token?project_id="+q(e.pid), nil)["data"]
	if again := e.admin.call("GET", "/api/project/token?project_id="+q(e.pid), nil)["data"]; again != first {
		t.Error("同一用户两次获取的 token 应相同")
	}
	expectCode(t, e.admin.call("GET", "/api/project/update_token?project_id="+q(e.pid), nil), 0)
	if after := e.admin.call("GET", "/api/project/token?project_id="+q(e.pid), nil)["data"]; after == first {
		t.Error("重置后 token 应改变")
	}
}

func TestProjectUpEdgeCases(t *testing.T) {
	e := newProjectEnv(t)
	r := e.admin.call("POST", "/api/project/add", map[string]any{"name": "另一个", "group_id": e.gid})
	other := r["data"].(map[string]any)["_id"].(float64)

	expectCode(t, e.admin.call("POST", "/api/project/up", map[string]any{}), 405)
	expectCode(t, e.admin.call("POST", "/api/project/up", map[string]any{"id": other, "name": "私有项目", "group_id": e.gid}), 401)
	expectCode(t, e.admin.call("POST", "/api/project/up", map[string]any{"id": other, "basepath": "bad path"}), 401)
	expectCode(t, e.admin.call("POST", "/api/project/up", map[string]any{"id": other, "project_type": "secret"}), 402)
	// 名字没变时不做重名检查
	expectCode(t, e.admin.call("POST", "/api/project/up", map[string]any{"id": other, "name": "另一个", "basepath": "/"}), 0)
	if bp := e.admin.call("GET", "/api/project/get?id="+q(other), nil)["data"].(map[string]any)["basepath"]; bp != "" {
		t.Errorf("basepath 为 / 时应保存为空字符串, 实际 %v", bp)
	}

	expectCode(t, e.admin.call("POST", "/api/project/up_env", map[string]any{"id": other, "env": "x"}), 405)
	expectCode(t, e.admin.call("POST", "/api/project/up_tag", map[string]any{"id": other}), 405)
	expectCode(t, e.admin.call("POST", "/api/project/upset", map[string]any{"icon": "x"}), 405)
	expectCode(t, e.admin.call("GET", "/api/project/get_env", nil), 405)
	expectCode(t, e.admin.call("GET", "/api/project/get_env?project_id=999", nil), 402)
	expectCode(t, e.dev.call("POST", "/api/project/up_env", map[string]any{"id": other, "env": []any{}}), 0) // 开发者可以改环境
}

func TestPersonalSpaceListsFollows(t *testing.T) {
	e := newProjectEnv(t)
	expectCode(t, e.dev.call("POST", "/api/follow/add", map[string]any{"projectid": e.pid}), 0)
	expectCode(t, e.dev.call("POST", "/api/follow/add", map[string]any{"projectid": 999}), 402)
	expectCode(t, e.dev.call("POST", "/api/follow/add", map[string]any{}), 400)

	mine := e.dev.call("GET", "/api/group/get_mygroup", nil)["data"].(map[string]any)["_id"].(float64)
	list := e.dev.call("GET", "/api/project/list?group_id="+q(mine), nil)["data"].(map[string]any)["list"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["follow"] != true {
		t.Errorf("个人空间应列出关注的项目: %v", list)
	}
}

func TestLogListValidation(t *testing.T) {
	e := newProjectEnv(t)
	expectCode(t, e.admin.call("GET", "/api/log/list?type=project", nil), 400)
	expectCode(t, e.admin.call("GET", "/api/log/list?typeid=1", nil), 400)
	r := e.admin.call("GET", "/api/log/list?type=project&typeid="+q(e.pid)+"&selectValue=999", nil)
	if list := r["data"].(map[string]any)["list"].([]any); len(list) != 0 {
		t.Errorf("按接口筛选时应只返回该接口的改动: %v", list)
	}
}

func TestUserProjectNotFound(t *testing.T) {
	e := newProjectEnv(t)
	expectCode(t, e.admin.call("GET", "/api/user/project?type=project&id=999", nil), 422)
	r := e.admin.call("GET", "/api/user/project?type=project&id="+q(e.pid), nil)
	data := r["data"].(map[string]any)
	if data["project"] == nil || data["group"] == nil {
		t.Errorf("应同时返回项目和所在分组: %v", data)
	}
}

func TestProjectMemberPermissions(t *testing.T) {
	e := newProjectEnv(t)
	outsideUID := e.outside.call("GET", "/api/user/status", nil)["data"].(map[string]any)["_id"]
	devUID := e.dev.call("GET", "/api/user/status", nil)["data"].(map[string]any)["_id"]
	expectCode(t, e.admin.call("POST", "/api/project/add_member",
		map[string]any{"id": e.pid, "member_uids": []any{outsideUID, devUID}, "role": "guest"}), 0)

	// 与 YApi 不同：私有项目的成员列表要求有查看权限；访客可以看
	expectCode(t, e.outside.call("GET", "/api/project/get_member_list?id="+q(e.pid), nil), 0)
	e.admin.call("POST", "/api/project/del_member", map[string]any{"id": e.pid, "member_uid": outsideUID})
	expectCode(t, e.outside.call("GET", "/api/project/get_member_list?id="+q(e.pid), nil), 406)
	expectCode(t, e.outside.call("POST", "/api/project/add_member", map[string]any{"id": e.pid, "member_uids": []any{devUID}}), 405)

	// 与 YApi 不同：只能修改自己的邮件通知，组长和管理员可以改任何人的
	notice := func(c *client, uid any) map[string]any {
		return c.call("POST", "/api/project/change_member_email_notice", map[string]any{"id": e.pid, "member_uid": uid, "notice": false})
	}
	expectCode(t, notice(e.dev, devUID), 0)
	expectCode(t, notice(e.admin, devUID), 0)
	expectCode(t, notice(e.admin, 99999), 400)

	devAsGuest := e.env.client()
	devAsGuest.login("outside@apiyard.test", "pass")
	expectCode(t, devAsGuest.call("POST", "/api/project/add_member", map[string]any{"id": e.pid, "member_uids": []any{outsideUID}}), 405)
	expectCode(t, notice(devAsGuest, devUID), 405)
}

func TestProjectAddMemberDeletedUser(t *testing.T) {
	e := newProjectEnv(t)
	devUID := e.dev.call("GET", "/api/user/status", nil)["data"].(map[string]any)["_id"]
	expectCode(t, e.admin.call("POST", "/api/project/add_member", map[string]any{"id": e.pid, "member_uids": []any{devUID}}), 0)
	expectCode(t, e.admin.call("POST", "/api/user/del", map[string]any{"id": devUID}), 0)

	// 用户已被删除但仍在成员列表里时，YApi 在 exist_members 中放 null
	r := e.admin.call("POST", "/api/project/add_member", map[string]any{"id": e.pid, "member_uids": []any{devUID}})
	if exist := r["data"].(map[string]any)["exist_members"].([]any); len(exist) != 1 || exist[0] != nil {
		t.Errorf("已删除用户应以 null 出现在 exist_members: %v", exist)
	}
}
