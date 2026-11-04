package api

import (
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/http"

	"github.com/PandaPanY/apiyard/internal/store"
)

func (a *API) registerLog(mux *http.ServeMux) {
	a.handle(mux, "GET /api/log/list", a.logList)
}

func logDoc(l *store.Log) map[string]any {
	m := map[string]any{
		"_id": l.ID, "uid": l.UID, "username": l.Username, "type": l.Type, "typeid": l.TypeID,
		"content": l.Content, "add_time": l.AddTime, "__v": 0,
	}
	if l.Data != nil {
		m["data"] = json.RawMessage(*l.Data)
	}
	return m
}

// logList 分页返回某个分组或项目的动态。分组动态包含分组内各项目的动态，并在前面加上项目名。
func (a *API) logList(c *call) error {
	typeID, ok := c.int("typeid")
	if !ok || typeID == 0 {
		return c.fail(400, "typeid不能为空")
	}
	typ, _ := c.str("type")
	if typ == "" {
		return c.fail(400, "type不能为空")
	}
	page, limit := 1, 10
	if n, ok := c.int("page"); ok && n > 0 {
		page = int(n)
	}
	if n, ok := c.int("limit"); ok && n > 0 {
		limit = int(n)
	}

	ctx := c.r.Context()
	var logs []*store.Log
	var count int
	var err error
	switch typ {
	case "group":
		projects, err := a.store.ProjectsByGroup(ctx, typeID)
		if err != nil {
			return err
		}
		ids := make([]int64, len(projects))
		names := map[int64]string{}
		for i, p := range projects {
			ids[i], names[p.ID] = p.ID, p.Name
		}
		if logs, err = a.store.ListGroupLogs(ctx, typeID, ids, page, limit); err != nil {
			return err
		}
		if count, err = a.store.CountGroupLogs(ctx, typeID, ids); err != nil {
			return err
		}
		for _, l := range logs {
			if l.Type == "project" {
				l.Content = fmt.Sprintf(`在 <a href="/project/%d">%s</a> 项目: `, l.TypeID, html.EscapeString(names[l.TypeID])) + l.Content
			}
		}
	case "project":
		f := store.LogFilter{Type: typ, TypeID: typeID}
		if id, ok := c.int("selectValue"); ok {
			f.InterfaceID = &id // 只看某个接口的改动
		}
		if logs, err = a.store.ListLogs(ctx, f, page, limit); err != nil {
			return err
		}
		if count, err = a.store.CountLogs(ctx, f); err != nil {
			return err
		}
	default:
		// YApi 对其他类型不设置响应体，Koa 返回 404
		http.NotFound(c.w, c.r)
		c.replied = true
		return nil
	}

	list := make([]map[string]any, 0, len(logs))
	for _, l := range logs {
		list = append(list, logDoc(l))
	}
	return c.ok(map[string]any{"list": list, "total": int(math.Ceil(float64(count) / float64(limit)))})
}
