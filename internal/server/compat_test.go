package server

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PandaPanY/apiyard/internal/compat"
	"github.com/PandaPanY/apiyard/internal/config"
)

// implementedScenarios 是 ApiYard 已经实现、必须与原版 YApi 完全一致的场景。每完成一部分就把对应场景加进来。
// 录制时所有场景按文件名顺序在同一个实例上执行，后面的场景依赖前面留下的数据（如 02 用到 01 注册的用户），
// 所以这里必须是从第一个场景开始的连续一段。
var implementedScenarios = map[string]bool{
	"01-user-and-group": true,
	"02-user-profile":   true,
	"03-project":        true,
	"04-project-member": true,
	"05-interface":      true,
	"06-misc":           true,
	"07-open-api":       true,
	"08-export":         true,
}

// TestCompatWithYApi 在进程内启动一个全新的 ApiYard，按顺序执行已实现的兼容场景，
// 并与从原版 YApi 录制的 golden 文件逐字段比对（录制方法见 docs/testing.md）。
func TestCompatWithYApi(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "compat")
	scenarios, err := compat.LoadScenarios(filepath.Join(dir, "scenarios"))
	if err != nil {
		t.Fatal(err)
	}

	// 初始状态与录制时相同：只有一个使用 YApi 默认密码的管理员
	srv := httptest.NewServer(newHandler(t, config.Config{AdminEmail: "admin@admin.com", AdminPassword: "ymfe.org"}))
	defer srv.Close()

	ran := 0
	for i, sc := range scenarios {
		if !implementedScenarios[sc.Name] {
			for _, later := range scenarios[i+1:] {
				if implementedScenarios[later.Name] {
					t.Fatalf("场景 %s 已实现，但它之前的 %s 还没实现：已实现的场景必须连续", later.Name, sc.Name)
				}
			}
			break
		}
		ran++
		t.Run(sc.Name, func(t *testing.T) {
			actual, err := compat.Run(context.Background(), srv.URL, sc)
			if err != nil {
				t.Fatal(err)
			}
			golden, err := compat.ReadGolden(filepath.Join(dir, "golden", sc.Name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			diffs, err := compat.Diff(golden, actual)
			if err != nil {
				t.Fatal(err)
			}
			if len(diffs) > 0 {
				t.Errorf("与原版 YApi 的响应有 %d 处差异:\n  %s", len(diffs), strings.Join(diffs, "\n  "))
			}
		})
	}
	if ran != len(implementedScenarios) {
		t.Errorf("implementedScenarios 里有 %d 个场景，实际执行了 %d 个，请检查名称", len(implementedScenarios), ran)
	}
}
