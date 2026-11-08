import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { DateRangePicker } from '@/components/DateRangePicker'
import { Pagination, Select, toast } from '@/components/ui'
import { keysAPI, usageAPI, userGroupsAPI } from '@/api'
import { useAppStore } from '@/stores/app'
import { FeatureFlags, resolveFeatureFlag } from '@/utils/featureFlags'
import { formatReasoningEffort } from '@/utils/format'
import { getBillingModeLabel, getDisplayBillingMode } from '@/utils/billingMode'
import { requestTypeToLegacyStream, resolveUsageRequestType } from '@/utils/usageRequestType'
import { COMMON_ERROR_STATUS_CODES } from '@/utils/errorBadges'
import { firstByteMs, strictFirstTokenMs, averageTps, tpsNote } from '@/utils/usageTiming'
import type { ApiKey, Group, UsageLog, UsageQueryParams, UserErrorRequest } from '@/types'
import type { Column } from '@/components/DataTable'
import { useColumnPreferenceKey } from './useColumnPreferenceKey'
import { useUsageSnapshot, getPersistedPageSize } from './useUsageSnapshot'
import { useLocaleRefresh as subscribeLocaleRefresh } from '@/utils/localeRefresh'
import { Icon } from './icons'
import { UsageTable } from './UsageTable'
import { LiveRequestsPanel } from '@/views/admin/parts/LiveRequestsPanel'
import { UserErrorRequestsTable } from './UserErrorRequestsTable'

const ALWAYS_VISIBLE = ['created_at']
const DEFAULT_HIDDEN_COLUMNS = ['user_agent']
const ERR_ALWAYS_VISIBLE = ['status', 'created_at']
const ERR_DEFAULT_HIDDEN_COLUMNS = ['user_agent', 'endpoint', 'client_ip', 'group', 'type', 'platform']

const errorCategoryCodes = [
  'auth',
  'rate_limit',
  'quota',
  'invalid_request',
  'service_unavailable',
  'upstream',
  'internal',
  'cyber',
  'security_audit'
]

const EXPORT_PAGE_SIZE = 1000
const EXPORT_RATE_LIMIT_MAX_RETRIES = 3
const EXPORT_RATE_LIMIT_MAX_WAIT_SECONDS = 60

