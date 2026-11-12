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

// tokenAuth resolves server-issued random credentials to their actual issuer.
// Legacy raw/encrypted YApi tokens are scoped integration capabilities with
// edit/view permission only; their unauthenticated uid never grants a role.
func (a *API) tokenAuth(ctx context.Context, token string) (projectID int64, user *store.User, err error) {
	projectID, uid, credentialErr := a.store.ProjectCredential(ctx, token)
	if credentialErr == nil {
		user, err = a.store.UserByID(ctx, uid)
		return projectID, user, err
	}
	if !errors.Is(credentialErr, store.ErrNotFound) {
		return 0, nil, credentialErr
	}
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
	_, err = a.store.UserByID(ctx, uid)
	if errors.Is(err, store.ErrNotFound) {
		return 0, nil, errInvalidToken
	}
	if err != nil {
		return 0, nil, err
	}
	// Legacy encrypted claims are unauthenticated. Never use their uid as an
	// authorization identity, even when the referenced user still exists.
	return projectID, &store.User{ID: legacyTokenUID, Role: "member", Username: "system"}, nil
}

// AuthenticateMCP resolves one or more comma-separated project tokens. Unlike the
// legacy open API behavior, MCP requires every supplied token to be valid.
func (a *API) AuthenticateMCP(ctx context.Context, tokens string) ([]int64, error) {
	parts := strings.Split(tokens, ",")
	projectIDs := make([]int64, 0, len(parts))
	seen := make(map[int64]struct{}, len(parts))
	for _, token := range parts {
		token = strings.TrimSpace(token)
		if token == "" {
			return nil, errInvalidToken
		}
		if _, _, err := a.store.ProjectCredential(ctx, token); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, errInvalidToken
			}
			return nil, err
		}
		projectID, user, err := a.tokenAuth(ctx, token)
		if err != nil {
			return nil, err
		}
		if user == nil {
			return nil, errInvalidToken
		}
		if ok, err := a.canProject(ctx, user, projectID, actionView); err != nil {
			return nil, err
		} else if !ok {
			return nil, errInvalidToken
		}
		if _, ok := seen[projectID]; !ok {
			seen[projectID] = struct{}{}
			projectIDs = append(projectIDs, projectID)
		}
	}
	return projectIDs, nil
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

// canWrite enforces both project scope and the issuer's current role.
func (a *API) canWrite(ctx context.Context, u *store.User, projectID int64, action string) (bool, error) {
	if scope, ok := tokenScope(ctx); ok {
		if projectID != scope {
			return false, nil
		}
	}
	return a.canProject(ctx, u, projectID, action)
}
