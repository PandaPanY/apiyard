// Package mock 实现 YApi 的 Mock 服务：/mock/{项目 ID}{项目基础路径}{接口路径}，对应 YApi 的 server/middleware/mockServer.js。
// 地址、接口的匹配规则、返回的数据和出错时的响应都与原版相同。
//
// 与原版刻意不同的地方（安全原因，见 docs/architecture.md 的 D5）：
//   - 不执行项目的全局 Mock 脚本（原版用 vm 沙箱执行，有未修复的远程代码执行漏洞），响应头 X-ApiYard-Mock-Script 会提示脚本被忽略；
//   - 不支持高级 Mock（期望和接口级脚本），这部分属于延后的插件功能；
//   - Mock.js 占位符的参数不用 eval 求值，见 internal/mockjs。
package mock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/PandaPanY/apiyard/internal/genbudget"
	"github.com/PandaPanY/apiyard/internal/jsjson"
	"github.com/PandaPanY/apiyard/internal/mockjs"
	"github.com/PandaPanY/apiyard/internal/schemafake"
	"github.com/PandaPanY/apiyard/internal/store"
)

// Handler 处理 /mock/ 下的全部请求。
type Handler struct {
	store  *store.Store
	logger *slog.Logger
}

// New 创建 Mock 服务。
func New(st *store.Store, logger *slog.Logger) *Handler {
	return &Handler{store: st, logger: logger}
}

// errResponse 是原版 resReturn 的格式；HTTP 状态码始终是 200。
type errResponse struct {
	code int
	msg  string
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	release, ok := genbudget.Acquire(r.Context())
	if !ok {
		http.Error(w, "Mock generation busy", http.StatusTooManyRequests)
		return
	}
	defer release()
	// Mock 返回的是项目成员写的任意内容，又与界面同源：直接在浏览器里打开时，sandbox 把页面隔离成独立的源，
	// 里面的脚本拿不到界面的登录状态（原版没有这层保护）。fetch、XHR 等调用不受影响
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// 与原版一样允许任意来源带 Cookie 跨域访问（Mock 本来就是给前端开发用的）。原版在没有 Origin 时会输出字符串 "undefined"，这里不输出
	if origin := r.Header.Get("Origin"); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	w.Header().Set("Access-Control-Allow-Credentials", "true")

	res, e := h.serve(w, r)
	if e != nil {
		writeJSON(w, jsjson.NewObject("errcode", json.Number(strconv.Itoa(e.code)), "errmsg", e.msg, "data", nil))
		return
	}
	switch {
	case res == nil: // 已经回复过（请求体格式不对）
	case res.preflight:
		// 找不到接口的跨域预检请求（原版的 handleCorsRequest）
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, HEADER, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", r.Header.Get("Access-Control-Request-Headers"))
		w.Header().Set("Access-Control-Max-Age", "1728000")
		writeBody(w, "ok")
	default:
		writeBody(w, res.value)
	}
}

// result 是要返回的 Mock 数据；value 为 nil 表示 undefined 或 null（Koa 返回 204）。
type result struct {
	value     any
	preflight bool
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) (*result, *errResponse) {
	ctx := r.Context()
	// Koa 的 ctx.path 是没有解码的路径
	segs := strings.Split(r.URL.EscapedPath(), "/")
	projectID := ""
	if len(segs) > 2 {
		projectID = segs[2]
	}
	path := "/"
	if len(segs) > 3 {
		path += strings.Join(segs[3:], "/")
	}
	if projectID == "" {
		return nil, &errResponse{400, "projectId不能为空"}
	}

	// Mongoose 把 ID 转成数字查询：转不了时报 CastError，转成小数等查不到的值时视为不存在
	id := jsNumber(projectID)
	if math.IsNaN(id) {
		return nil, &errResponse{403, fmt.Sprintf(`Cast to number failed for value "%s" at path "_id" for model "project"`, projectID)}
	}
	var project *store.Project
	var err error
	if id == math.Trunc(id) && math.Abs(id) < 1<<53 {
		project, err = h.store.ProjectByID(ctx, int64(id))
	} else {
		err = store.ErrNotFound
	}
	if errors.Is(err, store.ErrNotFound) {
		// 原版的 projectModel.get 会对查询结果调用 toObject()，项目不存在时就抛出 TypeError，走不到“不存在的项目”那个分支
		return nil, &errResponse{403, "Cannot read property 'toObject' of null"}
	}
	if err != nil {
		return nil, h.internal(err)
	}
	if project.Basepath == nil {
		return nil, &errResponse{409, "Cannot read property 'length' of undefined"}
	}
	// 原版直接按基础路径的长度截掉开头，并不检查前缀是否相同
	newPath := path[min(len(path), len(utf16.Encode([]rune(*project.Basepath)))):]

	req, err := readRequest(r)
	if err != nil {
		var be *bodyError
		if errors.As(err, &be) {
			http.Error(w, be.msg, be.status)
			return nil, nil
		}
		return nil, h.internal(err)
	}

	it, preflight, e := h.findInterface(ctx, project.ID, newPath, r, req)
	if e != nil {
		return nil, e
	}
	if preflight {
		return &result{preflight: true}, nil
	}

	if project.Strice {
		ok, msg, err := validate(it, req)
		if err != nil {
			return nil, &errResponse{409, err.Error()}
		}
		if !ok {
			return nil, &errResponse{404, "接口字段验证不通过, " + msg}
		}
	}

	if project.IsMockOpen && project.ProjectMockScript != nil && *project.ProjectMockScript != "" {
		w.Header().Set("X-ApiYard-Mock-Script", "ignored")
	}
	value, e := h.mockDataContext(ctx, it, req)
	if e != nil {
		return nil, e
	}
	return &result{value: value}, nil
}

