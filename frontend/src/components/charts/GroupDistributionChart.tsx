import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Cell, Pie, PieChart, ResponsiveContainer, Tooltip } from 'recharts'
import { useDocumentDarkMode } from '@/composables/useDocumentDarkMode'
import { cn } from '@/lib/utils'
import { getUserBreakdown } from '@/api/admin/dashboard'
import type { GroupStat, UserBreakdownItem } from '@/types'
import { formatTokenShare } from '@/utils/tokenShare'
import { Spinner } from '../ui'
import { DistributionTooltip, type DistributionMetric } from './DistributionTooltip'
import { UserBreakdownRows } from './UserBreakdownRows'

export interface GroupDistributionChartProps {
  groupStats: GroupStat[]
  loading?: boolean
  metric?: DistributionMetric
  showMetricToggle?: boolean
  enableBreakdown?: boolean
  showAccountCost?: boolean
  startDate?: string
  endDate?: string
  filters?: Record<string, any>
  className?: string
  onMetricChange?: (value: DistributionMetric) => void
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
  '#84cc16'
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
  return toFiniteNumber(value).toLocaleString()
}

function toFiniteNumber(value: unknown): number {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : 0
}

function formatCost(value: number | null | undefined): string {
  const safeValue = toFiniteNumber(value)
  if (safeValue >= 1000) {
    return (safeValue / 1000).toFixed(2) + 'K'
  } else if (safeValue >= 1) {
    return safeValue.toFixed(2)
  } else if (safeValue >= 0.01) {
    return safeValue.toFixed(3)
  }
  return safeValue.toFixed(4)
}

