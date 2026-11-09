package migrate

import (
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/PandaPanY/apiyard/internal/store"
)

// memSource 是内存里的数据源，collections 保持插入顺序。
type memSource map[string][]bson.D

func (s memSource) Describe() string { return "测试数据" }
func (s memSource) Collections(context.Context) ([]string, error) {
	var names []string
	for k := range s {
		names = append(names, k)
	}
	return names, nil
}
func (s memSource) Count(_ context.Context, c string) (int64, error) { return int64(len(s[c])), nil }
func (s memSource) Each(_ context.Context, c string, fn func(bson.D) error) error {
	for _, d := range s[c] {
		if err := fn(d); err != nil {
			return err
		}
	}
	return nil
}
func (s memSource) Close(context.Context) error { return nil }

var oid = primitive.ObjectID{0x6a, 0xec, 0x49, 0x8d, 1, 2, 3, 4, 5, 6, 7, 8}

// sample 是一份覆盖各种边界情况的 YApi 数据。
func sample() memSource {
	return memSource{
		"user": {
			{{Key: "_id", Value: 11}, {Key: "username", Value: "admin"}, {Key: "email", Value: "admin@admin.com"},
				{Key: "password", Value: "fc92dd29"}, {Key: "passsalt", Value: "salt"}, {Key: "role", Value: "admin"},
				{Key: "add_time", Value: 1700000000.0}, {Key: "up_time", Value: int64(1700000001)}, {Key: "__v", Value: 0},
				{Key: "plugin_field", Value: "保留"}},
			{{Key: "_id", Value: 12.0}, {Key: "username", Value: "dev"}, {Key: "email", Value: "dev@x.com"},
				{Key: "password", Value: "p"}, {Key: "study", Value: "true"}},
			{{Key: "_id", Value: 13}, {Key: "username", Value: "重复"}, {Key: "email", Value: "dev@x.com"}},
			{{Key: "_id", Value: 14}, {Key: "username", Value: "无邮箱"}},
		},
		"group": {
			{{Key: "_id", Value: 20}, {Key: "uid", Value: 11}, {Key: "group_name", Value: "后端"}, {Key: "type", Value: "public"},
				{Key: "members", Value: bson.A{
					bson.D{{Key: "_id", Value: oid}, {Key: "uid", Value: 12}, {Key: "role", Value: "dev"}, {Key: "username", Value: "dev"}},
					bson.D{{Key: "uid", Value: 12}, {Key: "role", Value: "owner"}},
					bson.D{{Key: "uid", Value: 13}, {Key: "role", Value: "boss"}},
					bson.D{{Key: "role", Value: "dev"}},
				}},
				{Key: "custom_field1", Value: nil}},
			{{Key: "_id", Value: 21}, {Key: "group_name", Value: "个人空间"}, {Key: "type", Value: "private"}, {Key: "uid", Value: 12}},
		},
		"project": {
			{{Key: "_id", Value: 30}, {Key: "uid", Value: 11}, {Key: "name", Value: "支付"}, {Key: "group_id", Value: 20},
				{Key: "project_type", Value: "private"}, {Key: "basepath", Value: "/pay"},
				{Key: "is_mock_open", Value: true}, {Key: "project_mock_script", Value: "mockJson.a = 1"},
				{Key: "env", Value: bson.A{bson.D{{Key: "_id", Value: oid}, {Key: "name", Value: "local"}, {Key: "domain", Value: "http://127.0.0.1"}}}},
				{Key: "members", Value: bson.A{bson.D{{Key: "uid", Value: 12}, {Key: "role", Value: "owner"}, {Key: "email_notice", Value: false}}}}},
			{{Key: "_id", Value: 31}, {Key: "name", Value: "孤儿项目"}, {Key: "group_id", Value: 99}, {Key: "project_type", Value: "public"}},
		},
		"interface_cat": {
			{{Key: "_id", Value: 40}, {Key: "name", Value: "公共分类"}, {Key: "project_id", Value: 30}, {Key: "uid", Value: 11}, {Key: "index", Value: "2"}},
			{{Key: "_id", Value: 41}, {Key: "name", Value: "孤儿分类"}, {Key: "project_id", Value: 31}},
		},
		"interface": {
			{{Key: "_id", Value: 50}, {Key: "title", Value: "下单"}, {Key: "path", Value: "/orders/{id}"}, {Key: "method", Value: "POST"},
				{Key: "project_id", Value: 30}, {Key: "catid", Value: 40}, {Key: "uid", Value: 11}, {Key: "edit_uid", Value: 12},
				{Key: "status", Value: "done"}, {Key: "req_body_type", Value: ""}, {Key: "res_body_type", Value: "json"},
				{Key: "res_body", Value: `{"type":"object"}`}, {Key: "res_body_is_json_schema", Value: true},
				{Key: "req_query", Value: bson.A{bson.D{{Key: "_id", Value: oid}, {Key: "name", Value: "page"}}}},
				{Key: "query_path", Value: bson.D{{Key: "path", Value: "/orders/{id}"}, {Key: "params", Value: bson.A{}}}},
				{Key: "tag", Value: bson.A{"v1"}}, {Key: "field2", Value: "插件字段"},
				{Key: "add_time", Value: primitive.DateTime(0)}},
			{{Key: "_id", Value: 51}, {Key: "title", Value: "分类不存在"}, {Key: "project_id", Value: 30}, {Key: "catid", Value: 41}},
			{{Key: "_id", Value: "abc"}, {Key: "title", Value: "ID 不对"}},
		},
		"token": {
			{{Key: "_id", Value: 1}, {Key: "project_id", Value: 30}, {Key: "token", Value: "aaaa"}},
			{{Key: "_id", Value: 2}, {Key: "project_id", Value: 30}, {Key: "token", Value: "bbbb"}},
			{{Key: "_id", Value: 3}, {Key: "project_id", Value: 31}, {Key: "token", Value: "cccc"}},
		},
		"follow": {
			{{Key: "_id", Value: 60}, {Key: "uid", Value: 12}, {Key: "projectid", Value: 30}, {Key: "projectname", Value: "支付"}},
		},
		"log": {
			{{Key: "_id", Value: 70}, {Key: "uid", Value: 11}, {Key: "username", Value: "admin"}, {Key: "type", Value: "project"},
				{Key: "typeid", Value: 30}, {Key: "content", Value: "<a>新建</a>"},
				{Key: "data", Value: bson.D{{Key: "interface_id", Value: 50}, {Key: "old", Value: nil}}}, {Key: "add_time", Value: 1700000000}},
			{{Key: "_id", Value: 71}, {Key: "type", Value: "wiki"}, {Key: "typeid", Value: 30}},
		},
		"avatar": {
			{{Key: "_id", Value: oid}, {Key: "uid", Value: 12}, {Key: "basecode", Value: "aGVsbG8"}, {Key: "type", Value: "image/png"}},
			{{Key: "_id", Value: oid}, {Key: "uid", Value: 12}, {Key: "basecode", Value: "d29ybGQ="}, {Key: "type", Value: "image/png"}},
		},
		"identitycounters": {
			{{Key: "model", Value: "interface"}, {Key: "field", Value: "_id"}, {Key: "count", Value: 500}},
			{{Key: "model", Value: "token"}, {Key: "field", Value: "_id"}, {Key: "count", Value: 9}},
		},
		"interface_col": {{{Key: "_id", Value: 1}}},
		"wiki":          {},
	}
}

