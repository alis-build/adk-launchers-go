package models

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseToolTrajectoryMatchType parses Python-compatible match type strings.
func ParseToolTrajectoryMatchType(raw string) (ToolTrajectoryMatchType, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	normalized = strings.ReplaceAll(normalized, "-", " ")
	normalized = strings.ReplaceAll(normalized, "_", " ")
	switch normalized {
	case "exact", "":
		return ToolTrajectoryMatchExact, nil
	case "in order":
		return ToolTrajectoryMatchInOrder, nil
	case "any order":
		return ToolTrajectoryMatchAnyOrder, nil
	default:
		return 0, fmt.Errorf("unknown tool trajectory match type %q", raw)
	}
}

// UnmarshalJSON accepts string or numeric matchType values.
func (c *ToolTrajectoryCriterion) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if v, ok := raw["threshold"]; ok {
		_ = json.Unmarshal(v, &c.Threshold)
	}
	if v, ok := raw["includeIntermediateResponsesInFinal"]; ok {
		_ = json.Unmarshal(v, &c.IncludeIntermediateResponsesInFinal)
	}
	if v, ok := raw["matchType"]; ok {
		var asString string
		if err := json.Unmarshal(v, &asString); err == nil {
			mt, err := ParseToolTrajectoryMatchType(asString)
			if err != nil {
				return err
			}
			c.MatchType = mt
		} else {
			var asInt int
			if err := json.Unmarshal(v, &asInt); err != nil {
				return err
			}
			c.MatchType = ToolTrajectoryMatchType(asInt)
		}
	}
	return nil
}

// AsToolTrajectory returns tool trajectory criterion when present.
func (c jsonCriterion) AsToolTrajectory() (ToolTrajectoryCriterion, bool) {
	v, ok := c.value.(ToolTrajectoryCriterion)
	return v, ok
}

// AsLlmJudge returns LLM-as-judge criterion when present.
func (c jsonCriterion) AsLlmJudge() (LlmAsAJudgeCriterion, bool) {
	v, ok := c.value.(LlmAsAJudgeCriterion)
	return v, ok
}

// AsRubrics returns rubric-based criterion when present.
func (c jsonCriterion) AsRubrics() (RubricsBasedCriterion, bool) {
	v, ok := c.value.(RubricsBasedCriterion)
	return v, ok
}

// AsHallucinations returns hallucinations criterion when present.
func (c jsonCriterion) AsHallucinations() (HallucinationsCriterion, bool) {
	v, ok := c.value.(HallucinationsCriterion)
	return v, ok
}
