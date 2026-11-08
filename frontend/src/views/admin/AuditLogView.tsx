import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Clock, Eye, Search, Shield, Trash2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { auditAPI } from '@/api/admin/audit'
import { totpAPI } from '@/api/totp'
import type { AuditLog } from '@/api/admin/audit'
import { useAppStore } from '@/stores/app'
import { Button, Input, Modal, Pagination, Select } from '@/components/ui'
import { AdminDataTable, type Column } from './AdminDataTable'
import TablePageLayout from '@/components/layout/TablePageLayout'

interface AuditFilters {
  q: string
  actor_email: string
  action: string
  client_ip: string
  method: string
  auth_method: string
  success: string
}

const EMPTY_FILTERS: AuditFilters = {
  q: '',
  actor_email: '',
  action: '',
  client_ip: '',
  method: '',
  auth_method: '',
  success: ''
}

const TIME_RANGE_MINUTES: Record<string, number> = {
  '30m': 30,
  '1h': 60,
  '6h': 6 * 60,
  '24h': 24 * 60,
  '7d': 7 * 24 * 60,
  '30d': 30 * 24 * 60
}

function formatTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

function prettyBody(body: string): string {
  try {
    return JSON.stringify(JSON.parse(body), null, 2)
  } catch {
    return body
  }
}

function toRFC3339(local: string): string | undefined {
  if (!local) return undefined
  const d = new Date(local)
  if (Number.isNaN(d.getTime())) return undefined
  return d.toISOString()
}

