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

// implementedScenarios 是 ApiYard 已经实现、必须与原版 YApi 完全一致的场景。
// 每完成一个里程碑就把对应场景加进来。
var implementedScenarios = map[string]bool{
	"01-user-and-group": true,
}

// TestCompatWithYApi 在进程内启动一个全新的 ApiYard，执行兼容场景，
// 并与从原版 YApi 录制的 golden 文件逐字段比对（录制方法见 docs/testing.md）。
func TestCompatWithYApi(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "compat")
	scenarios, err := compat.LoadScenarios(filepath.Join(dir, "scenarios"))
	if err != nil {
		t.Fatal(err)
	}

	for _, sc := range scenarios {
		if !implementedScenarios[sc.Name] {
			continue
		}
		t.Run(sc.Name, func(t *testing.T) {
			// 每个场景都从和录制时相同的初始状态开始：只有一个使用 YApi 默认密码的管理员
			srv := httptest.NewServer(newHandler(t, config.Config{AdminEmail: "admin@admin.com", AdminPassword: "ymfe.org"}))
			defer srv.Close()

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
}
