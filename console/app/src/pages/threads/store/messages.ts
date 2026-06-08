/**
 * Thread message state management.
 *
 * Maintains a reactive map of ChatMessage arrays keyed by thread ID.
 *
 * Supports two ingestion paths:
 * 1. **History replay** via `setMessagesFromAGUI()` - bulk-loads AG-UI Messages
 *    from the messages endpoint.
 * 2. **Live streaming** via `addMessage()` - appends individual ChatMessages
 *    produced by `useChatStream` during an active AG-UI session.
 *
 * @module pages/threads/store/messages
 */
import { ref, type Ref } from 'vue'
import type { AGUIMessage } from '../clients'
import type { ChatMessage, Interrupt } from '../types'
import { aguiHistoryMessageDebugData } from './aguiDebug'
import { deduplicateA2UISurfaces, mergeToolResultsIntoToolCalls } from './chatAdapter'
import { inferUnresolvedInterruptFromHistory } from './interruptHistoryHydration'
import { applyToolResultToCall } from './toolCallStatus'

const messagesByThread: Ref<Record<string, ChatMessage[]>> = ref({})
/** Reactive map of open AG-UI interrupts keyed by thread ID. */
const interruptState: Ref<Record<string, Interrupt[]>> = ref({})

/** Stores the active interrupts for a thread (set on interrupt receipt or history hydration). */
export function setThreadInterrupts(threadId: string, interrupts: Interrupt[]) {
  interruptState.value = { ...interruptState.value, [threadId]: interrupts }
}

/** Returns the stored interrupt array for a thread (empty if none). */
export function getThreadInterrupts(threadId: string): Interrupt[] {
  return interruptState.value[threadId] ?? []
}

/** Removes all stored interrupts for a thread (called on run completion or thread delete). */
export function clearThreadInterrupts(threadId: string) {
  const next = { ...interruptState.value }
  delete next[threadId]
  interruptState.value = next
}

export interface UnresolvedInterruptState {
  /** ChatMessage.id of the interrupt row still awaiting resume. */
  messageId: string
  interrupts: Interrupt[]
}

/** Latest unresolved tool_call interrupts and their transcript message id. */
export function getUnresolvedInterruptState(threadId: string): UnresolvedInterruptState | null {
  const msgs = getMessages(threadId)
  let lastCompletedIdx = -1
  for (let i = msgs.length - 1; i >= 0; i--) {
    if (msgs[i]?.payloadType === 'status_update' && msgs[i]?.status === 'completed') {
      lastCompletedIdx = i
      break
    }
  }
  for (let i = msgs.length - 1; i >= 0; i--) {
    const msg = msgs[i]
    if (msg?.payloadType !== 'interrupt' || !Array.isArray(msg.interrupts) || msg.interrupts.length === 0) {
      continue
    }
    if (lastCompletedIdx > i) continue
    const interrupts = msg.interrupts.filter((intr) => intr.reason === 'tool_call')
    if (interrupts.length === 0) return null
    return { messageId: msg.id, interrupts }
  }
  return inferUnresolvedInterruptFromHistory(msgs)
}

/** Latest unresolved tool_call interrupts from in-memory transcript. */
export function getUnresolvedInterrupts(threadId: string): Interrupt[] {
  return getUnresolvedInterruptState(threadId)?.interrupts ?? []
}

/** True if any unresolved interrupt has passed its expiration deadline. */
export function hasExpiredInterrupts(threadId: string): boolean {
  const interrupts = getUnresolvedInterrupts(threadId)
  if (interrupts.length === 0) return false
  const now = new Date()
  return interrupts.some(
    (interrupt) => interrupt.expiresAt && new Date(interrupt.expiresAt) < now,
  )
}

/**
 * Appends a ChatMessage to a thread's message list.
 *
 * If the message contains only tool results (no text, no tool calls),
 * it attempts to merge those results into the existing message that
 * originally made the tool calls.
 */
export function addMessage(threadId: string, message: ChatMessage): void {
  const list = messagesByThread.value[threadId] ?? []

  // Standalone tool-result rows: merge into the message that issued the tool calls.
  if (message.toolResults?.length && !message.toolCalls?.length && !message.content) {
    const resultMap = new Map(message.toolResults.map((r) => [r.toolCallId, r]))
    let merged = false
    const updated = list.map((existing) => {
      if (!existing.toolCalls?.length) return existing
      const matched = existing.toolCalls.some((tc) => resultMap.has(tc.id))
      if (!matched) return existing
      merged = true
      const enrichedCalls = existing.toolCalls.map((tc) => {
        const match = resultMap.get(tc.id)
        if (!match) return tc
        return applyToolResultToCall(tc, match.content)
      })
      return { ...existing, toolCalls: enrichedCalls, toolResults: [...(existing.toolResults ?? []), ...message.toolResults!] }
    })
    if (merged) {
      messagesByThread.value = { ...messagesByThread.value, [threadId]: updated }
      return
    }
  }

  messagesByThread.value = {
    ...messagesByThread.value,
    [threadId]: [...list, message],
  }

  if (message.payloadType === 'interrupt' && message.interrupts?.length) {
    setThreadInterrupts(threadId, message.interrupts)
  }
}

