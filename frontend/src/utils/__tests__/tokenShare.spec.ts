import { describe, expect, it } from 'vitest'
import {
  formatPromptCacheHitRate,
  formatTokenShare,
  promptCacheHitRate,
  sumTokenBuckets,
  tokenShare,
} from '../tokenShare'

describe('tokenShare', () => {
  it('returns no percentage when no meaningful denominator exists', () => {
    expect(tokenShare(0, 100)).toBeNull()
    expect(tokenShare(5, 0)).toBeNull()
    expect(formatTokenShare(5, 0)).toBeNull()
  })

  it('formats normal and tiny token shares consistently', () => {
    expect(formatTokenShare(25, 200)).toBe('12.5%')
    expect(formatTokenShare(1, 10_000)).toBe('<0.1%')
  })

  it('sums only valid positive token buckets', () => {
    expect(sumTokenBuckets(100, 20, null, -1, Number.NaN)).toBe(120)
  })
})

describe('promptCacheHitRate', () => {
  it('uses only ordinary input, cache read, and cache write in the denominator', () => {
    expect(promptCacheHitRate(100, 50, 50)).toBe(25)
    expect(formatPromptCacheHitRate(100, 50, 50)).toBe('25.0%')
  })

  it('represents zero and full cache hits', () => {
    expect(formatPromptCacheHitRate(100, 0, 0)).toBe('0.0%')
    expect(formatPromptCacheHitRate(0, 100, 0)).toBe('100.0%')
  })

  it('returns no rate without prompt input and clamps defensive values', () => {
    expect(promptCacheHitRate(0, 0, 0)).toBeNull()
    expect(promptCacheHitRate(-1, 200, -1)).toBe(100)
  })
})
