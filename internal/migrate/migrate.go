// Package migrate 把 YApi 的数据（MongoDB）迁移到 ApiYard 的 SQLite 数据库，原样保留 ID，
// 迁移后原来的账号密码、项目 token、接口链接都继续有效。字段对应关系见 docs/schema.md，用法见 docs/migration.md。
package migrate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"modernc.org/sqlite"

	"github.com/PandaPanY/apiyard/internal/store"
)

// Options 是一次迁移的设置。
type Options struct {
	Target    string    // 目标 SQLite 数据库文件
	Overwrite bool      // 目标文件已存在时替换它
	DryRun    bool      // 只预检：完整执行一遍迁移和核对，但不写入目标文件
	Progress  io.Writer // 进度输出，可以为 nil
}

// 各集合迁移到的表。顺序就是迁移顺序：被引用的数据在前。
var collections = []struct {
	name, table string
}{
	{"user", "users"}, {"group", "groups"}, {"project", "projects"}, {"interface_cat", "interface_cats"},
	{"interface", "interfaces"}, {"token", "tokens"}, {"follow", "follows"}, {"log", "logs"}, {"avatar", "avatars"},
}

const countersCollection = "identitycounters" // YApi 的自增 ID 计数器

type migrator struct {
	src     Source
	rep     *Report
	im      *store.Importer
	ids     map[string]map[int64]bool // 已迁移记录的 ID，按集合
	members map[string]int64          // 写入的成员数，按表
	checks  []check
}

// check 在迁移完成后用 ApiYard 平时读取数据的方法读回一条记录，与源数据对比；不一致时返回说明。
type check func(ctx context.Context, st *store.Store) (string, error)

// Run 执行迁移。数据先写进临时文件，全部写完并核对通过后才替换目标文件，中途失败不会留下半成品。
// 返回的报告列出各集合的数量和跳过、修正的记录。
func Run(ctx context.Context, src Source, opt Options) (*Report, error) {
	start := time.Now()
	lock, err := os.OpenFile(opt.Target+".migrate.lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("无法锁定迁移目标（确认没有其他迁移正在运行）: %w", err)
	}
	_ = lock.Close()
	defer func() { _ = os.Remove(opt.Target + ".migrate.lock") }()
	if !opt.DryRun && !opt.Overwrite {
		if _, err := os.Stat(opt.Target); err == nil {
			return nil, fmt.Errorf("目标数据库 %s 已存在；要用迁移的数据替换它，请加 -overwrite", opt.Target)
		}
	}
	f, err := os.CreateTemp(filepath.Dir(opt.Target), filepath.Base(opt.Target)+".migrating-*")
	if err != nil {
		return nil, err
	}
	tmp := f.Name()
	if err := f.Close(); err != nil {
		return nil, err
	}
	defer func() { _ = removeDB(tmp) }() // 成功时临时文件已改名，这里什么也不做

	st, err := store.Open(ctx, tmp)
	if err != nil {
		return nil, err
	}
	m := &migrator{src: src, rep: newReport(src.Describe()), ids: map[string]map[int64]bool{}, members: map[string]int64{}}
	err = st.Import(ctx, func(im *store.Importer) error {
		m.im = im
		return m.run(ctx, opt.Progress)
	})
	if err == nil {
		err = m.verify(ctx, st)
	}
	if cerr := st.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return m.rep, err
	}
	m.rep.Elapsed = time.Since(start)
	if opt.DryRun {
		return m.rep, nil
	}
	return m.rep, installDatabase(tmp, opt.Target, opt.Overwrite, opt.Progress, os.Rename)
}

