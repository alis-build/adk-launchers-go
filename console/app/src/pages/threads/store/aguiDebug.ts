/**
 * AG-UI debug payloads for the message debug dialog.
 *
 * @module pages/threads/store/aguiDebug
 */
import type { AGUIMessage } from '../clients'
import type { AGUIDebugPayload, ChatMessage } from '../types'

export type { AGUIDebugPayload } from '../types'

function cloneForDebug(value: unknown): unknown {
  if (value === null || value === undefined) {
    return value
  }
  try {
    return JSON.parse(JSON.stringify(value))
  } catch {
    return value
  }
}

/** Debug payload for a message returned by GET /agui/threads/.../messages. */
export function aguiHistoryMessageDebugData(message: AGUIMessage): AGUIDebugPayload {
  return {
    source: 'agui',
    kind: 'historyMessage',
    data: cloneForDebug(message),
  }
}

/** Debug payload for a user message composed in the console UI. */
export function aguiUserInputDebugData(input: {
  text: string
  parts?: unknown[]
}): AGUIDebugPayload {
  return {
    source: 'agui',
    kind: 'userInput',
    data: cloneForDebug(input),
  }
}

/** Debug payload for an AG-UI SSE subscriber event (or synthetic status). */
export function aguiStreamDebugData(kind: string, data: unknown): AGUIDebugPayload {
  return {
    source: 'agui',
    kind,
    data: cloneForDebug(data),
  }
}

/**
 * Returns JSON-safe debug data for the debug dialog (mirrors legacy `getDebugJson()`).
 */
export function getChatMessageDebugJson(msg: ChatMessage): unknown {
  if (msg.debugData !== undefined) {
    return msg.debugData
  }
  return cloneForDebug({
    id: msg.id,
    role: msg.role,
    content: msg.content,
    payloadType: msg.payloadType,
    resourceName: msg.resourceName,
    taskId: msg.taskId,
    contextId: msg.contextId,
  })
}
