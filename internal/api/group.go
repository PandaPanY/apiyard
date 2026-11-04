package api

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"slices"
	"strings"

	"github.com/PandaPanY/apiyard/internal/store"
)

var memberRoles = []string{"owner", "dev", "guest"}

// roleNames 用于动态内容，与 YApi 相同。
var roleNames = map[string]string{"owner": "组长", "dev": "开发者", "guest": "访客"}

func (a *API) registerGroup(mux *http.ServeMux) {
	id := field{name: "id", kind: "number", required: true}
	groupName := field{name: "group_name", kind: "string", required: true, min: 1}
	groupDesc := field{name: "group_desc", kind: "string"}
	role := field{name: "role", kind: "string", enum: memberRoles}
	memberUID := field{name: "member_uid", kind: "number", required: true}

	a.handle(mux, "GET /api/group/get_mygroup", a.groupMine)
	a.handle(mux, "GET /api/group/list", a.groupList)
	a.handle(mux, "GET /api/group/get", a.groupGet, schema(id))
	a.handle(mux, "POST /api/group/add", a.groupAdd,
		schema(groupName, groupDesc, field{name: "owner_uids", kind: "array"}))
	a.handle(mux, "POST /api/group/up", a.groupUp,
		schema(id, groupName, groupDesc, field{name: "custom_field1", kind: "object"}))
	a.handle(mux, "POST /api/group/del", a.groupDel, schema(id))
	a.handle(mux, "POST /api/group/add_member", a.groupAddMember,
		schema(id, role, field{name: "member_uids", kind: "array", required: true, min: 1}))
	a.handle(mux, "GET /api/group/get_member_list", a.groupMemberList, schema(id))
	a.handle(mux, "POST /api/group/change_member_role", a.groupChangeRole, schema(memberUID, id, role))
	a.handle(mux, "POST /api/group/del_member", a.groupDelMember, schema(id, memberUID))
}

// groupFields 对应 YApi 用 select 查询分组时返回的字段。
func groupFields(g *store.Group, withUID bool) map[string]any {
	m := map[string]any{
		"_id":           g.ID,
		"group_name":    g.Name,
		"type":          g.Type,
		"custom_field1": rawJSON(g.CustomField1),
		"add_time":      g.AddTime,
		"up_time":       g.UpTime,
	}
	if withUID {
		m["uid"] = g.UID
	}
	if g.Desc != nil {
		m["group_desc"] = *g.Desc
	}
	return m
}

