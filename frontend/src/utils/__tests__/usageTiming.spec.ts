import { describe, expect, it } from 'vitest'
import type { UsageLog } from '@/types'
import { averageTps, strictFirstTokenMs, tpsNote, tpsReason } from '../usageTiming'

const sample = {
  timing_version: 1,
  stream: true,
  request_type: 'stream',
  first_token_ms: 100,
  duration_ms: 1_100,
  first_output_kind: 'text',
  output_tokens: 100,
  image_output_tokens: 0,
  audio_output_tokens: 0,
  is_complete: true,
} as UsageLog

describe('request-average usage TPS', () => {
  it.each(['stream', 'ws_v2', 'sync', 'unknown', 'cyber'] as const)(
    'uses total duration for %s without relying on token-event timing', request_type => {
      expect(averageTps({ ...sample, request_type })).toBeCloseTo(100 / 1.1)
      expect(averageTps({ ...sample, request_type, first_token_ms: null, last_token_ms: null })).toBeCloseTo(100 / 1.1)
    },
  )

  it('does not inflate concentrated output after a long wait', () => {
    const burst = { ...sample, output_tokens: 500, duration_ms: 10_000, first_token_ms: 9_990, last_token_ms: 9_991 }
    expect(averageTps(burst)).toBe(50)
    expect(averageTps({ ...burst, last_token_ms: burst.first_token_ms })).toBe(50)
    expect(averageTps({ ...burst, first_token_ms: burst.duration_ms })).toBe(50)
    expect(averageTps({ ...burst, first_token_ms: burst.duration_ms + 1 })).toBe(50)
  })

  it('keeps historical averages independent of strict first-token display', () => {
    const historical = { ...sample, timing_version: 0 }
    expect(strictFirstTokenMs(historical)).toBeNull()
    expect(averageTps(historical)).toBeCloseTo(100 / 1.1)
    expect(tpsNote(historical)).toBeNull()
    for (const first_token_ms of [null, -1, Number.NaN, Infinity]) {
      expect(averageTps({ ...historical, first_token_ms })).toBeCloseTo(100 / 1.1)
    }
  })

  it('excludes both media counts and tolerates absent legacy modality fields', () => {
    expect(averageTps({ ...sample, output_tokens: 150, image_output_tokens: 30, audio_output_tokens: 20, duration_ms: 2_000 })).toBe(50)
    expect(averageTps({ ...sample, image_output_tokens: undefined, audio_output_tokens: undefined } as unknown as UsageLog)).toBeCloseTo(100 / 1.1)
    expect(tpsReason({ ...sample, output_tokens: 50, audio_output_tokens: 50 })).toBe('usage.timingUnavailableNoTextTokens')
    expect(tpsReason({ ...sample, output_tokens: 0 })).toBe('usage.timingUnavailableNoTextTokens')
  })

  it('excludes Live, compaction-only and known media-only records', () => {
    expect(tpsReason({ ...sample, request_type: 'live' })).toBe('usage.timingUnavailableLive')
    expect(averageTps({ ...sample, request_type: 'live' })).toBeNull()
    for (const first_output_kind of ['compaction', 'image', 'audio'] as const) {
      const row = { ...sample, first_output_kind, first_token_ms: null, last_token_ms: null }
      expect(averageTps(row)).toBeNull()
      expect(tpsReason(row)).toBe(first_output_kind === 'compaction'
        ? 'usage.timingUnavailableCompaction' : 'usage.timingUnavailableNoTextTokens')
      expect(averageTps({ ...row, first_token_ms: 100 })).toBeCloseTo(100 / 1.1)
    }
    expect(averageTps({ ...sample, native_compaction_v2: true, first_token_ms: null })).toBeNull()
  })

  it('returns explanatory notes for incomplete averages and prioritizes unavailable reasons', () => {
    const incomplete = { ...sample, is_complete: false }
    expect(averageTps(incomplete)).toBeCloseTo(100 / 1.1)
    expect(tpsReason(incomplete)).toBeNull()
    expect(tpsNote(incomplete)).toBe('usage.averageTpsIncomplete')
    expect(tpsNote({ ...incomplete, duration_ms: 0 })).toBe('usage.timingUnavailableInvalid')
  })

  it('rejects invalid counters, inconsistent modality totals and invalid duration', () => {
    const patches = [
      ...[Number.NaN, Infinity, -1].flatMap(value => [
        { output_tokens: value }, { image_output_tokens: value }, { audio_output_tokens: value },
      ]),
      { output_tokens: 100, image_output_tokens: 60, audio_output_tokens: 50 },
      ...[null, 0, -1, Number.NaN, Infinity].map(duration_ms => ({ duration_ms })),
      { output_tokens: Number.MAX_VALUE, duration_ms: Number.MIN_VALUE },
    ]
    for (const patch of patches) {
      const row = { ...sample, ...patch }
      expect(averageTps(row)).toBeNull()
      expect(tpsReason(row)).toBe('usage.timingUnavailableInvalid')
    }
  })

  it('retains short and small request averages without clipping or minimum gates', () => {
    expect(averageTps({ ...sample, output_tokens: 1, duration_ms: 10 })).toBe(100)
    expect(averageTps({ ...sample, output_tokens: 2_000, duration_ms: 1 })).toBe(2_000_000)
    expect(averageTps({ ...sample, output_tokens: 1, duration_ms: 120_500 })).toBeCloseTo(1 / 120.5)
    expect(strictFirstTokenMs(sample)).toBe(100)
  })
})
