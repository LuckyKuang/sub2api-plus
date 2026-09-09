import { describe, expect, it } from 'vitest'
import { estimatedTps, strictFirstTokenMs, tpsUnavailableReason } from '../usageTiming'
import type { UsageLog } from '@/types'

const sample = { timing_version: 1, stream: true, request_type: 'stream', is_complete: true, first_token_ms: 100, last_token_ms: 1100, first_output_kind: 'text', output_tokens: 100 } as UsageLog
describe('uniform usage timing', () => {
  it('rejects inconsistent token counts and Live token timestamps', () => {
    for (const patch of [{ image_output_tokens: -10 }, { audio_output_tokens: -1 }, { output_tokens: Infinity }, { image_output_tokens: 80, audio_output_tokens: 30 }]) {
      expect(estimatedTps({ ...sample, ...patch })).toBeNull()
      expect(tpsUnavailableReason({ ...sample, ...patch })).toBe('usage.timingUnavailableInvalid')
    }
    expect(strictFirstTokenMs({ ...sample, request_type: 'live' })).toBeNull()
  })
  it('explains aggregate compaction even on a non-streaming request', () => {
    expect(tpsUnavailableReason({ ...sample, request_type: 'sync', first_output_kind: 'compaction', first_token_ms: null, last_token_ms: null })).toBe('usage.timingUnavailableCompaction')
  })
  it('rejects historical semantic timestamps even when output kind exists', () => {
    const old = { ...sample, timing_version: 0 }
    expect(strictFirstTokenMs(old)).toBeNull()
    expect(estimatedTps(old)).toBeNull()
  })
  it('uses the same token window for HTTP and WS and excludes media tokens', () => {
    expect(estimatedTps(sample)).toBe(100)
    expect(estimatedTps({ ...sample, request_type: 'ws_v2', image_output_tokens: 20, audio_output_tokens: 10 })).toBe(70)
  })
  it('does not manufacture compact, incomplete, sync or short-window TPS', () => {
    expect(tpsUnavailableReason({ ...sample, first_output_kind: 'compaction', first_token_ms: null, last_token_ms: null })).toBe('usage.timingUnavailableCompaction')
    expect(estimatedTps({ ...sample, is_complete: false })).toBeNull()
    expect(estimatedTps({ ...sample, request_type: 'sync', stream: false })).toBeNull()
    expect(tpsUnavailableReason({ ...sample, request_type: 'live', first_token_ms: null })).toBe('usage.timingUnavailableLive')
    expect(estimatedTps({ ...sample, last_token_ms: 200 })).toBeNull()
  })
})
