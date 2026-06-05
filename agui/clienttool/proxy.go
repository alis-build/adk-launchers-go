package clienttool

import (
	"fmt"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// proxyTool is a long-running tool that proxies an AG-UI client-defined tool.
// When the LLM calls it, Run returns immediately with a "pending" status.
// The launcher emits the function call as TOOL_CALL events on the SSE stream;
// the client executes the tool and sends the result in a subsequent request.
type proxyTool struct {
	name        string
	description string
	params      any // AG-UI JSON Schema parameters, passed through to genai
}

func newProxyTool(name, description string, params any) *proxyTool {
	return &proxyTool{
		name:        name,
		description: description,
		params:      cleanSchemaForGenAI(params),
	}
}

// Name implements [tool.Tool].
func (t *proxyTool) Name() string { return t.name }

// Description implements [tool.Tool].
func (t *proxyTool) Description() string { return t.description }

// IsLongRunning implements [tool.Tool]. Always true — results come from the
// client asynchronously.
func (t *proxyTool) IsLongRunning() bool { return true }

// Declaration returns the genai.FunctionDeclaration for this proxy tool.
// The AG-UI JSON Schema parameters are passed through via ParametersJsonSchema.
func (t *proxyTool) Declaration() *genai.FunctionDeclaration {
	desc := t.description
	if desc != "" {
		desc += "\n\n"
	}
	desc += "NOTE: This is a long-running operation. Do not call this tool again if it has already returned some intermediate or pending status."

	return &genai.FunctionDeclaration{
		Name:                 t.name,
		Description:          desc,
		ParametersJsonSchema: t.params,
	}
}

// Run implements the runnable tool interface. It returns immediately with a
// pending status — the actual execution happens on the AG-UI client.
func (t *proxyTool) Run(_ agent.ToolContext, _ any) (map[string]any, error) {
	return map[string]any{
		"status":  "pending",
		"message": "Tool execution delegated to the AG-UI client.",
	}, nil
}

// ProcessRequest packs the function declaration into the LLM request.
// This method satisfies the internal RequestProcessor interface via Go's
// structural typing — ADK discovers it at runtime without needing an
// explicit import of the internal package.
func (t *proxyTool) ProcessRequest(_ agent.ToolContext, req *model.LLMRequest) error {
	if req.Tools == nil {
		req.Tools = make(map[string]any)
	}
	if _, ok := req.Tools[t.name]; ok {
		return fmt.Errorf("duplicate tool: %q", t.name)
	}
	req.Tools[t.name] = t

	decl := t.Declaration()
	if decl == nil {
		return nil
	}

	if req.Config == nil {
		req.Config = &genai.GenerateContentConfig{}
	}
	var funcTool *genai.Tool
	for _, gt := range req.Config.Tools {
		if gt != nil && gt.FunctionDeclarations != nil {
			funcTool = gt
			break
		}
	}
	if funcTool == nil {
		req.Config.Tools = append(req.Config.Tools, &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{decl},
		})
	} else {
		funcTool.FunctionDeclarations = append(funcTool.FunctionDeclarations, decl)
	}
	return nil
}
