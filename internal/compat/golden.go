package compat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
)

// WriteGolden 把录制结果写成缩进的 JSON，方便在 Git 里查看差异。
func WriteGolden(path string, exchanges []Exchange) error {
	data, err := marshal(exchanges)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// ReadGolden 读取 WriteGolden 写出的文件。
func ReadGolden(path string) ([]Exchange, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- golden 文件路径来自开发者指定的目录
	if err != nil {
		return nil, err
	}
	var out []Exchange
	if err := decodeJSON(data, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

// marshal 不转义 <>&，让 golden 文件里的中文和 HTML 片段保持可读。
func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// maxDiffs 限制每个场景报告的差异数量，避免一个根本问题刷屏。
const maxDiffs = 20

// Diff 比较 golden 与实际结果，返回可读的差异列表；为空表示完全一致。
func Diff(golden, actual []Exchange) ([]string, error) {
	// 实际结果经过一次 JSON 往返，保证和从文件读出的 golden 表示方式一致
	actual, err := roundTrip(actual)
	if err != nil {
		return nil, err
	}

	var diffs []string
	for i := 0; i < max(len(golden), len(actual)) && len(diffs) < maxDiffs; i++ {
		if i >= len(golden) {
			diffs = append(diffs, fmt.Sprintf("多出的步骤「%s」", actual[i].Step))
			continue
		}
		if i >= len(actual) {
			diffs = append(diffs, fmt.Sprintf("缺少步骤「%s」", golden[i].Step))
			continue
		}
		g, a := golden[i], actual[i]
		prefix := fmt.Sprintf("步骤「%s」", g.Step)
		if g.Status != a.Status {
			diffs = append(diffs, fmt.Sprintf("%s HTTP 状态码: 期望 %d, 实际 %d", prefix, g.Status, a.Status))
		}
		for _, h := range unionKeys(stringMap(g.Headers), stringMap(a.Headers)) {
			if g.Headers[h] != a.Headers[h] {
				diffs = append(diffs, fmt.Sprintf("%s 响应头 %s: 期望 %q, 实际 %q", prefix, h, g.Headers[h], a.Headers[h]))
			}
		}
		diffValue(prefix+" $", g.Response, a.Response, &diffs)
	}
	if len(diffs) > maxDiffs {
		diffs = diffs[:maxDiffs]
	}
	return diffs, nil
}

func diffValue(path string, want, got any, diffs *[]string) {
	if len(*diffs) >= maxDiffs {
		return
	}
	wm, wIsMap := want.(map[string]any)
	gm, gIsMap := got.(map[string]any)
	if wIsMap && gIsMap {
		for _, k := range unionKeys(wm, gm) {
			wv, wok := wm[k]
			gv, gok := gm[k]
			switch {
			case !gok:
				*diffs = append(*diffs, fmt.Sprintf("%s.%s: 缺少字段（期望 %s）", path, k, brief(wv)))
			case !wok:
				*diffs = append(*diffs, fmt.Sprintf("%s.%s: 多出字段（实际 %s）", path, k, brief(gv)))
			default:
				diffValue(path+"."+k, wv, gv, diffs)
			}
		}
		return
	}

	wa, wIsArr := want.([]any)
	ga, gIsArr := got.([]any)
	if wIsArr && gIsArr {
		if len(wa) != len(ga) {
			*diffs = append(*diffs, fmt.Sprintf("%s: 数组长度 期望 %d, 实际 %d", path, len(wa), len(ga)))
		}
		for i := 0; i < min(len(wa), len(ga)); i++ {
			diffValue(fmt.Sprintf("%s[%d]", path, i), wa[i], ga[i], diffs)
		}
		return
	}

	if !reflect.DeepEqual(want, got) {
		*diffs = append(*diffs, fmt.Sprintf("%s: 期望 %s, 实际 %s", path, brief(want), brief(got)))
	}
}

func stringMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func unionKeys(a, b map[string]any) []string {
	keys := make([]string, 0, len(a)+len(b))
	for k := range a {
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// brief 把值压成一行，过长时截断。
func brief(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	if s := string(data); len([]rune(s)) <= 80 {
		return s
	}
	return string([]rune(string(data))[:80]) + "…"
}

func roundTrip(exchanges []Exchange) ([]Exchange, error) {
	data, err := marshal(exchanges)
	if err != nil {
		return nil, err
	}
	var out []Exchange
	err = decodeJSON(data, &out)
	return out, err
}
