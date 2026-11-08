import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/components/DataTable'
import { IpGeoCell } from '@/components/IpGeoCell'
import { EmptyState } from '@/components/ui'
import { formatDateTime, formatReasoningEffort, reasoningEffortValuesEqual } from '@/utils/format'
import { formatCacheTokens } from '@/utils/formatters'
import { formatTokenShare, sumTokenBuckets } from '@/utils/tokenShare'
import { resolveUsageRequestType } from '@/utils/usageRequestType'
import { LATENCY_TEXT_CLASSES, firstByteSeverity, durationSeverity } from '@/utils/latencyHealth'
import { getBillingModeBadgeClass, getBillingModeLabel, getDisplayBillingMode, isImageUsage } from '@/utils/billingMode'
import { formatImageBillingSize, hasImageInputTokens, hasImageOutputTokens, textInputTokens, textOutputTokens } from '@/utils/imageUsage'
import { averageTps, firstByteMs, firstTokenUnavailableReason, formatTpsNumber, tpsNote } from '@/utils/usageTiming'
import { usageMoney } from '@/utils/usageInspection'
import { fetchBatch, getEntry } from '@/utils/ipGeoLookup'
import type { UsageLog } from '@/types'
import { Icon } from './icons'
import { useClipboard } from './useClipboard'

/** User-scoped usage rows carry the same optional audit fields as the admin rows. */
type UsageTableRow = UsageLog & {
  upstream_model?: string | null
  upstream_reasoning_effort?: string | null
  upstream_response_model?: string | null
  upstream_model_mismatch?: boolean | null
  model_mapping_chain?: string | null
  account_rate_multiplier?: number | null
  account_stats_cost?: number | null
  completion_status?: string | null
}

export interface UsageTableProps {
  workspace?: boolean
  data: UsageTableRow[]
  loading?: boolean
  columns: Column[]
  serverSideSort?: boolean
  showAccountBilling?: boolean
  showUpstreamEndpoint?: boolean
  modelMetadata?: boolean
  defaultSortKey?: string
  defaultSortOrder?: 'asc' | 'desc'
  onSort?: (key: string, order: 'asc' | 'desc') => void
  onIpGeoBatchFailed?: () => void
}

