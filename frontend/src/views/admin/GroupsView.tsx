import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Check, Copy, Grid2x2, Pencil, Plus, RefreshCw, Search, Trash2 } from 'lucide-react'
import { adminAPI } from '@/api/admin'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { AdminGroup, GroupPlatform } from '@/types'
import { platformLabel } from '@/utils/platformColors'
import { listPlatforms } from '@/utils/platformCatalog'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { Button, EmptyState, Input, Modal, Pagination, Select } from '@/components/ui'
import { AdminDataTable, type Column } from './AdminDataTable'
import TablePageLayout from '@/components/layout/TablePageLayout'

const ALWAYS_VISIBLE_COLUMNS = new Set(['name', 'actions'])
const DEFAULT_HIDDEN_COLUMNS = ['id']
const HIDDEN_COLUMNS_KEY = 'group-hidden-columns'
const COLUMN_SETTINGS_VERSION_KEY = 'group-column-settings-version'
const COLUMN_SETTINGS_VERSION = 2
const VERSION_NEW_HIDDEN_COLUMNS: Record<number, string[]> = {
  2: ['capacity']
}

const GROUP_PLATFORM_OPTIONS: Array<{ value: GroupPlatform; label: string }> = [
  ...listPlatforms().map((spec) => ({ value: spec.id as GroupPlatform, label: spec.display_name })),
  { value: 'composite' as GroupPlatform, label: 'Composite' }
]

interface GroupUsageSummary {
  today_cost: number
  yesterday_cost: number
  total_cost: number
}

interface GroupCapacitySummary {
  concurrencyUsed: number
  concurrencyMax: number
  sessionsUsed: number
  sessionsMax: number
  rpmUsed: number
  rpmMax: number
}

function capacityClass(used: number, max: number): string {
  if (max <= 0) return 'surface-inset surface-muted'
  const ratio = used / max
  if (ratio >= 1) return 'bg-red-100/[0.15] text-red-700 dark:bg-red-900/30 dark:text-red-400'
  if (ratio >= 0.8) return 'bg-amber-100/[0.15] text-amber-700 dark:bg-amber-900/30 dark:text-amber-400'
  return 'surface-inset surface-muted'
}

function GroupCapacityBadge({
  concurrencyUsed,
  concurrencyMax,
  sessionsUsed,
  sessionsMax,
  rpmUsed,
  rpmMax
}: GroupCapacitySummary) {
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center gap-1">
        <span className={`inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[10px] font-medium ${capacityClass(concurrencyUsed, concurrencyMax)}`}>
          <svg className="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
            <path strokeLinecap="round" strokeLinejoin="round" d="M3.75 6A2.25 2.25 0 016 3.75h2.25A2.25 2.25 0 0110.5 6v2.25a2.25 2.25 0 01-2.25 2.25H6a2.25 2.25 0 01-2.25-2.25V6zM3.75 15.75A2.25 2.25 0 016 13.5h2.25a2.25 2.25 0 012.25 2.25V18a2.25 2.25 0 01-2.25 2.25H6A2.25 2.25 0 013.75 18v-2.25zM13.5 6a2.25 2.25 0 012.25-2.25H18a2.25 2.25 0 0120.25 6v2.25A2.25 2.25 0 0118 10.5h-2.25a2.25 2.25 0 01-2.25-2.25V6zM13.5 15.75a2.25 2.25 0 012.25-2.25H18a2.25 2.25 0 0122.25 2.25V18A2.25 2.25 0 0118 20.25h-2.25A2.25 2.25 0 0113.5 18v-2.25z" />
          </svg>
          <span className="font-mono">{concurrencyUsed}</span>
          <span className="surface-muted">/</span>
          <span className="font-mono">{concurrencyMax}</span>
        </span>
      </div>

      {sessionsMax > 0 && (
        <div className="flex items-center gap-1">
          <span className={`inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[10px] font-medium ${capacityClass(sessionsUsed, sessionsMax)}`}>
            <svg className="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M15 19.128a9.38 9.38 0 002.625.372 9.337 9.337 0 004.121-.952 4.125 4.125 0 00-7.533-2.493M15 19.128v-.003c0-1.113-.285-2.16-.786-3.07M15 19.128v.106A12.318 12.318 0 018.624 21c-2.331 0-4.512-.645-6.374-1.766l-.001-.109a6.375 6.375 0 0111.964-3.07M12 6.375a3.375 3.375 0 11-6.75 0 3.375 3.375 0 016.75 0zm8.25 2.25a2.625 2.625 0 11-5.25 0 2.625 2.625 0 015.25 0z" />
            </svg>
            <span className="font-mono">{sessionsUsed}</span>
            <span className="surface-muted">/</span>
            <span className="font-mono">{sessionsMax}</span>
          </span>
        </div>
      )}

      {rpmMax > 0 && (
        <div className="flex items-center gap-1">
          <span className={`inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[10px] font-medium ${capacityClass(rpmUsed, rpmMax)}`}>
            <svg className="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M3.75 13.5l10.5-11.25L12 10.5h8.25L9.75 21.75 12 13.5H3.75z" />
            </svg>
            <span className="font-mono">{rpmUsed}</span>
            <span className="surface-muted">/</span>
            <span className="font-mono">{rpmMax}</span>
          </span>
        </div>
      )}
    </div>
  )
}

