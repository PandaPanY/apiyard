package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
)

// patchPrefix 是 ApiYard 自带前端资源的路径前缀，与 YApi 的静态文件互不重叠。
const patchPrefix = "/apiyard/"

// contentSecurityPolicy 禁止前端向其他站点发起 XHR 请求。
// 原版前端会请求 fastmock.site 检查新版本，内网环境里这既无意义又会暴露访问信息。
const contentSecurityPolicy = "connect-src 'self'"

// patchScriptTag 注入到 index.html 的 <head> 最前面，保证补丁先于 YApi 前端执行。
const patchScriptTag = `<script src="/apiyard/patch.js"></script>`

// themeLinkTag 注入到 </head> 之前，排在原版样式表之后，才能以相同的优先级覆盖它。
const themeLinkTag = `<link rel="stylesheet" href="/apiyard/theme.css">`

//go:embed patch
var patchFiles embed.FS

// patchFS 的根目录就是 patch 目录。
var patchFS = mustSub(patchFiles, "patch")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// loadIndex 读取 index.html，替换品牌文字并注入补丁脚本；没有 index.html 时返回 nil。
func loadIndex(fsys fs.FS, version string, logger *slog.Logger) []byte {
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return nil
	}
	if branded, err := applyRewrites(data, indexRewrites); err == nil {
		data = branded
	} else {
		logger.Warn("index.html 品牌替换失败，按原样返回", "err", err)
	}
	return injectPatch(injectTheme(data), version)
}

// loadLoginPage 读取 ApiYard 的登录页并写入版本号。它替代原版的首页和登录页（见 architecture.md D6）。
func loadLoginPage(version string) []byte {
	data, err := fs.ReadFile(patchFS, "login.html")
	if err != nil {
		panic(err) // 随可执行文件打包，读不到说明构建有误
	}
	return injectHead(data, versionScript(version))
}

func isLoginRoute(p string) bool {
	return p == "/" || p == "/login" || p == "/login/"
}

// serveIndex 返回处理过的 index.html；没有前端时返回 404。
func serveIndex(w http.ResponseWriter, r *http.Request, index []byte) {
	if index == nil {
		http.NotFound(w, r)
		return
	}
	serveHTML(w, index)
}

// serveHTML 返回页面，并附带 CSP 响应头。
func serveHTML(w http.ResponseWriter, page []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
	_, _ = w.Write(page)
}

// versionScript 生成设置 window.APIYARD_VERSION 的脚本。
func versionScript(version string) string {
	// json.Marshal 会转义 < > &，版本号不会破坏 <script> 标签
	v, _ := json.Marshal(version)
	return "<script>window.APIYARD_VERSION=" + string(v) + "</script>"
}

// injectPatch 把版本号和补丁脚本插到第一个 <head> 标签之后。
func injectPatch(html []byte, version string) []byte {
	return injectHead(html, versionScript(version)+patchScriptTag)
}

// injectTheme 把主题样式表插到最后一个 </head> 之前；找不到时原样返回。
func injectTheme(html []byte) []byte {
	pos := bytes.LastIndex(bytes.ToLower(html), []byte("</head>"))
	if pos < 0 {
		return html
	}
	out := make([]byte, 0, len(html)+len(themeLinkTag))
	out = append(out, html[:pos]...)
	out = append(out, themeLinkTag...)
	return append(out, html[pos:]...)
}

// injectHead 把 tags 插到第一个 <head> 标签之后；找不到 <head> 时插到文档最前面。
func injectHead(html []byte, tags string) []byte {
	pos := 0
	lower := bytes.ToLower(html)
	if start := bytes.Index(lower, []byte("<head")); start >= 0 {
		if end := bytes.IndexByte(lower[start:], '>'); end >= 0 {
			pos = start + end + 1
		}
	}
	out := make([]byte, 0, len(html)+len(tags))
	out = append(out, html[:pos]...)
	out = append(out, tags...)
	return append(out, html[pos:]...)
}

func isPatchPath(p string) bool {
	return strings.HasPrefix(p, patchPrefix)
}
