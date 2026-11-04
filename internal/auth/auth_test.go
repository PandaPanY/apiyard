package auth

import (
	"strings"
	"testing"
	"time"
)

// 以下向量取自 YApi 1.12.0 对照实例（测试环境，管理员使用 YApi 公开的默认密码）。
const (
	yapiSalt  = "xgskmxt1zf"
	yapiHash  = "255defe17b8aa0d1a75e674851ba76d6eeb22c72" // "ymfe.org" 的哈希
	yapiToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1aWQiOjExLCJpYXQiOjE3OTM3NzEyNzAsImV4cCI6MTc5NDM3NjA3MH0.N7aro7vjgmO71zAx8HYLbLGhh3f4xE2nq1FDMbXatMs"
	yapiIat   = 1793771270
	yapiExp   = 1794376070
)

func TestPasswordMatchesYApi(t *testing.T) {
	if got := HashPassword("ymfe.org", yapiSalt); got != yapiHash {
		t.Fatalf("HashPassword = %s, 期望 YApi 存储的 %s", got, yapiHash)
	}
	if !CheckPassword("ymfe.org", yapiSalt, yapiHash) {
		t.Error("正确的密码应当通过")
	}
	if CheckPassword("wrong", yapiSalt, yapiHash) {
		t.Error("错误的密码不应通过")
	}
}

func TestNewSalt(t *testing.T) {
	a, err := NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewSalt()
	if len(a) != saltLength || a == b {
		t.Errorf("NewSalt 应生成 %d 位的随机串: %q %q", saltLength, a, b)
	}
	if strings.Trim(a, saltAlphabet) != "" {
		t.Errorf("NewSalt 含有字母表之外的字符: %q", a)
	}
}

func TestVerifyYApiToken(t *testing.T) {
	uid, err := VerifyToken(yapiToken, yapiSalt, time.Unix(yapiIat+60, 0))
	if err != nil || uid != 11 {
		t.Fatalf("YApi 签发的令牌应能通过校验: uid=%d err=%v", uid, err)
	}
	if _, err := VerifyToken(yapiToken, yapiSalt, time.Unix(yapiExp, 0)); err == nil {
		t.Error("过期的令牌应被拒绝")
	}
	if _, err := VerifyToken(yapiToken, "other-salt", time.Unix(yapiIat, 0)); err == nil {
		t.Error("passsalt 不同（例如已改密码）时应被拒绝")
	}
}

func TestSignTokenRoundTrip(t *testing.T) {
	now := time.Unix(yapiIat, 0)
	token, err := SignToken(11, yapiSalt, now)
	if err != nil {
		t.Fatal(err)
	}
	// 载荷与 YApi 完全相同，所以生成的令牌应当一字不差
	if token != yapiToken {
		t.Errorf("生成的令牌与 YApi 不一致:\n  得到 %s\n  期望 %s", token, yapiToken)
	}
	if uid, err := VerifyToken(token, yapiSalt, now); err != nil || uid != 11 {
		t.Errorf("自己签发的令牌应能通过校验: uid=%d err=%v", uid, err)
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	parts := strings.Split(yapiToken, ".")
	noneHeader := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0" // {"alg":"none","typ":"JWT"}
	for _, bad := range []string{
		"",
		"a.b",
		parts[0] + "." + parts[1] + ".tampered",
		noneHeader + "." + parts[1] + ".",
	} {
		if _, err := VerifyToken(bad, yapiSalt, time.Unix(yapiIat, 0)); err == nil {
			t.Errorf("应拒绝令牌 %q", bad)
		}
	}
}
