import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, HelpTooltip, Modal, Select } from '@/components/ui'
import { adminAPI } from '@/api/admin'
import {
  opsAPI,
  type OpsDashboardOverview,
  type OpsMetricThresholds,
  type OpsRealtimeTrafficSummary
} from '@/api/admin/ops'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import { formatDateTimeLocalInput, formatNumber } from '@/utils/format'
import { formatTpsNumber } from '@/utils/usageTiming'
import { listPlatforms } from '@/utils/platformCatalog'
import { formatMemorySizeMB } from '../utils/opsFormatters'
import { DateTimePicker } from '../../parts/DateTimePicker'
import { Icon } from '../../AccountIcon'

type RealtimeWindow = '1min' | '5min' | '30min' | '1h'

export interface OpsRequestDetailsPreset {
  title: string
  kind?: string
  sort?: string
}

export interface OpsDashboardHeaderProps {
  overview?: OpsDashboardOverview | null
  platform: string
  groupId: number | null
  timeRange: string
  queryMode: string
  loading: boolean
  lastUpdated: Date | null
  thresholds?: OpsMetricThresholds | null
  autoRefreshEnabled?: boolean
  autoRefreshCountdown?: number
  fullscreen?: boolean
  workspace?: boolean
  showOverview?: boolean
  customStartTime?: string | null
  customEndTime?: string | null
  onPlatformChange: (value: string) => void
  onGroupChange: (value: number | null) => void
  onTimeRangeChange: (value: string) => void
  onQueryModeChange: (value: string) => void
  onCustomTimeRangeChange: (startTime: string, endTime: string) => void
  onRefresh: () => void
  onOpenRequestDetails: (preset?: OpsRequestDetailsPreset) => void
  onOpenErrorDetails: (kind: 'request' | 'upstream') => void
  onOpenSettings: () => void
  onOpenAlertRules: () => void
  onEnterFullscreen: () => void
  onExitFullscreen: () => void
  navigation?: React.ReactNode
  children?: React.ReactNode
}

const REALTIME_WINDOW_MINUTES: Record<RealtimeWindow, number> = {
  '1min': 1,
  '5min': 5,
  '30min': 30,
  '1h': 60
}

const TOOLBAR_RANGE_MINUTES: Record<string, number> = {
  '5m': 5,
  '30m': 30,
  '1h': 60,
  '6h': 6 * 60,
  '24h': 24 * 60
}

type ThresholdLevel = 'normal' | 'warning' | 'critical'

interface DiagnosisItem {
  type: 'critical' | 'warning' | 'info'
  message: string
  impact: string
  action?: string
}

function formatCustomTimeRangeLabel(startTime: string, endTime: string): string {
  const start = new Date(startTime)
  const end = new Date(endTime)
  const formatDate = (d: Date) => {
    const month = String(d.getMonth() + 1).padStart(2, '0')
    const day = String(d.getDate()).padStart(2, '0')
    const hour = String(d.getHours()).padStart(2, '0')
    const minute = String(d.getMinutes()).padStart(2, '0')
    return `${month}-${day} ${hour}:${minute}`
  }
  return `${formatDate(start)} ~ ${formatDate(end)}`
}

