import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { Button, ConfirmDialog, EmptyState, Modal, Pagination, Select, toast } from '@/components/ui'
import { cn } from '@/lib/utils'
import { adminAPI } from '@/api/admin'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { AdminUser, Group, GroupPlatform, SubscriptionType, UserSubscription } from '@/types'
import type { SimpleUser } from '@/api/admin/usage'
import type {
  BulkAssignSubscriptionResult,
  SubscriptionBulkAction,
  SubscriptionBulkActionResult
} from '@/api/admin/subscriptions'
import {
  getRemainingDurationParts,
  getRemainingExpiryDuration,
  isOneTimeDailyQuota,
  type RemainingDurationParts
} from '@/utils/subscriptionQuota'
import { formatDateTimeToMinute } from '@/utils/format'
import { listPlatforms } from '@/utils/platformCatalog'
import { AdminDataTable, type Column, type SortOrder } from './AdminDataTable'
import { AdminTablePageLayout } from './AdminTablePageLayout'
import { getPersistedPageSize } from './shared'
import { Icon } from './AccountIcon'
import { GroupBadge } from './GroupBadge'
import { GroupOptionItem } from './GroupOptionItem'
import { BulkSubscriptionActionDialog } from './BulkSubscriptionActionDialog'

interface GroupOption {
  value: number
  label: string
  description: string | null
  platform: GroupPlatform
  subscriptionType: SubscriptionType
  rate: number
}

const USER_COLUMN_MODE_KEY = 'subscription-user-column-mode'
const HIDDEN_COLUMNS_KEY = 'subscription-hidden-columns'
const DEFAULT_HIDDEN_COLUMNS: string[] = []
const BULK_ACTIONS: SubscriptionBulkAction[] = ['extend', 'reset_quota', 'revoke', 'restore']

const GROUP_PLATFORM_OPTIONS: Array<{ value: GroupPlatform; label: string }> = [
  ...listPlatforms().map((spec) => ({ value: spec.id as GroupPlatform, label: spec.display_name })),
  { value: 'composite' as GroupPlatform, label: 'Composite' }
]

interface AssignForm {
  user_id: number | null
  group_id: number | null
  validity_days: number
}

function getProgressWidth(used: number | null | undefined, limit: number | null): string {
  if (!limit || limit === 0) return '0%'
  const usedValue = used ?? 0
  const percentage = Math.min((usedValue / limit) * 100, 100)
  return `${percentage}%`
}

function getProgressClass(used: number | null | undefined, limit: number | null): string {
  if (!limit || limit === 0) return 'bg-gray-400'
  const usedValue = used ?? 0
  const percentage = (usedValue / limit) * 100
  if (percentage >= 90) return 'bg-red-500'
  if (percentage >= 70) return 'bg-orange-500'
  return 'bg-green-500'
}

function getDaysRemaining(expiresAt: string): number | null {
  const now = new Date()
  const expires = new Date(expiresAt)
  const diff = expires.getTime() - now.getTime()
  if (diff < 0) return null
  return Math.ceil(diff / (1000 * 60 * 60 * 24))
}

function isExpiringSoon(expiresAt: string): boolean {
  const days = getDaysRemaining(expiresAt)
  return days !== null && days <= 7
}

interface UsageRowProps {
  label: string
  used: number | undefined
  limit: number | null
  windowStart: string | null
  resetText: string
}

function UsageRow({ label, used, limit, windowStart, resetText }: UsageRowProps) {
  return (
    <div className="usage-row">
      <div className="flex items-center gap-2">
        <span className="usage-label">{label}</span>
        <div className="h-1.5 flex-1 rounded-full bg-gray-200 dark:bg-dark-600">
          <div
            className={cn('console-data-mark h-1.5 rounded-full transition-all', getProgressClass(used, limit))}
            style={{ width: getProgressWidth(used, limit) }}
          />
        </div>
        <span className="usage-amount">
          ${used?.toFixed(2) || '0.00'}
          <span className="surface-muted">/</span>
          ${limit?.toFixed(2)}
        </span>
      </div>
      {windowStart && (
        <div className="reset-info">
          <svg className="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
            />
          </svg>
          <span>{resetText}</span>
        </div>
      )}
    </div>
  )
}

