import type { UsageLog } from '@/types'
import { textOutputTokens } from './imageUsage'
import { resolveUsageRequestType } from './usageRequestType'

type TimingRow = Pick<UsageLog, 'timing_version' | 'first_token_ms' | 'last_token_ms' | 'first_output_kind' | 'is_complete' | 'output_tokens' | 'image_output_tokens' | 'audio_output_tokens' | 'stream' | 'openai_ws_mode' | 'request_type'>

export const strictFirstTokenMs = (row: TimingRow): number | null =>
  resolveUsageRequestType(row) !== 'live' && row.timing_version === 1 && row.first_token_ms != null && Number.isFinite(row.first_token_ms) && row.first_token_ms >= 0
    ? row.first_token_ms : null

export const tpsUnavailableReason = (row: TimingRow): string | null => {
  if (resolveUsageRequestType(row) === 'live') return 'usage.timingUnavailableLive'
  if (row.timing_version !== 1) return 'usage.timingUnavailableHistorical'
  if (row.first_output_kind === 'compaction' && row.first_token_ms == null) return 'usage.timingUnavailableCompaction'
  const type = resolveUsageRequestType(row)
  if (type !== 'stream' && type !== 'ws_v2') return 'usage.timingUnavailableNonStream'
  if (row.is_complete !== true) return 'usage.timingUnavailableIncomplete'
  if (strictFirstTokenMs(row) == null || row.last_token_ms == null) {
    return row.first_output_kind === 'compaction' ? 'usage.timingUnavailableCompaction' : 'usage.timingUnavailableNoTokens'
  }
  const window = row.last_token_ms - row.first_token_ms!
  if (!Number.isFinite(window) || window < 0) return 'usage.timingUnavailableInvalid'
  const counts = [row.output_tokens, row.image_output_tokens ?? 0, row.audio_output_tokens ?? 0]
  if (counts.some(count => !Number.isFinite(count) || count < 0) || counts[1]! + counts[2]! > counts[0]!) return 'usage.timingUnavailableInvalid'
  if (!Number.isFinite(textOutputTokens(row)) || textOutputTokens(row) < 8 || window < 300) return 'usage.timingUnavailableShort'
  return null
}

export const estimatedTps = (row: TimingRow): number | null =>
  tpsUnavailableReason(row) == null ? textOutputTokens(row) * 1000 / (row.last_token_ms! - row.first_token_ms!) : null

export const firstTokenUnavailableReason = (row: TimingRow): string | null => {
  if (resolveUsageRequestType(row) === 'live') return 'usage.timingUnavailableLive'
  if (strictFirstTokenMs(row) != null) return null
  if (row.timing_version !== 1) return 'usage.timingUnavailableHistorical'
  if (row.first_output_kind === 'compaction') return 'usage.timingUnavailableCompaction'
  return 'usage.timingUnavailableNoTokens'
}
