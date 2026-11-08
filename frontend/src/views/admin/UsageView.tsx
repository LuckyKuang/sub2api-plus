import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Check } from 'lucide-react'
import { adminAPI } from '@/api/admin'
import { adminUsageAPI } from '@/api/admin/usage'
import type { AdminUsageQueryParams, AdminUsageStatsResponse } from '@/api/admin/usage'
import { listErrorLogs } from '@/api/admin/ops'
import type { OpsErrorLog } from '@/api/admin/ops'
import { getPersistedPageSize, useColumnPreferenceKey } from '@/views/admin/shared'
import { useUsageSnapshot } from '@/views/user/useUsageSnapshot'
import { formatReasoningEffort } from '@/utils/format'
import { resolveUsageRequestType, requestTypeToLegacyStream } from '@/utils/usageRequestType'
import { averageTps, firstByteMs, strictFirstTokenMs, tpsNote } from '@/utils/usageTiming'
import { saveAs } from 'file-saver'
import { Pagination, Select } from '@/components/ui'
import { DateRangePicker } from '@/components/DateRangePicker'
import { Icon } from '@/components/Icon'
import {
  EndpointDistributionChart,
  GroupDistributionChart,
  ModelDistributionChart,
  TokenUsageTrend
} from '@/components/charts'
import type { DistributionMetric } from '@/components/charts'
import { WorkspaceTabs } from '@/components/WorkspaceTabs'
import type { AdminUsageLog, TrendDataPoint, ModelStat, GroupStat, EndpointStat, AdminUser } from '@/types'
import { useAppStore } from '@/stores/app'
import { UsageFilters, type UsageErrorView, type UsageFilterMode } from './parts/UsageFilters'
import { UsageStatsCards } from './parts/UsageStatsCards'
import { AdminUsageTable } from './parts/UsageTable'
import { UsageExportProgress } from './parts/UsageExportProgress'
import { UsageCleanupDialog } from './parts/UsageCleanupDialog'
import { UserTokenRanking } from './parts/UserTokenRanking'
import { ClientDisconnectEventsPanel } from './parts/ClientDisconnectEventsPanel'
import { LiveRequestsPanel } from './parts/LiveRequestsPanel'
import { OpsErrorLogTable } from './parts/OpsErrorLogTable'
import { OpsErrorDetailModal } from './parts/OpsErrorDetailModal'
import { UserBalanceHistoryModal } from './parts/UserBalanceHistoryModal'
import { buildXlsxBlob } from './parts/xlsxWriter'

type DetailTab = 'usage' | 'errors' | 'ranking' | 'disconnects' | 'live'
type EndpointSource = 'inbound' | 'upstream' | 'path'
type ModelDistributionSource = 'requested' | 'upstream' | 'mapping'

interface UsageColumn {
  key: string
  label: string
  sortable?: boolean
  sticky?: 'left' | 'right' | false
}

const ALWAYS_VISIBLE = ['user', 'created_at']
const DEFAULT_HIDDEN_COLUMNS = ['api_key', 'billing_mode', 'request_id', 'session_id', 'ip_address', 'service_tier']
const LEGACY_HIDDEN_COLUMNS_KEY = 'admin-usage-restored-v2-hidden-columns'
const HIDDEN_COLUMNS_KEY = 'admin-usage-native-v3-hidden-columns'

const ERR_ALWAYS_VISIBLE = ['user', 'status', 'created_at']
const ERR_DEFAULT_HIDDEN_COLUMNS = [
  'api_key',
  'platform',
  'endpoint',
  'group',
  'type',
  'user_agent',
  'client_ip',
  'actions'
]
const ERR_HIDDEN_COLUMNS_KEY = 'usage-error-hidden-columns'

// key 集合须与 OpsErrorLogTable 内部 allColumns 一致
const ERR_ALL_COLUMNS: UsageColumn[] = [
  { key: 'user', label: 'admin.ops.errorLog.user' },
  { key: 'api_key', label: 'admin.ops.errorLog.apiKey' },
  { key: 'account', label: 'admin.ops.errorLog.account' },
  { key: 'platform', label: 'admin.ops.errorLog.platform' },
  { key: 'model', label: 'admin.ops.errorLog.model' },
  { key: 'endpoint', label: 'admin.ops.errorLog.endpoint' },
  { key: 'group', label: 'admin.ops.errorLog.group' },
  { key: 'type', label: 'admin.ops.errorLog.type' },
  { key: 'category', label: 'usage.errors.category' },
  { key: 'status', label: 'admin.ops.errorLog.status' },
  { key: 'message', label: 'admin.ops.errorLog.message' },
  { key: 'created_at', label: 'admin.ops.errorLog.time' },
  { key: 'user_agent', label: 'usage.userAgent' },
  { key: 'client_ip', label: 'admin.ops.errorLog.ip' },
  { key: 'actions', label: 'admin.ops.errorLog.action' }
]

