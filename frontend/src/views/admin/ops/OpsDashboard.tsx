import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Button, Modal, Pagination, Toggle, toast } from '@/components/ui'
import { opsAPI } from '@/api/admin/ops'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import { useProductAppearance } from '@/components/auth/useProductAppearance'
import { formatDateTime } from '@/utils/format'
import { OpsDashboardHeader } from './components/OpsDashboardHeader'
import { OpsDashboardSkeleton } from './components/OpsDashboardSkeleton'
import { OpsErrorDistributionChart } from './components/OpsErrorDistributionChart'
import { OpsErrorTrendChart } from './components/OpsErrorTrendChart'
import { OpsLatencyChart } from './components/OpsLatencyChart'
import { OpsSwitchRateTrendChart } from './components/OpsSwitchRateTrendChart'
import { OpsThroughputTrendChart } from './components/OpsThroughputTrendChart'
import { OpsConcurrencyCard } from './components/OpsConcurrencyCard'
import { OpsAlertEventsCard } from './components/OpsAlertEventsCard'
import { OpsSystemLogTable } from './components/OpsSystemLogTable'
import { WorkspaceTabs } from '../parts/WorkspaceTabs'

type TimeRange = '5m' | '30m' | '1h' | '6h' | '24h' | 'custom'
const allowedTimeRanges = new Set<TimeRange>(['5m', '30m', '1h', '6h', '24h', 'custom'])

type QueryMode = 'auto' | 'raw' | 'preagg'
const allowedQueryModes = new Set<QueryMode>(['auto', 'raw', 'preagg'])

const QUERY_KEYS = {
  timeRange: 'tr',
  platform: 'platform',
  groupId: 'group_id',
  queryMode: 'mode',
  fullscreen: 'fullscreen',

  // Deep links
  openErrorDetails: 'open_error_details',
  errorType: 'error_type',
  alertRuleId: 'alert_rule_id',
  openAlertRules: 'open_alert_rules'
} as const

const switchTrendWindowHours = 5
const switchTrendTimeRange = `${switchTrendWindowHours}h`
const switchTrendWindowMs = switchTrendWindowHours * 60 * 60 * 1000

