import { describe, expect, it } from 'vitest'
import type { ChatMessage } from '../types'
import { inferUnresolvedInterruptFromHistory } from './interruptHistoryHydration'

function assistantToolCallMessage(
  id: string,
  toolCalls: ChatMessage['toolCalls'],
): ChatMessage {
  return {
    id,
    role: 'assistant',
    content: '',
    payloadType: 'message',
    toolCalls,
  }
}

describe('inferUnresolvedInterruptFromHistory', () => {
  it('infers interrupt from latest adk_request_confirmation in history', () => {
    const msgs: ChatMessage[] = [
      {
        id: 'user-1',
        role: 'user',
        content: 'hello',
        payloadType: 'message',
      },
      assistantToolCallMessage('asst-confirm', [{
        id: 'adk-confirm-1',
        name: 'adk_request_confirmation',
        args: {
          originalFunctionCall: {
            id: 'adk-fetch-1',
            name: 'fetch_support_ticket',
            args: { support_ticket_id: '1AB3546' },
          },
          toolConfirmation: {
            hint: 'Please confirm if you want to fetch the support ticket',
            confirmed: false,
            payload: { approve_support_ticket: false },
          },
        },
      }]),
    ]

    const state = inferUnresolvedInterruptFromHistory(msgs)
    expect(state?.messageId).toBe('asst-confirm')
    expect(state?.interrupts[0]).toMatchObject({
      id: 'adk-confirm-1',
      reason: 'tool_call',
      toolCallId: 'adk-fetch-1',
      message: 'Please confirm if you want to fetch the support ticket',
    })
  })

  it('returns null after completed status', () => {
    const msgs: ChatMessage[] = [
      assistantToolCallMessage('asst-confirm', [{
        id: 'adk-confirm-1',
        name: 'adk_request_confirmation',
        args: {
          originalFunctionCall: { id: 'tc-1', name: 'fetch_support_ticket', args: {} },
          toolConfirmation: { hint: 'confirm', confirmed: false },
        },
      }]),
      {
        id: 'status-done',
        role: 'assistant',
        content: '',
        payloadType: 'status_update',
        status: 'completed',
      },
    ]
    expect(inferUnresolvedInterruptFromHistory(msgs)).toBeNull()
  })
})
