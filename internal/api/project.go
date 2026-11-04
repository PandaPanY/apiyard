package api

import (
	"context"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- 与 YApi 生成原始 token 的方式一致，不用于安全校验
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/PandaPanY/apiyard/internal/store"
	"github.com/PandaPanY/apiyard/internal/yapitoken"
)

func (a *API) registerProject(mux *http.ServeMux) {
	name := field{name: "name", kind: "string", required: true, min: 1}
	str := func(n string) field { return field{name: n, kind: "string"} }
	projectID := field{name: "project_id", kind: "number", required: true}

	a.handle(mux, "POST /api/project/add", a.projectAdd, schema(
		name, str("basepath"), field{name: "group_id", kind: "number", required: true}, str("group_name"),
		str("desc"), str("color"), str("icon"), field{name: "project_type", kind: "string", enum: []string{"private", "public"}}))
	a.handle(mux, "GET /api/project/get", a.projectGet,
		schema(field{name: "id", kind: "number"}, field{name: "project_id", kind: "number"}))
	a.handle(mux, "GET /api/project/list", a.projectList, schema(field{name: "group_id", kind: "number", required: true}))
	a.handle(mux, "POST /api/project/up", a.projectUp)
	a.handle(mux, "POST /api/project/del", a.projectDel, schema(field{name: "id", kind: "number", required: true}))
	a.handle(mux, "POST /api/project/up_env", a.projectUpEnv)
	a.handle(mux, "POST /api/project/up_tag", a.projectUpTag)
	a.handle(mux, "POST /api/project/upset", a.projectUpSet)
	a.handle(mux, "GET /api/project/get_env", a.projectGetEnv)
	a.handle(mux, "GET /api/project/token", a.projectToken, schema(projectID))
	a.handle(mux, "GET /api/project/update_token", a.projectUpdateToken, schema(projectID))
	a.handle(mux, "GET /api/project/check_project_name", a.projectCheckName)
	a.handle(mux, "GET /api/project/search", a.projectSearch)
	a.handle(mux, "POST /api/project/copy", a.projectCopy, schema(
		name, field{name: "preName", kind: "string", min: 1}, str("basepath"),
		field{name: "group_id", kind: "number", required: true}, field{name: "_id", kind: "number"},
		field{name: "cat", kind: "array"}, str("pre_script"), str("after_script"), field{name: "env", kind: "array"},
		str("group_name"), str("desc"), str("color"), str("icon"),
		field{name: "project_type", kind: "string", enum: []string{"private", "public"}}))
}

// ---- 文档格式 ----

// projectBaseDoc 对应 YApi getBaseInfo 的 select 字段；可选字段为 nil 时不输出。
func projectBaseDoc(p *store.Project) map[string]any {
	m := map[string]any{
		"_id": p.ID, "uid": p.UID, "name": p.Name, "group_id": p.GroupID, "project_type": p.ProjectType,
		"switch_notice": p.SwitchNotice, "env": json.RawMessage(p.Env), "tag": json.RawMessage(p.Tag),
		"is_mock_open": p.IsMockOpen, "strice": p.Strice, "is_json5": p.IsJSON5,
		"add_time": p.AddTime, "up_time": p.UpTime,
	}
	setOptional(m, "basepath", p.Basepath)
	setOptional(m, "desc", p.Desc)
	setOptional(m, "icon", p.Icon)
	setOptional(m, "color", p.Color)
	setOptional(m, "pre_script", p.PreScript)
	setOptional(m, "after_script", p.AfterScript)
	setOptional(m, "project_mock_script", p.ProjectMockScript)
	return m
}

// projectListDoc 对应 YApi 项目列表的 select 字段。
func projectListDoc(p *store.Project) map[string]any {
	m := map[string]any{
		"_id": p.ID, "uid": p.UID, "name": p.Name, "group_id": p.GroupID, "project_type": p.ProjectType,
		"switch_notice": p.SwitchNotice, "env": json.RawMessage(p.Env), "add_time": p.AddTime, "up_time": p.UpTime,
	}
	setOptional(m, "basepath", p.Basepath)
	setOptional(m, "desc", p.Desc)
	setOptional(m, "icon", p.Icon)
	setOptional(m, "color", p.Color)
	return m
}

