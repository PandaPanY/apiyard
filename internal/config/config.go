// Package config 负责加载运行配置，优先级：命令行参数 > 环境变量 > 默认值。
package config

import (
	"flag"
	"os"
	"strconv"
)

// defaultAddr 与 YApi 的默认端口保持一致，方便原地替换。
const defaultAddr = ":3000"

// Config 是服务运行所需的全部配置。
type Config struct {
	Addr          string // HTTP 监听地址
	DBPath        string // SQLite 数据库文件
	WebDir        string // 前端静态文件目录（yapi-vendor 包里的 static 目录）
	AdminEmail    string // 首次启动时创建的管理员邮箱
	AdminPassword string // 首次启动时的管理员密码；为空则随机生成并打印到日志
	CloseRegister bool   // 关闭用户注册
	TokenSalt     string // 开放 API token 的加密口令，对应 YApi 配置文件里的 passsalt
	YAPIUpstream  string // 开发用：把全部 /api、/mock 请求转发到这个原版 YApi，为空则不转发
}

// Load 从命令行参数和环境变量构造配置，args 不包含程序名。
func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("apiyard", flag.ContinueOnError)
	var cfg Config
	fs.StringVar(&cfg.Addr, "addr", envOr("APIYARD_ADDR", defaultAddr), "HTTP 监听地址（环境变量 APIYARD_ADDR）")
	fs.StringVar(&cfg.DBPath, "db", envOr("APIYARD_DB", "apiyard.db"), "SQLite 数据库文件（环境变量 APIYARD_DB）")
	fs.StringVar(&cfg.WebDir, "web-dir", envOr("APIYARD_WEB_DIR", "web/yapi"), "前端静态文件目录（环境变量 APIYARD_WEB_DIR）")
	fs.StringVar(&cfg.AdminEmail, "admin-email", envOr("APIYARD_ADMIN_EMAIL", "admin@admin.com"),
		"首次启动时创建的管理员邮箱（环境变量 APIYARD_ADMIN_EMAIL）")
	fs.StringVar(&cfg.AdminPassword, "admin-password", envOr("APIYARD_ADMIN_PASSWORD", ""),
		"首次启动时的管理员密码，为空则随机生成（环境变量 APIYARD_ADMIN_PASSWORD）")
	fs.StringVar(&cfg.TokenSalt, "token-salt", envOr("APIYARD_TOKEN_SALT", "abcde"),
		"开放 API token 的加密口令，从 YApi 迁移时填原配置文件里的 passsalt（环境变量 APIYARD_TOKEN_SALT）")
	fs.BoolVar(&cfg.CloseRegister, "close-register", envBool("APIYARD_CLOSE_REGISTER"), "关闭用户注册（环境变量 APIYARD_CLOSE_REGISTER）")
	fs.StringVar(&cfg.YAPIUpstream, "yapi-upstream", envOr("APIYARD_YAPI_UPSTREAM", ""),
		"开发用：把全部 /api、/mock 请求转发到原版 YApi，如 http://localhost:13000（环境变量 APIYARD_YAPI_UPSTREAM）")
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

// envBool 把环境变量解析为布尔值，无法解析时为 false。
func envBool(key string) bool {
	b, _ := strconv.ParseBool(os.Getenv(key))
	return b
}
