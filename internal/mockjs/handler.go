package mockjs

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/PandaPanY/apiyard/internal/jsjson"
)

// mocker 保存一次 Mock 调用的状态。
type mocker struct {
	guid  int64
	order map[string]*orderCache // @order 的游标，按模板路径区分
}

// stack 是共享的访问路径，与原版一样在递归时 push / pop。
type stack struct {
	items []any
}

func (s *stack) push(v any) { s.items = append(s.items, v) }
func (s *stack) pop()       { s.items = s.items[:len(s.items)-1] }

// genContext 对应 Handler.gen 的 context。
type genContext struct {
	path, templatePath     *stack // 当前访问路径（只有属性名）和模板路径（包括生成规则）
	currentContext         any    // 最终值的上下文：正在生成的 *jsjson.Object 或 *rArray
	templateCurrentContext any    // 模板的上下文
	root, templateRoot     any
}

// options 对应 Handler 各方法的 options。
type options struct {
	template any
	name     string
	rule     rule
	ctx      *genContext
}

// gen 对应 Handler.gen：按模板的类型生成数据。
func (m *mocker) gen(template any, name string, in *genContext) (any, error) {
	if in == nil {
		in = &genContext{}
	}
	c := *in
	if c.path == nil {
		c.path = &stack{items: []any{m.guid}}
	}
	if c.templatePath == nil {
		c.templatePath = &stack{items: []any{guid.Add(1)}}
	}
	if c.templateCurrentContext == nil {
		c.templateCurrentContext = template
	}
	if c.root == nil {
		c.root = in.currentContext
	}
	if c.templateRoot == nil {
		c.templateRoot = in.templateCurrentContext
		if c.templateRoot == nil {
			c.templateRoot = template
		}
	}
	o := &options{template: template, name: name, rule: parseRule(name), ctx: &c}
	switch jsType(template) {
	case "array":
		return m.array(o)
	case "object":
		return m.object(o)
	case "number":
		return m.number(o), nil
	case "boolean":
		return m.boolean(o), nil
	case "string":
		return m.string(o)
	case "regexp":
		return m.regexp(o)
	}
	return template, nil
}

func firstNonNil(a, b any) any {
	if a != nil {
		return a
	}
	return b
}

// child 是生成子元素时用的上下文。
func (o *options) child(result any) *genContext {
	return &genContext{
		path: o.ctx.path, templatePath: o.ctx.templatePath,
		currentContext: result, templateCurrentContext: o.template,
		root: firstNonNil(o.ctx.root, result), templateRoot: firstNonNil(o.ctx.templateRoot, o.template),
	}
}

func (m *mocker) array(o *options) (any, error) {
	tpl := o.template.(*tArray)
	result := &rArray{}
	if len(tpl.items) == 0 {
		return result, nil
	}
	ctx := o.ctx
	switch {
	case o.rule.parameters == nil:
		// 'arr': [{ 'email': '@EMAIL' }, { 'email': '@EMAIL' }]
		for i := range tpl.items {
			ctx.path.push(i)
			ctx.templatePath.push(i)
			v, err := m.gen(tpl.items[i], strconv.Itoa(i), o.child(result))
			ctx.path.pop()
			ctx.templatePath.pop()
			if err != nil {
				return nil, err
			}
			result.items = append(result.items, v)
		}
		return result, nil

	case o.rule.min != nil && *o.rule.min == 1 && o.rule.max == nil:
		// 'method|1': ['GET', 'POST', 'HEAD', 'DELETE']：先生成整个数组，再随机取一个
		all, err := m.genWhole(o, result)
		if err != nil {
			return nil, err
		}
		return pickOne(all.items), nil

	case o.rule.parameters[2] != "":
		// 'data|+1': [{}, {}]：按顺序取
		all, err := m.genWhole(o, result)
		if err != nil {
			return nil, err
		}
		v := all.items[tpl.orderIndex%len(all.items)]
		step, _ := strconv.Atoi(o.rule.parameters[2])
		tpl.orderIndex += step
		return v, nil
	}

	// 'data|1-10': [{}]：重复 count 次
	count := math.NaN()
	if o.rule.count != nil {
		count = *o.rule.count
	}
	for i := 0; float64(i) < count; i++ {
		for ii := range tpl.items {
			index := len(result.items)
			ctx.path.push(index)
			ctx.templatePath.push(ii)
			v, err := m.gen(tpl.items[ii], strconv.Itoa(index), o.child(result))
			ctx.path.pop()
			ctx.templatePath.pop()
			if err != nil {
				return nil, err
			}
			result.items = append(result.items, v)
		}
	}
	return result, nil
}

