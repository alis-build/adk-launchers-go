// Package clienttool provides a dynamic [tool.Toolset] that exposes AG-UI
// client-defined tools to the LLM as long-running proxy functions.
//
// AG-UI clients (CopilotKit, custom frontends) send tool definitions in
// [types.RunAgentInput.Tools]. The launcher serialises these into session state
// under [StateKey]; this package reads them at invocation time and creates
// proxy [tool.Tool] instances so the model can call them.
//
// Each proxy tool is long-running: its Run method returns immediately with a
// "pending" status. ADK emits the function call as a session event, the AG-UI
// launcher maps it to TOOL_CALL_START/ARGS/END SSE events, and the client
// executes the tool locally. The client then sends the result back in a
// subsequent RunAgentInput (role "tool"), which the launcher converts to a
// genai.FunctionResponse and feeds into the next ADK run.
//
// # Agent opt-in
//
// Include the toolset in your agent definition:
//
//	agent, _ := llmagent.New(llmagent.Config{
//	    Name: "my_agent",
//	    Toolsets: []tool.Toolset{
//	        clienttool.NewToolset(),
//	    },
//	    Tools: []tool.Tool{
//	        // server-side tools work alongside client tools
//	    },
//	})
//
// When no client tools are present in state (e.g. the request came from a
// non-CopilotKit frontend or the agent is called without RunAgentInput.Tools),
// the toolset returns zero tools and does not interfere with the agent's
// server-side tools.
//
// # Validation
//
// Tool definitions with empty names are silently skipped. Duplicate names are
// deduplicated (first definition wins). JSON Schema parameters are sanitised
// for Gemini compatibility: $-prefixed keys are stripped, "examples" is mapped
// to "example" (first element), and "const" is mapped to "enum".
//
// # Security considerations
//
// Client tool definitions are untrusted input. The toolset does not execute
// arbitrary code — proxy tools only return a fixed "pending" response. Tool
// schemas are passed to the LLM's function calling interface, which applies
// its own validation. The launcher filters the [StateKey] from SSE state delta
// emissions so tool definitions are not leaked back to the client.
//
// # State key contract
//
// The launcher injects tool definitions into session state under [StateKey]
// ("_agui_client_tools") via RunRequest.StateDelta. The toolset reads this key
// in its [Toolset.Tools] method. This coupling is by design — the key is
// exported as a constant so both sides reference the same value.
package clienttool
