package clienttool

import (
	"encoding/json"
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
)

// StateKey is the session state key under which the launcher serialises
// AG-UI client tool definitions. The toolset reads this key at invocation
// time to create proxy tools.
const StateKey = "_agui_client_tools"

// toolDef mirrors the AG-UI Tool schema for JSON deserialization from state.
type toolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

// Toolset implements [tool.Toolset]. It reads AG-UI tool definitions from
// session state (injected by the launcher via StateDelta) and returns proxy
// [tool.Tool] instances. When no definitions are present, it returns nil.
type Toolset struct{}

// NewToolset returns a [Toolset] ready for use in an agent's Toolsets config.
func NewToolset() *Toolset {
	return &Toolset{}
}

// Name implements [tool.Toolset].
func (ts *Toolset) Name() string {
	return "ag-ui-client-tools"
}

// Tools implements [tool.Toolset]. It reads tool definitions from the session
// state key [StateKey] and creates proxy tools from them. Returns nil when the
// key is absent (no client tools for this request).
func (ts *Toolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	raw, err := ctx.ReadonlyState().Get(StateKey)
	if err != nil {
		if err == session.ErrStateKeyNotExist {
			return nil, nil
		}
		return nil, fmt.Errorf("clienttool: read state %q: %w", StateKey, err)
	}

	defs, err := parseToolDefs(raw)
	if err != nil {
		return nil, fmt.Errorf("clienttool: parse tool definitions: %w", err)
	}
	if len(defs) == 0 {
		return nil, nil
	}

	tools := make([]tool.Tool, 0, len(defs))
	seen := make(map[string]bool, len(defs))
	for _, def := range defs {
		if def.Name == "" {
			continue
		}
		if seen[def.Name] {
			continue
		}
		seen[def.Name] = true
		tools = append(tools, newProxyTool(def.Name, def.Description, def.Parameters))
	}
	if len(tools) == 0 {
		return nil, nil
	}
	return tools, nil
}

// parseToolDefs converts the raw state value (which may be a JSON string,
// []any, or already-decoded slice) into a slice of toolDef.
func parseToolDefs(raw any) ([]toolDef, error) {
	var jsonBytes []byte

	switch v := raw.(type) {
	case string:
		jsonBytes = []byte(v)
	case []byte:
		jsonBytes = v
	default:
		var err error
		jsonBytes, err = json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("marshal state value: %w", err)
		}
	}

	var defs []toolDef
	if err := json.Unmarshal(jsonBytes, &defs); err != nil {
		return nil, fmt.Errorf("unmarshal tool definitions: %w", err)
	}
	return defs, nil
}
