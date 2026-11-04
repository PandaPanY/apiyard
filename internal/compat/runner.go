package compat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Exchange 是一次接口调用的录制结果，也是 golden 文件的组成单位。
// URL 和 Request 记录的是场景里的模板（保留 {{变量}}），不是替换后的值：
// 变量多是每次运行都不同的 ID，记录模板能让重新录制时 golden 文件保持不变。比对只看 Status 和 Response。
type Exchange struct {
	Step     string `json:"step"`
	Method   string `json:"method"`
	URL      string `json:"url"`
	Request  any    `json:"request,omitempty"`
	Status   int    `json:"status"`
	Response any    `json:"response"` // 已归一化；非 JSON 响应保存为字符串
}

// Run 在 baseURL 上依次执行场景的每一步。
// 出错时返回已完成的结果和错误：后续步骤可能依赖出错步骤捕获的变量，无法继续。
func Run(ctx context.Context, baseURL string, sc Scenario) ([]Exchange, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	r := &runner{
		base:   strings.TrimSuffix(baseURL, "/"),
		client: &http.Client{Jar: jar, Timeout: 30 * time.Second},
		vars:   map[string]any{},
	}

	var out []Exchange
	for _, st := range sc.Steps {
		times := max(st.Repeat, 1)
		for i := 1; i <= times; i++ {
			name := st.Name
			if st.Repeat > 0 {
				r.vars["i"] = i
				name = fmt.Sprintf("%s #%d", st.Name, i)
			}
			ex, err := r.do(ctx, st, name)
			if err != nil {
				return out, fmt.Errorf("场景 %s 步骤「%s」: %w", sc.Name, name, err)
			}
			out = append(out, ex)
		}
	}
	return out, nil
}

type runner struct {
	base   string
	client *http.Client
	vars   map[string]any
}

func (r *runner) do(ctx context.Context, st Step, name string) (Exchange, error) {
	target, err := r.target(st)
	if err != nil {
		return Exchange{}, err
	}
	body, err := render(st.Body, r.vars)
	if err != nil {
		return Exchange{}, err
	}

	req, err := r.newRequest(ctx, st.Method, target, body)
	if err != nil {
		return Exchange{}, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return Exchange{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Exchange{}, err
	}

	ex := Exchange{Step: name, Method: st.Method, URL: templateURL(st), Request: st.Body, Status: resp.StatusCode}
	var parsed any
	if decodeJSON(raw, &parsed) != nil {
		text := string(raw)
		for _, rule := range st.TextReplace {
			re, err := regexp.Compile(rule[0])
			if err != nil {
				return ex, fmt.Errorf("textReplace 正则 %q: %w", rule[0], err)
			}
			text = re.ReplaceAllString(text, rule[1])
		}
		ex.Response = text
		return ex, nil
	}
	ex.Response = Normalize(parsed)
	for _, path := range st.Volatile {
		if err := replaceAt(ex.Response, path, "<volatile>"); err != nil {
			return ex, fmt.Errorf("volatile 路径 %s: %w", path, err)
		}
	}
	for _, path := range st.Unordered {
		if err := sortArrayAt(ex.Response, path); err != nil {
			return ex, fmt.Errorf("unordered 路径 %s: %w", path, err)
		}
	}

	if err := checkErrcode(parsed, st.Errcode); err != nil {
		return ex, err
	}
	for varName, path := range st.Capture {
		val, err := lookup(parsed, path)
		if err != nil {
			return ex, fmt.Errorf("捕获变量 %s: %w", varName, err)
		}
		r.vars[varName] = val
	}
	return ex, nil
}

// templateURL 返回未替换变量的路径和查询串，只用于记录；参数按名称排序且不转义，便于阅读。
func templateURL(st Step) string {
	if len(st.Query) == 0 {
		return st.Path
	}
	keys := make([]string, 0, len(st.Query))
	for k := range st.Query {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, len(keys))
	for i, k := range keys {
		pairs[i] = k + "=" + st.Query[k]
	}
	return st.Path + "?" + strings.Join(pairs, "&")
}

// target 返回替换过变量的路径和查询串。
func (r *runner) target(st Step) (string, error) {
	p, err := renderText(st.Path, r.vars)
	if err != nil {
		return "", err
	}
	if len(st.Query) == 0 {
		return p, nil
	}
	q := url.Values{}
	for k, v := range st.Query {
		val, err := renderText(v, r.vars)
		if err != nil {
			return "", err
		}
		q.Set(k, val)
	}
	return p + "?" + q.Encode(), nil
}

func (r *runner) newRequest(ctx context.Context, method, target string, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.base+target, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// checkErrcode 校验 YApi 响应体里的业务错误码；没有 errcode 字段的响应（如 Mock）不校验。
func checkErrcode(resp any, want int) error {
	m, ok := resp.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := m["errcode"]
	if !ok {
		return nil
	}
	got, err := strconv.Atoi(fmt.Sprint(raw))
	if err != nil {
		return fmt.Errorf("errcode 不是整数: %v", raw)
	}
	if got != want {
		return fmt.Errorf("errcode = %d（%v），期望 %d", got, m["errmsg"], want)
	}
	return nil
}
