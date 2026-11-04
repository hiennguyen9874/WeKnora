package chunker

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestApproxTokenCount_English(t *testing.T) {
	got := ApproxTokenCount("The quick brown fox jumps over the lazy dog.", LangEnglish)
	// 44 chars / 4 ≈ 11 tokens
	if got < 9 || got > 13 {
		t.Errorf("English token estimate out of range: got %d, want 9..13", got)
	}
}

func TestApproxTokenCount_Chinese(t *testing.T) {
	got := ApproxTokenCount("这是一段中文测试内容用于检验分词估算", LangChinese)
	// 18 runes / 1.7 ≈ 10
	if got < 9 || got > 12 {
		t.Errorf("Chinese token estimate out of range: got %d, want 9..12", got)
	}
}

func TestApproxTokenCount_Empty(t *testing.T) {
	if got := ApproxTokenCount("", LangEnglish); got != 0 {
		t.Errorf("empty string should return 0 tokens, got %d", got)
	}
}

func TestApproxTokenCount_UnknownLang(t *testing.T) {
	got := ApproxTokenCount("Hello world hello world", "xx")
	if got <= 0 {
		t.Errorf("unknown lang should fall back to mixed, got %d", got)
	}
}

func TestDetectLanguage_English(t *testing.T) {
	if got := DetectLanguage("The quick brown fox jumps over the lazy dog."); got != LangEnglish {
		t.Errorf("expected English, got %s", got)
	}
}

func TestDetectLanguage_German(t *testing.T) {
	if got := DetectLanguage("Der schnelle braune Fuchs springt über den faulen Hund."); got != LangGerman {
		t.Errorf("expected German, got %s", got)
	}
}

func TestDetectLanguage_GermanByStopwords(t *testing.T) {
	// No umlauts but plenty of German function words.
	if got := DetectLanguage("Das ist ein Test und nicht mit Umlauten."); got != LangGerman {
		t.Errorf("expected German via stopwords, got %s", got)
	}
}

func TestDetectLanguage_Chinese(t *testing.T) {
	if got := DetectLanguage("这是一段中文测试内容"); got != LangChinese {
		t.Errorf("expected Chinese, got %s", got)
	}
}

func TestDetectLanguage_Mixed(t *testing.T) {
	got := DetectLanguage("This 这是 mixed 测试 content with 多语言 inside")
	if got != LangMixed {
		t.Errorf("expected Mixed, got %s", got)
	}
}

func TestDetectLanguage_Vietnamese(t *testing.T) {
	cases := []string{
		"Điều 1. Phạm vi điều chỉnh\n\nNghị định này quy định về hoạt động kinh doanh vàng.",
		"CHƯƠNG II QUẢN LÝ HOẠT ĐỘNG KINH DOANH VÀNG",
		"Bảo hiểm xã hội là sự bảo đảm thay thế hoặc bù đắp một phần thu nhập của người lao động.",
	}
	for _, s := range cases {
		if got := DetectLanguage(s); got != LangVietnamese {
			t.Errorf("DetectLanguage(%q) = %s, want %s", s, got, LangVietnamese)
		}
	}
}

// A Vietnamese name or a French accent inside English text must not flip it to Vietnamese.
func TestDetectLanguage_EnglishWithVietnameseName(t *testing.T) {
	cases := []string{
		"Nguyễn Văn An is the chief executive of the company and lives in Hanoi with his family.",
		"The café served a crème brûlée to every guest at the résumé workshop.",
	}
	for _, s := range cases {
		if got := DetectLanguage(s); got != LangEnglish {
			t.Errorf("DetectLanguage(%q) = %s, want %s", s, got, LangEnglish)
		}
	}
}

func TestApproxTokenCount_Vietnamese(t *testing.T) {
	// 100 runes at the measured 2.5 chars/token (Qwen3-VL-Embedding-2B) = 40 tokens.
	s := strings.Repeat("Điều 1. ", 12) + "Phạm"
	if n := utf8.RuneCountInString(s); n != 100 {
		t.Fatalf("fixture has %d runes, want 100", n)
	}
	if got := ApproxTokenCount(s, LangVietnamese); got != 40 {
		t.Errorf("Vietnamese token estimate: got %d, want 40", got)
	}
}

func TestCharsForTokenLimit_AppliesSafetyMargin(t *testing.T) {
	got := CharsForTokenLimit(1000, LangEnglish)
	// 1000 * 4 * 0.9 = 3600
	if got < 3500 || got > 3700 {
		t.Errorf("char budget for 1000 EN tokens out of range: got %d", got)
	}
}

func TestCharsForTokenLimit_ZeroTokens(t *testing.T) {
	if got := CharsForTokenLimit(0, LangEnglish); got != 0 {
		t.Errorf("zero tokens should give zero chars, got %d", got)
	}
}