// installDatabase never deletes the destination before installing the verified
// database. A durable copy is retained before replacing an existing database.
func installDatabase(tmp, target string, overwrite bool, progress io.Writer, rename func(string, string) error) error {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(target + suffix); err == nil {
			return fmt.Errorf("目标数据库存在 %s；请停止服务并完成 checkpoint 后重试", suffix)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if !overwrite {
		return os.Link(tmp, target)
	} // atomic no-replace installation
	old, err := os.Open(target) // #nosec G304 -- target is the operator-selected CLI database path
	if errors.Is(err, os.ErrNotExist) {
		return os.Link(tmp, target)
	}
	if err != nil {
		return err
	}
	defer func() { _ = old.Close() }()
	backup, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".before-migrate-*")
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(backup, old)
	if copyErr == nil {
		copyErr = backup.Sync()
	}
	closeErr := backup.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return fmt.Errorf("备份旧库失败，原库保留: %w", copyErr)
	}
	if progress != nil {
		_, _ = fmt.Fprintf(progress, "旧库备份：%s\n", backup.Name())
	}
	if err := old.Close(); err != nil {
		return err
	}
	if err := rename(tmp, target); err != nil {
		return fmt.Errorf("替换数据库失败；旧库备份保留在 %s: %w", backup.Name(), err)
	}
	return nil
}