// groupFullDoc 是不带 select 的完整分组文档（含成员和 Mongoose 的 __v），
// YApi 在新建文档后直接返回、或用 find 查询时得到的就是这种形式。
func (a *API) groupFullDoc(ctx context.Context, g *store.Group) (map[string]any, error) {
	members, err := a.store.GroupMembers(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	m := groupFields(g, true)
	m["members"] = memberDocs(members)
	m["__v"] = 0
	return m, nil
}

func memberDocs(members []store.Member) []map[string]any {
	out := make([]map[string]any, 0, len(members))
	for _, m := range members {
		doc := map[string]any{"_id": m.SubID, "uid": m.UID, "role": m.Role}
		if m.Username != nil {
			doc["username"] = *m.Username
		}
		if m.Email != nil {
			doc["email"] = *m.Email
		}
		out = append(out, doc)
	}
	return out
}

// createPrivateGroup 为用户创建个人空间（YApi 的 handlePrivateGroup）。
func (a *API) createPrivateGroup(ctx context.Context, uid int64) (*store.Group, error) {
	now := store.Now()
	g := &store.Group{UID: uid, Name: fmt.Sprintf("User-%d", uid), Type: "private", AddTime: now, UpTime: now}
	return g, a.store.CreateGroup(ctx, g, nil)
}

// privateGroup 返回用户的个人空间，没有时当场创建；created 表示是否刚创建。
func (a *API) privateGroup(ctx context.Context, uid int64) (g *store.Group, created bool, err error) {
	g, err = a.store.PrivateGroupOf(ctx, uid)
	if errors.Is(err, store.ErrNotFound) {
		g, err = a.createPrivateGroup(ctx, uid)
		return g, true, err
	}
	return g, false, err
}

// privateGroupDoc 返回个人空间的文档：刚创建时是完整文档，已存在时是 select 后的字段（不含 uid）。
func (a *API) privateGroupDoc(ctx context.Context, uid int64) (map[string]any, error) {
	g, created, err := a.privateGroup(ctx, uid)
	if err != nil {
		return nil, err
	}
	if created {
		return a.groupFullDoc(ctx, g)
	}
	return groupFields(g, false), nil
}

func (a *API) groupMine(c *call) error {
	doc, err := a.privateGroupDoc(c.r.Context(), c.user.ID)
	if err != nil {
		return err
	}
	return c.ok(doc)
}

// groupList 返回当前用户可见的分组：个人空间排第一，其余按 YApi 的顺序排列。
func (a *API) groupList(c *call) error {
	ctx := c.r.Context()
	private, err := a.privateGroupDoc(ctx, c.user.ID)
	if err != nil {
		return err
	}
	private["group_name"] = "个人空间"
	private["role"] = "owner"

	var visible []map[string]any
	if c.user.Role == "admin" {
		visible, err = a.adminGroupList(ctx)
	} else {
		visible, err = a.memberGroupList(ctx, c.user.ID)
	}
	if err != nil {
		return err
	}
	return c.ok(append([]map[string]any{private}, visible...))
}

// adminGroupList：全部公共分组，新建的在前（YApi 逐个 unshift，结果是倒序）。
func (a *API) adminGroupList(ctx context.Context) ([]map[string]any, error) {
	groups, err := a.store.PublicGroups(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(groups))
	for _, g := range slices.Backward(groups) {
		out = append(out, groupFields(g, true))
	}
	return out, nil
}

// memberGroupList：用户创建或参与的公共分组（倒序），再追加能看到的项目所在、但还没列出的分组（正序、完整文档）。
func (a *API) memberGroupList(ctx context.Context, uid int64) ([]map[string]any, error) {
	groups, err := a.store.MemberGroups(ctx, uid)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(groups))
	listed := map[int64]bool{}
	for _, g := range slices.Backward(groups) {
		out = append(out, groupFields(g, true))
		listed[g.ID] = true
	}

	ids, err := a.store.VisibleProjectGroupIDs(ctx, uid)
	if err != nil {
		return nil, err
	}
	var extra []int64
	for _, id := range ids {
		if !listed[id] {
			listed[id] = true
			extra = append(extra, id)
		}
	}
	more, err := a.store.PublicGroupsByIDs(ctx, extra)
	if err != nil {
		return nil, err
	}
	for _, g := range more {
		doc, err := a.groupFullDoc(ctx, g)
		if err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, nil
}

func (a *API) groupGet(c *call) error {
	ctx := c.r.Context()
	id, _ := c.int("id")
	g, err := a.store.GroupByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		// YApi 找不到分组时不设置响应体，Koa 返回 404 Not Found
		http.NotFound(c.w, c.r)
		c.replied = true
		return nil
	}
	if err != nil {
		return err
	}
	role, err := a.groupRole(ctx, c.user, id)
	if err != nil {
		return err
	}
	doc := groupFields(g, true)
	doc["role"] = role
	if g.Type == "private" {
		doc["group_name"] = "个人空间"
	}
	return c.ok(doc)
}

func (a *API) groupAdd(c *call) error {
	ctx := c.r.Context()
	name, _ := c.str("group_name")
	var desc *string
	if d, ok := c.str("group_desc"); ok {
		desc = &d
	}

	// YApi 不传 owner_uids 时会抛异常（它总是被前端传成 []）；这里按空数组处理，默认由创建者担任组长
	ownerIDs := intList(c.params["owner_uids"])
	if len(ownerIDs) == 0 {
		ownerIDs = []int64{c.user.ID}
	}
	owners, err := a.membersOf(ctx, ownerIDs, "owner")
	if err != nil {
		return err
	}

	if exists, err := a.store.GroupNameExists(ctx, name); err != nil {
		return err
	} else if exists {
		return c.fail(401, "项目分组名已存在")
	}

	now := store.Now()
	g := &store.Group{UID: c.user.ID, Name: name, Desc: desc, Type: "public", AddTime: now, UpTime: now}
	if err := a.store.CreateGroup(ctx, g, owners); err != nil {
		return err
	}
	a.groupLog(ctx, c.user, g.ID, fmt.Sprintf(`新增了分组 <a href="/group/%d">%s</a>`, g.ID, html.EscapeString(name)))

	// 新建接口只返回部分字段（YApi 的 fieldSelect）
	doc := map[string]any{"_id": g.ID, "group_name": g.Name, "uid": g.UID, "type": g.Type, "members": memberDocs(owners)}
	if desc != nil {
		doc["group_desc"] = *desc
	}
	return c.ok(doc)
}

