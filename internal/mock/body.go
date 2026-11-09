package mock

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/PandaPanY/apiyard/internal/jsjson"
)

// request 是 Mock 用到的请求数据，与 Koa 的 ctx.query、ctx.request.body 相同。
type request struct {
	query     *jsjson.Object // 动态路由匹配后会并入路径参数
	body      any            // 没有请求体时为 {}
	multipart bool           // body 为 {fields, files}
}

// 请求体的大小限制与原版的 koa-body 配置相同（jsonLimit 2mb、formLimit 1mb、textLimit 1mb）；multipart 原版不限，这里限制为 10MB。
const (
	jsonLimit      = 2 << 20
	formLimit      = 1 << 20
	textLimit      = 1 << 20
	multipartLimit = 10 << 20
)

type bodyError struct {
	status int
	msg    string
}

func (e *bodyError) Error() string { return e.msg }

func readRequest(r *http.Request) (*request, error) {
	req := &request{query: parseQuery(r.URL.RawQuery), body: jsjson.NewObject()}
	ct, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch {
	case ct == "application/json" || ct == "application/csp-report" || strings.HasSuffix(ct, "+json"):
		text, err := readLimited(r, jsonLimit)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(text) == "" {
			return req, nil
		}
		v, err := jsjson.Parse(text)
		if err != nil {
			return nil, &bodyError{http.StatusBadRequest, "invalid JSON, " + err.Error()}
		}
		req.body = v
	case ct == "application/x-www-form-urlencoded":
		text, err := readLimited(r, formLimit)
		if err != nil {
			return nil, err
		}
		req.body = parseQuery(text)
	case ct == "text/plain":
		text, err := readLimited(r, textLimit)
		if err != nil {
			return nil, err
		}
		req.body = text
	case ct == "multipart/form-data":
		body, err := readMultipart(r, params["boundary"])
		if err != nil {
			return nil, err
		}
		req.body, req.multipart = body, true
	}
	return req, nil
}

func readLimited(r *http.Request, limit int64) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > limit {
		return "", &bodyError{http.StatusRequestEntityTooLarge, "request entity too large"}
	}
	return string(data), nil
}

// readMultipart 读出表单字段，文件只记录文件名、类型和大小（不保存内容），与 koa-body 2.x 的 {fields, files} 结构相同。
// 同名字段后面的覆盖前面的（formidable 的行为），同名文件合并成数组。
func readMultipart(r *http.Request, boundary string) (*jsjson.Object, error) {
	fields, files := jsjson.NewObject(), jsjson.NewObject()
	mr := multipart.NewReader(io.LimitReader(r.Body, multipartLimit+1), boundary)
	var total int64
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, &bodyError{http.StatusBadRequest, "invalid multipart body"}
		}
		var value strings.Builder
		var n int64
		if part.FileName() == "" {
			n, err = io.Copy(&value, part)
		} else {
			n, err = io.Copy(io.Discard, part)
		}
		_ = part.Close()
		if err != nil {
			return nil, &bodyError{http.StatusBadRequest, "invalid multipart body"}
		}
		if total += n; total > multipartLimit {
			return nil, &bodyError{http.StatusRequestEntityTooLarge, "request entity too large"}
		}
		name := part.FormName()
		if part.FileName() == "" {
			fields.Set(name, value.String())
			continue
		}
		file := jsjson.NewObject("size", json.Number(strconv.FormatInt(n, 10)), "name", part.FileName(), "type", part.Header.Get("Content-Type"))
		switch old, ok := files.Get(name); {
		case !ok:
			files.Set(name, file)
		case isList(old):
			files.Set(name, append(old.([]any), file))
		default:
			files.Set(name, []any{old, file})
		}
	}
	return jsjson.NewObject("fields", fields, "files", files), nil
}

// parseQuery 对应 Node 的 querystring.parse：+ 视为空格，解码失败时保留原文，同名参数合并成数组，保持出现顺序。
func parseQuery(raw string) *jsjson.Object {
	out := jsjson.NewObject()
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		k, v = unescape(k), unescape(v)
		switch old, ok := out.Get(k); {
		case !ok:
			out.Set(k, v)
		case isList(old):
			out.Set(k, append(old.([]any), v))
		default:
			out.Set(k, []any{old, v})
		}
	}
	return out
}

func isList(v any) bool {
	_, ok := v.([]any)
	return ok
}

func unescape(s string) string {
	s = strings.ReplaceAll(s, "+", " ")
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	// querystring.unescape 出错时退回逐个解码合法的 %XX
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(n))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
