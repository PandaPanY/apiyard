// Package web 托管 YApi 预构建的前端，行为与 YApi 的 server/app.js 保持一致：
// 前端路由回退到 index.html，/prd 下的资源优先返回预压缩的 .gz 文件。
// 此外在 index.html 中注入 ApiYard 的前端补丁（见 patch.go），用于隐藏暂不支持的功能和禁止外连。
package web

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
)

// contentTypes 固定常见前端资源的 MIME 类型。Go 默认会参考操作系统的注册表，
// 部分 Windows 机器把 .js 登记为 text/plain，浏览器会因此拒绝执行脚本。
var contentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".gif":   "image/gif",
	".ico":   "image/x-icon",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".eot":   "application/vnd.ms-fontobject",
}

// spaPath 与 YApi app.js 中的规则一致：只含字母、数字和 /-_ 的路径属于前端路由。
// YApi 的正则还用负向前瞻排除了 /api 开头的路径，Go 不支持前瞻，改在 isSPARoute 里单独判断。
var spaPath = regexp.MustCompile(`^/[a-zA-Z0-9/\-_]*$`)

// Handler 返回前端静态文件处理器，fsys 的根目录对应 yapi-vendor 包里的 static 目录。
func Handler(fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := path.Clean(r.URL.Path)
		switch {
		case isPatchPath(clean):
			serveFile(w, r, patchFS, strings.TrimPrefix(clean, patchPrefix))
		case isSPARoute(r.URL.Path) || clean == "/index.html":
			serveIndex(w, r, fsys)
		default:
			serveFile(w, r, fsys, strings.TrimPrefix(clean, "/"))
		}
	})
}

func isSPARoute(p string) bool {
	return !strings.HasPrefix(p, "/api") && spaPath.MatchString(p)
}

func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	w.Header().Set("Cache-Control", cacheControl(name))

	// 浏览器支持 gzip 且存在预压缩文件时直接返回 .gz，省去实时压缩
	if acceptsGzip(r) {
		if f, ok := openFile(fsys, name+".gz"); ok {
			defer closeQuietly(f)
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Add("Vary", "Accept-Encoding")
			serveContent(w, r, name, f)
			return
		}
	}

	f, ok := openFile(fsys, name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	defer closeQuietly(f)
	serveContent(w, r, name, f)
}

// openFile 打开普通文件；文件不存在或是目录时返回 false。
func openFile(fsys fs.FS, name string) (fs.File, bool) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, false
	}
	if info, err := f.Stat(); err != nil || info.IsDir() {
		closeQuietly(f)
		return nil, false
	}
	return f, true
}

// serveContent 按 name 的扩展名设置 Content-Type，并支持条件请求和范围请求。
func serveContent(w http.ResponseWriter, r *http.Request, name string, f fs.File) {
	// 预先设置的 Content-Type 会被 ServeContent 沿用；表里没有的扩展名交给它自行推断
	if ct, ok := contentTypes[path.Ext(name)]; ok {
		w.Header().Set("Content-Type", ct)
	}
	info, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, errors.ErrUnsupported.Error(), http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, name, info.ModTime(), rs)
}

// cacheControl：文件名带内容哈希（如 index@40d4…js）的资源永不变化，可以长期缓存；其余文件每次都要校验。
func cacheControl(name string) string {
	if strings.HasPrefix(name, "prd/") && strings.Contains(path.Base(name), "@") {
		return "public, max-age=31536000, immutable"
	}
	return "no-cache"
}

func acceptsGzip(r *http.Request) bool {
	for _, enc := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		token, _, _ := strings.Cut(strings.TrimSpace(enc), ";")
		if token == "gzip" {
			return true
		}
	}
	return false
}

func closeQuietly(c io.Closer) { _ = c.Close() }
