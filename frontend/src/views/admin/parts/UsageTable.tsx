import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/components/DataTable'
import { IpGeoCell } from '@/components/IpGeoCell'
import { EmptyState } from '@/components/ui'
import { Icon } from '@/components/Icon'
import { formatDateTime, formatReasoningEffort, reasoningEffortValuesEqual } from '@/utils/format'
import { formatCacheTokens, formatMultiplier } from '@/utils/formatters'
import { formatTokenShare, sumTokenBuckets } from '@/utils/tokenShare'
import { resolveUsageRequestType } from '@/utils/usageRequestType'
import {
  LATENCY_BAR_CLASSES,
  LATENCY_BAR_FROM_CLASSES,
  LATENCY_BAR_TO_CLASSES,
  LATENCY_TEXT_CLASSES,
  durationSeverity,
  firstByteSeverity
} from '@/utils/latencyHealth'
import {
  BILLING_MODE_TOKEN,
  getBillingModeBadgeClass,
  getBillingModeLabel,
  getDisplayBillingMode,
  imageUnitPrice,
  isImageUsage
} from '@/utils/billingMode'
import {
  formatImageBillingSize,
  formatImageInputSize,
  formatImageOutputSize,
  formatImageSizeBreakdown,
  formatImageSizeSource,
  hasImageInputCost,
  hasImageInputTokens,
  hasImageOutputCost,
  hasImageOutputTokens,
  textInputTokens,
  textOutputTokens
} from '@/utils/imageUsage'
import { formatRequestedServiceTier, formatResponseServiceTier } from '@/utils/usageServiceTier'
import {
  averageTps,
  firstByteMs,
  firstTokenUnavailableReason,
  formatTpsNumber,
  strictFirstTokenMs,
  tpsNote
} from '@/utils/usageTiming'
import { usageMoney } from '@/utils/usageInspection'
import { formatTokenPricePerMillion } from '@/utils/usagePricing'
import { fetchBatch, getEntry } from '@/utils/ipGeoLookup'
import { cn } from '@/lib/utils'
import type { AdminUsageLog } from '@/types'
import { useAppStore } from '@/stores/app'

export interface AdminUsageTableProps {
  data: AdminUsageLog[]
  loading?: boolean
  columns: Column[]
  serverSideSort?: boolean
  defaultSortKey?: string
  defaultSortOrder?: 'asc' | 'desc'
  showServiceTier?: boolean
  showAccountBilling?: boolean
  showUpstreamEndpoint?: boolean
  /** Keep request metadata beneath the requested model. */
  modelMetadata?: boolean
  workspace?: boolean
  onSort?: (key: string, order: 'asc' | 'desc') => void
  onUserClick?: (userID: number, email?: string) => void
  onIpGeoBatchFailed?: () => void
}

/** Compute the account-billed cost for display: (account_stats_cost ?? total_cost) * rate_multiplier */
function accountBilled(row: {
  total_cost?: number | null
  account_stats_cost?: number | null
  account_rate_multiplier?: number | null
}): number {
  const base = row.account_stats_cost != null ? row.account_stats_cost : (row.total_cost ?? 0)
  const result = base * (row.account_rate_multiplier ?? 1)
  return Number.isNaN(result) ? 0 : result
}

