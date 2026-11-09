package mockjs

import (
	"bufio"
	_ "embed"
	"errors"
	"math"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// randomFunc 是一个占位符函数，对应 Mock.Random 上的方法。args 的个数就是 JS 里 arguments.length。
type randomFunc func(m *mocker, args []any, o *options) (any, error)

const maxSafe = 9007199254740992 // 2^53，Mock.js 的整数默认范围

var reFloatPrefix = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?`)

// #nosec G404 -- Mock 数据不需要密码学安全的随机数
func random() float64 { return rand.Float64() }

// arg 取第 i 个参数，没有时为 undefined。
func arg(args []any, i int) any {
	if i < len(args) {
		return args[i]
	}
	return undefined
}

func isUndef(v any) bool { return v == any(undefined) }

// truthy 对应 JS 的布尔转换。
func truthy(v any) bool {
	switch x := v.(type) {
	case nil, undefinedType:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case string:
		return x != ""
	}
	return true
}

// jsRound 对应 Math.round（.5 向正无穷方向取整）。
func jsRound(f float64) float64 { return math.Floor(f + 0.5) }

func randomNatural(lo, hi float64) float64 { return jsRound(random()*(hi-lo)) + lo }

func randomInteger(lo, hi float64) float64 { return jsRound(random()*(hi-lo)) + lo }

// intArg 对应 typeof v !== 'undefined' ? parseInt(v, 10) : def。
func intArg(v any, def float64) float64 {
	if isUndef(v) {
		return def
	}
	return parseInt(v)
}

// ---- 基础 ----

func randomBoolean(args []any) any {
	minArg, maxArg, cur := arg(args, 0), arg(args, 1), arg(args, 2)
	if !isUndef(cur) {
		lo, hi := 1.0, 1.0
		if !isUndef(minArg) && !math.IsNaN(toNumber(minArg)) {
			lo = parseInt(minArg)
		}
		if !isUndef(maxArg) && !math.IsNaN(toNumber(maxArg)) {
			hi = parseInt(maxArg)
		}
		if random() > 1.0/(lo+hi)*lo {
			return !truthy(cur)
		}
		return cur
	}
	return random() >= 0.5
}

var charPools = map[string]string{
	"lower":  "abcdefghijklmnopqrstuvwxyz",
	"upper":  "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
	"number": "0123456789",
	"symbol": "!@#$%^&*()[]",
}

func init() {
	charPools["alpha"] = charPools["lower"] + charPools["upper"]
	charPools["undefined"] = charPools["lower"] + charPools["upper"] + charPools["number"] + charPools["symbol"]
}

// randomCharacter 对应 Random.character(pool)：pool 可以是 lower/upper/number/symbol/alpha，也可以是任意字符组成的字符串。
func randomCharacter(pool any) string {
	s, _ := characterPool(pool)
	return pickRune(s)
}

func characterPool(pool any) (string, error) {
	if p, ok := charPools[strings.ToLower(jsString(pool))]; ok {
		return p, nil
	}
	if s, ok := pool.(string); ok && s != "" {
		return s, nil
	}
	if s, ok := pool.(string); ok {
		return s, nil
	}
	return "", errors.New("pool.charAt is not a function")
}

func pickRune(s string) string {
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return ""
	}
	i := int(randomNatural(0, float64(n-1)))
	for j, r := range []rune(s) {
		if j == i {
			return string(r)
		}
	}
	return ""
}

func randomString(args []any) (any, error) {
	var pool any = undefined
	var length any
	switch len(args) {
	case 0:
		length = randomNatural(3, 7)
	case 1:
		length = args[0]
	case 2:
		if _, ok := arg(args, 0).(string); ok {
			pool, length = arg(args, 0), arg(args, 1)
		} else {
			length = randomNatural(parseInt(arg(args, 0)), parseInt(arg(args, 1)))
		}
	default:
		pool = arg(args, 0)
		length = randomNatural(parseInt(arg(args, 1)), parseInt(arg(args, 2)))
	}
	p, err := characterPool(pool)
	if err != nil {
		return nil, err
	}
	n := toNumber(length)
	if n > 1e6 {
		return nil, errors.New("字符串长度太大") // 原版会一直生成下去
	}
	var b strings.Builder
	for i := 0; float64(i) < n; i++ {
		b.WriteString(pickRune(p))
	}
	return b.String(), nil
}

func randomFloat(args []any) any {
	dmin, dmax := arg(args, 2), arg(args, 3)
	lo := 0.0
	if !isUndef(dmin) {
		lo = toNumber(dmin)
	}
	lo = math.Max(math.Min(lo, 17), 0)
	hi := 17.0
	if !isUndef(dmax) {
		hi = toNumber(dmax)
	}
	hi = math.Max(math.Min(hi, 17), 0)
	ret := jsString(randomInteger(intArg(arg(args, 0), -maxSafe), intArg(arg(args, 1), maxSafe))) + "."
	dcount := randomNatural(parseInt(lo), parseInt(hi))
	for i := 0; float64(i) < dcount; i++ {
		if float64(i) < dcount-1 {
			ret += randomCharacter("number")
		} else {
			ret += randomCharacter("123456789")
		}
	}
	return parseFloat(ret)
}

func randomRange(args []any) (any, error) {
	start, stop := arg(args, 0), arg(args, 1)
	if len(args) <= 1 {
		stop = start
		if !truthy(start) {
			stop = 0.0
		}
		start = 0.0
	}
	step := arg(args, 2)
	if !truthy(step) {
		step = 1.0
	}
	a, b, c := toNumber(start), toNumber(stop), toNumber(step)
	n := math.Max(math.Ceil((b-a)/c), 0)
	if math.IsNaN(n) {
		n = 0
	}
	if n > 1e5 {
		return nil, errors.New("range 的范围太大")
	}
	out := &rArray{}
	for i := 0; float64(i) < n; i++ {
		out.items = append(out.items, a)
		a += c
	}
	return out, nil
}

// ---- 日期 ----

var reDatePattern = regexp.MustCompile(`yyyy|yy|y|MM|M|dd|d|HH|H|hh|h|mm|m|ss|s|SS|S|A|a|T`)

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// formatDate 对应 Random._formatDate，使用本地时区（与 Node 的 Date 相同）。
func formatDate(t time.Time, format string) string {
	return reDatePattern.ReplaceAllStringFunc(format, func(flag string) string {
		switch flag {
		case "yyyy":
			return strconv.Itoa(t.Year())
		case "yy", "y":
			return strconv.Itoa(t.Year())[2:]
		case "MM":
			return pad2(int(t.Month()))
		case "M":
			return strconv.Itoa(int(t.Month()))
		case "dd":
			return pad2(t.Day())
		case "d":
			return strconv.Itoa(t.Day())
		case "HH":
			return pad2(t.Hour())
		case "H":
			return strconv.Itoa(t.Hour())
		case "hh":
			return pad2(t.Hour() % 12)
		case "h":
			return strconv.Itoa(t.Hour() % 12)
		case "mm":
			return pad2(t.Minute())
		case "m":
			return strconv.Itoa(t.Minute())
		case "ss":
			return pad2(t.Second())
		case "s":
			return strconv.Itoa(t.Second())
		case "SS":
			ms := t.Nanosecond() / 1e6
			switch {
			case ms < 10:
				return "00" + strconv.Itoa(ms)
			case ms < 100:
				return "0" + strconv.Itoa(ms)
			}
			return strconv.Itoa(ms)
		case "S":
			return strconv.Itoa(t.Nanosecond() / 1e6)
		case "A":
			if t.Hour() < 12 {
				return "AM"
			}
			return "PM"
		case "a":
			if t.Hour() < 12 {
				return "am"
			}
			return "pm"
		}
		return strconv.FormatInt(t.UnixMilli(), 10) // T
	})
}

// randomDate 对应 Random._randomDate()：1970 年到现在之间的随机时刻。
func randomDate() time.Time {
	return time.UnixMilli(int64(random() * float64(time.Now().UnixMilli())))
}

func dateFunc(def string) randomFunc {
	return func(_ *mocker, args []any, _ *options) (any, error) {
		format := def
		if f := arg(args, 0); truthy(f) {
			format = jsString(f)
		}
		return formatDate(randomDate(), format), nil
	}
}

var reTimeUnit = regexp.MustCompile(`year|month|day|hour|minute|second|week`)

func randomNow(args []any) any {
	unit, format := arg(args, 0), arg(args, 1)
	if len(args) == 1 && !reTimeUnit.MatchString(jsString(unit)) {
		format, unit = unit, undefined
	}
	u := ""
	if truthy(unit) {
		u = strings.ToLower(jsString(unit))
	}
	f := "yyyy-MM-dd HH:mm:ss"
	if truthy(format) {
		f = jsString(format)
	}
	d := time.Now()
	y, mon, day := d.Date()
	h, mi, s, ns := d.Hour(), d.Minute(), d.Second(), d.Nanosecond()
	// 与原版的 switch 一样逐级向下清零（case 之间没有 break）
	levels := []string{"year", "month", "day", "hour", "minute", "second"}
	start := -1
	for i, l := range levels {
		if u == l || (u == "week" && l == "day") {
			start = i
		}
	}
	if start >= 0 {
		for _, l := range levels[start:] {
			switch l {
			case "year":
				mon = time.January
			case "month":
				day = 1
			case "day":
				h = 0
			case "hour":
				mi = 0
			case "minute":
				s = 0
			case "second":
				ns = 0
			}
		}
	}
	d = time.Date(y, mon, day, h, mi, s, ns, d.Location())
	if u == "week" {
		d = d.AddDate(0, 0, -int(d.Weekday()))
	}
	return formatDate(d, f)
}

// ---- 图片和颜色 ----

var adSizes = []string{"300x250", "250x250", "240x400", "336x280", "180x150", "720x300", "468x60", "234x60", "88x31",
	"120x90", "120x60", "120x240", "125x125", "728x90", "160x600", "120x600", "300x600"}

func randomImage(args []any) any {
	size, bg, fg, format, text := arg(args, 0), arg(args, 1), arg(args, 2), arg(args, 3), arg(args, 4)
	switch len(args) {
	case 4:
		text, format = format, undefined
	case 3:
		text, fg = fg, undefined
	}
	if !truthy(size) {
		size = adSizes[int(randomNatural(0, float64(len(adSizes)-1)))]
	}
	url := "http://dummyimage.com/" + jsString(size)
	if truthy(bg) {
		url += "/" + strings.TrimPrefix(jsString(bg), "#")
	}
	if truthy(fg) {
		url += "/" + strings.TrimPrefix(jsString(fg), "#")
	}
	if truthy(format) {
		url += "." + jsString(format)
	}
	if truthy(text) {
		url += "&text=" + jsString(text)
	}
	return url
}

// colorNames 对应 color_dict.js 里的 nicer 颜色。
var colorNames = map[string]string{
	"navy": "#001F3F", "blue": "#0074D9", "aqua": "#7FDBFF", "teal": "#39CCCC", "olive": "#3D9970", "green": "#2ECC40",
	"lime": "#01FF70", "yellow": "#FFDC00", "orange": "#FF851B", "red": "#FF4136", "maroon": "#85144B",
	"fuchsia": "#F012BE", "purple": "#B10DC9", "silver": "#DDDDDD", "gray": "#AAAAAA", "black": "#111111", "white": "#FFFFFF",
}

// 色相在进程内一直累加黄金分割比，与原版相同，连续生成的颜色彼此区分得开。
var hue struct {
	sync.Mutex
	v float64
}

func goldenRatioColor() (h, s, v float64) {
	hue.Lock()
	defer hue.Unlock()
	if hue.v == 0 {
		hue.v = random()
	}
	hue.v = math.Mod(hue.v+0.618033988749895, 1)
	return hue.v * 360, 50, 95
}

func hsv2rgb(h, s, v float64) (r, g, b float64) {
	h, s, v = h/60, s/100, v/100
	hi := int(math.Floor(h)) % 6
	f := h - math.Floor(h)
	p := 255 * v * (1 - s)
	q := 255 * v * (1 - s*f)
	t := 255 * v * (1 - s*(1-f))
	v *= 255
	switch hi {
	case 0:
		return v, t, p
	case 1:
		return q, v, p
	case 2:
		return p, v, t
	case 3:
		return p, q, v
	case 4:
		return t, p, v
	}
	return v, p, q
}

func toInt32(f float64) int32 { return int32(int64(f)) } //nolint:gosec // 与 JS 的位运算一样截断

func randomHex() string {
	r, g, b := hsv2rgb(goldenRatioColor())
	n := (toInt32(256+r)<<8|toInt32(g))<<8 | toInt32(b)
	return "#" + strconv.FormatInt(int64(n), 16)[1:]
}

func randomRGB(alpha bool) string {
	r, g, b := hsv2rgb(goldenRatioColor())
	s := jsString(parseInt(r)) + ", " + jsString(parseInt(g)) + ", " + jsString(parseInt(b))
	if alpha {
		return "rgba(" + s + ", " + strconv.FormatFloat(random(), 'f', 2, 64) + ")"
	}
	return "rgb(" + s + ")"
}

func randomHSL() string {
	h, s, v := goldenRatioColor()
	s, v = s/100, v/100
	l := (2 - s) * v
	sl := s * v
	if l <= 1 {
		sl /= l
	} else {
		sl /= 2 - l
	}
	l /= 2
	return "hsl(" + jsString(parseInt(h)) + ", " + jsString(parseInt(sl*100)) + ", " + jsString(parseInt(l*100)) + ")"
}

// ---- 文本 ----

// textRange 对应 text.js 里的 range()。
func textRange(defMin, defMax float64, minArg, maxArg any) float64 {
	switch {
	case isUndef(minArg):
		return randomNatural(defMin, defMax)
	case isUndef(maxArg):
		return toNumber(minArg)
	}
	return randomNatural(parseInt(minArg), parseInt(maxArg))
}

func repeatJoin(n float64, sep string, fn func() string) (string, error) {
	if n > 1e5 {
		return "", errors.New("文本长度太大")
	}
	var parts []string
	for i := 0; float64(i) < n; i++ {
		parts = append(parts, fn())
	}
	return strings.Join(parts, sep), nil
}

func randomWord(args []any) (string, error) {
	return repeatJoin(textRange(3, 10, arg(args, 0), arg(args, 1)), "", func() string { return randomCharacter("lower") })
}

func word() string { w, _ := randomWord(nil); return w }

func randomSentence(args []any) (string, error) {
	s, err := repeatJoin(textRange(12, 18, arg(args, 0), arg(args, 1)), " ", word)
	return capitalize(s) + ".", err
}

const kanzi = "的一是在不了有和人这中大为上个国我以要他时来用们生到作地于出就分对成会可主发年动同工也能下过子说产种面而方后多定行学法所民得经十三之进着等部度家电力里如水化高自二理起小物现实加量都两体制机当使点从业本去把性好应开它合还因由其些然前外天政四日那社义事平形相全表间样与关各重新线内数正心反你明看原又么利比或但质气第向道命此变条只没结解问意建月公无系军很情者最立代想已通并提直题党程展五果料象员革位入常文总次品式活设及管特件长求老头基资边流路级少图山统接知较将组见计别她手角期根论运农指几九区强放决西被干做必战先回则任取据处队南给色光门即保治北造百规热领七海口东导器压志世金增争济阶油思术极交受联什认六共权收证改清己美再采转更单风切打白教速花带安场身车例真务具万每目至达走积示议声报斗完类八离华名确才科张信马节话米整空元况今集温传土许步群广石记需段研界拉林律叫且究观越织装影算低持音众书布复容儿须际商非验连断深难近矿千周委素技备半办青省列习响约支般史感劳便团往酸历市克何除消构府称太准精值号率族维划选标写存候毛亲快效斯院查江型眼王按格养易置派层片始却专状育厂京识适属圆包火住调满县局照参红细引听该铁价严龙飞"

func randomCword(args []any) (any, error) {
	var pool any = kanzi
	var length any
	switch len(args) {
	case 0:
		length = 1.0
	case 1:
		if _, ok := args[0].(string); ok {
			pool, length = args[0], 1.0
		} else {
			length = args[0]
		}
	case 2:
		if _, ok := args[0].(string); ok {
			pool, length = args[0], args[1]
		} else {
			length = randomNatural(parseInt(args[0]), parseInt(args[1]))
		}
	default:
		pool, length = args[0], randomNatural(parseInt(args[1]), parseInt(args[2]))
	}
	p, ok := pool.(string)
	if !ok {
		return nil, errors.New("pool.charAt is not a function")
	}
	w, err := repeatJoin(toNumber(length), "", func() string { return pickRune(p) })
	return w, err
}

func cword() string { w, _ := randomCword(nil); return w.(string) }

func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return s
	}
	return strings.ToUpper(string(r)) + s[size:]
}

// ---- 姓名、网络、地址 ----

var (
	firstNames = strings.Fields(`James John Robert Michael William David Richard Charles Joseph Thomas Christopher Daniel Paul Mark
		Donald George Kenneth Steven Edward Brian Ronald Anthony Kevin Jason Matthew Gary Timothy Jose Larry Jeffrey Frank Scott Eric
		Mary Patricia Linda Barbara Elizabeth Jennifer Maria Susan Margaret Dorothy Lisa Nancy Karen Betty Helen Sandra Donna Carol
		Ruth Sharon Michelle Laura Sarah Kimberly Deborah Jessica Shirley Cynthia Angela Melissa Brenda Amy Anna`)
	lastNames = strings.Fields(`Smith Johnson Williams Brown Jones Miller Davis Garcia Rodriguez Wilson Martinez Anderson Taylor
		Thomas Hernandez Moore Martin Jackson Thompson White Lopez Lee Gonzalez Harris Clark Lewis Robinson Walker Perez Hall Young Allen`)
	cFirstNames = strings.Fields(`王 李 张 刘 陈 杨 赵 黄 周 吴 徐 孙 胡 朱 高 林 何 郭 马 罗 梁 宋 郑 谢 韩 唐 冯 于 董 萧 程 曹 袁 邓 许 傅 沈 曾 彭 吕
		苏 卢 蒋 蔡 贾 丁 魏 薛 叶 阎 余 潘 杜 戴 夏 锺 汪 田 任 姜 范 方 石 姚 谭 廖 邹 熊 金 陆 郝 孔 白 崔 康 毛 邱 秦 江 史 顾 侯 邵 孟 龙 万 段 雷 钱 汤
		尹 黎 易 常 武 乔 贺 赖 龚 文`)
	cLastNames = strings.Fields(`伟 芳 娜 秀英 敏 静 丽 强 磊 军 洋 勇 艳 杰 娟 涛 明 超 秀兰 霞 平 刚 桂英`)
	protocols  = strings.Fields(`http ftp gopher mailto mid cid news nntp prospero telnet rlogin tn3270 wais`)
	tlds       = strings.Fields(`com net org edu gov int mil cn com.cn net.cn gov.cn org.cn 中国 中国互联.公司 中国互联.网络
		tel biz cc tv info name hk mobi asia cd travel pro museum coop aero ad ae af ag ai al am an ao aq ar as at au aw az ba bb bd
		be bf bg bh bi bj bm bn bo br bs bt bv bw by bz ca cc cf cg ch ci ck cl cm cn co cq cr cu cv cx cy cz de dj dk dm do dz ec ee
		eg eh es et ev fi fj fk fm fo fr ga gb gd ge gf gh gi gl gm gn gp gr gt gu gw gy hk hm hn hr ht hu id ie il in io iq ir is it
		jm jo jp ke kg kh ki km kn kp kr kw ky kz la lb lc li lk lr ls lt lu lv ly ma mc md mg mh ml mm mn mo mp mq mr ms mt mv mw
		mx my mz na nc ne nf ng ni nl no np nr nt nu nz om qa pa pe pf pg ph pk pl pm pn pr pt pw py re ro ru rw sa sb sc sd se sg sh
		si sj sk sl sm sn so sr st su sy sz tc td tf tg th tj tk tm tn to tp tr tt tv tw tz ua ug uk us uy va vc ve vg vn vu wf ws ye
		yu za zm zr zw`)
	regions = []string{"东北", "华北", "华东", "华中", "华南", "西南", "西北"}
)

func pickString(list []string) string {
	return list[int(randomNatural(0, float64(len(list)-1)))]
}

func tld() string    { return pickString(tlds) }
func domain() string { return word() + "." + tld() }

// region 是行政区划，数据来自 Mock.js 的 address_dict.js。
type region struct {
	id, name string
	children []*region
}

//go:embed address.txt
var addressText string

// provinces 与原版一样，按编码把 "编码 名称" 的列表组织成 省 → 市 → 区县 的树。
var provinces = func() []*region {
	var list []*region
	byID := map[string]*region{}
	sc := bufio.NewScanner(strings.NewReader(addressText))
	for sc.Scan() {
		id, name, _ := strings.Cut(sc.Text(), " ")
		r := &region{id: id, name: name}
		list = append(list, r)
		byID[id] = r
	}
	var out []*region
	for _, r := range list {
		var pid string
		switch {
		case r.id[2:] == "0000":
			out = append(out, r)
			continue
		case r.id[4:] == "00":
			pid = r.id[:2] + "0000"
		default:
			pid = r.id[:4] + "00"
		}
		if p := byID[pid]; p != nil {
			p.children = append(p.children, r)
		}
	}
	return out
}()

func pickRegion(list []*region) *region {
	if len(list) == 0 {
		return nil
	}
	return list[int(randomNatural(0, float64(len(list)-1)))]
}

// ---- 辅助 ----

// asArray 取出数组参数的元素。
func asArray(v any) ([]any, bool) {
	switch x := v.(type) {
	case *tArray:
		return x.items, true
	case *rArray:
		return x.items, true
	case []any:
		return x, true
	}
	return nil, false
}

func pickOne(items []any) any {
	i := int(randomNatural(0, float64(len(items)-1)))
	if i < 0 || i >= len(items) {
		return undefined
	}
	return items[i]
}

func shuffle[T any](list []T) []T {
	old := append([]T(nil), list...)
	out := make([]T, 0, len(list))
	for len(old) > 0 {
		i := int(randomNatural(0, float64(len(old)-1)))
		out = append(out, old[i])
		old = append(old[:i], old[i+1:]...)
	}
	return out
}

func randomShuffle(args []any) (any, error) {
	var items []any
	if a := arg(args, 0); truthy(a) {
		list, ok := asArray(a)
		if !ok {
			return nil, errors.New("arr.slice is not a function")
		}
		items = list
	}
	result := shuffle(items)
	switch len(args) {
	case 0, 1:
		return &rArray{items: result}, nil
	case 2:
		n := randomNatural(parseInt(args[1]), parseInt(args[1]))
		return &rArray{items: jsSlice(result, n)}, nil
	}
	return &rArray{items: jsSlice(result, randomNatural(parseInt(args[1]), parseInt(args[2])))}, nil
}

func randomPick(args []any) (any, error) {
	list, ok := asArray(arg(args, 0))
	if !ok {
		return pickOne(args), nil // @pick('a', 'b')：参数本身就是候选
	}
	lo, hi := arg(args, 1), arg(args, 2)
	if isUndef(lo) {
		lo = 1.0
	}
	if isUndef(hi) {
		hi = lo
	}
	if lo == any(1.0) && hi == any(1.0) {
		return pickOne(list), nil
	}
	return randomShuffle([]any{&rArray{items: list}, lo, hi})
}

type orderCache struct {
	index int
	items []any
}

func randomOrder(m *mocker, args []any, o *options) (any, error) {
	items, ok := asArray(arg(args, 0))
	if len(args) > 1 || !ok {
		items = args
	}
	key := ""
	for i, p := range o.ctx.templatePath.items {
		if i > 0 {
			key += "."
		}
		key += jsString(toFloatIfInt(p))
	}
	if m.order == nil {
		m.order = map[string]*orderCache{}
	}
	c := m.order[key]
	if c == nil {
		c = &orderCache{items: items}
		m.order[key] = c
	}
	if len(c.items) == 0 {
		return undefined, nil
	}
	v := c.items[c.index%len(c.items)]
	c.index++
	return v, nil
}

func toFloatIfInt(v any) any {
	switch x := v.(type) {
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return v
}

// ---- 杂项 ----

var increment struct {
	sync.Mutex
	v float64
}

func randomIncrement(args []any) any {
	step := toNumber(arg(args, 0))
	if step == 0 || math.IsNaN(step) {
		step = 1
	}
	increment.Lock()
	defer increment.Unlock()
	increment.v += step
	return increment.v
}

func randomID() (any, error) {
	digits, err := randomString([]any{"number", 3.0})
	if err != nil {
		return nil, err
	}
	id := pickRegion(provinces).id + formatDate(randomDate(), "yyyyMMdd") + digits.(string)
	rank := []int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}
	last := []string{"1", "0", "X", "9", "8", "7", "6", "5", "4", "3", "2"}
	sum := 0
	for i := range id {
		sum += int(id[i]-'0') * rank[i]
	}
	return id + last[sum%11], nil
}

func randomGUID() string {
	part := func(n float64) string {
		s, _ := randomString([]any{"abcdefABCDEF1234567890", n})
		return s.(string)
	}
	return part(8) + "-" + part(4) + "-" + part(4) + "-" + part(4) + "-" + part(12)
}

// ---- 注册 ----

// randomFuncs 是全部占位符，键与 Mock.Random 的方法名相同（查找时也接受全小写，如 @EMAIL）。
var randomFuncs map[string]randomFunc

// lowerNames 把小写名映射回方法名（Handler._all）。
var lowerNames = map[string]string{}

func simple(fn func(args []any) any) randomFunc {
	return func(_ *mocker, args []any, _ *options) (any, error) { return fn(args), nil }
}

func withErr(fn func(args []any) (any, error)) randomFunc {
	return func(_ *mocker, args []any, _ *options) (any, error) { return fn(args) }
}

func text(fn func(args []any) (string, error)) randomFunc {
	return func(_ *mocker, args []any, _ *options) (any, error) { return fn(args) }
}

func init() {
	integer := simple(func(a []any) any {
		return randomInteger(intArg(arg(a, 0), -maxSafe), intArg(arg(a, 1), maxSafe))
	})
	character := withErr(func(a []any) (any, error) {
		p, err := characterPool(arg(a, 0))
		return pickRune(p), err
	})
	str := withErr(randomString)
	image := simple(randomImage)
	paragraph := func(sep string, sentence func([]any) (string, error), suffix string) func([]any) (string, error) {
		return func(a []any) (string, error) {
			return repeatJoin(textRange(3, 7, arg(a, 0), arg(a, 1)), sep, func() string { s, _ := sentence(nil); return s + suffix })
		}
	}
	csentence := func(a []any) (string, error) {
		s, err := repeatJoin(textRange(12, 18, arg(a, 0), arg(a, 1)), "", cword)
		return s + "。", err
	}
	dice := func(n float64) randomFunc { return simple(func([]any) any { return randomNatural(1, n) }) }
	guidFn := simple(func([]any) any { return randomGUID() })
	inc := simple(randomIncrement)
	// @city(prefix)、@county(prefix)：prefix 为真时带上省（和市）的名称
	city := func(level int) randomFunc {
		return simple(func(a []any) any {
			p := pickRegion(provinces)
			c := pickRegion(p.children)
			names := []string{p.name, c.name}
			if level == 3 {
				name := "-"
				if county := pickRegion(c.children); county != nil {
					name = county.name
				}
				names = append(names, name)
			}
			if truthy(arg(a, 0)) {
				return strings.Join(names, " ")
			}
			return names[len(names)-1]
		})
	}

	randomFuncs = map[string]randomFunc{
		"boolean": simple(randomBoolean), "bool": simple(randomBoolean),
		"natural": simple(func(a []any) any { return randomNatural(intArg(arg(a, 0), 0), intArg(arg(a, 1), maxSafe)) }),
		"integer": integer, "int": integer,
		"float":     simple(randomFloat),
		"character": character, "char": character,
		"string": str, "str": str,
		"range": withErr(randomRange),

		"date":     dateFunc("yyyy-MM-dd"),
		"time":     dateFunc("HH:mm:ss"),
		"datetime": dateFunc("yyyy-MM-dd HH:mm:ss"),
		"now":      simple(randomNow),

		"image": image, "img": image,
		"dataImage": withErr(func([]any) (any, error) { return nil, errNoCanvas }),

		"color": withErr(func(a []any) (any, error) {
			if name := arg(a, 0); truthy(name) {
				if c, ok := colorNames[jsString(name)]; ok {
					return c, nil
				}
				return nil, jsErrorf("Cannot read property 'nicer' of undefined")
			}
			return randomHex(), nil
		}),
		"hex":  simple(func([]any) any { return randomHex() }),
		"rgb":  simple(func([]any) any { return randomRGB(false) }),
		"rgba": simple(func([]any) any { return randomRGB(true) }),
		"hsl":  simple(func([]any) any { return randomHSL() }),

		"paragraph":  text(paragraph(" ", randomSentence, "")),
		"cparagraph": text(paragraph("", csentence, "")),
		"sentence":   text(randomSentence),
		"csentence":  text(csentence),
		"word":       text(randomWord),
		"cword":      withErr(randomCword),
		"title": text(func(a []any) (string, error) {
			return repeatJoin(textRange(3, 7, arg(a, 0), arg(a, 1)), " ", func() string { return capitalize(word()) })
		}),
		"ctitle": text(func(a []any) (string, error) {
			return repeatJoin(textRange(3, 7, arg(a, 0), arg(a, 1)), "", cword)
		}),

		"first": simple(func([]any) any { return pickString(firstNames) }),
		"last":  simple(func([]any) any { return pickString(lastNames) }),
		"name": simple(func(a []any) any {
			middle := ""
			if truthy(arg(a, 0)) {
				middle = pickString(firstNames) + " "
			}
			return pickString(firstNames) + " " + middle + pickString(lastNames)
		}),
		"cfirst": simple(func([]any) any { return pickString(cFirstNames) }),
		"clast":  simple(func([]any) any { return pickString(cLastNames) }),
		"cname":  simple(func([]any) any { return pickString(cFirstNames) + pickString(cLastNames) }),

		"url": simple(func(a []any) any {
			protocol, host := pickString(protocols), ""
			if p := arg(a, 0); truthy(p) {
				protocol = jsString(p)
			}
			if h := arg(a, 1); truthy(h) {
				host = jsString(h)
			} else {
				host = domain()
			}
			return protocol + "://" + host + "/" + word()
		}),
		"protocol": simple(func([]any) any { return pickString(protocols) }),
		"domain": simple(func(a []any) any {
			if t := arg(a, 0); truthy(t) {
				return word() + "." + jsString(t)
			}
			return domain()
		}),
		"tld": simple(func([]any) any { return tld() }),
		"email": simple(func(a []any) any {
			d := word() + "." + tld()
			local := randomCharacter("lower") + "." + word()
			if v := arg(a, 0); truthy(v) {
				d = jsString(v)
			}
			return local + "@" + d
		}),
		"ip": simple(func([]any) any {
			return jsString(randomNatural(0, 255)) + "." + jsString(randomNatural(0, 255)) + "." +
				jsString(randomNatural(0, 255)) + "." + jsString(randomNatural(0, 255))
		}),

		"region":   simple(func([]any) any { return pickString(regions) }),
		"province": simple(func([]any) any { return pickRegion(provinces).name }),
		"city":     city(2),
		"county":   city(3),
		"zip": simple(func(a []any) any {
			n := 6.0
			if v := arg(a, 0); truthy(v) {
				n = toNumber(v)
			}
			zip := ""
			for i := 0; float64(i) < n && i < 1e5; i++ {
				zip += jsString(randomNatural(0, 9))
			}
			return zip
		}),

		"capitalize": simple(func(a []any) any { return capitalize(jsString(arg(a, 0))) }),
		"upper":      simple(func(a []any) any { return strings.ToUpper(jsString(arg(a, 0))) }),
		"lower":      simple(func(a []any) any { return strings.ToLower(jsString(arg(a, 0))) }),
		"pick":       withErr(randomPick),
		"shuffle":    withErr(randomShuffle),
		"order":      randomOrder,

		"d4": dice(4), "d6": dice(6), "d8": dice(8), "d12": dice(12), "d20": dice(20), "d100": dice(100),
		"guid": guidFn, "uuid": guidFn,
		"id":        withErr(func([]any) (any, error) { return randomID() }),
		"increment": inc, "inc": inc,

		// YApi 在 common/mock-extra.js 里扩展的占位符：当前的 Unix 时间（秒）
		"timestamp": simple(func([]any) any { return float64(time.Now().Unix()) }),
	}
	for name := range randomFuncs {
		lowerNames[strings.ToLower(name)] = name
	}
}
