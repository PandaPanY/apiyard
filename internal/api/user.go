package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/PandaPanY/apiyard/internal/auth"
	"github.com/PandaPanY/apiyard/internal/store"
)

func (a *API) registerUser(mux *http.ServeMux) {
	a.handle(mux, "GET /api/user/status", a.userStatus, public())
	a.handle(mux, "POST /api/user/login", a.userLogin, public())
	a.handle(mux, "POST /api/user/reg", a.userReg, public())
	a.handle(mux, "GET /api/user/logout", a.userLogout, public())
	a.handle(mux, "GET /api/user/avatar", a.userAvatar, public())
	a.handle(mux, "GET /api/user/search", a.userSearch)
	a.handle(mux, "GET /api/user/list", a.userList)
	a.handle(mux, "GET /api/user/up_study", a.userUpStudy)
}

// loginData 是登录、注册成功时返回的用户信息。
func loginData(u *store.User) map[string]any {
	return map[string]any{
		"uid":      u.ID,
		"username": u.Username,
		"email":    u.Email,
		"role":     u.Role,
		"type":     "site",
		"study":    u.Study,
		"add_time": u.AddTime,
		"up_time":  u.UpTime,
	}
}

// userDoc 是用户列表等接口返回的字段（YApi 的 select 字段）。
func userDoc(u *store.User) map[string]any {
	return map[string]any{
		"_id":      u.ID,
		"username": u.Username,
		"email":    u.Email,
		"role":     u.Role,
		"type":     u.Type,
		"study":    u.Study,
		"add_time": u.AddTime,
		"up_time":  u.UpTime,
	}
}

// userStatus 返回登录状态。未登录时 errcode 为 40011，并附带 ladp（YApi 原拼写）和 canRegister。
func (a *API) userStatus(c *call) error {
	body := map[string]any{"errcode": 40011, "errmsg": "请登录...", "data": nil}
	if c.user != nil {
		body = map[string]any{"errcode": 0, "errmsg": "成功！", "data": userDoc(c.user)}
	}
	body["ladp"] = false // ApiYard 暂不支持 LDAP 登录
	body["canRegister"] = !a.opts.CloseRegister
	return c.reply(body)
}

func (a *API) userLogin(c *call) error {
	email := c.trimmed("email")
	password, _ := c.str("password")
	if email == "" {
		return c.fail(400, "email不能为空")
	}
	if password == "" {
		return c.fail(400, "密码不能为空")
	}

	u, err := a.store.UserByEmail(c.r.Context(), email)
	if errors.Is(err, store.ErrNotFound) {
		return c.fail(404, "该用户不存在")
	}
	if err != nil {
		return err
	}
	if !auth.CheckPassword(password, u.Passsalt, u.Password) {
		return c.fail(405, "密码错误")
	}
	if err := a.setLoginCookie(c.w, c.r, u); err != nil {
		return err
	}
	// 登录成功的提示文字是 YApi 原版的笔误，保留以保证兼容
	return c.reply(map[string]any{"errcode": 0, "errmsg": "logout success...", "data": loginData(u)})
}

