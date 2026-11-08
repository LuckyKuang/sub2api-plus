import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { usageAPI, type UserDashboardStats as UserStatsType } from '@/api/usage'
import { getMyPlatformQuotas } from '@/api/user'
import { Select } from '@/components/ui'
import { DateRangePicker } from '@/components/DateRangePicker'
import { formatDateLocalInput } from '@/utils/format'
import { supportRequestGeneration } from '@/utils/adminSupportContext'
import { RefreshControl } from './RefreshControl'
import { UserCardScroll } from './UserCardScroll'
import { useAutoRefresh } from './useAutoRefresh'
import { useColumnPreferenceKey } from './useColumnPreferenceKey'
import { useUserView } from './useUserView'
import { UserDashboardCharts } from './dashboard/UserDashboardCharts'
import { UserDashboardRecentUsage } from './dashboard/UserDashboardRecentUsage'
import { UserDashboardStats } from './dashboard/UserDashboardStats'
import type { UsageLog, TrendDataPoint, ModelStat, PlatformQuotaItem } from '@/types'

export default function DashboardView() {
  const { t } = useTranslation()
  const authStore = useUserView()
  const user = authStore.user

  const [stats, setStats] = useState<UserStatsType | null>(null)
  const [loading, setLoading] = useState(false)
  const [loadingUsage, setLoadingUsage] = useState(false)
  const [loadingCharts, setLoadingCharts] = useState(false)
  const [trendData, setTrendData] = useState<TrendDataPoint[]>([])
  const [modelStats, setModelStats] = useState<ModelStat[]>([])
  const [recentUsage, setRecentUsage] = useState<UsageLog[]>([])
  const [platformQuotas, setPlatformQuotas] = useState<PlatformQuotaItem[] | null>(null)

  const [startDate, setStartDate] = useState(formatDateLocalInput(new Date(Date.now() - 6 * 86400000)))
  const [endDate, setEndDate] = useState(formatDateLocalInput(new Date()))
  const [granularity, setGranularity] = useState('day')

  const disposed = useRef(false)
  const chartSeq = useRef(0)
  const recentSeq = useRef(0)
  const chartUpdatedAt = useRef(0)
  const forceCharts = useRef(true)
  const scope = supportRequestGeneration()
  const loadingChartsRef = useRef(false)
  loadingChartsRef.current = loadingCharts

  const current = () => !disposed.current && scope === supportRequestGeneration()

  const granularityOptions = [
    { value: 'day', label: t('dashboard.day') },
    { value: 'hour', label: t('dashboard.hour') }
  ]

  const loadStats = async () => {
    setLoading(true)
    try {
      const data = await usageAPI.getDashboardStats()
      if (current()) setStats(data)
      await authStore.refreshUser()
    } finally {
      if (current()) setLoading(false)
    }
  }

  const loadCharts = async () => {
    const seq = ++chartSeq.current
    setLoadingCharts(true)
    try {
      const params = { start_date: startDate, end_date: endDate }
      const [trend, models] = await Promise.all([
        usageAPI.getDashboardTrend({ ...params, granularity: granularity as 'day' | 'hour' }),
        usageAPI.getDashboardModels(params)
      ])
      if (!current() || seq !== chartSeq.current) return
      setTrendData(trend.trend || [])
      setModelStats(models.models || [])
      chartUpdatedAt.current = Date.now()
    } finally {
      if (current() && seq === chartSeq.current) setLoadingCharts(false)
    }
  }

  const loadRecent = async () => {
    const seq = ++recentSeq.current
    setLoadingUsage(true)
    try {
      const data = await usageAPI.getByDateRange(startDate, endDate)
      if (current() && seq === recentSeq.current) setRecentUsage(data.items.slice(0, 5))
    } finally {
      if (current() && seq === recentSeq.current) setLoadingUsage(false)
    }
  }

  const loadPlatformQuotas = async () => {
    const data = await getMyPlatformQuotas()
    if (current()) setPlatformQuotas(data.platform_quotas ?? [])
  }

  const overviewRefresh = useAutoRefresh({
    storageKey: useColumnPreferenceKey('user-overview-refresh'),
    intervals: [30, 60, 120],
    defaultInterval: 30,
    defaultEnabled: true,
    shouldPause: () => loadingChartsRef.current,
    onRefresh: async () => {
      const includeCharts = forceCharts.current || Date.now() - chartUpdatedAt.current >= 60000
      forceCharts.current = false
      const results = await Promise.allSettled([
        loadStats(),
        loadRecent(),
        loadPlatformQuotas(),
        ...(includeCharts ? [loadCharts()] : [])
      ])
      const failure = results.find((result) => result.status === 'rejected')
      if (failure?.status === 'rejected') throw failure.reason
    }
  })

  const refreshAll = () => {
    forceCharts.current = true
    return overviewRefresh.refresh()
  }

  const changeCharts = async () => {
    try {
      await Promise.all([loadCharts(), loadRecent()])
    } catch (error) {
      overviewRefresh.reportError(error)
    }
  }

  useEffect(() => {
    void refreshAll()
    return () => {
      disposed.current = true
      chartSeq.current += 1
      recentSeq.current += 1
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="workspace-page user-dashboard">
      <div className="card workspace-fixed dashboard-toolbar flex flex-wrap items-center gap-3 p-4">
        <DateRangePicker
          startDate={startDate}
          endDate={endDate}
          onStartDateChange={setStartDate}
          onEndDateChange={setEndDate}
          onChange={() => void changeCharts()}
        />
        <div className="flex items-center gap-2">
          <span className="text-sm surface-muted">{t('dashboard.granularity')}</span>
          <Select
            value={granularity}
            className="w-28"
            searchable={false}
            options={granularityOptions}
            onValueChange={(value) => {
              setGranularity(String(value))
              void changeCharts()
            }}
          />
        </div>
        <div className="dashboard-refresh flex flex-wrap items-center gap-3 text-xs surface-muted">
          {stats && <span>{t('inspection.timezone', { zone: stats.timezone || 'UTC' })}</span>}
          <RefreshControl state={overviewRefresh} />
          {!stats && Boolean(overviewRefresh.error) && (
            <button className="btn btn-secondary" onClick={() => void refreshAll()}>
              {t('inspection.retry')}
            </button>
          )}
        </div>
      </div>
      <UserCardScroll className="dashboard-activity" data-testid="dashboard-activity">
        {loading && !stats ? (
          <div className="flex items-center justify-center py-12">
            <span className="spinner h-6 w-6 text-primary-500" aria-hidden="true" />
          </div>
        ) : stats ? (
          <>
            <UserDashboardStats
              workspace
              section="summary"
              stats={stats}
              balance={user?.balance || 0}
              frozenBalance={user?.frozen_balance}
              isSimple={authStore.isSimpleMode}
              platformQuotas={platformQuotas}
            />
            <section className="dashboard-section" data-testid="dashboard-charts-panel">
              <UserDashboardCharts
                workspace
                showToolbar={false}
                startDate={startDate}
                endDate={endDate}
                granularity={granularity}
                loading={loadingCharts}
                trend={trendData}
                models={modelStats}
                onStartDateChange={setStartDate}
                onEndDateChange={setEndDate}
                onGranularityChange={setGranularity}
                onDateRangeChange={() => void changeCharts()}
                onGranularityChanged={() => void changeCharts()}
                onRefresh={() => void refreshAll()}
              />
            </section>
            <section className="dashboard-section user-dashboard-details" data-testid="dashboard-quotas-panel">
              <h2 className="text-base font-semibold">{t('workspace.platformsQuotas')}</h2>
              <UserDashboardStats
                section="details"
                stats={stats}
                balance={user?.balance || 0}
                frozenBalance={user?.frozen_balance}
                isSimple={authStore.isSimpleMode}
                platformQuotas={platformQuotas}
              />
            </section>
            <section className="dashboard-section" data-testid="dashboard-recent-panel">
              <UserDashboardRecentUsage
                className="workspace-shell"
                data={recentUsage}
                loading={loadingUsage}
                startDate={startDate}
                endDate={endDate}
              />
            </section>
          </>
        ) : null}
      </UserCardScroll>
    </div>
  )
}
