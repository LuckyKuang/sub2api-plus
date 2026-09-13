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
      expect(estimatedTps({ ...compact, first_token_ms: 100, last_token_ms: 1100 })).toBe(100)
    }
  })
  it('rejects historical semantic timestamps even when output kind exists', () => {
    const old = { ...sample, timing_version: 0 }
    expect(strictFirstTokenMs(old)).toBeNull()
    expect(estimatedTps(old)).toBeNull()
  })
  it('uses the decode window last minus first and excludes media tokens', () => {
    expect(estimatedTps(sample)).toBe(100)
    expect(estimatedTps({ ...sample, request_type: 'ws_v2', image_output_tokens: 20, audio_output_tokens: 10 })).toBe(70)
  })
  it('does not fall back to total duration when last-token time is missing', () => {
    expect(estimatedTps({ ...sample, last_token_ms: null })).toBeNull()
    expect(tpsUnavailableReason({ ...sample, last_token_ms: null })).toBe('usage.timingUnavailableNoTokens')
    expect(estimatedTps({ ...sample, last_token_ms: 0 })).toBeNull()
    expect(tpsUnavailableReason({ ...sample, last_token_ms: 0 })).toBe('usage.timingUnavailableInvalid')
    expect(estimatedTps({ ...sample, last_token_ms: -1 })).toBeNull()
    expect(tpsUnavailableReason({ ...sample, last_token_ms: -1 })).toBe('usage.timingUnavailableInvalid')
    expect(estimatedTps({ ...sample, first_token_ms: null })).toBeNull()
    expect(tpsUnavailableReason({ ...sample, first_token_ms: null })).toBe('usage.timingUnavailableNoTokens')
    expect(estimatedTps({ ...sample, last_token_ms: Number.NaN })).toBeNull()
    expect(tpsUnavailableReason({ ...sample, last_token_ms: Number.NaN })).toBe('usage.timingUnavailableNoTokens')
  })
  it('rejects a non-positive decode window as invalid timing', () => {
    expect(estimatedTps({ ...sample, last_token_ms: 100 })).toBeNull()
    expect(tpsUnavailableReason({ ...sample, last_token_ms: 100 })).toBe('usage.timingUnavailableInvalid')
    expect(estimatedTps({ ...sample, last_token_ms: 50 })).toBeNull()
    expect(tpsUnavailableReason({ ...sample, last_token_ms: 50 })).toBe('usage.timingUnavailableInvalid')
  })
  it('still shows incomplete, sync and short-window TPS with a confidence note', () => {
    expect(estimatedTps({ ...sample, is_complete: false })).toBe(100)
    expect(tpsNoteReason({ ...sample, is_complete: false })).toBe('usage.timingUnavailableIncomplete')
    expect(estimatedTps({ ...sample, request_type: 'sync', stream: false })).toBe(100)
    expect(tpsNoteReason({ ...sample, request_type: 'sync', stream: false })).toBe('usage.timingUnavailableNonStream')
    expect(estimatedTps({ ...sample, is_complete: null })).toBe(100)
    expect(tpsNoteReason({ ...sample, is_complete: null })).toBe('usage.timingUnavailableIncomplete')
    expect(estimatedTps({ ...sample, last_token_ms: 200, duration_ms: 200 })).toBe(1000)
    expect(tpsNoteReason({ ...sample, last_token_ms: 200, duration_ms: 200 })).toBe('usage.timingUnavailableShort')
    expect(tpsReason({ ...sample, last_token_ms: 200, duration_ms: 200 })).toBe('usage.timingUnavailableShort')
    expect(estimatedTps({ ...sample, output_tokens: 7 })).toBe(7)
    expect(tpsNoteReason({ ...sample, output_tokens: 7 })).toBe('usage.timingUnavailableShort')
    expect(estimatedTps({ ...sample, output_tokens: 8, last_token_ms: 400 })).toBeCloseTo(8 * 1000 / 300)
    expect(tpsNoteReason({ ...sample, output_tokens: 8, last_token_ms: 400 })).toBeNull()
    expect(tpsReason(sample)).toBeNull()
    expect(tpsUnavailableReason({ ...sample, request_type: 'live', first_token_ms: null })).toBe('usage.timingUnavailableLive')
    expect(estimatedTps({ ...sample, first_output_kind: 'compaction', first_token_ms: null, last_token_ms: null, duration_ms: null, output_tokens: 0 })).toBeNull()
  })
  it('reports decode-rate TPS for long thinking then visible output', () => {
    const codex = { ...sample, output_tokens: 200, first_token_ms: 20_000, last_token_ms: 22_000, duration_ms: 23_000 }
    expect(estimatedTps(codex)).toBe(100)
    expect(tpsReason(codex)).toBeNull()
    const shortWrite = { ...sample, first_token_ms: 20_000, last_token_ms: 20_200, duration_ms: 20_500 }
    expect(estimatedTps(shortWrite)).toBe(500)
    expect(tpsNoteReason(shortWrite)).toBe('usage.timingUnavailableShort')
  })
})