func (a *API) userReg(c *call) error {
	if a.opts.CloseRegister {
		return c.fail(400, "禁止注册，请联系管理员")
	}
	email, password, username := c.trimmed("email"), c.trimmed("password"), c.trimmed("username")
	if email == "" {
		return c.fail(400, "邮箱不能为空")
	}
	if password == "" {
		return c.fail(400, "密码不能为空")
	}

	ctx := c.r.Context()
	if _, err := a.store.UserByEmail(ctx, email); err == nil {
		return c.fail(401, "该email已经注册")
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if username == "" {
		username, _, _ = strings.Cut(email, "@")
	}

	u, err := newUser(username, email, password, "member")
	if err != nil {
		return err
	}
	if err := a.store.CreateUser(ctx, u); err != nil {
		return err
	}
	if err := a.setLoginCookie(c.w, c.r, u); err != nil {
		return err
	}
	if _, err := a.createPrivateGroup(ctx, u.ID); err != nil {
		return err
	}
	return c.ok(loginData(u))
}

// newUser 生成新用户：随机 passsalt，按 YApi 的算法哈希密码。
func newUser(username, email, password, role string) (*store.User, error) {
	salt, err := auth.NewSalt()
	if err != nil {
		return nil, err
	}
	now := store.Now()
	return &store.User{
		Username: username, Email: email, Role: role, Type: "site",
		Password: auth.HashPassword(password, salt), Passsalt: salt,
		AddTime: now, UpTime: now,
	}, nil
}

func (a *API) userLogout(c *call) error {
	clearLoginCookie(c.w, c.r)
	return c.ok("ok")
}

// searchRejected 与 YApi 的 validateSearchKeyword 相同：以 * 开头、以 . 结尾或含 ?+$^\ 的关键词会被拒绝。
var searchRejected = regexp.MustCompile(`^\*|\?|\+|\$|\^|\\|\.$`)

func (a *API) userSearch(c *call) error {
	q, _ := c.str("q")
	if q == "" {
		return c.reply(map[string]any{"errcode": 400, "errmsg": "No keyword."}) // YApi 此时没有 data 字段
	}
	if searchRejected.MatchString(q) {
		return c.reply(map[string]any{"errcode": 400, "errmsg": "Bad query."})
	}
	users, err := a.store.SearchUsers(c.r.Context(), q)
	if err != nil {
		return err
	}
	list := make([]map[string]any, 0, len(users))
	for _, u := range users {
		list = append(list, map[string]any{
			"uid": u.ID, "username": u.Username, "email": u.Email, "role": u.Role,
			"addTime": u.AddTime, "upTime": u.UpTime,
		})
	}
	return c.reply(map[string]any{"errcode": 0, "errmsg": "ok", "data": list})
}

func (a *API) userList(c *call) error {
	page, limit := 1, 10
	if n, ok := c.int("page"); ok && n > 0 {
		page = int(n)
	}
	if n, ok := c.int("limit"); ok && n > 0 {
		limit = int(n)
	}
	ctx := c.r.Context()
	users, err := a.store.ListUsers(ctx, page, limit)
	if err != nil {
		return err
	}
	count, err := a.store.CountUsers(ctx)
	if err != nil {
		return err
	}
	list := make([]map[string]any, 0, len(users))
	for _, u := range users {
		list = append(list, userDoc(u))
	}
	return c.ok(map[string]any{
		"count": count,
		"total": int(math.Ceil(float64(count) / float64(limit))),
		"list":  list,
	})
}

func (a *API) userUpStudy(c *call) error {
	n, err := a.store.MarkStudied(c.r.Context(), c.user.ID)
	if err != nil {
		return err
	}
	return c.ok(updateResult(n, n))
}

// userAvatar 返回头像图片；用户没有上传过时返回前端自带的默认头像。
func (a *API) userAvatar(c *call) error {
	uid, ok := c.int("uid")
	if !ok && c.user != nil {
		uid = c.user.ID
	}
	img, err := a.store.AvatarOf(c.r.Context(), uid)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if img == nil {
		if img, err = a.defaultAvatar(); err != nil {
			http.NotFound(c.w, c.r)
			c.replied = true
			return nil
		}
	}
	c.replied = true
	c.w.Header().Set("Content-Type", img.Type)
	c.w.Header().Set("Content-Length", strconv.Itoa(len(img.Image)))
	_, err = c.w.Write(img.Image)
	return err
}

func (a *API) defaultAvatar() (*store.Avatar, error) {
	if a.opts.WebFS == nil {
		return nil, fs.ErrNotExist
	}
	data, err := fs.ReadFile(a.opts.WebFS, "image/avatar.png")
	if err != nil {
		return nil, err
	}
	return &store.Avatar{Image: data, Type: "image/png"}, nil
}

// Bootstrap 在数据库没有任何用户时创建管理员账号（对应 YApi 的 install 脚本），返回是否创建。
// password 为空时生成随机密码，并通过返回值交给调用方提示用户。
// 与 YApi 一样不为管理员创建个人空间，首次访问时再按需创建。
func Bootstrap(ctx context.Context, s *store.Store, email, password string) (created bool, usedPassword string, err error) {
	n, err := s.CountUsers(ctx)
	if err != nil || n > 0 {
		return false, "", err
	}
	if password == "" {
		if password, err = auth.NewSalt(); err != nil {
			return false, "", err
		}
	}
	username, _, _ := strings.Cut(email, "@")
	u, err := newUser(username, email, password, "admin")
	if err != nil {
		return false, "", err
	}
	if err := s.CreateUser(ctx, u); err != nil {
		return false, "", fmt.Errorf("创建管理员: %w", err)
	}
	return true, password, nil
}
