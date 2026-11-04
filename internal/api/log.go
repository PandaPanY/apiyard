package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math"
	"net/http"
	"strings"

	"github.com/PandaPanY/apiyard/internal/store"
)

func (a *API) registerLog(mux *http.ServeMux) {
	a.handle(mux, "GET /api/log/list", a.logList)
	a.handle(mux, "POST /api/log/list_by_update", a.logListByUpdate, schema(
		field{name: "type", kind: "string", required: true}, field{name: "typeid", kind: "number", required: true},
		field{name: "apis", kind: "array"}))
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
		// 与 YApi 不同：私有项目的动态要求查看权限
		if denied, err := a.privateDenied(ctx, c.user, typeID, actionView); err != nil || denied {
			return c.sendDenied(err)
		}
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

// updateLogFields 是 list_by_update 返回的动态字段（YApi listWithCatid 的 select）。
var updateLogFields = []string{"_id", "uid", "content", "type", "username", "typeid", "add_time"}

// logListByUpdate 按接口的方法和路径查找每个接口最近一次修改的动态，数据导入时用来提示哪些接口改过。
// apis 中的路径带有项目 basepath 时先去掉。与 YApi 不同：私有项目要求查看权限。
func (a *API) logListByUpdate(c *call) error {
	ctx := c.r.Context()
	typ, _ := c.str("type")
	typeID, _ := c.int("typeid")
	project, err := a.store.ProjectByID(ctx, typeID)
	if errors.Is(err, store.ErrNotFound) {
		return c.fail(402, "Cannot read property 'toObject' of null")
	}
	if err != nil {
		return err
	}
	apis, ok := c.params["apis"].([]any)
	if !ok {
		return c.fail(402, "Cannot read property 'length' of undefined")
	}
	if denied, err := a.privateDenied(ctx, c.user, typeID, actionView); err != nil || denied {
		return c.sendDenied(err)
	}

	basepath := deref(project.Basepath)
	list := []map[string]any{}
	for _, item := range apis {
		api, _ := item.(map[string]any)
		path, _, _ := castString(api["path"])
		method, _, _ := castString(api["method"]) // 没传 method 时与 YApi 一样匹配任意方法
		if basepath != "" {
			path = strings.TrimPrefix(path, basepath)
		}
		found, err := a.store.InterfacesByRoute(ctx, typeID, path, method)
		if err != nil {
			return err
		}
		for _, it := range found {
			id := it.ID
			logs, err := a.store.ListLogs(ctx, store.LogFilter{Type: typ, TypeID: typeID, InterfaceID: &id}, 1, 1)
			if err != nil {
				return err
			}
			for _, l := range logs {
				list = append(list, pickFields(logDoc(l), updateLogFields))
			}
		}
	}
	return c.ok(list)
}
