import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { Button, Select, toast } from '@/components/ui'
import { dashboardAPI } from '@/api/admin/dashboard'
import type {
  DashboardStats,
  ModelStat,
  TrendDataPoint,
  UserSpendingRankingItem,
  UserUsageTrendPoint
} from '@/types'
import { chartTooltipStyle } from '@/utils/chartTooltip'
import { formatTokenShare, sumTokenBuckets } from '@/utils/tokenShare'
import { useAutoRefresh } from '@/views/user/useAutoRefresh'
import { Icon } from '@/views/admin/AccountIcon'
import {
  useColumnPreferenceKey,
  useDocumentDarkMode,
  useMediaQuery
} from '@/views/admin/shared'
import { DateRangePicker } from './parts/DateRangePicker'
import { ModelDistributionChart } from './parts/ModelDistributionChart'
import { RefreshControl } from './parts/RefreshControl'
import { TokenUsageTrend } from './parts/TokenUsageTrend'
import { WorkspaceTabs } from './parts/WorkspaceTabs'
import { ChartCanvas } from './parts/ChartCanvas'

const rankingLimit = 12

function formatTokens(value: number | undefined | null): string {
  if (value === undefined || value === null) return '0'
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(2)}K`
  return value.toLocaleString()
}

function toFiniteNumber(value: unknown): number {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : 0
}

function formatNumber(value: number | null | undefined): string {
  return toFiniteNumber(value).toLocaleString()
}

function formatCost(value: number | null | undefined): string {
  const safeValue = toFiniteNumber(value)
  if (safeValue >= 1000) return (safeValue / 1000).toFixed(2) + 'K'
  if (safeValue >= 1) return safeValue.toFixed(2)
  if (safeValue >= 0.01) return safeValue.toFixed(3)
  return safeValue.toFixed(4)
}

function formatDuration(ms: number): string {
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)}s`
  return `${Math.round(ms)}ms`
}

