// Package api 实现与 YApi 路径、参数和响应格式一致的 /api 接口。
// 每个处理函数都对照 YApi 1.12.0 的 server/controllers 编写，刻意与原版不同的地方会在注释里说明。
package api

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/PandaPanY/apiyard/internal/store"
)

// Options 是接口层的配置。
type Options struct {
	CloseRegister bool   // 关闭注册（YApi 配置 closeRegister）
	TokenSalt     string // 开放 API token 的加密口令（YApi 配置 passsalt），为空时使用 YApi 的默认值
	WebFS         fs.FS  // 前端静态文件，用于读取默认头像；可以为 nil
}

// API 持有接口层的依赖。
type API struct {
	store  *store.Store
	opts   Options
	logger *slog.Logger
	now    func() time.Time
}

// New 创建接口层。
func New(s *store.Store, opts Options, logger *slog.Logger) *API {
	return &API{store: s, opts: opts, logger: logger, now: time.Now}
}

// Register 把已实现的接口挂到 mux 上。
func (a *API) Register(mux *http.ServeMux) {
	a.registerUser(mux)
	a.registerProfile(mux)
	a.registerGroup(mux)
	a.registerProject(mux)
	a.registerProjectMember(mux)
	a.registerFollow(mux)
	a.registerLog(mux)
}

// handlerFunc 处理一次调用；返回的错误统一转换为 YApi 的“服务器出错”响应。
type handlerFunc func(c *call) error

// route 描述接口的公共行为。
type route struct {
	public bool    // 不需要登录（对应 YApi base.js 的 ignoreRouter）
	schema []field // 参数校验（对应 YApi 控制器的 schemaMap）
}

type routeOption func(*route)

// public 标记接口不需要登录。
func public() routeOption { return func(r *route) { r.public = true } }

// schema 设置参数校验规则。
func schema(fields ...field) routeOption { return func(r *route) { r.schema = fields } }

// handle 注册一个接口，处理顺序与 YApi 的 createAction 一致：
// 解析登录状态 → 合并参数 → 参数校验（400）→ 登录检查（40011）→ 业务处理。
func (a *API) handle(mux *http.ServeMux, pattern string, h handlerFunc, opts ...routeOption) {
	var rt route
	for _, o := range opts {
		o(&rt)
	}
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		c := &call{api: a, w: w, r: r}
		if err := a.serve(c, rt, h); err != nil {
			a.logger.Error("接口处理出错", "path", r.URL.Path, "err", err)
			if !c.replied {
				_ = c.fail(40011, "服务器出错...")
			}
		}
	})
}

func (a *API) serve(c *call, rt route, h handlerFunc) error {
	user, err := a.currentUser(c.r)
	if err != nil {
		return err
	}
	c.user = user

	params, err := readParams(c.r)
	if err != nil {
		return c.fail(400, "请求体不是合法的 JSON")
	}
	if rt.schema != nil {
		var msg string
		if params, msg = validate(params, rt.schema); msg != "" {
			return c.fail(400, msg)
		}
	}
	c.params = params

	if !rt.public && c.user == nil {
		return c.fail(40011, "请登录...")
	}
	return h(c)
}

// call 是一次接口调用的上下文。
type call struct {
	api     *API
	w       http.ResponseWriter
	r       *http.Request
	params  map[string]any
	user    *store.User // 未登录时为 nil
	replied bool
}

// ok 返回成功响应，与 YApi 的 resReturn(data) 相同。
func (c *call) ok(data any) error {
	return c.reply(map[string]any{"errcode": 0, "errmsg": "成功！", "data": data})
}

// fail 返回错误响应，data 为 null。
func (c *call) fail(code int, msg string) error {
	return c.reply(map[string]any{"errcode": code, "errmsg": msg, "data": nil})
}

// reply 输出 JSON 响应。与 Koa 一致，不转义 <>&，保证动态内容里的 HTML 原样返回。
func (c *call) reply(body map[string]any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		return err
	}
	c.replied = true
	c.w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, err := c.w.Write(buf.Bytes())
	return err
}

// updateResult 模拟 MongoDB update 的返回值，YApi 直接把它作为 data 返回。
func updateResult(matched, modified int64) map[string]any {
	return map[string]any{"n": matched, "nModified": modified, "ok": 1}
}

// removeResult 模拟 MongoDB remove 的返回值。
func removeResult(deleted int64) map[string]any {
	return map[string]any{"deletedCount": deleted, "n": deleted, "ok": 1}
}

// marshalCompact 把参数里的对象序列化为 JSON 文本，用于存入 JSON 列。
func marshalCompact(v any) (string, error) {
	data, err := json.Marshal(v)
	return string(data), err
}

// rawJSON 把数据库里的 JSON 文本原样嵌入响应；nil 输出 null。
func rawJSON(s *string) any {
	if s == nil {
		return nil
	}
	return json.RawMessage(*s)
}
