package chunker

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestEnsureDefaults_TokenLimitClampsChunkSize(t *testing.T) {
	cfg := SplitterConfig{
		ChunkSize:  10000, // huge
		TokenLimit: 100,
		Languages:  []string{LangEnglish},
	}
	out := ensureDefaults(cfg)
	// 100 tokens * 4 chars/token * 0.9 ≈ 360 chars
	if out.ChunkSize >= 1000 {
		t.Errorf("expected ChunkSize clamped by TokenLimit, got %d", out.ChunkSize)
	}
	if out.ChunkOverlap >= out.ChunkSize {
		t.Errorf("overlap should be smaller than clamped chunk size: overlap=%d size=%d", out.ChunkOverlap, out.ChunkSize)
	}
}

func TestEnsureDefaults_TokenLimitChineseTighter(t *testing.T) {
	cfgEN := ensureDefaults(SplitterConfig{TokenLimit: 200, Languages: []string{LangEnglish}})
	cfgZH := ensureDefaults(SplitterConfig{TokenLimit: 200, Languages: []string{LangChinese}})
	if cfgZH.ChunkSize >= cfgEN.ChunkSize {
		t.Errorf("Chinese char budget should be tighter than English: zh=%d en=%d", cfgZH.ChunkSize, cfgEN.ChunkSize)
	}
}

func TestEnsureDefaults_NoTokenLimitKeepsChunkSize(t *testing.T) {
	cfg := SplitterConfig{ChunkSize: 800}
	out := ensureDefaults(cfg)
	if out.ChunkSize != 800 {
		t.Errorf("ChunkSize should stay 800, got %d", out.ChunkSize)
	}
}

// measuredViCharsPerToken is the chars/token ratio of Qwen3-VL-Embedding-2B on
// Vietnamese legal text (per-chunk p10, results/chars-per-token.json in the
// deployment repo).
const measuredViCharsPerToken = 2.5

func TestSplit_VietnameseChunksFitTokenLimit(t *testing.T) {
	// 133-rune clauses: four fit the old mixed-ratio budget (540) but not 500.
	text := strings.Repeat("Tổ chức tín dụng, doanh nghiệp được phép kinh doanh mua, bán vàng miếng theo quy định của Ngân hàng Nhà nước và pháp luật liên quan; ", 40)
	const tokenLimit = 200
	cfg := SplitterConfig{
		ChunkSize:  2000, // the token limit, not the char size, must bound chunks
		TokenLimit: tokenLimit,
		Languages:  []string{LangVietnamese},
		Separators: []string{"\n\n", "\n", ". ", "; "},
	}
	chunks := Split(text, cfg)
	if len(chunks) < 2 {
		t.Fatalf("expected the text to be split, got %d chunk(s)", len(chunks))
	}
	maxChars := int(tokenLimit * measuredViCharsPerToken)
	for _, c := range chunks {
		if n := utf8.RuneCountInString(c.Content); n > maxChars {
			t.Errorf("chunk %d has %d runes, more than %d tokens at %.1f chars/token (%d runes)",
				c.Seq, n, tokenLimit, measuredViCharsPerToken, maxChars)
		}
	}
}

func TestEnsureDefaults_VietnameseSeparators(t *testing.T) {
	got := ensureDefaults(SplitterConfig{Languages: []string{LangVietnamese}}).Separators
	want := []string{"\n\n", "\n", ". ", "; "}
	if !slices.Equal(got, want) {
		t.Errorf("default separators for vi: got %q, want %q", got, want)
	}
	if got := NormalizeSplitterConfig(SplitterConfig{Languages: []string{LangVietnamese}}).Separators; !slices.Equal(got, want) {
		t.Errorf("normalized separators for vi: got %q, want %q", got, want)
	}
	// Other languages keep the original default.
	if got := ensureDefaults(SplitterConfig{}).Separators; !slices.Equal(got, []string{"\n\n", "\n", "。"}) {
		t.Errorf("default separators without language changed: %q", got)
	}
}
