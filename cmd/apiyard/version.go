package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// buildVersion 由发布构建写入：go build -ldflags "-X main.buildVersion=v0.1.0"（见 tools/dist）。
var buildVersion = ""

// version 返回版本号；自己编译的版本显示 dev 和 git 提交（有未提交的修改时加 -dirty）。
func version() string {
	if buildVersion != "" {
		return buildVersion
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	return devVersion(info.Settings)
}

func devVersion(settings []debug.BuildSetting) string {
	var rev, modified string
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if rev == "" {
		return "dev"
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if modified == "true" {
		rev += "-dirty"
	}
	return "dev-" + rev
}

// versionString 是 apiyard version 的输出。
func versionString() string {
	return fmt.Sprintf("apiyard %s (%s, %s/%s)", version(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
