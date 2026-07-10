package metrics

import (
	"fmt"
	"regexp"
	"strings"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// Matches adk-python DefaultAutoRaterResponseParser regexes at
// rubric_based_evaluator.py:59-89. Python's parser strips wrapping punctuation
// (e.g. "[[yes]]") before an exact yes/no match; we mirror that in
// stripVerdictPunct below rather than relaxing the regex.
var (
	rubricPropertyRe  = regexp.MustCompile(`(?m)^\s*Property:\s*(.+)$`)
	rubricRationaleRe = regexp.MustCompile(`(?m)^\s*Rationale:\s*(.+)$`)
	rubricVerdictRe   = regexp.MustCompile(`(?m)^\s*Verdict:\s*(.+)$`)
)

// normalizeText lowercases and trims whitespace to match Python's
// rubric_based_evaluator._normalize_text used for rubric-text lookup.
func normalizeText(text string) string {
	return strings.ToLower(strings.TrimSpace(text))
}

// stripVerdictPunct trims the wrapping characters (brackets, bullets,
// asterisks, quotes, terminal punctuation, whitespace) that LLMs commonly emit
// around yes/no verdicts (e.g. "[[yes]]", "**no**", "yes.") before an exact
// match. Keeping the compare exact avoids substring false positives like
// "not applicable" matching "no" or "yesterday" matching "yes".
func stripVerdictPunct(v string) string {
	return strings.Trim(v, "[](){}*_`'\"“”‘’.,;:!? \t\r\n")
}

// parseRubricVerdicts extracts Property/Rationale/Verdict blocks from the
// judge response and maps each to the rubric with matching normalized
// text_property. Returns (mean, scores, error) where mean is the arithmetic
// mean of scored rubrics and error is set only when no verdicts could be
// parsed at all.
func parseRubricVerdicts(response string, rubrics []models.Rubric) (float64, []models.RubricScore, error) {
	props := rubricPropertyRe.FindAllStringSubmatch(response, -1)
	rats := rubricRationaleRe.FindAllStringSubmatch(response, -1)
	verds := rubricVerdictRe.FindAllStringSubmatch(response, -1)

	if len(props) == 0 || len(verds) == 0 {
		return 0, nil, fmt.Errorf("judge response contained no Property/Verdict blocks")
	}

	byText := make(map[string]models.Rubric, len(rubrics))
	for _, r := range rubrics {
		if r.RubricContent.TextProperty != nil {
			byText[normalizeText(*r.RubricContent.TextProperty)] = r
		}
	}

	var scores []models.RubricScore
	var total float64
	var counted int
	n := len(props)
	if len(verds) < n {
		n = len(verds)
	}
	for i := 0; i < n; i++ {
		text := normalizeText(props[i][1])
		r, ok := byText[text]
		if !ok {
			// Python logs a warning and skips unknown properties.
			continue
		}
		var s *float64
		verdict := stripVerdictPunct(strings.ToLower(verds[i][1]))
		switch verdict {
		case "yes":
			v := 1.0
			s = &v
		case "no":
			v := 0.0
			s = &v
		}
		rs := models.RubricScore{RubricID: r.RubricID, Score: s}
		if i < len(rats) {
			rat := strings.TrimSpace(rats[i][1])
			if rat != "" {
				rs.Rationale = &rat
			}
		}
		scores = append(scores, rs)
		if s != nil {
			total += *s
			counted++
		}
	}
	if counted == 0 {
		return 0, scores, fmt.Errorf("no rubric verdicts parsed (yes/no) from judge response")
	}
	return total / float64(counted), scores, nil
}
