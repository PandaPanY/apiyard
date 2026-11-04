package api

import (
	"errors"
	"io/fs"
	"net/http"
)

// registerUnsupported 注册 ApiYard 暂不提供、但前端可能调用的接口，让界面给出明确提示而不是报错。
// 另外 /api/project/download 在 YApi 1.12 中并没有实现（前端的“下载Mock数据”按钮是隐藏的），
// 原版返回 404，ApiYard 同样不注册它。
func (a *API) registerUnsupported(mux *http.ServeMux) {
	// YApi 在没有配置 LDAP 时返回 401 和一条异常信息，这里换成明确的提示
	a.handle(mux, "/api/user/login_by_ldap", func(c *call) error {
		return c.fail(401, "ApiYard 暂不支持 LDAP 登录")
	}, public())
	// 第三方登录：YApi 在没有登录插件时跳回首页
	a.handle(mux, "/api/user/login_by_token", func(c *call) error {
		c.replied = true
		http.Redirect(c.w, c.r, "/", http.StatusFound)
		return nil
	}, public())
	// 由服务端去请求用户给的 URL 有 SSRF 风险，暂不提供；格式与 YApi 请求失败时相同
	a.handle(mux, "GET /api/project/swagger_url", func(c *call) error {
		return c.fail(402, "Error: ApiYard 暂不支持从 URL 获取 Swagger 数据，请下载 JSON 文件后再导入")
	})
	a.handle(mux, "GET /api/interface/download_crx", a.interfaceDownloadCrx)
}

// interfaceDownloadCrx 下载 cross-request 浏览器插件（“运行”页面发请求时需要），文件随前端一起发布。
func (a *API) interfaceDownloadCrx(c *call) error {
	if a.opts.WebFS == nil {
		c.replied = true
		http.NotFound(c.w, c.r)
		return nil
	}
	data, err := fs.ReadFile(a.opts.WebFS, "attachment/cross-request.zip")
	if errors.Is(err, fs.ErrNotExist) {
		c.replied = true
		http.NotFound(c.w, c.r)
		return nil
	}
	if err != nil {
		return err
	}
	c.replied = true
	c.w.Header().Set("Content-Disposition", "attachment; filename=crossRequest.zip")
	c.w.Header().Set("Content-Type", "application/zip")
	_, err = c.w.Write(data)
	return err
}