func run(t *testing.T, src Source, opt Options) (*Report, *store.Store) {
	t.Helper()
	if opt.Target == "" {
		opt.Target = filepath.Join(t.TempDir(), "apiyard.db")
	}
	rep, err := Run(context.Background(), src, opt)
	if err != nil {
		t.Fatal(err)
	}
	if opt.DryRun {
		return rep, nil
	}
	st, err := store.Open(context.Background(), opt.Target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return rep, st
}

func TestMigrate(t *testing.T) {
	ctx := context.Background()
	rep, st := run(t, sample(), Options{})

	want := map[string][3]int{ // 源数据、已迁移、跳过
		"user": {4, 2, 2}, "group": {2, 2, 0}, "project": {2, 1, 1}, "interface_cat": {2, 1, 1}, "interface": {3, 1, 2},
		"token": {3, 1, 2}, "follow": {1, 1, 0}, "log": {2, 1, 1}, "avatar": {2, 1, 1},
	}
	for _, s := range rep.Collections {
		if got := [3]int{s.Source, s.Migrated, s.Skipped}; got != want[s.Name] {
			t.Errorf("%s 的数量为 %v，应为 %v", s.Name, got, want[s.Name])
		}
	}
	if len(rep.Unsupported) != 1 || rep.Unsupported[0].Name != "interface_col" {
		t.Errorf("未迁移的集合应只列出有数据的 interface_col: %+v", rep.Unsupported)
	}
	var out bytes.Buffer
	rep.Print(&out)
	for _, s := range []string{"所属分组不存在", "所属分类不存在", "_id 不是正整数", "UNIQUE constraint failed: users.email",
		"项目已有 token", "成员重复", "全局 Mock 脚本", "role 的取值 \"boss\" 无效", "req_body_type 的取值 \"\" 无效"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("报告里应提到 %q:\n%s", s, out.String())
		}
	}

	// 用户：数字类型的 ID、时间都能读取，字符串 "true" 按 Mongoose 的规则转成布尔值，未知字段进 extra
	u, err := st.UserByID(ctx, 12)
	if err != nil || !u.Study || u.Role != "member" || u.Type != "site" {
		t.Errorf("用户 12: %+v, %v", u, err)
	}
	admin, _ := st.UserByID(ctx, 11)
	if admin.AddTime != 1700000000 || admin.Password != "fc92dd29" {
		t.Errorf("管理员: %+v", admin)
	}

	// 分组成员：重复的只保留第一个，无效角色改为 guest，没有 uid 的忽略；子文档 ID 保留
	members, _ := st.GroupMembers(ctx, 20)
	if len(members) != 2 || members[0].UID != 12 || members[0].Role != "dev" || members[0].SubID != oid.Hex() ||
		members[1].UID != 13 || members[1].Role != "guest" {
		t.Errorf("分组成员: %+v", members)
	}
	g, _ := st.GroupByID(ctx, 20)
	if g.CustomField1 != nil {
		t.Errorf("custom_field1 为 null 时应保留 null: %v", *g.CustomField1)
	}
	if g, _ := st.GroupByID(ctx, 21); g.CustomField1 == nil || *g.CustomField1 != `{"enable":false}` {
		t.Errorf("custom_field1 不存在时应取默认值: %+v", g)
	}

	p, _ := st.ProjectByID(ctx, 30)
	if p.Env != `[{"_id":"`+oid.Hex()+`","name":"local","domain":"http://127.0.0.1","header":[],"global":[]}]` || !p.IsJSON5 {
		t.Errorf("项目环境: %s", p.Env)
	}
	pm, _ := st.ProjectMembers(ctx, 30)
	if len(pm) != 1 || pm[0].EmailNotice {
		t.Errorf("项目成员: %+v", pm)
	}

	c, _ := st.CatByID(ctx, 40)
	if c.Position != 2 {
		t.Errorf("分类的 index 是字符串 \"2\"，应读作 2: %+v", c)
	}

	it, err := st.InterfaceByID(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case it.EditUID != 0:
		t.Error("编辑锁不应迁移")
	case it.ReqBodyType != nil:
		t.Errorf("无效的 req_body_type 应清空: %v", *it.ReqBodyType)
	case it.ReqQuery != `[{"_id":"`+oid.Hex()+`","name":"page","required":"1"}]`:
		t.Errorf("req_query 应补上 required 的默认值: %s", it.ReqQuery)
	case it.Tag != `["v1"]` || it.Type != "static" || !it.ResBodyIsJSONSchema || it.Desc != nil:
		t.Errorf("接口: %+v", it)
	}

	if tok, _ := st.ProjectToken(ctx, 30); tok != "aaaa" {
		t.Errorf("同一项目有多个 token 时应保留第一个: %s", tok)
	}
	if a, _ := st.AvatarOf(ctx, 12); string(a.Image) != "hello" {
		t.Errorf("头像应按宽松的 base64 解码: %q", a.Image)
	}
	logs, _ := st.ListLogs(ctx, store.LogFilter{Type: "project", TypeID: 30}, 1, 10)
	if len(logs) != 1 || logs[0].Data == nil || *logs[0].Data != `{"interface_id":50,"old":null}` {
		t.Errorf("动态: %+v", logs)
	}

	// 自增序列：取 YApi 计数器和已有最大 ID 中较大的
	if seq, _ := st.Sequence(ctx, "interfaces"); seq != 500 {
		t.Errorf("interfaces 序列应抬高到 500: %d", seq)
	}
	if seq, _ := st.Sequence(ctx, "users"); seq != 12 {
		t.Errorf("users 序列应为最大 ID 12: %d", seq)
	}
}

