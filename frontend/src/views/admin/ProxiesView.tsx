import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button, EmptyState, Input, Pagination, Select, TextArea } from '@/components/ui'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { useDialogFocus } from '@/composables/useDialogFocus'
import { useClipboard } from '@/composables/useClipboard'
import { getCountryOptions } from '@/utils/countries'
import { getTimezoneOptions } from '@/utils/timezones'
import { formatDateTime } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'
import { proxyExpiryBadgeClass, proxyExpiryLabelKey } from '@/utils/proxyExpiry'
import { proxyTestSnapshot } from '@/utils/proxyTestSnapshot'
import { PlatformTypeBadge } from '@/components/badges'
import type {
  CreateProxyRequest,
  Proxy,
  ProxyAccountSummary,
  ProxyProtocol,
  ProxyQualityCheckResult,
  UpdateProxyRequest
} from '@/types'
import { AdminDataTable, type Column, type SortOrder } from '@/views/admin/AdminDataTable'
import { TablePageLayout } from '@/components/layout/TablePageLayout'
import { getPersistedPageSize } from '@/views/admin/shared'
import { Icon } from '@/views/admin/AccountIcon'
import { DateTimePicker } from './parts/DateTimePicker'
import { ImportDataModal } from './ImportDataModal'

const EXPIRY_PRESETS = [7, 30, 90, 180]

const DIALOG_SIZE_CLASS = {
  narrow: 'max-w-md',
  sm: 'max-w-sm',
  md: 'max-w-lg',
  lg: 'max-w-2xl',
  xl: 'max-w-4xl',
  full: 'max-w-6xl'
} as const

interface ProxyDialogProps {
  open: boolean
  title: string
  onClose: () => void
  children?: React.ReactNode
  footer?: React.ReactNode
  size?: keyof typeof DIALOG_SIZE_CLASS
}

/**
 * The console dialog shell. It renders the ported modal markup and additionally
 * labels the dialog with its own heading so assistive technology and the
 * browser management suite can address a dialog by its visible title.
 */
function ProxyDialog({ open, title, onClose, children, footer, size = 'md' }: ProxyDialogProps) {
  const { t } = useTranslation()
  const panelRef = useRef<HTMLDivElement>(null)
  const titleId = `proxy-dialog-title-${useId().replace(/:/g, '')}`

  useDialogFocus(open, panelRef, onClose)

  if (!open) return null

  return createPortal(
    <div className="modal-overlay">
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className={cn('modal-content surface-panel mx-auto', DIALOG_SIZE_CLASS[size])}
      >
        <div className="modal-header">
          <h2 id={titleId} className="modal-title">
            {title}
          </h2>
          <button
            type="button"
            onClick={onClose}
            aria-label={t('common.close')}
            className="-mr-2 shrink-0 rounded-xl p-2 transition-colors hover:surface-hover hover:text-gray-600 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/30 focus-visible:ring-offset-2 dark:hover:text-dark-300 dark:focus-visible:ring-offset-dark-900 surface-control surface-muted"
          >
            <Icon name="x" size="md" />
          </button>
        </div>
        <div className="modal-body">{children}</div>
        {footer && <div className="modal-footer">{footer}</div>}
      </div>
    </div>,
    document.body
  )
}

interface ConfirmProps {
  open: boolean
  title: string
  message: React.ReactNode
  confirmText: string
  cancelText: string
  danger?: boolean
  onConfirm: () => void
  onCancel: () => void
}

function ProxyConfirmDialog({
  open,
  title,
  message,
  confirmText,
  cancelText,
  danger = false,
  onConfirm,
  onCancel
}: ConfirmProps) {
  return (
    <ProxyDialog
      open={open}
      title={title}
      onClose={onCancel}
      size="narrow"
      footer={
        <div className="flex min-w-0 max-w-full flex-wrap justify-end gap-3 [overflow-wrap:anywhere]">
          <Button variant="secondary" onClick={onCancel}>
            {cancelText}
          </Button>
          <Button variant={danger ? 'danger' : 'primary'} onClick={onConfirm}>
            {confirmText}
          </Button>
        </div>
      }
    >
      <p className="text-sm text-gray-600 dark:text-gray-300">{message}</p>
    </ProxyDialog>
  )
}

type TestProxyResult = Awaited<ReturnType<typeof adminAPI.proxies.testProxy>>

interface ProbeRun<T> {
  snapshot?: string
  promise: Promise<T | null>
}

interface BatchParseEntry {
  protocol: ProxyProtocol
  host: string
  port: number
  username: string
  password: string
}

interface ProxyQueryOverrides {
  page?: number
  pageSize?: number
  protocol?: string
  status?: string
  search?: string
  sortKey?: string
  sortOrder?: SortOrder
}

interface ProxyFormState {
  name: string
  protocol: ProxyProtocol
  host: string
  port: string
  username: string
  password: string
  status: 'active' | 'inactive'
  expires_at: string
  fallback_mode: 'none' | 'proxy' | 'direct'
  backup_proxy_id: number | null
  expiry_warn_days: number
  egress_timezone: string
  egress_country: string
}

const EMPTY_PROXY_FORM: ProxyFormState = {
  name: '',
  protocol: 'http',
  host: '',
  port: '8080',
  username: '',
  password: '',
  status: 'active',
  expires_at: '',
  fallback_mode: 'none',
  backup_proxy_id: null,
  expiry_warn_days: 7,
  egress_timezone: '',
  egress_country: ''
}

