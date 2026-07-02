package models

// RubricContent is the testable criterion text for a rubric.
type RubricContent struct {
	TextProperty *string `json:"textProperty,omitempty"`
}

// Rubric is a single evaluation rubric.
type Rubric struct {
	RubricID       string        `json:"rubricId"`
	RubricContent  RubricContent `json:"rubricContent"`
	Description    *string       `json:"description,omitempty"`
	Type           *string       `json:"type,omitempty"`
}

// RubricScore holds the score for a rubric assessment.
type RubricScore struct {
	RubricID string   `json:"rubricId"`
	Score    *float64 `json:"score,omitempty"`
}
