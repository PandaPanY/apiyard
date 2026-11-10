package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"strings"
)

// 品牌替换：把沿用的 YApi 前端里显示给用户的名称、logo、链接和配色换成 ApiYard 的。
// 预构建前端固定为 yapi-vendor@1.12.0，所以直接替换主包里的字符串字面量，比在页面上按 DOM 匹配更稳，也不会闪烁。
// 每条规则都核对出现次数，前端版本一变就能发现，此时整体放弃替换，按原样返回前端。

const (
	repoURL = "https://github.com/PandaPanY/apiyard"
	docsURL = repoURL + "/tree/main/docs"
)

// rewrite 是一处文字替换，old 必须恰好出现 n 次。
type rewrite struct {
	old, new string
	n        int
}

// indexRewrites 作用于 index.html。
var indexRewrites = []rewrite{
	{`<title>YApi-高效、易用、功能强大的可视化接口管理平台</title>`, `<title>ApiYard</title>`, 1},
	{`<link rel="icon" type="image/png" sizes="192x192" href="/image/favicon.png">`,
		`<link rel="icon" type="image/svg+xml" href="/apiyard/logo.svg">`, 1},
	{`content="YApi 是高效、易用、功能强大的 api 管理平台，旨在为开发、产品、测试人员提供更优雅的接口管理服务。可以帮助开发者轻松创建、发布、维护 API，YApi 还为用户提供了优秀的交互体验，开发人员只需利用平台提供的接口数据写入工具以及简单的点击操作就可以实现接口的管理。"`,
		`content="ApiYard 是兼容 YApi 的轻量接口管理平台。"`, 1},
}

