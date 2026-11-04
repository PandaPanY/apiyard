package api

import (
	"cmp"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/PandaPanY/apiyard/internal/jsjson"
	"github.com/PandaPanY/apiyard/internal/store"
)

func (a *API) registerExport(mux *http.ServeMux) {
	a.handle(mux, "GET /api/plugin/export", a.exportData)
	a.handle(mux, "GET /api/plugin/exportSwagger", a.exportSwagger)
}

// errExportFailed 对应 YApi 导出出错时的 502 "下载出错"（例如项目不存在、请求体不是合法 JSON）。
var errExportFailed = errors.New("下载出错")

// exportProject 读取要导出的项目和接口：分类按 index 排序，分类内的接口按 index、标题排序（YApi 先按标题查出来再按
// index 稳定排序），status 为 open 时只导出公开接口，没有接口的分类不导出。
//
// 与 YApi 不同：私有项目要求查看权限（原版导出接口不检查任何权限，任何登录用户都能导出任意项目）。
func (a *API) exportProject(c *call) (*store.Project, []exportCat, error) {
	ctx := c.r.Context()
	pid, _ := c.int("pid")
	p, err := a.store.ProjectByID(ctx, pid)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, errExportFailed
	}
	if err != nil {
		return nil, nil, err
	}
	if p.ProjectType == "private" {
		if ok, err := a.canProject(ctx, c.user, pid, actionView); err != nil {
			return nil, nil, err
		} else if !ok {
			return nil, nil, errNoPermission
		}
	}
	cats, err := a.store.CatsByProject(ctx, pid)
	if err != nil {
		return nil, nil, err
	}
	all, err := a.store.InterfacesByProject(ctx, pid)
	if err != nil {
		return nil, nil, err
	}
	onlyOpen := c.params["status"] == "open"
	byCat := map[int64][]*store.Interface{}
	for _, it := range all {
		if !onlyOpen || it.APIOpened {
			byCat[it.CatID] = append(byCat[it.CatID], it)
		}
	}
	var out []exportCat
	for _, cat := range cats {
		list := byCat[cat.ID]
		if len(list) == 0 {
			continue
		}
		slices.SortStableFunc(list, func(x, y *store.Interface) int {
			return cmp.Or(cmp.Compare(x.Position, y.Position), strings.Compare(x.Title, y.Title), cmp.Compare(x.ID, y.ID))
		})
		out = append(out, exportCat{cat: cat, list: list})
	}
	return p, out, nil
}

var errNoPermission = errors.New("没有权限")

// sendExportError 把导出过程中的错误转换为 YApi 的响应。
func (c *call) sendExportError(err error) error {
	switch {
	case errors.Is(err, errExportFailed):
		return c.fail(502, "下载出错")
	case errors.Is(err, errNoPermission):
		return c.fail(406, "没有权限")
	}
	return err
}

// sendDownload 以附件形式返回导出结果。
func (c *call) sendDownload(filename string, data []byte) error {
	c.replied = true
	c.w.Header().Set("Content-Type", "application/octet-stream")
	c.w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	_, err := c.w.Write(data)
	return err
}

// exportData 导出项目接口：type 为 json、markdown，其他值（包括不传）导出 HTML。
// isWiki 用于附带 Wiki 内容，ApiYard 暂不支持 Wiki，忽略该参数。
func (a *API) exportData(c *call) error {
	p, cats, err := a.exportProject(c)
	if err != nil {
		return c.sendExportError(err)
	}
	table := &schemaTable{arraySeq: &a.schemaArraySeq}
	switch c.params["type"] {
	case "json":
		data, err := encodeJSON(exportJSON(cats), "  ")
		if err != nil {
			return err
		}
		return c.sendDownload("api.json", data)
	case "markdown":
		md, err := table.projectMarkdown(p, cats)
		if err != nil {
			return c.sendExportError(errExportFailed)
		}
		return c.sendDownload("api.md", []byte(md))
	default:
		page, err := exportHTML(table, p, cats)
		if err != nil {
			return c.sendExportError(errExportFailed)
		}
		return c.sendDownload("api.html", []byte(page))
	}
}

// exportJSON 生成 JSON 导出内容。与 YApi 一致：分类去掉 _id、__v、uid、project_id；
// 接口本想去掉同样的字段，但原版删的是 Mongoose 文档对象上的属性，没有生效，所以接口保留完整字段。
func exportJSON(cats []exportCat) []map[string]any {
	out := make([]map[string]any, 0, len(cats))
	for _, c := range cats {
		doc := catDoc(c.cat)
		for _, k := range []string{"_id", "__v", "uid", "project_id"} {
			delete(doc, k)
		}
		list := make([]map[string]any, 0, len(c.list))
		for _, it := range c.list {
			list = append(list, interfaceDoc(it))
		}
		doc["list"] = list
		out = append(out, doc)
	}
	return out
}

// exportSwagger 导出 Swagger 2.0（YApi 的 type 只支持 OpenAPIV2）。
func (a *API) exportSwagger(c *call) error {
	p, cats, err := a.exportProject(c)
	if err != nil {
		return c.sendExportError(err)
	}
	if c.params["type"] != "OpenAPIV2" {
		return c.fail(400, "type 无效参数")
	}
	doc, err := swaggerDoc(p, cats)
	if err != nil {
		return c.sendExportError(errExportFailed)
	}
	data, err := encodeJSON(doc, "  ")
	if err != nil {
		return err
	}
	return c.sendDownload("swaggerApi.json", data)
}

// ---- Swagger 2.0 ----

