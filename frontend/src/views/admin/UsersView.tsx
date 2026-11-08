import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { createPortal } from 'react-dom'
import {
  Ban,
  ChartBarBig,
  Check,
  CheckCircle,
  Columns3,
  Copy,
  DollarSign,
  Filter,
  Globe,
  KeyRound,
  ListChecks,
  MoreHorizontal,
  Plus,
  RefreshCw,
  Search,
  Settings,
  Shield,
  Trash2,
  UserRound,
  Users
} from 'lucide-react'
import { adminAPI } from '@/api/admin'
import type { AdminGroup, AdminUser, UserAttributeDefinition } from '@/types'
import type { BatchUserUsageStats } from '@/api/admin/dashboard'
import type { PlatformQuotaItem } from '@/api/admin/users'
import { formatDateTime } from '@/utils/format'
import { useAppStore } from '@/stores/app'
import { Button, ConfirmDialog, EmptyState, Input, Pagination, Select } from '@/components/ui'
import { AdminDataTable, type Column } from './AdminDataTable'
import AdminTablePageLayout from './AdminTablePageLayout'
import { buildApiKeyGroupFilterOptions } from './apiKeyGroupFilterOptions'
import { GroupBadge } from './GroupBadge'
import {
  PlatformCostCell,
  PlatformUsageBreakdown,
  UserConcurrencyCell,
  UserPlatformQuotaCell
} from './UserCells'
import {
  BulkEditUserModal,
  GroupReplaceModal,
  UserAllowedGroupsModal,
  UserApiKeysModal,
  UserBalanceHistoryModal,
  UserBalanceModal,
  UserCreateModal,
  UserEditModal,
  UserPlatformQuotaModal
} from './UserModals'
import UserAttributesConfigModal from './UserAttributesConfigModal'

const HIDDEN_COLUMNS_KEY = 'user-hidden-columns'
// 列设置 schema 版本号。每次给 DEFAULT_HIDDEN_COLUMNS 新增列时 bump 一次，
// 并在 VERSION_NEW_HIDDEN_COLUMNS 中登记该版本新增的 key。
// 这样老用户升级后这些新列会被自动隐藏一次，而不会影响他们对其它老列的偏好。
const COLUMN_SETTINGS_VERSION_KEY = 'user-column-settings-version'
const COLUMN_SETTINGS_VERSION = 3
const VERSION_NEW_HIDDEN_COLUMNS: Record<number, string[]> = {
  2: ['usage_anthropic', 'usage_openai', 'usage_gemini', 'usage_antigravity'],
  3: ['balance_platform_quota']
}

const DEFAULT_HIDDEN_COLUMNS = [
  'notes', 'groups', 'subscriptions', 'usage', 'concurrency',
  'usage_anthropic', 'usage_openai', 'usage_gemini', 'usage_antigravity',
  'balance_platform_quota'
]
const REMOVED_COLUMNS = new Set(['last_login_at'])
// 强制可见列：加载时会被强制移出 hiddenColumns，并在列设置 UI 上 disabled。
// 当前没有列需要强制可见 —— last_active_at 已改为可被用户隐藏。
const FORCED_VISIBLE_COLUMNS = new Set<string>()

const FILTER_VALUES_KEY = 'user-filter-values'
const VISIBLE_FILTERS_KEY = 'user-visible-filters'

const USER_SORT_STORAGE_KEY = 'admin-users-table-sort'
const USAGE_SORT_STORAGE_KEY = 'admin-users-usage-sort'

// 列 key → 平台名（'usage' 主列汇总所有平台时为 null）
// 显式数组取代 Object.keys()：保证迭代顺序（决定列头排序按钮渲染顺序）
// 不会因 JS 引擎差异或 USAGE_COLUMN_PLATFORMS 属性顺序调整而静默变化。
const USAGE_COLUMN_KEYS: readonly string[] = ['usage', 'usage_anthropic', 'usage_openai', 'usage_gemini', 'usage_antigravity']
const USAGE_COLUMN_PLATFORMS: Record<string, string | null> = {
  usage: null,
  usage_anthropic: 'anthropic',
  usage_openai: 'openai',
  usage_gemini: 'gemini',
  usage_antigravity: 'antigravity'
}
const PLATFORM_USAGE_COLUMNS = USAGE_COLUMN_KEYS.filter((k) => k !== 'usage')

const SORTABLE_COLUMNS = new Set(['email', 'id', 'username', 'role', 'balance', 'concurrency', 'status', 'last_used_at', 'last_active_at', 'created_at'])

type UsageMetric = 'today' | 'total'
type UsageSortState = { key: string; metric: UsageMetric; order: 'asc' | 'desc' } | null

function loadSavedColumns(): Set<string> {
  const hidden = new Set<string>()
  try {
    const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
    if (saved) {
      const parsed = JSON.parse(saved) as string[]
      parsed
        .filter((key) => !REMOVED_COLUMNS.has(key) && !FORCED_VISIBLE_COLUMNS.has(key))
        .forEach((key) => hidden.add(key))

      // 老用户升级：把每个未应用过的版本里新增的默认隐藏列自动追加到 hiddenColumns。
      const storedVersion = Number(localStorage.getItem(COLUMN_SETTINGS_VERSION_KEY) ?? '1')
      if (storedVersion < COLUMN_SETTINGS_VERSION) {
        let mutated = false
        for (let v = storedVersion + 1; v <= COLUMN_SETTINGS_VERSION; v += 1) {
          for (const key of VERSION_NEW_HIDDEN_COLUMNS[v] ?? []) {
            if (REMOVED_COLUMNS.has(key) || FORCED_VISIBLE_COLUMNS.has(key)) continue
            if (!hidden.has(key)) {
              hidden.add(key)
              mutated = true
            }
          }
        }
        if (mutated) saveColumnsToStorage(hidden)
        else localStorage.setItem(COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
      }
    } else {
      // Use default hidden columns on first load
      DEFAULT_HIDDEN_COLUMNS.forEach((key) => hidden.add(key))
      localStorage.setItem(COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
    }
  } catch (e) {
    console.error('Failed to load saved columns:', e)
    DEFAULT_HIDDEN_COLUMNS.forEach((key) => hidden.add(key))
  }
  return hidden
}

function saveColumnsToStorage(hidden: Set<string>) {
  try {
    localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hidden]))
    localStorage.setItem(COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
  } catch (e) {
    console.error('Failed to save columns:', e)
  }
}

function loadInitialSortState(): { sort_by: string; sort_order: 'asc' | 'desc' } {
  const fallback = { sort_by: 'created_at', sort_order: 'desc' as 'asc' | 'desc' }
  try {
    const raw = localStorage.getItem(USER_SORT_STORAGE_KEY)
    if (!raw) return fallback
    const parsed = JSON.parse(raw) as { key?: string; order?: string }
    const key = typeof parsed.key === 'string' ? parsed.key : ''
    if (!SORTABLE_COLUMNS.has(key)) return fallback
    return { sort_by: key, sort_order: parsed.order === 'asc' ? 'asc' : 'desc' }
  } catch {
    return fallback
  }
}

