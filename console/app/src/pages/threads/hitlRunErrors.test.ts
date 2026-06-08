import { describe, expect, it } from 'vitest'
import { friendlyResumeRequiredMessage, isResumeRequiredRunError } from './hitlRunErrors'

describe('hitlRunErrors', () => {
  it('detects server resume-required RunError', () => {
    expect(isResumeRequiredRunError('thread has 1 pending interrupt(s); resume is required')).toBe(true)
  })

  it('uses calmer copy when UI already shows interrupt', () => {
    expect(friendlyResumeRequiredMessage(true)).toContain('above')
    expect(friendlyResumeRequiredMessage(false)).toContain('Reload')
  })
})
