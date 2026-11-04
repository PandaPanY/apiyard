package api

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/PandaPanY/apiyard/internal/auth"
	"github.com/PandaPanY/apiyard/internal/store"
)

// 个人中心与用户管理：查看、修改、改密码、删除用户，上传头像，以及按页面查询自己的角色。

func (a *API) registerProfile(mux *http.ServeMux) {
	a.handle(mux, "GET /api/user/find", a.userFind)
	a.handle(mux, "POST /api/user/update", a.userUpdate)
	a.handle(mux, "POST /api/user/change_password", a.userChangePassword)
	a.handle(mux, "POST /api/user/del", a.userDel)
	a.handle(mux, "POST /api/user/upload_avatar", a.userUploadAvatar)
	a.handle(mux, "GET /api/user/project", a.userProject)
}

// isSelf 对应 YApi 的 `id == this.getUid()`（宽松相等：字符串 "11" 等于数字 11）。
func (c *call) isSelf(key string) bool {
	id, ok := c.int(key)
	return ok && id == c.user.ID
}

func (a *API) userFind(c *call) error {
	// 与 YApi 相同：先查权限再查参数，所以普通用户不传 id 时得到的是“没有权限”
	if c.user.Role != "admin" && !c.isSelf("id") {
		return c.fail(401, "没有权限")
	}
	id, ok := c.int("id")
	if !ok || id == 0 {
		return c.fail(400, "uid不能为空")
	}
	u, err := a.store.UserByID(c.r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return c.fail(402, "不存在的用户")
	}
	if err != nil {
		return err
	}
	return c.ok(map[string]any{
		"uid": u.ID, "username": u.Username, "email": u.Email, "role": u.Role, "type": u.Type,
		"add_time": u.AddTime, "up_time": u.UpTime,
	})
}