// projectFullDoc 是完整的项目文档（含成员和 __v）。
func (a *API) projectFullDoc(ctx context.Context, p *store.Project) (map[string]any, error) {
	members, err := a.store.ProjectMembers(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	m := projectBaseDoc(p)
	m["members"] = projectMemberDocs(members)
	m["__v"] = 0
	return m, nil
}

func projectMemberDocs(members []store.ProjectMember) []map[string]any {
	out := make([]map[string]any, 0, len(members))
	for _, m := range members {
		doc := memberDocs([]store.Member{m.Member})[0]
		doc["email_notice"] = m.EmailNotice
		out = append(out, doc)
	}
	return out
}

func catDoc(c *store.Cat) map[string]any {
	m := map[string]any{
		"_id": c.ID, "uid": c.UID, "project_id": c.ProjectID, "name": c.Name, "index": c.Position,
		"add_time": c.AddTime, "up_time": c.UpTime, "__v": 0,
	}
	setOptional(m, "desc", c.Desc)
	return m
}

func setOptional(m map[string]any, key string, v *string) {
	if v != nil {
		m[key] = *v
	}
}

// ---- 参数处理 ----

var basepathPattern = regexp.MustCompile(`^/[a-zA-Z0-9\-/._]+$`)

// handleBasepath 与 YApi 相同：补全开头的斜杠、去掉结尾的斜杠，"/" 视为空；格式不对时返回 false。
func handleBasepath(s string) (string, bool) {
	if s == "" || s == "/" {
		return "", true
	}
	if s[0] != '/' {
		s = "/" + s
	}
	s = strings.TrimSuffix(s, "/")
	return s, basepathPattern.MatchString(s)
}

// normalizeEnv 按 YApi 的 env 子文档结构整理：只保留 name、domain、header、global，
// 缺少 _id 的环境和全局变量补上 ObjectId（Mongoose 新建子文档时会这样做），header 是普通数组，不加 _id。
func normalizeEnv(items []any) (string, error) {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		src, ok := item.(map[string]any)
		if !ok {
			continue
		}
		env := map[string]any{"_id": objectIDOf(src), "header": arrayOr(src["header"]), "global": []map[string]any{}}
		copyKeys(env, src, "name", "domain")
		for _, g := range arrayOr(src["global"]) {
			if gm, ok := g.(map[string]any); ok {
				global := map[string]any{"_id": objectIDOf(gm)}
				copyKeys(global, gm, "name", "value")
				env["global"] = append(env["global"].([]map[string]any), global)
			}
		}
		out = append(out, env)
	}
	return marshalCompact(out)
}

// normalizeTag 整理项目 tag：只保留 name、desc，补上 _id。
func normalizeTag(items []any) (string, error) {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if src, ok := item.(map[string]any); ok {
			tag := map[string]any{"_id": objectIDOf(src)}
			copyKeys(tag, src, "name", "desc")
			out = append(out, tag)
		}
	}
	return marshalCompact(out)
}

func objectIDOf(m map[string]any) string {
	if id, ok := m["_id"].(string); ok && id != "" {
		return id
	}
	return store.NewObjectID()
}

func arrayOr(v any) []any {
	if arr, ok := v.([]any); ok {
		return arr
	}
	return []any{}
}

func copyKeys(dst, src map[string]any, keys ...string) {
	for _, k := range keys {
		if v, ok := src[k]; ok {
			dst[k] = v
		}
	}
}

// optionalStr 读取可选的字符串参数，没有时返回 nil。
func (c *call) optionalStr(key string) *string {
	if s, ok := c.str(key); ok {
		return &s
	}
	return nil
}

// optionalBool 读取可选的布尔参数（接受 true/false 和 "true"/"false"）。
func (c *call) optionalBool(key string) *bool {
	switch v := c.params[key].(type) {
	case bool:
		return &v
	case string:
		if b, err := strconv.ParseBool(v); err == nil {
			return &b
		}
	}
	return nil
}

// projectLog 写入一条项目动态。
func (a *API) projectLog(ctx context.Context, u *store.User, projectID int64, action string) {
	content := fmt.Sprintf(`<a href="/user/profile/%d">%s</a> %s`, u.ID, html.EscapeString(u.Username), action)
	if err := a.store.CreateLog(ctx, store.Log{UID: u.ID, Username: u.Username, Type: "project", TypeID: projectID, Content: content}); err != nil {
		a.logger.Error("写入动态失败", "err", err)
	}
}

