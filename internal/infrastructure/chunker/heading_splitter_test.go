package chunker

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitByHeadings_BasicSections(t *testing.T) {
	// Each section is intentionally larger than the merge-target (≈
	// ChunkSize/2) so the post-split coalesce pass leaves them as distinct
	// chunks. We're testing per-section emission + breadcrumb here, not
	// merging.
	body := strings.Repeat("Lorem ipsum dolor sit amet consectetur adipiscing elit. ", 4)
	doc := "# Top\n" + body + "\n\n## Section A\n" + body + "\n\n## Section B\n" + body + "\n\n## Section C\n" + body
	cfg := SplitterConfig{ChunkSize: 300, ChunkOverlap: 0}
	chunks := splitByHeadingsImpl(doc, cfg, nil)
	if len(chunks) < 3 {
		t.Fatalf("expected ≥3 chunks (one per section), got %d", len(chunks))
	}

	// Breadcrumb is delivered via ContextHeader, not Content.
	for i, c := range chunks {
		if !strings.Contains(c.ContextHeader, "# Top") {
			t.Errorf("chunk %d missing H1 in ContextHeader:\n%q", i, c.ContextHeader)
		}
		// EmbeddingContent merges header + content for the embedder.
		if !strings.Contains(c.EmbeddingContent(), "# Top") {
			t.Errorf("chunk %d EmbeddingContent missing H1", i)
		}
	}

	found := false
	for _, c := range chunks {
		if strings.Contains(c.Content, "## Section B") && strings.Contains(c.Content, "Lorem ipsum") {
			found = true
		}
	}
	if !found {
		t.Error("no chunk contains Section B with its body")
	}
}

func TestSplitByHeadings_FallsThroughForUnstructuredDoc(t *testing.T) {
	doc := "Just a plain paragraph without any headings at all in this text."
	cfg := SplitterConfig{ChunkSize: 200, ChunkOverlap: 0}
	chunks := splitByHeadingsImpl(doc, cfg, nil)
	// no headings → falls through to SplitText, which keeps the whole thing
	if len(chunks) != 1 {
		t.Errorf("expected fallthrough single chunk, got %d", len(chunks))
	}
}

func TestSplitByHeadings_LargeSectionRecursesIntoLegacy(t *testing.T) {
	body := strings.Repeat("This is a long sentence repeated many times. ", 50)
	doc := "# Top\n## Big\n" + body
	cfg := SplitterConfig{ChunkSize: 300, ChunkOverlap: 30, Separators: []string{". "}}
	chunks := splitByHeadingsImpl(doc, cfg, nil)
	if len(chunks) < 2 {
		t.Fatalf("large section should be sub-split, got %d chunks", len(chunks))
	}
	// Every sub-chunk should carry the breadcrumb via ContextHeader.
	for i, c := range chunks {
		if !strings.Contains(c.ContextHeader, "# Top") {
			t.Errorf("sub-chunk %d missing H1 in ContextHeader", i)
		}
	}
}

func TestSplitByHeadings_BreadcrumbReflectsLatestPath(t *testing.T) {
	// Sized so each section stays its own chunk after the tiny-section
	// coalesce pass — we're verifying breadcrumb assignment per section,
	// not the merge behavior.
	body := strings.Repeat("Lorem ipsum dolor sit amet consectetur adipiscing elit. ", 4)
	doc := "# Chapter 1\n" + body + "\n\n## Section A\n" + body + "\n\n## Section B\n" + body
	cfg := SplitterConfig{ChunkSize: 300, ChunkOverlap: 0}
	chunks := splitByHeadingsImpl(doc, cfg, nil)
	if len(chunks) < 3 {
		t.Fatalf("expected ≥3 chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if strings.Contains(c.Content, "text B") {
			if strings.Contains(c.ContextHeader, "## Section A") {
				t.Errorf("Section B chunk should not include Section A in breadcrumb:\n%s", c.ContextHeader)
			}
			if !strings.Contains(c.ContextHeader, "## Section B") {
				t.Errorf("Section B chunk should include its own heading in breadcrumb:\n%s", c.ContextHeader)
			}
		}
	}
}

