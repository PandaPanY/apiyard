package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/PandaPanY/apiyard/internal/store"
)

// 接口参数的校验规则，对应 YApi interface 控制器的 schemaMap。
var interfaceSchemas = func() (s struct{ add, up, save []field }) {
	str := func(n string) field { return field{name: n, kind: "string"} }
	text := func(n string, required bool) field { return field{name: n, kind: "string", required: required, min: 1} }
	num := func(n string, required bool) field { return field{name: n, kind: "number", required: required} }
	arr := func(n string) field { return field{name: n, kind: "array"} }
	boolean := func(n string) field { return field{name: n, kind: "boolean"} }
	common := []field{
		str("desc"), str("status"), arr("req_query"), arr("req_headers"), str("req_body_type"), arr("req_params"),
		arr("req_body_form"), str("req_body_other"), str("res_body_type"), str("res_body"), str("custom_field_value"),
		boolean("api_opened"), str("req_body_is_json_schema"), str("res_body_is_json_schema"), str("markdown"), arr("tag"),
	}
	s.add = append([]field{num("project_id", true), text("path", true), text("title", true), text("method", true),
		num("catid", true)}, common...)
	s.up = append([]field{num("id", true), num("project_id", false), text("path", false), text("title", false),
		text("method", false), num("catid", false), boolean("switch_notice"), text("message", false)}, common...)
	s.save = append([]field{num("project_id", false), num("catid", false), text("title", false), text("path", false),
		text("method", false), text("message", false), boolean("switch_notice"), str("dataSync")}, common...)
	return s
}()

func (a *API) registerInterface(mux *http.ServeMux) {
	a.handle(mux, "POST /api/interface/add", a.interfaceAdd, schema(interfaceSchemas.add...))
	a.handle(mux, "POST /api/interface/save", a.interfaceSave, schema(interfaceSchemas.save...))
	a.handle(mux, "POST /api/interface/up", a.interfaceUp, schema(interfaceSchemas.up...))
	a.handle(mux, "GET /api/interface/get", a.interfaceGet)
	a.handle(mux, "GET /api/interface/list", a.interfaceList)
	a.handle(mux, "GET /api/interface/list_cat", a.interfaceListCat)
	a.handle(mux, "GET /api/interface/list_menu", a.interfaceListMenu)
	a.handle(mux, "POST /api/interface/del", a.interfaceDel)
	a.handle(mux, "POST /api/interface/add_cat", a.interfaceAddCat)
	a.handle(mux, "POST /api/interface/up_cat", a.interfaceUpCat)
	a.handle(mux, "POST /api/interface/del_cat", a.interfaceDelCat)
	a.handle(mux, "GET /api/interface/getCatMenu", a.interfaceCatMenu)
	a.handle(mux, "POST /api/interface/up_index", a.interfaceUpIndex)
	a.handle(mux, "POST /api/interface/up_cat_index", a.interfaceUpCatIndex)
	a.handle(mux, "POST /api/interface/schema2json", a.interfaceSchema2JSON)
	mux.HandleFunc("GET /api/interface/solve_conflict", a.interfaceSolveConflict)
}

// outcome 是一次业务处理的结果。add、up 的逻辑要被 save 复用，而 save 不使用它们的响应，所以先返回结果再决定是否输出。
type outcome struct {
	code int
	msg  string
	data any
}

func success(data any) outcome             { return outcome{data: data} }
func failure(code int, msg string) outcome { return outcome{code: code, msg: msg} }

func (c *call) send(o outcome, err error) error {
	if err != nil {
		return err
	}
	if o.code != 0 {
		return c.fail(o.code, o.msg)
	}
	return c.ok(o.data)
}

// interfaceLog 写入一条项目动态，data 不为 nil 时一并保存（用于“查看改动”）。
func (a *API) interfaceLog(ctx context.Context, u *store.User, projectID int64, action string, data any) {
	l := store.Log{UID: u.ID, Username: u.Username, Type: "project", TypeID: projectID,
		Content: fmt.Sprintf(`<a href="/user/profile/%d">%s</a> %s`, u.ID, html.EscapeString(u.Username), action)}
	if data != nil {
		s, err := marshalCompact(data)
		if err != nil {
			a.logger.Error("序列化动态数据失败", "err", err)
			return
		}
		l.Data = &s
	}
	if err := a.store.CreateLog(ctx, l); err != nil {
		a.logger.Error("写入动态失败", "err", err)
	}
}