function formatLocalDate(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function getLast24HoursRangeDates(): { start: string; end: string } {
  const end = new Date()
  const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
  return { start: formatLocalDate(start), end: formatLocalDate(end) }
}

function getGranularityForRange(start: string, end: string): 'day' | 'hour' {
  const startTime = new Date(`${start}T00:00:00`).getTime()
  const endTime = new Date(`${end}T00:00:00`).getTime()
  const daysDiff = Math.ceil((endTime - startTime) / (1000 * 60 * 60 * 24))
  return daysDiff <= 1 ? 'hour' : 'day'
}

// 注意：'YYYY-MM-DDT00:00:00' 无时区后缀，按本地时区解析后再转 UTC——与页面其它日期处理语义一致，刻意如此，勿改成 'T00:00:00Z'
function toRFC3339(value: string | undefined, endOfDay = false): string | undefined {
  return value ? new Date(value + (endOfDay ? 'T23:59:59.999' : 'T00:00:00')).toISOString() : undefined
}

export default function UsageView() {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)
  const [searchParams, setSearchParams] = useSearchParams()
  const hadQueryParams = useRef(searchParams.size > 0)

  const [workspaceTab, setWorkspaceTab] = useState<'charts' | 'records'>(() =>
    hadQueryParams.current ? 'records' : 'charts'
  )
  const [activeTab, setActiveTab] = useState<DetailTab>('usage')
  const lastHistoryTab = useRef<DetailTab>('usage')
  const [rankingMounted, setRankingMounted] = useState(false)
  const [disconnectEventsMounted, setDisconnectEventsMounted] = useState(false)

  const defaultRange = useRef(getLast24HoursRangeDates())
  const [startDate, setStartDate] = useState(defaultRange.current.start)
  const [endDate, setEndDate] = useState(defaultRange.current.end)
  const [errorDatePreset, setErrorDatePreset] = useState<string | null>('last24Hours')
  const [granularity, setGranularity] = useState<'day' | 'hour'>(
    getGranularityForRange(defaultRange.current.start, defaultRange.current.end)
  )
  const [filters, setFilters] = useState<AdminUsageQueryParams>({
    user_id: undefined,
    model: undefined,
    group_id: undefined,
    request_type: undefined,
    native_compaction_v2: null,
    billing_type: null,
    start_date: defaultRange.current.start,
    end_date: defaultRange.current.end
  })
  const filtersRef = useRef(filters)
  filtersRef.current = filters

  // Loaders read the filter snapshot synchronously, so every update must refresh
  // the ref before the state commit lands.
  const updateFilters = useCallback((next: AdminUsageQueryParams) => {
    filtersRef.current = next
    setFilters(next)
  }, [])

  const startDateRef = useRef(startDate)
  const updateStartDate = useCallback((value: string) => {
    startDateRef.current = value
    setStartDate(value)
  }, [])
  const endDateRef = useRef(endDate)
  const updateEndDate = useCallback((value: string) => {
    endDateRef.current = value
    setEndDate(value)
  }, [])

  const [pagination, setPagination] = useState({ page: 1, page_size: getPersistedPageSize(), total: 0 })
  const paginationRef = useRef(pagination)
  paginationRef.current = pagination
  const [sortState, setSortState] = useState<{ sort_by: string; sort_order: 'asc' | 'desc' }>({
    sort_by: 'created_at',
    sort_order: 'desc'
  })
  const sortStateRef = useRef(sortState)
  sortStateRef.current = sortState

  const [usageStats, setUsageStats] = useState<AdminUsageStatsResponse | null>(null)
  const [usageLogs, setUsageLogs] = useState<AdminUsageLog[]>([])
  const [loading, setLoading] = useState(false)
  const [exporting, setExporting] = useState(false)
  const [trendData, setTrendData] = useState<TrendDataPoint[]>([])
  const [requestedModelStats, setRequestedModelStats] = useState<ModelStat[]>([])
  const [upstreamModelStats, setUpstreamModelStats] = useState<ModelStat[]>([])
  const [mappingModelStats, setMappingModelStats] = useState<ModelStat[]>([])
  const [groupStats, setGroupStats] = useState<GroupStat[]>([])
  const [chartsLoading, setChartsLoading] = useState(false)
  const [modelStatsLoading, setModelStatsLoading] = useState(false)
  const [modelDistributionMetric, setModelDistributionMetric] = useState<DistributionMetric>('tokens')
  const [modelDistributionSource, setModelDistributionSource] = useState<ModelDistributionSource>('requested')
  const loadedModelSources = useRef<Record<ModelDistributionSource, boolean>>({
    requested: false,
    upstream: false,
    mapping: false
  })
  const [groupDistributionMetric, setGroupDistributionMetric] = useState<DistributionMetric>('tokens')
  const [endpointDistributionMetric, setEndpointDistributionMetric] = useState<DistributionMetric>('tokens')
  const [endpointDistributionSource, setEndpointDistributionSource] = useState<EndpointSource>('inbound')
  const [inboundEndpointStats, setInboundEndpointStats] = useState<EndpointStat[]>([])
  const [upstreamEndpointStats, setUpstreamEndpointStats] = useState<EndpointStat[]>([])
  const [endpointPathStats, setEndpointPathStats] = useState<EndpointStat[]>([])
  const [endpointStatsLoading, setEndpointStatsLoading] = useState(false)

  const abortController = useRef<AbortController | null>(null)
  const exportAbortController = useRef<AbortController | null>(null)
  const chartReqSeq = useRef(0)
  const statsReqSeq = useRef(0)
  const modelStatsReqSeq = useRef(0)
  const [exportProgress, setExportProgress] = useState({
    show: false,
    progress: 0,
    current: 0,
    total: 0,
    estimatedTime: ''
  })
  const [cleanupDialogVisible, setCleanupDialogVisible] = useState(false)
  const [showBalanceHistoryModal, setShowBalanceHistoryModal] = useState(false)
  const [balanceHistoryUser, setBalanceHistoryUser] = useState<AdminUser | null>(null)

  // Error tab state
  const [errRows, setErrRows] = useState<OpsErrorLog[]>([])
  const [errLoading, setErrLoading] = useState(false)
  const [errPage, setErrPage] = useState(1)
  const [errPageSize, setErrPageSize] = useState(20)
  const [errTotal, setErrTotal] = useState(0)
  const [errSortBy, setErrSortBy] = useState('created_at')
  const [errSortOrder, setErrSortOrder] = useState<'asc' | 'desc'>('desc')
  const [errView, setErrView] = useState<UsageErrorView>('errors')
  const [showErrorModal, setShowErrorModal] = useState(false)
  const [selectedErrorId, setSelectedErrorId] = useState<number | null>(null)
  const errViewRef = useRef(errView)
  errViewRef.current = errView

  // Column visibility
  const hiddenColumnsKey = useColumnPreferenceKey(HIDDEN_COLUMNS_KEY)
  const errHiddenColumnsKey = useColumnPreferenceKey(ERR_HIDDEN_COLUMNS_KEY)
  const [hiddenColumns, setHiddenColumns] = useState<Set<string>>(new Set())
  const [errHiddenColumns, setErrHiddenColumns] = useState<Set<string>>(new Set())
  const [showColumnDropdown, setShowColumnDropdown] = useState(false)
  const columnDropdownRef = useRef<HTMLDivElement | null>(null)
  const [columnDropdownStyle, setColumnDropdownStyle] = useState<React.CSSProperties>({})
  const usageFiltersRef = useRef<{ getUserSearchRevision: () => number; setUserKeyword: (email: string) => void } | null>(
    null
  )
  const rankingRef = useRef<{ reload: () => void } | null>(null)

  const historySnapshot = useUsageSnapshot()

  const workspaceTabs = useMemo(
    () => [
      { value: 'charts', label: t('workspace.charts') },
      { value: 'records', label: t('workspace.usageRecords') }
    ],
    [t]
  )

  const detailTabs = useMemo(
    () => [
      { key: 'live' as const, label: t('inspection.live'), icon: 'bolt' as const },
      { key: 'usage' as const, label: t('usage.tabs.usage'), icon: 'document' as const },
      { key: 'errors' as const, label: t('usage.tabs.errors'), icon: 'exclamationTriangle' as const },
      { key: 'ranking' as const, label: t('usage.tabs.ranking'), icon: 'chart' as const },
      { key: 'disconnects' as const, label: t('usage.tabs.disconnects'), icon: 'shield' as const }
    ],
    [t]
  )

  const usageFilterMode = useMemo<UsageFilterMode>(
    () => (activeTab === 'disconnects' || activeTab === 'live' ? 'usage' : (activeTab as UsageFilterMode)),
    [activeTab]
  )

  const breakdownFilters = useMemo(() => {
    const result: Record<string, unknown> = {}
    if (filters.user_id) result.user_id = filters.user_id
    if (filters.api_key_id) result.api_key_id = filters.api_key_id
    if (filters.account_id) result.account_id = filters.account_id
    if (filters.group_id) result.group_id = filters.group_id
    if (filters.request_type != null) result.request_type = filters.request_type
    if (filters.native_compaction_v2 != null) result.native_compaction_v2 = filters.native_compaction_v2
    if (filters.billing_type != null) result.billing_type = filters.billing_type
    return result
  }, [filters])

  const modelNameOptions = useMemo(
    () => Array.from(new Set(requestedModelStats.map((m) => m.model).filter(Boolean))).sort(),
    [requestedModelStats]
  )

  const granularityOptions = useMemo(
    () => [
      { value: 'day', label: t('admin.dashboard.day') },
      { value: 'hour', label: t('admin.dashboard.hour') }
    ],
    [t]
  )

  const allColumns = useMemo<UsageColumn[]>(
    () => [
      { key: 'user', label: t('admin.usage.user'), sortable: false, sticky: 'left' },
      { key: 'api_key', label: t('usage.apiKeyFilter'), sortable: false, sticky: 'left' },
      { key: 'account', label: t('admin.usage.account'), sortable: false, sticky: 'left' },
      { key: 'model', label: t('usage.model'), sortable: true },
      { key: 'service_tier', label: t('admin.usage.serviceTier'), sortable: false },
      { key: 'endpoint', label: t('usage.endpoint'), sortable: false },
      { key: 'group', label: t('admin.usage.group'), sortable: false },
      { key: 'stream', label: t('usage.type'), sortable: false },
      { key: 'billing_mode', label: t('admin.usage.billingMode'), sortable: false },
      { key: 'tokens', label: t('usage.tokens'), sortable: false },
      { key: 'cost', label: t('usage.cost'), sortable: false },
      { key: 'latency', label: t('usage.latency'), sortable: false },
      { key: 'created_at', label: t('usage.time'), sortable: true },
      { key: 'request_id', label: t('admin.usage.requestId'), sortable: false },
      { key: 'session_id', label: t('usage.sessionId'), sortable: false },
      { key: 'ip_address', label: t('admin.usage.ipAddress'), sortable: false },
      { key: 'outbound', label: t('admin.usage.outbound'), sortable: false }
    ],
    [t]
  )

  const toggleableColumns = useMemo(
    () => allColumns.filter((col) => !ALWAYS_VISIBLE.includes(col.key)),
    [allColumns]
  )

  const visibleColumns = useMemo(
    () => allColumns.filter((col) => ALWAYS_VISIBLE.includes(col.key) || !hiddenColumns.has(col.key)),
    [allColumns, hiddenColumns]
  )

  const errAllColumns = useMemo<UsageColumn[]>(
    () =>
      ERR_ALL_COLUMNS.map((column) => ({
        key: column.key,
        label: t(column.label)
      })),
    [t]
  )

  const errToggleableColumns = useMemo(
    () => errAllColumns.filter((col) => !ERR_ALWAYS_VISIBLE.includes(col.key)),
    [errAllColumns]
  )

  const errVisibleColumnKeys = useMemo(
    () =>
      errAllColumns
        .filter((col) => ERR_ALWAYS_VISIBLE.includes(col.key) || !errHiddenColumns.has(col.key))
        .map((col) => col.key),
    [errAllColumns, errHiddenColumns]
  )

  const currentToggleableColumns = activeTab === 'errors' ? errToggleableColumns : toggleableColumns
  const isCurrentColumnVisible = (key: string) =>
    activeTab === 'errors' ? !errHiddenColumns.has(key) : !hiddenColumns.has(key)

  const toggleCurrentColumn = (key: string) => {
    if (activeTab === 'errors') {
      setErrHiddenColumns((prev) => {
        const next = new Set(prev)
        if (next.has(key)) next.delete(key)
        else next.add(key)
        try {
          localStorage.setItem(errHiddenColumnsKey, JSON.stringify([...next]))
        } catch (error) {
          console.error('Failed to save error columns:', error)
        }
        return next
      })
      return
    }
    setHiddenColumns((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      try {
        localStorage.setItem(hiddenColumnsKey, JSON.stringify([...next]))
      } catch (error) {
        console.error('Failed to save columns:', error)
      }
      return next
    })
  }

  const restoreRecommendedColumns = () => {
    const errors = activeTab === 'errors'
    const defaults = errors ? ERR_DEFAULT_HIDDEN_COLUMNS : DEFAULT_HIDDEN_COLUMNS
    if (errors) {
      setErrHiddenColumns(new Set(defaults))
    } else {
      setHiddenColumns(new Set(defaults))
    }
    try {
      localStorage.setItem(errors ? errHiddenColumnsKey : hiddenColumnsKey, JSON.stringify(defaults))
    } catch {
      /* storage unavailable */
    }
  }

  const loadSavedColumns = useCallback(() => {
    try {
      let saved = localStorage.getItem(hiddenColumnsKey)
      if (!saved) {
        const legacy = localStorage.getItem(LEGACY_HIDDEN_COLUMNS_KEY)
        if (legacy) {
          const values: unknown = JSON.parse(legacy)
          if (Array.isArray(values)) {
            saved = JSON.stringify([
              ...new Set([
                ...values.filter(
                  (key): key is string => typeof key === 'string' && key !== 'reasoning_effort' && key !== 'user_agent'
                ),
                'service_tier'
              ])
            ])
            localStorage.setItem(hiddenColumnsKey, saved)
          }
        }
      }
      if (saved) {
        const keys = JSON.parse(saved) as string[]
        setHiddenColumns(new Set(keys))
      } else {
        setHiddenColumns(new Set(DEFAULT_HIDDEN_COLUMNS))
      }
    } catch {
      setHiddenColumns(new Set(DEFAULT_HIDDEN_COLUMNS))
    }
  }, [hiddenColumnsKey])

  const loadSavedErrColumns = useCallback(() => {
    try {
      const saved = localStorage.getItem(errHiddenColumnsKey)
      const keys = saved ? (JSON.parse(saved) as string[]) : ERR_DEFAULT_HIDDEN_COLUMNS
      setErrHiddenColumns(new Set(keys))
    } catch {
      setErrHiddenColumns(new Set(ERR_DEFAULT_HIDDEN_COLUMNS))
    }
  }, [errHiddenColumnsKey])

  const buildUsageListParams = useCallback(
    (page: number, pageSize: number, exactTotal: boolean): AdminUsageQueryParams => {
      const current = filtersRef.current
      const requestType = current.request_type
      const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : current.stream
      return {
        page,
        page_size: pageSize,
        exact_total: exactTotal,
        ...current,
        snapshot_max_id: historySnapshot.forFilters(current),
        stream: legacyStream === null ? undefined : legacyStream,
        sort_by: sortStateRef.current.sort_by,
        sort_order: sortStateRef.current.sort_order
      }
    },
    [historySnapshot]
  )

  const loadLogs = useCallback(async () => {
    abortController.current?.abort()
    const controller = new AbortController()
    abortController.current = controller
    setLoading(true)
    try {
      const res = await adminAPI.usage.list(
        buildUsageListParams(paginationRef.current.page, paginationRef.current.page_size, false),
        { signal: controller.signal }
      )
      if (!controller.signal.aborted) {
        setUsageLogs(res.items)
        setPagination((prev) => ({ ...prev, total: res.total }))
        const isLatest =
          paginationRef.current.page === 1 &&
          sortStateRef.current.sort_by === 'created_at' &&
          sortStateRef.current.sort_order === 'desc'
        historySnapshot.accept(res, isLatest)
      }
    } catch (error: unknown) {
      if ((error as { name?: string })?.name !== 'AbortError') {
        console.error('Failed to load usage logs:', error)
      }
    } finally {
      if (abortController.current === controller) setLoading(false)
    }
  }, [buildUsageListParams, historySnapshot])

  const loadStats = useCallback(async (force = false) => {
    const seq = ++statsReqSeq.current
    setEndpointStatsLoading(true)
    try {
      const current = filtersRef.current
      const requestType = current.request_type
      const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : current.stream
      const stats = await adminAPI.usage.getStats({
        ...current,
        stream: legacyStream === null ? undefined : legacyStream,
        ...(force ? { nocache: 1 } : {})
      })
      if (seq !== statsReqSeq.current) return
      setUsageStats(stats)
      setInboundEndpointStats(stats.endpoints || [])
      setUpstreamEndpointStats(stats.upstream_endpoints || [])
      setEndpointPathStats(stats.endpoint_paths || [])
    } catch (error) {
      if (seq !== statsReqSeq.current) return
      console.error('Failed to load usage stats:', error)
      setInboundEndpointStats([])
      setUpstreamEndpointStats([])
      setEndpointPathStats([])
    } finally {
      if (seq === statsReqSeq.current) setEndpointStatsLoading(false)
    }
  }, [])

  // 失效模型统计缓存:仅标记需要重取,保留旧数据直到新数据到达(避免刷新时图表闪空)。
  const invalidateModelStatsCache = useCallback(() => {
    loadedModelSources.current = { requested: false, upstream: false, mapping: false }
  }, [])

  const loadModelStats = useCallback(
    async (source: ModelDistributionSource, force = false) => {
      if (!force && loadedModelSources.current[source]) return

      const seq = ++modelStatsReqSeq.current
      setModelStatsLoading(true)
      try {
        const current = filtersRef.current
        const requestType = current.request_type
        const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : current.stream
        const response = await adminAPI.dashboard.getModelStats({
          start_date: current.start_date || startDateRef.current,
          end_date: current.end_date || endDateRef.current,
          user_id: current.user_id,
          model: current.model,
          api_key_id: current.api_key_id,
          account_id: current.account_id,
          group_id: current.group_id,
          request_type: requestType,
          stream: legacyStream === null ? undefined : legacyStream,
          native_compaction_v2: current.native_compaction_v2,
          billing_type: current.billing_type,
          upstream_model_mismatch: current.upstream_model_mismatch,
          model_source: source
        })
        if (seq !== modelStatsReqSeq.current) return

        const models = response.models || []
        if (source === 'requested') setRequestedModelStats(models)
        else if (source === 'upstream') setUpstreamModelStats(models)
        else setMappingModelStats(models)
        loadedModelSources.current[source] = true
      } catch (error) {
        if (seq !== modelStatsReqSeq.current) return
        console.error('Failed to load model stats:', error)
        if (source === 'requested') setRequestedModelStats([])
        else if (source === 'upstream') setUpstreamModelStats([])
        else setMappingModelStats([])
        loadedModelSources.current[source] = false
      } finally {
        if (seq === modelStatsReqSeq.current) setModelStatsLoading(false)
      }
    },
    []
  )

  const loadChartData = useCallback(async () => {
    const seq = ++chartReqSeq.current
    setChartsLoading(true)
    try {
      const current = filtersRef.current
      const requestType = current.request_type
      const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : current.stream
      const snapshot = await adminAPI.dashboard.getSnapshotV2({
        start_date: current.start_date || startDateRef.current,
        end_date: current.end_date || endDateRef.current,
        granularity,
        user_id: current.user_id,
        model: current.model,
        api_key_id: current.api_key_id,
        account_id: current.account_id,
        group_id: current.group_id,
        request_type: requestType,
        stream: legacyStream === null ? undefined : legacyStream,
        native_compaction_v2: current.native_compaction_v2,
        billing_type: current.billing_type,
        upstream_model_mismatch: current.upstream_model_mismatch,
        include_stats: false,
        include_trend: true,
        include_model_stats: false,
        include_group_stats: true,
        include_users_trend: false
      })
      if (seq !== chartReqSeq.current) return
      setTrendData(snapshot.trend || [])
      setGroupStats(snapshot.groups || [])
    } catch (error) {
      console.error('Failed to load chart data:', error)
    } finally {
      if (seq === chartReqSeq.current) setChartsLoading(false)
    }
  }, [granularity])

  const loadAdminErrors = useCallback(async () => {
    setErrLoading(true)
    try {
      const current = filtersRef.current
      const resp = await listErrorLogs({
        page: errPage,
        page_size: errPageSize,
        view: errViewRef.current,
        time_range: errorDatePreset === 'last24Hours' ? '24h' : undefined,
        start_time: errorDatePreset === 'last24Hours' ? undefined : toRFC3339(current.start_date),
        end_time: errorDatePreset === 'last24Hours' ? undefined : toRFC3339(current.end_date, true),
        user_id: current.user_id ?? undefined,
        api_key_id: current.api_key_id ?? undefined,
        account_id: current.account_id ?? undefined,
        group_id: current.group_id ?? undefined,
        model: current.model || undefined,
        phase: current.error_phase || undefined,
        category: current.error_category || undefined,
        status_codes: current.status_code != null ? String(current.status_code) : undefined,
        sort_by: errSortBy,
        sort_order: errSortOrder
      })
      setErrRows(resp.items)
      setErrTotal(resp.total)
    } catch (error) {
      console.error('Failed to load admin errors:', error)
      showError(t('usage.errors.failedToLoad'))
    } finally {
      setErrLoading(false)
    }
  }, [errPage, errPageSize, errorDatePreset, errSortBy, errSortOrder, showError, t])

  const applyFilters = useCallback(() => {
    setPagination((prev) => ({ ...prev, page: 1 }))
    invalidateModelStatsCache()
    void loadLogs()
    void loadStats()
    void loadModelStats(modelDistributionSource, true)
    void loadChartData()
    setErrPage(1)
    if (activeTab === 'errors') {
      void loadAdminErrors()
    } else {
      setErrRows([])
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    invalidateModelStatsCache,
    loadLogs,
    loadStats,
    loadModelStats,
    loadChartData,
    modelDistributionSource,
    activeTab
  ])

  const refreshData = useCallback(async () => {
    historySnapshot.reset()
    invalidateModelStatsCache()
    await Promise.all([
      loadLogs(),
      loadStats(true),
      loadModelStats(modelDistributionSource, true),
      loadChartData(),
      ...(activeTab === 'errors' ? [loadAdminErrors()] : [])
    ])
    if (rankingMounted) await rankingRef.current?.reload()
  }, [
    historySnapshot,
    invalidateModelStatsCache,
    loadLogs,
    loadStats,
    loadModelStats,
    loadChartData,
    loadAdminErrors,
    modelDistributionSource,
    activeTab,
    rankingMounted
  ])

  const resetFilters = useCallback(() => {
    const range = getLast24HoursRangeDates()
    updateStartDate(range.start)
    updateEndDate(range.end)
    setErrorDatePreset('last24Hours')
    updateFilters({
      start_date: range.start,
      end_date: range.end,
      request_type: undefined,
      native_compaction_v2: null,
      billing_type: null,
      billing_mode: undefined
    })
    setGranularity(getGranularityForRange(range.start, range.end))
    applyFilters()
  }, [applyFilters, updateEndDate, updateFilters, updateStartDate])

  const onDateRangeChange = useCallback(
    (range: { startDate: string; endDate: string; preset: string | null }) => {
      updateStartDate(range.startDate)
      updateEndDate(range.endDate)
      setErrorDatePreset(range.preset)
      updateFilters({ ...filtersRef.current, start_date: range.startDate, end_date: range.endDate })
      setGranularity(getGranularityForRange(range.startDate, range.endDate))
      applyFilters()
    },
    [applyFilters, updateEndDate, updateFilters, updateStartDate]
  )

  const handlePageChange = useCallback(
    (page: number) => {
      setPagination((prev) => ({ ...prev, page }))
      void loadLogs()
    },
    [loadLogs]
  )

  const handlePageSizeChange = useCallback(
    (pageSize: number) => {
      setPagination((prev) => ({ ...prev, page_size: pageSize, page: 1 }))
      void loadLogs()
    },
    [loadLogs]
  )

  const handleSort = useCallback(
    (key: string, order: 'asc' | 'desc') => {
      setSortState({ sort_by: key, sort_order: order })
      setPagination((prev) => ({ ...prev, page: 1 }))
      void loadLogs()
    },
    [loadLogs]
  )

  const onErrSort = useCallback(
    (sortBy: string, sortOrder: 'asc' | 'desc') => {
      setErrSortBy(sortBy)
      setErrSortOrder(sortOrder)
      setErrPage(1)
      void loadAdminErrors()
    },
    [loadAdminErrors]
  )

  const onErrPage = useCallback(
    (page: number) => {
      setErrPage(page)
      void loadAdminErrors()
    },
    [loadAdminErrors]
  )

  const onErrPageSize = useCallback(
    (pageSize: number) => {
      setErrPageSize(pageSize)
      setErrPage(1)
      void loadAdminErrors()
    },
    [loadAdminErrors]
  )

  const onErrorViewChange = useCallback(
    (view: UsageErrorView) => {
      if (errViewRef.current === view) return
      errViewRef.current = view
      setErrView(view)
      setErrPage(1)
      if (activeTab === 'errors') void loadAdminErrors()
    },
    [activeTab, loadAdminErrors]
  )

  const openError = useCallback((id: number) => {
    setSelectedErrorId(id)
    setShowErrorModal(true)
  }, [])

  const handleIpGeoBatchFailed = useCallback(() => {
    showError(t('usage.ipGeo.batchFailed'))
  }, [showError, t])

  const cancelExport = useCallback(() => exportAbortController.current?.abort(), [])

  const getRequestTypeLabel = useCallback((log: AdminUsageLog): string => {
    const requestType = resolveUsageRequestType(log)
    if (requestType === 'cyber') return t('usage.cyber')
    if (requestType === 'live') return t('usage.live')
    if (requestType === 'ws_v2') return t('usage.ws')
    if (requestType === 'stream') return t('usage.stream')
    if (requestType === 'sync') return t('usage.sync')
    return t('usage.unknown')
  }, [t])

  const formatTpsNote = useCallback((log: AdminUsageLog): string => {
    const reason = tpsNote(log)
    return reason ? t(reason) : ''
  }, [t])

  const exportToExcel = useCallback(async () => {
    if (exporting) return
    setExporting(true)
    setExportProgress({ show: true, progress: 0, current: 0, total: 0, estimatedTime: '' })
    const controller = new AbortController()
    exportAbortController.current = controller
    const exportParams = buildUsageListParams(1, 100, true)
    try {
      let page = 1
      let total = paginationRef.current.total
      let exportedCount = 0
      const headers = [
        t('usage.time'),
        t('admin.usage.user'),
        t('usage.apiKeyFilter'),
        t('admin.usage.account'),
        t('usage.requestedModel'),
        t('usage.sentUpstreamModel'),
        t('usage.upstreamResponseModel'),
        t('usage.upstreamModelMismatch'),
        t('usage.requestedReasoningEffort'),
        t('usage.reasoningEffort'),
        t('admin.usage.group'),
        t('usage.inboundEndpoint'),
        t('usage.upstreamEndpoint'),
        t('usage.type'),
        t('admin.usage.inputTokens'),
        t('admin.usage.outputTokens'),
        t('admin.usage.cacheReadTokens'),
        t('admin.usage.cacheCreationTokens'),
        t('admin.usage.inputCost'),
        t('admin.usage.outputCost'),
        t('admin.usage.cacheReadCost'),
        t('admin.usage.cacheCreationCost'),
        t('usage.rate'),
        t('usage.accountMultiplier'),
        t('usage.original'),
        t('usage.userBilled'),
        t('usage.accountBilled'),
        t('usage.latencyFirstByte'),
        t('usage.latencyFirstToken'),
        t('usage.latencyFirstOutput'),
        t('usage.latencyFirstOutputKind'),
        t('usage.duration'),
        t('usage.averageTps'),
        t('usage.averageTpsNote'),
        t('admin.usage.requestId'),
        t('usage.sessionId'),
        t('usage.userAgent'),
        t('admin.usage.ipAddress')
      ]
      const rows: Array<Array<string | number | null | undefined>> = [headers]
      while (true) {
        const res = await adminUsageAPI.list({ ...exportParams, page }, { signal: controller.signal })
        if (controller.signal.aborted) break
        if (page === 1) {
          total = res.total
          setExportProgress((prev) => ({ ...prev, total }))
          if (res.snapshot_max_id != null) exportParams.snapshot_max_id = res.snapshot_max_id
        }
        const pageRows = (res.items || []).map((log: AdminUsageLog) => [
          log.created_at,
          log.user?.email || '',
          log.api_key?.name || '',
          log.account?.name || '',
          log.model,
          log.upstream_model || log.model,
          log.upstream_response_model || '',
          log.upstream_model_mismatch == null ? '' : t(log.upstream_model_mismatch ? 'common.yes' : 'common.no'),
          formatReasoningEffort(log.reasoning_effort),
          formatReasoningEffort(log.upstream_reasoning_effort || log.reasoning_effort),
          log.group?.name || '',
          log.inbound_endpoint || '',
          log.upstream_endpoint || '',
          getRequestTypeLabel(log),
          log.input_tokens,
          log.output_tokens,
          log.cache_read_tokens,
          log.cache_creation_tokens,
          log.input_cost?.toFixed(6) || '0.000000',
          log.output_cost?.toFixed(6) || '0.000000',
          log.cache_read_cost?.toFixed(6) || '0.000000',
          log.cache_creation_cost?.toFixed(6) || '0.000000',
          log.rate_multiplier?.toPrecision(4) || '1.00',
          (log.account_rate_multiplier ?? 1).toPrecision(4),
          log.total_cost?.toFixed(6) || '0.000000',
          log.actual_cost?.toFixed(6) || '0.000000',
          ((log.account_stats_cost ?? log.total_cost) * (log.account_rate_multiplier ?? 1)).toFixed(6),
          firstByteMs(log) ?? '',
          strictFirstTokenMs(log) ?? '',
          log.first_output_ms ?? '',
          log.first_output_kind ?? '',
          log.duration_ms ?? '',
          averageTps(log) ?? '',
          formatTpsNote(log),
          log.request_id || '',
          log.session_id || '',
          log.user_agent || '',
          log.ip_address || ''
        ])
        if (pageRows.length) rows.push(...pageRows)
        exportedCount += pageRows.length
        setExportProgress((prev) => ({
          ...prev,
          current: exportedCount,
          progress: total > 0 ? Math.min(100, Math.round((exportedCount / total) * 100)) : 0
        }))
        if (exportedCount >= total || res.items.length < 100) break
        page += 1
      }
      if (!controller.signal.aborted) {
        saveAs(
          buildXlsxBlob({ name: 'Usage', rows }),
          `usage_${exportParams.start_date}_to_${exportParams.end_date}.xlsx`
        )
        showSuccess(t('usage.exportSuccess'))
      }
    } catch (error) {
      console.error('Failed to export:', error)
      showError('Export Failed')
    } finally {
      if (exportAbortController.current === controller) {
        exportAbortController.current = null
        setExporting(false)
        setExportProgress((prev) => ({ ...prev, show: false }))
      }
    }
  }, [buildUsageListParams, exporting, showError, showSuccess, t, getRequestTypeLabel, formatTpsNote])

  const handleUserClick = useCallback(
    async (userId: number) => {
      try {
        const user = await adminAPI.users.getById(userId, true)
        setBalanceHistoryUser(user)
        setShowBalanceHistoryModal(true)
      } catch {
        showError(t('admin.usage.failedToLoadUser'))
      }
    },
    [showError, t]
  )

  // Drill down from the per-user token ranking: scope the whole usage view to
  // that user and jump to the usage-detail tab so the drill-down is visible.
  const handleRankingSelectUser = useCallback(
    (userId: number, email: string) => {
      updateFilters({ ...filtersRef.current, user_id: userId })
      usageFiltersRef.current?.setUserKeyword(email || '')
      setWorkspaceTab('records')
      setActiveTab('usage')
      applyFilters()
    },
    [applyFilters, updateFilters]
  )

  const switchTab = useCallback(
    (tab: DetailTab) => {
      setWorkspaceTab('records')
      setActiveTab(tab)
      if (tab === 'errors' && errRows.length === 0) void loadAdminErrors()
      if (tab === 'ranking') setRankingMounted(true)
      if (tab === 'disconnects') setDisconnectEventsMounted(true)
    },
    [errRows.length, loadAdminErrors]
  )

  useEffect(() => {
    if (activeTab !== 'live') lastHistoryTab.current = activeTab
  }, [activeTab])

  useEffect(() => {
    if (workspaceTab === 'charts' && activeTab === 'live') {
      setActiveTab(lastHistoryTab.current)
    }
    const timer = window.setTimeout(() => {
      window.dispatchEvent(new Event('resize'))
    }, 0)
    return () => window.clearTimeout(timer)
  }, [workspaceTab, activeTab])

  useEffect(() => {
    void loadModelStats(modelDistributionSource)
  }, [modelDistributionSource, loadModelStats])

  const getSingleQueryValue = (value: string | null): string | undefined =>
    typeof value === 'string' && value.length > 0 ? value : undefined

  const getNumericQueryValue = (value: string | null): number | undefined => {
    const raw = getSingleQueryValue(value)
    if (!raw) return undefined
    const parsed = Number(raw)
    return Number.isFinite(parsed) ? parsed : undefined
  }

  const loadRouteUserFilterLabel = useCallback(async () => {
    const requestedUserId = filtersRef.current.user_id
    if (!requestedUserId) return
    const userSearchRevision = usageFiltersRef.current?.getUserSearchRevision() ?? 0

    const routeUserFilterIsCurrent = () =>
      filtersRef.current.user_id === requestedUserId &&
      (usageFiltersRef.current?.getUserSearchRevision() ?? 0) === userSearchRevision

    try {
      const user = await adminAPI.users.getById(requestedUserId, true)
      if (!routeUserFilterIsCurrent()) return
      usageFiltersRef.current?.setUserKeyword(user.email || String(requestedUserId))
    } catch {
      if (!routeUserFilterIsCurrent()) return
      usageFiltersRef.current?.setUserKeyword(String(requestedUserId))
    }
  }, [])

  // Mount: drop the legacy `record` query flag, apply deep-linked filters, load data.
  useEffect(() => {
    if (searchParams.has('record')) {
      const next = new URLSearchParams(searchParams)
      next.delete('record')
      setSearchParams(next, { replace: true })
    }

    const queryStartDate = getSingleQueryValue(searchParams.get('start_date'))
    const queryEndDate = getSingleQueryValue(searchParams.get('end_date'))
    const queryUserId = getNumericQueryValue(searchParams.get('user_id'))

    if (queryStartDate) {
      setStartDate(queryStartDate)
      setErrorDatePreset(null)
    }
    if (queryEndDate) {
      setEndDate(queryEndDate)
      setErrorDatePreset(null)
    }
    setFilters((prev) => {
      const next = { ...prev, user_id: queryUserId, start_date: queryStartDate ?? prev.start_date, end_date: queryEndDate ?? prev.end_date }
      filtersRef.current = next
      return next
    })
    setGranularity(
      getGranularityForRange(queryStartDate ?? startDateRef.current, queryEndDate ?? endDateRef.current)
    )

    loadSavedColumns()
    loadSavedErrColumns()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    void loadRouteUserFilterLabel()
  }, [loadRouteUserFilterLabel])

  useEffect(() => {
    void loadLogs()
    void loadStats()
    void loadModelStats(modelDistributionSource, true)
    const timer = window.setTimeout(() => {
      void loadChartData()
    }, 120)
    return () => window.clearTimeout(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    document.addEventListener('click', handleColumnClickOutside)
    return () => {
      document.removeEventListener('click', handleColumnClickOutside)
      abortController.current?.abort()
      exportAbortController.current?.abort()
    }
  }, [])

  // Keep the anchored column dropdown aligned with its trigger while open.
  useEffect(() => {
    if (!showColumnDropdown) return
    const update = () => {
      if (!columnDropdownRef.current) return
      const rect = columnDropdownRef.current.getBoundingClientRect()
      setColumnDropdownStyle({ position: 'fixed', top: `${rect.bottom + 6}px`, right: '12px' })
    }
    update()
    window.addEventListener('resize', update)
    window.addEventListener('scroll', update, { capture: true, passive: true })
    return () => {
      window.removeEventListener('resize', update)
      window.removeEventListener('scroll', update, true)
    }
  }, [showColumnDropdown])

  function handleColumnClickOutside(event: MouseEvent) {
    if (columnDropdownRef.current && !columnDropdownRef.current.contains(event.target as HTMLElement)) {
      setShowColumnDropdown(false)
    }
  }

  const columnMenu = showColumnDropdown ? (
    <div
      className="absolute right-0 top-full z-50 mt-1 max-h-80 w-48 space-y-0.5 overflow-y-auto border border-gray-200 p-1 shadow-lg dark:border-dark-600 surface-panel"
      style={columnDropdownStyle}
      data-testid="usage-column-menu"
      onClick={(event) => event.stopPropagation()}
    >
      {currentToggleableColumns.map((col) => (
        <button
          key={col.key}
          type="button"
          data-testid={`usage-column-toggle-${col.key}`}
          onClick={() => toggleCurrentColumn(col.key)}
          className="flex w-full items-center justify-between rounded-[var(--surface-radius-item)] px-3 py-2 text-left text-sm hover:surface-hover surface-control surface-muted"
        >
          <span>{col.label}</span>
          {isCurrentColumnVisible(col.key) && <Check className="h-4 w-4 text-primary-500" strokeWidth={2} />}
        </button>
      ))}
      <button
        type="button"
        className="w-full rounded-[var(--surface-radius-item)] border-t px-3 py-2 text-left text-sm text-primary-600 hover:surface-hover surface-control"
        onClick={restoreRecommendedColumns}
      >
        {t('inspection.resetColumns')}
      </button>
    </div>
  ) : null

  return (
    <div className="workspace-page usage-workspace">
      {/* One card owns the workspace tabs and every shared query condition. */}
      <div
        className="usage-query-card card workspace-fixed flex flex-col gap-3 px-5 py-3"
        data-testid="usage-query-card"
      >
        <div className="flex flex-wrap items-center justify-between gap-4">
          <WorkspaceTabs
            value={workspaceTab}
            tabs={workspaceTabs}
            label={t('usage.tabs.usage')}
            embedded
            onValueChange={(value) => setWorkspaceTab(value as 'charts' | 'records')}
          />
          {activeTab !== 'live' && (
            <div className="filter-bar" data-testid="usage-chart-conditions">
              <div className="filter-pair">
                <span className="text-sm font-medium surface-muted">{t('admin.dashboard.timeRange')}:</span>
                <div className="filter-field">
                  <DateRangePicker
                    startDate={startDate}
                    endDate={endDate}
                    onStartDateChange={updateStartDate}
                    onEndDateChange={updateEndDate}
                    onChange={onDateRangeChange}
                  />
                </div>
              </div>
              <div className="filter-pair">
                <span className="text-sm font-medium surface-muted">{t('admin.dashboard.granularity')}:</span>
                <div className="filter-field">
                  <Select
                    value={granularity}
                    options={granularityOptions}
                    onValueChange={(value) => setGranularity((value as 'day' | 'hour') ?? 'hour')}
                    onChange={() => void loadChartData()}
                  />
                </div>
              </div>
            </div>
          )}
        </div>

        {/* Records-tab controls live in the same card, below the workspace tabs. */}
        {workspaceTab === 'records' && (
          <div className="usage-record-controls">
            <div
              className="usage-detail-tabs card-tabs workspace-fixed"
              role="tablist"
              aria-label={t('usage.tabs.usage')}
            >
              {detailTabs.map((tab) => (
                <button
                  key={tab.key}
                  type="button"
                  data-tab={tab.key}
                  role="tab"
                  aria-selected={activeTab === tab.key}
                  data-testid="usage-detail-tab"
                  className="card-tab"
                  onClick={() => switchTab(tab.key)}
                >
                  <Icon name={tab.icon} size="sm" />
                  {tab.label}
                </button>
              ))}
            </div>

            {activeTab !== 'disconnects' && activeTab !== 'live' && (
              <UsageFilters
                ref={usageFiltersRef}
                workspace
                filters={filters}
                onFiltersChange={updateFilters}
                exporting={exporting}
                startDate={startDate}
                endDate={endDate}
                mode={usageFilterMode}
                errorView={errView}
                onErrorViewChange={onErrorViewChange}
                flat
                className="workspace-fixed"
                modelOptions={modelNameOptions}
                onChange={applyFilters}
                onRefresh={() => void refreshData()}
                onReset={resetFilters}
                onCleanup={() => setCleanupDialogVisible(true)}
                onExport={() => void exportToExcel()}
                afterReset={
                  activeTab !== 'ranking' ? (
                    <div className="relative" ref={columnDropdownRef}>
                      <button
                        type="button"
                        data-testid="usage-column-settings"
                        onClick={() => setShowColumnDropdown((prev) => !prev)}
                        onKeyDown={(event) => {
                          if (event.key === 'Escape') {
                            event.stopPropagation()
                            event.preventDefault()
                            setShowColumnDropdown(false)
                          }
                        }}
                        className="btn btn-secondary px-2 md:px-3"
                        title={t('admin.users.columnSettings')}
                      >
                        <svg
                          className="h-4 w-4 md:mr-1.5"
                          fill="none"
                          stroke="currentColor"
                          viewBox="0 0 24 24"
                          strokeWidth="1.5"
                        >
                          <path
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            d="M9 4.5v15m6-15v15m-10.875 15h15.75c.621 0 1.125-.504 1.125-1.125V5.625c0-.621-.504-1.125-1.125-1.125H4.125C3.504 4.5 3 4.004 3 5.625v12.75c0 .621.504 1.125 1.125 1.125z"
                          />
                        </svg>
                        <span className="hidden md:inline">{t('admin.users.columnSettings')}</span>
                      </button>
                      {workspaceTab === 'records' && createPortal(columnMenu, document.body)}
                    </div>
                  ) : undefined
                }
              />
            )}
          </div>
        )}
      </div>

      {workspaceTab === 'charts' && activeTab !== 'live' && (
        <section className="workspace-shell workspace-scroll space-y-4" data-testid="usage-charts-panel">
          <UsageStatsCards stats={usageStats} />
          <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
            <ModelDistributionChart
              modelStats={requestedModelStats}
              upstreamModelStats={upstreamModelStats}
              mappingModelStats={mappingModelStats}
              loading={modelStatsLoading}
              showSourceToggle
              showMetricToggle
              metric={modelDistributionMetric}
              source={modelDistributionSource}
              startDate={startDate}
              endDate={endDate}
              filters={breakdownFilters}
              onMetricChange={setModelDistributionMetric}
              onSourceChange={setModelDistributionSource}
            />
            <GroupDistributionChart
              groupStats={groupStats}
              loading={chartsLoading}
              showMetricToggle
              metric={groupDistributionMetric}
              startDate={startDate}
              endDate={endDate}
              filters={breakdownFilters}
              onMetricChange={setGroupDistributionMetric}
            />
          </div>
          <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
            <EndpointDistributionChart
              endpointStats={inboundEndpointStats}
              upstreamEndpointStats={upstreamEndpointStats}
              endpointPathStats={endpointPathStats}
              loading={endpointStatsLoading}
              showSourceToggle
              showMetricToggle
              title={t('usage.endpointDistribution')}
              metric={endpointDistributionMetric}
              source={endpointDistributionSource}
              startDate={startDate}
              endDate={endDate}
              filters={breakdownFilters}
              onMetricChange={setEndpointDistributionMetric}
              onSourceChange={setEndpointDistributionSource}
            />
            <TokenUsageTrend trendData={trendData} loading={chartsLoading} />
          </div>
        </section>
      )}

      {workspaceTab === 'records' && (
        <div className="workspace-body usage-records-workspace" data-testid="usage-records-panel">
          {activeTab === 'usage' && (
            <div className="workspace-body usage-history-workspace gap-4">
              <AdminUsageTable
                workspace
                modelMetadata
                showServiceTier
                data={usageLogs}
                loading={loading}
                columns={visibleColumns}
                serverSideSort
                defaultSortKey="created_at"
                defaultSortOrder="desc"
                onSort={handleSort}
                onUserClick={(userId) => void handleUserClick(userId)}
                onIpGeoBatchFailed={handleIpGeoBatchFailed}
              />
              <Pagination
                className="workspace-fixed"
                page={pagination.page}
                total={pagination.total}
                pageSize={pagination.page_size}
                onPageChange={handlePageChange}
                onPageSizeChange={handlePageSizeChange}
              />
            </div>
          )}

          {activeTab === 'live' && <LiveRequestsPanel workspace mode="admin" />}

          {activeTab === 'errors' && (
            <div className="workspace-body usage-errors-workspace gap-4">
              <OpsErrorLogTable
                workspace
                compact
                activeRowKey={showErrorModal ? (selectedErrorId ?? undefined) : undefined}
                rows={errRows}
                total={errTotal}
                loading={errLoading}
                page={errPage}
                pageSize={errPageSize}
                visibleColumnKeys={errVisibleColumnKeys}
                userClickable
                onUserClick={(userId) => void handleUserClick(userId)}
                onOpenErrorDetail={openError}
                onSort={onErrSort}
                onPageChange={onErrPage}
                onPageSizeChange={onErrPageSize}
                onIpGeoBatchFailed={handleIpGeoBatchFailed}
              />
              <OpsErrorDetailModal
                show={showErrorModal}
                errorId={selectedErrorId}
                errorType="request"
                onClose={() => setShowErrorModal(false)}
              />
            </div>
          )}

          {/* 懒挂载：首次切到该 tab 才请求排行数据，之后随筛选自动刷新 */}
          {rankingMounted && (
            <div
              className="card workspace-table-card workspace-shell workspace-body !gap-0"
              hidden={activeTab !== 'ranking'}
            >
              <UserTokenRanking
                ref={rankingRef}
                workspace
                startDate={startDate}
                endDate={endDate}
                filters={breakdownFilters}
                model={filters.model}
                totalTokens={usageStats?.total_tokens ?? 0}
                onSelectUser={handleRankingSelectUser}
              />
            </div>
          )}

          {disconnectEventsMounted && (
            <ClientDisconnectEventsPanel workspace hidden={activeTab !== 'disconnects'} />
          )}
        </div>
      )}

      <UsageExportProgress
        show={exportProgress.show}
        progress={exportProgress.progress}
        current={exportProgress.current}
        total={exportProgress.total}
        estimatedTime={exportProgress.estimatedTime}
        onCancel={cancelExport}
      />
      <UsageCleanupDialog
        show={cleanupDialogVisible}
        filters={filters}
        startDate={startDate}
        endDate={endDate}
        onClose={() => setCleanupDialogVisible(false)}
      />
      {/* Balance history modal triggered from usage table user click */}
      <UserBalanceHistoryModal
        show={showBalanceHistoryModal}
        user={balanceHistoryUser}
        hideActions
        onClose={() => {
          setShowBalanceHistoryModal(false)
          setBalanceHistoryUser(null)
        }}
      />
    </div>
  )
}
