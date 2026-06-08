import { describe, expect, it } from 'vitest'
import { formatTimestampLike, parseTimestampLike, timestampLikeToMs, toProtojsonTimestamp } from './client'

describe('toProtojsonTimestamp', () => {
  it('encodes seconds and nanos as RFC 3339', () => {
    expect(toProtojsonTimestamp({ seconds: 1717603200n, nanos: 0 })).toBe('2024-06-05T16:00:00.000Z')
  })
})

describe('parseTimestampLike', () => {
  it('parses protojson RFC 3339 strings', () => {
    const date = parseTimestampLike('2024-06-05T16:00:00Z')
    expect(date?.toISOString()).toBe('2024-06-05T16:00:00.000Z')
  })

  it('parses legacy object timestamps', () => {
    const date = parseTimestampLike({ seconds: '1717603200', nanos: 0 })
    expect(date?.toISOString()).toBe('2024-06-05T16:00:00.000Z')
  })
})

describe('timestampLikeToMs', () => {
  it('returns comparable millis for strings and objects', () => {
    const fromString = timestampLikeToMs('2024-06-05T16:00:00Z')
    const fromObject = timestampLikeToMs({ seconds: 1717603200, nanos: 0 })
    expect(fromString).toBe(fromObject)
  })
})

describe('formatTimestampLike', () => {
  it('formats RFC 3339 timestamps', () => {
    expect(formatTimestampLike('2024-06-05T16:00:00Z')).not.toBe('Unknown')
  })
})
