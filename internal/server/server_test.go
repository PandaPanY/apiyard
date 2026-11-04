package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PandaPanY/apiyard/internal/config"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// newHandler 用给定配置构造处理器，失败时终止测试。
func newHandler(t *testing.T, cfg config.Config) http.Handler {
	t.Helper()
	h, err := New(cfg, discardLogger)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func do(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestHealth(t *testing.T) {
	h := newHandler(t, config.Config{})

	rec := do(h, http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK || rec.Body.String() != `{"status":"ok"}` {
		t.Errorf("GET /healthz: 状态码 %d, 响应 %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodPost, "/healthz"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /healthz: 状态码 %d, 期望 405", rec.Code)
	}
}

func TestUpstreamProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "yapi:"+r.URL.RequestURI())
	}))
	defer upstream.Close()

	h := newHandler(t, config.Config{YAPIUpstream: upstream.URL})
	for _, target := range []string{"/api/user/status?x=1", "/mock/11/users"} {
		if rec := do(h, http.MethodGet, target); rec.Body.String() != "yapi:"+target {
			t.Errorf("%s 应转发到上游, 实际响应 %q", target, rec.Body.String())
		}
	}
	if rec := do(h, http.MethodGet, "/healthz"); rec.Body.String() != `{"status":"ok"}` {
		t.Error("ApiYard 自己的路由不应被转发")
	}
}

func TestInvalidUpstream(t *testing.T) {
	for _, u := range []string{"localhost:3000", "ftp://host", "http://"} {
		if _, err := New(config.Config{YAPIUpstream: u}, discardLogger); err == nil {
			t.Errorf("上游地址 %q 应当报错", u)
		}
	}
}

func TestWebDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("index"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHandler(t, config.Config{WebDir: dir})
	if rec := do(h, http.MethodGet, "/group/11"); !strings.HasSuffix(rec.Body.String(), "index") {
		t.Errorf("前端路由应返回 index.html, 实际 %q", rec.Body.String())
	}

	// 前端目录不存在时服务照常启动，只是没有界面
	h = newHandler(t, config.Config{WebDir: filepath.Join(dir, "missing")})
	if rec := do(h, http.MethodGet, "/"); rec.Code != http.StatusNotFound {
		t.Errorf("没有前端目录时 / 应返回 404, 实际 %d", rec.Code)
	}
}

func TestServeShutsDownOnCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, ln, newHandler(t, config.Config{}), discardLogger) }()

	// 先确认服务能正常响应，再触发退出。
	resp, err := http.Get("http://" + ln.Addr().String() + "/healthz")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	_ = resp.Body.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve() 返回错误: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("服务没有在 5 秒内退出")
	}
}