function toLocalDateStr(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

/** base 为空 → 今天本地 00:00；否则该日期本地 00:00 */
function baseDateOrToday(baseDateStr: string): Date {
  const base = baseDateStr ? new Date(`${baseDateStr}T00:00:00`) : new Date()
  base.setHours(0, 0, 0, 0)
  return base
}

/** base + N 天 → 本地 YYYY-MM-DD；N≤0/空 → '' 表示永不过期 */
function addDaysToBase(baseDateStr: string, days: number | null): string {
  const value = Number(days)
  if (!value || value <= 0) return ''
  const date = baseDateOrToday(baseDateStr)
  date.setDate(date.getDate() + value)
  return toLocalDateStr(date)
}

/** target 相对 base 的整天数（本地日历差，避免时区/时刻抖动） */
function daysFromBase(baseDateStr: string, targetDateStr: string): number | null {
  if (!targetDateStr) return null
  const target = new Date(`${targetDateStr}T00:00:00`)
  return Math.round((target.getTime() - baseDateOrToday(baseDateStr).getTime()) / 86400000)
}

/**
 * Parse proxy URL: protocol://user:pass@host:port or protocol://host:port
 * Host may be a domain, IPv4, or bracketed IPv6 ([2001:db8::1]).
 */
function parseProxyUrl(line: string): BatchParseEntry | null {
  const trimmed = line.trim()
  if (!trimmed) return null

  // Regex to parse proxy URL (supports http, https, socks5, socks5h).
  // Host alternatives: [bracketed-IPv6] | hostname/IPv4 (colon-free, so the
  // match stops before the final :port).
  const regex = /^(https?|socks5h?):\/\/(?:([^:@[]+):([^@[]+)@)?(\[[0-9a-f:.]+\]|[^:[]+):(\d+)$/i
  const match = trimmed.match(regex)

  if (!match) return null

  const [, protocol, username, password, rawHost, port] = match
  const portNum = parseInt(port, 10)

  if (portNum < 1 || portNum > 65535) return null

  // Strip brackets from IPv6 literals; the backend re-brackets via net.JoinHostPort.
  const host = rawHost.replace(/^\[|\]$/g, '').trim()

  return {
    protocol: protocol.toLowerCase() as ProxyProtocol,
    host,
    port: portNum,
    username: username?.trim() || '',
    password: password?.trim() || ''
  }
}

function buildAuthPart(row: { username?: string | null; password?: string | null }): string {
  const user = row.username ? encodeURIComponent(row.username) : ''
  const pass = row.password ? encodeURIComponent(row.password) : ''
  if (user && pass) return `${user}:${pass}@`
  if (user) return `${user}@`
  if (pass) return `:${pass}@`
  return ''
}

function buildProxyUrl(row: { protocol: string; host: string; port: number; username?: string | null; password?: string | null }): string {
  return `${row.protocol}://${buildAuthPart(row)}${row.host}:${row.port}`
}

export default function ProxiesView() {
  const { t, i18n } = useTranslation()
  const showSuccess = useAppStore((state) => state.showSuccess)
  const showError = useAppStore((state) => state.showError)
  const showInfo = useAppStore((state) => state.showInfo)
  const { copyToClipboard } = useClipboard()

  const [proxies, setProxies] = useState<Proxy[]>([])
  const [loading, setLoading] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [protocolFilter, setProtocolFilter] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(getPersistedPageSize())
  const [total, setTotal] = useState(0)
  const [sortKey, setSortKey] = useState('id')
  const [sortOrder, setSortOrder] = useState<SortOrder>('desc')

  const [selectedProxyKeys, setSelectedProxyKeys] = useState<Array<string | number>>([])
  const selectedProxyIds = selectedProxyKeys.map((key) => Number(key)).filter((id) => Number.isFinite(id))
  const selectedCount = selectedProxyIds.length

  const [visiblePasswordIds, setVisiblePasswordIds] = useState<number[]>([])
  const [copyMenuProxyId, setCopyMenuProxyId] = useState<number | null>(null)

  const [showCreateModal, setShowCreateModal] = useState(false)
  const [createMode, setCreateMode] = useState<'standard' | 'batch'>('standard')
  const [createPasswordVisible, setCreatePasswordVisible] = useState(false)
  const [createForm, setCreateForm] = useState<ProxyFormState>(EMPTY_PROXY_FORM)
  // 有效期「选天数」⇄ 日历联动：天数自 base 起算（创建=今天；编辑=代理创建日）
  const createExpiresDays = daysFromBase('', createForm.expires_at)
  const applyCreateExpiresDays = (days: number | null) => {
    setCreateForm((previous) => ({ ...previous, expires_at: addDaysToBase('', days) }))
  }

  const [showEditModal, setShowEditModal] = useState(false)
  const [editPasswordVisible, setEditPasswordVisible] = useState(false)
  const [editPasswordDirty, setEditPasswordDirty] = useState(false)
  const [editingProxy, setEditingProxy] = useState<Proxy | null>(null)
  const [editForm, setEditForm] = useState<ProxyFormState>(EMPTY_PROXY_FORM)

  const [showImportData, setShowImportData] = useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [deletingProxy, setDeletingProxy] = useState<Proxy | null>(null)
  const [showBatchDeleteDialog, setShowBatchDeleteDialog] = useState(false)
  const [showExportDataDialog, setShowExportDataDialog] = useState(false)
  const [exportingData, setExportingData] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  const [showAccountsModal, setShowAccountsModal] = useState(false)
  const [accountsProxy, setAccountsProxy] = useState<Proxy | null>(null)
  const [proxyAccounts, setProxyAccounts] = useState<ProxyAccountSummary[]>([])
  const [accountsLoading, setAccountsLoading] = useState(false)

  const [showQualityReportDialog, setShowQualityReportDialog] = useState(false)
  const [qualityReportProxy, setQualityReportProxy] = useState<Proxy | null>(null)
  const [qualityReport, setQualityReport] = useState<ProxyQualityCheckResult | null>(null)

  const [testingProxyIds, setTestingProxyIds] = useState<number[]>([])
  const [qualityCheckingProxyIds, setQualityCheckingProxyIds] = useState<number[]>([])
  const [batchTesting, setBatchTesting] = useState(false)
  const [batchQualityChecking, setBatchQualityChecking] = useState(false)

  const [batchInput, setBatchInput] = useState('')
  const [batchParseResult, setBatchParseResult] = useState<{
    total: number
    valid: number
    invalid: number
    duplicate: number
    proxies: BatchParseEntry[]
  }>({ total: 0, valid: 0, invalid: 0, duplicate: 0, proxies: [] })

  const [allProxiesForBackup, setAllProxiesForBackup] = useState<Proxy[]>([])

  const disposedRef = useRef(false)
  const abortControllerRef = useRef<AbortController | null>(null)
  const searchTimerRef = useRef<number | null>(null)
  const proxiesRef = useRef(proxies)
  proxiesRef.current = proxies
  const connectionRuns = useRef(new Map<number, ProbeRun<TestProxyResult>>())
  const qualityRuns = useRef(new Map<number, ProbeRun<ProxyQualityCheckResult>>())

  useEffect(() => {
    const connectionRunsAtMount = connectionRuns.current
    const qualityRunsAtMount = qualityRuns.current
    disposedRef.current = false
    return () => {
      disposedRef.current = true
      connectionRunsAtMount.clear()
      qualityRunsAtMount.clear()
      if (searchTimerRef.current) window.clearTimeout(searchTimerRef.current)
      abortControllerRef.current?.abort()
    }
  }, [])

  useEffect(() => {
    const closeCopyMenu = (event: MouseEvent) => {
      const target = event.target as HTMLElement | null
      // The trigger and the format menu stop propagation, but never let an
      // unrelated click leave a dangling menu open either.
      if (target?.closest('[data-proxy-copy-menu], [data-proxy-copy-trigger]')) return
      setCopyMenuProxyId(null)
    }
    document.addEventListener('click', closeCopyMenu)
    return () => document.removeEventListener('click', closeCopyMenu)
  }, [])

  const egressTimezoneOptions = useMemo(
    () => [
      { value: '', label: t('admin.proxies.egressTimezoneNone') },
      ...getTimezoneOptions()
    ],
    [t]
  )

  const egressCountryOptions = useMemo(
    () => [
      { value: '', label: t('admin.proxies.egressCountryNone') },
      ...getCountryOptions(i18n.language || 'en')
    ],
    [t, i18n.language]
  )

  const protocolFilterOptions = useMemo(
    () => [
      { value: '', label: t('admin.proxies.allProtocols') },
      { value: 'http', label: 'HTTP' },
      { value: 'https', label: 'HTTPS' },
      { value: 'socks5', label: 'SOCKS5' },
      { value: 'socks5h', label: 'SOCKS5H' }
    ],
    [t]
  )

  const statusFilterOptions = useMemo(
    () => [
      { value: '', label: t('admin.proxies.allStatus') },
      { value: 'active', label: t('admin.accounts.status.active') },
      { value: 'inactive', label: t('admin.accounts.status.inactive') },
      { value: 'expired', label: t('admin.proxies.expired') }
    ],
    [t]
  )

  const protocolSelectOptions = useMemo(
    () => [
      { value: 'http', label: t('admin.proxies.protocols.http') },
      { value: 'https', label: t('admin.proxies.protocols.https') },
      { value: 'socks5', label: t('admin.proxies.protocols.socks5') },
      { value: 'socks5h', label: t('admin.proxies.protocols.socks5h') }
    ],
    [t]
  )

  const editStatusOptions = useMemo(
    () => [
      { value: 'active', label: t('admin.accounts.status.active') },
      { value: 'inactive', label: t('admin.accounts.status.inactive') }
    ],
    [t]
  )

  const fallbackModeOptions = useMemo(
    () => [
      { value: 'none', label: t('admin.proxies.fallbackNone') },
      { value: 'proxy', label: t('admin.proxies.fallbackProxy') },
      { value: 'direct', label: t('admin.proxies.fallbackDirect') }
    ],
    [t]
  )

  const columns = useMemo<Column[]>(
    () => [
      { key: 'name', label: t('admin.proxies.columns.name'), sortable: true },
      { key: 'protocol', label: t('admin.proxies.columns.protocol'), sortable: true },
      { key: 'address', label: t('admin.proxies.columns.address'), sortable: false },
      { key: 'auth', label: t('admin.proxies.columns.auth'), sortable: false },
      { key: 'location', label: t('admin.proxies.columns.location'), sortable: false },
      { key: 'account_count', label: t('admin.proxies.columns.accounts'), sortable: true },
      { key: 'latency', label: t('admin.proxies.columns.latency'), sortable: false },
      { key: 'expiry', label: t('admin.proxies.columns.expiry'), sortable: true },
      { key: 'created_at', label: t('admin.proxies.columns.createdAt'), sortable: true },
      { key: 'status', label: t('admin.proxies.columns.status'), sortable: true },
      { key: 'actions', label: t('admin.proxies.columns.actions'), sortable: false }
    ],
    [t]
  )

  const buildProxyQueryFilters = (overrides: ProxyQueryOverrides = {}) => {
    const protocol = overrides.protocol ?? protocolFilter
    const status = overrides.status ?? statusFilter
    const search = overrides.search ?? searchQuery
    return {
      protocol: protocol || undefined,
      status: (status || undefined) as 'active' | 'inactive' | 'expired' | undefined,
      search: search || undefined,
      sort_by: overrides.sortKey ?? sortKey,
      sort_order: overrides.sortOrder ?? sortOrder
    }
  }

  const isAbortError = (error: unknown) => {
    if (!error || typeof error !== 'object') return false
    const maybeError = error as { name?: string; code?: string }
    return maybeError.name === 'AbortError' || maybeError.code === 'ERR_CANCELED'
  }

  const loadProxies = async (overrides: ProxyQueryOverrides = {}) => {
    if (disposedRef.current) return
    if (abortControllerRef.current) {
      abortControllerRef.current.abort()
    }
    const currentAbortController = new AbortController()
    abortControllerRef.current = currentAbortController
    const targetPage = overrides.page ?? page
    const targetPageSize = overrides.pageSize ?? pageSize
    setLoading(true)
    try {
      const response = await adminAPI.proxies.list(targetPage, targetPageSize, buildProxyQueryFilters(overrides), {
        signal: currentAbortController.signal
      })
      if (currentAbortController.signal.aborted || abortControllerRef.current !== currentAbortController) {
        return
      }
      setProxies(response.items)
      setTotal(response.total)
    } catch (error) {
      if (isAbortError(error)) {
        return
      }
      showError(t('admin.proxies.failedToLoad'))
      console.error('Error loading proxies:', error)
    } finally {
      if (abortControllerRef.current === currentAbortController) {
        setLoading(false)
        abortControllerRef.current = null
      }
    }
  }

  useEffect(() => {
    void loadProxies()
    void loadBackupProxyOptions()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const loadBackupProxyOptions = async () => {
    try {
      setAllProxiesForBackup(await adminAPI.proxies.getAllWithCount())
    } catch (error) {
      console.error('Error loading backup proxy options:', error)
    }
  }

  const handleFilterChange = (overrides: ProxyQueryOverrides = {}) => {
    setPage(1)
    void loadProxies({ ...overrides, page: 1 })
  }

  const handleSearch = (value: string) => {
    setSearchQuery(value)
    if (searchTimerRef.current) window.clearTimeout(searchTimerRef.current)
    searchTimerRef.current = window.setTimeout(() => {
      setPage(1)
      void loadProxies({ search: value, page: 1 })
    }, 300)
  }

  const handlePageChange = (next: number) => {
    setPage(next)
    void loadProxies({ page: next })
  }

  const handlePageSizeChange = (next: number) => {
    setPageSize(next)
    setPage(1)
    void loadProxies({ pageSize: next, page: 1 })
  }

  const handleSort = (key: string, order: SortOrder) => {
    setSortKey(key)
    setSortOrder(order)
    setPage(1)
    void loadProxies({ sortKey: key, sortOrder: order, page: 1 })
  }

  const backupProxyOptions = (excludeId?: number) =>
    allProxiesForBackup
      .filter((proxy) => proxy.id !== excludeId)
      .map((proxy) => ({ value: proxy.id, label: `${proxy.name} (${proxy.host}:${proxy.port})` }))

  const closeCreateModal = () => {
    setShowCreateModal(false)
    setCreateMode('standard')
    setCreateForm(EMPTY_PROXY_FORM)
    setCreatePasswordVisible(false)
    setBatchInput('')
    setBatchParseResult({ total: 0, valid: 0, invalid: 0, duplicate: 0, proxies: [] })
  }

  const handleDataImported = () => {
    setShowImportData(false)
    void loadProxies()
  }

  const parseBatchInput = (value: string) => {
    const lines = value.split('\n').filter((line) => line.trim())
    const seen = new Set<string>()
    const parsed: BatchParseEntry[] = []
    let invalid = 0
    let duplicate = 0

    for (const line of lines) {
      const entry = parseProxyUrl(line)
      if (!entry) {
        invalid++
        continue
      }

      // Check for duplicates (same host:port:username:password)
      const key = `${entry.host}:${entry.port}:${entry.username}:${entry.password}`
      if (seen.has(key)) {
        duplicate++
        continue
      }
      seen.add(key)
      parsed.push(entry)
    }

    setBatchParseResult({
      total: lines.length,
      valid: parsed.length,
      invalid,
      duplicate,
      proxies: parsed
    })
  }

  const handleBatchCreate = async () => {
    if (batchParseResult.valid === 0) return

    setSubmitting(true)
    try {
      const result = await adminAPI.proxies.batchCreate(batchParseResult.proxies)
      const created = result.created || 0
      const skipped = result.skipped || 0

      if (created > 0) {
        showSuccess(t('admin.proxies.batchImportSuccess', { created, skipped }))
      } else {
        showInfo(t('admin.proxies.batchImportAllSkipped', { skipped }))
      }

      closeCreateModal()
      void loadProxies()
    } catch (error) {
      showError(extractApiErrorMessage(error, t('admin.proxies.failedToImport')))
      console.error('Error batch creating proxies:', error)
    } finally {
      setSubmitting(false)
    }
  }

  const handleCreateProxy = async () => {
    if (!createForm.name.trim()) {
      showError(t('admin.proxies.nameRequired'))
      return
    }
    if (!createForm.host.trim()) {
      showError(t('admin.proxies.hostRequired'))
      return
    }
    const port = Number(createForm.port)
    if (port < 1 || port > 65535) {
      showError(t('admin.proxies.portInvalid'))
      return
    }
    setSubmitting(true)
    try {
      const payload: CreateProxyRequest = {
        name: createForm.name.trim(),
        protocol: createForm.protocol,
        host: createForm.host.trim(),
        port,
        username: createForm.username.trim() || null,
        password: createForm.password.trim() || null,
        expires_at: createForm.expires_at ? Math.floor(new Date(createForm.expires_at).getTime() / 1000) : null,
        fallback_mode: createForm.fallback_mode,
        backup_proxy_id: createForm.fallback_mode === 'proxy' ? createForm.backup_proxy_id : null,
        expiry_warn_days: createForm.expiry_warn_days,
        egress_timezone: createForm.egress_timezone.trim(),
        egress_country: createForm.egress_country.trim().toUpperCase()
      }
      await adminAPI.proxies.create(payload)
      showSuccess(t('admin.proxies.proxyCreated'))
      closeCreateModal()
      void loadProxies()
    } catch (error) {
      showError(extractApiErrorMessage(error, t('admin.proxies.failedToCreate')))
      console.error('Error creating proxy:', error)
    } finally {
      setSubmitting(false)
    }
  }

  const handleEdit = (proxy: Proxy) => {
    setEditingProxy(proxy)
    setEditForm({
      name: proxy.name,
      protocol: proxy.protocol,
      host: proxy.host,
      port: String(proxy.port),
      username: proxy.username || '',
      password: proxy.password || '',
      status: proxy.status === 'expired' ? 'inactive' : proxy.status,
      expires_at: proxy.expires_at ? proxy.expires_at.slice(0, 10) : '',
      fallback_mode: proxy.fallback_mode || 'none',
      backup_proxy_id: proxy.backup_proxy_id ?? null,
      expiry_warn_days: proxy.expiry_warn_days ?? 7,
      egress_timezone: proxy.egress_timezone || '',
      egress_country: proxy.egress_country || ''
    })
    setEditPasswordVisible(false)
    setEditPasswordDirty(false)
    setShowEditModal(true)
  }

  // 编辑时有效期自「代理创建日」起算；创建时无 created_at → base='' 用今天
  const editBaseDate = editingProxy?.created_at ? editingProxy.created_at.slice(0, 10) : ''
  const editExpiresDays = daysFromBase(editBaseDate, editForm.expires_at)
  const applyEditExpiresDays = (days: number | null) => {
    setEditForm((previous) => ({ ...previous, expires_at: addDaysToBase(editBaseDate, days) }))
  }

  const closeEditModal = () => {
    setShowEditModal(false)
    setEditingProxy(null)
    setEditPasswordVisible(false)
    setEditPasswordDirty(false)
  }

  const handleUpdateProxy = async () => {
    if (!editingProxy) return
    if (!editForm.name.trim()) {
      showError(t('admin.proxies.nameRequired'))
      return
    }
    if (!editForm.host.trim()) {
      showError(t('admin.proxies.hostRequired'))
      return
    }
    const port = Number(editForm.port)
    if (port < 1 || port > 65535) {
      showError(t('admin.proxies.portInvalid'))
      return
    }

    setSubmitting(true)
    try {
      const updateData: UpdateProxyRequest = {
        name: editForm.name.trim(),
        protocol: editForm.protocol,
        host: editForm.host.trim(),
        port,
        username: editForm.username.trim(),
        status: editForm.status,
        expires_at: editForm.expires_at ? Math.floor(new Date(editForm.expires_at).getTime() / 1000) : null,
        fallback_mode: editForm.fallback_mode,
        backup_proxy_id: editForm.fallback_mode === 'proxy' ? editForm.backup_proxy_id : null,
        expiry_warn_days: editForm.expiry_warn_days,
        egress_timezone: editForm.egress_timezone.trim(),
        egress_country: editForm.egress_country.trim().toUpperCase()
      }

      // Only include password if user actually modified the field
      if (editPasswordDirty) {
        updateData.password = editForm.password.trim()
      }

      await adminAPI.proxies.update(editingProxy.id, updateData)
      showSuccess(t('admin.proxies.proxyUpdated'))
      closeEditModal()
      void loadProxies()
    } catch (error) {
      showError(extractApiErrorMessage(error, t('admin.proxies.failedToUpdate')))
      console.error('Error updating proxy:', error)
    } finally {
      setSubmitting(false)
    }
  }

  const applyLatencyResult = (
    proxyId: number,
    result: {
      success: boolean
      latency_ms?: number
      message?: string
      ip_address?: string
      country?: string
      country_code?: string
      region?: string
      city?: string
    }
  ) => {
    setProxies((previous) => {
      const target = previous.find((proxy) => proxy.id === proxyId)
      if (!target) return previous
      const next: Proxy = { ...target, latency_message: result.message }
      if (result.success) {
        next.latency_status = 'success'
        next.latency_ms = result.latency_ms
        next.ip_address = result.ip_address
        next.country = result.country
        next.country_code = result.country_code
        next.region = result.region
        next.city = result.city
      } else {
        next.latency_status = 'failed'
        next.latency_ms = undefined
        next.ip_address = undefined
        next.country = undefined
        next.country_code = undefined
        next.region = undefined
        next.city = undefined
      }
      return previous.map((proxy) => (proxy.id === proxyId ? next : proxy))
    })
  }

  const summarizeQualityStatus = (result: ProxyQualityCheckResult): Proxy['quality_status'] => {
    if (result.challenge_count > 0) return 'challenge'
    if (result.failed_count > 0) return 'failed'
    if (result.warn_count > 0) return 'warn'
    return 'healthy'
  }

  const applyQualityResult = (proxyId: number, result: ProxyQualityCheckResult) => {
    setProxies((previous) =>
      previous.map((proxy) =>
        proxy.id === proxyId
          ? {
              ...proxy,
              quality_status: summarizeQualityStatus(result),
              quality_score: result.score,
              quality_grade: result.grade,
              quality_summary: result.summary,
              quality_checked: result.checked_at
            }
          : proxy
      )
    )
  }

  const formatLocation = (proxy: Proxy) => {
    const parts = [proxy.country, proxy.city].filter(Boolean) as string[]
    return parts.join(' · ')
  }

  const flagUrl = (code: string) => `https://unpkg.com/flag-icons/flags/4x3/${code.toLowerCase()}.svg`

  const startTestingProxy = (proxyId: number) => {
    setTestingProxyIds((previous) => (previous.includes(proxyId) ? previous : [...previous, proxyId]))
  }

  const stopTestingProxy = (proxyId: number) => {
    setTestingProxyIds((previous) => previous.filter((id) => id !== proxyId))
  }

  const startQualityCheckingProxy = (proxyId: number) => {
    setQualityCheckingProxyIds((previous) => (previous.includes(proxyId) ? previous : [...previous, proxyId]))
  }

  const stopQualityCheckingProxy = (proxyId: number) => {
    setQualityCheckingProxyIds((previous) => previous.filter((id) => id !== proxyId))
  }

  const runProxyTest = async (
    proxyId: number,
    notify: boolean,
    captured?: Proxy
  ): Promise<TestProxyResult | null> => {
    if (disposedRef.current) return null
    const owner = proxiesRef.current.find((proxy) => proxy.id === proxyId) || captured
    const snapshot = owner ? proxyTestSnapshot(owner) : undefined
    const pending = connectionRuns.current.get(proxyId)
    if (pending && pending.snapshot === snapshot) return pending.promise
    const run: ProbeRun<TestProxyResult> = { snapshot, promise: Promise.resolve(null) }
    connectionRuns.current.set(proxyId, run)
    const isCurrent = () =>
      connectionRuns.current.get(proxyId) === run &&
      snapshot !== undefined &&
      proxiesRef.current.some((proxy) => proxy.id === proxyId && proxyTestSnapshot(proxy) === snapshot)
    startTestingProxy(proxyId)
    run.promise = (async () => {
      try {
        const result = await adminAPI.proxies.testProxy(proxyId)
        if (!isCurrent()) return null
        applyLatencyResult(proxyId, result)
        if (notify) {
          if (result.success) {
            const message = result.latency_ms
              ? t('admin.proxies.proxyWorkingWithLatency', { latency: result.latency_ms })
              : t('admin.proxies.proxyWorking')
            showSuccess(message)
          } else {
            showError(result.message || t('admin.proxies.proxyTestFailed'))
          }
        }
        return result
      } catch (error) {
        if (!isCurrent()) return null
        const message = extractApiErrorMessage(error, t('admin.proxies.failedToTest'))
        applyLatencyResult(proxyId, { success: false, message })
        if (notify) {
          showError(message)
        }
        console.error('Error testing proxy:', error)
        return null
      } finally {
        if (connectionRuns.current.get(proxyId) === run) {
          connectionRuns.current.delete(proxyId)
          stopTestingProxy(proxyId)
        }
      }
    })()
    return run.promise
  }

  const handleTestConnection = async (proxy: Proxy) => {
    await runProxyTest(proxy.id, true)
  }

  const runProxyQualityCheck = async (
    proxyId: number,
    notify: boolean,
    captured?: Proxy
  ): Promise<ProxyQualityCheckResult | null> => {
    if (disposedRef.current) return null
    const owner = proxiesRef.current.find((proxy) => proxy.id === proxyId) || captured
    const snapshot = owner ? proxyTestSnapshot(owner) : undefined
    const pending = qualityRuns.current.get(proxyId)
    if (pending && pending.snapshot === snapshot) return pending.promise
    const visibleAtStart = proxiesRef.current.some((proxy) => proxy.id === proxyId)
    const run: ProbeRun<ProxyQualityCheckResult> = { snapshot, promise: Promise.resolve(null) }
    qualityRuns.current.set(proxyId, run)
    const isCurrent = () => {
      if (qualityRuns.current.get(proxyId) !== run) return false
      const target = proxiesRef.current.find((proxy) => proxy.id === proxyId)
      return target ? snapshot !== undefined && proxyTestSnapshot(target) === snapshot : !visibleAtStart
    }
    startQualityCheckingProxy(proxyId)
    run.promise = (async () => {
      try {
        const result = await adminAPI.proxies.checkProxyQuality(proxyId)
        if (!isCurrent()) return null
        if (notify && owner) {
          setQualityReportProxy(owner)
          setQualityReport(result)
          setShowQualityReportDialog(true)
        }
        const baseStep = result.items.find((item) => item.target === 'base_connectivity')
        if (baseStep && baseStep.status === 'pass') {
          applyLatencyResult(proxyId, {
            success: true,
            latency_ms: result.base_latency_ms,
            message: result.summary,
            ip_address: result.exit_ip,
            country: result.country,
            country_code: result.country_code
          })
        }
        applyQualityResult(proxyId, result)
        if (notify) showSuccess(t('admin.proxies.qualityCheckDone', { score: result.score, grade: result.grade }))
        return result
      } catch (error) {
        if (!isCurrent()) return null
        if (notify) {
          showError(extractApiErrorMessage(error, t('admin.proxies.qualityCheckFailed')))
          console.error('Error checking proxy quality:', error)
        }
        throw error
      } finally {
        if (qualityRuns.current.get(proxyId) === run) {
          qualityRuns.current.delete(proxyId)
          stopQualityCheckingProxy(proxyId)
        }
      }
    })()
    return run.promise
  }

  const handleQualityCheck = async (proxy: Proxy) => {
    try {
      await runProxyQualityCheck(proxy.id, true)
    } catch {
      // The current single probe already presents its error; batch callers count
      // the same shared failure independently in their completion summary.
    }
  }

  const runBatchProxyQualityChecks = async (ids: number[], snapshots: Proxy[]) => {
    if (ids.length === 0) return { total: 0, healthy: 0, warn: 0, challenge: 0, failed: 0 }

    const concurrency = 3
    let index = 0
    let healthy = 0
    let warn = 0
    let challenge = 0
    let failed = 0

    const worker = async () => {
      while (!disposedRef.current && index < ids.length) {
        const current = ids[index]
        index++
        try {
          const result = await runProxyQualityCheck(current, false, snapshots.find((proxy) => proxy.id === current))
          if (!result) continue
          if (result.challenge_count > 0) {
            challenge++
          } else if (result.failed_count > 0) {
            failed++
          } else if (result.warn_count > 0) {
            warn++
          } else {
            healthy++
          }
        } catch {
          failed++
        }
      }
    }

    const workers = Array.from({ length: Math.min(concurrency, ids.length) }, () => worker())
    await Promise.all(workers)
    return { total: ids.length, healthy, warn, challenge, failed }
  }

  const closeQualityReportDialog = () => {
    setShowQualityReportDialog(false)
    setQualityReportProxy(null)
    setQualityReport(null)
  }

  const qualityStatusClass = (status: string) => {
    if (status === 'pass') return 'badge-success'
    if (status === 'warn') return 'badge-warning'
    if (status === 'challenge') return 'badge-danger'
    return 'badge-danger'
  }

  const qualityStatusLabel = (status: string) => {
    if (status === 'pass') return t('admin.proxies.qualityStatusPass')
    if (status === 'warn') return t('admin.proxies.qualityStatusWarn')
    if (status === 'challenge') return t('admin.proxies.qualityStatusChallenge')
    return t('admin.proxies.qualityStatusFail')
  }

  const qualityOverallClass = (status?: string) => {
    if (status === 'healthy') return 'badge-success'
    if (status === 'warn') return 'badge-warning'
    if (status === 'challenge') return 'badge-danger'
    return 'badge-danger'
  }

  const qualityOverallLabel = (status?: string) => {
    if (status === 'healthy') return t('admin.proxies.qualityStatusHealthy')
    if (status === 'warn') return t('admin.proxies.qualityStatusWarn')
    if (status === 'challenge') return t('admin.proxies.qualityStatusChallenge')
    return t('admin.proxies.qualityStatusFail')
  }

  const qualityTargetLabel = (target: string) => {
    switch (target) {
      case 'base_connectivity':
        return t('admin.proxies.qualityTargetBase')
      case 'openai':
        return 'OpenAI'
      case 'anthropic':
        return 'Anthropic'
      case 'gemini':
        return 'Gemini'
      case 'grok':
        return 'Grok'
      case 'kimi':
        return 'Kimi'
      case 'zhipu':
        return 'Zhipu GLM'
      case 'deepseek':
        return 'DeepSeek'
      case 'minimax':
        return 'MiniMax'
      default:
        return target
    }
  }

  const fetchAllProxiesForBatch = async (): Promise<Proxy[]> => {
    const pageSize = 200
    const result: Proxy[] = []
    let currentPage = 1
    let totalPages = 1

    while (!disposedRef.current && currentPage <= totalPages) {
      const response = await adminAPI.proxies.list(currentPage, pageSize, buildProxyQueryFilters())
      if (disposedRef.current) return []
      result.push(...response.items)
      totalPages = response.pages || 1
      currentPage++
    }

    return result
  }

  const runBatchProxyTests = async (ids: number[], snapshots: Proxy[]) => {
    if (ids.length === 0) return
    const concurrency = 5
    let index = 0

    const worker = async () => {
      while (!disposedRef.current && index < ids.length) {
        const current = ids[index]
        index++
        await runProxyTest(current, false, snapshots.find((proxy) => proxy.id === current))
      }
    }

    const workers = Array.from({ length: Math.min(concurrency, ids.length) }, () => worker())
    await Promise.all(workers)
  }

  const handleBatchTest = async () => {
    if (disposedRef.current || batchTesting) return

    setBatchTesting(true)
    try {
      let ids: number[] = []
      let snapshots: Proxy[] = []
      if (selectedCount > 0) {
        ids = [...selectedProxyIds]
      } else {
        const allProxies = await fetchAllProxiesForBatch()
        snapshots = allProxies
        ids = allProxies.map((proxy) => proxy.id)
      }
      if (disposedRef.current) return

      if (ids.length === 0) {
        showInfo(t('admin.proxies.batchTestEmpty'))
        return
      }

      await runBatchProxyTests(ids, snapshots)
      if (disposedRef.current) return
      showSuccess(t('admin.proxies.batchTestDone', { count: ids.length }))
      void loadProxies()
    } catch (error) {
      if (disposedRef.current) return
      showError(extractApiErrorMessage(error, t('admin.proxies.batchTestFailed')))
      console.error('Error batch testing proxies:', error)
    } finally {
      setBatchTesting(false)
    }
  }

  const handleBatchQualityCheck = async () => {
    if (disposedRef.current || batchQualityChecking) return

    setBatchQualityChecking(true)
    try {
      let ids: number[] = []
      let snapshots: Proxy[] = []
      if (selectedCount > 0) {
        ids = [...selectedProxyIds]
      } else {
        const allProxies = await fetchAllProxiesForBatch()
        snapshots = allProxies
        ids = allProxies.map((proxy) => proxy.id)
      }
      if (disposedRef.current) return

      if (ids.length === 0) {
        showInfo(t('admin.proxies.batchQualityEmpty'))
        return
      }

      const summary = await runBatchProxyQualityChecks(ids, snapshots)
      if (disposedRef.current) return
      showSuccess(
        t('admin.proxies.batchQualityDone', {
          count: summary.total,
          healthy: summary.healthy,
          warn: summary.warn,
          challenge: summary.challenge,
          failed: summary.failed
        })
      )
      void loadProxies()
    } catch (error) {
      if (disposedRef.current) return
      showError(extractApiErrorMessage(error, t('admin.proxies.batchQualityFailed')))
      console.error('Error batch checking quality:', error)
    } finally {
      setBatchQualityChecking(false)
    }
  }

  const formatExportTimestamp = () => {
    const now = new Date()
    const pad2 = (value: number) => String(value).padStart(2, '0')
    return `${now.getFullYear()}${pad2(now.getMonth() + 1)}${pad2(now.getDate())}${pad2(now.getHours())}${pad2(now.getMinutes())}${pad2(now.getSeconds())}`
  }

  const handleExportData = async () => {
    if (exportingData) return
    setExportingData(true)
    try {
      const dataPayload = await adminAPI.proxies.exportData(
        selectedCount > 0 ? { ids: [...selectedProxyIds] } : { filters: buildProxyQueryFilters() }
      )
      const timestamp = formatExportTimestamp()
      const filename = `sub2api-proxy-${timestamp}.json`
      const blob = new Blob([JSON.stringify(dataPayload, null, 2)], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = filename
      link.click()
      URL.revokeObjectURL(url)
      showSuccess(t('admin.proxies.dataExported'))
    } catch (error) {
      showError(extractApiErrorMessage(error, t('admin.proxies.dataExportFailed')))
    } finally {
      setExportingData(false)
      setShowExportDataDialog(false)
    }
  }

  const handleDelete = (proxy: Proxy) => {
    if ((proxy.account_count || 0) > 0) {
      showError(t('admin.proxies.deleteBlockedInUse'))
      return
    }
    setDeletingProxy(proxy)
    setShowDeleteDialog(true)
  }

  const openBatchDelete = () => {
    if (selectedCount === 0) {
      return
    }
    setShowBatchDeleteDialog(true)
  }

  const confirmDelete = async () => {
    if (!deletingProxy) return

    try {
      await adminAPI.proxies.delete(deletingProxy.id)
      showSuccess(t('admin.proxies.proxyDeleted'))
      setShowDeleteDialog(false)
      setSelectedProxyKeys((previous) => previous.filter((key) => key !== String(deletingProxy.id)))
      setDeletingProxy(null)
      void loadProxies()
    } catch (error) {
      showError(extractApiErrorMessage(error, t('admin.proxies.failedToDelete')))
      console.error('Error deleting proxy:', error)
    }
  }

  const confirmBatchDelete = async () => {
    const ids = [...selectedProxyIds]
    if (ids.length === 0) {
      setShowBatchDeleteDialog(false)
      return
    }

    try {
      const result = await adminAPI.proxies.batchDelete(ids)
      const deleted = result.deleted_ids?.length || 0
      const skipped = result.skipped?.length || 0

      if (deleted > 0) {
        showSuccess(t('admin.proxies.batchDeleteDone', { deleted, skipped }))
      } else if (skipped > 0) {
        showInfo(t('admin.proxies.batchDeleteSkipped', { skipped }))
      }

      setSelectedProxyKeys([])
      setShowBatchDeleteDialog(false)
      void loadProxies()
    } catch (error) {
      showError(extractApiErrorMessage(error, t('admin.proxies.batchDeleteFailed')))
      console.error('Error batch deleting proxies:', error)
    }
  }

  const openAccountsModal = async (proxy: Proxy) => {
    setAccountsProxy(proxy)
    setProxyAccounts([])
    setAccountsLoading(true)
    setShowAccountsModal(true)

    try {
      setProxyAccounts(await adminAPI.proxies.getProxyAccounts(proxy.id))
    } catch (error) {
      showError(extractApiErrorMessage(error, t('admin.proxies.accountsFailed')))
      console.error('Error loading proxy accounts:', error)
    } finally {
      setAccountsLoading(false)
    }
  }

  const closeAccountsModal = () => {
    setShowAccountsModal(false)
    setAccountsProxy(null)
    setProxyAccounts([])
  }

  const togglePasswordVisibility = (proxyId: number) => {
    setVisiblePasswordIds((previous) =>
      previous.includes(proxyId) ? previous.filter((id) => id !== proxyId) : [...previous, proxyId]
    )
  }

  const getCopyFormats = (row: Proxy) => {
    const hasAuth = row.username || row.password
    const fullUrl = buildProxyUrl(row)
    const formats = [{ label: fullUrl, value: fullUrl }]
    if (hasAuth) {
      const withoutProtocol = fullUrl.replace(/^[^:]+:\/\//, '')
      formats.push({ label: withoutProtocol, value: withoutProtocol })
    }
    formats.push({ label: `${row.host}:${row.port}`, value: `${row.host}:${row.port}` })
    return formats
  }

  const copyProxyUrl = (row: Proxy) => {
    void copyToClipboard(buildProxyUrl(row), t('admin.proxies.urlCopied'))
    setCopyMenuProxyId(null)
  }

  const copyFormat = (value: string) => {
    void copyToClipboard(value, t('admin.proxies.urlCopied'))
    setCopyMenuProxyId(null)
  }

  const expiryLabel = (row: Proxy): string => {
    const { key, params } = proxyExpiryLabelKey(row.expires_at, row.status)
    return params ? t(key, params) : t(key)
  }

  return (
    <>
      <TablePageLayout
        filters={
          <div className="filter-bar">
            <div className="filter-field filter-field--search relative">
              <Icon name="search" size="md" className="absolute left-3 top-1/2 -translate-y-1/2 surface-muted" />
              <input
                value={searchQuery}
                type="text"
                placeholder={t('admin.proxies.searchProxies')}
                className="input pl-10"
                onChange={(event) => handleSearch(event.target.value)}
              />
            </div>

            <div className="filter-field">
              <Select
                value={protocolFilter}
                options={protocolFilterOptions}
                placeholder={t('admin.proxies.allProtocols')}
                searchable={false}
                onValueChange={(next) => {
                  setProtocolFilter(String(next ?? ''))
                  handleFilterChange({ protocol: String(next ?? '') })
                }}
              />
            </div>
            <div className="filter-field">
              <Select
                value={statusFilter}
                options={statusFilterOptions}
                placeholder={t('admin.proxies.allStatus')}
                searchable={false}
                onValueChange={(next) => {
                  setStatusFilter(String(next ?? ''))
                  handleFilterChange({ status: String(next ?? '') })
                }}
              />
            </div>

            <div className="filter-actions">
              <Button onClick={() => void loadProxies()} disabled={loading} title={t('common.refresh')}>
                <Icon name="refresh" size="md" className={loading ? 'animate-spin' : ''} />
              </Button>
              <Button
                onClick={() => void handleBatchTest()}
                disabled={batchTesting || loading}
                title={t('admin.proxies.testConnection')}
              >
                <Icon name="play" size="md" className="mr-2" />
                {t('admin.proxies.testConnection')}
              </Button>
              <Button
                onClick={() => void handleBatchQualityCheck()}
                disabled={batchQualityChecking || loading}
                title={t('admin.proxies.batchQualityCheck')}
              >
                <Icon
                  name="shield"
                  size="md"
                  className={cn('mr-2', batchQualityChecking && 'animate-pulse')}
                />
                {t('admin.proxies.batchQualityCheck')}
              </Button>
              <Button onClick={openBatchDelete} disabled={selectedCount === 0} title={t('admin.proxies.batchDeleteAction')}>
                <Icon name="trash" size="md" className="mr-2" />
                {t('admin.proxies.batchDeleteAction')}
              </Button>
              <Button onClick={() => setShowImportData(true)}>{t('admin.proxies.dataImport')}</Button>
              <Button onClick={() => setShowExportDataDialog(true)}>
                {selectedCount > 0 ? t('admin.proxies.dataExportSelected') : t('admin.proxies.dataExport')}
              </Button>
              <Button variant="primary" onClick={() => setShowCreateModal(true)}>
                <Icon name="plus" size="md" className="mr-2" />
                {t('admin.proxies.createProxy')}
              </Button>
            </div>
          </div>
        }
      table={
        <AdminDataTable<Proxy>
          columns={columns}
          data={proxies}
          loading={loading}
          rowKey="id"
          selectable
          selectedKeys={selectedProxyKeys}
          serverSideSort
          sortKey={sortKey}
          sortOrder={sortOrder}
          onSort={handleSort}
          onSelectedKeysChange={setSelectedProxyKeys}
          renderCell={(key, row, value) => {
            if (key === 'name') {
              return <span className="font-medium text-gray-900 dark:text-white">{String(value ?? '')}</span>
            }
            if (key === 'protocol') {
              return value ? (
                <span className={cn('badge', String(value).startsWith('socks5') ? 'badge-primary' : 'badge-gray')}>
                  {String(value).toUpperCase()}
                </span>
              ) : (
                <span className="text-sm surface-muted">-</span>
              )
            }
            if (key === 'address') {
              return (
                <div className="flex items-center gap-1.5">
                  <code className="code text-xs">
                    {row.host}:{row.port}
                  </code>
                  <div className="relative">
                    <button
                      type="button"
                      data-proxy-copy-trigger
                      className="rounded p-0.5 hover:text-primary-600 dark:hover:text-primary-400 surface-control surface-muted"
                      title={t('admin.proxies.copyProxyUrl')}
                      onClick={(event) => {
                        event.stopPropagation()
                        copyProxyUrl(row)
                      }}
                      onContextMenu={(event) => {
                        event.preventDefault()
                        setCopyMenuProxyId((previous) => (previous === row.id ? null : row.id))
                      }}
                    >
                      <Icon name="copy" size="sm" />
                    </button>
                    {/* 右键展开格式选择菜单 */}
                    {copyMenuProxyId === row.id && (
                      <div
                        data-proxy-copy-menu
                        className="absolute left-0 top-full z-50 mt-1 w-auto min-w-[180px] space-y-0.5 border border-gray-200 p-1 shadow-lg dark:border-dark-500 surface-panel"
                      >
                        {getCopyFormats(row).map((format) => (
                          <button
                            key={format.label}
                            type="button"
                            className="flex w-full items-center gap-2 rounded-[var(--surface-radius-item)] px-3 py-1.5 text-left text-xs hover:surface-hover surface-control"
                            onClick={(event) => {
                              event.stopPropagation()
                              copyFormat(format.value)
                            }}
                          >
                            <span className="truncate font-mono surface-muted">{format.label}</span>
                          </button>
                        ))}
                      </div>
                    )}
                  </div>
                </div>
              )
            }
            if (key === 'auth') {
              return row.username || row.password ? (
                <div className="flex items-center gap-1.5">
                  <div className="flex flex-col text-xs">
                    {row.username && <span className="surface-muted">{row.username}</span>}
                    {row.password && (
                      <span className="font-mono surface-muted">
                        {visiblePasswordIds.includes(row.id) ? row.password : '••••••'}
                      </span>
                    )}
                  </div>
                  {row.password && (
                    <button
                      type="button"
                      className="ml-1 rounded p-0.5 hover:text-gray-600 dark:hover:text-gray-300 surface-control surface-muted"
                      onClick={(event) => {
                        event.stopPropagation()
                        togglePasswordVisibility(row.id)
                      }}
                    >
                      <Icon name={visiblePasswordIds.includes(row.id) ? 'eyeOff' : 'eye'} size="sm" />
                    </button>
                  )}
                </div>
              ) : (
                <span className="text-sm surface-muted">-</span>
              )
            }
            if (key === 'location') {
              return (
                <div className="flex items-center gap-2">
                  {row.country_code && (
                    <img
                      src={flagUrl(row.country_code)}
                      alt={row.country || row.country_code}
                      className="h-4 w-6 rounded-sm"
                    />
                  )}
                  {formatLocation(row) ? (
                    <span className="text-sm surface-muted">{formatLocation(row)}</span>
                  ) : (
                    <span className="text-sm surface-muted">-</span>
                  )}
                </div>
              )
            }
            if (key === 'account_count') {
              const count = (value as number) || 0
              return count > 0 ? (
                <button
                  type="button"
                  className="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium text-primary-700 hover:surface-hover dark:text-primary-300 dark:hover:bg-dark-500 surface-control"
                  onClick={() => void openAccountsModal(row)}
                >
                  {t('admin.groups.accountsCount', { count })}
                </button>
              ) : (
                <span className="inline-flex items-center rounded surface-inset px-2 py-0.5 text-xs font-medium text-gray-800 dark:text-gray-300">
                  {t('admin.groups.accountsCount', { count: 0 })}
                </span>
              )
            }
            if (key === 'latency') {
              return (
                <div className="flex flex-col gap-1">
                  {row.latency_status === 'failed' ? (
                    <span className="badge badge-danger" title={row.latency_message || undefined}>
                      {t('admin.proxies.latencyFailed')}
                    </span>
                  ) : typeof row.latency_ms === 'number' ? (
                    <span className={cn('badge', row.latency_ms < 200 ? 'badge-success' : 'badge-warning')}>
                      {row.latency_ms}ms
                    </span>
                  ) : (
                    <span className="text-sm surface-muted">-</span>
                  )}
                  {typeof row.quality_checked === 'number' && (
                    <div className="flex items-center gap-1 text-xs surface-muted" title={row.quality_summary || undefined}>
                      <span>
                        {t('admin.proxies.qualityInline', { grade: row.quality_grade || '-', score: row.quality_score ?? '-' })}
                      </span>
                      <span className={cn('badge', qualityOverallClass(row.quality_status))}>
                        {qualityOverallLabel(row.quality_status)}
                      </span>
                    </div>
                  )}
                </div>
              )
            }
            if (key === 'expiry') {
              return !row.expires_at ? (
                <span className="text-sm surface-muted">{t('admin.proxies.neverExpires')}</span>
              ) : (
                <div className="flex flex-col text-xs">
                  <span className="surface-muted">{formatDateTime(row.expires_at)}</span>
                  <span className={proxyExpiryBadgeClass(row.expires_at, row.status)}>{expiryLabel(row)}</span>
                </div>
              )
            }
            if (key === 'created_at') {
              return <span className="text-xs surface-muted">{formatDateTime(row.created_at)}</span>
            }
            if (key === 'status') {
              return (
                <span
                  className={cn(
                    'badge',
                    value === 'active' ? 'badge-success' : value === 'expired' ? 'badge-danger' : 'badge-danger'
                  )}
                >
                  {t('admin.accounts.status.' + String(value ?? ''))}
                </span>
              )
            }
            if (key === 'actions') {
              return (
                <div className="table-actions">
                  <button
                    type="button"
                    onClick={() => void handleTestConnection(row)}
                    disabled={testingProxyIds.includes(row.id)}
                    className="table-action"
                  >
                    {testingProxyIds.includes(row.id) ? (
                      <svg className="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24" aria-hidden="true">
                        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                        <path
                          className="opacity-75"
                          fill="currentColor"
                          d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                        />
                      </svg>
                    ) : (
                      <Icon name="checkCircle" size="sm" />
                    )}
                    <span className="text-xs">{t('admin.proxies.testConnection')}</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => void handleQualityCheck(row)}
                    disabled={qualityCheckingProxyIds.includes(row.id)}
                    className="table-action"
                  >
                    {qualityCheckingProxyIds.includes(row.id) ? (
                      <svg className="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24" aria-hidden="true">
                        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                        <path
                          className="opacity-75"
                          fill="currentColor"
                          d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                        />
                      </svg>
                    ) : (
                      <Icon name="shield" size="sm" />
                    )}
                    <span className="text-xs">{t('admin.proxies.qualityCheck')}</span>
                  </button>
                  <button type="button" onClick={() => handleEdit(row)} className="table-action">
                    <Icon name="edit" size="sm" />
                    <span className="text-xs">{t('common.edit')}</span>
                  </button>
                  <button type="button" onClick={() => handleDelete(row)} className="table-action table-action-danger">
                    <Icon name="trash" size="sm" />
                    <span className="text-xs">{t('common.delete')}</span>
                  </button>
                </div>
              )
            }
            return <span>{value == null ? '' : String(value)}</span>
          }}
          empty={
            <EmptyState
              title={t('admin.proxies.noProxiesYet')}
              description={t('admin.proxies.createFirstProxy')}
              action={
                <Button variant="primary" onClick={() => setShowCreateModal(true)}>
                  {t('admin.proxies.createProxy')}
                </Button>
              }
            />
          }
        />
      }
      pagination={
        total > 0 ? (
          <Pagination
            page={page}
            total={total}
            pageSize={pageSize}
            onPageChange={handlePageChange}
            onPageSizeChange={handlePageSizeChange}
          />
        ) : undefined
      }
      />
      <ProxyDialog
        open={showCreateModal}
        title={t('admin.proxies.createProxy')}
        onClose={closeCreateModal}
        footer={
          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={closeCreateModal}>
              {t('common.cancel')}
            </Button>
            {createMode === 'standard' ? (
              <Button type="submit" form="create-proxy-form" variant="primary" disabled={submitting}>
                {submitting ? t('admin.proxies.creating') : t('common.create')}
              </Button>
            ) : (
              <Button
                onClick={() => void handleBatchCreate()}
                variant="primary"
                disabled={submitting || batchParseResult.valid === 0}
              >
                {submitting ? t('admin.proxies.importing') : t('admin.proxies.importProxies', { count: batchParseResult.valid })}
              </Button>
            )}
          </div>
        }
      >
        <div className="mb-6 flex items-center justify-between gap-3">
          <div className="card-tabs" role="tablist" aria-label={t('admin.proxies.createProxy')}>
            <button
              type="button"
              role="tab"
              className="card-tab"
              aria-selected={createMode === 'standard'}
              onClick={() => setCreateMode('standard')}
            >
              <Icon name="plus" size="sm" className="mr-1.5 inline" />
              {t('admin.proxies.standardAdd')}
            </button>
            <button
              type="button"
              role="tab"
              className="card-tab"
              aria-selected={createMode === 'batch'}
              onClick={() => setCreateMode('batch')}
            >
              <svg className="mr-1.5 inline h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="1.5" aria-hidden="true">
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M3.75 12h16.5m-16.5 3.75h16.5M3.75 19.5h16.5M5.625 4.5h12.75a1.875 1.875 0 010 3.75H5.625a1.875 1.875 0 010-3.75z"
                />
              </svg>
              {t('admin.proxies.batchAdd')}
            </button>
          </div>
        </div>

        {createMode === 'standard' ? (
          <form
            id="create-proxy-form"
            onSubmit={(event) => {
              event.preventDefault()
              void handleCreateProxy()
            }}
            className="space-y-5"
          >
            <Input
              label={t('admin.proxies.name')}
              value={createForm.name}
              placeholder={t('admin.proxies.enterProxyName')}
              onValueChange={(next) => setCreateForm((previous) => ({ ...previous, name: next }))}
            />
            <div>
              <label className="input-label">{t('admin.proxies.protocol')}</label>
              <Select
                value={createForm.protocol}
                options={protocolSelectOptions}
                onValueChange={(next) =>
                  setCreateForm((previous) => ({ ...previous, protocol: String(next) as ProxyProtocol }))
                }
              />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <Input
                label={t('admin.proxies.host')}
                value={createForm.host}
                placeholder={t('admin.proxies.form.hostPlaceholder')}
                onValueChange={(next) => setCreateForm((previous) => ({ ...previous, host: next }))}
              />
              <Input
                label={t('admin.proxies.port')}
                type="number"
                min="1"
                max="65535"
                value={createForm.port}
                placeholder={t('admin.proxies.form.portPlaceholder')}
                onValueChange={(next) => setCreateForm((previous) => ({ ...previous, port: next }))}
              />
            </div>
            <Input
              label={t('admin.proxies.username')}
              value={createForm.username}
              placeholder={t('admin.proxies.optionalAuth')}
              onValueChange={(next) => setCreateForm((previous) => ({ ...previous, username: next }))}
            />
            <Input
              label={t('admin.proxies.password')}
              type={createPasswordVisible ? 'text' : 'password'}
              value={createForm.password}
              placeholder={t('admin.proxies.optionalAuth')}
              onValueChange={(next) => setCreateForm((previous) => ({ ...previous, password: next }))}
              suffix={
                <button
                  type="button"
                  className="hover:text-gray-600 dark:hover:text-gray-300 surface-control surface-muted"
                  onClick={() => setCreatePasswordVisible((previous) => !previous)}
                >
                  <Icon name={createPasswordVisible ? 'eyeOff' : 'eye'} size="md" />
                </button>
              }
            />
            <div>
              <label className="input-label">{t('admin.proxies.expiresAt')}</label>
              <div className="mb-2 flex flex-wrap gap-2">
                {EXPIRY_PRESETS.map((days) => (
                  <button
                    key={days}
                    type="button"
                    className={cn('btn btn-sm', createForm.expires_at === addDaysToBase('', days) ? 'btn-primary' : 'btn-secondary')}
                    onClick={() => applyCreateExpiresDays(days)}
                  >
                    {t('admin.proxies.nDays', { days })}
                  </button>
                ))}
              </div>
              <input
                value={createExpiresDays === null ? '' : String(createExpiresDays)}
                type="number"
                min="0"
                className="input mb-2"
                placeholder={t('admin.proxies.expiryDaysPlaceholder')}
                onChange={(event) => {
                  const raw = event.target.value
                  applyCreateExpiresDays(raw === '' ? null : Number(raw))
                }}
              />
              <DateTimePicker
                ariaLabel={t('admin.proxies.expiresAt')}
                value={createForm.expires_at}
                type="date"
                max="9999-12-31"
                className="input"
                onValueChange={(next) => {
                  setCreateForm((previous) => ({ ...previous, expires_at: next }))
                }}
              />
            </div>
            <div>
              <label className="input-label">{t('admin.proxies.fallbackMode')}</label>
              <Select
                value={createForm.fallback_mode}
                options={fallbackModeOptions}
                onValueChange={(next) =>
                  setCreateForm((previous) => ({ ...previous, fallback_mode: String(next) as ProxyFormState['fallback_mode'] }))
                }
              />
            </div>
            {createForm.fallback_mode === 'proxy' && (
              <div>
                <label className="input-label">{t('admin.proxies.backupProxy')}</label>
                <Select
                  value={createForm.backup_proxy_id}
                  options={backupProxyOptions()}
                  onValueChange={(next) =>
                    setCreateForm((previous) => ({ ...previous, backup_proxy_id: next === null ? null : Number(next) }))
                  }
                />
              </div>
            )}
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="input-label">{t('admin.proxies.egressTimezone')}</label>
                <Select
                  value={createForm.egress_timezone}
                  options={egressTimezoneOptions}
                  searchable
                  onValueChange={(next) =>
                    setCreateForm((previous) => ({ ...previous, egress_timezone: String(next ?? '') }))
                  }
                />
                <p className="input-hint mt-1">{t('admin.proxies.egressTimezoneHint')}</p>
              </div>
              <div>
                <label className="input-label">{t('admin.proxies.egressCountry')}</label>
                <Select
                  value={createForm.egress_country}
                  options={egressCountryOptions}
                  searchable
                  onValueChange={(next) =>
                    setCreateForm((previous) => ({ ...previous, egress_country: String(next ?? '') }))
                  }
                />
                <p className="input-hint mt-1">{t('admin.proxies.egressCountryHint')}</p>
              </div>
            </div>
          </form>
        ) : (
          <div className="space-y-5">
            <div>
              <label className="input-label">{t('admin.proxies.batchInput')}</label>
              <TextArea
                value={batchInput}
                rows={10}
                className="font-mono text-sm"
                placeholder={t('admin.proxies.batchInputPlaceholder')}
                onValueChange={(next) => {
                  setBatchInput(next)
                  parseBatchInput(next)
                }}
              />
              <p className="input-hint mt-2">{t('admin.proxies.batchInputHint')}</p>
            </div>

            {batchParseResult.total > 0 && (
              <div className="rounded-lg surface-inset p-4">
                <div className="flex items-center gap-4 text-sm">
                  <div className="flex items-center gap-1.5">
                    <Icon name="checkCircle" size="sm" strokeWidth={2} className="text-primary-500" />
                    <span className="surface-muted">{t('admin.proxies.parsedCount', { count: batchParseResult.valid })}</span>
                  </div>
                  {batchParseResult.invalid > 0 && (
                    <div className="flex items-center gap-1.5">
                      <Icon name="exclamationCircle" size="sm" strokeWidth={2} className="text-amber-500" />
                      <span className="text-amber-600 dark:text-amber-400">
                        {t('admin.proxies.invalidCount', { count: batchParseResult.invalid })}
                      </span>
                    </div>
                  )}
                  {batchParseResult.duplicate > 0 && (
                    <div className="flex items-center gap-1.5">
                      <svg className="h-4 w-4 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2" aria-hidden="true">
                        <path
                          strokeLinecap="round"
                          strokeLinejoin="round"
                          d="M15.75 17.25v3.375c0 .621-.504 1.125-1.125 1.125h-9.75a1.125 1.125 0 01-1.125-1.125V7.875c0-.621.504 1.125 1.125 1.125H6.75a9.06 9.06 0 011.5.124m7.5 10.376h3.375c.621 0 1.125-.504 1.125-1.125V11.25c0-4.46-3.243-8.161-7.5-8.876a9.06 9.06 0 00-1.5-.124H9.375c-.621 0-1.125-.504-1.125-1.125v-3.5m7.5 10.375H9.375a1.125 1.125 0 01-1.125-1.125v-9.25m12 6.625v-1.875a3.375 3.375 0 00-3.375-3.375h-1.5a1.125 1.125 0 01-1.125-1.125v-1.5a3.375 3.375 0 00-3.375-3.375H9.75"
                        />
                      </svg>
                      <span className="surface-muted">{t('admin.proxies.duplicateCount', { count: batchParseResult.duplicate })}</span>
                    </div>
                  )}
                </div>
              </div>
            )}
          </div>
        )}
      </ProxyDialog>

      <ProxyDialog
        open={showEditModal}
        title={t('admin.proxies.editProxy')}
        onClose={closeEditModal}
        footer={
          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={closeEditModal}>
              {t('common.cancel')}
            </Button>
            {editingProxy && (
              <Button type="submit" form="edit-proxy-form" variant="primary" disabled={submitting}>
                {submitting ? t('admin.proxies.updating') : t('common.update')}
              </Button>
            )}
          </div>
        }
      >
        {editingProxy && (
          <form
            id="edit-proxy-form"
            onSubmit={(event) => {
              event.preventDefault()
              void handleUpdateProxy()
            }}
            className="space-y-5"
          >
            <Input
              label={t('admin.proxies.name')}
              value={editForm.name}
              onValueChange={(next) => setEditForm((previous) => ({ ...previous, name: next }))}
            />
            <div>
              <label className="input-label">{t('admin.proxies.protocol')}</label>
              <Select
                value={editForm.protocol}
                options={protocolSelectOptions}
                onValueChange={(next) =>
                  setEditForm((previous) => ({ ...previous, protocol: String(next) as ProxyProtocol }))
                }
              />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <Input
                label={t('admin.proxies.host')}
                value={editForm.host}
                onValueChange={(next) => setEditForm((previous) => ({ ...previous, host: next }))}
              />
              <Input
                label={t('admin.proxies.port')}
                type="number"
                min="1"
                max="65535"
                value={editForm.port}
                onValueChange={(next) => setEditForm((previous) => ({ ...previous, port: next }))}
              />
            </div>
            <Input
              label={t('admin.proxies.username')}
              value={editForm.username}
              onValueChange={(next) => setEditForm((previous) => ({ ...previous, username: next }))}
            />
            <Input
              label={t('admin.proxies.password')}
              type={editPasswordVisible ? 'text' : 'password'}
              value={editForm.password}
              placeholder={t('admin.proxies.leaveEmptyToKeep')}
              onValueChange={(next) => {
                setEditPasswordDirty(true)
                setEditForm((previous) => ({ ...previous, password: next }))
              }}
              suffix={
                <button
                  type="button"
                  className="hover:text-gray-600 dark:hover:text-gray-300 surface-control surface-muted"
                  onClick={() => setEditPasswordVisible((previous) => !previous)}
                >
                  <Icon name={editPasswordVisible ? 'eyeOff' : 'eye'} size="md" />
                </button>
              }
            />
            <div>
              <label className="input-label">{t('admin.proxies.status')}</label>
              <Select
                value={editForm.status}
                options={editStatusOptions}
                onValueChange={(next) =>
                  setEditForm((previous) => ({ ...previous, status: String(next) as ProxyFormState['status'] }))
                }
              />
            </div>
            <div>
              <label className="input-label">{t('admin.proxies.expiresAt')}</label>
              <div className="mb-2 flex flex-wrap gap-2">
                {EXPIRY_PRESETS.map((days) => (
                  <button
                    key={days}
                    type="button"
                    className={cn('btn btn-sm', editForm.expires_at === addDaysToBase(editBaseDate, days) ? 'btn-primary' : 'btn-secondary')}
                    onClick={() => applyEditExpiresDays(days)}
                  >
                    {t('admin.proxies.nDays', { days })}
                  </button>
                ))}
              </div>
              <input
                value={editExpiresDays === null ? '' : String(editExpiresDays)}
                type="number"
                min="0"
                className="input mb-2"
                placeholder={t('admin.proxies.expiryDaysPlaceholder')}
                onChange={(event) => {
                  const raw = event.target.value
                  applyEditExpiresDays(raw === '' ? null : Number(raw))
                }}
              />
              <DateTimePicker
                ariaLabel={t('admin.proxies.expiresAt')}
                value={editForm.expires_at}
                type="date"
                max="9999-12-31"
                className="input"
                onValueChange={(next) => {
                  setEditForm((previous) => ({ ...previous, expires_at: next }))
                }}
              />
            </div>
            <div>
              <label className="input-label">{t('admin.proxies.fallbackMode')}</label>
              <Select
                value={editForm.fallback_mode}
                options={fallbackModeOptions}
                onValueChange={(next) =>
                  setEditForm((previous) => ({ ...previous, fallback_mode: String(next) as ProxyFormState['fallback_mode'] }))
                }
              />
            </div>
            {editForm.fallback_mode === 'proxy' && (
              <div>
                <label className="input-label">{t('admin.proxies.backupProxy')}</label>
                <Select
                  value={editForm.backup_proxy_id}
                  options={backupProxyOptions(editingProxy.id)}
                  onValueChange={(next) =>
                    setEditForm((previous) => ({ ...previous, backup_proxy_id: next === null ? null : Number(next) }))
                  }
                />
              </div>
            )}
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="input-label">{t('admin.proxies.egressTimezone')}</label>
                <Select
                  value={editForm.egress_timezone}
                  options={egressTimezoneOptions}
                  searchable
                  onValueChange={(next) =>
                    setEditForm((previous) => ({ ...previous, egress_timezone: String(next ?? '') }))
                  }
                />
                <p className="input-hint mt-1">{t('admin.proxies.egressTimezoneHint')}</p>
              </div>
              <div>
                <label className="input-label">{t('admin.proxies.egressCountry')}</label>
                <Select
                  value={editForm.egress_country}
                  options={egressCountryOptions}
                  searchable
                  onValueChange={(next) =>
                    setEditForm((previous) => ({ ...previous, egress_country: String(next ?? '') }))
                  }
                />
                <p className="input-hint mt-1">{t('admin.proxies.egressCountryHint')}</p>
              </div>
            </div>
          </form>
        )}
      </ProxyDialog>

      <ProxyConfirmDialog
        open={showDeleteDialog}
        title={t('admin.proxies.deleteProxy')}
        message={t('admin.proxies.deleteConfirm', { name: deletingProxy?.name })}
        confirmText={t('common.delete')}
        cancelText={t('common.cancel')}
        danger
        onConfirm={() => void confirmDelete()}
        onCancel={() => setShowDeleteDialog(false)}
      />

      <ProxyConfirmDialog
        open={showBatchDeleteDialog}
        title={t('admin.proxies.batchDelete')}
        message={t('admin.proxies.batchDeleteConfirm', { count: selectedCount })}
        confirmText={t('common.delete')}
        cancelText={t('common.cancel')}
        danger
        onConfirm={() => void confirmBatchDelete()}
        onCancel={() => setShowBatchDeleteDialog(false)}
      />

      <ProxyConfirmDialog
        open={showExportDataDialog}
        title={t('admin.proxies.dataExport')}
        message={t('admin.proxies.dataExportConfirmMessage')}
        confirmText={t('admin.proxies.dataExportConfirm')}
        cancelText={t('common.cancel')}
        onConfirm={() => void handleExportData()}
        onCancel={() => setShowExportDataDialog(false)}
      />

      <ImportDataModal show={showImportData} onClose={() => setShowImportData(false)} onImported={handleDataImported} />

      <ProxyDialog
        open={showQualityReportDialog}
        title={t('admin.proxies.qualityReportTitle')}
        onClose={closeQualityReportDialog}
        footer={
          <div className="flex justify-end">
            <Button variant="secondary" onClick={closeQualityReportDialog}>
              {t('common.close')}
            </Button>
          </div>
        }
      >
        {qualityReport && (
          <div className="space-y-4">
            <div className="rounded-lg border border-gray-200 surface-inset p-4 dark:border-dark-600">
              <div className="flex items-center justify-between gap-4">
                <div>
                  <div className="text-sm surface-muted">{qualityReportProxy?.name || '-'}</div>
                  <div className="mt-1 text-sm surface-muted">{qualityReport.summary}</div>
                </div>
                <div className="text-right">
                  <div className="text-2xl font-semibold text-gray-900 dark:text-white">{qualityReport.score}</div>
                  <div className="text-xs surface-muted">{t('admin.proxies.qualityGrade', { grade: qualityReport.grade })}</div>
                </div>
              </div>
              <div className="mt-3 grid grid-cols-2 gap-2 text-xs surface-muted">
                <div>
                  {t('admin.proxies.qualityExitIP')}: {qualityReport.exit_ip || '-'}
                </div>
                <div>
                  {t('admin.proxies.qualityCountry')}: {qualityReport.country || '-'}
                </div>
                <div>
                  {t('admin.proxies.qualityBaseLatency')}:{' '}
                  {typeof qualityReport.base_latency_ms === 'number' ? `${qualityReport.base_latency_ms}ms` : '-'}
                </div>
                <div>
                  {t('admin.proxies.qualityCheckedAt')}: {new Date(qualityReport.checked_at * 1000).toLocaleString()}
                </div>
              </div>
            </div>

            <div className="record-table-scroll max-h-80 overflow-auto rounded-lg border border-gray-200 dark:border-dark-600">
              <table className="record-table min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
                <thead className="surface-inset text-xs uppercase surface-muted">
                  <tr>
                    <th className="whitespace-nowrap px-3 py-2 text-left">{t('admin.proxies.qualityTableTarget')}</th>
                    <th className="whitespace-nowrap px-3 py-2 text-left">{t('admin.proxies.qualityTableStatus')}</th>
                    <th className="whitespace-nowrap px-3 py-2 text-left">HTTP</th>
                    <th className="whitespace-nowrap px-3 py-2 text-left">{t('admin.proxies.qualityTableLatency')}</th>
                    <th className="px-3 py-2 text-left">{t('admin.proxies.qualityTableMessage')}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-200 surface-inset dark:divide-dark-700">
                  {qualityReport.items.map((item) => (
                    <tr key={item.target}>
                      <td className="whitespace-nowrap px-3 py-2 text-gray-900 dark:text-white">{qualityTargetLabel(item.target)}</td>
                      <td className="whitespace-nowrap px-3 py-2">
                        <span className={cn('badge whitespace-nowrap', qualityStatusClass(item.status))}>
                          {qualityStatusLabel(item.status)}
                        </span>
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 surface-muted">{item.http_status ?? '-'}</td>
                      <td className="whitespace-nowrap px-3 py-2 surface-muted">
                        {typeof item.latency_ms === 'number' ? `${item.latency_ms}ms` : '-'}
                      </td>
                      <td className="px-3 py-2 surface-muted">
                        <span>{item.message || '-'}</span>
                        {item.cf_ray && <span className="ml-1 text-xs surface-muted">(cf-ray: {item.cf_ray})</span>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </ProxyDialog>

      <ProxyDialog
        open={showAccountsModal}
        title={t('admin.proxies.accountsTitle', { name: accountsProxy?.name || '' })}
        onClose={closeAccountsModal}
        footer={
          <div className="flex justify-end">
            <Button variant="secondary" onClick={closeAccountsModal}>
              {t('common.close')}
            </Button>
          </div>
        }
      >
        {accountsLoading ? (
          <div className="flex items-center justify-center py-8 text-sm surface-muted">
            <Icon name="refresh" size="md" className="mr-2 animate-spin" />
            {t('common.loading')}
          </div>
        ) : proxyAccounts.length === 0 ? (
          <div className="py-6 text-center text-sm surface-muted">{t('admin.proxies.accountsEmpty')}</div>
        ) : (
          <div className="record-table-scroll table-container max-h-80 overflow-auto">
            <table className="record-table record-table-compact min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
              <thead className="surface-inset text-xs uppercase surface-muted">
                <tr>
                  <th className="px-4 py-2 text-left">{t('admin.proxies.accountName')}</th>
                  <th className="px-4 py-2 text-left">{t('admin.accounts.columns.platformType')}</th>
                  <th className="px-4 py-2 text-left">{t('admin.proxies.accountNotes')}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200 surface-inset dark:divide-dark-700">
                {proxyAccounts.map((account) => (
                  <tr key={account.id}>
                    <td className="px-4 py-2 font-medium text-gray-900 dark:text-white">{account.name}</td>
                    <td className="px-4 py-2">
                      <PlatformTypeBadge platform={account.platform} type={account.type} />
                    </td>
                    <td className="px-4 py-2 surface-muted">{account.notes || '-'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </ProxyDialog>
    </>
  )
}