// genWhole 不带规则地生成整个数组模板。
func (m *mocker) genWhole(o *options, result *rArray) (*rArray, error) {
	o.ctx.path.push(o.name)
	o.ctx.templatePath.push(o.name)
	defer o.ctx.path.pop()
	defer o.ctx.templatePath.pop()
	v, err := m.gen(o.template, "", o.child(result))
	if err != nil {
		return nil, err
	}
	return v.(*rArray), nil
}

func (m *mocker) object(o *options) (any, error) {
	tpl := o.template.(*tObject)
	result := jsjson.NewObject()
	ctx := o.ctx
	genKey := func(key string) error {
		pk := parsedName(key)
		ctx.path.push(pk)
		ctx.templatePath.push(key)
		v, err := m.gen(tpl.vals[key], key, o.child(result))
		ctx.path.pop()
		ctx.templatePath.pop()
		if err != nil {
			return err
		}
		result.Set(pk, v)
		return nil
	}

	if o.rule.min != nil {
		// 'obj|min-max': {}：随机取 count 个属性
		keys := shuffle(append([]string(nil), tpl.keys...))
		count := math.NaN()
		if o.rule.count != nil {
			count = *o.rule.count
		}
		for _, key := range jsSlice(keys, count) {
			if err := genKey(key); err != nil {
				return nil, err
			}
		}
		return result, nil
	}

	for _, key := range append([]string(nil), tpl.keys...) {
		if err := genKey(key); err != nil {
			return nil, err
		}
		// 'id|+1': 1：每生成一次，模板里的值加 step（数组重复生成同一个模板时就会递增）
		if inc := reKey.FindStringSubmatch(key); inc != nil && inc[2] != "" {
			if f, ok := tpl.vals[key].(float64); ok {
				tpl.vals[key] = f + parseInt(inc[2])
			}
		}
	}
	return result, nil
}

func (m *mocker) number(o *options) any {
	tpl := o.template.(float64)
	r := o.rule
	if r.hasDecimal {
		// 'float1|.1-10': 10、'float2|1-100.1-10': 1
		parts := strings.Split(jsjson.FormatNumber(tpl), ".")
		if r.hasRange {
			parts[0] = jsString(*r.count)
		}
		frac := ""
		if len(parts) > 1 {
			frac = parts[1]
		}
		dcount := *r.dcount
		if float64(len(frac)) > dcount {
			frac = frac[:max(0, int(dcount))]
		}
		for float64(len(frac)) < dcount {
			// 最后一位不能为 0，否则会被 JS 引擎忽略掉
			if float64(len(frac)) < dcount-1 {
				frac += randomCharacter("number")
			} else {
				frac += randomCharacter("123456789")
			}
		}
		return parseFloat(parts[0] + "." + frac)
	}
	// 'grade1|1-100': 1
	if r.hasRange && r.parameters[2] == "" {
		return *r.count
	}
	return tpl
}

func (m *mocker) boolean(o *options) any {
	// 'prop|multiple': false 表示当前值是相反值的概率倍数；'prop|min-max': false 表示当前值与相反值的概率
	if o.rule.parameters == nil {
		return o.template
	}
	var minArg, maxArg any = undefined, undefined
	if o.rule.min != nil {
		minArg = *o.rule.min
	}
	if o.rule.max != nil {
		maxArg = *o.rule.max
	}
	return randomBoolean([]any{minArg, maxArg, o.template})
}

