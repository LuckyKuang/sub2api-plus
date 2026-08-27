import { describe, expect, it } from 'vitest'
import {
  formatTokenShare,
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
