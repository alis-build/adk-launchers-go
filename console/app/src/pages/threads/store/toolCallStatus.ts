/**
 * Tool call status derivation from result payloads.
 *
 * @module pages/threads/store/toolCallStatus
 */
import type { ChatToolCall } from '../types'

/** Derives display status from a parsed tool result payload. */
export function toolCallStatusFromResult(result: unknown): ChatToolCall['status'] | 'pending' {
  if (result === null || result === undefined) return 'pending'
  if (typeof result === 'object') {
    const obj = result as Record<string, unknown>
    if (typeof obj.error === 'string' && obj.error.length > 0) return 'error'
    if (obj.status === 'pending_confirmation') return 'pending'
  }
  return 'completed'
}

/** Parses a raw result string and merges it into a tool call with derived status. */
export function applyToolResultToCall(
  tc: ChatToolCall,
  resultContent: string,
): ChatToolCall {
  let parsedResult: unknown
  try {
    parsedResult = JSON.parse(resultContent)
  } catch {
    parsedResult = resultContent
  }
  const status = toolCallStatusFromResult(parsedResult)
  return {
    ...tc,
    result: parsedResult,
    status: status === 'pending' ? undefined : status,
  }
}