// bundleRewrites 作用于前端主包（prd/index@….js），按顺序执行，n 是执行到这一条时的出现次数。
// 只替换正常使用时能看到的地方；已隐藏的功能（测试集合等）里的文字不动。
// 具体功能的帮助链接（Mock 规则、数据导入、开放 API 等）仍指向 YApi 文档，这些用法与 YApi 相同。
var bundleRewrites = []rewrite{
	// 页脚：源码、反馈指向 ApiYard，保留对 YApi 的致谢；版本号取 ApiYard 的版本
	{`footList:[{title:"GitHub",iconType:"github",linkList:[{itemTitle:"YApi 源码仓库",itemLink:"https://github.com/YMFE/yapi"}]},{title:"团队",iconType:"team",linkList:[{itemTitle:"YMFE",itemLink:"https://ymfe.org"}]},{title:"反馈",iconType:"aliwangwang-o",linkList:[{itemTitle:"Github Issues",itemLink:"https://github.com/YMFE/yapi/issues"},{itemTitle:"Github Pull Requests",itemLink:"https://github.com/YMFE/yapi/pulls"}]},{title:"Copyright © 2018-"+(new Date).getFullYear()+" YMFE",linkList:[{itemTitle:"版本: "+k+" ",itemLink:"https://github.com/YMFE/yapi/blob/master/CHANGELOG.md"},{itemTitle:"使用文档",itemLink:"https://hellosean1025.github.io/yapi/"}]}]`,
		`footList:[{title:"GitHub",iconType:"github",linkList:[{itemTitle:"ApiYard 源码仓库",itemLink:"` + repoURL + `"}]},{title:"致谢",iconType:"team",linkList:[{itemTitle:"基于 YApi（YMFE）",itemLink:"https://github.com/YMFE/yapi"}]},{title:"反馈",iconType:"aliwangwang-o",linkList:[{itemTitle:"GitHub Issues",itemLink:"` + repoURL + `/issues"},{itemTitle:"GitHub Pull Requests",itemLink:"` + repoURL + `/pulls"}]},{title:"ApiYard",linkList:[{itemTitle:"版本: "+k+" ",itemLink:"` + repoURL + `/releases"},{itemTitle:"使用文档",itemLink:"` + docsURL + `"}]}]`, 1},
	{`w=r(E),k="1.10.2",S=(i=a=function`, `w=r(E),k=window.APIYARD_VERSION||"",S=(i=a=function`, 1},
	// 升级提示里的“版本详情”（外部版本检查已被 CSP 拦截，正常不会出现，一并改掉）
	{`"https://github.com/YMFE/yapi/blob/master/CHANGELOG.md"`, `"` + repoURL + `/releases"`, 1},

	// “使用文档”入口：顶栏、首页两处、新手引导
	{`"https://hellosean1025.github.io/yapi"`, `"` + docsURL + `"`, 3},
	{`"https://hellosean1025.github.io/yapi/"`, `"` + docsURL + `"`, 1},
	{`"初次使用 YApi，强烈建议你阅读"`, `"初次使用 ApiYard，建议先阅读"`, 1},
	{`"，我们为你提供了通俗易懂的快速入门教程，更有详细的使用说明，欢迎阅读！"`,
		`"，里面有部署、迁移和与 YApi 的差异说明。界面操作与 YApi 相同。"`, 1},

	// 首页和登录页的名称
	{`"YAPI"`, `"ApiYard"`, 3},
	{`"YApi让接口开发更简单高效，让接口的管理更具可读性、可维护性，让团队协作更合理。"`,
		`"ApiYard 让接口开发更简单高效，让接口的管理更具可读性、可维护性，让团队协作更合理。"`, 1},
	{`"YAPI 将扁平化管理模式的思想引入到产品的权限管理中，超级管理员拥有最高的权限，并将权限分配给若干组长，超级管理员只需管理组长即可，实际上管理YAPI各大分组与项目的是“组长”。`,
		`"ApiYard 将扁平化管理模式的思想引入到产品的权限管理中，超级管理员拥有最高的权限，并将权限分配给若干组长，超级管理员只需管理组长即可，实际上管理各大分组与项目的是“组长”。`, 1},

	// 个人空间的引导、浏览器提示、接口页离开时恢复的标题
	{`"欢迎使用 YApi ~"`, `"欢迎使用 ApiYard ~"`, 1},
	{`探索 YApi 的功能。"`, `探索 ApiYard 的功能。"`, 1},
	{`"YApi 的接口测试等功能仅支持 Chrome 浏览器，`, `"ApiYard 的接口测试等功能仅支持 Chrome 浏览器，`, 1},
	{`"YApi 新版如何查看 http 请求数据"`, `"如何查看 http 请求数据"`, 1},
	{`r.title="YApi-高效、易用、功能强大的可视化接口管理平台"`, `r.title="ApiYard"`, 1},

	// 请求方法标签和几处写死的主色，与主题配色一致
	{`METHOD_COLOR:{post:{bac:"#d2eafb",color:"#108ee9"},get:{bac:"#cfefdf",color:"#00a854"},put:{bac:"#fff3cf",color:"#ffbf00"},delete:{bac:"#fcdbd9",color:"#f04134"},head:{bac:"#fff3cf",color:"#ffbf00"},patch:{bac:"#fff3cf",color:"#ffbf00"},options:{bac:"#fff3cf",color:"#ffbf00"}}`,
		`METHOD_COLOR:{post:{bac:"#e3edfb",color:"#1d4fa8"},get:{bac:"#e4f4ea",color:"#17663a"},put:{bac:"#fdebdd",color:"#a2470c"},delete:{bac:"#fbe5e3",color:"#b42318"},head:{bac:"#eef1f3",color:"#4e5966"},patch:{bac:"#f0e7fb",color:"#6b2fb3"},options:{bac:"#eef1f3",color:"#4e5966"}}`, 1},
	{`cursor:"pointer",color:"#2395f1"}`, `cursor:"pointer",color:"#0f766e"}`, 2},
	{`"新环境"===t.name&&"#2395f1"`, `"新环境"===t.name&&"#0f766e"`, 1},
	// 项目导航不再给两个字的名称中间加空格（“接 口”→“接口”）
	{`return 2===t.name.length&&(t.name=t.name[0]+" "+t.name[1]),m.default.createElement(s.default.Item`,
		`return m.default.createElement(s.default.Item`, 1},

	// 接口“运行”页的“保存”是保存到测试集合，ApiYard 暂不支持测试集合：去掉这个按钮和它打开的对话框。
	// 对话框一挂载就会请求 /api/col/list，后端没有这个接口，每次打开“运行”页都会弹出 404 报错。
	// 不能让后端返回空列表来消除报错：原版每个项目都有默认集合，对话框直接读取第一个集合，列表为空时页面会崩溃。
	// 按钮所在的组件在测试集合里显示为“更新”（type 为 case），那部分界面已隐藏，不受影响
	{`te.default.createElement(m.default,{placement:"bottom",title:function(){return"inter"===e.props.type?"保存到测试集":"更新该用例"}}`,
		`"inter"!==e.props.type&&te.default.createElement(m.default,{placement:"bottom",title:function(){return"inter"===e.props.type?"保存到测试集":"更新该用例"}}`, 1},
	{`,T.default.createElement(R.default,{visible:this.state.saveCaseModalVisible,caseName:n.title,onCancel:function(){return e.setState({saveCaseModalVisible:!1})},onOk:this.saveCase})`, ``, 1},

	// logo：原组件画的是 YApi 的 SVG 图标，改为渲染 ApiYard 的 logo；原来的子元素作为 children 传入后被忽略。
	// 不沿用 className "svg"：原版 CSS 会让首页和登录页的 .svg 一直旋转
	{`l=function(e){var t=e.length;return i.default.createElement("svg",{className:"svg",width:t,height:t,viewBox:"0 0 64 64",version:"1.1"},`,
		`l=function(e){var t=e.length;return i.default.createElement(function(){return i.default.createElement("img",{className:"apiyard-logo",width:t,height:t,src:"/apiyard/logo.svg",alt:"ApiYard"})},null,`, 1},
}

