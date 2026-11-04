package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// newUpstreamProxy 创建转发到原版 YApi 的反向代理。
// 开发期用它把 ApiYard 尚未实现的接口交给 YApi 处理，界面始终可用；也支持 WebSocket 升级。
func newUpstreamProxy(rawURL string, logger *slog.Logger) (http.Handler, error) {
	target, err := url.Parse(rawURL)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return nil, fmt.Errorf("无效的 YApi 上游地址 %q，需要形如 http://host:port", rawURL)
	}
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.SetXForwarded()
		},
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}, nil
}
