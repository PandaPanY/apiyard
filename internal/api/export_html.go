package api

import (
	"bytes"
	_ "embed" // 导出 HTML 使用的样式
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	gmhtml "github.com/yuin/goldmark/renderer/html"

	"github.com/PandaPanY/apiyard/internal/store"
)

// exportThemeCSS 来自 YApi 的 exts/yapi-plugin-export-data/defaultTheme.css（Apache-2.0），保持导出页面的外观一致。
//
//go:embed export_theme.css
var exportThemeCSS string

// markdownRenderer 对应 YApi 的 markdown-it 配置：允许 HTML、换行转为 <br>、支持表格。
var markdownRenderer = goldmark.New(
	goldmark.WithExtensions(extension.Table),
	goldmark.WithRendererOptions(gmhtml.WithUnsafe(), gmhtml.WithHardWraps()),
)

// exportHTML 把 Markdown 导出内容渲染成带左侧目录的单页 HTML。
// YApi 用 markdown-it 渲染，HTML 无法与原版逐字一致；这里保持相同的页面结构和样式类名，目录按分类和接口生成。
func exportHTML(table *schemaTable, p *store.Project, cats []exportCat) (string, error) {
	md, err := table.projectMarkdown(p, cats)
	if err != nil {
		return "", err
	}
	var body bytes.Buffer
	if err := markdownRenderer.Convert([]byte(md), &body); err != nil {
		return "", err
	}

	var toc strings.Builder
	toc.WriteString(`<div class="table-of-contents"><ul>`)
	for _, c := range cats {
		fmt.Fprintf(&toc, "<li>%s<ul>", html.EscapeString(c.cat.Name))
		for _, it := range c.list {
			// 锚点与 Markdown 中的 <a id=标题+分类ID> 对应
			anchor := it.Title + strconv.FormatInt(it.CatID, 10)
			fmt.Fprintf(&toc, `<li><a href="#%s">%s</a></li>`, html.EscapeString(anchor), html.EscapeString(it.Title))
		}
		toc.WriteString("</ul></li>")
	}
	toc.WriteString("</ul></div>")

	return `<!DOCTYPE html>
<html>
<head>
<title>` + html.EscapeString(p.Name) + `</title>
<meta charset="utf-8" />
<style>` + exportThemeCSS + `</style>
</head>
<body>
  <div class="m-header">
    <a href="#"><h1 class="title">接口文档</h1></a>
  </div>
  <div class="g-doc">
    ` + toc.String() + `
    <div id="right" class="content-right">
    ` + body.String() + `
      <footer class="m-footer">
        <p>由 ApiYard 生成</p>
      </footer>
    </div>
  </div>
</body>
</html>
`, nil
}
