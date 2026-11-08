import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState
} from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { adminAPI } from '@/api/admin'
import { Select, type SelectOption } from '@/components/ui'
import { Icon } from '@/components/Icon'
import { cn } from '@/lib/utils'
import { COMMON_ERROR_STATUS_CODES } from '@/utils/errorBadges'
import { useAuthStore } from '@/stores/auth'
import type { AdminUsageQueryParams } from '@/api/admin/usage'
import type { SimpleApiKey, SimpleUser } from '@/api/admin/usage'
import { useAnchoredDropdown } from './useAnchoredDropdown'

export type UsageFilterMode = 'usage' | 'errors' | 'ranking'
export type UsageErrorView = 'errors' | 'excluded' | 'all'

interface SimpleAccount {
  id: number
  name: string
}

export interface UsageFiltersProps {
  filters: AdminUsageQueryParams
  onFiltersChange: (filters: AdminUsageQueryParams) => void
  exporting?: boolean
  startDate: string
  endDate: string
  onStartDateChange?: (value: string) => void
  onEndDateChange?: (value: string) => void
  showActions?: boolean
  modelOptions?: string[]
  /** errors hides usage-only fields/buttons; ranking hides billing mode and cleanup/export. */
  mode?: UsageFilterMode
  /** Current Ops error record view; meaningful only in errors mode. */
  errorView?: UsageErrorView
  onErrorViewChange?: (view: UsageErrorView) => void
  /** Embedded inside the shared query card: drop the card chrome. */
  flat?: boolean
  workspace?: boolean
  className?: string
  onChange?: () => void
  onRefresh?: () => void
  onReset?: () => void
  onExport?: () => void
  onCleanup?: () => void
  afterReset?: React.ReactNode
}

export interface UsageFiltersHandle {
  getUserSearchRevision: () => number
  setUserKeyword: (email: string) => void
}

/**
 * Fields rendered inside the collapsed advanced block, keyed by the mode that
 * gates them. A field the current mode never renders is not part of the query,
 * so counting it would make the badge lie.
 */
const ADVANCED_FILTER_FIELDS: Record<UsageFilterMode, readonly string[]> = {
  usage: [
    'request_type',
    'native_compaction_v2',
    'billing_type',
    'billing_mode',
    'upstream_model_mismatch',
    'completion_status',
    'usage_source',
    'group_id'
  ],
  ranking: [
    'request_type',
    'native_compaction_v2',
    'billing_type',
    'upstream_model_mismatch',
    'completion_status',
    'usage_source',
    'group_id'
  ],
  errors: ['error_phase', 'error_category', 'status_code', 'group_id']
}

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

