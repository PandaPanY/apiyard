package main

import (
	"runtime/debug"
	"testing"
)

func TestHealthURL(t *testing.T) {
	for addr, want := range map[string]string{
		":3000":          "http://127.0.0.1:3000/healthz",
		"0.0.0.0:8080":   "http://127.0.0.1:8080/healthz",
		"[::]:3000":      "http://127.0.0.1:3000/healthz",
		"10.0.0.5:3000":  "http://10.0.0.5:3000/healthz",
		"localhost:3000": "http://localhost:3000/healthz",
		"[::1]:3000":     "http://[::1]:3000/healthz",
	} {
		if got, err := healthURL(addr); err != nil || got != want {
			t.Errorf("healthURL(%q) = %q, %v，应为 %q", addr, got, err, want)
		}
	}
	if _, err := healthURL("3000"); err == nil {
		t.Error("没有端口的地址应报错")
	}
}

func TestDevVersion(t *testing.T) {
	for _, c := range []struct {
		settings []debug.BuildSetting
		want     string
	}{
		{nil, "dev"},
		{[]debug.BuildSetting{{Key: "vcs.revision", Value: "9a4b021f00d"}, {Key: "vcs.modified", Value: "false"}}, "dev-9a4b021"},
		{[]debug.BuildSetting{{Key: "vcs.revision", Value: "9a4b021f00d"}, {Key: "vcs.modified", Value: "true"}}, "dev-9a4b021-dirty"},
	} {
		if got := devVersion(c.settings); got != c.want {
			t.Errorf("devVersion = %q，应为 %q", got, c.want)
		}
	}
}
