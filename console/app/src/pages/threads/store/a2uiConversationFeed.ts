/**
 * A2UI conversation feed processor.
 *
 * Iterates over a list of ChatMessages and feeds any embedded A2UI payloads
 * into the A2UI MessageProcessor for the given scope. This is used when
 * loading thread history to replay A2UI surface state (createSurface,
 * updateComponents, etc.) so that surfaces render correctly on page load.
 *
 * Each payload is deduplicated by a key composed of the message's resource
 * name (or ID) and the payload index, ensuring idempotent replay.
 *
 * @module pages/threads/store/a2uiConversationFeed
 */
import { ingestA2uiPayloadOnce } from '@/a2ui'
import type { ChatMessage } from '@/pages/threads/types'

/**
 * Feeds A2UI payloads from a list of ChatMessages into the A2UI processor.
 * Processes each payload at most once per scope (deduplication by message key + index).
 *
 * @param scopeKey - The A2UI processor scope key (typically the thread's context ID).
 * @param messages - The ChatMessages to scan for A2UI payloads.
 */
export function processConversationA2uiMessages(scopeKey: string, messages: ChatMessage[]): void {
  if (!scopeKey) return
  for (const message of messages) {
    const payloads = message.a2uiPayloads
    if (!payloads?.length) continue
    const keyBase = message.resourceName || message.id
    payloads.forEach((payload, idx) => {
      ingestA2uiPayloadOnce(scopeKey, `${keyBase}:${idx}`, payload)
    })
  }
}
