import { describe, expect, it } from 'vitest'
import type { UsageLog } from '@/types'
import { estimatedTps, strictFirstTokenMs, tpsReason } from '../usageTiming'

const sample = {
  timing_version: 1,
  stream: true,
  request_type: 'stream',
  first_token_ms: 100,
  duration_ms: 1_100,
  first_output_kind: 'text',
  output_tokens: 100,
} as UsageLog

describe('uniform usage timing', () => {
  it('uses duration minus first-token latency for streaming requests', () => {
    expect(estimatedTps(sample)).toBe(100)
    expect(estimatedTps({ ...sample, request_type: 'ws_v2', output_tokens: 52.5 })).toBe(52.5)
    expect(estimatedTps({ ...sample, timing_version: 0 })).toBe(100)
    expect(estimatedTps({ ...sample, last_token_ms: null })).toBe(100)
  })

  it('uses total duration for non-streaming requests or requests without first-token latency', () => {
    expect(estimatedTps({ ...sample, request_type: 'sync', stream: false })).toBeCloseTo(100 / 1.1)
    expect(estimatedTps({ ...sample, first_token_ms: null })).toBeCloseTo(100 / 1.1)
    expect(estimatedTps({ ...sample, first_token_ms: Number.NaN })).toBeCloseTo(100 / 1.1)
    expect(estimatedTps({ ...sample, first_token_ms: -1 })).toBeCloseTo(100 / 1.1)
  })

  it('uses the complete output token count', () => {
    expect(estimatedTps({ ...sample, output_tokens: 105, image_output_tokens: 5 })).toBe(105)
    expect(estimatedTps({ ...sample, output_tokens: 150, audio_output_tokens: 50 })).toBe(150)
    expect(estimatedTps({ ...sample, output_tokens: 0 })).toBe(0)
  })

  it('returns unavailable for invalid inputs and non-positive generation windows', () => {
    for (const patch of [
      { output_tokens: -1 },
      { output_tokens: Number.NaN },
      { output_tokens: Infinity },
      { duration_ms: null },
      { duration_ms: 0 },
      { duration_ms: -1 },
      { duration_ms: Number.NaN },
      { duration_ms: Infinity },
      { duration_ms: 100, first_token_ms: 100 },
      { duration_ms: 50, first_token_ms: 100 },
    ]) {
      expect(estimatedTps({ ...sample, ...patch })).toBeNull()
      expect(tpsReason({ ...sample, ...patch })).toBe('usage.timingUnavailableInvalid')
    }
  })

  it('keeps strict first-token display semantics separate from TPS calculation', () => {
    expect(strictFirstTokenMs(sample)).toBe(100)
    expect(strictFirstTokenMs({ ...sample, timing_version: 0 })).toBeNull()
    expect(strictFirstTokenMs({ ...sample, request_type: 'live' })).toBeNull()
  })
})