// touchProject 刷新项目的 up_time（YApi 在接口变动后都会这样做）。
func (a *API) touchProject(ctx context.Context, projectID int64) {
	if _, err := a.store.UpdateProject(ctx, projectID, store.ProjectPatch{}); err != nil {
		a.logger.Error("刷新项目更新时间失败", "err", err)
	}
}

// autoAddTag 把接口用到、项目里还没有的 tag 加到项目的 tag 列表中（YApi 的 autoAddTag）。
func (a *API) autoAddTag(ctx context.Context, projectID int64, p map[string]any) error {
	tags, _ := p["tag"].([]any)
	if len(tags) == 0 {
		return nil
	}
	project, err := a.store.ProjectByID(ctx, projectID)
	if err != nil {
		return err
	}
	var existing []any
	if err := json.Unmarshal([]byte(project.Tag), &existing); err != nil {
		return err
	}
	has := func(name any) bool {
		for _, e := range existing {
			if m, ok := e.(map[string]any); ok && m["name"] == name {
				return true
			}
		}
		return false
	}
	add := func(tag any) { existing = append(existing, map[string]any{"name": tag, "desc": tag}) }
	if len(existing) == 0 {
		// 项目还没有 tag 时 YApi 直接加入全部 tag，不去重
		for _, tag := range tags {
			add(tag)
		}
	} else {
		changed := false
		for _, tag := range tags {
			if !has(tag) {
				add(tag)
				changed = true
			}
		}
		if !changed {
			return nil
		}
	}
	s, err := normalizeTag(existing)
	if err != nil {
		return err
	}
	_, err = a.store.UpdateProject(ctx, projectID, store.ProjectPatch{Tag: &s})
	return err
}

// catOf 返回属于项目的分类。YApi 不检查分类是否存在、是否属于该项目；这里检查，避免接口挂到别的项目的分类下。
func (a *API) catOf(ctx context.Context, projectID, catID int64) (*store.Cat, error) {
	cat, err := a.store.CatByID(ctx, catID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && cat.ProjectID != projectID) {
		return nil, nil
	}
	return cat, err
}

// ---- 新建、保存、修改 ----

func (a *API) interfaceAdd(c *call) error {
	return c.send(a.addInterface(c.r.Context(), c.user, c.params))
}

func (a *API) addInterface(ctx context.Context, u *store.User, p map[string]any) (outcome, error) {
	projectID, _ := toInt(p["project_id"])
	if ok, err := a.canWrite(ctx, u, projectID, actionEdit); err != nil || !ok {
		return failure(40033, "没有权限"), err
	}
	method, _ := p["method"].(string)
	if method == "" {
		method = "GET"
	}
	p["method"] = strings.ToUpper(method)
	resType, _ := p["res_body_type"].(string)
	if resType == "" {
		resType = "json"
	}
	p["res_body_type"] = strings.ToLower(resType)
	path, _ := p["path"].(string)
	pathname, query := splitPath(path)
	if !interfacePathPattern.MatchString(pathname) {
		return failure(400, badPathMsg), nil
	}
	handleHeaders(p)

	taken, err := a.store.InterfacesByRoute(ctx, projectID, path, p["method"].(string))
	if err != nil {
		return outcome{}, err
	}
	if len(taken) > 0 {
		return failure(40022, "已存在的接口:"+path+"["+p["method"].(string)+"]"), nil
	}
	catID, _ := toInt(p["catid"])
	cat, err := a.catOf(ctx, projectID, catID)
	if err != nil || cat == nil {
		return failure(400, "不存在的分类"), err
	}

	now := store.Now()
	it := &store.Interface{UID: u.ID, ProjectID: projectID, Status: "undone", Type: "static", ReqQuery: "[]",
		ReqHeaders: "[]", ReqParams: "[]", ReqBodyForm: "[]", Tag: "[]", AddTime: now, UpTime: now}
	if it.QueryPath, err = queryPathJSON(pathname, query); err != nil {
		return outcome{}, err
	}
	p["req_params"] = addPathParams(path, arrayOr(p["req_params"]))
	if len(p["req_params"].([]any)) > 0 {
		it.Type = "var"
	}
	if err := applyInterfaceParams(it, p); err != nil {
		return outcome{}, err
	}

	// 新建接口的人如果还不是项目成员，自动成为开发者（旧格式 token 的 system 用户除外）
	if u.Role != "admin" && u.ID != legacyTokenUID {
		if _, err := a.store.ProjectMemberOf(ctx, projectID, u.ID); errors.Is(err, store.ErrNotFound) {
			m := store.Member{UID: u.ID, Role: "dev", Username: &u.Username, Email: &u.Email}
			if _, err := a.store.AddProjectMembers(ctx, projectID, []store.Member{m}); err != nil {
				return outcome{}, err
			}
		} else if err != nil {
			return outcome{}, err
		}
	}
	if err := a.store.CreateInterface(ctx, it); err != nil {
		return outcome{}, err
	}
	a.interfaceLog(ctx, u, projectID, fmt.Sprintf(
		`为分类 <a href="/project/%d/interface/api/cat_%d">%s</a> 添加了接口 <a href="/project/%d/interface/api/%d">%s</a> `,
		projectID, cat.ID, html.EscapeString(cat.Name), projectID, it.ID, html.EscapeString(it.Title)), nil)
	a.touchProject(ctx, projectID)
	if err := a.autoAddTag(ctx, projectID, p); err != nil {
		return outcome{}, err
	}
	return success(interfaceDoc(it)), nil
}

