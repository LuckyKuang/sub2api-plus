import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { getUserBreakdown } from '@/api/admin/dashboard'
import type { EndpointStat, UserBreakdownItem } from '@/types'
import { formatTokenShare } from '@/utils/tokenShare'
import { chartTooltipStyle } from '@/utils/chartTooltip'
import { useDocumentDarkMode, LoadingSpinner } from './shared'
import { ChartCanvas } from './parts/ChartCanvas'

type DistributionMetric = 'tokens' | 'actual_cost'
type EndpointSource = 'inbound' | 'upstream' | 'path'

const chartColors = [
  '#3b82f6',
  '#10b981',
  '#f59e0b',
  '#ef4444',
  '#8b5cf6',
  '#ec4899',
  '#14b8a6',
  '#f97316',
  '#6366f1',
  '#84cc16',
  '#06b6d4',
  '#a855f7'
]

function formatTokens(value: number): string {
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(2)}K`
  return value.toLocaleString()
}

function formatNumber(value: number): string {
  return value.toLocaleString()
}

function formatCost(value: number): string {
  if (value >= 1000) return (value / 1000).toFixed(2) + 'K'
  if (value >= 1) return value.toFixed(2)
  if (value >= 0.01) return value.toFixed(3)
  return value.toFixed(4)
}

export interface EndpointDistributionChartProps {
  endpointStats: EndpointStat[]
  upstreamEndpointStats?: EndpointStat[]
  endpointPathStats?: EndpointStat[]
  loading?: boolean
  title?: string
  metric?: DistributionMetric
  source?: EndpointSource
  showMetricToggle?: boolean
  showSourceToggle?: boolean
  enableBreakdown?: boolean
  startDate?: string
  endDate?: string
  filters?: Record<string, unknown>
  onMetricChange?: (value: DistributionMetric) => void
  onSourceChange?: (value: EndpointSource) => void
}

export function EndpointDistributionChart({
  endpointStats,
  upstreamEndpointStats = [],
  endpointPathStats = [],
  loading = false,
  title = '',
  metric = 'tokens',
  source = 'inbound',
  showMetricToggle = false,
  showSourceToggle = false,
  enableBreakdown = true,
  startDate,
  endDate,
  filters,
  onMetricChange,
  onSourceChange
}: EndpointDistributionChartProps) {
  const { t } = useTranslation()
  const isDarkMode = useDocumentDarkMode()

  const [expandedKey, setExpandedKey] = useState<string | null>(null)
  const [breakdownItems, setBreakdownItems] = useState<UserBreakdownItem[]>([])
  const [breakdownLoading, setBreakdownLoading] = useState(false)

  const toggleBreakdown = async (endpoint: string) => {
    if (expandedKey === endpoint) {
      setExpandedKey(null)
      return
    }
    setExpandedKey(endpoint)
    setBreakdownLoading(true)
    setBreakdownItems([])
    try {
      const res = await getUserBreakdown({
        ...filters,
        start_date: startDate,
        end_date: endDate,
        endpoint,
        endpoint_type: source
      } as Parameters<typeof getUserBreakdown>[0])
      setBreakdownItems(res.users || [])
    } catch {
      setBreakdownItems([])
    } finally {
      setBreakdownLoading(false)
    }
  }

  const displayEndpointStats = useMemo(() => {
    const sourceStats =
      source === 'upstream' ? upstreamEndpointStats : source === 'path' ? endpointPathStats : endpointStats
    if (!sourceStats?.length) return []

    const metricKey = metric === 'actual_cost' ? 'actual_cost' : 'total_tokens'
    return [...sourceStats].sort(
      (a, b) => Number((b as unknown as Record<string, number>)[metricKey]) - Number((a as unknown as Record<string, number>)[metricKey])
    )
  }, [source, endpointStats, upstreamEndpointStats, endpointPathStats, metric])

  const distributionTotalTokens = displayEndpointStats.reduce((sum, endpoint) => sum + Number(endpoint.total_tokens || 0), 0)

  const endpointTokenShare = (tokens: number | null | undefined): string | null =>
    formatTokenShare(tokens, distributionTotalTokens)

  const chartData = useMemo(() => {
    if (!displayEndpointStats?.length) return null

    return {
      labels: displayEndpointStats.map((item) => item.endpoint),
      datasets: [
        {
          data: displayEndpointStats.map((item) => (metric === 'actual_cost' ? item.actual_cost : item.total_tokens)),
          backgroundColor: chartColors.slice(0, displayEndpointStats.length),
          borderWidth: 0
        }
      ]
    }
  }, [displayEndpointStats, metric])

  const doughnutOptions = useMemo(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: { display: false },
        tooltip: {
          ...chartTooltipStyle(isDarkMode),
          callbacks: {
            label: (context: { raw: unknown; label?: string; dataset: { data: unknown[] } }) => {
              const value = context.raw as number
              const total = (context.dataset.data as number[]).reduce((a, b) => a + b, 0)
              const percentage = total > 0 ? ((value / total) * 100).toFixed(1) : '0.0'
              const formattedValue = metric === 'actual_cost' ? `$${formatCost(value)}` : formatTokens(value)
              return `${context.label}: ${formattedValue} (${percentage}%)`
            }
          }
        }
      }
    }),
    [isDarkMode, metric]
  )

  const toggleButtonClass = (active: boolean) =>
    `rounded-md px-2.5 py-1 text-xs font-medium transition-colors surface-control ${
      active
        ? 'surface-field text-gray-900 shadow-sm dark:surface-field dark:text-white'
        : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'
    }`

  return (
    <div className="card p-4">
      <div className="mb-4 flex items-center justify-between gap-3">
        <h3 className="text-sm font-semibold text-gray-900 dark:text-white">{title || t('usage.endpointDistribution')}</h3>
        <div className="flex flex-wrap items-center justify-end gap-2">
          {showSourceToggle && (
            <div className="inline-flex rounded-lg border border-gray-200 surface-inset p-0.5 dark:border-dark-700">
              <button type="button" className={toggleButtonClass(source === 'inbound')} onClick={() => onSourceChange?.('inbound')}>
                {t('usage.inbound')}
              </button>
              <button type="button" className={toggleButtonClass(source === 'upstream')} onClick={() => onSourceChange?.('upstream')}>
                {t('usage.upstream')}
              </button>
              <button type="button" className={toggleButtonClass(source === 'path')} onClick={() => onSourceChange?.('path')}>
                {t('usage.path')}
              </button>
            </div>
          )}

          {showMetricToggle && (
            <div className="inline-flex rounded-lg border border-gray-200 surface-inset p-0.5 dark:border-dark-700">
              <button type="button" className={toggleButtonClass(metric === 'tokens')} onClick={() => onMetricChange?.('tokens')}>
                {t('admin.dashboard.metricTokens')}
              </button>
              <button
                type="button"
                className={toggleButtonClass(metric === 'actual_cost')}
                onClick={() => onMetricChange?.('actual_cost')}
              >
                {t('admin.dashboard.metricActualCost')}
              </button>
            </div>
          )}
        </div>
      </div>
      {loading ? (
        <div className="flex h-48 items-center justify-center">
          <LoadingSpinner />
        </div>
      ) : displayEndpointStats.length > 0 && chartData ? (
        <div className="flex flex-col items-center gap-4 sm:flex-row sm:gap-6">
          <div className="h-48 w-48 shrink-0">
            <ChartCanvas kind="doughnut" data={chartData as never} options={doughnutOptions as never} />
          </div>
          <div className="record-table-scroll max-h-48 w-full min-w-0 flex-1 overflow-auto">
            <table className="record-table record-table-compact w-full text-xs">
              <thead>
                <tr className="surface-muted">
                  <th className="pb-2 text-left">{t('usage.endpoint')}</th>
                  <th className="pb-2 text-right">{t('admin.dashboard.requests')}</th>
                  <th className="pb-2 text-right">{t('admin.dashboard.tokens')}</th>
                  <th className="pb-2 text-right">{t('admin.dashboard.actual')}</th>
                  <th className="pb-2 text-right">{t('admin.dashboard.standard')}</th>
                </tr>
              </thead>
              {displayEndpointStats.map((item) => (
                <tbody key={item.endpoint} className="record-group">
                  <tr
                    className={`transition-colors ${enableBreakdown ? 'cursor-pointer hover:surface-hover dark:hover:surface-hover' : ''}`}
                    onClick={enableBreakdown ? () => void toggleBreakdown(item.endpoint) : undefined}
                  >
                    <td
                      data-record-edges={expandedKey === item.endpoint ? 't l tl' : 't b l tl bl'}
                      className={`max-w-[180px] truncate py-1.5 font-medium ${
                        enableBreakdown
                          ? 'text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300'
                          : 'text-gray-900 dark:text-white'
                      }`}
                      title={item.endpoint}
                    >
                      <span className="inline-flex items-center gap-1">
                        {enableBreakdown && expandedKey === item.endpoint ? (
                          <svg className="h-3 w-3 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M19 9l-7 7-7-7" />
                          </svg>
                        ) : enableBreakdown ? (
                          <svg className="h-3 w-3 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M9 5l7 7-7 7" />
                          </svg>
                        ) : null}
                        {item.endpoint}
                      </span>
                    </td>
                    <td data-record-edges={expandedKey === item.endpoint ? 't' : 't b'} className="py-1.5 text-right surface-muted">
                      {formatNumber(item.requests)}
                    </td>
                    <td data-record-edges={expandedKey === item.endpoint ? 't' : 't b'} className="py-1.5 text-right surface-muted">
                      {formatTokens(item.total_tokens)}{' '}
                      {endpointTokenShare(item.total_tokens) && <span className="surface-muted">({endpointTokenShare(item.total_tokens)})</span>}
                    </td>
                    <td data-record-edges={expandedKey === item.endpoint ? 't' : 't b'} className="py-1.5 text-right text-green-600 dark:text-green-400">
                      ${formatCost(item.actual_cost)}
                    </td>
                    <td
                      data-record-edges={expandedKey === item.endpoint ? 't r tr' : 't b r tr br'}
                      className="py-1.5 text-right surface-muted"
                    >
                      ${formatCost(item.cost)}
                    </td>
                  </tr>
                  {expandedKey === item.endpoint && (
                    <>
                      {breakdownLoading ? (
                        <tr>
                          <td colSpan={5} className="py-2 text-center">
                            <LoadingSpinner size="sm" />
                          </td>
                        </tr>
                      ) : breakdownItems.length === 0 ? (
                        <tr>
                          <td colSpan={5} className="py-2 text-center text-xs surface-muted">
                            {t('admin.dashboard.noDataAvailable')}
                          </td>
                        </tr>
                      ) : (
                        breakdownItems.map((user, index) => (
                          <tr key={`${user.user_id}-${index}`}>
                            <td data-record-edges={index === breakdownItems.length - 1 ? 'b l bl' : 'l'} className="py-1 pl-4 text-xs surface-muted">
                              {user.email || `#${user.user_id}`}
                            </td>
                            <td data-record-edges={index === breakdownItems.length - 1 ? 'b' : ''} className="py-1 text-right text-xs surface-muted">
                              {formatNumber(user.requests)}
                            </td>
                            <td data-record-edges={index === breakdownItems.length - 1 ? 'b' : ''} className="py-1 text-right text-xs surface-muted">
                              {formatTokens(user.total_tokens)}{' '}
                              {endpointTokenShare(user.total_tokens) && (
                                <span className="surface-muted">({endpointTokenShare(user.total_tokens)})</span>
                              )}
                            </td>
                            <td data-record-edges={index === breakdownItems.length - 1 ? 'b' : ''} className="py-1 text-right text-xs text-green-600 dark:text-green-400">
                              ${formatCost(user.actual_cost)}
                            </td>
                            <td data-record-edges={index === breakdownItems.length - 1 ? 'b r br' : 'r'} className="py-1 text-right text-xs surface-muted">
                              ${formatCost(user.cost)}
                            </td>
                          </tr>
                        ))
                      )}
                    </>
                  )}
                </tbody>
              ))}
            </table>
          </div>
        </div>
      ) : (
        <div className="flex h-48 items-center justify-center text-sm surface-muted">{t('admin.dashboard.noDataAvailable')}</div>
      )}
    </div>
  )
}

export default EndpointDistributionChart
