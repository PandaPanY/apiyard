// Package server 组装 HTTP 路由并管理服务的启动与优雅退出。
package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/PandaPanY/apiyard/internal/api"
	"github.com/PandaPanY/apiyard/internal/config"
	"github.com/PandaPanY/apiyard/internal/store"
	"github.com/PandaPanY/apiyard/internal/web"
)

const (
	// readHeaderTimeout 限制读取请求头的时间，防止慢速连接耗尽资源。
	readHeaderTimeout = 10 * time.Second
	// shutdownTimeout 是退出时等待进行中请求完成的最长时间。
	shutdownTimeout = 10 * time.Second
)

// New 按配置组装全部路由。st 在转发模式下可以为 nil。
func New(cfg config.Config, st *store.Store, logger *slog.Logger) (http.Handler, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)

	var webFS fs.FS
	if info, err := os.Stat(cfg.WebDir); err == nil && info.IsDir() {
		webFS = os.DirFS(cfg.WebDir)
		mux.Handle("/", web.Handler(webFS))
	} else {
		logger.Warn("找不到前端目录，界面不可用；可执行 go run ./tools/fetch-yapi-web 获取", "web_dir", cfg.WebDir)
	}

	if cfg.YAPIUpstream == "" {
		api.New(st, api.Options{CloseRegister: cfg.CloseRegister, WebFS: webFS}, logger).Register(mux)
		return mux, nil
	}

	// 转发模式把全部接口交给原版 YApi，不使用 ApiYard 自己的实现：
	// 两边的用户和数据不互通（ApiYard 签发的登录 Cookie 原版无法识别），混用会导致界面失效。
	proxy, err := newUpstreamProxy(cfg.YAPIUpstream, logger)
	if err != nil {
		return nil, err
	}
	mux.Handle("/api/", proxy)
	mux.Handle("/mock/", proxy)
	logger.Warn("转发模式：全部 /api 和 /mock 请求交给原版 YApi", "upstream", cfg.YAPIUpstream)
	return mux, nil
}

// openStore 打开数据库；数据库里还没有用户时创建管理员账号。
func openStore(ctx context.Context, cfg config.Config, logger *slog.Logger) (*store.Store, error) {
	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("打开数据库 %s: %w", cfg.DBPath, err)
	}
	created, password, err := api.Bootstrap(ctx, st, cfg.AdminEmail, cfg.AdminPassword)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	if created && cfg.AdminPassword == "" {
		// 随机密码只在首次启动时出现这一次
		logger.Warn("已创建管理员账号，请登录后尽快修改密码", "email", cfg.AdminEmail, "password", password)
	} else if created {
		logger.Info("已创建管理员账号", "email", cfg.AdminEmail)
	}
	return st, nil
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// Run 按配置启动服务，阻塞到 ctx 被取消后优雅退出。
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	var st *store.Store
	if cfg.YAPIUpstream == "" {
		var err error
		if st, err = openStore(ctx, cfg, logger); err != nil {
			return err
		}
		defer func() { _ = st.Close() }()
	}

	handler, err := New(cfg, st, logger)
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
