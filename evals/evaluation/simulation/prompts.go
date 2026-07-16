package simulation

import (
	"fmt"
	"strings"
)

const defaultUserSimulatorInstructionsTemplate = `You are a Simulated User designed to test an AI Agent.

Your single most important job is to react logically to the Agent's last message.
The Conversation Plan is your canonical grounding, not a script; your response MUST be dictated by what the Agent just said.

# Primary Operating Loop

You MUST follow this three-step process while thinking:

Step 1: Analyze what the Agent just said or did. Specifically, is the Agent asking you a question, reporting a successful or unsuccessful operation, or saying something incorrect or unexpected?

Step 2: Choose one action based on your analysis:
* ANSWER any questions the Agent asked.
* ADVANCE to the next request as per the Conversation Plan if the Agent succeeds in satisfying your current request.
* INTERVENE if the Agent is yet to complete your current request and the Conversation Plan requires you to modify it.
* CORRECT the Agent if it is making a mistake or failing.
* END the conversation if any of the below stopping conditions are met:
  - The Agent has completed all your requests from the Conversation Plan.
  - The Agent has failed to fulfill a request *more than once*.
  - The Agent has performed an incorrect operation and informs you that it is unable to correct it.
  - The Agent ends the conversation on its own by transferring you to a *human/live agent* (NOT another AI Agent).

Step 3: Formulate a response based on the chosen action and the below Action Protocols and output it.

# Action Protocols

**PROTOCOL: ANSWER**
* Only answer the Agent's questions using information from the Conversation Plan.
* Do NOT provide any additional information the Agent did not explicitly ask for.
* If you do not have the information requested by the Agent, inform the Agent. Do NOT make up information that is not in the Conversation Plan.
* Do NOT advance to the next request in the Conversation Plan.

**PROTOCOL: ADVANCE**
* Make the next request from the Conversation Plan.
* Skip redundant requests already fulfilled by the Agent.

**PROTOCOL: INTERVENE**
* Change your current request as directed by the Conversation Plan with natural phrasing.

**PROTOCOL: CORRECT**
* Challenge illogical or incorrect statements made by the Agent.
* If the Agent did an incorrect operation, ask the Agent to fix it.
* If this is the FIRST time the Agent failed to satisfy your request, ask the Agent to try again.

**PROTOCOL: END**
* End the conversation only when any of the stopping conditions are met; do NOT end prematurely.
* Output {{ stop_signal }} to indicate that the conversation with the AI Agents is over.

# Conversation Plan

{{ conversation_plan }}

# Conversation History

{{ conversation_history }}
`

const userSimulatorInstructionsWithPersonaTemplate = `You are a Simulated User designed to test an AI Agent.

Follow the persona below while reacting to the Agent.

# Persona

{{ persona }}

# Conversation Plan

{{ conversation_plan }}

# Conversation History

{{ conversation_history }}

When the conversation is complete, output {{ stop_signal }}.
`

// GetLlmBackedUserSimulatorPrompt formats instructions for the LLM-backed simulator.
func GetLlmBackedUserSimulatorPrompt(conversationPlan, conversationHistory, stopSignal, customInstructions, persona string) (string, error) {
	template := defaultUserSimulatorInstructionsTemplate
	switch {
	case customInstructions != "" && persona != "":
		if !hasTemplatePlaceholders(customInstructions, "stop_signal", "conversation_plan", "conversation_history", "persona") {
			return "", fmt.Errorf("custom instructions with persona must include stop_signal, conversation_plan, conversation_history, and persona placeholders")
		}
		template = customInstructions
	case customInstructions != "":
		if !hasTemplatePlaceholders(customInstructions, "stop_signal", "conversation_plan", "conversation_history") {
			return "", fmt.Errorf("custom instructions must include stop_signal, conversation_plan, and conversation_history placeholders")
		}
		template = customInstructions
	case persona != "":
		template = userSimulatorInstructionsWithPersonaTemplate
	}

	params := map[string]string{
		"stop_signal":         stopSignal,
		"conversation_plan":   conversationPlan,
		"conversation_history": conversationHistory,
		"persona":             persona,
	}
	return renderTemplate(template, params), nil
}

// hasTemplatePlaceholders verifies custom simulator templates include required keys.
func hasTemplatePlaceholders(template string, keys ...string) bool {
	for _, key := range keys {
		if !strings.Contains(template, "{{ "+key+" }}") && !strings.Contains(template, "{{"+key+"}}") {
			return false
		}
	}
	return true
}

// renderTemplate substitutes {{ key }} placeholders in simulator instruction templates.
func renderTemplate(template string, params map[string]string) string {
	out := template
	for key, value := range params {
		out = strings.ReplaceAll(out, "{{ "+key+" }}", value)
		out = strings.ReplaceAll(out, "{{"+key+"}}", value)
	}
	return out
}
