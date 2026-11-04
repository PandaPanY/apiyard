package compat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	vars := map[string]any{"gid": json.Number("11"), "name": "后端组"}

	got, err := render(map[string]any{
		"id":    "{{gid}}",
		"title": "{{name}}-{{gid}}",
		"uids":  []any{"{{gid}}"},
	}, vars)
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	if m["id"] != json.Number("11") {
		t.Errorf("整串变量应保留原类型, 实际 %#v", m["id"])
	}
	if m["title"] != "后端组-11" {
		t.Errorf("嵌入的变量应替换为文本, 实际 %#v", m["title"])
	}
	if m["uids"].([]any)[0] != json.Number("11") {
		t.Errorf("数组中的变量也应替换")
	}

	if _, err := render("{{missing}}", vars); err == nil {
		t.Error("未定义的变量应当报错")
	}
	if _, err := renderText("/p/{{missing}}", vars); err == nil {
		t.Error("路径中未定义的变量应当报错")
	}
}

func TestLookup(t *testing.T) {
	var v any
	if err := decodeJSON([]byte(`{"data":{"list":[{"_id":12}]}}`), &v); err != nil {
		t.Fatal(err)
	}
	got, err := lookup(v, "data.list.0._id")
	if err != nil || got != json.Number("12") {
		t.Errorf("lookup = %v, %v", got, err)
	}
	for _, bad := range []string{"data.missing", "data.list.5", "data.list.0._id.x"} {
		if _, err := lookup(v, bad); err == nil {
			t.Errorf("路径 %s 应当报错", bad)
		}
	}
}

func TestNormalize(t *testing.T) {
	in := map[string]any{
		"_id":         json.Number("11"),
		"group_id":    json.Number("12"),
		"projectid":   json.Number("15"),
		"member_uids": []any{json.Number("13"), json.Number("14")},
		"index":       json.Number("3"),
		"add_time":    json.Number("1793764954"),
		"upTime":      json.Number("1793764954"),
		"token":       "abc",
		"env":         []any{map[string]any{"_id": "65f0c0ffee0ddba11c0ffee0"}},
		"desc":        nil,
	}
	got := Normalize(in).(map[string]any)
	if got["_id"] != "<id>" || got["group_id"] != "<id>" || got["projectid"] != "<id>" {
		t.Errorf("数字 ID 应替换为占位符: %#v", got)
	}
	if ids := got["member_uids"].([]any); ids[0] != "<id>" || ids[1] != "<id>" {
		t.Errorf("ID 数组的每一项都应替换: %#v", ids)
	}
	if got["index"] != json.Number("3") {
		t.Error("非 ID 的数字不应被替换")
	}
	if got["add_time"] != "<time>" || got["upTime"] != "<time>" || got["token"] != "<token>" {
		t.Errorf("易变字段应替换为占位符: %#v", got)
	}
	if got["env"].([]any)[0].(map[string]any)["_id"] != "<objectid>" {
		t.Error("ObjectId 应替换为占位符")
	}
	if got["desc"] != nil {
		t.Error("null 应保持不变")
	}
}

// fakeYApi 模拟登录后才能访问的接口，用来验证 Cookie、变量捕获和重复步骤。
func fakeYApi() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "_yapi_token", Value: "t"})
		_, _ = fmt.Fprint(w, `{"errcode":0,"data":{"_id":11,"add_time":1793764954}}`)
	})
	mux.HandleFunc("GET /group", func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie("_yapi_token"); err != nil {
			_, _ = fmt.Fprint(w, `{"errcode":40011,"errmsg":"请登录..."}`)
			return
		}
		_, _ = fmt.Fprintf(w, `{"errcode":0,"data":{"id":%s}}`, r.URL.Query().Get("id"))
	})
	mux.HandleFunc("GET /item/{n}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"errcode":0,"data":"%s"}`, r.PathValue("n"))
	})
	return httptest.NewServer(mux)
}

