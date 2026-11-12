package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "import", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// importEnv 准备好项目 P（已有一个名为 pet 的分类）和管理员的 token。
type importEnv struct {
	*openEnv
	tok string
}

func newImportEnv(t *testing.T) *importEnv {
	t.Helper()
	e := &importEnv{openEnv: newOpenEnv(t)}
	e.tok = e.admin.call("GET", "/api/project/token?project_id="+q(e.pid), nil)["data"].(string)
	expectCode(t, e.admin.call("POST", "/api/interface/add_cat", map[string]any{"name": "pet", "project_id": e.pid, "desc": "已有的宠物分类"}), 0)
	return e
}

func (e *importEnv) importDoc(doc string, extra map[string]any) map[string]any {
	body := map[string]any{"type": "swagger", "token": e.tok, "json": doc}
	for k, v := range extra {
		body[k] = v
	}
	return e.anon.call("POST", "/api/open/import_data", body)
}

// interfacesByRoute 读出项目里的全部接口详情，按 "METHOD path" 索引。
func (e *importEnv) interfacesByRoute(t *testing.T) map[string]map[string]any {
	t.Helper()
	list := e.admin.call("GET", "/api/interface/list?limit=all&project_id="+q(e.pid), nil)["data"].(map[string]any)["list"].([]any)
	out := map[string]map[string]any{}
	for _, item := range list {
		id := item.(map[string]any)["_id"].(float64)
		it := e.admin.call("GET", "/api/interface/get?id="+q(id), nil)["data"].(map[string]any)
		out[it["method"].(string)+" "+it["path"].(string)] = it
	}
	return out
}

func expectMessage(t *testing.T, r map[string]any, want string) {
	t.Helper()
	expectCode(t, r, 0)
	if r["errmsg"] != want {
		t.Errorf("提示 = %v, 期望 %s", r["errmsg"], want)
	}
}

func TestImportSwagger2(t *testing.T) {
	e := newImportEnv(t)
	doc := readFixture(t, "swagger2.json")
	expectMessage(t, e.importDoc(doc, nil), "成功导入接口 5 个, 已存在的接口 0 个")

	// 写入的接口与原版 run.js 的转换结果一致
	var oracle struct {
		APIs []map[string]any `json:"apis"`
	}
	if err := json.Unmarshal([]byte(readFixture(t, "swagger2.yapi.json")), &oracle); err != nil {
		t.Fatal(err)
	}
	got := e.interfacesByRoute(t)
	if len(got) != len(oracle.APIs) {
		t.Fatalf("导入了 %d 个接口, 期望 %d", len(got), len(oracle.APIs))
	}
	for _, want := range oracle.APIs {
		it := got[want["method"].(string)+" "+want["path"].(string)]
		if it == nil {
			t.Errorf("缺少接口 %v %v", want["method"], want["path"])
			continue
		}
		for _, k := range []string{"title", "desc", "req_body_other", "res_body", "req_body_type", "res_body_type"} {
			if w, ok := want[k]; ok && it[k] != w {
				t.Errorf("%v %v 的 %s = %#v, 期望 %#v", want["method"], want["path"], k, it[k], w)
			}
		}
	}
	if it := got["POST /pet/{petId}/uploadImage"]; it["req_body_form"].([]any)[1].(map[string]any)["example"] != "备注" {
		t.Errorf("表单参数不对: %v", it["req_body_form"])
	}

	// 分类：pet 按名称复用，store 新建，没有标签的接口放进第一个分类；basePath 写入项目
	menu := e.admin.call("GET", "/api/interface/list_menu?project_id="+q(e.pid), nil)["data"].([]any)
	counts := map[string]int{}
	for _, c := range menu {
		cat := c.(map[string]any)
		counts[cat["name"].(string)] = len(cat["list"].([]any))
		if cat["name"] == "pet" && cat["desc"] != "已有的宠物分类" {
			t.Errorf("已有分类不应被修改: %v", cat)
		}
	}
	if counts["pet"] != 3 || counts["store"] != 1 || counts["公共分类"] != 1 {
		t.Errorf("各分类的接口数不对: %v", counts)
	}
	if bp := e.admin.call("GET", "/api/project/get?id="+q(e.pid), nil)["data"].(map[string]any)["basepath"]; bp != "/petstore" {
		t.Errorf("basepath = %v", bp)
	}

	// 普通模式再导入一次：全部已存在
	expectMessage(t, e.importDoc(doc, nil), "成功导入接口 0 个, 已存在的接口 5 个")
	// 旧参数名 dataSync 在原版中被参数校验去掉了，不起作用，仍按普通模式
	expectMessage(t, e.importDoc(doc, map[string]any{"dataSync": "merge"}), "成功导入接口 0 个, 已存在的接口 5 个")
}

