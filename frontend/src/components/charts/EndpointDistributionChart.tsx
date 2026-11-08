import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Cell, Pie, PieChart, ResponsiveContainer, Tooltip } from 'recharts'
import { useDocumentDarkMode } from '@/composables/useDocumentDarkMode'
import { cn } from '@/lib/utils'
import { getUserBreakdown } from '@/api/admin/dashboard'
import type { EndpointStat, UserBreakdownItem } from '@/types'
import { formatTokenShare } from '@/utils/tokenShare'
import { Spinner } from '../ui'
import { UserBreakdownRows } from './UserBreakdownRows'
import { DistributionTooltip } from './DistributionTooltip'

export type DistributionMetric = 'tokens' | 'actual_cost'
export type EndpointSource = 'inbound' | 'upstream' | 'path'

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
  filters?: Record<string, any>
  className?: string
  onMetricChange?: (value: DistributionMetric) => void
  onSourceChange?: (value: EndpointSource) => void
}

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
  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`
  } else if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`
  } else if (value >= 1_000) {
    return `${(value / 1_000).toFixed(2)}K`
  }
  return value.toLocaleString()
}

function formatNumber(value: number): string {
  return value.toLocaleString()
}

function formatCost(value: number): string {
  if (value >= 1000) {
    return (value / 1000).toFixed(2) + 'K'
  } else if (value >= 1) {
    return value.toFixed(2)
  } else if (value >= 0.01) {
    return value.toFixed(3)
  }
  return value.toFixed(4)
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
  className,
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
      })
      setBreakdownItems(res.users || [])
    } catch {
      setBreakdownItems([])
    } finally {
      setBreakdownLoading(false)
    }
  }

  const displayEndpointStats = (() => {
    const sourceStats =
      source === 'upstream'
        ? upstreamEndpointStats
        : source === 'path'
          ? endpointPathStats
          : endpointStats
    if (!sourceStats?.length) return []

    const metricKey = metric === 'actual_cost' ? 'actual_cost' : 'total_tokens'
    return [...sourceStats].sort((a, b) => b[metricKey] - a[metricKey])
  })()

  const distributionTotalTokens = displayEndpointStats.reduce(
    (sum, endpoint) => sum + Number(endpoint.total_tokens || 0),
    0
  )

  const endpointTokenShare = (tokens: number | null | undefined): string | null =>
    formatTokenShare(tokens, distributionTotalTokens)

  const chartData = (() => {
    if (!displayEndpointStats?.length) return null
    return displayEndpointStats.map((item) => ({
      label: item.endpoint,
      value: metric === 'actual_cost' ? item.actual_cost : item.total_tokens
    }))
  })()

  const chartTotal = chartData?.reduce((sum, entry) => sum + Number(entry.value || 0), 0) ?? 0

  const doughnutTooltip = (
    <Tooltip
      content={(props) => (
        <DistributionTooltip {...props} metric={metric} isDarkMode={isDarkMode} total={chartTotal} />
      )}
    />
  )

  return (
    <div className={cn('card p-4', className)}>
      <div className="mb-4 flex items-center justify-between gap-3">
        <h3 className="text-sm font-semibold text-gray-900 dark:text-white">
          {title || t('usage.endpointDistribution')}
        </h3>
        <div className="flex flex-wrap items-center justify-end gap-2">
          {showSourceToggle && (
            <div className="inline-flex rounded-lg border border-gray-200 surface-inset p-0.5 dark:border-dark-700">
              {(['inbound', 'upstream', 'path'] as const).map((entry) => (
                <button
                  key={entry}
                  type="button"
                  className={cn(
                    'rounded-md px-2.5 py-1 text-xs font-medium transition-colors surface-control',
                    source === entry
                      ? 'surface-field text-gray-900 shadow-sm dark:surface-field dark:text-white'
                      : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'
                  )}
                  onClick={() => onSourceChange?.(entry)}
                >
                  {t(entry === 'inbound' ? 'usage.inbound' : entry === 'upstream' ? 'usage.upstream' : 'usage.path')}
                </button>
              ))}
            </div>
          )}

          {showMetricToggle && (
            <div className="inline-flex rounded-lg border border-gray-200 surface-inset p-0.5 dark:border-dark-700">
              {(['tokens', 'actual_cost'] as const).map((entry) => (
                <button
                  key={entry}
                  type="button"
                  className={cn(
                    'rounded-md px-2.5 py-1 text-xs font-medium transition-colors surface-control',
                    metric === entry
                      ? 'surface-field text-gray-900 shadow-sm dark:surface-field dark:text-white'
                      : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'
                  )}
                  onClick={() => onMetricChange?.(entry)}
                >
                  {t(entry === 'tokens' ? 'admin.dashboard.metricTokens' : 'admin.dashboard.metricActualCost')}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>
      {loading ? (
        <div className="flex h-48 items-center justify-center">
          <Spinner />
        </div>
      ) : displayEndpointStats.length > 0 && chartData ? (
        <div className="flex flex-col items-center gap-4 sm:flex-row sm:gap-6">
          <div className="h-48 w-48 shrink-0">
            <ResponsiveContainer width="100%" height="100%">
              <PieChart>
                <Pie
                  data={chartData}
                  dataKey="value"
                  nameKey="label"
                  cx="50%"
                  cy="50%"
                  innerRadius={48}
                  outerRadius={88}
                  startAngle={90}
                  endAngle={-270}
                  stroke="none"
                  isAnimationActive={false}
                >
                  {chartData.map((entry, index) => (
                    <Cell key={entry.label} fill={chartColors[index % chartColors.length]} />
                  ))}
                </Pie>
                {doughnutTooltip}
              </PieChart>
            </ResponsiveContainer>
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
                    className="transition-colors"
                    onClick={enableBreakdown ? () => void toggleBreakdown(item.endpoint) : undefined}
                  >
                    <td
                      data-record-edges={expandedKey === item.endpoint ? 't l tl' : 't b l tl bl'}
                      className="max-w-[180px] truncate py-1.5 font-medium"
                      title={item.endpoint}
                    >
                      <span
                        className={cn(
                          'inline-flex items-center gap-1',
                          enableBreakdown
                            ? 'cursor-pointer text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300'
                            : 'text-gray-900 dark:text-white'
                        )}
                      >
                        {enableBreakdown &&
                          (expandedKey === item.endpoint ? (
                            <svg className="h-3 w-3 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M19 9l-7 7-7-7" />
                            </svg>
                          ) : (
                            <svg className="h-3 w-3 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M9 5l7 7-7 7" />
                            </svg>
                          ))}
                        {item.endpoint}
                      </span>
                    </td>
                    <td
                      data-record-edges={expandedKey === item.endpoint ? 't' : 't b'}
                      className="py-1.5 text-right surface-muted"
                    >
                      {formatNumber(item.requests)}
                    </td>
                    <td
                      data-record-edges={expandedKey === item.endpoint ? 't' : 't b'}
                      className="py-1.5 text-right surface-muted"
                    >
                      {formatTokens(item.total_tokens)}{' '}
                      {endpointTokenShare(item.total_tokens) && (
                        <span className="surface-muted">({endpointTokenShare(item.total_tokens)})</span>
                      )}
                    </td>
                    <td
                      data-record-edges={expandedKey === item.endpoint ? 't' : 't b'}
                      className="py-1.5 text-right text-green-600 dark:text-green-400"
                    >
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
                    <UserBreakdownRows
                      items={breakdownItems}
                      loading={breakdownLoading}
                      showAccountCost={false}
                      totalTokens={item.total_tokens}
                    />
                  )}
                </tbody>
              ))}
            </table>
          </div>
        </div>
      ) : (
        <div className="flex h-48 items-center justify-center text-sm surface-muted">
          {t('admin.dashboard.noDataAvailable')}
        </div>
      )}
    </div>
  )
}