function formatLocalDate(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

function getLast24HoursRangeDates(): { start: string; end: string } {
  const end = new Date()
  const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
  return { start: formatLocalDate(start), end: formatLocalDate(end) }
}

export default function UsageView() {
  const { t } = useTranslation()
  const appStore = useAppStore()
  const [searchParams, setSearchParams] = useSearchParams()

  const defaultRange = getLast24HoursRangeDates()
  const [startDate, setStartDate] = useState(defaultRange.start)
  const [endDate, setEndDate] = useState(defaultRange.end)
  const [activeTab, setActiveTab] = useState<'usage' | 'errors' | 'live'>('usage')

  const [usageLogs, setUsageLogs] = useState<UsageLog[]>([])
  const [loading, setLoading] = useState(false)
  const [exporting, setExporting] = useState(false)
  const [errorRows, setErrorRows] = useState<UserErrorRequest[]>([])
  const [errorLoading, setErrorLoading] = useState(false)
  const [errorPage, setErrorPage] = useState(1)
  const [errorPageSize, setErrorPageSize] = useState(20)
  const [errorSortBy, setErrorSortBy] = useState('created_at')
  const [errorSortOrder, setErrorSortOrder] = useState<'asc' | 'desc'>('desc')
  const [errorTotal, setErrorTotal] = useState(0)
  const [errorFilter, setErrorFilter] = useState<{
    model: string | null
    category: string
    api_key_id: number | null
    status_code: number | null
  }>({ model: '', category: '', api_key_id: null, status_code: null })

  const [filters, setFilters] = useState<UsageQueryParams>({
    start_date: defaultRange.start,
    end_date: defaultRange.end,
    request_type: undefined,
    native_compaction_v2: null,
    billing_type: null,
    billing_mode: null
  })
  const [pagination, setPagination] = useState({ page: 1, page_size: getPersistedPageSize(), total: 0 })
  const [sortState, setSortState] = useState<{ sort_by: string; sort_order: 'asc' | 'desc' }>({
    sort_by: 'created_at',
    sort_order: 'desc'
  })
  const [apiKeys, setApiKeys] = useState<ApiKey[]>([])
  const [groups, setGroups] = useState<Group[]>([])
  const [modelOptionValues, setModelOptionValues] = useState<string[]>([])

  const [hiddenColumns, setHiddenColumns] = useState<Set<string>>(new Set())
  const [errHiddenColumns, setErrHiddenColumns] = useState<Set<string>>(new Set())
  const [showColumnDropdown, setShowColumnDropdown] = useState(false)
  const columnDropdownRef = useRef<HTMLDivElement>(null)
  const [columnDropdownStyle, setColumnDropdownStyle] = useState<React.CSSProperties>({})

  const abortController = useRef<AbortController | null>(null)
  const filterRequest = useRef(0)
  const historySnapshot = useUsageSnapshot()

  const HIDDEN_COLUMNS_KEY = useColumnPreferenceKey('user-usage-restored-v2-hidden-columns')
  const ERR_HIDDEN_COLUMNS_KEY = useColumnPreferenceKey('user-usage-error-hidden-columns')

  const errorViewEnabled = appStore.cachedPublicSettings?.allow_user_view_error_requests ?? false
  const subscriptionFeatureEnabled = resolveFeatureFlag(appStore.cachedPublicSettings, FeatureFlags.subscription)

  const allColumns = useMemo<Column[]>(
    () => [
      { key: 'api_key', label: t('usage.apiKeyFilter'), sortable: false },
      { key: 'model', label: t('usage.model'), sortable: true },
      { key: 'endpoint', label: t('usage.endpoint'), sortable: false },
      { key: 'ip_address', label: 'IP', sortable: false },
      { key: 'group', label: t('admin.usage.group'), sortable: false },
      { key: 'stream', label: t('usage.type'), sortable: false },
      { key: 'billing_mode', label: t('admin.usage.billingMode'), sortable: false },
      { key: 'tokens', label: t('usage.tokens'), sortable: false },
      { key: 'cost', label: t('usage.cost'), sortable: false },
      { key: 'latency', label: t('usage.latency'), sortable: false },
      { key: 'created_at', label: t('usage.time'), sortable: true },
      { key: 'user_agent', label: t('usage.userAgent'), sortable: false }
    ],
    [t]
  )

  const errAllColumns = useMemo<Column[]>(
    () => [
      { key: 'key_name', label: t('usage.errors.keyName') },
      { key: 'model', label: t('usage.errors.model') },
      { key: 'endpoint', label: t('usage.errors.endpoint') },
      { key: 'client_ip', label: 'IP' },
      { key: 'group', label: t('admin.usage.group') },
      { key: 'type', label: t('usage.type') },
      { key: 'platform', label: t('usage.errors.platform') },
      { key: 'category', label: t('usage.errors.category') },
      { key: 'status', label: t('usage.errors.status') },
      { key: 'message', label: t('usage.errors.message') },
      { key: 'created_at', label: t('usage.errors.time') },
      { key: 'user_agent', label: t('usage.userAgent') }
    ],
    [t]
  )

  const toggleableColumns = allColumns.filter((col) => !ALWAYS_VISIBLE.includes(col.key))
  const visibleColumns = allColumns.filter((col) => ALWAYS_VISIBLE.includes(col.key) || !hiddenColumns.has(col.key))
  const errToggleableColumns = errAllColumns.filter((col) => !ERR_ALWAYS_VISIBLE.includes(col.key))
  const errVisibleColumnKeys = errAllColumns
    .filter((col) => ERR_ALWAYS_VISIBLE.includes(col.key) || !errHiddenColumns.has(col.key))
    .map((col) => col.key)

  const errorKeyOptions = useMemo(
    () => [
      { value: null as number | null, label: t('usage.errors.allKeys') },
      ...apiKeys.map((k) => ({ value: k.id as number | null, label: k.name }))
    ],
    [apiKeys, t]
  )

  const errorModelOptions = useMemo(() => {
    const seen = new Set<string>()
    const opts: Array<{ value: string; label: string }> = []
    for (const r of errorRows) {
      if (r.model && !seen.has(r.model)) {
        seen.add(r.model)
        opts.push({ value: r.model, label: r.model })
      }
    }
    return opts
  }, [errorRows])

  const errorCategoryOptions = useMemo(
    () => [
      { value: '', label: t('usage.errors.allCategories') },
      ...errorCategoryCodes.map((c) => ({ value: c, label: t('usage.errors.categories.' + c) }))
    ],
    [t]
  )

  const errorStatusOptions = useMemo(
    () => [
      { value: null as number | null, label: t('usage.errors.allStatuses') },
      ...COMMON_ERROR_STATUS_CODES.map((c) => ({ value: c as number | null, label: String(c) }))
    ],
    [t]
  )

  const requestTypeOptions = useMemo(
    () => [
      { value: null as string | null, label: t('admin.usage.allTypes') },
      { value: 'ws_v2', label: t('usage.ws') },
      { value: 'live', label: t('usage.live') },
      { value: 'stream', label: t('usage.stream') },
      { value: 'sync', label: t('usage.sync') }
    ],
    [t]
  )
  const compactionOptions = useMemo(
    () => [
      { value: null as boolean | null, label: t('usage.allCompactionTypes') },
      { value: true, label: t('usage.compactionOnly') }
    ],
    [t]
  )
  // 订阅功能关闭后只剩余额计费，「计费类型」筛选（余额/订阅）失去意义，整块隐藏。
  const billingTypeOptions = useMemo(
    () => [
      { value: null as number | null, label: t('admin.usage.allBillingTypes') },
      { value: 0, label: t('admin.usage.billingTypeBalance') },
      { value: 1, label: t('admin.usage.billingTypeSubscription') }
    ],
    [t]
  )
  const billingModeOptions = useMemo(
    () => [
      { value: null as string | null, label: t('admin.usage.allBillingModes') },
      { value: 'token', label: t('admin.usage.billingModeToken') },
      { value: 'per_request', label: t('admin.usage.billingModePerRequest') },
      { value: 'image', label: t('admin.usage.billingModeImage') },
      { value: 'video', label: t('admin.usage.billingModeVideo') }
    ],
    [t]
  )

  const apiKeyOptions = useMemo(
    () => [
      { value: null as number | null, label: t('usage.allApiKeys') },
      ...apiKeys.map((key) => ({ value: key.id as number | null, label: key.name }))
    ],
    [apiKeys, t]
  )
  const groupOptions = useMemo(
    () => [
      { value: null as number | null, label: t('admin.usage.allGroups') },
      ...groups.map((group) => ({ value: group.id as number | null, label: group.name }))
    ],
    [groups, t]
  )
  const modelOptions = useMemo(
    () => [
      { value: null as string | null, label: t('admin.usage.allModels') },
      ...modelOptionValues.map((model) => ({ value: model, label: model }))
    ],
    [modelOptionValues, t]
  )

  const normalizedFilters = useMemo<UsageQueryParams>(() => {
    const requestType = filters.request_type
    const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.stream
    return {
      ...filters,
      start_date: startDate,
      end_date: endDate,
      stream: legacyStream === null ? undefined : legacyStream
    }
  }, [filters, startDate, endDate])

  const buildUsageListParams = (page: number, pageSize: number): UsageQueryParams => ({
    page,
    page_size: pageSize,
    ...normalizedFilters,
    snapshot_max_id: historySnapshot.forFilters(normalizedFilters),
    sort_by: sortState.sort_by,
    sort_order: sortState.sort_order
  })

  const loadLogs = useCallback(async () => {
    abortController.current?.abort()
    const controller = new AbortController()
    abortController.current = controller
    setLoading(true)
    try {
      const res = await usageAPI.query(buildUsageListParams(pagination.page, pagination.page_size), {
        signal: controller.signal
      })
      if (!controller.signal.aborted) {
        setUsageLogs(res.items)
        refreshModelOptions(res.items)
        historySnapshot.accept(
          res,
          pagination.page === 1 && sortState.sort_by === 'created_at' && sortState.sort_order === 'desc'
        )
        setPagination((previous) => ({ ...previous, total: res.total }))
      }
    } catch (error: unknown) {
      const err = error as { name?: string; code?: string }
      if (err?.name !== 'AbortError' && err?.code !== 'ERR_CANCELED') {
        toast.error(t('usage.failedToLoad'))
      }
    } finally {
      if (abortController.current === controller) setLoading(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pagination.page, pagination.page_size, sortState, normalizedFilters, t])

  // Use loaded records for suggestions; custom model names remain searchable.
  const refreshModelOptions = (models: Pick<UsageLog, 'model'>[]) => {
    setModelOptionValues((previous) => {
      const current = filters.model
      const set = new Set(previous)
      models.forEach((item) => {
        if (item.model) set.add(item.model)
      })
      if (current) set.add(current)
      return Array.from(set).sort()
    })
  }

  const applyFilters = () => {
    setPagination((previous) => ({ ...previous, page: 1 }))
    resetErrorRows()
  }

  const resetErrorRows = () => {
    setErrorPage(1)
    if (activeTab === 'errors') {
      void loadErrors()
    } else {
      setErrorRows([])
      setErrorTotal(0)
    }
  }

  const loadErrors = useCallback(async () => {
    setErrorLoading(true)
    try {
      const resp = await usageAPI.listMyErrorRequests({
        page: errorPage,
        page_size: errorPageSize,
        start_date: startDate,
        end_date: endDate,
        model: (errorFilter.model ?? '').trim() || undefined,
        category: errorFilter.category || undefined,
        api_key_id: errorFilter.api_key_id ?? undefined,
        status_code: errorFilter.status_code ?? undefined,
        sort_by: errorSortBy,
        sort_order: errorSortOrder
      })
      setErrorRows(resp.items)
      setErrorTotal(resp.total)
    } catch (error) {
      console.error('[UsageView] loadErrors failed:', error)
      toast.error(t('usage.errors.failedToLoad'))
    } finally {
      setErrorLoading(false)
    }
  }, [errorPage, errorPageSize, startDate, endDate, errorFilter, errorSortBy, errorSortOrder, t])

  const refreshData = async () => {
    historySnapshot.reset()
    await Promise.all([loadLogs(), ...(activeTab === 'errors' ? [loadErrors()] : [])])
  }

  const resetFilters = () => {
    const range = getLast24HoursRangeDates()
    setStartDate(range.start)
    setEndDate(range.end)
    setFilters({
      start_date: range.start,
      end_date: range.end,
      request_type: undefined,
      native_compaction_v2: null,
      billing_type: null,
      billing_mode: null
    })
    applyFilters()
    if (activeTab === 'errors') {
      setErrorFilter({ model: '', category: '', api_key_id: null, status_code: null })
      void loadErrors()
    }
  }

  const onDateRangeChange = (range: { startDate: string; endDate: string; preset: string | null }) => {
    setStartDate(range.startDate)
    setEndDate(range.endDate)
    setFilters((previous) => ({ ...previous, start_date: range.startDate, end_date: range.endDate }))
    applyFilters()
  }

  const handlePageChange = (page: number) => {
    setPagination((previous) => ({ ...previous, page }))
  }

  const handlePageSizeChange = (pageSize: number) => {
    setPagination((previous) => ({ ...previous, page: 1, page_size: pageSize }))
  }

  const handleSort = (key: string, order: 'asc' | 'desc') => {
    setSortState({ sort_by: key, sort_order: order })
    setPagination((previous) => ({ ...previous, page: 1 }))
  }

  const handleIpGeoBatchFailed = () => {
    toast.error(t('usage.ipGeo.batchFailed'))
  }

  const getRequestTypeExportText = (log: UsageLog): string => {
    const requestType = resolveUsageRequestType(log)
    if (requestType === 'cyber') return 'Cyber'
    if (requestType === 'live') return 'Live'
    if (requestType === 'ws_v2') return 'WS'
    if (requestType === 'stream') return 'Stream'
    if (requestType === 'sync') return 'Sync'
    return 'Unknown'
  }

  const escapeCSVValue = (value: unknown): string => {
    if (value == null) return ''
    const str = String(value)
    const escaped = str.replace(/"/g, '""')
    if (/^[=+\-@\t\r]/.test(str)) return `"'${escaped}"`
    if (/[,"\n\r]/.test(str)) return `"${escaped}"`
    return str
  }

  const formatTpsNote = (log: UsageLog): string => {
    const reason = tpsNote(log)
    return reason ? t(reason) : ''
  }

  // 导出逐页请求计入按用户的重查询限流；429 时按 Retry-After 等待后重试同一页。
  const queryExportPage = async (params: UsageQueryParams) => {
    for (let attempt = 0; ; attempt++) {
      try {
        return await usageAPI.query(params)
      } catch (error: unknown) {
        const err = error as { status?: number; retry_after_seconds?: number }
        if (err?.status !== 429 || attempt >= EXPORT_RATE_LIMIT_MAX_RETRIES) throw error
        const waitSeconds = Math.min(
          Math.max(err.retry_after_seconds ?? EXPORT_RATE_LIMIT_MAX_WAIT_SECONDS, 1),
          EXPORT_RATE_LIMIT_MAX_WAIT_SECONDS
        )
        toast.info(t('usage.exportRateLimited', { seconds: waitSeconds }))
        await new Promise((resolve) => setTimeout(resolve, waitSeconds * 1000))
      }
    }
  }

  const exportToCSV = async () => {
    if (pagination.total === 0) {
      toast.warning(t('usage.noDataToExport'))
      return
    }
    setExporting(true)
    toast.info(t('usage.preparingExport'))
    try {
      const allLogs: UsageLog[] = []
      const exportParams = buildUsageListParams(1, EXPORT_PAGE_SIZE)
      const totalPages = Math.ceil(pagination.total / EXPORT_PAGE_SIZE)
      for (let page = 1; page <= totalPages; page++) {
        const response = await queryExportPage({ ...exportParams, page })
        if (page === 1 && response.snapshot_max_id != null) exportParams.snapshot_max_id = response.snapshot_max_id
        allLogs.push(...response.items)
      }
      if (allLogs.length === 0) {
        toast.warning(t('usage.noDataToExport'))
        return
      }
      const headers = [
        'Time',
        'API Key Name',
        'Model',
        'Reasoning Effort',
        'Inbound Endpoint',
        'IP Address',
        'Type',
        'Billing Mode',
        'Input Tokens',
        'Output Tokens',
        'Cache Read Tokens',
        'Cache Creation Tokens',
        'Rate Multiplier',
        'Billed Cost',
        'Original Cost',
        'First Byte (ms)',
        'First Token (ms)',
        'First Output (ms)',
        'First Output Kind',
        'Duration (ms)',
        'Average TPS',
        'Average TPS note'
      ]
      const rows = allLogs.map((log) =>
        [
          log.created_at,
          log.api_key?.name || '',
          log.model,
          formatReasoningEffort(log.reasoning_effort),
          log.inbound_endpoint || '',
          log.ip_address || '',
          getRequestTypeExportText(log),
          getBillingModeLabel(getDisplayBillingMode(log), t),
          log.input_tokens,
          log.output_tokens,
          log.cache_read_tokens,
          log.cache_creation_tokens,
          log.rate_multiplier,
          log.actual_cost.toFixed(8),
          log.total_cost.toFixed(8),
          firstByteMs(log) ?? '',
          strictFirstTokenMs(log) ?? '',
          log.first_output_ms ?? '',
          log.first_output_kind ?? '',
          log.duration_ms ?? '',
          averageTps(log) ?? '',
          formatTpsNote(log)
        ].map(escapeCSVValue)
      )
      const csvContent = [headers.map(escapeCSVValue).join(','), ...rows.map((row) => row.join(','))].join('\n')
      const blob = new Blob(['\uFEFF' + csvContent], { type: 'text/csv;charset=utf-8;' })
      const url = window.URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `usage_${exportParams.start_date}_to_${exportParams.end_date}.csv`
      link.click()
      window.URL.revokeObjectURL(url)
      toast.success(t('usage.exportSuccess'))
    } catch (error) {
      console.error('CSV Export failed:', error)
      toast.error(t('usage.exportFailed'))
    } finally {
      setExporting(false)
    }
  }

  const toggleColumn = (key: string) => {
    setHiddenColumns((previous) => {
      const next = new Set(previous)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      try {
        localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...next]))
      } catch {
        /* storage unavailable */
      }
      return next
    })
  }

  const loadSavedColumns = () => {
    let values: string[] = DEFAULT_HIDDEN_COLUMNS
    try {
      const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
      const parsed = saved ? (JSON.parse(saved) as string[]) : DEFAULT_HIDDEN_COLUMNS
      if (!Array.isArray(parsed)) throw new Error('Invalid column preferences')
      values = parsed
    } catch {
      values = DEFAULT_HIDDEN_COLUMNS
    }
    const valid = values.filter((key) => allColumns.some((col) => col.key === key))
    setHiddenColumns(new Set(valid))
  }

  const toggleErrColumn = (key: string) => {
    setErrHiddenColumns((previous) => {
      const next = new Set(previous)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      try {
        localStorage.setItem(ERR_HIDDEN_COLUMNS_KEY, JSON.stringify([...next]))
      } catch {
        /* storage unavailable */
      }
      return next
    })
  }

  const loadSavedErrColumns = () => {
    let values: string[] = ERR_DEFAULT_HIDDEN_COLUMNS
    try {
      const saved = localStorage.getItem(ERR_HIDDEN_COLUMNS_KEY)
      const parsed = saved ? (JSON.parse(saved) as string[]) : ERR_DEFAULT_HIDDEN_COLUMNS
      if (Array.isArray(parsed)) values = parsed
    } catch {
      values = ERR_DEFAULT_HIDDEN_COLUMNS
    }
    setErrHiddenColumns(new Set(values))
  }

  const restoreRecommendedColumns = () => {
    const errors = activeTab === 'errors'
    if (errors) {
      setErrHiddenColumns(new Set(ERR_DEFAULT_HIDDEN_COLUMNS))
      try {
        localStorage.setItem(ERR_HIDDEN_COLUMNS_KEY, JSON.stringify(ERR_DEFAULT_HIDDEN_COLUMNS))
      } catch {
        /* storage unavailable */
      }
    } else {
      setHiddenColumns(new Set(DEFAULT_HIDDEN_COLUMNS))
      try {
        localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify(DEFAULT_HIDDEN_COLUMNS))
      } catch {
        /* storage unavailable */
      }
    }
  }

  const currentToggleableColumns = activeTab === 'errors' ? errToggleableColumns : toggleableColumns
  const isCurrentColumnVisible = (key: string) =>
    activeTab === 'errors' ? !errHiddenColumns.has(key) : !hiddenColumns.has(key)
  const toggleCurrentColumn = (key: string) => {
    if (activeTab === 'errors') toggleErrColumn(key)
    else toggleColumn(key)
  }

  const loadApiKeys = async () => {
    const firstPage = await keysAPI.list(1, 100)
    const keys = [...firstPage.items]
    for (let page = 2; page <= firstPage.pages && keys.length > 0; page++) {
      const response = await keysAPI.list(page, 100)
      if (response.items.length === 0) break
      keys.push(...response.items)
    }
    return keys
  }

  const loadFilterOptions = async () => {
    const request = ++filterRequest.current
    try {
      const [keys, availableGroups] = await Promise.all([loadApiKeys(), userGroupsAPI.getAvailable()])
      if (request !== filterRequest.current) return
      setApiKeys(keys)
      setGroups(availableGroups)
    } catch (error) {
      console.error('Failed to load usage filter options:', error)
    }
  }

  useEffect(() => {
    if (searchParams.has('record')) {
      const next = new URLSearchParams(searchParams)
      next.delete('record')
      setSearchParams(next, { replace: true })
    }
    loadSavedColumns()
    loadSavedErrColumns()
    void loadFilterOptions()
    void refreshData()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    return () => {
      abortController.current?.abort()
    }
  }, [])

  // Plain subscription helper (returns a disposer) despite its `use*` name.
  const loadLogsRef = useRef(loadLogs)
  loadLogsRef.current = loadLogs
  const loadFilterOptionsRef = useRef(loadFilterOptions)
  loadFilterOptionsRef.current = loadFilterOptions
  useEffect(
    () =>
      subscribeLocaleRefresh(() => {
        void Promise.all([loadLogsRef.current(), loadFilterOptionsRef.current()])
      }),
    []
  )

  // Anchor the column dropdown under its trigger.
  useEffect(() => {
    if (!showColumnDropdown) return
    const place = () => {
      const trigger = columnDropdownRef.current
      if (!trigger) return
      const rect = trigger.getBoundingClientRect()
      setColumnDropdownStyle({ top: `${rect.bottom + 4}px`, right: `${window.innerWidth - rect.right}px` })
    }
    place()
    window.addEventListener('resize', place)
    window.addEventListener('scroll', place, true)
    return () => {
      window.removeEventListener('resize', place)
      window.removeEventListener('scroll', place, true)
    }
  }, [showColumnDropdown])

  const selectRecordTab = (tab: 'usage' | 'live') => setActiveTab(tab)

  return (
    <div className="workspace-page usage-workspace">
      {activeTab !== 'live' && (
        <div className="card workspace-fixed usage-date p-4">
          <div className="filter-bar">
            <span className="text-sm font-medium surface-muted">{t('admin.dashboard.timeRange')}:</span>
            <div className="filter-field">
              <DateRangePicker
                startDate={startDate}
                endDate={endDate}
                onStartDateChange={setStartDate}
                onEndDateChange={setEndDate}
                onChange={onDateRangeChange}
              />
            </div>
          </div>
        </div>
      )}

      <section className="workspace-body usage-records-workspace" data-testid="usage-records-panel">
        <div className="card workspace-fixed overflow-hidden">
          <div className="usage-detail-tabs card-tabs workspace-fixed" role="tablist" aria-label={t('usage.tabs.usage')}>
            <button
              type="button"
              role="tab"
              className="card-tab"
              aria-selected={activeTab === 'live'}
              onClick={() => selectRecordTab('live')}
            >
              {t('inspection.live')}
            </button>
            <button
              type="button"
              role="tab"
              className="card-tab"
              aria-selected={activeTab === 'usage'}
              onClick={() => selectRecordTab('usage')}
            >
              {t('usage.tabs.usage')}
            </button>
            {errorViewEnabled && (
              <button type="button" role="tab" className="card-tab" aria-selected={activeTab === 'errors'} onClick={() => setActiveTab('errors')}>
                {t('usage.tabs.errors')}
              </button>
            )}
          </div>

          {activeTab !== 'live' && (
            <div className="usage-workspace-filters workspace-fixed border-b border-gray-100 p-4 dark:border-dark-700/50">
              <div className="usage-filter-layout flex flex-wrap items-end justify-between gap-4">
                {activeTab === 'errors' ? (
                  <div className="usage-filter-fields filter-bar flex-1">
                    <div className="filter-field">
                      <label className="input-label">{t('usage.errors.keyName')}</label>
                      <Select
                        value={errorFilter.api_key_id}
                        options={errorKeyOptions}
                        searchable={false}
                        onValueChange={(value) => {
                          setErrorFilter((previous) => ({ ...previous, api_key_id: value === null ? null : Number(value) }))
                          setErrorPage(1)
                          void loadErrors()
                        }}
                      />
                    </div>
                    <div className="filter-field">
                      <label className="input-label">{t('usage.errors.model')}</label>
                      <Select
                        value={errorFilter.model}
                        options={errorModelOptions}
                        searchable
                        creatable
                        clearable
                        placeholder={t('usage.errors.modelPlaceholder')}
                        onValueChange={(value) => {
                          setErrorFilter((previous) => ({ ...previous, model: value === null ? '' : String(value) }))
                          setErrorPage(1)
                          void loadErrors()
                        }}
                      />
                    </div>
                    <div className="filter-field">
                      <label className="input-label">{t('usage.errors.category')}</label>
                      <Select
                        value={errorFilter.category}
                        options={errorCategoryOptions}
                        searchable={false}
                        onValueChange={(value) => {
                          setErrorFilter((previous) => ({ ...previous, category: value === null ? '' : String(value) }))
                          setErrorPage(1)
                          void loadErrors()
                        }}
                      />
                    </div>
                    <div className="filter-field">
                      <label className="input-label">{t('usage.errors.status')}</label>
                      <Select
                        value={errorFilter.status_code}
                        options={errorStatusOptions}
                        searchable={false}
                        onValueChange={(value) => {
                          setErrorFilter((previous) => ({
                            ...previous,
                            status_code: value === null ? null : Number(value)
                          }))
                          setErrorPage(1)
                          void loadErrors()
                        }}
                      />
                    </div>
                  </div>
                ) : (
                  <div className="usage-filter-fields filter-bar flex-1">
                    <div className="filter-field">
                      <label className="input-label">{t('usage.apiKeyFilter')}</label>
                      <Select
                        value={filters.api_key_id ?? null}
                        options={apiKeyOptions}
                        searchable={false}
                        onValueChange={(value) => {
                          setFilters((previous) => ({
                            ...previous,
                            api_key_id: value === null ? undefined : Number(value)
                          }))
                          applyFilters()
                        }}
                      />
                    </div>
                    <div className="filter-field">
                      <label className="input-label">{t('usage.model')}</label>
                      <Select
                        value={filters.model ?? ''}
                        options={modelOptions}
                        searchable
                        creatable
                        clearable
                        onValueChange={(value) => {
                          setFilters((previous) => ({ ...previous, model: value === null ? '' : String(value) }))
                          applyFilters()
                        }}
                      />
                    </div>
                    <div className="filter-field">
                      <label className="input-label">{t('admin.usage.group')}</label>
                      <Select
                        value={filters.group_id ?? null}
                        options={groupOptions}
                        searchable
                        onValueChange={(value) => {
                          setFilters((previous) => ({ ...previous, group_id: value === null ? undefined : Number(value) }))
                          applyFilters()
                        }}
                      />
                    </div>
                    <div className="filter-field">
                      <label className="input-label">{t('usage.type')}</label>
                      <Select
                        value={filters.request_type ?? null}
                        options={requestTypeOptions}
                        searchable={false}
                        onValueChange={(value) => {
                          setFilters((previous) => ({
                            ...previous,
                            request_type: value === null ? undefined : (value as UsageQueryParams['request_type'])
                          }))
                          applyFilters()
                        }}
                      />
                    </div>
                    <div className="filter-field">
                      <label className="input-label">{t('usage.compactionFilter')}</label>
                      <Select
                        value={filters.native_compaction_v2 ?? null}
                        options={compactionOptions}
                        searchable={false}
                        onValueChange={(value) => {
                          setFilters((previous) => ({
                            ...previous,
                            native_compaction_v2: value === null ? null : Boolean(value)
                          }))
                          applyFilters()
                        }}
                      />
                    </div>
                    {subscriptionFeatureEnabled && (
                      <div className="w-full sm:w-auto sm:min-w-[200px]">
                        <label className="input-label">{t('admin.usage.billingType')}</label>
                        <Select
                          value={filters.billing_type ?? null}
                          options={billingTypeOptions}
                          searchable={false}
                          onValueChange={(value) => {
                            setFilters((previous) => ({
                              ...previous,
                              billing_type: value === null ? null : Number(value)
                            }))
                            applyFilters()
                          }}
                        />
                      </div>
                    )}
                    <div className="filter-field">
                      <label className="input-label">{t('admin.usage.billingMode')}</label>
                      <Select
                        value={filters.billing_mode ?? null}
                        options={billingModeOptions}
                        searchable={false}
                        onValueChange={(value) => {
                          setFilters((previous) => ({
                            ...previous,
                            billing_mode: value === null ? null : String(value)
                          }))
                          applyFilters()
                        }}
                      />
                    </div>
                  </div>
                )}

                <div className="usage-filter-actions flex w-full flex-wrap items-center justify-end gap-3 sm:w-auto">
                  <button
                    type="button"
                    onClick={() => void refreshData()}
                    disabled={activeTab === 'errors' ? errorLoading : loading}
                    className="btn btn-secondary"
                  >
                    {t('common.refresh')}
                  </button>
                  <button type="button" onClick={resetFilters} className="btn btn-secondary">
                    {t('common.reset')}
                  </button>
                  <div className="relative" ref={columnDropdownRef}>
                    <button
                      type="button"
                      data-testid="usage-column-settings"
                      onClick={() => setShowColumnDropdown((value) => !value)}
                      className="btn btn-secondary px-2 md:px-3"
                      title={t('admin.users.columnSettings')}
                    >
                      <Icon name="grid" size="sm" />
                      <span className="hidden md:inline">{t('admin.users.columnSettings')}</span>
                    </button>
                    {showColumnDropdown && (
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
                            className="surface-control surface-muted flex w-full items-center justify-between rounded-[var(--surface-radius-item)] px-3 py-2 text-left text-sm hover:surface-hover"
                          >
                            <span>{col.label}</span>
                            {isCurrentColumnVisible(col.key) && <Icon name="check" size="sm" className="text-primary-500" />}
                          </button>
                        ))}
                        <button
                          type="button"
                          className="surface-control w-full rounded-[var(--surface-radius-item)] border-t px-3 py-2 text-left text-sm text-primary-600 hover:surface-hover"
                          onClick={restoreRecommendedColumns}
                        >
                          {t('inspection.resetColumns')}
                        </button>
                      </div>
                    )}
                  </div>
                  {activeTab !== 'errors' && (
                    <button type="button" onClick={() => void exportToCSV()} disabled={exporting} className="btn btn-primary">
                      {exporting ? t('usage.exporting') : t('usage.exportCsv')}
                    </button>
                  )}
                </div>
              </div>
            </div>
          )}
        </div>

        {activeTab === 'usage' ? (
          <div className="workspace-body usage-history-workspace gap-4">
            <UsageTable
              workspace
              data={usageLogs}
              loading={loading}
              columns={visibleColumns}
              serverSideSort
              showAccountBilling={false}
              showUpstreamEndpoint={false}
              modelMetadata
              defaultSortKey="created_at"
              defaultSortOrder="desc"
              onSort={handleSort}
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
        ) : activeTab === 'live' ? (
          <LiveRequestsPanel workspace mode="user" />
        ) : errorViewEnabled ? (
          <UserErrorRequestsTable
            workspace
            rows={errorRows}
            total={errorTotal}
            loading={errorLoading}
            page={errorPage}
            pageSize={errorPageSize}
            visibleColumnKeys={errVisibleColumnKeys}
            onSort={(sortBy, sortOrder) => {
              setErrorSortBy(sortBy)
              setErrorSortOrder(sortOrder)
              setErrorPage(1)
              void loadErrors()
            }}
            onPageChange={(page) => {
              setErrorPage(page)
              void loadErrors()
            }}
            onPageSizeChange={(pageSize) => {
              setErrorPageSize(pageSize)
              setErrorPage(1)
              void loadErrors()
            }}
            onIpGeoBatchFailed={handleIpGeoBatchFailed}
          />
        ) : null}
      </section>
    </div>
  )
}