/** Generic list dialog shared by the request-details and error-details entry points. */
function OpsListDialog({
  open,
  title,
  fetchPage
}: {
  open: boolean
  title: string
  fetchPage: (page: number, pageSize: number) => Promise<{ items: unknown[]; total: number }>
}) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [pageSize] = useState(20)
  const [items, setItems] = useState<unknown[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)

  const fetchPageRef = useRef(fetchPage)
  fetchPageRef.current = fetchPage

  useEffect(() => {
    if (!open) return
    let cancelled = false
    setLoading(true)
    void (async () => {
      try {
        const res = await fetchPageRef.current(page, pageSize)
        if (cancelled) return
        setItems(res.items || [])
        setTotal(res.total || 0)
      } catch (error) {
        if (!cancelled) toast.error((error as { message?: string })?.message || t('common.error'))
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [open, page, pageSize, t])

  return (
    <Modal
      open={open}
      onClose={() => setPage(1)}
      title={title}
      size="xl"
      footer={
        total > 0 ? (
          <Pagination
            page={page}
            total={total}
            pageSize={pageSize}
            onPageChange={(next) => setPage(next)}
            onPageSizeChange={() => setPage(1)}
          />
        ) : undefined
      }
    >
      {loading ? (
        <div className="py-10 text-center text-sm surface-muted">{t('common.loading')}</div>
      ) : items.length === 0 ? (
        <div className="py-10 text-center text-sm surface-muted">{t('common.noData')}</div>
      ) : (
        <div className="space-y-2">
          {items.map((item, index) => {
            const row = item as Record<string, unknown>
            const label = String(row.path ?? row.model ?? row.error_message ?? row.request_id ?? `#${index + 1}`)
            return (
              <div
                key={index}
                className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-gray-100 surface-inset p-3 dark:border-dark-700"
              >
                <span className="min-w-0 truncate text-sm text-gray-900 dark:text-white">{label}</span>
                <span className="text-xs surface-muted">
                  {row.created_at ? formatDateTime(String(row.created_at)) : ''}
                </span>
              </div>
            )
          })}
        </div>
      )}
    </Modal>
  )
}

function OpsSettingsDialog({ open, onClose, onSaved }: { open: boolean; onClose: () => void; onSaved: () => void }) {
  const { t } = useTranslation()
  const [settings, setSettings] = useState<Awaited<ReturnType<typeof opsAPI.getAdvancedSettings>> | null>(null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    void (async () => {
      try {
        setSettings(await opsAPI.getAdvancedSettings())
      } catch (error) {
        toast.error((error as { message?: string })?.message || t('common.error'))
      }
    })()
  }, [open, t])

  async function save() {
    if (!settings) return
    setSaving(true)
    try {
      await opsAPI.updateAdvancedSettings(settings)
      toast.success(t('common.saved'))
      onSaved()
      onClose()
    } catch (error) {
      toast.error((error as { message?: string })?.message || t('common.error'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={t('admin.ops.settings.title')}
      size="md"
      footer={
        <div className="flex justify-end gap-3">
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" loading={saving} onClick={() => void save()}>
            {saving ? t('common.saving') : t('common.save')}
          </Button>
        </div>
      }
    >
      {settings && (
        <div className="space-y-4">
          <div className="flex items-center justify-between gap-4">
            <span className="text-sm surface-muted">{t('admin.ops.settings.autoRefresh')}</span>
            <Toggle
              checked={settings.auto_refresh_enabled}
              onCheckedChange={(next) => setSettings({ ...settings, auto_refresh_enabled: next })}
            />
          </div>
          <div className="flex items-center justify-between gap-4">
            <span className="text-sm surface-muted">{t('admin.ops.settings.displayAlertEvents')}</span>
            <Toggle
              checked={settings.display_alert_events}
              onCheckedChange={(next) => setSettings({ ...settings, display_alert_events: next })}
            />
          </div>
          <div className="flex items-center justify-between gap-4">
            <span className="text-sm surface-muted">{t('admin.ops.settings.displayOpenAITokenStats')}</span>
            <Toggle
              checked={settings.display_openai_token_stats}
              onCheckedChange={(next) => setSettings({ ...settings, display_openai_token_stats: next })}
            />
          </div>
        </div>
      )}
    </Modal>
  )
}

export default function OpsDashboard() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const opsMonitoringEnabled = useAdminSettingsStore((s) => s.opsMonitoringEnabled)
  const opsQueryModeDefault = useAdminSettingsStore((s) => s.opsQueryModeDefault)
  const fetchAdminSettings = useAdminSettingsStore((s) => s.fetch)

  // Fullscreen replaces AppLayout, so the view must keep owning its palette.
  useProductAppearance()

  const [activeTab, setActiveTab] = useState('overview')
  const workspaceTabs = [
    { value: 'overview', label: t('workspace.opsMonitoring'), panelId: 'ops-panel-overview' },
    { value: 'alerts', label: t('workspace.alerts'), panelId: 'ops-panel-alerts' },
    { value: 'logs', label: t('workspace.systemLogs'), panelId: 'ops-panel-logs' }
  ]

  const opsEnabled = opsMonitoringEnabled

  const [loading, setLoading] = useState(true)
  const [hasLoadedOnce, setHasLoadedOnce] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')
  const [lastUpdated, setLastUpdated] = useState<Date | null>(new Date())

  const [timeRange, setTimeRange] = useState<TimeRange>('1h')
  const [platform, setPlatform] = useState('')
  const [groupId, setGroupId] = useState<number | null>(null)
  const [queryMode, setQueryMode] = useState<QueryMode>('auto')
  const [customStartTime, setCustomStartTime] = useState<string | null>(null)
  const [customEndTime, setCustomEndTime] = useState<string | null>(null)

  type Overview = Awaited<ReturnType<typeof opsAPI.getDashboardOverview>>
  type Trend = Awaited<ReturnType<typeof opsAPI.getThroughputTrend>>

  const [overview, setOverview] = useState<Overview | null>(null)
  const [metricThresholds, setMetricThresholds] = useState<Awaited<ReturnType<typeof opsAPI.getMetricThresholds>> | null>(null)

  const [throughputTrend, setThroughputTrend] = useState<Trend | null>(null)
  const [loadingTrend, setLoadingTrend] = useState(false)

  const [switchTrend, setSwitchTrend] = useState<Trend | null>(null)
  const [loadingSwitchTrend, setLoadingSwitchTrend] = useState(false)

  const [latencyHistogram, setLatencyHistogram] = useState<Awaited<ReturnType<typeof opsAPI.getLatencyHistogram>> | null>(null)
  const [loadingLatency, setLoadingLatency] = useState(false)

  const [errorTrend, setErrorTrend] = useState<Awaited<ReturnType<typeof opsAPI.getErrorTrend>> | null>(null)
  const [loadingErrorTrend, setLoadingErrorTrend] = useState(false)

  const [errorDistribution, setErrorDistribution] = useState<Awaited<ReturnType<typeof opsAPI.getErrorDistribution>> | null>(null)
  const [loadingErrorDistribution, setLoadingErrorDistribution] = useState(false)

  const [showSettingsDialog, setShowSettingsDialog] = useState(false)
  const [showRequestDetails, setShowRequestDetails] = useState(false)
  const [showErrorDetails, setShowErrorDetails] = useState(false)
  const [errorDetailsKind, setErrorDetailsKind] = useState<'request' | 'upstream'>('request')

  const [showAlertEvents, setShowAlertEvents] = useState(true)
  const [autoRefreshEnabled, setAutoRefreshEnabled] = useState(false)
  const [autoRefreshIntervalMs, setAutoRefreshIntervalMs] = useState(30000)
  const [autoRefreshCountdown, setAutoRefreshCountdown] = useState(0)

  // Used to trigger child component refreshes in a single shared cadence.
  const [dashboardRefreshToken, setDashboardRefreshToken] = useState(0)

  const dashboardFetchController = useRef<AbortController | null>(null)
  const dashboardFetchSeq = useRef(0)
  const isApplyingRouteQuery = useRef(false)
  const isSyncingRouteQuery = useRef(false)

  const isFullscreen = searchParams.get(QUERY_KEYS.fullscreen) === '1' || searchParams.get(QUERY_KEYS.fullscreen) === 'true'

  function exitFullscreen() {
    const next = new URLSearchParams(searchParams)
    next.delete(QUERY_KEYS.fullscreen)
    setSearchParams(next, { replace: true })
  }

  function enterFullscreen() {
    const next = new URLSearchParams(searchParams)
    next.set(QUERY_KEYS.fullscreen, '1')
    setSearchParams(next, { replace: true })
  }

  useEffect(() => {
    if (!isFullscreen) return
    const handleKeydown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') exitFullscreen()
    }
    window.addEventListener('keydown', handleKeydown)
    return () => window.removeEventListener('keydown', handleKeydown)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isFullscreen])

  function isCanceledRequest(err: unknown): boolean {
    return (
      !!err &&
      typeof err === 'object' &&
      'code' in err &&
      (err as Record<string, unknown>).code === 'ERR_CANCELED'
    )
  }

  function abortDashboardFetch() {
    if (dashboardFetchController.current) {
      dashboardFetchController.current.abort()
      dashboardFetchController.current = null
    }
  }

  const readQueryString = (key: string): string => searchParams.get(key) ?? ''

  const readQueryNumber = (key: string): number | null => {
    const raw = readQueryString(key)
    if (!raw) return null
    const n = Number.parseInt(raw, 10)
    return Number.isFinite(n) ? n : null
  }

  const applyRouteQueryToState = () => {
    const nextTimeRange = readQueryString(QUERY_KEYS.timeRange)
    if (nextTimeRange && allowedTimeRanges.has(nextTimeRange as TimeRange)) {
      setTimeRange(nextTimeRange as TimeRange)
    }

    setPlatform(readQueryString(QUERY_KEYS.platform) || '')

    const groupIdRaw = readQueryNumber(QUERY_KEYS.groupId)
    setGroupId(typeof groupIdRaw === 'number' && groupIdRaw > 0 ? groupIdRaw : null)

    const nextMode = readQueryString(QUERY_KEYS.queryMode)
    if (nextMode && allowedQueryModes.has(nextMode as QueryMode)) {
      setQueryMode(nextMode as QueryMode)
    } else {
      const fallback = opsQueryModeDefault || 'auto'
      setQueryMode(allowedQueryModes.has(fallback as QueryMode) ? (fallback as QueryMode) : 'auto')
    }
  }

  const buildQueryFromState = () => {
    const next = new URLSearchParams(searchParams)

    Object.values(QUERY_KEYS).forEach((key) => {
      next.delete(key)
    })

    if (timeRange !== '1h') next.set(QUERY_KEYS.timeRange, timeRange)
    if (platform) next.set(QUERY_KEYS.platform, platform)
    if (typeof groupId === 'number' && groupId > 0) next.set(QUERY_KEYS.groupId, String(groupId))
    if (queryMode !== 'auto') next.set(QUERY_KEYS.queryMode, queryMode)

    return next
  }

  const syncQueryToRoute = useCallback(() => {
    if (isApplyingRouteQuery.current) return
    const nextQuery = buildQueryFromState()

    const nextKeys = Array.from(nextQuery.keys())
    const currKeys = Array.from(searchParams.keys())
    const sameLength = nextKeys.length === currKeys.length
    const sameValues =
      sameLength && nextKeys.every((key) => String(searchParams.get(key) ?? '') === String(nextQuery.get(key) ?? ''))
    if (sameValues) return

    isSyncingRouteQuery.current = true
    setSearchParams(nextQuery, { replace: true })
    window.setTimeout(() => {
      isSyncingRouteQuery.current = false
    }, 0)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [timeRange, platform, groupId, queryMode, searchParams])

  const buildApiParams = useCallback(() => {
    const params: Record<string, string | number | undefined> = {
      platform: platform || undefined,
      group_id: groupId ?? undefined,
      mode: queryMode
    }

    if (timeRange === 'custom') {
      if (customStartTime && customEndTime) {
        params.start_time = customStartTime
        params.end_time = customEndTime
      } else {
        // Safety fallback: avoid sending time_range=custom (backend may not support it)
        params.time_range = '1h'
      }
    } else {
      params.time_range = timeRange
    }

    return params
  }, [platform, groupId, queryMode, timeRange, customStartTime, customEndTime])

  const buildSwitchTrendParams = useCallback(() => {
    const params: Record<string, string | number | undefined> = {
      platform: platform || undefined,
      group_id: groupId ?? undefined,
      mode: queryMode
    }
    const endTime = new Date()
    const startTime = new Date(endTime.getTime() - switchTrendWindowMs)
    params.start_time = startTime.toISOString()
    params.end_time = endTime.toISOString()
    return params
  }, [platform, groupId, queryMode])

  async function refreshOverviewWithCancel(fetchSeq: number, signal: AbortSignal) {
    if (!opsEnabled) return
    try {
      const data = await opsAPI.getDashboardOverview(buildApiParams(), { signal })
      if (fetchSeq !== dashboardFetchSeq.current) return
      setOverview(data)
    } catch (err) {
      if (fetchSeq !== dashboardFetchSeq.current || isCanceledRequest(err)) return
      setOverview(null)
      toast.error((err as { message?: string })?.message || t('admin.ops.failedToLoadOverview'))
    }
  }

  async function refreshSwitchTrendWithCancel(fetchSeq: number, signal: AbortSignal) {
    if (!opsEnabled) return
    setLoadingSwitchTrend(true)
    try {
      const data = await opsAPI.getThroughputTrend(buildSwitchTrendParams(), { signal })
      if (fetchSeq !== dashboardFetchSeq.current) return
      setSwitchTrend(data)
    } catch (err) {
      if (fetchSeq !== dashboardFetchSeq.current || isCanceledRequest(err)) return
      setSwitchTrend(null)
      toast.error((err as { message?: string })?.message || t('admin.ops.failedToLoadSwitchTrend'))
    } finally {
      if (fetchSeq === dashboardFetchSeq.current) {
        setLoadingSwitchTrend(false)
      }
    }
  }

  async function refreshThroughputTrendWithCancel(fetchSeq: number, signal: AbortSignal) {
    if (!opsEnabled) return
    setLoadingTrend(true)
    try {
      const data = await opsAPI.getThroughputTrend(buildApiParams(), { signal })
      if (fetchSeq !== dashboardFetchSeq.current) return
      setThroughputTrend(data)
    } catch (err) {
      if (fetchSeq !== dashboardFetchSeq.current || isCanceledRequest(err)) return
      setThroughputTrend(null)
      toast.error((err as { message?: string })?.message || t('admin.ops.failedToLoadThroughputTrend'))
    } finally {
      if (fetchSeq === dashboardFetchSeq.current) {
        setLoadingTrend(false)
      }
    }
  }

  async function refreshErrorTrendWithCancel(fetchSeq: number, signal: AbortSignal) {
    if (!opsEnabled) return
    setLoadingErrorTrend(true)
    try {
      const data = await opsAPI.getErrorTrend(buildApiParams(), { signal })
      if (fetchSeq !== dashboardFetchSeq.current) return
      setErrorTrend(data)
    } catch (err) {
      if (fetchSeq !== dashboardFetchSeq.current || isCanceledRequest(err)) return
      setErrorTrend(null)
      toast.error((err as { message?: string })?.message || t('admin.ops.failedToLoadErrorTrend'))
    } finally {
      if (fetchSeq === dashboardFetchSeq.current) {
        setLoadingErrorTrend(false)
      }
    }
  }

  async function refreshCoreSnapshotWithCancel(fetchSeq: number, signal: AbortSignal) {
    if (!opsEnabled) return
    setLoadingTrend(true)
    setLoadingErrorTrend(true)
    try {
      const data = await opsAPI.getDashboardSnapshotV2(buildApiParams(), { signal })
      if (fetchSeq !== dashboardFetchSeq.current) return
      setOverview(data.overview)
      setThroughputTrend(data.throughput_trend)
      setErrorTrend(data.error_trend)
    } catch (err) {
      if (fetchSeq !== dashboardFetchSeq.current || isCanceledRequest(err)) return
      // Fallback to legacy split endpoints when snapshot endpoint is unavailable.
      await Promise.all([
        refreshOverviewWithCancel(fetchSeq, signal),
        refreshThroughputTrendWithCancel(fetchSeq, signal),
        refreshErrorTrendWithCancel(fetchSeq, signal)
      ])
    } finally {
      if (fetchSeq === dashboardFetchSeq.current) {
        setLoadingTrend(false)
        setLoadingErrorTrend(false)
      }
    }
  }

  async function refreshLatencyHistogramWithCancel(fetchSeq: number, signal: AbortSignal) {
    if (!opsEnabled) return
    setLoadingLatency(true)
    try {
      const data = await opsAPI.getLatencyHistogram(buildApiParams(), { signal })
      if (fetchSeq !== dashboardFetchSeq.current) return
      setLatencyHistogram(data)
    } catch (err) {
      if (fetchSeq !== dashboardFetchSeq.current || isCanceledRequest(err)) return
      setLatencyHistogram(null)
      toast.error((err as { message?: string })?.message || t('admin.ops.failedToLoadLatencyHistogram'))
    } finally {
      if (fetchSeq === dashboardFetchSeq.current) {
        setLoadingLatency(false)
      }
    }
  }

  async function refreshErrorDistributionWithCancel(fetchSeq: number, signal: AbortSignal) {
    if (!opsEnabled) return
    setLoadingErrorDistribution(true)
    try {
      const data = await opsAPI.getErrorDistribution(buildApiParams(), { signal })
      if (fetchSeq !== dashboardFetchSeq.current) return
      setErrorDistribution(data)
    } catch (err) {
      if (fetchSeq !== dashboardFetchSeq.current || isCanceledRequest(err)) return
      setErrorDistribution(null)
      toast.error((err as { message?: string })?.message || t('admin.ops.failedToLoadErrorDistribution'))
    } finally {
      if (fetchSeq === dashboardFetchSeq.current) {
        setLoadingErrorDistribution(false)
      }
    }
  }

  async function refreshDeferredPanels(fetchSeq: number, signal: AbortSignal) {
    if (!opsEnabled) return
    await Promise.all([
      refreshLatencyHistogramWithCancel(fetchSeq, signal),
      refreshErrorDistributionWithCancel(fetchSeq, signal)
    ])
  }

  function isOpsDisabledError(err: unknown): boolean {
    return (
      !!err &&
      typeof err === 'object' &&
      'code' in err &&
      typeof (err as Record<string, unknown>).code === 'string' &&
      (err as Record<string, unknown>).code === 'OPS_DISABLED'
    )
  }

  const fetchData = useCallback(async () => {
    if (!opsEnabled) return

    abortDashboardFetch()
    dashboardFetchSeq.current += 1
    const fetchSeq = dashboardFetchSeq.current
    dashboardFetchController.current = new AbortController()

    setLoading(true)
    setErrorMessage('')
    try {
      await Promise.all([
        refreshCoreSnapshotWithCancel(fetchSeq, dashboardFetchController.current.signal),
        refreshSwitchTrendWithCancel(fetchSeq, dashboardFetchController.current.signal)
      ])
      if (fetchSeq !== dashboardFetchSeq.current) return

      setLastUpdated(new Date())

      // Trigger child component refreshes using the same cadence as the header.
      setDashboardRefreshToken((previous) => previous + 1)

      // Reset auto refresh countdown after successful fetch
      if (autoRefreshEnabled) {
        setAutoRefreshCountdown(Math.floor(autoRefreshIntervalMs / 1000))
      }

      // Defer non-core visual panels to reduce initial blocking.
      void refreshDeferredPanels(fetchSeq, dashboardFetchController.current.signal)
    } catch (err) {
      if (!isOpsDisabledError(err)) {
        console.error('[ops] failed to fetch dashboard data', err)
        setErrorMessage(t('admin.ops.failedToLoadData'))
      }
    } finally {
      if (fetchSeq === dashboardFetchSeq.current) {
        setLoading(false)
        setHasLoadedOnce(true)
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [opsEnabled, autoRefreshEnabled, autoRefreshIntervalMs, t])

  // Reload when the shared query state changes.
  useEffect(() => {
    if (isApplyingRouteQuery.current) return
    if (opsEnabled) {
      void fetchData()
    }
    syncQueryToRoute()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [timeRange, platform, groupId, queryMode])

  // React to external route changes (back/forward, deep links).
  useEffect(() => {
    if (isSyncingRouteQuery.current) return

    const prevTimeRange = timeRange
    const prevPlatform = platform
    const prevGroupId = groupId

    isApplyingRouteQuery.current = true
    applyRouteQueryToState()
    isApplyingRouteQuery.current = false

    const changed = prevTimeRange !== timeRange || prevPlatform !== platform || prevGroupId !== groupId
    if (changed && opsEnabled) {
      void fetchData()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchParams])

  async function loadDashboardAdvancedSettings() {
    try {
      const settings = await opsAPI.getAdvancedSettings()
      setShowAlertEvents(settings.display_alert_events)
      setAutoRefreshEnabled(settings.auto_refresh_enabled)
      setAutoRefreshIntervalMs(settings.auto_refresh_interval_seconds * 1000)
      setAutoRefreshCountdown(settings.auto_refresh_interval_seconds)
    } catch (err) {
      console.error('[OpsDashboard] Failed to load dashboard advanced settings', err)
      setShowAlertEvents(true)
      setAutoRefreshEnabled(false)
      setAutoRefreshIntervalMs(30000)
      setAutoRefreshCountdown(0)
    }
  }

  function loadThresholds() {
    void (async () => {
      try {
        const thresholds = await opsAPI.getMetricThresholds()
        setMetricThresholds(thresholds || null)
      } catch (err) {
        console.warn('[OpsDashboard] Failed to load thresholds', err)
        setMetricThresholds(null)
      }
    })()
  }

  useEffect(() => {
    void (async () => {
      await fetchAdminSettings()
      if (!opsMonitoringEnabled) {
        void navigate('/admin/settings')
        return
      }

      loadThresholds()
      await loadDashboardAdvancedSettings()

      if (opsMonitoringEnabled) {
        await fetchData()
      }
    })()
    return () => {
      abortDashboardFetch()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Countdown timer (drives auto refresh; updates every second)
  useEffect(() => {
    if (!autoRefreshEnabled || !opsEnabled) return
    const timer = window.setInterval(() => {
      if (loading) return

      if (autoRefreshCountdown <= 0) {
        // Fetch immediately when the countdown reaches 0.
        // fetchData() will reset the countdown to the full interval.
        void fetchData()
        return
      }

      setAutoRefreshCountdown((previous) => previous - 1)
    }, 1000)

    return () => window.clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoRefreshEnabled, opsEnabled, loading, autoRefreshCountdown])

  // Watch auto refresh settings changes
  useEffect(() => {
    if (autoRefreshEnabled) {
      setAutoRefreshCountdown(Math.floor(autoRefreshIntervalMs / 1000))
    } else {
      setAutoRefreshCountdown(0)
    }
  }, [autoRefreshEnabled, autoRefreshIntervalMs])

  function handleThroughputSelectPlatform(nextPlatform: string) {
    setPlatform(nextPlatform || '')
    setGroupId(null)
  }

  function handleThroughputSelectGroup(nextGroupId: number) {
    const id = Number.isFinite(nextGroupId) && nextGroupId > 0 ? nextGroupId : null
    setGroupId(id)
  }

  function handleOpenRequestDetails() {
    // Ensure only one modal visible at a time.
    setShowErrorDetails(false)
    setShowRequestDetails(true)
  }

  function openErrorDetails(kind: 'request' | 'upstream') {
    setErrorDetailsKind(kind)
    // Ensure only one modal visible at a time.
    setShowRequestDetails(false)
    setShowErrorDetails(true)
  }

  function onTimeRangeChange(value: string | number | boolean | null) {
    if (typeof value !== 'string') return
    if (!allowedTimeRanges.has(value as TimeRange)) return
    setTimeRange(value as TimeRange)
  }

  function onCustomTimeRangeChange(startTime: string, endTime: string) {
    setCustomStartTime(startTime)
    setCustomEndTime(endTime)
  }

  async function onSettingsSaved() {
    await loadDashboardAdvancedSettings()
    loadThresholds()
    await fetchData()
  }

  function onPlatformChange(value: string | number | boolean | null) {
    setPlatform(typeof value === 'string' ? value : '')
  }

  function onGroupChange(value: string | number | boolean | null) {
    if (value === null) {
      setGroupId(null)
      return
    }
    if (typeof value === 'number') {
      setGroupId(value > 0 ? value : null)
      return
    }
    if (typeof value === 'string') {
      const n = Number.parseInt(value, 10)
      setGroupId(Number.isFinite(n) && n > 0 ? n : null)
    }
  }

  function onQueryModeChange(value: string | number | boolean | null) {
    if (typeof value !== 'string') return
    if (!allowedQueryModes.has(value as QueryMode)) return
    setQueryMode(value as QueryMode)
  }

  const requestDetailsFetch = useCallback(
    (page: number, pageSize: number) =>
      opsAPI.listRequestDetails({
        page,
        page_size: pageSize,
        platform: platform || undefined,
        group_id: groupId ?? undefined,
        time_range: timeRange === 'custom' ? undefined : timeRange,
        start_time: timeRange === 'custom' ? customStartTime ?? undefined : undefined,
        end_time: timeRange === 'custom' ? customEndTime ?? undefined : undefined
      }),
    [platform, groupId, timeRange, customStartTime, customEndTime]
  )

  const errorDetailsFetch = useCallback(
    (page: number, pageSize: number) =>
      errorDetailsKind === 'upstream'
        ? opsAPI.listUpstreamErrors({
            page,
            page_size: pageSize,
            platform: platform || undefined,
            group_id: groupId ?? undefined,
            time_range: timeRange === 'custom' ? undefined : timeRange,
            start_time: timeRange === 'custom' ? customStartTime ?? undefined : undefined,
            end_time: timeRange === 'custom' ? customEndTime ?? undefined : undefined
          })
        : opsAPI.listRequestErrors({
            page,
            page_size: pageSize,
            platform: platform || undefined,
            group_id: groupId ?? undefined,
            time_range: timeRange === 'custom' ? undefined : timeRange,
            start_time: timeRange === 'custom' ? customStartTime ?? undefined : undefined,
            end_time: timeRange === 'custom' ? customEndTime ?? undefined : undefined
          }),
    [errorDetailsKind, platform, groupId, timeRange, customStartTime, customEndTime]
  )

  const header = useMemo(
    () => (
      <OpsDashboardHeader
        overview={overview}
        platform={platform}
        groupId={groupId}
        timeRange={timeRange}
        queryMode={queryMode}
        loading={loading}
        lastUpdated={lastUpdated}
        thresholds={metricThresholds}
        autoRefreshEnabled={autoRefreshEnabled}
        autoRefreshCountdown={autoRefreshCountdown}
        fullscreen={isFullscreen}
        workspace={!isFullscreen}
        showOverview={activeTab === 'overview'}
        customStartTime={customStartTime}
        customEndTime={customEndTime}
        onPlatformChange={onPlatformChange}
        onGroupChange={onGroupChange}
        onTimeRangeChange={onTimeRangeChange}
        onQueryModeChange={onQueryModeChange}
        onCustomTimeRangeChange={onCustomTimeRangeChange}
        onRefresh={() => void fetchData()}
        onOpenRequestDetails={handleOpenRequestDetails}
        onOpenErrorDetails={openErrorDetails}
        onOpenSettings={() => setShowSettingsDialog(true)}
        onOpenAlertRules={() => setActiveTab('alerts')}
        onEnterFullscreen={enterFullscreen}
        onExitFullscreen={exitFullscreen}
        navigation={
          // Full width keeps the tabs on their own line inside the toolbar card.
          <div className="w-full">
            <WorkspaceTabs
              value={activeTab}
              tabs={workspaceTabs}
              label={t('admin.ops.title')}
              embedded
              onValueChange={setActiveTab}
            />
          </div>
        }
      >
        <>
          {/* Row: Concurrency + Throughput */}
          {opsEnabled && !(loading && !hasLoadedOnce) && (
            <div className="grid grid-cols-1 gap-6 lg:grid-cols-4">
              <div className="min-h-[360px] lg:col-span-1">
                <OpsConcurrencyCard
                  platformFilter={platform}
                  groupIdFilter={groupId}
                  refreshToken={dashboardRefreshToken}
                />
              </div>
              <div className="h-[360px] lg:col-span-1">
                <OpsSwitchRateTrendChart
                  points={switchTrend?.points ?? []}
                  loading={loadingSwitchTrend}
                  timeRange={switchTrendTimeRange}
                  fullscreen={isFullscreen}
                />
              </div>
              <div className="h-[360px] lg:col-span-2">
                <OpsThroughputTrendChart
                  points={throughputTrend?.points ?? []}
                  byPlatform={throughputTrend?.by_platform ?? []}
                  topGroups={throughputTrend?.top_groups ?? []}
                  loading={loadingTrend}
                  timeRange={timeRange}
                  fullscreen={isFullscreen}
                  onSelectPlatform={handleThroughputSelectPlatform}
                  onSelectGroup={handleThroughputSelectGroup}
                  onOpenDetails={handleOpenRequestDetails}
                />
              </div>
            </div>
          )}

          {/* Row: Visual Analysis (baseline 3-up grid) */}
          {opsEnabled && !(loading && !hasLoadedOnce) && (
            <div className="grid grid-cols-1 gap-6 md:grid-cols-3">
              <OpsLatencyChart latencyData={latencyHistogram} loading={loadingLatency} />
              <OpsErrorDistributionChart
                data={errorDistribution}
                loading={loadingErrorDistribution}
                onOpenDetails={() => openErrorDetails('request')}
              />
              <OpsErrorTrendChart
                points={errorTrend?.points ?? []}
                loading={loadingErrorTrend}
                timeRange={timeRange}
                onOpenRequestErrors={() => openErrorDetails('request')}
                onOpenUpstreamErrors={() => openErrorDetails('upstream')}
              />
            </div>
          )}

          {/* Tab panels */}
          <div
            id="ops-panel-alerts"
            data-testid="ops-panel-alerts"
            role="tabpanel"
            aria-label={t('workspace.alerts')}
            className={`${isFullscreen ? '' : 'workspace-body'} ${activeTab === 'alerts' ? '' : 'hidden'}`}
          >
            {showAlertEvents ? (
              <OpsAlertEventsCard workspace={!isFullscreen} />
            ) : (
              <p className="card p-6 text-sm surface-muted">
                {t('workspace.alerts')} · {t('common.disabled')}
              </p>
            )}
          </div>
          <div
            id="ops-panel-logs"
            data-testid="ops-panel-logs"
            role="tabpanel"
            aria-label={t('workspace.systemLogs')}
            className={`${isFullscreen ? '' : 'workspace-shell workspace-body workspace-scroll'} ${
              activeTab === 'logs' ? '' : 'hidden'
            }`}
          >
            <OpsSystemLogTable
              className="workspace-shell"
              platformFilter={platform}
              refreshToken={dashboardRefreshToken}
            />
          </div>
        </>
      </OpsDashboardHeader>
    ),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [
      overview,
      platform,
      groupId,
      timeRange,
      queryMode,
      loading,
      hasLoadedOnce,
      lastUpdated,
      metricThresholds,
      autoRefreshEnabled,
      autoRefreshCountdown,
      isFullscreen,
      activeTab,
      customStartTime,
      customEndTime,
      switchTrend,
      loadingSwitchTrend,
      throughputTrend,
      loadingTrend,
      latencyHistogram,
      loadingLatency,
      errorDistribution,
      loadingErrorDistribution,
      errorTrend,
      loadingErrorTrend,
      dashboardRefreshToken,
      showAlertEvents,
      t
    ]
  )

  return (
    <div
      className={
        isFullscreen
          ? 'flex min-h-screen flex-col justify-center bg-[var(--console-canvas)] text-[var(--surface-text)]'
          : ''
      }
    >
      <div className={isFullscreen ? 'space-y-6 p-4 md:p-6' : 'workspace-page'}>
        {errorMessage && (
          <div className="workspace-fixed rounded-2xl bg-red-50/[0.15] p-4 text-sm text-red-600 dark:bg-red-900/20 dark:text-red-400">
            {errorMessage}
          </div>
        )}

        {loading && !hasLoadedOnce ? (
          <OpsDashboardSkeleton fullscreen={isFullscreen} />
        ) : opsEnabled ? (
          header
        ) : (
          <div className="card p-6 text-sm surface-muted">{t('admin.ops.disabled')}</div>
        )}
      </div>

      <OpsListDialog
        open={showRequestDetails}
        title={t('admin.ops.requestDetails.title')}
        fetchPage={requestDetailsFetch}
      />
      <OpsListDialog
        open={showErrorDetails}
        title={
          errorDetailsKind === 'upstream'
            ? t('admin.ops.errorDetails.upstreamErrors')
            : t('admin.ops.errorDetails.requestErrors')
        }
        fetchPage={errorDetailsFetch}
      />
      <OpsSettingsDialog
        open={showSettingsDialog}
        onClose={() => setShowSettingsDialog(false)}
        onSaved={() => void onSettingsSaved()}
      />
    </div>
  )
}