function formatLocalDate(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

function getLast24HoursRangeDates(): { start: string; end: string } {
  const end = new Date()
  const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
  return { start: formatLocalDate(start), end: formatLocalDate(end) }
}

export default function DashboardView() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const isDarkMode = useDocumentDarkMode()
  const compactCharts = useMediaQuery('(min-width: 1024px) and (max-height: 959px)')

  const [chartTab, setChartTab] = useState('models')
  const [stats, setStats] = useState<DashboardStats | null>(null)
  const [loading, setLoading] = useState(false)
  const [chartsLoading, setChartsLoading] = useState(false)
  const [userTrendLoading, setUserTrendLoading] = useState(false)
  const [rankingLoading, setRankingLoading] = useState(false)
  const [rankingError, setRankingError] = useState(false)

  const [trendData, setTrendData] = useState<TrendDataPoint[]>([])
  const [modelStats, setModelStats] = useState<ModelStat[]>([])
  const [userTrend, setUserTrend] = useState<UserUsageTrendPoint[]>([])
  const [userTrendMetric, setUserTrendMetric] = useState<'tokens' | 'actual_cost'>('tokens')
  const [rankingItems, setRankingItems] = useState<UserSpendingRankingItem[]>([])
  const [rankingTotalActualCost, setRankingTotalActualCost] = useState(0)
  const [rankingTotalRequests, setRankingTotalRequests] = useState(0)
  const [rankingTotalTokens, setRankingTotalTokens] = useState(0)

  const defaultRange = useMemo(() => getLast24HoursRangeDates(), [])
  const [startDate, setStartDate] = useState(defaultRange.start)
  const [endDate, setEndDate] = useState(defaultRange.end)
  const [granularity, setGranularity] = useState<'day' | 'hour'>('hour')

  const chartLoadSeq = useRef(0)
  const usersTrendLoadSeq = useRef(0)
  const rankingLoadSeq = useRef(0)
  const chartUpdatedAt = useRef(0)
  const forceCharts = useRef(false)

  const granularityOptions = [
    { value: 'day', label: t('admin.dashboard.day') },
    { value: 'hour', label: t('admin.dashboard.hour') }
  ]

  const dashboardTabs = [
    { value: 'models', label: t('workspace.modelsTrend') },
    { value: 'users', label: t('workspace.userTrend') }
  ]

  const todayCacheTokens = sumTokenBuckets(
    stats?.today_cache_creation_tokens,
    stats?.today_cache_read_tokens
  )

  const totalCacheTokens = sumTokenBuckets(
    stats?.total_cache_creation_tokens,
    stats?.total_cache_read_tokens
  )

  const todayTokenShare = (tokens: number | null | undefined): string =>
    formatTokenShare(tokens, stats?.today_tokens) ?? '0.0%'

  const totalTokenShare = (tokens: number | null | undefined): string =>
    formatTokenShare(tokens, stats?.total_tokens) ?? '0.0%'

  const loadDashboardSnapshot = useCallback(
    async (includeStats: boolean, includeCharts = true, quiet = false) => {
      const currentSeq = ++chartLoadSeq.current
      if (includeStats && !stats) setLoading(true)
      setChartsLoading(includeCharts)
      try {
        const response = await dashboardAPI.getSnapshotV2({
          start_date: startDate,
          end_date: endDate,
          granularity,
          include_stats: includeStats,
          include_trend: includeCharts,
          include_model_stats: includeCharts,
          include_group_stats: false,
          include_users_trend: false
        })
        if (currentSeq !== chartLoadSeq.current) return
        if (includeStats && response.stats) setStats(response.stats)
        if (includeCharts) {
          setTrendData(response.trend || [])
          setModelStats(response.models || [])
        }
      } catch (error) {
        if (currentSeq !== chartLoadSeq.current) return
        if (quiet) throw error
        toast.error(t('admin.dashboard.failedToLoad'))
        console.error('Error loading dashboard snapshot:', error)
      } finally {
        if (currentSeq === chartLoadSeq.current) {
          setLoading(false)
          setChartsLoading(false)
        }
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [startDate, endDate, granularity]
  )

  const loadUsersTrend = useCallback(
    async (quiet = false) => {
      const currentSeq = ++usersTrendLoadSeq.current
      setUserTrendLoading(true)
      try {
        const response = await dashboardAPI.getUserUsageTrend({
          start_date: startDate,
          end_date: endDate,
          granularity,
          limit: 12,
          metric: userTrendMetric
        })
        if (currentSeq !== usersTrendLoadSeq.current) return
        setUserTrend(response.trend || [])
      } catch (error) {
        if (currentSeq !== usersTrendLoadSeq.current) return
        console.error('Error loading users trend:', error)
        if (quiet) throw error
      } finally {
        if (currentSeq === usersTrendLoadSeq.current) setUserTrendLoading(false)
      }
    },
     
    [startDate, endDate, granularity, userTrendMetric]
  )

  const loadUserSpendingRanking = useCallback(
    async (quiet = false) => {
      const currentSeq = ++rankingLoadSeq.current
      setRankingLoading(true)
      setRankingError(false)
      try {
        const response = await dashboardAPI.getUserSpendingRanking({
          start_date: startDate,
          end_date: endDate,
          limit: rankingLimit
        })
        if (currentSeq !== rankingLoadSeq.current) return
        setRankingItems(response.ranking || [])
        setRankingTotalActualCost(response.total_actual_cost || 0)
        setRankingTotalRequests(response.total_requests || 0)
        setRankingTotalTokens(response.total_tokens || 0)
      } catch (error) {
        if (currentSeq !== rankingLoadSeq.current) return
        console.error('Error loading user spending ranking:', error)
        setRankingError(true)
        if (quiet) throw error
      } finally {
        if (currentSeq === rankingLoadSeq.current) setRankingLoading(false)
      }
    },
     
    [startDate, endDate]
  )

  const loadChartData = useCallback(async () => {
    await Promise.all([loadDashboardSnapshot(false), loadUsersTrend(), loadUserSpendingRanking()])
  }, [loadDashboardSnapshot, loadUsersTrend, loadUserSpendingRanking])

  const storageKey = useColumnPreferenceKey('admin-overview-refresh')

  const overviewRefresh = useAutoRefresh({
    storageKey,
    intervals: [30, 60, 120],
    defaultInterval: 30,
    defaultEnabled: true,
    shouldPause: () => chartsLoading,
    onRefresh: async () => {
      const includeCharts = forceCharts.current || Date.now() - chartUpdatedAt.current >= 60000
      forceCharts.current = false
      const results = await Promise.allSettled([
        loadDashboardSnapshot(true, includeCharts, true),
        ...(includeCharts ? [loadUsersTrend(true), loadUserSpendingRanking(true)] : [])
      ])
      const failure = results.find((result) => result.status === 'rejected')
      if (failure?.status === 'rejected') throw failure.reason
      if (includeCharts) chartUpdatedAt.current = Date.now()
    }
  })

  const refreshAll = useCallback(() => {
    forceCharts.current = true
    return overviewRefresh.refresh()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [overviewRefresh.refresh])

  useEffect(() => {
    void refreshAll()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const chartColors = {
    text: isDarkMode ? '#e5e5e5' : '#525252',
    grid: isDarkMode ? '#404040' : '#e5e5e5'
  }

  const formatUserTrendValue = (value: number): string =>
    userTrendMetric === 'tokens' ? formatTokens(value) : `$${formatCost(value)}`

  const lineOptions = useMemo(
    () => ({
      animation: false as const,
      responsive: true,
      maintainAspectRatio: false,
      interaction: { intersect: false, mode: 'index' as const },
      plugins: {
        legend: {
          position: 'top' as const,
          labels: {
            color: chartColors.text,
            usePointStyle: true,
            pointStyle: 'circle',
            padding: 15,
            font: { size: 11 }
          }
        },
        tooltip: {
          ...chartTooltipStyle(isDarkMode),
          itemSort: (a: { raw?: unknown; parsed?: { y?: number } }, b: { raw?: unknown; parsed?: { y?: number } }) => {
            const aValue = typeof a?.raw === 'number' ? a.raw : Number(a?.parsed?.y ?? 0)
            const bValue = typeof b?.raw === 'number' ? b.raw : Number(b?.parsed?.y ?? 0)
            return bValue - aValue
          },
          callbacks: {
            label: (context: { raw: unknown }) => `${String(context.raw)}: ${formatUserTrendValue(Number(context.raw))}`
          }
        }
      },
      scales: {
        x: {
          grid: { color: chartColors.grid },
          ticks: { color: chartColors.text, font: { size: 10 } }
        },
        y: {
          grid: { color: chartColors.grid },
          ticks: { color: chartColors.text, font: { size: 10 }, callback: (value: string | number) => formatUserTrendValue(Number(value)) }
        }
      }
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [isDarkMode, userTrendMetric]
  )

  const userTrendChartData = useMemo(() => {
    if (!userTrend?.length) return null

    const getDisplayName = (point: UserUsageTrendPoint): string => {
      const username = point.username?.trim()
      if (username) return username

      const email = point.email?.trim()
      if (email) return email

      return t('admin.redeem.userPrefix', { id: point.user_id })
    }

    // Group by user_id to avoid merging different users with the same display name
    const userGroups = new Map<number, { name: string; data: Map<string, number> }>()
    const allDates = new Set<string>()

    userTrend.forEach((point) => {
      allDates.add(point.date)
      const key = point.user_id
      if (!userGroups.has(key)) {
        userGroups.set(key, { name: getDisplayName(point), data: new Map() })
      }
      userGroups.get(key)!.data.set(point.date, userTrendMetric === 'tokens' ? point.tokens : point.actual_cost)
    })

    const sortedDates = Array.from(allDates).sort()
    const colors = [
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

    const datasets = Array.from(userGroups.values()).map((group, idx) => ({
      label: group.name,
      data: sortedDates.map((date) => group.data.get(date) || 0),
      borderColor: colors[idx % colors.length],
      backgroundColor: `${colors[idx % colors.length]}20`,
      fill: false,
      tension: 0.3
    }))

    return { labels: sortedDates, datasets }
  }, [userTrend, userTrendMetric, t])

  function onDateRangeChange(range: { startDate: string; endDate: string; preset: string | null }) {
    // Auto-select granularity based on date range
    const start = new Date(range.startDate)
    const end = new Date(range.endDate)
    const daysDiff = Math.ceil((end.getTime() - start.getTime()) / (1000 * 60 * 60 * 24))

    // If range is 1 day, use hourly granularity
    setGranularity(daysDiff <= 1 ? 'hour' : 'day')

    void loadChartData()
  }

  function goToUserUsage(item: UserSpendingRankingItem) {
    void navigate({
      pathname: '/admin/usage',
      search: `?user_id=${item.user_id}&start_date=${startDate}&end_date=${endDate}`
    })
  }

  return (
    <div className="workspace-page admin-dashboard">
      <div className="card workspace-fixed dashboard-toolbar p-4">
        <div className="flex flex-wrap items-center gap-4">
          <div className="flex min-w-0 max-w-full flex-wrap items-center gap-2">
            <span className="text-sm font-medium surface-muted">{t('admin.dashboard.timeRange')}:</span>
            <DateRangePicker
              className="min-w-0 max-w-full"
              startDate={startDate}
              endDate={endDate}
              onStartDateChange={setStartDate}
              onEndDateChange={setEndDate}
              onChange={onDateRangeChange}
            />
          </div>
          <Button variant="secondary" onClick={() => void refreshAll()} disabled={chartsLoading || overviewRefresh.fetching}>
            {t('common.refresh')}
          </Button>
          <div className="flex w-full min-w-0 flex-wrap items-center gap-2 sm:ml-auto sm:w-auto">
            <span className="text-sm font-medium surface-muted">{t('admin.dashboard.granularity')}:</span>
            <div className="w-28 min-w-0 max-w-full">
              <Select
                value={granularity}
                options={granularityOptions}
                searchable={false}
                onValueChange={(next) => {
                  setGranularity(next as 'day' | 'hour')
                  void loadChartData()
                }}
              />
            </div>
          </div>
          <div className="dashboard-refresh flex flex-wrap items-center gap-3">
            <RefreshControl state={overviewRefresh} />
            {!stats && overviewRefresh.error != null && (
              <Button variant="secondary" onClick={() => void refreshAll()}>
                {t('inspection.retry')}
              </Button>
            )}
          </div>
        </div>
      </div>

      {loading && !stats ? (
        <div className="dashboard-kpis workspace-fixed flex flex-col gap-4">
          <div className="card p-5">
            <div className="skeleton h-3 w-24" />
            <div className="skeleton mt-3 h-9 w-44" />
            <div className="skeleton mt-3 h-3 w-64" />
          </div>
          <div className="grid grid-cols-2 gap-4 md:grid-cols-3">
            {Array.from({ length: 6 }).map((_, index) => (
              <div key={index} className="card p-4">
                <div className="skeleton h-3 w-16" />
                <div className="skeleton mt-3 h-6 w-20" />
                <div className="skeleton mt-2 h-3 w-24" />
              </div>
            ))}
          </div>
        </div>
      ) : stats ? (
        <>
          <div className="dashboard-kpis workspace-fixed flex flex-col gap-4">
            <div className="dashboard-hero card p-5">
              <div className="flex flex-wrap items-start justify-between gap-6">
                <div className="min-w-0">
                  <p className="text-xs font-medium surface-muted">{t('admin.dashboard.todayTokens')}</p>
                  <p className="mt-1 text-3xl font-bold tracking-tight text-gray-900 dark:text-white">
                    {formatTokens(stats.today_tokens)}
                  </p>
                  <p className="mt-1 flex flex-wrap gap-x-1.5 text-xs surface-muted">
                    <span>
                      {t('dashboard.input')} {todayTokenShare(stats.today_input_tokens)}
                    </span>
                    <span aria-hidden="true">·</span>
                    <span>
                      {t('dashboard.output')} {todayTokenShare(stats.today_output_tokens)}
                    </span>
                    {todayCacheTokens > 0 && (
                      <>
                        <span aria-hidden="true">·</span>
                        <span>
                          {t('dashboard.cache')} {todayTokenShare(todayCacheTokens)}
                        </span>
                      </>
                    )}
                  </p>
                </div>
                <div className="shrink-0 text-right">
                  <p className="text-xs font-medium surface-muted">{t('admin.dashboard.todayRequests')}</p>
                  <p className="mt-1 text-2xl font-bold text-gray-900 dark:text-white">
                    {formatNumber(stats.today_requests)}
                  </p>
                  <p className="mt-1 text-xs surface-muted">
                    {t('common.total')}: {formatNumber(stats.total_requests)}
                  </p>
                </div>
              </div>
              <div className="mt-4 flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-gray-100 pt-3 text-xs dark:border-dark-700">
                <span className="surface-muted">{t('admin.dashboard.actual')}</span>
                <span className="font-semibold text-green-600 dark:text-green-400">${formatCost(stats.today_actual_cost)}</span>
                <span className="surface-muted" aria-hidden="true">
                  /
                </span>
                <span className="surface-muted">{t('admin.dashboard.accountCost')}</span>
                <span className="font-medium text-orange-500 dark:text-orange-400">
                  ${formatCost(stats.today_account_cost)}
                </span>
                <span className="surface-muted" aria-hidden="true">
                  /
                </span>
                <span className="surface-muted">{t('admin.dashboard.standard')}</span>
                <span className="font-medium surface-muted">${formatCost(stats.today_cost)}</span>
              </div>
            </div>

            <div className="dashboard-kpi-grid grid grid-cols-2 gap-4 md:grid-cols-3">
              <div className="card p-4">
                <div className="flex items-center gap-3">
                  <div className="rounded-lg surface-inset p-2">
                    <Icon name="key" size="md" className="surface-muted" strokeWidth={2} />
                  </div>
                  <div>
                    <p className="text-xs font-medium surface-muted">{t('admin.dashboard.apiKeys')}</p>
                    <p className="text-xl font-bold text-gray-900 dark:text-white">{stats.total_api_keys}</p>
                    <p className="text-xs text-green-600 dark:text-green-400">
                      {stats.active_api_keys} {t('common.active')}
                    </p>
                  </div>
                </div>
              </div>

              <div className="card p-4">
                <div className="flex items-center gap-3">
                  <div className="rounded-lg surface-inset p-2">
                    <Icon name="server" size="md" className="surface-muted" strokeWidth={2} />
                  </div>
                  <div>
                    <p className="text-xs font-medium surface-muted">{t('admin.dashboard.accounts')}</p>
                    <p className="text-xl font-bold text-gray-900 dark:text-white">{stats.total_accounts}</p>
                    <p className="text-xs">
                      <span className="text-green-600 dark:text-green-400">
                        {stats.normal_accounts} {t('common.active')}
                      </span>
                      {stats.error_accounts > 0 && (
                        <span className="ml-1 text-red-500">
                          {stats.error_accounts} {t('common.error')}
                        </span>
                      )}
                    </p>
                  </div>
                </div>
              </div>

              <div className="card p-4">
                <div className="flex items-center gap-3">
                  <div className="rounded-lg surface-inset p-2">
                    <Icon name="userPlus" size="md" className="surface-muted" strokeWidth={2} />
                  </div>
                  <div>
                    <p className="text-xs font-medium surface-muted">{t('admin.dashboard.users')}</p>
                    <p className="text-xl font-bold text-emerald-600 dark:text-emerald-400">+{stats.today_new_users}</p>
                    <p className="text-xs surface-muted">
                      {t('common.total')}: {formatNumber(stats.total_users)}
                    </p>
                  </div>
                </div>
              </div>

              <div className="card p-4">
                <div className="flex items-center gap-3">
                  <div className="rounded-lg surface-inset p-2">
                    <Icon name="database" size="md" className="surface-muted" strokeWidth={2} />
                  </div>
                  <div>
                    <p className="text-xs font-medium surface-muted">{t('admin.dashboard.totalTokens')}</p>
                    <p className="text-xl font-bold text-gray-900 dark:text-white">{formatTokens(stats.total_tokens)}</p>
                    <p className="flex flex-wrap gap-x-1 text-xs surface-muted">
                      <span>
                        {t('dashboard.input')} {totalTokenShare(stats.total_input_tokens)}
                      </span>
                      <span>&middot;</span>
                      <span>
                        {t('dashboard.output')} {totalTokenShare(stats.total_output_tokens)}
                      </span>
                      {totalCacheTokens > 0 && (
                        <span>
                          &middot; {t('dashboard.cache')} {totalTokenShare(totalCacheTokens)}
                        </span>
                      )}
                    </p>
                    <p className="text-xs">
                      <span className="text-green-600 dark:text-green-400" title={t('admin.dashboard.actual')}>
                        ${formatCost(stats.total_actual_cost)}
                      </span>
                      <span className="surface-muted"> / </span>
                      <span className="text-orange-500 dark:text-orange-400" title={t('admin.dashboard.accountCost')}>
                        ${formatCost(stats.total_account_cost)}
                      </span>
                      <span className="surface-muted"> / </span>
                      <span className="surface-muted" title={t('admin.dashboard.standard')}>
                        ${formatCost(stats.total_cost)}
                      </span>
                    </p>
                  </div>
                </div>
              </div>

              <div className="card p-4">
                <div className="flex items-center gap-3">
                  <div className="rounded-lg surface-inset p-2">
                    <Icon name="bolt" size="md" className="surface-muted" strokeWidth={2} />
                  </div>
                  <div className="flex-1">
                    <p className="text-xs font-medium surface-muted">{t('admin.dashboard.performance')}</p>
                    <div className="flex items-baseline gap-2">
                      <p className="text-xl font-bold text-gray-900 dark:text-white">{formatTokens(stats.rpm)}</p>
                      <span className="text-xs surface-muted">RPM</span>
                    </div>
                    <div className="flex items-baseline gap-2">
                      <p className="text-sm font-semibold text-violet-600 dark:text-violet-400">{formatTokens(stats.tpm)}</p>
                      <span className="text-xs surface-muted">TPM</span>
                    </div>
                  </div>
                </div>
              </div>

              <div className="card p-4">
                <div className="flex items-center gap-3">
                  <div className="rounded-lg surface-inset p-2">
                    <Icon name="clock" size="md" className="surface-muted" strokeWidth={2} />
                  </div>
                  <div>
                    <p className="text-xs font-medium surface-muted">{t('admin.dashboard.avgResponse')}</p>
                    <p className="text-xl font-bold text-gray-900 dark:text-white">
                      {formatDuration(stats.average_duration_ms)}
                    </p>
                    <p className="text-xs surface-muted">
                      {stats.active_users} {t('admin.dashboard.activeUsers')}
                    </p>
                  </div>
                </div>
              </div>
            </div>
          </div>

          {compactCharts && (
            <WorkspaceTabs value={chartTab} tabs={dashboardTabs} label={t('workspace.charts')} onValueChange={setChartTab} />
          )}
          <div className={`workspace-body dashboard-charts ${compactCharts ? 'dashboard-charts-compact' : ''}`}>
            <div
              className={`dashboard-chart-grid grid grid-cols-1 gap-6 lg:grid-cols-2 ${
                compactCharts && chartTab !== 'models' ? 'hidden' : ''
              }`}
            >
              <ModelDistributionChart
                workspace
                modelStats={modelStats}
                enableRankingView
                rankingItems={rankingItems}
                rankingTotalActualCost={rankingTotalActualCost}
                rankingTotalRequests={rankingTotalRequests}
                rankingTotalTokens={rankingTotalTokens}
                loading={chartsLoading}
                rankingLoading={rankingLoading}
                rankingError={rankingError}
                startDate={startDate}
                endDate={endDate}
                onRankingClick={goToUserUsage}
              />
              <TokenUsageTrend workspace trendData={trendData} loading={chartsLoading} />
            </div>

            <div className={`card dashboard-user-trend p-4 ${compactCharts && chartTab !== 'users' ? 'hidden' : ''}`}>
              <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
                <h3 className="text-sm font-semibold text-gray-900 dark:text-white">
                  {t('admin.dashboard.recentUsage')} (Top 12)
                </h3>
                <div className="segmented-control" role="group" aria-label={t('admin.dashboard.recentUsage')}>
                  {(['tokens', 'actual_cost'] as const).map((metric) => (
                    <button
                      key={metric}
                      type="button"
                      className="segmented-option"
                      aria-pressed={userTrendMetric === metric}
                      onClick={() => {
                        if (userTrendMetric === metric) return
                        setUserTrendMetric(metric)
                        setUserTrend([])
                        void loadUsersTrend()
                      }}
                    >
                      {t(metric === 'tokens' ? 'admin.dashboard.tokens' : 'admin.dashboard.actualSpending')}
                    </button>
                  ))}
                </div>
              </div>
              <div className="dashboard-line-canvas h-64">
                {userTrendLoading && !userTrendChartData ? (
                  <div className="flex h-full items-center justify-center">
                    <span className="spinner h-6 w-6 text-primary-500" aria-hidden="true" />
                  </div>
                ) : userTrendChartData ? (
                  <ChartCanvas kind="line" data={userTrendChartData as never} options={lineOptions as never} />
                ) : (
                  <div className="flex h-full items-center justify-center">
                    <p className="text-sm surface-muted">{t('admin.dashboard.noDataAvailable')}</p>
                  </div>
                )}
              </div>
            </div>
          </div>
        </>
      ) : (
        <div className="flex flex-1 items-center justify-center p-8">
          <div className="empty-state">
            <h3 className="empty-state-title">{t('common.noData')}</h3>
            <div className="mt-4">
              <Button variant="secondary" onClick={() => void refreshAll()}>
                {t('common.refresh')}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