/**
 * Replaces the full message history for a thread from AG-UI Messages.
 */
export function setMessagesFromAGUI(threadId: string, aguiMessages: AGUIMessage[]): void {
  const converted: ChatMessage[] = []
  for (const msg of aguiMessages) {
    const chatMsg = aguiMessageToChat(msg)
    if (chatMsg) converted.push(chatMsg)
  }
  const merged = deduplicateA2UISurfaces(mergeToolResultsIntoToolCalls(converted))
  messagesByThread.value = {
    ...messagesByThread.value,
    [threadId]: merged,
  }
  const inferred = inferUnresolvedInterruptFromHistory(merged)
  if (inferred?.interrupts.length) {
    setThreadInterrupts(threadId, inferred.interrupts)
  } else {
    clearThreadInterrupts(threadId)
  }
}

/**
 * Maps one AG-UI history message to {@link ChatMessage}.
 * Returns null for unknown roles.
 */
function aguiMessageToChat(msg: AGUIMessage): ChatMessage | null {
  switch (msg.role) {
    case 'user':
      return {
        id: msg.id,
        role: 'user',
        content: typeof msg.content === 'string' ? msg.content : '',
        payloadType: 'message',
        debugData: aguiHistoryMessageDebugData(msg),
      }

    case 'assistant': {
      const toolCalls = msg.toolCalls?.map((tc) => {
        let parsedArgs: Record<string, unknown> = {}
        try { parsedArgs = JSON.parse(tc.function.arguments) } catch { /* keep empty */ }
        return { id: tc.id, name: tc.function.name, args: parsedArgs }
      })

      return {
        id: msg.id,
        role: 'assistant',
        content: typeof msg.content === 'string' ? msg.content : '',
        toolCalls: toolCalls?.length ? toolCalls : undefined,
        payloadType: 'message',
        debugData: aguiHistoryMessageDebugData(msg),
      }
    }

    case 'tool':
      // AG-UI tool role is a separate row; UI model attaches results to assistant tool-call messages.
      return {
        id: msg.id,
        role: 'assistant',
        content: '',
        toolResults: [{
          toolCallId: msg.toolCallId ?? '',
          content: typeof msg.content === 'string' ? msg.content : JSON.stringify(msg.content ?? ''),
        }],
        payloadType: 'message',
        debugData: aguiHistoryMessageDebugData(msg),
      }

    case 'reasoning':
      // Shown in the status/thoughts block, not as a standalone chat bubble.
      return {
        id: msg.id,
        role: 'assistant',
        content: '',
        reasoning: typeof msg.content === 'string' ? msg.content : '',
        payloadType: 'artifact_update',
        debugData: aguiHistoryMessageDebugData(msg),
      }

    case 'activity': {
      const activityContent = msg.content as Record<string, unknown> | undefined
      const ops = activityContent?.a2ui_operations
      // Replay path: payloads are ingested again by processConversationA2uiMessages on the thread watch.
      const payloads = Array.isArray(ops) ? ops : ops != null ? [ops] : undefined
      return {
        id: msg.id,
        role: 'assistant',
        content: '',
        a2uiPayloads: payloads,
        payloadType: 'message',
        debugData: aguiHistoryMessageDebugData(msg),
      }
    }

    default:
      return null
  }
}

/**
 * Appends a page of AG-UI Messages to an existing thread's message list.
 */
export function appendMessagesFromAGUI(threadId: string, aguiMessages: AGUIMessage[]): void {
  const existing = messagesByThread.value[threadId] ?? []
  const converted: ChatMessage[] = []
  for (const msg of aguiMessages) {
    const chatMsg = aguiMessageToChat(msg)
    if (chatMsg) converted.push(chatMsg)
  }
  messagesByThread.value = {
    ...messagesByThread.value,
    [threadId]: deduplicateA2UISurfaces(mergeToolResultsIntoToolCalls([...existing, ...converted])),
  }
}

/** Returns the message list for a thread (empty array if none). */
export function getMessages(threadId: string): ChatMessage[] {
  return messagesByThread.value[threadId] ?? []
}

/** Removes all messages for a thread from the in-memory store. */
export function clearMessages(threadId: string): void {
  const nextMessages = { ...messagesByThread.value }
  delete nextMessages[threadId]
  messagesByThread.value = nextMessages
  clearThreadInterrupts(threadId)
}

/**
 * @deprecated No longer used — events come from AG-UI messages endpoint, not A2A history.
 */
export function getOrCreateContextId(threadId: string): string {
  return threadId
}