function platformBadgeClass(platform: string): string {
  switch (platform) {
    case 'anthropic':
      return 'bg-orange-100/[0.15] text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
    case 'openai':
      return 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
    case 'antigravity':
      return 'bg-purple-100/[0.15] text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
    case 'grok':
      return 'bg-zinc-200 text-zinc-800 dark:bg-zinc-700 dark:text-zinc-100'
    case 'kimi':
      return 'bg-pink-100/[0.15] text-pink-700 dark:bg-pink-900/30 dark:text-pink-400'
    case 'zhipu':
      return 'bg-indigo-100/[0.15] text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-400'
    case 'deepseek':
      return 'bg-teal-100/[0.15] text-teal-700 dark:bg-teal-900/30 dark:text-teal-400'
    case 'minimax':
      return 'bg-rose-100/[0.15] text-rose-700 dark:bg-rose-900/30 dark:text-rose-400'
    case 'opencode_go':
      return 'bg-amber-100/[0.15] text-amber-800 dark:bg-amber-900/30 dark:text-amber-300'
    default:
      return 'bg-blue-100/[0.15] text-blue-700 dark:bg-blue-900/30 dark:text-blue-400'
  }
}

const formatCost = (cost: number): string => {
  if (cost >= 1000) return cost.toFixed(0)
  if (cost >= 100) return cost.toFixed(1)
  return cost.toFixed(2)
}

const formatUsd = (cost: number | null | undefined): string => `$${formatCost(cost ?? 0)}`

const getQuotaUsageClass = (used: number, limit: number | null | undefined): string => {
  if (!limit || limit <= 0) return 'font-medium surface-muted'
  const ratio = used / limit
  if (ratio >= 1) return 'font-semibold text-red-600 dark:text-red-400'
  if (ratio >= 0.8) return 'font-semibold text-amber-600 dark:text-amber-400'
  return 'font-medium surface-muted'
}

interface GroupFilters {
  platform: string
  status: string
  is_exclusive: string
}

const EMPTY_FILTERS: GroupFilters = { platform: '', status: '', is_exclusive: '' }

