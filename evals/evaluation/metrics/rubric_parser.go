package metrics

import (
	"regexp"
	"strings"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// Matches adk-python DefaultAutoRaterResponseParser regexes at
// rubric_based_evaluator.py:59-89. Python's parser strips wrapping punctuation
// (e.g. "[[yes]]") before an exact yes/no match; we mirror that in
// stripVerdictPunct below rather than relaxing the regex.
//
// adk-python's Rationale and Verdict patterns use a look-behind, which RE2
// lacks; capture group 1 takes its place. As there, neither is line-anchored.
var (
	rubricIDRe        = regexp.MustCompile(`(?m)^\s*ID: (.*)$`)
	rubricPropertyRe  = regexp.MustCompile(`(?m)^\s*Property: (.*)$`)
	rubricRationaleRe = regexp.MustCompile(`Rationale: (.*)`)
	rubricVerdictRe   = regexp.MustCompile(`Verdict: (.*)`)
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
// text_property. It returns the mean over scored rubrics, or nil when nothing
// scored, plus the matched rubric scores.
//
// When the Property, Rationale and Verdict counts differ the reply is a
// partial parse, which could silently drop a failed rubric and inflate the
// score, so it scores nothing, as in adk-python.
func parseRubricVerdicts(response string, rubrics []models.Rubric) (*float64, []models.RubricScore) {
	props := rubricPropertyRe.FindAllStringSubmatchIndex(response, -1)
	ids := rubricIDRe.FindAllStringSubmatchIndex(response, -1)
	rats := rubricRationaleRe.FindAllStringSubmatch(response, -1)
	verds := rubricVerdictRe.FindAllStringSubmatch(response, -1)

	if len(props) == 0 || len(props) != len(rats) || len(props) != len(verds) {
		return nil, nil
	}

	byID := make(map[string]models.Rubric, len(rubrics))
	byText := make(map[string]models.Rubric, len(rubrics))
	for _, r := range rubrics {
		byID[r.RubricID] = r
		if r.RubricContent.TextProperty != nil {
			byText[normalizeText(*r.RubricContent.TextProperty)] = r
		}
	}

	var scores []models.RubricScore
	var total float64
	var counted int
	for i, prop := range props {
		// The echoed ID is the last ID line between the previous Property
		// and this one, so an omitted ID cannot shift a later ID onto an
		// earlier property. A rubric is matched by ID first, then by text.
		previousStart := -1
		if i > 0 {
			previousStart = props[i-1][0]
		}
		var echoedID string
		for _, id := range ids {
			if id[0] > previousStart && id[0] < prop[0] {
				echoedID = strings.TrimSpace(response[id[2]:id[3]])
			}
		}
		r, ok := byID[echoedID]
		if echoedID == "" || !ok {
			r, ok = byText[normalizeText(response[prop[2]:prop[3]])]
		}
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
		if rat := strings.TrimSpace(rats[i][1]); rat != "" {
			rs.Rationale = &rat
		}
		scores = append(scores, rs)
		if s != nil {
			total += *s
			counted++
		}
	}
	if counted == 0 {
		return nil, scores
	}
	mean := total / float64(counted)
	return &mean, scores
}