func TestSplitByHeadings_IgnoresHeadingsInsideCodeFence(t *testing.T) {
	doc := "# Real\n\n```\n# Fake heading inside code\n```\n\nbody"
	cfg := SplitterConfig{ChunkSize: 500, ChunkOverlap: 0}
	chunks := splitByHeadingsImpl(doc, cfg, nil)
	for _, c := range chunks {
		if strings.Contains(c.ContextHeader, "# Real") || strings.Contains(c.Content, "# Real") {
			return
		}
	}
	t.Error("expected real H1 breadcrumb on some chunk")
}

func TestSplitByHeadings_PreservesPositionRelativeToOriginal(t *testing.T) {
	doc := "# Top\nintro\n\n## A\nbody A\n\n## B\nbody B"
	cfg := SplitterConfig{ChunkSize: 500, ChunkOverlap: 0}
	chunks := splitByHeadingsImpl(doc, cfg, nil)
	for i, c := range chunks {
		if c.Start < 0 {
			t.Errorf("chunk %d has negative Start", i)
		}
		if c.End < c.Start {
			t.Errorf("chunk %d End < Start", i)
		}
	}
}

// TestSplitByHeadings_PositionInvariant ensures End-Start == len(Content)
// and runes[Start:End] == Content for every emitted chunk. This invariant
// is required by knowledge.go:2278+ document reconstruction logic.
func TestSplitByHeadings_PositionInvariant(t *testing.T) {
	doc := `# Top
intro paragraph here.

## Section A
content of A here, several sentences.

## Section B
content of B here.

## Section C
content of C here.`
	cfg := SplitterConfig{ChunkSize: 200, ChunkOverlap: 20}
	chunks := splitByHeadingsImpl(doc, cfg, nil)
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	docRunes := []rune(doc)
	for i, c := range chunks {
		contentRuneLen := len([]rune(c.Content))
		span := c.End - c.Start
		if span != contentRuneLen {
			t.Errorf("chunk %d: span(%d) != content_runes(%d)\nContent:\n%q", i, span, contentRuneLen, c.Content)
		}
		if c.Start >= 0 && c.End <= len(docRunes) {
			if string(docRunes[c.Start:c.End]) != c.Content {
				t.Errorf("chunk %d: runes[Start:End] != Content", i)
			}
		}
	}
}

// TestSplitByHeadings_CoalescesTinyAdjacentSections covers the FAQ /
// install-log case where a parent heading hosts many short sub-sections.
// Without merging, each `##` becomes its own <50-char chunk and the
// validator rejects the tier with "too many tiny chunks". After merging,
// they collapse into a small number of properly-sized chunks while still
// surfacing the shared parent breadcrumb.
func TestSplitByHeadings_CoalescesTinyAdjacentSections(t *testing.T) {
	doc := `# Install Log

## Docker镜像
使用 daocloud 部署 v0.3.1。

## 前端老版本
浏览器缓存了旧前端资源。

## 登录报错
ERROR: column missing.

## 解析失败
embedding 表缺列。`
	cfg := SplitterConfig{ChunkSize: 500, ChunkOverlap: 0}
	chunks := splitByHeadingsImpl(doc, cfg, nil)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
	if len(chunks) >= 5 {
		t.Errorf("expected coalesce to produce <5 chunks, got %d", len(chunks))
	}
	// Every merged chunk must carry the shared parent in its breadcrumb so
	// retrieval can still answer "what document is this from".
	for i, c := range chunks {
		if !strings.Contains(c.ContextHeader, "# Install Log") {
			t.Errorf("chunk %d missing parent H1 in breadcrumb: %q", i, c.ContextHeader)
		}
	}
	// All four sub-section headings must remain visible somewhere in the
	// merged content (heading_splitter keeps the heading line as part of
	// each section's Content).
	for _, h := range []string{"## Docker镜像", "## 前端老版本", "## 登录报错", "## 解析失败"} {
		seen := false
		for _, c := range chunks {
			if strings.Contains(c.Content, h) {
				seen = true
				break
			}
		}
		if !seen {
			t.Errorf("merged chunks should still contain heading %q somewhere", h)
		}
	}
}

func TestSplitByHeadings_DoesNotCoalesceDistinctTopLevelHeadings(t *testing.T) {
	doc := `# Intro
short intro.

# Usage
short usage.

# FAQ
short faq.`
	cfg := SplitterConfig{ChunkSize: 500, ChunkOverlap: 0}
	chunks := splitByHeadingsImpl(doc, cfg, nil)
	if len(chunks) != 3 {
		t.Fatalf("expected one chunk per top-level heading, got %d:\n%v", len(chunks), chunks)
	}
	for i, heading := range []string{"# Intro", "# Usage", "# FAQ"} {
		if !strings.Contains(chunks[i].Content, heading) {
			t.Errorf("chunk %d should contain heading %q, got:\n%s", i, heading, chunks[i].Content)
		}
	}
}