// ---- 接口 ----

func (a *API) projectAdd(c *call) error {
	ctx := c.r.Context()
	groupID, _ := c.int("group_id")
	if ok, err := a.canGroup(ctx, c.user, groupID, actionEdit); err != nil {
		return err
	} else if !ok {
		return c.fail(405, "没有权限")
	}
	name, _ := c.str("name")
	if exists, err := a.store.ProjectNameExists(ctx, name, groupID); err != nil {
		return err
	} else if exists {
		return c.fail(401, "已存在的项目名")
	}
	raw, _ := c.str("basepath")
	basepath, ok := handleBasepath(raw)
	if !ok {
		return c.fail(401, "basepath格式有误")
	}
	projectType, _ := c.str("project_type")
	if projectType == "" {
		projectType = "private"
	}
	env, err := normalizeEnv([]any{map[string]any{"name": "local", "domain": "http://127.0.0.1"}})
	if err != nil {
		return err
	}

	now := store.Now()
	p := &store.Project{
		UID: c.user.ID, GroupID: groupID, Name: name, Basepath: &basepath, Desc: c.optionalStr("desc"),
		ProjectType: projectType, SwitchNotice: true, Icon: c.optionalStr("icon"), Color: c.optionalStr("color"),
		Env: env, Tag: "[]", IsJSON5: false, AddTime: now, UpTime: now,
	}
	defaultCat := &store.Cat{UID: c.user.ID, Name: "公共分类", Desc: strPtr("公共分类"), AddTime: now, UpTime: now}
	// 非管理员创建项目时自动成为项目组长
	var owner *store.Member
	if c.user.Role != "admin" {
		owner = &store.Member{UID: c.user.ID, Role: "owner", Username: &c.user.Username, Email: &c.user.Email}
	}
	if err := a.store.CreateProject(ctx, p, defaultCat, owner); err != nil {
		return err
	}
	a.projectLog(ctx, c.user, p.ID, fmt.Sprintf(`添加了项目 <a href="/project/%d">%s</a>`, p.ID, html.EscapeString(name)))

	// YApi 在加入组长之前就返回了保存结果，所以 members 总是空数组
	doc := projectBaseDoc(p)
	doc["members"] = []any{}
	doc["__v"] = 0
	return c.ok(doc)
}

func (a *API) projectGet(c *call) error {
	ctx := c.r.Context()
	id, ok := c.int("id")
	if !ok {
		id, _ = c.int("project_id") // 通过开放 API token 访问时用 project_id
	}
	p, err := a.store.ProjectByID(ctx, id)
	if err != nil {
		return err // YApi 在项目不存在时同样会抛异常
	}
	if p.ProjectType == "private" {
		if ok, err := a.canProject(ctx, c.user, id, actionView); err != nil {
			return err
		} else if !ok {
			return c.fail(406, "没有权限")
		}
	}
	doc := projectBaseDoc(p)
	if p.Env == "[]" {
		doc["env"] = []map[string]any{{"name": "local", "domain": "http://127.0.0.1"}}
	}
	// YApi 查分类和角色时用的是 params.id；只给 project_id（开放 API 的用法）时 id 为空，
	// 结果分类总是空数组、角色是 false。周边工具按这个结果编写，这里保持一致。
	if !ok {
		doc["cat"], doc["role"] = []any{}, false
		return c.ok(doc)
	}
	cats, err := a.store.CatsByProject(ctx, id)
	if err != nil {
		return err
	}
	role, err := a.projectRole(ctx, c.user, id)
	if err != nil {
		return err
	}
	catDocs := make([]map[string]any, 0, len(cats))
	for _, cat := range cats {
		catDocs = append(catDocs, catDoc(cat))
	}
	doc["cat"] = catDocs
	doc["role"] = role
	return c.ok(doc)
}