/** 超过 1 分钟简化为 "Xm Ys"，免去人工换算（超过 1 小时再进位为 "Xh Ym"） */
function formatDuration(ms: number | null | undefined): string {
  if (ms == null) return '-'
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(2)}s`
  const totalSec = Math.round(ms / 1000)
  if (totalSec < 3600) return `${Math.floor(totalSec / 60)}m ${totalSec % 60}s`
  return `${Math.floor(totalSec / 3600)}h ${Math.floor((totalSec % 3600) / 60)}m`
}

const formatUserAgent = (ua: string): string => ua

function requestTokenTotal(row: Pick<UsageTableRow, 'input_tokens' | 'output_tokens' | 'cache_creation_tokens' | 'cache_read_tokens'>): number {
  return sumTokenBuckets(row.input_tokens, row.output_tokens, row.cache_creation_tokens, row.cache_read_tokens)
}

function requestTokenShare(
  row: Pick<UsageTableRow, 'input_tokens' | 'output_tokens' | 'cache_creation_tokens' | 'cache_read_tokens'>,
  tokens: number | null | undefined
): string | null {
  return formatTokenShare(tokens, requestTokenTotal(row))
}

const firstOutputModalityLabel = (
  row: UsageTableRow | null | undefined,
  role: 'kind' | 'timing',
  t: (key: string) => string
): string => {
  switch (row?.first_output_kind) {
    case 'compaction':
      return t('usage.latencyCompaction')
    case 'image':
      return t('usage.latencyFirstImage')
    case 'audio':
      return t('usage.latencyFirstAudio')
    case 'reasoning':
      return t('usage.latencyFirstReasoning')
    case 'tool':
      return t('usage.latencyFirstTool')
    case 'text':
      return role === 'kind' ? t('usage.latencyOutputKindText') : t('usage.latencyFirstOutput')
    default:
      return role === 'kind' ? (row?.first_output_kind ?? '-') : t('usage.latencyFirstOutput')
  }
}

const latencyTooltipNote = (row: UsageTableRow | null | undefined, t: (key: string) => string): string | null => {
  if (row == null) return null
  const reason = firstTokenUnavailableReason(row)
  if (reason) return t(reason)
  if (row.first_output_kind === 'image' || row.first_output_kind === 'audio') {
    if (row.first_token_ms == null) return t('usage.latencyMediaOnlyHint')
    if (row.first_output_ms != null && row.first_output_ms !== row.first_token_ms) {
      return t('usage.latencyMixedModalityHint')
    }
  }
  if ((row.first_output_kind === 'tool' || row.first_output_kind === 'reasoning') && row.first_token_ms != null) {
    return t('usage.latencyNonTextFirstHint')
  }
  if (
    row.first_output_kind === 'text' &&
    row.first_output_ms != null &&
    row.first_token_ms != null &&
    row.first_output_ms !== row.first_token_ms
  ) {
    return t('usage.latencyMixedModalityHint')
  }
  return null
}

function latencyDetailText(row: UsageTableRow, t: (key: string) => string): string {
  const lines: string[] = []
  if (row.first_byte_ms != null) lines.push(`${t('usage.latencyFirstByte')}: ${formatDuration(row.first_byte_ms)}`)
  if (row.first_token_ms != null) {
    lines.push(`${t('usage.latencyFirstToken')}: ${formatDuration(row.first_token_ms)}`)
  }
  const firstOutput = row.first_output_ms ?? null
  if (firstOutput != null) {
    lines.push(`${firstOutputModalityLabel(row, 'timing', t)}: ${formatDuration(firstOutput)}`)
  }
  if (row.first_output_kind) {
    lines.push(`${t('usage.latencyOutputKind')}: ${firstOutputModalityLabel(row, 'kind', t)}`)
  }
  if (row.duration_ms != null) lines.push(`${t('usage.latencyDuration')}: ${formatDuration(row.duration_ms)}`)
  const tps = averageTps(row)
  if (tps != null) lines.push(`${t('usage.latencyTps')}: ${formatTpsNumber(tps)}`)
  const note = latencyTooltipNote(row, t)
  if (note) lines.push(note)
  return lines.join('\n')
}

function tokenDetailText(row: UsageTableRow, t: (key: string) => string): string {
  const lines: string[] = [t('usage.tokenDetails')]
  if (row.input_tokens > 0 && !hasImageInputTokens(row)) {
    lines.push(`${t('admin.usage.inputTokens')}: ${row.input_tokens.toLocaleString()}`)
  }
  if (hasImageInputTokens(row) && textInputTokens(row) > 0) {
    lines.push(`${t('admin.usage.inputTokens')}: ${textInputTokens(row).toLocaleString()}`)
  }
  if (hasImageInputTokens(row)) {
    lines.push(`${t('usage.imageInputTokens')}: ${row.image_input_tokens.toLocaleString()}`)
  }
  if (row.output_tokens > 0 && !hasImageOutputTokens(row)) {
    lines.push(`${t('admin.usage.outputTokens')}: ${row.output_tokens.toLocaleString()}`)
  }
  if (hasImageOutputTokens(row) && textOutputTokens(row) > 0) {
    lines.push(`${t('admin.usage.outputTokens')}: ${textOutputTokens(row).toLocaleString()}`)
  }
  if (hasImageOutputTokens(row)) {
    lines.push(`${t('usage.imageOutputTokens')}: ${row.image_output_tokens.toLocaleString()}`)
  }
  if (row.cache_creation_tokens > 0) {
    if (row.cache_creation_5m_tokens > 0 || row.cache_creation_1h_tokens > 0) {
      if (row.cache_creation_5m_tokens > 0) {
        lines.push(`${t('admin.usage.cacheCreation5mTokens')}: ${row.cache_creation_5m_tokens.toLocaleString()}`)
      }
      if (row.cache_creation_1h_tokens > 0) {
        lines.push(`${t('admin.usage.cacheCreation1hTokens')}: ${row.cache_creation_1h_tokens.toLocaleString()}`)
      }
    } else {
      lines.push(`${t('admin.usage.cacheCreationTokens')}: ${row.cache_creation_tokens.toLocaleString()}`)
    }
  }
  if (row.cache_read_tokens > 0) {
    lines.push(`${t('admin.usage.cacheReadTokens')}: ${row.cache_read_tokens.toLocaleString()}`)
  }
  lines.push(`${t('usage.totalTokens')}: ${requestTokenTotal(row).toLocaleString()}`)
  return lines.join('\n')
}

function costDetailText(row: UsageTableRow, t: (key: string) => string): string {
  const lines: string[] = []
  lines.push(`${t('usage.actualCost')}: ${usageMoney(row.actual_cost)}`)
  lines.push(`${t('usage.standardCost')}: ${usageMoney(row.total_cost)}`)
  if (row.rate_multiplier != null) {
    lines.push(`${t('usage.rateMultiplier')}: ×${row.rate_multiplier}`)
  }
  if (row.long_context_billing_applied) lines.push(t('admin.usage.longContextPricingTooltip'))
  return lines.join('\n')
}

export function UsageTable({
  workspace = false,
  data,
  loading = false,
  columns,
  serverSideSort = false,
  showAccountBilling = true,
  showUpstreamEndpoint = true,
  modelMetadata = false,
  defaultSortKey,
  defaultSortOrder,
  onSort,
  onIpGeoBatchFailed
}: UsageTableProps) {
  const { t } = useTranslation()
  const { copyToClipboard } = useClipboard()
  const [ipGeoBatchLoading, setIpGeoBatchLoading] = useState(false)

  const showIpGeoToolbar = columns.some((col) => col.key === 'ip_address')

  const currentPageIps = useMemo(
    () => Array.from(new Set(data.map((row) => row.ip_address).filter((ip): ip is string => Boolean(ip)))),
    [data]
  )

  const pendingIpCount = useMemo(() => {
    if (!showIpGeoToolbar) return 0
    return currentPageIps.filter((ip) => {
      const status = getEntry(ip).status
      return status === 'idle' || status === 'error'
    }).length
  }, [showIpGeoToolbar, currentPageIps])

  const handleBatchFetchIpGeo = async () => {
    setIpGeoBatchLoading(true)
    try {
      const ok = await fetchBatch(currentPageIps)
      if (!ok) onIpGeoBatchFailed?.()
    } finally {
      setIpGeoBatchLoading(false)
    }
  }

  const hasReasoningEffortMapping = (row: UsageTableRow): boolean => {
    const requested = row.reasoning_effort?.trim() || ''
    const forwarded = row.upstream_reasoning_effort?.trim() || ''
    return requested !== '' && forwarded !== '' && !reasoningEffortValuesEqual(requested, forwarded)
  }

  const sentUpstreamModel = (row: UsageTableRow): string => row.upstream_model?.trim() || row.model?.trim() || ''

  const normalizeModelVariant = (model: string): string =>
    model
      .trim()
      .toLowerCase()
      .replace(/-latest$/, '')
      .replace(/-\d{4}-\d{2}-\d{2}$/, '')
      .replace(/-\d{8}$/, '')

  const isLikelyModelVariant = (row: UsageTableRow): boolean => {
    const sent = sentUpstreamModel(row)
    const response = row.upstream_response_model?.trim() || ''
    return sent !== '' && response !== '' && normalizeModelVariant(sent) === normalizeModelVariant(response)
  }

  const modelAuditTitle = (row: UsageTableRow): string =>
    [
      `${t('usage.requestedModel')}: ${row.model || '-'}`,
      `${t('usage.sentUpstreamModel')}: ${sentUpstreamModel(row) || '-'}`,
      `${t('usage.upstreamResponseModel')}: ${row.upstream_response_model || '-'}`
    ].join('\n')

  const getRequestTypeLabel = (row: UsageTableRow): string => {
    const requestType = resolveUsageRequestType(row)
    if (requestType === 'cyber') return t('usage.cyber')
    if (requestType === 'live') return t('usage.live')
    if (requestType === 'ws_v2') return t('usage.ws')
    if (requestType === 'stream') return t('usage.stream')
    if (requestType === 'sync') return t('usage.sync')
    return t('usage.unknown')
  }

  const getRequestTypeBadgeClass = (row: UsageTableRow): string => {
    const requestType = resolveUsageRequestType(row)
    if (requestType === 'cyber') return 'bg-red-100/[0.15] text-red-800 dark:bg-red-900/25 dark:text-red-200'
    if (requestType === 'live') return 'bg-emerald-100/[0.15] text-emerald-800 dark:bg-emerald-900/25 dark:text-emerald-200'
    if (requestType === 'ws_v2') return 'bg-violet-100/[0.15] text-violet-800 dark:bg-violet-900/25 dark:text-violet-200'
    if (requestType === 'stream') return 'bg-blue-100/[0.15] text-blue-800 dark:bg-blue-900/25 dark:text-blue-200'
    if (requestType === 'sync') return 'surface-inset text-gray-800 dark:surface-inset dark:text-gray-200'
    return 'bg-amber-100/[0.15] text-amber-800 dark:bg-amber-900/25 dark:text-amber-200'
  }

  const primaryFirstByteTextClass = (row: UsageTableRow): string => {
    const first = firstByteMs(row)
    return first == null ? 'surface-muted' : LATENCY_TEXT_CLASSES[firstByteSeverity(first)]
  }

  const durationTextClass = (row: UsageTableRow): string =>
    row.duration_ms == null ? 'surface-muted' : LATENCY_TEXT_CLASSES[durationSeverity(row.duration_ms)]

  const hasLatencyDetails = (row: UsageTableRow): boolean =>
    row.first_byte_ms != null ||
    row.first_token_ms != null ||
    row.last_token_ms != null ||
    row.duration_ms != null ||
    row.first_output_ms != null ||
    !!row.first_output_kind ||
    !!firstTokenUnavailableReason(row) ||
    !!tpsNote(row)

  const latencyBarClasses = (row: UsageTableRow): string => {
    const first = firstByteMs(row)
    const severity = first == null ? durationSeverity(row.duration_ms ?? 0) : firstByteSeverity(first)
    return severity === 'good'
      ? 'bg-emerald-400/80'
      : severity === 'warn'
        ? 'bg-amber-400/80'
        : severity === 'slow'
          ? 'bg-orange-500/80'
          : 'bg-red-500/80'
  }

  const renderCell = (key: string, row: UsageTableRow, value: unknown): React.ReactNode => {
    switch (key) {
      case 'api_key':
        return <span className="text-sm text-gray-900 dark:text-white">{row.api_key?.name || '-'}</span>
      case 'model':
        return (
          <div className="space-y-0.5 text-xs">
            {row.model_mapping_chain && row.model_mapping_chain.includes('→') ? (
              <div className="space-y-0.5">
                {row.model_mapping_chain.split('→').map((step: string, i: number) => (
                  <div
                    key={i}
                    className={`break-all ${i === 0 ? 'font-medium text-gray-900 dark:text-white' : 'surface-muted'}`}
                    style={i > 0 ? { paddingLeft: `${i * 0.75}rem` } : undefined}
                  >
                    {i > 0 && <span className="mr-0.5">↳</span>}
                    {step}
                  </div>
                ))}
              </div>
            ) : row.upstream_model && row.upstream_model !== row.model ? (
              <div className="space-y-0.5">
                <div className="break-all font-medium text-gray-900 dark:text-white">{row.model}</div>
                <div className="break-all surface-muted">
                  <span className="mr-0.5">↳</span>
                  {row.upstream_model}
                </div>
              </div>
            ) : (
              <span className="font-medium text-gray-900 dark:text-white">{row.model}</span>
            )}
            {modelMetadata && (
              <div data-testid="usage-model-metadata" className="usage-model-metadata pt-1 text-[11px] surface-muted">
                <span title={t('usage.reasoningEffort')}>
                  {formatReasoningEffort(row.reasoning_effort)}
                  {hasReasoningEffortMapping(row) && <> ↳ {formatReasoningEffort(row.upstream_reasoning_effort)}</>}
                </span>
                <span
                  className={`inline-flex items-center rounded border px-1.5 py-px text-[10px] leading-tight ${
                    row.request_fast_enabled === true
                      ? 'border-primary-200 bg-primary-50/40 text-primary-600 dark:border-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                      : 'border-gray-200 dark:border-dark-600 surface-muted'
                  }`}
                  title={t('usage.fastStateHint')}
                >
                  {t('usage.fastStateLabel')}:{' '}
                  {t(
                    row.request_fast_enabled === true
                      ? 'usage.fastEnabled'
                      : row.request_fast_enabled === false
                        ? 'usage.fastDisabled'
                        : 'usage.fastUnrecorded'
                  )}
                </span>
              </div>
            )}
            {(row.completion_status === 'client_disconnected' || row.is_complete === false) && (
              <span
                data-testid="usage-incomplete-badge"
                className="inline-flex w-fit items-center rounded bg-amber-50/[0.15] px-1.5 py-px text-[10px] font-medium text-amber-700 ring-1 ring-inset ring-amber-200 dark:bg-amber-500/10 dark:text-amber-300 dark:ring-amber-500/30"
                title={
                  row.completion_status === 'client_disconnected' ? t('usage.clientDisconnectedHint') : t('usage.incompleteHint')
                }
              >
                {row.completion_status === 'client_disconnected' ? t('usage.clientDisconnected') : t('usage.incomplete')}
              </span>
            )}
            {row.upstream_model_mismatch === true && row.upstream_response_model && (
              <div
                className={`break-all pl-3 text-[11px] ${
                  isLikelyModelVariant(row) ? 'text-amber-600 dark:text-amber-400' : 'text-orange-600 dark:text-orange-400'
                }`}
                title={modelAuditTitle(row)}
              >
                <span className="mr-1">↳ {t('usage.upstreamResponseModel')}:</span>
                {row.upstream_response_model}
                <span
                  className={`ml-1 inline-flex rounded px-1 py-px text-[10px] font-medium ring-1 ring-inset ${
                    isLikelyModelVariant(row)
                      ? 'bg-amber-50/[0.15] text-amber-700 ring-amber-200 dark:bg-amber-500/10 dark:text-amber-300 dark:ring-amber-500/30'
                      : 'bg-orange-50/[0.15] text-orange-700 ring-orange-200 dark:bg-orange-500/10 dark:text-orange-300 dark:ring-orange-500/30'
                  }`}
                >
                  {isLikelyModelVariant(row) ? t('usage.modelVariant') : t('usage.modelMismatch')}
                </span>
              </div>
            )}
          </div>
        )
      case 'endpoint':
        return (
          <div className="max-w-[320px] space-y-1 text-xs">
            <div className="break-all surface-muted">
              <span className="font-medium surface-muted">{t('usage.inbound')}:</span>
              <span className="ml-1">{row.inbound_endpoint?.trim() || '-'}</span>
            </div>
            {showUpstreamEndpoint && (
              <div className="break-all surface-muted">
                <span className="font-medium surface-muted">{t('usage.upstream')}:</span>
                <span className="ml-1">{row.upstream_endpoint?.trim() || '-'}</span>
              </div>
            )}
          </div>
        )
      case 'group':
        return row.group ? (
          <span className="inline-flex items-center rounded bg-indigo-100/[0.15] px-2 py-0.5 text-xs font-medium text-indigo-800 dark:bg-indigo-900/25 dark:text-indigo-200">
            {row.group.name}
          </span>
        ) : (
          <span className="text-sm surface-muted">-</span>
        )
      case 'stream':
        return (
          <div className="flex flex-wrap items-center gap-1">
            <span
              data-testid="request-type-badge"
              className={`inline-flex items-center rounded px-2 py-0.5 text-xs font-medium ${getRequestTypeBadgeClass(row)}`}
            >
              {getRequestTypeLabel(row)}
            </span>
            {row.native_compaction_v2 && (
              <span
                data-testid="native-compaction-badge"
                className="inline-flex items-center rounded bg-teal-100/[0.15] px-2 py-0.5 text-xs font-medium text-teal-800 dark:bg-teal-900/25 dark:text-teal-200"
              >
                {t('usage.nativeCompactionV2')}
              </span>
            )}
          </div>
        )
      case 'billing_mode':
        return (
          <span
            className={`inline-flex items-center rounded px-2 py-0.5 text-xs font-medium ${getBillingModeBadgeClass(
              getDisplayBillingMode(row)
            )}`}
          >
            {getBillingModeLabel(getDisplayBillingMode(row), t)}
          </span>
        )
      case 'tokens':
        if (isImageUsage(row)) {
          return (
            <div className="flex items-center gap-1.5">
              <svg className="h-4 w-4 text-indigo-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth="2"
                  d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z"
                />
              </svg>
              <span className="font-medium text-gray-900 dark:text-white">
                {row.image_count}
                {t('usage.imageUnit')}
              </span>
              <span className="surface-muted">({formatImageBillingSize(row, t)})</span>
            </div>
          )
        }
        return (
          <div className="flex items-center gap-1.5">
            <div className="space-y-1 text-sm">
              <div className="flex items-center gap-2">
                <div className="inline-flex items-center gap-1">
                  <Icon name="arrowDown" size="sm" className="h-3.5 w-3.5 text-emerald-500" />
                  <span className="font-medium text-gray-900 dark:text-white">{textInputTokens(row).toLocaleString()}</span>
                  {requestTokenShare(row, textInputTokens(row)) && (
                    <span className="text-[10px] surface-muted">{requestTokenShare(row, textInputTokens(row))}</span>
                  )}
                </div>
                <div className="inline-flex items-center gap-1">
                  <Icon name="arrowUp" size="sm" className="h-3.5 w-3.5 text-violet-500" />
                  <span className="font-medium text-gray-900 dark:text-white">{textOutputTokens(row).toLocaleString()}</span>
                  {requestTokenShare(row, textOutputTokens(row)) && (
                    <span className="text-[10px] surface-muted">{requestTokenShare(row, textOutputTokens(row))}</span>
                  )}
                </div>
              </div>
              {(row.cache_read_tokens > 0 || row.cache_creation_tokens > 0) && (
                <div className="flex items-center gap-2">
                  {row.cache_read_tokens > 0 && (
                    <div className="inline-flex items-center gap-1">
                      <svg className="h-3.5 w-3.5 text-sky-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path
                          strokeLinecap="round"
                          strokeLinejoin="round"
                          strokeWidth="2"
                          d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4"
                        />
                      </svg>
                      <span className="font-medium text-sky-600 dark:text-sky-400">{formatCacheTokens(row.cache_read_tokens)}</span>
                      {requestTokenShare(row, row.cache_read_tokens) && (
                        <span className="text-[10px] surface-muted">{requestTokenShare(row, row.cache_read_tokens)}</span>
                      )}
                    </div>
                  )}
                  {row.cache_creation_tokens > 0 && (
                    <div className="inline-flex items-center gap-1">
                      <svg className="h-3.5 w-3.5 text-amber-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path
                          strokeLinecap="round"
                          strokeLinejoin="round"
                          strokeWidth="2"
                          d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"
                        />
                      </svg>
                      <span className="font-medium text-amber-600 dark:text-amber-400">
                        {formatCacheTokens(row.cache_creation_tokens)}
                      </span>
                      {requestTokenShare(row, row.cache_creation_tokens) && (
                        <span className="text-[10px] surface-muted">{requestTokenShare(row, row.cache_creation_tokens)}</span>
                      )}
                      {row.cache_creation_1h_tokens > 0 && (
                        <span className="inline-flex items-center rounded bg-orange-100/[0.15] px-1 py-px text-[10px] font-medium leading-tight text-orange-600 ring-1 ring-inset ring-orange-200 dark:bg-orange-500/20 dark:text-orange-400 dark:ring-orange-500/30">
                          1h
                        </span>
                      )}
                      {row.cache_ttl_overridden && (
                        <span
                          className="inline-flex cursor-help items-center rounded bg-rose-100/[0.15] px-1 py-px text-[10px] font-medium leading-tight text-rose-600 ring-1 ring-inset ring-rose-200 dark:bg-rose-500/20 dark:text-rose-400 dark:ring-rose-500/30"
                          title={t('usage.cacheTtlOverriddenHint')}
                        >
                          R
                        </span>
                      )}
                    </div>
                  )}
                </div>
              )}
              {hasImageInputTokens(row) && (
                <div className="flex items-center gap-2">
                  <div className="inline-flex items-center gap-1">
                    <svg className="h-3.5 w-3.5 text-fuchsia-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        strokeWidth="2"
                        d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z"
                      />
                    </svg>
                    <span className="font-medium text-fuchsia-600 dark:text-fuchsia-400">
                      {row.image_input_tokens.toLocaleString()}
                    </span>
                    {requestTokenShare(row, row.image_input_tokens) && (
                      <span className="text-[10px] surface-muted">{requestTokenShare(row, row.image_input_tokens)}</span>
                    )}
                  </div>
                </div>
              )}
              {hasImageOutputTokens(row) && (
                <div className="flex items-center gap-2">
                  <div className="inline-flex items-center gap-1">
                    <svg className="h-3.5 w-3.5 text-pink-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        strokeWidth="2"
                        d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z"
                      />
                    </svg>
                    <span className="font-medium text-pink-600 dark:text-pink-400">
                      {row.image_output_tokens.toLocaleString()}
                    </span>
                    {requestTokenShare(row, row.image_output_tokens) && (
                      <span className="text-[10px] surface-muted">{requestTokenShare(row, row.image_output_tokens)}</span>
                    )}
                  </div>
                </div>
              )}
            </div>
            <div className="group relative" title={tokenDetailText(row, t)}>
              <div className="flex h-4 w-4 cursor-help items-center justify-center rounded-full bg-gray-100 transition-colors group-hover:bg-blue-100/25 dark:bg-gray-700 dark:group-hover:bg-blue-900/50">
                <Icon name="infoCircle" size="xs" className="text-gray-400 group-hover:text-blue-500 dark:text-gray-500 dark:group-hover:text-blue-400" />
              </div>
            </div>
          </div>
        )
      case 'cost':
        return (
          <div className="text-sm">
            <div className="flex items-center gap-1.5">
              <span className="font-medium text-green-600 dark:text-green-400">{usageMoney(row.actual_cost)}</span>
              {row.long_context_billing_applied && (
                <span
                  data-testid="long-context-billing-marker"
                  className="inline-flex items-center rounded bg-amber-100/[0.15] px-1 py-px text-[10px] font-semibold leading-tight text-amber-700 ring-1 ring-inset ring-amber-200 dark:bg-amber-500/20 dark:text-amber-300 dark:ring-amber-500/30"
                  title={t('admin.usage.longContextPricingTooltip')}
                >
                  {t('admin.usage.longContext')}
                </span>
              )}
              <div className="group relative" title={costDetailText(row, t)}>
                <div className="flex h-4 w-4 cursor-help items-center justify-center rounded-full bg-gray-100 transition-colors group-hover:bg-blue-100/25 dark:bg-gray-700 dark:group-hover:bg-blue-900/50">
                  <Icon name="infoCircle" size="xs" className="text-gray-400 group-hover:text-blue-500 dark:text-gray-500 dark:group-hover:text-blue-400" />
                </div>
              </div>
            </div>
            {showAccountBilling && row.account_rate_multiplier != null && (
              <div className="mt-0.5 text-[11px] text-orange-500 dark:text-orange-400">
                A ${((row.account_stats_cost ?? row.total_cost ?? 0) * (row.account_rate_multiplier ?? 1)).toFixed(6)}
              </div>
            )}
          </div>
        )
      case 'latency':
        return (
          <div className="flex items-stretch gap-2">
            <span className={`w-1 shrink-0 rounded-full ${latencyBarClasses(row)}`} aria-hidden="true" />
            <div className="grid grid-cols-[max-content_max-content] items-baseline gap-x-2 gap-y-0.5 text-xs">
              <span className="surface-muted inline-flex items-center gap-1">
                {t('usage.latencyFirstByte')}
                {hasLatencyDetails(row) && (
                  <span
                    className="group relative"
                    tabIndex={0}
                    role="button"
                    aria-label={t('usage.latencyDetails')}
                    title={latencyDetailText(row, t)}
                  >
                    <div className="flex h-3.5 w-3.5 cursor-help items-center justify-center rounded-full surface-inset transition-colors group-hover:bg-blue-100/25 dark:group-hover:bg-blue-900/50">
                      <Icon name="infoCircle" size="xs" className="text-gray-400 group-hover:text-blue-500 dark:text-gray-500 dark:group-hover:text-blue-400" />
                    </div>
                  </span>
                )}
              </span>
              {firstByteMs(row) != null ? (
                <span className={`font-medium tabular-nums ${primaryFirstByteTextClass(row)}`}>
                  {formatDuration(firstByteMs(row) as number)}
                </span>
              ) : (
                <span className="surface-muted">-</span>
              )}
              <span className="surface-muted">{t('usage.latencyDuration')}</span>
              <span className={`font-medium tabular-nums ${durationTextClass(row)}`}>{formatDuration(row.duration_ms)}</span>
              <span className="surface-muted cursor-help">{t('usage.latencyTps')}</span>
              <span
                className={`whitespace-nowrap font-medium tabular-nums ${
                  averageTps(row) == null ? 'surface-muted' : 'text-cyan-600 dark:text-cyan-400'
                }`}
              >
                {averageTps(row) == null ? '-' : formatTpsNumber(averageTps(row) as number)}
              </span>
            </div>
          </div>
        )
      case 'created_at':
        return <span className="text-sm surface-muted">{formatDateTime(value as string)}</span>
      case 'user_agent':
        return row.user_agent ? (
          <div className="flex max-w-[320px] items-center gap-1.5">
            <span className="truncate text-sm surface-muted" title={row.user_agent}>
              {formatUserAgent(row.user_agent)}
            </span>
            <button
              type="button"
              className="surface-control surface-muted shrink-0 rounded p-0.5 transition-colors hover:surface-hover hover:text-gray-600 dark:hover:text-gray-300"
              title={t('keys.copyToClipboard')}
              onClick={() => void copyToClipboard(row.user_agent as string)}
            >
              <Icon name="copy" size="sm" className="h-3.5 w-3.5" />
            </button>
          </div>
        ) : (
          <span className="text-sm surface-muted">-</span>
        )
      case 'ip_address':
        return row.ip_address ? (
          <div>
            <span className="font-mono text-sm surface-muted">{row.ip_address}</span>
            <IpGeoCell ip={row.ip_address} />
          </div>
        ) : (
          <span className="text-sm surface-muted">-</span>
        )
      default:
        return value === null || value === undefined ? '' : String(value)
    }
  }

  return (
    <div
      className={`card overflow-hidden ${workspace ? 'workspace-table-card workspace-shell' : ''}`}
    >
      {showIpGeoToolbar && (
        <div
          className={`flex items-center justify-end gap-2 border-b border-gray-200 px-4 py-2 dark:border-dark-700 ${
            workspace ? 'workspace-fixed' : ''
          }`}
        >
          {pendingIpCount > 0 && (
            <span className="text-xs surface-muted">{t('usage.ipGeo.pending', { count: pendingIpCount })}</span>
          )}
          <button
            type="button"
            className="surface-control inline-flex items-center gap-1 rounded px-2 py-1 text-xs font-medium text-primary-600 transition-colors hover:bg-primary-50/25 disabled:cursor-not-allowed disabled:opacity-50 dark:text-primary-400 dark:hover:bg-primary-900/30"
            disabled={ipGeoBatchLoading || pendingIpCount === 0}
            onClick={() => void handleBatchFetchIpGeo()}
          >
            {ipGeoBatchLoading ? t('usage.ipGeo.batchFetching') : t('usage.ipGeo.batchFetch')}
          </button>
        </div>
      )}
      <div className={`min-w-0 ${workspace ? 'workspace-body !gap-0' : ''}`}>
        <DataTable
          className={workspace ? 'workspace-table-viewport' : undefined}
          columns={columns}
          data={data}
          loading={loading}
          serverSideSort={serverSideSort}
          defaultSortKey={defaultSortKey}
          defaultSortOrder={defaultSortOrder}
          onSort={onSort}
          renderCell={(key, row, cellValue) => renderCell(key, row as UsageTableRow, cellValue)}
          renderEmpty={() => <EmptyState title={t('usage.noRecords')} />}
        />
      </div>
    </div>
  )
}

export default UsageTable
