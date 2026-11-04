package web

import (
	"bytes"
	"embed"
	"io/fs"
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

// serveIndex 返回注入了补丁脚本的 index.html，并附带 CSP 响应头。
func serveIndex(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
	_, _ = w.Write(injectPatch(data))
}

// injectPatch 把补丁脚本插到第一个 <head> 标签之后；找不到 <head> 时插到文档最前面。
func injectPatch(html []byte) []byte {
	lower := bytes.ToLower(html)
	start := bytes.Index(lower, []byte("<head"))
	if start < 0 {
		return append([]byte(patchScriptTag), html...)
	}
	end := bytes.IndexByte(lower[start:], '>')
	if end < 0 {
		return append([]byte(patchScriptTag), html...)
	}
	pos := start + end + 1

	out := make([]byte, 0, len(html)+len(patchScriptTag))
	out = append(out, html[:pos]...)
	out = append(out, patchScriptTag...)
	return append(out, html[pos:]...)
}

func isPatchPath(p string) bool {
	return strings.HasPrefix(p, patchPrefix)
}
