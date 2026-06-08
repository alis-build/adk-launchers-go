/**
 * Chat adapter — utility functions for processing ChatMessage arrays.
 *
 * Provides deduplication and merge utilities used by the messages store
 * when processing AG-UI Message history.
 *
 * @module pages/threads/store/chatAdapter
 */
import { getA2UICreateSurfaceId } from '@/a2ui'
import type { ChatMessage } from '../types'
import { applyToolResultToCall } from './toolCallStatus'

/**
 * Removes duplicate A2UI surface messages that share the same surface ID.
 * Keeps the first occurrence; subsequent messages for the same surface
 * are filtered out since they are handled by the A2UI processor.
 */
export function deduplicateA2UISurfaces(messages: ChatMessage[]): ChatMessage[] {
  const seenSurfaces = new Set<string>()
  return messages.filter((msg) => {
    // Processor keeps one surface per id; drop duplicate snapshot-only rows from history.
    if (!msg.a2uiPayloads?.length || msg.content) return true
    const surfaceId = msg.a2uiPayloads
      .map((p) => getA2UICreateSurfaceId(p as Record<string, unknown>))
      .find(Boolean)
    if (!surfaceId) return true
    if (seenSurfaces.has(surfaceId)) return false
    seenSurfaces.add(surfaceId)
    return true
  })
}

/**
 * Merges standalone tool-result messages into the messages that originally
 * made the corresponding tool calls.
 */
export function mergeToolResultsIntoToolCalls(messages: ChatMessage[]): ChatMessage[] {
  const toolCallById = new Map<string, number>()
  // First pass: index messages that own toolCalls by tool call id.
  for (let i = 0; i < messages.length; i++) {
    const msg = messages[i]!
    if (msg.toolCalls?.length) {
      for (const tc of msg.toolCalls) {
        if (tc.id) toolCallById.set(tc.id, i)
      }
    }
  }

  const mergedIndices = new Set<number>()
  for (let i = 0; i < messages.length; i++) {
    const msg = messages[i]!
    if (!msg.toolResults?.length || msg.toolCalls?.length) continue
    for (const tr of msg.toolResults) {
      const callIdx = toolCallById.get(tr.toolCallId)
      if (callIdx === undefined) continue
      const callMsg = messages[callIdx]!
      callMsg.toolCalls = callMsg.toolCalls!.map((tc) =>
        tc.id === tr.toolCallId ? applyToolResultToCall(tc, tr.content) : tc,
      )
    }
    // Drop result-only rows once merged; keep rows that still carry visible content.
    if (!msg.content && !msg.files?.length && !msg.a2uiPayloads?.length) {
      mergedIndices.add(i)
    }
  }

  if (mergedIndices.size === 0) return messages
  return messages.filter((_, i) => !mergedIndices.has(i))
}
