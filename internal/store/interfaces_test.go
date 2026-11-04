package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// newInterface 在分类 cat 下新建接口，extra 可以修改默认字段。
func newInterface(t *testing.T, s *Store, cat *Cat, title, method, path string, extra func(*Interface)) *Interface {
	t.Helper()
	it := &Interface{UID: 11, ProjectID: cat.ProjectID, CatID: cat.ID, Title: title, Path: path, Method: method,
		Status: "undone", Type: "static", QueryPath: `{"path":"` + path + `","params":[]}`, ReqQuery: "[]",
		ReqHeaders: "[]", ReqParams: "[]", ReqBodyForm: "[]", Tag: "[]", AddTime: 1, UpTime: 1}
	if extra != nil {
		extra(it)
	}
	if err := s.CreateInterface(context.Background(), it); err != nil {
		t.Fatal(err)
	}
	return it
}

func defaultCat(t *testing.T, s *Store, projectID int64) *Cat {
	t.Helper()
	cats, err := s.CatsByProject(context.Background(), projectID)
	if err != nil || len(cats) == 0 {
		t.Fatalf("项目没有分类: %v", err)
	}
	return cats[0]
}

func titles(list []*Interface) string {
	out := make([]string, len(list))
	for i, it := range list {
		out[i] = it.Title
	}
	return strings.Join(out, ",")
}

func TestInterfaceCRUD(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	_, p := newProject(t, s, "crud", nil)
	cat := defaultCat(t, s, p.ID)

	desc, body := "说明", `{"a":1}`
	it := newInterface(t, s, cat, "查询", "GET", "/q?x=1", func(it *Interface) {
		it.Desc, it.ResBody, it.APIOpened, it.ReqBodyIsJSONSchema = &desc, &body, true, true
	})
	got, err := s.InterfaceByID(ctx, it.ID)
	if err != nil || *got.Desc != "说明" || *got.ResBody != body || !got.APIOpened || !got.ReqBodyIsJSONSchema ||
		got.ReqBodyType != nil || got.Markdown != nil {
		t.Fatalf("读回的接口不对: %+v %v", got, err)
	}

	// 路径原样比较（含查询串）；method 为空时匹配任意方法
	if list, _ := s.InterfacesByRoute(ctx, p.ID, "/q?x=1", "GET"); len(list) != 1 {
		t.Errorf("按路径和方法应找到接口: %d", len(list))
	}
	if list, _ := s.InterfacesByRoute(ctx, p.ID, "/q", "GET"); len(list) != 0 {
		t.Error("路径不同时不应匹配")
	}
	if list, _ := s.InterfacesByRoute(ctx, p.ID, "/q?x=1", ""); len(list) != 1 {
		t.Error("method 为空时应匹配任意方法")
	}

	got.Title, got.Desc, got.Status = "查询2", nil, "done"
	if err := s.UpdateInterface(ctx, got); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.InterfaceByID(ctx, it.ID); again.Title != "查询2" || again.Desc != nil || again.Status != "done" {
		t.Errorf("UpdateInterface 应整条覆盖: %+v", again)
	}
	// 约束由数据库保证：非法状态写不进去
	got.Status = "unknown"
	if err := s.UpdateInterface(ctx, got); err == nil {
		t.Error("非法的 status 应被拒绝")
	}

	if n, _ := s.SetInterfacePosition(ctx, it.ID, 7); n != 1 {
		t.Error("SetInterfacePosition 应影响 1 行")
	}
	if n, _ := s.DeleteInterface(ctx, it.ID); n != 1 {
		t.Error("DeleteInterface 应影响 1 行")
	}
	if _, err := s.InterfaceByID(ctx, it.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("删除后应查不到: %v", err)
	}
}

