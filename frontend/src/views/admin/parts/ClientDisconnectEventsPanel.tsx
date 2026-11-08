import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/Icon'
import { Pagination, Select, type SelectOption } from '@/components/ui'
import { adminUsageAPI } from '@/api/admin/usage'
import type {
  ClientDisconnectCompletionStatus,
  ClientDisconnectEventQueryParams,
  ClientDisconnectOutcome,
  ClientDisconnectRiskEvent,
  SimpleApiKey,
  SimpleUser
} from '@/api/admin/usage'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'
import { toast } from '@/components/ui'
import { getPersistedPageSize } from '@/views/admin/shared'
import { useAnchoredDropdown } from './useAnchoredDropdown'
import { DateTimePicker } from './DateTimePicker'

type UsageSource = NonNullable<ClientDisconnectEventQueryParams['usage_source']>

interface DisconnectDraft {
  user_id: number | undefined
  api_key_id: number | undefined
  request_id: string
  session_id: string
  protocol: string
  accepted_from: string
  accepted_to: string
  finalized_from: string
  finalized_to: string
  outcome: '' | ClientDisconnectOutcome
  completion_status: '' | ClientDisconnectCompletionStatus
  usage_source: '' | UsageSource
  usage_missing: '' | 'true' | 'false'
  enforce: '' | 'true' | 'false'
  auto_banned: '' | 'true' | 'false'
}

const EMPTY_DRAFT: DisconnectDraft = {
  user_id: undefined,
  api_key_id: undefined,
  request_id: '',
  session_id: '',
  protocol: '',
  accepted_from: '',
  accepted_to: '',
  finalized_from: '',
  finalized_to: '',
  outcome: '',
  completion_status: '',
  usage_source: '',
  usage_missing: '',
  enforce: '',
  auto_banned: ''
}

export interface ClientDisconnectEventsPanelProps {
  workspace?: boolean
  /** Keeps the panel mounted while another tab is active. */
  hidden?: boolean
}

function booleanFilter(value: '' | 'true' | 'false'): boolean | undefined {
  return value === '' ? undefined : value === 'true'
}

function toRFC3339(value: string): string | undefined {
  if (!value) return undefined
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString()
}