func (a *API) projectList(c *call) error {
	ctx := c.r.Context()
	groupID, _ := c.int("group_id")
	g, err := a.store.GroupByID(ctx, groupID)
	if err != nil {
		return err // YApi 在分组不存在时会抛异常
	}
	canView, err := a.canGroup(ctx, c.user, groupID, actionView)
	if err != nil {
		return err
	}
	projects, err := a.store.ProjectsByGroup(ctx, groupID)
	if err != nil {
		return err
	}
	follows, err := a.store.FollowsOf(ctx, c.user.ID)
	if err != nil {
		return err
	}
	followed := map[int64]bool{}
	for _, f := range follows {
		followed[f.ProjectID] = true
	}

	// 个人空间：先列出关注的项目（来自所有分组），再列出个人空间里的项目，按 _id 去重
	if g.Type == "private" && g.UID == c.user.ID {
		list := make([]map[string]any, 0, len(follows)+len(projects))
		seen := map[int64]bool{}
		for _, f := range follows {
			doc := followDoc(f)
			doc["_id"], doc["follow"] = f.ProjectID, true
			list = append(list, doc)
			seen[f.ProjectID] = true
		}
		for _, p := range projects {
			if !seen[p.ID] {
				list = append(list, projectListDoc(p))
			}
		}
		return c.ok(map[string]any{"list": list})
	}

	// 普通分组：没有分组查看权限的用户只能看到公开项目和自己参与的私有项目；关注的项目排在前面
	var front, back []map[string]any
	for _, p := range projects {
		if p.ProjectType == "private" && !canView {
			if _, err := a.store.ProjectMemberOf(ctx, p.ID, c.user.ID); errors.Is(err, store.ErrNotFound) {
				continue
			} else if err != nil {
				return err
			}
		}
		doc := projectListDoc(p)
		doc["follow"] = followed[p.ID]
		if followed[p.ID] {
			front = append([]map[string]any{doc}, front...)
		} else {
			back = append(back, doc)
		}
	}
	return c.ok(map[string]any{"list": append(append([]map[string]any{}, front...), back...)})
}

func (a *API) projectUp(c *call) error {
	ctx := c.r.Context()
	id, ok := c.int("id")
	if !ok || id == 0 {
		return c.fail(405, "项目id不能为空")
	}
	if ok, err := a.canProject(ctx, c.user, id, actionDanger); err != nil {
		return err
	} else if !ok {
		return c.fail(405, "没有权限")
	}
	p, err := a.store.ProjectByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return c.fail(402, "Cannot read property 'name' of null")
	}
	if err != nil {
		return err
	}

	var patch store.ProjectPatch
	if s := c.trimmed("basepath"); s != "" {
		bp, ok := handleBasepath(s)
		if !ok {
			return c.fail(401, "basepath格式有误")
		}
		patch.Basepath = &bp
	} else if _, present := c.str("basepath"); present {
		patch.Basepath = strPtr("")
	}
	groupID, hasGroup := c.int("group_id")
	if hasGroup {
		patch.GroupID = &groupID
	}
	if name := c.trimmed("name"); name != "" && name != p.Name {
		if exists, err := a.store.ProjectNameExists(ctx, name, groupID); err != nil {
			return err
		} else if exists {
			return c.fail(401, "已存在的项目名")
		}
		patch.Name = &name
	}
	if t, ok := c.str("project_type"); ok {
		if t != "public" && t != "private" {
			return c.fail(402, "Validation failed")
		}
		patch.ProjectType = &t
	}
	for key, dst := range map[string]**string{
		"desc": &patch.Desc, "icon": &patch.Icon, "color": &patch.Color, "pre_script": &patch.PreScript,
		"after_script": &patch.AfterScript, "project_mock_script": &patch.ProjectMockScript,
	} {
		*dst = c.optionalStr(key)
	}
	patch.SwitchNotice, patch.IsMockOpen = c.optionalBool("switch_notice"), c.optionalBool("is_mock_open")
	patch.Strice, patch.JSON5 = c.optionalBool("strice"), c.optionalBool("is_json5")
	if err := c.jsonArrayPatch("env", normalizeEnv, &patch.Env); err != nil {
		return err
	}
	if err := c.jsonArrayPatch("tag", normalizeTag, &patch.Tag); err != nil {
		return err
	}

	n, err := a.store.UpdateProject(ctx, id, patch)
	if err != nil {
		return err
	}
	a.projectLog(ctx, c.user, id, fmt.Sprintf(`更新了项目 <a href="/project/%d/interface/api">%s</a>`, id, html.EscapeString(p.Name)))
	return c.ok(updateResult(n, n))
}

// jsonArrayPatch 把数组参数整理后写入 dst；参数不存在时不修改。
func (c *call) jsonArrayPatch(key string, normalize func([]any) (string, error), dst **string) error {
	items, ok := c.params[key].([]any)
	if !ok {
		return nil
	}
	s, err := normalize(items)
	if err != nil {
		return err
	}
	*dst = &s
	return nil
}

