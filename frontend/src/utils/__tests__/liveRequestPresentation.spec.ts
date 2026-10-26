import { describe, expect, it } from 'vitest'
import type { LiveRequest } from '@/api/liveRequests'
import { liveAbnormal, liveFirstToken, liveRate, liveCSVValue } from '../liveRequestPresentation'

const row = (data: Partial<LiveRequest>): LiveRequest => ({ id: 'r', version: 1, user_id: 7, api_key_id: 9, key_name: 'key', model: 'model', endpoint: '/v1/responses', request_id: '', request_type: 'stream', status: 'completed', started_at: '', updated_at: '', elapsed_ms: 9000, ...data })
describe('approved live-list data contract', () => {
  it('retains the existing forwarding-average TPS without substituting ingress elapsed or fabricating timing metadata', () => {
    expect(liveRate(row({ duration_ms: 2000, elapsed_ms: 9000, output_tokens: 100, timing_version: 1, first_token_ms: 800 }))).toBe('50 tok/s')
    expect(liveFirstToken(row({ first_token_ms: 800 }))).toBeNull()
    expect(liveRate(row({ duration_ms: 2000, output_tokens: 100, native_compaction_v2: true }))).toBe('—')
    expect(liveRate(row({ duration_ms: 2000, output_tokens: 100, image_output_tokens: 100 }))).toBe('—')
    expect(liveRate(row({ request_type: 'live', duration_ms: 2000, output_tokens: 100 }))).toBe('—')
  })
  it('does not classify insufficient observations as successful or abnormal', () => {
    expect(liveAbnormal(row({ status: 'finished' }))).toBe(false)
    expect(liveAbnormal(row({ status: 'unknown' }))).toBe(false)
    expect(liveAbnormal(row({ status: 'client_disconnected' }))).toBe(true)
    expect(liveAbnormal(row({ status: 'failed', http_status: 200 }))).toBe(true)
  })
  it('preserves missing and tiny fee values and protects CSV cells', () => {
    expect(liveCSVValue(undefined)).toBe('""')
    expect(liveCSVValue(0.000000001)).toBe('"1e-9"')
    expect(liveCSVValue('=SUM(A1)')).toBe('"\'=SUM(A1)"')
    expect(liveCSVValue('a,"b"')).toBe('"a,""b"""')
  })
})
