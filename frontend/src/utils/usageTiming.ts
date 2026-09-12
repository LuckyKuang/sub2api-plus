import type { UsageLog } from '@/types'
import { textOutputTokens } from './imageUsage'
import { resolveUsageRequestType } from './usageRequestType'

type TimingRow = Pick<UsageLog, 'timing_version' | 'first_token_ms' | 'last_token_ms' | 'first_output_kind' | 'is_complete' | 'output_tokens' | 'image_output_tokens' | 'audio_output_tokens' | 'stream' | 'openai_ws_mode' | 'request_type'>

export const strictFirstTokenMs = (row: TimingRow): number | null =>
  resolveUsageRequestType(row) !== 'live' && row.timing_version === 1 && row.first_token_ms != null && Number.isFinite(row.first_token_ms) && row.first_token_ms >= 0
    ? row.first_token_ms : null

const tpsWindowMs = (row: TimingRow): number | null => {
  const first = strictFirstTokenMs(row)
  if (first == null || row.last_token_ms == null || !Number.isFinite(row.last_token_ms)) return null
  const window = row.last_token_ms - first
  return Number.isFinite(window) && window > 0 ? window : null
}

const invalidTokenCounts = (row: TimingRow): boolean => {
  const output = row.output_tokens
  const image = row.image_output_tokens ?? 0
  const audio = row.audio_output_tokens ?? 0
  return [output, image, audio].some(count => !Number.isFinite(count) || count < 0) || image + audio > output
}

const noTokenTpsReason = (row: TimingRow): string =>
  row.first_output_kind === 'compaction' ? 'usage.timingUnavailableCompaction' : 'usage.timingUnavailableNoTokens'

export const tpsUnavailableReason = (row: TimingRow): string | null => {
  if (resolveUsageRequestType(row) === 'live') return 'usage.timingUnavailableLive'
  if (row.timing_version !== 1) return 'usage.timingUnavailableHistorical'
  if (invalidTokenCounts(row)) return 'usage.timingUnavailableInvalid'
  if (row.first_output_kind === 'compaction' && strictFirstTokenMs(row) == null) return 'usage.timingUnavailableCompaction'
  const tokens = textOutputTokens(row)
  if (!Number.isFinite(tokens) || tokens <= 0) return noTokenTpsReason(row)
  const first = strictFirstTokenMs(row)
  if (first == null || row.last_token_ms == null || !Number.isFinite(row.last_token_ms)) {
    return noTokenTpsReason(row)
  }
  const window = row.last_token_ms - first
  if (!Number.isFinite(window) || window <= 0) return 'usage.timingUnavailableInvalid'
  return null
}

export const tpsNoteReason = (row: TimingRow): string | null => {
  if (tpsUnavailableReason(row) != null) return null
  if (row.is_complete !== true) return 'usage.timingUnavailableIncomplete'
  const type = resolveUsageRequestType(row)
  if (type !== 'stream' && type !== 'ws_v2') return 'usage.timingUnavailableNonStream'
  const window = tpsWindowMs(row)
  if (window == null || textOutputTokens(row) < 8 || window < 300) return 'usage.timingUnavailableShort'
  return null
}

export const tpsReason = (row: TimingRow): string | null =>
  tpsUnavailableReason(row) ?? tpsNoteReason(row)

export const estimatedTps = (row: TimingRow): number | null => {
  if (tpsUnavailableReason(row) != null) return null
  const window = tpsWindowMs(row)
  if (window == null) return null
  const value = textOutputTokens(row) * 1000 / window
  return Number.isFinite(value) && value > 0 ? value : null
}

export const firstTokenUnavailableReason = (row: TimingRow): string | null => {
  if (resolveUsageRequestType(row) === 'live') return 'usage.timingUnavailableLive'
  if (strictFirstTokenMs(row) != null) return null
  if (row.timing_version !== 1) return 'usage.timingUnavailableHistorical'
  if (row.first_output_kind === 'compaction') return 'usage.timingUnavailableCompaction'
  return 'usage.timingUnavailableNoTokens'
}