export default function SubscriptionsView() {
  const { t } = useTranslation()

  const [subscriptions, setSubscriptions] = useState<UserSubscription[]>([])
  const [groups, setGroups] = useState<Group[]>([])
  const [loading, setLoading] = useState(false)
  const [selectedIds, setSelectedIds] = useState<Array<string | number>>([])

  const [filters, setFilters] = useState({
    status: 'active',
    group_id: '',
    platform: '',
    user_id: null as number | null
  })
  const [sortState, setSortState] = useState<{ sort_by: string; sort_order: SortOrder }>({
    sort_by: 'created_at',
    sort_order: 'desc'
  })
  const [pagination, setPagination] = useState({ page: 1, page_size: getPersistedPageSize(), total: 0, pages: 0 })

  const [userColumnMode, setUserColumnMode] = useState<'email' | 'username'>('email')
  const [hiddenColumns, setHiddenColumns] = useState<Set<string>>(() => new Set(DEFAULT_HIDDEN_COLUMNS))
  const [showColumnDropdown, setShowColumnDropdown] = useState(false)
  const [showGuideModal, setShowGuideModal] = useState(false)

  // Toolbar user filter (fuzzy search -> select user_id)
  const [filterUserKeyword, setFilterUserKeyword] = useState('')
  const [filterUserResults, setFilterUserResults] = useState<SimpleUser[]>([])
  const [filterUserLoading, setFilterUserLoading] = useState(false)
  const [showFilterUserDropdown, setShowFilterUserDropdown] = useState(false)
  const [selectedFilterUser, setSelectedFilterUser] = useState<SimpleUser | null>(null)

  // Assign modal user search
  const [userSearchKeyword, setUserSearchKeyword] = useState('')
  const [userSearchResults, setUserSearchResults] = useState<AdminUser[]>([])
  const [userSearchLoading, setUserSearchLoading] = useState(false)
  const [showUserDropdown, setShowUserDropdown] = useState(false)
  const [selectedUser, setSelectedUser] = useState<AdminUser | null>(null)
  const [batchAssignEnabled, setBatchAssignEnabled] = useState(false)
  const [assignUsers, setAssignUsers] = useState<AdminUser[]>([])
  const [batchAssignResult, setBatchAssignResult] = useState<BulkAssignSubscriptionResult | null>(null)

  const [assignForm, setAssignForm] = useState<AssignForm>({
    user_id: null,
    group_id: null,
    validity_days: 30
  })
  const [showAssignModal, setShowAssignModal] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  const [showExtendModal, setShowExtendModal] = useState(false)
  const [extendingSubscription, setExtendingSubscription] = useState<UserSubscription | null>(null)
  const [extendDays, setExtendDays] = useState<number | string>(30)

  const [showRevokeDialog, setShowRevokeDialog] = useState(false)
  const [revokingSubscription, setRevokingSubscription] = useState<UserSubscription | null>(null)
  const [showRestoreDialog, setShowRestoreDialog] = useState(false)
  const [restoringSubscription, setRestoringSubscription] = useState<UserSubscription | null>(null)
  const [showResetQuotaConfirm, setShowResetQuotaConfirm] = useState(false)
  const [resettingSubscription, setResettingSubscription] = useState<UserSubscription | null>(null)
  const [resettingQuota, setResettingQuota] = useState(false)

  const [bulkAction, setBulkAction] = useState<SubscriptionBulkAction | null>(null)
  const [bulkSubscriptions, setBulkSubscriptions] = useState<UserSubscription[]>([])

  const abortController = useRef<AbortController | null>(null)
  const filterUserSearchTimeout = useRef<number | null>(null)
  const userSearchTimeout = useRef<number | null>(null)
  const columnDropdownRef = useRef<HTMLDivElement | null>(null)

  // Query state is mirrored into a ref so a reload always sends the values the
  // triggering control just produced, even though the state update has not been
  // committed yet.
  const stateRef = useRef({ filters, sortState, pagination })
  stateRef.current = { filters, sortState, pagination }
  const subscriptionsRef = useRef(subscriptions)
  subscriptionsRef.current = subscriptions
  const filterUserKeywordRef = useRef(filterUserKeyword)
  filterUserKeywordRef.current = filterUserKeyword
  const selectedFilterUserRef = useRef(selectedFilterUser)
  selectedFilterUserRef.current = selectedFilterUser
  const userSearchKeywordRef = useRef(userSearchKeyword)
  userSearchKeywordRef.current = userSearchKeyword
  const selectedUserRef = useRef(selectedUser)
  selectedUserRef.current = selectedUser

  const statusOptions = useMemo(
    () => [
      { value: '', label: t('admin.subscriptions.allStatus') },
      { value: 'active', label: t('admin.subscriptions.status.active') },
      { value: 'expired', label: t('admin.subscriptions.status.expired') },
      { value: 'revoked', label: t('admin.subscriptions.status.revoked') }
    ],
    [t]
  )

  const groupOptions = useMemo(
    () => [
      { value: '', label: t('admin.subscriptions.allGroups') },
      ...groups.map((group) => ({ value: group.id.toString(), label: group.name }))
    ],
    [groups, t]
  )

  const platformFilterOptions = useMemo(
    () => [{ value: '', label: t('admin.subscriptions.allPlatforms') }, ...GROUP_PLATFORM_OPTIONS],
    [t]
  )

  const subscriptionGroupOptions = useMemo<GroupOption[]>(
    () =>
      groups
        .filter((group) => group.subscription_type === 'subscription' && group.status === 'active')
        .map((group) => ({
          value: group.id,
          label: group.name,
          description: group.description,
          platform: group.platform,
          subscriptionType: group.subscription_type,
          rate: group.rate_multiplier
        })),
    [groups]
  )

  const allColumns = useMemo<Column[]>(
    () => [
      {
        key: 'user',
        label:
          userColumnMode === 'email'
            ? t('admin.subscriptions.columns.user')
            : t('admin.users.columns.username'),
        sortable: false
      },
      { key: 'group', label: t('admin.subscriptions.columns.group'), sortable: false },
      { key: 'usage', label: t('admin.subscriptions.columns.usage'), sortable: false },
      { key: 'expires_at', label: t('admin.subscriptions.columns.expires'), sortable: true },
      { key: 'status', label: t('admin.subscriptions.columns.status'), sortable: true },
      { key: 'actions', label: t('admin.subscriptions.columns.actions'), sortable: false }
    ],
    [t, userColumnMode]
  )

  const toggleableColumns = useMemo(
    () => allColumns.filter((column) => column.key !== 'user' && column.key !== 'actions'),
    [allColumns]
  )

  const columns = useMemo(
    () =>
      allColumns.filter(
        (column) => column.key === 'user' || column.key === 'actions' || !hiddenColumns.has(column.key)
      ),
    [allColumns, hiddenColumns]
  )

  const guideActionRows = useMemo(
    () => [
      { action: t('admin.subscriptions.guide.actions.adjust'), desc: t('admin.subscriptions.guide.actions.adjustDesc') },
      {
        action: t('admin.subscriptions.guide.actions.resetQuota'),
        desc: t('admin.subscriptions.guide.actions.resetQuotaDesc')
      },
      { action: t('admin.subscriptions.guide.actions.revoke'), desc: t('admin.subscriptions.guide.actions.revokeDesc') }
    ],
    [t]
  )

  const selectedCount = selectedIds.length

  const bulkTargets = useMemo(() => {
    const selected = subscriptions.filter((subscription) => selectedIds.includes(subscription.id))
    return {
      extend: selected.filter((subscription) => ['active', 'expired'].includes(subscription.status)),
      reset_quota: selected.filter((subscription) => subscription.status === 'active'),
      revoke: selected.filter((subscription) => subscription.status === 'active'),
      restore: selected.filter((subscription) => subscription.status === 'revoked')
    }
  }, [subscriptions, selectedIds])

  const clearSelection = useCallback(() => setSelectedIds([]), [])

  const removeSelectedIds = useCallback((ids: number[]) => {
    setSelectedIds((previous) => previous.filter((key) => !ids.includes(key as number)))
  }, [])

  const loadSubscriptions = useCallback(async () => {
    const current = stateRef.current
    abortController.current?.abort()
    const requestController = new AbortController()
    abortController.current = requestController
    const { signal } = requestController

    setLoading(true)
    try {
      const response = await adminAPI.subscriptions.list(
        current.pagination.page,
        current.pagination.page_size,
        {
          status: (current.filters.status || undefined) as
            | 'active'
            | 'expired'
            | 'revoked'
            | 'suspended'
            | undefined,
          group_id: current.filters.group_id ? parseInt(current.filters.group_id) : undefined,
          platform: current.filters.platform || undefined,
          user_id: current.filters.user_id || undefined,
          sort_by: current.sortState.sort_by,
          sort_order: current.sortState.sort_order
        },
        { signal }
      )
      if (signal.aborted || abortController.current !== requestController) return
      setSubscriptions(response.items)
      const visibleIds = new Set(response.items.map((subscription) => subscription.id))
      setSelectedIds((previous) => previous.filter((key) => typeof key === 'number' && visibleIds.has(key)))
      setPagination((previous) => ({ ...previous, total: response.total, pages: response.pages }))
    } catch (error) {
      const err = error as { name?: string; code?: string }
      if (signal.aborted || err?.name === 'AbortError' || err?.code === 'ERR_CANCELED') {
        return
      }
      console.error('Error loading subscriptions:', error)
      toast.error(t('admin.subscriptions.failedToLoad'))
    } finally {
      if (abortController.current === requestController) {
        setLoading(false)
        abortController.current = null
      }
    }
  }, [t])

  const loadGroups = useCallback(async () => {
    try {
      setGroups(await adminAPI.groups.getAll())
    } catch (error) {
      console.error('Error loading groups:', error)
    }
  }, [])

  const applyFilters = useCallback(
    (patch: Partial<typeof filters> = {}) => {
      clearSelection()
      const current = stateRef.current
      const nextFilters = { ...current.filters, ...patch }
      stateRef.current = {
        ...current,
        filters: nextFilters,
        pagination: { ...current.pagination, page: 1 }
      }
      setFilters(nextFilters)
      setPagination(stateRef.current.pagination)
      void loadSubscriptions()
    },
    [clearSelection, loadSubscriptions]
  )

  // Toolbar user filter search with debounce
  const debounceSearchFilterUsers = useCallback(() => {
    if (filterUserSearchTimeout.current) window.clearTimeout(filterUserSearchTimeout.current)
    filterUserSearchTimeout.current = window.setTimeout(async () => {
      const keyword = filterUserKeywordRef.current.trim()

      // Clear active user filter if user modified the search keyword
      if (selectedFilterUserRef.current && keyword !== selectedFilterUserRef.current.email) {
        setSelectedFilterUser(null)
        applyFilters({ user_id: null })
      }

      if (!keyword) {
        setFilterUserResults([])
        return
      }

      setFilterUserLoading(true)
      try {
        setFilterUserResults(await adminAPI.usage.searchUsers(keyword))
      } catch (error) {
        console.error('Failed to search users:', error)
        setFilterUserResults([])
      } finally {
        setFilterUserLoading(false)
      }
    }, 300)
  }, [applyFilters])

  const selectFilterUser = useCallback(
    (user: SimpleUser) => {
      setSelectedFilterUser(user)
      setFilterUserKeyword(user.email)
      setShowFilterUserDropdown(false)
      applyFilters({ user_id: user.id })
    },
    [applyFilters]
  )

  const clearFilterUser = useCallback(() => {
    setSelectedFilterUser(null)
    setFilterUserKeyword('')
    setFilterUserResults([])
    setShowFilterUserDropdown(false)
    applyFilters({ user_id: null })
  }, [applyFilters])

  // User search with debounce
  const debounceSearchUsers = useCallback(() => {
    // Invalidate the assignment target before the debounced search runs.
    if (selectedUserRef.current && userSearchKeywordRef.current.trim() !== selectedUserRef.current.email) {
      setSelectedUser(null)
      setAssignForm((previous) => ({ ...previous, user_id: null }))
    }
    if (userSearchTimeout.current) window.clearTimeout(userSearchTimeout.current)
    userSearchTimeout.current = window.setTimeout(async () => {
      const keyword = userSearchKeywordRef.current.trim()

      if (!keyword) {
        setUserSearchResults([])
        return
      }

      setUserSearchLoading(true)
      try {
        const result = await adminAPI.users.list(1, 30, {
          search: keyword,
          sort_by: 'email',
          sort_order: 'asc'
        })
        setUserSearchResults(result.items)
      } catch (error) {
        console.error('Failed to search users:', error)
        setUserSearchResults([])
      } finally {
        setUserSearchLoading(false)
      }
    }, 300)
  }, [])

  const selectUser = useCallback(
    (user: AdminUser) => {
      if (submitting) return
      if (batchAssignEnabled) {
        if (assignUsers.length < 100 && !assignUsers.some((selected) => selected.id === user.id)) {
          setAssignUsers((previous) => [...previous, user])
        }
        setUserSearchKeyword('')
        setUserSearchResults([])
        setShowUserDropdown(false)
        return
      }
      setSelectedUser(user)
      setUserSearchKeyword(user.email)
      setShowUserDropdown(false)
      setAssignForm((previous) => ({ ...previous, user_id: user.id }))
    },
    [assignUsers, batchAssignEnabled, submitting]
  )

  const clearUserSelection = useCallback(() => {
    setSelectedUser(null)
    setUserSearchKeyword('')
    setUserSearchResults([])
    setAssignForm((previous) => ({ ...previous, user_id: null }))
  }, [])

  const resetAssignUsers = useCallback(() => {
    clearUserSelection()
    setAssignUsers([])
    setBatchAssignResult(null)
  }, [clearUserSelection])

  const handlePageChange = useCallback(
    (page: number) => {
      clearSelection()
      const current = stateRef.current
      stateRef.current = { ...current, pagination: { ...current.pagination, page } }
      setPagination(stateRef.current.pagination)
      void loadSubscriptions()
    },
    [clearSelection, loadSubscriptions]
  )

  const handlePageSizeChange = useCallback(
    (pageSize: number) => {
      clearSelection()
      const current = stateRef.current
      stateRef.current = {
        ...current,
        pagination: { ...current.pagination, page_size: pageSize, page: 1 }
      }
      setPagination(stateRef.current.pagination)
      void loadSubscriptions()
    },
    [clearSelection, loadSubscriptions]
  )

  const handleSort = useCallback(
    (key: string, order: SortOrder) => {
      clearSelection()
      const current = stateRef.current
      stateRef.current = {
        ...current,
        sortState: { sort_by: key, sort_order: order },
        pagination: { ...current.pagination, page: 1 }
      }
      setSortState(stateRef.current.sortState)
      setPagination(stateRef.current.pagination)
      void loadSubscriptions()
    },
    [clearSelection, loadSubscriptions]
  )

  const resetAssignModal = useCallback(() => {
    setShowAssignModal(false)
    setBatchAssignEnabled(false)
    setAssignUsers([])
    setBatchAssignResult(null)
    setAssignForm({ user_id: null, group_id: null, validity_days: 30 })
    // Clear user search state
    setSelectedUser(null)
    setUserSearchKeyword('')
    setUserSearchResults([])
    setShowUserDropdown(false)
  }, [])

  const closeAssignModal = useCallback(() => {
    if (submitting) return
    resetAssignModal()
  }, [resetAssignModal, submitting])

  const handleAssignSubscription = useCallback(async () => {
    if (submitting) return
    if (batchAssignEnabled ? assignUsers.length === 0 : !assignForm.user_id) {
      toast.error(t('admin.subscriptions.pleaseSelectUser'))
      return
    }
    if (!assignForm.group_id) {
      toast.error(t('admin.subscriptions.pleaseSelectGroup'))
      return
    }
    if (
      !Number.isInteger(assignForm.validity_days) ||
      assignForm.validity_days < 1 ||
      assignForm.validity_days > 36500
    ) {
      toast.error(t('admin.subscriptions.validityDaysRequired'))
      return
    }

    setSubmitting(true)
    try {
      if (batchAssignEnabled) {
        const result = await adminAPI.subscriptions.bulkAssign({
          user_ids: assignUsers.map((user) => user.id),
          group_id: assignForm.group_id,
          validity_days: assignForm.validity_days
        })
        setBatchAssignResult(result)
        const successIds = new Set(result.subscriptions.map((subscription) => subscription.user_id))
        setAssignUsers((previous) => previous.filter((user) => !successIds.has(user.id)))
        if (result.success_count > 0) {
          toast.success(
            t('admin.subscriptions.batchAssign.result', {
              success: result.success_count,
              failed: result.failed_count
            })
          )
          await loadSubscriptions()
        }
        return
      }
      await adminAPI.subscriptions.assign({
        user_id: assignForm.user_id!,
        group_id: assignForm.group_id,
        validity_days: assignForm.validity_days
      })
      toast.success(t('admin.subscriptions.subscriptionAssigned'))
      setSubmitting(false)
      resetAssignModal()
      void loadSubscriptions()
    } catch (error) {
      console.error('Error assigning subscription:', error)
      toast.error(extractApiErrorMessage(error, t('admin.subscriptions.failedToAssign')))
    } finally {
      setSubmitting(false)
    }
  }, [assignForm, assignUsers, batchAssignEnabled, loadSubscriptions, resetAssignModal, submitting, t])

  const handleExtend = useCallback((subscription: UserSubscription) => {
    setExtendingSubscription(subscription)
    setExtendDays(30)
    setShowExtendModal(true)
  }, [])

  const closeExtendModal = useCallback(() => {
    setShowExtendModal(false)
    setExtendingSubscription(null)
  }, [])

  const handleExtendSubscription = useCallback(async () => {
    if (!extendingSubscription) return

    // 前端验证：调整后的过期时间必须在未来
    if (extendingSubscription.expires_at) {
      const expiresAt = new Date(extendingSubscription.expires_at)
      const newExpiresAt = new Date(expiresAt.getTime() + Number(extendDays) * 24 * 60 * 60 * 1000)
      if (newExpiresAt <= new Date()) {
        toast.error(t('admin.subscriptions.adjustWouldExpire'))
        return
      }
    }

    setSubmitting(true)
    try {
      await adminAPI.subscriptions.extend(extendingSubscription.id, { days: Number(extendDays) })
      toast.success(t('admin.subscriptions.subscriptionAdjusted'))
      closeExtendModal()
      void loadSubscriptions()
    } catch (error) {
      console.error('Error adjusting subscription:', error)
      toast.error(extractApiErrorMessage(error, t('admin.subscriptions.failedToAdjust')))
    } finally {
      setSubmitting(false)
    }
  }, [closeExtendModal, extendDays, extendingSubscription, loadSubscriptions, t])

  const handleRevoke = useCallback((subscription: UserSubscription) => {
    setRevokingSubscription(subscription)
    setShowRevokeDialog(true)
  }, [])

  const confirmRevoke = useCallback(async () => {
    if (!revokingSubscription) return

    try {
      await adminAPI.subscriptions.revoke(revokingSubscription.id)
      toast.success(t('admin.subscriptions.subscriptionRevoked'))
      setShowRevokeDialog(false)
      setRevokingSubscription(null)
      void loadSubscriptions()
    } catch (error) {
      console.error('Error revoking subscription:', error)
      toast.error(extractApiErrorMessage(error, t('admin.subscriptions.failedToRevoke')))
    }
  }, [loadSubscriptions, revokingSubscription, t])

  const handleRestore = useCallback((subscription: UserSubscription) => {
    setRestoringSubscription(subscription)
    setShowRestoreDialog(true)
  }, [])

  const confirmRestore = useCallback(async () => {
    if (!restoringSubscription) return

    try {
      await adminAPI.subscriptions.restore(restoringSubscription.id)
      toast.success(t('admin.subscriptions.subscriptionRestored'))
      setShowRestoreDialog(false)
      setRestoringSubscription(null)
      void loadSubscriptions()
    } catch (error) {
      console.error('Error restoring subscription:', error)
      toast.error(extractApiErrorMessage(error, t('admin.subscriptions.failedToRestore')))
    }
  }, [loadSubscriptions, restoringSubscription, t])

  const handleResetQuota = useCallback((subscription: UserSubscription) => {
    setResettingSubscription(subscription)
    setShowResetQuotaConfirm(true)
  }, [])

  const confirmResetQuota = useCallback(async () => {
    if (!resettingSubscription) return
    if (resettingQuota) return
    setResettingQuota(true)
    try {
      await adminAPI.subscriptions.resetQuota(resettingSubscription.id, {
        daily: true,
        weekly: true,
        monthly: true,
        five_hour: true
      })
      toast.success(t('admin.subscriptions.quotaResetSuccess'))
      setShowResetQuotaConfirm(false)
      setResettingSubscription(null)
      await loadSubscriptions()
    } catch (error) {
      console.error('Error resetting quota:', error)
      toast.error(extractApiErrorMessage(error, t('admin.subscriptions.failedToResetQuota')))
    } finally {
      setResettingQuota(false)
    }
  }, [loadSubscriptions, resettingQuota, resettingSubscription, t])

  const openBulkAction = useCallback(
    (action: SubscriptionBulkAction) => {
      if (loading || bulkTargets[action].length === 0) return
      setBulkSubscriptions([...bulkTargets[action]])
      setBulkAction(action)
    },
    [bulkTargets, loading]
  )

  const handleBulkCompleted = useCallback(
    async (result: SubscriptionBulkActionResult) => {
      removeSelectedIds(result.results.filter((item) => item.success).map((item) => item.subscription_id))
      await loadSubscriptions()
    },
    [loadSubscriptions, removeSelectedIds]
  )

  const formatRemainingExpiry = useCallback(
    (expiresAt: string): string | null => {
      const duration = getRemainingExpiryDuration(expiresAt)
      if (!duration) return null
      if (duration.unit === 'days') {
        return t('admin.subscriptions.daysRemaining', { days: duration.days })
      }
      if (duration.hours) {
        return t('admin.subscriptions.hoursMinutesRemaining', {
          hours: duration.hours,
          minutes: duration.minutes
        })
      }
      return t('admin.subscriptions.minutesRemaining', { minutes: duration.minutes })
    },
    [t]
  )

  const formatResetDuration = useCallback(
    (parts: RemainingDurationParts): string => {
      if (parts.days > 0) {
        return t('admin.subscriptions.resetInDaysHours', { days: parts.days, hours: parts.hours })
      }
      if (parts.hours > 0) {
        return t('admin.subscriptions.resetInHoursMinutes', { hours: parts.hours, minutes: parts.minutes })
      }
      return t('admin.subscriptions.resetInMinutes', { minutes: parts.minutes })
    },
    [t]
  )

  const formatQuotaEndDuration = useCallback(
    (parts: RemainingDurationParts): string => {
      if (parts.days > 0) {
        return t('admin.subscriptions.quotaEndsInDaysHours', { days: parts.days, hours: parts.hours })
      }
      if (parts.hours > 0) {
        return t('admin.subscriptions.quotaEndsInHoursMinutes', { hours: parts.hours, minutes: parts.minutes })
      }
      return t('admin.subscriptions.quotaEndsInMinutes', { minutes: parts.minutes })
    },
    [t]
  )

  // Format reset time based on window start and period type
  const formatResetTime = useCallback(
    (windowStart: string | null, period: 'daily' | 'weekly' | 'monthly' | 'five_hour'): string => {
      if (!windowStart) return t('admin.subscriptions.windowNotActive')

      const start = new Date(windowStart)
      const now = new Date()

      // Calculate reset time based on period
      let resetTime: Date
      switch (period) {
        case 'daily':
          resetTime = new Date(start.getTime() + 24 * 60 * 60 * 1000)
          break
        case 'weekly':
          resetTime = new Date(start.getTime() + 7 * 24 * 60 * 60 * 1000)
          break
        case 'monthly':
          resetTime = new Date(start.getTime() + 30 * 24 * 60 * 60 * 1000)
          break
        case 'five_hour':
          resetTime = new Date(start.getTime() + 5 * 60 * 60 * 1000)
          break
      }

      const parts = getRemainingDurationParts(resetTime, now)

      return parts ? formatResetDuration(parts) : t('admin.subscriptions.windowNotActive')
    },
    [formatResetDuration, t]
  )

  const formatDailyUsageWindow = useCallback(
    (subscription: UserSubscription): string => {
      if (isOneTimeDailyQuota(subscription) && subscription.expires_at) {
        const parts = getRemainingDurationParts(subscription.expires_at)
        return parts ? formatQuotaEndDuration(parts) : t('admin.subscriptions.windowNotActive')
      }

      return formatResetTime(subscription.daily_window_start, 'daily')
    },
    [formatQuotaEndDuration, formatResetTime, t]
  )

  const loadUserColumnMode = useCallback(() => {
    try {
      const saved = localStorage.getItem(USER_COLUMN_MODE_KEY)
      if (saved === 'email' || saved === 'username') {
        setUserColumnMode(saved)
      }
    } catch (error) {
      console.error('Failed to load user column mode:', error)
    }
  }, [])

  const saveUserColumnMode = useCallback((mode: 'email' | 'username') => {
    try {
      localStorage.setItem(USER_COLUMN_MODE_KEY, mode)
    } catch (error) {
      console.error('Failed to save user column mode:', error)
    }
  }, [])

  const setUserColumnModeAndPersist = useCallback(
    (mode: 'email' | 'username') => {
      setUserColumnMode(mode)
      saveUserColumnMode(mode)
    },
    [saveUserColumnMode]
  )

  const loadSavedColumns = useCallback(() => {
    try {
      const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
      const next = new Set<string>()
      if (saved) {
        const parsed = JSON.parse(saved) as string[]
        parsed.forEach((key) => next.add(key))
      } else {
        DEFAULT_HIDDEN_COLUMNS.forEach((key) => next.add(key))
      }
      setHiddenColumns(next)
    } catch (error) {
      console.error('Failed to load saved columns:', error)
      setHiddenColumns(new Set(DEFAULT_HIDDEN_COLUMNS))
    }
  }, [])

  const saveColumnsToStorage = useCallback((next: Set<string>) => {
    try {
      localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...next]))
    } catch (error) {
      console.error('Failed to save columns:', error)
    }
  }, [])

  const toggleColumn = useCallback(
    (key: string) => {
      setHiddenColumns((previous) => {
        const next = new Set(previous)
        if (next.has(key)) next.delete(key)
        else next.add(key)
        saveColumnsToStorage(next)
        return next
      })
    },
    [saveColumnsToStorage]
  )

  const handleSelectedKeysUpdate = useCallback((keys: Array<string | number>) => {
    const visibleIds = new Set(subscriptionsRef.current.map((subscription) => subscription.id))
    setSelectedIds(keys.filter((key): key is number => typeof key === 'number' && visibleIds.has(key)))
  }, [])

  // Handle click outside to close dropdowns
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as HTMLElement
      if (!target.closest('[data-assign-user-search]')) setShowUserDropdown(false)
      if (!target.closest('[data-filter-user-search]')) setShowFilterUserDropdown(false)
      if (columnDropdownRef.current && !columnDropdownRef.current.contains(target)) {
        setShowColumnDropdown(false)
      }
    }
    document.addEventListener('click', handleClickOutside)
    return () => document.removeEventListener('click', handleClickOutside)
  }, [])

  useEffect(() => {
    loadUserColumnMode()
    loadSavedColumns()
    void loadSubscriptions()
    void loadGroups()
    return () => {
      if (filterUserSearchTimeout.current) window.clearTimeout(filterUserSearchTimeout.current)
      if (userSearchTimeout.current) window.clearTimeout(userSearchTimeout.current)
      abortController.current?.abort()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const renderUsageCell = (row: UserSubscription) => (
    <div className="min-w-0 space-y-2 sm:min-w-[280px]">
      {row.group?.five_hour_limit_usd && (
        <UsageRow
          label={t('admin.subscriptions.fiveHour')}
          used={row.five_hour_usage_usd}
          limit={row.group.five_hour_limit_usd}
          windowStart={row.five_hour_window_start}
          resetText={
            row.five_hour_window_start
              ? formatResetTime(row.five_hour_window_start, 'five_hour')
              : t('admin.subscriptions.windowNotActive')
          }
        />
      )}

      {row.group?.daily_limit_usd && (
        <UsageRow
          label={t('admin.subscriptions.daily')}
          used={row.daily_usage_usd}
          limit={row.group.daily_limit_usd}
          windowStart={row.daily_window_start}
          resetText={formatDailyUsageWindow(row)}
        />
      )}

      {row.group?.weekly_limit_usd && (
        <UsageRow
          label={t('admin.subscriptions.weekly')}
          used={row.weekly_usage_usd}
          limit={row.group.weekly_limit_usd}
          windowStart={row.weekly_window_start}
          resetText={formatResetTime(row.weekly_window_start, 'weekly')}
        />
      )}

      {row.group?.monthly_limit_usd && (
        <UsageRow
          label={t('admin.subscriptions.monthly')}
          used={row.monthly_usage_usd}
          limit={row.group.monthly_limit_usd}
          windowStart={row.monthly_window_start}
          resetText={formatResetTime(row.monthly_window_start, 'monthly')}
        />
      )}

      {!row.group?.daily_limit_usd &&
        !row.group?.weekly_limit_usd &&
        !row.group?.monthly_limit_usd &&
        !row.group?.five_hour_limit_usd && (
          <div className="flex items-center gap-2 rounded-lg bg-gradient-to-r from-emerald-50 to-teal-50 px-3 py-2 dark:from-emerald-900/20 dark:to-teal-900/20">
            <span className="text-lg text-emerald-600 dark:text-emerald-400">∞</span>
            <span className="text-xs font-medium text-emerald-700 dark:text-emerald-300">
              {t('admin.subscriptions.unlimited')}
            </span>
          </div>
        )}
    </div>
  )

  return (
    <AdminTablePageLayout
      filters={
        <>
          {/* Top Toolbar: Left (search + filters) / Right (actions) */}
          <div className="filter-bar">
            {/* Left: Fuzzy user search + filters (wrap to multiple lines) */}
            <div className="filter-bar filter-bar--inline">
              <div className="filter-field filter-field--search relative" data-filter-user-search>
                <Icon
                  name="search"
                  size="md"
                  className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"
                />
                <input
                  value={filterUserKeyword}
                  type="text"
                  placeholder={t('admin.users.searchUsers')}
                  className="input pl-10 pr-8"
                  onChange={(event) => {
                    setFilterUserKeyword(event.target.value)
                    debounceSearchFilterUsers()
                  }}
                  onFocus={() => setShowFilterUserDropdown(true)}
                />
                {selectedFilterUser && (
                  <button
                    type="button"
                    className="absolute right-2 top-1/2 -translate-y-1/2 hover:text-gray-600 dark:hover:text-gray-300 surface-control surface-muted"
                    title={t('common.clear')}
                    onClick={clearFilterUser}
                  >
                    <Icon name="x" size="sm" strokeWidth={2} />
                  </button>
                )}

                {showFilterUserDropdown && (filterUserResults.length > 0 || filterUserKeyword) && (
                  <div className="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border border-gray-200 shadow-lg dark:border-dark-700 surface-panel">
                    {filterUserLoading ? (
                      <div className="px-4 py-3 text-sm surface-muted">{t('common.loading')}</div>
                    ) : filterUserResults.length === 0 && filterUserKeyword ? (
                      <div className="px-4 py-3 text-sm surface-muted">{t('common.noOptionsFound')}</div>
                    ) : (
                      filterUserResults.map((user) => (
                        <button
                          key={user.id}
                          type="button"
                          className="w-full px-4 py-2 text-left text-sm hover:surface-hover surface-control"
                          onClick={() => selectFilterUser(user)}
                        >
                          <span className="font-medium text-gray-900 dark:text-white">{user.email}</span>
                          <span className="ml-2 surface-muted">#{user.id}</span>
                        </button>
                      ))
                    )}
                  </div>
                )}
              </div>

              <div className="filter-field">
                <Select
                  value={filters.status}
                  options={statusOptions}
                  searchable={false}
                  placeholder={t('admin.subscriptions.allStatus')}
                  onValueChange={(next) => applyFilters({ status: String(next ?? '') })}
                />
              </div>
              <div className="filter-field">
                <Select
                  value={filters.group_id}
                  options={groupOptions}
                  searchable={false}
                  placeholder={t('admin.subscriptions.allGroups')}
                  onValueChange={(next) => applyFilters({ group_id: String(next ?? '') })}
                />
              </div>
              <div className="filter-field">
                <Select
                  value={filters.platform}
                  options={platformFilterOptions}
                  searchable={false}
                  placeholder={t('admin.subscriptions.allPlatforms')}
                  onValueChange={(next) => applyFilters({ platform: String(next ?? '') })}
                />
              </div>
            </div>

            {/* Right: Actions */}
            <div className="filter-actions">
              <Button
                variant="secondary"
                onClick={() => void loadSubscriptions()}
                disabled={loading}
                title={t('common.refresh')}
              >
                <Icon name="refresh" size="md" className={loading ? 'animate-spin' : ''} />
              </Button>
              <div className="relative" ref={columnDropdownRef}>
                <Button
                  variant="secondary"
                  className="px-2 md:px-3"
                  title={t('admin.users.columnSettings')}
                  onClick={() => setShowColumnDropdown((previous) => !previous)}
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
                      d="M9 4.5v15m6-15v15m-10.875 15h15.75c.621 0 1.125-.504 1.125-1.125V5.625c0 .621-.504 1.125-1.125 1.125H4.125C3.504 4.5 3 5.004 3 5.625v12.75c0 .621.504 1.125 1.125 1.125z"
                    />
                  </svg>
                  <span className="hidden md:inline">{t('admin.users.columnSettings')}</span>
                </Button>
                {showColumnDropdown && (
                  <div className="absolute right-0 z-50 mt-2 w-48 origin-top-right rounded-lg border border-gray-200 shadow-lg dark:border-dark-700 surface-panel">
                    <div className="p-2">
                      <div className="mb-2 border-b border-gray-200 pb-2 dark:border-dark-700">
                        <div className="px-3 py-1 text-xs font-medium surface-muted">
                          {t('admin.subscriptions.columns.user')}
                        </div>
                        <button
                          type="button"
                          className="flex w-full items-center justify-between rounded-md px-3 py-2 text-sm hover:surface-hover surface-control surface-muted"
                          onClick={() => setUserColumnModeAndPersist('email')}
                        >
                          <span>{t('admin.users.columns.email')}</span>
                          {userColumnMode === 'email' && <Icon name="check" size="sm" className="text-primary-500" />}
                        </button>
                        <button
                          type="button"
                          className="flex w-full items-center justify-between rounded-md px-3 py-2 text-sm hover:surface-hover surface-control surface-muted"
                          onClick={() => setUserColumnModeAndPersist('username')}
                        >
                          <span>{t('admin.users.columns.username')}</span>
                          {userColumnMode === 'username' && <Icon name="check" size="sm" className="text-primary-500" />}
                        </button>
                      </div>
                      {toggleableColumns.map((column) => (
                        <button
                          key={column.key}
                          type="button"
                          className="flex w-full items-center justify-between rounded-md px-3 py-2 text-sm hover:surface-hover surface-control surface-muted"
                          onClick={() => toggleColumn(column.key)}
                        >
                          <span>{column.label}</span>
                          {!hiddenColumns.has(column.key) && (
                            <Icon name="check" size="sm" className="text-primary-500" />
                          )}
                        </button>
                      ))}
                    </div>
                  </div>
                )}
              </div>
              <Button
                variant="secondary"
                title={t('admin.subscriptions.guide.showGuide')}
                onClick={() => setShowGuideModal(true)}
              >
                <Icon name="questionCircle" size="md" />
              </Button>
              <Button variant="primary" onClick={() => setShowAssignModal(true)}>
                <Icon name="plus" size="md" className="mr-2" />
                {t('admin.subscriptions.assignSubscription')}
              </Button>
            </div>
          </div>

          {selectedCount > 0 && (
            <div
              className="mt-3 space-y-2 rounded-xl border border-primary-200 bg-primary-50/[0.15] p-3 dark:border-primary-800 dark:bg-primary-900/20"
              data-test="subscription-bulk-actions"
            >
              <div className="flex flex-wrap items-center gap-2">
                <span className="mr-2 text-sm font-medium text-primary-800 dark:text-primary-200">
                  {t('admin.subscriptions.bulk.selected', { count: selectedCount })}
                </span>
                {BULK_ACTIONS.map((action) => (
                  <button
                    key={action}
                    type="button"
                    className={action === 'revoke' ? 'btn btn-danger btn-sm' : 'btn btn-secondary btn-sm'}
                    data-test={`bulk-${action}`}
                    disabled={loading || bulkTargets[action].length === 0}
                    onClick={() => openBulkAction(action)}
                  >
                    {t(`admin.subscriptions.bulk.${action}`)} ({bulkTargets[action].length})
                  </button>
                ))}
                <button type="button" className="btn btn-secondary btn-sm" onClick={clearSelection}>
                  {t('admin.subscriptions.bulk.clearSelection')}
                </button>
              </div>
              <p className="text-xs surface-muted">{t('admin.subscriptions.bulk.selectionHint')}</p>
            </div>
          )}
        </>
      }
      table={
        <AdminDataTable<UserSubscription>
          columns={columns}
          data={subscriptions}
          loading={loading}
          rowKey="id"
          selectable
          selectedKeys={selectedIds}
          selectionLabel={(subscription) =>
            t('admin.subscriptions.bulk.selectSubscription', { id: subscription.id })
          }
          serverSideSort
          sortKey={sortState.sort_by}
          sortOrder={sortState.sort_order}
          onSort={handleSort}
          onSelectedKeysChange={handleSelectedKeysUpdate}
          renderCell={(key, row, value) => {
            if (key === 'user') {
              return (
                <div className="flex items-center gap-2">
                  <div className="flex h-8 w-8 items-center justify-center rounded-full bg-primary-100/[0.15] dark:bg-primary-900/30">
                    <span className="text-sm font-medium text-primary-700 dark:text-primary-300">
                      {userColumnMode === 'email'
                        ? row.user?.email?.charAt(0).toUpperCase() || '?'
                        : row.user?.username?.charAt(0).toUpperCase() || '?'}
                    </span>
                  </div>
                  <Link
                    to={`/admin/usage?user_id=${row.user_id}`}
                    className="rounded font-medium text-gray-900 hover:text-primary-600 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:text-white dark:hover:text-primary-400 dark:focus-visible:ring-offset-dark-800"
                  >
                    {userColumnMode === 'email'
                      ? row.user?.email || t('admin.redeem.userPrefix', { id: row.user_id })
                      : row.user?.username || t('admin.redeem.userPrefix', { id: row.user_id })}
                  </Link>
                </div>
              )
            }
            if (key === 'group') {
              return row.group ? (
                <div className="max-w-full">
                  <GroupBadge
                    name={row.group.name}
                    platform={row.group.platform}
                    subscriptionType={row.group.subscription_type}
                    rateMultiplier={row.group.rate_multiplier}
                    showRate={false}
                  />
                </div>
              ) : (
                <span className="text-sm surface-muted">-</span>
              )
            }
            if (key === 'usage') {
              return renderUsageCell(row)
            }
            if (key === 'expires_at') {
              if (!value) return <span className="text-sm surface-muted">{t('admin.subscriptions.noExpiration')}</span>
              return (
                <div>
                  <span
                    className={
                      isExpiringSoon(String(value))
                        ? 'text-sm text-orange-600 dark:text-orange-400'
                        : 'text-sm surface-muted'
                    }
                  >
                    {formatDateTimeToMinute(String(value))}
                  </span>
                  {formatRemainingExpiry(String(value)) && (
                    <div className="text-xs surface-muted">{formatRemainingExpiry(String(value))}</div>
                  )}
                </div>
              )
            }
            if (key === 'status') {
              const badge =
                value === 'active' ? 'badge-success' : value === 'expired' ? 'badge-warning' : 'badge-danger'
              return <span className={`badge ${badge}`}>{t(`admin.subscriptions.status.${value}`)}</span>
            }
            if (key === 'actions') {
              return (
                <div className="table-actions">
                  {(row.status === 'active' || row.status === 'expired') && (
                    <button type="button" className="table-action" onClick={() => handleExtend(row)}>
                      <Icon name="calendar" size="sm" />
                      <span className="text-xs">{t('admin.subscriptions.adjust')}</span>
                    </button>
                  )}
                  {row.status === 'active' && (
                    <button
                      type="button"
                      className="table-action table-action-warning"
                      disabled={resettingQuota && resettingSubscription?.id === row.id}
                      onClick={() => handleResetQuota(row)}
                    >
                      <Icon name="refresh" size="sm" />
                      <span className="text-xs">{t('admin.subscriptions.resetQuota')}</span>
                    </button>
                  )}
                  {row.status === 'active' && (
                    <button
                      type="button"
                      className="table-action table-action-danger"
                      onClick={() => handleRevoke(row)}
                    >
                      <Icon name="ban" size="sm" />
                      <span className="text-xs">{t('admin.subscriptions.revoke')}</span>
                    </button>
                  )}
                  {row.status === 'revoked' && (
                    <button type="button" className="table-action" onClick={() => handleRestore(row)}>
                      <Icon name="refresh" size="sm" />
                      <span className="text-xs">{t('admin.subscriptions.restore')}</span>
                    </button>
                  )}
                </div>
              )
            }
            return <span>{value == null ? '' : String(value)}</span>
          }}
          empty={
            <EmptyState
              title={t('admin.subscriptions.noSubscriptionsYet')}
              description={t('admin.subscriptions.assignFirstSubscription')}
              action={
                <Button variant="primary" onClick={() => setShowAssignModal(true)}>
                  {t('admin.subscriptions.assignSubscription')}
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
        ) : undefined
      }
    >
      {bulkAction && (
        <BulkSubscriptionActionDialog
          action={bulkAction}
          subscriptions={bulkSubscriptions}
          onClose={() => setBulkAction(null)}
          onCompleted={(result) => void handleBulkCompleted(result)}
        />
      )}

      <Modal
        open={showAssignModal}
        onClose={closeAssignModal}
        title={t('admin.subscriptions.assignSubscription')}
        size="md"
        showClose={!submitting}
        closeOnOverlayClick={!submitting}
        footer={
          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={closeAssignModal} disabled={submitting}>
              {batchAssignResult ? t('common.close') : t('common.cancel')}
            </Button>
            <Button
              type="submit"
              form="assign-subscription-form"
              loading={submitting}
              disabled={submitting || (batchAssignEnabled && assignUsers.length === 0)}
            >
              {submitting ? t('admin.subscriptions.assigning') : t('admin.subscriptions.assign')}
            </Button>
          </div>
        }
      >
        <form
          id="assign-subscription-form"
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault()
            void handleAssignSubscription()
          }}
        >
          <label className="flex items-center gap-2 text-sm surface-muted">
            <input
              type="checkbox"
              checked={batchAssignEnabled}
              disabled={submitting}
              onChange={(event) => {
                setBatchAssignEnabled(event.target.checked)
                resetAssignUsers()
              }}
            />
            {t('admin.subscriptions.batchAssign.enable')}
          </label>
          {batchAssignEnabled && <p className="input-hint">{t('admin.subscriptions.batchAssign.hint')}</p>}
          <div>
            <label className="input-label">{t('admin.subscriptions.form.user')}</label>
            <div className="relative" data-assign-user-search>
              <input
                value={userSearchKeyword}
                type="text"
                disabled={submitting || (batchAssignEnabled && assignUsers.length >= 100)}
                className="input pr-8"
                placeholder={t('admin.usage.searchUserPlaceholder')}
                onChange={(event) => {
                  setUserSearchKeyword(event.target.value)
                  debounceSearchUsers()
                }}
                onFocus={() => setShowUserDropdown(true)}
              />
              {selectedUser && (
                <button
                  type="button"
                  className="absolute right-2 top-1/2 -translate-y-1/2 hover:text-gray-600 dark:hover:text-gray-300 surface-control surface-muted"
                  onClick={clearUserSelection}
                >
                  <Icon name="x" size="sm" strokeWidth={2} />
                </button>
              )}
              {showUserDropdown && (userSearchResults.length > 0 || userSearchKeyword) && (
                <div className="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border border-gray-200 shadow-lg dark:border-dark-700 surface-panel">
                  {userSearchLoading ? (
                    <div className="px-4 py-3 text-sm surface-muted">{t('common.loading')}</div>
                  ) : userSearchResults.length === 0 && userSearchKeyword ? (
                    <div className="px-4 py-3 text-sm surface-muted">{t('common.noOptionsFound')}</div>
                  ) : (
                    userSearchResults.map((user) => (
                      <button
                        key={user.id}
                        type="button"
                        disabled={
                          submitting || (batchAssignEnabled && assignUsers.some((selected) => selected.id === user.id))
                        }
                        className="w-full px-4 py-2 text-left text-sm hover:surface-hover disabled:opacity-50 surface-control"
                        onClick={() => selectUser(user)}
                      >
                        <span className="font-medium text-gray-900 dark:text-white">{user.email}</span>
                        <span className="ml-2 surface-muted">#{user.id}</span>
                      </button>
                    ))
                  )}
                </div>
              )}
            </div>
            {batchAssignEnabled && assignUsers.length > 0 && (
              <div className="mt-2 space-y-2" data-test="assign-users">
                <p className="text-sm surface-muted">
                  {t('admin.subscriptions.batchAssign.selected', { count: assignUsers.length })}
                </p>
                <ul className="max-h-40 space-y-1 overflow-y-auto">
                  {assignUsers.map((user) => (
                    <li
                      key={user.id}
                      className="flex items-center justify-between gap-2 rounded-lg bg-gray-50 px-3 py-1 text-sm dark:bg-dark-700"
                    >
                      <span className="truncate">
                        {user.email} <span className="surface-muted">#{user.id}</span>
                      </span>
                      <button
                        type="button"
                        disabled={submitting}
                        aria-label={t('admin.subscriptions.batchAssign.removeUser', { email: user.email })}
                        onClick={() =>
                          setAssignUsers((previous) => previous.filter((selected) => selected.id !== user.id))
                        }
                      >
                        <Icon name="x" size="sm" />
                      </button>
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
          <div>
            <label className="input-label">{t('admin.subscriptions.form.group')}</label>
            <Select
              value={assignForm.group_id ?? undefined}
              disabled={submitting}
              options={subscriptionGroupOptions}
              searchable={false}
              placeholder={t('admin.subscriptions.selectGroup')}
              renderSelected={(option) => {
                const group = option as unknown as GroupOption | null
                return group ? (
                  <GroupBadge
                    name={group.label}
                    platform={group.platform}
                    subscriptionType={group.subscriptionType}
                    rateMultiplier={group.rate}
                  />
                ) : (
                  <span className="surface-muted">{t('admin.subscriptions.selectGroup')}</span>
                )
              }}
              renderOption={(option, { selected }) => {
                const group = option as unknown as GroupOption
                return (
                  <GroupOptionItem
                    name={group.label}
                    platform={group.platform}
                    subscriptionType={group.subscriptionType}
                    rateMultiplier={group.rate}
                    description={group.description}
                    selected={selected}
                  />
                )
              }}
              onValueChange={(next) => setAssignForm((previous) => ({ ...previous, group_id: Number(next) }))}
            />
            <p className="input-hint">{t('admin.subscriptions.groupHint')}</p>
          </div>
          <div>
            <label className="input-label">{t('admin.subscriptions.form.validityDays')}</label>
            <input
              value={assignForm.validity_days}
              type="number"
              min={1}
              max={36500}
              step={1}
              disabled={submitting}
              className="input"
              onChange={(event) =>
                setAssignForm((previous) => ({ ...previous, validity_days: Number(event.target.value) }))
              }
            />
            <p className="input-hint">{t('admin.subscriptions.validityHint')}</p>
          </div>
          {batchAssignResult && (
            <div className="space-y-2 text-sm" role="status" data-test="batch-assign-result">
              <p>
                {t('admin.subscriptions.batchAssign.result', {
                  success: batchAssignResult.success_count,
                  failed: batchAssignResult.failed_count
                })}
              </p>
              {batchAssignResult.errors.length > 0 && (
                <ul className="max-h-40 space-y-1 overflow-y-auto text-red-600 dark:text-red-400">
                  {batchAssignResult.errors.map((error, index) => (
                    <li key={index}>{error}</li>
                  ))}
                </ul>
              )}
              {batchAssignResult.failed_count > 0 && (
                <p className="input-hint">{t('admin.subscriptions.batchAssign.retryHint')}</p>
              )}
            </div>
          )}
        </form>
      </Modal>

      <Modal
        open={showExtendModal}
        onClose={closeExtendModal}
        title={t('admin.subscriptions.adjustSubscription')}
        size="sm"
        footer={
          extendingSubscription ? (
            <div className="flex justify-end gap-3">
              <Button variant="secondary" onClick={closeExtendModal}>
                {t('common.cancel')}
              </Button>
              <Button type="submit" form="extend-subscription-form" loading={submitting}>
                {submitting ? t('admin.subscriptions.adjusting') : t('admin.subscriptions.adjust')}
              </Button>
            </div>
          ) : undefined
        }
      >
        {extendingSubscription && (
          <form
            id="extend-subscription-form"
            className="space-y-5"
            onSubmit={(event) => {
              event.preventDefault()
              void handleExtendSubscription()
            }}
          >
            <div className="rounded-lg surface-inset p-4">
              <p className="text-sm surface-muted">
                {t('admin.subscriptions.adjustingFor')}
                <span className="font-medium text-gray-900 dark:text-white">
                  {extendingSubscription.user?.email}
                </span>
              </p>
              <p className="mt-1 text-sm surface-muted">
                {t('admin.subscriptions.currentExpiration')}:
                <span className="font-medium text-gray-900 dark:text-white">
                  {extendingSubscription.expires_at
                    ? formatDateTimeToMinute(extendingSubscription.expires_at)
                    : t('admin.subscriptions.noExpiration')}
                </span>
              </p>
              {extendingSubscription.expires_at && (
                <p className="mt-1 text-sm surface-muted">
                  {t('admin.subscriptions.remainingDays')}:
                  <span className="font-medium text-gray-900 dark:text-white">
                    {getDaysRemaining(extendingSubscription.expires_at) ?? 0}
                  </span>
                </p>
              )}
            </div>
            <div>
              <label className="input-label" htmlFor="extend-subscription-days">
                {t('admin.subscriptions.form.adjustDays')}
              </label>
              <div className="flex items-center gap-2">
                <input
                  id="extend-subscription-days"
                  value={extendDays}
                  type="number"
                  required
                  className="input text-center"
                  placeholder={t('admin.subscriptions.adjustDaysPlaceholder')}
                  onChange={(event) => setExtendDays(event.target.value)}
                />
              </div>
              <p className="input-hint">{t('admin.subscriptions.adjustHint')}</p>
            </div>
          </form>
        )}
      </Modal>

      <ConfirmDialog
        open={showRevokeDialog}
        title={t('admin.subscriptions.revokeSubscription')}
        message={t('admin.subscriptions.revokeConfirm', { user: revokingSubscription?.user?.email })}
        confirmText={t('admin.subscriptions.revoke')}
        cancelText={t('common.cancel')}
        variant="danger"
        onConfirm={() => void confirmRevoke()}
        onCancel={() => setShowRevokeDialog(false)}
      />

      <ConfirmDialog
        open={showRestoreDialog}
        title={t('admin.subscriptions.restoreSubscription')}
        message={t('admin.subscriptions.restoreConfirm', { user: restoringSubscription?.user?.email })}
        confirmText={t('admin.subscriptions.restore')}
        cancelText={t('common.cancel')}
        onConfirm={() => void confirmRestore()}
        onCancel={() => setShowRestoreDialog(false)}
      />

      <ConfirmDialog
        open={showResetQuotaConfirm}
        title={t('admin.subscriptions.resetQuotaTitle')}
        message={t('admin.subscriptions.resetQuotaConfirm', { user: resettingSubscription?.user?.email })}
        confirmText={t('admin.subscriptions.resetQuota')}
        cancelText={t('common.cancel')}
        variant="danger"
        onConfirm={() => void confirmResetQuota()}
        onCancel={() => setShowResetQuotaConfirm(false)}
      />

      <Modal
        open={showGuideModal}
        onClose={() => setShowGuideModal(false)}
        title={t('admin.subscriptions.guide.title')}
        size="lg"
        footer={
          <div className="text-right">
            <Button variant="primary" size="sm" onClick={() => setShowGuideModal(false)}>
              {t('common.close')}
            </Button>
          </div>
        }
      >
        <div className="space-y-5">
          <p className="mb-5 text-sm surface-muted">{t('admin.subscriptions.guide.subtitle')}</p>

          <div className="mb-5">
            <h3 className="mb-2 flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white">
              <span className="flex h-6 w-6 items-center justify-center rounded-full bg-primary-100/[0.15] text-xs font-bold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">
                1
              </span>
              {t('admin.subscriptions.guide.step1.title')}
            </h3>
            <ol className="ml-8 list-decimal space-y-1 text-sm surface-muted">
              <li>{t('admin.subscriptions.guide.step1.line1')}</li>
              <li>{t('admin.subscriptions.guide.step1.line2')}</li>
              <li>{t('admin.subscriptions.guide.step1.line3')}</li>
            </ol>
            <div className="ml-8 mt-2">
              <Link
                to="/admin/groups"
                onClick={() => setShowGuideModal(false)}
                className="inline-flex items-center gap-1 text-sm font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
              >
                {t('admin.subscriptions.guide.step1.link')}
                <Icon name="arrowRight" size="xs" />
              </Link>
            </div>
          </div>

          <div className="mb-5">
            <h3 className="mb-2 flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white">
              <span className="flex h-6 w-6 items-center justify-center rounded-full bg-primary-100/[0.15] text-xs font-bold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">
                2
              </span>
              {t('admin.subscriptions.guide.step2.title')}
            </h3>
            <ol className="ml-8 list-decimal space-y-1 text-sm surface-muted">
              <li>{t('admin.subscriptions.guide.step2.line1')}</li>
              <li>{t('admin.subscriptions.guide.step2.line2')}</li>
              <li>{t('admin.subscriptions.guide.step2.line3')}</li>
            </ol>
          </div>

          <div className="mb-5">
            <h3 className="mb-2 flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white">
              <span className="flex h-6 w-6 items-center justify-center rounded-full bg-primary-100/[0.15] text-xs font-bold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">
                3
              </span>
              {t('admin.subscriptions.guide.step3.title')}
            </h3>
            <div className="record-table-scroll ml-8 overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600">
              <table className="record-table w-full text-sm">
                <tbody>
                  {guideActionRows.map((row, index) => (
                    <tr key={index} className="border-b border-gray-100 dark:border-dark-700 last:border-0">
                      <td className="whitespace-nowrap surface-inset px-3 py-2 font-medium surface-muted">
                        {row.action}
                      </td>
                      <td className="px-3 py-2 surface-muted">{row.desc}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          <div className="rounded-lg bg-blue-50/[0.15] p-3 text-xs text-blue-700 dark:bg-blue-900/20 dark:text-blue-300">
            {t('admin.subscriptions.guide.tip')}
          </div>
        </div>
      </Modal>
    </AdminTablePageLayout>
  )
}