func TestListInterfaces(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	_, p := newProject(t, s, "list", nil)
	cat := defaultCat(t, s, p.ID)
	other := &Cat{UID: 11, ProjectID: p.ID, Name: "其他"}
	if err := s.CreateCat(ctx, other); err != nil {
		t.Fatal(err)
	}
	newInterface(t, s, cat, "c", "GET", "/c", func(it *Interface) { it.Tag, it.Position = `["v1","v2"]`, 2 })
	newInterface(t, s, cat, "a", "GET", "/a", func(it *Interface) { it.Status, it.Position = "done", 1 })
	newInterface(t, s, other, "b", "GET", "/b", func(it *Interface) { it.Tag = `["v2"]` })

	cases := []struct {
		name    string
		filter  InterfaceFilter
		byTitle bool
		offset  int64
		limit   int64
		want    string
		count   int64
	}{
		{"按 index 排序", InterfaceFilter{ProjectID: p.ID}, false, 0, 0, "b,a,c", 3},
		{"按标题排序", InterfaceFilter{ProjectID: p.ID}, true, 0, 0, "a,b,c", 3},
		{"分页", InterfaceFilter{ProjectID: p.ID}, false, 1, 1, "a", 3},
		{"按分类", InterfaceFilter{CatID: other.ID}, false, 0, 10, "b", 1},
		{"按状态", InterfaceFilter{ProjectID: p.ID, Status: []string{"done"}}, false, 0, 10, "a", 1},
		{"多个状态", InterfaceFilter{ProjectID: p.ID, Status: []string{"done", "undone"}}, false, 0, 10, "b,a,c", 3},
		{"按 tag", InterfaceFilter{ProjectID: p.ID, Tag: []string{"v1"}}, false, 0, 10, "c", 1},
		{"多个 tag", InterfaceFilter{ProjectID: p.ID, Tag: []string{"v1", "v2"}}, false, 0, 10, "b,c", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			list, err := s.ListInterfaces(ctx, tc.filter, tc.byTitle, tc.offset, tc.limit)
			if err != nil {
				t.Fatal(err)
			}
			if got := titles(list); got != tc.want {
				t.Errorf("结果 %s, 期望 %s", got, tc.want)
			}
			if n, _ := s.CountInterfaces(ctx, tc.filter); n != tc.count {
				t.Errorf("数量 %d, 期望 %d", n, tc.count)
			}
		})
	}
	if all, _ := s.InterfacesByProject(ctx, p.ID); titles(all) != "b,a,c" {
		t.Errorf("InterfacesByProject 应按 index 排序: %s", titles(all))
	}
}

func TestCatsAndCascade(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	_, p := newProject(t, s, "cats", nil)
	desc := "描述"
	cat := &Cat{UID: 11, ProjectID: p.ID, Name: "用户", Desc: &desc}
	if err := s.CreateCat(ctx, cat); err != nil {
		t.Fatal(err)
	}
	if got, err := s.CatByID(ctx, cat.ID); err != nil || got.Name != "用户" || *got.Desc != "描述" {
		t.Fatalf("CatByID 不对: %+v %v", got, err)
	}
	if _, err := s.CatByID(ctx, 99999); !errors.Is(err, ErrNotFound) {
		t.Errorf("不存在的分类应返回 ErrNotFound: %v", err)
	}

	name := "用户中心"
	if n, _ := s.UpdateCat(ctx, cat.ID, &name, nil); n != 1 {
		t.Error("UpdateCat 应影响 1 行")
	}
	if got, _ := s.CatByID(ctx, cat.ID); got.Name != "用户中心" || *got.Desc != "描述" {
		t.Errorf("只应修改给出的字段: %+v", got)
	}
	if n, _ := s.SetCatPosition(ctx, cat.ID, -1); n != 1 {
		t.Error("SetCatPosition 应影响 1 行")
	}
	if cats, _ := s.CatsByProject(ctx, p.ID); cats[0].ID != cat.ID {
		t.Error("index 更小的分类应排在前面")
	}

	newInterface(t, s, cat, "x", "GET", "/x", nil)
	newInterface(t, s, cat, "y", "GET", "/y", nil)
	if n, err := s.DeleteCat(ctx, cat.ID); err != nil || n != 2 {
		t.Errorf("DeleteCat 应返回删除的接口数 2: %d %v", n, err)
	}
	if _, err := s.CatByID(ctx, cat.ID); !errors.Is(err, ErrNotFound) {
		t.Error("分类应已删除")
	}
}