// interfaceSave 按项目、路径和方法查找接口：存在就修改，不存在就新建。
// 与 YApi 一样，响应只是查找结果（[{_id, res_body}]），不反映修改或新建是否成功。
func (a *API) interfaceSave(c *call) error {
	ctx := c.r.Context()
	p := c.params
	projectID, _ := toInt(p["project_id"])
	if ok, err := a.canWrite(ctx, c.user, projectID, actionEdit); err != nil {
		return err
	} else if !ok {
		return c.fail(40033, "没有权限")
	}
	method, _ := p["method"].(string)
	if method == "" {
		method = "GET"
	}
	p["method"] = strings.ToUpper(method)
	path, _ := p["path"].(string)
	if pathname, _ := splitPath(path); !interfacePathPattern.MatchString(pathname) {
		return c.fail(400, badPathMsg)
	}

	found, err := a.store.InterfacesByRoute(ctx, projectID, path, p["method"].(string))
	if err != nil {
		return err
	}
	result := make([]map[string]any, 0, len(found))
	for _, it := range found {
		item := map[string]any{"_id": it.ID}
		setOptional(item, "res_body", it.ResBody)
		result = append(result, item)
	}

	if len(found) == 0 {
		params, msg := validate(p, interfaceSchemas.add)
		if msg != "" {
			return c.fail(400, msg)
		}
		if _, err := a.addInterface(ctx, c.user, params); err != nil {
			return err
		}
		return c.ok(result)
	}
	for _, it := range found {
		withID := make(map[string]any, len(p)+1)
		for k, v := range p {
			withID[k] = v
		}
		withID["id"] = it.ID
		params, msg := validate(withID, interfaceSchemas.up)
		if msg != "" {
			return c.fail(400, msg)
		}
		if p["dataSync"] == "good" && params["res_body_is_json_schema"] != nil {
			if isSchema, _ := castBool(params["res_body_is_json_schema"]); isSchema {
				params["res_body"] = mergeResBodySchema(it.ResBody, params["res_body"])
			}
		}
		if _, err := a.updateInterface(ctx, c.user, params); err != nil {
			return err
		}
	}
	return c.ok(result)
}

func (a *API) interfaceUp(c *call) error {
	return c.send(a.updateInterface(c.r.Context(), c.user, c.params))
}

