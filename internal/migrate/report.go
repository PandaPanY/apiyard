package migrate

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Report 是一次迁移的结果：各集合的数量、跳过和修正的记录。
type Report struct {
	Source      string
	Collections []*CollectionStat
	Skipped     []*Note // 没有迁移的记录，按出现顺序
	Fixed       []*Note // 迁移时修正过的字段
	Notices     []*Note // 照常迁移、但迁移后表现会不同的记录（例如不再执行的 Mock 脚本）
	Unsupported []CollectionCount
	Elapsed     time.Duration

	stats map[string]*CollectionStat
	notes map[string]*Note
}

// CollectionStat 是一个集合的数量统计。
type CollectionStat struct {
	Name     string
	Source   int // 源数据条数
	Migrated int
	Skipped  int
}

// CollectionCount 是 ApiYard 暂不支持、没有迁移的集合。
type CollectionCount struct {
	Name  string
	Count int64
}

// Note 是同一集合里同一原因的记录汇总，Examples 最多保留 maxExamples 个 ID。
type Note struct {
	Collection string
	Reason     string
	Count      int
	Examples   []string
}

const maxExamples = 5

func newReport(source string) *Report {
	return &Report{Source: source, stats: map[string]*CollectionStat{}, notes: map[string]*Note{}}
}

func (r *Report) stat(coll string) *CollectionStat {
	s, ok := r.stats[coll]
	if !ok {
		s = &CollectionStat{Name: coll}
		r.stats[coll] = s
		r.Collections = append(r.Collections, s)
	}
	return s
}

func (r *Report) note(list *[]*Note, kind, coll, reason string, id any) {
	key := kind + "\x00" + coll + "\x00" + reason
	n, ok := r.notes[key]
	if !ok {
		n = &Note{Collection: coll, Reason: reason}
		r.notes[key] = n
		*list = append(*list, n)
	}
	n.Count++
	if len(n.Examples) < maxExamples {
		n.Examples = append(n.Examples, brief(id))
	}
}

// skipped 记录一条没有迁移的记录。
func (r *Report) skipped(coll, reason string, id any) {
	r.stat(coll).Skipped++
	r.note(&r.Skipped, "skip", coll, reason, id)
}

// notice 记录一条需要用户注意的记录。
func (r *Report) notice(coll, reason string, id any) {
	r.note(&r.Notices, "notice", coll, reason, id)
}

// fixed 记录一处修正，记录本身照常迁移。
func (r *Report) fixed(coll, reason string, id any) {
	r.note(&r.Fixed, "fix", coll, reason, id)
}

// Print 以文本表格输出报告。
func (r *Report) Print(w io.Writer) {
	_, _ = fmt.Fprintf(w, "数据来源：%s\n\n", r.Source)
	_, _ = fmt.Fprintf(w, "%s %s %s %s\n", padRight("集合", 16), padLeft("源数据", 10), padLeft("已迁移", 10), padLeft("跳过", 10))
	for _, s := range r.Collections {
		_, _ = fmt.Fprintf(w, "%-16s %10d %10d %10d\n", s.Name, s.Source, s.Migrated, s.Skipped)
	}
	printNotes(w, "跳过的记录", r.Skipped)
	printNotes(w, "修正过的记录（照常迁移）", r.Fixed)
	printNotes(w, "需要注意", r.Notices)
	if len(r.Unsupported) > 0 {
		parts := make([]string, len(r.Unsupported))
		for i, c := range r.Unsupported {
			parts[i] = fmt.Sprintf("%s %d 条", c.Name, c.Count)
		}
		_, _ = fmt.Fprintf(w, "\n未迁移的集合（ApiYard 暂不支持）：%s\n", strings.Join(parts, "，"))
	}
	if r.Elapsed > 0 {
		_, _ = fmt.Fprintf(w, "\n耗时 %.1f 秒\n", r.Elapsed.Seconds())
	}
}

func printNotes(w io.Writer, title string, notes []*Note) {
	if len(notes) == 0 {
		return
	}
	_, _ = fmt.Fprintf(w, "\n%s：\n", title)
	for _, n := range notes {
		more := ""
		if n.Count > len(n.Examples) {
			more = " 等"
		}
		_, _ = fmt.Fprintf(w, "  %s：%s × %d（_id %s%s）\n", n.Collection, n.Reason, n.Count, strings.Join(n.Examples, "、"), more)
	}
}

// displayWidth 返回终端里的显示宽度：中文等宽字符占两格。
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if r >= 0x1100 {
			w += 2
		} else {
			w++
		}
	}
	return w
}

func padRight(s string, w int) string { return s + strings.Repeat(" ", max(0, w-displayWidth(s))) }
func padLeft(s string, w int) string  { return strings.Repeat(" ", max(0, w-displayWidth(s))) + s }

func sortCounts(list []CollectionCount) {
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
}
