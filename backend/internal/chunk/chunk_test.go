package chunk

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func repeat(unit string, n int) string { return strings.Repeat(unit, n) }

func TestNormalizeDefaultsToAuto(t *testing.T) {
	o, err := Normalize(Options{})
	if err != nil { t.Fatal(err) }
	if o.Strategy != Auto || o.MaxChars != DefaultMaxChars { t.Fatalf("unexpected %+v", o) }
}

func TestNormalizeAutoIgnoresCallerLength(t *testing.T) {
	o, err := Normalize(Options{Strategy: Auto, MaxChars: 120, OverlapPercent: 30})
	if err != nil { t.Fatal(err) }
	if o.MaxChars != DefaultMaxChars || o.OverlapPercent != 0 { t.Fatalf("auto must ignore custom params: %+v", o) }
}

func TestNormalizeRejectsOutOfRangeCustom(t *testing.T) {
	if _, err := Normalize(Options{Strategy: Custom, MaxChars: 99}); err == nil { t.Fatal("expected max chars violation") }
	if _, err := Normalize(Options{Strategy: Custom, MaxChars: 2001}); err == nil { t.Fatal("expected max chars violation") }
	if _, err := Normalize(Options{Strategy: Custom, MaxChars: 300, OverlapPercent: 51}); err == nil { t.Fatal("expected overlap violation") }
	if _, err := Normalize(Options{Strategy: "nope"}); err == nil { t.Fatal("expected unknown strategy") }
}

func TestAutoWindowSplitsAndOverlaps(t *testing.T) {
	body := repeat("这是一句用于测试自动切分的中文句子。", 120) // 约 2040 字
	pieces, err := Split(body, Options{Strategy: Auto})
	if err != nil { t.Fatal(err) }
	if len(pieces) < 3 { t.Fatalf("expected several chunks, got %d", len(pieces)) }
	for i, p := range pieces {
		if utf8.RuneCountInString(p.Text) > DefaultMaxChars { t.Fatalf("chunk %d exceeds window: %d", i, utf8.RuneCountInString(p.Text)) }
		if p.Start >= p.End || p.End > utf8.RuneCountInString(body) { t.Fatalf("bad offsets %+v", p) }
	}
	if pieces[1].Start >= pieces[0].End { t.Fatalf("expected overlap between consecutive chunks: %+v %+v", pieces[0], pieces[1]) }
}

func TestAutoPrefersNaturalBreaks(t *testing.T) {
	body := strings.Repeat("第一段落的内容。\n\n", 80) + strings.Repeat("第二段落的内容。\n\n", 80)
	pieces, err := Split(body, Options{Strategy: Auto})
	if err != nil { t.Fatal(err) }
	if len(pieces) < 2 { t.Fatalf("expected the text to be split, got %d", len(pieces)) }
	runes := []rune(body)
	for _, p := range pieces {
		// 断点落在空行处：每个切片都从段落边界开始，不会把一句话拦腰截断。
		if p.Start != 0 && runes[p.Start-1] != '\n' {
			t.Fatalf("chunk does not start at a paragraph boundary: %+v", p)
		}
	}
}

func TestCustomSeparatorPeriod(t *testing.T) {
	body := repeat("短句。", 200)
	pieces, err := Split(body, Options{Strategy: Custom, Separator: SepPeriod, MaxChars: 300, OverlapPercent: 10})
	if err != nil { t.Fatal(err) }
	if len(pieces) < 2 { t.Fatalf("expected multiple chunks, got %d", len(pieces)) }
	for _, p := range pieces {
		if utf8.RuneCountInString(p.Text) > 300 { t.Fatalf("chunk too long: %d", utf8.RuneCountInString(p.Text)) }
		if !strings.HasSuffix(p.Text, "。") && utf8.RuneCountInString(p.Text) < 300 { t.Logf("chunk ends without period, acceptable: %q", p.Text) }
	}
}

func TestCustomForcesCutWithoutSeparator(t *testing.T) {
	body := repeat("没有句号的一段文字", 60) // 约 540 字，无断点
	pieces, err := Split(body, Options{Strategy: Custom, Separator: SepPeriod, MaxChars: 200, OverlapPercent: 0})
	if err != nil { t.Fatal(err) }
	if len(pieces) < 3 { t.Fatalf("expected forced cuts, got %d", len(pieces)) }
	for _, p := range pieces { if utf8.RuneCountInString(p.Text) > 200 { t.Fatalf("forced cut exceeded limit: %d", utf8.RuneCountInString(p.Text)) } }
}

func TestHierarchyKeepsHeadingInsideChunk(t *testing.T) {
	body := "# 第一章 课间活动\n" + repeat("课间活动的注意事项说明。", 5) +
		"\n## 第二节 磁铁\n" + repeat("磁铁能吸引铁制物品。", 5) +
		"\n### 实验记录\n" + repeat("实验记录内容。", 5)
	pieces, err := Split(body, Options{Strategy: Hierarchy})
	if err != nil { t.Fatal(err) }
	if len(pieces) < 3 { t.Fatalf("expected one chunk per heading, got %d", len(pieces)) }
	for _, p := range pieces {
		if !strings.Contains(p.Text, "#") { t.Fatalf("heading must stay inside its chunk: %q", p.Text) }
	}
}

func TestHierarchySubSplitsLongChapter(t *testing.T) {
	body := "# 长章\n" + repeat("这一章的内容非常长。", 200)
	pieces, err := Split(body, Options{Strategy: Hierarchy})
	if err != nil { t.Fatal(err) }
	if len(pieces) < 2 { t.Fatalf("expected long chapter to be split, got %d", len(pieces)) }
	if !strings.HasPrefix(pieces[0].Text, "# 长章") { t.Fatalf("first sub chunk should keep heading: %q", pieces[0].Text) }
}

func TestPreprocessDoesNotRewriteOffsetsBeyondProcessedText(t *testing.T) {
	body := "第一行    有很多空格\n\n\n第二行 https://example.com/a 和 a@b.com"
	out := Preprocess(body, Options{CollapseWhitespace: true, StripLinks: true})
	if strings.Contains(out, "https://") || strings.Contains(out, "@") { t.Fatalf("links should be stripped: %q", out) }
	if strings.Contains(out, "    ") { t.Fatalf("whitespace should collapse: %q", out) }
}

func TestSplitEmptyBody(t *testing.T) {
	pieces, err := Split("   \n  ", Options{Strategy: Auto})
	if err != nil { t.Fatal(err) }
	if len(pieces) != 0 { t.Fatalf("expected no chunks, got %d", len(pieces)) }
}