func TestExtraAndTimes(t *testing.T) {
	rep := newReport("")
	d := newDoc("interface", bson.D{{Key: "_id", Value: 1}, {Key: "title", Value: "a"}, {Key: "field2", Value: "x"},
		{Key: "at", Value: primitive.DateTime(1700000000123)}, {Key: "n", Value: 1.5}}, rep)
	d.str("title")
	if got := d.extra(); got != `{"field2":"x","at":"2023-11-14T22:13:20.123Z","n":1.5}` {
		t.Errorf("extra: %s", got)
	}
	if d.int("n", 7) != 7 || len(rep.Fixed) != 1 {
		t.Errorf("小数不能当整数读取，应记一处修正: %+v", rep.Fixed)
	}
}

func TestTargetHandling(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "apiyard.db")

	// 预检不写入目标文件，也不留下临时文件
	run(t, sample(), Options{Target: target, DryRun: true})
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("预检后目录应为空: %v", entries)
	}

	if _, err := Run(context.Background(), sample(), Options{Target: target}); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), sample(), Options{Target: target}); err == nil || !strings.Contains(err.Error(), "-overwrite") {
		t.Errorf("目标已存在时应要求 -overwrite: %v", err)
	}

	// 覆盖：替换成新数据，而不是叠加
	src := sample()
	src["user"] = src["user"][:1]
	_, st := run(t, src, Options{Target: target, Overwrite: true})
	if n, _ := st.CountRows(context.Background(), "users"); n != 1 {
		t.Errorf("覆盖后应只有新数据: %d", n)
	}
}