func (a *API) updateInterface(ctx context.Context, u *store.User, p map[string]any) (outcome, error) {
	if method, ok := p["method"].(string); ok {
		p["method"] = strings.ToUpper(method)
	}
	id, _ := toInt(p["id"])
	message, _ := p["message"].(string)
	message = strings.ReplaceAll(html.EscapeString(message), "\n", "<br>")
	handleHeaders(p)

	old, err := a.store.InterfaceByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return failure(400, "不存在的接口"), nil
	}
	if err != nil {
		return outcome{}, err
	}
	if ok, err := a.canWrite(ctx, u, old.ProjectID, actionEdit); err != nil || !ok {
		return failure(400, "没有权限"), err
	}

	cur := *old
	path, hasPath := p["path"].(string)
	if hasPath {
		pathname, query := splitPath(path)
		if !interfacePathPattern.MatchString(pathname) {
			return failure(400, badPathMsg), nil
		}
		if cur.QueryPath, err = queryPathJSON(pathname, query); err != nil {
			return outcome{}, err
		}
		// 与 YApi 一样只在路径或方法变化时查重；没传 method 时 YApi 按任意方法查重，提示里写 undefined
		method, hasMethod := p["method"].(string)
		if path != old.Path || method != old.Method {
			taken, err := a.store.InterfacesByRoute(ctx, old.ProjectID, path, method)
			if err != nil {
				return outcome{}, err
			}
			if len(taken) > 0 {
				if !hasMethod {
					method = "undefined"
				}
				return failure(401, "已存在的接口:"+path+"["+method+"]"), nil
			}
		}
	}
	catID, hasCat := toInt(p["catid"])
	if hasCat {
		if cat, err := a.catOf(ctx, old.ProjectID, catID); err != nil || cat == nil {
			return failure(400, "不存在的分类"), err
		}
	}
	if items, ok := p["req_params"].([]any); ok {
		cur.Type = "static"
		if len(items) > 0 {
			cur.Type = "var"
		}
	}
	if err := applyInterfaceParams(&cur, p); err != nil {
		return outcome{}, err
	}
	cur.UpTime = store.Now()
	modified := !sameInterface(old, &cur)
	if err := a.store.UpdateInterface(ctx, &cur); err != nil {
		return outcome{}, err
	}

	// YApi 用接口原来所在的分类写动态，链接里的分类 ID 却取自参数（没传时是 undefined）
	if interfaceChanged(old, &cur) {
		if cat, err := a.store.CatByID(ctx, old.CatID); err == nil {
			catRef := "undefined"
			data := map[string]any{"interface_id": id, "current": interfaceDoc(&cur), "old": interfaceDoc(old)}
			if hasCat {
				catRef = strconv.FormatInt(catID, 10)
				data["cat_id"] = catID
			}
			a.interfaceLog(ctx, u, cat.ProjectID, fmt.Sprintf("\n                    更新了分类 "+
				`<a href="/project/%d/interface/api/cat_%s">%s</a> `+"\n                    下的接口 "+
				`<a href="/project/%d/interface/api/%d">%s</a><p>%s</p>`,
				cat.ProjectID, catRef, html.EscapeString(cat.Name), cat.ProjectID, id, html.EscapeString(old.Title), message), data)
		} else if !errors.Is(err, store.ErrNotFound) {
			return outcome{}, err
		}
	}
	a.touchProject(ctx, old.ProjectID)
	if err := a.autoAddTag(ctx, old.ProjectID, p); err != nil {
		return outcome{}, err
	}
	n := int64(0)
	if modified {
		n = 1
	}
	return success(updateResult(1, n)), nil
}

// ---- 查询 ----

