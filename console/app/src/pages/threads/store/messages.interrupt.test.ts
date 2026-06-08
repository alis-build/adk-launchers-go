import { beforeEach, describe, expect, it } from 'vitest'
import {
  addMessage,
  clearMessages,
  getUnresolvedInterruptState,
  getUnresolvedInterrupts,
} from './messages'
import type { ChatMessage, Interrupt } from '../types'

const THREAD = 'thread-test-1'

function toolInterrupt(id: string): Interrupt {
  return {
    id,
    reason: 'tool_call',
    toolCallId: `tc-${id}`,
  }
}

function interruptMessage(interrupts: Interrupt[]): ChatMessage {
  return {
    id: `intr-${interrupts[0]?.id ?? 'x'}`,
    role: 'assistant',
    content: '',
    payloadType: 'interrupt',
    interrupts,
  }
}

function completedStatus(): ChatMessage {
  return {
    id: 'status-completed',
    role: 'assistant',
    content: '',
    payloadType: 'status_update',
    status: 'completed',
  }
}

describe('getUnresolvedInterrupts', () => {
  beforeEach(() => {
    clearMessages(THREAD)
  })

  it('returns tool_call interrupts when not followed by completed status', () => {
    const intr = toolInterrupt('a')
    addMessage(THREAD, interruptMessage([intr]))
    expect(getUnresolvedInterrupts(THREAD)).toEqual([intr])
  })

  it('returns empty after completed status_update', () => {
    const intr = toolInterrupt('b')
    addMessage(THREAD, interruptMessage([intr]))
    addMessage(THREAD, completedStatus())
    expect(getUnresolvedInterrupts(THREAD)).toEqual([])
  })

  it('ignores non-tool_call interrupt reasons', () => {
    addMessage(THREAD, interruptMessage([{
      id: 'conf-1',
      reason: 'confirmation',
    }]))
    expect(getUnresolvedInterrupts(THREAD)).toEqual([])
  })

  it('uses latest interrupt when multiple exist and last is unresolved', () => {
    addMessage(THREAD, interruptMessage([toolInterrupt('old')]))
    addMessage(THREAD, completedStatus())
    const latest = toolInterrupt('new')
    const latestMsg = interruptMessage([latest])
    addMessage(THREAD, latestMsg)
    expect(getUnresolvedInterrupts(THREAD)).toEqual([latest])
    expect(getUnresolvedInterruptState(THREAD)).toEqual({
      messageId: latestMsg.id,
      interrupts: [latest],
    })
  })

  it('picks latest interrupt message when multiple exist in one turn without completion', () => {
    const firstMsg = interruptMessage([toolInterrupt('first')])
    const secondMsg = interruptMessage([toolInterrupt('second')])
    addMessage(THREAD, firstMsg)
    addMessage(THREAD, secondMsg)
    expect(getUnresolvedInterruptState(THREAD)?.messageId).toBe(secondMsg.id)
    expect(getUnresolvedInterruptState(THREAD)?.interrupts[0]?.id).toBe('second')
  })

  it('resolved older interrupt message is not the unresolved source', () => {
    const oldMsg = interruptMessage([toolInterrupt('old')])
    addMessage(THREAD, oldMsg)
    addMessage(THREAD, completedStatus())
    const latest = toolInterrupt('new')
    const latestMsg = interruptMessage([latest])
    addMessage(THREAD, latestMsg)
    expect(getUnresolvedInterruptState(THREAD)?.messageId).toBe(latestMsg.id)
    expect(getUnresolvedInterruptState(THREAD)?.messageId).not.toBe(oldMsg.id)
  })
})