export const UsageFilters = forwardRef<UsageFiltersHandle, UsageFiltersProps>(function UsageFilters(
  {
    filters,
    onFiltersChange,
    exporting = false,
    startDate,
    endDate,
    onStartDateChange,
    onEndDateChange,
    showActions = true,
    modelOptions,
    mode = 'usage',
    errorView = 'errors',
    onErrorViewChange,
    flat = false,
    workspace = false,
    className,
    onChange,
    onRefresh,
    onReset,
    onExport,
    onCleanup,
    afterReset
  },
  ref
) {
  const { t } = useTranslation()

  const filtersRef = useRef(filters)
  filtersRef.current = filters

  // Debounced searches read the live keyword, never the value captured when the
  // handler was created.
  const userKeywordRef = useRef('')
  const apiKeyKeywordRef = useRef('')
  const accountKeywordRef = useRef('')

  const patch = useCallback(
    (partial: Partial<AdminUsageQueryParams>) => {
      onFiltersChange({ ...filtersRef.current, ...partial })
    },
    [onFiltersChange]
  )

  const userSearchRef = useRef<HTMLDivElement | null>(null)
  const apiKeySearchRef = useRef<HTMLDivElement | null>(null)
  const accountSearchRef = useRef<HTMLDivElement | null>(null)

  const [userKeyword, setUserKeyword] = useState('')
  const [userResults, setUserResults] = useState<SimpleUser[]>([])
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [showUserDropdown, setShowUserDropdown] = useState(false)
  const [apiKeyKeyword, setApiKeyKeyword] = useState('')
  const [apiKeyResults, setApiKeyResults] = useState<SimpleApiKey[]>([])
  const [showApiKeyDropdown, setShowApiKeyDropdown] = useState(false)
  const [accountKeyword, setAccountKeyword] = useState('')
  const [accountResults, setAccountResults] = useState<SimpleAccount[]>([])
  const [showAccountDropdown, setShowAccountDropdown] = useState(false)
  const [groupOptions, setGroupOptions] = useState<SelectOption[]>([
    { value: null, label: t('admin.usage.allGroups') }
  ])

  const userSearchTimeout = useRef<ReturnType<typeof setTimeout> | null>(null)
  const userSearchSequence = useRef(0)
  const apiKeySearchTimeout = useRef<ReturnType<typeof setTimeout> | null>(null)
  const accountSearchTimeout = useRef<ReturnType<typeof setTimeout> | null>(null)

  const userDropdownStyle = useAnchoredDropdown(userSearchRef, workspace && showUserDropdown)
  const apiKeyDropdownStyle = useAnchoredDropdown(apiKeySearchRef, workspace && showApiKeyDropdown)
  const accountDropdownStyle = useAnchoredDropdown(accountSearchRef, workspace && showAccountDropdown)

  const clearPendingUserSearch = useCallback(() => {
    if (userSearchTimeout.current) {
      clearTimeout(userSearchTimeout.current)
      userSearchTimeout.current = null
    }
    userSearchSequence.current += 1
  }, [])

  const updateUserKeyword = useCallback((value: string) => {
    userKeywordRef.current = value
    setUserKeyword(value)
  }, [])

  const updateApiKeyKeyword = useCallback((value: string) => {
    apiKeyKeywordRef.current = value
    setApiKeyKeyword(value)
  }, [])

  const updateAccountKeyword = useCallback((value: string) => {
    accountKeywordRef.current = value
    setAccountKeyword(value)
  }, [])

  const emitChange = useCallback(() => {
    onChange?.()
  }, [onChange])

  const modelSelectOptions = useMemo<SelectOption[]>(
    () => [
      { value: null, label: t('admin.usage.allModels') },
      ...(modelOptions ?? []).map((m) => ({ value: m, label: m }))
    ],
    [modelOptions, t]
  )

  const requestTypeOptions = useMemo<SelectOption[]>(
    () => [
      { value: null, label: t('admin.usage.allTypes') },
      { value: 'ws_v2', label: t('usage.ws') },
      { value: 'live', label: t('usage.live') },
      { value: 'stream', label: t('usage.stream') },
      { value: 'sync', label: t('usage.sync') },
      { value: 'cyber', label: t('usage.cyber') }
    ],
    [t]
  )

  const compactionOptions = useMemo<SelectOption[]>(
    () => [
      { value: null, label: t('usage.allCompactionTypes') },
      { value: true, label: t('usage.compactionOnly') }
    ],
    [t]
  )

  const billingTypeOptions = useMemo<SelectOption[]>(
    () => [
      { value: null, label: t('admin.usage.allBillingTypes') },
      { value: 0, label: t('admin.usage.billingTypeBalance') },
      { value: 1, label: t('admin.usage.billingTypeSubscription') }
    ],
    [t]
  )

  const billingModeOptions = useMemo<SelectOption[]>(
    () => [
      { value: null, label: t('admin.usage.allBillingModes') },
      { value: 'token', label: t('admin.usage.billingModeToken') },
      { value: 'per_request', label: t('admin.usage.billingModePerRequest') },
      { value: 'image', label: t('admin.usage.billingModeImage') },
      { value: 'video', label: t('admin.usage.billingModeVideo') }
    ],
    [t]
  )

  const upstreamModelMismatchOptions = useMemo<SelectOption[]>(
    () => [
      { value: null, label: t('admin.usage.allUpstreamModelAudit') },
      { value: true, label: t('admin.usage.upstreamModelMismatchOnly') },
      { value: false, label: t('admin.usage.upstreamModelMatchedOnly') }
    ],
    [t]
  )

  const completionStatusOptions = useMemo<SelectOption[]>(
    () => [
      { value: null, label: t('admin.usage.allCompletionStatuses') },
      { value: 'completed', label: t('admin.usage.completionCompleted') },
      { value: 'client_disconnected', label: t('usage.clientDisconnected') },
      { value: 'incomplete', label: t('usage.incomplete') },
      { value: 'unknown', label: t('usage.unknown') }
    ],
    [t]
  )

  const usageSourceOptions = useMemo<SelectOption[]>(
    () => [
      { value: null, label: t('admin.usage.allUsageSources') },
      { value: 'upstream_exact', label: t('admin.usage.usageSourceExact') },
      { value: 'partial', label: t('admin.usage.usageSourcePartial') },
      { value: 'estimated', label: t('admin.usage.usageSourceEstimated') },
      { value: 'reconciled', label: t('admin.usage.usageSourceReconciled') },
      { value: 'unknown', label: t('usage.unknown') }
    ],
    [t]
  )

  // 错误类型对应后端 phase 参数(与错误表"类型"徽章同语义)
  const errorPhaseOptions = useMemo<SelectOption[]>(
    () => [
      { value: null, label: t('admin.usage.allTypes') },
      { value: 'upstream', label: t('admin.ops.errorLog.typeUpstream') },
      { value: 'account_auth', label: t('admin.ops.errorLog.typeAccountAuth') },
      { value: 'request', label: t('admin.ops.errorLog.typeRequest') },
      { value: 'auth', label: t('admin.ops.errorLog.typeAuth') },
      { value: 'routing', label: t('admin.ops.errorLog.typeRouting') },
      { value: 'internal', label: t('admin.ops.errorLog.typeInternal') }
    ],
    [t]
  )

  // 分类码同用户端 /usage 错误筛选;"other" 无法反查为过滤条件,刻意不列
  const errorCategoryOptions = useMemo<SelectOption[]>(
    () => [
      { value: null, label: t('usage.errors.allCategories') },
      ...errorCategoryCodes.map((c) => ({ value: c, label: t('usage.errors.categories.' + c) }))
    ],
    [t]
  )

  const statusCodeOptions = useMemo<SelectOption[]>(
    () => [
      { value: null, label: t('usage.errors.allStatuses') },
      ...COMMON_ERROR_STATUS_CODES.map((c) => ({ value: c, label: String(c) }))
    ],
    [t]
  )

  const errorViewOptions = useMemo<SelectOption[]>(
    () => [
      { value: 'errors', label: t('admin.ops.errorDetails.viewErrors') },
      { value: 'excluded', label: t('admin.ops.errorDetails.viewExcluded') },
      { value: 'all', label: t('admin.ops.errorDetails.viewAll') }
    ],
    [t]
  )

  const activeAdvancedCount = useMemo(() => {
    const values = filters as Record<string, unknown>
    const applied = ADVANCED_FILTER_FIELDS[mode].filter((field) => {
      const value = values[field]
      return value !== undefined && value !== null && value !== ''
    }).length
    // errors mode also hides the record view in the advanced block; its default
    // shows only errors, so any other value changes which rows come back.
    if (mode === 'errors' && errorView !== 'errors') return applied + 1
    return applied
  }, [filters, mode, errorView])

  const debounceUserSearch = useCallback(() => {
    clearPendingUserSearch()
    const query = userKeywordRef.current.trim()
    if (!query) {
      setUserResults([])
      return
    }

    const sequence = userSearchSequence.current
    userSearchTimeout.current = setTimeout(async () => {
      userSearchTimeout.current = null
      try {
        const results = await adminAPI.usage.searchUsers(query)
        if (sequence === userSearchSequence.current) {
          // The signed-in administrator stays first so an admin auditing their own
          // traffic can switch back to that view without retyping.
          const adminId = useAuthStore.getState().user?.id
          const ordered = [...results].sort((a, b) => Number(a.deleted) - Number(b.deleted))
          setUserResults(
            adminId === undefined
              ? ordered
              : [...ordered.filter((u) => u.id === adminId), ...ordered.filter((u) => u.id !== adminId)]
          )
        }
      } catch {
        if (sequence === userSearchSequence.current) {
          setUserResults([])
        }
      }
    }, 300)
  }, [clearPendingUserSearch])

  const clearApiKey = useCallback(() => {
    if (apiKeySearchTimeout.current) {
      clearTimeout(apiKeySearchTimeout.current)
      apiKeySearchTimeout.current = null
    }
    updateApiKeyKeyword('')
    setApiKeyResults([])
    setShowApiKeyDropdown(false)
    patch({ api_key_id: undefined })
  }, [patch, updateApiKeyKeyword])

  const debounceApiKeySearch = useCallback(() => {
    if (apiKeySearchTimeout.current) clearTimeout(apiKeySearchTimeout.current)
    const query = apiKeyKeywordRef.current
    apiKeySearchTimeout.current = setTimeout(async () => {
      try {
        setApiKeyResults(await adminAPI.usage.searchApiKeys(filtersRef.current.user_id, query))
      } catch {
        setApiKeyResults([])
      }
    }, 300)
  }, [])

  const selectUser = useCallback(
    async (user: SimpleUser) => {
      clearPendingUserSearch()
      updateUserKeyword(user.email)
      setShowUserDropdown(false)
      patch({ user_id: user.id })
      clearApiKey()

      // Auto-load API keys for this user
      try {
        setApiKeyResults(await adminAPI.usage.searchApiKeys(user.id, ''))
      } catch {
        setApiKeyResults([])
      }

      emitChange()
    },
    [clearPendingUserSearch, patch, clearApiKey, emitChange, updateUserKeyword]
  )

  const clearUser = useCallback(() => {
    clearPendingUserSearch()
    updateUserKeyword('')
    setUserResults([])
    setShowUserDropdown(false)
    patch({ user_id: undefined })
    clearApiKey()
  }, [clearPendingUserSearch, patch, clearApiKey, updateUserKeyword])

  const selectApiKey = useCallback(
    (key: SimpleApiKey) => {
      updateApiKeyKeyword(key.name || String(key.id))
      setShowApiKeyDropdown(false)
      patch({ api_key_id: key.id })
      emitChange()
    },
    [patch, emitChange, updateApiKeyKeyword]
  )

  const onClearApiKey = useCallback(() => {
    clearApiKey()
    emitChange()
  }, [clearApiKey, emitChange])

  const debounceAccountSearch = useCallback(() => {
    if (accountSearchTimeout.current) clearTimeout(accountSearchTimeout.current)
    const query = accountKeywordRef.current
    accountSearchTimeout.current = setTimeout(async () => {
      if (!query) {
        setAccountResults([])
        return
      }
      try {
        const res = await adminAPI.accounts.list(1, 20, { search: query })
        setAccountResults(res.items.map((a) => ({ id: a.id, name: a.name })))
      } catch {
        setAccountResults([])
      }
    }, 300)
  }, [])

  const selectAccount = useCallback(
    (account: SimpleAccount) => {
      updateAccountKeyword(account.name)
      setShowAccountDropdown(false)
      patch({ account_id: account.id })
      emitChange()
    },
    [patch, emitChange, updateAccountKeyword]
  )

  const clearAccount = useCallback(() => {
    updateAccountKeyword('')
    setAccountResults([])
    setShowAccountDropdown(false)
    patch({ account_id: undefined })
    emitChange()
  }, [patch, emitChange, updateAccountKeyword])

  const onApiKeyFocus = useCallback(() => {
    setShowApiKeyDropdown(true)
    // Trigger search if no results yet
    if (apiKeyResults.length === 0) {
      debounceApiKeySearch()
    }
  }, [apiKeyResults.length, debounceApiKeySearch])

  const updateErrorView = useCallback(
    (value: unknown) => {
      if (value === 'errors' || value === 'excluded' || value === 'all') {
        onErrorViewChange?.(value)
      }
    },
    [onErrorViewChange]
  )

  useImperativeHandle(
    ref,
    (): UsageFiltersHandle => ({
      getUserSearchRevision: () => userSearchSequence.current,
      setUserKeyword: (email: string) => {
        clearPendingUserSearch()
        updateUserKeyword(email)
        setUserResults([])
        setShowUserDropdown(false)
      }
    }),
    [clearPendingUserSearch, updateUserKeyword]
  )

  // Keep the shared query dates on the filter object, mirroring the Vue watches.
  const syncedStartDate = useRef(startDate)
  useEffect(() => {
    if (syncedStartDate.current === startDate) return
    syncedStartDate.current = startDate
    patch({ start_date: startDate })
    onStartDateChange?.(startDate)
  }, [startDate, patch, onStartDateChange])

  const syncedEndDate = useRef(endDate)
  useEffect(() => {
    if (syncedEndDate.current === endDate) return
    syncedEndDate.current = endDate
    patch({ end_date: endDate })
    onEndDateChange?.(endDate)
  }, [endDate, patch, onEndDateChange])

  // A cleared identity also clears its keyword echo.
  useEffect(() => {
    if (!filters.user_id) {
      clearPendingUserSearch()
      updateUserKeyword('')
      setUserResults([])
    }
  }, [filters.user_id, clearPendingUserSearch, updateUserKeyword])

  useEffect(() => {
    if (!filters.api_key_id) {
      updateApiKeyKeyword('')
      setApiKeyResults([])
    }
  }, [filters.api_key_id, updateApiKeyKeyword])

  useEffect(() => {
    if (!filters.account_id) {
      updateAccountKeyword('')
      setAccountResults([])
    }
  }, [filters.account_id, updateAccountKeyword])

  useEffect(() => {
    document.addEventListener('click', onDocumentClick)
    return () => {
      document.removeEventListener('click', onDocumentClick)
    }
  }, [])

  useEffect(() => {
    let active = true
    void (async () => {
      try {
        const groups = await adminAPI.groups.list(1, 1000)
        if (!active) return
        setGroupOptions((prev) => [
          ...prev,
          ...groups.items.map((group) => ({ value: group.id, label: group.name }))
        ])
      } catch {
        // Ignore filter option loading errors (page still usable)
      }
    })()
    return () => {
      active = false
    }
  }, [])

  useEffect(
    () => () => {
      clearPendingUserSearch()
      if (apiKeySearchTimeout.current) clearTimeout(apiKeySearchTimeout.current)
      if (accountSearchTimeout.current) clearTimeout(accountSearchTimeout.current)
    },
    [clearPendingUserSearch]
  )

  function onDocumentClick(event: MouseEvent) {
    const target = event.target as Node | null
    if (!target) return

    const clickedInsideUser = userSearchRef.current?.contains(target) ?? false
    const clickedInsideApiKey = apiKeySearchRef.current?.contains(target) ?? false
    const clickedInsideAccount = accountSearchRef.current?.contains(target) ?? false

    if (!clickedInsideUser) setShowUserDropdown(false)
    if (!clickedInsideApiKey) setShowApiKeyDropdown(false)
    if (!clickedInsideAccount) setShowAccountDropdown(false)
  }

  const dropdownClass =
    'absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border shadow-lg surface-panel'

  const renderUserDropdown = () =>
    showUserDropdown && (userResults.length > 0 || userKeyword) ? (
      <div
        className={dropdownClass}
        style={workspace ? userDropdownStyle : undefined}
        data-testid="usage-user-options"
        onClick={(event) => event.stopPropagation()}
      >
        {userResults.map((user) => (
          <button
            key={user.id}
            type="button"
            className="w-full px-4 py-2 text-left hover:surface-hover surface-control"
            onClick={() => void selectUser(user)}
          >
            <span>
              {user.email}
              {user.deleted && (
                <span className="ml-1 text-xs surface-muted">（{t('admin.usage.userDeletedBadge')}）</span>
              )}
            </span>
            <span className="ml-2 text-xs surface-muted">#{user.id}</span>
          </button>
        ))}
      </div>
    ) : null

  const renderApiKeyDropdown = () =>
    showApiKeyDropdown && apiKeyResults.length > 0 ? (
      <div
        className={dropdownClass}
        style={workspace ? apiKeyDropdownStyle : undefined}
        data-testid="usage-api-key-options"
        onClick={(event) => event.stopPropagation()}
      >
        {apiKeyResults.map((key) => (
          <button
            key={key.id}
            type="button"
            className="w-full px-4 py-2 text-left hover:surface-hover surface-control"
            onClick={() => selectApiKey(key)}
          >
            <span className="truncate">{key.name || `#${key.id}`}</span>
            <span className="ml-2 text-xs surface-muted">#{key.id}</span>
          </button>
        ))}
      </div>
    ) : null

  const renderAccountDropdown = () =>
    showAccountDropdown && (accountResults.length > 0 || accountKeyword) ? (
      <div
        className={dropdownClass}
        style={workspace ? accountDropdownStyle : undefined}
        data-testid="usage-account-options"
        onClick={(event) => event.stopPropagation()}
      >
        {accountResults.map((account) => (
          <button
            key={account.id}
            type="button"
            className="w-full px-4 py-2 text-left hover:surface-hover surface-control"
            onClick={() => selectAccount(account)}
          >
            <span className="truncate">{account.name}</span>
            <span className="ml-2 text-xs surface-muted">#{account.id}</span>
          </button>
        ))}
      </div>
    ) : null

  return (
    <div
      className={cn(
        flat ? (workspace ? 'usage-workspace-filters' : '') : 'card p-6',
        workspace && 'usage-workspace-filters',
        className
      )}
    >
      {/* Toolbar: left filters (multi-line) + right actions */}
      <div className="usage-filter-layout filter-bar">
        {/* Left: filters (allowed to wrap to multiple rows) */}
        <div className="usage-filter-fields filter-bar--inline">
          {/* User Search */}
          <div ref={userSearchRef} className="usage-filter-dropdown filter-field relative">
            <label className="input-label">{t('admin.usage.userFilter')}</label>
            <input
              value={userKeyword}
              type="text"
              className="input pr-8"
              placeholder={t('admin.usage.searchUserPlaceholder')}
              onChange={(event) => {
                updateUserKeyword(event.target.value)
                debounceUserSearch()
              }}
              onFocus={() => setShowUserDropdown(true)}
            />
            {filters.user_id ? (
              <button
                type="button"
                onClick={clearUser}
                className="absolute right-2 top-9 surface-control surface-muted"
                aria-label="Clear user filter"
              >
                ✕
              </button>
            ) : null}
            {workspace ? createPortal(renderUserDropdown(), document.body) : renderUserDropdown()}
          </div>

          {/* API Key Search */}
          <div ref={apiKeySearchRef} className="usage-filter-dropdown filter-field relative">
            <label className="input-label">{t('usage.apiKeyFilter')}</label>
            <input
              value={apiKeyKeyword}
              type="text"
              className="input pr-8"
              placeholder={t('admin.usage.searchApiKeyPlaceholder')}
              onChange={(event) => {
                updateApiKeyKeyword(event.target.value)
                debounceApiKeySearch()
              }}
              onFocus={onApiKeyFocus}
            />
            {filters.api_key_id ? (
              <button
                type="button"
                onClick={onClearApiKey}
                className="absolute right-2 top-9 surface-control surface-muted"
                aria-label="Clear API key filter"
              >
                ✕
              </button>
            ) : null}
            {workspace ? createPortal(renderApiKeyDropdown(), document.body) : renderApiKeyDropdown()}
          </div>

          {/* Model Filter */}
          <div className="filter-field">
            <label className="input-label">{t('usage.model')}</label>
            <Select
              value={filters.model ?? null}
              options={modelSelectOptions}
              searchable
              onValueChange={(value) => patch({ model: (value as string | undefined) ?? undefined })}
              onChange={emitChange}
            />
          </div>

          {/* Account Filter */}
          <div ref={accountSearchRef} className="usage-filter-dropdown filter-field relative">
            <label className="input-label">{t('admin.usage.account')}</label>
            <input
              value={accountKeyword}
              type="text"
              className="input pr-8"
              placeholder={t('admin.usage.searchAccountPlaceholder')}
              onChange={(event) => {
                updateAccountKeyword(event.target.value)
                debounceAccountSearch()
              }}
              onFocus={() => setShowAccountDropdown(true)}
            />
            {filters.account_id ? (
              <button
                type="button"
                onClick={clearAccount}
                className="absolute right-2 top-9 surface-control surface-muted"
                aria-label="Clear account filter"
              >
                ✕
              </button>
            ) : null}
            {workspace ? createPortal(renderAccountDropdown(), document.body) : renderAccountDropdown()}
          </div>

          {/* Advanced filters (collapsed by default to keep the primary row calm) */}
          <div className="usage-filter-advanced" style={{ display: showAdvanced ? 'contents' : 'none' }}>
            {/* Request Type Filter (usage only) */}
            {mode !== 'errors' && (
              <div className="filter-field">
                <label className="input-label">{t('usage.type')}</label>
                <Select
                  value={filters.request_type ?? null}
                  options={requestTypeOptions}
                  onValueChange={(value) =>
                    patch({ request_type: (value as AdminUsageQueryParams['request_type']) ?? undefined })
                  }
                  onChange={emitChange}
                />
              </div>
            )}

            {/* Native compaction is independent of the transport request type. */}
            {mode !== 'errors' && (
              <div className="filter-field">
                <label className="input-label">{t('usage.compactionFilter')}</label>
                <Select
                  value={filters.native_compaction_v2 ?? null}
                  options={compactionOptions}
                  onValueChange={(value) => patch({ native_compaction_v2: (value as boolean | null) ?? null })}
                  onChange={emitChange}
                />
              </div>
            )}

            {/* Billing Type Filter (usage only) */}
            {mode !== 'errors' && (
              <div className="filter-field">
                <label className="input-label">{t('admin.usage.billingType')}</label>
                <Select
                  value={filters.billing_type ?? null}
                  options={billingTypeOptions}
                  onValueChange={(value) => patch({ billing_type: (value as number | null) ?? null })}
                  onChange={emitChange}
                />
              </div>
            )}

            {/* Billing Mode Filter (usage only; user-breakdown has no such dimension) */}
            {mode === 'usage' && (
              <div className="filter-field">
                <label className="input-label">{t('admin.usage.billingMode')}</label>
                <Select
                  value={filters.billing_mode ?? null}
                  options={billingModeOptions}
                  onValueChange={(value) => patch({ billing_mode: (value as string | undefined) ?? undefined })}
                  onChange={emitChange}
                />
              </div>
            )}

            {mode === 'usage' && (
              <div className="filter-field">
                <label className="input-label">{t('admin.usage.upstreamModelAudit')}</label>
                <Select
                  value={filters.upstream_model_mismatch ?? null}
                  options={upstreamModelMismatchOptions}
                  onValueChange={(value) =>
                    patch({ upstream_model_mismatch: (value as boolean | undefined) ?? undefined })
                  }
                  onChange={emitChange}
                />
              </div>
            )}

            {mode === 'usage' && (
              <div className="filter-field">
                <label className="input-label">{t('admin.usage.completionStatus')}</label>
                <Select
                  value={filters.completion_status ?? null}
                  options={completionStatusOptions}
                  onValueChange={(value) =>
                    patch({
                      completion_status:
                        (value as AdminUsageQueryParams['completion_status']) ?? undefined
                    })
                  }
                  onChange={emitChange}
                />
              </div>
            )}

            {mode === 'usage' && (
              <div className="filter-field">
                <label className="input-label">{t('admin.usage.usageSource')}</label>
                <Select
                  value={filters.usage_source ?? null}
                  options={usageSourceOptions}
                  onValueChange={(value) =>
                    patch({ usage_source: (value as AdminUsageQueryParams['usage_source']) ?? undefined })
                  }
                  onChange={emitChange}
                />
              </div>
            )}

            {/* Error record view (errors only) */}
            {mode === 'errors' && (
              <div className="filter-field">
                <label className="input-label">{t('admin.ops.errorDetails.view')}</label>
                <Select
                  value={errorView}
                  options={errorViewOptions}
                  onValueChange={updateErrorView}
                />
              </div>
            )}

            {/* Error Phase Filter (errors only) */}
            {mode === 'errors' && (
              <div className="filter-field">
                <label className="input-label">{t('admin.ops.errorLog.type')}</label>
                <Select
                  value={filters.error_phase ?? null}
                  options={errorPhaseOptions}
                  onValueChange={(value) =>
                    patch({ error_phase: (value as string | null) ?? null })
                  }
                  onChange={emitChange}
                />
              </div>
            )}

            {/* Error Category Filter (errors only) */}
            {mode === 'errors' && (
              <div className="filter-field">
                <label className="input-label">{t('usage.errors.category')}</label>
                <Select
                  value={filters.error_category ?? null}
                  options={errorCategoryOptions}
                  onValueChange={(value) =>
                    patch({ error_category: (value as string | null) ?? null })
                  }
                  onChange={emitChange}
                />
              </div>
            )}

            {/* Status Code Filter (errors only) */}
            {mode === 'errors' && (
              <div className="filter-field">
                <label className="input-label">{t('admin.ops.errorLog.status')}</label>
                <Select
                  value={filters.status_code ?? null}
                  options={statusCodeOptions}
                  onValueChange={(value) => patch({ status_code: (value as number | null | undefined) ?? null })}
                  onChange={emitChange}
                />
              </div>
            )}

            {/* Group Filter */}
            <div className="filter-field">
              <label className="input-label">{t('admin.usage.group')}</label>
              <Select
                value={filters.group_id ?? null}
                options={groupOptions}
                searchable
                onValueChange={(value) => patch({ group_id: (value as number | undefined) ?? undefined })}
                onChange={emitChange}
              />
            </div>
          </div>

          <button
            type="button"
            className="usage-filter-more inline-flex items-center gap-1 rounded-lg border border-gray-200 px-3 py-2 text-sm font-medium surface-muted surface-control transition-colors hover:surface-hover dark:border-dark-600"
            aria-expanded={showAdvanced}
            onClick={() => setShowAdvanced((prev) => !prev)}
          >
            <span>{showAdvanced ? t('admin.usage.fewerFilters') : t('admin.usage.moreFilters')}</span>
            {/* A collapsed advanced block keeps its filters active, so the count
                is the only cue that the visible row is not the whole query. */}
            {!showAdvanced && activeAdvancedCount > 0 && (
              <span
                className="usage-filter-more-badge rounded-full bg-primary-500/15 px-1.5 py-0.5 text-xs font-semibold text-primary-700 dark:text-primary-300"
                data-testid="usage-advanced-filter-count"
              >
                {activeAdvancedCount}
              </span>
            )}
            <Icon name="chevronDown" size="sm" className={showAdvanced ? 'rotate-180' : undefined} />
          </button>
        </div>

        {/* Right: actions */}
        {showActions && (
          <div className="usage-filter-actions filter-actions">
            <button type="button" onClick={onRefresh} className="btn btn-secondary">
              {t('common.refresh')}
            </button>
            <button type="button" onClick={onReset} className="btn btn-secondary">
              {t('common.reset')}
            </button>
            {afterReset}
            {mode === 'usage' && (
              <>
                <button type="button" onClick={onCleanup} className="btn btn-danger">
                  {t('admin.usage.cleanup.button')}
                </button>
                <button type="button" onClick={onExport} disabled={exporting} className="btn btn-primary">
                  {t('usage.exportExcel')}
                </button>
              </>
            )}
          </div>
        )}
      </div>
    </div>
  )
})

export default UsageFilters
