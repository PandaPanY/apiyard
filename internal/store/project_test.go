package store

import (
	"context"
	"errors"
	"testing"
)

// newProject 在一个新分组里建项目，owner 为 nil 时不加成员。
func newProject(t *testing.T, s *Store, name string, owner *Member) (*Group, *Project) {
	t.Helper()
	ctx := context.Background()
	g := &Group{UID: 11, Name: "g-" + name, Type: "public"}
	if err := s.CreateGroup(ctx, g, nil); err != nil {
		t.Fatal(err)
	}
	bp := "/" + name
	p := &Project{UID: 11, GroupID: g.ID, Name: name, Basepath: &bp, ProjectType: "private", SwitchNotice: true, Env: "[]", Tag: "[]"}
	if err := s.CreateProject(ctx, p, &Cat{UID: 11, Name: "公共分类"}, owner); err != nil {
		t.Fatal(err)
	}
	return g, p
}

func TestProjectsAndCats(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	u := newUser(t, s, "own@example.com")
	g, p := newProject(t, s, "orders", &Member{UID: u.ID, Role: "owner", Username: &u.Username, Email: &u.Email})

	cats, _ := s.CatsByProject(ctx, p.ID)
	if len(cats) != 1 || cats[0].Name != "公共分类" {
		t.Errorf("新建项目应带一个默认分类: %+v", cats)
	}
	if m, err := s.ProjectMemberOf(ctx, p.ID, u.ID); err != nil || m.Role != "owner" || !m.EmailNotice {
		t.Errorf("创建者应成为组长且默认接收通知: %+v %v", m, err)
	}
	if exists, _ := s.ProjectNameExists(ctx, "orders", g.ID); !exists {
		t.Error("ProjectNameExists 应返回 true")
	}

	name, notice := "orders2", false
	if n, _ := s.UpdateProject(ctx, p.ID, ProjectPatch{Name: &name, SwitchNotice: &notice}); n != 1 {
		t.Error("UpdateProject 应影响 1 行")
	}
	got, _ := s.ProjectByID(ctx, p.ID)
	if got.Name != "orders2" || got.SwitchNotice || *got.Basepath != "/orders" {
		t.Errorf("只应修改 patch 中给出的字段: %+v", got)
	}
	if list, _ := s.ProjectsByGroup(ctx, g.ID); len(list) != 1 {
		t.Errorf("ProjectsByGroup = %d 个", len(list))
	}
	if ids, _ := s.VisibleProjectGroupIDs(ctx, u.ID); len(ids) != 1 || ids[0] != g.ID {
		t.Errorf("成员应能看到项目所在的分组: %v", ids)
	}

	if n, _ := s.DeleteProject(ctx, p.ID); n != 1 {
		t.Error("DeleteProject 应影响 1 行")
	}
	if cats, _ := s.CatsByProject(ctx, p.ID); len(cats) != 0 {
		t.Error("删除项目后分类应被级联删除")
	}
	if _, err := s.ProjectMemberOf(ctx, p.ID, u.ID); !errors.Is(err, ErrNotFound) {
		t.Error("删除项目后成员应被级联删除")
	}
}

func TestProjectMembers(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	_, p := newProject(t, s, "pay", nil)
	a, b := newUser(t, s, "aaa@example.com"), newUser(t, s, "bbb@example.com")

	if ok, err := s.AddProjectMembers(ctx, p.ID, []Member{{UID: a.ID, Role: "dev"}, {UID: b.ID, Role: "guest"}}); !ok || err != nil {
		t.Fatalf("AddProjectMembers: %v %v", ok, err)
	}
	if ok, _ := s.AddProjectMembers(ctx, 999, []Member{{UID: a.ID, Role: "dev"}}); ok {
		t.Error("向不存在的项目添加成员应返回 false")
	}
	members, _ := s.ProjectMembers(ctx, p.ID)
	if len(members) != 2 || members[0].UID != a.ID || !members[0].EmailNotice || len(members[1].SubID) != 24 {
		t.Errorf("成员应按加入顺序排列、默认接收通知: %+v", members)
	}

	if n, _ := s.ChangeProjectMemberRole(ctx, p.ID, b.ID, "owner"); n != 1 {
		t.Error("ChangeProjectMemberRole 应影响 1 行")
	}
	if n, _ := s.SetProjectMemberNotice(ctx, p.ID, a.ID, false); n != 1 {
		t.Error("SetProjectMemberNotice 应影响 1 行")
	}
	if m, _ := s.ProjectMemberOf(ctx, p.ID, a.ID); m.EmailNotice {
		t.Error("邮件通知应已关闭")
	}
	if n, _ := s.RemoveProjectMember(ctx, p.ID, a.ID); n != 1 {
		t.Error("RemoveProjectMember 应影响 1 行")
	}
	if members, _ := s.ProjectMembers(ctx, p.ID); len(members) != 1 || members[0].Role != "owner" {
		t.Errorf("移除后应只剩一名组长: %+v", members)
	}
}

