package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/PandaPanY/apiyard/internal/migrate"
)

// runMigrate 执行 apiyard migrate 子命令：把 YApi 的数据迁移到 ApiYard 的数据库。
func runMigrate(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("apiyard migrate", flag.ContinueOnError)
	fs.SetOutput(out)
	mongoURI := fs.String("mongo", "", "YApi 的 MongoDB 连接串，如 mongodb://user:pass@127.0.0.1:27017/yapi（MongoDB 3.6 及以上）")
	dump := fs.String("dump", "", "mongodump 导出的目录，代替 -mongo（任何版本的 MongoDB 都可以）")
	dbName := fs.String("mongo-db", "", "YApi 的数据库名，默认取连接串里的库名，都没有时为 yapi")
	target := fs.String("db", envOr("APIYARD_DB", "apiyard.db"), "迁移到的 SQLite 数据库文件（环境变量 APIYARD_DB）")
	overwrite := fs.Bool("overwrite", false, "目标数据库已存在时用迁移的数据替换它（请先停止 ApiYard）")
	dryRun := fs.Bool("dry-run", false, "只预检：完整执行一遍迁移和核对，输出报告，不写入目标数据库")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(out, "用法：apiyard migrate (-mongo <连接串> | -dump <目录>) [-db apiyard.db] [-overwrite] [-dry-run]")
		_, _ = fmt.Fprintln(out, "把 YApi 的用户、分组、项目、分类、接口、token、关注、动态和头像迁移到 ApiYard，ID 保持不变。")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if (*mongoURI == "") == (*dump == "") {
		fs.Usage()
		return errors.New("-mongo 和 -dump 必须指定一个")
	}

	var src migrate.Source
	var err error
	if *mongoURI != "" {
		src, err = migrate.OpenMongo(ctx, *mongoURI, *dbName)
	} else {
		src, err = migrate.OpenDump(*dump, *dbName)
	}
	if err != nil {
		return err
	}
	defer func() { _ = src.Close(context.WithoutCancel(ctx)) }()

	rep, err := migrate.Run(ctx, src, migrate.Options{Target: *target, Overwrite: *overwrite, DryRun: *dryRun, Progress: out})
	if rep != nil {
		_, _ = fmt.Fprintln(out)
		rep.Print(out)
	}
	if err != nil {
		return err
	}
	if *dryRun {
		_, _ = fmt.Fprintln(out, "\n预检完成，没有写入任何数据。去掉 -dry-run 即可正式迁移。")
		return nil
	}
	_, _ = fmt.Fprintf(out, "\n迁移完成：%s\n", *target)
	_, _ = fmt.Fprintln(out, "启动时请用 -token-salt 指定 YApi 配置文件（config.json）里的 passsalt，没有配置过就是默认值 abcde，原来的项目 token 才能继续使用。")
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
