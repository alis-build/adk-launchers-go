import type { Interrupt } from '@/pages/threads/types'
import { describe, expect, it } from 'vitest'
import { reactive } from 'vue'
import { buildInterruptResumeEntries, buildSingleResumeEntry } from './toolInterruptResume'

describe('buildSingleResumeEntry', () => {
  it('returns one resolved entry with plain payload', () => {
    const entries = buildSingleResumeEntry('intr-1', { approved: true })
    expect(entries).toHaveLength(1)
    expect(entries[0]).toEqual({
      interruptId: 'intr-1',
      status: 'resolved',
      payload: { approved: true },
    })
    expect(() => structuredClone(entries)).not.toThrow()
  })
})

describe('buildInterruptResumeEntries', () => {
  const interrupts: Interrupt[] = [{
    id: 'intr-1',
    reason: 'tool_call',
    toolCallId: 'tc-1',
  }]

  it('produces structuredClone-safe entries from reactive userInputs', () => {
    const userInputs = reactive({
      'intr-1': { payload: { approved: true, editedArgs: { support_ticket_id: '1AB3546' } } },
    })

    const entries = buildInterruptResumeEntries(interrupts, userInputs)
    expect(() => structuredClone(entries)).not.toThrow()
    expect(entries[0]?.payload).toEqual({ approved: true, editedArgs: { support_ticket_id: '1AB3546' } })
  })
})
