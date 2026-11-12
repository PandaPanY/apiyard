package api

import (
	"context"
	"strings"
	"testing"

	"github.com/PandaPanY/apiyard/internal/store"
)

func TestPrivateProjectReadBoundaries(t *testing.T) {
	e := newInterfaceEnv(t)
	id := e.addInterface(e.admin, "private", "/private", nil)
	expectCode(t, e.admin.call("POST", "/api/interface/up", map[string]any{"id": id, "title": "private changed", "res_body": "secret-marker"}), 0)
	for _, path := range []string{
		"/api/project/get?id=" + q(e.pid),
		"/api/user/project?type=project&id=" + q(e.pid),
		"/api/user/project?type=interface&id=" + q(id),
		"/api/log/list?type=project&typeid=" + q(e.pid),
	} {
		expectCode(t, e.outside.call("GET", path, nil), 406)
		expectCode(t, e.dev.call("GET", path, nil), 0)
	}
	path := "/api/log/list?type=group&typeid=" + q(e.gid)
	result := e.outside.call("GET", path, nil)["data"].(map[string]any)
	if len(result["list"].([]any)) != 0 || result["total"] != float64(0) {
		t.Fatalf("outside user saw private logs: %v", result)
	}
	result = e.dev.call("GET", path, nil)["data"].(map[string]any)
	if len(result["list"].([]any)) == 0 {
		t.Fatal("member should see group events")
	}
	uid := e.dev.call("GET", "/api/user/status", nil)["data"].(map[string]any)["_id"].(float64)
	private, err := e.env.st.PrivateGroupOf(context.Background(), int64(uid))
	if err != nil {
		t.Fatal(err)
	}
	expectCode(t, e.outside.call("GET", "/api/user/project?type=group&id="+q(float64(private.ID)), nil), 406)
	r := e.admin.call("POST", "/api/project/add", map[string]any{"name": "personal project", "group_id": private.ID})
	expectCode(t, r, 0)
	pid := r["data"].(map[string]any)["_id"].(float64)
	outsideID := e.outside.call("GET", "/api/user/status", nil)["data"].(map[string]any)["_id"].(float64)
	if _, err := e.env.st.AddProjectMembers(context.Background(), int64(pid), []store.Member{{UID: int64(outsideID), Role: "guest"}}); err != nil {
		t.Fatal(err)
	}
	r = e.outside.call("GET", "/api/user/project?type=project&id="+q(pid), nil)
	expectCode(t, r, 0)
	group := r["data"].(map[string]any)["group"].(map[string]any)
	if _, ok := group["members"]; ok {
		t.Fatal("project-only member received personal space members")
	}
}

func TestProjectCredentialsCannotForgeIdentity(t *testing.T) {
	e := newOpenEnv(t)
	dev := e.dev.call("GET", "/api/project/token?project_id="+q(e.pid), nil)["data"].(string)
	admin := e.admin.call("GET", "/api/project/token?project_id="+q(e.pid), nil)["data"].(string)
	if dev == admin {
		t.Fatal("credentials must be independently random per issuer")
	}
	expectCode(t, e.anon.call("POST", "/api/project/up?token="+dev, map[string]any{"id": e.pid, "desc": "blocked"}), 405)
	expectCode(t, e.anon.call("POST", "/api/project/up?token="+admin, map[string]any{"id": e.pid, "desc": "allowed"}), 0)
	adminUID := e.admin.call("GET", "/api/user/status", nil)["data"].(map[string]any)["_id"]
	forged := tokenFor(t, adminUID, e.rawP)
	expectCode(t, e.anon.call("POST", "/api/project/up?token="+forged, map[string]any{"id": e.pid, "desc": "forged"}), 405)
	uid := e.dev.call("GET", "/api/user/status", nil)["data"].(map[string]any)["_id"].(float64)
	if _, err := e.env.st.ChangeGroupMemberRole(context.Background(), int64(e.gid), int64(uid), "guest"); err != nil {
		t.Fatal(err)
	}
	expectCode(t, e.anon.call("POST", "/api/interface/add?token="+dev, map[string]any{"project_id": e.pid, "catid": e.cat, "title": "revoked", "path": "/revoked", "method": "GET"}), 40033)
	expectCode(t, e.admin.call("POST", "/api/project/up", map[string]any{"id": e.qid, "project_type": "public"}), 0)
	expectCode(t, e.anon.call("GET", "/api/project/get?id="+q(e.qid)+"&token="+admin, nil), 406)
	expectCode(t, e.anon.call("GET", "/api/interface/list_cat?catid="+q(e.qcat)+"&token="+admin, nil), 406)
	tampered := strings.Repeat("0", len(admin))
	expectCode(t, e.anon.call("GET", "/api/interface/list_menu?token="+tampered, nil), 40011)
	expectCode(t, e.admin.call("GET", "/api/project/update_token?project_id="+q(e.pid), nil), 0)
	expectCode(t, e.anon.call("GET", "/api/interface/list_menu?token="+admin, nil), 40011)
}