func (a *API) projectDel(c *call) error {
	ctx := c.r.Context()
	id, _ := c.int("id")
	if ok, err := a.canProject(ctx, c.user, id, actionDanger); err != nil {
		return err
	} else if !ok {
		return c.fail(405, "没有权限")
	}
	n, err := a.store.DeleteProject(ctx, id)
	if err != nil {
		return err
	}
	return c.ok(removeResult(n))
}

func (a *API) projectUpEnv(c *call) error {
	return a.projectUpArray(c, "env", "env参数格式有误", "的环境", func(items []any) (string, error) {
		names := map[any]bool{}
		for _, item := range items {
			m, _ := item.(map[string]any)
			names[m["name"]] = true
		}
		if len(names) != len(items) {
			return "", errDuplicateEnv
		}
		return normalizeEnv(items)
	})
}

var errDuplicateEnv = errors.New("环境变量名重复")

func (a *API) projectUpTag(c *call) error {
	return a.projectUpArray(c, "tag", "tag参数格式有误", "的tag", normalizeTag)
}

// projectUpArray 是修改环境和 tag 的公共流程：校验 id 和权限（edit），整理数组后保存并记录动态。
func (a *API) projectUpArray(c *call, key, badFormat, logSuffix string, normalize func([]any) (string, error)) error {
	ctx := c.r.Context()
	id, ok := c.int("id")
	if !ok || id == 0 {
		return c.fail(405, "项目id不能为空")
	}
	if ok, err := a.canProject(ctx, c.user, id, actionEdit); err != nil {
		return err
	} else if !ok {
		return c.fail(405, "没有权限")
	}
	items, ok := c.params[key].([]any)
	if !ok {
		return c.fail(405, badFormat)
	}
	p, err := a.store.ProjectByID(ctx, id)
	if err != nil {
		return err
	}
	value, err := normalize(items)
	if errors.Is(err, errDuplicateEnv) {
		return c.fail(405, err.Error())
	}
	if err != nil {
		return err
	}
	patch := store.ProjectPatch{}
	if key == "env" {
		patch.Env = &value
	} else {
		patch.Tag = &value
	}
	n, err := a.store.UpdateProject(ctx, id, patch)
	if err != nil {
		return err
	}
	a.projectLog(ctx, c.user, id, fmt.Sprintf(`更新了项目 <a href="/project/%d/interface/api">%s</a> %s`,
		id, html.EscapeString(p.Name), logSuffix))
	return c.ok(updateResult(n, n))
}

func (a *API) projectUpSet(c *call) error {
	ctx := c.r.Context()
	id, _ := c.int("id")
	// 与 YApi 相同：先查权限，再查 id
	if ok, err := a.canProject(ctx, c.user, id, actionDanger); err != nil {
		return err
	} else if !ok {
		return c.fail(405, "没有权限")
	}
	if id == 0 {
		return c.fail(405, "项目id不能为空")
	}
	icon, color := c.optionalStr("icon"), c.optionalStr("color")
	n, err := a.store.UpdateProject(ctx, id, store.ProjectPatch{Icon: icon, Color: color})
	if err != nil {
		return err
	}
	if err := a.store.UpdateFollowStyle(ctx, id, icon, color); err != nil {
		return err
	}
	a.projectLog(ctx, c.user, id, "修改了项目图标、颜色")
	return c.ok(updateResult(n, n))
}

func (a *API) projectGetEnv(c *call) error {
	id, ok := c.int("project_id")
	if !ok || id == 0 {
		return c.fail(405, "项目id不能为空")
	}
	p, err := a.store.ProjectByID(c.r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return c.fail(402, "Cannot read property 'toObject' of null")
	}
	if err != nil {
		return err
	}
	return c.ok(map[string]any{"_id": p.ID, "env": json.RawMessage(p.Env)})
}

