// Package chunk 把材料正文切分为可检索的切片。
//
// 切分只读取待切分文本，不调用嵌入服务，也不写任何存储：返回切片文本、序号
// 与其在「预处理之后」的文本中的位置。预处理（移除链接/邮箱、折叠空白）只作用
// 于切分与嵌入的输入，不改写 knowledge_entries.body_text，因此这里的偏移量
// 不能当作原文件中的字符下标。
package chunk

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Strategy 是三种切分策略。
type Strategy string

const (
	Auto      Strategy = "auto"
	Custom    Strategy = "custom"
	Hierarchy Strategy = "hierarchy"
)

// 自动窗口的默认值：最大约 800 字，重叠约 80 字。
const (
	DefaultMaxChars        = 800
	DefaultOverlapChars    = 80
	MinCustomMaxChars      = 100
	MaxCustomMaxChars      = 2000
	MinOverlapPercent      = 0
	MaxOverlapPercent      = 50
)

// Separator 是自定义策略下的分隔符选择。
type Separator string

const (
	SepNewline    Separator = "newline"
	SepBlankLine  Separator = "blankline"
	SepPeriod     Separator = "period"
)

// Options 描述一次切分请求。策略为 auto 时，除 Strategy 之外的字段均不生效。
type Options struct {
	Strategy           Strategy
	Separator          Separator
	MaxChars           int
	OverlapPercent     int
	CollapseWhitespace bool
	StripLinks         bool
}

// Piece 是一个切片：Text 为切片正文，Start/End 为相对预处理后文本的字符区间。
type Piece struct {
	Text  string
	Start int
	End   int
}

var (
	urlPattern   = regexp.MustCompile(`https?://[^\s，。；）)"'\]]+`)
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	blankLine    = regexp.MustCompile(`\n[ \t]*\n`)
	spaceRun     = regexp.MustCompile("[ \t\u3000]+")
	headingLine  = regexp.MustCompile(`(?m)^#{1,6}[ \t]+\S`)
)

// Normalize 把请求中的策略参数补齐并校验。空字符串按 auto 处理。
func Normalize(o Options) (Options, error) {
	if strings.TrimSpace(string(o.Strategy)) == "" { o.Strategy = Auto }
	switch o.Strategy {
	case Auto:
		// 自动窗口忽略调用方另行填写的长度与预处理参数。
		o.Separator = ""
		o.MaxChars = DefaultMaxChars
		o.OverlapPercent = 0
		o.CollapseWhitespace = false
		o.StripLinks = false
	case Custom:
		if o.MaxChars == 0 { o.MaxChars = DefaultMaxChars }
		if o.MaxChars < MinCustomMaxChars || o.MaxChars > MaxCustomMaxChars {
			return o, fmt.Errorf("chunk_max_chars must be between %d and %d", MinCustomMaxChars, MaxCustomMaxChars)
		}
		if o.OverlapPercent < MinOverlapPercent || o.OverlapPercent > MaxOverlapPercent {
			return o, fmt.Errorf("chunk_overlap_percent must be between %d and %d", MinOverlapPercent, MaxOverlapPercent)
		}
		switch o.Separator {
		case "", SepNewline, SepBlankLine, SepPeriod:
		default:
			return o, fmt.Errorf("unknown chunk_separator %q", o.Separator)
		}
	case Hierarchy:
		o.Separator = ""
		o.MaxChars = DefaultMaxChars
		o.OverlapPercent = 0
	default:
		return o, fmt.Errorf("unknown chunk_strategy %q", o.Strategy)
	}
	return o, nil
}

// Preprocess 只作用于切分与嵌入的输入，不写回原文。
func Preprocess(body string, o Options) string {
	if o.StripLinks {
		body = urlPattern.ReplaceAllString(body, " ")
		body = emailPattern.ReplaceAllString(body, " ")
	}
	if o.CollapseWhitespace {
		body = blankLine.ReplaceAllString(body, "\n")
		body = spaceRun.ReplaceAllString(body, " ")
	}
	return strings.TrimSpace(body)
}

// Split 按策略切分已预处理的正文。
func Split(body string, o Options) ([]Piece, error) {
	o, err := Normalize(o)
	if err != nil { return nil, err }
	body = Preprocess(body, o)
	if body == "" { return []Piece{}, nil }
	if o.Strategy == Hierarchy { return splitHierarchy(body, o) }
	return splitByWindow(body, o), nil
}

