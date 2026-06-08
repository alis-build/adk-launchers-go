import { describe, expect, it } from 'vitest'
import type { Interrupt } from '@/pages/threads/types'
import { getAdkInvocationId } from './toolInterruptDisplay'

describe('getAdkInvocationId', () => {
  it('returns invocation id from metadata.adk', () => {
    const interrupt = {
      id: 'confirm-1',
      reason: 'tool_call',
      metadata: {
        adk: {
          invocationId: 'e-abc123',
          confirmationCallId: 'confirm-1',
        },
      },
    } satisfies Interrupt

    expect(getAdkInvocationId(interrupt)).toBe('e-abc123')
  })

  it('returns undefined when missing or empty', () => {
    expect(getAdkInvocationId({ id: 'x', reason: 'tool_call' })).toBeUndefined()
    expect(getAdkInvocationId({
      id: 'x',
      reason: 'tool_call',
      metadata: { adk: { invocationId: '' } },
    })).toBeUndefined()
  })
})
