package web

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

var testFS = fstest.MapFS{
	"index.html":                {Data: []byte("<html>index</html>")},
	"prd/assets.js":             {Data: []byte("assets")},
	"prd/index@abc123.js":       {Data: []byte("plain-js")},
	"prd/index@abc123.js.gz":    {Data: []byte("gzipped-js")},
	"image/favicon.png":         {Data: []byte("png")},
	"iconfont/iconfont.css":     {Data: []byte("css")},
	"attachment/readme_test.md": {Data: []byte("md")},
}

func get(t *testing.T, target, acceptEncoding string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	Handler(testFS).ServeHTTP(rec, req)
	return rec
}

func TestSPARoutesServeIndex(t *testing.T) {
	for _, p := range []string{"/", "/login", "/group/11", "/project/11/interface/api/25", "/prd/", "/index.html"} {
		rec := get(t, p, "")
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, "<html>index</html>") {
			t.Errorf("%s: 状态码 %d, 响应 %q, 期望返回 index.html", p, rec.Code, body)
		}
		if !strings.Contains(body, patchScriptTag) {
			t.Errorf("%s: index.html 中没有注入补丁脚本", p)
		}
		if rec.Header().Get("Content-Security-Policy") != contentSecurityPolicy {
			t.Errorf("%s: 缺少 CSP 响应头", p)
		}
	}
}

func TestInjectPatch(t *testing.T) {
	tests := []struct{ in, want string }{
		{"<html><head><title>x</title></head></html>", "<html><head>" + patchScriptTag + "<title>x</title></head></html>"},
		{`<HEAD lang="zh"><meta>`, `<HEAD lang="zh">` + patchScriptTag + "<meta>"},
		{"<body>no head</body>", patchScriptTag + "<body>no head</body>"},
	}
	for _, tt := range tests {
		if got := string(injectPatch([]byte(tt.in))); got != tt.want {
			t.Errorf("injectPatch(%q) = %q, 期望 %q", tt.in, got, tt.want)
		}
	}
}

func TestPatchScript(t *testing.T) {
	rec := get(t, "/apiyard/patch.js", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "google-analytics") {
		t.Fatalf("补丁脚本: 状态码 %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/javascript; charset=utf-8" {
		t.Errorf("补丁脚本 Content-Type = %q", ct)
	}
	if rec := get(t, "/apiyard/../index.html", ""); strings.Contains(rec.Body.String(), "google-analytics") {
		t.Error("路径穿越不应读到补丁目录之外")
	}
}

func TestStaticFiles(t *testing.T) {
	rec := get(t, "/image/favicon.png", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("favicon: 状态码 %d, Content-Type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestPrecompressedGzip(t *testing.T) {
	rec := get(t, "/prd/index@abc123.js", "gzip, deflate, br")
	if rec.Body.String() != "gzipped-js" || rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("支持 gzip 时应返回 .gz: 响应 %q, Content-Encoding %q", rec.Body.String(), rec.Header().Get("Content-Encoding"))
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type 应按原文件名推断, 实际 %q", ct)
	}

	rec = get(t, "/prd/index@abc123.js", "")
	if rec.Body.String() != "plain-js" || rec.Header().Get("Content-Encoding") != "" {
		t.Errorf("不支持 gzip 时应返回原文件: 响应 %q", rec.Body.String())
	}
	if rec.Header().Get("Vary") != "Accept-Encoding" {
		t.Error("有 .gz 时，返回原文件也要带 Vary")
	}
}

// 打包的前端只有 .gz 时，不支持 gzip 的客户端得到解压后的内容。
func TestGzipOnly(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte("console.log(1)"))
	_ = zw.Close()
	fsys := fstest.MapFS{"prd/lib@x.js.gz": {Data: buf.Bytes()}}

	req := httptest.NewRequest(http.MethodGet, "/prd/lib@x.js", nil)
	rec := httptest.NewRecorder()
	Handler(fsys).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "console.log(1)" || rec.Header().Get("Content-Encoding") != "" {
		t.Errorf("应返回解压后的内容: %d %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type: %q", ct)
	}

	req.Header.Set("Accept-Encoding", "gzip")
	rec = httptest.NewRecorder()
	Handler(fsys).ServeHTTP(rec, req)
	if !bytes.Equal(rec.Body.Bytes(), buf.Bytes()) || rec.Header().Get("Content-Encoding") != "gzip" {
		t.Error("支持 gzip 时应原样返回 .gz")
	}
}

func TestCacheControl(t *testing.T) {
	tests := map[string]string{
		"/prd/index@abc123.js": "public, max-age=31536000, immutable",
		"/prd/assets.js":       "no-cache",
		"/":                    "no-cache",
	}
	for p, want := range tests {
		if got := get(t, p, "").Header().Get("Cache-Control"); got != want {
			t.Errorf("%s: Cache-Control = %q, 期望 %q", p, got, want)
		}
	}
}

func TestNotFound(t *testing.T) {
	// 注意 /image 这类不带点的路径按 YApi 规则属于前端路由，会返回 index.html 而不是 404
	for _, p := range []string{"/api/user/status", "/missing.js", "/prd/../../etc/passwd"} {
		if rec := get(t, p, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s: 状态码 %d, 期望 404", p, rec.Code)
		}
	}
}

func TestOnlyGetAndHead(t *testing.T) {
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		Handler(testFS).ServeHTTP(rec, httptest.NewRequest(m, "/group/11", nil))
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("%s: 状态码 %d, 应为 405", m, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	Handler(testFS).ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/image/favicon.png", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("HEAD: 状态码 %d", rec.Code)
	}
}