func (h *Handler) internal(err error) *errResponse {
	h.logger.Error("Mock 出错", "err", err)
	return &errResponse{409, err.Error()}
}

// findInterface 按原版的顺序查找接口：完全相同的路径 → query_path 匹配 → 路径参数模板。找不到时返回错误响应，
// 是跨域预检请求时 preflight 为 true。
func (h *Handler) findInterface(ctx context.Context, projectID int64, path string, r *http.Request, req *request) (it *store.Interface, preflight bool, e *errResponse) {
	method := r.Method
	found, err := h.store.InterfacesByRoute(ctx, projectID, path, method)
	if err != nil {
		return nil, false, h.internal(err)
	}
	byQuery, err := h.store.InterfacesByQueryPath(ctx, projectID, path, method)
	if err != nil {
		return nil, false, h.internal(err)
	}
	// 路径里写了查询参数的接口（如 /list?type=1）：按查询参数区分。原版的循环只看最后一个参数是否相同
	if len(found) == 0 || len(found) != len(byQuery) {
		for _, it := range byQuery {
			var qp struct {
				Path   string `json:"path"`
				Params []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"params"`
			}
			if json.Unmarshal([]byte(it.QueryPath), &qp) != nil || qp.Path == "" || len(qp.Params) == 0 {
				continue
			}
			last := qp.Params[len(qp.Params)-1]
			if v, _ := req.query.Get(last.Name); v == any(last.Value) {
				found = []*store.Interface{it}
				break
			}
		}
	}

	if len(found) == 0 {
		vars, err := h.store.VarInterfaces(ctx, projectID, method)
		if err != nil {
			return nil, false, h.internal(err)
		}
		var match *store.Interface
		for _, it := range vars {
			params, ok := matchAPI(path, it.Path)
			if !ok {
				continue
			}
			// 原版的权重比较写错了（weight 一直是 0），结果是最后一个匹配的接口胜出；每个匹配的路径参数都会并入查询参数
			match = it
			for _, k := range req.query.Keys() {
				v, _ := req.query.Get(k)
				params.Set(k, v)
			}
			req.query = params
		}
		if match == nil {
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				return nil, true, nil
			}
			return nil, false, &errResponse{404, fmt.Sprintf("不存在的api, 当前请求path为 %s， 请求方法为 %s ，请确认是否定义此请求。", path, method)}
		}
		found = []*store.Interface{match}
	}
	if len(found) > 1 {
		return nil, false, &errResponse{405, "存在多个api，请检查数据库"}
	}
	return found[0], false, nil
}

// mockData 生成 Mock 数据，对应原版“mock 返回值处理”一段。
func (h *Handler) mockDataContext(ctx context.Context, it *store.Interface, req *request) (any, *errResponse) {
	if it.ResBody == nil {
		return nil, nil
	}
	if it.ResBodyType == nil || *it.ResBodyType != "json" {
		return *it.ResBody, nil // 其他类型原样返回
	}
	var res any
	template := parseJSON5(*it.ResBody)
	if err := mockjs.ValidateContext(ctx, template); err != nil {
		return nil, &errResponse{400, err.Error()}
	}
	if it.ResBodyIsJSONSchema {
		res = schemaToJSONContext(ctx, template)
	} else {
		body := req.body
		if req.multipart { // 原版把 multipart 请求体换成其中的 fields
			body, _ = req.body.(*jsjson.Object).Get("fields")
		}
		vars := jsjson.NewObject("query", req.query, "body", body, "params", assignParams(req.query, body))
		var err error
		if res, err = mockjs.Extra(template, vars); err != nil {
			return nil, &errResponse{400, "解析出错，请检查。Error: " + err.Error()}
		}
	}
	// Mock.js 出错时原版只记日志，返回处理前的数据
	if v, err := mockjs.MockContext(ctx, res); err == nil {
		res = v
	} else {
		if errors.Is(err, genbudget.ErrLimit) || ctx.Err() != nil {
			return nil, &errResponse{400, err.Error()}
		}
		h.logger.Info("Mock.js 处理出错，返回原始数据", "interface", it.ID, "err", err)
	}
	return res, nil
}

// parseJSON5 对应原版的 json_parse：按 JSON5 解析，失败时返回原字符串。
func parseJSON5(s string) any {
	v, err := jsjson.ParseJSON5(s)
	if err != nil {
		return s
	}
	return v
}

// schemaToJSON 对应原版的 schemaToJson（json-schema-faker，非必填字段也生成）；出错时与原版一样返回错误信息。
func schemaToJSONContext(ctx context.Context, schema any) any {
	data, err := jsjson.Marshal(schema, "")
	if err != nil {
		return err.Error()
	}
	var plain any
	if err := json.Unmarshal(data, &plain); err != nil {
		return err.Error()
	}
	v, err := schemafake.GenerateContext(ctx, plain, schemafake.Options{AlwaysFakeOptionals: true})
	if err != nil {
		return err.Error()
	}
	if data, err = json.Marshal(v); err != nil {
		return err.Error()
	}
	out, err := jsjson.Parse(string(data))
	if err != nil {
		return err.Error()
	}
	return out
}

// assignParams 对应 Object.assign({}, query, body)：字符串请求体按下标展开成属性，与 JS 相同。
func assignParams(query *jsjson.Object, body any) *jsjson.Object {
	out := query.Clone()
	switch b := body.(type) {
	case *jsjson.Object:
		for _, k := range b.Keys() {
			v, _ := b.Get(k)
			out.Set(k, v)
		}
	case string:
		for i, c := range utf16.Encode([]rune(b)) {
			out.Set(strconv.Itoa(i), string(utf16.Decode([]uint16{c})))
		}
	}
	return out
}

// matchAPI 对应原版的 matchApi：按 / 分段比较，{name} 或 :name 整段匹配任意值，段内的 {name} 按正则匹配。
// 返回的路径参数保持出现顺序。
func matchAPI(path, rule string) (*jsjson.Object, bool) {
	rules, paths := strings.Split(rule, "/"), strings.Split(path, "/")
	params := jsjson.NewObject()
	if len(rules) != len(paths) {
		return nil, false
	}
	for i, seg := range rules {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		switch {
		case len(seg) > 2 && seg[0] == '{' && seg[len(seg)-1] == '}':
			params.Set(seg[1:len(seg)-1], paths[i])
		case strings.HasPrefix(seg, ":"):
			params.Set(seg[1:], paths[i])
		case len(seg) > 2 && strings.Contains(seg, "{") && strings.Contains(seg, "}"):
			var names []string
			pattern := reSegVar.ReplaceAllStringFunc(seg, func(m string) string {
				names = append(names, m[1:len(m)-1])
				return `([^\/\s]+)`
			})
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, false
			}
			m := re.FindStringSubmatch(paths[i])
			if m == nil {
				return nil, false
			}
			for j, name := range names {
				params.Set(name, m[j+1])
			}
		default:
			if seg != paths[i] {
				return nil, false
			}
		}
	}
	return params, true
}

var reSegVar = regexp.MustCompile(`\{(.+?)\}`)

// jsNumber 对应 JS 的 Number(s)。
func jsNumber(s string) float64 {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0
	}
	if f, err := strconv.ParseFloat(t, 64); err == nil && !strings.ContainsAny(t, "xXpP_") &&
		!strings.HasPrefix(strings.ToLower(strings.TrimLeft(t, "+-")), "inf") && !strings.EqualFold(t, "nan") {
		return f
	}
	switch t {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	if strings.HasPrefix(t, "0x") || strings.HasPrefix(t, "0X") {
		if n, err := strconv.ParseUint(t[2:], 16, 64); err == nil {
			return float64(n)
		}
	}
	return math.NaN()
}

// ---- 输出 ----

func writeJSON(w http.ResponseWriter, v any) {
	data, err := jsjson.Marshal(v, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(data) // #nosec G705 -- 响应带有 CSP sandbox，见 ServeHTTP
}

// writeBody 按 Koa 的规则输出：null 为 204，字符串为 text/plain（以 < 开头时为 text/html），其余输出 JSON。
func writeBody(w http.ResponseWriter, v any) {
	switch x := v.(type) {
	case nil:
		w.WriteHeader(http.StatusNoContent)
	case string:
		if reHTML.MatchString(x) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		} else {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		_, _ = w.Write([]byte(x)) // #nosec G705 -- 响应带有 CSP sandbox，见 ServeHTTP
	default:
		writeJSON(w, v)
	}
}

var reHTML = regexp.MustCompile(`^\s*<`)