export function ClientDisconnectEventsPanel({
  workspace = false,
  hidden = false
}: ClientDisconnectEventsPanelProps) {
  const { t } = useTranslation()

  const [loading, setLoading] = useState(false)
  const [events, setEvents] = useState<ClientDisconnectRiskEvent[]>([])
  const [pagination, setPagination] = useState({
    page: 1,
    page_size: getPersistedPageSize(),
    total: 0
  })
  const [draft, setDraft] = useState<DisconnectDraft>(EMPTY_DRAFT)
  const draftRef = useRef(draft)
  draftRef.current = draft
  const paginationRef = useRef(pagination)
  paginationRef.current = pagination

  const userSearchRef = useRef<HTMLDivElement | null>(null)
  const apiKeySearchRef = useRef<HTMLDivElement | null>(null)
  const [userKeyword, setUserKeyword] = useState('')
  const [apiKeyKeyword, setApiKeyKeyword] = useState('')
  const [userResults, setUserResults] = useState<SimpleUser[]>([])
  const [apiKeyResults, setApiKeyResults] = useState<SimpleApiKey[]>([])
  const [showUserDropdown, setShowUserDropdown] = useState(false)
  const [showAPIKeyDropdown, setShowAPIKeyDropdown] = useState(false)
  const userDropdownStyle = useAnchoredDropdown(userSearchRef, workspace && showUserDropdown)
  const apiKeyDropdownStyle = useAnchoredDropdown(apiKeySearchRef, workspace && showAPIKeyDropdown)

  const userSearchTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const apiKeySearchTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const userSearchSequence = useRef(0)
  const apiKeySearchSequence = useRef(0)
  const eventLoadSequence = useRef(0)

  const patchDraft = useCallback((partial: Partial<DisconnectDraft>) => {
    setDraft((prev) => ({ ...prev, ...partial }))
  }, [])

  const queryParams = useCallback((): ClientDisconnectEventQueryParams => {
    const current = draftRef.current
    return {
      user_id: current.user_id,
      api_key_id: current.api_key_id,
      request_id: current.request_id || undefined,
      session_id: current.session_id || undefined,
      protocol: current.protocol || undefined,
      accepted_from: toRFC3339(current.accepted_from),
      accepted_to: toRFC3339(current.accepted_to),
      finalized_from: toRFC3339(current.finalized_from),
      finalized_to: toRFC3339(current.finalized_to),
      outcome: current.outcome || undefined,
      completion_status: current.completion_status || undefined,
      usage_source: current.usage_source || undefined,
      usage_missing: booleanFilter(current.usage_missing),
      enforce: booleanFilter(current.enforce),
      auto_banned: booleanFilter(current.auto_banned),
      page: paginationRef.current.page,
      page_size: paginationRef.current.page_size
    }
  }, [])

  const loadEvents = useCallback(async () => {
    eventLoadSequence.current += 1
    const sequence = eventLoadSequence.current
    setLoading(true)
    try {
      const result = await adminUsageAPI.listClientDisconnectEvents(queryParams())
      if (sequence !== eventLoadSequence.current) return
      setEvents(result.items)
      setPagination((prev) => ({
        ...prev,
        total: result.total,
        page: result.page,
        page_size: result.page_size
      }))
    } catch (error: unknown) {
      if (sequence !== eventLoadSequence.current) return
      toast.error(extractApiErrorMessage(error, t('admin.usage.disconnectEvents.failedToLoad')))
    } finally {
      if (sequence === eventLoadSequence.current) setLoading(false)
    }
  }, [queryParams, t])

  const loadAPIKeys = useCallback(async (query: string) => {
    apiKeySearchSequence.current += 1
    const sequence = apiKeySearchSequence.current
    try {
      const result = await adminUsageAPI.searchApiKeys(draftRef.current.user_id, query)
      if (sequence === apiKeySearchSequence.current) setApiKeyResults(result)
    } catch {
      if (sequence === apiKeySearchSequence.current) setApiKeyResults([])
    }
  }, [])

  const clearAPIKey = useCallback(() => {
    if (apiKeySearchTimer.current) {
      clearTimeout(apiKeySearchTimer.current)
      apiKeySearchTimer.current = undefined
    }
    apiKeySearchSequence.current += 1
    setApiKeyKeyword('')
    patchDraft({ api_key_id: undefined })
    setApiKeyResults([])
    setShowAPIKeyDropdown(false)
  }, [patchDraft])

  const applyFilters = useCallback(() => {
    setPagination((prev) => ({ ...prev, page: 1 }))
    void loadEvents()
  }, [loadEvents])

  const searchUsers = useCallback(() => {
    const clearedAppliedIdentity =
      draftRef.current.user_id !== undefined || draftRef.current.api_key_id !== undefined
    patchDraft({ user_id: undefined })
    clearAPIKey()
    if (clearedAppliedIdentity) applyFilters()
    userSearchSequence.current += 1
    const sequence = userSearchSequence.current
    if (userSearchTimer.current) {
      clearTimeout(userSearchTimer.current)
      userSearchTimer.current = undefined
    }
    const query = userKeyword.trim()
    if (!query) {
      setUserResults([])
      return
    }
    userSearchTimer.current = setTimeout(async () => {
      try {
        const result = await adminUsageAPI.searchUsers(query)
        if (sequence === userSearchSequence.current) setUserResults(result)
      } catch {
        if (sequence === userSearchSequence.current) setUserResults([])
      }
    }, 300)
  }, [userKeyword, patchDraft, clearAPIKey, applyFilters])

  const selectUser = useCallback(
    (user: SimpleUser) => {
      if (userSearchTimer.current) {
        clearTimeout(userSearchTimer.current)
        userSearchTimer.current = undefined
      }
      userSearchSequence.current += 1
      setUserKeyword(user.email)
      patchDraft({ user_id: user.id })
      setUserResults([])
      setShowUserDropdown(false)
      clearAPIKey()
      void loadAPIKeys('')
      applyFilters()
    },
    [patchDraft, clearAPIKey, loadAPIKeys, applyFilters]
  )

  const clearUser = useCallback(() => {
    if (userSearchTimer.current) {
      clearTimeout(userSearchTimer.current)
      userSearchTimer.current = undefined
    }
    userSearchSequence.current += 1
    setUserKeyword('')
    patchDraft({ user_id: undefined })
    setUserResults([])
    setShowUserDropdown(false)
    clearAPIKey()
  }, [patchDraft, clearAPIKey])

  const onClearUser = useCallback(() => {
    clearUser()
    applyFilters()
  }, [clearUser, applyFilters])

  const searchAPIKeys = useCallback(() => {
    const clearedAppliedAPIKey = draftRef.current.api_key_id !== undefined
    patchDraft({ api_key_id: undefined })
    if (clearedAppliedAPIKey) applyFilters()
    apiKeySearchSequence.current += 1
    if (apiKeySearchTimer.current) clearTimeout(apiKeySearchTimer.current)
    const query = apiKeyKeyword.trim()
    apiKeySearchTimer.current = setTimeout(() => void loadAPIKeys(query), 300)
  }, [apiKeyKeyword, patchDraft, applyFilters, loadAPIKeys])

  const openAPIKeyDropdown = useCallback(() => {
    setShowAPIKeyDropdown(true)
    if (apiKeyResults.length === 0) void loadAPIKeys(apiKeyKeyword.trim())
  }, [apiKeyResults.length, apiKeyKeyword, loadAPIKeys])

  const selectAPIKey = useCallback(
    (key: SimpleApiKey) => {
      if (apiKeySearchTimer.current) {
        clearTimeout(apiKeySearchTimer.current)
        apiKeySearchTimer.current = undefined
      }
      apiKeySearchSequence.current += 1
      setApiKeyKeyword(key.name || String(key.id))
      patchDraft({ api_key_id: key.id })
      setShowAPIKeyDropdown(false)
      applyFilters()
    },
    [patchDraft, applyFilters]
  )

  const onClearAPIKey = useCallback(() => {
    clearAPIKey()
    applyFilters()
  }, [clearAPIKey, applyFilters])

  const resetFilters = useCallback(() => {
    clearUser()
    setDraft((prev) => ({
      ...prev,
      request_id: '',
      session_id: '',
      protocol: '',
      accepted_from: '',
      accepted_to: '',
      finalized_from: '',
      finalized_to: '',
      outcome: '',
      completion_status: '',
      usage_source: '',
      usage_missing: '',
      enforce: '',
      auto_banned: ''
    }))
    applyFilters()
  }, [clearUser, applyFilters])

  const changePage = useCallback(
    (page: number) => {
      setPagination((prev) => ({ ...prev, page }))
      void loadEvents()
    },
    [loadEvents]
  )

  const changePageSize = useCallback(
    (pageSize: number) => {
      setPagination((prev) => ({ ...prev, page_size: pageSize, page: 1 }))
      void loadEvents()
    },
    [loadEvents]
  )

  useEffect(() => {
    document.addEventListener('click', closeDropdowns)
    void loadEvents()
    return () => {
      eventLoadSequence.current += 1
      if (userSearchTimer.current) clearTimeout(userSearchTimer.current)
      if (apiKeySearchTimer.current) clearTimeout(apiKeySearchTimer.current)
      document.removeEventListener('click', closeDropdowns)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function closeDropdowns(event: MouseEvent) {
    const target = event.target as Node | null
    if (!target) return
    if (!userSearchRef.current?.contains(target)) setShowUserDropdown(false)
    if (!apiKeySearchRef.current?.contains(target)) setShowAPIKeyDropdown(false)
  }

  const camelCase = (value: string): string =>
    value.replace(/_([a-z])/g, (_, char: string) => char.toUpperCase())

  const statusLabel = (status: ClientDisconnectCompletionStatus): string =>
    t(`admin.usage.disconnectEvents.${camelCase(status)}`)
  const outcomeLabel = (outcome: ClientDisconnectOutcome): string =>
    t(`admin.usage.disconnectEvents.${camelCase(outcome)}`)
  const usageSourceLabel = (source: UsageSource): string =>
    t(`admin.usage.disconnectEvents.${camelCase(source)}`)
  const enforcementLabel = (enforce: boolean | undefined): string => {
    if (enforce === undefined) return '-'
    return enforce ? t('admin.usage.disconnectEvents.enforced') : t('admin.usage.disconnectEvents.auditOnly')
  }
  const statusClass = (status: ClientDisconnectCompletionStatus): string => {
    if (status === 'client_disconnected' || status === 'usage_missing') {
      return 'bg-red-100/[0.15] text-red-700 dark:bg-red-900/30 dark:text-red-300'
    }
    if (status === 'completed') return 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    if (status === 'pending') return 'bg-amber-100/[0.15] text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    return 'surface-inset surface-muted dark:surface-inset'
  }
  const copyValue = (value: string) => {
    void navigator.clipboard?.writeText(value)
  }

  const outcomeOptions: SelectOption[] = [
    { value: '', label: t('admin.usage.disconnectEvents.allOutcomes') },
    { value: 'pending', label: t('admin.usage.disconnectEvents.pending') },
    { value: 'completed', label: t('admin.usage.disconnectEvents.completed') },
    { value: 'client_disconnected', label: t('admin.usage.disconnectEvents.clientDisconnected') },
    { value: 'neutral', label: t('admin.usage.disconnectEvents.neutral') }
  ]

  const completionStatusOptions: SelectOption[] = [
    { value: '', label: t('admin.usage.disconnectEvents.allStatuses') },
    { value: 'pending', label: t('admin.usage.disconnectEvents.pending') },
    { value: 'completed', label: t('admin.usage.disconnectEvents.completed') },
    { value: 'client_disconnected', label: t('admin.usage.disconnectEvents.clientDisconnected') },
    { value: 'upstream_failed', label: t('admin.usage.disconnectEvents.upstreamFailed') },
    { value: 'upstream_timeout', label: t('admin.usage.disconnectEvents.upstreamTimeout') },
    { value: 'usage_missing', label: t('admin.usage.disconnectEvents.usageMissing') }
  ]

  const usageSourceOptions: SelectOption[] = [
    { value: '', label: t('admin.usage.disconnectEvents.allSources') },
    { value: 'upstream_exact', label: t('admin.usage.disconnectEvents.upstreamExact') },
    { value: 'partial', label: t('admin.usage.disconnectEvents.partial') },
    { value: 'estimated', label: t('admin.usage.disconnectEvents.estimated') },
    { value: 'reconciled', label: t('admin.usage.disconnectEvents.reconciled') }
  ]

  const yesNoOptions: SelectOption[] = [
    { value: '', label: t('admin.usage.disconnectEvents.allValues') },
    { value: 'true', label: t('common.yes') },
    { value: 'false', label: t('common.no') }
  ]

  const enforceOptions: SelectOption[] = [
    { value: '', label: t('admin.usage.disconnectEvents.allValues') },
    { value: 'true', label: t('admin.usage.disconnectEvents.enforced') },
    { value: 'false', label: t('admin.usage.disconnectEvents.auditOnly') }
  ]

  const headings = [
    t('admin.usage.disconnectEvents.acceptedAt'),
    t('admin.usage.disconnectEvents.finalizedAt'),
    t('admin.usage.disconnectEvents.userAccount'),
    t('admin.usage.disconnectEvents.apiKeyName'),
    t('admin.usage.disconnectEvents.requestId'),
    t('admin.usage.disconnectEvents.sessionId'),
    t('admin.usage.disconnectEvents.protocol'),
    t('admin.usage.disconnectEvents.completionStatus'),
    t('admin.usage.disconnectEvents.outcome'),
    t('admin.usage.disconnectEvents.usageSource'),
    t('admin.usage.disconnectEvents.usageMissing'),
    t('admin.usage.disconnectEvents.consecutiveCount'),
    t('admin.usage.disconnectEvents.threshold'),
    t('admin.usage.disconnectEvents.enforcement'),
    t('admin.usage.disconnectEvents.action')
  ]

  const userDropdown = showUserDropdown && userResults.length > 0 && (
    <div
      className="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-md border border-gray-200 shadow-lg dark:border-dark-600 surface-panel"
      style={workspace ? userDropdownStyle : undefined}
      data-testid="disconnect-user-options"
      onClick={(event) => event.stopPropagation()}
    >
      {userResults.map((user) => (
        <button
          key={user.id}
          type="button"
          className="block w-full px-3 py-2 text-left text-sm hover:surface-hover surface-control"
          onClick={() => selectUser(user)}
        >
          <span>{user.email}</span>
          {user.deleted && (
            <span className="ml-2 text-xs surface-muted">{t('admin.usage.userDeletedBadge')}</span>
          )}
        </button>
      ))}
    </div>
  )

  const apiKeyDropdown = showAPIKeyDropdown && apiKeyResults.length > 0 && (
    <div
      className="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-md border border-gray-200 shadow-lg dark:border-dark-600 surface-panel"
      style={workspace ? apiKeyDropdownStyle : undefined}
      data-testid="disconnect-api-key-options"
      onClick={(event) => event.stopPropagation()}
    >
      {apiKeyResults.map((key) => (
        <button
          key={key.id}
          type="button"
          className="block w-full px-3 py-2 text-left text-sm hover:surface-hover surface-control"
          onClick={() => selectAPIKey(key)}
        >
          {key.name || `#${key.id}`}
        </button>
      ))}
    </div>
  )

  return (
    <div
      className={`min-w-0 w-full ${workspace ? 'workspace-body !gap-0 disconnect-workspace' : ''}`}
      data-testid="client-disconnect-events-panel"
      hidden={hidden}
    >
      <div
        className={`disconnect-filters border-b border-gray-100 px-4 py-4 dark:border-dark-700/50 ${
          workspace ? 'workspace-fixed' : ''
        }`}
      >
        <div className="disconnect-filter-fields grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4 2xl:grid-cols-5">
          <div ref={userSearchRef} className="relative">
            <label className="input-label">{t('admin.usage.disconnectEvents.userAccount')}</label>
            <input
              value={userKeyword}
              data-testid="disconnect-user-filter"
              type="text"
              className="input pr-9"
              placeholder={t('admin.usage.searchUserPlaceholder')}
              onChange={(event) => {
                setUserKeyword(event.target.value)
                searchUsers()
              }}
              onFocus={() => setShowUserDropdown(true)}
            />
            {draft.user_id && (
              <button
                type="button"
                className="absolute right-2 top-8 p-1 hover:text-gray-700 dark:hover:text-gray-200 surface-control surface-muted"
                title={t('admin.usage.disconnectEvents.clearFilter')}
                onClick={onClearUser}
              >
                <Icon name="x" size="xs" />
              </button>
            )}
            {workspace ? createPortal(userDropdown, document.body) : userDropdown}
          </div>

          <div ref={apiKeySearchRef} className="relative">
            <label className="input-label">{t('admin.usage.disconnectEvents.apiKeyName')}</label>
            <input
              value={apiKeyKeyword}
              data-testid="disconnect-api-key-filter"
              type="text"
              className="input pr-9"
              placeholder={t('admin.usage.searchApiKeyPlaceholder')}
              onChange={(event) => {
                setApiKeyKeyword(event.target.value)
                searchAPIKeys()
              }}
              onFocus={openAPIKeyDropdown}
            />
            {draft.api_key_id && (
              <button
                type="button"
                className="absolute right-2 top-8 p-1 hover:text-gray-700 dark:hover:text-gray-200 surface-control surface-muted"
                title={t('admin.usage.disconnectEvents.clearFilter')}
                onClick={onClearAPIKey}
              >
                <Icon name="x" size="xs" />
              </button>
            )}
            {workspace ? createPortal(apiKeyDropdown, document.body) : apiKeyDropdown}
          </div>

          <label className="block">
            <span className="input-label">{t('admin.usage.disconnectEvents.requestId')}</span>
            <input
              value={draft.request_id}
              data-testid="disconnect-request-filter"
              type="text"
              className="input"
              onChange={(event) => patchDraft({ request_id: event.target.value })}
              onKeyDown={(event) => {
                if (event.key === 'Enter') applyFilters()
              }}
            />
          </label>
          <label className="block">
            <span className="input-label">{t('admin.usage.disconnectEvents.sessionId')}</span>
            <input
              value={draft.session_id}
              data-testid="disconnect-session-filter"
              type="text"
              className="input"
              onChange={(event) => patchDraft({ session_id: event.target.value })}
              onKeyDown={(event) => {
                if (event.key === 'Enter') applyFilters()
              }}
            />
          </label>
          <label className="block">
            <span className="input-label">{t('admin.usage.disconnectEvents.protocol')}</span>
            <input
              value={draft.protocol}
              data-testid="disconnect-protocol-filter"
              type="text"
              className="input"
              onChange={(event) => patchDraft({ protocol: event.target.value })}
              onKeyDown={(event) => {
                if (event.key === 'Enter') applyFilters()
              }}
            />
          </label>

          <label className="block">
            <span className="input-label">{t('admin.usage.disconnectEvents.acceptedFrom')}</span>
            <DateTimePicker
              ariaLabel={t('admin.usage.disconnectEvents.acceptedFrom')}
              value={draft.accepted_from}
              data-testid="disconnect-accepted-from"
              type="datetime-local"
              className="input"
              onValueChange={(value) => patchDraft({ accepted_from: value })}
            />
          </label>
          <label className="block">
            <span className="input-label">{t('admin.usage.disconnectEvents.acceptedTo')}</span>
            <DateTimePicker
              ariaLabel={t('admin.usage.disconnectEvents.acceptedTo')}
              value={draft.accepted_to}
              data-testid="disconnect-accepted-to"
              type="datetime-local"
              className="input"
              onValueChange={(value) => patchDraft({ accepted_to: value })}
            />
          </label>
          <label className="block">
            <span className="input-label">{t('admin.usage.disconnectEvents.finalizedFrom')}</span>
            <DateTimePicker
              ariaLabel={t('admin.usage.disconnectEvents.finalizedFrom')}
              value={draft.finalized_from}
              data-testid="disconnect-finalized-from"
              type="datetime-local"
              className="input"
              onValueChange={(value) => patchDraft({ finalized_from: value })}
            />
          </label>
          <label className="block">
            <span className="input-label">{t('admin.usage.disconnectEvents.finalizedTo')}</span>
            <DateTimePicker
              ariaLabel={t('admin.usage.disconnectEvents.finalizedTo')}
              value={draft.finalized_to}
              data-testid="disconnect-finalized-to"
              type="datetime-local"
              className="input"
              onValueChange={(value) => patchDraft({ finalized_to: value })}
            />
          </label>

          <label className="block">
            <span className="input-label">{t('admin.usage.disconnectEvents.outcome')}</span>
            <Select
              value={draft.outcome}
              options={outcomeOptions}
              searchable={false}
              data-testid="disconnect-outcome-filter"
              onValueChange={(value) => {
                patchDraft({ outcome: (value ?? '') as '' | ClientDisconnectOutcome })
                applyFilters()
              }}
            />
          </label>
          <label className="block">
            <span className="input-label">{t('admin.usage.disconnectEvents.completionStatus')}</span>
            <Select
              value={draft.completion_status}
              options={completionStatusOptions}
              searchable={false}
              data-testid="disconnect-completion-filter"
              onValueChange={(value) => {
                patchDraft({ completion_status: (value ?? '') as '' | ClientDisconnectCompletionStatus })
                applyFilters()
              }}
            />
          </label>
          <label className="block">
            <span className="input-label">{t('admin.usage.disconnectEvents.usageSource')}</span>
            <Select
              value={draft.usage_source}
              options={usageSourceOptions}
              searchable={false}
              data-testid="disconnect-usage-source-filter"
              onValueChange={(value) => {
                patchDraft({ usage_source: (value ?? '') as '' | UsageSource })
                applyFilters()
              }}
            />
          </label>
          <label className="block">
            <span className="input-label">{t('admin.usage.disconnectEvents.usageMissing')}</span>
            <Select
              value={draft.usage_missing}
              options={yesNoOptions}
              searchable={false}
              data-testid="disconnect-usage-missing-filter"
              onValueChange={(value) => {
                patchDraft({ usage_missing: (value ?? '') as '' | 'true' | 'false' })
                applyFilters()
              }}
            />
          </label>
          <label className="block">
            <span className="input-label">{t('admin.usage.disconnectEvents.enforcement')}</span>
            <Select
              value={draft.enforce}
              options={enforceOptions}
              searchable={false}
              data-testid="disconnect-enforce-filter"
              onValueChange={(value) => {
                patchDraft({ enforce: (value ?? '') as '' | 'true' | 'false' })
                applyFilters()
              }}
            />
          </label>
          <label className="block">
            <span className="input-label">{t('admin.usage.disconnectEvents.autoBanned')}</span>
            <Select
              value={draft.auto_banned}
              options={yesNoOptions}
              searchable={false}
              data-testid="disconnect-auto-ban-filter"
              onValueChange={(value) => {
                patchDraft({ auto_banned: (value ?? '') as '' | 'true' | 'false' })
                applyFilters()
              }}
            />
          </label>
        </div>

        <div className="disconnect-filter-actions mt-3 flex flex-wrap items-center justify-between gap-3">
          <p className="text-xs surface-muted">{t('admin.usage.disconnectEvents.hint')}</p>
          <div className="flex gap-2">
            <button type="button" className="btn btn-secondary" onClick={resetFilters}>
              {t('common.reset')}
            </button>
            <button
              type="button"
              className="btn btn-primary inline-flex items-center gap-2"
              disabled={loading}
              onClick={applyFilters}
            >
              <Icon name="refresh" size="sm" className={loading ? 'animate-spin' : undefined} />
              {t('common.refresh')}
            </button>
          </div>
        </div>
      </div>

      <div
        className={`record-table-scroll table-container min-w-0 w-full max-w-full overflow-x-auto ${
          workspace ? 'workspace-table-viewport' : ''
        }`}
      >
        <table className="record-table w-full min-w-[2200px] divide-y divide-gray-200 dark:divide-dark-700">
          <thead className="surface-inset">
            <tr>
              {headings.map((heading) => (
                <th
                  key={heading}
                  className="whitespace-nowrap px-4 py-3 text-left text-xs font-medium uppercase surface-muted"
                >
                  {heading}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100 surface-inset dark:divide-dark-800">
            {loading && (
              <tr>
                <td colSpan={headings.length} className="px-4 py-12 text-center text-sm surface-muted">
                  {t('common.loading')}
                </td>
              </tr>
            )}
            {!loading && events.length === 0 && (
              <tr>
                <td colSpan={headings.length} className="px-4 py-12 text-center text-sm surface-muted">
                  {t('admin.usage.disconnectEvents.empty')}
                </td>
              </tr>
            )}
            {!loading &&
              events.map((event) => (
                <tr key={`${event.user_id}:${event.generation}:${event.request_id}`} className="hover:surface-hover">
                  <td className="whitespace-nowrap px-4 py-4 text-sm surface-muted">
                    {formatDateTime(event.accepted_at)}
                  </td>
                  <td className="whitespace-nowrap px-4 py-4 text-sm surface-muted">
                    {event.finalized_at ? formatDateTime(event.finalized_at) : '-'}
                  </td>
                  <td className="max-w-[240px] truncate px-4 py-4 text-sm surface-muted" title={event.user_email || ''}>
                    {event.user_email || '-'}
                  </td>
                  <td className="max-w-[220px] truncate px-4 py-4 text-sm surface-muted" title={event.api_key_name || ''}>
                    {event.api_key_name || '-'}
                  </td>
                  <td className="max-w-[240px] px-4 py-4 text-sm surface-muted">
                    <div className="flex items-center gap-2">
                      <span className="truncate font-mono" title={event.request_id}>
                        {event.request_id || '-'}
                      </span>
                      {event.request_id && (
                        <button
                          type="button"
                          className="shrink-0 hover:text-gray-700 dark:hover:text-gray-200 surface-control surface-muted"
                          title={t('common.copy')}
                          onClick={() => copyValue(event.request_id)}
                        >
                          <Icon name="copy" size="xs" />
                        </button>
                      )}
                    </div>
                  </td>
                  <td className="max-w-[240px] px-4 py-4 text-sm surface-muted">
                    <div className="flex items-center gap-2">
                      <span className="truncate font-mono" title={event.session_id || ''}>
                        {event.session_id || '-'}
                      </span>
                      {event.session_id && (
                        <button
                          type="button"
                          className="shrink-0 hover:text-gray-700 dark:hover:text-gray-200 surface-control surface-muted"
                          title={t('common.copy')}
                          onClick={() => copyValue(event.session_id as string)}
                        >
                          <Icon name="copy" size="xs" />
                        </button>
                      )}
                    </div>
                  </td>
                  <td className="whitespace-nowrap px-4 py-4 text-sm surface-muted">{event.protocol || '-'}</td>
                  <td className="whitespace-nowrap px-4 py-4">
                    <span
                      className={`inline-flex rounded-md px-2 py-1 text-xs font-medium ${statusClass(event.completion_status)}`}
                    >
                      {statusLabel(event.completion_status)}
                    </span>
                  </td>
                  <td className="whitespace-nowrap px-4 py-4 text-sm surface-muted">{outcomeLabel(event.outcome)}</td>
                  <td className="whitespace-nowrap px-4 py-4 text-sm surface-muted">
                    {event.usage_source ? usageSourceLabel(event.usage_source) : '-'}
                  </td>
                  <td
                    className={`whitespace-nowrap px-4 py-4 text-sm ${
                      event.usage_missing ? 'font-medium text-red-600 dark:text-red-300' : 'surface-muted'
                    }`}
                  >
                    {event.usage_missing ? t('common.yes') : t('common.no')}
                  </td>
                  <td className="whitespace-nowrap px-4 py-4 text-sm surface-muted">{event.consecutive_after ?? '-'}</td>
                  <td className="whitespace-nowrap px-4 py-4 text-sm surface-muted">{event.threshold ?? '-'}</td>
                  <td className="whitespace-nowrap px-4 py-4 text-sm surface-muted">{enforcementLabel(event.enforce)}</td>
                  <td className="whitespace-nowrap px-4 py-4 text-sm">
                    {event.auto_banned ? (
                      <span className="inline-flex rounded-md bg-red-100/[0.15] px-2 py-1 text-xs font-medium text-red-700 dark:bg-red-900/30 dark:text-red-300">
                        {t('admin.usage.disconnectEvents.banned')}
                      </span>
                    ) : (
                      <span className="surface-muted">-</span>
                    )}
                  </td>
                </tr>
              ))}
          </tbody>
        </table>
      </div>

      {pagination.total > 0 && (
        <Pagination
          className={workspace ? 'workspace-fixed' : undefined}
          page={pagination.page}
          total={pagination.total}
          pageSize={pagination.page_size}
          onPageChange={changePage}
          onPageSizeChange={changePageSize}
        />
      )}
    </div>
  )
}

export default ClientDisconnectEventsPanel