export function GroupDistributionChart({
  groupStats,
  loading = false,
  metric = 'tokens',
  showMetricToggle = false,
  enableBreakdown = true,
  showAccountCost = true,
  startDate,
  endDate,
  filters,
  className,
  onMetricChange
}: GroupDistributionChartProps) {
  const { t } = useTranslation()
  const isDarkMode = useDocumentDarkMode()

  const [expandedKey, setExpandedKey] = useState<string | null>(null)
  const [breakdownItems, setBreakdownItems] = useState<UserBreakdownItem[]>([])
  const [breakdownLoading, setBreakdownLoading] = useState(false)

  const toggleBreakdown = async (type: string, id: number | string) => {
    const key = `${type}-${id}`
    if (expandedKey === key) {
      setExpandedKey(null)
      return
    }
    setExpandedKey(key)
    setBreakdownLoading(true)
    setBreakdownItems([])
    try {
      const res = await getUserBreakdown({
        ...filters,
        start_date: startDate,
        end_date: endDate,
        group_id: Number(id)
      })
      setBreakdownItems(res.users || [])
    } catch {
      setBreakdownItems([])
    } finally {
      setBreakdownLoading(false)
    }
  }

  const displayGroupStats = (() => {
    if (!groupStats?.length) return []

    const metricKey = metric === 'actual_cost' ? 'actual_cost' : 'total_tokens'
    return [...groupStats].sort((a, b) => toFiniteNumber(b[metricKey]) - toFiniteNumber(a[metricKey]))
  })()

  const distributionTotalTokens = displayGroupStats.reduce(
    (sum, group) => sum + toFiniteNumber(group.total_tokens),
    0
  )

  const groupTokenShare = (tokens: number | null | undefined): string | null =>
    formatTokenShare(tokens, distributionTotalTokens)

  const chartData = (() => {
    if (!groupStats?.length) return null
    return displayGroupStats.map((g) => ({
      label: g.group_name || String(g.group_id),
      value: toFiniteNumber(metric === 'actual_cost' ? g.actual_cost : g.total_tokens)
    }))
  })()

  const chartTotal = chartData?.reduce((sum, entry) => sum + Number(entry.value || 0), 0) ?? 0

  return (
    <div className={cn('card p-4', className)}>
      <div className="mb-4 flex items-center justify-between gap-3">
        <h3 className="text-sm font-semibold text-gray-900 dark:text-white">
          {t('admin.dashboard.groupDistribution')}
        </h3>
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
      {loading ? (
        <div className="flex h-48 items-center justify-center">
          <Spinner />
        </div>
      ) : displayGroupStats.length > 0 && chartData ? (
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
                <Tooltip
                  content={(props) => (
                    <DistributionTooltip
                      {...props}
                      metric={metric}
                      isDarkMode={isDarkMode}
                      total={chartTotal}
                    />
                  )}
                />
              </PieChart>
            </ResponsiveContainer>
          </div>
          <div className="record-table-scroll max-h-48 w-full min-w-0 flex-1 overflow-auto">
            <table className="record-table record-table-compact w-full text-xs">
              <thead>
                <tr className="surface-muted">
                  <th className="pb-2 text-left">{t('admin.dashboard.group')}</th>
                  <th className="pb-2 text-right">{t('admin.dashboard.requests')}</th>
                  <th className="pb-2 text-right">{t('admin.dashboard.tokens')}</th>
                  <th className="pb-2 text-right">{t('admin.dashboard.actual')}</th>
                  {showAccountCost && <th className="pb-2 text-right">{t('admin.dashboard.accountCost')}</th>}
                  <th className="pb-2 text-right">{t('admin.dashboard.standard')}</th>
                </tr>
              </thead>
              {displayGroupStats.map((group) => (
                <tbody key={group.group_id} className="record-group">
                  <tr
                    className={cn(
                      'transition-colors',
                      enableBreakdown && group.group_id > 0 && 'cursor-pointer hover:surface-hover dark:hover:surface-hover'
                    )}
                    onClick={
                      enableBreakdown && group.group_id > 0
                        ? () => void toggleBreakdown('group', group.group_id)
                        : undefined
                    }
                  >
                    <td
                      data-record-edges={expandedKey === `group-${group.group_id}` ? 't l tl' : 't b l tl bl'}
                      className="max-w-[100px] truncate py-1.5 font-medium"
                      title={group.group_name || String(group.group_id)}
                    >
                      <span
                        className={cn(
                          'inline-flex items-center gap-1',
                          enableBreakdown && group.group_id > 0
                            ? 'text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300'
                            : 'text-gray-900 dark:text-white'
                        )}
                      >
                        {enableBreakdown &&
                          group.group_id > 0 &&
                          (expandedKey === `group-${group.group_id}` ? (
                            <svg className="h-3 w-3 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M19 9l-7 7-7-7" />
                            </svg>
                          ) : (
                            <svg className="h-3 w-3 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M9 5l7 7-7 7" />
                            </svg>
                          ))}
                        {group.group_name || t('admin.dashboard.noGroup')}
                      </span>
                    </td>
                    <td
                      data-record-edges={expandedKey === `group-${group.group_id}` ? 't' : 't b'}
                      className="py-1.5 text-right surface-muted"
                    >
                      {formatNumber(group.requests)}
                    </td>
                    <td
                      data-record-edges={expandedKey === `group-${group.group_id}` ? 't' : 't b'}
                      className="py-1.5 text-right surface-muted"
                    >
                      {formatTokens(group.total_tokens)}{' '}
                      {groupTokenShare(group.total_tokens) && (
                        <span className="surface-muted">({groupTokenShare(group.total_tokens)})</span>
                      )}
                    </td>
                    <td
                      data-record-edges={expandedKey === `group-${group.group_id}` ? 't' : 't b'}
                      className="py-1.5 text-right text-green-600 dark:text-green-400"
                    >
                      ${formatCost(group.actual_cost)}
                    </td>
                    {showAccountCost && (
                      <td
                        data-record-edges={expandedKey === `group-${group.group_id}` ? 't' : 't b'}
                        className="py-1.5 text-right text-orange-500 dark:text-orange-400"
                      >
                        ${formatCost(group.account_cost)}
                      </td>
                    )}
                    <td
                      data-record-edges={expandedKey === `group-${group.group_id}` ? 't r tr' : 't b r tr br'}
                      className="py-1.5 text-right surface-muted"
                    >
                      ${formatCost(group.cost)}
                    </td>
                  </tr>
                  {expandedKey === `group-${group.group_id}` && (
                    <UserBreakdownRows
                      items={breakdownItems}
                      loading={breakdownLoading}
                      showAccountCost={showAccountCost}
                      totalTokens={group.total_tokens}
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