// swaggerDoc 对应 YApi 的 convertToSwaggerV2Model。字段顺序与原版一致；请求体和响应体的 JSON 原样嵌入，保持键的顺序。
func swaggerDoc(p *store.Project, cats []exportCat) (*jsjson.Object, error) {
	info := jsjson.NewObject("title", p.Name, "version", "last")
	if p.Desc != nil {
		info.Set("description", *p.Desc)
	}
	basePath := "/"
	if p.Basepath != nil && *p.Basepath != "" {
		basePath = *p.Basepath
	}
	tags := make([]any, 0, len(cats))
	paths := jsjson.NewObject()
	for _, c := range cats {
		tag := jsjson.NewObject("name", c.cat.Name)
		if c.cat.Desc != nil {
			tag.Set("description", *c.cat.Desc)
		}
		tags = append(tags, tag)
		for _, it := range c.list {
			op, err := swaggerOperation(c.cat.Name, it)
			if err != nil {
				return nil, err
			}
			existing, _ := paths.Get(it.Path)
			methods, ok := existing.(*jsjson.Object)
			if !ok {
				methods = jsjson.NewObject()
				paths.Set(it.Path, methods)
			}
			methods.Set(strings.ToLower(it.Method), op)
		}
	}
	return jsjson.NewObject("swagger", "2.0", "info", info, "basePath", basePath, "tags", tags,
		"schemes", []any{"http"}, "paths", paths), nil
}

func swaggerOperation(tag string, it *store.Interface) (*jsjson.Object, error) {
	op := jsjson.NewObject("tags", []any{tag}, "summary", it.Title)
	if it.Markdown != nil {
		op.Set("description", *it.Markdown)
	}
	bodyType := deref(it.ReqBodyType)
	switch bodyType {
	case "form", "file":
		op.Set("consumes", []any{"multipart/form-data"})
	case "json":
		op.Set("consumes", []any{"application/json"})
	case "raw":
		op.Set("consumes", []any{"text/plain"})
	}

	params := []any{}
	for _, h := range paramItems(it.ReqHeaders) {
		if h["name"] == "Content-Type" { // Swagger 用 consumes 表达
			continue
		}
		p := jsjson.NewObject("name", h["name"], "in", "header",
			"description", templateValue(h["name"])+" (Only:"+templateValue(h["value"])+")",
			"required", h["required"] == "1", "type", "string")
		putIfPresent(p, "default", h["value"])
		params = append(params, p)
	}
	for _, q := range paramItems(it.ReqParams) {
		p := jsjson.NewObject("name", q["name"], "in", "path")
		putIfPresent(p, "description", q["desc"])
		p.Set("required", true)
		p.Set("type", "string")
		params = append(params, p)
	}
	for _, q := range paramItems(it.ReqQuery) {
		p := jsjson.NewObject("name", q["name"], "in", "query", "required", q["required"] == "1")
		putIfPresent(p, "description", q["desc"])
		p.Set("type", "string")
		params = append(params, p)
	}
	switch bodyType {
	case "form":
		for _, f := range paramItems(it.ReqBodyForm) {
			p := jsjson.NewObject("name", f["name"], "in", "formData", "required", f["required"] == "1")
			putIfPresent(p, "description", f["desc"])
			typ := "file"
			if f["type"] == "text" {
				typ = "string"
			}
			p.Set("type", typ)
			params = append(params, p)
		}
	case "json":
		if other := deref(it.ReqBodyOther); other != "" {
			schema, err := jsjson.Parse(other) // 原版用 JSON.parse，不是合法 JSON 时整个导出失败
			if err != nil {
				return nil, err
			}
			if truthy(schema) {
				p := jsjson.NewObject("name", "root", "in", "body")
				if desc := jsField(schema, "description"); !isUndefined(desc) {
					p.Set("description", desc)
				}
				p.Set("schema", schema)
				params = append(params, p)
			}
		}
	case "file":
		p := jsjson.NewObject("name", "upfile", "in", "formData")
		if it.ReqBodyOther != nil {
			p.Set("description", *it.ReqBodyOther)
		}
		p.Set("type", "file")
		params = append(params, p)
	case "raw":
		schema := jsjson.NewObject("type", "string", "format", "binary")
		if it.ReqBodyOther != nil {
			schema.Set("default", *it.ReqBodyOther)
		}
		// 描述里的拼写错误来自原版，周边工具可能按原文匹配，保持一致
		params = append(params, jsjson.NewObject("name", "raw", "in", "body", "description", "raw paramter", "schema", schema)) //nolint:misspell // 与原版一致
	}
	op.Set("parameters", params)

	var schema any = jsjson.NewObject()
	switch deref(it.ResBodyType) {
	case "raw":
		s := jsjson.NewObject("type", "string", "format", "binary")
		if it.ResBody != nil {
			s.Set("default", *it.ResBody)
		}
		schema = s
	case "json":
		if body := deref(it.ResBody); body != "" {
			parsed, err := jsjson.Parse(body)
			if err != nil {
				return nil, err
			}
			if parsed != nil {
				schema = parsed
			}
		}
	}
	op.Set("responses", jsjson.NewObject("200", jsjson.NewObject("description", "successful operation", "schema", schema)))
	return op, nil
}

// templateValue 对应模板字符串里的 `${x}`（参数项的值都是字符串或不存在）。
func templateValue(v any) string {
	if v == nil {
		return "undefined"
	}
	return jsjson.String(v)
}

func putIfPresent(o *jsjson.Object, key string, v any) {
	if v != nil {
		o.Set(key, v)
	}
}
