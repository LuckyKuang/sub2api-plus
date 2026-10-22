import { describe, it, expect } from 'vitest'
import { usageCompletion, usageMoney, usageMilliseconds } from '../usageInspection'

describe('request inspection presentation contract', () => {
  it('does not turn historical unknown or incomplete usage into success', () => {
    expect(usageCompletion({ completion_status: 'unknown', is_complete: true })).toBe('unknown')
    expect(usageCompletion({ completion_status: 'completed', is_complete: false })).toBe('incomplete')
    expect(usageCompletion({ completion_status: 'client_disconnected', is_complete: false })).toBe('client_disconnected')
  })
  it('distinguishes unknown, zero, and nonzero small charges', () => {
    expect(usageMoney(undefined)).toBe('—')
    expect(usageMoney(0)).toBe('$0.00')
    expect(usageMoney(0.00000004)).toBe('$4.00e-8')
    expect(usageMilliseconds(null)).toBe('—')
    expect(usageMilliseconds(0)).toBe('0 ms')
  })
})