func TestImportSyncModes(t *testing.T) {
	e := newImportEnv(t)
	expectMessage(t, e.importDoc(readFixture(t, "swagger2.json"), nil), "成功导入接口 5 个, 已存在的接口 0 个")

	// 修改后的文档：Pet 去掉 name 的描述，增加 age
	var doc map[string]any
	_ = json.Unmarshal([]byte(readFixture(t, "swagger2.json")), &doc)
	props := doc["definitions"].(map[string]any)["Pet"].(map[string]any)["properties"].(map[string]any)
	props["name"] = map[string]any{"type": "string"}
	props["age"] = map[string]any{"type": "integer"}
	changed, _ := json.Marshal(doc)

	listBody := func() string {
		return e.interfacesByRoute(t)["GET /pet"]["res_body"].(string)
	}
	// 智能合并：保留原有字段上的描述，加入新字段
	expectMessage(t, e.importDoc(string(changed), map[string]any{"merge": "good"}), "成功导入接口 5 个, 已存在的接口 5 个")
	if body := listBody(); !strings.Contains(body, "名字") || !strings.Contains(body, `"age"`) {
		t.Errorf("智能合并后的响应体不对: %s", body)
	}
	// 完全覆盖：以新文档为准
	expectMessage(t, e.importDoc(string(changed), map[string]any{"merge": "merge"}), "成功导入接口 5 个, 已存在的接口 5 个")
	if body := listBody(); strings.Contains(body, "名字") || !strings.Contains(body, `"age"`) {
		t.Errorf("完全覆盖后的响应体不对: %s", body)
	}
}

func TestImportOpenAPI3RoundTrip(t *testing.T) {
	e := newImportEnv(t)
	expectMessage(t, e.importDoc(readFixture(t, "openapi3.json"), nil), "成功导入接口 3 个, 已存在的接口 0 个")

	// 导入后再导出 Swagger，主要信息不丢失
	_, exported := e.admin.download("/api/plugin/exportSwagger?type=OpenAPIV2&pid=" + q(e.pid))
	var sw struct {
		Tags  []map[string]any                     `json:"tags"`
		Paths map[string]map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal([]byte(exported), &sw); err != nil {
		t.Fatal(err)
	}
	post := sw.Paths["/users"]["post"]
	if post["summary"] != "创建用户" {
		t.Errorf("POST /users 的名称不对: %v", post)
	}
	schema := post["parameters"].([]any)[0].(map[string]any)["schema"].(map[string]any)
	if email := schema["properties"].(map[string]any)["email"].(map[string]any); email["format"] != "email" {
		t.Errorf("请求体 Schema 丢失了信息: %v", schema)
	}
	if res := post["responses"].(map[string]any)["200"].(map[string]any)["schema"].(map[string]any); res["type"] != "object" {
		t.Errorf("响应 Schema 丢失了信息: %v", res)
	}
	if sw.Paths["/health"]["get"]["summary"] != "健康检查" || len(sw.Tags) != 2 {
		t.Errorf("接口或分类缺失: %v %v", sw.Paths, sw.Tags)
	}
}

func TestImportErrors(t *testing.T) {
	e := newImportEnv(t)
	doc := readFixture(t, "swagger2.json")
	expectCode(t, e.importDoc("", map[string]any{"url": "http://127.0.0.1/swagger.json"}), 40022)
	expectCode(t, e.importDoc("https://example.com/swagger.json", nil), 40022)
	expectCode(t, e.importDoc(`{"swagger":"2.0","paths":{}}`, nil), 404) // 解析数据为空
	expectCode(t, e.anon.call("POST", "/api/open/import_data", map[string]any{"type": "swagger", "token": "garbage", "json": doc}), 40011)
	// token 必填（参数校验）
	expectCode(t, e.admin.call("POST", "/api/open/import_data", map[string]any{"type": "swagger", "json": doc}), 400)

	// 开发者的 token 可以导入接口，但没有权限修改项目，basePath 不会写入
	devUID := e.dev.call("GET", "/api/user/status", nil)["data"].(map[string]any)["_id"]
	e.tok = tokenFor(t, devUID, e.rawP)
	expectMessage(t, e.importDoc(doc, nil), "成功导入接口 5 个, 已存在的接口 0 个")
	if bp := e.admin.call("GET", "/api/project/get?id="+q(e.pid), nil)["data"].(map[string]any)["basepath"]; bp == "/petstore" {
		t.Error("开发者不应能修改项目的 basepath")
	}
}
