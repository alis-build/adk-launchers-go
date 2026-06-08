import { describe, expect, it } from 'vitest'
import { applyToolResultToCall, toolCallStatusFromResult } from './toolCallStatus'

describe('toolCallStatusFromResult', () => {
  it('marks rejection errors', () => {
    expect(toolCallStatusFromResult({ error: 'call is rejected' })).toBe('error')
  })

  it('marks pending_confirmation', () => {
    expect(toolCallStatusFromResult({ status: 'pending_confirmation', message: 'confirm' })).toBe('pending')
  })

  it('marks success payloads completed', () => {
    expect(toolCallStatusFromResult({ status: 'success', message: 'ok' })).toBe('completed')
  })
})

describe('applyToolResultToCall', () => {
  it('does not set completed status for pending_confirmation', () => {
    const tc = applyToolResultToCall(
      { id: 'tc-1', name: 'fetch_support_ticket', args: {} },
      JSON.stringify({ status: 'pending_confirmation' }),
    )
    expect(tc.status).toBeUndefined()
    expect(tc.result).toEqual({ status: 'pending_confirmation' })
  })
})
