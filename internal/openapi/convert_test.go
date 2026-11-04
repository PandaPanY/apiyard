package openapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/PandaPanY/apiyard/internal/jsjson"
)

// TestConvertMatchesYApi 把 testdata/import 下的每个文档转换后，与在 YApi 容器里运行原版 run.js 得到的结果（*.yapi.json）比较。
func TestConvertMatchesYApi(t *testing.T) {
	inputs, err := filepath.Glob(filepath.Join("..", "..", "testdata", "import", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, in := range inputs {
		if strings.HasSuffix(in, ".yapi.json") {
			continue
		}
		ran++
		name := filepath.Base(in)
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(in)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(strings.TrimSuffix(in, ".json") + ".yapi.json")
			if err != nil {
				t.Fatal(err)
			}
			res, err := Convert(string(src))
			if err != nil {
				t.Fatal(err)
			}
			got, err := jsjson.Marshal(resultJSON(res), "")
			if err != nil {
				t.Fatal(err)
			}
			var g, w any
			_ = json.Unmarshal(got, &g)
			_ = json.Unmarshal(want, &w)
			if !reflect.DeepEqual(g, w) {
				diffAPIs(t, g, w)
			}
		})
	}
	if ran == 0 {
		t.Fatal("没有找到测试文档")
	}
}

// resultJSON 按 run.js 的输出格式组织结果，便于比较。
func resultJSON(r *Result) *jsjson.Object {
	cats := []any{}
	for _, c := range r.Cats {
		cat := jsjson.NewObject("name", c.Name)
		if c.Desc != nil {
			cat.Set("desc", c.Desc)
		}
		cats = append(cats, cat)
	}
	apis := []any{}
	for _, a := range r.APIs {
		apis = append(apis, a)
	}
	return jsjson.NewObject("apis", apis, "cats", cats, "basePath", r.BasePath)
}

func diffAPIs(t *testing.T, got, want any) {
	t.Helper()
	g, _ := got.(map[string]any)
	w, _ := want.(map[string]any)
	if !reflect.DeepEqual(g["cats"], w["cats"]) || g["basePath"] != w["basePath"] {
		t.Errorf("分类或 basePath 不同:\n得到 %v %v\n期望 %v %v", g["cats"], g["basePath"], w["cats"], w["basePath"])
	}
	ga, _ := g["apis"].([]any)
	wa, _ := w["apis"].([]any)
	if len(ga) != len(wa) {
		t.Fatalf("接口数量不同: 得到 %d, 期望 %d", len(ga), len(wa))
	}
	for i := range wa {
		gm, wm := ga[i].(map[string]any), wa[i].(map[string]any)
		for k := range mergeKeys(gm, wm) {
			if !reflect.DeepEqual(gm[k], wm[k]) {
				t.Errorf("第 %d 个接口 %v %v 的 %s 不同:\n得到 %#v\n期望 %#v", i, wm["method"], wm["path"], k, gm[k], wm[k])
			}
		}
	}
}

func mergeKeys(a, b map[string]any) map[string]bool {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	return keys
}
