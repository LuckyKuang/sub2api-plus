import { describe, expect, it } from 'vitest'
import { estimatedTps, strictFirstTokenMs, tpsNoteReason, tpsReason, tpsUnavailableReason } from '../usageTiming'
import type { UsageLog } from '@/types'

const sample = { timing_version: 1, stream: true, request_type: 'stream', is_complete: true, first_token_ms: 100, last_token_ms: 1100, duration_ms: 1200, first_output_kind: 'text', output_tokens: 100 } as UsageLog
describe('uniform usage timing', () => {
  it('rejects inconsistent token counts and Live token timestamps', () => {
    for (const patch of [{ image_output_tokens: -10 }, { audio_output_tokens: -1 }, { output_tokens: Infinity }, { image_output_tokens: 80, audio_output_tokens: 30 }]) {
      expect(estimatedTps({ ...sample, ...patch })).toBeNull()
      expect(tpsUnavailableReason({ ...sample, ...patch })).toBe('usage.timingUnavailableInvalid')
    }
    expect(strictFirstTokenMs({ ...sample, request_type: 'live' })).toBeNull()
  })
  it('explains aggregate compaction even on a non-streaming request', () => {
    expect(tpsUnavailableReason({ ...sample, request_type: 'sync', first_output_kind: 'compaction', first_token_ms: null, last_token_ms: null, duration_ms: null, output_tokens: 0 })).toBe('usage.timingUnavailableCompaction')
  })
  it('excludes compaction-only billed tokens but accepts later text output', () => {
    for (const request_type of ['sync', 'stream', 'ws_v2'] as const) {
      const compact = { ...sample, request_type, first_output_kind: 'compaction' as const, first_token_ms: null, last_token_ms: null }
      expect(estimatedTps(compact)).toBeNull()
      expect(tpsUnavailableReason(compact)).toBe('usage.timingUnavailableCompaction')
      expect(estimatedTps({ ...compact, first_token_ms: 100, last_token_ms: 1100 })).toBeCloseTo(100 * 1000 / 1100)
    }
  })
  it('rejects historical semantic timestamps even when output kind exists', () => {
    const old = { ...sample, timing_version: 0 }
    expect(strictFirstTokenMs(old)).toBeNull()
    expect(estimatedTps(old)).toBeNull()
  })
  it('uses last-token wall time, including thinking wait, and excludes media tokens', () => {
    expect(estimatedTps(sample)).toBeCloseTo(1000 * 100 / 1100)
    expect(estimatedTps({ ...sample, request_type: 'ws_v2', image_output_tokens: 20, audio_output_tokens: 10 })).toBeCloseTo(70 * 1000 / 1100)
  })
  it('falls back to total duration when last-token time is missing', () => {
    expect(estimatedTps({ ...sample, last_token_ms: null })).toBeCloseTo(100 * 1000 / 1200)
    expect(estimatedTps({ ...sample, last_token_ms: 0 })).toBeCloseTo(100 * 1000 / 1200)
    expect(estimatedTps({ ...sample, last_token_ms: -1 })).toBeCloseTo(100 * 1000 / 1200)
  })
  it('still shows incomplete, sync and short-window TPS with a confidence note', () => {
    expect(estimatedTps({ ...sample, is_complete: false })).toBeCloseTo(100 * 1000 / 1100)
    expect(tpsNoteReason({ ...sample, is_complete: false })).toBe('usage.timingUnavailableIncomplete')
    expect(estimatedTps({ ...sample, request_type: 'sync', stream: false })).toBeCloseTo(100 * 1000 / 1100)
    expect(tpsNoteReason({ ...sample, request_type: 'sync', stream: false })).toBe('usage.timingUnavailableNonStream')
    expect(estimatedTps({ ...sample, last_token_ms: 200, duration_ms: 200 })).toBe(500)
    expect(tpsNoteReason({ ...sample, last_token_ms: 200, duration_ms: 200 })).toBe('usage.timingUnavailableShort')
    expect(tpsReason({ ...sample, last_token_ms: 200, duration_ms: 200 })).toBe('usage.timingUnavailableShort')
    expect(tpsReason(sample)).toBeNull()
    expect(tpsUnavailableReason({ ...sample, request_type: 'live', first_token_ms: null })).toBe('usage.timingUnavailableLive')
    expect(estimatedTps({ ...sample, first_output_kind: 'compaction', first_token_ms: null, last_token_ms: null, duration_ms: null, output_tokens: 0 })).toBeNull()
  })
})
