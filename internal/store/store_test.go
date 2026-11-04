package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// openTest 在临时目录里打开一个全新的数据库。
func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newUser(t *testing.T, s *Store, email string) *User {
	t.Helper()
	u := &User{Username: email[:3], Email: email, Password: "h", Passsalt: "s", Role: "member", Type: "site", AddTime: 1, UpTime: 1}
	if err := s.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestMigrateIsIdempotentAndIDsStartAt11(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	u := &User{Username: "admin", Email: "admin@admin.com", Password: "h", Passsalt: "s", Role: "admin", Type: "site"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if u.ID != 11 {
		t.Errorf("第一个用户 ID = %d, 期望与 YApi 一样从 11 开始", u.ID)
	}
	_ = s.Close()

	// 重新打开时不应重复执行迁移，数据保留
	s, err = Open(ctx, file)
	if err != nil {
		t.Fatalf("重新打开失败: %v", err)
	}
	defer func() { _ = s.Close() }()
	if n, _ := s.CountUsers(ctx); n != 1 {
		t.Errorf("重新打开后用户数 = %d, 期望 1", n)
	}
}

func TestUsers(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	a := newUser(t, s, "alice@example.com")
	b := newUser(t, s, "bob@Example.com")

	if got, err := s.UserByEmail(ctx, "bob@Example.com"); err != nil || got.ID != b.ID {
		t.Errorf("UserByEmail = %+v, %v", got, err)
	}
	if _, err := s.UserByID(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("不存在的用户应返回 ErrNotFound, 实际 %v", err)
	}
	if err := s.CreateUser(ctx, &User{Username: "x", Email: a.Email, Password: "h", Passsalt: "s", Role: "member", Type: "site"}); err == nil {
		t.Error("重复邮箱应被拒绝")
	}

	list, _ := s.ListUsers(ctx, 1, 1)
	if len(list) != 1 || list[0].ID != b.ID {
		t.Errorf("ListUsers 应按 ID 倒序分页, 实际 %+v", list)
	}
	found, _ := s.SearchUsers(ctx, "EXAMPLE")
	if len(found) != 2 {
		t.Errorf("SearchUsers 应不区分大小写, 找到 %d 个", len(found))
	}

	if n, _ := s.MarkStudied(ctx, a.ID); n != 1 {
		t.Errorf("MarkStudied 影响行数 = %d", n)
	}
	if got, _ := s.UserByID(ctx, a.ID); !got.Study {
		t.Error("MarkStudied 后 Study 应为 true")
	}
}

func TestUpdateProfileSyncsMemberCopies(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	u := newUser(t, s, "dev@example.com")
	g := &Group{UID: u.ID, Name: "g", Type: "public"}
	if err := s.CreateGroup(ctx, g, []Member{{UID: u.ID, Role: "owner", Username: &u.Username, Email: &u.Email}}); err != nil {
		t.Fatal(err)
	}

	name := "新名字"
	if n, err := s.UpdateProfile(ctx, u.ID, &name, nil); n != 1 || err != nil {
		t.Fatalf("UpdateProfile: %d %v", n, err)
	}
	got, _ := s.UserByID(ctx, u.ID)
	if got.Username != name || got.Email != u.Email {
		t.Errorf("只应修改用户名: %+v", got)
	}
	if m, _ := s.GroupMember(ctx, g.ID, u.ID); *m.Username != name {
		t.Errorf("分组成员里的用户名副本应同步更新: %v", *m.Username)
	}
	if n, _ := s.UpdateProfile(ctx, 999, &name, nil); n != 0 {
		t.Error("不存在的用户应返回 0")
	}
}

func TestPasswordAndDelete(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	u := newUser(t, s, "dev@example.com")

	if n, _ := s.UpdatePassword(ctx, u.ID, "h2", "s2"); n != 1 {
		t.Error("UpdatePassword 应影响 1 行")
	}
	if got, _ := s.UserByID(ctx, u.ID); got.Password != "h2" || got.Passsalt != "s2" {
		t.Errorf("密码未更新: %+v", got)
	}
	if n, _ := s.DeleteUser(ctx, u.ID); n != 1 {
		t.Error("DeleteUser 应影响 1 行")
	}
	if _, err := s.UserByID(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Error("删除后应查不到用户")
	}
}

func TestAvatars(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	if created, err := s.SaveAvatar(ctx, 11, Avatar{Image: []byte{1}, Type: "image/png"}); !created || err != nil {
		t.Fatalf("首次保存应为新建: %v %v", created, err)
	}
	if created, _ := s.SaveAvatar(ctx, 11, Avatar{Image: []byte{2}, Type: "image/jpeg"}); created {
		t.Error("再次保存应为覆盖")
	}
	if a, _ := s.AvatarOf(ctx, 11); a.Type != "image/jpeg" || a.Image[0] != 2 {
		t.Errorf("头像未被覆盖: %+v", a)
	}
}

func TestGroupsAndMembers(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	owner := newUser(t, s, "own@example.com")
	dev := newUser(t, s, "dev@example.com")

	g := &Group{UID: owner.ID, Name: "后端组", Type: "public", AddTime: 1, UpTime: 1}
	if err := s.CreateGroup(ctx, g, []Member{{UID: owner.ID, Role: "owner"}}); err != nil {
		t.Fatal(err)
	}
	if g.CustomField1 == nil || *g.CustomField1 != `{"enable":false}` {
		t.Errorf("custom_field1 默认值不对: %v", g.CustomField1)
	}
	if exists, _ := s.GroupNameExists(ctx, "后端组"); !exists {
		t.Error("GroupNameExists 应返回 true")
	}

	if ok, err := s.AddGroupMembers(ctx, g.ID, []Member{{UID: dev.ID, Role: "dev"}}); !ok || err != nil {
		t.Fatalf("AddGroupMembers: %v %v", ok, err)
	}
	if ok, _ := s.AddGroupMembers(ctx, 999, []Member{{UID: dev.ID, Role: "dev"}}); ok {
		t.Error("向不存在的分组添加成员应返回 false")
	}
	members, _ := s.GroupMembers(ctx, g.ID)
	if len(members) != 2 || members[0].UID != owner.ID || members[1].UID != dev.ID || len(members[1].SubID) != 24 {
		t.Errorf("成员应按加入顺序排列且带 24 位 SubID: %+v", members)
	}

	if groups, _ := s.MemberGroups(ctx, dev.ID); len(groups) != 1 {
		t.Errorf("成员应能看到所在分组, 实际 %d 个", len(groups))
	}
	if n, _ := s.ChangeGroupMemberRole(ctx, g.ID, dev.ID, "owner"); n != 1 {
		t.Error("ChangeGroupMemberRole 应影响 1 行")
	}
	if m, _ := s.GroupMember(ctx, g.ID, dev.ID); m.Role != "owner" {
		t.Errorf("角色未更新: %+v", m)
	}

	// 修改时不传 custom_field1 会写成 null，与 YApi 一致
	if n, _ := s.UpdateGroup(ctx, g.ID, "后端组2", nil, nil); n != 1 {
		t.Error("UpdateGroup 应影响 1 行")
	}
	if got, _ := s.GroupByID(ctx, g.ID); got.Name != "后端组2" || got.CustomField1 != nil || got.Desc != nil {
		t.Errorf("UpdateGroup 结果不对: %+v", got)
	}

	if n, _ := s.RemoveGroupMember(ctx, g.ID, dev.ID); n != 1 {
		t.Error("RemoveGroupMember 应影响 1 行")
	}
	if n, _ := s.DeleteGroup(ctx, g.ID); n != 1 {
		t.Error("DeleteGroup 应影响 1 行")
	}
	if members, _ := s.GroupMembers(ctx, g.ID); len(members) != 0 {
		t.Error("删除分组后成员应被级联删除")
	}
}

func TestPrivateAndVisibleGroups(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	u := newUser(t, s, "u@example.com")

	if _, err := s.PrivateGroupOf(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("尚未创建个人空间时应返回 ErrNotFound, 实际 %v", err)
	}
	if err := s.CreateGroup(ctx, &Group{UID: u.ID, Name: "User-11", Type: "private"}, nil); err != nil {
		t.Fatal(err)
	}
	if g, err := s.PrivateGroupOf(ctx, u.ID); err != nil || g.Type != "private" {
		t.Errorf("PrivateGroupOf = %+v, %v", g, err)
	}

	pub := &Group{UID: 999, Name: "公共组", Type: "public"}
	if err := s.CreateGroup(ctx, pub, nil); err != nil {
		t.Fatal(err)
	}
	if all, _ := s.PublicGroups(ctx); len(all) != 1 || all[0].ID != pub.ID {
		t.Errorf("PublicGroups 不应包含个人空间: %+v", all)
	}
	if got, _ := s.PublicGroupsByIDs(ctx, []int64{pub.ID, 12345}); len(got) != 1 {
		t.Errorf("PublicGroupsByIDs 应只返回存在的公共分组: %+v", got)
	}
	if ids, err := s.VisibleProjectGroupIDs(ctx, u.ID); err != nil || len(ids) != 0 {
		t.Errorf("没有项目时应返回空列表: %v %v", ids, err)
	}
}

func TestCreateLog(t *testing.T) {
	s := openTest(t)
	if err := s.CreateLog(context.Background(), Log{UID: 11, Username: "admin", Type: "group", TypeID: 12, Content: "<a>x</a>"}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateLog(context.Background(), Log{UID: 11, Username: "admin", Type: "bad", TypeID: 1, Content: "x"}); err == nil {
		t.Error("非法的动态类型应被拒绝")
	}
}

func TestNewObjectID(t *testing.T) {
	a, b := NewObjectID(), NewObjectID()
	if len(a) != 24 || a == b {
		t.Errorf("NewObjectID 应生成不重复的 24 位十六进制: %s %s", a, b)
	}
}
