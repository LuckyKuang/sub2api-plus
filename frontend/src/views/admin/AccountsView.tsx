import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { createPortal } from 'react-dom'
import { cn } from '@/lib/utils'
import { adminAPI } from '@/api'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { CONCRETE_PLATFORM_OPTIONS } from './platformCatalog'
import { fetchAllAccountIds } from '@/utils/accountSelection'
import { buildGrokUsageRefreshKey, buildOpenAIUsageRefreshKey } from '@/utils/accountUsageRefresh'
import { formatDateTime, formatRelativeTime } from '@/utils/format'
import { proxyExpiryBadgeClass, proxyExpiryLabelKey } from '@/utils/proxyExpiry'
import { extractApiErrorMessage } from '@/utils/apiError'
import { sanitizeUrl } from '@/utils/url'
import { getFloatingPanelPosition } from '@/utils/floatingPanel'
import { formatMultiplier } from '@/utils/formatters'
import { Checkbox, Modal, Pagination } from '@/components/ui'
import type {
  Account,
  AccountSchedulerGroupScore,
  AccountUsageInfo,
  Proxy as AccountProxy,
  AdminGroup,
  WindowStats,
  ClaudeModel
} from '@/types'
import { AdminDataTable, type Column, type SortOrder } from './AdminDataTable'
import TablePageLayout from '@/components/layout/TablePageLayout'
import { ConfirmDialog, useMediaQuery } from './shared'
import { Icon } from './AccountIcon'
import { AccountTableFilters } from './AccountTableFilters'
import { AccountTableActions } from './AccountTableActions'
import { AccountBulkActionsBar } from './AccountBulkActionsBar'
import { AccountActionMenu } from './AccountActionMenu'
import { AccountCapacityCell } from './AccountCapacityCell'
import { AccountStatusIndicator } from './AccountStatusIndicator'
import { AccountTodayStatsCell } from './AccountTodayStatsCell'
import { AccountGroupsCell } from './AccountGroupsCell'
import { AccountPriorityCell } from './AccountPriorityCell'
import { AccountUsageCell } from './AccountUsageCell'
import { PlatformTypeBadge } from './PlatformTypeBadge'
import { SyncFromCrsModal } from './SyncFromCrsModal'
import { TempUnschedStatusModal } from './TempUnschedStatusModal'
import { ImportDataModal } from './ImportDataModal'
import { ReAuthAccountModal } from './ReAuthAccountModal'
import { AccountTestModal } from './AccountTestModal'
import { AccountStatsModal } from './AccountStatsModal'
import { ScheduledTestsPanel } from './ScheduledTestsPanel'
import { UsageAlertPanel } from './UsageAlertPanel'
import { TriggerTooltip } from './TriggerTooltip'

type AccountSortOrder = 'asc' | 'desc'

const DEFAULT_HIDDEN_COLUMNS = ['today_stats', 'proxy', 'notes', 'scheduler_score', 'rate_multiplier']
const HIDDEN_COLUMNS_KEY = 'account-hidden-columns'
// One-time migration: hide scheduler score for existing admins too, because showing it opt-ins to heavy backend scoring.
const HIDDEN_COLUMNS_VERSION_KEY = 'account-hidden-columns-version'
const HIDDEN_COLUMNS_CURRENT_VERSION = 'scheduler-score-hidden-by-default'

const ACCOUNT_FILTER_STORAGE_KEY = 'account-filter-values'
const ACCOUNT_SORT_STORAGE_KEY = 'account-table-sort'
const AUTO_REFRESH_STORAGE_KEY = 'account-auto-refresh'
const autoRefreshIntervals = [5, 10, 15, 30] as const

const ACCOUNT_SORTABLE_KEYS = new Set([
  'id',
  'name',
  'status',
  'schedulable',
  'priority',
  'rate_multiplier',
  'last_used_at',
  'created_at',
  'expires_at'
])

const ACCOUNT_UNGROUPED_GROUP_QUERY_VALUE = 'ungrouped'
const ACCOUNT_PRIVACY_MODE_UNSET_QUERY_VALUE = '__unset__'

const USAGE_BATCH_CACHE_TTL = 5 * 60 * 1000
const AUTO_REFRESH_SILENT_WINDOW_MS = 15000

type AccountFilterValues = {
  platform: string
  status: string
}

type AccountSortState = {
  sort_by: string
  sort_order: AccountSortOrder
}

const loadInitialAccountFilterValues = (): AccountFilterValues => {
  const fallback: AccountFilterValues = { platform: '', status: '' }
  try {
    const raw = localStorage.getItem(ACCOUNT_FILTER_STORAGE_KEY)
    if (!raw) return fallback
    const parsed: unknown = JSON.parse(raw)
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return fallback
    const values = parsed as Record<string, unknown>
    return {
      platform:
        typeof values.platform === 'string' &&
        CONCRETE_PLATFORM_OPTIONS.some((option) => option.value === values.platform)
          ? values.platform
          : '',
      status:
        typeof values.status === 'string' &&
        ['active', 'inactive', 'error', 'rate_limited', 'temp_unschedulable', 'unschedulable'].includes(values.status)
          ? values.status
          : ''
    }
  } catch {
    return fallback
  }
}

const loadInitialAccountSortState = (): AccountSortState => {
  const fallback: AccountSortState = { sort_by: 'name', sort_order: 'asc' }
  try {
    const raw = localStorage.getItem(ACCOUNT_SORT_STORAGE_KEY)
    if (!raw) return fallback
    const parsed = JSON.parse(raw) as { key?: string; order?: string }
    const key = typeof parsed.key === 'string' ? parsed.key : ''
    if (!ACCOUNT_SORTABLE_KEYS.has(key)) return fallback
    return {
      sort_by: key,
      sort_order: parsed.order === 'desc' ? 'desc' : 'asc'
    }
  } catch {
    return fallback
  }
}

const loadSavedAutoRefresh = (): { enabled: boolean; interval_seconds: number } => {
  try {
    const saved = localStorage.getItem(AUTO_REFRESH_STORAGE_KEY)
    if (!saved) return { enabled: false, interval_seconds: 30 }
    const parsed = JSON.parse(saved) as { enabled?: boolean; interval_seconds?: number }
    const interval = Number(parsed.interval_seconds)
    return {
      enabled: parsed.enabled === true,
      interval_seconds: (autoRefreshIntervals as readonly number[]).includes(interval) ? interval : 30
    }
  } catch (e) {
    console.error('Failed to load saved auto refresh settings:', e)
    return { enabled: false, interval_seconds: 30 }
  }
}

const loadSavedColumns = (): Set<string> => {
  const hidden = new Set<string>()
  try {
    const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
    if (saved) {
      const parsed = JSON.parse(saved) as string[]
      parsed.forEach((key) => hidden.add(key))
      // Older saved column layouts may have scheduler_score visible; migrate them to the new safe default once.
      if (localStorage.getItem(HIDDEN_COLUMNS_VERSION_KEY) !== HIDDEN_COLUMNS_CURRENT_VERSION) {
        hidden.add('scheduler_score')
        localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hidden]))
        localStorage.setItem(HIDDEN_COLUMNS_VERSION_KEY, HIDDEN_COLUMNS_CURRENT_VERSION)
      }
    } else {
      DEFAULT_HIDDEN_COLUMNS.forEach((key) => hidden.add(key))
      localStorage.setItem(HIDDEN_COLUMNS_VERSION_KEY, HIDDEN_COLUMNS_CURRENT_VERSION)
    }
  } catch (e) {
    console.error('Failed to load saved columns:', e)
    DEFAULT_HIDDEN_COLUMNS.forEach((key) => hidden.add(key))
  }
  return hidden
}

const saveColumnsToStorage = (hidden: Set<string>) => {
  try {
    localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hidden]))
    localStorage.setItem(HIDDEN_COLUMNS_VERSION_KEY, HIDDEN_COLUMNS_CURRENT_VERSION)
  } catch (e) {
    console.error('Failed to save columns:', e)
  }
}

const saveAutoRefreshToStorage = (enabled: boolean, intervalSeconds: number) => {
  try {
    localStorage.setItem(AUTO_REFRESH_STORAGE_KEY, JSON.stringify({ enabled, interval_seconds: intervalSeconds }))
  } catch (e) {
    console.error('Failed to save auto refresh settings:', e)
  }
}

const buildDefaultTodayStats = (): WindowStats => ({
  requests: 0,
  tokens: 0,
  cost: 0,
  standard_cost: 0,
  user_cost: 0
})

const accountSupportsBatchUsage = (account: Account) => {
  if (account.platform === 'anthropic') {
    return account.type === 'oauth' || account.type === 'setup-token'
  }
  if (account.platform === 'gemini') return true
  if (account.platform === 'antigravity') return account.type === 'oauth'
  if (account.platform === 'openai') return account.type === 'oauth'
  if (account.platform === 'grok') return account.type === 'oauth'
  return false
}

const formatSchedulerScore = (value: unknown): string => {
  const num = Number(value)
  if (!Number.isFinite(num)) return '-'
  return num.toFixed(6).replace(/\.?0+$/, '')
}

const formatStickySchedulerScore = (score: AccountSchedulerGroupScore): string => {
  if (!score) return '-'
  if (score.sticky_score_infinity) return '+∞'
  return formatSchedulerScore(score.sticky_score)
}

const getSchedulerScoreRows = (account: Account): AccountSchedulerGroupScore[] => {
  const groupRows = Array.isArray(account.scheduler_scores)
    ? account.scheduler_scores.filter((score) => score.group_id != null)
    : []
  if (groupRows.length) return groupRows
  // 未分组账号没有分组维度分数，回退展示后端返回的基础分
  if (account.scheduler_score) {
    return [{ group_id: null, ...account.scheduler_score }]
  }
  return []
}

function firstNonBlankString(...values: unknown[]): string | undefined {
  return values.find((value): value is string => typeof value === 'string' && value.trim().length > 0)
}

function normalizeGrokPlanKey(value: unknown): string {
  if (typeof value !== 'string') return ''
  return value
    .trim()
    .toLowerCase()
    .replace(/[\s_-]+/g, '')
}

function grokPersistedQuotaSnapshot(extra: Record<string, unknown>): Record<string, unknown> | undefined {
  const usage = extra.grok_usage_snapshot
  if (usage && typeof usage === 'object' && !Array.isArray(usage)) {
    return usage as Record<string, unknown>
  }
  const legacy = extra.grok_quota_snapshot
  if (legacy && typeof legacy === 'object' && !Array.isArray(legacy)) {
    return legacy as Record<string, unknown>
  }
  return undefined
}

function isGrokQuotaTimestampFresh(raw: unknown): boolean {
  const value = String(raw || '').trim()
  if (!value) return false
  const observedAt = Date.parse(value)
  if (!Number.isFinite(observedAt)) return false
  const age = Date.now() - observedAt
  return age <= 24 * 60 * 60 * 1000 && age >= -5 * 60 * 1000
}

