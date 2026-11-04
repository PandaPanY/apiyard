package yapitoken

import "testing"

// 取自 YApi 1.12.0 对照实例：管理员（uid 11）调用 /api/project/token 得到的 token，以及数据库里的原始 token。
const (
	yapiRaw     = "6aba0b44ea72e988552f"
	yapiEncoded = "b5ada1817af21a26df3bcebc085114ba7a5fd045b83e1ff7d32a4f650a144644"
)

func TestEncodeMatchesYApi(t *testing.T) {
	got, err := Encode("11", yapiRaw, DefaultSalt)
	if err != nil {
		t.Fatal(err)
	}
	if got != yapiEncoded {
		t.Errorf("Encode 与 YApi 不一致:\n  得到 %s\n  期望 %s", got, yapiEncoded)
	}
}

func TestDecodeYApiToken(t *testing.T) {
	uid, raw, err := Decode(yapiEncoded, DefaultSalt)
	if err != nil || uid != "11" || raw != yapiRaw {
		t.Fatalf("Decode = %q %q %v", uid, raw, err)
	}
	// passsalt 不同时解不出来，与 YApi 一样按原始 token 处理
	if _, _, err := Decode(yapiEncoded, "other-salt"); err == nil {
		t.Error("passsalt 不对时应返回错误")
	}
}

func TestDecodeRejectsRawToken(t *testing.T) {
	// 旧版 YApi 直接使用 20 位原始 token，它不是加密格式
	for _, tok := range []string{yapiRaw, "", "zz", "00112233445566778899aabbccddeeff"} {
		if _, _, err := Decode(tok, DefaultSalt); err == nil {
			t.Errorf("Decode(%q) 应返回错误", tok)
		}
	}
}

func TestRoundTripWithCustomSalt(t *testing.T) {
	enc, err := Encode("42", "abc", "my-salt")
	if err != nil {
		t.Fatal(err)
	}
	if uid, raw, err := Decode(enc, "my-salt"); err != nil || uid != "42" || raw != "abc" {
		t.Errorf("往返结果不对: %q %q %v", uid, raw, err)
	}
}