func splitByWindow(body string, o Options) []Piece {
	o, _ = Normalize(o) // 保证 MaxChars 已按策略补齐
	maxChars := o.MaxChars
	overlap := overlapChars(o)
	breakers := breakersFor(o)
	pieces := []Piece{}
	runes := []rune(body)
	total := len(runes)
	start := 0
	for start < total {
		if total-start <= maxChars {
			p := trimPiece(runes, start, total)
			if p != nil { pieces = append(pieces, *p) }
			break
		}
		windowEnd := start + maxChars
		cut := findBreak(runes, start, windowEnd, breakers)
		if cut <= start { cut = windowEnd }
		p := trimPiece(runes, start, cut)
		if p == nil { break }
		pieces = append(pieces, *p)
		next := cut - overlap
		if next <= start { next = cut }
		start = next
	}
	return pieces
}

// overlapChars 计算重叠字符数：custom 按最大长度的比例，auto/hierarchy 用固定值。
func overlapChars(o Options) int {
	if o.Strategy == Custom {
		return o.MaxChars * o.OverlapPercent / 100
	}
	return DefaultOverlapChars
}

// breakersFor 返回按优先级排列的断点：下标越小优先级越高。
func breakersFor(o Options) []string {
	if o.Strategy == Custom {
		switch o.Separator {
		case SepNewline:
			return []string{"\n"}
		case SepBlankLine:
			return []string{"\n\n"}
		case SepPeriod:
			return []string{"。"}
		default:
			return []string{"\n"}
		}
	}
	// auto / hierarchy 的细分：优先空行，其次换行，最后句号。
	return []string{"\n\n", "\n", "。"}
}

// findBreak 在 [start+min, end) 区间内从后往前找优先级最高的断点，返回切分位置（断点之后）。
// 找不到时返回 end，由调用方按最大长度强制截断。
func findBreak(runes []rune, start, end int, breakers []string) int {
	minKeep := start + end/2 - start/2 // 至少保留窗口的一半，避免切出极短切片
	if minKeep < start+1 { minKeep = start + 1 }
	for _, b := range breakers {
		br := []rune(b)
		for i := end - 1; i >= minKeep && i+len(br) <= end; i-- {
			if matchesAt(runes, i, br) {
				pos := i + len(br)
				if pos > start && pos <= end { return pos }
			}
		}
	}
	return end
}

func matchesAt(runes []rune, at int, pattern []rune) bool {
	if at+len(pattern) > len(runes) { return false }
	for i, r := range pattern { if runes[at+i] != r { return false } }
	return true
}

// trimPiece 去掉切片两端空白，并把 Start/End 调整为去空白后的真实位置。
func trimPiece(runes []rune, start, end int) *Piece {
	if end > len(runes) { end = len(runes) }
	s, e := start, end
	for s < e && isSpace(runes[s]) { s++ }
	for e > s && isSpace(runes[e-1]) { e-- }
	if s >= e { return nil }
	return &Piece{Text: string(runes[s:e]), Start: s, End: e}
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\u3000' }

// splitHierarchy 按 Markdown 标题分章；标题保留在该章切片内，超长章节再按自动窗口细分。
func splitHierarchy(body string, o Options) ([]Piece, error) {
	runes := []rune(body)
	// 正则给出的是字节下标，这里统一换算成 rune 下标后再切分。
	byteHeads := headingLine.FindAllStringIndex(body, -1)
	if len(byteHeads) == 0 { return splitByWindow(body, Options{Strategy: Auto}), nil }
	heads := make([]int, 0, len(byteHeads))
	for _, h := range byteHeads { heads = append(heads, utf8.RuneCountInString(body[:h[0]])) }
	// 标题行之前若存在正文，先作为独立一章处理。
	sections := [][2]int{}
	if heads[0] > 0 { sections = append(sections, [2]int{0, heads[0]}) }
	for i, h := range heads {
		end := len(runes)
		if i+1 < len(heads) { end = heads[i+1] }
		sections = append(sections, [2]int{h, end})
	}
	pieces := []Piece{}
	for _, sec := range sections {
		s, e := sec[0], sec[1]
		// 去掉章节末尾空白，避免下一章标题前出现空切片。
		for e > s && isSpace(runes[e-1]) { e-- }
		if e <= s { continue }
		section := string(runes[s:e])
		if utf8.RuneCountInString(section) <= o.MaxChars {
			if p := trimPiece(runes, s, e); p != nil { pieces = append(pieces, *p) }
			continue
		}
		sub := splitByWindow(section, Options{Strategy: Auto})
		for _, sp := range sub {
			pieces = append(pieces, Piece{Text: sp.Text, Start: s + sp.Start, End: s + sp.End})
		}
	}
	return pieces, nil
}
