package metrics

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
	"google.golang.org/genai"
)

var tokenRe = regexp.MustCompile(`[a-z0-9]+`)

// Rouge1Scores holds ROUGE-1 precision, recall, and F-measure.
type Rouge1Scores struct {
	Precision float64
	Recall    float64
	FMeasure  float64
}

// CalculateRouge1Scores computes ROUGE-1 F-measure between candidate and reference text.
func CalculateRouge1Scores(candidate, reference string) Rouge1Scores {
	cTokens := tokens(candidate)
	rTokens := tokens(reference)
	if len(cTokens) == 0 || len(rTokens) == 0 {
		return Rouge1Scores{}
	}

	overlap := 0
	rCounts := tokenCounts(rTokens)
	for _, tok := range cTokens {
		if rCounts[tok] > 0 {
			overlap++
			rCounts[tok]--
		}
	}
	precision := float64(overlap) / float64(len(cTokens))
	recall := float64(overlap) / float64(len(rTokens))
	fmeasure := 0.0
	if precision+recall > 0 {
		fmeasure = 2 * precision * recall / (precision + recall)
	}
	return Rouge1Scores{Precision: precision, Recall: recall, FMeasure: fmeasure}
}

// tokens tokenizes text for ROUGE overlap counting the way adk-python's
// _UnicodeAwareTokenizer does (without stemming): NFKC-normalise and
// lowercase; each CJK character is its own token; in scripts written without
// spaces (Thai, Lao, Khmer, Myanmar) each base character starts a token and
// combining marks stay attached to it; other letters, numbers and marks form
// words; anything else separates. ASCII words then go through the default
// [a-z0-9]+ tokenizer, so English scoring is unchanged.
func tokens(text string) []string {
	text = strings.ToLower(norm.NFKC.String(text))
	var b strings.Builder
	for _, r := range text {
		switch {
		case isCJK(r):
			b.WriteRune(' ')
			b.WriteRune(r)
			b.WriteRune(' ')
		case isNonSpacedScript(r):
			if !unicode.IsMark(r) {
				b.WriteRune(' ')
			}
			b.WriteRune(r)
		case unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	var out []string
	for _, word := range strings.Fields(b.String()) {
		if isASCII(word) {
			out = append(out, tokenRe.FindAllString(word, -1)...)
		} else {
			out = append(out, word)
		}
	}
	return out
}

// isCJK reports Chinese, Japanese and Korean characters, which are tokenized
// one per character.
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0x3040 && r <= 0x309F) || // Hiragana
		(r >= 0x30A0 && r <= 0x30FF) || // Katakana
		(r >= 0xAC00 && r <= 0xD7AF) // Hangul Syllables
}

// isNonSpacedScript reports scripts written without spaces between words.
func isNonSpacedScript(r rune) bool {
	return (r >= 0x0E00 && r <= 0x0E7F) || // Thai
		(r >= 0x0E80 && r <= 0x0EFF) || // Lao
		(r >= 0x1780 && r <= 0x17FF) || // Khmer
		(r >= 0x1000 && r <= 0x109F) // Myanmar
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// tokenCounts builds a multiset of lowercase alphanumeric tokens for ROUGE overlap.
func tokenCounts(tok []string) map[string]int {
	out := make(map[string]int, len(tok))
	for _, t := range tok {
		out[t]++
	}
	return out
}

// textFromContent concatenates text parts from genai Content for metric input.
// Thought parts are model reasoning, not the response, and are left out, as
// in adk-python.
func textFromContent(c *genai.Content) string {
	if c == nil {
		return ""
	}
	var parts []string
	for _, p := range c.Parts {
		if p != nil && p.Text != "" && !p.Thought {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}
