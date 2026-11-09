// Package web 把 web/yapi 下的 YApi 预构建前端打进可执行文件。
//
// web/yapi 由 tools/fetch-yapi-web 下载，不随仓库提交，仓库里只有一个占位文件 .gitkeep：
// 没有下载前端时编译照常通过，只是可执行文件里没有界面。
package web

import (
	"embed"
	"io/fs"
)

// all: 前缀让占位文件 .gitkeep 也能匹配，否则目录里只有它时 go:embed 会报错。
//
//go:embed all:yapi
var files embed.FS

// FS 返回内嵌的前端文件，根目录对应 yapi-vendor 包里的 static 目录；编译时没有前端则返回 nil。
func FS() fs.FS {
	sub, err := fs.Sub(files, "yapi")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
