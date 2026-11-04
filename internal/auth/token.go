package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// TokenTTL 是登录令牌和登录 Cookie 的有效期，与 YApi 相同。
const TokenTTL = 7 * 24 * time.Hour

// ErrInvalidToken 表示令牌格式错误、签名不符或已过期。
var ErrInvalidToken = errors.New("登录令牌无效")

// tokenHeader 与 YApi 使用的 jsonwebtoken 生成的头部一致。
var tokenHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

type tokenClaims struct {
	UID int64 `json:"uid"`
	Iat int64 `json:"iat"`
	Exp int64 `json:"exp"`
}

// SignToken 生成 YApi 格式的登录令牌（Cookie _yapi_token）：
// HS256 签名的 JWT，载荷为 {uid, iat, exp}，密钥是该用户自己的 passsalt。
// 用户修改密码时 passsalt 随之更换，旧令牌自动失效。
func SignToken(uid int64, passsalt string, now time.Time) (string, error) {
	payload, err := json.Marshal(tokenClaims{UID: uid, Iat: now.Unix(), Exp: now.Add(TokenTTL).Unix()})
	if err != nil {
		return "", err
	}
	signingInput := tokenHeader + "." + base64.RawURLEncoding.EncodeToString(payload)
	return signingInput + "." + sign(signingInput, passsalt), nil
}

// VerifyToken 校验令牌并返回其中的 uid。
func VerifyToken(token, passsalt string, now time.Time) (int64, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, ErrInvalidToken
	}
	if !verifyHeader(parts[0]) {
		return 0, ErrInvalidToken
	}
	want := sign(parts[0]+"."+parts[1], passsalt)
	if !hmac.Equal([]byte(want), []byte(parts[2])) {
		return 0, ErrInvalidToken
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, ErrInvalidToken
	}
	var claims tokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return 0, ErrInvalidToken
	}
	if claims.Exp != 0 && now.Unix() >= claims.Exp {
		return 0, ErrInvalidToken
	}
	return claims.UID, nil
}

// verifyHeader 只接受 HS256，防止 alg=none 之类的降级攻击。
func verifyHeader(encoded string) bool {
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}
	var h struct {
		Alg string `json:"alg"`
	}
	return json.Unmarshal(data, &h) == nil && h.Alg == "HS256"
}

func sign(signingInput, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(signingInput))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
