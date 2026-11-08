import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  AlertTriangle,
  BarChart3,
  Copy,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Settings,
  Trash2
} from 'lucide-react'
import { adminAPI } from '@/api/admin'
import { extractApiErrorMessage } from '@/utils/apiError'
import type {
  APIMode,
  CheckMode,
  CheckResult,
  ChannelMonitor,
  ListParams,
  MonitorStatus,
  Provider
} from '@/api/admin/channelMonitor'
import {
  getChannelMonitorMode,
  isChannelMonitorActiveProbeEnabled,
  isChannelMonitorV1Mode
} from '@/utils/featureFlags'
import { useAppStore } from '@/stores/app'
import { Button, EmptyState, Input, Modal, Pagination, Select, Toggle } from '@/components/ui'
import { AdminDataTable, type Column } from './AdminDataTable'
import TablePageLayout from '@/components/layout/TablePageLayout'

const NEUTRAL_BADGE = 'surface-inset text-gray-800 dark:surface-inset dark:text-gray-300'

const statusLabel = (t: (key: string) => string, s: MonitorStatus | ''): string =>
  s ? t(`monitorCommon.status.${s}`) : t('monitorCommon.status.unknown')

function statusBadgeClass(s: MonitorStatus | ''): string {
  switch (s) {
    case 'operational':
      return 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-500/[0.15] dark:text-emerald-300'
    case 'degraded':
      return 'bg-amber-100/[0.15] text-amber-700 dark:bg-amber-500/[0.15] dark:text-amber-300'
    case 'failed':
      return 'bg-red-100/[0.15] text-red-700 dark:bg-red-500/[0.15] dark:text-red-300'
    default:
      return NEUTRAL_BADGE
  }
}

function providerBadgeClass(p: Provider | string): string {
  switch (p) {
    case 'openai':
      return 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-500/[0.15] dark:text-emerald-300'
    case 'anthropic':
      return 'bg-orange-100/[0.15] text-orange-700 dark:bg-orange-500/[0.15] dark:text-orange-300'
    case 'gemini':
      return 'bg-sky-100/[0.15] text-sky-700 dark:bg-sky-500/[0.15] dark:text-sky-300'
    case 'grok':
      return 'bg-zinc-100/[0.15] text-zinc-700 dark:bg-zinc-500/[0.15] dark:text-zinc-300'
    case 'antigravity':
      return 'bg-purple-100/[0.15] text-purple-700 dark:bg-purple-500/[0.15] dark:text-purple-300'
    case 'kimi':
      return 'bg-pink-100/[0.15] text-pink-700 dark:bg-pink-500/[0.15] dark:text-pink-300'
    case 'zhipu':
      return 'bg-indigo-100/[0.15] text-indigo-700 dark:bg-indigo-500/[0.15] dark:text-indigo-300'
    case 'deepseek':
      return 'bg-teal-100/[0.15] text-teal-700 dark:bg-teal-500/[0.15] dark:text-teal-300'
    case 'minimax':
      return 'bg-rose-100/[0.15] text-rose-700 dark:bg-rose-500/[0.15] dark:text-rose-300'
    case 'opencode_go':
      return 'bg-amber-100/[0.15] text-amber-800 dark:bg-amber-500/[0.15] dark:text-amber-300'
    default:
      return NEUTRAL_BADGE
  }
}

function checkModeBadgeClass(m: CheckMode | string): string {
  switch (m) {
    case 'quota':
    case 'quota_probe':
      return 'bg-blue-100/[0.15] text-blue-700 dark:bg-blue-500/[0.15] dark:text-blue-300'
    default:
      return NEUTRAL_BADGE
  }
}

const QUOTA_MODEL_PLACEHOLDER = 'quota'

const formatMonitorModel = (t: (key: string) => string, model: string): string =>
  model === QUOTA_MODEL_PLACEHOLDER ? t('monitorCommon.checkMode.quota') : model

const formatLatency = (t: (key: string) => string, ms: number | null | undefined): string =>
  ms == null ? t('monitorCommon.latencyEmpty') : String(Math.round(ms))

const formatAvailability = (row: ChannelMonitor): string => {
  if (!row.primary_status) return '-'
  return `${row.availability_7d.toFixed(2)}%`
}

const PROVIDER_LIST: Provider[] = [
  'openai',
  'anthropic',
  'gemini',
  'grok',
  'antigravity',
  'kimi',
  'zhipu',
  'deepseek',
  'minimax',
  'opencode_go'
]

interface MonitorFormState {
  name: string
  provider: Provider
  api_mode: APIMode
  endpoint: string
  api_key: string
  check_mode: CheckMode
  account_id: number | null
  primary_model: string
  extra_models: string[]
  group_name: string
  enabled: boolean
  interval_seconds: number
  jitter_seconds: number
  stream_probe: boolean
}

