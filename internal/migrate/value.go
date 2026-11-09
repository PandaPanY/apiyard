package migrate

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/PandaPanY/apiyard/internal/jsjson"
	"github.com/PandaPanY/apiyard/internal/store"
)

// doc 包装一条 MongoDB 文档，按 YApi 的 Mongoose schema 读取字段，并记下读过的字段：没读过的字段存进 extra，迁移不丢数据。
// 类型不对的字段按 Mongoose 的转换规则处理（例如字符串 "12" 读作数字 12），转换不了时用默认值并记入报告。
type doc struct {
	coll string
	id   any // 用于报告
	vals map[string]any
	keys []string // 原文档的字段顺序
	used map[string]bool
	rep  *Report
}

func newDoc(coll string, d bson.D, rep *Report) *doc {
	x := &doc{coll: coll, vals: make(map[string]any, len(d)), used: map[string]bool{"_id": true, "__v": true}, rep: rep}
	for _, e := range d {
		if _, dup := x.vals[e.Key]; !dup {
			x.keys = append(x.keys, e.Key)
		}
		x.vals[e.Key] = e.Value
	}
	x.id = x.vals["_id"]
	return x
}

// get 读取字段；字段不存在或为 null 时 ok 为 false。
func (d *doc) get(key string) (any, bool) {
	d.used[key] = true
	v, ok := d.vals[key]
	if !ok || v == nil {
		return nil, false
	}
	if _, undefined := v.(primitive.Undefined); undefined {
		return nil, false
	}
	return v, true
}

// present 读取字段，区分“不存在”和“值为 null”：ok 为 false 表示字段不存在。
func (d *doc) present(key string) (any, bool) {
	d.used[key] = true
	v, ok := d.vals[key]
	return v, ok
}

// fix 记录一处修正。
func (d *doc) fix(format string, args ...any) {
	d.rep.fixed(d.coll, fmt.Sprintf(format, args...), d.id)
}

// intID 读取整数主键 _id。
func (d *doc) intID() (int64, bool) {
	n, ok := toInt(d.vals["_id"])
	return n, ok && n > 0
}

func (d *doc) int(key string, def int64) int64 {
	v, ok := d.get(key)
	if !ok {
		return def
	}
	n, ok := toInt(v)
	if !ok {
		d.fix("%s 不是数字（%s），已改为 %d", key, brief(v), def)
		return def
	}
	return n
}

func (d *doc) bool(key string, def bool) bool {
	v, ok := d.get(key)
	if !ok {
		return def
	}
	b, ok := toBool(v)
	if !ok {
		d.fix("%s 不是布尔值（%s），已改为 %t", key, brief(v), def)
		return def
	}
	return b
}

// str 读取可选文本字段；不存在或为 null 时返回 nil（ApiYard 响应里同样不输出这个字段）。
func (d *doc) str(key string) *string {
	v, ok := d.get(key)
	if !ok {
		return nil
	}
	s, ok := toStr(v)
	if !ok {
		text := jsonText(v)
		d.fix("%s 不是字符串，已转成 JSON 文本", key)
		return &text
	}
	return &s
}

func (d *doc) strOr(key, def string) string {
	if s := d.str(key); s != nil {
		return *s
	}
	return def
}

// enum 读取取值受限的字段：不存在时用 def（为 nil 表示存成 NULL），取值不在 allowed 里时同样用 def 并记入报告。
func (d *doc) enum(key string, allowed []string, def *string) *string {
	s := d.str(key)
	if s == nil {
		return def
	}
	for _, a := range allowed {
		if *s == a {
			return s
		}
	}
	if def == nil {
		d.fix("%s 的取值 %q 无效，已清空", key, *s)
	} else {
		d.fix("%s 的取值 %q 无效，已改为 %q", key, *s, *def)
	}
	return def
}

// array 读取数组字段并转成 JSON 文本；不存在时为 "[]"（与 Mongoose 对数组字段的默认值相同）。
// defaults 给数组里每个对象补上缺少的字段，对应 schema 里子文档字段的默认值。
func (d *doc) array(key string, defaults ...any) string {
	v, ok := d.get(key)
	if !ok {
		return "[]"
	}
	list, ok := v.(primitive.A)
	if !ok {
		d.fix("%s 不是数组，已改为 []", key)
		return "[]"
	}
	js := toJS(list).([]any)
	for _, item := range js {
		if obj, ok := item.(*jsjson.Object); ok {
			for i := 0; i+1 < len(defaults); i += 2 {
				name := defaults[i].(string)
				if _, has := obj.Get(name); !has {
					obj.Set(name, defaults[i+1])
				}
			}
		}
	}
	return jsonText(js)
}

// object 读取嵌套对象字段并转成 JSON 文本，不存在时为 def。
func (d *doc) object(key, def string) string {
	v, ok := d.get(key)
	if !ok {
		return def
	}
	if _, ok := v.(primitive.D); !ok {
		d.fix("%s 不是对象，已改为默认值", key)
		return def
	}
	return jsonText(v)
}