function requestTokenTotal(
  row: Pick<AdminUsageLog, 'input_tokens' | 'output_tokens' | 'cache_creation_tokens' | 'cache_read_tokens'>
): number {
  return sumTokenBuckets(row.input_tokens, row.output_tokens, row.cache_creation_tokens, row.cache_read_tokens)
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

export function AdminUsageTable({
  data,
  loading = false,
  columns,
  serverSideSort = false,
  defaultSortKey,
  defaultSortOrder,
  showServiceTier = false,
  showAccountBilling = true,
  showUpstreamEndpoint = true,
  modelMetadata = false,
  workspace = false,
  onSort,
  onUserClick,
  onIpGeoBatchFailed
}: AdminUsageTableProps) {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)
  const [ipGeoBatchLoading, setIpGeoBatchLoading] = useState(false)
  const [copiedRequestId, setCopiedRequestId] = useState<string | null>(null)
  const [copiedUserAgent, setCopiedUserAgent] = useState<string | null>(null)

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

  const copyRequestId = async (requestId: string) => {
    try {
      await navigator.clipboard.writeText(requestId)
      setCopiedRequestId(requestId)
      showSuccess(t('admin.usage.requestIdCopied'))
      window.setTimeout(() => {
        setCopiedRequestId((current) => (current === requestId ? null : current))
      }, 2000)
    } catch {
      showError(t('common.copyFailed'))
    }
  }

  const copyUserAgent = async (userAgent: string) => {
    try {
      await navigator.clipboard.writeText(userAgent)
      setCopiedUserAgent(userAgent)
      showSuccess(t('admin.usage.userAgentCopied'))
      window.setTimeout(() => {
        setCopiedUserAgent((current) => (current === userAgent ? null : current))
      }, 2000)
    } catch {
      showError(t('common.copyFailed'))
    }
  }

  const hasReasoningEffortMapping = (row: AdminUsageLog): boolean => {
    const requested = row.reasoning_effort?.trim() || ''
    const forwarded = row.upstream_reasoning_effort?.trim() || ''
    return requested !== '' && forwarded !== '' && !reasoningEffortValuesEqual(requested, forwarded)
  }

  const sentUpstreamModel = (row: AdminUsageLog): string => row.upstream_model?.trim() || row.model?.trim() || ''

  const normalizeModelVariant = (model: string): string =>
    model
      .trim()
      .toLowerCase()
      .replace(/-latest$/, '')
      .replace(/-\d{4}-\d{2}-\d{2}$/, '')
      .replace(/-\d{8}$/, '')

  const isLikelyModelVariant = (row: AdminUsageLog): boolean => {
    const sent = sentUpstreamModel(row)
    const response = row.upstream_response_model?.trim() || ''
    return sent !== '' && response !== '' && normalizeModelVariant(sent) === normalizeModelVariant(response)
  }

  const modelAuditTitle = (row: AdminUsageLog): string =>
    [
      `${t('usage.requestedModel')}: ${row.model || '-'}`,
      `${t('usage.sentUpstreamModel')}: ${sentUpstreamModel(row) || '-'}`,
      `${t('usage.upstreamResponseModel')}: ${row.upstream_response_model || '-'}`
    ].join('\n')

  const getRequestTypeLabel = (row: AdminUsageLog): string => {
    const requestType = resolveUsageRequestType(row)
    if (requestType === 'cyber') return t('usage.cyber')
    if (requestType === 'live') return t('usage.live')
    if (requestType === 'ws_v2') return t('usage.ws')
    if (requestType === 'stream') return t('usage.stream')
    if (requestType === 'sync') return t('usage.sync')
    return t('usage.unknown')
  }

  const getRequestTypeBadgeClass = (row: AdminUsageLog): string => {
    const requestType = resolveUsageRequestType(row)
    if (requestType === 'cyber') return 'bg-red-100/[0.15] text-red-800 dark:bg-red-900/25 dark:text-red-200'
    if (requestType === 'live') return 'bg-emerald-100/[0.15] text-emerald-800 dark:bg-emerald-900/25 dark:text-emerald-200'
    if (requestType === 'ws_v2') return 'bg-violet-100/[0.15] text-violet-800 dark:bg-violet-900/25 dark:text-violet-200'
    if (requestType === 'stream') return 'bg-blue-100/[0.15] text-blue-800 dark:bg-blue-900/25 dark:text-blue-200'
    if (requestType === 'sync') return 'surface-inset text-gray-800 dark:surface-inset dark:text-gray-200'
    return 'bg-amber-100/[0.15] text-amber-800 dark:bg-amber-900/25 dark:text-amber-200'
  }

  const requestTokenShare = (
    row: Pick<AdminUsageLog, 'input_tokens' | 'output_tokens' | 'cache_creation_tokens' | 'cache_read_tokens'>,
    tokens: number | null | undefined
  ): string | null => formatTokenShare(tokens, requestTokenTotal(row))

  const primaryFirstByteTextClass = (row: AdminUsageLog): string => {
    const first = firstByteMs(row)
    return first == null ? 'surface-muted' : LATENCY_TEXT_CLASSES[firstByteSeverity(first)]
  }

  const durationTextClass = (row: AdminUsageLog): string =>
    row.duration_ms == null ? 'surface-muted' : LATENCY_TEXT_CLASSES[durationSeverity(row.duration_ms)]

  const hasLatencyDetails = (row: AdminUsageLog): boolean =>
    row.first_byte_ms != null ||
    row.first_token_ms != null ||
    row.last_token_ms != null ||
    row.duration_ms != null ||
    row.first_output_ms != null ||
    !!row.first_output_kind ||
    !!firstTokenUnavailableReason(row) ||
    !!tpsNote(row)

  const latencyBarClasses = (row: AdminUsageLog): string => {
    if (row.duration_ms == null) return 'bg-gray-300 dark:bg-gray-600'
    const first = firstByteMs(row)
    if (first != null) {
      return cn(
        'bg-gradient-to-b from-40% to-60%',
        LATENCY_BAR_FROM_CLASSES[firstByteSeverity(first)],
        LATENCY_BAR_TO_CLASSES[durationSeverity(row.duration_ms)]
      )
    }
    return LATENCY_BAR_CLASSES[durationSeverity(row.duration_ms)]
  }

  const formatTpsDisplay = (value: number | null): string =>
    value == null ? '-' : `${formatTpsNumber(value)} tok/s`

  const firstOutputModalityLabel = (
    row: AdminUsageLog | null | undefined,
    role: 'kind' | 'timing'
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

  const latencyTooltipNote = (row: AdminUsageLog | null | undefined): string | null => {
    if (row == null) return null
    const reason = firstTokenUnavailableReason(row)
    if (reason) return t(reason)
    if (row.first_output_kind === 'image' || row.first_output_kind === 'audio') {
      if (row.first_token_ms == null) return t('usage.latencyMediaOnlyHint')
      if (row.first_output_ms != null && row.first_output_ms !== row.first_token_ms) {
        return t('usage.latencyMixedModalityHint')
      }
    }
    if (
      (row.first_output_kind === 'tool' || row.first_output_kind === 'reasoning') &&
      row.first_token_ms != null
    ) {
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

  const tokenDetailText = (row: AdminUsageLog): string => {
    const lines: string[] = [t('usage.tokenDetails')]
    if (row.input_tokens > 0 && !hasImageInputTokens(row)) {
      lines.push(
        `${t('admin.usage.inputTokens')}: ${row.input_tokens.toLocaleString()}${
          requestTokenShare(row, row.input_tokens) ? ` (${requestTokenShare(row, row.input_tokens)})` : ''
        }`
      )
    }
    if (hasImageInputTokens(row) && textInputTokens(row) > 0) {
      lines.push(
        `${t('admin.usage.inputTokens')}: ${textInputTokens(row).toLocaleString()}${
          requestTokenShare(row, textInputTokens(row)) ? ` (${requestTokenShare(row, textInputTokens(row))})` : ''
        }`
      )
    }
    if (hasImageInputTokens(row)) {
      lines.push(
        `${t('usage.imageInputTokens')}: ${row.image_input_tokens.toLocaleString()}${
          requestTokenShare(row, row.image_input_tokens) ? ` (${requestTokenShare(row, row.image_input_tokens)})` : ''
        }`
      )
    }
    if (row.output_tokens > 0 && !hasImageOutputTokens(row)) {
      lines.push(
        `${t('admin.usage.outputTokens')}: ${row.output_tokens.toLocaleString()}${
          requestTokenShare(row, row.output_tokens) ? ` (${requestTokenShare(row, row.output_tokens)})` : ''
        }`
      )
    }
    if (hasImageOutputTokens(row) && textOutputTokens(row) > 0) {
      lines.push(
        `${t('admin.usage.outputTokens')}: ${textOutputTokens(row).toLocaleString()}${
          requestTokenShare(row, textOutputTokens(row)) ? ` (${requestTokenShare(row, textOutputTokens(row))})` : ''
        }`
      )
    }
    if (hasImageOutputTokens(row)) {
      lines.push(
        `${t('usage.imageOutputTokens')}: ${row.image_output_tokens.toLocaleString()}${
          requestTokenShare(row, row.image_output_tokens) ? ` (${requestTokenShare(row, row.image_output_tokens)})` : ''
        }`
      )
    }
    if (row.cache_creation_tokens > 0) {
      // 有 5m/1h 明细时，展开显示
      if (row.cache_creation_5m_tokens > 0 || row.cache_creation_1h_tokens > 0) {
        if (row.cache_creation_5m_tokens > 0) {
          lines.push(
            `${t('admin.usage.cacheCreation5mTokens')}: ${row.cache_creation_5m_tokens.toLocaleString()}${formatTokenShare(
              row.cache_creation_5m_tokens,
              row.cache_creation_tokens
            )}`
          )
        }
        if (row.cache_creation_1h_tokens > 0) {
          lines.push(
            `${t('admin.usage.cacheCreation1hTokens')}: ${row.cache_creation_1h_tokens.toLocaleString()}${formatTokenShare(
              row.cache_creation_1h_tokens,
              row.cache_creation_tokens
            )}`
          )
        }
      } else {
        lines.push(
          `${t('admin.usage.cacheCreationTokens')}: ${row.cache_creation_tokens.toLocaleString()}${
            requestTokenShare(row, row.cache_creation_tokens) ? ` (${requestTokenShare(row, row.cache_creation_tokens)})` : ''
          }`
        )
      }
    }
    if (row.cache_ttl_overridden) {
      lines.push(
        `${t('usage.cacheTtlOverriddenLabel')}: ${
          row.cache_creation_1h_tokens > 0 ? t('usage.cacheTtlOverridden1h') : t('usage.cacheTtlOverridden5m')
        }`
      )
    }
    if (row.cache_read_tokens > 0) {
      lines.push(
        `${t('admin.usage.cacheReadTokens')}: ${row.cache_read_tokens.toLocaleString()}${
          requestTokenShare(row, row.cache_read_tokens) ? ` (${requestTokenShare(row, row.cache_read_tokens)})` : ''
        }`
      )
    }
    lines.push(`${t('usage.totalTokens')}: ${requestTokenTotal(row).toLocaleString()}`)
    return lines.join('\n')
  }

  const costDetailText = (row: AdminUsageLog): string => {
    const lines: string[] = [t('usage.costDetails')]
    if (row.input_cost > 0) lines.push(`${t('admin.usage.inputCost')}: $${row.input_cost.toFixed(8)}`)
    if (hasImageInputCost(row)) lines.push(`${t('usage.imageInputCost')}: $${row.image_input_cost.toFixed(8)}`)
    if (row.output_cost > 0) lines.push(`${t('admin.usage.outputCost')}: $${row.output_cost.toFixed(8)}`)
    if (hasImageOutputCost(row)) {
      lines.push(`${t('usage.imageOutputCost')}: $${row.image_output_cost.toFixed(8)}`)
    }
    // Token billing: show unit prices per 1M tokens
    if (!isImageUsage(row) && (!row.billing_mode || row.billing_mode === BILLING_MODE_TOKEN)) {
      if (textInputTokens(row) > 0) {
        lines.push(
          `${t('usage.inputTokenPrice')}: ${formatTokenPricePerMillion(row.input_cost, textInputTokens(row))} ${t(
            'usage.perMillionTokens'
          )}`
        )
      }
      if (hasImageInputTokens(row)) {
        lines.push(
          `${t('usage.imageInputTokenPrice')}: ${formatTokenPricePerMillion(
            row.image_input_cost ?? 0,
            row.image_input_tokens
          )} ${t('usage.perMillionTokens')}`
        )
      }
      if (row.output_cost > 0 && textOutputTokens(row) > 0) {
        lines.push(
          `${t('usage.outputTokenPrice')}: ${formatTokenPricePerMillion(row.output_cost, textOutputTokens(row))} ${t(
            'usage.perMillionTokens'
          )}`
        )
      }
      if (hasImageOutputTokens(row)) {
        lines.push(
          `${t('usage.imageOutputTokenPrice')}: ${formatTokenPricePerMillion(
            row.image_output_cost ?? 0,
            row.image_output_tokens
          )} ${t('usage.perMillionTokens')}`
        )
      }
    } else if (isImageUsage(row)) {
      lines.push(`${t('usage.imageCount')}: ${row.image_count}${t('usage.imageUnit')}`)
      lines.push(`${t('usage.imageBillingSize')}: ${formatImageBillingSize(row, t)}`)
      lines.push(`${t('usage.imageSizeSource')}: ${formatImageSizeSource(row, t)}`)
      lines.push(`${t('usage.imageInputSize')}: ${formatImageInputSize(row, t)}`)
      lines.push(`${t('usage.imageOutputSize')}: ${formatImageOutputSize(row, t)}`)
      if (formatImageSizeBreakdown(row)) {
        lines.push(`${t('usage.imageSizeBreakdown')}: ${formatImageSizeBreakdown(row)}`)
      }
      lines.push(`${t('usage.imageUnitPrice')}: $${imageUnitPrice(row).toFixed(8)}`)
      lines.push(`${t('usage.imageTotalPrice')}: $${row.total_cost?.toFixed(8) || '0.00000000'}`)
    } else {
      lines.push(`${t('usage.unitPrice')}: $${row.total_cost?.toFixed(8) || '0.00000000'}`)
    }
    if (row.cache_creation_cost > 0) {
      lines.push(`${t('admin.usage.cacheCreationCost')}: $${row.cache_creation_cost.toFixed(8)}`)
    }
    if (row.cache_read_cost > 0) {
      lines.push(`${t('admin.usage.cacheReadCost')}: $${row.cache_read_cost.toFixed(8)}`)
    }
    lines.push(`${t('usage.rate')}: ${formatMultiplier(row.rate_multiplier ?? 1)}x`)
    lines.push(`${t('usage.original')}: $${row.total_cost?.toFixed(8) || '0.00000000'}`)
    lines.push(`${t('usage.userBilled')}: $${row.actual_cost?.toFixed(8) || '0.00000000'}`)
    if (showAccountBilling) {
      lines.push(`${t('usage.accountMultiplier')}: ${formatMultiplier(row.account_rate_multiplier ?? 1)}x`)
      lines.push(
        `${t('usage.accountBilled')}: $${accountBilled({
          total_cost: row.total_cost,
          account_stats_cost: row.account_stats_cost,
          account_rate_multiplier: row.account_rate_multiplier
        }).toFixed(8)}`
      )
    }
    return lines.join('\n')
  }

  const latencyDetailText = (row: AdminUsageLog): string => {
    const lines: string[] = [t('usage.latencyDetails')]
    lines.push(`${t('usage.latencyFirstByte')}: ${formatDuration(firstByteMs(row))}`)
    if (firstByteMs(row) == null) lines.push(t('usage.latencyFirstByteUnavailable'))
    if (row.first_output_kind) {
      lines.push(`${t('usage.latencyFirstOutputKind')}: ${firstOutputModalityLabel(row, 'kind')}`)
    }
    if (row.first_output_ms != null) {
      lines.push(`${firstOutputModalityLabel(row, 'timing')}: ${formatDuration(row.first_output_ms)}`)
    }
    lines.push(`${t('usage.latencyFirstToken')}: ${formatDuration(strictFirstTokenMs(row))}`)
    lines.push(
      `${t('usage.latencyLastToken')}: ${
        strictFirstTokenMs(row) == null ? '-' : formatDuration(row.last_token_ms)
      }`
    )
    if (row.duration_ms != null) lines.push(`${t('usage.latencyDuration')}: ${formatDuration(row.duration_ms)}`)
    if (averageTps(row) != null) {
      lines.push(`${t('usage.latencyTps')}: ${formatTpsDisplay(averageTps(row))}`)
    }
    const note = tpsNote(row)
    if (note) lines.push(t(note))
    const tooltipNote = latencyTooltipNote(row)
    if (tooltipNote) lines.push(tooltipNote)
    return lines.join('\n')
  }

  const renderCell = useCallback(
    (key: string, row: AdminUsageLog, value: unknown): React.ReactNode => {
      switch (key) {
        case 'user':
          return (
            <div className="text-sm">
              {row.user?.email ? (
                <button
                  className="font-medium text-primary-600 underline decoration-dashed underline-offset-2 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300 surface-control"
                  onClick={() => onUserClick?.(row.user_id, row.user?.email)}
                  title={t('admin.usage.clickToViewBalance')}
                >
                  {row.user.email}
                </button>
              ) : (
                <span className="font-medium text-gray-900 dark:text-white">-</span>
              )}
              {row.user?.deleted_at && (
                <span className="ml-1 inline-flex items-center rounded bg-rose-100/[0.15] px-1 py-px text-[10px] font-medium leading-tight text-rose-600 ring-1 ring-inset ring-rose-200 dark:bg-rose-500/20 dark:text-rose-400 dark:ring-rose-500/30">
                  {t('admin.usage.userDeletedBadge')}
                </span>
              )}
              <span className="ml-1 surface-muted">#{row.user_id}</span>
            </div>
          )
        case 'api_key':
          return <span className="text-sm text-gray-900 dark:text-white">{row.api_key?.name || '-'}</span>
        case 'account':
          return <span className="text-sm text-gray-900 dark:text-white">{row.account?.name || '-'}</span>
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
                    {hasReasoningEffortMapping(row) && (
                      <> ↳ {formatReasoningEffort(row.upstream_reasoning_effort)}</>
                    )}
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
                    row.completion_status === 'client_disconnected'
                      ? t('usage.clientDisconnectedHint')
                      : t('usage.incompleteHint')
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
        case 'service_tier':
          if (!showServiceTier) return null
          return (
            <div data-testid="service-tier-cell" className="space-y-0.5 whitespace-nowrap text-xs">
              <div className="text-gray-900 dark:text-white">
                {t('admin.usage.serviceTierRequest')} {formatRequestedServiceTier(row.requested_service_tier, t)}
              </div>
              <div className="surface-muted">
                {t('admin.usage.serviceTierResponse')} {formatResponseServiceTier(row.response_service_tier, t)}
              </div>
              <div className="surface-muted">
                {t('admin.usage.serviceTierBilling')} {formatRequestedServiceTier(row.service_tier, t)}
              </div>
              {row.requested_service_tier === 'priority' && row.response_service_tier === 'default' && (
                <div data-testid="service-tier-difference" className="text-amber-700 dark:text-amber-300">
                  {t('admin.usage.serviceTierDifference')}
                </div>
              )}
            </div>
          )
        case 'outbound':
          return (
            <div data-testid="outbound-cell" className="max-w-[320px] space-y-0.5 text-xs">
              <div className="font-medium">{t('admin.usage.inboundEvidence')}</div>
              <div className="truncate surface-muted" title={row.user_agent || undefined}>
                UA: {row.user_agent || '—'}
              </div>
              {row.inbound_endpoint && <div className="break-all surface-muted">{row.inbound_endpoint}</div>}
              <div className="pt-1 font-medium">{t('admin.usage.outboundEvidence')}</div>
              <div className="font-medium text-gray-900 dark:text-white">
                {t(`admin.usage.outboundTransport.${row.outbound_transport || 'unknown'}`)}
              </div>
              {row.outbound_metadata && (
                <>
                  <div className="surface-muted">
                    {t(
                      row.outbound_metadata.evidence === 'cli_process'
                        ? 'admin.usage.outboundProcess'
                        : 'admin.usage.outboundDispatch'
                    )}
                  </div>
                  {row.outbound_metadata.cli_version && <div>CLI {row.outbound_metadata.cli_version}</div>}
                  {row.outbound_metadata.user_agent && (
                    <div className="truncate surface-muted" title={row.outbound_metadata.user_agent}>
                      UA: {row.outbound_metadata.user_agent}
                    </div>
                  )}
                  {row.outbound_metadata.originator && (
                    <div className="surface-muted">Originator: {row.outbound_metadata.originator}</div>
                  )}
                  {row.outbound_metadata.client_version && (
                    <div className="surface-muted">Version: {row.outbound_metadata.client_version}</div>
                  )}
                  {row.outbound_metadata.tier_known && (
                    <div className="surface-muted">
                      {t('admin.usage.outboundTier')}{' '}
                      {formatRequestedServiceTier(row.outbound_metadata.service_tier || '', t)}
                    </div>
                  )}
                </>
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
              {/* Token Detail Tooltip */}
              <div className="group relative" title={tokenDetailText(row)}>
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
                {/* Cost Detail Tooltip */}
                <div className="group relative" title={costDetailText(row)} data-testid="cost-details-trigger">
                  <div className="flex h-4 w-4 cursor-help items-center justify-center rounded-full bg-gray-100 transition-colors group-hover:bg-blue-100/25 dark:bg-gray-700 dark:group-hover:bg-blue-900/50">
                    <Icon name="infoCircle" size="xs" className="text-gray-400 group-hover:text-blue-500 dark:text-gray-500 dark:group-hover:text-blue-400" />
                  </div>
                </div>
              </div>
              {showAccountBilling && row.account_rate_multiplier != null && (
                <div className="mt-0.5 text-[11px] text-orange-500 dark:text-orange-400">
                  A ${accountBilled(row).toFixed(6)}
                </div>
              )}
            </div>
          )
        // 主列固定：首字/总耗时/TPS；首输出模态与时间细节放入 ⓘ hover
        case 'latency':
          return (
            <div className="flex items-stretch gap-2">
              <span data-testid="latency-bar" className={`w-1 shrink-0 rounded-full ${latencyBarClasses(row)}`} aria-hidden="true" />
              <div className="grid grid-cols-[max-content_max-content] items-baseline gap-x-2 gap-y-0.5 text-xs">
                <span className="inline-flex items-center gap-1 surface-muted">
                  {t('usage.latencyFirstByte')}
                  {hasLatencyDetails(row) && (
                    <span
                      className="group relative"
                      tabIndex={0}
                      role="button"
                      aria-label={t('usage.latencyDetails')}
                      data-testid="latency-details-trigger"
                      title={latencyDetailText(row)}
                    >
                      <div className="flex h-3.5 w-3.5 cursor-help items-center justify-center rounded-full surface-inset transition-colors group-hover:bg-blue-100/25 dark:group-hover:bg-blue-900/50">
                        <Icon name="infoCircle" size="xs" className="text-gray-400 group-hover:text-blue-500 dark:text-gray-500 dark:group-hover:text-blue-400" />
                      </div>
                    </span>
                  )}
                </span>
                {firstByteMs(row) != null ? (
                  <span
                    data-testid="first-latency-value"
                    className={`font-medium tabular-nums ${primaryFirstByteTextClass(row)}`}
                  >
                    {formatDuration(firstByteMs(row))}
                  </span>
                ) : (
                  <span data-testid="first-latency-value" className="surface-muted">-</span>
                )}
                <span className="surface-muted">{t('usage.latencyDuration')}</span>
                <span data-testid="latency-duration" className={`font-medium tabular-nums ${durationTextClass(row)}`}>
                  {formatDuration(row.duration_ms)}
                </span>
                <span className="cursor-help surface-muted">{t('usage.latencyTps')}</span>
                <span
                  data-testid="latency-tps"
                  className={`cursor-help whitespace-nowrap font-medium tabular-nums ${
                    averageTps(row) == null ? 'surface-muted' : 'text-cyan-600 dark:text-cyan-400'
                  }`}
                >
                  {formatTpsDisplay(averageTps(row))}
                </span>
              </div>
            </div>
          )
        case 'created_at':
          return <span className="text-sm surface-muted">{formatDateTime(value as string)}</span>
        case 'request_id':
          return row.request_id ? (
            <div className="flex max-w-[160px] items-center gap-1.5">
              <span className="truncate font-mono text-xs surface-muted" title={row.request_id}>
                {row.request_id}
              </span>
              <button
                type="button"
                className={`shrink-0 rounded p-0.5 transition-colors hover:surface-hover hover:text-gray-600 dark:hover:text-gray-300 surface-control surface-muted ${
                  copiedRequestId === row.request_id ? 'text-green-500 hover:text-green-500' : ''
                }`}
                title={copiedRequestId === row.request_id ? t('keys.copied') : t('keys.copyToClipboard')}
                onClick={() => void copyRequestId(row.request_id)}
              >
                <Icon name={copiedRequestId === row.request_id ? 'check' : 'copy'} size="sm" className="h-3.5 w-3.5" />
              </button>
            </div>
          ) : (
            <span className="text-sm surface-muted">-</span>
          )
        case 'user_agent':
          return row.user_agent ? (
            <div className="flex max-w-[320px] items-center gap-1.5">
              <span className="truncate text-sm surface-muted" title={row.user_agent}>
                {formatUserAgent(row.user_agent)}
              </span>
              <button
                type="button"
                className={`shrink-0 rounded p-0.5 transition-colors hover:surface-hover hover:text-gray-600 dark:hover:text-gray-300 surface-control surface-muted ${
                  copiedUserAgent === row.user_agent ? 'text-green-500 hover:text-green-500' : ''
                }`}
                title={copiedUserAgent === row.user_agent ? t('keys.copied') : t('keys.copyToClipboard')}
                onClick={() => void copyUserAgent(row.user_agent as string)}
              >
                <Icon name={copiedUserAgent === row.user_agent ? 'check' : 'copy'} size="sm" className="h-3.5 w-3.5" />
              </button>
            </div>
          ) : (
            <span className="text-sm surface-muted">-</span>
          )
        case 'session_id':
          return row.session_id ? (
            <span
              data-testid="usage-session-id"
              className="block max-w-[320px] truncate font-mono text-sm surface-muted"
              title={row.session_id}
            >
              {row.session_id}
            </span>
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
    },
    // Tooltip/label helpers are derived from t, which is stable per language.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [
      t,
      modelMetadata,
      showServiceTier,
      showAccountBilling,
      showUpstreamEndpoint,
      copiedRequestId,
      copiedUserAgent,
      onUserClick
    ]
  )

  return (
    <div className={`card overflow-hidden ${workspace ? 'workspace-table-card workspace-shell' : ''}`}>
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
          renderCell={(key, row, cellValue) => renderCell(key, row as AdminUsageLog, cellValue)}
          renderEmpty={() => <EmptyState title={t('usage.noRecords')} />}
        />
      </div>
    </div>
  )
}

export default AdminUsageTable