function toDatetimeLocal(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function formatCustomTimeRangeLabel(startTime: string, endTime: string): string {
  const fmt = (raw: string) => {
    const d = new Date(raw)
    if (Number.isNaN(d.getTime())) return raw
    const pad = (n: number) => String(n).padStart(2, '0')
    return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
  }
  return `${fmt(startTime)} ~ ${fmt(endTime)}`
}

const STATUS_BADGE_BASE = 'inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-semibold'

function statusBadgeClass(status: number): string {
  if (status >= 500) return `${STATUS_BADGE_BASE} bg-red-100/[0.15] text-red-700 dark:bg-red-900/30 dark:text-red-300`
  if (status >= 400) return `${STATUS_BADGE_BASE} bg-amber-100/[0.15] text-amber-700 dark:bg-amber-900/30 dark:text-amber-300`
  return `${STATUS_BADGE_BASE} bg-green-100/[0.15] text-green-700 dark:bg-green-900/30 dark:text-green-300`
}

function statusDotClass(status: number): string {
  if (status >= 500) return 'bg-red-500'
  if (status >= 400) return 'bg-amber-500'
  return 'bg-green-500'
}

export default function AuditLogView() {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)

  const [loading, setLoading] = useState(false)
  const [logs, setLogs] = useState<AuditLog[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [filters, setFilters] = useState<AuditFilters>(EMPTY_FILTERS)

  // 时间范围：预设窗口（同 /admin/ops 时间下拉）+ 自定义起止（datetime-local，支持时分）
  const [timeRange, setTimeRange] = useState('')
  const [customStartTime, setCustomStartTime] = useState('')
  const [customEndTime, setCustomEndTime] = useState('')
  const [showCustomTimeRangeDialog, setShowCustomTimeRangeDialog] = useState(false)
  const [customStartTimeInput, setCustomStartTimeInput] = useState('')
  const [customEndTimeInput, setCustomEndTimeInput] = useState('')

  const [detailVisible, setDetailVisible] = useState(false)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detail, setDetail] = useState<AuditLog | null>(null)

  const [clearConfirmVisible, setClearConfirmVisible] = useState(false)
  const [clearTotpVisible, setClearTotpVisible] = useState(false)
  const [clearTotpCode, setClearTotpCode] = useState('')
  const [clearing, setClearing] = useState(false)
  const [checkingTotpStatus, setCheckingTotpStatus] = useState(false)

  // The loader always reads the latest query state so every entry point
  // (mount, filter change, page change, reset) shares one code path.
  const queryRef = useRef({ page, pageSize, filters, timeRange, customStartTime, customEndTime })
  queryRef.current = { page, pageSize, filters, timeRange, customStartTime, customEndTime }

  const load = useCallback(async () => {
    const current = queryRef.current
    setLoading(true)
    try {
      const res = await auditAPI.list({
        page: current.page,
        page_size: current.pageSize,
        q: current.filters.q || undefined,
        actor_email: current.filters.actor_email || undefined,
        action: current.filters.action || undefined,
        client_ip: current.filters.client_ip || undefined,
        method: current.filters.method || undefined,
        auth_method: current.filters.auth_method || undefined,
        success: current.filters.success || undefined,
        ...buildTimeRangeQuery(current.timeRange, current.customStartTime, current.customEndTime)
      })
      setLogs(res.items)
      setTotal(res.total)
    } catch (err) {
      showError((err as { message?: string })?.message || t('admin.audit.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [showError, t])

  const [reloadKey, setReloadKey] = useState(0)
  useEffect(() => {
    void load()
  }, [load, reloadKey])

  const search = useCallback(() => {
    setPage(1)
    setReloadKey((key) => key + 1)
  }, [])

  const resetFilters = () => {
    setFilters(EMPTY_FILTERS)
    setTimeRange('')
    setCustomStartTime('')
    setCustomEndTime('')
    search()
  }

  const onPageChange = (p: number) => {
    setPage(p)
    setReloadKey((key) => key + 1)
  }

  const onPageSizeChange = (ps: number) => {
    setPageSize(ps)
    setPage(1)
    setReloadKey((key) => key + 1)
  }

  const methodOptions = useMemo(() => [
    { value: '', label: t('admin.audit.filters.all') },
    { value: 'POST', label: 'POST' },
    { value: 'PUT', label: 'PUT' },
    { value: 'PATCH', label: 'PATCH' },
    { value: 'DELETE', label: 'DELETE' },
    { value: 'GET', label: 'GET' }
  ], [t])

  const authMethodOptions = useMemo(() => [
    { value: '', label: t('admin.audit.filters.all') },
    { value: 'jwt', label: 'JWT' },
    { value: 'admin_api_key', label: 'Admin API Key' },
    { value: 'mcp_oauth', label: 'MCP OAuth' }
  ], [t])

  const resultOptions = useMemo(() => [
    { value: '', label: t('admin.audit.filters.all') },
    { value: 'true', label: t('admin.audit.filters.resultSuccess') },
    { value: 'false', label: t('admin.audit.filters.resultFailure') }
  ], [t])

  const timeRangeOptions = useMemo(() => [
    { value: '', label: t('admin.audit.filters.all') },
    { value: '30m', label: t('admin.ops.timeRange.30m') },
    { value: '1h', label: t('admin.ops.timeRange.1h') },
    { value: '6h', label: t('admin.ops.timeRange.6h') },
    { value: '24h', label: t('admin.ops.timeRange.24h') },
    { value: '7d', label: t('admin.ops.timeRange.7d') },
    { value: '30d', label: t('admin.ops.timeRange.30d') },
    {
      value: 'custom',
      label:
        timeRange === 'custom' && customStartTime && customEndTime
          ? `${t('admin.ops.timeRange.custom')} (${formatCustomTimeRangeLabel(customStartTime, customEndTime)})`
          : t('admin.ops.timeRange.custom')
    }
  ], [t, timeRange, customStartTime, customEndTime])

  const authMethodLabel = (method: string): string => {
    const found = authMethodOptions.find((o) => o.value === method)
    return found && found.value ? found.label : method
  }

  const columns: Column[] = useMemo(() => [
    { key: 'created_at', label: t('admin.audit.columns.time') },
    { key: 'actor', label: t('admin.audit.columns.actor') },
    { key: 'action', label: t('admin.audit.columns.action') },
    { key: 'status_code', label: t('admin.audit.columns.result') },
    { key: 'latency_ms', label: t('admin.audit.detail.latency') },
    { key: 'client_ip', label: t('admin.audit.columns.clientIp') },
    { key: 'actions', label: t('common.actions') }
  ], [t])

  const openDetail = async (id: number) => {
    setDetailVisible(true)
    setDetailLoading(true)
    setDetail(null)
    try {
      setDetail(await auditAPI.get(id))
    } catch (err) {
      showError((err as { message?: string })?.message || t('admin.audit.loadFailed'))
      setDetailVisible(false)
    } finally {
      setDetailLoading(false)
    }
  }

  const handleTimeRangeChange = (val: string | number | boolean | null) => {
    const value = String(val ?? '')
    if (value === 'custom') {
      // 预填：已有自定义值沿用，否则默认最近1小时（本地时区）
      const now = new Date()
      setCustomStartTimeInput(customStartTime || toDatetimeLocal(new Date(now.getTime() - 60 * 60 * 1000)))
      setCustomEndTimeInput(customEndTime || toDatetimeLocal(now))
      setShowCustomTimeRangeDialog(true)
      return
    }
    setTimeRange(value)
    search()
  }

  const handleCustomTimeRangeConfirm = () => {
    if (!customStartTimeInput || !customEndTimeInput) return
    setCustomStartTime(customStartTimeInput)
    setCustomEndTime(customEndTimeInput)
    setTimeRange('custom')
    setShowCustomTimeRangeDialog(false)
    search()
  }

  // 未确认不改变当前时间范围；Select 是受控组件，展示值保持不变。
  const handleCustomTimeRangeCancel = () => {
    setShowCustomTimeRangeDialog(false)
  }

  // 与其他敏感操作一致：未启用 2FA 时直接提示去个人资料启用 TOTP，
  // 而不是弹出一个无法完成的验证码输入框（后端会以 TOTP_NOT_SETUP 拒绝）。
  const openClearDialog = async () => {
    if (checkingTotpStatus) return
    setCheckingTotpStatus(true)
    try {
      const status = await totpAPI.getStatus()
      if (!status.enabled) {
        showError(t('stepUp.notEnabled'))
        return
      }
    } catch (err) {
      showError((err as { message?: string })?.message || t('admin.audit.loadFailed'))
      return
    } finally {
      setCheckingTotpStatus(false)
    }
    setClearConfirmVisible(true)
  }

  const onClearConfirmed = () => {
    setClearConfirmVisible(false)
    setClearTotpCode('')
    setClearTotpVisible(true)
  }

  const cancelClearTotp = () => {
    if (clearing) return
    setClearTotpVisible(false)
  }

  const submitClear = async () => {
    if (clearTotpCode.length !== 6) return
    setClearing(true)
    try {
      const res = await auditAPI.clear(clearTotpCode)
      setClearTotpVisible(false)
      showSuccess(t('admin.audit.clearConfirm.success', { count: res.deleted }))
      search()
    } catch (err) {
      showError((err as { message?: string })?.message || t('admin.audit.clearConfirm.failed'))
      setClearTotpCode('')
    } finally {
      setClearing(false)
    }
  }

  const statusText = (status: number): string =>
    status < 400 ? t('admin.audit.filters.resultSuccess') : t('admin.audit.filters.resultFailure')

  return (
    <>
      <TablePageLayout
        filters={
        <div className="card p-4 sm:p-6">
          <div className="filter-bar">
            <div className="filter-bar filter-bar--inline">
              <div className="filter-field filter-field--search">
                <label className="input-label">{t('admin.audit.filters.q')}</label>
                <div className="relative">
                  <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
                  <Input
                    value={filters.q}
                    onValueChange={(value) => setFilters((prev) => ({ ...prev, q: value }))}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter') search()
                    }}
                    placeholder={t('admin.audit.filters.qPlaceholder')}
                    className="pl-10"
                  />
                </div>
              </div>

              <div className="filter-field">
                <label className="input-label">{t('admin.audit.filters.actorEmail')}</label>
                <Input
                  value={filters.actor_email}
                  onValueChange={(value) => setFilters((prev) => ({ ...prev, actor_email: value }))}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') search()
                  }}
                />
              </div>

              <div className="filter-field">
                <label className="input-label">{t('admin.audit.filters.action')}</label>
                <Input
                  value={filters.action}
                  onValueChange={(value) => setFilters((prev) => ({ ...prev, action: value }))}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') search()
                  }}
                />
              </div>

              <div className="filter-field">
                <label className="input-label">{t('admin.audit.filters.clientIp')}</label>
                <Input
                  value={filters.client_ip}
                  onValueChange={(value) => setFilters((prev) => ({ ...prev, client_ip: value }))}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') search()
                  }}
                />
              </div>

              <div className="filter-field">
                <label className="input-label">{t('admin.audit.filters.method')}</label>
                <Select
                  value={filters.method}
                  options={methodOptions}
                  onValueChange={(value) => {
                    setFilters((prev) => ({ ...prev, method: String(value ?? '') }))
                    search()
                  }}
                />
              </div>

              <div className="filter-field">
                <label className="input-label">{t('admin.audit.filters.authMethod')}</label>
                <Select
                  value={filters.auth_method}
                  options={authMethodOptions}
                  onValueChange={(value) => {
                    setFilters((prev) => ({ ...prev, auth_method: String(value ?? '') }))
                    search()
                  }}
                />
              </div>

              <div className="filter-field">
                <label className="input-label">{t('admin.audit.filters.result')}</label>
                <Select
                  value={filters.success}
                  options={resultOptions}
                  onValueChange={(value) => {
                    setFilters((prev) => ({ ...prev, success: String(value ?? '') }))
                    search()
                  }}
                />
              </div>

              <div className="filter-field">
                <label className="input-label">{t('admin.dashboard.timeRange')}</label>
                <Select value={timeRange} options={timeRangeOptions} onValueChange={handleTimeRangeChange} />
              </div>
            </div>

            <div className="filter-actions">
              <Button variant="primary" disabled={loading} onClick={search}>
                {t('common.search')}
              </Button>
              <Button variant="secondary" disabled={loading} onClick={resetFilters}>
                {t('common.reset')}
              </Button>
              <Button variant="danger" onClick={openClearDialog}>
                <Trash2 className="mr-1.5 h-4 w-4" />
                {t('admin.audit.clearAll')}
              </Button>
            </div>
          </div>
        </div>
      }
      table={
        <AdminDataTable<AuditLog>
          columns={columns}
          data={logs}
          loading={loading}
          rowKey="id"
          renderCell={(key, row) => {
            if (key === 'created_at') {
              return <span className="whitespace-nowrap surface-muted">{formatTime(row.created_at)}</span>
            }
            if (key === 'actor') {
              return (
                <div className="min-w-0 max-w-[220px]">
                  <div className="truncate font-medium text-gray-900 dark:text-white" title={row.actor_email}>
                    {row.actor_email || '—'}
                  </div>
                  <div className="mt-0.5 truncate text-xs surface-muted">
                    {row.actor_role}
                    {row.auth_method && <span> · {authMethodLabel(row.auth_method)}</span>}
                  </div>
                </div>
              )
            }
            if (key === 'action') {
              return (
                <div className="min-w-0 max-w-xs">
                  <div className="truncate font-mono text-sm text-gray-800 dark:text-gray-200" title={row.action}>
                    {row.action}
                  </div>
                  <div className="mt-0.5 truncate font-mono text-xs surface-muted" title={`${row.method} ${row.path}`}>
                    {row.method} {row.path}
                  </div>
                </div>
              )
            }
            if (key === 'status_code') {
              return (
                <span className={statusBadgeClass(row.status_code)}>
                  <span className={cn('console-data-mark h-1.5 w-1.5 rounded-full', statusDotClass(row.status_code))} />
                  {row.status_code}
                </span>
              )
            }
            if (key === 'latency_ms') {
              return <span className="whitespace-nowrap surface-muted">{row.latency_ms} ms</span>
            }
            if (key === 'client_ip') {
              return <span className="whitespace-nowrap font-mono surface-muted">{row.client_ip || '—'}</span>
            }
            if (key === 'actions') {
              return (
                <button
                  type="button"
                  className="inline-flex items-center gap-1 font-medium text-primary-600 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300 surface-control"
                  onClick={() => openDetail(row.id)}
                >
                  <Eye className="h-4 w-4" />
                  {t('admin.audit.columns.detail')}
                </button>
              )
            }
            return null
          }}
          empty={
            <div className="flex flex-col items-center py-8">
              <Shield className="mb-4 h-12 w-12 text-gray-300 dark:text-dark-600" />
              <p className="text-sm font-medium surface-muted">{t('admin.audit.empty')}</p>
            </div>
          }
        />
      }
      pagination={
        total > 0 ? (
          <Pagination
            page={page}
            total={total}
            pageSize={pageSize}
            onPageChange={onPageChange}
            onPageSizeChange={onPageSizeChange}
          />
        ) : null
      }
    />

    <Modal
      open={detailVisible}
      onClose={() => setDetailVisible(false)}
      title={t('admin.audit.detail.title')}
      size="xl"
      closeOnOverlayClick
    >
      {detailLoading ? (
        <div className="flex items-center justify-center py-16">
          <div className="flex flex-col items-center gap-3">
            <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600" />
            <div className="text-sm font-medium surface-muted">{t('common.loading')}</div>
          </div>
        </div>
      ) : detail ? (
        <div className="space-y-5 py-2">
          {/* Hero: action + result at a glance */}
          <div className="glass-surface border p-5">
            <div className="flex flex-wrap items-center gap-3">
              <span className={statusBadgeClass(detail.status_code)}>
                <span className={cn('console-data-mark h-1.5 w-1.5 rounded-full', statusDotClass(detail.status_code))} />
                {detail.status_code} {statusText(detail.status_code)}
              </span>
              <span className="break-all font-mono text-base font-semibold text-gray-900 dark:text-white">
                {detail.action}
              </span>
            </div>

            <div className="mt-3 flex items-center gap-2 rounded-lg surface-inset px-3 py-2 ring-1 ring-gray-200 dark:ring-dark-600">
              <span className="rounded surface-inset px-1.5 py-0.5 font-mono text-[11px] font-bold surface-muted">
                {detail.method}
              </span>
              <span className="break-all font-mono text-xs surface-muted">{detail.path}</span>
            </div>

            <div className="mt-3 flex flex-wrap items-center gap-x-5 gap-y-1.5 text-xs surface-muted">
              <span className="inline-flex items-center gap-1.5">
                <Clock className="h-3.5 w-3.5" />
                {formatTime(detail.created_at)}
              </span>
              <span>
                {t('admin.audit.detail.latency')} {detail.latency_ms} ms
              </span>
              {detail.request_id && (
                <span className="inline-flex items-center gap-1">
                  {t('admin.audit.detail.requestId')}
                  <span className="break-all font-mono">{detail.request_id}</span>
                </span>
              )}
            </div>
          </div>

          {/* Actor / auth / source */}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <div className="rounded-xl surface-inset p-4">
              <div className="text-xs font-bold uppercase tracking-wider surface-muted">
                {t('admin.audit.columns.actor')}
              </div>
              <div className="mt-1 break-all text-sm font-medium text-gray-900 dark:text-white">
                {detail.actor_email || '—'}
              </div>
              <div className="mt-0.5 text-xs surface-muted">{detail.actor_role}</div>
            </div>

            <div className="rounded-xl surface-inset p-4">
              <div className="text-xs font-bold uppercase tracking-wider surface-muted">
                {t('admin.audit.filters.authMethod')}
              </div>
              <div className="mt-1 text-sm font-medium text-gray-900 dark:text-white">
                {authMethodLabel(detail.auth_method) || '—'}
              </div>
              {detail.credential_masked && (
                <div className="mt-0.5 break-all font-mono text-xs surface-muted">
                  {detail.credential_masked}
                </div>
              )}
            </div>

            <div className="rounded-xl surface-inset p-4">
              <div className="text-xs font-bold uppercase tracking-wider surface-muted">
                {t('admin.audit.columns.clientIp')}
              </div>
              <div className="mt-1 break-all font-mono text-sm font-medium text-gray-900 dark:text-white">
                {detail.client_ip || '—'}
              </div>
            </div>
          </div>

          <section>
            <h4 className="mb-1.5 text-xs font-bold uppercase tracking-wider surface-muted">
              {t('admin.audit.detail.userAgent')}
            </h4>
            <div className="break-all rounded-xl surface-inset p-3 font-mono text-xs leading-relaxed surface-muted">
              {detail.user_agent || '—'}
            </div>
          </section>

          {detail.request_body && (
            <section>
              <h4 className="mb-1.5 text-xs font-bold uppercase tracking-wider surface-muted">
                {t('admin.audit.detail.requestBody')}
              </h4>
              <pre className="max-h-72 overflow-auto rounded-xl surface-inset p-4 font-mono text-xs leading-relaxed surface-muted">
                {prettyBody(detail.request_body)}
              </pre>
            </section>
          )}

          {detail.extra && Object.keys(detail.extra).length > 0 && (
            <section>
              <h4 className="mb-1.5 text-xs font-bold uppercase tracking-wider surface-muted">
                {t('admin.audit.detail.extra')}
              </h4>
              <pre className="max-h-48 overflow-auto rounded-xl surface-inset p-4 font-mono text-xs leading-relaxed surface-muted">
                {JSON.stringify(detail.extra, null, 2)}
              </pre>
            </section>
          )}
        </div>
      ) : null}
    </Modal>

    {/* Custom time range dialog (与 /admin/ops 时间下拉一致的自定义范围，支持时分) */}
    <Modal
      open={showCustomTimeRangeDialog}
      onClose={handleCustomTimeRangeCancel}
      title={t('admin.ops.timeRange.custom')}
      size="sm"
      footer={
        <>
          <Button variant="secondary" onClick={handleCustomTimeRangeCancel}>
            {t('common.cancel')}
          </Button>
          <Button
            variant="primary"
            disabled={!customStartTimeInput || !customEndTimeInput}
            onClick={handleCustomTimeRangeConfirm}
          >
            {t('common.confirm')}
          </Button>
        </>
      }
    >
      <div className="space-y-4 py-2">
        <div>
          <label className="input-label">{t('admin.ops.customTimeRange.startTime')}</label>
          <Input
            type="datetime-local"
            aria-label={t('admin.ops.customTimeRange.startTime')}
            value={customStartTimeInput}
            onValueChange={setCustomStartTimeInput}
          />
        </div>
        <div>
          <label className="input-label">{t('admin.ops.customTimeRange.endTime')}</label>
          <Input
            type="datetime-local"
            aria-label={t('admin.ops.customTimeRange.endTime')}
            value={customEndTimeInput}
            onValueChange={setCustomEndTimeInput}
          />
        </div>
      </div>
    </Modal>

    {/* Clear confirmation → step-up TOTP */}
    <Modal
      open={clearConfirmVisible}
      onClose={() => setClearConfirmVisible(false)}
      title={t('admin.audit.clearConfirm.title')}
      size="sm"
      footer={
        <>
          <Button variant="ghost" onClick={() => setClearConfirmVisible(false)}>
            {t('common.cancel')}
          </Button>
          <Button variant="danger" onClick={onClearConfirmed}>
            {t('admin.audit.clearAll')}
          </Button>
        </>
      }
    >
      <div className="text-sm text-gray-600 dark:text-gray-300">{t('admin.audit.clearConfirm.message')}</div>
    </Modal>

    {/* TOTP prompt for the clear operation */}
    <Modal
      open={clearTotpVisible}
      onClose={cancelClearTotp}
      title={t('admin.audit.clearConfirm.totpTitle')}
      size="sm"
      footer={
        <>
          <Button variant="secondary" disabled={clearing} onClick={cancelClearTotp}>
            {t('common.cancel')}
          </Button>
          <Button variant="danger" disabled={clearing || clearTotpCode.length !== 6} onClick={submitClear}>
            {clearing ? t('common.loading') : t('admin.audit.clearAll')}
          </Button>
        </>
      }
    >
      <div className="py-2">
        <p className="text-sm surface-muted">{t('admin.audit.clearConfirm.totpHint')}</p>
        <Input
          value={clearTotpCode}
          onValueChange={setClearTotpCode}
          type="text"
          inputMode="numeric"
          maxLength={6}
          autoComplete="one-time-code"
          className="mt-4 text-center text-lg tracking-[0.5em]"
          placeholder="••••••"
          onKeyDown={(event) => {
            if (event.key === 'Enter') void submitClear()
          }}
        />
      </div>
    </Modal>
    </>
  )
}

function buildTimeRangeQuery(
  timeRange: string,
  customStartTime: string,
  customEndTime: string
): { start_time?: string; end_time?: string } {
  if (timeRange === 'custom') {
    return {
      start_time: toRFC3339(customStartTime),
      end_time: toRFC3339(customEndTime)
    }
  }
  const minutes = TIME_RANGE_MINUTES[timeRange]
  if (!minutes) return {}
  return { start_time: new Date(Date.now() - minutes * 60 * 1000).toISOString() }
}