function loadSavedColumns(toggleableKeys: string[]): Set<string> {
  const hidden = new Set<string>()
  const validKeys = new Set(toggleableKeys)
  try {
    const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
    if (saved) {
      const parsed = JSON.parse(saved)
      if (Array.isArray(parsed)) {
        for (const key of parsed) {
          if (validKeys.has(key)) hidden.add(key)
        }
        const storedVersion = Number(localStorage.getItem(COLUMN_SETTINGS_VERSION_KEY) ?? '1')
        if (storedVersion < COLUMN_SETTINGS_VERSION) {
          for (let v = storedVersion + 1; v <= COLUMN_SETTINGS_VERSION; v += 1) {
            for (const key of VERSION_NEW_HIDDEN_COLUMNS[v] ?? []) {
              if (validKeys.has(key)) hidden.add(key)
            }
          }
          saveColumnsToStorage(hidden)
        }
      } else {
        DEFAULT_HIDDEN_COLUMNS.forEach((key) => {
          if (validKeys.has(key)) hidden.add(key)
        })
        saveColumnsToStorage(hidden)
      }
    } else {
      DEFAULT_HIDDEN_COLUMNS.forEach((key) => {
        if (validKeys.has(key)) hidden.add(key)
      })
      saveColumnsToStorage(hidden)
    }
  } catch {
    DEFAULT_HIDDEN_COLUMNS.forEach((key) => {
      if (validKeys.has(key)) hidden.add(key)
    })
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

export default function GroupsView() {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)
  const isSimpleMode = useAuthStore((state) => state.runMode === 'simple')

  const [groups, setGroups] = useState<AdminGroup[]>([])
  const [loading, setLoading] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [filters, setFilters] = useState<GroupFilters>(EMPTY_FILTERS)
  const [pagination, setPagination] = useState({ page: 1, page_size: 20, total: 0, pages: 0 })
  const [sortState, setSortState] = useState({ sort_by: 'sort_order', sort_order: 'asc' as 'asc' | 'desc' })

  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [deletingGroup, setDeletingGroup] = useState<AdminGroup | null>(null)
  const [duplicatingGroupIds, setDuplicatingGroupIds] = useState<Set<number>>(new Set())
  const [showColumnDropdown, setShowColumnDropdown] = useState(false)
  const columnDropdownRef = useRef<HTMLDivElement>(null)

  const [usageMap, setUsageMap] = useState<Map<number, GroupUsageSummary>>(new Map())
  const [usageLoading, setUsageLoading] = useState(false)
  const [capacityMap, setCapacityMap] = useState<Map<number, GroupCapacitySummary>>(new Map())

  const abortController = useRef<AbortController | null>(null)
  const searchTimeout = useRef<ReturnType<typeof setTimeout> | null>(null)

  const allColumns: Column[] = useMemo(() => {
    const basic: Column[] = [
      { key: 'name', label: t('admin.groups.columns.name'), sortable: true },
      { key: 'id', label: t('admin.groups.columns.id'), sortable: true },
      { key: 'platform', label: t('admin.groups.columns.platform'), sortable: true },
      { key: 'account_count', label: t('admin.groups.columns.accounts'), sortable: true },
      { key: 'status', label: t('admin.groups.columns.status'), sortable: true },
      { key: 'actions', label: t('admin.groups.columns.actions'), sortable: false }
    ]
    if (isSimpleMode) return basic
    return [
      ...basic.slice(0, 3),
      { key: 'billing_type', label: t('admin.groups.columns.billingType'), sortable: true },
      { key: 'rate_multiplier', label: t('admin.groups.columns.rateMultiplier'), sortable: true },
      { key: 'is_exclusive', label: t('admin.groups.columns.type'), sortable: true },
      basic[3],
      { key: 'capacity', label: t('admin.groups.columns.capacity'), sortable: false },
      { key: 'usage', label: t('admin.groups.columns.usage'), sortable: false },
      ...basic.slice(4)
    ]
  }, [isSimpleMode, t])

  const toggleableColumns = useMemo(() => allColumns.filter((col) => !ALWAYS_VISIBLE_COLUMNS.has(col.key)), [allColumns])

  const [hiddenColumns, setHiddenColumns] = useState<Set<string>>(() =>
    loadSavedColumns([
      'id',
      'platform',
      'account_count',
      'status',
      'billing_type',
      'rate_multiplier',
      'is_exclusive',
      'capacity',
      'usage'
    ])
  )

  const isColumnVisible = (key: string) => !hiddenColumns.has(key)
  const hasVisibleUsageSummaryConsumer = !isSimpleMode && (isColumnVisible('usage') || isColumnVisible('billing_type'))
  const hasVisibleCapacityColumn = !isSimpleMode && isColumnVisible('capacity')

  const columns = useMemo(
    () => allColumns.filter((col) => ALWAYS_VISIBLE_COLUMNS.has(col.key) || !hiddenColumns.has(col.key)),
    [allColumns, hiddenColumns]
  )

  const loadUsageSummary = useCallback(async () => {
    setUsageLoading(true)
    try {
      const data = await adminAPI.groups.getUsageSummary()
      const map = new Map<number, GroupUsageSummary>()
      for (const item of data) {
        map.set(item.group_id, {
          today_cost: item.today_cost,
          yesterday_cost: item.yesterday_cost,
          total_cost: item.total_cost
        })
      }
      setUsageMap(map)
    } catch (error) {
      console.error('Error loading group usage summary:', error)
    } finally {
      setUsageLoading(false)
    }
  }, [])

  const loadCapacitySummary = useCallback(async () => {
    try {
      const data = await adminAPI.groups.getCapacitySummary()
      const map = new Map<number, GroupCapacitySummary>()
      for (const item of data) {
        map.set(item.group_id, {
          concurrencyUsed: item.concurrency_used,
          concurrencyMax: item.concurrency_max,
          sessionsUsed: item.sessions_used,
          sessionsMax: item.sessions_max,
          rpmUsed: item.rpm_used,
          rpmMax: item.rpm_max
        })
      }
      setCapacityMap(map)
    } catch (error) {
      console.error('Error loading group capacity summary:', error)
    }
  }, [])

  const queryRef = useRef({ pagination, filters, searchQuery, sortState, isSimpleMode })
  queryRef.current = { pagination, filters, searchQuery, sortState, isSimpleMode }

  const loadGroups = useCallback(async () => {
    if (abortController.current) abortController.current.abort()
    const controller = new AbortController()
    abortController.current = controller
    const { signal } = controller
    const { pagination: page, filters: f, searchQuery: search, sortState: sort, isSimpleMode: simple } = queryRef.current
    setLoading(true)
    try {
      const response = await adminAPI.groups.list(
        page.page,
        page.page_size,
        {
          platform: (f.platform as GroupPlatform) || undefined,
          status: f.status as 'active' | 'inactive' | undefined,
          is_exclusive: !simple && f.is_exclusive ? f.is_exclusive === 'true' : undefined,
          search: search.trim() || undefined,
          sort_by: sort.sort_by,
          sort_order: sort.sort_order
        },
        { signal }
      )
      if (signal.aborted) return
      setGroups(response.items)
      setPagination((prev) => ({ ...prev, total: response.total, pages: response.pages }))
      if (hasVisibleUsageSummaryConsumer) void loadUsageSummary()
      else setUsageLoading(false)
      if (hasVisibleCapacityColumn) void loadCapacitySummary()
    } catch (error) {
      const e = error as { name?: string; code?: string }
      if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
      showError(extractApiErrorMessage(error, t('admin.groups.failedToLoad')))
    } finally {
      if (abortController.current === controller) {
        setLoading(false)
        abortController.current = null
      }
    }
  }, [hasVisibleCapacityColumn, hasVisibleUsageSummaryConsumer, loadCapacitySummary, loadUsageSummary, showError, t])

  const [reloadKey, setReloadKey] = useState(0)
  useEffect(() => {
    void loadGroups()
  }, [loadGroups, reloadKey])

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as HTMLElement
      if (columnDropdownRef.current && !columnDropdownRef.current.contains(target)) {
        setShowColumnDropdown(false)
      }
    }
    document.addEventListener('click', handleClickOutside)
    return () => document.removeEventListener('click', handleClickOutside)
  }, [])

  useEffect(() => {
    return () => {
      if (searchTimeout.current) clearTimeout(searchTimeout.current)
      abortController.current?.abort()
    }
  }, [])

  const toggleColumn = (key: string) => {
    const validKeys = new Set(toggleableColumns.map((col) => col.key))
    if (!validKeys.has(key)) return

    const wasHidden = hiddenColumns.has(key)
    setHiddenColumns((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      saveColumnsToStorage(next)
      return next
    })

    if (wasHidden && (key === 'usage' || key === 'billing_type')) void loadUsageSummary()
    if (wasHidden && key === 'capacity') void loadCapacitySummary()
  }

  const handleSearch = () => {
    if (searchTimeout.current) clearTimeout(searchTimeout.current)
    searchTimeout.current = setTimeout(() => {
      setPagination((prev) => ({ ...prev, page: 1 }))
      setReloadKey((k) => k + 1)
    }, 300)
  }

  const handlePageChange = (page: number) => {
    setPagination((prev) => ({ ...prev, page }))
    setReloadKey((k) => k + 1)
  }

  const handlePageSizeChange = (pageSize: number) => {
    setPagination((prev) => ({ ...prev, page_size: pageSize, page: 1 }))
    setReloadKey((k) => k + 1)
  }

  const handleSort = (key: string, order: 'asc' | 'desc') => {
    setSortState({ sort_by: key, sort_order: order })
    setPagination((prev) => ({ ...prev, page: 1 }))
    setReloadKey((k) => k + 1)
  }

  const handleDelete = (group: AdminGroup) => {
    setDeletingGroup(group)
    setShowDeleteDialog(true)
  }

  const confirmDelete = async () => {
    if (!deletingGroup) return

    try {
      await adminAPI.groups.delete(deletingGroup.id)
      showSuccess(t('admin.groups.groupDeleted'))
      setShowDeleteDialog(false)
      setDeletingGroup(null)
      setReloadKey((k) => k + 1)
    } catch (error) {
      showError(
        (error as { response?: { data?: { detail?: string } } })?.response?.data?.detail || t('admin.groups.failedToDelete')
      )
      console.error('Error deleting group:', error)
    }
  }

  const handleDuplicate = async (group: AdminGroup) => {
    if (duplicatingGroupIds.has(group.id)) return

    setDuplicatingGroupIds((prev) => new Set(prev).add(group.id))
    try {
      const duplicate = await adminAPI.groups.duplicate(group.id)
      showSuccess(t('admin.groups.duplicateSuccess', { name: duplicate.name }))
      setReloadKey((k) => k + 1)
    } catch (error) {
      showError(extractApiErrorMessage(error, t('admin.groups.duplicateFailed')))
    } finally {
      setDuplicatingGroupIds((prev) => {
        const next = new Set(prev)
        next.delete(group.id)
        return next
      })
    }
  }

  const deleteConfirmMessage = (() => {
    if (!deletingGroup) return ''
    if (deletingGroup.subscription_type === 'subscription') {
      return t('admin.groups.deleteConfirmSubscription', { name: deletingGroup.name })
    }
    return t('admin.groups.deleteConfirm', { name: deletingGroup.name })
  })()

  const statusOptions = useMemo(
    () => [
      { value: '', label: t('admin.groups.allStatus') },
      { value: 'active', label: t('admin.accounts.status.active') },
      { value: 'inactive', label: t('admin.accounts.status.inactive') }
    ],
    [t]
  )

  const exclusiveOptions = useMemo(
    () => [
      { value: '', label: t('admin.groups.allGroups') },
      { value: 'true', label: t('admin.groups.exclusive') },
      { value: 'false', label: t('admin.groups.nonExclusive') }
    ],
    [t]
  )

  const platformFilterOptions = useMemo(
    () => [{ value: '', label: t('admin.groups.allPlatforms') }, ...GROUP_PLATFORM_OPTIONS],
    [t]
  )

  return (
    <>
      <TablePageLayout
        filters={
          <div className="filter-bar">
            <div className="filter-bar filter-bar--inline">
              <div className="filter-field filter-field--search relative">
                <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 surface-muted" />
                <Input
                  value={searchQuery}
                  onValueChange={(value) => {
                    setSearchQuery(value)
                    handleSearch()
                  }}
                  type="text"
                  placeholder={t('admin.groups.searchGroups')}
                  className="pl-10"
                />
              </div>
              <div className="filter-field">
                <Select
                  value={filters.platform}
                  options={platformFilterOptions}
                  placeholder={t('admin.groups.allPlatforms')}
                  onValueChange={(value) => {
                    setFilters((prev) => ({ ...prev, platform: String(value ?? '') }))
                    setReloadKey((k) => k + 1)
                  }}
                />
              </div>
              <div className="filter-field">
                <Select
                  value={filters.status}
                  options={statusOptions}
                  placeholder={t('admin.groups.allStatus')}
                  onValueChange={(value) => {
                    setFilters((prev) => ({ ...prev, status: String(value ?? '') }))
                    setReloadKey((k) => k + 1)
                  }}
                />
              </div>
              {!isSimpleMode && (
                <div className="filter-field">
                  <Select
                    value={filters.is_exclusive}
                    options={exclusiveOptions}
                    placeholder={t('admin.groups.allGroups')}
                    onValueChange={(value) => {
                      setFilters((prev) => ({ ...prev, is_exclusive: String(value ?? '') }))
                      setReloadKey((k) => k + 1)
                    }}
                  />
                </div>
              )}
            </div>

            <div className="filter-actions">
              <Button
                variant="secondary"
                size="icon"
                disabled={loading}
                onClick={() => setReloadKey((k) => k + 1)}
                title={t('common.refresh')}
              >
                <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
              </Button>
              <div className="relative" ref={columnDropdownRef}>
                <Button
                  variant="secondary"
                  onClick={() => setShowColumnDropdown((prev) => !prev)}
                  title={t('admin.groups.columnSettings')}
                >
                  <Grid2x2 className="mr-2 h-4 w-4" />
                  <span className="hidden md:inline">{t('admin.groups.columnSettings')}</span>
                </Button>
                {showColumnDropdown && (
                  <div className="absolute right-0 top-full z-50 mt-1 max-h-80 w-48 space-y-0.5 overflow-y-auto border border-gray-200 p-1 shadow-lg dark:border-dark-600 surface-panel">
                    {toggleableColumns.map((col) => (
                      <button
                        key={col.key}
                        onClick={() => toggleColumn(col.key)}
                        className="flex w-full items-center justify-between rounded-[var(--surface-radius-item)] px-3 py-2 text-left text-sm hover:surface-hover surface-control surface-muted"
                      >
                        <span>{col.label}</span>
                        {isColumnVisible(col.key) && <Check className="h-4 w-4 text-primary-500" strokeWidth={2} />}
                      </button>
                    ))}
                  </div>
                )}
              </div>
              <Button variant="primary" data-tour="groups-create-btn">
                <Plus className="mr-2 h-4 w-4" />
                {t('admin.groups.createGroup')}
              </Button>
            </div>
          </div>
        }
        table={
          <AdminDataTable<AdminGroup>
            columns={columns}
            data={groups}
            loading={loading}
            rowKey="id"
            sortKey={sortState.sort_by}
            sortOrder={sortState.sort_order}
            onSort={handleSort}
            renderCell={(key, row) => {
              if (key === 'name') {
                return <span className="font-medium text-gray-900 dark:text-white">{row.name}</span>
              }
              if (key === 'id') {
                return <span className="font-mono text-xs surface-muted">#{row.id}</span>
              }
              if (key === 'platform') {
                return (
                  <span
                    className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ${platformBadgeClass(row.platform)}`}
                  >
                    {t('admin.groups.platforms.' + row.platform, platformLabel(row.platform))}
                  </span>
                )
              }
              if (key === 'billing_type') {
                return (
                  <div className="space-y-1">
                    <span
                      className={`inline-block rounded-full px-2 py-0.5 text-xs font-medium ${
                        row.subscription_type === 'subscription'
                          ? 'bg-violet-100/[0.15] text-violet-700 dark:bg-violet-900/30 dark:text-violet-400'
                          : 'surface-inset text-gray-600 dark:surface-inset dark:text-gray-300'
                      }`}
                    >
                      {row.subscription_type === 'subscription'
                        ? t('admin.groups.subscription.subscription')
                        : t('admin.groups.subscription.standard')}
                    </span>
                    {row.subscription_type === 'subscription' && (
                      <div className="space-y-0.5 text-xs surface-muted">
                        {row.daily_limit_usd || row.weekly_limit_usd || row.monthly_limit_usd || row.five_hour_limit_usd ? (
                          <div className="flex flex-wrap items-center gap-x-1 gap-y-0.5">
                            {row.daily_limit_usd && (
                              <span className="whitespace-nowrap">
                                <span className={getQuotaUsageClass(usageMap.get(row.id)?.today_cost ?? 0, row.daily_limit_usd)}>
                                  {formatUsd(usageMap.get(row.id)?.today_cost ?? 0)}
                                </span>
                                <span className="surface-muted">
                                  / {formatUsd(row.daily_limit_usd)}/{t('admin.groups.limitDay')}
                                </span>
                              </span>
                            )}
                            {row.daily_limit_usd && (row.weekly_limit_usd || row.monthly_limit_usd || row.five_hour_limit_usd) && (
                              <span className="mx-1 text-gray-300 dark:text-gray-600">·</span>
                            )}
                            {row.weekly_limit_usd && (
                              <span className="whitespace-nowrap">
                                {formatUsd(row.weekly_limit_usd)}/{t('admin.groups.limitWeek')}
                              </span>
                            )}
                            {row.weekly_limit_usd && (row.monthly_limit_usd || row.five_hour_limit_usd) && (
                              <span className="mx-1 text-gray-300 dark:text-gray-600">·</span>
                            )}
                            {row.monthly_limit_usd && (
                              <span className="whitespace-nowrap">
                                {formatUsd(row.monthly_limit_usd)}/{t('admin.groups.limitMonth')}
                              </span>
                            )}
                            {row.monthly_limit_usd && row.five_hour_limit_usd && (
                              <span className="mx-1 text-gray-300 dark:text-gray-600">·</span>
                            )}
                            {row.five_hour_limit_usd && (
                              <span className="whitespace-nowrap">
                                {formatUsd(row.five_hour_limit_usd)}/{t('admin.groups.limitFiveHours')}
                              </span>
                            )}
                          </div>
                        ) : (
                          <span className="surface-muted">{t('admin.groups.subscription.noLimit')}</span>
                        )}
                        <div className="surface-muted">
                          {t('admin.groups.usageTotal')}
                          <span className="ml-1 font-medium surface-muted">
                            {usageLoading ? '—' : formatUsd(usageMap.get(row.id)?.total_cost ?? 0)}
                          </span>
                        </div>
                      </div>
                    )}
                  </div>
                )
              }
              if (key === 'rate_multiplier') {
                return <span className="text-sm surface-muted">{row.rate_multiplier}x</span>
              }
              if (key === 'is_exclusive') {
                return (
                  <span className={`badge ${row.is_exclusive ? 'badge-primary' : 'badge-gray'}`}>
                    {row.is_exclusive ? t('admin.groups.exclusive') : t('admin.groups.public')}
                  </span>
                )
              }
              if (key === 'account_count') {
                return (
                  <div className="space-y-0.5 text-xs">
                    <div>
                      <span className="surface-muted">{t('admin.groups.accountsAvailable')}</span>
                      <span className="ml-1 font-medium text-emerald-600 dark:text-emerald-400">
                        {row.active_account_count || 0}
                      </span>
                      <span className="ml-1 inline-flex items-center rounded surface-inset px-1.5 py-0.5 font-medium text-gray-800 dark:text-gray-300">
                        {t('admin.groups.accountsUnit')}
                      </span>
                    </div>
                    {!!row.rate_limited_account_count && (
                      <div>
                        <span className="surface-muted">{t('admin.groups.accountsRateLimited')}</span>
                        <span className="ml-1 font-medium text-amber-600 dark:text-amber-400">
                          {row.rate_limited_account_count}
                        </span>
                        <span className="ml-1 inline-flex items-center rounded surface-inset px-1.5 py-0.5 font-medium text-gray-800 dark:text-gray-300">
                          {t('admin.groups.accountsUnit')}
                        </span>
                      </div>
                    )}
                  </div>
                )
              }
              if (key === 'capacity') {
                const capacity = capacityMap.get(row.id)
                return capacity ? <GroupCapacityBadge {...capacity} /> : <span className="text-xs surface-muted">—</span>
              }
              if (key === 'usage') {
                if (usageLoading) return <div className="text-xs surface-muted">—</div>
                return (
                  <div className="space-y-0.5 text-xs">
                    <div className="surface-muted">
                      <span className="surface-muted">{t('admin.groups.usageToday')}</span>
                      <span className="ml-1 font-medium surface-muted">${formatCost(usageMap.get(row.id)?.today_cost ?? 0)}</span>
                    </div>
                    <div className="surface-muted">
                      <span className="surface-muted">{t('admin.groups.usageYesterday')}</span>
                      <span className="ml-1 font-medium surface-muted">${formatCost(usageMap.get(row.id)?.yesterday_cost ?? 0)}</span>
                    </div>
                    <div className="surface-muted">
                      <span className="surface-muted">{t('admin.groups.usageTotal')}</span>
                      <span className="ml-1 font-medium surface-muted">${formatCost(usageMap.get(row.id)?.total_cost ?? 0)}</span>
                    </div>
                  </div>
                )
              }
              if (key === 'status') {
                return (
                  <span className={`badge ${row.status === 'active' ? 'badge-success' : 'badge-danger'}`}>
                    {t('admin.accounts.status.' + row.status)}
                  </span>
                )
              }
              if (key === 'actions') {
                const duplicating = duplicatingGroupIds.has(row.id)
                return (
                  <div className="table-actions">
                    <button className="table-action">
                      <Pencil className="h-3.5 w-3.5" />
                      <span className="text-xs">{t('common.edit')}</span>
                    </button>
                    {!isSimpleMode && (
                      <button
                        data-testid="group-duplicate"
                        title={duplicating ? t('admin.groups.duplicating') : t('admin.groups.duplicate')}
                        disabled={duplicating}
                        onClick={() => handleDuplicate(row)}
                        className="table-action"
                      >
                        <Copy className="h-3.5 w-3.5" />
                        <span className="text-xs">
                          {duplicating ? t('admin.groups.duplicating') : t('admin.groups.duplicate')}
                        </span>
                      </button>
                    )}
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
                title={t('admin.groups.noGroupsYet')}
                description={t('admin.groups.createFirstGroup')}
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

      <ConfirmDialogLite
        open={showDeleteDialog}
        title={t('admin.groups.deleteGroup')}
        message={deleteConfirmMessage}
        onConfirm={confirmDelete}
        onCancel={() => setShowDeleteDialog(false)}
      />
    </>
  )
}

function ConfirmDialogLite({
  open,
  title,
  message,
  onConfirm,
  onCancel
}: {
  open: boolean
  title: string
  message: string
  onConfirm: () => void
  onCancel: () => void
}) {
  const { t } = useTranslation()
  return (
    <ModalShell
      open={open}
      onCancel={onCancel}
      title={title}
      footer={
        <>
          <Button variant="ghost" onClick={onCancel}>
            {t('common.cancel')}
          </Button>
          <Button variant="danger" onClick={onConfirm}>
            {t('common.delete')}
          </Button>
        </>
      }
    >
      <div className="text-sm text-gray-600 dark:text-gray-300">{message}</div>
    </ModalShell>
  )
}

function ModalShell({
  open,
  onCancel,
  title,
  footer,
  children
}: {
  open: boolean
  onCancel: () => void
  title: string
  footer: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <Modal open={open} onClose={onCancel} title={title} size="sm" footer={footer}>
      {children}
    </Modal>
  )
}