export function OpsDashboardHeader({
  overview = null,
  platform,
  groupId,
  timeRange,
  queryMode,
  loading,
  lastUpdated,
  thresholds = null,
  autoRefreshEnabled = false,
  autoRefreshCountdown = 0,
  fullscreen = false,
  workspace = false,
  showOverview = true,
  customStartTime = null,
  customEndTime = null,
  onPlatformChange,
  onGroupChange,
  onTimeRangeChange,
  onQueryModeChange,
  onCustomTimeRangeChange,
  onRefresh,
  onOpenRequestDetails,
  onOpenErrorDetails,
  onOpenSettings,
  onOpenAlertRules,
  onEnterFullscreen,
  onExitFullscreen,
  navigation,
  children
}: OpsDashboardHeaderProps) {
  const { t } = useTranslation()
  const adminSettingsStore = useAdminSettingsStore()

  const [realtimeWindow, setRealtimeWindow] = useState<RealtimeWindow>('1min')
  const [showCustomTimeRangeDialog, setShowCustomTimeRangeDialog] = useState(false)
  const [customStartTimeInput, setCustomStartTimeInput] = useState('')
  const [customEndTimeInput, setCustomEndTimeInput] = useState('')
  const [groups, setGroups] = useState<Array<{ id: number; name: string; platform: string }>>([])
  const [realtimeTrafficSummary, setRealtimeTrafficSummary] = useState<OpsRealtimeTrafficSummary | null>(null)
  const [, setRealtimeTrafficLoading] = useState(false)

  const realtimeTrafficLoadingRef = useRef(false)
  const stateRef = useRef({ realtimeWindow, platform, groupId })
  stateRef.current = { realtimeWindow, platform, groupId }

  const systemMetrics = overview?.system_metrics ?? null

  const availableRealtimeWindows = (() => {
    const toolbarMinutes = TOOLBAR_RANGE_MINUTES[timeRange] ?? 60
    return (['1min', '5min', '30min', '1h'] as const).filter((w) => REALTIME_WINDOW_MINUTES[w] <= toolbarMinutes)
  })()

  const platformOptions = useMemo(
    () => [
      { value: '', label: t('common.all') },
      ...listPlatforms().map((spec) => ({ value: spec.id, label: spec.display_name }))
    ],
    [t]
  )

  const timeRangeOptions = [
    { value: '5m', label: t('admin.ops.timeRange.5m') },
    { value: '30m', label: t('admin.ops.timeRange.30m') },
    { value: '1h', label: t('admin.ops.timeRange.1h') },
    { value: '6h', label: t('admin.ops.timeRange.6h') },
    { value: '24h', label: t('admin.ops.timeRange.24h') },
    {
      value: 'custom',
      label:
        timeRange === 'custom' && customStartTime && customEndTime
          ? `${t('admin.ops.timeRange.custom')} (${formatCustomTimeRangeLabel(customStartTime, customEndTime)})`
          : t('admin.ops.timeRange.custom')
    }
  ]

  const queryModeOptions = [
    { value: 'auto', label: t('admin.ops.queryMode.auto') },
    { value: 'raw', label: t('admin.ops.queryMode.raw') },
    { value: 'preagg', label: t('admin.ops.queryMode.preagg') }
  ]

  const groupOptions = useMemo(() => {
    const filtered = platform ? groups.filter((group) => group.platform === platform) : groups
    return [{ value: null, label: t('common.all') }, ...filtered.map((group) => ({ value: group.id, label: group.name }))]
  }, [groups, platform, t])

  // A platform change can invalidate the selected group.
  useEffect(() => {
    if (!platform) return
    const currentGroup = groups.find((group) => group.id === groupId)
    if (currentGroup && currentGroup.platform !== platform) {
      onGroupChange(null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [platform])

  useEffect(() => {
    void (async () => {
      try {
        const list = await adminAPI.groups.getAll()
        setGroups(list.map((group) => ({ id: group.id, name: group.name, platform: group.platform })))
      } catch (error) {
        console.error('[OpsDashboardHeader] Failed to load groups', error)
        setGroups([])
      }
    })()
  }, [])

  function makeZeroRealtimeTrafficSummary(): OpsRealtimeTrafficSummary {
    const now = new Date().toISOString()
    return {
      window: stateRef.current.realtimeWindow,
      start_time: now,
      end_time: now,
      platform: stateRef.current.platform,
      group_id: stateRef.current.groupId,
      qps: { current: 0, peak: 0, avg: 0 },
      tps: { current: 0, peak: 0, avg: 0 }
    }
  }

  const loadRealtimeTrafficSummary = async () => {
    if (realtimeTrafficLoadingRef.current) return
    if (!adminSettingsStore.opsRealtimeMonitoringEnabled) {
      setRealtimeTrafficSummary(makeZeroRealtimeTrafficSummary())
      return
    }
    realtimeTrafficLoadingRef.current = true
    setRealtimeTrafficLoading(true)
    try {
      const current = stateRef.current
      const res = await opsAPI.getRealtimeTrafficSummary(current.realtimeWindow, current.platform, current.groupId)
      if (res && res.enabled === false) {
        adminSettingsStore.setOpsRealtimeMonitoringEnabledLocal(false)
      }
      setRealtimeTrafficSummary(res?.summary ?? null)
    } catch (error) {
      console.error('[OpsDashboardHeader] Failed to load realtime traffic summary', error)
      setRealtimeTrafficSummary(null)
    } finally {
      realtimeTrafficLoadingRef.current = false
      setRealtimeTrafficLoading(false)
    }
  }

  // The realtime window must be inside the toolbar window; reset to keep UX predictable.
  useEffect(() => {
    setRealtimeWindow('1min')
    void loadRealtimeTrafficSummary()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [timeRange])

  useEffect(() => {
    void loadRealtimeTrafficSummary()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [realtimeWindow, platform, groupId])

  // --- Threshold checking helpers ---
  function getSLAThresholdLevel(slaPercent: number | null): ThresholdLevel {
    if (slaPercent == null) return 'normal'
    const threshold = thresholds?.sla_percent_min
    if (threshold == null) return 'normal'

    // SLA is "higher is better":
    // - below threshold => critical
    // - within +0.1% buffer => warning
    const warningBuffer = 0.1

    if (slaPercent < threshold) return 'critical'
    if (slaPercent < threshold + warningBuffer) return 'warning'
    return 'normal'
  }

  function getTTFTThresholdLevel(ttftMs: number | null): ThresholdLevel {
    if (ttftMs == null) return 'normal'
    const threshold = thresholds?.ttft_p99_ms_max
    if (threshold == null) return 'normal'
    if (ttftMs >= threshold) return 'critical'
    if (ttftMs >= threshold * 0.8) return 'warning'
    return 'normal'
  }

  function getRequestErrorRateThresholdLevel(errorRatePercent: number | null): ThresholdLevel {
    if (errorRatePercent == null) return 'normal'
    const threshold = thresholds?.request_error_rate_percent_max
    if (threshold == null) return 'normal'
    if (errorRatePercent >= threshold) return 'critical'
    if (errorRatePercent >= threshold * 0.8) return 'warning'
    return 'normal'
  }

  function getUpstreamErrorRateThresholdLevel(upstreamErrorRatePercent: number | null): ThresholdLevel {
    if (upstreamErrorRatePercent == null) return 'normal'
    const threshold = thresholds?.upstream_error_rate_percent_max
    if (threshold == null) return 'normal'
    if (upstreamErrorRatePercent >= threshold) return 'critical'
    if (upstreamErrorRatePercent >= threshold * 0.8) return 'warning'
    return 'normal'
  }

  function getThresholdColorClass(level: ThresholdLevel): string {
    switch (level) {
      case 'critical':
        return 'text-red-600 dark:text-red-400'
      case 'warning':
        return 'text-yellow-600 dark:text-yellow-400'
      default:
        return 'text-green-600 dark:text-green-400'
    }
  }

  // --- Realtime / Overview labels ---
  const totalRequestsLabel = formatNumber(overview?.request_count_total ?? 0)
  const totalTokensLabel = formatNumber(overview?.token_consumed ?? 0)

  const qpsCurrent = realtimeTrafficSummary?.qps?.current ?? 0
  const qpsPeak = realtimeTrafficSummary?.qps?.peak ?? 0
  const qpsAvg = realtimeTrafficSummary?.qps?.avg ?? 0
  const tpsCurrent = realtimeTrafficSummary?.tps?.current ?? 0
  const tpsPeak = realtimeTrafficSummary?.tps?.peak ?? 0
  const tpsAvg = realtimeTrafficSummary?.tps?.avg ?? 0

  const errorRatePercent = typeof overview?.error_rate === 'number' ? overview.error_rate * 100 : null
  const upstreamErrorRatePercent =
    typeof overview?.upstream_error_rate === 'number' ? overview.upstream_error_rate * 100 : null

  const durationP99Ms = overview?.duration?.p99_ms ?? null
  const durationP95Ms = overview?.duration?.p95_ms ?? null
  const durationP90Ms = overview?.duration?.p90_ms ?? null
  const durationP50Ms = overview?.duration?.p50_ms ?? null
  const durationAvgMs = overview?.duration?.avg_ms ?? null
  const durationMaxMs = overview?.duration?.max_ms ?? null

  const ttftP99Ms = overview?.ttft?.p99_ms ?? null
  const ttftP95Ms = overview?.ttft?.p95_ms ?? null
  const ttftP90Ms = overview?.ttft?.p90_ms ?? null
  const ttftP50Ms = overview?.ttft?.p50_ms ?? null
  const ttftAvgMs = overview?.ttft?.avg_ms ?? null
  const ttftMaxMs = overview?.ttft?.max_ms ?? null

  // --- Health Score & Diagnosis (primary) ---
  const isSystemIdle = (() => {
    const ov = overview
    if (!ov) return true
    const qps = ov.qps?.current
    const errorRate = ov.error_rate ?? 0
    return (qps ?? 0) === 0 && errorRate === 0
  })()

  const healthScoreValue = (() => {
    const value = overview?.health_score
    return typeof value === 'number' && Number.isFinite(value) ? value : null
  })()

  const healthScoreColor = (() => {
    if (isSystemIdle) return '#9ca3af'
    const score = healthScoreValue
    if (score == null) return '#9ca3af'
    if (score >= 90) return '#10b981'
    if (score >= 60) return '#f59e0b'
    return '#ef4444'
  })()

  const healthScoreClass = (() => {
    if (isSystemIdle) return 'text-gray-400'
    const score = healthScoreValue
    if (score == null) return 'text-gray-400'
    if (score >= 90) return 'text-green-500'
    if (score >= 60) return 'text-yellow-500'
    return 'text-red-500'
  })()

  const circleSize = fullscreen ? 140 : 100
  const strokeWidth = fullscreen ? 10 : 8
  const radius = (circleSize - strokeWidth) / 2
  const circumference = 2 * Math.PI * radius
  const dashOffset = (() => {
    if (isSystemIdle) return 0
    if (healthScoreValue == null) return 0
    const score = Math.max(0, Math.min(100, healthScoreValue))
    return circumference - (score / 100) * circumference
  })()

  const diagnosisReport = useMemo<DiagnosisItem[]>(() => {
    const ov = overview
    if (!ov) return []

    const report: DiagnosisItem[] = []

    if (isSystemIdle) {
      report.push({
        type: 'info',
        message: t('admin.ops.diagnosis.idle'),
        impact: t('admin.ops.diagnosis.idleImpact')
      })
      return report
    }

    // Resource diagnostics (highest priority)
    const sm = ov.system_metrics
    if (sm) {
      if (sm.db_ok === false) {
        report.push({
          type: 'critical',
          message: t('admin.ops.diagnosis.dbDown'),
          impact: t('admin.ops.diagnosis.dbDownImpact'),
          action: t('admin.ops.diagnosis.dbDownAction')
        })
      }
      if (sm.redis_ok === false) {
        report.push({
          type: 'warning',
          message: t('admin.ops.diagnosis.redisDown'),
          impact: t('admin.ops.diagnosis.redisDownImpact'),
          action: t('admin.ops.diagnosis.redisDownAction')
        })
      }

      const cpuPct = sm.cpu_usage_percent ?? 0
      if (cpuPct > 90) {
        report.push({
          type: 'critical',
          message: t('admin.ops.diagnosis.cpuCritical', { usage: cpuPct.toFixed(1) }),
          impact: t('admin.ops.diagnosis.cpuCriticalImpact'),
          action: t('admin.ops.diagnosis.cpuCriticalAction')
        })
      } else if (cpuPct > 80) {
        report.push({
          type: 'warning',
          message: t('admin.ops.diagnosis.cpuHigh', { usage: cpuPct.toFixed(1) }),
          impact: t('admin.ops.diagnosis.cpuHighImpact'),
          action: t('admin.ops.diagnosis.cpuHighAction')
        })
      }

      const memPct = sm.memory_usage_percent ?? 0
      if (memPct > 90) {
        report.push({
          type: 'critical',
          message: t('admin.ops.diagnosis.memoryCritical', { usage: memPct.toFixed(1) }),
          impact: t('admin.ops.diagnosis.memoryCriticalImpact'),
          action: t('admin.ops.diagnosis.memoryCriticalAction')
        })
      } else if (memPct > 85) {
        report.push({
          type: 'warning',
          message: t('admin.ops.diagnosis.memoryHigh', { usage: memPct.toFixed(1) }),
          impact: t('admin.ops.diagnosis.memoryHighImpact'),
          action: t('admin.ops.diagnosis.memoryHighAction')
        })
      }
    }

    const ttftP99 = ov.ttft?.p99_ms ?? 0
    if (ttftP99 > 500) {
      report.push({
        type: 'warning',
        message: t('admin.ops.diagnosis.ttftHigh', { ttft: ttftP99.toFixed(0) }),
        impact: t('admin.ops.diagnosis.ttftHighImpact'),
        action: t('admin.ops.diagnosis.ttftHighAction')
      })
    }

    // Error rate diagnostics (adjusted thresholds)
    const upstreamRatePct = (ov.upstream_error_rate ?? 0) * 100
    if (upstreamRatePct > 5) {
      report.push({
        type: 'critical',
        message: t('admin.ops.diagnosis.upstreamCritical', { rate: upstreamRatePct.toFixed(2) }),
        impact: t('admin.ops.diagnosis.upstreamCriticalImpact'),
        action: t('admin.ops.diagnosis.upstreamCriticalAction')
      })
    } else if (upstreamRatePct > 2) {
      report.push({
        type: 'warning',
        message: t('admin.ops.diagnosis.upstreamHigh', { rate: upstreamRatePct.toFixed(2) }),
        impact: t('admin.ops.diagnosis.upstreamHighImpact'),
        action: t('admin.ops.diagnosis.upstreamHighAction')
      })
    }

    const errorPct = (ov.error_rate ?? 0) * 100
    if (errorPct > 3) {
      report.push({
        type: 'critical',
        message: t('admin.ops.diagnosis.errorHigh', { rate: errorPct.toFixed(2) }),
        impact: t('admin.ops.diagnosis.errorHighImpact'),
        action: t('admin.ops.diagnosis.errorHighAction')
      })
    } else if (errorPct > 0.5) {
      report.push({
        type: 'warning',
        message: t('admin.ops.diagnosis.errorElevated', { rate: errorPct.toFixed(2) }),
        impact: t('admin.ops.diagnosis.errorElevatedImpact'),
        action: t('admin.ops.diagnosis.errorElevatedAction')
      })
    }

    // SLA diagnostics
    const slaPct = (ov.sla ?? 0) * 100
    if (slaPct < 90) {
      report.push({
        type: 'critical',
        message: t('admin.ops.diagnosis.slaCritical', { sla: slaPct.toFixed(2) }),
        impact: t('admin.ops.diagnosis.slaCriticalImpact'),
        action: t('admin.ops.diagnosis.slaCriticalAction')
      })
    } else if (slaPct < 98) {
      report.push({
        type: 'warning',
        message: t('admin.ops.diagnosis.slaLow', { sla: slaPct.toFixed(2) }),
        impact: t('admin.ops.diagnosis.slaLowImpact'),
        action: t('admin.ops.diagnosis.slaLowAction')
      })
    }

    // Health score diagnostics (lowest priority)
    if (healthScoreValue != null) {
      if (healthScoreValue < 60) {
        report.push({
          type: 'critical',
          message: t('admin.ops.diagnosis.healthCritical', { score: healthScoreValue }),
          impact: t('admin.ops.diagnosis.healthCriticalImpact'),
          action: t('admin.ops.diagnosis.healthCriticalAction')
        })
      } else if (healthScoreValue < 90) {
        report.push({
          type: 'warning',
          message: t('admin.ops.diagnosis.healthLow', { score: healthScoreValue }),
          impact: t('admin.ops.diagnosis.healthLowImpact'),
          action: t('admin.ops.diagnosis.healthLowAction')
        })
      }
    }

    if (report.length === 0) {
      report.push({
        type: 'info',
        message: t('admin.ops.diagnosis.healthy'),
        impact: t('admin.ops.diagnosis.healthyImpact')
      })
    }

    return report
  }, [overview, isSystemIdle, healthScoreValue, t])

  // --- System health (secondary) ---
  function formatTimeShort(ts?: string | null): string {
    if (!ts) return '-'
    const d = new Date(ts)
    if (Number.isNaN(d.getTime())) return '-'
    return d.toLocaleTimeString()
  }

  const cpuPercentValue = (() => {
    const value = systemMetrics?.cpu_usage_percent
    return typeof value === 'number' && Number.isFinite(value) ? value : null
  })()
  const cpuPercentClass = (() => {
    if (cpuPercentValue == null) return 'text-gray-900 dark:text-white'
    if (cpuPercentValue >= 95) return 'text-rose-600 dark:text-rose-400'
    if (cpuPercentValue >= 80) return 'text-yellow-600 dark:text-yellow-400'
    return 'text-emerald-600 dark:text-emerald-400'
  })()

  const memPercentValue = (() => {
    const value = systemMetrics?.memory_usage_percent
    return typeof value === 'number' && Number.isFinite(value) ? value : null
  })()
  const memPercentClass = (() => {
    if (memPercentValue == null) return 'text-gray-900 dark:text-white'
    if (memPercentValue >= 95) return 'text-rose-600 dark:text-rose-400'
    if (memPercentValue >= 85) return 'text-yellow-600 dark:text-yellow-400'
    return 'text-emerald-600 dark:text-emerald-400'
  })()

  const dbConnActiveValue = (() => {
    const value = systemMetrics?.db_conn_active
    return typeof value === 'number' && Number.isFinite(value) ? value : null
  })()
  const dbConnIdleValue = (() => {
    const value = systemMetrics?.db_conn_idle
    return typeof value === 'number' && Number.isFinite(value) ? value : null
  })()
  const dbMaxOpenConnsValue = (() => {
    const value = systemMetrics?.db_max_open_conns
    return typeof value === 'number' && Number.isFinite(value) ? value : null
  })()
  const dbConnOpenValue = dbConnActiveValue == null || dbConnIdleValue == null ? null : dbConnActiveValue + dbConnIdleValue
  const dbUsagePercent =
    dbConnOpenValue == null || dbMaxOpenConnsValue == null || dbMaxOpenConnsValue <= 0
      ? null
      : Math.min(100, Math.max(0, (dbConnOpenValue / dbMaxOpenConnsValue) * 100))
  const dbMiddleLabel = (() => {
    if (systemMetrics?.db_ok === false) return 'FAIL'
    if (dbUsagePercent != null) return `${dbUsagePercent.toFixed(0)}%`
    if (systemMetrics?.db_ok === true) return t('admin.ops.ok')
    return t('admin.ops.noData')
  })()
  const dbMiddleClass = (() => {
    if (systemMetrics?.db_ok === false) return 'text-rose-600 dark:text-rose-400'
    if (dbUsagePercent != null) {
      if (dbUsagePercent >= 90) return 'text-rose-600 dark:text-rose-400'
      if (dbUsagePercent >= 70) return 'text-yellow-600 dark:text-yellow-400'
      return 'text-emerald-600 dark:text-emerald-400'
    }
    if (systemMetrics?.db_ok === true) return 'text-emerald-600 dark:text-emerald-400'
    return 'text-gray-900 dark:text-white'
  })()

  const redisConnTotalValue = (() => {
    const value = systemMetrics?.redis_conn_total
    return typeof value === 'number' && Number.isFinite(value) ? value : null
  })()
  const redisPoolSizeValue = (() => {
    const value = systemMetrics?.redis_pool_size
    return typeof value === 'number' && Number.isFinite(value) ? value : null
  })()
  const redisUsagePercent =
    redisConnTotalValue == null || redisPoolSizeValue == null || redisPoolSizeValue <= 0
      ? null
      : Math.min(100, Math.max(0, (redisConnTotalValue / redisPoolSizeValue) * 100))
  const redisMiddleLabel = (() => {
    if (systemMetrics?.redis_ok === false) return 'FAIL'
    if (redisUsagePercent != null) return `${redisUsagePercent.toFixed(0)}%`
    if (systemMetrics?.redis_ok === true) return t('admin.ops.ok')
    return t('admin.ops.noData')
  })()
  const redisMiddleClass = (() => {
    if (systemMetrics?.redis_ok === false) return 'text-rose-600 dark:text-rose-400'
    if (redisUsagePercent != null) {
      if (redisUsagePercent >= 90) return 'text-rose-600 dark:text-rose-400'
      if (redisUsagePercent >= 70) return 'text-yellow-600 dark:text-yellow-400'
      return 'text-emerald-600 dark:text-emerald-400'
    }
    if (systemMetrics?.redis_ok === true) return 'text-emerald-600 dark:text-emerald-400'
    return 'text-gray-900 dark:text-white'
  })()

  const goroutineCountValue = (() => {
    const value = systemMetrics?.goroutine_count
    return typeof value === 'number' && Number.isFinite(value) ? value : null
  })()
  const goroutinesWarnThreshold = 8_000
  const goroutinesCriticalThreshold = 15_000
  const goroutineStatus: 'ok' | 'warning' | 'critical' | 'unknown' = (() => {
    const n = goroutineCountValue
    if (n == null) return 'unknown'
    if (n >= goroutinesCriticalThreshold) return 'critical'
    if (n >= goroutinesWarnThreshold) return 'warning'
    return 'ok'
  })()
  const goroutineStatusLabel = (() => {
    switch (goroutineStatus) {
      case 'ok':
        return t('admin.ops.ok')
      case 'warning':
        return t('common.warning')
      case 'critical':
        return t('common.critical')
      default:
        return t('admin.ops.noData')
    }
  })()
  const goroutineStatusClass = (() => {
    switch (goroutineStatus) {
      case 'ok':
        return 'text-emerald-600 dark:text-emerald-400'
      case 'warning':
        return 'text-yellow-600 dark:text-yellow-400'
      case 'critical':
        return 'text-rose-600 dark:text-rose-400'
      default:
        return 'text-gray-900 dark:text-white'
    }
  })()

  const jobHeartbeats = overview?.job_heartbeats ?? []
  const jobsWarnCount = jobHeartbeats.filter(
    (hb) => hb && hb.last_error_at && (!hb.last_success_at || hb.last_error_at > hb.last_success_at)
  ).length
  const jobsStatus: 'ok' | 'warn' | 'unknown' = jobHeartbeats.length === 0 ? 'unknown' : jobsWarnCount > 0 ? 'warn' : 'ok'
  const jobsStatusLabel = (() => {
    switch (jobsStatus) {
      case 'ok':
        return t('admin.ops.ok')
      case 'warn':
        return t('common.warning')
      default:
        return t('admin.ops.noData')
    }
  })()
  const jobsStatusClass = (() => {
    switch (jobsStatus) {
      case 'ok':
        return 'text-emerald-600 dark:text-emerald-400'
      case 'warn':
        return 'text-yellow-600 dark:text-yellow-400'
      default:
        return 'text-gray-900 dark:text-white'
    }
  })()

  function handlePlatformChange(value: string | number | boolean | null) {
    onPlatformChange(String(value || ''))
  }

  function handleGroupChange(value: string | number | boolean | null) {
    if (value === null || value === '' || typeof value === 'boolean') {
      onGroupChange(null)
      return
    }
    const id = typeof value === 'number' ? value : Number.parseInt(String(value), 10)
    onGroupChange(Number.isFinite(id) && id > 0 ? id : null)
  }

  function handleTimeRangeChange(value: string | number | boolean | null) {
    const newValue = String(value || '1h')
    if (newValue === 'custom') {
      // 初始化为最近1小时
      const now = new Date()
      const oneHourAgo = new Date(now.getTime() - 60 * 60 * 1000)
      setCustomStartTimeInput(formatDateTimeLocalInput(oneHourAgo.getTime() / 1000))
      setCustomEndTimeInput(formatDateTimeLocalInput(now.getTime() / 1000))
      setShowCustomTimeRangeDialog(true)
    } else {
      onTimeRangeChange(newValue)
    }
  }

  function handleCustomTimeRangeConfirm() {
    if (!customStartTimeInput || !customEndTimeInput) return
    const startTime = new Date(customStartTimeInput).toISOString()
    const endTime = new Date(customEndTimeInput).toISOString()
    // Emit custom time range first so the parent can build correct API params
    // when it reacts to timeRange switching to "custom".
    onCustomTimeRangeChange(startTime, endTime)
    onTimeRangeChange('custom')
    setShowCustomTimeRangeDialog(false)
  }

  function handleQueryModeChange(value: string | number | boolean | null) {
    onQueryModeChange(String(value || 'auto'))
  }

  function openDetails(preset?: OpsRequestDetailsPreset) {
    onOpenRequestDetails(preset)
  }

  function handleToolbarRefresh() {
    void loadRealtimeTrafficSummary()
    onRefresh()
  }

  const overviewSlaPercent = typeof overview?.sla === 'number' ? overview.sla * 100 : null

  return (
    <div className={workspace ? 'workspace-body' : `card flex flex-col gap-4 ${fullscreen ? 'p-8' : 'p-6'}`}>
      <div
        data-testid="ops-toolbar"
        className={`flex flex-wrap items-center justify-between gap-4 ${
          workspace ? 'ops-workspace-toolbar workspace-fixed card p-6' : 'border-b border-gray-100 pb-4 dark:border-dark-700'
        }`}
      >
        <div className="ops-toolbar-summary">
          <h1 className="flex items-center gap-2 text-xl font-black text-gray-900 dark:text-white">
            <svg className="h-6 w-6 text-blue-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth="2"
                d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 01-2 2h-2a2 2 0 01-2-2z"
              />
            </svg>
            {t('admin.ops.title')}
          </h1>

          {!fullscreen && (
            <div className="mt-1 flex items-center gap-3 text-xs surface-muted">
              <span className="flex items-center gap-1.5" title={loading ? t('admin.ops.loadingText') : t('admin.ops.ready')}>
                <span className="relative flex h-2 w-2">
                  <span
                    className={`console-data-mark relative inline-flex h-2 w-2 rounded-full ${loading ? 'bg-gray-400' : 'bg-green-500'}`}
                  />
                </span>
                {loading ? t('admin.ops.loadingText') : t('admin.ops.ready')}
              </span>

              <span>·</span>
              <span>
                {t('common.refresh')}:{' '}
                {lastUpdated
                  ? lastUpdated
                      .toLocaleString('zh-CN', {
                        year: 'numeric',
                        month: '2-digit',
                        day: '2-digit',
                        hour: '2-digit',
                        minute: '2-digit',
                        second: '2-digit'
                      })
                      .replace(/\//g, '-')
                  : t('common.unknown')}
              </span>

              {autoRefreshEnabled && (
                <>
                  <span>·</span>
                  <span>{t('admin.ops.autoRefreshRemaining', { seconds: autoRefreshCountdown })}</span>
                </>
              )}
            </div>
          )}
        </div>

        <div className="ops-toolbar-controls flex flex-wrap items-center gap-3">
          {!fullscreen && (
            <>
              <Select
                value={platform}
                options={platformOptions}
                className="w-full sm:w-[140px]"
                onValueChange={handlePlatformChange}
              />

              <Select
                value={groupId}
                options={groupOptions}
                className="w-full sm:w-[160px]"
                onValueChange={handleGroupChange}
              />

              <div className="ops-toolbar-separator mx-1 hidden h-4 w-[1px] bg-gray-200 dark:bg-dark-700 sm:block" />

              <Select
                value={timeRange}
                options={timeRangeOptions}
                className="relative w-full sm:w-[150px]"
                onValueChange={handleTimeRangeChange}
              />
            </>
          )}

          <button
            type="button"
            className="flex h-8 w-8 items-center justify-center rounded-lg transition-colors hover:surface-hover surface-control surface-muted"
            disabled={loading}
            title={t('common.refresh')}
            onClick={handleToolbarRefresh}
          >
            <svg className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth="2"
                d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
              />
            </svg>
          </button>

          {!fullscreen && (
            <>
              <div className="ops-toolbar-separator mx-1 hidden h-4 w-[1px] bg-gray-200 dark:bg-dark-700 sm:block" />

              <button
                type="button"
                className="flex h-8 items-center gap-1.5 rounded-lg bg-blue-100/[0.15] px-3 text-xs font-bold text-blue-700 transition-colors hover:bg-blue-200/25 dark:bg-blue-900/30 dark:text-blue-400 dark:hover:bg-blue-900/50"
                title={t('admin.ops.alertRules.title')}
                onClick={onOpenAlertRules}
              >
                <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth="2"
                    d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9"
                  />
                </svg>
                <span className="hidden sm:inline">{t('admin.ops.alertRules.manage')}</span>
              </button>

              <button
                type="button"
                className="flex h-8 items-center gap-1.5 rounded-lg px-3 text-xs font-bold transition-colors hover:surface-hover surface-control surface-muted"
                title={t('admin.ops.settings.title')}
                onClick={onOpenSettings}
              >
                <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth="2"
                    d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"
                  />
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                </svg>
                <span className="hidden sm:inline">{t('common.settings')}</span>
              </button>

              <button
                type="button"
                className="flex h-8 w-8 items-center justify-center rounded-lg transition-colors hover:surface-hover surface-control surface-muted"
                title={t('admin.ops.fullscreen.enter')}
                onClick={onEnterFullscreen}
              >
                <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth="2"
                    d="M4 8V4m0 0h4M4 4l5 5m11-1V4m0 0h-4m4 0l-5 5M4 16v4m0 0h4m-4 0l5-5m11 5l-5-5m5 5v-4m0 4h-4"
                  />
                </svg>
              </button>
            </>
          )}

          {fullscreen && (
            <button
              type="button"
              className="flex h-8 items-center gap-1.5 rounded-lg px-3 text-xs font-bold transition-colors hover:surface-hover surface-control surface-muted"
              title={t('admin.ops.fullscreen.exit')}
              onClick={onExitFullscreen}
            >
              <span className="hidden sm:inline">{t('admin.ops.fullscreen.exit')}</span>
            </button>
          )}
        </div>
      </div>

      {/* Tab navigation lives inside the toolbar card so the header stays one surface. */}
      {navigation}

      <div className={workspace ? 'workspace-body' : 'contents'}>
        <div
          id="ops-panel-overview"
          data-testid="ops-panel-overview"
          role="tabpanel"
          aria-label={t('workspace.opsMonitoring')}
          className={`${workspace ? 'workspace-shell workspace-body workspace-scroll' : 'contents'} ${showOverview ? '' : 'hidden'}`}
        >
          <div className={workspace ? 'card workspace-shell flex flex-col gap-4 p-6' : 'contents'}>
            {overview && (
              <div className="grid grid-cols-1 gap-6 lg:grid-cols-12">
                <div className={`glass-surface lg:col-span-5 ${fullscreen ? 'p-6' : 'p-4'}`}>
                  <div className="grid h-full grid-cols-1 gap-6 md:grid-cols-[200px_1fr] md:items-center">
                    <div className="group relative flex cursor-pointer flex-col items-center justify-center rounded-xl py-2 transition-all hover:surface-hover md:border-r md:border-gray-200 md:pr-6 dark:md:border-dark-700">
                      {/* Diagnosis Popover (hover) */}
                      <div className="pointer-events-none absolute left-1/2 top-full z-50 mt-2 w-72 -translate-x-1/2 opacity-0 transition-opacity duration-200 group-hover:pointer-events-auto group-hover:opacity-100 md:left-full md:top-0 md:ml-2 md:mt-0 md:translate-x-0">
                        <div className="rounded-xl surface-inset p-4 shadow-xl ring-1 ring-black/5 dark:ring-white/10">
                          <h4 className="mb-3 flex items-center gap-2 border-b border-gray-100 pb-2 text-sm font-bold text-gray-900 dark:border-dark-700 dark:text-white">
                            <Icon name="brain" size="sm" className="text-blue-500" />
                            {t('admin.ops.diagnosis.title')}
                          </h4>

                          <div className="space-y-3">
                            {diagnosisReport.map((item, idx) => (
                              <div key={idx} className="flex gap-3">
                                <div className="mt-0.5 shrink-0">
                                  {item.type === 'critical' ? (
                                    <svg className="h-4 w-4 text-red-500" fill="currentColor" viewBox="0 0 20 20">
                                      <path
                                        fillRule="evenodd"
                                        d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z"
                                        clipRule="evenodd"
                                      />
                                    </svg>
                                  ) : item.type === 'warning' ? (
                                    <svg className="h-4 w-4 text-yellow-500" fill="currentColor" viewBox="0 0 20 20">
                                      <path
                                        fillRule="evenodd"
                                        d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z"
                                        clipRule="evenodd"
                                      />
                                    </svg>
                                  ) : (
                                    <svg className="h-4 w-4 text-blue-500" fill="currentColor" viewBox="0 0 20 20">
                                      <path
                                        fillRule="evenodd"
                                        d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-8-3a1 1 0 100 2 1 1 0 000-2zm-1 3a1 1 0 012 0v4a1 1 0 11-2 0v-4z"
                                        clipRule="evenodd"
                                      />
                                    </svg>
                                  )}
                                </div>
                                <div className="flex-1">
                                  <div className="text-xs font-semibold text-gray-900 dark:text-white">{item.message}</div>
                                  <div className="mt-0.5 text-[11px] surface-muted">{item.impact}</div>
                                  {item.action && (
                                    <div className="mt-1 flex items-center gap-1 text-[11px] text-blue-600 dark:text-blue-400">
                                      <Icon name="lightbulb" size="xs" />
                                      {item.action}
                                    </div>
                                  )}
                                </div>
                              </div>
                            ))}
                          </div>

                          <div className="mt-3 border-t border-gray-100 pt-2 text-[10px] dark:border-dark-700 surface-muted">
                            {t('admin.ops.diagnosis.footer')}
                          </div>
                        </div>
                      </div>

                      <div className="relative flex items-center justify-center">
                        <svg width={circleSize} height={circleSize} className="-rotate-90 transform">
                          <circle
                            cx={circleSize / 2}
                            cy={circleSize / 2}
                            r={radius}
                            strokeWidth={strokeWidth}
                            fill="transparent"
                            className="text-gray-200 dark:text-dark-700"
                            stroke="currentColor"
                          />
                          <circle
                            cx={circleSize / 2}
                            cy={circleSize / 2}
                            r={radius}
                            strokeWidth={strokeWidth}
                            fill="transparent"
                            stroke={healthScoreColor}
                            strokeLinecap="round"
                            strokeDasharray={circumference}
                            strokeDashoffset={dashOffset}
                            className="transition-all duration-1000 ease-out"
                          />
                        </svg>

                        <div className="absolute flex flex-col items-center">
                          <span className={`${fullscreen ? 'text-5xl' : 'text-3xl'} font-black ${healthScoreClass}`}>
                            {isSystemIdle ? t('admin.ops.idleStatus') : (overview.health_score ?? '--')}
                          </span>
                          <span
                            className={`${fullscreen ? 'text-xs' : 'text-[10px]'} font-bold uppercase tracking-wider text-gray-400`}
                          >
                            {t('admin.ops.health')}
                          </span>
                        </div>
                      </div>

                      {!fullscreen && (
                        <div className="mt-4 text-center">
                          <div className="flex items-center justify-center gap-1 text-xs font-medium surface-muted">
                            {t('admin.ops.healthCondition')}
                            <HelpTooltip content={t('admin.ops.healthHelp')} />
                          </div>
                          <div className={`mt-1 text-xs font-bold ${healthScoreClass}`}>
                            {isSystemIdle
                              ? t('admin.ops.idleStatus')
                              : typeof overview.health_score === 'number' && overview.health_score >= 90
                                ? t('admin.ops.healthyStatus')
                                : t('admin.ops.riskyStatus')}
                          </div>
                        </div>
                      )}
                    </div>

                    <div className="flex h-full flex-col justify-center py-2">
                      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                        <div className="flex items-center gap-2">
                          <div className="relative flex h-3 w-3 shrink-0">
                            <span className="console-data-mark absolute inline-flex h-full w-full animate-ping rounded-full bg-blue-400 opacity-75" />
                            <span className="console-data-mark relative inline-flex h-3 w-3 rounded-full bg-blue-500" />
                          </div>
                          <h3 className="text-xs font-bold uppercase tracking-wider surface-muted">
                            {t('admin.ops.realtime.title')}
                          </h3>
                          {!fullscreen && <HelpTooltip content={t('admin.ops.tooltips.qps')} />}
                        </div>

                        <div className="flex flex-wrap gap-1">
                          {availableRealtimeWindows.map((window) => (
                            <button
                              key={window}
                              type="button"
                              className={`rounded px-1.5 py-0.5 text-[9px] font-bold transition-colors sm:px-2 sm:text-[10px] surface-control ${
                                realtimeWindow === window
                                  ? 'bg-blue-500 text-white'
                                  : 'surface-field text-gray-600 hover:bg-gray-300 dark:surface-field dark:text-gray-400 dark:hover:surface-hover'
                              }`}
                              onClick={() => setRealtimeWindow(window)}
                            >
                              {window}
                            </button>
                          ))}
                        </div>
                      </div>

                      <div className={fullscreen ? 'space-y-4' : 'space-y-3'}>
                        <div>
                          <div className={`${fullscreen ? 'text-xs' : 'text-[10px]'} font-bold uppercase text-gray-400`}>
                            {t('admin.ops.current')}
                          </div>
                          <div className="mt-1 flex flex-wrap items-baseline gap-x-4 gap-y-2">
                            <div className="flex items-baseline gap-1.5">
                              <span
                                className={`${fullscreen ? 'text-4xl' : 'text-xl sm:text-2xl'} font-black text-gray-900 dark:text-white`}
                              >
                                {qpsCurrent.toFixed(1)}
                              </span>
                              <span className={`${fullscreen ? 'text-sm' : 'text-xs'} font-bold surface-muted`}>QPS</span>
                            </div>
                            <div className="flex items-baseline gap-1.5">
                              <span
                                className={`${fullscreen ? 'text-4xl' : 'text-xl sm:text-2xl'} font-black text-gray-900 dark:text-white`}
                              >
                                {tpsCurrent.toFixed(1)}
                              </span>
                              <span className={`${fullscreen ? 'text-sm' : 'text-xs'} font-bold surface-muted`}>
                                {t('admin.ops.tps')}
                              </span>
                            </div>
                          </div>
                        </div>

                        <div className="grid grid-cols-2 gap-3">
                          <div>
                            <div className={`${fullscreen ? 'text-xs' : 'text-[10px]'} font-bold uppercase text-gray-400`}>
                              {t('admin.ops.peak')}
                            </div>
                            <div
                              className={`${fullscreen ? 'text-base' : 'text-sm'} mt-1 space-y-0.5 font-medium surface-muted`}
                            >
                              <div className="flex items-baseline gap-1.5">
                                <span className="font-black text-gray-900 dark:text-white">{qpsPeak.toFixed(1)}</span>
                                <span className="text-xs">QPS</span>
                              </div>
                              <div className="flex items-baseline gap-1.5">
                                <span className="font-black text-gray-900 dark:text-white">{tpsPeak.toFixed(1)}</span>
                                <span className="text-xs">{t('admin.ops.tps')}</span>
                              </div>
                            </div>
                          </div>

                          <div>
                            <div className={`${fullscreen ? 'text-xs' : 'text-[10px]'} font-bold uppercase text-gray-400`}>
                              {t('admin.ops.average')}
                            </div>
                            <div
                              className={`${fullscreen ? 'text-base' : 'text-sm'} mt-1 space-y-0.5 font-medium surface-muted`}
                            >
                              <div className="flex items-baseline gap-1.5">
                                <span className="font-black text-gray-900 dark:text-white">{qpsAvg.toFixed(1)}</span>
                                <span className="text-xs">QPS</span>
                              </div>
                              <div className="flex items-baseline gap-1.5">
                                <span className="font-black text-gray-900 dark:text-white">{tpsAvg.toFixed(1)}</span>
                                <span className="text-xs">{t('admin.ops.tps')}</span>
                              </div>
                            </div>
                          </div>
                        </div>

                        <div className="h-8 w-full overflow-hidden opacity-50">
                          <svg className="h-full w-full" viewBox="0 0 280 32" preserveAspectRatio="none">
                            <path
                              d="M0 16 Q 20 16, 40 16 T 80 16 T 120 10 T 160 22 T 200 16 T 240 16 T 280 16"
                              fill="none"
                              stroke="#3b82f6"
                              strokeWidth="2"
                              vectorEffect="non-scaling-stroke"
                            >
                              <animate
                                attributeName="d"
                                dur="2s"
                                repeatCount="indefinite"
                                values="M0 16 Q 20 16, 40 16 T 80 16 T 120 10 T 160 22 T 200 16 T 240 16 T 280 16;
                                        M0 16 Q 20 16, 40 16 T 80 16 T 120 16 T 160 16 T 200 10 T 240 22 T 280 16;
                                        M0 16 Q 20 16, 40 16 T 80 16 T 120 16 T 160 16 T 200 16 T 240 16 T 280 16"
                                keyTimes="0;0.5;1"
                              />
                            </path>
                          </svg>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>

                <div className="grid h-full grid-cols-1 content-center gap-4 sm:grid-cols-2 lg:col-span-7 lg:grid-cols-3">
                  {/* Card 1: Requests */}
                  <div className="glass-surface p-4" style={{ order: 1 }}>
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-1">
                        <span className="text-[10px] font-bold uppercase surface-muted">
                          {t('admin.ops.requestsTitle')}
                        </span>
                        {!fullscreen && <HelpTooltip content={t('admin.ops.tooltips.totalRequests')} />}
                      </div>
                      {!fullscreen && (
                        <button
                          className="text-[10px] font-bold text-blue-500 hover:underline surface-control"
                          type="button"
                          onClick={() => openDetails({ title: t('admin.ops.requestDetails.title') })}
                        >
                          {t('admin.ops.requestDetails.details')}
                        </button>
                      )}
                    </div>
                    <div className="mt-2 space-y-2 text-xs">
                      <div className="flex justify-between">
                        <span className="surface-muted">{t('admin.ops.requests')}:</span>
                        <span className="font-bold text-gray-900 dark:text-white">{totalRequestsLabel}</span>
                      </div>
                      <div className="flex justify-between">
                        <span className="surface-muted">{t('admin.ops.tokens')}:</span>
                        <span className="font-bold text-gray-900 dark:text-white">{totalTokensLabel}</span>
                      </div>
                      <div className="flex justify-between">
                        <span className="surface-muted">{t('admin.ops.avgQps')}:</span>
                        <span className="font-bold text-gray-900 dark:text-white">
                          {(overview.qps?.avg ?? 0).toFixed(1)}
                        </span>
                      </div>
                      <div className="flex justify-between">
                        <span className="surface-muted">{t('admin.ops.avgTps')}:</span>
                        <span className="font-bold text-gray-900 dark:text-white">
                          {(overview.tps?.avg ?? 0).toFixed(1)}
                        </span>
                      </div>
                    </div>
                  </div>

                  {/* Card 2: SLA */}
                  <div className="glass-surface p-4" style={{ order: 2 }}>
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <span className="text-[10px] font-bold uppercase surface-muted">{t('admin.ops.sla')}</span>
                        {!fullscreen && <HelpTooltip content={t('admin.ops.tooltips.sla')} />}
                        <span
                          className={`console-data-mark h-1.5 w-1.5 rounded-full ${
                            getSLAThresholdLevel(overviewSlaPercent) === 'critical'
                              ? 'bg-red-500'
                              : getSLAThresholdLevel(overviewSlaPercent) === 'warning'
                                ? 'bg-yellow-500'
                                : 'bg-green-500'
                          }`}
                        />
                      </div>
                      {!fullscreen && (
                        <button
                          className="text-[10px] font-bold text-blue-500 hover:underline surface-control"
                          type="button"
                          onClick={() => openDetails({ title: t('admin.ops.requestDetails.title'), kind: 'error' })}
                        >
                          {t('admin.ops.requestDetails.details')}
                        </button>
                      )}
                    </div>
                    <div
                      className={`mt-2 text-3xl font-black ${getThresholdColorClass(getSLAThresholdLevel(overviewSlaPercent))}`}
                    >
                      {overviewSlaPercent == null ? '-' : `${overviewSlaPercent.toFixed(3)}%`}
                    </div>
                    <div className="mt-3 h-2 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
                      <div
                        className={`console-data-mark h-full transition-all ${
                          getSLAThresholdLevel(overviewSlaPercent) === 'critical'
                            ? 'bg-red-500'
                            : getSLAThresholdLevel(overviewSlaPercent) === 'warning'
                              ? 'bg-yellow-500'
                              : 'bg-green-500'
                        }`}
                        style={{ width: `${Math.max((overviewSlaPercent ?? 0) - 90, 0) * 10}%` }}
                      />
                    </div>
                    <div className="mt-3 text-xs">
                      <div className="flex justify-between">
                        <span className="surface-muted">{t('admin.ops.exceptions')}:</span>
                        <span className="font-bold text-gray-900 dark:text-white">
                          {formatNumber((overview.request_count_sla ?? 0) - (overview.success_count ?? 0))}
                        </span>
                      </div>
                    </div>
                  </div>

                  {/* Card 3: Request Errors */}
                  <div className="glass-surface p-4" style={{ order: 3 }}>
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-1">
                        <span className="text-[10px] font-bold uppercase surface-muted">
                          {t('admin.ops.requestErrors')}
                        </span>
                        {!fullscreen && <HelpTooltip content={t('admin.ops.tooltips.errors')} />}
                      </div>
                      {!fullscreen && (
                        <button
                          className="text-[10px] font-bold text-blue-500 hover:underline surface-control"
                          type="button"
                          onClick={() => onOpenErrorDetails('request')}
                        >
                          {t('admin.ops.requestDetails.details')}
                        </button>
                      )}
                    </div>
                    <div
                      className={`mt-2 text-3xl font-black ${getThresholdColorClass(getRequestErrorRateThresholdLevel(errorRatePercent))}`}
                    >
                      {errorRatePercent == null ? '-' : `${errorRatePercent.toFixed(2)}%`}
                    </div>
                    <div className="mt-3 space-y-1 text-xs">
                      <div className="flex justify-between">
                        <span className="surface-muted">{t('admin.ops.errorCount')}:</span>
                        <span className="font-bold text-gray-900 dark:text-white">
                          {formatNumber(overview.error_count_sla ?? 0)}
                        </span>
                      </div>
                      <div className="flex justify-between">
                        <span className="surface-muted">{t('admin.ops.businessLimited')}:</span>
                        <span className="font-bold text-gray-900 dark:text-white">
                          {formatNumber(overview.business_limited_count ?? 0)}
                        </span>
                      </div>
                    </div>
                  </div>

                  {/* Card 4: Request Duration */}
                  <div className="glass-surface p-4" style={{ order: 4 }}>
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-1">
                        <span className="text-[10px] font-bold uppercase surface-muted">
                          {t('admin.ops.latencyDuration')}
                        </span>
                        {!fullscreen && <HelpTooltip content={t('admin.ops.tooltips.latency')} />}
                      </div>
                      {!fullscreen && (
                        <button
                          className="text-[10px] font-bold text-blue-500 hover:underline surface-control"
                          type="button"
                          onClick={() =>
                            openDetails({ title: t('admin.ops.latencyDuration'), sort: 'duration_desc' })
                          }
                        >
                          {t('admin.ops.requestDetails.details')}
                        </button>
                      )}
                    </div>
                    <div className="mt-2 flex items-baseline gap-2">
                      <div className="text-3xl font-black text-gray-900 dark:text-white">{durationP99Ms ?? '-'}</div>
                      <span className="text-xs font-bold surface-muted">ms (P99)</span>
                    </div>
                    <div className="mt-3 grid grid-cols-1 gap-x-3 gap-y-1 text-xs 2xl:grid-cols-2">
                      {[
                        { label: 'P95', value: durationP95Ms },
                        { label: 'P90', value: durationP90Ms },
                        { label: 'P50', value: durationP50Ms },
                        { label: 'Avg', value: durationAvgMs },
                        { label: 'Max', value: durationMaxMs }
                      ].map((entry) => (
                        <div key={entry.label} className="flex items-baseline gap-1 whitespace-nowrap">
                          <span className="surface-muted">{entry.label}:</span>
                          <span className="font-bold text-gray-900 dark:text-white">{entry.value ?? '-'}</span>
                          <span className="surface-muted">ms</span>
                        </div>
                      ))}
                    </div>
                  </div>

                  {/* Card 5: TTFT */}
                  <div className="glass-surface p-4" style={{ order: 5 }}>
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-1">
                        <span className="text-[10px] font-bold uppercase surface-muted">TTFT</span>
                        {!fullscreen && <HelpTooltip content={t('admin.ops.tooltips.ttft')} />}
                      </div>
                      {!fullscreen && (
                        <button
                          className="text-[10px] font-bold text-blue-500 hover:underline surface-control"
                          type="button"
                          onClick={() => openDetails({ title: t('admin.ops.ttftLabel'), kind: 'success', sort: 'ttft_desc' })}
                        >
                          {t('admin.ops.requestDetails.details')}
                        </button>
                      )}
                    </div>
                    <div
                      className={`mt-2 flex items-baseline gap-2 ${getThresholdColorClass(getTTFTThresholdLevel(ttftP99Ms))}`}
                    >
                      <div className="text-3xl font-black">{ttftP99Ms ?? '-'}</div>
                      <span className="text-xs font-bold surface-muted">ms (P99)</span>
                    </div>
                    <div className="mt-3 grid grid-cols-1 gap-x-3 gap-y-1 text-xs 2xl:grid-cols-2">
                      {[
                        { label: 'P95', value: ttftP95Ms },
                        { label: 'P90', value: ttftP90Ms },
                        { label: 'P50', value: ttftP50Ms },
                        { label: 'Avg', value: ttftAvgMs },
                        { label: 'Max', value: ttftMaxMs }
                      ].map((entry) => (
                        <div key={entry.label} className="flex items-baseline gap-1 whitespace-nowrap">
                          <span className="surface-muted">{entry.label}:</span>
                          <span className={`font-bold ${getThresholdColorClass(getTTFTThresholdLevel(entry.value))}`}>
                            {entry.value ?? '-'}
                          </span>
                          <span className="surface-muted">ms</span>
                        </div>
                      ))}
                    </div>
                  </div>

                  {/* Card 6: Upstream Errors */}
                  <div className="glass-surface p-4" style={{ order: 6 }}>
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-1">
                        <span className="text-[10px] font-bold uppercase surface-muted">
                          {t('admin.ops.upstreamErrors')}
                        </span>
                        {!fullscreen && <HelpTooltip content={t('admin.ops.tooltips.upstreamErrors')} />}
                      </div>
                      {!fullscreen && (
                        <button
                          className="text-[10px] font-bold text-blue-500 hover:underline surface-control"
                          type="button"
                          onClick={() => onOpenErrorDetails('upstream')}
                        >
                          {t('admin.ops.requestDetails.details')}
                        </button>
                      )}
                    </div>
                    <div
                      className={`mt-2 text-3xl font-black ${getThresholdColorClass(getUpstreamErrorRateThresholdLevel(upstreamErrorRatePercent))}`}
                    >
                      {upstreamErrorRatePercent == null ? '-' : `${upstreamErrorRatePercent.toFixed(2)}%`}
                    </div>
                    <div className="mt-3 space-y-1 text-xs">
                      <div className="flex justify-between">
                        <span className="surface-muted">{t('admin.ops.errorCountExcl429529')}:</span>
                        <span className="font-bold text-gray-900 dark:text-white">
                          {formatNumber(overview.upstream_error_count_excl_429_529 ?? 0)}
                        </span>
                      </div>
                      <div className="flex justify-between">
                        <span className="surface-muted">429/529:</span>
                        <span className="font-bold text-gray-900 dark:text-white">
                          {formatNumber((overview.upstream_429_count ?? 0) + (overview.upstream_529_count ?? 0))}
                        </span>
                      </div>
                    </div>
                  </div>

                  {/* Per-request output speed, separate from system throughput. */}
                  <div className="glass-surface p-4" data-testid="output-tps-card" style={{ order: 7 }}>
                    <div className="flex items-center gap-1">
                      <span className="text-[10px] font-bold uppercase surface-muted">{t('admin.ops.outputTps')}</span>
                      {!fullscreen && <HelpTooltip content={t('admin.ops.tooltips.outputTps')} />}
                    </div>
                    <div className="mt-2 flex flex-wrap items-baseline gap-2">
                      <div className="text-3xl font-black text-gray-900 dark:text-white" data-testid="output-tps-p50">
                        {overview.output_tps?.p50 == null ? '—' : formatTpsNumber(overview.output_tps.p50)}
                      </div>
                      <span className="text-xs font-bold surface-muted">tok/s (P50)</span>
                    </div>
                    <div className="mt-3 grid grid-cols-1 gap-x-3 gap-y-1 text-xs 2xl:grid-cols-2">
                      {(['p5', 'p10', 'avg'] as const).map((metric) => (
                        <div key={metric} className="flex items-baseline gap-1 whitespace-nowrap">
                          <span className="surface-muted">{metric === 'avg' ? 'Avg' : metric.toUpperCase()}:</span>
                          <span
                            className="font-bold text-gray-900 dark:text-white"
                            data-testid={`output-tps-${metric}`}
                          >
                            {overview.output_tps?.[metric] == null ? '—' : formatTpsNumber(overview.output_tps[metric]!)}
                          </span>
                          <span className="surface-muted">tok/s</span>
                        </div>
                      ))}
                    </div>
                    <div className="mt-2 text-xs surface-muted" data-testid="output-tps-samples">
                      {t('admin.ops.outputTpsSamples', {
                        count: overview.output_tps == null ? '—' : formatNumber(overview.output_tps.sample_count)
                      })}
                    </div>
                  </div>
                </div>
              </div>
            )}

            {/* Integrated: System health (cards) */}
            {overview && (
              <div className="mt-2 border-t border-gray-100 pt-4 dark:border-dark-700">
                <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
                  <div className="glass-surface p-3">
                    <div className="text-[10px] font-bold uppercase surface-muted">{t('admin.ops.cpu')}</div>
                    <div className={`mt-1 text-lg font-black ${cpuPercentClass}`}>
                      {cpuPercentValue == null ? '-' : `${cpuPercentValue.toFixed(1)}%`}
                    </div>
                  </div>
                  <div className="glass-surface p-3">
                    <div className="text-[10px] font-bold uppercase surface-muted">{t('admin.ops.memory')}</div>
                    <div className={`mt-1 text-lg font-black ${memPercentClass}`}>
                      {memPercentValue == null ? '-' : `${memPercentValue.toFixed(1)}%`}
                    </div>
                    {systemMetrics?.memory_used_mb != null && systemMetrics?.memory_total_mb != null && (
                      <div className="mt-0.5 text-[10px] surface-muted">
                        {formatMemorySizeMB(systemMetrics.memory_used_mb)} / {formatMemorySizeMB(systemMetrics.memory_total_mb)}
                      </div>
                    )}
                  </div>
                  <div className="glass-surface p-3">
                    <div className="text-[10px] font-bold uppercase surface-muted">{t('admin.ops.db')}</div>
                    <div className={`mt-1 text-lg font-black ${dbMiddleClass}`}>{dbMiddleLabel}</div>
                    <div className="mt-0.5 text-[10px] surface-muted">
                      {dbConnActiveValue ?? 0} / {dbConnIdleValue ?? 0} / {dbMaxOpenConnsValue ?? '-'}
                    </div>
                  </div>
                  <div className="glass-surface p-3">
                    <div className="text-[10px] font-bold uppercase surface-muted">{t('admin.ops.redis')}</div>
                    <div className={`mt-1 text-lg font-black ${redisMiddleClass}`}>{redisMiddleLabel}</div>
                    <div className="mt-0.5 text-[10px] surface-muted">
                      {redisConnTotalValue ?? 0} / {redisPoolSizeValue ?? '-'}
                    </div>
                  </div>
                  <div className="glass-surface p-3">
                    <div className="text-[10px] font-bold uppercase surface-muted">{t('admin.ops.goroutines')}</div>
                    <div className={`mt-1 text-lg font-black ${goroutineStatusClass}`}>
                      {goroutineCountValue == null ? '-' : formatNumber(goroutineCountValue)}
                    </div>
                    <div className={`mt-0.5 text-[10px] ${goroutineStatusClass}`}>{goroutineStatusLabel}</div>
                  </div>
                  <div className="glass-surface p-3">
                    <div className="text-[10px] font-bold uppercase surface-muted">{t('admin.ops.jobs')}</div>
                    <div className={`mt-1 text-lg font-black ${jobsStatusClass}`}>{jobsStatusLabel}</div>
                    <div className="mt-0.5 text-[10px] surface-muted">
                      {jobHeartbeats.length > 0
                        ? jobHeartbeats
                            .map((hb) => `${hb?.job_name ?? '?'}: ${formatTimeShort(hb?.last_success_at)}`)
                            .join(' · ')
                        : '-'}
                    </div>
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>

        {children}
      </div>

      <Modal
        open={showCustomTimeRangeDialog}
        onClose={() => setShowCustomTimeRangeDialog(false)}
        title={t('admin.ops.timeRange.custom')}
        size="sm"
        footer={
          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={() => setShowCustomTimeRangeDialog(false)}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" onClick={handleCustomTimeRangeConfirm}>
              {t('common.confirm')}
            </Button>
          </div>
        }
      >
        <div className="space-y-4">
          <div>
            <label className="input-label">{t('dates.startDate')}</label>
            <DateTimePicker
              type="datetime-local"
              ariaLabel={t('dates.startDate')}
              value={customStartTimeInput}
              onValueChange={setCustomStartTimeInput}
            />
          </div>
          <div>
            <label className="input-label">{t('dates.endDate')}</label>
            <DateTimePicker
              type="datetime-local"
              ariaLabel={t('dates.endDate')}
              value={customEndTimeInput}
              onValueChange={setCustomEndTimeInput}
            />
          </div>
        </div>
      </Modal>

      {/* Query mode stays available for deep links and keyboard-driven operation. */}
      <span className="hidden">
        <Select value={queryMode} options={queryModeOptions} onValueChange={handleQueryModeChange} />
      </span>
    </div>
  )
}

export default OpsDashboardHeader
