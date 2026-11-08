import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Cell, Pie, PieChart, ResponsiveContainer, Tooltip } from 'recharts'
import { useDocumentDarkMode } from '@/composables/useDocumentDarkMode'
import { cn } from '@/lib/utils'
import { getUserBreakdown } from '@/api/admin/dashboard'
import type { ModelStat, UserBreakdownItem, UserSpendingRankingItem } from '@/types'
import { formatTokenShare } from '@/utils/tokenShare'
import { Spinner } from '../ui'
import { DistributionTooltip, type DistributionMetric } from './DistributionTooltip'
import { UserBreakdownRows } from './UserBreakdownRows'

export type ModelSource = 'requested' | 'upstream' | 'mapping'

type RankingDisplayItem = UserSpendingRankingItem & { isOther?: boolean }

export interface ModelDistributionChartProps {
  workspace?: boolean
  modelStats: ModelStat[]
  upstreamModelStats?: ModelStat[]
  mappingModelStats?: ModelStat[]
  source?: ModelSource
  enableRankingView?: boolean
  rankingItems?: UserSpendingRankingItem[]
  rankingTotalActualCost?: number
  rankingTotalRequests?: number
  rankingTotalTokens?: number
  loading?: boolean
  metric?: DistributionMetric
  showSourceToggle?: boolean
  showMetricToggle?: boolean
  enableBreakdown?: boolean
  showAccountCost?: boolean
  rankingLoading?: boolean
  rankingError?: boolean
  startDate?: string
  endDate?: string
  filters?: Record<string, any>
  className?: string
  onMetricChange?: (value: DistributionMetric) => void
  onSourceChange?: (value: ModelSource) => void
  onRankingClick?: (item: UserSpendingRankingItem) => void
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

const chevronSvg = (expanded: boolean) => (
  <svg className="h-3 w-3 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
    <path
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth="2"
      d={expanded ? 'M19 9l-7 7-7-7' : 'M9 5l7 7-7 7'}
    />
  </svg>
)

export function ModelDistributionChart({
  workspace = false,
  modelStats,
  upstreamModelStats = [],
  mappingModelStats = [],
  source = 'requested',
  enableRankingView = false,
  rankingItems = [],
  rankingTotalActualCost = 0,
  rankingTotalRequests = 0,
  rankingTotalTokens = 0,
  loading = false,
  metric = 'tokens',
  showSourceToggle = false,
  showMetricToggle = false,
  enableBreakdown = true,
  showAccountCost = true,
  rankingLoading = false,
  rankingError = false,
  startDate,
  endDate,
  filters,
  className,
  onMetricChange,
  onSourceChange,
  onRankingClick
}: ModelDistributionChartProps) {
  const { t } = useTranslation()
  const isDarkMode = useDocumentDarkMode()

  const [expandedKey, setExpandedKey] = useState<string | null>(null)
  const [breakdownItems, setBreakdownItems] = useState<UserBreakdownItem[]>([])
  const [breakdownLoading, setBreakdownLoading] = useState(false)
  const [activeView, setActiveView] = useState<'model_distribution' | 'spending_ranking'>(
    'model_distribution'
  )

  const toggleBreakdown = async (type: string, id: string) => {
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
        model: id,
        model_source: source
      })
      setBreakdownItems(res.users || [])
    } catch {
      setBreakdownItems([])
    } finally {
      setBreakdownLoading(false)
    }
  }

  const getRankingUserLabel = (item: UserSpendingRankingItem): string => {
    if (item.username?.trim()) return item.username.trim()
    if (item.email?.trim()) return item.email.trim()
    return t('admin.redeem.userPrefix', { id: item.user_id })
  }

  const getRankingRowLabel = (item: RankingDisplayItem): string => {
    if (item.isOther) return t('admin.dashboard.spendingRankingOther')
    return getRankingUserLabel(item)
  }

  const otherRankingItem = ((): RankingDisplayItem | null => {
    if (!rankingItems?.length) return null

    const rankedActualCost = rankingItems.reduce((sum, item) => sum + toFiniteNumber(item.actual_cost), 0)
    const rankedRequests = rankingItems.reduce((sum, item) => sum + toFiniteNumber(item.requests), 0)
    const rankedTokens = rankingItems.reduce((sum, item) => sum + toFiniteNumber(item.tokens), 0)

    const otherActualCost = Math.max((rankingTotalActualCost || 0) - rankedActualCost, 0)
    const otherRequests = Math.max((rankingTotalRequests || 0) - rankedRequests, 0)
    const otherTokens = Math.max((rankingTotalTokens || 0) - rankedTokens, 0)

    if (otherActualCost <= 0.000001 && otherRequests <= 0 && otherTokens <= 0) return null

    return {
      user_id: 0,
      email: '',
      username: '',
      actual_cost: otherActualCost,
      requests: otherRequests,
      tokens: otherTokens,
      isOther: true
    }
  })()

  const rankingDisplayItems = ((): RankingDisplayItem[] => {
    if (!rankingItems?.length) return []
    return otherRankingItem ? [...rankingItems, otherRankingItem] : [...rankingItems]
  })()

  const displayModelStats = (() => {
    const sourceStats =
      source === 'upstream'
        ? upstreamModelStats
        : source === 'mapping'
          ? mappingModelStats
          : modelStats
    if (!sourceStats?.length) return []

    const metricKey = metric === 'actual_cost' ? 'actual_cost' : 'total_tokens'
    return [...sourceStats].sort((a, b) => toFiniteNumber(b[metricKey]) - toFiniteNumber(a[metricKey]))
  })()

  const distributionTotalTokens = displayModelStats.reduce(
    (sum, model) => sum + toFiniteNumber(model.total_tokens),
    0
  )

  const modelTokenShare = (tokens: number | null | undefined): string | null =>
    formatTokenShare(tokens, distributionTotalTokens)

  const rankingTokenShare = (tokens: number | null | undefined): string | null =>
    formatTokenShare(tokens, rankingTotalTokens)

  const chartData = (() => {
    if (!displayModelStats.length) return null
    return displayModelStats.map((m) => ({
      label: m.model,
      value: toFiniteNumber(metric === 'actual_cost' ? m.actual_cost : m.total_tokens)
    }))
  })()

  const chartTotal = chartData?.reduce((sum, entry) => sum + Number(entry.value || 0), 0) ?? 0

  const rankingChartData = (() => {
    if (!rankingItems?.length) return null

    const labels = rankingItems.map((item, index) => `#${index + 1} ${getRankingUserLabel(item)}`)
    const data = rankingItems.map((item) => toFiniteNumber(item.actual_cost))
    const backgroundColor = chartColors.slice(0, rankingItems.length)

    if (otherRankingItem) {
      labels.push(t('admin.dashboard.spendingRankingOther'))
      data.push(otherRankingItem.actual_cost)
      backgroundColor.push('#94a3b8')
    }

    return { labels, data, backgroundColor }
  })()

  const rankingTotal = rankingChartData?.data.reduce((sum, value) => sum + Number(value || 0), 0) ?? 0

  const doughnut = (data: Array<{ label: string; value: number }>, total: number) => (
    <PieChart>
      <Pie
        data={data}
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
        {data.map((entry, index) => (
          <Cell key={entry.label} fill={chartColors[index % chartColors.length]} />
        ))}
      </Pie>
      <Tooltip
        content={(props) => (
          <DistributionTooltip
            {...props}
            metric={metric === 'actual_cost' ? 'actual_cost' : 'tokens'}
            isDarkMode={isDarkMode}
            total={total}
          />
        )}
      />
    </PieChart>
  )

  return (
    <div className={cn('card p-4', workspace && 'chart-workspace chart-distribution', className)}>
      <div className="mb-4 flex items-center justify-between gap-3">
        <h3 className="text-sm font-semibold text-gray-900 dark:text-white">
          {!enableRankingView || activeView === 'model_distribution'
            ? t('admin.dashboard.modelDistribution')
            : t('admin.dashboard.spendingRankingTitle')}
        </h3>
        <div className="flex flex-wrap items-center justify-end gap-2">
          {showSourceToggle && (
            <div className="inline-flex rounded-lg border border-gray-200 surface-inset p-0.5 dark:border-dark-700">
              {(['requested', 'upstream', 'mapping'] as const).map((entry) => (
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
                  {t(
                    entry === 'requested'
                      ? 'usage.requestedModel'
                      : entry === 'upstream'
                        ? 'usage.upstreamModel'
                        : 'usage.mapping'
                  )}
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
          {enableRankingView && (
            <div className="inline-flex rounded-lg surface-inset p-1">
              {(['model_distribution', 'spending_ranking'] as const).map((entry) => (
                <button
                  key={entry}
                  type="button"
                  className={cn(
                    'rounded-md px-2.5 py-1 text-xs font-medium transition-colors surface-control',
                    activeView === entry
                      ? 'surface-field text-gray-900 shadow-sm dark:surface-field dark:text-white'
                      : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'
                  )}
                  onClick={() => setActiveView(entry)}
                >
                  {t(
                    entry === 'model_distribution'
                      ? 'admin.dashboard.viewModelDistribution'
                      : 'admin.dashboard.viewSpendingRanking'
                  )}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      {activeView === 'model_distribution' && loading && displayModelStats.length === 0 ? (
        <div className="flex h-48 items-center justify-center">
          <Spinner />
        </div>
      ) : activeView === 'model_distribution' && displayModelStats.length > 0 && chartData ? (
        <div className="flex flex-col items-center gap-4 sm:flex-row sm:gap-6">
          <div className="h-48 w-48 shrink-0">
            <ResponsiveContainer width="100%" height="100%">
              {doughnut(chartData, chartTotal)}
            </ResponsiveContainer>
          </div>
          <div className="record-table-scroll max-h-48 w-full min-w-0 flex-1 overflow-auto">
            <table className="record-table record-table-compact w-full text-xs">
              <thead>
                <tr className="surface-muted">
                  <th className="pb-2 text-left">{t('admin.dashboard.model')}</th>
                  <th className="pb-2 text-right">{t('admin.dashboard.requests')}</th>
                  <th className="pb-2 text-right">{t('admin.dashboard.tokens')}</th>
                  <th className="pb-2 text-right">{t('admin.dashboard.actual')}</th>
                  {showAccountCost && <th className="pb-2 text-right">{t('admin.dashboard.accountCost')}</th>}
                  <th className="pb-2 text-right">{t('admin.dashboard.standard')}</th>
                </tr>
              </thead>
              {displayModelStats.map((model) => (
                <tbody key={model.model} className="record-group">
                  <tr
                    className={cn('transition-colors', enableBreakdown && 'cursor-pointer hover:surface-hover dark:hover:surface-hover')}
                    onClick={enableBreakdown ? () => void toggleBreakdown('model', model.model) : undefined}
                  >
                    <td
                      data-record-edges={expandedKey === `model-${model.model}` ? 't l tl' : 't b l tl bl'}
                      className="max-w-[100px] truncate py-1.5 font-medium"
                      title={model.model}
                    >
                      <span
                        className={cn(
                          'inline-flex items-center gap-1',
                          enableBreakdown
                            ? 'text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300'
                            : 'text-gray-900 dark:text-white'
                        )}
                      >
                        {enableBreakdown && chevronSvg(expandedKey === `model-${model.model}`)}
                        {model.model}
                      </span>
                    </td>
                    <td
                      data-record-edges={expandedKey === `model-${model.model}` ? 't' : 't b'}
                      className="py-1.5 text-right surface-muted"
                    >
                      {formatNumber(model.requests)}
                    </td>
                    <td
                      data-record-edges={expandedKey === `model-${model.model}` ? 't' : 't b'}
                      className="py-1.5 text-right surface-muted"
                    >
                      {formatTokens(model.total_tokens)}{' '}
                      {modelTokenShare(model.total_tokens) && (
                        <span className="surface-muted">({modelTokenShare(model.total_tokens)})</span>
                      )}
                    </td>
                    <td
                      data-record-edges={expandedKey === `model-${model.model}` ? 't' : 't b'}
                      className="py-1.5 text-right text-green-600 dark:text-green-400"
                    >
                      ${formatCost(model.actual_cost)}
                    </td>
                    {showAccountCost && (
                      <td
                        data-record-edges={expandedKey === `model-${model.model}` ? 't' : 't b'}
                        className="py-1.5 text-right text-orange-500 dark:text-orange-400"
                      >
                        ${formatCost(model.account_cost)}
                      </td>
                    )}
                    <td
                      data-record-edges={expandedKey === `model-${model.model}` ? 't r tr' : 't b r tr br'}
                      className="py-1.5 text-right surface-muted"
                    >
                      ${formatCost(model.cost)}
                    </td>
                  </tr>
                  {expandedKey === `model-${model.model}` && (
                    <UserBreakdownRows
                      items={breakdownItems}
                      loading={breakdownLoading}
                      showAccountCost={showAccountCost}
                      totalTokens={model.total_tokens}
                    />
                  )}
                </tbody>
              ))}
            </table>
          </div>
        </div>
      ) : activeView === 'model_distribution' ? (
        <div className="flex h-48 items-center justify-center text-sm surface-muted">
          {t('admin.dashboard.noDataAvailable')}
        </div>
      ) : rankingLoading && rankingDisplayItems.length === 0 ? (
        <div className="flex h-48 items-center justify-center">
          <Spinner />
        </div>
      ) : rankingError && rankingDisplayItems.length === 0 ? (
        <div className="flex h-48 items-center justify-center text-sm surface-muted">
          {t('admin.dashboard.failedToLoad')}
        </div>
      ) : rankingDisplayItems.length > 0 && rankingChartData ? (
        <div className="flex flex-col items-center gap-4 sm:flex-row sm:gap-6">
          <div className="h-48 w-48 shrink-0">
            <ResponsiveContainer width="100%" height="100%">
              <PieChart>
                <Pie
                  data={rankingChartData.data.map((value, index) => ({
                    label: rankingChartData.labels[index],
                    value
                  }))}
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
                  {rankingChartData.data.map((_, index) => (
                    <Cell key={index} fill={rankingChartData.backgroundColor[index]} />
                  ))}
                </Pie>
                <Tooltip
                  content={(props) => (
                    <DistributionTooltip
                      {...props}
                      metric="actual_cost"
                      isDarkMode={isDarkMode}
                      total={rankingTotal}
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
                  <th className="pb-2 text-left">{t('admin.dashboard.spendingRankingUser')}</th>
                  <th className="pb-2 text-right">{t('admin.dashboard.spendingRankingRequests')}</th>
                  <th className="pb-2 text-right">{t('admin.dashboard.spendingRankingTokens')}</th>
                  <th className="pb-2 text-right">{t('admin.dashboard.spendingRankingSpend')}</th>
                </tr>
              </thead>
              <tbody>
                {rankingDisplayItems.map((item, index) => (
                  <tr
                    key={item.isOther ? 'others' : `${item.user_id}-${index}`}
                    className={cn(
                      'border-t border-gray-100 transition-colors dark:border-dark-700',
                      item.isOther
                        ? 'surface-inset dark:surface-inset'
                        : 'cursor-pointer hover:surface-hover dark:hover:surface-hover'
                    )}
                    onClick={item.isOther ? undefined : () => onRankingClick?.(item)}
                  >
                    <td className="py-1.5">
                      <div className="flex min-w-0 items-center gap-2">
                        <span className="shrink-0 text-[11px] font-semibold surface-muted">
                          {item.isOther ? 'Σ' : `#${index + 1}`}
                        </span>
                        <span
                          className="block max-w-[140px] truncate font-medium text-gray-900 dark:text-white"
                          title={getRankingRowLabel(item)}
                        >
                          {getRankingRowLabel(item)}
                        </span>
                      </div>
                    </td>
                    <td className="py-1.5 text-right surface-muted">{formatNumber(item.requests)}</td>
                    <td className="py-1.5 text-right surface-muted">
                      {formatTokens(item.tokens)}{' '}
                      {rankingTokenShare(item.tokens) && (
                        <span className="surface-muted">({rankingTokenShare(item.tokens)})</span>
                      )}
                    </td>
                    <td className="py-1.5 text-right text-green-600 dark:text-green-400">
                      ${formatCost(item.actual_cost)}
                    </td>
                  </tr>
                ))}
              </tbody>
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