func (a *API) interfaceGet(c *call) error {
	ctx := c.r.Context()
	id, ok := c.int("id")
	if !ok || id == 0 {
		return c.fail(400, "接口id不能为空")
	}
	it, err := a.store.InterfaceByID(ctx, id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	// 与 YApi 的顺序一致：先核对 token 所属项目（接口不存在时原版在这里抛异常），再判断接口是否存在
	if scope, ok := tokenScope(ctx); ok {
		if it == nil {
			return c.fail(402, "Cannot read property 'project_id' of null")
		}
		if it.ProjectID != scope {
			return c.fail(400, "token有误")
		}
	}
	if it == nil {
		return c.fail(490, "不存在的")
	}
	if denied, err := a.privateDenied(ctx, c.user, it.ProjectID, actionView); err != nil || denied {
		return c.sendDenied(err)
	}
	doc := interfaceDoc(it)
	if owner, err := a.store.UserByID(ctx, it.UID); err == nil {
		doc["username"] = owner.Username
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return c.ok(doc)
}

// privateDenied 判断私有项目是否拒绝当前用户执行 action；项目不存在时不拒绝（交给调用方处理）。
func (a *API) privateDenied(ctx context.Context, u *store.User, projectID int64, action string) (bool, error) {
	p, err := a.store.ProjectByID(ctx, projectID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil || p.ProjectType != "private" {
		return false, err
	}
	ok, err := a.canProject(ctx, u, projectID, action)
	return !ok, err
}

func (c *call) sendDenied(err error) error {
	if err != nil {
		return err
	}
	return c.fail(406, "没有权限")
}

// pageArgs 读取 page、limit。limit 为 all 或不是正整数时不分页，此时 total 与 YApi 一样为 null（NaN 或 Infinity）。
func (c *call) pageArgs() (offset, limit int64, all bool) {
	page, ok := c.int("page")
	if !ok || page < 1 {
		page = 1
	}
	raw, present := c.params["limit"]
	if !present {
		return (page - 1) * 10, 10, false
	}
	if raw == "all" {
		return 0, 0, true
	}
	limit, ok = toInt(raw)
	if !ok || limit <= 0 {
		return 0, 0, false
	}
	return (page - 1) * limit, limit, false
}

func pageResult(list []map[string]any, count, limit int64) map[string]any {
	var total any
	if limit > 0 {
		total = int64(math.Ceil(float64(count) / float64(limit)))
	}
	return map[string]any{"count": count, "total": total, "list": list}
}

// stringList 读取可以是单个值或数组的查询参数（YApi 的 status、tag 筛选）。
func stringList(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func (a *API) interfaceList(c *call) error {
	ctx := c.r.Context()
	projectID, ok := c.int("project_id")
	if !ok || projectID == 0 {
		return c.fail(400, "项目id不能为空")
	}
	if _, err := a.store.ProjectByID(ctx, projectID); errors.Is(err, store.ErrNotFound) {
		return c.fail(407, "不存在的项目")
	} else if err != nil {
		return err
	}
	if denied, err := a.privateDenied(ctx, c.user, projectID, actionView); err != nil || denied {
		return c.sendDenied(err)
	}

	offset, limit, all := c.pageArgs()
	filter := store.InterfaceFilter{ProjectID: projectID}
	fields := interfaceAllFields
	if !all {
		filter.Status, filter.Tag = stringList(c.params["status"]), stringList(c.params["tag"])
		fields = interfacePageFields
	}
	list, err := a.store.ListInterfaces(ctx, filter, all, offset, limit)
	if err != nil {
		return err
	}
	count, err := a.store.CountInterfaces(ctx, filter)
	if err != nil {
		return err
	}
	return c.ok(pageResult(interfaceDocs(list, fields), count, limit))
}

func (a *API) interfaceListCat(c *call) error {
	ctx := c.r.Context()
	catID, ok := c.int("catid")
	if !ok || catID == 0 {
		return c.fail(400, "catid不能为空")
	}
	cat, err := a.store.CatByID(ctx, catID)
	if errors.Is(err, store.ErrNotFound) {
		return c.fail(402, "Cannot read property 'project_id' of null1") // YApi 把异常信息加上 "1" 返回
	}
	if err != nil {
		return err
	}
	if denied, err := a.privateDenied(ctx, c.user, cat.ProjectID, actionView); err != nil || denied {
		return c.sendDenied(err)
	}
	offset, limit, _ := c.pageArgs()
	filter := store.InterfaceFilter{CatID: catID, Status: stringList(c.params["status"]), Tag: stringList(c.params["tag"])}
	list, err := a.store.ListInterfaces(ctx, filter, false, offset, limit)
	if err != nil {
		return err
	}
	count, err := a.store.CountInterfaces(ctx, filter)
	if err != nil {
		return err
	}
	return c.ok(pageResult(interfaceDocs(list, interfacePageFields), count, limit))
}

func (a *API) interfaceListMenu(c *call) error {
	ctx := c.r.Context()
	projectID, ok := c.int("project_id")
	if !ok || projectID == 0 {
		return c.fail(400, "项目id不能为空")
	}
	if _, err := a.store.ProjectByID(ctx, projectID); errors.Is(err, store.ErrNotFound) {
		return c.fail(406, "不存在的项目")
	} else if err != nil {
		return err
	}
	if denied, err := a.privateDenied(ctx, c.user, projectID, actionView); err != nil || denied {
		return c.sendDenied(err)
	}
	cats, err := a.store.CatsByProject(ctx, projectID)
	if err != nil {
		return err
	}
	all, err := a.store.InterfacesByProject(ctx, projectID)
	if err != nil {
		return err
	}
	byCat := map[int64][]*store.Interface{}
	for _, it := range all {
		byCat[it.CatID] = append(byCat[it.CatID], it)
	}
	out := make([]map[string]any, 0, len(cats))
	for _, cat := range cats {
		doc := catDoc(cat)
		doc["list"] = interfaceDocs(byCat[cat.ID], interfaceMenuFields)
		out = append(out, doc)
	}
	return c.ok(out)
}

// ---- 删除 ----

func (a *API) interfaceDel(c *call) error {
	ctx := c.r.Context()
	id, ok := c.int("id")
	if !ok || id == 0 {
		return c.fail(400, "接口id不能为空")
	}
	it, err := a.store.InterfaceByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return c.fail(402, "Cannot read property 'uid' of null")
	}
	if err != nil {
		return err
	}
	// YApi 允许创建者直接删除自己的接口；这里还要求创建者仍有编辑权限（例如已被移出项目时不能再删）
	action := actionDanger
	if it.UID == c.user.ID {
		action = actionEdit
	}
	if ok, err := a.canProject(ctx, c.user, it.ProjectID, action); err != nil {
		return err
	} else if !ok {
		return c.fail(400, "没有权限")
	}
	n, err := a.store.DeleteInterface(ctx, id)
	if err != nil {
		return err
	}
	if cat, err := a.store.CatByID(ctx, it.CatID); err == nil {
		a.interfaceLog(ctx, c.user, cat.ProjectID, fmt.Sprintf(
			`删除了分类 <a href="/project/%d/interface/api/cat_%d">%s</a> 下的接口 "%s"`,
			cat.ProjectID, it.CatID, html.EscapeString(cat.Name), html.EscapeString(it.Title)), nil)
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	a.touchProject(ctx, it.ProjectID)
	return c.ok(removeResult(n))
}

// ---- 分类 ----

// interfaceAddCat 新建分类。与 YApi 一样只读请求体，所以通过 token 访问时也必须在请求体里给出 project_id。
func (a *API) interfaceAddCat(c *call) error {
	ctx := c.r.Context()
	c = c.bodyOnly()
	projectID, _ := c.int("project_id")
	if projectID == 0 {
		return c.fail(400, "项目id不能为空")
	}
	if ok, err := a.canWrite(ctx, c.user, projectID, actionEdit); err != nil {
		return err
	} else if !ok {
		return c.fail(400, "没有权限")
	}
	name := c.trimmed("name")
	if name == "" {
		return c.fail(400, "名称不能为空")
	}
	var desc *string
	if _, ok := c.str("desc"); ok {
		d := c.trimmed("desc")
		desc = &d
	}
	now := store.Now()
	cat := &store.Cat{UID: c.user.ID, ProjectID: projectID, Name: name, Desc: desc, AddTime: now, UpTime: now}
	if err := a.store.CreateCat(ctx, cat); err != nil {
		return err
	}
	a.interfaceLog(ctx, c.user, projectID, fmt.Sprintf(`添加了分类  <a href="/project/%d/interface/api/cat_%d">%s</a>`,
		projectID, cat.ID, html.EscapeString(name)), nil)
	return c.ok(catDoc(cat))
}

func (a *API) interfaceUpCat(c *call) error {
	ctx := c.r.Context()
	catID, _ := c.int("catid")
	cat, err := a.store.CatByID(ctx, catID)
	if errors.Is(err, store.ErrNotFound) {
		return c.fail(400, "Cannot read property 'project_id' of null")
	}
	if err != nil {
		return err
	}
	if ok, err := a.canProject(ctx, c.user, cat.ProjectID, actionEdit); err != nil {
		return err
	} else if !ok {
		return c.fail(400, "没有权限")
	}
	name, desc := c.optionalStr("name"), c.optionalStr("desc")
	modified := (name != nil && *name != cat.Name) || (desc != nil && !samePtr(desc, cat.Desc))
	n, err := a.store.UpdateCat(ctx, catID, name, desc)
	if err != nil {
		return err
	}
	a.interfaceLog(ctx, c.user, cat.ProjectID, fmt.Sprintf(`更新了分类 <a href="/project/%d/interface/api/cat_%d">%s</a>`,
		cat.ProjectID, catID, html.EscapeString(cat.Name)), nil)
	if !modified {
		return c.ok(updateResult(n, 0))
	}
	return c.ok(updateResult(n, n))
}

func (a *API) interfaceDelCat(c *call) error {
	ctx := c.r.Context()
	catID, _ := c.int("catid")
	cat, err := a.store.CatByID(ctx, catID)
	if errors.Is(err, store.ErrNotFound) {
		return c.fail(400, "不存在的分类")
	}
	if err != nil {
		return err
	}
	// 与删除接口相同：创建者也需要仍有编辑权限
	action := actionDanger
	if cat.UID == c.user.ID {
		action = actionEdit
	}
	if ok, err := a.canProject(ctx, c.user, cat.ProjectID, action); err != nil {
		return err
	} else if !ok {
		return c.fail(400, "没有权限")
	}
	a.interfaceLog(ctx, c.user, cat.ProjectID, fmt.Sprintf(`删除了分类 "%s" 及该分类下的接口`, html.EscapeString(cat.Name)), nil)
	n, err := a.store.DeleteCat(ctx, catID)
	if err != nil {
		return err
	}
	return c.ok(removeResult(n))
}

// interfaceCatMenu 返回项目的分类列表。与 YApi 一样，私有项目要求编辑权限（访客看不到）。
func (a *API) interfaceCatMenu(c *call) error {
	ctx := c.r.Context()
	projectID, ok := c.int("project_id")
	if !ok || projectID == 0 {
		return c.fail(400, "项目id不能为空")
	}
	if _, err := a.store.ProjectByID(ctx, projectID); errors.Is(err, store.ErrNotFound) {
		// YApi 在这里抛出的异常没有被转换为响应，Koa 返回 404
		c.replied = true
		http.NotFound(c.w, c.r)
		return nil
	} else if err != nil {
		return err
	}
	if denied, err := a.privateDenied(ctx, c.user, projectID, actionEdit); err != nil || denied {
		return c.sendDenied(err)
	}
	cats, err := a.store.CatsByProject(ctx, projectID)
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(cats))
	for _, cat := range cats {
		out = append(out, catDoc(cat))
	}
	return c.ok(out)
}

// ---- 排序 ----

// positionItems 读取 [{id, index}] 形式的请求体。请求体不是数组时与 YApi 一样报错。
func (c *call) positionItems() ([][2]int64, bool) {
	items, ok := c.body.([]any)
	if !ok {
		return nil, false
	}
	var out [][2]int64
	for _, item := range items {
		m, _ := item.(map[string]any)
		id, okID := toInt(m["id"])
		index, okIndex := toInt(m["index"])
		if okID && okIndex && id != 0 {
			out = append(out, [2]int64{id, index})
		}
	}
	return out, true
}

// interfaceUpIndex 调整接口顺序。YApi 不检查权限；这里只修改当前用户有编辑权限的项目中的接口，其余忽略。
func (a *API) interfaceUpIndex(c *call) error {
	ctx := c.r.Context()
	items, ok := c.positionItems()
	if !ok {
		return c.fail(400, "params.forEach is not a function")
	}
	for _, item := range items {
		it, err := a.store.InterfaceByID(ctx, item[0])
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if ok, err := a.canProject(ctx, c.user, it.ProjectID, actionEdit); err != nil {
			return err
		} else if ok {
			if _, err := a.store.SetInterfacePosition(ctx, item[0], item[1]); err != nil {
				return err
			}
		}
	}
	return c.ok("成功！")
}

// interfaceUpCatIndex 调整分类顺序，权限处理同 interfaceUpIndex。
func (a *API) interfaceUpCatIndex(c *call) error {
	ctx := c.r.Context()
	items, ok := c.positionItems()
	if !ok {
		return c.fail(400, "params.forEach is not a function")
	}
	for _, item := range items {
		cat, err := a.store.CatByID(ctx, item[0])
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if ok, err := a.canProject(ctx, c.user, cat.ProjectID, actionEdit); err != nil {
			return err
		} else if ok {
			if _, err := a.store.SetCatPosition(ctx, item[0], item[1]); err != nil {
				return err
			}
		}
	}
	return c.ok("成功！")
}
