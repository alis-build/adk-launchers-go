/**
 * Builders for A2UI-specific action payloads.
 *
 * These are used when sending A2UI client actions (user interactions with
 * rendered surfaces) back to the agent as text messages via AG-UI.
 *
 * @module a2ui/actionBuilder
 */
import type { A2uiClientAction } from '@a2ui/web_core/v0_9'

/**
 * Build a text prompt from an A2UI v0.9 client action.
 *
 * The action is formatted as a structured prompt that the agent's A2UI
 * converter recognizes, matching the format used by the backend
 * `A2uiA2APartConverter`.
 *
 * @param action - The client action from the A2UI renderer (button click, form submit, etc.).
 * @returns A text string suitable for sending as an AG-UI user message.
 */
export async function buildA2UIActionPart(action: A2uiClientAction): Promise<string> {
  const actionMsg: Record<string, unknown> = {
    name: action.name,
    surfaceId: action.surfaceId ?? 'unknown-surface',
    sourceComponentId: action.sourceComponentId ?? 'unknown-component',
    timestamp: action.timestamp ?? new Date().toISOString(),
    context: action.context ?? {},
  }

  const payload = [{ version: 'v0.9', action: actionMsg }]
  const actionBytes = JSON.stringify(payload, null, 2)

  return (
    'SYSTEM EVENT: The user interacted with the A2UI interface you generated. ' +
    'Here is the structured action payload:\n' +
    '```json\n' +
    actionBytes +
    '\n```\n' +
    'Treat this payload as the user\'s direct response to your previous action. ' +
    'Process this data to continue fulfilling their ongoing request.'
  )
}
