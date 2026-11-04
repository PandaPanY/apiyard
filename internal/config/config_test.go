package config

import "testing"

func TestLoadAddr(t *testing.T) {
	tests := []struct {
		name string
		env  string
		args []string
		want string
	}{
		{name: "默认值", want: ":3000"},
		{name: "环境变量", env: ":8080", want: ":8080"},
		{name: "命令行参数优先于环境变量", env: ":8080", args: []string{"-addr", ":9090"}, want: ":9090"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APIYARD_ADDR", tt.env)
			cfg, err := Load(tt.args)
			if err != nil {
				t.Fatalf("Load() 返回错误: %v", err)
			}
			if cfg.Addr != tt.want {
				t.Errorf("Addr = %q, 期望 %q", cfg.Addr, tt.want)
			}
		})
	}
}

func TestLoadWebAndUpstream(t *testing.T) {
	t.Setenv("APIYARD_WEB_DIR", "")
	t.Setenv("APIYARD_YAPI_UPSTREAM", "http://env:13000")

	cfg, err := Load([]string{"-web-dir", "/srv/web"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WebDir != "/srv/web" {
		t.Errorf("WebDir = %q", cfg.WebDir)
	}
	if cfg.YAPIUpstream != "http://env:13000" {
		t.Errorf("YAPIUpstream = %q", cfg.YAPIUpstream)
	}

	t.Setenv("APIYARD_YAPI_UPSTREAM", "")
	cfg, _ = Load(nil)
	if cfg.WebDir != "web/yapi" || cfg.YAPIUpstream != "" {
		t.Errorf("默认值不对: WebDir=%q YAPIUpstream=%q", cfg.WebDir, cfg.YAPIUpstream)
	}
}

func TestLoadUnknownFlag(t *testing.T) {
	if _, err := Load([]string{"-unknown"}); err == nil {
		t.Fatal("未知参数应当返回错误")
	}
}