// membersOf 把 uid 转换为指定角色的成员，跳过不存在的用户。
func (a *API) membersOf(ctx context.Context, uids []int64, role string) ([]store.Member, error) {
	var out []store.Member
	for _, uid := range uids {
		u, err := a.store.UserByID(ctx, uid)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, store.Member{UID: u.ID, Role: role, Username: &u.Username, Email: &u.Email})
	}
	return out, nil
}

func (a *API) groupUp(c *call) error {
	ctx := c.r.Context()
	id, _ := c.int("id")
	if ok, err := a.canManageGroup(ctx, c.user, id); err != nil {
		return err
	} else if !ok {
		return c.fail(405, "没有权限")
	}

	name, _ := c.str("group_name")
	var desc, customField *string
	if d, ok := c.str("group_desc"); ok {
		desc = &d
	}
	if cf, ok := c.params["custom_field1"].(map[string]any); ok {
		s, err := marshalCompact(cf)
		if err != nil {
			return err
		}
		customField = &s
	}
	n, err := a.store.UpdateGroup(ctx, id, name, desc, customField)
	if err != nil {
		return err
	}
	a.groupLog(ctx, c.user, id, fmt.Sprintf(`更新了 <a href="/group/%d">%s</a> 分组`, id, html.EscapeString(name)))
	return c.ok(updateResult(n, n))
}

func (a *API) groupDel(c *call) error {
	if c.user.Role != "admin" {
		return c.fail(401, "没有权限")
	}
	id, _ := c.int("id")
	n, err := a.store.DeleteGroup(c.r.Context(), id)
	if err != nil {
		return err
	}
	return c.ok(removeResult(n))
}

func (a *API) groupAddMember(c *call) error {
	ctx := c.r.Context()
	id, _ := c.int("id")
	// 与 YApi 不同：YApi 的这个接口没有任何权限检查，任何登录用户都能往任意分组加人。这里要求组长或管理员。
	if ok, err := a.canManageGroup(ctx, c.user, id); err != nil {
		return err
	} else if !ok {
		return c.fail(405, "没有权限")
	}

	role, _ := c.str("role")
	if !slices.Contains(memberRoles, role) {
		role = "dev"
	}
	add, exist, missing := []store.Member{}, []map[string]any{}, []int64{}
	for _, uid := range intList(c.params["member_uids"]) {
		u, err := a.store.UserByID(ctx, uid)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		_, memberErr := a.store.GroupMember(ctx, id, uid)
		switch {
		case memberErr == nil:
			// 已是成员。YApi 返回的这类条目带有 _role（用户的站点角色）
			if u != nil {
				exist = append(exist, map[string]any{"_role": u.Role, "role": role, "uid": u.ID, "username": u.Username, "email": u.Email})
			}
		case u == nil:
			missing = append(missing, uid)
		default:
			add = append(add, store.Member{UID: u.ID, Role: role, Username: &u.Username, Email: &u.Email})
		}
	}

	exists, err := a.store.AddGroupMembers(ctx, id, add)
	if err != nil {
		return err
	}
	if len(add) > 0 {
		links := make([]string, len(add))
		for i, m := range add {
			links[i] = fmt.Sprintf(`<a href = "/user/profile/%d">%s</a>`, m.UID, html.EscapeString(*m.Username))
		}
		a.groupLog(ctx, c.user, id, fmt.Sprintf("新增了分组成员 %s 为 %s", strings.Join(links, "、"), roleNames[role]))
	}

	matched, modified := int64(0), int64(0)
	if exists {
		matched = 1
		if len(add) > 0 {
			modified = 1
		}
	}
	added := make([]map[string]any, len(add))
	for i, m := range add {
		added[i] = map[string]any{"role": m.Role, "uid": m.UID, "username": *m.Username, "email": *m.Email}
	}
	return c.ok(map[string]any{
		"result":        updateResult(matched, modified),
		"add_members":   added,
		"exist_members": exist,
		"no_members":    missing,
	})
}