// mixed 读取任意类型的字段（Mongoose 的 Mixed）并转成 JSON 文本；不存在时为 nil。
func (d *doc) mixed(key string) *string {
	v, ok := d.get(key)
	if !ok {
		return nil
	}
	text := jsonText(v)
	return &text
}

// extra 把没有读过的字段按原顺序转成 JSON 对象文本。
func (d *doc) extra() string {
	obj := jsjson.NewObject()
	for _, k := range d.keys {
		if !d.used[k] {
			obj.Set(k, toJS(d.vals[k]))
		}
	}
	return jsonText(obj)
}

// ---- 值的转换 ----

// toInt 按 Mongoose 的 Number 规则把值转成整数：数字、数字字符串、布尔值都可以，小数和其他类型不行。
func toInt(v any) (int64, bool) {
	var f float64
	switch x := v.(type) {
	case int: // 只在测试里直接构造文档时出现，解码出来的数字是 int32/int64/float64
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case float64:
		f = x
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0, false
		}
		var err error
		if f, err = strconv.ParseFloat(s, 64); err != nil {
			return 0, false
		}
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case primitive.DateTime: // Mongoose 把日期转成毫秒数
		return int64(x), true
	default:
		return 0, false
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) || math.Abs(f) > 1<<53 {
		return 0, false
	}
	return int64(f), true
}

// toBool 按 Mongoose 的 Boolean 规则转换：true/1/"true"/"1"/"yes" 为真，false/0/"false"/"0"/"no" 为假。
func toBool(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case int, int32, int64, float64:
		n, ok := toInt(x)
		if ok && (n == 0 || n == 1) {
			return n == 1, true
		}
	case string:
		switch x {
		case "true", "1", "yes":
			return true, true
		case "false", "0", "no":
			return false, true
		}
	}
	return false, false
}

// toStr 按 Mongoose 的 String 规则转换标量；对象和数组不行。
func toStr(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case int32:
		return strconv.FormatInt(int64(x), 10), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case float64:
		return jsjson.FormatNumber(x), true
	case bool:
		return strconv.FormatBool(x), true
	case primitive.ObjectID:
		return x.Hex(), true
	case primitive.DateTime:
		return isoTime(x), true
	}
	return "", false
}

// toJS 把 BSON 值转成 jsjson 的值，结果与 YApi 把文档输出成 JSON 时相同：
// ObjectId 为 24 位十六进制字符串，日期为 ISO 8601 字符串，对象保持字段顺序。
func toJS(v any) any {
	switch x := v.(type) {
	case nil, primitive.Undefined, primitive.Null:
		return nil
	case primitive.D:
		obj := jsjson.NewObject()
		for _, e := range x {
			obj.Set(e.Key, toJS(e.Value))
		}
		return obj
	case primitive.M:
		obj := jsjson.NewObject()
		for k, val := range x {
			obj.Set(k, toJS(val))
		}
		return obj
	case primitive.A:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = toJS(item)
		}
		return out
	case string, bool:
		return x
	case int:
		return json.Number(strconv.Itoa(x))
	case int32:
		return json.Number(strconv.FormatInt(int64(x), 10))
	case int64:
		return json.Number(strconv.FormatInt(x, 10))
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil // JSON.stringify 的结果
		}
		return json.Number(jsjson.FormatNumber(x))
	case primitive.ObjectID:
		return x.Hex()
	case primitive.DateTime:
		return isoTime(x)
	case primitive.Decimal128:
		return x.String()
	case primitive.Binary:
		return base64.StdEncoding.EncodeToString(x.Data)
	default:
		return fmt.Sprint(x)
	}
}

func isoTime(t primitive.DateTime) string {
	return time.UnixMilli(int64(t)).UTC().Format("2006-01-02T15:04:05.000Z")
}

// jsonText 把 BSON 值或 jsjson 值序列化成紧凑的 JSON 文本。
func jsonText(v any) string {
	switch v.(type) {
	case *jsjson.Object, []any:
	default:
		v = toJS(v)
	}
	data, err := jsjson.Marshal(v, "")
	if err != nil { // jsjson 的值总能序列化，这里只是兜底
		return "null"
	}
	return string(data)
}

// brief 用于报告里显示一个值，过长时截断。
func brief(v any) string {
	s := jsonText(v)
	if r := []rune(s); len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return s
}

// decodeBase64 与 Node 的 Buffer.from(s, 'base64') 一样宽松：忽略空白，补齐缺少的 "="，也接受 URL 安全的字母表。
func decodeBase64(s string) ([]byte, error) {
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, s)
	s = strings.TrimRight(s, "=")
	s = strings.NewReplacer("-", "+", "_", "/").Replace(s)
	return base64.RawStdEncoding.DecodeString(s)
}

// subID 读取子文档的 _id，没有时生成一个新的。
func subID(v any) string {
	if oid, ok := v.(primitive.ObjectID); ok {
		return oid.Hex()
	}
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return store.NewObjectID()
}

func ptr(s string) *string { return &s }
