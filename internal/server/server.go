// Package server 组装 HTTP 路由并管理服务的启动与优雅退出。
package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/PandaPanY/apiyard/internal/config"
	"github.com/PandaPanY/apiyard/internal/web"
)

const (
	// readHeaderTimeout 限制读取请求头的时间，防止慢速连接耗尽资源。
	readHeaderTimeout = 10 * time.Second
	// shutdownTimeout 是退出时等待进行中请求完成的最长时间。
	shutdownTimeout = 10 * time.Second
)

// New 按配置组装全部路由。
func New(cfg config.Config, logger *slog.Logger) (http.Handler, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)

	if cfg.YAPIUpstream != "" {
		proxy, err := newUpstreamProxy(cfg.YAPIUpstream, logger)
		if err != nil {
			return nil, err
		}
		// 以后实现的接口会注册更具体的路由，ServeMux 优先匹配它们，其余的才转发
		mux.Handle("/api/", proxy)
		mux.Handle("/mock/", proxy)
		logger.Warn("开发模式：/api 和 /mock 请求将转发到原版 YApi", "upstream", cfg.YAPIUpstream)
	}

	if info, err := os.Stat(cfg.WebDir); err == nil && info.IsDir() {
		mux.Handle("/", web.Handler(os.DirFS(cfg.WebDir)))
	} else {
		logger.Warn("找不到前端目录，界面不可用；可执行 go run ./tools/fetch-yapi-web 获取", "web_dir", cfg.WebDir)
	}
	return mux, nil
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// Run 按配置启动服务，阻塞到 ctx 被取消后优雅退出。
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	handler, err := New(cfg, logger)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	return serve(ctx, ln, handler, logger)
}

// serve 在已建立的监听器上提供服务，便于测试时使用随机端口。
func serve(ctx context.Context, ln net.Listener, handler http.Handler, logger *slog.Logger) error {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	logger.Info("服务已启动", "addr", ln.Addr().String())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("正在退出")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