// TestSplitByHeadings_CoalescePreservesPositionInvariant guards the
// End-Start == len([]rune(Content)) invariant after merging. Adjacent
// chunks (cur.End == next.Start) must concatenate cleanly; the merge must
// refuse to combine non-adjacent chunks (e.g. legacy sub-chunks from an
// oversized section that overlap).
func TestSplitByHeadings_CoalescePreservesPositionInvariant(t *testing.T) {
	doc := `# Top

## A
short A.

## B
short B.

## C
short C.`
	cfg := SplitterConfig{ChunkSize: 500, ChunkOverlap: 0}
	chunks := splitByHeadingsImpl(doc, cfg, nil)
	docRunes := []rune(doc)
	for i, c := range chunks {
		contentRuneLen := len([]rune(c.Content))
		if c.End-c.Start != contentRuneLen {
			t.Errorf("chunk %d: End-Start(%d) != content_runes(%d) after merge",
				i, c.End-c.Start, contentRuneLen)
		}
		if c.Start >= 0 && c.End <= len(docRunes) {
			if string(docRunes[c.Start:c.End]) != c.Content {
				t.Errorf("chunk %d: source[Start:End] != Content after merge", i)
			}
		}
	}
}

// TestSplitByHeadings_CoalesceRespectsChunkSize ensures the merge target
// stays within the ChunkSize budget — the validator caps oversize chunks
// at 2x and we should never approach that line via merging.
func TestSplitByHeadings_CoalesceRespectsChunkSize(t *testing.T) {
	const sections = 30
	var sb strings.Builder
	sb.WriteString("# Doc\n")
	for i := 0; i < sections; i++ {
		sb.WriteString("\n## Section ")
		sb.WriteString(strings.Repeat("X", 1)) // unique-ish heading
		sb.WriteString("\nshort body line.\n")
	}
	cfg := SplitterConfig{ChunkSize: 200, ChunkOverlap: 0}
	chunks := splitByHeadingsImpl(sb.String(), cfg, nil)
	for i, c := range chunks {
		if l := len([]rune(c.Content)); l > cfg.ChunkSize {
			t.Errorf("chunk %d exceeds ChunkSize: %d > %d", i, l, cfg.ChunkSize)
		}
	}
}

