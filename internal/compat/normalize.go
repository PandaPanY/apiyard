package compat

import (
	"encoding/json"
	"regexp"
	"strings"
)

// volatileKeys 的值每次运行都不同（时间戳、随机 token），比对前替换成占位符。
// 用户搜索接口用的是驼峰写法 addTime/upTime，与其他接口不同。
var volatileKeys = map[string]string{
	"add_time": "<time>",
	"up_time":  "<time>",
	"addTime":  "<time>",
	"upTime":   "<time>",
	"token":    "<token>",
}

// objectIDPattern 匹配 MongoDB 自动生成的 ObjectId（如日志、环境配置的 _id），它们是随机的。
var objectIDPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)

// isIDKey 判断字段是否保存自增 ID。
// YApi 每次进程启动都会给每个集合随机选 1~10 的自增步长，同样的操作得到的 ID 每次不同，
// 所以只比对“这里是一个 ID”；ID 之间的引用关系由场景本身保证：后续步骤用捕获的 ID 继续调用，
// 引用错了会得到错误的数据或错误码。
func isIDKey(k string) bool {
	switch k {
	case "_id", "id", "uid", "catid", "typeid", "projectid": // projectid 是关注列表里的原字段名
		return true
	}
	return strings.HasSuffix(k, "_id") || strings.HasSuffix(k, "_uid") ||
		strings.HasSuffix(k, "_ids") || strings.HasSuffix(k, "_uids")
}

// Normalize 返回 v 的副本，其中易变的值已替换为占位符。
func Normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			switch {
			case item == nil:
				out[k] = nil
			case volatileKeys[k] != "":
				out[k] = volatileKeys[k]
			case isIDKey(k):
				out[k] = normalizeID(item)
			default:
				out[k] = Normalize(item)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = Normalize(item)
		}
		return out
	case string:
		if objectIDPattern.MatchString(x) {
			return "<objectid>"
		}
		return x
	default:
		return v
	}
}

// normalizeID 把数字 ID（或 ID 数组）替换为 "<id>"，其他类型照常归一化。
func normalizeID(v any) any {
	switch x := v.(type) {
	case json.Number, int, int64, float64:
		return "<id>"
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeID(item)
		}
		return out
	default:
		return Normalize(v)
	}
}
