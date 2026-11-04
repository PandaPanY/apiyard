package api

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/PandaPanY/apiyard/internal/store"
	"github.com/PandaPanY/apiyard/internal/yapitoken"
)

// openAPIPaths 是可以用项目 token 访问的接口（YApi base.js 的 openApiRouter），另外 /api/open/ 下的全部接口也可以。
var openAPIPaths = map[string]bool{
	"/api/open/run_auto_test":   true,
	"/api/open/import_data":     true,
	"/api/interface/add":        true,
	"/api/interface/save":       true,
	"/api/interface/up":         true,
	"/api/interface/get":        true,
	"/api/interface/list":       true,
	"/api/interface/list_menu":  true,
	"/api/interface/add_cat":    true,
	"/api/interface/getCatMenu": true,
	"/api/interface/list_cat":   true,
	"/api/project/get":          true,
	"/api/plugin/export":        true,
	"/api/project/up":           true,
	"/api/plugin/exportSwagger": true,
}

func isOpenAPIPath(path string) bool {
	return openAPIPaths[path] || strings.HasPrefix(path, "/api/open/")
}

// legacyTokenUID 是旧格式 token（未加密的原始项目 token）对应的用户：YApi 把它当作一个名为 system 的普通会员。
const legacyTokenUID = 999999

// tokenScopeKey 在请求的 context 中记录 token 所属的项目。
type tokenScopeKey struct{}

// tokenScope 返回 token 所属的项目；不是通过 token 访问时 ok 为 false。
func tokenScope(ctx context.Context) (projectID int64, ok bool) {
	projectID, ok = ctx.Value(tokenScopeKey{}).(int64)
	return projectID, ok
}

// errInvalidToken 表示 token 能解密，但对应的项目或用户不存在。
var errInvalidToken = errors.New("token 无效")

// tokenAuth 按 YApi 的规则解析开放 API 的 token，返回 token 所属的项目和代表的用户。
//   - 新格式：用 passsalt 加密的 "uid|原始 token"，代表生成 token 的用户；
//   - 旧格式：原始项目 token，代表 uid 为 999999 的 system 用户；
//   - 两种都对不上时返回 user 为 nil，与 YApi 一样忽略 token，沿用 Cookie 的登录状态。
//
// 与 YApi 不同：能解密但项目 token 或用户不存在时返回 errInvalidToken。YApi 只写了一个 42014 的响应就继续往下执行，
// 结果把请求当成 token 里 uid 对应的用户；在默认的 passsalt 下，这等于可以伪造任意用户的身份。
func (a *API) tokenAuth(ctx context.Context, token string) (projectID int64, user *store.User, err error) {
	uidText, raw, decodeErr := yapitoken.Decode(token, a.tokenSalt())
	if decodeErr != nil {
		projectID, err = a.store.ProjectIDByToken(ctx, token)
		if errors.Is(err, store.ErrNotFound) {
			return 0, nil, nil
		}
		if err != nil {
			return 0, nil, err
		}
		return projectID, &store.User{ID: legacyTokenUID, Role: "member", Username: "system"}, nil
	}

	projectID, err = a.store.ProjectIDByToken(ctx, raw)
	if errors.Is(err, store.ErrNotFound) {
		return 0, nil, errInvalidToken
	}
	if err != nil {
		return 0, nil, err
	}
	uid, convErr := strconv.ParseInt(uidText, 10, 64)
	if convErr != nil {
		return 0, nil, errInvalidToken
	}
	if uid == legacyTokenUID {
		return projectID, &store.User{ID: legacyTokenUID, Role: "member", Username: "system"}, nil
	}
	user, err = a.store.UserByID(ctx, uid)
	if errors.Is(err, store.ErrNotFound) {
		return 0, nil, errInvalidToken
	}
	return projectID, user, err
}

// applyToken 在参数校验之前处理 token：认证成功时以 token 代表的用户身份继续，并把 project_id（以及导出接口用的 pid）
// 改成 token 所属的项目，与 YApi 覆盖 ctx.params.project_id 的做法一致。返回 false 表示已经输出了错误响应。
func (a *API) applyToken(c *call, params map[string]any) (bool, error) {
	token, ok := params["token"].(string)
	if !ok || token == "" || !isOpenAPIPath(c.r.URL.Path) {
		return true, nil
	}
	projectID, user, err := a.tokenAuth(c.r.Context(), token)
	if errors.Is(err, errInvalidToken) {
		return false, c.fail(42014, "token 无效")
	}
	if err != nil || user == nil {
		return err == nil, err
	}
	c.user = user
	params["project_id"] = projectID
	params["pid"] = projectID
	c.r = c.r.WithContext(context.WithValue(c.r.Context(), tokenScopeKey{}, projectID))
	return true, nil
}

// canWrite 对应 YApi 中 "if (!this.$tokenAuth) checkAuth(...)" 的写法：通过 token 访问时不检查角色。
// 与 YApi 不同：token 只能操作所属的项目。YApi 在这里完全不检查，任何项目的 token 都能改其他项目的接口和分类。
func (a *API) canWrite(ctx context.Context, u *store.User, projectID int64, action string) (bool, error) {
	if scope, ok := tokenScope(ctx); ok {
		return projectID == scope, nil
	}
	return a.canProject(ctx, u, projectID, action)
}
