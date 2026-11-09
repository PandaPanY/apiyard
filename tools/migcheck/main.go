// Command migcheck 核对迁移结果：用同样的请求遍历原版 YApi 和迁移后的 ApiYard，逐条对比响应。
// 迁移保留了全部 ID，所以两边的响应应当完全相同，不需要像兼容测试那样忽略 ID。
//
// 用法（先用 apiyard migrate 迁移，再用 -token-salt 启动 ApiYard）：
//
//	go run ./tools/migcheck -yapi http://localhost:13000 -yard http://localhost:3101
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/PandaPanY/apiyard/internal/compat"
)

func main() {
	yapiURL := flag.String("yapi", "http://localhost:13000", "原版 YApi 的地址")
	yardURL := flag.String("yard", "http://localhost:3101", "迁移后的 ApiYard 的地址")
	email := flag.String("email", "admin@admin.com", "两边都用这个账号登录（管理员能看到全部数据）")
	password := flag.String("password", "ymfe.org", "登录密码；迁移后原密码应当仍然可用")
	verbose := flag.Bool("v", false, "列出每一个一致的请求")
	flag.Parse()

	c := &checker{verbose: *verbose}
	var err error
	if c.yapi, err = login(*yapiURL, *email, *password); err == nil {
		c.yard, err = login(*yardURL, *email, *password)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
	c.crawl()
	fmt.Printf("\n共对比 %d 个请求，%d 个不一致\n", c.total, c.failed)
	if c.failed > 0 {
		os.Exit(1)
	}
}

type client struct {
	base string
	http *http.Client
}

func login(base, email, password string) (*client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	c := &client{base: strings.TrimSuffix(base, "/"), http: &http.Client{Jar: jar}}
	_, body, err := c.do("POST", "/api/user/login", map[string]string{"email": email, "password": password})
	if err != nil {
		return nil, err
	}
	var r struct {
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &r); err != nil || r.Errcode != 0 {
		return nil, fmt.Errorf("登录 %s 失败: %s", base, body)
	}
	return c, nil
}

func (c *client) do(method, path string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	return resp.StatusCode, data, err
}

type checker struct {
	yapi, yard    *client
	verbose       bool
	total, failed int
}

// compare 在两边发同样的请求并对比，返回 YApi 的响应（JSON 已解析）供继续遍历。
// 非 JSON 响应（头像、导出的文件）比较内容的摘要。
func (c *checker) compare(path string) any {
	c.total++
	want, err := fetch(c.yapi, path)
	if err == nil && strings.HasPrefix(path, "/api/log/list?") {
		sortLogs(want.Response)
	}
	if err != nil {
		c.fail(path, "请求原版 YApi 失败: "+err.Error())
		return nil
	}
	got, err := fetch(c.yard, path)
	if err != nil {
		c.fail(path, "请求 ApiYard 失败: "+err.Error())
		return want.Response
	}
	if strings.HasPrefix(path, "/api/log/list?") {
		sortLogs(got.Response)
	}
	diffs, err := compat.Diff([]compat.Exchange{want}, []compat.Exchange{got})
	if err != nil {
		c.fail(path, err.Error())
	} else if len(diffs) > 0 {
		c.fail(path, strings.Join(diffs, "\n    "))
	} else if c.verbose {
		fmt.Println("✓", path)
	}
	return want.Response
}

func (c *checker) fail(path, msg string) {
	c.failed++
	fmt.Printf("✗ %s\n    %s\n", path, msg)
}

func fetch(cl *client, path string) (compat.Exchange, error) {
	status, body, err := cl.do("GET", path, nil)
	if err != nil {
		return compat.Exchange{}, err
	}
	ex := compat.Exchange{Step: path, Status: status}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber() // 与 compat.Diff 读取 golden 文件的方式一致
	if err := dec.Decode(&ex.Response); err != nil {
		sum := sha256.Sum256(body)
		ex.Response = fmt.Sprintf("非 JSON 内容 %d 字节，sha256 %s", len(body), hex.EncodeToString(sum[:8]))
	}
	return ex, nil
}

// sortLogs 把动态列表按时间倒序、同一秒内按 ID 排好。YApi 只按秒级时间排序，分组动态同一秒内的先后取决于
// MongoDB 的执行计划（见 internal/store/logs.go），所以只核对每一秒里有哪些动态，不核对它们的先后。
// 分页时同一秒的动态跨页就核对不了，因此调用时 limit 取得足够大。
func sortLogs(resp any) {
	r, _ := resp.(map[string]any)
	data, _ := r["data"].(map[string]any)
	items, _ := data["list"].([]any)
	key := func(i int) (string, string) {
		m, _ := items[i].(map[string]any)
		return fmt.Sprint(m["add_time"]), fmt.Sprint(m["_id"])
	}
	sort.SliceStable(items, func(i, j int) bool {
		ti, ii := key(i)
		tj, ij := key(j)
		if ti != tj {
			return numLess(tj, ti)
		}
		return numLess(ii, ij)
	})
}

// numLess 比较两个十进制整数字符串。
func numLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

// ---- 遍历 ----

func (c *checker) crawl() {
	c.compare("/api/user/status")
	for _, u := range list(c.compare("/api/user/list?page=1&limit=10000"), "list") {
		uid := id(u)
		c.compare("/api/user/find?id=" + uid)
		c.compare("/api/user/avatar?uid=" + uid)
		c.compare("/api/user/project?type=user&id=" + uid)
	}
	c.compare("/api/follow/list")
	c.compare("/api/group/get_mygroup")

	var projects []string
	for _, g := range list(c.compare("/api/group/list"), "") {
		gid := id(g)
		c.compare("/api/group/get?id=" + gid)
		c.compare("/api/group/get_member_list?id=" + gid)
		c.compare("/api/log/list?type=group&typeid=" + gid + "&page=1&limit=10000")
		for _, p := range list(c.compare("/api/project/list?group_id="+gid), "list") {
			projects = append(projects, id(p))
		}
	}
	sort.Strings(projects)
	for _, pid := range projects {
		c.project(pid)
	}
}

func (c *checker) project(pid string) {
	c.compare("/api/project/get?id=" + pid)
	c.compare("/api/project/get_member_list?id=" + pid)
	c.compare("/api/project/get_env?project_id=" + pid)
	c.compare("/api/user/project?type=project&id=" + pid)
	c.compare("/api/log/list?type=project&typeid=" + pid + "&page=1&limit=10000")
	c.compare("/api/interface/list?project_id=" + pid + "&page=1&limit=10000")
	c.compare("/api/interface/list_menu?project_id=" + pid)
	// HTML 导出用的渲染器与 YApi 不同，本来就不逐字一致（见 docs/architecture.md），这里不比
	for _, typ := range []string{"json", "markdown"} {
		c.compare("/api/plugin/export?type=" + typ + "&pid=" + pid + "&status=all&isWiki=false")
	}
	c.compare("/api/plugin/exportSwagger?type=OpenAPIV2&pid=" + pid + "&status=all&isWiki=false")
	for _, cat := range list(c.compare("/api/interface/getCatMenu?project_id="+pid), "") {
		c.compare("/api/interface/list_cat?catid=" + id(cat) + "&page=1&limit=10000")
	}
	for _, it := range list(c.compare("/api/interface/list?project_id="+pid+"&page=1&limit=10000"), "list") {
		iid := id(it)
		c.compare("/api/interface/get?id=" + iid)
		c.compare("/api/user/project?type=interface&id=" + iid)
	}

	// 原来的 token 应当继续可用：用 YApi 生成的 token 调用开放 API
	token, err := c.yapiToken(pid)
	if err != nil {
		c.total++
		c.fail("/api/project/token?project_id="+pid, err.Error())
		return
	}
	q := "token=" + url.QueryEscape(token)
	c.compare("/api/project/get?" + q)
	c.compare("/api/interface/list_menu?" + q)
	c.compare("/api/interface/list?page=1&limit=10000&" + q)
}

// yapiToken 读取 YApi 生成的项目 token，并对比两边是否相同。
func (c *checker) yapiToken(pid string) (string, error) {
	r, ok := c.compare("/api/project/token?project_id=" + pid).(map[string]any)
	if !ok {
		return "", errors.New("读取 token 失败")
	}
	token, ok := r["data"].(string)
	if !ok {
		return "", fmt.Errorf("读取 token 失败: %v", r)
	}
	return token, nil
}

// list 取出响应里的数组：key 为空时是 data 本身，否则是 data[key]。
func list(resp any, key string) []map[string]any {
	r, _ := resp.(map[string]any)
	data := r["data"]
	if key != "" {
		m, _ := data.(map[string]any)
		data = m[key]
	}
	items, _ := data.([]any)
	var out []map[string]any
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func id(m map[string]any) string {
	return fmt.Sprint(m["_id"])
}
