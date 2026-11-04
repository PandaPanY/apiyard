package api

import (
	"context"
	"strconv"
	"testing"

	"github.com/PandaPanY/apiyard/internal/yapitoken"
)

// openEnv 在 interfaceEnv 的基础上准备第二个私有项目 Q（含一个接口），并给两个项目设置已知的原始 token。
type openEnv struct {
	*interfaceEnv
	qid, qcat, qIface float64
	rawP, rawQ        string
	anon              *client
}

func newOpenEnv(t *testing.T) *openEnv {
	t.Helper()
	e := &openEnv{interfaceEnv: newInterfaceEnv(t), rawP: "rawtokenp00000000001", rawQ: "rawtokenq00000000002"}
	r := e.admin.call("POST", "/api/project/add", map[string]any{"name": "Q", "group_id": e.gid})
	e.qid = r["data"].(map[string]any)["_id"].(float64)
	e.qcat = e.admin.call("GET", "/api/project/get?id="+q(e.qid), nil)["data"].(map[string]any)["cat"].([]any)[0].(map[string]any)["_id"].(float64)
	r = e.admin.call("POST", "/api/interface/add", map[string]any{"project_id": e.qid, "catid": e.qcat, "title": "q", "path": "/q", "method": "GET"})
	e.qIface = r["data"].(map[string]any)["_id"].(float64)
	ctx := context.Background()
	for pid, raw := range map[float64]string{e.pid: e.rawP, e.qid: e.rawQ} {
		if err := e.env.st.SetProjectToken(ctx, int64(pid), raw); err != nil {
			t.Fatal(err)
		}
	}
	e.anon = e.env.client()
	return e
}

// tokenFor 生成新格式 token（"uid|原始 token" 加密），使用默认的 passsalt。
func tokenFor(t *testing.T, uid any, raw string) string {
	t.Helper()
	tok, err := yapitoken.Encode(strconv.FormatFloat(uid.(float64), 'f', -1, 64), raw, yapitoken.DefaultSalt)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestTokenCannotLeaveItsProject(t *testing.T) {
	e := newOpenEnv(t)
	adminUID := e.admin.call("GET", "/api/user/status", nil)["data"].(map[string]any)["_id"]
	// 即使 token 代表管理员，也只能操作 token 所属的项目 P
	tok := tokenFor(t, adminUID, e.rawP)

	expectCode(t, e.anon.call("GET", "/api/interface/get?id="+q(e.qIface)+"&token="+tok, nil), 400)
	expectCode(t, e.anon.call("POST", "/api/interface/up?token="+tok, map[string]any{"id": e.qIface, "title": "被改了"}), 400)
	expectCode(t, e.anon.call("POST", "/api/interface/add_cat?token="+tok, map[string]any{"name": "x", "project_id": e.qid}), 400)
	expectCode(t, e.anon.call("GET", "/api/project/get?id="+q(e.qid)+"&token="+tok, nil), 406)
	expectCode(t, e.anon.call("GET", "/api/interface/list_cat?catid="+q(e.qcat)+"&token="+tok, nil), 406)
	expectCode(t, e.anon.call("POST", "/api/project/up?token="+tok, map[string]any{"id": e.qid, "desc": "x"}), 405)
	if title := e.admin.call("GET", "/api/interface/get?id="+q(e.qIface), nil)["data"].(map[string]any)["title"]; title != "q" {
		t.Errorf("其他项目的接口不应被修改: %v", title)
	}

	// 在 P 里正常工作，新建的接口一定落在 P
	r := e.anon.call("POST", "/api/interface/add?token="+tok, map[string]any{"project_id": e.qid, "catid": e.cat, "title": "p", "path": "/p", "method": "GET"})
	expectCode(t, r, 0)
	if pid := r["data"].(map[string]any)["project_id"]; pid != e.pid {
		t.Errorf("project_id 应被 token 覆盖为 P, 实际 %v", pid)
	}
	expectCode(t, e.anon.call("POST", "/api/project/up?token="+tok, map[string]any{"id": e.pid, "desc": "管理员可以改"}), 0)
}

func TestForgedAndInvalidTokens(t *testing.T) {
	e := newOpenEnv(t)
	adminUID := e.admin.call("GET", "/api/user/status", nil)["data"].(map[string]any)["_id"]

	// 能解密但项目 token 不存在：YApi 会把请求当成 token 里的 uid（这里是管理员），ApiYard 拒绝
	forged := tokenFor(t, adminUID, "no-such-project-token")
	expectCode(t, e.anon.call("GET", "/api/interface/list_menu?token="+forged, nil), 42014)

	// 用户已删除的 token 无效
	outsideUID := e.outside.call("GET", "/api/user/status", nil)["data"].(map[string]any)["_id"]
	gone := tokenFor(t, outsideUID, e.rawP)
	expectCode(t, e.admin.call("POST", "/api/user/del", map[string]any{"id": outsideUID}), 0)
	expectCode(t, e.anon.call("GET", "/api/interface/list_menu?token="+gone, nil), 42014)

	// 既解不开也不是原始 token：与 YApi 一样忽略 token，沿用 Cookie 的登录状态
	expectCode(t, e.anon.call("GET", "/api/interface/list_menu?token=garbage", nil), 40011)
	expectCode(t, e.dev.call("GET", "/api/interface/list_menu?project_id="+q(e.pid)+"&token=garbage", nil), 0)

	// 不在白名单里的接口不认 token
	expectCode(t, e.anon.call("GET", "/api/project/list?group_id="+q(e.gid)+"&token="+tokenFor(t, adminUID, e.rawP), nil), 40011)
}

func TestLegacyRawToken(t *testing.T) {
	e := newOpenEnv(t)
	// 旧格式 token 代表 system 普通会员：与 YApi 一样，看不了私有项目，但可以写接口（写操作不检查角色）
	expectCode(t, e.anon.call("GET", "/api/interface/list_menu?token="+e.rawP, nil), 406)
	r := e.anon.call("POST", "/api/interface/add?token="+e.rawP, map[string]any{"catid": e.cat, "title": "旧 token", "path": "/legacy", "method": "GET"})
	expectCode(t, r, 0)
	if uid := r["data"].(map[string]any)["uid"]; uid != float64(legacyTokenUID) {
		t.Errorf("旧 token 新建的接口应归 999999, 实际 %v", uid)
	}
	// system 用户不会被加进项目成员
	members := e.admin.call("GET", "/api/project/get_member_list?id="+q(e.pid), nil)["data"].([]any)
	for _, m := range members {
		if m.(map[string]any)["uid"] == float64(legacyTokenUID) {
			t.Error("system 用户不应成为项目成员")
		}
	}
	logs := e.admin.call("GET", "/api/log/list?type=project&typeid="+q(e.pid), nil)["data"].(map[string]any)["list"].([]any)
	if logs[0].(map[string]any)["username"] != "system" {
		t.Errorf("动态应记在 system 名下: %v", logs[0])
	}

	// 公开项目可以读
	expectCode(t, e.admin.call("POST", "/api/project/up", map[string]any{"id": e.qid, "project_type": "public"}), 0)
	expectCode(t, e.anon.call("GET", "/api/interface/list_menu?token="+e.rawQ, nil), 0)
}
