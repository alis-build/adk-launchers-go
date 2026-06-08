import type { Interrupt } from '@/pages/threads/types'
import { describe, expect, it } from 'vitest'
import { buildInterruptResumeEntries, buildSingleResumeEntry } from './toolInterruptResume'

describe('ToolInterruptResolver resume payloads', () => {
  it('single interrupt: Allow & run uses buildSingleResumeEntry', () => {
    const entries = buildSingleResumeEntry('intr-1', { approved: true })
    expect(entries[0]?.status).toBe('resolved')
    expect(entries[0]?.payload).toEqual({ approved: true })
  })

  it('batch: approve-all state does not submit until buildInterruptResumeEntries', () => {
    const interrupts: Interrupt[] = [
      { id: 'a', reason: 'tool_call' },
      { id: 'b', reason: 'tool_call' },
    ]
    const partialInputs = { a: { payload: { approved: true } } }
    const partial = buildInterruptResumeEntries(interrupts, partialInputs)
    expect(partial).toHaveLength(2)
    expect(partial[1]?.payload).toEqual({ approved: false })

    const fullInputs = {
      a: { payload: { approved: true } },
      b: { payload: { approved: false } },
    }
    const full = buildInterruptResumeEntries(interrupts, fullInputs)
    expect(full[0]?.payload).toEqual({ approved: true })
    expect(full[1]?.payload).toEqual({ approved: false })
  })

  it('maps cancelled interrupts to cancelled status', () => {
    const interrupts: Interrupt[] = [{ id: 'intr-2', reason: 'tool_call' }]
    const entries = buildInterruptResumeEntries(interrupts, {
      'intr-2': { cancelled: true, payload: { approved: false } },
    })
    expect(entries[0]).toEqual({ interruptId: 'intr-2', status: 'cancelled' })
  })
})