func TestInterfaceEditor(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	_, p := newProject(t, s, "editor", nil)
	it := newInterface(t, s, defaultCat(t, s, p.ID), "x", "GET", "/x", nil)

	claim := func(uid int64) bool {
		ok, err := s.ClaimInterfaceEditor(ctx, it.ID, uid)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !claim(21) {
		t.Error("无人编辑时可以占用")
	}
	if !claim(21) {
		t.Error("同一用户可以重复占用")
	}
	if claim(22) {
		t.Error("别人正在编辑时不能占用")
	}
	// 只有编辑者本人能释放
	if err := s.ReleaseInterfaceEditor(ctx, it.ID, 22); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.InterfaceByID(ctx, it.ID); got.EditUID != 21 {
		t.Errorf("非编辑者不应能释放, edit_uid = %d", got.EditUID)
	}
	if err := s.ReleaseInterfaceEditor(ctx, it.ID, 21); err != nil {
		t.Fatal(err)
	}
	if !claim(22) {
		t.Error("释放后别人可以占用")
	}
	// 进程重启时清除所有编辑状态
	if err := s.resetInterfaceEditors(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.InterfaceByID(ctx, it.ID); got.EditUID != 0 {
		t.Errorf("重置后 edit_uid 应为 0: %d", got.EditUID)
	}
}

func TestCopyProject(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	u := newUser(t, s, "copier@example.com")
	g, src := newProject(t, s, "src", &Member{UID: u.ID, Role: "owner", Username: &u.Username, Email: &u.Email})
	cat := defaultCat(t, s, src.ID)
	newInterface(t, s, cat, "b", "GET", "/b", func(it *Interface) { it.Position = 3 })
	newInterface(t, s, cat, "a", "POST", "/a", nil)
	_, foreign := newProject(t, s, "foreign", nil)
	foreignCat := defaultCat(t, s, foreign.ID)
	newInterface(t, s, foreignCat, "secret", "GET", "/secret", nil)

	members, _ := s.ProjectMembers(ctx, src.ID)
	oldSubID := members[0].SubID
	for i := range members {
		members[i].SubID = ""
	}
	dst := &Project{UID: 31, GroupID: g.ID, Name: "copy", ProjectType: "private", Env: "[]", Tag: "[]", AddTime: 5, UpTime: 5}
	desc := "复制的分类"
	cats := []CopyCat{{SourceID: cat.ID, Name: "公共分类", Desc: &desc}, {SourceID: foreignCat.ID, Name: "借来的"}}
	if err := s.CopyProject(ctx, dst, src.ID, cats, members, 31, 5); err != nil {
		t.Fatal(err)
	}

	copied, _ := s.CatsByProject(ctx, dst.ID)
	if len(copied) != 2 || copied[0].Name != "公共分类" || *copied[0].Desc != "复制的分类" {
		t.Fatalf("应按顺序新建分类: %+v", copied)
	}
	list, _ := s.InterfacesByProject(ctx, dst.ID)
	// 按标题顺序复制，保留原 index；不属于源项目的分类下的接口不复制
	if got := titles(list); got != "a,b" {
		t.Errorf("复制的接口不对: %s", got)
	}
	for _, it := range list {
		if it.UID != 31 || it.ProjectID != dst.ID || it.CatID != copied[0].ID || it.AddTime != 5 {
			t.Errorf("复制出的接口应属于新项目和复制者: %+v", it)
		}
	}
	if m, err := s.ProjectMemberOf(ctx, dst.ID, u.ID); err != nil || m.Role != "owner" || m.SubID == oldSubID {
		t.Errorf("应复制成员并使用新的子文档 ID: %+v %v", m, err)
	}
}
