package api

import (
	"errors"
	"net/http"

	"github.com/PandaPanY/apiyard/internal/store"
)

func (a *API) registerFollow(mux *http.ServeMux) {
	a.handle(mux, "GET /api/follow/list", a.followList)
	a.handle(mux, "POST /api/follow/add", a.followAdd)
	a.handle(mux, "POST /api/follow/del", a.followDel)
}

// followDoc 是完整的关注文档；icon、color 为空时不输出。
func followDoc(f *store.Follow) map[string]any {
	m := map[string]any{"_id": f.ID, "uid": f.UID, "projectid": f.ProjectID, "projectname": f.ProjectName, "__v": 0}
	setOptional(m, "icon", f.Icon)
	setOptional(m, "color", f.Color)
	return m
}

func (a *API) followList(c *call) error {
	follows, err := a.store.FollowsOf(c.r.Context(), c.user.ID)
	if err != nil {
		return err
	}
	list := make([]map[string]any, 0, len(follows))
	for _, f := range follows {
		list = append(list, followDoc(f))
	}
	return c.ok(map[string]any{"list": list})
}

func (a *API) followAdd(c *call) error {
	ctx := c.r.Context()
	projectID, ok := c.int("projectid")
	if !ok || projectID == 0 {
		return c.fail(400, "项目id不能为空")
	}
	if following, err := a.store.IsFollowing(ctx, c.user.ID, projectID); err != nil {
		return err
	} else if following {
		return c.fail(401, "项目已关注")
	}
	p, err := a.store.ProjectByID(ctx, projectID)
	if errors.Is(err, store.ErrNotFound) {
		return c.fail(402, "Cannot read property 'toObject' of null")
	}
	if err != nil {
		return err
	}

	f := &store.Follow{UID: c.user.ID, ProjectID: p.ID, ProjectName: p.Name, Icon: p.Icon, Color: p.Color}
	if err := a.store.CreateFollow(ctx, f); err != nil {
		return err
	}
	doc := followDoc(f)
	delete(doc, "__v") // 新建接口只返回部分字段
	return c.ok(doc)
}

func (a *API) followDel(c *call) error {
	ctx := c.r.Context()
	projectID, ok := c.int("projectid")
	if !ok || projectID == 0 {
		return c.fail(400, "项目id不能为空")
	}
	if following, err := a.store.IsFollowing(ctx, c.user.ID, projectID); err != nil {
		return err
	} else if !following {
		return c.fail(401, "项目未关注")
	}
	n, err := a.store.DeleteFollow(ctx, c.user.ID, projectID)
	if err != nil {
		return err
	}
	return c.ok(removeResult(n))
}
