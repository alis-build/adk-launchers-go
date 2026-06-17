// Package interrupt implements AG-UI human-in-the-loop interrupt protocol helpers.
//
// It defines the persisted [Record] type, maps AG-UI resume entries to ADK
// tool-confirmation [genai.FunctionResponse] content, validates resume payloads
// against pending session state (including optional JSON Schema checks), and
// exposes the tool-confirmation response schema for interrupt outcomes.
//
// Session persistence (load/persist/clear pending interrupts) remains in the
// parent [agui] package; this package holds pure protocol logic only.
//
// This package is internal to [agui]; import agui for the public launcher API.
package interrupt
