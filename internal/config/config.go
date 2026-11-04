// Package config 负责加载运行配置，优先级：命令行参数 > 环境变量 > 默认值。
package config

import (
	"flag"
	"os"
)

// defaultAddr 与 YApi 的默认端口保持一致，方便原地替换。
const defaultAddr = ":3000"

// Config 是服务运行所需的全部配置。
type Config struct {
	Addr         string // HTTP 监听地址
	WebDir       string // 前端静态文件目录（yapi-vendor 包里的 static 目录）
	YAPIUpstream string // 开发用：把未实现的 /api、/mock 请求转发到这个原版 YApi，为空则不转发
}

// Load 从命令行参数和环境变量构造配置，args 不包含程序名。
func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("apiyard", flag.ContinueOnError)
	var cfg Config
	fs.StringVar(&cfg.Addr, "addr", envOr("APIYARD_ADDR", defaultAddr), "HTTP 监听地址（环境变量 APIYARD_ADDR）")
	fs.StringVar(&cfg.WebDir, "web-dir", envOr("APIYARD_WEB_DIR", "web/yapi"), "前端静态文件目录（环境变量 APIYARD_WEB_DIR）")
	fs.StringVar(&cfg.YAPIUpstream, "yapi-upstream", envOr("APIYARD_YAPI_UPSTREAM", ""),
		"开发用：把未实现的接口转发到原版 YApi，如 http://localhost:13000（环境变量 APIYARD_YAPI_UPSTREAM）")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// envOr 返回环境变量 key 的值；未设置或为空时返回 fallback。
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