func TestFollowsAndTokens(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	_, p := newProject(t, s, "pay", nil)

	f := &Follow{UID: 11, ProjectID: p.ID, ProjectName: p.Name}
	if err := s.CreateFollow(ctx, f); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.IsFollowing(ctx, 11, p.ID); !ok {
		t.Error("IsFollowing 应返回 true")
	}
	icon := "star"
	if err := s.UpdateFollowStyle(ctx, p.ID, &icon, nil); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.FollowsOf(ctx, 11); len(list) != 1 || *list[0].Icon != "star" {
		t.Errorf("关注记录的图标应同步: %+v", list)
	}
	if n, _ := s.DeleteFollow(ctx, 11, p.ID); n != 1 {
		t.Error("DeleteFollow 应影响 1 行")
	}

	if _, err := s.ProjectToken(ctx, p.ID); !errors.Is(err, ErrNotFound) {
		t.Error("还没生成 token 时应返回 ErrNotFound")
	}
	for _, tok := range []string{"aaa", "bbb"} {
		if err := s.SetProjectToken(ctx, p.ID, tok); err != nil {
			t.Fatal(err)
		}
	}
	if tok, _ := s.ProjectToken(ctx, p.ID); tok != "bbb" {
		t.Errorf("SetProjectToken 应覆盖旧值, 实际 %s", tok)
	}
}

func TestLogQueries(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	g, p := newProject(t, s, "pay", nil)
	data := `{"interface_id": 7}`
	for _, l := range []Log{
		{UID: 11, Username: "a", Type: "group", TypeID: g.ID, Content: "g1"},
		{UID: 11, Username: "a", Type: "project", TypeID: p.ID, Content: "p1"},
		{UID: 11, Username: "a", Type: "project", TypeID: p.ID, Content: "p2", Data: &data},
	} {
		if err := s.CreateLog(ctx, l); err != nil {
			t.Fatal(err)
		}
	}

	logs, _ := s.ListLogs(ctx, LogFilter{Type: "project", TypeID: p.ID}, 1, 10)
	if len(logs) != 2 || logs[0].Content != "p1" {
		t.Errorf("同一秒内的动态应按 ID 升序（与 YApi 相同）: %+v", logs)
	}
	iface := int64(7)
	if n, _ := s.CountLogs(ctx, LogFilter{Type: "project", TypeID: p.ID, InterfaceID: &iface}); n != 1 {
		t.Errorf("按接口筛选应只有 1 条, 实际 %d", n)
	}
	if n, _ := s.CountGroupLogs(ctx, g.ID, []int64{p.ID}); n != 3 {
		t.Errorf("分组动态应包含分组和项目的动态, 实际 %d 条", n)
	}
	if page2, _ := s.ListGroupLogs(ctx, g.ID, []int64{p.ID}, 2, 2); len(page2) != 1 {
		t.Errorf("第 2 页应有 1 条, 实际 %d", len(page2))
	}
	if n, _ := s.CountGroupLogs(ctx, g.ID, nil); n != 1 {
		t.Errorf("没有项目时只统计分组本身的动态, 实际 %d", n)
	}
}

func TestSearch(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	newProject(t, s, "Orders", nil)
	if list, _ := s.SearchProjects(ctx, "order"); len(list) != 1 {
		t.Errorf("项目搜索应不区分大小写: %d", len(list))
	}
	if list, _ := s.SearchGroups(ctx, "G-ORDERS"); len(list) != 1 {
		t.Errorf("分组搜索应不区分大小写: %d", len(list))
	}
	if list, err := s.SearchInterfaces(ctx, "x"); err != nil || len(list) != 0 {
		t.Errorf("没有接口时应返回空: %v %v", list, err)
	}
}
