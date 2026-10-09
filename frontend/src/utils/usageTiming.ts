import type { UsageLog } from '@/types'
import { resolveUsageRequestType } from './usageRequestType'

type RequestKindRow = Pick<UsageLog, 'stream' | 'openai_ws_mode' | 'request_type'>
type FirstTokenTimingRow = RequestKindRow & Pick<UsageLog, 'timing_version' | 'first_token_ms' | 'first_output_kind'>
type TpsTimingRow = FirstTokenTimingRow & Pick<UsageLog,
  'duration_ms' | 'output_tokens' | 'image_output_tokens' | 'audio_output_tokens' | 'native_compaction_v2'
>
type TpsNoteRow = TpsTimingRow & Pick<UsageLog, 'is_complete'>

export const strictFirstTokenMs = (row: FirstTokenTimingRow): number | null =>
  resolveUsageRequestType(row) !== 'live' && row.timing_version === 1 && row.first_token_ms != null && Number.isFinite(row.first_token_ms) && row.first_token_ms >= 0
    ? row.first_token_ms : null

const nonMediaOutputTokens = (row: TpsTimingRow): number =>
  row.output_tokens - (row.image_output_tokens ?? 0) - (row.audio_output_tokens ?? 0)

export const tpsReason = (row: TpsTimingRow): string | null => {
  if (resolveUsageRequestType(row) === 'live') return 'usage.timingUnavailableLive'
  const counts = [row.output_tokens, row.image_output_tokens ?? 0, row.audio_output_tokens ?? 0]
  if (counts.some(value => !Number.isFinite(value) || value < 0) ||
    row.duration_ms == null || !Number.isFinite(row.duration_ms) || row.duration_ms <= 0 ||
    nonMediaOutputTokens(row) < 0) return 'usage.timingUnavailableInvalid'

  const hasObservedTokens = strictFirstTokenMs(row) != null
  if ((row.first_output_kind === 'compaction' || row.native_compaction_v2) && !hasObservedTokens) {
    return 'usage.timingUnavailableCompaction'
  }
  if (nonMediaOutputTokens(row) === 0 ||
    ((row.first_output_kind === 'image' || row.first_output_kind === 'audio') && !hasObservedTokens)) {
    return 'usage.timingUnavailableNoTextTokens'
  }
  return Number.isFinite(nonMediaOutputTokens(row) * 1000 / row.duration_ms)
    ? null : 'usage.timingUnavailableInvalid'
}

// This is a forwarding/turn average, not a token-event decode-window rate.
export const averageTps = (row: TpsTimingRow): number | null =>
  tpsReason(row) == null ? nonMediaOutputTokens(row) * 1000 / row.duration_ms! : null

export const tpsNote = (row: TpsNoteRow): string | null =>
  tpsReason(row) ?? (row.is_complete === false ? 'usage.averageTpsIncomplete' : null)

export const firstTokenUnavailableReason = (row: FirstTokenTimingRow): string | null => {
  if (resolveUsageRequestType(row) === 'live') return 'usage.timingUnavailableLive'
  if (strictFirstTokenMs(row) != null) return null
  if (row.timing_version !== 1) return 'usage.timingUnavailableHistorical'
  if (row.first_output_kind === 'compaction') return 'usage.timingUnavailableCompaction'
  return 'usage.timingUnavailableNoTokens'
}

// Presentation only: statistics always aggregate unrounded per-request values.
export const formatTpsNumber = (value: number): string => {
  if (value < 0.1) return Number(value.toPrecision(2)).toString()
  if (value >= 100) return String(Math.round(value))
  return (Math.round(value * 10) / 10).toFixed(1).replace(/\.0$/, '')
}