// TestCommonHeadingPrefix exercises the breadcrumb-prefix helper directly.
func TestCommonHeadingPrefix(t *testing.T) {
	cases := []struct {
		a, b, want string
	}{
		{"# Top\n## A", "# Top\n## B", "# Top"},
		{"# Top", "# Top", "# Top"},
		{"# X", "# Y", ""},
		{"# Top\n## A\n### x", "# Top\n## A\n### y", "# Top\n## A"},
		{"", "# Top", ""},
	}
	for _, tc := range cases {
		got := commonHeadingPrefix(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("commonHeadingPrefix(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestSplitByHeadings_NoBreadcrumbDuplication ensures the section's own
// heading line does not appear twice in the chunk content (once as part of
// the breadcrumb, once as the section's first line).
func TestSplitByHeadings_NoBreadcrumbDuplication(t *testing.T) {
	doc := `# Chapter 1
intro.

## Section A
body A.

## Section B
body B.`
	cfg := SplitterConfig{ChunkSize: 500, ChunkOverlap: 0}
	chunks := splitByHeadingsImpl(doc, cfg, nil)
	for i, c := range chunks {
		// Count occurrences of "## Section A" / "## Section B"
		for _, heading := range []string{"## Section A", "## Section B"} {
			n := strings.Count(c.Content, heading)
			if n > 1 {
				t.Errorf("chunk %d contains %q %d times — duplicated by breadcrumb prepend:\n%s",
					i, heading, n, c.Content)
			}
		}
	}
}

// TestSplitByHeadings_DeepSubHeadingInLargeSection covers issue #1674: when a
// shallow heading level repeats and therefore becomes the dominant split
// backbone, a long section split into sub-chunks must still carry the deep
// `###`/`####` heading that defines a sub-chunk's meaning, not just the
// section-level header. The marker line sits far below its deep heading, in a
// later sub-chunk that has no heading of its own.
func TestSplitByHeadings_DeepSubHeadingInLargeSection(t *testing.T) {
	filler := strings.Repeat("clause body sentence that pads the section out. ", 20)
	doc := "# Standard XYZ\n" +
		"## Preface\n" + filler + "\n\n" +
		"## 5 Classification\n" + filler + "\n\n" +
		"### 5.9 Grade Nine\n" + filler + "\n\n" +
		"#### 5.9.2 Clause Series\n" + filler + "\n\n" +
		"the item users search for is MARKER_ITEM_23 graded here.\n\n" +
		"## Appendix A\n" + filler + "\n\n" +
		"## Appendix B\n" + filler + "\n\n" +
		"## Appendix C\n" + filler

	cfg := SplitterConfig{ChunkSize: 300, ChunkOverlap: 0, Separators: []string{". "}}
	chunks := splitByHeadingsImpl(doc, cfg, nil)

	var marker *Chunk
	for i := range chunks {
		if strings.Contains(chunks[i].Content, "MARKER_ITEM_23") {
			marker = &chunks[i]
			break
		}
	}
	if marker == nil {
		t.Fatal("no chunk contains the marker line")
	}
	if !strings.Contains(marker.ContextHeader, "#### 5.9.2 Clause Series") {
		t.Errorf("marker chunk lost its deep sub-heading in the breadcrumb:\nContextHeader=%q", marker.ContextHeader)
	}
	if !strings.Contains(marker.ContextHeader, "## 5 Classification") {
		t.Errorf("marker chunk lost its section heading in the breadcrumb:\nContextHeader=%q", marker.ContextHeader)
	}
}

// vietnameseLawDoc mimics PaddleOCR-VL output for a Vietnamese decree: some
// Chương/Điều lines come out as `##` headings, others as plain lines, Khoản
// are "1."/"2." lines, the Chương title sits on its own all-caps line, a
// page footer survives, and Điều 3 has a title longer than a child chunk.
var vietnameseLawDoc = "## NGHỊ ĐỊNH\nVề quản lý hoạt động kinh doanh vàng\n\n" +
	"## Chương I NHỮNG QUY ĐỊNH CHUNG\n\n" +
	"Điều 1. Phạm vi điều chỉnh\n" +
	"Nghị định này quy định về hoạt động kinh doanh vàng và quản lý nhà nước đối với hoạt động kinh doanh vàng. MARKER_D1 thuộc Điều 1.\n\n" +
	"## Điều 2. Đối tượng áp dụng\n" +
	"1. Tổ chức tín dụng, chi nhánh ngân hàng nước ngoài được phép kinh doanh vàng theo quy định.\n" +
	"2. Doanh nghiệp kinh doanh vàng và các tổ chức, cá nhân có liên quan đến hoạt động kinh doanh vàng. MARKER_D2 thuộc Điều 2.\n\n" +
	"Trang 3/10\n\n" +
	"Chương II\nHOẠT ĐỘNG SẢN XUẤT, MUA, BÁN VÀNG MIẾNG\n\n" +
	"Mục 1. MUA, BÁN VÀNG MIẾNG\n\n" +
	"Điều 3. Điều kiện cấp Giấy phép kinh doanh mua, bán vàng miếng đối với tổ chức tín dụng và doanh nghiệp, bao gồm cả các trường hợp được sửa đổi, bổ sung theo quy định tại Nghị định số 24/2012/NĐ-CP và Nghị định số 232/2025/NĐ-CP của Chính phủ\n" +
	"1. Doanh nghiệp được Ngân hàng Nhà nước xem xét cấp Giấy phép kinh doanh mua, bán vàng miếng khi đáp ứng đủ các điều kiện về vốn điều lệ.\n" +
	"2. Tổ chức tín dụng được Ngân hàng Nhà nước xem xét cấp Giấy phép kinh doanh mua, bán vàng miếng khi có vốn điều lệ từ ba nghìn tỷ đồng trở lên.\n" +
	"3. Có mạng lưới chi nhánh tại các tỉnh, thành phố trực thuộc trung ương theo quy định của Ngân hàng Nhà nước. MARKER_D3 thuộc Điều 3.\n\n" +
	"Mục 2. XUẤT KHẨU, NHẬP KHẨU VÀNG\n\n" +
	"## Điều 4. Xuất khẩu, nhập khẩu vàng\n" +
	"1. Ngân hàng Nhà nước cấp giấy phép xuất khẩu, nhập khẩu vàng nguyên liệu cho doanh nghiệp sản xuất vàng trang sức, mỹ nghệ.\n" +
	"2. Doanh nghiệp xuất khẩu vàng nguyên liệu phải đáp ứng các điều kiện theo quy định của pháp luật. MARKER_D4 thuộc Điều 4.\n"

// TestSplitParentChild_VietnameseLegalStructure runs the ingestion path
// (auto strategy, parent-child) on a Vietnamese decree. Parents must start at
// a Chương/Mục/Điều boundary, never at a Khoản; every child carries its
// Chương and Điều in the breadcrumb; and the long Điều 3 heading line is not
// cut across child chunks.
func TestSplitParentChild_VietnameseLegalStructure(t *testing.T) {
	base := NormalizeSplitterConfig(SplitterConfig{
		Strategy:  StrategyAuto,
		Languages: []string{LangVietnamese},
	})
	parentCfg, childCfg := DeriveParentChildConfigs(base, 700, 200)
	res := SplitParentChild(vietnameseLawDoc, parentCfg, childCfg)
	if len(res.Parents) < 3 {
		t.Fatalf("expected the decree to split into ≥3 parents, got %d", len(res.Parents))
	}

	for i, p := range res.Parents {
		first := strings.SplitN(strings.TrimSpace(p.Content), "\n", 2)[0]
		if !MarkdownHeadingPattern.MatchString(first) && !VietnameseChapterPattern.MatchString(first) {
			t.Errorf("parent %d starts mid-article at %q", i, first)
		}
	}

	// The breadcrumb is the heading path at the child's start: it must name
	// the enclosing Chương/Mục, and the Điều unless the child itself opens
	// with that Điều's heading.
	type want struct {
		ancestors []string
		dieu      string
	}
	wantHeader := map[string]want{
		"MARKER_D1": {[]string{"Chương I"}, "Điều 1. Phạm vi điều chỉnh"},
		"MARKER_D2": {[]string{"Chương I"}, "Điều 2. Đối tượng áp dụng"},
		"MARKER_D3": {[]string{"Chương II", "Mục 1. MUA, BÁN VÀNG MIẾNG"}, "Điều 3. Điều kiện cấp Giấy phép"},
		"MARKER_D4": {[]string{"Chương II", "Mục 2. XUẤT KHẨU, NHẬP KHẨU VÀNG"}, "Điều 4. Xuất khẩu, nhập khẩu vàng"},
	}
	for marker, w := range wantHeader {
		found := false
		for _, c := range res.Children {
			if !strings.Contains(c.Content, marker) {
				continue
			}
			found = true
			for _, a := range w.ancestors {
				if !strings.Contains(c.ContextHeader, a) {
					t.Errorf("child with %s: breadcrumb lacks %q:\n%s", marker, a, c.ContextHeader)
				}
			}
			if !strings.Contains(c.ContextHeader, w.dieu) && !strings.Contains(c.Content, w.dieu) {
				t.Errorf("child with %s names %q in neither breadcrumb nor content:\n%s", marker, w.dieu, c.ContextHeader)
			}
		}
		if !found {
			t.Errorf("no child contains %s", marker)
		}
	}

	for i, c := range res.Children {
		for _, line := range strings.Split(c.ContextHeader, "\n") {
			title := strings.TrimLeft(line, "# ")
			if title != "" && title[0] >= '0' && title[0] <= '9' {
				t.Errorf("child %d: Khoản line %q used as a heading", i, line)
			}
		}
	}

	longHeading := strings.SplitN(vietnameseLawDoc[strings.Index(vietnameseLawDoc, "Điều 3."):], "\n", 2)[0]
	if utf8.RuneCountInString(longHeading) <= childCfg.ChunkSize {
		t.Fatalf("fixture: Điều 3 heading (%d runes) must exceed the child size %d", utf8.RuneCountInString(longHeading), childCfg.ChunkSize)
	}
	whole := false
	for _, c := range res.Children {
		if strings.Contains(c.Content, longHeading) {
			whole = true
		}
	}
	if !whole {
		t.Error("Điều 3 heading line was cut across child chunks")
	}
}
