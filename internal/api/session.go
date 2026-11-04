package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/PandaPanY/apiyard/internal/auth"
	"github.com/PandaPanY/apiyard/internal/store"
)

// 登录状态保存在两个 Cookie 里，名称与 YApi 相同，迁移后用户无需重新登录。
const (
	tokenCookie = "_yapi_token"
	uidCookie   = "_yapi_uid"
)

// currentUser 按 YApi 的 checkLogin 校验登录 Cookie；未登录或令牌无效时返回 nil。
func (a *API) currentUser(r *http.Request) (*store.User, error) {
	token, err1 := r.Cookie(tokenCookie)
	uidValue, err2 := r.Cookie(uidCookie)
	if err1 != nil || err2 != nil || token.Value == "" || uidValue.Value == "" {
		return nil, nil
	}
	uid, err := strconv.ParseInt(uidValue.Value, 10, 64)
	if err != nil {
		return nil, nil
	}
	u, err := a.store.UserByID(r.Context(), uid)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if got, err := auth.VerifyToken(token.Value, u.Passsalt, a.now()); err != nil || got != uid {
		return nil, nil
	}
	return u, nil
}

// setLoginCookie 写入登录 Cookie，有效期 7 天。
func (a *API) setLoginCookie(w http.ResponseWriter, r *http.Request, u *store.User) error {
	now := a.now()
	token, err := auth.SignToken(u.ID, u.Passsalt, now)
	if err != nil {
		return err
	}
	expires := now.Add(auth.TokenTTL)
	http.SetCookie(w, sessionCookie(r, tokenCookie, token, expires))
	http.SetCookie(w, sessionCookie(r, uidCookie, strconv.FormatInt(u.ID, 10), expires))
	return nil
}

// clearLoginCookie 清除登录 Cookie。
func clearLoginCookie(w http.ResponseWriter, r *http.Request) {
	for _, name := range []string{tokenCookie, uidCookie} {
		http.SetCookie(w, sessionCookie(r, name, "", time.Unix(0, 0)))
	}
}

// sessionCookie 构造登录 Cookie。与 YApi 相比增加了 SameSite=Lax（防 CSRF）；
// Secure 只在 HTTPS 访问时设置：内网常用 HTTP 部署，强制 Secure 会导致无法登录。
func sessionCookie(r *http.Request, name, value string, expires time.Time) *http.Cookie {
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	return &http.Cookie{ // #nosec G124 -- Secure 按上面的规则按需设置
		Name: name, Value: value, Path: "/", Expires: expires,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secure,
	}
}

// groupRole 返回当前用户在分组中的角色，对应 YApi getProjectRole(id, 'group')：
// admin（站点管理员）、owner（创建者或组长）、dev、guest，都不是时为 member。
// 分组不存在时返回空字符串（YApi 在这种情况下返回 false）。
func (a *API) groupRole(ctx context.Context, u *store.User, groupID int64) (string, error) {
	if u.Role == "admin" {
		return "admin", nil
	}
	g, err := a.store.GroupByID(ctx, groupID)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if g.UID == u.ID {
		return "owner", nil
	}
	m, err := a.store.GroupMember(ctx, groupID, u.ID)
	if errors.Is(err, store.ErrNotFound) {
		return "member", nil
	}
	if err != nil {
		return "", err
	}
	switch m.Role {
	case "owner", "dev":
		return m.Role, nil
	default:
		return "guest", nil
	}
}

// canManageGroup 对应 YApi checkAuth(id, 'group', 'danger')：只有管理员和组长可以执行。
func (a *API) canManageGroup(ctx context.Context, u *store.User, groupID int64) (bool, error) {
	role, err := a.groupRole(ctx, u, groupID)
	return allows(role, actionDanger), err
}

// projectRole 返回当前用户在项目中的角色，对应 YApi getProjectRole(id, 'project')：
// 管理员 → admin；项目创建者 → owner；项目成员按其角色；都不是时看所在分组的角色。项目不存在时返回空字符串。
func (a *API) projectRole(ctx context.Context, u *store.User, projectID int64) (string, error) {
	if u.Role == "admin" {
		return "admin", nil
	}
	p, err := a.store.ProjectByID(ctx, projectID)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if p.UID == u.ID {
		return "owner", nil
	}
	m, err := a.store.ProjectMemberOf(ctx, projectID, u.ID)
	if err == nil {
		if m.Role == "owner" || m.Role == "dev" {
			return m.Role, nil
		}
		return "guest", nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	return a.groupRole(ctx, u, p.GroupID)
}

// 权限动作，对应 YApi checkAuth 的 action 参数。
const (
	actionDanger = "danger" // 删除、修改设置等：管理员和组长
	actionEdit   = "edit"   // 编辑内容：再加上开发者
	actionView   = "view"   // 查看：再加上访客
)

// allows 判断角色能否执行某类动作。
func allows(role, action string) bool {
	switch role {
	case "admin", "owner":
		return true
	case "dev":
		return action == actionEdit || action == actionView
	case "guest":
		return action == actionView
	default:
		return false
	}
}

// canProject 对应 YApi checkAuth(id, 'project', action)。
func (a *API) canProject(ctx context.Context, u *store.User, projectID int64, action string) (bool, error) {
	role, err := a.projectRole(ctx, u, projectID)
	return allows(role, action), err
}

// canGroup 对应 YApi checkAuth(id, 'group', action)。
func (a *API) canGroup(ctx context.Context, u *store.User, groupID int64, action string) (bool, error) {
	role, err := a.groupRole(ctx, u, groupID)
	return allows(role, action), err
}
