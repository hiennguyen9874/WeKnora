// Package chunker - tokens.go provides language-aware token count approximation.
//
// We avoid pulling in a tokenizer dependency (e.g. tiktoken) and instead use
// per-language chars-per-token ratios derived from common embedding model
// vocabularies. The numbers are conservative — they tend to slightly
// over-estimate token counts so that chunks stay safely under model limits.
package chunker

import (
	"unicode"
	"unicode/utf8"
)

// Language identifiers used by the token estimator and the heuristic splitter.
const (
	LangEnglish    = "en"
	LangGerman     = "de"
	LangChinese    = "zh"
	LangVietnamese = "vi"
	LangMixed      = "mixed"
)

// charsPerToken holds approximate chars/token ratios per language.
// Numbers err on the conservative side so estimates over-shoot a little.
var charsPerToken = map[string]float64{
	LangEnglish: 4.0,
	LangGerman:  4.5,
	LangChinese: 1.7,
	// Qwen3-VL-Embedding-2B on Vietnamese legal text averages 3.18 chars/token;
	// 2.5 is the per-chunk p10, so tables and numbers are not under-counted.
	LangVietnamese: 2.5,
	LangMixed:      3.0,
}

// ApproxTokenCount returns a conservative token estimate for s in the given
// language. An empty or unknown lang falls back to "mixed".
func ApproxTokenCount(s string, lang string) int {
	if s == "" {
		return 0
	}
	return ApproxTokenCountFromRuneLen(utf8.RuneCountInString(s), lang)
}

// ApproxTokenCountFromRuneLen is the allocation-free variant of
// ApproxTokenCount when the caller has already computed the rune length.
// Use this in hot loops where the same content's rune count would
// otherwise be recomputed multiple times (e.g. preview endpoint emitting
// per-chunk stats).
func ApproxTokenCountFromRuneLen(runeLen int, lang string) int {
	if runeLen <= 0 {
		return 0
	}
	ratio, ok := charsPerToken[lang]
	if !ok {
		ratio = charsPerToken[LangMixed]
	}
	approx := float64(runeLen) / ratio
	if approx < 1 {
		return 1
	}
	return int(approx + 0.5)
}

// DetectLanguage returns a coarse language label by counting CJK runes vs.
// Latin runes. The result is one of LangChinese, LangVietnamese, LangGerman,
// LangEnglish or LangMixed. Detection is cheap and meant only for heuristic
// dispatch — it is NOT a replacement for proper language identification.
func DetectLanguage(s string) string {
	if s == "" {
		return LangMixed
	}
	var cjk, latin, umlaut, vietnamese int
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r):
			cjk++
		case isVietnameseLetter(r):
			vietnamese++
			latin++
		case isGermanUmlaut(r):
			umlaut++
			latin++
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			latin++
		}
	}
	total := cjk + latin
	if total == 0 {
		return LangMixed
	}
	cjkRatio := float64(cjk) / float64(total)
	latinRatio := float64(latin) / float64(total)
	// Mixed: meaningful presence of both scripts (>=15% each).
	if cjkRatio >= 0.15 && latinRatio >= 0.15 {
		return LangMixed
	}
	if cjkRatio > 0.3 {
		return LangChinese
	}
	// Vietnamese prose has ~20% such letters; a Vietnamese name in English text far fewer.
	if vietnamese*10 >= latin {
		return LangVietnamese
	}
	if umlaut > 0 || hasGermanWords(s) {
		return LangGerman
	}
	return LangEnglish
}

// isVietnameseLetter reports precomposed letters that only Vietnamese uses:
// ă, đ, ĩ, ũ, ơ, ư and the vowels with a hook, dot below or stacked tone
// (U+1EA0..U+1EF9). Accents shared with French or German (é, à, ô, ü) are
// excluded so those languages are not mistaken for Vietnamese.
func isVietnameseLetter(r rune) bool {
	switch r {
	case 'ă', 'Ă', 'đ', 'Đ', 'ĩ', 'Ĩ', 'ũ', 'Ũ', 'ơ', 'Ơ', 'ư', 'Ư':
		return true
	}
	return r >= 0x1EA0 && r <= 0x1EF9
}

func isGermanUmlaut(r rune) bool {
	switch r {
	case 'ä', 'ö', 'ü', 'Ä', 'Ö', 'Ü', 'ß':
		return true
	}
	return false
}

// hasGermanWords does a tiny stop-word check to bias towards "de" when the
// text uses common German function words. Cheap heuristic — false positives
// on borrowed terms are acceptable.
func hasGermanWords(s string) bool {
	const sample = 512
	if len(s) > sample {
		s = s[:sample]
	}
	for _, w := range []string{" der ", " die ", " das ", " und ", " ist ", " nicht ", " mit ", " auf "} {
		if containsLower(s, w) {
			return true
		}
	}
	return false
}

func containsLower(haystack, needle string) bool {
	if len(haystack) < len(needle) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			h := haystack[i+j]
			if h >= 'A' && h <= 'Z' {
				h += 'a' - 'A'
			}
			if h != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// CharsForTokenLimit converts a token limit into an approximate character
// budget for a given language. Used to size chunks so they fit within an
// embedding model's max-token window with a small safety margin.
func CharsForTokenLimit(tokens int, lang string) int {
	if tokens <= 0 {
		return 0
	}
	ratio, ok := charsPerToken[lang]
	if !ok {
		ratio = charsPerToken[LangMixed]
	}
	// 0.9 safety factor so we under-shoot the model limit instead of overshooting.
	return int(float64(tokens) * ratio * 0.9)
}
