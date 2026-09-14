import type { UsageLog } from '@/types'
import { resolveUsageRequestType } from './usageRequestType'

type RequestKindRow = Pick<UsageLog, 'stream' | 'openai_ws_mode' | 'request_type'>
type FirstTokenTimingRow = RequestKindRow & Pick<UsageLog, 'timing_version' | 'first_token_ms' | 'first_output_kind'>
type TpsTimingRow = RequestKindRow & Pick<UsageLog, 'duration_ms' | 'first_token_ms' | 'output_tokens'>

export const strictFirstTokenMs = (row: FirstTokenTimingRow): number | null =>
  resolveUsageRequestType(row) !== 'live' && row.timing_version === 1 && row.first_token_ms != null && Number.isFinite(row.first_token_ms) && row.first_token_ms >= 0
    ? row.first_token_ms : null

export const estimatedTps = (row: TpsTimingRow): number | null => {
  if (!Number.isFinite(row.output_tokens) || row.output_tokens < 0) return null
  if (row.duration_ms == null || !Number.isFinite(row.duration_ms) || row.duration_ms <= 0) return null

  const requestType = resolveUsageRequestType(row)
  const firstTokenMs = row.first_token_ms
  const hasFirstToken = firstTokenMs != null && Number.isFinite(firstTokenMs) && firstTokenMs >= 0
  const isStreaming = requestType === 'stream' || requestType === 'ws_v2'
  const generationMs = isStreaming && hasFirstToken
    ? row.duration_ms - firstTokenMs
    : row.duration_ms

  if (!Number.isFinite(generationMs) || generationMs <= 0) return null
  const value = row.output_tokens * 1000 / generationMs
  return Number.isFinite(value) ? value : null
}

export const tpsReason = (row: TpsTimingRow): string | null =>
  estimatedTps(row) == null ? 'usage.timingUnavailableInvalid' : null

export const firstTokenUnavailableReason = (row: FirstTokenTimingRow): string | null => {
  if (resolveUsageRequestType(row) === 'live') return 'usage.timingUnavailableLive'
  if (strictFirstTokenMs(row) != null) return null
  if (row.timing_version !== 1) return 'usage.timingUnavailableHistorical'
  if (row.first_output_kind === 'compaction') return 'usage.timingUnavailableCompaction'
  return 'usage.timingUnavailableNoTokens'
}