func (a *API) userUpdate(c *call) error {
	if c.user.Role != "admin" && !c.isSelf("uid") {
		return c.fail(401, "没有权限")
	}
	uid, ok := c.int("uid")
	if !ok || uid == 0 {
		return c.fail(400, "uid不能为空")
	}
	ctx := c.r.Context()
	if _, err := a.store.UserByID(ctx, uid); errors.Is(err, store.ErrNotFound) {
		return c.fail(400, "uid不存在")
	} else if err != nil {
		return err
	}

	var username, email *string
	if s := c.trimmed("username"); s != "" {
		username = &s
	}
	if s := c.trimmed("email"); s != "" {
		email = &s
		// YApi 只要邮箱已存在就拒绝，即使是用户自己当前的邮箱
		if _, err := a.store.UserByEmail(ctx, s); err == nil {
			return c.fail(401, "该email已经注册")
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	n, err := a.store.UpdateProfile(ctx, uid, username, email)
	if err != nil {
		return err
	}
	return c.ok(updateResult(n, n))
}

func (a *API) userChangePassword(c *call) error {
	uid, ok := c.int("uid")
	if !ok || uid == 0 {
		return c.fail(400, "uid不能为空")
	}
	password, _ := c.str("password")
	if password == "" {
		return c.fail(400, "密码不能为空")
	}
	ctx := c.r.Context()
	target, err := a.store.UserByID(ctx, uid)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if c.user.Role != "admin" && uid != c.user.ID {
		return c.fail(402, "没有权限")
	}
	if target == nil {
		return errors.New("要修改密码的用户不存在") // YApi 在这里同样会抛异常
	}

	// 管理员可以直接重置普通用户的密码；修改自己或其他管理员的密码时必须提供旧密码
	if c.user.Role != "admin" || target.Role == "admin" {
		old, _ := c.str("old_password")
		if old == "" {
			return c.fail(400, "旧密码不能为空")
		}
		if !auth.CheckPassword(old, target.Passsalt, target.Password) {
			return c.fail(402, "旧密码错误")
		}
	}

	salt, err := auth.NewSalt()
	if err != nil {
		return err
	}
	n, err := a.store.UpdatePassword(ctx, uid, auth.HashPassword(password, salt), salt)
	if err != nil {
		return err
	}
	return c.ok(updateResult(n, n))
}

func (a *API) userDel(c *call) error {
	if c.user.Role != "admin" {
		return c.fail(402, "Without permission.")
	}
	if c.isSelf("id") {
		return c.fail(403, "禁止删除管理员")
	}
	id, ok := c.int("id")
	if !ok || id == 0 {
		return c.fail(400, "uid不能为空")
	}
	n, err := a.store.DeleteUser(c.r.Context(), id)
	if err != nil {
		return err
	}
	return c.ok(removeResult(n))
}

// avatarPrefixes 是 YApi 支持的头像格式（data URL 前缀 → MIME 类型）。
var avatarPrefixes = map[string]string{
	"data:image/png;base64,":  "image/png",
	"data:image/jpeg;base64,": "image/jpeg",
}

// maxAvatarSize 是 YApi 的头像大小上限（按 base64 长度估算的字节数）。
const maxAvatarSize = 200000

func (a *API) userUploadAvatar(c *call) error {
	basecode, _ := c.str("basecode")
	if basecode == "" {
		return c.fail(400, "basecode不能为空")
	}
	var mimeType, encoded string
	for prefix, t := range avatarPrefixes {
		if rest, ok := strings.CutPrefix(basecode, prefix); ok {
			mimeType, encoded = t, rest
		}
	}
	if mimeType == "" {
		return c.fail(400, "仅支持jpeg和png格式的图片")
	}
	// 与 YApi 相同的估算方式：base64 长度减去四分之一
	if n := float64(len(encoded)); n-n/8*2 > maxAvatarSize {
		return c.fail(400, "图片大小不能超过200kb")
	}
	// 与 YApi 不同：YApi 把 base64 字符串原样存下，这里解码后保存，非法数据直接拒绝
	image, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return c.fail(400, "图片数据不是合法的 base64")
	}

	created, err := a.store.SaveAvatar(c.r.Context(), c.user.ID, store.Avatar{Image: image, Type: mimeType})
	if err != nil {
		return err
	}
	if created {
		// MongoDB upsert 新建文档时的返回值
		result := updateResult(1, 0)
		result["upserted"] = []map[string]any{{"index": 0, "_id": store.NewObjectID()}}
		return c.ok(result)
	}
	return c.ok(updateResult(1, 1))
}

// userProject 返回当前用户在某个分组（或项目、接口）里的角色，前端进入对应页面时调用。
func (a *API) userProject(c *call) error {
	ctx := c.r.Context()
	id, _ := c.int("id")
	typ, _ := c.str("type")

	switch typ {
	case "interface":
		// 项目和接口要到 M2 才能创建，目前只会查不到。YApi 查不到接口时的响应如下
		return c.reply(map[string]any{"errcode": 422, "errmsg": "Cannot read property 'project_id' of null",
			"data": map[string]any{"interface": nil}})
	case "project":
		return c.reply(map[string]any{"errcode": 422, "errmsg": "Cannot read property 'toObject' of null", "data": map[string]any{}})
	case "group":
		g, err := a.store.GroupByID(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			return c.reply(map[string]any{"errcode": 422, "errmsg": "Cannot read property 'toObject' of null", "data": map[string]any{}})
		}
		if err != nil {
			return err
		}
		doc, err := a.groupFullDoc(ctx, g)
		if err != nil {
			return err
		}
		// YApi 用 checkAuth(..., 'site') 判断开发者，但 'site' 不是有效动作，结果总是 false，
		// 所以这里只会出现 owner 或 member
		manage, err := a.canManageGroup(ctx, c.user, id)
		if err != nil {
			return err
		}
		doc["role"] = "member"
		if manage {
			doc["role"] = "owner"
		}
		return c.ok(map[string]any{"group": doc})
	default:
		return c.ok(map[string]any{})
	}
}
