// Package auth 实现与 YApi 兼容的密码哈希和登录令牌，保证从 YApi 迁移来的账号密码和登录状态继续有效。
package auth

import (
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- 必须与 YApi 的密码哈希算法一致，否则迁移来的密码无法验证
	"crypto/subtle"
	"encoding/hex"
	"math/big"
)

// HashPassword 返回 YApi 的密码哈希：sha1(明文 + sha1(passsalt))，十六进制小写。
func HashPassword(password, passsalt string) string {
	return sha1Hex(password + sha1Hex(passsalt))
}

// CheckPassword 以恒定时间比较，避免通过响应耗时推测哈希。
func CheckPassword(password, passsalt, hash string) bool {
	return subtle.ConstantTimeCompare([]byte(HashPassword(password, passsalt)), []byte(hash)) == 1
}

func sha1Hex(s string) string {
	sum := sha1.Sum([]byte(s)) // #nosec G401 -- 同上，兼容 YApi
	return hex.EncodeToString(sum[:])
}

const (
	saltAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	saltLength   = 11
)

// NewSalt 生成随机 passsalt。YApi 用 Math.random().toString(36) 生成约 11 位的 36 进制串；
// 这里保持相同的形态，但改用密码学安全的随机数。passsalt 同时也是登录令牌的签名密钥。
func NewSalt() (string, error) {
	b := make([]byte, saltLength)
	limit := big.NewInt(int64(len(saltAlphabet)))
	for i := range b {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}
		b[i] = saltAlphabet[n.Int64()]
	}
	return string(b), nil
}
