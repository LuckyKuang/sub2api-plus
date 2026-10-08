import { describe, expect, it } from 'vitest'
import { mapErrorCategory } from '../errorCategory'

// Approved usage audit: local refusals have their own category; provider
// policy denials, dependency failures and unknown types must keep attribution.
describe('security audit refusal category', () => {
  it.each(['content_policy_violation', 'session_blocked_by_content_policy', 'prompt_guard_blocked'])('classifies local %s only in request phase', (code) => {
    expect(mapErrorCategory('request', code)).toBe('security_audit')
    expect(mapErrorCategory('upstream', code)).toBe('upstream')
    expect(mapErrorCategory('internal', code)).toBe('internal')
  })
  it('keeps dependency outages and unknown codes outside the refusal category', () => {
    expect(mapErrorCategory('internal', 'prompt_guard_unavailable')).toBe('internal')
    expect(mapErrorCategory('request', 'unrecognized')).toBe('other')
    expect(mapErrorCategory('request', 'cyber_policy')).toBe('cyber')
  })
})
