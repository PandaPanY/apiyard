// Command compat 运行接口兼容性测试。
//
//	record：在原版 YApi 上执行全部场景，把响应保存为 golden 文件（执行前先重置对照实例）。
//	check ：在 ApiYard 上执行同样的场景，与 golden 文件逐字段比对。
//
// 用法：
//
//	go run ./tools/compat record -target http://localhost:13000
//	go run ./tools/compat check  -target http://localhost:3000 [-run 正则]
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/PandaPanY/apiyard/internal/compat"
)

func main() {
	if len(os.Args) < 2 || (os.Args[1] != "record" && os.Args[1] != "check") {
		fmt.Fprintln(os.Stderr, "用法: compat record|check -target URL [-dir testdata/compat] [-run 正则]")
		os.Exit(2)
	}
	mode := os.Args[1]

	fs := flag.NewFlagSet(mode, flag.ExitOnError)
	target := fs.String("target", "", "被测服务地址，如 http://localhost:13000")
	dir := fs.String("dir", "testdata/compat", "场景（scenarios/）和 golden 文件（golden/）所在目录")
	run := fs.String("run", "", "只执行名称匹配该正则的场景")
	_ = fs.Parse(os.Args[2:])

	if *target == "" {
		fmt.Fprintln(os.Stderr, "必须指定 -target")
		os.Exit(2)
	}
	if err := execute(mode, *target, *dir, *run); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

// errMismatch 表示比对发现差异，具体内容已经打印。
var errMismatch = errors.New("存在不兼容的响应")

func execute(mode, target, dir, run string) error {
	filter, err := regexp.Compile(run)
	if err != nil {
		return err
	}
	scenarios, err := compat.LoadScenarios(filepath.Join(dir, "scenarios"))
	if err != nil {
		return err
	}

	failed := false
	for _, sc := range scenarios {
		if !filter.MatchString(sc.Name) {
			continue
		}
		exchanges, err := compat.Run(context.Background(), target, sc)
		if err != nil {
			fmt.Printf("✗ %s: %v\n", sc.Name, err)
			failed = true
			if mode == "record" {
				return errors.New("场景在原版 YApi 上执行失败，请先修正场景")
			}
			continue
		}

		goldenPath := filepath.Join(dir, "golden", sc.Name+".json")
		if mode == "record" {
			if err := compat.WriteGolden(goldenPath, exchanges); err != nil {
				return err
			}
			fmt.Printf("✓ %s: 已录制 %d 次调用\n", sc.Name, len(exchanges))
			continue
		}

		golden, err := compat.ReadGolden(goldenPath)
		if err != nil {
			return err
		}
		diffs, err := compat.Diff(golden, exchanges)
		if err != nil {
			return err
		}
		if len(diffs) == 0 {
			fmt.Printf("✓ %s: %d 次调用全部一致\n", sc.Name, len(exchanges))
			continue
		}
		failed = true
		fmt.Printf("✗ %s: %d 处差异\n", sc.Name, len(diffs))
		for _, d := range diffs {
			fmt.Println("    " + d)
		}
	}
	if failed {
		return errMismatch
	}
	return nil
}