func (m *mocker) string(o *options) (any, error) {
	tpl := o.template.(string)
	if tpl == "" {
		// 'ASCII|1-10': ''
		if o.rule.hasRange {
			return randomString([]any{*o.rule.count})
		}
		return tpl, nil
	}
	result := ""
	if o.rule.count == nil {
		result += tpl
	} else {
		for i := 0; float64(i) < *o.rule.count; i++ {
			result += tpl
		}
	}
	placeholders := rePlaceholder.FindAllString(result, -1)
	for i := 0; i < len(placeholders); i++ {
		ph := placeholders[i]
		if strings.HasPrefix(ph, `\`) { // 前面有反斜杠转义，不解析
			placeholders = append(placeholders[:i], placeholders[i+1:]...)
			i--
			continue
		}
		phed, err := m.placeholder(ph, o.ctx.currentContext, o.ctx.templateCurrentContext, o)
		if err != nil {
			return nil, err
		}
		// 只有一个占位符、没有其他字符时，保留占位符结果的类型（例如 '@integer' 得到数字）
		if len(placeholders) == 1 && ph == result && typeOf(phed) != "string" {
			return phed, nil
		}
		result = jsReplace(result, ph, jsString(phed))
	}
	return result, nil
}

func (m *mocker) regexp(o *options) (any, error) {
	tpl := o.template.(RegExp)
	source := ""
	if o.rule.count == nil {
		source += tpl.Source
	} else {
		for i := 0; float64(i) < *o.rule.count; i++ {
			source += tpl.Source
		}
	}
	return genRegexp(source), nil
}

// placeholder 计算一个占位符。obj 是正在生成的对象（优先引用里面已经生成的属性），templateContext 是对应的模板。
func (m *mocker) placeholder(ph string, obj, templateContext any, o *options) (any, error) {
	parts := rePlaceholder.FindStringSubmatchIndex(ph)
	if parts == nil {
		return ph, nil
	}
	key := ph[parts[2]:parts[3]]
	lkey := strings.ToLower(key)
	params := []any{}
	if parts[4] >= 0 {
		params = parseParams(ph[parts[4]:parts[5]])
	}
	pathParts := splitPath(key)

	// 占位符优先引用数据模板中的属性
	if v, ok := lookup(obj, key); ok {
		return v, nil
	}
	// 绝对路径或相对路径
	if strings.HasPrefix(key, "/") || len(pathParts) > 1 {
		return m.valueByKeyPath(key, o)
	}
	// 递归引用数据模板中的属性：先计算被引用的属性，写回模板（避免自己依赖自己）
	if v, ok := lookup(templateContext, key); ok && (jsType(templateContext) == "object" || jsType(templateContext) == "array") && v != any(ph) {
		gv, err := m.gen(v, key, &genContext{currentContext: obj, templateCurrentContext: templateContext})
		if err != nil {
			return nil, err
		}
		assign(templateContext, key, fromJS(toJS(gv)))
		return gv, nil
	}

	fn, ok := randomFuncs[key]
	if !ok {
		fn, ok = randomFuncs[lkey]
	}
	if !ok {
		fn, ok = randomFuncs[lowerNames[lkey]]
	}
	if !ok {
		return ph, nil // 不认识的占位符原样保留
	}
	// 参数里也可以有占位符
	for i, p := range params {
		if s, isStr := p.(string); isStr && rePlaceholder.MatchString(s) {
			v, err := m.placeholder(s, obj, templateContext, o)
			if err != nil {
				return nil, err
			}
			params[i] = v
		}
	}
	v, err := fn(m, params, o)
	if err != nil {
		return nil, err
	}
	if v == any(undefined) {
		return "", nil // 在字符串里，所以默认为空字符串
	}
	return v, nil
}

// valueByKeyPath 对应 Handler.getValueByKeyPath：按 '@/a/b' 或 '@../x' 这样的路径引用其他属性。
func (m *mocker) valueByKeyPath(key string, o *options) (any, error) {
	original := key
	keyParts := splitPath(key)
	var abs []any
	if strings.HasPrefix(key, "/") {
		abs = append([]any{o.ctx.path.items[0]}, normalizePath(keyParts)...)
	} else if len(keyParts) > 1 {
		abs = append([]any(nil), o.ctx.path.items...)
		abs = abs[:len(abs)-1]
		abs = normalizePath(append(abs, keyParts...))
	}
	if len(keyParts) == 0 {
		return undefined, nil
	}
	key = jsString(keyParts[len(keyParts)-1])
	cur, tcur := o.ctx.root, o.ctx.templateRoot
	for i := 1; i < len(abs)-1; i++ {
		name := jsString(abs[i])
		if cur == nil || cur == any(undefined) {
			return nil, jsErrorf("Cannot read property '%s' of undefined", name)
		}
		cur, _ = lookup(cur, name)
		if cur == nil {
			cur = undefined
		}
		if tcur == nil || tcur == any(undefined) {
			return nil, jsErrorf("Cannot read property '%s' of undefined", name)
		}
		tcur, _ = lookup(tcur, name)
		if tcur == nil {
			tcur = undefined
		}
	}
	if v, ok := lookup(cur, key); ok {
		return v, nil
	}
	if v, ok := lookup(tcur, key); ok && v != any(original) {
		gv, err := m.gen(v, key, &genContext{currentContext: cur, templateCurrentContext: tcur})
		if err != nil {
			return nil, err
		}
		assign(tcur, key, fromJS(toJS(gv)))
		return gv, nil
	}
	return undefined, nil
}

// lookup 对应 JS 的 (key in obj) && obj[key]，只看自身的属性；数组支持下标和 length。
func lookup(container any, key string) (any, bool) {
	switch c := container.(type) {
	case *jsjson.Object:
		return c.Get(key)
	case *tObject:
		return c.get(key)
	case *rArray:
		return arrayLookup(c.items, key)
	case *tArray:
		return arrayLookup(c.items, key)
	}
	return nil, false
}

func arrayLookup(items []any, key string) (any, bool) {
	if key == "length" {
		return float64(len(items)), true
	}
	i, err := strconv.Atoi(key)
	if err != nil || i < 0 || i >= len(items) || strconv.Itoa(i) != key {
		return nil, false
	}
	return items[i], true
}

// assign 把计算好的值写回模板。
func assign(container any, key string, v any) {
	switch c := container.(type) {
	case *tObject:
		if _, ok := c.vals[key]; !ok {
			c.keys = append(c.keys, key)
		}
		c.vals[key] = v
	case *tArray:
		if i, err := strconv.Atoi(key); err == nil && i >= 0 && i < len(c.items) {
			c.items[i] = v
		}
	}
}

// splitPath 对应 Handler.splitPathToArray。
func splitPath(p string) []any {
	var parts []any
	for _, s := range splitSlashes(p) {
		parts = append(parts, s)
	}
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > 0 && parts[0] == "" {
		parts = parts[1:]
	}
	return parts
}

// splitSlashes 对应 path.split(/\/+/)。
func splitSlashes(p string) []string {
	var out []string
	start := 0
	for i := 0; i < len(p); {
		if p[i] != '/' {
			i++
			continue
		}
		out = append(out, p[start:i])
		for i < len(p) && p[i] == '/' {
			i++
		}
		start = i
	}
	return append(out, p[start:])
}

func normalizePath(parts []any) []any {
	var out []any
	for _, p := range parts {
		switch p {
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		case ".":
		default:
			out = append(out, p)
		}
	}
	return out
}

// jsSlice 对应 arr.slice(0, end)：end 为负数时从末尾往前数，NaN 视为 0。
func jsSlice[T any](list []T, end float64) []T {
	if math.IsNaN(end) {
		return nil
	}
	n := len(list)
	e := int(math.Trunc(end))
	if end < 0 {
		e = max(n+e, 0)
	}
	return list[:min(e, n)]
}

// jsReplace 对应 str.replace(substr, replacement)：只替换第一处，replacement 里的 $$、$&、$`、$' 有特殊含义。
func jsReplace(s, substr, repl string) string {
	i := strings.Index(s, substr)
	if i < 0 {
		return s
	}
	if strings.Contains(repl, "$") {
		var b strings.Builder
		for j := 0; j < len(repl); j++ {
			if repl[j] != '$' || j+1 == len(repl) {
				b.WriteByte(repl[j])
				continue
			}
			switch repl[j+1] {
			case '$':
				b.WriteByte('$')
			case '&':
				b.WriteString(substr)
			case '`':
				b.WriteString(s[:i])
			case '\'':
				b.WriteString(s[i+len(substr):])
			default:
				b.WriteByte('$')
				continue
			}
			j++
		}
		repl = b.String()
	}
	return s[:i] + repl + s[i+len(substr):]
}

// parseFloat 对应 JS 的 parseFloat：取开头能解析的部分。
func parseFloat(s string) float64 {
	s = trimJSSpace(s)
	if m := reFloatPrefix.FindString(s); m != "" {
		if f, err := strconv.ParseFloat(m, 64); err == nil {
			return f
		}
	}
	for _, inf := range []string{"Infinity", "+Infinity"} {
		if strings.HasPrefix(s, inf) {
			return math.Inf(1)
		}
	}
	if strings.HasPrefix(s, "-Infinity") {
		return math.Inf(-1)
	}
	return math.NaN()
}

var errNoCanvas = jsErrorf("document is not defined") // 原版的 @dataImage 依赖浏览器的 canvas，在 Node 里会出这个错

// jsError 是与原版 JS 异常信息相同的错误（例如 "Cannot read property 'x' of undefined"），会原样出现在响应里。
type jsError string

func (e jsError) Error() string { return string(e) }

func jsErrorf(format string, args ...any) error { return jsError(fmt.Sprintf(format, args...)) }
