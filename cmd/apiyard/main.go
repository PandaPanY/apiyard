// Command apiyard 启动 ApiYard 服务。子命令：
//
//	apiyard migrate      把 YApi 的数据迁移过来
//	apiyard version      显示版本
//	apiyard healthcheck  检查本机运行的服务是否正常（供 Docker 的 HEALTHCHECK 使用）
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/PandaPanY/apiyard/internal/config"
	"github.com/PandaPanY/apiyard/internal/server"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("服务异常退出", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	args := os.Args[1:]
	if len(args) == 0 {
		return serve(ctx, args, logger)
	}
	var err error
	switch args[0] {
	case "migrate":
		err = runMigrate(ctx, args[1:], os.Stdout)
	case "version", "-version", "--version":
		_, err = fmt.Fprintln(os.Stdout, versionString())
	case "healthcheck":
		err = runHealthcheck(ctx, args[1:])
	default:
		return serve(ctx, args, logger)
	}
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

func serve(ctx context.Context, args []string, logger *slog.Logger) error {
	cfg, err := config.Load(args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	cfg.Version = version()
	logger.Info("ApiYard 启动", "version", cfg.Version)
	return server.Run(ctx, cfg, logger)
}
