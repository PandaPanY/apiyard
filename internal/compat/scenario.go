// Package compat 实现 ApiYard 与原版 YApi 的接口兼容性测试：
// 按场景依次调用接口，把原版 YApi 的响应录制成 golden 文件，之后用同样的场景请求 ApiYard 并逐字段比对。
package compat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Scenario 是一组按顺序执行的接口调用，每个场景使用独立的 Cookie。
type Scenario struct {
	Name        string `json:"-"` // 取自文件名，也是 golden 文件名
	Description string `json:"description"`
	Steps       []Step `json:"steps"`
}

// Step 是一次接口调用。Path、Query、Body 中的 {{变量}} 会被替换为前面步骤捕获的值。
type Step struct {
	Name   string            `json:"name"`
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Query  map[string]string `json:"query,omitempty"`
	Body   any               `json:"body,omitempty"`
	// Capture 按点分路径从响应体取值并保存为变量，如 {"gid": "data._id"}、{"uid": "data.list.0.uid"}。
	Capture map[string]string `json:"capture,omitempty"`
	// Repeat 大于 0 时把该步骤执行 Repeat 次，变量 {{i}} 依次为 1..Repeat。
	Repeat int `json:"repeat,omitempty"`
	// Errcode 是期望的 YApi 业务错误码，默认 0 表示成功。
	Errcode int `json:"errcode,omitempty"`
	// Volatile 列出响应中每次运行都不同、又无法按字段名统一处理的值（点分路径），比对前替换为 "<volatile>"。
	// 例如 /api/project/token 直接把随机生成的 token 字符串放在 data 里。
	Volatile []string `json:"volatile,omitempty"`
	// Unordered 列出顺序不确定的数组（点分路径），比对前按元素内容排序，只比较有哪些元素。
	// 例如 YApi 的动态按秒级时间排序，同一秒内的多条动态每次查询顺序都可能不同。
	Unordered []string `json:"unordered,omitempty"`
	// TextReplace 只用于不是 JSON 的响应（如 Markdown 导出）：按正则把每次运行都不同的片段替换掉，
	// 每项是 [正则, 替换文本]，替换文本可以用 $1 引用分组。例如把锚点里拼接的分类 ID 换成 <id>。
	TextReplace [][2]string `json:"textReplace,omitempty"`
}

// LoadScenarios 按文件名顺序读取 dir 下的全部场景；场景之间可能有状态依赖，顺序有意义。
func LoadScenarios(dir string) ([]Scenario, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	scenarios := make([]Scenario, 0, len(paths))
	for _, p := range paths {
		sc, err := loadScenario(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		scenarios = append(scenarios, sc)
	}
	return scenarios, nil
}

func loadScenario(path string) (Scenario, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- 场景文件路径来自开发者指定的目录
	if err != nil {
		return Scenario{}, err
	}
	var sc Scenario
	if err := decodeJSON(data, &sc); err != nil {
		return Scenario{}, err
	}
	sc.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	for i, st := range sc.Steps {
		if st.Name == "" || st.Method == "" || st.Path == "" {
			return Scenario{}, fmt.Errorf("第 %d 步缺少 name、method 或 path", i+1)
		}
	}
	return sc, nil
}

// decodeJSON 解码时保留数字的原始写法（json.Number），避免整数 ID 变成浮点数。
func decodeJSON(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}
