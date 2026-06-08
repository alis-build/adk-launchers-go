/**
 * Reconstructs AG-UI interrupt state from persisted chat history.
 *
 * After a full page reload the live SSE interrupt rows are gone. This module
 * scans the message transcript for unresolved `adk_request_confirmation` tool
 * calls and synthesises equivalent {@link Interrupt} objects so the permission
 * UI can be shown without a new agent run.
 *
 * @module pages/threads/store/interruptHistoryHydration
 */
import type { ChatMessage, ChatToolCall, Interrupt } from '../types'

/** JSON Schema matching adk-launchers-go toolConfirmationResponseSchema(). */
const TOOL_CONFIRMATION_RESPONSE_SCHEMA: Record<string, unknown> = {
  type: 'object',
  properties: {
    approved: { type: 'boolean' },
    editedArgs: {
      type: 'object',
      description: 'Full replacement of the tool args. Not merged.',
    },
  },
  required: ['approved'],
}

/** Parsed fields from an `adk_request_confirmation` tool call's arguments. */
interface ConfirmationArgs {
  originalCallId: string
  originalCallName: string
  originalArgs: Record<string, unknown>
  hint: string
  payload?: Record<string, unknown>
}

/** Extracts confirmation fields from a tool call's args, or null if malformed. */
function parseConfirmationArgs(args: Record<string, unknown>): ConfirmationArgs | null {
  const original = args.originalFunctionCall as Record<string, unknown> | undefined
  const toolConfirmation = args.toolConfirmation as Record<string, unknown> | undefined
  const originalCallId = typeof original?.id === 'string' ? original.id : ''
  const originalCallName = typeof original?.name === 'string' ? original.name : ''
  if (!originalCallId || !originalCallName) return null

  const originalArgs = (original?.args as Record<string, unknown>) ?? {}
  const hint = typeof toolConfirmation?.hint === 'string'
    ? toolConfirmation.hint
    : 'Please confirm this tool call'

  const payload = toolConfirmation?.payload as Record<string, unknown> | undefined

  return {
    originalCallId,
    originalCallName,
    originalArgs,
    hint,
    payload,
  }
}

/** Synthesises an AG-UI Interrupt from a legacy confirmation tool call. */
function buildInterruptFromConfirmation(
  confirmationCall: ChatToolCall,
  parsed: ConfirmationArgs,
): Interrupt {
  const metadata: Record<string, unknown> = {
    adk: {
      confirmationCallId: confirmationCall.id,
      confirmationCallName: 'adk_request_confirmation',
      ...(parsed.payload ? { confirmationPayload: parsed.payload } : {}),
    },
  }
  if (parsed.hint) {
    metadata.hitl = { summary: parsed.hint }
  }

  return {
    id: confirmationCall.id,
    reason: 'tool_call',
    message: parsed.hint,
    toolCallId: parsed.originalCallId,
    responseSchema: TOOL_CONFIRMATION_RESPONSE_SCHEMA,
    metadata,
  }
}

/** True if any message after `fromIndex` is a completed status update. */
function hasCompletedStatusAfter(msgs: ChatMessage[], fromIndex: number): boolean {
  return msgs.slice(fromIndex + 1).some(
    (m) => m.payloadType === 'status_update' && m.status === 'completed',
  )
}

/** True when the original tool later returned a non-pending success result. */
function isOriginalToolResolvedAfter(
  msgs: ChatMessage[],
  fromIndex: number,
  originalToolCallId: string,
): boolean {
  for (let i = fromIndex + 1; i < msgs.length; i++) {
    const msg = msgs[i]
    if (!msg?.toolCalls?.length) continue
    for (const tc of msg.toolCalls) {
      if (tc.id !== originalToolCallId) continue
      if (tc.result === undefined) continue
      if (typeof tc.result === 'object' && tc.result !== null) {
        const obj = tc.result as Record<string, unknown>
        if (obj.status === 'pending_confirmation') return false
        if (typeof obj.error === 'string' && obj.error) return false
        if (obj.status === 'success' || obj.support_ticket !== undefined) return true
      }
    }
  }
  return false
}

/**
 * Reconstruct open tool_call interrupts from AG-UI history when live SSE interrupt
 * rows are absent (e.g. after full page reload). Uses the latest
 * adk_request_confirmation tool call that is not followed by completed status.
 */
export function inferUnresolvedInterruptFromHistory(
  msgs: ChatMessage[] | undefined,
): { messageId: string, interrupts: Interrupt[] } | null {
  if (!msgs?.length) return null
  for (let i = msgs.length - 1; i >= 0; i--) {
    const msg = msgs[i]!
    if (!msg.toolCalls?.length) continue

    for (const tc of msg.toolCalls) {
      if (tc.name !== 'adk_request_confirmation') continue
      const parsed = parseConfirmationArgs(tc.args ?? {})
      if (!parsed) continue
      if (hasCompletedStatusAfter(msgs, i)) continue
      if (isOriginalToolResolvedAfter(msgs, i, parsed.originalCallId)) continue

      const toolConfirmation = (tc.args?.toolConfirmation ?? {}) as Record<string, unknown>
      if (toolConfirmation.confirmed === true && tc.status === 'completed') continue

      return {
        messageId: msg.id,
        interrupts: [buildInterruptFromConfirmation(tc, parsed)],
      }
    }
  }
  return null
}