// applyRewrites 依次执行替换；任何一条的出现次数不符都返回错误，不做部分替换。
func applyRewrites(src []byte, rules []rewrite) ([]byte, error) {
	out := src
	var mismatched []string
	for _, r := range rules {
		if got := bytes.Count(out, []byte(r.old)); got != r.n {
			mismatched = append(mismatched, fmt.Sprintf("%.40q 出现 %d 次（应为 %d 次）", r.old, got, r.n))
			continue
		}
		out = bytes.ReplaceAll(out, []byte(r.old), []byte(r.new))
	}
	if len(mismatched) > 0 {
		return nil, fmt.Errorf("前端与 yapi-vendor@1.12.0 不一致：%s", strings.Join(mismatched, "；"))
	}
	return out, nil
}

// 主题配色：把 YApi 样式表里的蓝色主色换成 ApiYard 的青绿色，文字和边框改用偏冷的中性色，控件圆角加大。
// 结构上的调整（顶栏、侧栏、卡片等）在 patch/theme.css 里覆盖，这里只换颜色值，原版所有控件的各种状态都能一并换掉。
var cssRewrites = []rewrite{
	{"#2395f1", "#0f766e", 173},                         // 主色
	{"#4fb6ff", "#14897f", 51},                          // 悬停
	{"#56b2fd", "#14897f", 6},                           // 帮助链接
	{"#1473cc", "#0b5f58", 23},                          // 按下
	{"#f0fbff", "#eaf5f3", 25},                          // 浅底：菜单项、下拉选项悬停
	{"#d5ebfc", "#d3ebe7", 7},                           // 树节点选中
	{"#c9eeff", "#cce8e3", 6},                           // 日历、穿梭框选中
	{"#eef7fe", "#eef6f4", 2},                           // 分类树的操作按钮底色
	{"rgba(35,149,241,.2)", "rgba(15,118,110,.18)", 13}, // 输入框聚焦光晕
	{"rgba(13,27,62,.65)", "rgba(22,32,42,.85)", 127},   // 正文
	{"rgba(13,27,62,.45)", "rgba(22,32,42,.64)", 120},   // 次要文字
	{"rgba(13,27,62,.43)", "rgba(22,32,42,.62)", 61},
	{"#e9e9e9", "#e2e6ea", 100}, // 分隔线
	{"#d9d9d9", "#d5dbe0", 73},  // 输入框边框
	{"border-radius:4px", "border-radius:6px", 102},
	// antd 的字体栈：去掉 Monospaced Number、Chinese Quote 两个别名字体，中文优先用苹方、微软雅黑
	{"font-family:Monospaced Number,Chinese Quote,-apple-system,BlinkMacSystemFont,Segoe UI,Roboto,PingFang SC,Hiragino Sans GB,Microsoft YaHei,Helvetica Neue,Helvetica,Arial,sans-serif",
		"font-family:-apple-system,BlinkMacSystemFont,Segoe UI,PingFang SC,Hiragino Sans GB,Microsoft YaHei,Roboto,Helvetica Neue,Arial,sans-serif", 65},
}