function loadInitialUsageSort(): UsageSortState {
  try {
    const raw = localStorage.getItem(USAGE_SORT_STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<{ key: string; metric: string; order: string }>
    if (!parsed.key || !USAGE_COLUMN_KEYS.includes(parsed.key)) return null
    const metric: UsageMetric = parsed.metric === 'total' ? 'total' : 'today'
    const order: 'asc' | 'desc' = parsed.order === 'asc' ? 'asc' : 'desc'
    return { key: parsed.key, metric, order }
  } catch {
    return null
  }
}

interface UserFilters {
  role: string
  status: string
  group: string
  apiKeyGroup: number | null
}

const EMPTY_FILTERS: UserFilters = { role: '', status: '', group: '', apiKeyGroup: null }

export default function UsersView() {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)

  const [users, setUsers] = useState<AdminUser[]>([])
  const [loading, setLoading] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [sortState, setSortState] = useState(loadInitialSortState)
  const [usageSort, setUsageSort] = useState<UsageSortState>(loadInitialUsageSort)
  const [openUsageSortMenu, setOpenUsageSortMenu] = useState<string | null>(null)
  const [hiddenColumns, setHiddenColumns] = useState<Set<string>>(loadSavedColumns)
  const [filters, setFilters] = useState<UserFilters>(EMPTY_FILTERS)
  const [activeAttributeFilters, setActiveAttributeFilters] = useState<Record<number, string>>({})
  const [visibleFilters, setVisibleFilters] = useState<Set<string>>(() => {
    try {
      const saved = localStorage.getItem(VISIBLE_FILTERS_KEY)
      if (saved) return new Set(JSON.parse(saved) as string[])
    } catch (e) {
      console.error('Failed to load saved filters:', e)
    }
    return new Set()
  })
  const [showFilterDropdown, setShowFilterDropdown] = useState(false)
  const [showColumnDropdown, setShowColumnDropdown] = useState(false)
  const filterDropdownRef = useRef<HTMLDivElement>(null)
  const columnDropdownRef = useRef<HTMLDivElement>(null)

  const [allGroups, setAllGroups] = useState<AdminGroup[]>([])
  const [allGroupsForApiKeyFilter, setAllGroupsForApiKeyFilter] = useState<AdminGroup[]>([])

  const [attributeDefinitions, setAttributeDefinitions] = useState<UserAttributeDefinition[]>([])
  const [userAttributeValues, setUserAttributeValues] = useState<Record<number, Record<number, string>>>({})
  const [usageStats, setUsageStats] = useState<Record<string, BatchUserUsageStats>>({})
  const [platformQuotaStats, setPlatformQuotaStats] = useState<Record<number, PlatformQuotaItem[]>>({})
  const [pagination, setPagination] = useState({ page: 1, page_size: 20, total: 0, pages: 0 })

  const [selectedIds, setSelectedIds] = useState<Array<string | number>>([])
  const [activeMenuId, setActiveMenuId] = useState<number | null>(null)
  const [menuPosition, setMenuPosition] = useState<{ top: number; left: number } | null>(null)
  const [expandedGroupUserId, setExpandedGroupUserId] = useState<number | null>(null)

  const [showCreateModal, setShowCreateModal] = useState(false)
  const [showEditModal, setShowEditModal] = useState(false)
  const [showBulkEditModal, setShowBulkEditModal] = useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [bulkDeleteIds, setBulkDeleteIds] = useState<number[]>([])
  const [bulkDeleting, setBulkDeleting] = useState(false)
  const [showApiKeysModal, setShowApiKeysModal] = useState(false)
  const [showAttributesModal, setShowAttributesModal] = useState(false)
  const [showPlatformQuotaModal, setShowPlatformQuotaModal] = useState(false)
  const [showAllowedGroupsModal, setShowAllowedGroupsModal] = useState(false)
  const [showGroupReplaceModal, setShowGroupReplaceModal] = useState(false)
  const [showBalanceModal, setShowBalanceModal] = useState(false)
  const [showBalanceHistoryModal, setShowBalanceHistoryModal] = useState(false)
  const [editingUser, setEditingUser] = useState<AdminUser | null>(null)
  const [deletingUser, setDeletingUser] = useState<AdminUser | null>(null)
  const [viewingUser, setViewingUser] = useState<AdminUser | null>(null)
  const [platformQuotaUser, setPlatformQuotaUser] = useState<AdminUser | null>(null)
  const [allowedGroupsUser, setAllowedGroupsUser] = useState<AdminUser | null>(null)
  const [groupReplaceUser, setGroupReplaceUser] = useState<AdminUser | null>(null)
  const [groupReplaceOldGroup, setGroupReplaceOldGroup] = useState<{ id: number; name: string } | null>(null)
  const [balanceUser, setBalanceUser] = useState<AdminUser | null>(null)
  const [balanceOperation, setBalanceOperation] = useState<'add' | 'subtract'>('add')
  const [balanceHistoryUser, setBalanceHistoryUser] = useState<AdminUser | null>(null)

  const abortController = useRef<AbortController | null>(null)
  const secondaryDataSeq = useRef(0)
  const searchTimeout = useRef<ReturnType<typeof setTimeout> | null>(null)
  const allGroupsLoaded = useRef(false)
  const apiKeyGroupsLoaded = useRef(false)

  const hasVisibleUsageColumn = useMemo(
    () => !hiddenColumns.has('usage') || PLATFORM_USAGE_COLUMNS.some((k) => !hiddenColumns.has(k)),
    [hiddenColumns]
  )
  const hasVisibleGroupsColumn = !hiddenColumns.has('groups')
  const hasVisiblePlatformQuotaColumn = !hiddenColumns.has('balance_platform_quota')
  const hasVisibleAttributeColumns = useMemo(
    () => attributeDefinitions.some((def) => def.enabled && !hiddenColumns.has(`attr_${def.id}`)),
    [attributeDefinitions, hiddenColumns]
  )

  const loadAllGroups = useCallback(async () => {
    if (allGroupsLoaded.current) return
    allGroupsLoaded.current = true
    try {
      setAllGroups(await adminAPI.groups.getAll())
    } catch (e) {
      allGroupsLoaded.current = false
      console.error('Failed to load groups:', e)
    }
  }, [])

  const loadAllGroupsForApiKeyFilter = useCallback(async () => {
    if (apiKeyGroupsLoaded.current) return
    apiKeyGroupsLoaded.current = true
    try {
      setAllGroupsForApiKeyFilter(await adminAPI.groups.getAllIncludingInactive())
    } catch (e) {
      apiKeyGroupsLoaded.current = false
      console.error('Failed to load groups for API key filter:', e)
    }
  }, [])

  const loadUsersSecondaryData = useCallback(
    async (userIds: number[], signal?: AbortSignal, expectedSeq?: number) => {
      if (userIds.length === 0) return

      const tasks: Promise<void>[] = []

      if (hasVisibleUsageColumn) {
        tasks.push(
          (async () => {
            try {
              const usageResponse = await adminAPI.dashboard.getBatchUsersUsage(userIds)
              if (signal?.aborted) return
              if (typeof expectedSeq === 'number' && expectedSeq !== secondaryDataSeq.current) return
              setUsageStats(usageResponse.stats)
            } catch (e) {
              if (signal?.aborted) return
              console.error('Failed to load usage stats:', e)
            }
          })()
        )
      }

      if (attributeDefinitions.length > 0 && hasVisibleAttributeColumns) {
        tasks.push(
          (async () => {
            try {
              const attrResponse = await adminAPI.userAttributes.getBatchUserAttributes(userIds)
              if (signal?.aborted) return
              if (typeof expectedSeq === 'number' && expectedSeq !== secondaryDataSeq.current) return
              setUserAttributeValues(attrResponse.attributes)
            } catch (e) {
              if (signal?.aborted) return
              console.error('Failed to load user attribute values:', e)
            }
          })()
        )
      }

      if (hasVisiblePlatformQuotaColumn) {
        tasks.push(
          (async () => {
            try {
              // 无批量端点：对当前页用户逐个拉取，分块并发（每批 6），批间检查中止条件，避免大 pageSize 时请求洪峰
              const CHUNK = 6
              for (let i = 0; i < userIds.length; i += CHUNK) {
                if (signal?.aborted) return
                if (typeof expectedSeq === 'number' && expectedSeq !== secondaryDataSeq.current) return
                const chunk = userIds.slice(i, i + CHUNK)
                const results = await Promise.allSettled(chunk.map((id) => adminAPI.users.getPlatformQuotas(id)))
                if (signal?.aborted) return
                if (typeof expectedSeq === 'number' && expectedSeq !== secondaryDataSeq.current) return
                setPlatformQuotaStats((prev) => {
                  const merged = { ...prev }
                  results.forEach((r, idx) => {
                    if (r.status === 'fulfilled') {
                      merged[chunk[idx]] = r.value.platform_quotas || []
                    }
                  })
                  return merged
                })
              }
            } catch (e) {
              if (signal?.aborted) return
              console.error('Failed to load platform quotas:', e)
            }
          })()
        )
      }

      if (tasks.length > 0) {
        await Promise.allSettled(tasks)
      }
    },
    [attributeDefinitions.length, hasVisibleAttributeColumns, hasVisiblePlatformQuotaColumn, hasVisibleUsageColumn]
  )

  // Latest-state refs so every entry point (mount, filter, page, sort) shares one load path.
  const queryRef = useRef({ pagination, filters, searchQuery, sortState, activeAttributeFilters })
  queryRef.current = { pagination, filters, searchQuery, sortState, activeAttributeFilters }

  const loadUsers = useCallback(async () => {
    abortController.current?.abort()
    const controller = new AbortController()
    abortController.current = controller
    const { signal } = controller
    const { pagination: page, filters: f, searchQuery: search, sortState: sort, activeAttributeFilters: attrs } = queryRef.current
    setLoading(true)
    try {
      // Build attribute filters from active filters
      const attrFilters: Record<number, string> = {}
      for (const [attrId, value] of Object.entries(attrs)) {
        if (value) attrFilters[Number(attrId)] = value
      }

      const response = await adminAPI.users.list(
        page.page,
        page.page_size,
        {
          role: f.role as 'user' | 'admin' | undefined,
          status: f.status as 'active' | 'disabled' | undefined,
          search: search || undefined,
          group_name: f.group || undefined,
          api_key_group_id: f.apiKeyGroup ?? undefined,
          attributes: Object.keys(attrFilters).length > 0 ? attrFilters : undefined,
          // 始终请求 subscriptions：列隐藏时仍需用于 UserPlatformQuotaModal 的 active-subscription 警示 banner
          include_subscriptions: true,
          sort_by: sort.sort_by,
          sort_order: sort.sort_order
        },
        { signal }
      )
      if (signal.aborted) return
      setUsers(response.items)
      setPagination((prev) => ({ ...prev, total: response.total, pages: response.pages }))
      setUsageStats({})
      setUserAttributeValues({})
      setPlatformQuotaStats({})

      // Defer heavy secondary data so table can render first.
      if (response.items.length > 0) {
        const userIds = response.items.map((u) => u.id)
        const seq = (secondaryDataSeq.current += 1)
        window.setTimeout(() => {
          if (signal.aborted || seq !== secondaryDataSeq.current) return
          void loadUsersSecondaryData(userIds, signal, seq)
        }, 50)
      }
    } catch (error) {
      const errorInfo = error as {
        name?: string
        code?: string
        response?: { data?: { detail?: string } }
        message?: string
      }
      if (errorInfo?.name === 'AbortError' || errorInfo?.name === 'CanceledError' || errorInfo?.code === 'ERR_CANCELED') {
        return
      }
      const message = errorInfo.response?.data?.detail || errorInfo.message || t('admin.users.failedToLoad')
      showError(message)
      console.error('Error loading users:', error)
    } finally {
      if (abortController.current === controller) {
        setLoading(false)
      }
    }
  }, [loadUsersSecondaryData, showError, t])

  const refreshCurrentPageSecondaryData = useCallback(() => {
    const userIds = users.map((u) => u.id)
    if (userIds.length === 0) return
    const seq = (secondaryDataSeq.current += 1)
    void loadUsersSecondaryData(userIds, undefined, seq)
  }, [users, loadUsersSecondaryData])

  const [reloadKey, setReloadKey] = useState(0)

  // 首屏加载：加载属性定义、恢复持久化的筛选值后拉取用户列表。
  useEffect(() => {
    const mount = async () => {
      try {
        setAttributeDefinitions(await adminAPI.userAttributes.listEnabledDefinitions())
      } catch (e) {
        console.error('Failed to load attribute definitions:', e)
      }
      // Load saved filter values
      try {
        const savedValues = localStorage.getItem(FILTER_VALUES_KEY)
        if (savedValues) {
          const parsed = JSON.parse(savedValues)
          setFilters((prev) => ({
            role: parsed.role || prev.role,
            status: parsed.status || prev.status,
            group: parsed.group || prev.group,
            apiKeyGroup: typeof parsed.apiKeyGroup === 'number' ? parsed.apiKeyGroup : prev.apiKeyGroup
          }))
          if (parsed.attributes) setActiveAttributeFilters({ ...parsed.attributes })
        }
      } catch (e) {
        console.error('Failed to load saved filters:', e)
      }
      void loadUsers()
    }
    void mount()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Lazy-load the group catalogs the visible columns / filters need.
  useEffect(() => {
    if (hasVisibleGroupsColumn || visibleFilters.has('group')) void loadAllGroups()
  }, [hasVisibleGroupsColumn, visibleFilters, loadAllGroups])

  useEffect(() => {
    if (visibleFilters.has('apiKeyGroup')) void loadAllGroupsForApiKeyFilter()
  }, [visibleFilters, loadAllGroupsForApiKeyFilter])

  useEffect(() => {
    void loadUsers()
  }, [loadUsers, reloadKey])

  // Close menus when clicking outside / scrolling.
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as HTMLElement
      if (!target.closest('.action-menu-trigger') && !target.closest('.action-menu-content')) {
        setActiveMenuId(null)
        setMenuPosition(null)
      }
      if (filterDropdownRef.current && !filterDropdownRef.current.contains(target)) {
        setShowFilterDropdown(false)
      }
      if (columnDropdownRef.current && !columnDropdownRef.current.contains(target)) {
        setShowColumnDropdown(false)
      }
      if (openUsageSortMenu !== null && !target.closest('.usage-sort-trigger')) {
        setOpenUsageSortMenu(null)
      }
      if (expandedGroupUserId !== null) {
        setExpandedGroupUserId(null)
      }
    }
    // 滚动时关闭菜单
    const handleScroll = () => {
      setActiveMenuId(null)
      setMenuPosition(null)
    }
    document.addEventListener('click', handleClickOutside)
    window.addEventListener('scroll', handleScroll, true)
    return () => {
      document.removeEventListener('click', handleClickOutside)
      window.removeEventListener('scroll', handleScroll, true)
    }
  }, [openUsageSortMenu, expandedGroupUserId])

  useEffect(() => {
    return () => {
      if (searchTimeout.current) clearTimeout(searchTimeout.current)
      abortController.current?.abort()
    }
  }, [])

  const getAttributeDefinition = useCallback(
    (attrId: number): UserAttributeDefinition | undefined => attributeDefinitions.find((d) => d.id === attrId),
    [attributeDefinitions]
  )

  const getAttributeValue = useCallback(
    (userId: number, attrId: number): string => {
      const userAttrs = userAttributeValues[userId]
      if (!userAttrs) return '-'
      const value = userAttrs[attrId]
      if (!value) return '-'

      const def = attributeDefinitions.find((d) => d.id === attrId)
      if (!def) return value

      if (def.type === 'multi_select' && value) {
        try {
          const arr = JSON.parse(value)
          if (Array.isArray(arr)) {
            return arr
              .map((v) => {
                const opt = def.options?.find((o) => o.value === v)
                return opt?.label || v
              })
              .join(', ')
          }
        } catch {
          return value
        }
      }

      if (def.type === 'select' && value && def.options) {
        const opt = def.options.find((o) => o.value === value)
        return opt?.label || value
      }

      return value
    },
    [attributeDefinitions, userAttributeValues]
  )

  const getAttributeDefinitionName = (attrId: number): string => {
    const def = attributeDefinitions.find((d) => d.id === attrId)
    return def?.name || String(attrId)
  }

  const getUserGroups = useCallback(
    (user: AdminUser) => {
      const exclusive: AdminGroup[] = []
      const publicGroups: AdminGroup[] = []
      for (const g of allGroups) {
        if (g.status !== 'active' || g.subscription_type !== 'standard') continue
        if (g.is_exclusive) {
          if (user.allowed_groups?.includes(g.id)) exclusive.push(g)
        } else {
          publicGroups.push(g)
        }
      }
      return { exclusive, publicGroups }
    },
    [allGroups]
  )

  const groupFilterOptions = useMemo(() => {
    const options: { value: string; label: string }[] = [{ value: '', label: t('admin.users.allAuthorizedGroups') }]
    for (const g of allGroups) {
      if (g.status !== 'active' || !g.is_exclusive || g.subscription_type !== 'standard') continue
      options.push({ value: g.name, label: g.name })
    }
    return options
  }, [allGroups, t])

  const apiKeyGroupFilterOptions = useMemo(
    () =>
      buildApiKeyGroupFilterOptions(allGroupsForApiKeyFilter, {
        all: t('admin.users.allApiKeyGroups'),
        exclusive: t('admin.users.apiKeyGroupExclusive'),
        public: t('admin.users.apiKeyGroupPublic'),
        subscription: t('admin.users.apiKeyGroupSubscription'),
        disabled: t('admin.users.apiKeyGroupDisabled')
      }),
    [allGroupsForApiKeyFilter, t]
  )

  const attributeColumns: Column[] = useMemo(
    () =>
      attributeDefinitions
        .filter((def) => def.enabled)
        .map((def) => ({ key: `attr_${def.id}`, label: def.name, sortable: false })),
    [attributeDefinitions]
  )

  const allColumns: Column[] = useMemo(
    () => [
      { key: 'email', label: t('admin.users.columns.user'), sortable: true },
      { key: 'id', label: t('admin.users.columns.id'), sortable: true },
      { key: 'username', label: t('admin.users.columns.username'), sortable: true },
      { key: 'notes', label: t('admin.users.columns.notes'), sortable: false },
      ...attributeColumns,
      { key: 'role', label: t('admin.users.columns.role'), sortable: true },
      { key: 'groups', label: t('admin.users.columns.groups'), sortable: false },
      { key: 'subscriptions', label: t('admin.users.columns.subscriptions'), sortable: false },
      { key: 'balance', label: t('admin.users.columns.balance'), sortable: true },
      { key: 'balance_platform_quota', label: t('admin.users.columns.balancePlatformQuota'), sortable: false },
      { key: 'usage', label: t('admin.users.columns.usage'), sortable: false },
      { key: 'usage_anthropic', label: t('admin.users.columns.usageAnthropic'), sortable: false },
      { key: 'usage_openai', label: t('admin.users.columns.usageOpenAI'), sortable: false },
      { key: 'usage_gemini', label: t('admin.users.columns.usageGemini'), sortable: false },
      { key: 'usage_antigravity', label: t('admin.users.columns.usageAntigravity'), sortable: false },
      { key: 'concurrency', label: t('admin.users.columns.concurrency'), sortable: true },
      { key: 'status', label: t('admin.users.columns.status'), sortable: true },
      { key: 'last_active_at', label: t('admin.users.columns.lastActive'), sortable: true },
      { key: 'last_used_at', label: t('admin.users.columns.lastUsed'), sortable: true },
      { key: 'created_at', label: t('admin.users.columns.created'), sortable: true },
      { key: 'actions', label: t('admin.users.columns.actions'), sortable: false }
    ],
    [attributeColumns, t]
  )

  const toggleableColumns = useMemo(
    () => allColumns.filter((col) => col.key !== 'email' && col.key !== 'actions'),
    [allColumns]
  )

  const columns = useMemo(
    () => allColumns.filter((col) => col.key === 'email' || col.key === 'actions' || !hiddenColumns.has(col.key)),
    [allColumns, hiddenColumns]
  )

  const isColumnVisible = (key: string) => !hiddenColumns.has(key)
  const isForcedVisibleColumn = (key: string) => FORCED_VISIBLE_COLUMNS.has(key)

  const toggleColumn = (key: string) => {
    // 强制可见列(如 last_active_at)在加载时会被恢复成可见，
    // 这里阻止用户在当前会话隐藏它，避免"取消勾选 → 刷新又恢复"的反直觉行为。
    if (FORCED_VISIBLE_COLUMNS.has(key)) return
    const wasHidden = hiddenColumns.has(key)
    setHiddenColumns((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      saveColumnsToStorage(next)
      return next
    })
    if (wasHidden && (key === 'usage' || key.startsWith('usage_') || key.startsWith('attr_') || key === 'balance_platform_quota')) {
      refreshCurrentPageSecondaryData()
    }
    if (key === 'subscriptions') {
      setReloadKey((k) => k + 1)
    }
    if (wasHidden && key === 'groups') {
      void loadAllGroups()
    }
  }

  const builtInFilters = useMemo(
    () => [
      { key: 'role', name: t('admin.users.columns.role'), type: 'select' as const },
      { key: 'status', name: t('admin.users.columns.status'), type: 'select' as const },
      { key: 'group', name: t('admin.users.authorizedGroupFilter'), type: 'select' as const },
      { key: 'apiKeyGroup', name: t('admin.users.apiKeyGroupFilter'), type: 'select' as const }
    ],
    [t]
  )

  const filterableAttributes = useMemo(() => attributeDefinitions.filter((def) => def.enabled), [attributeDefinitions])

  const persistUsageSort = (next: UsageSortState) => {
    try {
      if (next) localStorage.setItem(USAGE_SORT_STORAGE_KEY, JSON.stringify(next))
      else localStorage.removeItem(USAGE_SORT_STORAGE_KEY)
    } catch (e) {
      console.error('Failed to persist usage sort:', e)
    }
  }

  const clearUsageSort = () => {
    if (!usageSort) return
    setUsageSort(null)
    setOpenUsageSortMenu(null)
    persistUsageSort(null)
  }

  const isUsageSortActive = (key: string, metric: UsageMetric) =>
    !!usageSort && usageSort.key === key && usageSort.metric === metric
  const getUsageSortOrder = (key: string, metric: UsageMetric): 'asc' | 'desc' | null =>
    isUsageSortActive(key, metric) ? usageSort!.order : null

  // 三态循环：desc → asc → off。选完即关闭菜单（用户大多希望"选中即应用"，
  // 想再切换 order 时重新打开菜单点同一项即可）。
  const toggleUsageSort = (key: string, metric: UsageMetric) => {
    const cur = usageSort
    let next: UsageSortState
    if (cur && cur.key === key && cur.metric === metric) {
      next = cur.order === 'desc' ? { key, metric, order: 'asc' } : null
    } else {
      next = { key, metric, order: 'desc' }
    }
    setUsageSort(next)
    persistUsageSort(next)
    setOpenUsageSortMenu(null)
  }

  // 点击图标本身不触发排序，仅开关菜单；首次排序由用户在菜单内选择 metric 触发（默认 desc，详见 toggleUsageSort）。
  const toggleUsageSortMenu = (key: string) => {
    setOpenUsageSortMenu((prev) => (prev === key ? null : key))
  }

  const getUsageValue = (userId: number, key: string, metric: UsageMetric): number => {
    const stats = usageStats[userId]
    if (!stats) return 0
    const platform = USAGE_COLUMN_PLATFORMS[key]
    if (platform === null) {
      return metric === 'today' ? stats.today_actual_cost ?? 0 : stats.total_actual_cost ?? 0
    }
    const p = stats.by_platform?.find((x) => x.platform === platform)
    if (!p) return 0
    return metric === 'today' ? p.today_actual_cost ?? 0 : p.total_actual_cost ?? 0
  }

  // 在 server-side 排序结果之上叠加用量列的本地排序；无 usageSort 时直接透传原数组。
  // 稳定排序：等值按原 index 保序，避免拉取新用量数据时表行抖动。
  const sortedUsers = useMemo(() => {
    const s = usageSort
    if (!s) return users
    return [...users]
      .map((row, index) => ({ row, index }))
      .sort((a, b) => {
        const av = getUsageValue(a.row.id, s.key, s.metric)
        const bv = getUsageValue(b.row.id, s.key, s.metric)
        if (av !== bv) return s.order === 'asc' ? av - bv : bv - av
        return a.index - b.index
      })
      .map((x) => x.row)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [users, usageSort, usageStats])

  const selectedCount = selectedIds.length
  const selectedNumbers = selectedIds.filter((key): key is number => typeof key === 'number')

  const handleSelectedKeysUpdate = (keys: Array<string | number>) => {
    // The table stringifies row ids; the rest of this view works in numeric
    // user ids, so convert back rather than dropping every key.
    setSelectedIds(keys.map((key) => Number(key)).filter((key) => Number.isFinite(key)))
  }

  const clearSelection = () => setSelectedIds([])

  const removeSelectedIds = (ids: number[]) => {
    setSelectedIds((prev) => prev.filter((key) => !ids.includes(key as number)))
  }

  const getUserSelectionLabel = (user: AdminUser) => t('admin.users.bulkLimits.selectUser', { email: user.email })

  const handleBulkLimitsSuccess = async () => {
    clearSelection()
    setReloadKey((k) => k + 1)
  }

  const handleSearch = () => {
    if (searchTimeout.current) clearTimeout(searchTimeout.current)
    searchTimeout.current = setTimeout(() => {
      setPagination((prev) => ({ ...prev, page: 1 }))
      setReloadKey((k) => k + 1)
    }, 300)
  }

  const handlePageChange = (page: number) => {
    // 确保页码在有效范围内
    const validPage = Math.max(1, Math.min(page, pagination.pages || 1))
    setPagination((prev) => ({ ...prev, page: validPage }))
    setReloadKey((k) => k + 1)
  }

  const handlePageSizeChange = (pageSize: number) => {
    setPagination((prev) => ({ ...prev, page_size: pageSize, page: 1 }))
    setReloadKey((k) => k + 1)
  }

  const handleSort = (key: string, order: 'asc' | 'desc') => {
    clearUsageSort()
    setSortState({ sort_by: key, sort_order: order })
    setPagination((prev) => ({ ...prev, page: 1 }))
    setReloadKey((k) => k + 1)
  }

  const saveFiltersToStorage = (nextVisible: Set<string>, nextFilters: UserFilters, nextAttrs: Record<number, string>) => {
    try {
      localStorage.setItem(VISIBLE_FILTERS_KEY, JSON.stringify([...nextVisible]))
      const values = {
        role: nextFilters.role,
        status: nextFilters.status,
        group: nextFilters.group,
        apiKeyGroup: nextFilters.apiKeyGroup,
        attributes: nextAttrs
      }
      localStorage.setItem(FILTER_VALUES_KEY, JSON.stringify(values))
    } catch (e) {
      console.error('Failed to save filters:', e)
    }
  }

  // Toggle a built-in filter (role/status)
  const toggleBuiltInFilter = (key: string) => {
    const nextVisible = new Set(visibleFilters)
    let nextFilters = filters
    if (nextVisible.has(key)) {
      nextVisible.delete(key)
      if (key === 'role') nextFilters = { ...nextFilters, role: '' }
      if (key === 'status') nextFilters = { ...nextFilters, status: '' }
      if (key === 'group') nextFilters = { ...nextFilters, group: '' }
      if (key === 'apiKeyGroup') nextFilters = { ...nextFilters, apiKeyGroup: null }
    } else {
      nextVisible.add(key)
      if (key === 'group') void loadAllGroups()
      if (key === 'apiKeyGroup') void loadAllGroupsForApiKeyFilter()
    }
    setVisibleFilters(nextVisible)
    setFilters(nextFilters)
    saveFiltersToStorage(nextVisible, nextFilters, activeAttributeFilters)
    setPagination((prev) => ({ ...prev, page: 1 }))
    setReloadKey((k) => k + 1)
  }

  // Toggle a custom attribute filter
  const toggleAttributeFilter = (attr: UserAttributeDefinition) => {
    const key = `attr_${attr.id}`
    const nextVisible = new Set(visibleFilters)
    let nextAttrs = activeAttributeFilters
    if (nextVisible.has(key)) {
      nextVisible.delete(key)
      nextAttrs = { ...nextAttrs }
      delete nextAttrs[attr.id]
    } else {
      nextVisible.add(key)
      nextAttrs = { ...nextAttrs, [attr.id]: '' }
    }
    setVisibleFilters(nextVisible)
    setActiveAttributeFilters(nextAttrs)
    saveFiltersToStorage(nextVisible, filters, nextAttrs)
    setPagination((prev) => ({ ...prev, page: 1 }))
    setReloadKey((k) => k + 1)
  }

  const updateAttributeFilter = (attrId: number, value: string) => {
    setActiveAttributeFilters((prev) => ({ ...prev, [attrId]: value }))
  }

  // Apply filter and save to localStorage
  const applyFilter = () => {
    saveFiltersToStorage(visibleFilters, filters, activeAttributeFilters)
    setReloadKey((k) => k + 1)
  }

  const handleEdit = (user: AdminUser) => {
    setEditingUser(user)
    setShowEditModal(true)
  }

  const closeEditModal = () => {
    setShowEditModal(false)
    setEditingUser(null)
  }

  const handleToggleStatus = async (user: AdminUser) => {
    const newStatus = user.status === 'active' ? 'disabled' : 'active'
    try {
      await adminAPI.users.toggleStatus(user.id, newStatus)
      showSuccess(newStatus === 'active' ? t('admin.users.userEnabled') : t('admin.users.userDisabled'))
      setReloadKey((k) => k + 1)
    } catch (error) {
      showError(
        (error as { response?: { data?: { detail?: string } } })?.response?.data?.detail || t('admin.users.failedToToggle')
      )
      console.error('Error toggling user status:', error)
    }
  }

  const handleViewApiKeys = (user: AdminUser) => {
    setViewingUser(user)
    setShowApiKeysModal(true)
  }

  const closeApiKeysModal = () => {
    setShowApiKeysModal(false)
    setViewingUser(null)
  }

  const handleAllowedGroups = (user: AdminUser) => {
    setAllowedGroupsUser(user)
    setShowAllowedGroupsModal(true)
  }

  const closeAllowedGroupsModal = () => {
    setShowAllowedGroupsModal(false)
    setAllowedGroupsUser(null)
  }

  const openGroupReplace = (user: AdminUser, group: { id: number; name: string }) => {
    setExpandedGroupUserId(null)
    setGroupReplaceUser(user)
    setGroupReplaceOldGroup(group)
    setShowGroupReplaceModal(true)
  }

  const closeGroupReplaceModal = () => {
    setShowGroupReplaceModal(false)
    setGroupReplaceUser(null)
    setGroupReplaceOldGroup(null)
  }

  const handleDelete = (user: AdminUser) => {
    setDeletingUser(user)
    setShowDeleteDialog(true)
  }

  const confirmDelete = async () => {
    if (!deletingUser) return
    try {
      await adminAPI.users.delete(deletingUser.id)
      showSuccess(t('common.success'))
      setShowDeleteDialog(false)
      setDeletingUser(null)
      setReloadKey((k) => k + 1)
    } catch (error) {
      showError(
        (error as { response?: { data?: { detail?: string } } })?.response?.data?.detail || t('admin.users.failedToDelete')
      )
      console.error('Error deleting user:', error)
    }
  }

  const confirmBulkDelete = async () => {
    const ids = bulkDeleteIds
    setBulkDeleteIds([])
    setBulkDeleting(true)
    const deletedIds: number[] = []
    for (const id of ids) {
      try {
        await adminAPI.users.delete(id)
        deletedIds.push(id)
      } catch (error) {
        console.error('Error deleting user:', error)
      }
    }
    removeSelectedIds(deletedIds)
    if (deletedIds.length > 0) {
      showSuccess(t('admin.users.bulkDelete.success', { count: deletedIds.length }))
      setPagination((prev) => ({ ...prev, page: 1 }))
    }
    const failed = ids.length - deletedIds.length
    if (failed > 0) showError(t('admin.users.bulkDelete.failed', { count: failed }))
    setReloadKey((k) => k + 1)
    setBulkDeleting(false)
  }

  const handleDeposit = (user: AdminUser) => {
    setBalanceUser(user)
    setBalanceOperation('add')
    setShowBalanceModal(true)
  }

  const handleWithdraw = (user: AdminUser) => {
    setBalanceUser(user)
    setBalanceOperation('subtract')
    setShowBalanceModal(true)
  }

  const closeBalanceModal = () => {
    setShowBalanceModal(false)
    setBalanceUser(null)
  }

  const handleBalanceHistory = (user: AdminUser) => {
    setBalanceHistoryUser(user)
    setShowBalanceHistoryModal(true)
  }

  const closeBalanceHistoryModal = () => {
    setShowBalanceHistoryModal(false)
    setBalanceHistoryUser(null)
  }

  // Handle deposit from balance history modal
  const handleDepositFromHistory = () => {
    if (balanceHistoryUser) handleDeposit(balanceHistoryUser)
  }

  // Handle withdraw from balance history modal
  const handleWithdrawFromHistory = () => {
    if (balanceHistoryUser) handleWithdraw(balanceHistoryUser)
  }

  const handlePlatformQuota = (user: AdminUser) => {
    setPlatformQuotaUser(user)
    setShowPlatformQuotaModal(true)
  }

  const closePlatformQuotaModal = () => {
    setShowPlatformQuotaModal(false)
    setPlatformQuotaUser(null)
  }

  const openActionMenu = (user: AdminUser, e: React.MouseEvent) => {
    if (activeMenuId === user.id) {
      closeActionMenu()
      return
    }
    const target = e.currentTarget as HTMLElement
    if (!target) {
      closeActionMenu()
      return
    }

    const rect = target.getBoundingClientRect()
    const menuWidth = 200
    const menuHeight = 240
    const padding = 8
    const viewportWidth = window.innerWidth
    const viewportHeight = window.innerHeight

    let left: number
    let top: number

    if (viewportWidth < 768) {
      // 居中显示,水平位置
      left = Math.max(padding, Math.min(rect.left + rect.width / 2 - menuWidth / 2, viewportWidth - menuWidth - padding))

      // 优先显示在按钮下方
      top = rect.bottom + 4

      // 如果下方空间不够,显示在上方
      if (top + menuHeight > viewportHeight - padding) {
        top = rect.top - menuHeight - 4
        // 如果上方也不够,就贴在视口顶部
        if (top < padding) top = padding
      }
    } else {
      left = Math.max(padding, Math.min(e.clientX - menuWidth, viewportWidth - menuWidth - padding))
      top = e.clientY
      if (top + menuHeight > viewportHeight - padding) {
        top = viewportHeight - menuHeight - padding
      }
    }

    setMenuPosition({ top, left })
    setActiveMenuId(user.id)
  }

  const closeActionMenu = () => {
    setActiveMenuId(null)
    setMenuPosition(null)
  }

  const getPlatformUsage = (userId: number, platform: string) =>
    usageStats[userId]?.by_platform?.find((p) => p.platform === platform)

  const getDaysRemaining = (expiresAt: string): number => {
    const now = new Date()
    const expires = new Date(expiresAt)
    const diffMs = expires.getTime() - now.getTime()
    return Math.ceil(diffMs / (1000 * 60 * 60 * 24))
  }

  const handleAttributesModalClose = async () => {
    setShowAttributesModal(false)
    await loadAttributeDefinitionsSafe()
    setReloadKey((k) => k + 1)
  }

  const loadAttributeDefinitionsSafe = async () => {
    try {
      setAttributeDefinitions(await adminAPI.userAttributes.listEnabledDefinitions())
    } catch (e) {
      console.error('Failed to load attribute definitions:', e)
    }
  }

  const menuUser = activeMenuId !== null ? users.find((user) => user.id === activeMenuId) : null

  return (
    <>
      <AdminTablePageLayout
        filters={
          <div className="flex flex-wrap items-center gap-3">
            <div className="filter-bar flex-1">
              <div className="filter-field filter-field--search relative">
                <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
                <Input
                  value={searchQuery}
                  onValueChange={(value) => {
                    setSearchQuery(value)
                    handleSearch()
                  }}
                  placeholder={t('admin.users.searchUsers')}
                  className="pl-10"
                />
              </div>

              {visibleFilters.has('role') && (
                <div className="filter-field">
                  <Select
                    value={filters.role}
                    options={[
                      { value: '', label: t('admin.users.allRoles') },
                      { value: 'admin', label: t('admin.users.admin') },
                      { value: 'user', label: t('admin.users.user') }
                    ]}
                    onValueChange={(value) => {
                      setFilters((prev) => ({ ...prev, role: String(value ?? '') }))
                      applyFilter()
                    }}
                  />
                </div>
              )}

              {visibleFilters.has('status') && (
                <div className="filter-field">
                  <Select
                    value={filters.status}
                    options={[
                      { value: '', label: t('admin.users.allStatus') },
                      { value: 'active', label: t('common.active') },
                      { value: 'disabled', label: t('admin.users.disabled') }
                    ]}
                    onValueChange={(value) => {
                      setFilters((prev) => ({ ...prev, status: String(value ?? '') }))
                      applyFilter()
                    }}
                  />
                </div>
              )}

              {visibleFilters.has('group') && (
                <div className="filter-field">
                  <Select
                    value={filters.group}
                    options={groupFilterOptions}
                    searchable
                    creatable
                    creatablePrefix={t('admin.users.fuzzySearch')}
                    searchPlaceholder={t('admin.users.searchAuthorizedGroups')}
                    onValueChange={(value) => {
                      setFilters((prev) => ({ ...prev, group: String(value ?? '') }))
                      applyFilter()
                    }}
                  />
                </div>
              )}

              {visibleFilters.has('apiKeyGroup') && (
                <div className="filter-field">
                  <Select
                    value={filters.apiKeyGroup}
                    options={apiKeyGroupFilterOptions as never}
                    searchPlaceholder={t('admin.users.searchApiKeyGroups')}
                    onValueChange={(value) => {
                      setFilters((prev) => ({ ...prev, apiKeyGroup: typeof value === 'number' ? value : null }))
                      applyFilter()
                    }}
                  />
                </div>
              )}

              {Object.entries(activeAttributeFilters).map(([attrId, value]) => {
                if (!visibleFilters.has(`attr_${attrId}`)) return null
                const def = getAttributeDefinition(Number(attrId))
                const type = def?.type || 'text'
                const name = getAttributeDefinitionName(Number(attrId))
                if (['text', 'textarea', 'email', 'url', 'date', 'number'].includes(type)) {
                  return (
                    <div key={attrId} className="filter-field relative">
                      <Input
                        type={type === 'number' ? 'number' : 'text'}
                        value={value}
                        onValueChange={(next) => updateAttributeFilter(Number(attrId), next)}
                        onKeyDown={(event) => {
                          if (event.key === 'Enter') applyFilter()
                        }}
                        placeholder={name}
                      />
                    </div>
                  )
                }
                if (['select', 'multi_select'].includes(type)) {
                  return (
                    <div key={attrId} className="filter-field relative">
                      <Select
                        value={value}
                        options={[{ value: '', label: name }, ...(def?.options || [])]}
                        onValueChange={(next) => {
                          updateAttributeFilter(Number(attrId), String(next ?? ''))
                          applyFilter()
                        }}
                      />
                    </div>
                  )
                }
                return (
                  <div key={attrId} className="filter-field relative">
                    <Input
                      value={value}
                      onValueChange={(next) => updateAttributeFilter(Number(attrId), next)}
                      onKeyDown={(event) => {
                        if (event.key === 'Enter') applyFilter()
                      }}
                      placeholder={name}
                    />
                  </div>
                )
              })}
            </div>

            <div className="flex w-full flex-wrap items-center justify-end gap-2 md:w-auto">
              <div className="flex items-center gap-2 md:contents">
                <Button
                  variant="secondary"
                  size="icon"
                  className="px-2 md:px-3"
                  disabled={loading}
                  onClick={() => setReloadKey((k) => k + 1)}
                  title={t('common.refresh')}
                >
                  <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
                </Button>

                <div className="relative" ref={filterDropdownRef}>
                  <Button
                    variant="secondary"
                    className="px-2 md:px-3"
                    onClick={() => setShowFilterDropdown((prev) => !prev)}
                    title={t('admin.users.filterSettings')}
                  >
                    <Filter className="h-4 w-4 md:mr-1.5" />
                    <span className="hidden md:inline">{t('admin.users.filterSettings')}</span>
                  </Button>
                  {showFilterDropdown && (
                    <div className="absolute right-0 top-full z-50 mt-1 w-48 rounded-lg border border-gray-200 py-1 shadow-lg dark:border-dark-600 surface-panel">
                      {builtInFilters.map((filter) => (
                        <button
                          key={filter.key}
                          onClick={() => toggleBuiltInFilter(filter.key)}
                          className="flex w-full items-center justify-between px-4 py-2 text-left text-sm hover:surface-hover surface-control surface-muted"
                        >
                          <span>{filter.name}</span>
                          {visibleFilters.has(filter.key) && <Check className="h-4 w-4 text-primary-500" strokeWidth={2} />}
                        </button>
                      ))}
                      {filterableAttributes.length > 0 && <div className="my-1 border-t border-gray-100 dark:border-dark-700" />}
                      {filterableAttributes.map((attr) => (
                        <button
                          key={attr.id}
                          onClick={() => toggleAttributeFilter(attr)}
                          className="flex w-full items-center justify-between px-4 py-2 text-left text-sm hover:surface-hover surface-control surface-muted"
                        >
                          <span>{attr.name}</span>
                          {visibleFilters.has(`attr_${attr.id}`) && (
                            <Check className="h-4 w-4 text-primary-500" strokeWidth={2} />
                          )}
                        </button>
                      ))}
                    </div>
                  )}
                </div>

                <div className="relative" ref={columnDropdownRef}>
                  <Button
                    variant="secondary"
                    className="px-2 md:px-3"
                    onClick={() => setShowColumnDropdown((prev) => !prev)}
                    title={t('admin.users.columnSettings')}
                  >
                    <Columns3 className="h-4 w-4 md:mr-1.5" />
                    <span className="hidden md:inline">{t('admin.users.columnSettings')}</span>
                  </Button>
                  {showColumnDropdown && (
                    <div className="absolute right-0 top-full z-50 mt-1 max-h-80 w-48 overflow-y-auto rounded-lg border border-gray-200 py-1 shadow-lg dark:border-dark-600 surface-panel">
                      {toggleableColumns.map((col) => (
                        <button
                          key={col.key}
                          disabled={isForcedVisibleColumn(col.key)}
                          onClick={() => toggleColumn(col.key)}
                          className={`flex w-full items-center justify-between px-4 py-2 text-left text-sm ${
                            isForcedVisibleColumn(col.key)
                              ? 'cursor-not-allowed surface-muted'
                              : 'text-gray-700 hover:surface-hover dark:text-gray-300 dark:hover:surface-hover'
                          }`}
                          title={isForcedVisibleColumn(col.key) ? t('admin.users.columnAlwaysVisible') : ''}
                        >
                          <span>{col.label}</span>
                          {isColumnVisible(col.key) && (
                            <Check
                              className={`h-4 w-4 ${isForcedVisibleColumn(col.key) ? 'surface-muted' : 'text-primary-500'}`}
                              strokeWidth={2}
                            />
                          )}
                        </button>
                      ))}
                    </div>
                  )}
                </div>

                <Button
                  variant="secondary"
                  className="px-2 md:px-3"
                  onClick={() => setShowAttributesModal(true)}
                  title={t('admin.users.attributes.configButton')}
                >
                  <Settings className="h-4 w-4 md:mr-1.5" />
                  <span className="hidden md:inline">{t('admin.users.attributes.configButton')}</span>
                </Button>
              </div>

              {selectedCount > 0 && (
                <Button
                  variant="secondary"
                  className="min-w-max flex-1 whitespace-nowrap md:flex-initial"
                  data-test="bulk-edit-limits"
                  onClick={() => setShowBulkEditModal(true)}
                >
                  <Users className="mr-2 h-4 w-4" />
                  {t('admin.users.bulkLimits.action', { count: selectedCount })}
                </Button>
              )}

              {selectedCount > 0 && (
                <Button
                  variant="danger"
                  className="min-w-max flex-1 whitespace-nowrap md:flex-initial"
                  data-test="bulk-delete-users"
                  disabled={bulkDeleting}
                  onClick={() => setBulkDeleteIds([...selectedNumbers])}
                >
                  <Trash2 className="mr-2 h-4 w-4" />
                  {t('admin.users.bulkDelete.action', { count: selectedCount })}
                </Button>
              )}

              <Button
                variant="primary"
                className="min-w-max flex-1 whitespace-nowrap md:flex-initial"
                onClick={() => setShowCreateModal(true)}
              >
                <Plus className="mr-2 h-4 w-4" />
                {t('admin.users.createUser')}
              </Button>
            </div>
          </div>
        }
        table={
          <AdminDataTable<AdminUser>
            columns={columns}
            data={sortedUsers}
            loading={loading}
            rowKey="id"
            selectable
            selectedKeys={selectedIds}
            selectionLabel={getUserSelectionLabel}
            sortKey={sortState.sort_by}
            sortOrder={sortState.sort_order}
            onSort={handleSort}
            onSelectedKeysChange={handleSelectedKeysUpdate}
            renderHeader={(key, column) => {
              if (USAGE_COLUMN_KEYS.includes(key)) {
                return (
                  <div className="flex items-center gap-1.5">
                    <span>{column.label}</span>
                    <div className="usage-sort-trigger relative">
                      <button
                        type="button"
                        className={`flex items-center gap-1 rounded px-1 py-0.5 transition-colors hover:surface-hover surface-control ${
                          usageSort && usageSort.key === key ? 'text-primary-600 dark:text-primary-400' : 'surface-muted'
                        }`}
                        title={t('admin.users.sortBy')}
                        data-test={`usage-sort-trigger-${key}`}
                        onClick={(event) => {
                          event.stopPropagation()
                          toggleUsageSortMenu(key)
                        }}
                      >
                        {usageSort && usageSort.key === key && (
                          <span className="text-[10px] normal-case font-medium tracking-normal">
                            {usageSort.metric === 'today' ? t('admin.users.today') : t('admin.users.total')}
                          </span>
                        )}
                        {usageSort && usageSort.key === key ? (
                          <svg
                            className={`h-3.5 w-3.5 ${usageSort.order === 'desc' ? 'rotate-180' : ''}`}
                            fill="currentColor"
                            viewBox="0 0 20 20"
                          >
                            <path
                              fillRule="evenodd"
                              d="M14.707 12.707a1 1 0 01-1.414 0L10 9.414l-3.293 3.293a1 1 0 01-1.414-1.414l4-4a1 1 0 011.414 0l4 4a1 1 0 010 1.414z"
                              clipRule="evenodd"
                            />
                          </svg>
                        ) : (
                          <svg className="h-3.5 w-3.5" fill="currentColor" viewBox="0 0 20 20">
                            <path d="M10 3l-4 5h8l-4-5zM10 17l4-5H6l4 5z" />
                          </svg>
                        )}
                      </button>
                      {openUsageSortMenu === key && (
                        <div className="absolute right-0 top-full z-50 mt-1 min-w-[120px] rounded-lg border border-gray-200 py-1 shadow-lg dark:border-dark-600 surface-panel">
                          {(['today', 'total'] as const).map((metric) => (
                            <button
                              key={metric}
                              type="button"
                              className={`flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left text-xs normal-case tracking-normal hover:surface-hover surface-control ${
                                isUsageSortActive(key, metric)
                                  ? 'font-medium text-primary-600 dark:text-primary-400'
                                  : 'surface-muted'
                              }`}
                              data-test={`usage-sort-${key}-${metric}`}
                              onClick={(event) => {
                                event.stopPropagation()
                                toggleUsageSort(key, metric)
                              }}
                            >
                              <span>{metric === 'today' ? t('admin.users.today') : t('admin.users.total')}</span>
                              {getUsageSortOrder(key, metric) && (
                                <svg
                                  className={`h-3 w-3 ${getUsageSortOrder(key, metric) === 'desc' ? 'rotate-180' : ''}`}
                                  fill="currentColor"
                                  viewBox="0 0 20 20"
                                >
                                  <path
                                    fillRule="evenodd"
                                    d="M14.707 12.707a1 1 0 01-1.414 0L10 9.414l-3.293 3.293a1 1 0 01-1.414-1.414l4-4a1 1 0 011.414 0l4 4a1 1 0 010 1.414z"
                                    clipRule="evenodd"
                                  />
                                </svg>
                              )}
                            </button>
                          ))}
                          <div className="mt-1 border-t border-gray-100 px-3 py-1 text-[10px] normal-case tracking-normal dark:border-dark-700 surface-muted">
                            {t('admin.users.sortCurrentPageOnly')}
                          </div>
                        </div>
                      )}
                    </div>
                  </div>
                )
              }
              return null
            }}
            renderCell={(key, row) => {
              if (key === 'email') {
                return (
                  <div className="flex items-center gap-2">
                    <div className="flex h-8 w-8 items-center justify-center rounded-full bg-primary-100/[0.15] dark:bg-primary-900/30">
                      <span className="text-sm font-medium text-primary-700 dark:text-primary-300">
                        {row.email.charAt(0).toUpperCase()}
                      </span>
                    </div>
                    <span className="font-medium text-gray-900 dark:text-white">{row.email}</span>
                  </div>
                )
              }
              if (key === 'username') {
                return <span className="text-sm surface-muted">{row.username || '-'}</span>
              }
              if (key === 'notes') {
                return (
                  <div className="max-w-xs">
                    {row.notes ? (
                      <span
                        title={row.notes.length > 30 ? row.notes : undefined}
                        className="block truncate text-sm surface-muted"
                      >
                        {row.notes.length > 30 ? row.notes.substring(0, 25) + '...' : row.notes}
                      </span>
                    ) : (
                      <span className="text-sm surface-muted">-</span>
                    )}
                  </div>
                )
              }
              if (key.startsWith('attr_')) {
                const attrId = Number(key.slice('attr_'.length))
                return (
                  <div className="max-w-xs">
                    <span className="block truncate text-sm surface-muted" title={getAttributeValue(row.id, attrId)}>
                      {getAttributeValue(row.id, attrId)}
                    </span>
                  </div>
                )
              }
              if (key === 'role') {
                return (
                  <span className={`badge ${row.role === 'admin' ? 'badge-purple' : 'badge-gray'}`}>
                    {t('admin.users.roles.' + row.role)}
                  </span>
                )
              }
              if (key === 'groups') {
                const { exclusive, publicGroups } = getUserGroups(row)
                if (allGroups.length > 0) {
                  return (
                    <div className="flex flex-col gap-1">
                      {exclusive.length > 0 && (
                        <span
                          className="group/ex relative inline-flex cursor-pointer items-center gap-1 whitespace-nowrap text-xs"
                          onClick={(event) => {
                            event.stopPropagation()
                            setExpandedGroupUserId((prev) => (prev === row.id ? null : row.id))
                          }}
                        >
                          <Shield className="h-3.5 w-3.5 text-purple-500 dark:text-purple-400" />
                          <span className="font-medium text-purple-600 dark:text-purple-400">{exclusive.length}</span>
                          <span className="surface-muted">{t('admin.users.exclusiveLabel')}</span>
                          {expandedGroupUserId !== row.id && (
                            <div className="pointer-events-none absolute left-0 top-full z-50 mt-1.5 px-3 py-2 text-xs opacity-0 shadow-lg transition-opacity duration-75 group-hover/ex:opacity-100 surface-panel">
                              <div className="flex flex-col gap-0.5 whitespace-nowrap">
                                {exclusive.map((g) => (
                                  <span key={g.id}>{g.name}</span>
                                ))}
                              </div>
                            </div>
                          )}
                          {expandedGroupUserId === row.id && (
                            <div className="absolute left-0 top-full z-50 mt-1.5 min-w-[160px] overflow-hidden border border-gray-200 p-1 text-xs shadow-xl dark:border-dark-600 surface-panel">
                              <div className="border-b border-gray-100 px-3 py-1.5 text-[10px] font-medium uppercase tracking-wider dark:border-dark-600 surface-muted">
                                {t('admin.users.clickToReplace')}
                              </div>
                              {exclusive.map((g) => (
                                <div
                                  key={g.id}
                                  className="flex cursor-pointer items-center gap-2 rounded-[var(--surface-radius-item)] px-3 py-2 transition-colors hover:bg-primary-50/25 hover:text-primary-600 dark:hover:bg-primary-900/30 dark:hover:text-primary-400 surface-muted"
                                  onClick={(event) => {
                                    event.stopPropagation()
                                    openGroupReplace(row, g)
                                  }}
                                >
                                  <Copy className="h-3.5 w-3.5 flex-shrink-0 opacity-50" />
                                  <span className="flex-1">{g.name}</span>
                                </div>
                              ))}
                            </div>
                          )}
                        </span>
                      )}
                      {publicGroups.length > 0 && (
                        <span className="group/pub relative inline-flex cursor-default items-center gap-1 whitespace-nowrap text-xs">
                          <Globe className="h-3.5 w-3.5 surface-muted" />
                          <span className="font-medium surface-muted">{publicGroups.length}</span>
                          <span className="surface-muted">{t('admin.users.publicLabel')}</span>
                          <div className="pointer-events-none absolute left-0 top-full z-50 mt-1.5 px-3 py-2 text-xs opacity-0 shadow-lg transition-opacity duration-75 group-hover/pub:opacity-100 surface-panel">
                            <div className="flex flex-col gap-0.5 whitespace-nowrap">
                              {publicGroups.map((g) => (
                                <span key={g.id}>{g.name}</span>
                              ))}
                            </div>
                          </div>
                        </span>
                      )}
                      {exclusive.length === 0 && publicGroups.length === 0 && <span className="text-xs surface-muted">-</span>}
                    </div>
                  )
                }
                return <span className="text-xs surface-muted">-</span>
              }
              if (key === 'subscriptions') {
                if (row.subscriptions && row.subscriptions.length > 0) {
                  return (
                    <div className="flex flex-wrap gap-1.5">
                      {row.subscriptions.map((sub) => (
                        <GroupBadge
                          key={sub.id}
                          name={sub.group?.name || ''}
                          platform={sub.group?.platform}
                          subscriptionType={sub.group?.subscription_type}
                          rateMultiplier={sub.group?.rate_multiplier}
                          daysRemaining={sub.expires_at ? getDaysRemaining(sub.expires_at) : null}
                          title={sub.expires_at ? formatDateTime(sub.expires_at) : ''}
                        />
                      ))}
                    </div>
                  )
                }
                return (
                  <span className="inline-flex items-center gap-1.5 rounded-md surface-inset px-2 py-1 text-xs surface-muted">
                    <Ban className="h-3.5 w-3.5" />
                    <span>{t('admin.users.noSubscription')}</span>
                  </span>
                )
              }
              if (key === 'balance') {
                return (
                  <div className="flex items-center gap-2">
                    <div className="group relative">
                      <button
                        className="font-medium text-gray-900 underline decoration-dashed decoration-gray-300 underline-offset-4 transition-colors hover:text-primary-600 dark:text-white dark:decoration-dark-500 dark:hover:text-primary-400 surface-control"
                        onClick={() => handleBalanceHistory(row)}
                      >
                        ${row.balance.toFixed(2)}
                      </button>
                      <div className="pointer-events-none absolute bottom-full left-1/2 z-50 mb-1.5 -translate-x-1/2 whitespace-nowrap px-3 py-2 text-xs opacity-0 shadow-lg transition-opacity duration-75 group-hover:opacity-100 surface-panel">
                        {t('admin.users.balanceHistoryTip')}
                      </div>
                    </div>
                    <button
                      onClick={(event) => {
                        event.stopPropagation()
                        handleDeposit(row)
                      }}
                      className="rounded px-2 py-0.5 text-xs font-medium text-emerald-600 transition-colors hover:bg-primary-50/25 dark:text-emerald-400 dark:hover:bg-primary-900/20 surface-control"
                      title={t('admin.users.deposit')}
                    >
                      {t('admin.users.deposit')}
                    </button>
                  </div>
                )
              }
              if (key === 'balance_platform_quota') {
                return (
                  <button
                    type="button"
                    className="block text-left underline decoration-dashed decoration-gray-300 underline-offset-4 transition-colors hover:decoration-primary-400 dark:decoration-dark-500 surface-control"
                    title={t('admin.users.platformQuota.cellColumnTooltip')}
                    onClick={() => handlePlatformQuota(row)}
                  >
                    <UserPlatformQuotaCell quotas={platformQuotaStats[row.id]} />
                  </button>
                )
              }
              if (key === 'usage') {
                return (
                  <PlatformUsageBreakdown
                    today={usageStats[row.id]?.today_actual_cost ?? 0}
                    total={usageStats[row.id]?.total_actual_cost ?? 0}
                    byPlatform={usageStats[row.id]?.by_platform}
                  />
                )
              }
              if (key === 'usage_anthropic') return <PlatformCostCell usage={getPlatformUsage(row.id, 'anthropic')} />
              if (key === 'usage_openai') return <PlatformCostCell usage={getPlatformUsage(row.id, 'openai')} />
              if (key === 'usage_gemini') return <PlatformCostCell usage={getPlatformUsage(row.id, 'gemini')} />
              if (key === 'usage_antigravity') return <PlatformCostCell usage={getPlatformUsage(row.id, 'antigravity')} />
              if (key === 'concurrency') {
                return <UserConcurrencyCell current={row.current_concurrency ?? 0} max={row.concurrency} />
              }
              if (key === 'status') {
                return (
                  <div className="flex items-center gap-1.5">
                    <span
                      className={`console-data-mark inline-block h-2 w-2 rounded-full ${
                        row.status === 'active' ? 'bg-green-500' : 'bg-red-500'
                      }`}
                    />
                    <span className="text-sm surface-muted">
                      {row.status === 'active' ? t('common.active') : t('admin.users.disabled')}
                    </span>
                  </div>
                )
              }
              if (key === 'created_at') {
                return <span className="text-sm surface-muted">{formatDateTime(row.created_at)}</span>
              }
              if (key === 'last_used_at') {
                return <span className="text-sm surface-muted">{row.last_used_at ? formatDateTime(row.last_used_at) : '-'}</span>
              }
              if (key === 'last_active_at') {
                return <span className="text-sm surface-muted">{row.last_active_at ? formatDateTime(row.last_active_at) : '-'}</span>
              }
              if (key === 'actions') {
                return (
                  <div className="table-actions">
                    <button className="table-action" onClick={() => handleEdit(row)}>
                      <ListChecks className="h-3.5 w-3.5" />
                      <span className="text-xs">{t('common.edit')}</span>
                    </button>

                    {row.role !== 'admin' && (
                      <button
                        className={`table-action ${row.status === 'active' ? 'table-action-warning' : ''}`}
                        onClick={() => handleToggleStatus(row)}
                      >
                        {row.status === 'active' ? <Ban className="h-3.5 w-3.5" /> : <CheckCircle className="h-3.5 w-3.5" />}
                        <span className="text-xs">
                          {row.status === 'active' ? t('admin.users.disable') : t('admin.users.enable')}
                        </span>
                      </button>
                    )}

                    <button
                      className={`table-action table-action-neutral action-menu-trigger ${activeMenuId === row.id ? 'table-action-active' : ''}`}
                      onClick={(event) => openActionMenu(row, event)}
                    >
                      <MoreHorizontal className="h-3.5 w-3.5" />
                      <span className="text-xs">{t('common.more')}</span>
                    </button>
                  </div>
                )
              }
              return null
            }}
            empty={
              <EmptyState
                icon={<UserRound className="h-10 w-10" />}
                title={t('admin.users.noUsersYet')}
                description={t('admin.users.createFirstUser')}
                action={
                  <Button variant="primary" onClick={() => setShowCreateModal(true)}>
                    {t('admin.users.createUser')}
                  </Button>
                }
              />
            }
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

      {activeMenuId !== null && menuPosition && menuUser &&
        createPortal(
          <div
            className="action-menu-content fixed z-[9999] w-48 overflow-hidden rounded-xl shadow-lg ring-1 ring-black/5 dark:ring-white/10 surface-panel"
            style={{ top: `${menuPosition.top}px`, left: `${menuPosition.left}px` }}
          >
            <div className="py-1">
              <button
                onClick={() => {
                  handleViewApiKeys(menuUser)
                  closeActionMenu()
                }}
                className="flex w-full items-center gap-2 px-4 py-2 text-sm hover:surface-hover surface-control surface-muted"
              >
                <KeyRound className="h-4 w-4 text-gray-400" strokeWidth={2} />
                {t('admin.users.apiKeys')}
              </button>

              <button
                onClick={() => {
                  handleAllowedGroups(menuUser)
                  closeActionMenu()
                }}
                className="flex w-full items-center gap-2 px-4 py-2 text-sm hover:surface-hover surface-control surface-muted"
              >
                <Users className="h-4 w-4 text-gray-400" strokeWidth={2} />
                {t('admin.users.groups')}
              </button>

              <div className="my-1 border-t border-gray-100 dark:border-dark-700" />

              <button
                onClick={() => {
                  handleDeposit(menuUser)
                  closeActionMenu()
                }}
                className="flex w-full items-center gap-2 px-4 py-2 text-sm hover:surface-hover surface-control surface-muted"
              >
                <Plus className="h-4 w-4 text-emerald-500" strokeWidth={2} />
                {t('admin.users.deposit')}
              </button>

              <button
                onClick={() => {
                  handleWithdraw(menuUser)
                  closeActionMenu()
                }}
                className="flex w-full items-center gap-2 px-4 py-2 text-sm hover:surface-hover surface-control surface-muted"
              >
                <svg className="h-4 w-4 text-amber-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M20 12H4" />
                </svg>
                {t('admin.users.withdraw')}
              </button>

              <button
                onClick={() => {
                  handlePlatformQuota(menuUser)
                  closeActionMenu()
                }}
                className="flex w-full items-center gap-2 px-4 py-2 text-sm hover:surface-hover surface-control surface-muted"
              >
                <ChartBarBig className="h-4 w-4 text-gray-400" strokeWidth={2} />
                {t('admin.users.platformQuota.menuItem')}
              </button>

              <button
                onClick={() => {
                  handleBalanceHistory(menuUser)
                  closeActionMenu()
                }}
                className="flex w-full items-center gap-2 px-4 py-2 text-sm hover:surface-hover surface-control surface-muted"
              >
                <DollarSign className="h-4 w-4 text-gray-400" strokeWidth={2} />
                {t('admin.users.balanceHistory')}
              </button>

              <div className="my-1 border-t border-gray-100 dark:border-dark-700" />

              {menuUser.role !== 'admin' && (
                <button
                  onClick={() => {
                    handleDelete(menuUser)
                    closeActionMenu()
                  }}
                  className="flex w-full items-center gap-2 px-4 py-2 text-sm text-red-600 hover:bg-red-50/25 dark:text-red-400 dark:hover:bg-red-900/20 surface-control"
                >
                  <Trash2 className="h-4 w-4" strokeWidth={2} />
                  {t('common.delete')}
                </button>
              )}
            </div>
          </div>,
          document.body
        )}

      <ConfirmDialog
        open={showDeleteDialog}
        title={t('admin.users.deleteUser')}
        message={t('admin.users.deleteConfirm', { email: deletingUser?.email })}
        variant="danger"
        onConfirm={confirmDelete}
        onCancel={() => setShowDeleteDialog(false)}
      />

      <ConfirmDialog
        open={bulkDeleteIds.length > 0}
        title={t('admin.users.bulkDelete.title')}
        message={t('admin.users.bulkDelete.confirm', { count: bulkDeleteIds.length })}
        confirmText={t('common.delete')}
        variant="danger"
        onConfirm={confirmBulkDelete}
        onCancel={() => setBulkDeleteIds([])}
      />

      <UserCreateModal show={showCreateModal} onClose={() => setShowCreateModal(false)} onSuccess={() => setReloadKey((k) => k + 1)} />
      <UserEditModal
        show={showEditModal}
        user={editingUser}
        onClose={closeEditModal}
        onSuccess={() => setReloadKey((k) => k + 1)}
      />
      <BulkEditUserModal
        show={showBulkEditModal}
        selectedIds={selectedNumbers}
        onClose={() => setShowBulkEditModal(false)}
        onSuccess={handleBulkLimitsSuccess}
      />
      <UserPlatformQuotaModal
        show={showPlatformQuotaModal}
        user={platformQuotaUser}
        onClose={closePlatformQuotaModal}
        onSuccess={() => setReloadKey((k) => k + 1)}
      />
      <UserApiKeysModal show={showApiKeysModal} user={viewingUser} onClose={closeApiKeysModal} />
      <UserAllowedGroupsModal
        show={showAllowedGroupsModal}
        user={allowedGroupsUser}
        onClose={closeAllowedGroupsModal}
        onSuccess={() => setReloadKey((k) => k + 1)}
      />
      <UserBalanceModal
        show={showBalanceModal}
        user={balanceUser}
        operation={balanceOperation}
        onClose={closeBalanceModal}
        onSuccess={() => setReloadKey((k) => k + 1)}
      />
      <UserBalanceHistoryModal
        show={showBalanceHistoryModal}
        user={balanceHistoryUser}
        onClose={closeBalanceHistoryModal}
        onDeposit={handleDepositFromHistory}
        onWithdraw={handleWithdrawFromHistory}
      />
      <GroupReplaceModal
        show={showGroupReplaceModal}
        user={groupReplaceUser}
        oldGroup={groupReplaceOldGroup}
        allGroups={allGroups}
        onClose={closeGroupReplaceModal}
        onSuccess={() => setReloadKey((k) => k + 1)}
      />
      <UserAttributesConfigModal
        show={showAttributesModal}
        onClose={() => setShowAttributesModal(false)}
        onSaved={handleAttributesModalClose}
      />
    </>
  )
}
