package metrics

import (
	"regexp"
	"strings"

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

// tokens lowercases and tokenizes text for ROUGE overlap counting.
func tokens(text string) []string {
	return tokenRe.FindAllString(strings.ToLower(text), -1)
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
func textFromContent(c *genai.Content) string {
	if c == nil {
		return ""
	}
	var parts []string
	for _, p := range c.Parts {
		if p != nil && p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}