func (a *API) groupMemberList(c *call) error {
	ctx := c.r.Context()
	id, _ := c.int("id")
	if _, err := a.store.GroupByID(ctx, id); err != nil {
		return err // YApi 在分组不存在时同样会抛异常
	}
	members, err := a.store.GroupMembers(ctx, id)
	if err != nil {
		return err
	}
	return c.ok(memberDocs(members))
}

func (a *API) groupChangeRole(c *call) error {
	ctx := c.r.Context()
	id, _ := c.int("id")
	uid, _ := c.int("member_uid")
	member, err := a.store.GroupMember(ctx, id, uid)
	if errors.Is(err, store.ErrNotFound) {
		return c.fail(400, "分组成员不存在")
	}
	if err != nil {
		return err
	}
	if ok, err := a.canManageGroup(ctx, c.user, id); err != nil {
		return err
	} else if !ok {
		return c.fail(405, "没有权限")
	}

	role, _ := c.str("role")
	if !slices.Contains(memberRoles, role) {
		role = "dev"
	}
	n, err := a.store.ChangeGroupMemberRole(ctx, id, uid, role)
	if err != nil {
		return err
	}
	modified := n
	if member.Role == role {
		modified = 0 // MongoDB 在值没有变化时 nModified 为 0
	}
	a.groupLog(ctx, c.user, id, fmt.Sprintf(`更改了分组成员 <a href="/user/profile/%d">%s</a> 的权限为 "%s"`,
		uid, html.EscapeString(a.usernameOf(ctx, uid)), roleNames[role]))
	return c.ok(updateResult(n, modified))
}

func (a *API) groupDelMember(c *call) error {
	ctx := c.r.Context()
	id, _ := c.int("id")
	uid, _ := c.int("member_uid")
	if _, err := a.store.GroupMember(ctx, id, uid); errors.Is(err, store.ErrNotFound) {
		return c.fail(400, "分组成员不存在")
	} else if err != nil {
		return err
	}
	if ok, err := a.canManageGroup(ctx, c.user, id); err != nil {
		return err
	} else if !ok {
		return c.fail(405, "没有权限")
	}

	n, err := a.store.RemoveGroupMember(ctx, id, uid)
	if err != nil {
		return err
	}
	a.groupLog(ctx, c.user, id, fmt.Sprintf(`删除了分组成员 <a href="/user/profile/%d">%s</a>`,
		uid, html.EscapeString(a.usernameOf(ctx, uid))))
	return c.ok(updateResult(1, n))
}

// usernameOf 返回用户名，用户不存在时返回空字符串（与 YApi 一致）。
func (a *API) usernameOf(ctx context.Context, uid int64) string {
	u, err := a.store.UserByID(ctx, uid)
	if err != nil {
		return ""
	}
	return u.Username
}

// groupLog 写入一条分组动态。与 YApi 一样，写入失败只记录日志，不影响接口结果。
// 与 YApi 不同：用户名、分组名等会做 HTML 转义。YApi 原样拼接，前端又按 HTML 渲染，会造成存储型 XSS。
func (a *API) groupLog(ctx context.Context, u *store.User, groupID int64, action string) {
	content := fmt.Sprintf(`<a href="/user/profile/%d">%s</a> %s`, u.ID, html.EscapeString(u.Username), action)
	if err := a.store.CreateLog(ctx, store.Log{UID: u.ID, Username: u.Username, Type: "group", TypeID: groupID, Content: content}); err != nil {
		a.logger.Error("写入动态失败", "err", err)
	}
}