const EMPTY_FORM: MonitorFormState = {
  name: '',
  provider: 'openai',
  api_mode: 'chat_completions',
  endpoint: '',
  api_key: '',
  check_mode: 'probe',
  account_id: null,
  primary_model: '',
  extra_models: [],
  group_name: '',
  enabled: true,
  interval_seconds: 60,
  jitter_seconds: 0,
  stream_probe: false
}

function MonitorPrimaryModelCell({
  row,
  t
}: {
  row: ChannelMonitor
  t: (key: string) => string
}) {
  return (
    <div className="flex flex-col gap-0.5">
      <div className="flex items-center gap-2">
        <span className="text-sm text-gray-900 dark:text-gray-100">{formatMonitorModel(t, row.primary_model)}</span>
        <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-medium ${statusBadgeClass(row.primary_status)}`}>
          {statusLabel(t, row.primary_status)}
        </span>
      </div>
      {(row.extra_models?.length ?? 0) > 0 && (
        <div className="record-table-scroll space-y-1">
          <div className="text-[11px] font-semibold uppercase tracking-wide surface-muted">
            {t('monitorCommon.extraModelsHeader')}
          </div>
          <table className="record-table record-table-compact record-table-dense w-full text-left text-[11px]">
            <thead>
              <tr className="surface-muted">
                <th className="py-0.5 pr-2 font-medium">{t('admin.channelMonitor.columns.primaryModel')}</th>
                <th className="py-0.5 pr-2 font-medium">{t('admin.channelMonitor.columns.actions')}</th>
                <th className="py-0.5 font-medium">
                  {t('monitorCommon.firstText')} / {t('monitorCommon.firstByte')} / {t('usage.latencyDuration')}
                </th>
              </tr>
            </thead>
            <tbody>
              {(row.extra_models_status || []).map((m) => (
                <tr key={m.model}>
                  <td className="py-0.5 pr-2 text-[var(--surface-text)]">{m.model}</td>
                  <td className="py-0.5 pr-2">
                    <span className={`inline-flex items-center rounded-full px-1.5 py-0.5 text-[10px] ${statusBadgeClass(m.status)}`}>
                      {statusLabel(t, m.status)}
                    </span>
                  </td>
                  <td className="py-0.5 text-[var(--surface-text)]">
                    {formatLatency(t, m.first_text_ms)} / {formatLatency(t, m.first_byte_ms)} / {formatLatency(t, m.latency_ms)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

export default function ChannelMonitorView() {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)

  const isV1Mode = isChannelMonitorV1Mode()
  const activeProbesEnabled = isChannelMonitorActiveProbeEnabled()
  const initialMode = getChannelMonitorMode()
  const [adminMonitorTab, setAdminMonitorTab] = useState<string>(initialMode === 'v1' ? 'legacy' : initialMode)

  const monitorTabs = useMemo(
    () => [
      { value: 'v3', label: t('channelMonitorV3.modeV3') },
      { value: 'v2', label: t('channelMonitorV2.admin.tabV2') },
      {
        value: 'legacy',
        label: activeProbesEnabled ? t('channelMonitorV2.admin.tabV1Active') : t('channelMonitorV2.admin.tabV1History')
      }
    ],
    [activeProbesEnabled, t]
  )

  const [monitors, setMonitors] = useState<ChannelMonitor[]>([])
  const [loading, setLoading] = useState(false)
  const [runningId, setRunningId] = useState<number | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [providerFilter, setProviderFilter] = useState<Provider | ''>('')
  const [enabledFilter, setEnabledFilter] = useState<'' | 'true' | 'false'>('')
  const [pagination, setPagination] = useState({ page: 1, page_size: 20, total: 0 })

  const [showDialog, setShowDialog] = useState(false)
  const [editing, setEditing] = useState<ChannelMonitor | null>(null)
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [deleting, setDeleting] = useState<ChannelMonitor | null>(null)
  const [showRunResult, setShowRunResult] = useState(false)
  const [runResults, setRunResults] = useState<CheckResult[]>([])
  const [duplicatingIds, setDuplicatingIds] = useState<Set<number>>(new Set())

  const abortController = useRef<AbortController | null>(null)
  const searchTimeout = useRef<ReturnType<typeof setTimeout> | null>(null)

  const columns: Column[] = useMemo(
    () => [
      { key: 'name', label: t('admin.channelMonitor.columns.name'), sortable: false },
      { key: 'provider', label: t('admin.channelMonitor.columns.provider'), sortable: false },
      { key: 'primary_model', label: t('admin.channelMonitor.columns.primaryModel'), sortable: false },
      { key: 'availability_7d', label: t('admin.channelMonitor.columns.availability7d'), sortable: false },
      { key: 'latency', label: t('admin.channelMonitor.columns.latency'), sortable: false },
      { key: 'enabled', label: t('admin.channelMonitor.columns.enabled'), sortable: false },
      { key: 'actions', label: t('admin.channelMonitor.columns.actions'), sortable: false }
    ],
    [t]
  )

  const providerFilterOptions = useMemo(
    () => [
      { value: '', label: t('admin.channelMonitor.allProviders') },
      ...PROVIDER_LIST.map((value) => ({ value, label: t(`monitorCommon.providers.${value}`) }))
    ],
    [t]
  )

  const enabledFilterOptions = useMemo(
    () => [
      { value: '', label: t('admin.channelMonitor.allStatus') },
      { value: 'true', label: t('admin.channelMonitor.onlyEnabled') },
      { value: 'false', label: t('admin.channelMonitor.onlyDisabled') }
    ],
    [t]
  )

  const queryRef = useRef({ pagination, providerFilter, enabledFilter, searchQuery })
  queryRef.current = { pagination, providerFilter, enabledFilter, searchQuery }

  const reload = useCallback(async () => {
    if (abortController.current) abortController.current.abort()
    const ctrl = new AbortController()
    abortController.current = ctrl
    const { pagination: page, providerFilter: provider, enabledFilter: enabled, searchQuery: search } = queryRef.current
    setLoading(true)
    try {
      const params: ListParams = {
        page: page.page,
        page_size: page.page_size
      }
      if (provider) params.provider = provider
      if (enabled === 'true') params.enabled = true
      if (enabled === 'false') params.enabled = false
      if (search.trim()) params.search = search.trim()

      const res = await adminAPI.channelMonitor.list(params, { signal: ctrl.signal })
      if (ctrl.signal.aborted || abortController.current !== ctrl) return
      setMonitors(res.items || [])
      setPagination((prev) => ({ ...prev, total: res.total }))
    } catch (err) {
      const e = err as { name?: string; code?: string }
      if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
      showError(extractApiErrorMessage(err, t('admin.channelMonitor.loadError')))
    } finally {
      if (abortController.current === ctrl) {
        setLoading(false)
        abortController.current = null
      }
    }
  }, [showError, t])

  const handleSearch = () => {
    if (searchTimeout.current) clearTimeout(searchTimeout.current)
    searchTimeout.current = setTimeout(() => {
      setPagination((prev) => ({ ...prev, page: 1 }))
      setReloadKey((k) => k + 1)
    }, 300)
  }

  const onPageChange = (page: number) => {
    setPagination((prev) => ({ ...prev, page }))
    setReloadKey((k) => k + 1)
  }

  const onPageSizeChange = (pageSize: number) => {
    setPagination((prev) => ({ ...prev, page_size: pageSize, page: 1 }))
    setReloadKey((k) => k + 1)
  }

  const openCreateDialog = () => {
    setEditing(null)
    setShowDialog(true)
  }

  const openEditDialog = (row: ChannelMonitor) => {
    setEditing(row)
    setShowDialog(true)
  }

  const closeDialog = () => {
    setShowDialog(false)
    setEditing(null)
  }

  const toggleEnabled = async (row: ChannelMonitor) => {
    const next = !row.enabled
    try {
      await adminAPI.channelMonitor.update(row.id, { enabled: next })
      setMonitors((prev) => prev.map((entry) => (entry.id === row.id ? { ...entry, enabled: next } : entry)))
    } catch (err) {
      showError(extractApiErrorMessage(err, t('common.error')))
    }
  }

  const handleRunNow = async (row: ChannelMonitor) => {
    if (!activeProbesEnabled) {
      showError(t('admin.channelMonitor.probesDisabledHint'))
      return
    }
    if (runningId != null) return
    setRunningId(row.id)
    try {
      const res = await adminAPI.channelMonitor.runNow(row.id)
      setRunResults(res.results || [])
      setShowRunResult(true)
      showSuccess(t('admin.channelMonitor.runSuccess'))
      // Refresh row to get latest status from backend
      setReloadKey((k) => k + 1)
    } catch (err) {
      showError(extractApiErrorMessage(err, t('admin.channelMonitor.runFailed')))
    } finally {
      setRunningId(null)
    }
  }

  const handleDuplicate = async (row: ChannelMonitor) => {
    if (row.api_key_decrypt_failed) {
      showError(t('admin.channelMonitor.duplicateKeyUnavailable'))
      return
    }
    if (duplicatingIds.has(row.id)) return

    setDuplicatingIds((prev) => new Set(prev).add(row.id))
    try {
      const duplicate = await adminAPI.channelMonitor.duplicate(row.id)
      showSuccess(t('admin.channelMonitor.duplicateSuccess', { name: duplicate.name }))
      setReloadKey((k) => k + 1)
    } catch (err) {
      showError(extractApiErrorMessage(err, t('admin.channelMonitor.duplicateFailed')))
    } finally {
      setDuplicatingIds((prev) => {
        const next = new Set(prev)
        next.delete(row.id)
        return next
      })
    }
  }

  const handleDelete = (row: ChannelMonitor) => {
    setDeleting(row)
    setShowDeleteDialog(true)
  }

  const confirmDelete = async () => {
    if (!deleting) return
    try {
      await adminAPI.channelMonitor.del(deleting.id)
      showSuccess(t('admin.channelMonitor.deleteSuccess'))
      setShowDeleteDialog(false)
      setDeleting(null)
      setReloadKey((k) => k + 1)
    } catch (err) {
      showError(extractApiErrorMessage(err, t('common.error')))
    }
  }

  const deleteConfirmMessage = deleting?.name
    ? t('admin.channelMonitor.deleteConfirm', { name: deleting.name })
    : ''

  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    void reload()
  }, [reload, reloadKey])

  // The legacy tab owns the only in-page list; v2/v3 render their own panels.
  useEffect(() => {
    if (adminMonitorTab === 'legacy' && monitors.length === 0) setReloadKey((k) => k + 1)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [adminMonitorTab])

  useEffect(() => {
    return () => {
      if (searchTimeout.current) clearTimeout(searchTimeout.current)
      abortController.current?.abort()
    }
  }, [])

  return (
    <div className="workspace-page">
      <header className="workspace-fixed glass-surface page-header mb-0 p-5 ring-1 ring-gray-900/5 dark:ring-dark-700 sm:p-6">
        <h1 className="page-title flex items-center gap-2 text-xl font-black text-gray-900 dark:text-white">
          <span className="inline-flex h-8 w-8 items-center justify-center rounded-xl bg-blue-50/[0.15] text-blue-500 dark:bg-blue-900/30 dark:text-blue-400">
            <BarChart3 className="h-4 w-4" />
          </span>
          {t('admin.channelMonitor.title')}
        </h1>
        <p className="page-description mt-1.5 text-xs surface-muted">
          {getChannelMonitorMode() === 'v3'
            ? t('channelMonitorV3.modeV3Hint')
            : isV1Mode
              ? t('channelMonitorV2.admin.descriptionV1')
              : t('channelMonitorV2.admin.descriptionV2')}
        </p>
        {/* Tabs live inside the header card so the page keeps one surface. */}
        <div className="mt-4 w-full">
          <div className="card-tabs flex w-full flex-wrap" role="tablist" aria-label={t('channelMonitorV2.admin.tabAria')}>
            {monitorTabs.map((tab) => (
              <button
                key={tab.value}
                type="button"
                role="tab"
                aria-selected={adminMonitorTab === tab.value}
                className="card-tab"
                onClick={() => setAdminMonitorTab(tab.value)}
              >
                {tab.label}
              </button>
            ))}
          </div>
        </div>
      </header>

      <div
        className={adminMonitorTab === 'legacy' ? 'workspace-shell workspace-body' : 'workspace-shell workspace-scroll'}
        data-test="monitor-settings-scroll"
      >
        {adminMonitorTab === 'v3' || adminMonitorTab === 'v2' ? (
          <div className="card p-8">
            <EmptyState
              icon={<Settings className="h-10 w-10" />}
              title={t('channelMonitorV2.admin.tabV2')}
              description={t('common.portInProgress')}
            />
          </div>
        ) : (
          <TablePageLayout
            filters={
              <div className="flex flex-col justify-between gap-4 lg:flex-row lg:items-start">
                <div className="filter-bar flex-1">
                  {!activeProbesEnabled && (
                    <p
                      className="monitor-legacy-notice mb-3 rounded-lg bg-amber-50/[0.15] p-3 text-sm text-amber-800 dark:bg-amber-950/30 dark:text-amber-200"
                      role="status"
                    >
                      {t('admin.channelMonitor.probesDisabledHint')}
                    </p>
                  )}
                  <div className="filter-field filter-field--search relative">
                    <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 surface-muted" />
                    <Input
                      value={searchQuery}
                      onValueChange={(value) => {
                        setSearchQuery(value)
                        handleSearch()
                      }}
                      type="text"
                      placeholder={t('admin.channelMonitor.searchPlaceholder')}
                      className="input pl-10"
                    />
                  </div>
                  <div className="filter-field">
                    <Select
                      value={providerFilter}
                      options={providerFilterOptions}
                      placeholder={t('admin.channelMonitor.allProviders')}
                      onValueChange={(value) => {
                        setProviderFilter(String(value ?? '') as Provider | '')
                        setPagination((prev) => ({ ...prev, page: 1 }))
                        setReloadKey((k) => k + 1)
                      }}
                    />
                  </div>
                  <div className="filter-field">
                    <Select
                      value={enabledFilter}
                      options={enabledFilterOptions}
                      placeholder={t('admin.channelMonitor.enabledFilter')}
                      onValueChange={(value) => {
                        setEnabledFilter(String(value ?? '') as '' | 'true' | 'false')
                        setPagination((prev) => ({ ...prev, page: 1 }))
                        setReloadKey((k) => k + 1)
                      }}
                    />
                  </div>
                </div>

                <div className="monitor-filter-actions flex w-full flex-shrink-0 flex-wrap items-center justify-end gap-3 lg:w-auto">
                  <Button
                    variant="secondary"
                    size="icon"
                    disabled={loading}
                    onClick={() => setReloadKey((k) => k + 1)}
                    title={t('common.refresh')}
                  >
                    <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
                  </Button>
                  <Button variant="secondary" title={t('admin.channelMonitor.template.manageButton')}>
                    <Settings className="mr-2 h-4 w-4" />
                    {t('admin.channelMonitor.template.manageButton')}
                  </Button>
                  <Button variant="primary" onClick={openCreateDialog}>
                    <Plus className="mr-2 h-4 w-4" />
                    {t('admin.channelMonitor.createButton')}
                  </Button>
                </div>
              </div>
            }
            table={
              <AdminDataTable<ChannelMonitor>
                columns={columns}
                data={monitors}
                loading={loading}
                rowKey="id"
                renderCell={(key, row) => {
                  if (key === 'name') {
                    return (
                      <div className="flex items-center gap-1.5">
                        <span className="font-medium text-gray-900 dark:text-white">{row.name}</span>
                        {row.api_key_decrypt_failed && (
                          <span title={t('admin.channelMonitor.apiKeyDecryptFailed')}>
                            <AlertTriangle className="h-4 w-4 text-red-500" />
                          </span>
                        )}
                      </div>
                    )
                  }
                  if (key === 'provider') {
                    return (
                      <>
                        <span
                          className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium ${providerBadgeClass(row.provider)}`}
                        >
                          {t(`monitorCommon.providers.${row.provider}`)}
                        </span>
                        {/* 三种检测模式并列展示，quota 系配额数据源与纯探活一眼可分 */}
                        <span
                          className={`ml-1 inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium ${checkModeBadgeClass(row.check_mode)}`}
                        >
                          {t(`monitorCommon.checkMode.${row.check_mode}`)}
                        </span>
                      </>
                    )
                  }
                  if (key === 'primary_model') {
                    return <MonitorPrimaryModelCell row={row} t={t} />
                  }
                  if (key === 'availability_7d') {
                    return <span className="text-sm text-gray-900 dark:text-gray-100">{formatAvailability(row)}</span>
                  }
                  if (key === 'latency') {
                    return (
                      <div className="text-xs text-gray-900 dark:text-gray-100" title={t('monitorCommon.timingHint')}>
                        <div>
                          {t('monitorCommon.firstText')} {formatLatency(t, row.primary_first_text_ms)} ms
                        </div>
                        <div>
                          {t('monitorCommon.firstByte')} {formatLatency(t, row.primary_first_byte_ms)} ms
                        </div>
                        <div>
                          {t('usage.latencyDuration')} {formatLatency(t, row.primary_latency_ms)} ms
                        </div>
                      </div>
                    )
                  }
                  if (key === 'enabled') {
                    return <Toggle checked={row.enabled} onCheckedChange={() => toggleEnabled(row)} />
                  }
                  if (key === 'actions') {
                    const running = runningId === row.id
                    const duplicating = duplicatingIds.has(row.id)
                    const runTitle = running
                      ? t('admin.channelMonitor.running')
                      : !activeProbesEnabled
                        ? t('admin.channelMonitor.probesDisabledHint')
                        : t('admin.channelMonitor.runNow')
                    return (
                      <div className="table-actions">
                        <span
                          title={runTitle}
                          className={running || !activeProbesEnabled ? 'cursor-not-allowed' : ''}
                        >
                          <button
                            data-testid="monitor-run"
                            title={runTitle}
                            onClick={() => handleRunNow(row)}
                            disabled={running || !activeProbesEnabled}
                            className="table-action disabled:pointer-events-none"
                          >
                            <RefreshCw className={`h-3.5 w-3.5 ${running ? 'animate-spin' : ''}`} />
                            <span className="text-xs">
                              {running ? t('admin.channelMonitor.running') : t('admin.channelMonitor.runNow')}
                            </span>
                          </button>
                        </span>
                        <button
                          data-testid="monitor-duplicate"
                          title={
                            row.api_key_decrypt_failed
                              ? t('admin.channelMonitor.duplicateKeyUnavailable')
                              : duplicating
                                ? t('admin.channelMonitor.duplicating')
                                : t('admin.channelMonitor.duplicate')
                          }
                          disabled={duplicating || Boolean(row.api_key_decrypt_failed)}
                          onClick={() => handleDuplicate(row)}
                          className="table-action"
                        >
                          <Copy className="h-3.5 w-3.5" />
                          <span className="text-xs">
                            {duplicating ? t('admin.channelMonitor.duplicating') : t('admin.channelMonitor.duplicate')}
                          </span>
                        </button>
                        <button className="table-action" onClick={() => openEditDialog(row)}>
                          <Pencil className="h-3.5 w-3.5" />
                          <span className="text-xs">{t('common.edit')}</span>
                        </button>
                        <button className="table-action table-action-danger" onClick={() => handleDelete(row)}>
                          <Trash2 className="h-3.5 w-3.5" />
                          <span className="text-xs">{t('common.delete')}</span>
                        </button>
                      </div>
                    )
                  }
                  return null
                }}
                empty={
                  <EmptyState
                    icon={<BarChart3 className="h-10 w-10" />}
                    title={t('admin.channelMonitor.noMonitorsYet')}
                    description={t('admin.channelMonitor.createFirstMonitor')}
                  />
                }
              />
            }
            pagination={
              pagination.total > 0 ? (
                <div className="monitor-legacy-pagination">
                  <Pagination
                    page={pagination.page}
                    total={pagination.total}
                    pageSize={pagination.page_size}
                    onPageChange={onPageChange}
                    onPageSizeChange={onPageSizeChange}
                  />
                </div>
              ) : null
            }
          />
        )}
      </div>

      <MonitorFormDialog
        show={showDialog}
        monitor={editing}
        onClose={closeDialog}
        onSaved={() => setReloadKey((k) => k + 1)}
      />

      <Modal
        open={showRunResult}
        onClose={() => setShowRunResult(false)}
        title={t('admin.channelMonitor.runResultTitle')}
        footer={
          <div className="flex justify-end">
            <Button variant="primary" onClick={() => setShowRunResult(false)}>
              {t('common.close')}
            </Button>
          </div>
        }
      >
        <div className="space-y-2">
          {runResults.map((r) => (
            <div
              key={r.model}
              className="flex items-center justify-between rounded-lg border border-gray-200 px-3 py-2 text-sm dark:border-dark-600"
            >
              <div className="flex flex-col">
                <span className="font-medium text-gray-900 dark:text-white">{formatMonitorModel(t, r.model)}</span>
                {r.message && <span className="text-xs surface-muted">{r.message}</span>}
              </div>
              <div className="flex items-center gap-2">
                <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-[11px] ${statusBadgeClass(r.status)}`}>
                  {statusLabel(t, r.status)}
                </span>
                <span className="text-xs surface-muted" title={t('monitorCommon.timingHint')}>
                  {t('monitorCommon.firstText')} {formatLatency(t, r.first_text_ms)} ms · {t('monitorCommon.firstByte')}{' '}
                  {formatLatency(t, r.first_byte_ms)} ms · {t('usage.latencyDuration')} {formatLatency(t, r.latency_ms)} ms
                </span>
              </div>
            </div>
          ))}
        </div>
      </Modal>

      <Modal
        open={showDeleteDialog}
        onClose={() => setShowDeleteDialog(false)}
        title={t('common.delete')}
        size="sm"
        footer={
          <>
            <Button variant="ghost" onClick={() => setShowDeleteDialog(false)}>
              {t('common.cancel')}
            </Button>
            <Button variant="danger" onClick={confirmDelete}>
              {t('common.delete')}
            </Button>
          </>
        }
      >
        <div className="text-sm text-gray-600 dark:text-gray-300">{deleteConfirmMessage}</div>
      </Modal>
    </div>
  )
}

function MonitorFormDialog({
  show,
  monitor,
  onClose,
  onSaved
}: {
  show: boolean
  monitor: ChannelMonitor | null
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)

  const [form, setForm] = useState<MonitorFormState>(EMPTY_FORM)
  const [submitting, setSubmitting] = useState(false)
  const [extraModelsText, setExtraModelsText] = useState('')

  useEffect(() => {
    if (!show) return
    if (monitor) {
      setForm({
        name: monitor.name,
        provider: monitor.provider,
        api_mode: monitor.api_mode,
        endpoint: monitor.endpoint,
        api_key: '',
        check_mode: monitor.check_mode,
        account_id: monitor.account_id,
        primary_model: monitor.primary_model,
        extra_models: monitor.extra_models || [],
        group_name: monitor.group_name,
        enabled: monitor.enabled,
        interval_seconds: monitor.interval_seconds,
        jitter_seconds: monitor.jitter_seconds,
        stream_probe: monitor.stream_probe ?? false
      })
      setExtraModelsText((monitor.extra_models || []).join(', '))
    } else {
      setForm(EMPTY_FORM)
      setExtraModelsText('')
    }
  }, [show, monitor])

  const usesProbePart = form.check_mode !== 'quota'

  const handleSubmit = async () => {
    if (submitting) return
    if (!form.name.trim()) {
      showError(t('admin.channelMonitor.form.nameRequired'))
      return
    }
    setSubmitting(true)
    try {
      const extraModels = extraModelsText
        .split(/[,\n;]+/)
        .map((s) => s.trim())
        .filter(Boolean)
      if (monitor) {
        await adminAPI.channelMonitor.update(monitor.id, {
          name: form.name.trim(),
          provider: form.provider,
          api_mode: form.api_mode,
          endpoint: form.endpoint,
          api_key: form.api_key,
          check_mode: form.check_mode,
          account_id: form.account_id,
          primary_model: form.primary_model,
          extra_models: extraModels,
          group_name: form.group_name,
          enabled: form.enabled,
          interval_seconds: form.interval_seconds,
          jitter_seconds: form.jitter_seconds,
          stream_probe: form.stream_probe
        })
        showSuccess(t('admin.channelMonitor.updateSuccess'))
      } else {
        await adminAPI.channelMonitor.create({
          name: form.name.trim(),
          provider: form.provider,
          api_mode: form.api_mode,
          endpoint: form.endpoint,
          api_key: form.api_key,
          check_mode: form.check_mode,
          account_id: form.account_id,
          primary_model: form.primary_model,
          extra_models: extraModels,
          group_name: form.group_name,
          enabled: form.enabled,
          interval_seconds: form.interval_seconds,
          jitter_seconds: form.jitter_seconds,
          stream_probe: form.stream_probe
        })
        showSuccess(t('admin.channelMonitor.createSuccess'))
      }
      onSaved()
      onClose()
    } catch (err) {
      showError(extractApiErrorMessage(err, t('common.error')))
    } finally {
      setSubmitting(false)
    }
  }

  const checkModeOptions = [
    { value: 'probe' as CheckMode, label: t('monitorCommon.checkMode.probe') },
    { value: 'quota' as CheckMode, label: t('monitorCommon.checkMode.quota') },
    { value: 'quota_probe' as CheckMode, label: t('monitorCommon.checkMode.quota_probe') }
  ]

  const apiModeOptions = [
    { value: 'chat_completions' as APIMode, label: 'Chat Completions' },
    { value: 'responses' as APIMode, label: 'Responses' }
  ]

  return (
    <Modal
      open={show}
      onClose={onClose}
      title={monitor ? t('admin.channelMonitor.editTitle') : t('admin.channelMonitor.createTitle')}
      size="xl"
      footer={
        <div className="flex justify-end gap-3">
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" loading={submitting} onClick={handleSubmit}>
            {submitting ? t('common.saving') : t('common.save')}
          </Button>
        </div>
      }
    >
      <div className="space-y-5">
        <div>
          <label className="input-label">
            {t('admin.channelMonitor.form.name')} <span className="text-red-500">*</span>
          </label>
          <Input
            type="text"
            required
            value={form.name}
            onValueChange={(value) => setForm((prev) => ({ ...prev, name: value }))}
            placeholder={t('admin.channelMonitor.form.namePlaceholder')}
          />
        </div>

        <div>
          <label className="input-label">{t('admin.channelMonitor.form.checkMode')}</label>
          <div className="flex w-full gap-2">
            {checkModeOptions.map((opt) => (
              <button
                key={opt.value}
                type="button"
                data-testid={`monitor-check-mode-${opt.value}`}
                aria-pressed={form.check_mode === opt.value}
                onClick={() => setForm((prev) => ({ ...prev, check_mode: opt.value }))}
                className={`flex-1 rounded-lg border px-3 py-2 text-sm font-medium transition-colors ${
                  form.check_mode === opt.value
                    ? 'border-primary-400 bg-primary-50/[0.15] text-primary-700 dark:border-primary-500 dark:bg-primary-900/20 dark:text-primary-300'
                    : 'border-gray-200 surface-control surface-muted hover:surface-hover dark:border-dark-600'
                }`}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>

        <div>
          <label className="input-label">
            {t('admin.channelMonitor.form.provider')} <span className="text-red-500">*</span>
          </label>
          <Select
            value={form.provider}
            searchable={false}
            options={PROVIDER_LIST.map((value) => ({ value, label: t(`monitorCommon.providers.${value}`) }))}
            onValueChange={(value) => setForm((prev) => ({ ...prev, provider: String(value) as Provider }))}
          />
        </div>

        {form.provider === 'openai' && usesProbePart && (
          <div>
            <label className="input-label">{t('admin.channelMonitor.form.apiMode')}</label>
            <Select
              value={form.api_mode}
              searchable={false}
              options={apiModeOptions.map((opt) => ({ value: opt.value, label: opt.label }))}
              onValueChange={(value) => setForm((prev) => ({ ...prev, api_mode: String(value) as APIMode }))}
            />
          </div>
        )}

        {usesProbePart && (
          <div>
            <label className="input-label">
              {t('admin.channelMonitor.form.endpoint')} <span className="text-red-500">*</span>
            </label>
            <div className="flex gap-2">
              <Input
                data-testid="monitor-endpoint"
                type="text"
                required
                className="flex-1"
                value={form.endpoint}
                onValueChange={(value) => setForm((prev) => ({ ...prev, endpoint: value }))}
                placeholder={t('admin.channelMonitor.form.endpointPlaceholder')}
              />
              <Button variant="secondary" className="whitespace-nowrap" onClick={() => setForm((prev) => ({ ...prev, endpoint: window.location.origin }))}>
                {t('admin.channelMonitor.form.useCurrentDomain')}
              </Button>
            </div>
          </div>
        )}

        {usesProbePart && (
          <div>
            <label className="input-label">
              {t('admin.channelMonitor.form.apiKey')}
              {!monitor && <span className="text-red-500"> *</span>}
            </label>
            <div className="flex gap-2">
              <Input
                type="password"
                required={!monitor}
                className="flex-1"
                value={form.api_key}
                onValueChange={(value) => setForm((prev) => ({ ...prev, api_key: value }))}
                placeholder={
                  monitor ? t('admin.channelMonitor.form.apiKeyEditPlaceholder') : t('admin.channelMonitor.form.apiKeyPlaceholder')
                }
              />
            </div>
            {monitor && monitor.api_key_masked && <p className="mt-1 text-xs surface-muted">{monitor.api_key_masked}</p>}
          </div>
        )}

        <div>
          <label className="input-label">
            {t('admin.channelMonitor.form.primaryModel')} <span className="text-red-500">*</span>
          </label>
          <Input
            data-testid="monitor-primary-model"
            type="text"
            required
            value={form.primary_model}
            onValueChange={(value) => setForm((prev) => ({ ...prev, primary_model: value }))}
            placeholder={t('admin.channelMonitor.form.primaryModelPlaceholder')}
          />
        </div>

        <div>
          <label className="input-label">{t('admin.channelMonitor.form.extraModels')}</label>
          <Input
            type="text"
            value={extraModelsText}
            onValueChange={setExtraModelsText}
            placeholder={t('admin.channelMonitor.form.extraModelsPlaceholder')}
          />
        </div>

        <div>
          <label className="input-label">{t('admin.channelMonitor.form.groupName')}</label>
          <Input
            type="text"
            value={form.group_name}
            onValueChange={(value) => setForm((prev) => ({ ...prev, group_name: value }))}
            placeholder={t('admin.channelMonitor.form.groupNamePlaceholder')}
          />
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label className="input-label">{t('admin.channelMonitor.form.interval')}</label>
            <Input
              type="number"
              min="0"
              step="1"
              value={form.interval_seconds}
              onValueChange={(value) => setForm((prev) => ({ ...prev, interval_seconds: Number(value) }))}
            />
          </div>
          <div>
            <label className="input-label">{t('admin.channelMonitor.form.jitter')}</label>
            <Input
              type="number"
              min="0"
              step="1"
              value={form.jitter_seconds}
              onValueChange={(value) => setForm((prev) => ({ ...prev, jitter_seconds: Number(value) }))}
            />
          </div>
        </div>

        <div className="flex items-center gap-6">
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={form.enabled}
              onChange={(event) => setForm((prev) => ({ ...prev, enabled: event.target.checked }))}
              className="h-4 w-4 rounded border-gray-300 text-primary-600"
            />
            <span className="text-sm surface-muted">{t('admin.channelMonitor.form.enabled')}</span>
          </label>
          {usesProbePart && (
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={form.stream_probe}
                onChange={(event) => setForm((prev) => ({ ...prev, stream_probe: event.target.checked }))}
                className="h-4 w-4 rounded border-gray-300 text-primary-600"
              />
              <span className="text-sm surface-muted">{t('admin.channelMonitor.form.streamProbe')}</span>
            </label>
          )}
        </div>
      </div>
    </Modal>
  )
}
