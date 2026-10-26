import type { LiveRequest } from '@/api/liveRequests'
import { averageTps, formatTpsNumber, strictFirstTokenMs } from './usageTiming'

export const liveAbnormal = (row: LiveRequest) => ['failed', 'rejected', 'cancelled', 'client_disconnected', 'incomplete'].includes(row.status)
export const liveActive = (row: LiveRequest) => row.status === 'processing' || row.status === 'transmitting'
export function liveTiming(row: LiveRequest) {
  return { ...row, timing_version: row.timing_version ?? 0, native_compaction_v2: row.native_compaction_v2 ?? false,
    stream: row.request_type !== 'sync', openai_ws_mode: row.request_type === 'ws_v2',
    duration_ms: row.duration_ms ?? null, output_tokens: row.output_tokens ?? NaN,
    image_output_tokens: row.image_output_tokens ?? 0, audio_output_tokens: row.audio_output_tokens ?? 0,
    first_token_ms: row.first_token_ms ?? null, first_output_kind: row.first_output_kind ?? null }
}
export function liveRate(row: LiveRequest) {
  if (liveActive(row)) return '—'
  const rate = averageTps(liveTiming(row))
  return rate == null ? '—' : `${formatTpsNumber(rate)} tok/s`
}
export const liveFirstToken = (row: LiveRequest) => strictFirstTokenMs(liveTiming(row))

export function liveStatusKey(row: LiveRequest): string {
  return `requestList.status.${row.status}`
}

/** Shared CSV escaping, including spreadsheet formula injection protection. */
export function liveCSVValue(value: unknown) {
  const raw = value == null ? '' : String(value)
  const safe = /^[=+\-@\t\r]/.test(raw) ? `'${raw}` : raw
  return `"${safe.replace(/"/g, '""')}"`
}