// projectToken 返回开放 API 使用的项目 token，首次调用时生成原始 token。
// 与 YApi 不同：YApi 不检查权限，任何登录用户都能拿到任意项目的 token；这里要求开发者及以上。
func (a *API) projectToken(c *call) error {
	ctx := c.r.Context()
	id, _ := c.int("project_id")
	if ok, err := a.canProject(ctx, c.user, id, actionEdit); err != nil {
		return err
	} else if !ok {
		return c.fail(405, "没有权限")
	}
	raw, err := a.store.ProjectToken(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		if raw, err = newRawToken(); err != nil {
			return err
		}
		err = a.store.SetProjectToken(ctx, id, raw)
	}
	if err != nil {
		return err
	}
	token, err := yapitoken.Encode(strconv.FormatInt(c.user.ID, 10), raw, a.tokenSalt())
	if err != nil {
		return err
	}
	return c.ok(token)
}

// projectUpdateToken 重新生成项目 token，旧 token 立即失效。
// 与 YApi 不同：要求组长或管理员；返回的 token 带上当前用户的 uid（YApi 漏传 uid，生成的 token 无法对应到用户）。
func (a *API) projectUpdateToken(c *call) error {
	ctx := c.r.Context()
	id, _ := c.int("project_id")
	if ok, err := a.canProject(ctx, c.user, id, actionDanger); err != nil {
		return err
	} else if !ok {
		return c.fail(405, "没有权限")
	}
	if _, err := a.store.ProjectToken(ctx, id); errors.Is(err, store.ErrNotFound) {
		return c.fail(402, "没有查到token信息")
	} else if err != nil {
		return err
	}
	raw, err := newRawToken()
	if err != nil {
		return err
	}
	if err := a.store.SetProjectToken(ctx, id, raw); err != nil {
		return err
	}
	token, err := yapitoken.Encode(strconv.FormatInt(c.user.ID, 10), raw, a.tokenSalt())
	if err != nil {
		return err
	}
	result := updateResult(1, 1)
	result["token"] = token
	return c.ok(result)
}

func (a *API) tokenSalt() string {
	if a.opts.TokenSalt != "" {
		return a.opts.TokenSalt
	}
	return yapitoken.DefaultSalt
}

// newRawToken 生成 20 位十六进制的原始项目 token，形式与 YApi 相同（sha1 摘要的前 20 位），但改用安全随机数。
func newRawToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	sum := sha1.Sum(b) // #nosec G401 -- 只用于生成与 YApi 形式相同的随机串
	return hex.EncodeToString(sum[:])[:20], nil
}

func (a *API) projectCheckName(c *call) error {
	name, _ := c.str("name")
	if name == "" {
		return c.fail(401, "项目名不能为空")
	}
	groupID, _ := c.int("group_id")
	if exists, err := a.store.ProjectNameExists(c.r.Context(), name, groupID); err != nil {
		return err
	} else if exists {
		return c.fail(401, "已存在的项目名")
	}
	return c.ok(map[string]any{})
}

// projectSearch 同时搜索项目、分组和接口，供页面顶部的搜索框使用。
func (a *API) projectSearch(c *call) error {
	q, _ := c.str("q")
	if q == "" {
		return c.reply(map[string]any{"errcode": 400, "errmsg": "No keyword."})
	}
	if searchRejected.MatchString(q) {
		return c.reply(map[string]any{"errcode": 400, "errmsg": "Bad query."})
	}
	ctx := c.r.Context()
	projects, err := a.store.SearchProjects(ctx, q)
	if err != nil {
		return err
	}
	groups, err := a.store.SearchGroups(ctx, q)
	if err != nil {
		return err
	}
	interfaces, err := a.store.SearchInterfaces(ctx, q)
	if err != nil {
		return err
	}

	projectList := make([]map[string]any, 0, len(projects))
	for _, p := range projects {
		members, err := a.store.ProjectMembers(ctx, p.ID)
		if err != nil {
			return err
		}
		doc := map[string]any{
			"_id": p.ID, "name": p.Name, "uid": p.UID, "env": json.RawMessage(p.Env), "members": projectMemberDocs(members),
			"groupId": p.GroupID, "upTime": p.UpTime, "addTime": p.AddTime,
		}
		setOptional(doc, "basepath", p.Basepath)
		projectList = append(projectList, doc)
	}
	groupList := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		doc := map[string]any{"_id": g.ID, "uid": g.UID, "groupName": g.Name, "addTime": g.AddTime, "upTime": g.UpTime}
		setOptional(doc, "groupDesc", g.Desc)
		groupList = append(groupList, doc)
	}
	interfaceList := make([]map[string]any, 0, len(interfaces))
	for _, it := range interfaces {
		interfaceList = append(interfaceList, map[string]any{
			"_id": it.ID, "uid": it.UID, "title": it.Title, "projectId": it.ProjectID, "addTime": it.AddTime, "upTime": it.UpTime,
		})
	}
	return c.reply(map[string]any{"errcode": 0, "errmsg": "ok", "data": map[string]any{
		"project": projectList, "group": groupList, "interface": interfaceList,
	}})
}

