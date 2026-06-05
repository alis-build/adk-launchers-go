package clienttool

import (
	"encoding/json"
	"testing"
)

func TestParseToolDefs_FromJSONString(t *testing.T) {
	input := `[{"name":"search","description":"Search the web","parameters":{"type":"object","properties":{"query":{"type":"string"}}}}]`

	defs, err := parseToolDefs(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(defs))
	}
	if defs[0].Name != "search" {
		t.Errorf("expected name=search, got %s", defs[0].Name)
	}
	if defs[0].Description != "Search the web" {
		t.Errorf("expected description='Search the web', got %s", defs[0].Description)
	}
}

func TestParseToolDefs_FromSlice(t *testing.T) {
	input := []any{
		map[string]any{
			"name":        "calculate",
			"description": "Do math",
			"parameters":  map[string]any{"type": "object"},
		},
	}

	defs, err := parseToolDefs(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(defs))
	}
	if defs[0].Name != "calculate" {
		t.Errorf("expected name=calculate, got %s", defs[0].Name)
	}
}

func TestParseToolDefs_EmptySlice(t *testing.T) {
	defs, err := parseToolDefs("[]")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 0 {
		t.Errorf("expected 0 tools, got %d", len(defs))
	}
}

func TestParseToolDefs_InvalidJSON(t *testing.T) {
	_, err := parseToolDefs("not-json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseToolDefs_FromBytes(t *testing.T) {
	input := []byte(`[{"name":"test","description":"A test tool"}]`)
	defs, err := parseToolDefs(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 1 || defs[0].Name != "test" {
		t.Errorf("unexpected result: %+v", defs)
	}
}

func TestNewToolset_Name(t *testing.T) {
	ts := NewToolset()
	if ts.Name() != "ag-ui-client-tools" {
		t.Errorf("expected name='ag-ui-client-tools', got %s", ts.Name())
	}
}

func TestProxyTool_LongRunning(t *testing.T) {
	pt := newProxyTool("test", "A test tool", nil)
	if !pt.IsLongRunning() {
		t.Error("expected IsLongRunning() to return true")
	}
}

func TestProxyTool_Declaration(t *testing.T) {
	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
		},
	}
	pt := newProxyTool("search", "Search the web", params)
	decl := pt.Declaration()

	if decl.Name != "search" {
		t.Errorf("expected name=search, got %s", decl.Name)
	}
	if decl.ParametersJsonSchema == nil {
		t.Error("expected ParametersJsonSchema to be set")
	}
}

func TestProxyTool_Run(t *testing.T) {
	pt := newProxyTool("test", "desc", nil)
	result, err := pt.Run(nil, map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["status"] != "pending" {
		t.Errorf("expected status=pending, got %v", result["status"])
	}
}

func TestParseToolDefs_SkipsEmptyNames(t *testing.T) {
	input := []any{
		map[string]any{"name": "", "description": "no name"},
		map[string]any{"name": "valid", "description": "has name"},
	}

	defs, err := parseToolDefs(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("expected 2 parsed defs (filter happens in Tools()), got %d", len(defs))
	}
}

func TestToolset_SkipsEmptyAndDuplicateNames(t *testing.T) {
	// Simulate what the Toolset.Tools method does with the parsed defs.
	// Since we can't easily create an agent.ReadonlyContext, test the
	// filtering logic via the internal parse + filter path.
	input := []any{
		map[string]any{"name": "", "description": "no name"},
		map[string]any{"name": "search", "description": "first"},
		map[string]any{"name": "search", "description": "duplicate"},
		map[string]any{"name": "calc", "description": "second"},
	}

	defs, err := parseToolDefs(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Replicate the filtering logic from Toolset.Tools
	seen := make(map[string]bool)
	var filtered []toolDef
	for _, def := range defs {
		if def.Name == "" || seen[def.Name] {
			continue
		}
		seen[def.Name] = true
		filtered = append(filtered, def)
	}

	if len(filtered) != 2 {
		t.Fatalf("expected 2 tools after filtering, got %d", len(filtered))
	}
	if filtered[0].Name != "search" {
		t.Errorf("expected first tool=search, got %s", filtered[0].Name)
	}
	if filtered[1].Name != "calc" {
		t.Errorf("expected second tool=calc, got %s", filtered[1].Name)
	}
}

func TestProxyTool_SchemaCleanedInDeclaration(t *testing.T) {
	params := map[string]any{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type":    "object",
		"properties": map[string]any{
			"name": map[string]any{
				"$comment": "should be stripped",
				"type":     "string",
			},
		},
	}
	pt := newProxyTool("test", "desc", params)
	decl := pt.Declaration()

	raw, _ := json.Marshal(decl.ParametersJsonSchema)
	var schema map[string]any
	json.Unmarshal(raw, &schema)

	if _, ok := schema["$schema"]; ok {
		t.Error("expected $schema to be stripped from declaration parameters")
	}
}