func TestRun(t *testing.T) {
	srv := fakeYApi()
	defer srv.Close()

	sc := Scenario{Name: "demo", Steps: []Step{
		{Name: "未登录", Method: "GET", Path: "/group", Query: map[string]string{"id": "1"}, Errcode: 40011},
		{Name: "登录", Method: "POST", Path: "/login", Body: map[string]any{"email": "a@b.c"}, Capture: map[string]string{"gid": "data._id"}},
		{Name: "查询", Method: "GET", Path: "/group", Query: map[string]string{"id": "{{gid}}"}},
		{Name: "批量", Method: "GET", Path: "/item/{{i}}", Repeat: 2},
	}}
	got, err := Run(context.Background(), srv.URL, sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("录制结果数 = %d, 期望 5（重复步骤展开为 2 条）", len(got))
	}
	if got[1].Response.(map[string]any)["data"].(map[string]any)["add_time"] != "<time>" {
		t.Error("录制结果应已归一化")
	}
	if got[2].URL != "/group?id=11" {
		t.Errorf("捕获的变量应替换到查询参数里, 实际 %s", got[2].URL)
	}
	if got[4].Step != "批量 #2" || got[4].URL != "/item/2" {
		t.Errorf("重复步骤展开不对: %+v", got[4])
	}
}

func TestRunErrcodeMismatch(t *testing.T) {
	srv := fakeYApi()
	defer srv.Close()

	sc := Scenario{Name: "demo", Steps: []Step{{Name: "未登录", Method: "GET", Path: "/group"}}}
	got, err := Run(context.Background(), srv.URL, sc)
	if err == nil || !strings.Contains(err.Error(), "40011") {
		t.Fatalf("errcode 不符时应报错, 实际 %v", err)
	}
	if len(got) != 0 {
		t.Error("出错的步骤不应计入结果")
	}
}

func TestDiff(t *testing.T) {
	golden := []Exchange{{Step: "s1", Status: 200, Response: map[string]any{
		"errcode": json.Number("0"),
		"data":    map[string]any{"name": "a", "list": []any{json.Number("1"), json.Number("2")}},
	}}}

	same := []Exchange{{Step: "s1", Status: 200, Response: map[string]any{
		"errcode": 0,
		"data":    map[string]any{"name": "a", "list": []any{1, 2}},
	}}}
	if d, err := Diff(golden, same); err != nil || len(d) != 0 {
		t.Errorf("相同结果不应有差异: %v %v", d, err)
	}

	changed := []Exchange{{Step: "s1", Status: 500, Response: map[string]any{
		"errcode": 0,
		"data":    map[string]any{"name": "b", "list": []any{1}, "extra": true},
	}}}
	d, err := Diff(golden, changed)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"HTTP 状态码", "$.data.extra: 多出字段", "$.data.list: 数组长度", `$.data.name: 期望 "a", 实际 "b"`}
	for _, w := range want {
		if !strings.Contains(strings.Join(d, "\n"), w) {
			t.Errorf("差异中缺少 %q, 实际:\n%s", w, strings.Join(d, "\n"))
		}
	}

	if d, _ := Diff(golden, nil); len(d) != 1 || !strings.Contains(d[0], "缺少步骤") {
		t.Errorf("缺少步骤应被报告: %v", d)
	}
}

func TestGoldenRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "golden", "demo.json")
	ex := []Exchange{{Step: "s", Method: "GET", URL: "/x", Status: 200,
		Response: map[string]any{"html": "<p>中文</p>", "n": json.Number("11")}}}

	if err := WriteGolden(path, ex); err != nil {
		t.Fatal(err)
	}
	back, err := ReadGolden(path)
	if err != nil {
		t.Fatal(err)
	}
	if d, err := Diff(back, ex); err != nil || len(d) != 0 {
		t.Errorf("写入再读出后应与原结果一致: %v %v", d, err)
	}
}

func TestLoadScenarios(t *testing.T) {
	scenarios, err := LoadScenarios(filepath.Join("..", "..", "testdata", "compat", "scenarios"))
	if err != nil {
		t.Fatal(err)
	}
	if len(scenarios) == 0 {
		t.Fatal("没有读到任何场景")
	}
	for i := 1; i < len(scenarios); i++ {
		if scenarios[i-1].Name >= scenarios[i].Name {
			t.Errorf("场景应按文件名排序: %s 在 %s 之前", scenarios[i-1].Name, scenarios[i].Name)
		}
	}
}
