// Package yapitoken 实现 YApi 开放 API token 的编解码。
//
// 每个项目在数据库里保存一个原始 token；对外提供的 token 是把 "uid|原始 token" 用
// Node 的 crypto.createCipher('aes192', passsalt) 加密后的十六进制串（见 YApi 的 server/utils/token.js）。
// createCipher 已被 Node 废弃，它用 OpenSSL 的 EVP_BytesToKey（MD5、迭代 1 次、不加盐）从口令派生密钥和 IV，
// 算法为 AES-192-CBC + PKCS#7 填充。这里严格按此实现，迁移后已经配置在各种工具里的 token 继续有效。
package yapitoken

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5" // #nosec G501 -- EVP_BytesToKey 规定用 MD5，必须与 YApi 一致
	"encoding/hex"
	"errors"
	"strings"
)

const (
	keyLen = 24 // AES-192
	ivLen  = aes.BlockSize
)

// DefaultSalt 是 YApi 配置文件没有设置 passsalt 时使用的默认值。
const DefaultSalt = "abcde"

// Encode 生成开放 API 使用的 token。
func Encode(uid, projectToken, passsalt string) (string, error) {
	key, iv := deriveKeyIV(passsalt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	plain := pkcs7Pad([]byte(uid+"|"+projectToken), aes.BlockSize)
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, plain)
	return hex.EncodeToString(out), nil
}

// ErrNotEncoded 表示 token 不是加密格式。YApi 此时把整个 token 当作原始项目 token 使用。
var ErrNotEncoded = errors.New("不是加密的 token")

// Decode 解出 uid 和原始项目 token。
func Decode(token, passsalt string) (uid, projectToken string, err error) {
	data, err := hex.DecodeString(token)
	if err != nil || len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return "", "", ErrNotEncoded
	}
	key, iv := deriveKeyIV(passsalt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", err
	}
	plain := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, data)
	plain, ok := pkcs7Unpad(plain, aes.BlockSize)
	if !ok {
		return "", "", ErrNotEncoded
	}
	// 与 YApi 一致：竖线必须存在且不在开头
	uid, projectToken, found := strings.Cut(string(plain), "|")
	if !found || uid == "" {
		return "", "", ErrNotEncoded
	}
	return uid, projectToken, nil
}

// deriveKeyIV 实现 EVP_BytesToKey(MD5, 无盐, 1 次迭代)：D_i = MD5(D_{i-1} || 口令)，拼接后依次截取密钥和 IV。
func deriveKeyIV(password string) (key, iv []byte) {
	var buf, prev []byte
	for len(buf) < keyLen+ivLen {
		h := md5.New() // #nosec G401 -- 同上
		h.Write(prev)
		h.Write([]byte(password))
		prev = h.Sum(nil)
		buf = append(buf, prev...)
	}
	return buf[:keyLen], buf[keyLen : keyLen+ivLen]
}

func pkcs7Pad(data []byte, size int) []byte {
	n := size - len(data)%size
	return append(data, bytes.Repeat([]byte{byte(n)}, n)...) // #nosec G115 -- n 在 1~16 之间
}

func pkcs7Unpad(data []byte, size int) ([]byte, bool) {
	if len(data) == 0 {
		return nil, false
	}
	n := int(data[len(data)-1])
	if n == 0 || n > size || n > len(data) {
		return nil, false
	}
	for _, b := range data[len(data)-n:] {
		if int(b) != n {
			return nil, false
		}
	}
	return data[:len(data)-n], true
}
