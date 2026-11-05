// Package chunker - patterns.go is the source of truth for multilingual
// regex patterns used by the heading-aware and heuristic splitters.
//
// Patterns are grouped by purpose (chapter markers, numbering, separators)
// and tagged with a priority that the heuristic splitter uses to rank
// candidate chunk boundaries.
package chunker

import (
	"regexp"
	"slices"
	"strings"
)

// BoundaryPriority levels for heuristic chunk boundaries. Higher = stronger.
const (
	PrioFormFeed       = 100
	PrioNumberedHead   = 90
	PrioChapterMarker  = 85
	PrioAllCapsHeading = 70
	PrioVisualSep      = 60
	PrioPageFooter     = 50
	PrioBlankBlock     = 40
)

// MarkdownHeadingPattern matches an ATX-style Markdown heading at line start.
// Capture groups: (1) hashes, (2) heading text.
// Closing hashes are delimiters only after a space or tab; otherwise they
// belong to the title (for example, C# and F#).
var MarkdownHeadingPattern = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+?)(?:[ \t]+#+)?\s*$`)

// FormFeedPattern matches the form-feed control character used by some PDF
// converters as a page break marker.
var FormFeedPattern = regexp.MustCompile(`\f`)

// NumberedSectionPattern matches lines starting with numeric or roman numbering
// followed by a non-empty title, e.g. "1. Intro", "2.3 Methods", "IV. Results",
// "2.2.1 用户与权限". The trailing dot after a multi-level numeral is optional
// because many technical documents write "1.1 Foo" without a closing dot.
var NumberedSectionPattern = regexp.MustCompile(`(?m)^[ \t]*(?:\d+(?:\.\d+){1,3}\.?|(?:\d+|[IVX]{1,5})\.)[ \t]+\S.{0,200}$`)

// VietnameseNumberedSectionPattern is NumberedSectionPattern without the
// single-level Arabic form "1. Title". Vietnamese legal text uses "1.", "2."
// for Khoản (clauses inside an Điều), which are not section headings.
var VietnameseNumberedSectionPattern = regexp.MustCompile(`(?m)^[ \t]*(?:\d+(?:\.\d+){1,3}\.?|[IVX]{1,5}\.)[ \t]+\S.{0,200}$`)

// vietnameseUpper lists the precomposed Vietnamese capitals outside A-Z.
const vietnameseUpper = `ÀÁẢÃẠĂẰẮẲẴẶÂẦẤẨẪẬĐÈÉẺẼẸÊỀẾỂỄỆÌÍỈĨỊÒÓỎÕỌÔỒỐỔỖỘƠỜỚỞỠỢÙÚỦŨỤƯỪỨỬỮỰỲÝỶỸỴ`

// AllCapsHeadingPattern matches short all-caps lines (likely section titles
// rendered without Markdown headings). It requires at least 4 letters and
// up to ~10 words. Trailing colons are tolerated. Vietnamese capitals and
// commas are accepted ("HOẠT ĐỘNG SẢN XUẤT, MUA, BÁN VÀNG MIẾNG").
var AllCapsHeadingPattern = regexp.MustCompile(`(?m)^[ \t]*([A-ZÄÖÜ` + vietnameseUpper + `][A-ZÄÖÜ` + vietnameseUpper + ` ,\-]{3,80}):?\s*$`)

// VisualSeparatorPattern matches horizontal rules / divider lines used as
// section separators in plain text or pre-Markdown documents.
var VisualSeparatorPattern = regexp.MustCompile(`(?m)^[ \t]*(?:-{3,}|={3,}|\*{3,}|_{3,})[ \t]*$`)

// ExcessiveBlanksPattern matches three or more consecutive newlines, which
// usually denote a hard section break.
var ExcessiveBlanksPattern = regexp.MustCompile(`\n{3,}`)

// PageFooterPattern matches typical "Seite X von Y" / "Page X of Y" /
// "Trang X/Y" lines.
var PageFooterPattern = regexp.MustCompile(`(?mi)^[ \t]*(?:Seite|Page|Trang|页码?)\s+\d+(?:\s*(?:von|of|/)\s*\d+)?[ \t]*$`)

// GermanChapterPattern matches German chapter / section markers.
var GermanChapterPattern = regexp.MustCompile(`(?m)^[ \t]*(?:Kapitel|Abschnitt|Teil)\s+(?:[0-9]+|[IVX]{1,5})[\.: ].{0,200}$`)

// EnglishChapterPattern matches English chapter / section markers.
var EnglishChapterPattern = regexp.MustCompile(`(?m)^[ \t]*(?:Chapter|Section|Part)\s+(?:[0-9]+|[IVX]{1,5})[\.: ].{0,200}$`)

// ChineseChapterPattern matches CJK chapter / section markers like 第一章,
// 第3节, 第 1 章 (whitespace between 第 / numeral / unit is tolerated).
var ChineseChapterPattern = regexp.MustCompile(`(?m)^[ \t]*第[ \t]*[一二三四五六七八九十百千零〇0-9]+[ \t]*(?:章|节|節|部分|篇)[ \t]?.{0,200}$`)

// VietnameseChapterPattern matches Vietnamese legal structure markers: Phần,
// Chương or Mục with a Roman or Arabic numeral, followed by ".", ":", an
// upper-case title or end of line; and Điều with a Roman or Arabic number
// (including "Điều 11a.") followed by a dot. An opening quote marks a quoted amendment
// ("“Điều 4. ..."). A Markdown heading prefix is allowed. Body text that
// cites a marker ("Điều 37 của Thông tư", "Chương II của Luật này") and Khoản
// lines ("1. ...") do not match. Submatches: (1) opening quote, (2) Phần /
// Chương / Mục, (3) Điều. Article titles can run to several hundred
// characters, hence the long tail.
var VietnameseChapterPattern = regexp.MustCompile(`(?m)^[ \t]*(?:#{1,6}[ \t]+)?([“"]?)(?:(Phần|PHẦN|Chương|CHƯƠNG|Mục|MỤC)[ \t]+(?:[0-9]+|[IVXLC]{1,7})(?:[.:]|[ \t]+[A-Z` + vietnameseUpper + `]|[ \t]*$)|(Điều|ĐIỀU)[ \t]+(?:[0-9]+[a-zđ]?|[IVXLC]{1,7})\.).{0,1000}$`)

// vietnameseLegalLevel returns the heading level of a Vietnamese legal marker
// line: Phần 1, Chương 2, Mục 3, Điều 4, and 5 for a quoted amendment so it
// nests under the Điều that amends it. Returns 0 when line is not a marker.
func vietnameseLegalLevel(line string) int {
	m := VietnameseChapterPattern.FindStringSubmatch(line)
	if m == nil {
		return 0
	}
	if m[1] != "" {
		return 5
	}
	switch m[2] {
	case "Phần", "PHẦN":
		return 1
	case "Chương", "CHƯƠNG":
		return 2
	case "Mục", "MỤC":
		return 3
	}
	return 4
}

// parseHeading returns the level and text of a heading line, or (0, "") when
// line is not a heading. ATX Markdown headings use their hash count, except
// that Vietnamese legal markers always take their legal level: OCR emits
// Chương and Điều at the same `##` depth, or as plain lines, so the hash count
// cannot express Chương > Mục > Điều. Plain accented Vietnamese all-caps
// titles default to level 1; HeadingHierarchy attaches a title following a
// bare legal marker to that marker instead. This needs no language hint.
func parseHeading(line string) (int, string) {
	level, text := 0, strings.TrimSpace(line)
	if m := MarkdownHeadingPattern.FindStringSubmatch(line); m != nil {
		level, text = len(m[1]), strings.TrimSpace(m[2])
	}
	if l := vietnameseLegalLevel(text); l > 0 {
		return l, text
	}
	if level == 0 && AllCapsHeadingPattern.MatchString(text) && strings.ContainsAny(text, vietnameseUpper) {
		level = 1
	}
	if level == 0 {
		return 0, ""
	}
	return level, text
}

// SentenceSeparators returns sentence-level separators tuned for the language.
// Used for fine-grained sub-splitting when a section is still too large.
func SentenceSeparators(lang string) []string {
	switch lang {
	case LangChinese:
		return []string{"。", "！", "？", "；", "\n"}
	case LangGerman, LangEnglish, LangVietnamese:
		return []string{". ", "! ", "? ", "; ", "\n"}
	default:
		return []string{"。", "！", "？", "；", ". ", "! ", "? ", "; ", "\n"}
	}
}

// NumberedSectionPatternForLangs returns the numbered-heading regex for the
// language hints. Vietnamese drops the single-level "1. Title" form because
// those lines are Khoản, not headings.
func NumberedSectionPatternForLangs(langs []string) *regexp.Regexp {
	if slices.Contains(langs, LangVietnamese) {
		return VietnameseNumberedSectionPattern
	}
	return NumberedSectionPattern
}

// ChapterPatternsForLangs returns the chapter-marker regexes that apply for
// the given language hints. An empty / unknown list returns all of them so
// that auto-detected documents still match.
func ChapterPatternsForLangs(langs []string) []*regexp.Regexp {
	all := []*regexp.Regexp{GermanChapterPattern, EnglishChapterPattern, ChineseChapterPattern, VietnameseChapterPattern}
	var out []*regexp.Regexp
	for _, l := range langs {
		switch l {
		case LangGerman:
			out = append(out, GermanChapterPattern)
		case LangEnglish:
			out = append(out, EnglishChapterPattern)
		case LangChinese:
			out = append(out, ChineseChapterPattern)
		case LangVietnamese:
			out = append(out, VietnameseChapterPattern)
		}
	}
	if len(out) == 0 {
		return all
	}
	return out
}