function isGrok45ResponsesQuotaModel(model: unknown): boolean {
  const value = String(model || '')
    .trim()
    .toLowerCase()
    .replace(/^(x-ai|xai)\//, '')
  return value === 'grok-4.5' || value.startsWith('grok-4.5-')
}

function grokQuotaLooksHeavy(snapshot: Record<string, unknown> | undefined): boolean {
  const req = Number((snapshot?.requests as { limit?: unknown } | undefined)?.limit ?? 0)
  const tok = Number((snapshot?.tokens as { limit?: unknown } | undefined)?.limit ?? 0)
  return req >= 8300 || tok >= 53_000_000
}

function grok45ResponsesPlanIsHeavy(snapshot: Record<string, unknown> | undefined): boolean {
  if (!snapshot) return false
  const hint = normalizeGrokPlanKey(snapshot.plan_from_45_responses)
  if (hint === 'supergrokheavy' && isGrokQuotaTimestampFresh(snapshot.plan_from_45_responses_at)) {
    return true
  }
  const observedAt = snapshot.last_headers_seen_at || snapshot.updated_at
  return (
    isGrok45ResponsesQuotaModel(snapshot.model) && isGrokQuotaTimestampFresh(observedAt) && grokQuotaLooksHeavy(snapshot)
  )
}

// JWT / unambiguous credentials outrank snapshots. SuperGrokPro is ambiguous
// (covers SuperGrok and Heavy). 8300/53M only upgrades when the window came
// from grok-4.5 Responses (or a carried 4.5 hint).
function getAccountPlanType(row: Account): string | undefined {
  if (!row) return undefined
  if (row.platform === 'grok') {
    const extra = (row.extra || {}) as Record<string, unknown>
    const billing = extra.grok_billing_snapshot as Record<string, unknown> | undefined
    const usage = extra.grok_usage_snapshot as Record<string, unknown> | undefined
    const legacyQuota = extra.grok_quota_snapshot as Record<string, unknown> | undefined
    const quota = grokPersistedQuotaSnapshot(extra)
    const cred = firstNonBlankString((row.credentials as Record<string, unknown> | undefined)?.subscription_tier)
    const credKey = normalizeGrokPlanKey(cred)
    if (credKey && credKey !== 'supergrokpro') {
      return cred
    }
    if (
      grok45ResponsesPlanIsHeavy(quota) &&
      (credKey === 'supergrokpro' ||
        normalizeGrokPlanKey(billing?.plan) === 'supergrok' ||
        normalizeGrokPlanKey(billing?.plan) === 'supergrokpro')
    ) {
      return 'SuperGrok Heavy'
    }
    if (credKey === 'supergrokpro') {
      return firstNonBlankString(billing?.plan) || 'SuperGrok'
    }
    return firstNonBlankString(
      billing?.plan,
      usage?.subscription_tier,
      legacyQuota?.subscription_tier,
      extra.subscription_tier,
      (row.credentials as Record<string, unknown> | undefined)?.plan_type,
      row.parent_plan_type
    )
  }
  return firstNonBlankString(
    (row.credentials as Record<string, unknown> | undefined)?.plan_type,
    row.parent_plan_type
  )
}

function getOpenAIAuthMode(row: Account): string | undefined {
  if (!row || row.platform !== 'openai' || row.type !== 'oauth') return undefined
  const authMode = (row.credentials as Record<string, unknown> | undefined)?.auth_mode
  return typeof authMode === 'string' && authMode.trim() ? authMode : undefined
}

function getAntigravityTierFromRow(row: Account): string | null {
  if (row.platform !== 'antigravity') return null
  const extra = row.extra as Record<string, unknown> | undefined
  if (!extra) return null
  const lca = extra.load_code_assist as Record<string, unknown> | undefined
  if (!lca) return null
  const paid = lca.paidTier as Record<string, unknown> | undefined
  if (paid && typeof paid.id === 'string') return paid.id
  const current = lca.currentTier as Record<string, unknown> | undefined
  if (current && typeof current.id === 'string') return current.id
  return null
}

function getAntigravityTierLabelRow(row: Account): string | null {
  const tier = getAntigravityTierFromRow(row)
  switch (tier) {
    case 'free-tier':
      return 'admin.accounts.tier.free'
    case 'g1-pro-tier':
      return 'admin.accounts.tier.pro'
    case 'g1-ultra-tier':
      return 'admin.accounts.tier.ultra'
    default:
      return null
  }
}

function getAntigravityTierClassRow(row: Account): string {
  const tier = getAntigravityTierFromRow(row)
  switch (tier) {
    case 'free-tier':
      return 'surface-inset surface-muted dark:surface-inset'
    case 'g1-pro-tier':
      return 'bg-blue-100/[0.15] text-blue-600 dark:bg-blue-900/40 dark:text-blue-300'
    case 'g1-ultra-tier':
      return 'bg-purple-100/[0.15] text-purple-600 dark:bg-purple-900/40 dark:text-purple-300'
    default:
      return ''
  }
}

// 账号显示邮箱:优先账号自身(extra/credentials),影子账号回退母账号 parent_email。
function accountDisplayEmail(row: Account): string {
  const extra = row.extra as Record<string, unknown> | undefined
  const credentials = row.credentials as Record<string, unknown> | undefined
  return (
    (extra?.email_address as string) ||
    (extra?.email as string) ||
    (credentials?.email as string) ||
    row.parent_email ||
    ''
  )
}

function accountHomepageUrl(row: Account): string {
  const credentials = row.credentials as Record<string, unknown> | undefined
  if (row.type !== 'apikey' || typeof credentials?.base_url !== 'string') return ''
  const baseUrl = sanitizeUrl(credentials.base_url as string)
  return baseUrl ? new URL(baseUrl).origin : ''
}

type OpenAICompactBadgeState = 'active' | 'blocked' | 'auto'

function getOpenAICompactState(row: Account): OpenAICompactBadgeState | null {
  if (row.platform !== 'openai' || (row.type !== 'oauth' && row.type !== 'apikey')) return null
  const extra = row.extra as Record<string, unknown> | undefined
  const mode = typeof extra?.openai_compact_mode === 'string' ? extra.openai_compact_mode : 'auto'
  if (mode === 'force_on') return 'active'
  if (mode === 'force_off') return 'blocked'
  if (typeof extra?.openai_compact_supported === 'boolean') {
    return extra.openai_compact_supported ? 'active' : 'blocked'
  }
  return 'auto'
}

function getOpenAICompactMeta(row: Account): { label: string; className: string; dotClass: string } | null {
  const state = getOpenAICompactState(row)
  if (!state) return null
  switch (state) {
    case 'active':
      return {
        label: 'admin.accounts.openai.compactSupported',
        className: 'text-emerald-600 dark:text-emerald-300',
        dotClass: 'bg-emerald-500 shadow-[0_0_0_2px_rgba(16,185,129,0.14)]'
      }
    case 'blocked':
      return {
        label: 'admin.accounts.openai.compactUnsupported',
        className: 'text-rose-600 dark:text-rose-300',
        dotClass: 'bg-rose-500 shadow-[0_0_0_2px_rgba(244,63,94,0.14)]'
      }
    case 'auto':
      return {
        label: 'admin.accounts.openai.compactAuto',
        className: 'text-slate-500 dark:text-slate-400',
        dotClass: 'bg-slate-300 dark:bg-slate-500'
      }
  }
}

function getOpenAICompactTitleRow(row: Account): string {
  const extra = row.extra as Record<string, unknown> | undefined
  const checkedAt = typeof extra?.openai_compact_checked_at === 'string' ? extra.openai_compact_checked_at : ''
  const meta = getOpenAICompactMeta(row)
  const label = meta?.label || ''
  if (!checkedAt) return label
  return `${label} | admin.accounts.openai.compactLastChecked: ${formatDateTime(new Date(checkedAt))}`
}

function formatExpiresAt(value: number | null): string {
  if (!value) return '-'
  return formatDateTime(
    new Date(value * 1000),
    { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false },
    'sv-SE'
  )
}

const isExpired = (value: number | null) => {
  if (!value) return false
  return value * 1000 <= Date.now()
}

function formatSchedulerScoreGroup(score: AccountSchedulerGroupScore): string {
  if ('group_name' in score && score.group_name) return score.group_name
  if ('group_id' in score && score.group_id != null) return `#${score.group_id}`
  return 'admin.accounts.schedulerScore.ungrouped'
}

export default function AccountsView() {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)
  const showWarning = useAppStore((state) => state.showWarning)
  const isSimpleMode = useAuthStore((state) => state.isSimpleMode())
  const isDesktopViewport = useMediaQuery('(min-width: 768px)')

  const [groups, setGroups] = useState<AdminGroup[]>([])
  const [accountGroupCatalog, setAccountGroupCatalog] = useState<AdminGroup[]>([])

  const initialSort = useRef<AccountSortState>(loadInitialAccountSortState())
  const initialFilters = useRef<AccountFilterValues>(loadInitialAccountFilterValues())
  const [hiddenColumns, setHiddenColumns] = useState<Set<string>>(() => loadSavedColumns())

  const [params, setParams] = useState<Record<string, unknown>>(() => ({
    lite: '1',
    ...initialFilters.current,
    type: '',
    privacy_mode: '',
    group: '',
    search: '',
    include_scheduler_score: hiddenColumns.has('scheduler_score') ? '0' : '1',
    sort_by: initialSort.current.sort_by,
    sort_order: initialSort.current.sort_order
  }))
  const paramsRef = useRef(params)
  paramsRef.current = params

  const [accounts, setAccounts] = useState<Account[]>([])
  const [loading, setLoading] = useState(false)
  const [pagination, setPagination] = useState({ page: 1, page_size: 20, total: 0, pages: 0 })

  const [selectedSet, setSelectedSet] = useState<Set<number>>(new Set())
  const [selectingAllResults, setSelectingAllResults] = useState(false)
  const selectedAllResultIDs = useRef<Set<number> | null>(null)
  const selectionRequestVersion = useRef(0)

  // Modal state
  const [showSync, setShowSync] = useState(false)
  const [showImportData, setShowImportData] = useState(false)
  const [showExportDataDialog, setShowExportDataDialog] = useState(false)
  const [includeProxyOnExport, setIncludeProxyOnExport] = useState(true)
  const [exportPassword, setExportPassword] = useState('')
  const [exportTotpCode, setExportTotpCode] = useState('')
  const [exportStepUpRequired, setExportStepUpRequired] = useState(false)
  const [exportRequirementsLoading, setExportRequirementsLoading] = useState(false)
  const [showTempUnsched, setShowTempUnsched] = useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [showCreateShadowDialog, setShowCreateShadowDialog] = useState(false)
  const [showReAuth, setShowReAuth] = useState(false)
  const [showTest, setShowTest] = useState(false)
  const [showStats, setShowStats] = useState(false)
  const [showSchedulePanel, setShowSchedulePanel] = useState(false)
  const [showUsageAlertPanel, setShowUsageAlertPanel] = useState(false)
  const [tempUnschedAcc, setTempUnschedAcc] = useState<Account | null>(null)
  const [deletingAcc, setDeletingAcc] = useState<Account | null>(null)
  const [creatingShadowAcc, setCreatingShadowAcc] = useState<Account | null>(null)
  const [reAuthAcc, setReAuthAcc] = useState<Account | null>(null)
  const [testingAcc, setTestingAcc] = useState<Account | null>(null)
  const [statsAcc, setStatsAcc] = useState<Account | null>(null)
  const [scheduleAcc, setScheduleAcc] = useState<Account | null>(null)
  const [scheduleModelOptions, setScheduleModelOptions] = useState<Array<{ value: string; label: string }>>([])
  const [usageAlertAcc, setUsageAlertAcc] = useState<Account | null>(null)
  const [togglingSchedulable, setTogglingSchedulable] = useState<number | null>(null)
  const [menu, setMenu] = useState<{ show: boolean; acc: Account | null; anchorRect: DOMRect | null }>({
    show: false,
    acc: null,
    anchorRect: null
  })
  const [exportingData, setExportingData] = useState(false)

  // Account tools dropdown
  const [showAccountToolsDropdown, setShowAccountToolsDropdown] = useState(false)
  const accountToolsTriggerRef = useRef<HTMLButtonElement | null>(null)
  const accountToolsDropdownRef = useRef<HTMLDivElement | null>(null)
  const [accountToolsDropdownPosition, setAccountToolsDropdownPosition] = useState({
    top: null as number | null,
    bottom: null as number | null,
    left: 16,
    width: 320,
    maxHeight: 0
  })
  const [showAutoRefreshDropdown, setShowAutoRefreshDropdown] = useState(false)
  const autoRefreshDropdownRef = useRef<HTMLDivElement | null>(null)

  // Auto refresh
  const savedAutoRefresh = useRef(loadSavedAutoRefresh())
  const [autoRefreshEnabled, setAutoRefreshEnabled] = useState(savedAutoRefresh.current.enabled)
  const [autoRefreshIntervalSeconds, setAutoRefreshIntervalSeconds] = useState(savedAutoRefresh.current.interval_seconds)
  const [autoRefreshCountdown, setAutoRefreshCountdown] = useState(0)
  const autoRefreshETag = useRef<string | null>(null)
  const autoRefreshFetching = useRef(false)
  const autoRefreshSilentUntil = useRef(0)
  const [hasPendingListSync, setHasPendingListSync] = useState(false)

  const [todayStatsByAccountId, setTodayStatsByAccountId] = useState<Record<string, WindowStats>>({})
  const [todayStatsLoading, setTodayStatsLoading] = useState(false)
  const [todayStatsError, setTodayStatsError] = useState<string | null>(null)
  const todayStatsReqSeq = useRef(0)
  const pendingTodayStatsRefresh = useRef(false)
  const [usageManualRefreshToken, setUsageManualRefreshToken] = useState(0)

  const usageBatchByAccountId = useRef<Record<string, AccountUsageInfo | null>>({})
  const usageBatchErrorByAccountId = useRef<Record<string, string | null>>({})
  const usageBatchLoadingByAccountId = useRef<Record<string, boolean>>({})
  const usageBatchRequestTokenByAccountId = useRef<Record<string, number>>({})
  const usageBatchCache = useRef(new Map<number, { data: AccountUsageInfo; ts: number }>())
  const pendingUsageBatchIds = useRef(new Set<number>())
  const usageBatchFlushTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const queuedUsageBatchForce = useRef(false)
  const usageBatchRequestToken = useRef(0)
  const [, forceUsageBatchRender] = useState(0)

  const accountsRef = useRef(accounts)
  accountsRef.current = accounts
  const loadingRef = useRef(loading)
  loadingRef.current = loading

  const isAnyModalOpen =
    showSync ||
    showImportData ||
    showExportDataDialog ||
    showTempUnsched ||
    showDeleteDialog ||
    showReAuth ||
    showTest ||
    showStats ||
    showSchedulePanel ||
    showUsageAlertPanel

  const syncAccountListDerivedParams = useCallback(() => {
    setParams((prev) => ({
      ...prev,
      include_scheduler_score: hiddenColumns.has('scheduler_score') ? '0' : '1',
      lite: '1'
    }))
  }, [hiddenColumns])

  const refreshTodayStatsBatch = useCallback(async () => {
    // Why this checks both columns:
    // - today_stats column shows dedicated today's metrics.
    // - usage column also embeds today's stats for Key/Bedrock rows.
    // So we only skip fetching when BOTH columns are hidden.
    if (hiddenColumns.has('today_stats') && hiddenColumns.has('usage')) {
      setTodayStatsLoading(false)
      setTodayStatsError(null)
      return
    }

    const accountIDs = accountsRef.current.map((account) => account.id)
    const reqSeq = ++todayStatsReqSeq.current
    if (accountIDs.length === 0) {
      setTodayStatsByAccountId({})
      setTodayStatsError(null)
      setTodayStatsLoading(false)
      return
    }

    setTodayStatsLoading(true)
    setTodayStatsError(null)

    try {
      const result = await adminAPI.accounts.getBatchTodayStats(accountIDs)
      if (reqSeq !== todayStatsReqSeq.current) return
      const serverStats = result.stats ?? {}
      const nextStats: Record<string, WindowStats> = {}
      for (const accountID of accountIDs) {
        const key = String(accountID)
        nextStats[key] = serverStats[key] ?? buildDefaultTodayStats()
      }
      setTodayStatsByAccountId(nextStats)
    } catch (error) {
      if (reqSeq !== todayStatsReqSeq.current) return
      setTodayStatsError('Failed')
      console.error('Failed to load account today stats:', error)
    } finally {
      if (reqSeq === todayStatsReqSeq.current) {
        setTodayStatsLoading(false)
      }
    }
  }, [hiddenColumns])

  const load = useCallback(async () => {
    syncAccountListDerivedParams()
    setHasPendingListSync(false)
    autoRefreshETag.current = null
    pendingTodayStatsRefresh.current = false
    setLoading(true)
    try {
      const current = paramsRef.current
      const data = await adminAPI.accounts.list(pagination.page, pagination.page_size, {
        platform: current.platform as string,
        type: current.type as string,
        status: current.status as string,
        group: current.group as string,
        search: current.search as string,
        privacy_mode: current.privacy_mode as string,
        include_scheduler_score: current.include_scheduler_score as string,
        sort_by: current.sort_by as string,
        sort_order: current.sort_order as 'asc' | 'desc'
      })
      setAccounts(data.items || [])
      setPagination((prev) => ({
        ...prev,
        total: data.total || 0,
        pages: data.pages || 0
      }))
    } catch (error) {
      console.error('Failed to load accounts:', error)
    } finally {
      setLoading(false)
      await refreshTodayStatsBatch()
    }
  }, [pagination.page, pagination.page_size, syncAccountListDerivedParams, refreshTodayStatsBatch])

  const reload = useCallback(async () => {
    await load()
  }, [load])

  const saveAccountFilterValues = () => {
    try {
      const current = paramsRef.current
      localStorage.setItem(
        ACCOUNT_FILTER_STORAGE_KEY,
        JSON.stringify({ platform: current.platform, status: current.status })
      )
    } catch {
      // Filtering remains available when browser storage is unavailable.
    }
  }

  const clearSelection = useCallback(() => {
    selectionRequestVersion.current += 1
    setSelectingAllResults(false)
    selectedAllResultIDs.current = null
    setSelectedSet(new Set())
  }, [])

  const debouncedReload = useCallback(() => {
    saveAccountFilterValues()
    clearSelection()
    syncAccountListDerivedParams()
    setHasPendingListSync(false)
    autoRefreshETag.current = null
    pendingTodayStatsRefresh.current = true
    void load()
  }, [load, syncAccountListDerivedParams, clearSelection])

  const handlePageChange = (page: number) => {
    syncAccountListDerivedParams()
    setHasPendingListSync(false)
    autoRefreshETag.current = null
    pendingTodayStatsRefresh.current = true
    setPagination((prev) => ({ ...prev, page }))
  }

  const handlePageSizeChange = (size: number) => {
    syncAccountListDerivedParams()
    setHasPendingListSync(false)
    autoRefreshETag.current = null
    pendingTodayStatsRefresh.current = true
    setPagination((prev) => ({ ...prev, page: 1, page_size: size }))
  }

  const handleSort = (key: string, order: SortOrder) => {
    const nextOrder: AccountSortOrder = order === 'desc' ? 'desc' : 'asc'
    initialSort.current = { sort_by: key, sort_order: nextOrder }
    try {
      localStorage.setItem(ACCOUNT_SORT_STORAGE_KEY, JSON.stringify({ key, order: nextOrder }))
    } catch {
      // ignore storage failures
    }
    setParams((prev) => ({ ...prev, sort_by: key, sort_order: nextOrder }))
    setPagination((prev) => ({ ...prev, page: 1 }))
    setHasPendingListSync(false)
    autoRefreshETag.current = null
    pendingTodayStatsRefresh.current = true
    void load()
  }

  // Reload when page / page size change.
  useEffect(() => {
    void load()
    // load changes identity with pagination; re-run on those changes only.
  }, [pagination.page, pagination.page_size])

  // Selection helpers
  const selIds = useMemo(() => [...selectedSet], [selectedSet])

  const isSelected = useCallback((id: number) => selectedSet.has(id), [selectedSet])

  const setSelectedIds = useCallback((ids: number[]) => {
    setSelectedSet(new Set(ids))
  }, [])

  const selectPage = useCallback(() => {
    setSelectedSet(new Set(accounts.map((account) => account.id)))
  }, [accounts])

  const toggleSel = useCallback((id: number) => {
    setSelectedSet((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const removeSelectedAccounts = useCallback((ids: number[]) => {
    const remove = new Set(ids)
    setSelectedSet((prev) => new Set([...prev].filter((id) => !remove.has(id))))
  }, [])

  const allResultsSelected = useMemo(() => {
    const snapshot = selectedAllResultIDs.current
    if (!snapshot || snapshot.size === 0 || snapshot.size !== selectedSet.size) return false
    return Array.from(snapshot).every((id) => selectedSet.has(id))
  }, [selectedSet])

  const groupsById = useMemo(() => new Map(accountGroupCatalog.map((group) => [group.id, group])), [accountGroupCatalog])
  const resolveAccountGroups = useCallback(
    (account: Account) =>
      account.groups ??
      (account.group_ids ?? []).flatMap((id) => {
        const group = groupsById.get(id)
        return group ? [group] : []
      }),
    [groupsById]
  )

  // Columns
  const allColumns = useMemo<Column[]>(() => {
    const c: Column[] = [
      { key: 'select', label: '', sortable: false },
      { key: 'name', label: t('admin.accounts.columns.name'), sortable: true },
      { key: 'id', label: t('admin.accounts.columns.id'), sortable: true },
      { key: 'platform_type', label: t('admin.accounts.columns.platformType'), sortable: false },
      { key: 'capacity', label: t('admin.accounts.columns.capacity'), sortable: false },
      { key: 'status', label: t('admin.accounts.columns.status'), sortable: true },
      { key: 'schedulable', label: t('admin.accounts.columns.schedulable'), sortable: true },
      { key: 'today_stats', label: t('admin.accounts.columns.todayStats'), sortable: false }
    ]
    if (!isSimpleMode) {
      c.push({ key: 'groups', label: t('admin.accounts.columns.groups'), sortable: false })
    }
    c.push({ key: 'usage', label: t('admin.accounts.columns.usageWindows'), sortable: false })
    c.push(
      { key: 'proxy', label: t('admin.accounts.columns.proxy'), sortable: false },
      { key: 'priority', label: t('admin.accounts.columns.priority'), sortable: true },
      { key: 'scheduler_score', label: t('admin.accounts.columns.schedulerScore'), sortable: false },
      { key: 'rate_multiplier', label: t('admin.accounts.columns.billingRateMultiplier'), sortable: true },
      { key: 'last_used_at', label: t('admin.accounts.columns.lastUsed'), sortable: true },
      { key: 'created_at', label: t('admin.accounts.columns.createdAt'), sortable: true },
      { key: 'expires_at', label: t('admin.accounts.columns.expiresAt'), sortable: true },
      { key: 'notes', label: t('admin.accounts.columns.notes'), sortable: false },
      { key: 'actions', label: t('admin.accounts.columns.actions'), sortable: false }
    )
    return c
  }, [t, isSimpleMode])

  const toggleableColumns = useMemo(
    () => allColumns.filter((col) => col.key !== 'select' && col.key !== 'name' && col.key !== 'actions'),
    [allColumns]
  )

  const cols = useMemo(
    () =>
      allColumns.filter(
        (col) => col.key === 'select' || col.key === 'name' || col.key === 'actions' || !hiddenColumns.has(col.key)
      ),
    [allColumns, hiddenColumns]
  )

  const isColumnVisible = useCallback((key: string) => !hiddenColumns.has(key), [hiddenColumns])

  const toggleColumn = (key: string) => {
    const wasHidden = hiddenColumns.has(key)
    const next = new Set(hiddenColumns)
    if (next.has(key)) next.delete(key)
    else next.add(key)
    setHiddenColumns(next)
    saveColumnsToStorage(next)
    if ((key === 'today_stats' || key === 'usage') && wasHidden) {
      void refreshTodayStatsBatch().catch((error) => {
        console.error('Failed to load account today stats after showing column:', error)
      })
    }
    if (key === 'scheduler_score') {
      // The server only returns scheduler scores when this column is visible, so reload the current page immediately.
      syncAccountListDerivedParams()
      void load().catch((error) => {
        console.error('Failed to reload accounts after toggling scheduler score column:', error)
      })
    }
  }

  // Account tools dropdown
  const accountToolsDropdownStyle = {
    top: accountToolsDropdownPosition.top == null ? 'auto' : `${accountToolsDropdownPosition.top}px`,
    bottom: accountToolsDropdownPosition.bottom == null ? 'auto' : `${accountToolsDropdownPosition.bottom}px`,
    left: `${accountToolsDropdownPosition.left}px`,
    width: `${accountToolsDropdownPosition.width}px`
  }

  const updateAccountToolsDropdownPosition = () => {
    const trigger = accountToolsTriggerRef.current
    if (!trigger) return

    const position = getFloatingPanelPosition(
      trigger.getBoundingClientRect(),
      document.documentElement.clientWidth || window.innerWidth,
      window.innerHeight
    )
    setAccountToolsDropdownPosition({
      top: position.top,
      bottom: position.bottom,
      left: position.left,
      width: position.width,
      maxHeight: position.maxHeight
    })
  }

  const toggleAccountToolsDropdown = () => {
    const nextVisible = !showAccountToolsDropdown
    setShowAutoRefreshDropdown(false)
    if (nextVisible) updateAccountToolsDropdownPosition()
    setShowAccountToolsDropdown(nextVisible)
  }

  // Auto refresh
  const setAutoRefreshEnabledFn = (enabled: boolean) => {
    setAutoRefreshEnabled(enabled)
    saveAutoRefreshToStorage(enabled, autoRefreshIntervalSeconds)
    if (enabled) {
      setAutoRefreshCountdown(autoRefreshIntervalSeconds)
    } else {
      setAutoRefreshCountdown(0)
    }
  }

  const setAutoRefreshIntervalFn = (seconds: number) => {
    setAutoRefreshIntervalSeconds(seconds as (typeof autoRefreshIntervals)[number])
    saveAutoRefreshToStorage(autoRefreshEnabled, seconds)
    if (autoRefreshEnabled) {
      setAutoRefreshCountdown(seconds)
    }
  }

  const autoRefreshIntervalLabel = (sec: number) => {
    if (sec === 5) return t('admin.accounts.refreshInterval5s')
    if (sec === 10) return t('admin.accounts.refreshInterval10s')
    if (sec === 15) return t('admin.accounts.refreshInterval15s')
    if (sec === 30) return t('admin.accounts.refreshInterval30s')
    return `${sec}s`
  }

  const enterAutoRefreshSilentWindow = () => {
    autoRefreshSilentUntil.current = Date.now() + AUTO_REFRESH_SILENT_WINDOW_MS
    setAutoRefreshCountdown(autoRefreshIntervalSeconds)
  }

  const shouldReplaceAutoRefreshRow = (current: Account, next: Account) => {
    return (
      current.updated_at !== next.updated_at ||
      current.oauth_credential_owner !== next.oauth_credential_owner ||
      current.current_concurrency !== next.current_concurrency ||
      current.current_window_cost !== next.current_window_cost ||
      current.active_sessions !== next.active_sessions ||
      current.schedulable !== next.schedulable ||
      current.status !== next.status ||
      current.rate_limit_reset_at !== next.rate_limit_reset_at ||
      current.overload_until !== next.overload_until ||
      current.temp_unschedulable_until !== next.temp_unschedulable_until ||
      buildOpenAIUsageRefreshKey(current) !== buildOpenAIUsageRefreshKey(next) ||
      buildGrokUsageRefreshKey(current) !== buildGrokUsageRefreshKey(next)
    )
  }

  const syncAccountRefs = (nextAccount: Account) => {
    setReAuthAcc((prev) => (prev?.id === nextAccount.id ? nextAccount : prev))
    setTempUnschedAcc((prev) => (prev?.id === nextAccount.id ? nextAccount : prev))
    setDeletingAcc((prev) => (prev?.id === nextAccount.id ? nextAccount : prev))
    setMenu((prev) => (prev.acc?.id === nextAccount.id ? { ...prev, acc: nextAccount } : prev))
  }

  const mergeAccountsIncrementally = (nextRows: Account[]) => {
    const currentRows = accountsRef.current
    const currentByID = new Map(currentRows.map((row) => [row.id, row]))
    let changed = nextRows.length !== currentRows.length
    const mergedRows = nextRows.map((nextRow) => {
      const currentRow = currentByID.get(nextRow.id)
      if (!currentRow) {
        changed = true
        return nextRow
      }
      if (shouldReplaceAutoRefreshRow(currentRow, nextRow)) {
        changed = true
        syncAccountRefs(nextRow)
        return nextRow
      }
      return currentRow
    })
    if (!changed) {
      for (let i = 0; i < mergedRows.length; i += 1) {
        if (mergedRows[i].id !== currentRows[i]?.id) {
          changed = true
          break
        }
      }
    }
    if (changed) {
      setAccounts(mergedRows)
    }
  }

  const refreshAccountsIncrementally = async () => {
    if (autoRefreshFetching.current) return
    syncAccountListDerivedParams()
    autoRefreshFetching.current = true
    try {
      const current = paramsRef.current
      const result = await adminAPI.accounts.listWithEtag(
        pagination.page,
        pagination.page_size,
        {
          platform: current.platform as string,
          type: current.type as string,
          status: current.status as string,
          privacy_mode: current.privacy_mode as string,
          group: current.group as string,
          search: current.search as string,
          sort_by: current.sort_by as string,
          sort_order: current.sort_order as 'asc' | 'desc'
        },
        { etag: autoRefreshETag.current }
      )

      if (result.etag) {
        autoRefreshETag.current = result.etag
      }
      if (!result.notModified && result.data) {
        setPagination((prev) => ({ ...prev, total: result.data?.total || 0, pages: result.data?.pages || 0 }))
        mergeAccountsIncrementally(result.data.items || [])
        setHasPendingListSync(false)
      }

      await refreshTodayStatsBatch()
    } catch (error) {
      console.error('Auto refresh failed:', error)
    } finally {
      autoRefreshFetching.current = false
    }
  }

  const handleManualRefresh = async () => {
    await load()
    // Force usage cells to refetch /usage on explicit user refresh.
    setUsageManualRefreshToken((prev) => prev + 1)
  }

  const syncPendingListChanges = async () => {
    setHasPendingListSync(false)
    await load()
    // Keep behavior consistent with manual refresh.
    setUsageManualRefreshToken((prev) => prev + 1)
  }

  // Keep the latest incremental refresh reachable without re-arming the interval.
  const refreshAccountsIncrementallyRef = useRef(refreshAccountsIncrementally)
  refreshAccountsIncrementallyRef.current = refreshAccountsIncrementally

  // Auto-refresh countdown tick.
  useEffect(() => {
    if (!autoRefreshEnabled) return
    const timer = setInterval(() => {
      if (document.hidden) return
      if (loadingRef.current || autoRefreshFetching.current) return
      if (isAnyModalOpen) return
      if (menu.show || showAccountToolsDropdown || showAutoRefreshDropdown) return
      if (Date.now() < autoRefreshSilentUntil.current) {
        setAutoRefreshCountdown(Math.max(0, Math.ceil((autoRefreshSilentUntil.current - Date.now()) / 1000)))
        return
      }

      if (autoRefreshCountdown <= 0) {
        setAutoRefreshCountdown(autoRefreshIntervalSeconds)
        void refreshAccountsIncrementallyRef.current()
        return
      }

      setAutoRefreshCountdown((prev) => prev - 1)
    }, 1000)
    return () => clearInterval(timer)
  }, [
    autoRefreshEnabled,
    autoRefreshCountdown,
    autoRefreshIntervalSeconds,
    isAnyModalOpen,
    menu.show,
    showAccountToolsDropdown,
    showAutoRefreshDropdown
  ])

  // Usage batching
  const setUsageBatchLoading = (accountID: number, loadingState: boolean) => {
    usageBatchLoadingByAccountId.current = {
      ...usageBatchLoadingByAccountId.current,
      [String(accountID)]: loadingState
    }
  }

  const setUsageBatchState = (accountID: number, usage: AccountUsageInfo | null, error: string | null) => {
    const key = String(accountID)
    usageBatchByAccountId.current = { ...usageBatchByAccountId.current, [key]: usage }
    usageBatchErrorByAccountId.current = { ...usageBatchErrorByAccountId.current, [key]: error }
  }

  const handleAccountUsageLoaded = (accountID: number, usage: AccountUsageInfo) => {
    if (usageBatchByAccountId.current[String(accountID)] === usage) return
    setUsageBatchState(accountID, usage, null)
  }

  const flushQueuedUsageBatch = async () => {
    usageBatchFlushTimer.current = null
    const accountIDs = Array.from(pendingUsageBatchIds.current)
    const force = queuedUsageBatchForce.current
    pendingUsageBatchIds.current.clear()
    queuedUsageBatchForce.current = false

    if (accountIDs.length === 0) return

    const requestTokensByAccount = accountIDs.reduce<Record<string, number>>((acc, accountID) => {
      acc[String(accountID)] = usageBatchRequestTokenByAccountId.current[String(accountID)] ?? 0
      return acc
    }, {})

    try {
      const result = await adminAPI.accounts.getBatchUsage(accountIDs, force)

      const usageMap = result.usage ?? {}
      const errorMap = result.errors ?? {}
      const now = Date.now()
      const nextUsage = { ...usageBatchByAccountId.current }
      const nextErrors = { ...usageBatchErrorByAccountId.current }
      const nextLoading = { ...usageBatchLoadingByAccountId.current }

      for (const accountID of accountIDs) {
        const key = String(accountID)
        if ((usageBatchRequestTokenByAccountId.current[key] ?? 0) !== requestTokensByAccount[key]) {
          continue
        }
        const usage = usageMap[key] ?? null
        nextUsage[key] = usage
        nextErrors[key] = errorMap[key] ?? null
        nextLoading[key] = false
        if (usage) {
          usageBatchCache.current.set(accountID, { data: usage, ts: now })
        } else {
          usageBatchCache.current.delete(accountID)
        }
      }

      usageBatchByAccountId.current = nextUsage
      usageBatchErrorByAccountId.current = nextErrors
      usageBatchLoadingByAccountId.current = nextLoading
    } catch (error) {
      const nextErrors = { ...usageBatchErrorByAccountId.current }
      const nextLoading = { ...usageBatchLoadingByAccountId.current }
      for (const accountID of accountIDs) {
        const key = String(accountID)
        if ((usageBatchRequestTokenByAccountId.current[key] ?? 0) !== requestTokensByAccount[key]) {
          continue
        }
        nextErrors[key] = 'Failed'
        nextLoading[key] = false
      }
      usageBatchErrorByAccountId.current = nextErrors
      usageBatchLoadingByAccountId.current = nextLoading
      console.error('Failed to load account usage batch:', error)
    }
    forceUsageBatchRender((prev) => prev + 1)
  }

  const queueBatchedUsage = (account: Account, options?: { force?: boolean }) => {
    if (!isDesktopViewport) return
    if (!accountSupportsBatchUsage(account)) return

    const force = options?.force === true
    const cacheKey = account.id
    const key = String(cacheKey)

    if (force) {
      usageBatchCache.current.delete(cacheKey)
    } else {
      const cached = usageBatchCache.current.get(cacheKey)
      if (cached && Date.now() - cached.ts < USAGE_BATCH_CACHE_TTL) {
        setUsageBatchState(cacheKey, cached.data, null)
        setUsageBatchLoading(cacheKey, false)
        forceUsageBatchRender((prev) => prev + 1)
        return
      }
    }

    usageBatchErrorByAccountId.current = { ...usageBatchErrorByAccountId.current, [key]: null }
    usageBatchRequestTokenByAccountId.current = {
      ...usageBatchRequestTokenByAccountId.current,
      [key]: ++usageBatchRequestToken.current
    }
    setUsageBatchLoading(cacheKey, true)
    pendingUsageBatchIds.current.add(cacheKey)
    queuedUsageBatchForce.current = queuedUsageBatchForce.current || force
    forceUsageBatchRender((prev) => prev + 1)

    if (usageBatchFlushTimer.current !== null) return
    usageBatchFlushTimer.current = setTimeout(() => {
      void flushQueuedUsageBatch()
    }, 0)
  }

  // Prune batch state for rows that are no longer visible.
  useEffect(() => {
    const visibleIDs = new Set(accounts.map((row) => String(row.id)))
    usageBatchByAccountId.current = Object.fromEntries(
      Object.entries(usageBatchByAccountId.current).filter(([key]) => visibleIDs.has(key))
    )
    usageBatchErrorByAccountId.current = Object.fromEntries(
      Object.entries(usageBatchErrorByAccountId.current).filter(([key]) => visibleIDs.has(key))
    )
    usageBatchLoadingByAccountId.current = Object.fromEntries(
      Object.entries(usageBatchLoadingByAccountId.current).filter(([key]) => visibleIDs.has(key))
    )
    usageBatchRequestTokenByAccountId.current = Object.fromEntries(
      Object.entries(usageBatchRequestTokenByAccountId.current).filter(([key]) => visibleIDs.has(key))
    )
  }, [accounts])

  // Refresh today stats after a table load completes.
  useEffect(() => {
    if (loading) return
    if (!pendingTodayStatsRefresh.current) return
    pendingTodayStatsRefresh.current = false
    void refreshTodayStatsBatch().catch((error) => {
      console.error('Failed to refresh account today stats after table load:', error)
    })
  }, [loading, refreshTodayStatsBatch])

  // Click outside / scroll handlers
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as HTMLElement
      if (accountToolsDropdownRef.current && !accountToolsDropdownRef.current.contains(target)) {
        setShowAccountToolsDropdown(false)
      }
      if (autoRefreshDropdownRef.current && !autoRefreshDropdownRef.current.contains(target)) {
        setShowAutoRefreshDropdown(false)
      }
    }
    document.addEventListener('click', handleClickOutside)
    return () => document.removeEventListener('click', handleClickOutside)
  }, [])

  // 表格滚动时关闭行操作菜单，并让顶部工具菜单继续贴紧触发按钮。
  useEffect(() => {
    const handleScroll = (event: Event) => {
      if (event.target instanceof Element && event.target.closest('.action-menu-content')) return
      setMenu((prev) => ({ ...prev, show: false }))
      if (showAccountToolsDropdown) updateAccountToolsDropdownPosition()
    }
    const handleViewportResize = () => {
      if (showAccountToolsDropdown) updateAccountToolsDropdownPosition()
    }
    window.addEventListener('scroll', handleScroll, true)
    window.addEventListener('resize', handleViewportResize)
    return () => {
      window.removeEventListener('scroll', handleScroll, true)
      window.removeEventListener('resize', handleViewportResize)
    }
  }, [showAccountToolsDropdown])

  // Initial data load
  useEffect(() => {
    void load()
    void Promise.allSettled([adminAPI.groups.getAllIncludingInactive()]).then(([groupsResult]) => {
      if (groupsResult.status === 'fulfilled') {
        setAccountGroupCatalog(groupsResult.value)
        setGroups(groupsResult.value.filter((group) => group.status === 'active'))
      } else {
        console.error('Failed to load groups:', groupsResult.reason)
      }
    })
    // Mount-only initial load.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Account detail loading (for modals)
  const accountDetailLoading = useRef(new Set<number>())
  const loadAccountDetails = async (account: Pick<Account, 'id'>): Promise<Account | null> => {
    if (accountDetailLoading.current.has(account.id)) return null
    accountDetailLoading.current.add(account.id)
    try {
      return await adminAPI.accounts.getById(account.id)
    } catch (error) {
      console.error('Failed to load account details:', error)
      showError(extractApiErrorMessage(error, t('common.error')))
      return null
    } finally {
      accountDetailLoading.current.delete(account.id)
    }
  }

  const openMenu = (a: Account, e: React.MouseEvent) => {
    const target = e.currentTarget as HTMLElement
    setMenu({ show: true, acc: a, anchorRect: target.getBoundingClientRect() })
  }

  const handleBulkDelete = async () => {
    const accountIds = [...selIds]
    if (!window.confirm(t('admin.accounts.bulkActions.confirmDelete', { count: accountIds.length }))) return
    try {
      const result = await adminAPI.accounts.batchDelete(accountIds)
      if (result.failed > 0) {
        showError(t('admin.accounts.bulkActions.partialSuccess', { success: result.success, failed: result.failed }))
        setSelectedIds(result.failed_ids?.length ? result.failed_ids : accountIds)
      } else {
        showSuccess(t('admin.accounts.bulkActions.deleteSuccess', { count: result.success }))
        clearSelection()
      }
      await reload()
    } catch (error) {
      console.error('Failed to bulk delete accounts:', error)
      showError(String(error))
    }
  }

  const handleBulkResetStatus = async () => {
    if (!window.confirm(t('common.confirm'))) return
    try {
      const result = await adminAPI.accounts.batchClearError(selIds)
      if (result.failed > 0) {
        showError(t('admin.accounts.bulkActions.partialSuccess', { success: result.success, failed: result.failed }))
      } else {
        showSuccess(t('admin.accounts.bulkActions.resetStatusSuccess', { count: result.success }))
        clearSelection()
      }
      void reload()
    } catch (error) {
      console.error('Failed to bulk reset status:', error)
      showError(String(error))
    }
  }

  const handleBulkRefreshToken = async () => {
    if (!window.confirm(t('common.confirm'))) return
    const accountIds = [...selIds]
    try {
      const result = await adminAPI.accounts.batchRefresh(accountIds)
      if (result.failed > 0) {
        showError(t('admin.accounts.bulkActions.partialSuccess', { success: result.success, failed: result.failed }))
        const failedIds = result.errors?.map((error) => error.account_id) ?? []
        setSelectedIds(failedIds.length > 0 ? failedIds : accountIds)
      } else {
        showSuccess(t('admin.accounts.bulkActions.refreshTokenSuccess', { count: result.success }))
        clearSelection()
      }
      void reload()
    } catch (error) {
      console.error('Failed to bulk refresh token:', error)
      showError(String(error))
    }
  }

  const updateSchedulableInList = (accountIds: number[], schedulable: boolean) => {
    if (accountIds.length === 0) return
    const idSet = new Set(accountIds)
    setAccounts((prev) => prev.map((account) => (idSet.has(account.id) ? { ...account, schedulable } : account)))
  }

  const normalizeBulkSchedulableResult = (
    result: {
      success?: number
      failed?: number
      success_ids?: number[]
      failed_ids?: number[]
      results?: Array<{ account_id: number; success: boolean }>
    },
    accountIds: number[]
  ) => {
    const responseSuccessIds = Array.isArray(result.success_ids) ? result.success_ids : []
    const responseFailedIds = Array.isArray(result.failed_ids) ? result.failed_ids : []
    if (responseSuccessIds.length > 0 || responseFailedIds.length > 0) {
      return {
        successIds: responseSuccessIds,
        failedIds: responseFailedIds,
        successCount: typeof result.success === 'number' ? result.success : responseSuccessIds.length,
        failedCount: typeof result.failed === 'number' ? result.failed : responseFailedIds.length,
        hasIds: true,
        hasCounts: true
      }
    }

    const results = Array.isArray(result.results) ? result.results : []
    if (results.length > 0) {
      const successIds = results.filter((item) => item.success).map((item) => item.account_id)
      const failedIds = results.filter((item) => !item.success).map((item) => item.account_id)
      return {
        successIds,
        failedIds,
        successCount: typeof result.success === 'number' ? result.success : successIds.length,
        failedCount: typeof result.failed === 'number' ? result.failed : failedIds.length,
        hasIds: true,
        hasCounts: true
      }
    }

    const hasExplicitCounts = typeof result.success === 'number' || typeof result.failed === 'number'
    const successCount = typeof result.success === 'number' ? result.success : 0
    const failedCount = typeof result.failed === 'number' ? result.failed : 0
    if (hasExplicitCounts && failedCount === 0 && successCount === accountIds.length && accountIds.length > 0) {
      return {
        successIds: accountIds,
        failedIds: [],
        successCount,
        failedCount,
        hasIds: true,
        hasCounts: true
      }
    }

    return {
      successIds: [],
      failedIds: [],
      successCount,
      failedCount,
      hasIds: false,
      hasCounts: hasExplicitCounts
    }
  }

  const handleBulkToggleSchedulable = async (schedulable: boolean) => {
    const accountIds = [...selIds]
    try {
      const result = await adminAPI.accounts.bulkUpdate(accountIds, { schedulable })
      const { successIds, failedIds, successCount, failedCount, hasIds, hasCounts } = normalizeBulkSchedulableResult(
        result,
        accountIds
      )
      if (!hasIds && !hasCounts) {
        showError(t('admin.accounts.bulkSchedulableResultUnknown'))
        setSelectedIds(accountIds)
        void load().catch((error) => {
          console.error('Failed to refresh accounts:', error)
        })
        return
      }
      if (successIds.length > 0) {
        updateSchedulableInList(successIds, schedulable)
      }
      if (successCount > 0 && failedCount === 0) {
        const message = schedulable
          ? t('admin.accounts.bulkSchedulableEnabled', { count: successCount })
          : t('admin.accounts.bulkSchedulableDisabled', { count: successCount })
        showSuccess(message)
      }
      if (failedCount > 0) {
        const message =
          hasCounts || hasIds
            ? t('admin.accounts.bulkSchedulablePartial', { success: successCount, failed: failedCount })
            : t('admin.accounts.bulkSchedulableResultUnknown')
        showError(message)
        setSelectedIds(failedIds.length > 0 ? failedIds : accountIds)
      } else {
        if (hasIds) clearSelection()
        else setSelectedIds(accountIds)
      }
    } catch (error) {
      console.error('Failed to bulk toggle schedulable:', error)
      showError(t('common.error'))
    }
  }

  const buildBulkEditFilterSnapshot = () => {
    const rawParams = paramsRef.current
    const sortOrder: AccountSortOrder = rawParams.sort_order === 'desc' ? 'desc' : 'asc'
    return {
      platform: typeof rawParams.platform === 'string' ? rawParams.platform : '',
      type: typeof rawParams.type === 'string' ? rawParams.type : '',
      status: typeof rawParams.status === 'string' ? rawParams.status : '',
      group: typeof rawParams.group === 'string' ? rawParams.group : '',
      search: typeof rawParams.search === 'string' ? rawParams.search : '',
      privacy_mode: typeof rawParams.privacy_mode === 'string' ? rawParams.privacy_mode : '',
      sort_by: typeof rawParams.sort_by === 'string' ? rawParams.sort_by : '',
      sort_order: sortOrder
    }
  }

  const handleSelectAllResults = async () => {
    if (selectingAllResults || pagination.total === 0) return

    const requestVersion = ++selectionRequestVersion.current
    const filters = buildBulkEditFilterSnapshot()
    setSelectingAllResults(true)
    try {
      const ids = await fetchAllAccountIds(
        (page, pageSize, requestFilters) =>
          adminAPI.accounts.list(page, pageSize, requestFilters as Parameters<typeof adminAPI.accounts.list>[2]),
        filters
      )
      if (requestVersion !== selectionRequestVersion.current) return

      setSelectedIds(ids)
      selectedAllResultIDs.current = new Set(ids)
    } catch (error) {
      if (requestVersion !== selectionRequestVersion.current) return
      console.error('Failed to select all account results:', error)
      showError(t('admin.accounts.bulkActions.selectAllFailed'))
    } finally {
      if (requestVersion === selectionRequestVersion.current) {
        setSelectingAllResults(false)
      }
    }
  }

  const handleDataImported = () => {
    setShowImportData(false)
    void reload()
  }

  const buildAccountQueryFilters = () => {
    const current = paramsRef.current
    return {
      platform: (current.platform as string) || '',
      type: (current.type as string) || '',
      status: (current.status as string) || '',
      group: (current.group as string) || '',
      privacy_mode: (current.privacy_mode as string) || '',
      search: (current.search as string) || '',
      sort_by: initialSort.current.sort_by,
      sort_order: initialSort.current.sort_order
    }
  }

  const accountMatchesCurrentFilters = (account: Account) => {
    const filters = buildAccountQueryFilters()
    if (filters.platform && account.platform !== filters.platform) return false
    if (filters.type && account.type !== filters.type) return false
    if (filters.status) {
      const now = Date.now()
      const rateLimitResetAt = account.rate_limit_reset_at ? new Date(account.rate_limit_reset_at).getTime() : Number.NaN
      const isRateLimited = Number.isFinite(rateLimitResetAt) && rateLimitResetAt > now
      const tempUnschedUntil = account.temp_unschedulable_until
        ? new Date(account.temp_unschedulable_until).getTime()
        : Number.NaN
      const isTempUnschedulable = Number.isFinite(tempUnschedUntil) && tempUnschedUntil > now

      if (filters.status === 'active') {
        if (account.status !== 'active' || isRateLimited || isTempUnschedulable || !account.schedulable) return false
      } else if (filters.status === 'rate_limited') {
        if (account.status !== 'active' || !isRateLimited || isTempUnschedulable) return false
      } else if (filters.status === 'temp_unschedulable') {
        if (account.status !== 'active' || !isTempUnschedulable) return false
      } else if (filters.status === 'unschedulable') {
        if (account.status !== 'active' || account.schedulable || isRateLimited || isTempUnschedulable) return false
      } else if (account.status !== filters.status) {
        return false
      }
    }
    if (filters.group) {
      const groupIds = account.group_ids ?? account.groups?.map((group) => group.id) ?? []
      if (filters.group === ACCOUNT_UNGROUPED_GROUP_QUERY_VALUE) {
        if (groupIds.length > 0) return false
      } else if (!groupIds.includes(Number(filters.group))) {
        return false
      }
    }
    const privacyMode = typeof account.extra?.privacy_mode === 'string' ? account.extra.privacy_mode : ''
    if (filters.privacy_mode) {
      if (filters.privacy_mode === ACCOUNT_PRIVACY_MODE_UNSET_QUERY_VALUE) {
        if (privacyMode.trim() !== '') return false
      } else if (privacyMode !== filters.privacy_mode) {
        return false
      }
    }
    const search = String(filters.search || '')
      .trim()
      .toLowerCase()
    if (search && !account.name.toLowerCase().includes(search)) return false
    return true
  }

  const mergeRuntimeFields = (oldAccount: Account, updatedAccount: Account): Account => ({
    ...updatedAccount,
    current_concurrency: updatedAccount.current_concurrency ?? oldAccount.current_concurrency,
    current_window_cost: updatedAccount.current_window_cost ?? oldAccount.current_window_cost,
    active_sessions: updatedAccount.active_sessions ?? oldAccount.active_sessions
  })

  const syncPaginationAfterLocalRemoval = () => {
    setPagination((prev) => {
      const nextTotal = Math.max(0, prev.total - 1)
      const nextPages = nextTotal > 0 ? Math.ceil(nextTotal / prev.page_size) : 0
      const maxPage = Math.max(1, nextPages || 1)
      return {
        ...prev,
        total: nextTotal,
        pages: nextPages,
        page: prev.page > maxPage ? maxPage : prev.page
      }
    })
    // 行被本地移除后不立刻全量补页，改为提示用户手动同步。
    setHasPendingListSync(true)
  }

  const patchAccountInList = (updatedAccount: Account) => {
    const index = accounts.findIndex((account) => account.id === updatedAccount.id)
    if (index === -1) return
    const mergedAccount = mergeRuntimeFields(accounts[index], updatedAccount)
    if (!accountMatchesCurrentFilters(mergedAccount)) {
      setAccounts((prev) => prev.filter((account) => account.id !== mergedAccount.id))
      syncPaginationAfterLocalRemoval()
      removeSelectedAccounts([mergedAccount.id])
      setMenu((prev) => {
        if (prev.acc?.id === mergedAccount.id) {
          return { show: false, acc: null, anchorRect: null }
        }
        return prev
      })
      return
    }
    setAccounts((prev) => prev.map((account, i) => (i === index ? mergedAccount : account)))
    syncAccountRefs(mergedAccount)
  }

  const handleAccountUpdated = (updatedAccount: Account) => {
    patchAccountInList(updatedAccount)
    enterAutoRefreshSilentWindow()
  }

  const formatExportTimestamp = () => {
    const now = new Date()
    const pad2 = (value: number) => String(value).padStart(2, '0')
    return `${now.getFullYear()}${pad2(now.getMonth() + 1)}${pad2(now.getDate())}${pad2(now.getHours())}${pad2(now.getMinutes())}${pad2(now.getSeconds())}`
  }

  const closeExportDataDialog = () => {
    setShowExportDataDialog(false)
    setExportPassword('')
    setExportTotpCode('')
    setExportStepUpRequired(false)
  }

  const openExportDataDialog = async () => {
    if (exportRequirementsLoading) return
    setExportRequirementsLoading(true)
    try {
      const requirements = await adminAPI.accounts.getExportRequirements()
      setIncludeProxyOnExport(true)
      setExportPassword('')
      setExportTotpCode('')
      setExportStepUpRequired(requirements.step_up_required === true)
      setShowExportDataDialog(true)
    } catch (error) {
      showError(extractApiErrorMessage(error, t('admin.accounts.dataExportFailed')))
    } finally {
      setExportRequirementsLoading(false)
    }
  }

  const handleExportData = async () => {
    if (exportingData || !exportPassword) return
    setExportingData(true)
    try {
      const scope =
        selIds.length > 0
          ? { ids: selIds, includeProxies: includeProxyOnExport }
          : { includeProxies: includeProxyOnExport, filters: buildAccountQueryFilters() }
      const dataPayload = await adminAPI.accounts.exportData({
        password: exportPassword,
        totpCode: exportTotpCode,
        ...scope
      })
      const timestamp = formatExportTimestamp()
      const filename = `sub2api-account-${timestamp}.json`
      const blob = new Blob([JSON.stringify(dataPayload, null, 2)], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = filename
      link.click()
      URL.revokeObjectURL(url)
      // spark 影子账号被后端排除出备份(其凭据透传母账号、调度配置不可经凭据型导入重建);
      // 跳过非零时明确提示用户,避免「下载成功但少了账号」的静默丢失。
      if (dataPayload.skipped_shadows && dataPayload.skipped_shadows > 0) {
        showWarning(t('admin.accounts.dataExportedSkippedShadows', { count: dataPayload.skipped_shadows }))
      } else {
        showSuccess(t('admin.accounts.dataExported'))
      }
    } catch (error) {
      showError(extractApiErrorMessage(error, t('admin.accounts.dataExportFailed')))
    } finally {
      setExportingData(false)
      closeExportDataDialog()
    }
  }

  const handleTest = async (a: Account) => {
    const account = await loadAccountDetails(a)
    if (!account) return
    setTestingAcc(account)
    setShowTest(true)
  }

  const handleViewStats = async (a: Account) => {
    const account = await loadAccountDetails(a)
    if (!account) return
    setStatsAcc(account)
    setShowStats(true)
  }

  const handleSchedule = async (a: Account) => {
    setScheduleAcc(a)
    setScheduleModelOptions([])
    setShowSchedulePanel(true)
    try {
      const models = await adminAPI.accounts.getAvailableModels(a.id)
      setScheduleModelOptions(models.map((m: ClaudeModel) => ({ value: m.id, label: m.display_name || m.id })))
    } catch {
      setScheduleModelOptions([])
    }
  }

  const handleUsageAlert = (a: Account) => {
    setUsageAlertAcc(a)
    setShowUsageAlertPanel(true)
  }

  const handleReAuth = async (a: Account) => {
    const account = await loadAccountDetails(a)
    if (!account) return
    setReAuthAcc(account)
    setShowReAuth(true)
  }

  const duplicatingAccountIDs = useRef(new Set<number>())
  const handleDuplicateAccount = async (a: Account) => {
    if (duplicatingAccountIDs.current.has(a.id)) return
    duplicatingAccountIDs.current.add(a.id)
    try {
      const duplicate = await adminAPI.accounts.duplicate(a.id)
      showSuccess(t('admin.accounts.duplicateSuccess', { name: duplicate.name }))
      void reload()
    } catch (error) {
      console.error('Failed to duplicate account:', error)
      showError((error as { message?: string })?.message || t('admin.accounts.duplicateFailed'))
    } finally {
      duplicatingAccountIDs.current.delete(a.id)
    }
  }

  const handleRefresh = async (a: Account) => {
    try {
      const result = await adminAPI.accounts.refreshCredentials(a.id)
      patchAccountInList(result.account)
      enterAutoRefreshSilentWindow()
      if (result.warning) showWarning(result.message)
    } catch (error) {
      console.error('Failed to refresh credentials:', error)
    }
  }

  const handleRecoverState = async (a: Account) => {
    try {
      const updated = await adminAPI.accounts.recoverState(a.id)
      patchAccountInList(updated)
      enterAutoRefreshSilentWindow()
      showSuccess(t('admin.accounts.recoverStateSuccess'))
    } catch (error) {
      console.error('Failed to recover account state:', error)
      showError((error as { message?: string })?.message || t('admin.accounts.recoverStateFailed'))
    }
  }

  const handleResetQuota = async (a: Account) => {
    try {
      const updated = await adminAPI.accounts.resetAccountQuota(a.id)
      patchAccountInList(updated)
      enterAutoRefreshSilentWindow()
      showSuccess(t('common.success'))
    } catch (error) {
      console.error('Failed to reset quota:', error)
    }
  }

  const privacyResultMessageKey = (account: Account): { type: 'success' | 'error'; key: string } => {
    const mode = typeof account.extra?.privacy_mode === 'string' ? account.extra.privacy_mode : ''
    if (account.platform === 'openai') {
      switch (mode) {
        case 'training_off':
          return { type: 'success', key: 'admin.accounts.privacyTrainingOff' }
        case 'training_set_cf_blocked':
          return { type: 'error', key: 'admin.accounts.privacyCfBlocked' }
        default:
          return { type: 'error', key: 'admin.accounts.privacyFailed' }
      }
    }
    if (account.platform === 'antigravity') {
      if (mode === 'privacy_set') {
        return { type: 'success', key: 'admin.accounts.privacyAntigravitySet' }
      }
      return { type: 'error', key: 'admin.accounts.privacyAntigravityFailed' }
    }
    return { type: 'error', key: 'admin.accounts.privacyFailed' }
  }

  const handleSetPrivacy = async (a: Account) => {
    try {
      const updated = await adminAPI.accounts.setPrivacy(a.id)
      patchAccountInList(updated)
      enterAutoRefreshSilentWindow()
      const result = privacyResultMessageKey(updated)
      if (result.type === 'success') {
        showSuccess(t(result.key))
      } else {
        showError(t(result.key))
      }
    } catch (error) {
      console.error('Failed to set privacy:', error)
      showError(
        (error as { response?: { data?: { message?: string } } })?.response?.data?.message ||
          t('admin.accounts.privacyFailed')
      )
    }
  }

  const onRevertFallback = async (a: Account) => {
    try {
      await adminAPI.accounts.revertProxyFallback(a.id)
      showSuccess(t('admin.accounts.revertProxySuccess'))
      void reload()
    } catch (error) {
      console.error('Failed to revert proxy fallback:', error)
      showError(
        (error as { response?: { data?: { message?: string } } })?.response?.data?.message ||
          t('admin.accounts.revertProxyFailed')
      )
    }
  }

  const handleCreateSparkShadow = (a: Account) => {
    setCreatingShadowAcc(a)
    setShowCreateShadowDialog(true)
  }

  const confirmCreateSparkShadow = async () => {
    const a = creatingShadowAcc
    if (!a) return
    try {
      await adminAPI.accounts.createSparkShadow(a.id, { name: `${a.name} (Spark)` })
      setShowCreateShadowDialog(false)
      setCreatingShadowAcc(null)
      showSuccess(t('admin.accounts.createSparkShadowSuccess'))
      void reload()
    } catch (error) {
      console.error('Failed to create spark shadow:', error)
      showError(
        (error as { response?: { data?: { message?: string } } })?.response?.data?.message ||
          t('admin.accounts.createSparkShadowFailed')
      )
    }
  }

  const handleDelete = (a: Account) => {
    setDeletingAcc(a)
    setShowDeleteDialog(true)
  }

  const confirmDelete = async () => {
    if (!deletingAcc) return
    try {
      await adminAPI.accounts.delete(deletingAcc.id)
      setShowDeleteDialog(false)
      setDeletingAcc(null)
      void reload()
    } catch (error) {
      console.error('Failed to delete account:', error)
    }
  }

  const handleToggleSchedulable = async (a: Account) => {
    const nextSchedulable = !a.schedulable
    setTogglingSchedulable(a.id)
    try {
      const updated = await adminAPI.accounts.setSchedulable(a.id, nextSchedulable)
      updateSchedulableInList([a.id], updated?.schedulable ?? nextSchedulable)
      enterAutoRefreshSilentWindow()
    } catch (error) {
      console.error('Failed to toggle schedulable:', error)
      showError(t('admin.accounts.failedToToggleSchedulable'))
    } finally {
      setTogglingSchedulable(null)
    }
  }

  const handleShowTempUnsched = (a: Account) => {
    setTempUnschedAcc(a)
    setShowTempUnsched(true)
  }

  const handleTempUnschedReset = async (updated: Account) => {
    setShowTempUnsched(false)
    setTempUnschedAcc(null)
    patchAccountInList(updated)
    enterAutoRefreshSilentWindow()
  }

  // 所绑定代理的有效期(逻辑同 /admin/proxies,见 utils/proxyExpiry)
  const proxyExpiryBadge = (p: AccountProxy): string => proxyExpiryBadgeClass(p.expires_at, p.status)
  const proxyExpiryText = (p: AccountProxy): string => {
    const { key, params } = proxyExpiryLabelKey(p.expires_at, p.status)
    return params ? t(key, params) : t(key)
  }

  const renderCell = (key: string, row: Account, value: unknown) => {
    switch (key) {
      case 'select':
        return (
          <input
            type="checkbox"
            checked={isSelected(row.id)}
            onChange={() => toggleSel(row.id)}
            className="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        )
      case 'id':
        return <span className="font-mono text-xs surface-muted">#{String(value)}</span>
      case 'name': {
        const homepage = accountHomepageUrl(row)
        const email = accountDisplayEmail(row)
        return (
          <div className="flex flex-col">
            {homepage ? (
              <a
                href={homepage}
                target="_blank"
                rel="noopener noreferrer"
                className="self-start border-b border-dotted border-gray-300 font-medium text-gray-900 dark:border-dark-600 dark:text-white"
              >
                {String(value)}
              </a>
            ) : (
              <span className="font-medium text-gray-900 dark:text-white">{String(value)}</span>
            )}
            {email && (
              <span
                className="text-xs truncate max-w-[200px] surface-muted"
                title={email + (row.parent_chatgpt_account_id ? ` · ${row.parent_chatgpt_account_id}` : '')}
              >
                {email}
              </span>
            )}
          </div>
        )
      }
      case 'notes':
        return value ? (
          <span title={String(value)} className="block max-w-xs truncate text-sm surface-muted">
            {String(value)}
          </span>
        ) : (
          <span className="text-sm surface-muted">-</span>
        )
      case 'platform_type': {
        const compactMeta = getOpenAICompactMeta(row)
        return (
          <div className="flex min-w-0 flex-col gap-1">
            <div className="flex flex-wrap items-center gap-1">
              <PlatformTypeBadge
                platform={row.platform}
                type={row.type}
                authMode={getOpenAIAuthMode(row)}
                planType={getAccountPlanType(row)}
                privacyMode={(row.extra?.privacy_mode as string) || row.parent_privacy_mode || undefined}
                subscriptionExpiresAt={
                  ((row.credentials as Record<string, unknown> | undefined)?.subscription_expires_at as string) ||
                  row.parent_subscription_expires_at ||
                  undefined
                }
              />
              {getAntigravityTierLabelRow(row) && (
                <span
                  className={cn(
                    'inline-block rounded px-1.5 py-0.5 text-[10px] font-medium',
                    getAntigravityTierClassRow(row)
                  )}
                >
                  {t(getAntigravityTierLabelRow(row) as string)}
                </span>
              )}
            </div>
            {compactMeta && (
              <div
                className={cn(
                  'inline-flex items-center gap-1.5 pl-0.5 text-[11px] font-medium leading-4',
                  compactMeta.className
                )}
                title={getOpenAICompactTitleRow(row)}
              >
                <span className={cn('console-data-mark h-1.5 w-1.5 rounded-full', compactMeta.dotClass)} />
                <span>{t(compactMeta.label)}</span>
              </div>
            )}
          </div>
        )
      }
      case 'capacity':
        return <AccountCapacityCell account={row} />
      case 'status':
        return (
          <div className="flex items-center gap-1.5">
            <AccountStatusIndicator account={row} onShowTempUnsched={handleShowTempUnsched} />
          </div>
        )
      case 'schedulable':
        return (
          <Checkbox
            checked={!!row.schedulable}
            disabled={togglingSchedulable === row.id}
            onCheckedChange={() => void handleToggleSchedulable(row)}
            aria-label={row.schedulable ? t('admin.accounts.schedulableEnabled') : t('admin.accounts.schedulableDisabled')}
          />
        )
      case 'today_stats':
        return (
          <AccountTodayStatsCell
            stats={todayStatsByAccountId[String(row.id)] ?? null}
            loading={todayStatsLoading}
            error={todayStatsError}
          />
        )
      case 'groups':
        return <AccountGroupsCell groups={resolveAccountGroups(row)} maxDisplay={4} />
      case 'usage':
        return (
          <AccountUsageCell
            account={row}
            todayStats={todayStatsByAccountId[String(row.id)] ?? null}
            todayStatsLoading={todayStatsLoading}
            manualRefreshToken={usageManualRefreshToken}
            batchedUsage={usageBatchByAccountId.current[String(row.id)] ?? null}
            batchedUsageError={usageBatchErrorByAccountId.current[String(row.id)] ?? null}
            batchedUsageLoading={usageBatchLoadingByAccountId.current[String(row.id)] === true}
            requestBatchedUsage={isDesktopViewport ? queueBatchedUsage : null}
            onAccountUpdated={handleAccountUpdated}
            onUsageLoaded={(usage) => handleAccountUsageLoaded(row.id, usage)}
          />
        )
      case 'proxy': {
        const p = row.proxy
        return (
          <div className="flex flex-col gap-1">
            {p ? (
              <>
                <div className="flex items-center gap-2">
                  <span className="text-sm surface-muted">{p.name}</span>
                  {p.country_code && <span className="text-xs surface-muted">({p.country_code})</span>}
                </div>
                {p.expires_at && (
                  <div className="flex items-center gap-2 text-xs">
                    <span className="surface-muted">{formatDateTime(p.expires_at)}</span>
                    <span className={proxyExpiryBadge(p)}>{proxyExpiryText(p)}</span>
                  </div>
                )}
              </>
            ) : (
              <span className="text-sm surface-muted">-</span>
            )}
            {row.proxy_fallback_origin_id && (
              <div className="flex items-center gap-1">
                <span
                  className="inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium bg-yellow-100/[0.15] text-yellow-800 dark:bg-yellow-900/25 dark:text-yellow-200"
                  title={t('admin.accounts.fallbackActiveTip', { origin: row.proxy_fallback_origin_name })}
                >
                  {t('admin.accounts.fallbackActive')}
                </span>
                <button
                  className="text-xs px-1.5 py-0.5 rounded border border-gray-300 dark:border-dark-600 hover:surface-hover surface-control surface-muted"
                  onClick={() => void onRevertFallback(row)}
                >
                  {t('admin.accounts.revertProxy')}
                </button>
              </div>
            )}
          </div>
        )
      }
      case 'rate_multiplier':
        return (
          <span className="inline-flex items-center gap-1 text-sm font-mono surface-muted">
            <span>{formatMultiplier(row.rate_multiplier ?? 1)}x</span>
          </span>
        )
      case 'priority':
        return (
          <AccountPriorityCell account={row} onUpdated={handleAccountUpdated} onError={(message) => showError(message)} />
        )
      case 'scheduler_score': {
        const rows = getSchedulerScoreRows(row)
        if (rows.length === 0) return <span className="text-sm surface-muted">-</span>
        return (
          <div className="flex min-w-[7rem] flex-col gap-0.5 font-mono text-[11px] leading-4">
            {rows.map((score) => (
              <div
                key={String(score.group_id)}
                className="flex items-center gap-1 whitespace-nowrap surface-muted"
                title={`${formatSchedulerScoreGroup(score)} / ${formatSchedulerScore(score.base_score)} / ${formatStickySchedulerScore(score)}`}
              >
                <span className="max-w-[4.75rem] truncate surface-muted">{formatSchedulerScoreGroup(score)}</span>
                <span className="text-gray-300 dark:text-gray-600">/</span>
                <span>{formatSchedulerScore(score.base_score)}</span>
                <span className="text-gray-300 dark:text-gray-600">/</span>
                <span className="text-primary-700 dark:text-primary-300">{formatStickySchedulerScore(score)}</span>
              </div>
            ))}
          </div>
        )
      }
      case 'last_used_at':
        return <span className="text-sm surface-muted">{formatRelativeTime(value as string)}</span>
      case 'created_at':
        return <span className="text-sm surface-muted">{formatDateTime(value as string)}</span>
      case 'expires_at': {
        const expiresValue = row.expires_at
        return (
          <div className="flex flex-col items-start gap-1">
            <span className="text-sm surface-muted">{formatExpiresAt(expiresValue)}</span>
            {(isExpired(expiresValue) || (row.auto_pause_on_expired && expiresValue)) && (
              <div className="flex items-center gap-1">
                {isExpired(expiresValue) && (
                  <span className="inline-flex items-center rounded-md bg-amber-100/[0.15] px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300">
                    {t('admin.accounts.expired')}
                  </span>
                )}
                {row.auto_pause_on_expired && expiresValue && (
                  <span className="inline-flex items-center rounded-md bg-emerald-100/[0.15] px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300">
                    {t('admin.accounts.autoPauseOnExpired')}
                  </span>
                )}
              </div>
            )}
          </div>
        )
      }
      case 'actions':
        return (
          <div className="table-actions">
            <button onClick={() => handleDelete(row)} className="table-action table-action-danger">
              <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="1.5">
                <path strokeLinecap="round" strokeLinejoin="round" d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0" />
              </svg>
              <span className="text-xs">{t('common.delete')}</span>
            </button>
            <button onClick={(event) => openMenu(row, event)} className="table-action table-action-neutral">
              <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="1.5">
                <path strokeLinecap="round" strokeLinejoin="round" d="M6.75 12a.75.75 0 11-1.5 0 .75.75 0 011.5 0zM12.75 12a.75.75 0 11-1.5 0 .75.75 0 011.5 0zM18.75 12a.75.75 0 11-1.5 0 .75.75 0 011.5 0z" />
              </svg>
              <span className="text-xs">{t('common.more')}</span>
            </button>
          </div>
        )
      default:
        return null
    }
  }

  const renderHeader = (key: string, column: Column) => {
    if (key === 'usage') {
      return (
        <span className="inline-flex items-center">
          <span>{column.label}</span>
          <TriggerTooltip content={t('admin.accounts.usageWindowsHint')} widthClass="w-72">
            <span className="ml-1 inline-flex h-4 w-4 cursor-help items-center justify-center rounded-full border border-gray-400/70 text-[10px] font-semibold text-gray-400">
              ?
            </span>
          </TriggerTooltip>
        </span>
      )
    }
    if (key === 'scheduler_score') {
      return (
        <span className="inline-flex items-center">
          <span>{column.label}</span>
          <TriggerTooltip content={t('admin.accounts.schedulerScore.hint')} widthClass="w-80">
            <span className="ml-1 inline-flex h-4 w-4 cursor-help items-center justify-center rounded-full border border-gray-400/70 text-[10px] font-semibold text-gray-400">
              ?
            </span>
          </TriggerTooltip>
        </span>
      )
    }
    return <span>{column.label}</span>
  }

  return (
    <>
    <TablePageLayout
      filters={
        <div className="card p-4 sm:p-6">
          <div className="filter-bar">
            <AccountTableFilters
              searchQuery={(params.search as string) ?? ''}
              filters={params}
              groups={groups}
              onUpdateSearchQuery={(value) => setParams((prev) => ({ ...prev, search: value }))}
              onUpdateFilters={(newFilters) => setParams((prev) => ({ ...prev, ...newFilters }))}
              onChange={debouncedReload}
            />
            <AccountTableActions
              loading={loading}
              onRefresh={() => void handleManualRefresh()}
              onCreate={() => undefined}
              after={
                <>
                  {/* Auto Refresh Dropdown */}
                  <div className="relative" ref={autoRefreshDropdownRef}>
                    <button
                      onClick={() => {
                        setShowAutoRefreshDropdown((prev) => !prev)
                        setShowAccountToolsDropdown(false)
                      }}
                      className="btn btn-secondary px-2 md:px-3"
                      title={t('admin.accounts.autoRefresh')}
                    >
                      <Icon name="refresh" size="sm" className={autoRefreshEnabled ? 'animate-spin' : ''} />
                      <span className="hidden md:inline">
                        {autoRefreshEnabled
                          ? t('admin.accounts.autoRefreshCountdown', { seconds: autoRefreshCountdown })
                          : t('admin.accounts.autoRefresh')}
                      </span>
                    </button>
                    {showAutoRefreshDropdown && (
                      <div className="absolute right-0 z-50 mt-2 w-56 origin-top-right rounded-lg border border-gray-200 shadow-lg dark:border-dark-700 surface-panel">
                        <div className="p-2">
                          <button
                            className="flex w-full items-center justify-between rounded-md px-3 py-2 text-sm hover:surface-hover surface-control surface-muted"
                            onClick={() => setAutoRefreshEnabledFn(!autoRefreshEnabled)}
                          >
                            <span>{t('admin.accounts.enableAutoRefresh')}</span>
                            {autoRefreshEnabled && <Icon name="check" size="sm" className="text-primary-500" />}
                          </button>
                          <div className="my-1 border-t border-gray-100 dark:border-dark-700" />
                          {autoRefreshIntervals.map((sec) => (
                            <button
                              key={sec}
                              className="flex w-full items-center justify-between rounded-md px-3 py-2 text-sm hover:surface-hover surface-control surface-muted"
                              onClick={() => setAutoRefreshIntervalFn(sec)}
                            >
                              <span>{autoRefreshIntervalLabel(sec)}</span>
                              {autoRefreshIntervalSeconds === sec && (
                                <Icon name="check" size="sm" className="text-primary-500" />
                              )}
                            </button>
                          ))}
                        </div>
                      </div>
                    )}
                  </div>

                  {/* More Tools Dropdown */}
                  <div className="relative">
                    <button
                      ref={accountToolsTriggerRef}
                      onClick={toggleAccountToolsDropdown}
                      className="btn btn-secondary px-2 md:px-3"
                      title={t('admin.accounts.moreActions')}
                      aria-expanded={showAccountToolsDropdown}
                    >
                      <Icon name="more" size="sm" className="md:mr-1.5" />
                      <span className="hidden md:inline">{t('admin.accounts.moreActions')}</span>
                      <Icon name="chevronDown" size="xs" className="ml-1 hidden md:inline" />
                    </button>
                    {showAccountToolsDropdown &&
                      createPortal(
                        <div
                          ref={accountToolsDropdownRef}
                          className="fixed z-[9999] origin-top-right overflow-hidden rounded-lg border border-gray-200 shadow-xl dark:border-dark-700 surface-panel"
                          style={accountToolsDropdownStyle}
                          onClick={(event) => event.stopPropagation()}
                        >
                          <div
                            className="overflow-y-auto p-2"
                            style={{ maxHeight: `${accountToolsDropdownPosition.maxHeight}px` }}
                          >
                            <div className="px-2 py-2">
                              <div className="text-xs font-semibold uppercase tracking-wide surface-muted">
                                {t('admin.accounts.dataActions')}
                              </div>
                            </div>
                            <button
                              className="account-tools-menu-item surface-control"
                              onClick={() => {
                                setShowAccountToolsDropdown(false)
                                setShowSync(true)
                              }}
                            >
                              <span className="account-tools-menu-icon bg-blue-50/[0.15] text-blue-600 dark:bg-blue-900/30 dark:text-blue-300">
                                <Icon name="sync" size="sm" />
                              </span>
                              <span className="flex-1 text-left">{t('admin.accounts.syncFromCrs')}</span>
                            </button>
                            <button
                              className="account-tools-menu-item surface-control"
                              onClick={() => {
                                setShowAccountToolsDropdown(false)
                                setShowImportData(true)
                              }}
                            >
                              <span className="account-tools-menu-icon bg-emerald-50/[0.15] text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-300">
                                <Icon name="upload" size="sm" />
                              </span>
                              <span className="flex-1 text-left">{t('admin.accounts.dataImport')}</span>
                            </button>
                            <button
                              className="account-tools-menu-item surface-control"
                              onClick={() => {
                                setShowAccountToolsDropdown(false)
                                void openExportDataDialog()
                              }}
                            >
                              <span className="account-tools-menu-icon bg-violet-50/[0.15] text-violet-600 dark:bg-violet-900/30 dark:text-violet-300">
                                <Icon name="download" size="sm" />
                              </span>
                              <span className="flex-1 text-left">
                                {selIds.length ? t('admin.accounts.dataExportSelected') : t('admin.accounts.dataExport')}
                              </span>
                              {selIds.length > 0 && (
                                <span className="rounded-full bg-primary-100/[0.15] px-2 py-0.5 text-xs font-medium text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">
                                  {t('admin.accounts.selectedCount', { count: selIds.length })}
                                </span>
                              )}
                            </button>

                            <div className="my-2 border-t border-gray-100 dark:border-dark-700" />
                            <div className="px-2 py-2">
                              <div className="text-xs font-semibold uppercase tracking-wide surface-muted">
                                {t('admin.accounts.viewColumns')}
                              </div>
                            </div>
                            <div className="grid grid-cols-1 gap-1">
                              {toggleableColumns.map((col) => (
                                <button
                                  key={col.key}
                                  className="flex w-full items-center justify-between rounded-md px-3 py-2 text-sm transition-colors hover:surface-hover surface-control surface-muted"
                                  onClick={() => toggleColumn(col.key)}
                                >
                                  <span className="truncate">{col.label}</span>
                                  {isColumnVisible(col.key) && <Icon name="check" size="sm" className="text-primary-500" />}
                                </button>
                              ))}
                            </div>
                          </div>
                        </div>,
                        document.body
                      )}
                  </div>
                </>
              }
            />
          </div>
          {hasPendingListSync && (
            <div className="mt-2 flex items-center justify-between rounded-lg border border-amber-200 bg-amber-50/[0.15] px-3 py-2 text-sm text-amber-800 dark:border-amber-700/40 dark:bg-amber-900/20 dark:text-amber-200">
              <span>{t('admin.accounts.listPendingSyncHint')}</span>
              <button className="btn btn-secondary px-2 py-1 text-xs" onClick={() => void syncPendingListChanges()}>
                {t('admin.accounts.listPendingSyncAction')}
              </button>
            </div>
          )}
          {/* Bulk actions belong to the filter card, below its controls. */}
          <AccountBulkActionsBar
            selectedIds={selIds}
            totalResults={pagination.total}
            selectingAll={selectingAllResults}
            allResultsSelected={allResultsSelected}
            onDelete={() => void handleBulkDelete()}
            onResetStatus={() => void handleBulkResetStatus()}
            onRefreshToken={() => void handleBulkRefreshToken()}
            onClear={clearSelection}
            onSelectPage={selectPage}
            onSelectAllResults={() => void handleSelectAllResults()}
            onToggleSchedulable={(schedulable) => void handleBulkToggleSchedulable(schedulable)}
          />
        </div>
      }
      table={
        <AdminDataTable<Account>
          columns={cols}
          data={accounts}
          loading={loading}
          rowKey="id"
          selectable
          selectedKeys={selIds}
          onSelectedKeysChange={(keys) =>
            setSelectedIds(keys.map((key) => Number(key)).filter((key) => Number.isFinite(key)))
          }
          sortKey={params.sort_by as string}
          sortOrder={params.sort_order as SortOrder}
          onSort={handleSort}
          renderCell={renderCell}
          renderHeader={renderHeader}
        />
      }
      pagination={
        pagination.total > 0 ? (
          <Pagination
            page={pagination.page}
            total={pagination.total}
            pageSize={pagination.page_size}
            onPageChange={handlePageChange}
            onPageSizeChange={handlePageSizeChange}
          />
        ) : null
      }
    />

    <AccountActionMenu
      show={menu.show}
      account={menu.acc}
      anchorRect={menu.anchorRect}
      onClose={() => setMenu((prev) => ({ ...prev, show: false }))}
      onTest={(a) => void handleTest(a)}
      onStats={(a) => void handleViewStats(a)}
      onSchedule={(a) => void handleSchedule(a)}
      onUsageAlert={handleUsageAlert}
      onDuplicate={(a) => void handleDuplicateAccount(a)}
      onReauth={(a) => void handleReAuth(a)}
      onRefreshToken={(a) => void handleRefresh(a)}
      onRecoverState={(a) => void handleRecoverState(a)}
      onResetQuota={(a) => void handleResetQuota(a)}
      onSetPrivacy={(a) => void handleSetPrivacy(a)}
      onCreateSparkShadow={handleCreateSparkShadow}
    />

    <SyncFromCrsModal show={showSync} onClose={() => setShowSync(false)} onSynced={() => void reload()} />
    <ImportDataModal show={showImportData} onClose={() => setShowImportData(false)} onImported={handleDataImported} />
    <TempUnschedStatusModal
      show={showTempUnsched}
      account={tempUnschedAcc}
      onClose={() => setShowTempUnsched(false)}
      onReset={(updated) => void handleTempUnschedReset(updated)}
    />
    <ReAuthAccountModal
      show={showReAuth}
      account={reAuthAcc}
      onClose={() => {
        setShowReAuth(false)
        setReAuthAcc(null)
      }}
      onReauthorized={() => {
        if (reAuthAcc) handleAccountUpdated(reAuthAcc)
      }}
    />
    <AccountTestModal
      show={showTest}
      account={testingAcc}
      onClose={() => {
        setShowTest(false)
        setTestingAcc(null)
      }}
    />
    <AccountStatsModal
      show={showStats}
      account={statsAcc}
      onClose={() => {
        setShowStats(false)
        setStatsAcc(null)
      }}
    />
    <ScheduledTestsPanel
      show={showSchedulePanel}
      accountId={scheduleAcc?.id ?? null}
      modelOptions={scheduleModelOptions}
      onClose={() => {
        setShowSchedulePanel(false)
        setScheduleAcc(null)
        setScheduleModelOptions([])
      }}
    />
    <UsageAlertPanel
      show={showUsageAlertPanel}
      accountId={usageAlertAcc?.id ?? null}
      onClose={() => {
        setShowUsageAlertPanel(false)
        setUsageAlertAcc(null)
      }}
    />
    <ConfirmDialog
      open={showDeleteDialog}
      title={t('admin.accounts.deleteAccount')}
      message={t('admin.accounts.deleteConfirm', { name: deletingAcc?.name })}
      confirmText={t('common.delete')}
      cancelText={t('common.cancel')}
      danger
      onConfirm={() => void confirmDelete()}
      onCancel={() => setShowDeleteDialog(false)}
    />
    <ConfirmDialog
      open={showCreateShadowDialog}
      title={t('admin.accounts.createSparkShadow')}
      message={t('admin.accounts.createSparkShadowConfirm', { name: creatingShadowAcc?.name })}
      onConfirm={() => void confirmCreateSparkShadow()}
      onCancel={() => setShowCreateShadowDialog(false)}
    />

    {/* Export data dialog */}
    {showExportDataDialog && (
      <Modal
        open={showExportDataDialog}
        onClose={closeExportDataDialog}
        title={t('admin.accounts.dataExport')}
        size="sm"
        footer={
          <div className="flex justify-end gap-3">
            <button type="button" className="btn btn-secondary" disabled={exportingData} onClick={closeExportDataDialog}>
              {t('common.cancel')}
            </button>
            <button
              type="submit"
              form="account-export-form"
              className="btn btn-primary"
              disabled={exportingData || !exportPassword || (exportStepUpRequired && !exportTotpCode)}
            >
              {exportingData ? t('admin.accounts.dataExporting') : t('admin.accounts.dataExportConfirm')}
            </button>
          </div>
        }
      >
        <form
          id="account-export-form"
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            void handleExportData()
          }}
        >
          <p className="text-sm surface-muted">{t('admin.accounts.dataExportConfirmMessage')}</p>
          <label className="flex items-center gap-2 text-sm surface-muted">
            <input
              checked={includeProxyOnExport}
              onChange={(event) => setIncludeProxyOnExport(event.target.checked)}
              type="checkbox"
              className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
            />
            <span>{t('admin.accounts.dataExportIncludeProxies')}</span>
          </label>
          <div>
            <label htmlFor="account-export-password" className="input-label">
              {t('admin.accounts.dataExportPassword')}
            </label>
            <input
              id="account-export-password"
              value={exportPassword}
              onChange={(event) => setExportPassword(event.target.value)}
              type="password"
              autoComplete="current-password"
              className="input w-full"
              disabled={exportingData}
              required
            />
          </div>
          {exportStepUpRequired && (
            <div>
              <label htmlFor="account-export-totp" className="input-label">
                {t('admin.accounts.dataExportTotpCode')}
              </label>
              <input
                id="account-export-totp"
                value={exportTotpCode}
                onChange={(event) => setExportTotpCode(event.target.value)}
                type="text"
                inputMode="numeric"
                autoComplete="one-time-code"
                maxLength={6}
                className="input w-full"
                disabled={exportingData}
                required
              />
            </div>
          )}
        </form>
      </Modal>
    )}
  )
    </>
  )
}