// removeDB 删除 SQLite 数据库文件及其 WAL 文件，文件不存在不算错误。
func removeDB(file string) error {
	for _, f := range []string{file, file + "-wal", file + "-shm", file + "-journal"} {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (m *migrator) run(ctx context.Context, progress io.Writer) error {
	handlers := map[string]func(context.Context, *doc) error{
		"user": m.user, "group": m.group, "project": m.project, "interface_cat": m.cat, "interface": m.iface,
		"token": m.token, "follow": m.follow, "log": m.log, "avatar": m.avatar,
	}
	for _, c := range collections {
		begin := time.Now()
		stat := m.rep.stat(c.name)
		m.ids[c.name] = map[int64]bool{}
		err := m.src.Each(ctx, c.name, func(raw bson.D) error {
			stat.Source++
			return handlers[c.name](ctx, newDoc(c.name, raw, m.rep))
		})
		if err != nil {
			return fmt.Errorf("迁移 %s: %w", c.name, err)
		}
		if progress != nil {
			_, _ = fmt.Fprintf(progress, "%-14s 源数据 %d 条，已迁移 %d 条，%.1f 秒\n",
				c.name, stat.Source, stat.Migrated, time.Since(begin).Seconds())
		}
	}
	if err := m.sequences(ctx); err != nil {
		return err
	}
	return m.unsupported(ctx)
}

// insert 写入一行。违反约束（例如邮箱重复）时跳过这条记录并记入报告，其他错误中止迁移。
func (m *migrator) insert(ctx context.Context, d *doc, table string, cols []string, vals []any) (bool, error) {
	err := m.im.Insert(ctx, table, cols, vals)
	if err == nil {
		return true, nil
	}
	var se *sqlite.Error
	if errors.As(err, &se) && se.Code()&0xff == 19 { // SQLITE_CONSTRAINT
		m.rep.skipped(d.coll, "写入时违反约束："+constraintMsg(err), d.id)
		return false, nil
	}
	return false, fmt.Errorf("写入 %s（_id %s）: %w", table, brief(d.id), err)
}

// constraintMsg 去掉错误信息里的前缀和错误码，例如 "UNIQUE constraint failed: users.email"，便于按原因汇总。
func constraintMsg(err error) string {
	msg := strings.TrimPrefix(err.Error(), "constraint failed: ")
	return errCodeSuffix.ReplaceAllString(msg, "")
}

var errCodeSuffix = regexp.MustCompile(`\s*\(\d+\)$`)

// done 记下一条迁移成功的记录。
func (m *migrator) done(coll string, id int64) {
	m.rep.stat(coll).Migrated++
	m.ids[coll][id] = true
}

// need 检查 d 引用的记录（例如接口所属的项目）已经迁移过来；没有时跳过 d。
func (m *migrator) need(d *doc, key, coll, what string) (int64, bool) {
	id := d.int(key, 0)
	if !m.ids[coll][id] {
		m.rep.skipped(d.coll, what+"不存在", d.id)
		return 0, false
	}
	return id, true
}

func (m *migrator) mainID(d *doc) (int64, bool) {
	id, ok := d.intID()
	if !ok {
		m.rep.skipped(d.coll, "_id 不是正整数", d.id)
	}
	return id, ok
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---- 各集合 ----

func (m *migrator) user(ctx context.Context, d *doc) error {
	id, ok := m.mainID(d)
	if !ok {
		return nil
	}
	username, email, password := d.strOr("username", ""), d.strOr("email", ""), d.strOr("password", "")
	if email == "" {
		m.rep.skipped(d.coll, "缺少邮箱，无法登录", d.id)
		return nil
	}
	vals := []any{id, username, email, password, d.strOr("passsalt", ""), d.strOr("role", "member"),
		*d.enum("type", []string{"site", "third"}, ptr("site")), boolInt(d.bool("study", false)),
		d.int("add_time", 0), d.int("up_time", 0)}
	vals = append(vals, d.extra())
	ok, err := m.insert(ctx, d, "users",
		[]string{"id", "username", "email", "password", "passsalt", "role", "type", "study", "add_time", "up_time", "extra"}, vals)
	if !ok {
		return err
	}
	m.done(d.coll, id)
	m.checks = append(m.checks, func(ctx context.Context, st *store.Store) (string, error) {
		u, err := st.UserByID(ctx, id)
		if err != nil {
			return "", err
		}
		return diff("user", id, "username", u.Username, username, "email", u.Email, email, "password", u.Password, password), nil
	})
	return nil
}

// member 是分组或项目的一个成员。
type member struct {
	uid             int64
	role            string
	username, email *string
	notice          bool
	subID           string
}

var memberRoles = []string{"owner", "dev", "guest"}

// readMembers 读取 members 数组：去掉没有 uid 的成员；同一用户出现多次时只保留第一个（YApi 的权限判断也只看第一个）。
func (m *migrator) readMembers(d *doc) []member {
	v, ok := d.get("members")
	if !ok {
		return nil
	}
	list, ok := v.(primitive.A)
	if !ok {
		d.fix("members 不是数组，已忽略")
		return nil
	}
	var out []member
	seen := map[int64]bool{}
	for _, item := range list {
		raw, ok := item.(primitive.D)
		if !ok {
			d.fix("members 里有不是对象的元素，已忽略")
			continue
		}
		sub := newDoc(d.coll, raw, m.rep)
		sub.id = d.id
		uid, ok := toInt(sub.vals["uid"])
		switch {
		case !ok || uid <= 0:
			d.fix("有成员缺少 uid，已忽略")
			continue
		case seen[uid]:
			d.fix("成员重复，只保留第一个")
			continue
		}
		seen[uid] = true
		out = append(out, member{
			uid: uid, role: *sub.enum("role", memberRoles, ptr("guest")),
			username: sub.str("username"), email: sub.str("email"),
			notice: sub.bool("email_notice", true), subID: subID(sub.vals["_id"]),
		})
	}
	return out
}

func (m *migrator) insertMembers(ctx context.Context, d *doc, table, owner string, ownerID int64, list []member) error {
	for i, mb := range list {
		cols := []string{owner, "uid", "role", "username", "email", "sub_id", "position"}
		vals := []any{ownerID, mb.uid, mb.role, mb.username, mb.email, mb.subID, i}
		if table == "project_members" {
			cols = append(cols, "email_notice")
			vals = append(vals, boolInt(mb.notice))
		}
		if err := m.im.Insert(ctx, table, cols, vals); err != nil {
			return fmt.Errorf("写入 %s（_id %s）的成员: %w", d.coll, brief(d.id), err)
		}
		m.members[table]++
	}
	return nil
}

// memberCheck 读回成员列表，对比 uid 和角色。
func memberCheck(coll string, id int64, want []member, got []store.Member) string {
	format := func(uid int64, role string) string { return fmt.Sprintf("%d:%s", uid, role) }
	var w, g []string
	for _, mb := range want {
		w = append(w, format(mb.uid, mb.role))
	}
	for _, mb := range got {
		g = append(g, format(mb.UID, mb.Role))
	}
	return diff(coll, id, "members", strings.Join(g, ","), strings.Join(w, ","))
}

func (m *migrator) group(ctx context.Context, d *doc) error {
	id, ok := m.mainID(d)
	if !ok {
		return nil
	}
	// custom_field1 不存在时取 Mongoose 的默认值；值为 null 时保留 null（YApi 修改分组时不传这个字段就会变成 null）
	var custom any = `{"enable":false}`
	if v, present := d.present("custom_field1"); present {
		if v == nil {
			custom = nil
		} else {
			custom = d.object("custom_field1", `{"enable":false}`)
		}
	}
	name, typ := d.strOr("group_name", ""), *d.enum("type", []string{"public", "private"}, ptr("public"))
	members := m.readMembers(d)
	vals := []any{id, d.int("uid", 0), name, d.str("group_desc"), typ, custom, d.int("add_time", 0), d.int("up_time", 0)}
	vals = append(vals, d.extra())
	ok, err := m.insert(ctx, d, "groups",
		[]string{"id", "uid", "group_name", "group_desc", "type", "custom_field1", "add_time", "up_time", "extra"}, vals)
	if !ok {
		return err
	}
	if err := m.insertMembers(ctx, d, "group_members", "group_id", id, members); err != nil {
		return err
	}
	m.done(d.coll, id)
	m.checks = append(m.checks, func(ctx context.Context, st *store.Store) (string, error) {
		g, err := st.GroupByID(ctx, id)
		if err != nil {
			return "", err
		}
		got, err := st.GroupMembers(ctx, id)
		if err != nil {
			return "", err
		}
		return join(diff("group", id, "group_name", g.Name, name, "type", g.Type, typ), memberCheck("group", id, members, got)), nil
	})
	return nil
}

func (m *migrator) project(ctx context.Context, d *doc) error {
	id, ok := m.mainID(d)
	if !ok {
		return nil
	}
	groupID, ok := m.need(d, "group_id", "group", "所属分组")
	if !ok {
		return nil
	}
	name, typ := d.strOr("name", ""), *d.enum("project_type", []string{"public", "private"}, ptr("private"))
	members := m.readMembers(d)
	if d.bool("is_mock_open", false) && d.strOr("project_mock_script", "") != "" {
		// 脚本照常迁移，但 ApiYard 不执行（D5），这些项目的 Mock 数据会与原来不同
		m.rep.notice(d.coll, "开启了全局 Mock 脚本，ApiYard 不执行脚本，Mock 数据会与原来不同", d.id)
	}
	vals := []any{id, d.int("uid", 0), groupID, name, d.str("basepath"), d.str("desc"), typ,
		boolInt(d.bool("switch_notice", true)), d.str("icon"), d.str("color"),
		d.array("env", "header", []any{}, "global", []any{}), d.array("tag"),
		d.str("pre_script"), d.str("after_script"), d.str("project_mock_script"),
		boolInt(d.bool("is_mock_open", false)), boolInt(d.bool("strice", false)), boolInt(d.bool("is_json5", true)),
		d.int("add_time", 0), d.int("up_time", 0)}
	vals = append(vals, d.extra())
	ok, err := m.insert(ctx, d, "projects", []string{"id", "uid", "group_id", "name", "basepath", "description",
		"project_type", "switch_notice", "icon", "color", "env", "tag", "pre_script", "after_script",
		"project_mock_script", "is_mock_open", "strice", "is_json5", "add_time", "up_time", "extra"}, vals)
	if !ok {
		return err
	}
	if err := m.insertMembers(ctx, d, "project_members", "project_id", id, members); err != nil {
		return err
	}
	m.done(d.coll, id)
	m.checks = append(m.checks, func(ctx context.Context, st *store.Store) (string, error) {
		p, err := st.ProjectByID(ctx, id)
		if err != nil {
			return "", err
		}
		list, err := st.ProjectMembers(ctx, id)
		if err != nil {
			return "", err
		}
		got := make([]store.Member, len(list))
		for i, pm := range list {
			got[i] = pm.Member
		}
		return join(diff("project", id, "name", p.Name, name, "group_id", p.GroupID, groupID, "project_type", p.ProjectType, typ),
			memberCheck("project", id, members, got)), nil
	})
	return nil
}

func (m *migrator) cat(ctx context.Context, d *doc) error {
	id, ok := m.mainID(d)
	if !ok {
		return nil
	}
	projectID, ok := m.need(d, "project_id", "project", "所属项目")
	if !ok {
		return nil
	}
	name := d.strOr("name", "")
	vals := []any{id, d.int("uid", 0), projectID, name, d.str("desc"), d.int("index", 0), d.int("add_time", 0), d.int("up_time", 0)}
	vals = append(vals, d.extra())
	ok, err := m.insert(ctx, d, "interface_cats",
		[]string{"id", "uid", "project_id", "name", "description", "position", "add_time", "up_time", "extra"}, vals)
	if !ok {
		return err
	}
	m.done(d.coll, id)
	m.checks = append(m.checks, func(ctx context.Context, st *store.Store) (string, error) {
		c, err := st.CatByID(ctx, id)
		if err != nil {
			return "", err
		}
		return diff("interface_cat", id, "name", c.Name, name, "project_id", c.ProjectID, projectID), nil
	})
	return nil
}

var (
	reqBodyTypes = []string{"form", "json", "text", "file", "raw"}
	resBodyTypes = []string{"json", "text", "xml", "raw", "json-schema"}
)

func (m *migrator) iface(ctx context.Context, d *doc) error {
	id, ok := m.mainID(d)
	if !ok {
		return nil
	}
	projectID, ok := m.need(d, "project_id", "project", "所属项目")
	if !ok {
		return nil
	}
	catID, ok := m.need(d, "catid", "interface_cat", "所属分类")
	if !ok {
		return nil
	}
	d.get("edit_uid") // 编辑锁不迁移：ApiYard 启动时会清空
	title, path, method := d.strOr("title", ""), d.strOr("path", ""), d.strOr("method", "")
	reqQuery, resBody := d.array("req_query", "required", "1"), d.str("res_body")
	vals := []any{id, d.int("uid", 0), 0, projectID, catID, title, path, method,
		*d.enum("status", []string{"undone", "done"}, ptr("undone")), *d.enum("type", []string{"static", "var"}, ptr("static")),
		boolInt(d.bool("api_opened", false)), d.int("index", 0), d.str("desc"), d.str("markdown"),
		d.object("query_path", `{"path":"","params":[]}`), reqQuery, d.array("req_headers", "required", "1"),
		d.array("req_params"), d.array("req_body_form", "required", "1"), d.enum("req_body_type", reqBodyTypes, nil),
		boolInt(d.bool("req_body_is_json_schema", false)), d.str("req_body_other"), d.enum("res_body_type", resBodyTypes, nil),
		boolInt(d.bool("res_body_is_json_schema", false)), resBody, d.str("custom_field_value"), d.array("tag"),
		d.int("add_time", 0), d.int("up_time", 0)}
	vals = append(vals, d.extra())
	ok, err := m.insert(ctx, d, "interfaces", []string{"id", "uid", "edit_uid", "project_id", "catid", "title", "path",
		"method", "status", "type", "api_opened", "position", "description", "markdown", "query_path", "req_query",
		"req_headers", "req_params", "req_body_form", "req_body_type", "req_body_is_json_schema", "req_body_other",
		"res_body_type", "res_body_is_json_schema", "res_body", "custom_field_value", "tag", "add_time", "up_time",
		"extra"}, vals)
	if !ok {
		return err
	}
	m.done(d.coll, id)
	m.checks = append(m.checks, func(ctx context.Context, st *store.Store) (string, error) {
		it, err := st.InterfaceByID(ctx, id)
		if err != nil {
			return "", err
		}
		return diff("interface", id, "title", it.Title, title, "path", it.Path, path, "method", it.Method, method,
			"project_id", it.ProjectID, projectID, "catid", it.CatID, catID, "req_query", it.ReqQuery, reqQuery,
			"res_body", deref(it.ResBody), deref(resBody)), nil
	})
	return nil
}

func (m *migrator) token(ctx context.Context, d *doc) error {
	projectID, ok := m.need(d, "project_id", "project", "所属项目")
	if !ok {
		return nil
	}
	if m.ids[d.coll][projectID] {
		m.rep.skipped(d.coll, "项目已有 token，只保留第一条（与 YApi 的查询结果相同）", d.id)
		return nil
	}
	token := d.strOr("token", "")
	if token == "" {
		m.rep.skipped(d.coll, "token 为空", d.id)
		return nil
	}
	ok, err := m.insert(ctx, d, "tokens", []string{"project_id", "token"}, []any{projectID, token})
	if !ok {
		return err
	}
	m.done(d.coll, projectID) // token 按项目 ID 记录，用于判断重复
	m.checks = append(m.checks, func(ctx context.Context, st *store.Store) (string, error) {
		got, err := st.ProjectToken(ctx, projectID)
		if err != nil {
			return "", err
		}
		return diff("token", projectID, "token", got, token), nil
	})
	return nil
}

func (m *migrator) follow(ctx context.Context, d *doc) error {
	id, ok := m.mainID(d)
	if !ok {
		return nil
	}
	projectID, ok := m.need(d, "projectid", "project", "关注的项目")
	if !ok {
		return nil
	}
	ok, err := m.insert(ctx, d, "follows", []string{"id", "uid", "project_id", "projectname", "icon", "color"},
		[]any{id, d.int("uid", 0), projectID, d.strOr("projectname", ""), d.str("icon"), d.str("color")})
	if ok {
		m.done(d.coll, id)
	}
	return err
}

var logTypes = []string{"user", "group", "interface", "project", "other", "interface_col"}

func (m *migrator) log(ctx context.Context, d *doc) error {
	id, ok := m.mainID(d)
	if !ok {
		return nil
	}
	typ := d.str("type")
	if typ == nil || !slices.Contains(logTypes, *typ) {
		m.rep.skipped(d.coll, "type 无效", d.id)
		return nil
	}
	ok, err := m.insert(ctx, d, "logs", []string{"id", "uid", "username", "type", "typeid", "content", "data", "add_time"},
		[]any{id, d.int("uid", 0), d.strOr("username", ""), *typ, d.int("typeid", 0), d.strOr("content", ""),
			d.mixed("data"), d.int("add_time", 0)})
	if ok {
		m.done(d.coll, id)
	}
	return err
}

func (m *migrator) avatar(ctx context.Context, d *doc) error {
	uid := d.int("uid", 0)
	if uid <= 0 {
		m.rep.skipped(d.coll, "缺少 uid", d.id)
		return nil
	}
	if m.ids[d.coll][uid] {
		m.rep.skipped(d.coll, "用户已有头像，只保留第一条（与 YApi 的查询结果相同）", d.id)
		return nil
	}
	code := d.strOr("basecode", "")
	if code == "" {
		m.rep.skipped(d.coll, "没有图片数据（YApi 显示默认头像，ApiYard 也一样）", d.id)
		return nil
	}
	image, err := decodeBase64(code)
	if err != nil {
		m.rep.skipped(d.coll, "图片数据不是有效的 base64", d.id)
		return nil
	}
	ok, err := m.insert(ctx, d, "avatars", []string{"uid", "image", "type"}, []any{uid, image, d.strOr("type", "")})
	if !ok {
		return err
	}
	m.done(d.coll, uid)
	m.checks = append(m.checks, func(ctx context.Context, st *store.Store) (string, error) {
		a, err := st.AvatarOf(ctx, uid)
		if err != nil {
			return "", err
		}
		return diff("avatar", uid, "image", string(a.Image), string(image)), nil
	})
	return nil
}

// sequences 按 YApi 的计数器抬高各表的自增序列，YApi 里已删除数据的 ID 不会被新数据复用（旧链接不会指向别的接口）。
func (m *migrator) sequences(ctx context.Context) error {
	tables := map[string]string{}
	for _, c := range collections {
		tables[c.name] = c.table
	}
	return m.src.Each(ctx, countersCollection, func(raw bson.D) error {
		d := newDoc(countersCollection, raw, m.rep)
		table, field := tables[d.strOr("model", "")], d.strOr("field", "")
		count, ok := toInt(d.vals["count"])
		if table == "" || field != "_id" || !ok || table == "tokens" || table == "avatars" {
			return nil // tokens、avatars 在 ApiYard 里没有自增 ID
		}
		return m.im.RaiseSequence(ctx, table, count)
	})
}

// unsupported 列出有数据、但 ApiYard 暂不支持的集合（自动化测试、插件数据等）。
func (m *migrator) unsupported(ctx context.Context) error {
	known := map[string]bool{countersCollection: true}
	for _, c := range collections {
		known[c.name] = true
	}
	names, err := m.src.Collections(ctx)
	if err != nil {
		return err
	}
	for _, name := range names {
		if known[name] || strings.HasPrefix(name, "system.") {
			continue
		}
		n, err := m.src.Count(ctx, name)
		if err != nil {
			return err
		}
		if n > 0 {
			m.rep.Unsupported = append(m.rep.Unsupported, CollectionCount{Name: name, Count: n})
		}
	}
	sortCounts(m.rep.Unsupported)
	return nil
}

// ---- 核对 ----

// verify 核对迁移结果：各表的行数与迁移的条数一致，每条记录用 ApiYard 平时的读取方法读回后与源数据一致。
func (m *migrator) verify(ctx context.Context, st *store.Store) error {
	var problems []string
	for _, c := range collections {
		n, err := st.CountRows(ctx, c.table)
		if err != nil {
			return err
		}
		if want := int64(m.rep.stat(c.name).Migrated); n != want {
			problems = append(problems, fmt.Sprintf("%s 表有 %d 行，应为 %d 行", c.table, n, want))
		}
	}
	for table, want := range m.members {
		n, err := st.CountRows(ctx, table)
		if err != nil {
			return err
		}
		if n != want {
			problems = append(problems, fmt.Sprintf("%s 表有 %d 行，应为 %d 行", table, n, want))
		}
	}
	for _, c := range m.checks {
		msg, err := c(ctx, st)
		if err != nil {
			return fmt.Errorf("核对时读取失败: %w", err)
		}
		if msg != "" {
			problems = append(problems, msg)
		}
	}
	if len(problems) == 0 {
		return nil
	}
	if len(problems) > 20 {
		problems = append(problems[:20], fmt.Sprintf("……共 %d 处", len(problems)))
	}
	return fmt.Errorf("迁移后核对不一致：\n  %s", strings.Join(problems, "\n  "))
}

// diff 对比成对给出的字段（名称、读回的值、期望的值），返回不一致的说明。
func diff(coll string, id int64, pairs ...any) string {
	var out []string
	for i := 0; i+2 < len(pairs); i += 3 {
		if got, want := pairs[i+1], pairs[i+2]; got != want {
			out = append(out, fmt.Sprintf("%s %d 的 %s 读回为 %v，应为 %v", coll, id, pairs[i], brief(got), brief(want)))
		}
	}
	return join(out...)
}

// join 用分号连接非空的说明。
func join(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "；")
}

func deref(s *string) string {
	if s == nil {
		return "<null>"
	}
	return *s
}