func TestDumpSource(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dump")
	db := filepath.Join(dir, "yapi")
	if err := os.MkdirAll(db, 0o750); err != nil {
		t.Fatal(err)
	}
	write := func(name string, docs []bson.D, gz bool) {
		var buf bytes.Buffer
		for _, d := range docs {
			data, err := bson.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			buf.Write(data)
		}
		data := buf.Bytes()
		if gz {
			var z bytes.Buffer
			w := gzip.NewWriter(&z)
			_, _ = w.Write(data)
			_ = w.Close()
			data = z.Bytes()
		}
		if gz {
			name += ".gz"
		}
		if err := os.WriteFile(filepath.Join(db, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	src := sample()
	for name, docs := range src {
		write(name+".bson", docs, name == "interface")
	}
	if _, err := OpenDump(filepath.Join(t.TempDir(), "none"), ""); err == nil {
		t.Error("没有 user.bson 的目录应报错")
	}
	dump, err := OpenDump(dir, "") // 上级目录：自动进入 yapi 子目录
	if err != nil {
		t.Fatal(err)
	}
	rep, st := run(t, dump, Options{})
	if it, err := st.InterfaceByID(context.Background(), 50); err != nil || it.Title != "下单" {
		t.Errorf("应能读取 .bson.gz: %+v, %v", it, err)
	}
	if rep.stat("user").Migrated != 2 || len(rep.Unsupported) != 1 {
		t.Errorf("从导出目录迁移的结果应与直连相同: %+v", rep.Collections)
	}

	if err := os.WriteFile(filepath.Join(db, "user.bson"), []byte{1, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), dump, Options{Target: filepath.Join(t.TempDir(), "x.db")}); err == nil {
		t.Error("损坏的 .bson 文件应报错")
	}
}
