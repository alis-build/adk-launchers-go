package clienttool

import (
	"testing"
)

func TestCleanSchemaForGenAI_NilPassthrough(t *testing.T) {
	if got := cleanSchemaForGenAI(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestCleanSchemaForGenAI_StripsDollarKeys(t *testing.T) {
	input := map[string]any{
		"$schema":    "http://json-schema.org/draft-07/schema#",
		"$id":        "test",
		"$ref":       "#/defs/Foo",
		"$defs":      map[string]any{"Foo": map[string]any{"type": "object"}},
		"$comment":   "internal note",
		"type":       "object",
		"properties": map[string]any{"name": map[string]any{"type": "string"}},
	}

	got := cleanSchemaForGenAI(input).(map[string]any)

	for _, key := range []string{"$schema", "$id", "$ref", "$defs", "$comment"} {
		if _, ok := got[key]; ok {
			t.Errorf("expected %q to be stripped, but it was present", key)
		}
	}
	if got["type"] != "object" {
		t.Errorf("expected type=object, got %v", got["type"])
	}
	props, ok := got["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties to be a map")
	}
	if _, ok := props["name"]; !ok {
		t.Error("expected properties.name to be preserved")
	}
}

func TestCleanSchemaForGenAI_ExamplesToExample(t *testing.T) {
	input := map[string]any{
		"type":     "string",
		"examples": []any{"hello", "world"},
	}

	got := cleanSchemaForGenAI(input).(map[string]any)

	if _, ok := got["examples"]; ok {
		t.Error("expected examples to be removed")
	}
	if got["example"] != "hello" {
		t.Errorf("expected example=hello, got %v", got["example"])
	}
}

func TestCleanSchemaForGenAI_ConstToEnum(t *testing.T) {
	input := map[string]any{
		"type":  "string",
		"const": "fixed_value",
	}

	got := cleanSchemaForGenAI(input).(map[string]any)

	if _, ok := got["const"]; ok {
		t.Error("expected const to be removed")
	}
	enum, ok := got["enum"].([]any)
	if !ok || len(enum) != 1 || enum[0] != "fixed_value" {
		t.Errorf("expected enum=[fixed_value], got %v", got["enum"])
	}
}

func TestCleanSchemaForGenAI_RecursesIntoProperties(t *testing.T) {
	input := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"nested": map[string]any{
				"$comment": "should be stripped",
				"type":     "string",
			},
		},
	}

	got := cleanSchemaForGenAI(input).(map[string]any)
	props := got["properties"].(map[string]any)
	nested := props["nested"].(map[string]any)

	if _, ok := nested["$comment"]; ok {
		t.Error("expected $comment to be stripped from nested schema")
	}
	if nested["type"] != "string" {
		t.Errorf("expected nested type=string, got %v", nested["type"])
	}
}

func TestCleanSchemaForGenAI_PreservesArrayItems(t *testing.T) {
	input := map[string]any{
		"type": "array",
		"items": map[string]any{
			"$id":  "strip-me",
			"type": "integer",
		},
	}

	got := cleanSchemaForGenAI(input).(map[string]any)
	items := got["items"].(map[string]any)

	if _, ok := items["$id"]; ok {
		t.Error("expected $id to be stripped from items")
	}
	if items["type"] != "integer" {
		t.Errorf("expected items type=integer, got %v", items["type"])
	}
}

func TestCleanSchemaForGenAI_ScalarPassthrough(t *testing.T) {
	if got := cleanSchemaForGenAI("hello"); got != "hello" {
		t.Errorf("expected string passthrough, got %v", got)
	}
	if got := cleanSchemaForGenAI(42); got != 42 {
		t.Errorf("expected int passthrough, got %v", got)
	}
	if got := cleanSchemaForGenAI(true); got != true {
		t.Errorf("expected bool passthrough, got %v", got)
	}
}