func strPtr(s string) *string { return &s }

// projectCopy 复制项目：新建项目，按参数 cat 的顺序新建分类并复制其中的接口，再复制源项目的成员。
// 与 YApi 不同：要求对源项目有查看权限，且只复制属于源项目的分类下的接口；全部操作在一个事务中完成。
func (a *API) projectCopy(c *call) error {
	ctx := c.r.Context()
	groupID, _ := c.int("group_id")
	if ok, err := a.canGroup(ctx, c.user, groupID, actionEdit); err != nil {
		return err
	} else if !ok {
		return c.fail(405, "没有权限")
	}
	sourceID, _ := c.int("_id")
	source, err := a.store.ProjectByID(ctx, sourceID)
	if errors.Is(err, store.ErrNotFound) {
		return c.fail(402, "Cannot read property 'members' of null")
	}
	if err != nil {
		return err
	}
	if ok, err := a.canProject(ctx, c.user, sourceID, actionView); err != nil {
		return err
	} else if !ok {
		return c.fail(405, "没有权限")
	}
	catParams, ok := c.params["cat"].([]any)
	if !ok {
		return c.fail(402, "Cannot read property 'length' of undefined")
	}
	cats := make([]store.CopyCat, 0, len(catParams))
	for _, item := range catParams {
		m, _ := item.(map[string]any)
		name, _, _ := castString(m["name"])
		if name == "" {
			return c.fail(402, "interface_cat validation failed: name: Path `name` is required.")
		}
		sourceCat, _ := toInt(m["_id"])
		cat := store.CopyCat{SourceID: sourceCat, Name: name}
		if desc, present, _ := castString(m["desc"]); present {
			cat.Desc = &desc
		}
		cats = append(cats, cat)
	}

	envItems, ok := c.params["env"].([]any)
	if !ok {
		envItems = []any{map[string]any{"name": "local", "domain": "http://127.0.0.1"}}
	}
	env, err := normalizeEnv(envItems)
	if err != nil {
		return err
	}
	projectType, _ := c.str("project_type")
	if projectType == "" {
		projectType = "private"
	}
	basepath, _ := c.str("basepath") // YApi 复制时不整理 basepath
	name, _ := c.str("name")
	now := store.Now()
	p := &store.Project{
		UID: c.user.ID, GroupID: groupID, Name: name, Basepath: &basepath, Desc: c.optionalStr("desc"),
		ProjectType: projectType, SwitchNotice: true, Icon: c.optionalStr("icon"), Color: c.optionalStr("color"),
		Env: env, Tag: "[]", PreScript: c.optionalStr("pre_script"), AfterScript: c.optionalStr("after_script"),
		IsJSON5: true, AddTime: now, UpTime: now,
	}

	members, err := a.store.ProjectMembers(ctx, sourceID)
	if err != nil {
		return err
	}
	// 非管理员复制时，如果不是源项目成员，自动成为新项目的组长
	if c.user.Role != "admin" && !slices.ContainsFunc(members, func(m store.ProjectMember) bool { return m.UID == c.user.ID }) {
		members = append(members, store.ProjectMember{EmailNotice: true, Member: store.Member{
			UID: c.user.ID, Role: "owner", Username: &c.user.Username, Email: &c.user.Email}})
	}
	for i := range members {
		members[i].SubID = "" // 新项目的成员子文档使用新的 _id
	}
	if err := a.store.CopyProject(ctx, p, source.ID, cats, members, c.user.ID, now); err != nil {
		return err
	}

	preName, ok := c.str("preName")
	if !ok {
		preName = "undefined"
	}
	a.projectLog(ctx, c.user, p.ID, fmt.Sprintf(`复制了项目 %s 为 <a href="/project/%d">%s</a>`,
		html.EscapeString(preName), p.ID, html.EscapeString(name)))

	// 与新建项目一样，YApi 返回的是加入成员之前的保存结果
	doc := projectBaseDoc(p)
	doc["members"] = []any{}
	doc["__v"] = 0
	return c.ok(doc)
}
