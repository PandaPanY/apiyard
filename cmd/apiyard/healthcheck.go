package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"time"
)

// runHealthcheck 请求本机服务的 /healthz，状态码不是 200 时返回错误（退出码为 1）。
// Docker 镜像基于 distroless，里面没有 curl、wget，HEALTHCHECK 只能调用 apiyard 自己。
func runHealthcheck(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("apiyard healthcheck", flag.ContinueOnError)
	addr := fs.String("addr", envOr("APIYARD_ADDR", ":3000"), "服务的监听地址，与启动服务时的 -addr 相同（环境变量 APIYARD_ADDR）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	url, err := healthURL(*addr)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req) // #nosec G704 -- 只请求本机地址
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s 返回 %d", url, resp.StatusCode)
	}
	return nil
}

// healthURL 把监听地址转成本机的检查地址：没有主机名或监听全部地址时改用 127.0.0.1。
func healthURL(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("监听地址 %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz", nil
}
