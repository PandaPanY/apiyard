package web

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	webassets "github.com/PandaPanY/apiyard/web"
)

func TestApplyRewrites(t *testing.T) {
	rules := []rewrite{{"a", "b", 2}, {"bb", "c", 1}}
	got, err := applyRewrites([]byte("xaax"), rules)
	if err != nil || string(got) != "xcx" {
		t.Fatalf("applyRewrites = %q, %v；期望按顺序替换得到 xcx", got, err)
	}
	// 次数不符时整体失败，不做部分替换
	if _, err := applyRewrites([]byte("xax"), rules); err == nil {
		t.Error("出现次数不符时应返回错误")
	}
}

// 用打包的真实前端核对：每条替换都命中，界面上不再出现 YApi 的名称、文档地址和原版主色。
func TestBrandEmbeddedFrontend(t *testing.T) {
	fsys := webassets.FS()
	if fsys == nil {
		t.Skip("没有打包前端（先执行 go run ./tools/fetch-yapi-web）")
	}
	front, err := loadBrandedFrontend(fsys)
	if err != nil {
		t.Fatal(err)
	}
	js, css := front.files[front.jsPath].plain, front.files[front.cssPath].plain
	for _, s := range []string{`"YAPI"`, `"https://hellosean1025.github.io/yapi"`, `"https://ymfe.org"`, `"欢迎使用 YApi ~"`, `color:"#108ee9"`} {
		if bytes.Contains(js, []byte(s)) {
			t.Errorf("主包里还有 %s", s)
		}
	}
	if bytes.Contains(css, []byte("#2395f1")) {
		t.Error("样式表里还有原版主色 #2395f1")
	}

	h := Handler(fsys, Options{Version: "v9.9.9"})
	serve := func(target, enc string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if enc != "" {
			req.Header.Set("Accept-Encoding", enc)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	index := serve("/group", "").Body.String()
	for _, s := range []string{"<title>ApiYard</title>", `window.APIYARD_VERSION="v9.9.9"`, "/apiyard/logo.svg", themeLinkTag} {
		if !strings.Contains(index, s) {
			t.Errorf("index.html 里没有 %s", s)
		}
	}
	// 主题样式表要排在原版样式表之后才能覆盖它
	if strings.Index(index, themeLinkTag) < strings.Index(index, "WEBPACK_ASSETS['index.js'].css") {
		t.Error("主题样式表应在原版样式表之后加载")
	}
	if login := serve("/", "").Body.String(); !strings.Contains(login, "/apiyard/login.js") || !strings.Contains(login, "v9.9.9") {
		t.Error("/ 应返回 ApiYard 的登录页")
	}

	// assets.js 里的主包和样式表地址带上内容哈希，浏览器不会继续用缓存里的原版文件
	assets := serve("/prd/assets.js", "").Body.String()
	for _, p := range []string{front.jsPath, front.cssPath} {
		if !strings.Contains(assets, strings.TrimPrefix(p, "prd/")+"?v=") {
			t.Errorf("assets.js 没有改写 %s 的地址: %s", p, assets)
		}
	}

	for _, p := range []string{front.jsPath, front.cssPath} {
		plain := front.files[p].plain
		rec := serve("/"+p+"?v=x", "gzip")
		if rec.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("%s: 支持 gzip 时应返回压缩的内容", p)
		}
		zr, err := gzip.NewReader(rec.Body)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := io.ReadAll(zr); !bytes.Equal(got, plain) {
			t.Errorf("%s: 压缩的内容与替换后的内容不一致", p)
		}
		if got := serve("/"+p, "").Body.Bytes(); !bytes.Equal(got, plain) {
			t.Errorf("%s: 不支持 gzip 时应返回替换后的原文", p)
		}
	}
	for name, ct := range map[string]string{"logo.svg": "image/svg+xml", "theme.css": "text/css; charset=utf-8", "login.js": "text/javascript; charset=utf-8"} {
		if rec := serve("/apiyard/"+name, ""); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != ct {
			t.Errorf("%s: 状态码 %d, Content-Type %q", name, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
}
