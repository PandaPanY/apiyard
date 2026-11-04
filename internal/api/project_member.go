package api

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"slices"
	"strings"

	"github.com/PandaPanY/apiyard/internal/store"
)

func (a *API) registerProjectMember(mux *http.ServeMux) {
	id := field{name: "id", kind: "number", required: true}
	memberUID := field{name: "member_uid", kind: "number", required: true}
	role := field{name: "role", kind: "string", enum: memberRoles}

	a.handle(mux, "GET /api/project/get_member_list", a.projectMemberList, schema(id))
	a.handle(mux, "POST /api/project/add_member", a.projectAddMember,
		schema(id, role, field{name: "member_uids", kind: "array", required: true}))
	a.handle(mux, "POST /api/project/del_member", a.projectDelMember, schema(id, memberUID))
	a.handle(mux, "POST /api/project/change_member_role", a.projectChangeRole, schema(id, memberUID, role))
	a.handle(mux, "POST /api/project/change_member_email_notice", a.projectChangeNotice)
}

// projectMemberList 返回项目成员。
// 与 YApi 不同：YApi 不检查权限，任何登录用户都能看到私有项目的成员；这里与 project/get 一致，私有项目要求有查看权限。
func (a *API) projectMemberList(c *call) error {
	ctx := c.r.Context()
	id, _ := c.int("id")
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
	members, err := a.store.ProjectMembers(ctx, id)
	if err != nil {
		return err
	}
	return c.ok(projectMemberDocs(members))
}

func (a *API) projectAddMember(c *call) error {
	ctx := c.r.Context()
	id, _ := c.int("id")
	if ok, err := a.canProject(ctx, c.user, id, actionEdit); err != nil {
		return err
	} else if !ok {
		return c.fail(405, "没有权限")
	}
	role, _ := c.str("role")
	if !slices.Contains(memberRoles, role) {
		role = "dev"
	}

	add, exist, missing := []store.Member{}, []any{}, []int64{}
	for _, uid := range intList(c.params["member_uids"]) {
		u, err := a.store.UserByID(ctx, uid)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		_, memberErr := a.store.ProjectMemberOf(ctx, id, uid)
		switch {
		case memberErr == nil:
			// 已是成员；用户已被删除时 YApi 在这里放的是 null
			if u == nil {
				exist = append(exist, nil)
			} else {
				exist = append(exist, map[string]any{"role": role, "uid": u.ID, "username": u.Username, "email": u.Email})
			}
		case u == nil:
			missing = append(missing, uid)
		default:
			add = append(add, store.Member{UID: u.ID, Role: role, Username: &u.Username, Email: &u.Email})
		}
	}

	exists, err := a.store.AddProjectMembers(ctx, id, add)
	if err != nil {
		return err
	}
	if len(add) > 0 {
		links := make([]string, len(add))
		for i, m := range add {
			links[i] = fmt.Sprintf(`<a href = "/user/profile/%d">%s</a>`, m.UID, html.EscapeString(*m.Username))
		}
		a.projectLog(ctx, c.user, id, "添加了项目成员 "+strings.Join(links, "、"))
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

// requireProjectMember 返回项目成员；不存在时已经写好 400 响应，返回 nil。
func (a *API) requireProjectMember(c *call, projectID, uid int64) (*store.ProjectMember, error) {
	m, err := a.store.ProjectMemberOf(c.r.Context(), projectID, uid)
	if errors.Is(err, store.ErrNotFound) {
		return nil, c.fail(400, "项目成员不存在")
	}
	return m, err
}

func (a *API) projectDelMember(c *call) error {
	ctx := c.r.Context()
	id, _ := c.int("id")
	uid, _ := c.int("member_uid")
	if m, err := a.requireProjectMember(c, id, uid); m == nil {
		return err
	}
	if ok, err := a.canProject(ctx, c.user, id, actionDanger); err != nil {
		return err
	} else if !ok {
		return c.fail(405, "没有权限")
	}
	n, err := a.store.RemoveProjectMember(ctx, id, uid)
	if err != nil {
		return err
	}
	a.projectLog(ctx, c.user, id, fmt.Sprintf(`删除了项目中的成员 <a href="/user/profile/%d">%s</a>`,
		uid, html.EscapeString(a.usernameOf(ctx, uid))))
	return c.ok(updateResult(1, n))
}

func (a *API) projectChangeRole(c *call) error {
	ctx := c.r.Context()
	id, _ := c.int("id")
	uid, _ := c.int("member_uid")
	member, err := a.requireProjectMember(c, id, uid)
	if member == nil {
		return err
	}
	if ok, err := a.canProject(ctx, c.user, id, actionDanger); err != nil {
		return err
	} else if !ok {
		return c.fail(405, "没有权限")
	}
	role, _ := c.str("role")
	if !slices.Contains(memberRoles, role) {
		role = "dev"
	}
	n, err := a.store.ChangeProjectMemberRole(ctx, id, uid, role)
	if err != nil {
		return err
	}
	modified := n
	if member.Role == role {
		modified = 0
	}
	a.projectLog(ctx, c.user, id, fmt.Sprintf(`修改了项目中的成员 <a href="/user/profile/%d">%s</a> 的角色为 "%s"`,
		uid, html.EscapeString(a.usernameOf(ctx, uid)), roleNames[role]))
	return c.ok(updateResult(n, modified))
}

// projectChangeNotice 设置成员是否接收项目的邮件通知。
// 与 YApi 不同：YApi 不检查权限，任何登录用户都能改别人的设置；这里只允许本人或组长、管理员修改。
func (a *API) projectChangeNotice(c *call) error {
	ctx := c.r.Context()
	id, _ := c.int("id")
	uid, _ := c.int("member_uid")
	member, err := a.requireProjectMember(c, id, uid)
	if member == nil {
		return err
	}
	if uid != c.user.ID {
		if ok, err := a.canProject(ctx, c.user, id, actionDanger); err != nil {
			return err
		} else if !ok {
			return c.fail(405, "没有权限")
		}
	}
	notice := false
	if b := c.optionalBool("notice"); b != nil {
		notice = *b
	}
	n, err := a.store.SetProjectMemberNotice(ctx, id, uid, notice)
	if err != nil {
		return err
	}
	modified := n
	if member.EmailNotice == notice {
		modified = 0
	}
	return c.ok(updateResult(n, modified))
}