// brandedFrontend 是启动时替换好的前端文件，保存在内存里。
type brandedFrontend struct {
	jsPath, cssPath string // 主包和样式表在前端目录里的路径，如 prd/index@40d4….js
	files           map[string]memFile
}

// memFile 是内存里的文件内容；gz 为空表示不提供压缩版本。
type memFile struct {
	plain, gz []byte
}

const assetsPath = "prd/assets.js"

// assetsIndex 从 assets.js 里取出主包和样式表的文件名。
var assetsIndex = regexp.MustCompile(`"index\.js":\{"js":"([^"?]+)","css":"([^"?]+)"`)

func loadBrandedFrontend(fsys fs.FS) (*brandedFrontend, error) {
	assets, err := readMaybeGzipped(fsys, assetsPath)
	if err != nil {
		return nil, err
	}
	m := assetsIndex.FindSubmatch(assets)
	if m == nil {
		return nil, errors.New(assetsPath + " 里找不到主包 index.js")
	}
	f := &brandedFrontend{
		jsPath:  "prd/" + string(m[1]),
		cssPath: "prd/" + string(m[2]),
		files:   map[string]memFile{},
	}
	for _, item := range []struct {
		name  string
		rules []rewrite
	}{{string(m[1]), bundleRewrites}, {string(m[2]), cssRewrites}} {
		src, err := readMaybeGzipped(fsys, "prd/"+item.name)
		if err != nil {
			return nil, err
		}
		plain, err := applyRewrites(src, item.rules)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", item.name, err)
		}
		gz, err := gzipBytes(plain)
		if err != nil {
			return nil, err
		}
		f.files["prd/"+item.name] = memFile{plain, gz}
		// 文件地址带上内容哈希，浏览器里缓存的原版文件不会被继续使用
		sum := sha256.Sum256(plain)
		assets = bytes.Replace(assets, []byte(`"`+item.name+`"`),
			[]byte(`"`+item.name+"?v="+hex.EncodeToString(sum[:6])+`"`), 1)
	}
	f.files[assetsPath] = memFile{plain: assets}
	return f, nil
}

func gzipBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// readMaybeGzipped 读取文件内容，优先读 .gz 并解压（tools/fetch-yapi-web 下载的前端只保留 .gz）。
func readMaybeGzipped(fsys fs.FS, name string) ([]byte, error) {
	if f, ok := openFile(fsys, name+".gz"); ok {
		defer closeQuietly(f)
		zr, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("解压 %s.gz: %w", name, err)
		}
		return io.ReadAll(io.LimitReader(zr, maxGunzipped))
	}
	return fs.ReadFile(fsys, name)
}
