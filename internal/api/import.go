package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/PandaPanY/apiyard/internal/jsjson"
	"github.com/PandaPanY/apiyard/internal/openapi"
	"github.com/PandaPanY/apiyard/internal/store"
)

func (a *API) registerImport(mux *http.ServeMux) {
	str := func(n string) field { return field{name: n, kind: "string"} }
	a.handle(mux, "POST /api/open/import_data", a.openImportData, schema(
		field{name: "type", kind: "string", required: true}, str("url"), field{name: "token", kind: "string", required: true},
		str("json"), str("project_id"), str("merge")))
}

// openImportData 是开放 API 的数据导入：把 Swagger 2 / OpenAPI 3 文档转换后逐个写入 token 所属的项目。
// merge 为 normal 时只新建（已存在的接口跳过），good 时按路径更新并合并 JSON Schema，merge 时按路径完全覆盖。
//
// 与 YApi 不同：
//   - 原版 1.12 的服务端转换依赖浏览器才有的 File 对象，在 Node 里加载就出错，这个接口实际上总是返回 40011；ApiYard 按代码本意实现。
//   - 不支持通过 url 参数（或以 http 开头的 json）导入：服务端代取任意 URL 有 SSRF 风险。
//   - token 无效时直接返回 40011；原版会用这个 token 去调用其他接口，再逐个失败。
func (a *API) openImportData(c *call) error {
	ctx := c.r.Context()
	if c.params["type"] != "swagger" { // 服务端只注册了 swagger 一种导入方式
		return c.fail(40022, "不存在的导入方式")
	}
	content, _ := c.str("json")
	url, _ := c.str("url")
	if content == "" && url == "" {
		return c.fail(40022, "json 或者 url 参数，不能都为空")
	}
	if url != "" || strings.HasPrefix(content, "http://") || strings.HasPrefix(content, "https://") {
		return c.fail(40022, "ApiYard 不支持通过 URL 导入，请把文档内容放在 json 参数里")
	}
	var probe any
	if err := json.Unmarshal([]byte(content), &probe); err != nil {
		return c.fail(40022, "json 格式有误:SyntaxError: "+err.Error())
	}
	projectID, ok := tokenScope(ctx)
	if !ok {
		return c.fail(40011, "请登录...")
	}

	menu, err := a.store.CatsByProject(ctx, projectID)
	if err != nil {
		return err
	}
	if len(menu) == 0 { // 分类都被删掉时先建一个默认分类
		now := store.Now()
		desc := "默认分类"
		cat := &store.Cat{UID: c.user.ID, ProjectID: projectID, Name: "默认分类", Desc: &desc, AddTime: now, UpTime: now}
		if err := a.store.CreateCat(ctx, cat); err != nil {
			return err
		}
		menu = append(menu, cat)
	}
	data, err := openapi.Convert(content)
	if err != nil {
		return c.fail(40011, "服务器出错...") // 原版转换失败时同样抛异常
	}
	mode, _ := c.str("merge")
	if mode == "" {
		mode = "normal"
	}
	message, errs, err := a.importInterfaces(ctx, c.user, projectID, data, menu, mode)
	if err != nil {
		return err
	}
	if len(errs) > 0 {
		return c.fail(404, strings.Join(errs, "\n"))
	}
	return c.reply(map[string]any{"errcode": 0, "errmsg": message, "data": nil})
}

// importInterfaces 对应 YApi 的 common/HandleImportData.js：先按名称复用或新建分类，有 basePath 时更新项目，
// 再逐个新建（normal）或保存（其他模式）接口。返回成功提示和错误信息。
func (a *API) importInterfaces(ctx context.Context, u *store.User, projectID int64, data *openapi.Result,
	menu []*store.Cat, mode string) (string, []string, error) {
	catIDs := map[string]int64{}
	for _, cat := range data.Cats {
		var id int64
		for _, m := range menu {
			if m.Name == cat.Name {
				id = m.ID
				break
			}
		}
		if id == 0 {
			params := map[string]any{"name": cat.Name, "project_id": projectID}
			if cat.Desc != nil {
				params["desc"] = cat.Desc
			}
			r, err := a.selfCall(ctx, u, a.interfaceAddCat, nil, params)
			if err != nil {
				return "", nil, err
			}
			if code, msg := resultCode(r); code != 0 {
				return "", []string{msg}, nil
			}
			id, _ = toInt(r["data"].(map[string]any)["_id"])
		}
		catIDs[cat.Name] = id
	}
	if len(data.APIs) == 0 {
		return "", []string{"解析数据为空"}, nil
	}
	if data.BasePath != "" { // 结果被忽略：没有权限修改项目时什么也不发生
		if _, err := a.selfCall(ctx, u, a.projectUp, nil, map[string]any{"id": projectID, "basepath": data.BasePath}); err != nil {
			return "", nil, err
		}
	}

	success, exist := len(data.APIs), 0
	var errs []string
	for _, api := range data.APIs {
		params, _ := plainJSON(api).(map[string]any)
		params["project_id"] = projectID
		params["catid"] = menu[0].ID
		if name, ok := params["catname"].(string); ok && catIDs[name] != 0 {
			params["catid"] = catIDs[name]
		}
		if mode != "normal" {
			params["dataSync"] = mode
			r, err := a.selfCall(ctx, u, a.interfaceSave, interfaceSchemas.save, params)
			if err != nil {
				return "", nil, err
			}
			if code, msg := resultCode(r); code != 0 {
				success--
				errs = append(errs, msg)
			} else if list, ok := r["data"].([]any); ok {
				exist += len(list)
			}
			continue
		}
		r, err := a.selfCall(ctx, u, a.interfaceAdd, interfaceSchemas.add, params)
		if err != nil {
			return "", nil, err
		}
		switch code, _ := resultCode(r); code {
		case 0:
		case 40022:
			success--
			exist++
		case 40033:
			return "", append(errs, "没有权限"), nil
		default:
			success--
		}
	}
	return fmt.Sprintf("成功导入接口 %d 个, 已存在的接口 %d 个", success, exist), errs, nil
}

// selfCall 以当前用户的身份在进程内调用另一个接口，对应 YApi 导入时带着 token 向自己发 HTTP 请求的做法：
// 同样先做参数校验，同样受 token 所属项目的限制（ctx 中带有 token 的范围）。返回解析后的响应。
func (a *API) selfCall(ctx context.Context, u *store.User, h handlerFunc, fields []field, params map[string]any) (map[string]any, error) {
	body := params
	if fields != nil {
		validated, msg := validate(params, fields)
		if msg != "" {
			return map[string]any{"errcode": json.Number("400"), "errmsg": msg}, nil
		}
		params = validated
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/", nil)
	c := &call{api: a, w: rec, r: req, params: params, body: body, user: u}
	if err := h(c); err != nil {
		return nil, err
	}
	var out map[string]any
	dec := json.NewDecoder(rec.Body)
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func resultCode(r map[string]any) (int64, string) {
	code, _ := toInt(r["errcode"])
	msg, _ := r["errmsg"].(string)
	return code, msg
}

// plainJSON 把 jsjson 的有序对象转换成普通的 map，供参数校验和接口处理使用。
func plainJSON(v any) any {
	switch x := v.(type) {
	case *jsjson.Object:
		m := make(map[string]any, x.Len())
		for _, k := range x.Keys() {
			val, _ := x.Get(k)
			m[k] = plainJSON(val)
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = plainJSON(item)
		}
		return out
	default:
		return v
	}
}
