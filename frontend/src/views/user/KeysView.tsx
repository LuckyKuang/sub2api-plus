import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/components/DataTable'
import { Modal } from '@/components/ui'
import { TablePageLayout } from '@/components/layout/TablePageLayout'
import { EmptyState, Pagination, SearchInput, Select, ConfirmDialog, toast } from '@/components/ui'
import { DateTimePicker } from '@/components/auth/DateTimePicker'
import { GroupBadge } from '@/components/badges'
import { GroupOptionItem } from '@/components/GroupOptionItem'
import { keysAPI, authAPI, usageAPI, userGroupsAPI } from '@/api'
import { getKeyQuotes } from '@/api/channels'
import type {
  ApiKey,
  Group,
  PublicSettings,
  SubscriptionType,
  GroupPlatform,
  UpdateApiKeyRequest
} from '@/types'
import type { BatchApiKeyUsageStats } from '@/api/usage'
import type { CatalogGroup } from '@/api/channels'
import { formatDateTime } from '@/utils/format'
import { maskApiKey } from '@/utils/maskApiKey'
import { platformBadgeLightClass } from '@/utils/platformColors'
import { KEY_GROUP_PROVIDERS, KEY_GROUP_PROVIDER_ICONS, getKeyGroupProvider, type KeyGroupProvider } from '@/utils/keyGroupProviders'
import {
  CC_SWITCH_USAGE_SCRIPT,
  buildCcSwitchImportDeeplink,
  type CcSwitchClientType
} from '@/utils/ccswitchImport'
import { useLocaleRefresh as subscribeLocaleRefresh } from '@/utils/localeRefresh'
import { getPersistedPageSize, setPersistedPageSize } from './useUsageSnapshot'
import { useClipboard } from './useClipboard'
import { Icon } from './icons'
import { PlatformIcon } from './PlatformIcon'
import { BulkEditKeysModal } from './BulkEditKeysModal'
import { EndpointPopover } from './EndpointPopover'
import { GroupRateSummary } from './GroupRateSummary'
import { NativeAccessFields, nativeAccessValidateRef } from './NativeAccessFields'
import { UseKeyModal } from './UseKeyModal'

interface GroupOption {
  value: number
  label: string
  description: string | null
  rate: number
  userRate: number | null
  peakRateEnabled: boolean
  peakStart: string
  peakEnd: string
  peakRateMultiplier: number
  subscriptionType: SubscriptionType
  platform: GroupPlatform
}

const formatDateTimeLocal = (isoDate: string): string => {
  const date = new Date(isoDate)
  const pad = (n: number) => n.toString().padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

const ALWAYS_VISIBLE_COLUMNS = new Set(['name', 'actions'])
const DEFAULT_HIDDEN_COLUMNS = ['id', 'rate_limit', 'last_used_at', 'last_used_ip']
const HIDDEN_COLUMNS_KEY = 'api-key-hidden-columns'
const COLUMN_SETTINGS_VERSION_KEY = 'api-key-column-settings-version'
const COLUMN_SETTINGS_VERSION = 3
const VERSION_NEW_HIDDEN_COLUMNS: Record<number, string[]> = {
  2: ['last_used_ip'],
  3: ['id']
}

interface KeyFormData {
  name: string
  conversion_enabled: boolean
  default_client: string
  default_endpoint: string
  group_id: number | null
  status: 'active' | 'inactive'
  use_custom_key: boolean
  custom_key: string
  enable_ip_restriction: boolean
  ip_whitelist: string
  ip_blacklist: string
  enable_quota: boolean
  quota: number | null
  enable_rate_limit: boolean
  rate_limit_5h: number | null
  rate_limit_1d: number | null
  rate_limit_7d: number | null
  enable_expiration: boolean
  expiration_preset: '7' | '30' | '90' | 'custom'
  expiration_date: string
}

const emptyForm = (): KeyFormData => ({
  name: '',
  conversion_enabled: false,
  default_client: '',
  default_endpoint: '',
  group_id: null,
  status: 'active',
  use_custom_key: false,
  custom_key: '',
  enable_ip_restriction: false,
  ip_whitelist: '',
  ip_blacklist: '',
  enable_quota: false,
  quota: null,
  enable_rate_limit: false,
  rate_limit_5h: null,
  rate_limit_1d: null,
  rate_limit_7d: null,
  enable_expiration: false,
  expiration_preset: '30',
  expiration_date: ''
})

export default function KeysView() {
  const { t } = useTranslation()
  const { copyToClipboard: clipboardCopy } = useClipboard()

  const [apiKeys, setApiKeys] = useState<ApiKey[]>([])
  const [selectedIds, setSelectedIds] = useState<number[]>([])
  const [showBulkEditModal, setShowBulkEditModal] = useState(false)
  const [groups, setGroups] = useState<Group[]>([])
  const [quotedGroups, setQuotedGroups] = useState<CatalogGroup[]>([])
  const [quotesLoading, setQuotesLoading] = useState(false)
  const [quotesFailed, setQuotesFailed] = useState(false)
  const [loading, setLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [now, setNow] = useState(() => new Date())
  const [usageStats, setUsageStats] = useState<Record<string, BatchApiKeyUsageStats>>({})
  const [userGroupRates, setUserGroupRates] = useState<Record<number, number>>({})

  const [pagination, setPagination] = useState({ page: 1, page_size: getPersistedPageSize(), total: 0, pages: 0 })
  const [sortState, setSortState] = useState({ sort_by: 'created_at', sort_order: 'desc' as 'asc' | 'desc' })

  const [filterSearch, setFilterSearch] = useState('')
  const [filterStatus, setFilterStatus] = useState('')
  const [filterGroupId, setFilterGroupId] = useState<string | number>('')

  const [showCreateModal, setShowCreateModal] = useState(false)
  const [showEditModal, setShowEditModal] = useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [showResetQuotaDialog, setShowResetQuotaDialog] = useState(false)
  const [showResetRateLimitDialog, setShowResetRateLimitDialog] = useState(false)
  const [showUseKeyModal, setShowUseKeyModal] = useState(false)
  const [showCcsClientSelect, setShowCcsClientSelect] = useState(false)
  const [showColumnDropdown, setShowColumnDropdown] = useState(false)
  const [pendingCcsRow, setPendingCcsRow] = useState<ApiKey | null>(null)
  const [selectedKey, setSelectedKey] = useState<ApiKey | null>(null)
  const [copiedKeyId, setCopiedKeyId] = useState<number | null>(null)
  const [changingGroupIds, setChangingGroupIds] = useState<Set<number>>(new Set())
  const [groupSelectorKeyId, setGroupSelectorKeyId] = useState<number | null>(null)
  const [publicSettings, setPublicSettings] = useState<PublicSettings | null>(null)
  const [dropdownPosition, setDropdownPosition] = useState<{ top?: number; bottom?: number; left: number } | null>(null)
  const [groupSearchQuery, setGroupSearchQuery] = useState('')
  const [createProvider, setCreateProvider] = useState<KeyGroupProvider>('anthropic')
  const [formData, setFormData] = useState<KeyFormData>(emptyForm)
  const [hiddenColumns, setHiddenColumns] = useState<Set<string>>(new Set())

  const dropdownRef = useRef<HTMLDivElement>(null)
  const columnDropdownRef = useRef<HTMLDivElement>(null)
  const groupButtonRefs = useRef<Map<number, HTMLElement>>(new Map())
  const abortController = useRef<AbortController | null>(null)
  const resetTimer = useRef<ReturnType<typeof setInterval> | null>(null)

  const filtersRef = useRef({ filterSearch, filterStatus, filterGroupId })
  filtersRef.current = { filterSearch, filterStatus, filterGroupId }
  const sortRef = useRef(sortState)
  sortRef.current = sortState
  const paginationRef = useRef(pagination)
  paginationRef.current = pagination

  const quoteFor = (id: number | null) => quotedGroups.find((group) => group.id === id)

  const allColumns = useMemo<Column[]>(
    () => [
      { key: 'name', label: t('common.name'), sortable: true },
      { key: 'id', label: t('keys.id'), sortable: true },
      { key: 'key', label: t('keys.apiKey'), sortable: false },
      { key: 'group', label: t('keys.group'), sortable: true },
      { key: 'current_concurrency', label: t('keys.currentConcurrency'), sortable: true },
      { key: 'usage', label: t('keys.usage'), sortable: false },
      { key: 'rate_limit', label: t('keys.rateLimitColumn'), sortable: false },
      { key: 'expires_at', label: t('keys.expiresAt'), sortable: true },
      { key: 'status', label: t('common.status'), sortable: true },
      { key: 'last_used_at', label: t('keys.lastUsedAt'), sortable: true },
      { key: 'last_used_ip', label: t('keys.lastUsedIP'), sortable: false },
      { key: 'created_at', label: t('keys.created'), sortable: true },
      { key: 'actions', label: t('common.actions'), sortable: false }
    ],
    [t]
  )

  const toggleableColumns = allColumns.filter((col) => !ALWAYS_VISIBLE_COLUMNS.has(col.key))
  const columns = allColumns.filter((col) => ALWAYS_VISIBLE_COLUMNS.has(col.key) || !hiddenColumns.has(col.key))
  const isColumnVisible = (key: string) => !hiddenColumns.has(key)

  const saveColumnsToStorage = (next: Set<string>) => {
    try {
      localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...next]))
      localStorage.setItem(COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
    } catch (error) {
      console.error('Failed to save API key table columns:', error)
    }
  }

  const loadSavedColumns = useCallback(() => {
    const next = new Set<string>()
    try {
      const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
      if (saved) {
        const parsed = JSON.parse(saved) as string[]
        const validColumnKeys = new Set(allColumns.map((col) => col.key))
        parsed
          .filter((key) => typeof key === 'string' && validColumnKeys.has(key) && !ALWAYS_VISIBLE_COLUMNS.has(key))
          .forEach((key) => next.add(key))
        const storedVersion = Number(localStorage.getItem(COLUMN_SETTINGS_VERSION_KEY) ?? '1')
        if (storedVersion < COLUMN_SETTINGS_VERSION) {
          for (let v = storedVersion + 1; v <= COLUMN_SETTINGS_VERSION; v++) {
            for (const key of VERSION_NEW_HIDDEN_COLUMNS[v] ?? []) {
              if (validColumnKeys.has(key) && !ALWAYS_VISIBLE_COLUMNS.has(key)) next.add(key)
            }
          }
          saveColumnsToStorage(next)
        } else {
          localStorage.setItem(COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
        }
      } else {
        DEFAULT_HIDDEN_COLUMNS.forEach((key) => next.add(key))
        localStorage.setItem(COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
      }
    } catch (error) {
      console.error('Failed to load API key table columns:', error)
      DEFAULT_HIDDEN_COLUMNS.forEach((key) => next.add(key))
    }
    setHiddenColumns(next)
  }, [allColumns])

  const toggleColumn = (key: string) => {
    if (ALWAYS_VISIBLE_COLUMNS.has(key)) return
    setHiddenColumns((previous) => {
      const next = new Set(previous)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      saveColumnsToStorage(next)
      return next
    })
  }

  const selectedApiKeys = apiKeys.filter((key) => selectedIds.includes(key.id))

  const handleSelectionChange = (ids: Array<string | number>) => {
    const visibleIds = new Set(apiKeys.map((key) => key.id))
    setSelectedIds([...new Set(ids.map(Number))].filter((id) => visibleIds.has(id)))
  }

  const handleBulkUpdated = (succeededIds: number[]) => {
    const succeeded = new Set(succeededIds)
    setSelectedIds((previous) => previous.filter((id) => !succeeded.has(id)))
    void loadApiKeys()
  }

  const quotesRequest = useRef(0)
  const quotesController = useRef<AbortController | undefined>(undefined)

  // An authorized backend quote is the only source of customer selling rates.
  const refreshQuotes = useCallback(async () => {
    const current = ++quotesRequest.current
    quotesController.current?.abort()
    quotesController.current = new AbortController()
    setQuotedGroups([])
    setQuotesLoading(true)
    setQuotesFailed(false)
    try {
      const catalog = await getKeyQuotes({ signal: quotesController.current.signal })
      if (current !== quotesRequest.current) return
      if (catalog.user_rate_status === 'unavailable') setQuotesFailed(true)
      else setQuotedGroups(catalog.groups)
    } catch {
      if (current === quotesRequest.current) setQuotesFailed(true)
    } finally {
      if (current === quotesRequest.current) setQuotesLoading(false)
    }
  }, [])

  // Refresh displayed request-time quotes while the page remains visible.
  useEffect(() => {
    let running = false
    let disposed = false
    const update = async () => {
      if (disposed || running || document.visibilityState === 'hidden') return
      running = true
      try {
        await refreshQuotes()
      } catch {
        /* The owning page renders its own error state. */
      } finally {
        running = false
      }
    }
    const interval = window.setInterval(() => void update(), 60_000)
    window.addEventListener('focus', update)
    document.addEventListener('visibilitychange', update)
    return () => {
      disposed = true
      window.clearInterval(interval)
      window.removeEventListener('focus', update)
      document.removeEventListener('visibilitychange', update)
    }
  }, [refreshQuotes])

  const isAbortError = (error: unknown) => {
    if (!error || typeof error !== 'object') return false
    const { name, code } = error as { name?: string; code?: string }
    return name === 'AbortError' || code === 'ERR_CANCELED'
  }

  const loadApiKeys = useCallback(async () => {
    abortController.current?.abort()
    const controller = new AbortController()
    abortController.current = controller
    const { signal } = controller
    setLoading(true)
    try {
      const filters: {
        search?: string
        status?: string
        group_id?: number | string
        sort_by?: string
        sort_order?: 'asc' | 'desc'
      } = {}
      if (filtersRef.current.filterSearch) filters.search = filtersRef.current.filterSearch
      if (filtersRef.current.filterStatus) filters.status = filtersRef.current.filterStatus
      if (filtersRef.current.filterGroupId !== '') filters.group_id = filtersRef.current.filterGroupId
      filters.sort_by = sortRef.current.sort_by
      filters.sort_order = sortRef.current.sort_order

      const response = await keysAPI.list(paginationRef.current.page, paginationRef.current.page_size, filters, {
        signal
      })
      if (signal.aborted) return
      setApiKeys(response.items)
      handleSelectionChange(selectedIds)
      setPagination((previous) => ({ ...previous, total: response.total, pages: response.pages }))

      if (response.items.length > 0) {
        const keyIds = response.items.map((k) => k.id)
        try {
          const usageResponse = await usageAPI.getDashboardApiKeysUsage(keyIds, { signal })
          if (signal.aborted) return
          setUsageStats(usageResponse.stats)
        } catch (e) {
          if (!isAbortError(e)) console.error('Failed to load usage stats:', e)
        }
      }
    } catch (error) {
      if (isAbortError(error)) return
      toast.error(t('keys.failedToLoad'))
    } finally {
      if (abortController.current === controller) setLoading(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedIds, t])

  const loadGroups = useCallback(async () => {
    const request = Date.now()
    groupsRequest.current = request
    try {
      const result = await userGroupsAPI.getAvailable()
      if (groupsRequest.current === request) setGroups(result)
    } catch (error) {
      console.error('Failed to load groups:', error)
    }
  }, [])
  const groupsRequest = useRef(0)

  const loadUserGroupRates = useCallback(async () => {
    try {
      setUserGroupRates(await userGroupsAPI.getUserGroupRates())
    } catch (error) {
      console.error('Failed to load user group rates:', error)
    }
  }, [])

  const loadPublicSettings = useCallback(async () => {
    try {
      setPublicSettings(await authAPI.getPublicSettings())
    } catch (error) {
      console.error('Failed to load public settings:', error)
    }
  }, [])

  useEffect(() => {
    loadSavedColumns()
    void loadApiKeys()
    void loadGroups()
    void loadUserGroupRates()
    void refreshQuotes()
    void loadPublicSettings()
    resetTimer.current = setInterval(() => setNow(new Date()), 60000)
    return () => {
      if (resetTimer.current) clearInterval(resetTimer.current)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    const closeGroupSelector = (event: MouseEvent) => {
      const target = event.target as HTMLElement
      if (!target.closest('.group\\/dropdown') && !dropdownRef.current?.contains(target)) {
        setGroupSelectorKeyId(null)
        setDropdownPosition(null)
      }
      if (columnDropdownRef.current && !columnDropdownRef.current.contains(target)) {
        setShowColumnDropdown(false)
      }
    }
    document.addEventListener('click', closeGroupSelector)
    return () => document.removeEventListener('click', closeGroupSelector)
  }, [])

  useEffect(() => {
    return () => abortController.current?.abort()
  }, [])

  const groupOptions = useMemo<GroupOption[]>(
    () =>
      groups.map((group) => ({
        value: group.id,
        label: group.name,
        description: group.description,
        rate: group.rate_multiplier,
        userRate: userGroupRates[group.id] ?? null,
        peakRateEnabled: group.peak_rate_enabled,
        peakStart: group.peak_start,
        peakEnd: group.peak_end,
        peakRateMultiplier: group.peak_rate_multiplier,
        subscriptionType: group.subscription_type,
        platform: group.platform
      })),
    [groups, userGroupRates]
  )

  const createProviderOptions = KEY_GROUP_PROVIDERS.map((value) => ({
    value,
    label: t(`keys.providers.${value}`),
    count: groups.filter((group) => getKeyGroupProvider(group.platform) === value).length
  }))

  const formGroupOptions = showEditModal
    ? groupOptions
    : groupOptions.filter((group) => getKeyGroupProvider(group.platform) === createProvider)

  // Also handles groups arriving after the create dialog has already opened.
  useEffect(() => {
    if (!showCreateModal) return
    if (!createProviderOptions.some((provider) => provider.value === createProvider && provider.count > 0)) {
      setCreateProvider(createProviderOptions.find((provider) => provider.count > 0)?.value ?? 'anthropic')
    }
    if (!formGroupOptions.some((group) => group.value === formData.group_id)) {
      setFormData((previous) => ({ ...previous, group_id: null }))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [showCreateModal, createProviderOptions.map((p) => `${p.value}:${p.count}`).join(',')])

  const selectCreateProvider = (provider: KeyGroupProvider) => {
    if (createProvider === provider) return
    setCreateProvider(provider)
    setFormData((previous) => ({ ...previous, group_id: null }))
  }

  const filteredGroupOptions = useMemo(() => {
    const query = groupSearchQuery.trim().toLowerCase()
    if (!query) return groupOptions
    return groupOptions.filter(
      (opt) =>
        opt.label.toLowerCase().includes(query) ||
        (opt.description && opt.description.toLowerCase().includes(query))
    )
  }, [groupOptions, groupSearchQuery])

  const groupFilterOptions = useMemo(
    () => [
      { value: '', label: t('keys.allGroups') },
      { value: 0, label: t('keys.noGroup') },
      ...groups.map((g) => ({ value: g.id, label: g.name }))
    ],
    [groups, t]
  )

  const statusFilterOptions = useMemo(
    () => [
      { value: '', label: t('keys.allStatus') },
      { value: 'active', label: t('keys.status.active') },
      { value: 'inactive', label: t('keys.status.inactive') },
      { value: 'disabled', label: t('keys.status.disabled') },
      { value: 'quota_exhausted', label: t('keys.status.quota_exhausted') },
      { value: 'expired', label: t('keys.status.expired') }
    ],
    [t]
  )

  const statusOptions = useMemo(
    () => [
      { value: 'active', label: t('common.active') },
      { value: 'inactive', label: t('common.inactive') }
    ],
    [t]
  )

  const customKeyError = (() => {
    if (!formData.use_custom_key || !formData.custom_key) return ''
    const key = formData.custom_key
    if (key.length < 16) return t('keys.customKeyTooShort')
    if (!/^[a-zA-Z0-9_-]+$/.test(key)) return t('keys.customKeyInvalidChars')
    return ''
  })()

  const shouldSubmitEditStatus = (key: ApiKey, status: 'active' | 'inactive') => {
    if (key.status === 'disabled' || key.status === 'quota_exhausted' || key.status === 'expired') {
      return status === 'active'
    }
    return true
  }

  const onFilterChange = () => {
    setSelectedIds([])
    setPagination((previous) => ({ ...previous, page: 1 }))
    void loadApiKeys()
  }

  const copyToClipboard = async (text: string, keyId: number) => {
    const success = await clipboardCopy(text, t('keys.copied'))
    if (success) {
      setCopiedKeyId(keyId)
      setTimeout(() => setCopiedKeyId(null), 800)
    }
  }

  const handlePageChange = (page: number) => {
    setSelectedIds([])
    setPagination((previous) => ({ ...previous, page }))
    void loadApiKeys()
  }

  const handlePageSizeChange = (pageSize: number) => {
    setSelectedIds([])
    setPagination((previous) => ({ ...previous, page_size: pageSize, page: 1 }))
    void loadApiKeys()
  }

  const handleSort = (key: string, order: 'asc' | 'desc') => {
    setSelectedIds([])
    setSortState({ sort_by: key, sort_order: order })
    setPagination((previous) => ({ ...previous, page: 1 }))
    void loadApiKeys()
  }

  const editKey = (key: ApiKey) => {
    setSelectedKey(key)
    const hasIPRestriction = (key.ip_whitelist?.length ?? 0) > 0 || (key.ip_blacklist?.length ?? 0) > 0
    const hasExpiration = !!key.expires_at
    setFormData({
      name: key.name,
      conversion_enabled: key.conversion_enabled || false,
      default_client: key.default_client || '',
      default_endpoint: key.default_endpoint || '',
      group_id: key.group_id,
      status: key.status === 'active' ? 'active' : 'inactive',
      use_custom_key: false,
      custom_key: '',
      enable_ip_restriction: hasIPRestriction,
      ip_whitelist: (key.ip_whitelist || []).join('\n'),
      ip_blacklist: (key.ip_blacklist || []).join('\n'),
      enable_quota: key.quota > 0,
      quota: key.quota > 0 ? key.quota : null,
      enable_rate_limit: key.rate_limit_5h > 0 || key.rate_limit_1d > 0 || key.rate_limit_7d > 0,
      rate_limit_5h: key.rate_limit_5h || null,
      rate_limit_1d: key.rate_limit_1d || null,
      rate_limit_7d: key.rate_limit_7d || null,
      enable_expiration: hasExpiration,
      expiration_preset: 'custom',
      expiration_date: key.expires_at ? formatDateTimeLocal(key.expires_at) : ''
    })
    setShowEditModal(true)
  }

  const toggleKeyStatus = async (key: ApiKey) => {
    const newStatus = key.status === 'active' ? 'inactive' : 'active'
    try {
      await keysAPI.toggleStatus(key.id, newStatus)
      toast.success(newStatus === 'active' ? t('keys.keyEnabledSuccess') : t('keys.keyDisabledSuccess'))
      void loadApiKeys()
    } catch {
      toast.error(t('keys.failedToUpdateStatus'))
    }
  }

  const setGroupButtonRef = (keyId: number, el: HTMLElement | null) => {
    if (el) groupButtonRefs.current.set(keyId, el)
    else groupButtonRefs.current.delete(keyId)
  }

  const selectedKeyForGroup = groupSelectorKeyId === null ? null : apiKeys.find((k) => k.id === groupSelectorKeyId) || null

  const openGroupSelector = (key: ApiKey) => {
    if (groupSelectorKeyId === key.id) {
      setGroupSelectorKeyId(null)
      setDropdownPosition(null)
      return
    }
    const buttonEl = groupButtonRefs.current.get(key.id)
    if (buttonEl) {
      const rect = buttonEl.getBoundingClientRect()
      const dropdownEstHeight = 400
      const dropdownEstWidth = Math.min(380, window.innerWidth - 16)
      const spaceBelow = window.innerHeight - rect.bottom
      const spaceAbove = rect.top
      const left = Math.max(8, Math.min(rect.left, window.innerWidth - dropdownEstWidth - 8))

      if (spaceBelow < dropdownEstHeight && spaceAbove > spaceBelow) {
        setDropdownPosition({ bottom: window.innerHeight - rect.top + 4, left })
      } else {
        setDropdownPosition({ top: rect.bottom + 4, left })
      }
    }
    setGroupSelectorKeyId(key.id)
    setGroupSearchQuery('')
  }

  const changeGroup = async (key: ApiKey, newGroupId: number | null) => {
    setGroupSelectorKeyId(null)
    setDropdownPosition(null)
    if (key.group_id === newGroupId || changingGroupIds.has(key.id)) return

    setChangingGroupIds((previous) => new Set(previous).add(key.id))
    try {
      await keysAPI.update(key.id, { group_id: newGroupId })
      toast.success(t('keys.groupChangedSuccess'))
      void loadApiKeys()
    } catch {
      toast.error(t('keys.failedToChangeGroup'))
    } finally {
      setChangingGroupIds((previous) => {
        const next = new Set(previous)
        next.delete(key.id)
        return next
      })
    }
  }

  const confirmDelete = (key: ApiKey) => {
    setSelectedKey(key)
    setShowDeleteDialog(true)
  }

  const handleSubmit = async () => {
    if (!nativeAccessValidateRef.current()) return
    if (formData.group_id === null) {
      toast.error(t('keys.groupRequired'))
      return
    }

    if (!showEditModal && formData.use_custom_key) {
      if (!formData.custom_key) {
        toast.error(t('keys.customKeyRequired'))
        return
      }
      if (customKeyError) {
        toast.error(customKeyError)
        return
      }
    }

    const parseIPList = (text: string): string[] =>
      text.split('\n').map((ip) => ip.trim()).filter((ip) => ip.length > 0)
    const ipWhitelist = formData.enable_ip_restriction ? parseIPList(formData.ip_whitelist) : []
    const ipBlacklist = formData.enable_ip_restriction ? parseIPList(formData.ip_blacklist) : []

    const quota = formData.quota && formData.quota > 0 ? formData.quota : 0

    let expiresInDays: number | undefined
    let expiresAt: string | null | undefined
    if (formData.enable_expiration && formData.expiration_date) {
      if (!showEditModal) {
        const expDate = new Date(formData.expiration_date)
        const current = new Date()
        const diffDays = Math.ceil((expDate.getTime() - current.getTime()) / (1000 * 60 * 60 * 24))
        expiresInDays = diffDays > 0 ? diffDays : 1
      } else {
        expiresAt = new Date(formData.expiration_date).toISOString()
      }
    } else if (showEditModal) {
      expiresAt = ''
    }

    const rateLimitData = formData.enable_rate_limit
      ? {
          rate_limit_5h: formData.rate_limit_5h && formData.rate_limit_5h > 0 ? formData.rate_limit_5h : 0,
          rate_limit_1d: formData.rate_limit_1d && formData.rate_limit_1d > 0 ? formData.rate_limit_1d : 0,
          rate_limit_7d: formData.rate_limit_7d && formData.rate_limit_7d > 0 ? formData.rate_limit_7d : 0
        }
      : { rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0 }

    setSubmitting(true)
    try {
      if (showEditModal && selectedKey) {
        const updates: UpdateApiKeyRequest = {
          name: formData.name,
          conversion_enabled:
            groups.find((group) => group.id === formData.group_id)?.platform === 'composite' && formData.conversion_enabled,
          default_client: formData.default_client,
          default_endpoint: formData.default_endpoint,
          group_id: formData.group_id,
          ip_whitelist: ipWhitelist,
          ip_blacklist: ipBlacklist,
          quota,
          expires_at: expiresAt,
          rate_limit_5h: rateLimitData.rate_limit_5h,
          rate_limit_1d: rateLimitData.rate_limit_1d,
          rate_limit_7d: rateLimitData.rate_limit_7d
        }
        if (shouldSubmitEditStatus(selectedKey, formData.status)) updates.status = formData.status
        await keysAPI.update(selectedKey.id, updates)
        toast.success(t('keys.keyUpdatedSuccess'))
      } else {
        const customKey = formData.use_custom_key ? formData.custom_key : undefined
        await keysAPI.create(
          formData.name,
          formData.group_id,
          customKey,
          ipWhitelist,
          ipBlacklist,
          quota,
          expiresInDays,
          rateLimitData,
          {
            conversion_enabled:
              groups.find((group) => group.id === formData.group_id)?.platform === 'composite' && formData.conversion_enabled,
            default_client: formData.default_client,
            default_endpoint: formData.default_endpoint
          }
        )
        toast.success(t('keys.keyCreatedSuccess'))
      }
      closeModals()
      void loadApiKeys()
    } catch (error: unknown) {
      const errorMsg = (error as { response?: { data?: { detail?: string } } }).response?.data?.detail || t('keys.failedToSave')
      toast.error(errorMsg)
    } finally {
      setSubmitting(false)
    }
  }

  const handleDelete = async () => {
    if (!selectedKey) return
    try {
      await keysAPI.delete(selectedKey.id)
      toast.success(t('keys.keyDeletedSuccess'))
      setShowDeleteDialog(false)
      void loadApiKeys()
    } catch (error: unknown) {
      const errorMsg = (error as { message?: string })?.message || t('keys.failedToDelete')
      toast.error(errorMsg)
    }
  }

  const closeModals = () => {
    setShowCreateModal(false)
    setShowEditModal(false)
    setSelectedKey(null)
    setFormData(emptyForm())
  }

  const confirmResetQuota = () => setShowResetQuotaDialog(true)

  const setExpirationDays = (days: number) => {
    setFormData((previous) => ({ ...previous, expiration_preset: days.toString() as '7' | '30' | '90' }))
    const expDate = new Date()
    expDate.setDate(expDate.getDate() + days)
    setFormData((previous) => ({ ...previous, expiration_date: formatDateTimeLocal(expDate.toISOString()) }))
  }

  const resetQuotaUsed = async () => {
    const key = selectedKey
    if (!key) return
    setShowResetQuotaDialog(false)
    try {
      const updatedKey = await keysAPI.update(key.id, { reset_quota: true })
      toast.success(t('keys.quotaResetSuccess'))
      setApiKeys((previous) =>
        previous.map((item) => (item.id === key.id ? { ...item, quota_used: updatedKey.quota_used } : item))
      )
      if (key.status !== updatedKey.status) {
        setApiKeys((previous) => previous.map((item) => (item.id === key.id ? { ...item, status: updatedKey.status } : item)))
        setSelectedKey((current) =>
          current && current.id === key.id
            ? { ...current, status: updatedKey.status === 'active' ? 'active' : 'inactive' }
            : current
        )
        setFormData((previous) => ({ ...previous, status: updatedKey.status === 'active' ? 'active' : 'inactive' }))
      }
    } catch (error: unknown) {
      const errorMsg = (error as { response?: { data?: { detail?: string } } }).response?.data?.detail || t('keys.failedToResetQuota')
      toast.error(errorMsg)
    }
  }

  const confirmResetRateLimit = () => setShowResetRateLimitDialog(true)

  const confirmResetRateLimitFromTable = (row: ApiKey) => {
    setSelectedKey(row)
    setShowResetRateLimitDialog(true)
  }

  const resetRateLimitUsage = async () => {
    if (!selectedKey) return
    setShowResetRateLimitDialog(false)
    try {
      await keysAPI.update(selectedKey.id, { reset_rate_limit_usage: true })
      toast.success(t('keys.rateLimitResetSuccess'))
      await loadApiKeys()
      const refreshedKey = apiKeys.find((k) => k.id === selectedKey.id)
      if (refreshedKey) setSelectedKey(refreshedKey)
    } catch (error: unknown) {
      const errorMsg = (error as { response?: { data?: { detail?: string } } }).response?.data?.detail || t('keys.failedToResetRateLimit')
      toast.error(errorMsg)
    }
  }

  const importToCcswitch = (row: ApiKey) => {
    const platform = row.group?.platform || 'anthropic'

    if (platform === 'antigravity') {
      setPendingCcsRow(row)
      setShowCcsClientSelect(true)
      return
    }

    executeCcsImport(row, platform === 'gemini' ? 'gemini' : 'claude')
  }

  const executeCcsImport = (row: ApiKey, clientType: CcSwitchClientType) => {
    const baseUrl = publicSettings?.api_base_url || window.location.origin
    const platform = row.group?.platform || 'anthropic'

    const usageScript = CC_SWITCH_USAGE_SCRIPT
    const providerName = (publicSettings?.site_name || 'sub2api').trim() || 'sub2api'
    const deeplink = buildCcSwitchImportDeeplink({ baseUrl, platform, clientType, providerName, apiKey: row.key, usageScript })

    try {
      window.open(deeplink, '_self')
      setTimeout(() => {
        if (document.hasFocus()) toast.error(t('keys.ccSwitchNotInstalled'))
      }, 100)
    } catch {
      toast.error(t('keys.ccSwitchNotInstalled'))
    }
  }

  const handleCcsClientSelect = (clientType: CcSwitchClientType) => {
    if (pendingCcsRow) executeCcsImport(pendingCcsRow, clientType)
    setShowCcsClientSelect(false)
    setPendingCcsRow(null)
  }

  const formatResetTime = (resetAt: string | null): string => {
    if (!resetAt) return ''
    const diff = new Date(resetAt).getTime() - now.getTime()
    if (diff <= 0) return t('keys.resetNow')
    const days = Math.floor(diff / 86400000)
    const hours = Math.floor((diff % 86400000) / 3600000)
    const mins = Math.floor((diff % 3600000) / 60000)
    if (days > 0) return `${days}d ${hours}h`
    if (hours > 0) return `${hours}h ${mins}m`
    return `${mins}m`
  }

  const loadLogsRef = useRef(loadApiKeys)
  loadLogsRef.current = loadApiKeys
  useEffect(
    () =>
      subscribeLocaleRefresh(() => {
        void Promise.all([loadLogsRef.current(), loadGroups(), refreshQuotes()])
      }),
    [loadGroups, refreshQuotes]
  )

  const renderCell = (key: string, row: ApiKey, value: unknown): React.ReactNode => {
    switch (key) {
      case 'id':
        return <span className="font-mono text-xs surface-muted">#{String(value)}</span>
      case 'key':
        return (
          <div className="flex items-center gap-2">
            <code className="code text-xs">{maskApiKey(row.key)}</code>
            <button
              onClick={() => void copyToClipboard(row.key, row.id)}
              className={`surface-control rounded-lg p-1 transition-colors hover:surface-hover ${
                copiedKeyId === row.id ? 'text-green-500' : 'text-gray-400 hover:text-gray-600 dark:hover:text-gray-300'
              }`}
              title={copiedKeyId === row.id ? t('keys.copied') : t('keys.copyToClipboard')}
            >
              {copiedKeyId === row.id ? (
                <Icon name="check" size="sm" strokeWidth={2} />
              ) : (
                <Icon name="clipboard" size="sm" />
              )}
            </button>
          </div>
        )
      case 'name':
        return (
          <div className="flex items-center gap-1.5">
            <span className="font-medium text-gray-900 dark:text-white">{String(value)}</span>
            {((row.ip_whitelist?.length ?? 0) > 0 || (row.ip_blacklist?.length ?? 0) > 0) && (
              <span title={t('keys.ipRestrictionEnabled')} className="inline-flex">
                <Icon name="shield" size="sm" className="text-blue-500" />
              </span>
            )}
          </div>
        )
      case 'group':
        return (
          <div className="group/dropdown relative">
            <button
              ref={(el) => setGroupButtonRef(row.id, el)}
              onClick={() => openGroupSelector(row)}
              className="surface-control -mx-2 -my-1 flex cursor-pointer flex-wrap items-center gap-2 rounded-lg px-2 py-1 transition-all duration-200 hover:surface-hover sm:flex-nowrap"
              title={t('keys.clickToChangeGroup')}
              disabled={changingGroupIds.has(row.id)}
            >
              {row.group ? (
                <GroupBadge
                  name={row.group.name}
                  showRate={row.group.platform !== 'composite'}
                  platform={row.group.platform}
                  subscriptionType={row.group.subscription_type}
                  rateMultiplier={row.group.rate_multiplier}
                  userRateMultiplier={userGroupRates[row.group.id]}
                  peakRateEnabled={row.group.peak_rate_enabled}
                  peakStart={row.group.peak_start}
                  peakEnd={row.group.peak_end}
                  peakRateMultiplier={row.group.peak_rate_multiplier}
                />
              ) : (
                <span className="text-sm surface-muted">{t('keys.noGroup')}</span>
              )}
              <span className="shrink-0 text-xs surface-muted">{t('keys.selectGroup')}</span>
              <svg
                className="h-3.5 w-3.5 shrink-0 text-gray-400 opacity-60 transition-opacity group-hover/dropdown:opacity-100"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
                strokeWidth="2"
              >
                <path strokeLinecap="round" strokeLinejoin="round" d="M8.25 15L12 18.75 15.75 15m-7.5-6L12 5.25 15.75 9" />
              </svg>
            </button>
          </div>
        )
      case 'current_concurrency':
        return (
          <span
            className={`inline-flex min-w-8 items-center justify-center rounded px-2 py-1 text-sm font-semibold tabular-nums ${
              (row.current_concurrency ?? 0) > 0
                ? 'bg-emerald-50/[0.15] text-emerald-700 ring-1 ring-emerald-200 dark:bg-emerald-900/25 dark:text-emerald-300 dark:ring-emerald-800'
                : 'surface-inset text-gray-500 dark:surface-inset dark:text-dark-400'
            }`}
          >
            {row.current_concurrency ?? 0}
          </span>
        )
      case 'usage':
        return (
          <div className="text-sm">
            <div className="flex items-center gap-1.5">
              <span className="surface-muted">{t('keys.today')}:</span>
              <span className="font-medium text-gray-900 dark:text-white">
                ${(usageStats[row.id]?.today_actual_cost ?? 0).toFixed(4)}
              </span>
            </div>
            <div className="mt-0.5 flex items-center gap-1.5">
              <span className="surface-muted">{t('keys.total')}:</span>
              <span className="font-medium text-gray-900 dark:text-white">
                ${(usageStats[row.id]?.total_actual_cost ?? 0).toFixed(4)}
              </span>
            </div>
            {row.quota > 0 && (
              <div className="mt-1.5">
                <div className="flex items-center gap-1.5">
                  <span className="surface-muted">{t('keys.quota')}:</span>
                  <span
                    className={`font-medium ${
                      row.quota_used >= row.quota
                        ? 'text-red-500'
                        : row.quota_used >= row.quota * 0.8
                          ? 'text-yellow-500'
                          : 'text-gray-900 dark:text-white'
                    }`}
                  >
                    ${row.quota_used?.toFixed(2) || '0.00'} / ${row.quota?.toFixed(2)}
                  </span>
                </div>
                <div className="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                  <div
                    className={`console-data-mark h-full rounded-full transition-all ${
                      row.quota_used >= row.quota
                        ? 'bg-red-500'
                        : row.quota_used >= row.quota * 0.8
                          ? 'bg-yellow-500'
                          : 'bg-primary-500'
                    }`}
                    style={{ width: Math.min((row.quota_used / row.quota) * 100, 100) + '%' }}
                  />
                </div>
              </div>
            )}
          </div>
        )
      case 'rate_limit': {
        const windows: Array<{ label: string; usage: number; limit: number; resetAt: string | null }> = [
          { label: '5h', usage: row.usage_5h, limit: row.rate_limit_5h, resetAt: row.reset_5h_at },
          { label: '1d', usage: row.usage_1d, limit: row.rate_limit_1d, resetAt: row.reset_1d_at },
          { label: '7d', usage: row.usage_7d, limit: row.rate_limit_7d, resetAt: row.reset_7d_at }
        ]
        const active = windows.filter((w) => w.limit > 0)
        if (active.length === 0) return <span className="text-sm surface-muted">-</span>
        return (
          <div className="min-w-[140px] space-y-1.5">
            {active.map((w) => (
              <div key={w.label}>
                <div className="flex items-center justify-between text-xs">
                  <span className="surface-muted">{w.label}</span>
                  <span
                    className={`font-medium tabular-nums ${
                      w.usage >= w.limit ? 'text-red-500' : w.usage >= w.limit * 0.8 ? 'text-yellow-500' : 'surface-muted'
                    }`}
                  >
                    ${w.usage?.toFixed(2) || '0.00'}/${w.limit?.toFixed(2)}
                  </span>
                </div>
                <div className="h-1 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                  <div
                    className={`console-data-mark h-full rounded-full transition-all ${
                      w.usage >= w.limit ? 'bg-red-500' : w.usage >= w.limit * 0.8 ? 'bg-yellow-500' : 'bg-emerald-500'
                    }`}
                    style={{ width: Math.min((w.usage / w.limit) * 100, 100) + '%' }}
                  />
                </div>
                {w.resetAt && formatResetTime(w.resetAt) && (
                  <div className="text-[10px] tabular-nums surface-muted">⟳ {formatResetTime(w.resetAt)}</div>
                )}
              </div>
            ))}
            {(row.usage_5h > 0 || row.usage_1d > 0 || row.usage_7d > 0) && (
              <button
                className="surface-control surface-muted mt-0.5 inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs transition-colors hover:surface-hover hover:text-primary-600 dark:hover:text-primary-400"
                title={t('keys.resetRateLimitUsage')}
                onClick={(event) => {
                  event.stopPropagation()
                  confirmResetRateLimitFromTable(row)
                }}
              >
                <Icon name="refresh" size="xs" />
                {t('keys.resetUsage')}
              </button>
            )}
          </div>
        )
      }
      case 'expires_at':
        return value ? (
          <span
            className={`text-sm ${new Date(String(value)) < new Date() ? 'text-red-500 dark:text-red-400' : 'surface-muted'}`}
          >
            {formatDateTime(String(value))}
          </span>
        ) : (
          <span className="text-sm surface-muted">{t('keys.noExpiration')}</span>
        )
      case 'status':
        return (
          <span
            className={`badge ${
              value === 'active'
                ? 'badge-success'
                : value === 'quota_exhausted'
                  ? 'badge-warning'
                  : value === 'expired'
                    ? 'badge-danger'
                    : 'badge-gray'
            }`}
          >
            {t('keys.status.' + String(value))}
          </span>
        )
      case 'last_used_at':
        return value ? (
          <span className="text-sm surface-muted">{formatDateTime(String(value))}</span>
        ) : (
          <span className="text-sm surface-muted">-</span>
        )
      case 'last_used_ip':
        return value ? (
          <span className="text-sm surface-muted">{String(value)}</span>
        ) : (
          <span className="text-sm surface-muted">-</span>
        )
      case 'created_at':
        return <span className="text-sm surface-muted">{formatDateTime(String(value))}</span>
      case 'actions':
        return (
          <div className="table-actions">
            <button className="table-action" onClick={() => openUseKeyModal(row)}>
              <Icon name="terminal" size="sm" />
              <span className="text-xs">{t('keys.useKey')}</span>
            </button>
            {!publicSettings?.hide_ccs_import_button && (
              <button className="table-action" onClick={() => importToCcswitch(row)}>
                <Icon name="upload" size="sm" />
                <span className="text-xs">{t('keys.importToCcSwitch')}</span>
              </button>
            )}
            <button
              className={`table-action ${row.status === 'active' ? 'table-action-warning' : ''}`}
              onClick={() => void toggleKeyStatus(row)}
            >
              {row.status === 'active' ? <Icon name="ban" size="sm" /> : <Icon name="checkCircle" size="sm" />}
              <span className="text-xs">{row.status === 'active' ? t('keys.disable') : t('keys.enable')}</span>
            </button>
            <button className="table-action" onClick={() => editKey(row)}>
              <Icon name="edit" size="sm" />
              <span className="text-xs">{t('common.edit')}</span>
            </button>
            <button className="table-action table-action-danger" onClick={() => confirmDelete(row)}>
              <Icon name="trash" size="sm" />
              <span className="text-xs">{t('common.delete')}</span>
            </button>
          </div>
        )
      default:
        return value === null || value === undefined ? '' : String(value)
    }
  }

  const openUseKeyModal = (key: ApiKey) => {
    setSelectedKey(key)
    setShowUseKeyModal(true)
  }

  const moreMenuJob = groupSelectorKeyId === null ? null : apiKeys.find((job) => job.id === groupSelectorKeyId) || null

  return (
    <TablePageLayout
      filters={
        <div className="flex flex-col gap-3">
          <div className="filter-bar">
          <div className="filter-field filter-field--search">
            <SearchInput
              value={filterSearch}
              placeholder={t('keys.searchPlaceholder')}
              onValueChange={setFilterSearch}
              onSearch={onFilterChange}
            />
          </div>
          <div className="filter-field">
            <Select
              value={filterGroupId}
              options={groupFilterOptions}
              onValueChange={(value) => {
                setFilterGroupId(value === null || value === undefined ? '' : (value as string | number))
                onFilterChange()
              }}
            />
          </div>
          <div className="filter-field">
            <Select
              value={filterStatus}
              options={statusFilterOptions}
              onValueChange={(value) => {
                setFilterStatus(value === null ? '' : String(value))
                onFilterChange()
              }}
            />
          </div>
        </div>
        {(publicSettings?.api_base_url || (publicSettings?.custom_endpoints?.length ?? 0) > 0) && (
          <EndpointPopover
            apiBaseUrl={publicSettings?.api_base_url || ''}
            customEndpoints={publicSettings?.custom_endpoints || []}
          />
        )}
        <div className="flex flex-wrap items-center justify-end gap-3">
          <button
            onClick={() => void loadApiKeys()}
            disabled={loading}
            className="btn btn-secondary"
            title={t('common.refresh')}
          >
            <Icon name="refresh" size="md" className={loading ? 'animate-spin' : ''} />
          </button>
          <div className="relative" ref={columnDropdownRef}>
            <button
              onClick={() => setShowColumnDropdown((value) => !value)}
              className="btn btn-secondary px-2 md:px-3"
              title={t('keys.columnSettings')}
            >
              <svg className="h-4 w-4 md:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="1.5">
                <path strokeLinecap="round" strokeLinejoin="round" d="M9 4.5v15m6-15v15m-10.875 0h15.75c.621 0 1.125-.504 1.125-1.125V5.625c0-.621-.504-1.125-1.125-1.125H4.125C3.504 4.5 3 5.004 3 5.625v12.75c0 .621.504 1.125 1.125 1.125z" />
              </svg>
              <span className="hidden md:inline">{t('keys.columnSettings')}</span>
            </button>
            {showColumnDropdown && (
              <div className="surface-panel absolute right-0 top-full z-50 mt-1 max-h-80 w-48 space-y-0.5 overflow-y-auto border border-gray-200 p-1 shadow-lg dark:border-dark-600">
                {toggleableColumns.map((col) => (
                  <button
                    key={col.key}
                    onClick={() => toggleColumn(col.key)}
                    className="surface-control surface-muted flex w-full items-center justify-between rounded-[var(--surface-radius-item)] px-3 py-2 text-left text-sm hover:surface-hover"
                  >
                    <span>{col.label}</span>
                    {isColumnVisible(col.key) && <Icon name="check" size="sm" className="text-primary-500" strokeWidth={2} />}
                  </button>
                ))}
              </div>
            )}
          </div>
          <button onClick={() => setShowCreateModal(true)} className="btn btn-primary" data-tour="keys-create-btn">
            <Icon name="plus" size="md" className="mr-2" />
            {t('keys.createKey')}
          </button>
        </div>

        {selectedIds.length > 0 && (
          <div className="flex flex-wrap items-center gap-3 text-sm">
            <span className="surface-muted">{t('keys.bulkEdit.selectedCount', { count: selectedIds.length })}</span>
            <button
              className="btn btn-primary btn-sm"
              disabled={loading}
              data-test="bulk-edit-keys"
              onClick={() => setShowBulkEditModal(true)}
            >
              {t('keys.bulkEdit.title')}
            </button>
            <button className="btn btn-secondary btn-sm" onClick={() => setSelectedIds([])}>
              {t('keys.bulkEdit.clearSelection')}
            </button>
          </div>
        )}
        </div>
      }
      table={
        <DataTable
          className="workspace-table-viewport"
          columns={columns}
          data={apiKeys}
          loading={loading}
          selectable
          rowKey="id"
          selectedKeys={selectedIds}
          selectionLabel={(key) => t('keys.bulkEdit.selectKey', { name: (key as ApiKey).name })}
          onSelectedKeysChange={handleSelectionChange}
          serverSideSort
          defaultSortKey="created_at"
          defaultSortOrder="desc"
          onSort={handleSort}
          renderCell={(key, row, value) => renderCell(key, row as ApiKey, value)}
          renderEmpty={() => (
            <EmptyState
              title={t('keys.noKeysYet')}
              description={t('keys.createFirstKey')}
              action={
                <button className="btn btn-primary" onClick={() => setShowCreateModal(true)}>
                  {t('keys.createKey')}
                </button>
              }
            />
          )}
        />
      }
      pagination={
        <Pagination
          page={pagination.page}
          total={pagination.total}
          pageSize={pagination.page_size}
          onPageChange={handlePageChange}
          onPageSizeChange={(size) => {
            setPersistedPageSize(size)
            handlePageSizeChange(size)
          }}
        />
      }
    >

      {/* Create/Edit Modal */}
      <Modal
        open={showCreateModal || showEditModal}
        title={showEditModal ? t('keys.editKey') : t('keys.createKey')}
        onClose={closeModals}
        footer={
          <div className="flex justify-end gap-3">
            <button type="button" className="btn btn-secondary" onClick={closeModals}>
              {t('common.cancel')}
            </button>
            <button
              form="key-form"
              type="submit"
              disabled={submitting}
              className="btn btn-primary"
              data-tour="key-form-submit"
            >
              {submitting && (
                <svg className="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                  <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                  <path
                    className="opacity-75"
                    fill="currentColor"
                    d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                  />
                </svg>
              )}
              {submitting ? t('keys.saving') : showEditModal ? t('common.update') : t('common.create')}
            </button>
          </div>
        }
      >
        <form
          id="key-form"
          onSubmit={(event) => {
            event.preventDefault()
            void handleSubmit()
          }}
          className="space-y-5"
        >
          <div>
            <label className="input-label">{t('keys.nameLabel')}</label>
            <input
              value={formData.name}
              onChange={(event) => setFormData((previous) => ({ ...previous, name: event.target.value }))}
              type="text"
              required
              className="input"
              placeholder={t('keys.namePlaceholder')}
              data-tour="key-form-name"
            />
          </div>

          {!showEditModal && (
            <fieldset data-tour="key-form-provider">
              <legend className="input-label">{t('keys.providerLabel')}</legend>
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                {createProviderOptions.map((provider) => (
                  <label
                    key={provider.value}
                    className={`relative min-w-0 ${provider.count === 0 ? 'cursor-not-allowed' : 'cursor-pointer'}`}
                  >
                    <input
                      type="radio"
                      name="key-provider"
                      value={provider.value}
                      checked={createProvider === provider.value}
                      disabled={provider.count === 0}
                      className="peer sr-only"
                      onChange={() => selectCreateProvider(provider.value)}
                    />
                    <span
                      className={`flex h-full flex-col items-center gap-2 rounded-xl border border-gray-200 surface-inset px-2 py-3 text-center transition-colors peer-checked:border-primary-500 peer-checked:bg-primary-50/60 peer-checked:ring-1 peer-checked:ring-primary-500 peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-primary-500 peer-disabled:opacity-40 dark:border-dark-600 dark:peer-checked:bg-primary-500/10 ${
                        provider.count > 0 ? 'hover:border-primary-300 dark:hover:border-primary-700' : ''
                      }`}
                    >
                      <span className="flex h-8 items-center justify-center gap-1.5" aria-hidden="true">
                        {KEY_GROUP_PROVIDER_ICONS[provider.value].map((platform) => (
                          <span
                            key={platform}
                            className={`flex h-8 w-8 items-center justify-center rounded-lg ${platformBadgeLightClass(platform)}`}
                          >
                            <PlatformIcon platform={platform} size="lg" />
                          </span>
                        ))}
                      </span>
                      <span className="text-sm font-semibold text-gray-800 dark:text-gray-100">{provider.label}</span>
                    </span>
                    {createProvider === provider.value && (
                      <span
                        className="absolute right-1.5 top-1.5 flex h-4 w-4 items-center justify-center rounded-full bg-primary-500 text-white"
                        aria-hidden="true"
                      >
                        <Icon name="check" size="xs" strokeWidth={3} />
                      </span>
                    )}
                  </label>
                ))}
              </div>
              <p className="mt-2 text-xs leading-5 surface-muted" aria-live="polite">
                {groups.length === 0 ? t('common.noGroupsAvailable') : t(`keys.providerHints.${createProvider}`)}
              </p>
            </fieldset>
          )}

          <div>
            <label className="input-label" htmlFor="key-form-group">
              {t('keys.groupLabel')}
            </label>
            <Select
              key={showEditModal ? 'edit' : createProvider}
              id="key-form-group"
              aria-label={t('keys.groupLabel')}
              value={formData.group_id}
              options={formGroupOptions.map((group) => ({ ...group, label: group.label }))}
              placeholder={t('keys.selectGroup')}
              emptyText={t('common.noGroupsAvailable')}
              searchable
              searchPlaceholder={t('keys.searchGroup')}
              data-tour="key-form-group"
              onValueChange={(value) => setFormData((previous) => ({ ...previous, group_id: value === null ? null : Number(value) }))}
              renderSelected={(option) =>
                option ? (
                  <GroupBadge
                    name={(option as unknown as GroupOption).label}
                    showRate={false}
                    platform={(option as unknown as GroupOption).platform}
                    subscriptionType={(option as unknown as GroupOption).subscriptionType}
                    rateMultiplier={(option as unknown as GroupOption).rate}
                    userRateMultiplier={(option as unknown as GroupOption).userRate}
                    peakRateEnabled={(option as unknown as GroupOption).peakRateEnabled}
                    peakStart={(option as unknown as GroupOption).peakStart}
                    peakEnd={(option as unknown as GroupOption).peakEnd}
                    peakRateMultiplier={(option as unknown as GroupOption).peakRateMultiplier}
                  />
                ) : (
                  <span className="surface-muted">{t('keys.selectGroup')}</span>
                )
              }
              renderOption={(option) => {
                const group = option as unknown as GroupOption
                return (
                  <GroupOptionItem
                    name={group.label}
                    platform={group.platform}
                    subscriptionType={group.subscriptionType}
                    rateMultiplier={group.rate}
                    userRateMultiplier={group.userRate}
                    peakRateEnabled={group.peakRateEnabled}
                    peakStart={group.peakStart}
                    peakEnd={group.peakEnd}
                    peakRateMultiplier={group.peakRateMultiplier}
                    description={group.description}
                    selected={formData.group_id === group.value}
                    rates={
                      <GroupRateSummary
                        group={quoteFor(group.value)}
                        loading={quotesLoading}
                        failed={quotesFailed}
                        compact
                      />
                    }
                  />
                )
              }}
            />
            {formData.group_id && (
              <GroupRateSummary
                className="mt-3 rounded-lg surface-inset p-3"
                group={quoteFor(formData.group_id)}
                loading={quotesLoading}
                failed={quotesFailed}
                onRetry={() => void refreshQuotes()}
              />
            )}
          </div>

          {groups.find((group) => group.id === formData.group_id)?.platform === 'composite' && (
            <NativeAccessFields
              conversionEnabled={formData.conversion_enabled}
              groupId={formData.group_id}
              client={formData.default_client}
              endpoint={formData.default_endpoint}
              baseUrl={publicSettings?.api_base_url}
              onConversionEnabledChange={(value) => setFormData((previous) => ({ ...previous, conversion_enabled: value }))}
              onClientChange={(value) => setFormData((previous) => ({ ...previous, default_client: value }))}
              onEndpointChange={(value) => setFormData((previous) => ({ ...previous, default_endpoint: value }))}
            />
          )}

          {!showEditModal && (
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <label className="input-label mb-0">{t('keys.customKeyLabel')}</label>
                <input
                  type="checkbox"
                  className="checkbox"
                  role="switch"
                  checked={formData.use_custom_key}
                  onChange={(event) => setFormData((previous) => ({ ...previous, use_custom_key: event.target.checked }))}
                />
              </div>
              {formData.use_custom_key && (
                <div>
                  <input
                    value={formData.custom_key}
                    onChange={(event) => setFormData((previous) => ({ ...previous, custom_key: event.target.value }))}
                    type="text"
                    className={`input font-mono ${customKeyError ? 'border-red-500 dark:border-red-500' : ''}`}
                    placeholder={t('keys.customKeyPlaceholder')}
                  />
                  {customKeyError ? (
                    <p className="mt-1 text-sm text-red-500">{customKeyError}</p>
                  ) : (
                    <p className="input-hint">{t('keys.customKeyHint')}</p>
                  )}
                </div>
              )}
            </div>
          )}

          {showEditModal && (
            <div>
              <label className="input-label">{t('keys.statusLabel')}</label>
              <Select
                value={formData.status}
                options={statusOptions}
                placeholder={t('keys.selectStatus')}
                onValueChange={(value) =>
                  setFormData((previous) => ({ ...previous, status: value === 'inactive' ? 'inactive' : 'active' }))
                }
              />
            </div>
          )}

          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <label className="input-label mb-0">{t('keys.ipRestriction')}</label>
              <input
                type="checkbox"
                className="checkbox"
                role="switch"
                checked={formData.enable_ip_restriction}
                onChange={(event) =>
                  setFormData((previous) => ({ ...previous, enable_ip_restriction: event.target.checked }))
                }
              />
            </div>

            {formData.enable_ip_restriction && (
              <div className="space-y-4 pt-2">
                <div>
                  <label className="input-label">{t('keys.ipWhitelist')}</label>
                  <textarea
                    value={formData.ip_whitelist}
                    onChange={(event) => setFormData((previous) => ({ ...previous, ip_whitelist: event.target.value }))}
                    rows={3}
                    className="input font-mono text-sm"
                    placeholder={t('keys.ipWhitelistPlaceholder')}
                  />
                  <p className="input-hint">{t('keys.ipWhitelistHint')}</p>
                </div>
                <div>
                  <label className="input-label">{t('keys.ipBlacklist')}</label>
                  <textarea
                    value={formData.ip_blacklist}
                    onChange={(event) => setFormData((previous) => ({ ...previous, ip_blacklist: event.target.value }))}
                    rows={3}
                    className="input font-mono text-sm"
                    placeholder={t('keys.ipBlacklistPlaceholder')}
                  />
                  <p className="input-hint">{t('keys.ipBlacklistHint')}</p>
                </div>
              </div>
            )}
          </div>

          <div className="space-y-3">
            <label className="input-label">{t('keys.quotaLimit')}</label>
            <div className="space-y-4">
              <div>
                <div className="relative">
                  <span className="absolute left-3 top-1/2 -translate-y-1/2 surface-muted">$</span>
                  <input
                    value={formData.quota ?? ''}
                    onChange={(event) =>
                      setFormData((previous) => ({
                        ...previous,
                        quota: event.target.value === '' ? null : Number(event.target.value)
                      }))
                    }
                    type="number"
                    step="0.01"
                    min="0"
                    className="input pl-7"
                    placeholder={t('keys.quotaAmountPlaceholder')}
                  />
                </div>
                <p className="input-hint">{t('keys.quotaAmountHint')}</p>
              </div>

              {showEditModal && selectedKey && selectedKey.quota > 0 && (
                <div>
                  <label className="input-label">{t('keys.quotaUsed')}</label>
                  <div className="flex items-center gap-2">
                    <div className="surface-inset flex-1 rounded-lg px-3 py-2">
                      <span className="font-medium text-gray-900 dark:text-white">
                        ${selectedKey.quota_used?.toFixed(4) || '0.0000'}
                      </span>
                      <span className="mx-2 surface-muted">/</span>
                      <span className="surface-muted">${selectedKey.quota?.toFixed(2) || '0.00'}</span>
                    </div>
                    <button
                      type="button"
                      onClick={confirmResetQuota}
                      className="btn btn-secondary text-sm"
                      title={t('keys.resetQuotaUsed')}
                    >
                      {t('keys.reset')}
                    </button>
                  </div>
                </div>
              )}
            </div>
          </div>

          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <label className="input-label mb-0">{t('keys.rateLimitSection')}</label>
              <input
                type="checkbox"
                className="checkbox"
                role="switch"
                checked={formData.enable_rate_limit}
                onChange={(event) => setFormData((previous) => ({ ...previous, enable_rate_limit: event.target.checked }))}
              />
            </div>

            {formData.enable_rate_limit && (
              <div className="space-y-4 pt-2">
                <p className="input-hint -mt-2">{t('keys.rateLimitHint')}</p>
                {(['rate_limit_5h', 'rate_limit_1d', 'rate_limit_7d'] as const).map((field, index) => {
                  const labels = [t('keys.rateLimit5h'), t('keys.rateLimit1d'), t('keys.rateLimit7d')]
                  const usageKeys = ['usage_5h', 'usage_1d', 'usage_7d'] as const
                  return (
                    <div key={field}>
                      <label className="input-label">{labels[index]}</label>
                      <div className="relative">
                        <span className="absolute left-3 top-1/2 -translate-y-1/2 surface-muted">$</span>
                        <input
                          value={formData[field] ?? ''}
                          onChange={(event) =>
                            setFormData((previous) => ({
                              ...previous,
                              [field]: event.target.value === '' ? null : Number(event.target.value)
                            }))
                          }
                          type="number"
                          step="0.01"
                          min="0"
                          className="input pl-7"
                          placeholder="0"
                        />
                      </div>
                      {showEditModal && selectedKey && selectedKey[field] > 0 && (
                        <div className="mt-2">
                          <div className="flex items-center gap-2">
                            <div className="surface-inset flex-1 rounded-lg px-3 py-2 text-sm">
                              <span
                                className={`font-medium ${
                                  selectedKey[usageKeys[index]] >= selectedKey[field]
                                    ? 'text-red-500'
                                    : selectedKey[usageKeys[index]] >= selectedKey[field] * 0.8
                                      ? 'text-yellow-500'
                                      : 'text-gray-900 dark:text-white'
                                }`}
                              >
                                ${selectedKey[usageKeys[index]]?.toFixed(4) || '0.0000'}
                              </span>
                              <span className="mx-2 surface-muted">/</span>
                              <span className="surface-muted">${selectedKey[field]?.toFixed(2) || '0.00'}</span>
                            </div>
                          </div>
                          <div className="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                            <div
                              className={`console-data-mark h-full rounded-full transition-all ${
                                selectedKey[usageKeys[index]] >= selectedKey[field]
                                  ? 'bg-red-500'
                                  : selectedKey[usageKeys[index]] >= selectedKey[field] * 0.8
                                    ? 'bg-yellow-500'
                                    : 'bg-green-500'
                              }`}
                              style={{
                                width: Math.min((selectedKey[usageKeys[index]] / selectedKey[field]) * 100, 100) + '%'
                              }}
                            />
                          </div>
                        </div>
                      )}
                    </div>
                  )
                })}
                {showEditModal &&
                  selectedKey &&
                  (selectedKey.rate_limit_5h > 0 || selectedKey.rate_limit_1d > 0 || selectedKey.rate_limit_7d > 0) && (
                    <div>
                      <button type="button" onClick={confirmResetRateLimit} className="btn btn-secondary text-sm">
                        {t('keys.resetRateLimitUsage')}
                      </button>
                    </div>
                  )}
              </div>
            )}
          </div>

          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <label className="input-label mb-0">{t('keys.expiration')}</label>
              <input
                type="checkbox"
                className="checkbox"
                role="switch"
                checked={formData.enable_expiration}
                onChange={(event) => setFormData((previous) => ({ ...previous, enable_expiration: event.target.checked }))}
              />
            </div>

            {formData.enable_expiration && (
              <div className="space-y-4 pt-2">
                <div className="flex flex-wrap gap-2">
                  {['7', '30', '90'].map((days) => (
                    <button
                      key={days}
                      type="button"
                      onClick={() => setExpirationDays(parseInt(days))}
                      className={`rounded-lg px-3 py-1.5 text-sm transition-colors ${
                        formData.expiration_preset === days
                          ? 'bg-primary-100/[0.15] text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                          : 'surface-inset text-gray-600 hover:surface-hover dark:surface-inset dark:text-gray-400 dark:hover:surface-hover'
                      }`}
                    >
                      {showEditModal ? t('keys.extendDays', { days }) : t('keys.expiresInDays', { days })}
                    </button>
                  ))}
                  <button
                    type="button"
                    onClick={() => setFormData((previous) => ({ ...previous, expiration_preset: 'custom' }))}
                    className={`rounded-lg px-3 py-1.5 text-sm transition-colors ${
                      formData.expiration_preset === 'custom'
                        ? 'bg-primary-100/[0.15] text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                        : 'surface-inset text-gray-600 hover:surface-hover dark:surface-inset dark:text-gray-400 dark:hover:surface-hover'
                    }`}
                  >
                    {t('keys.customDate')}
                  </button>
                </div>

                <div>
                  <label className="input-label">{t('keys.expirationDate')}</label>
                  <DateTimePicker
                    aria-label={t('keys.expirationDate')}
                    value={formData.expiration_date}
                    onValueChange={(value) => setFormData((previous) => ({ ...previous, expiration_date: value }))}
                    type="datetime-local"
                    className="input"
                  />
                  <p className="input-hint">{t('keys.expirationDateHint')}</p>
                </div>

                {showEditModal && selectedKey?.expires_at && (
                  <div className="text-sm">
                    <span className="surface-muted">{t('keys.currentExpiration')}: </span>
                    <span className="font-medium text-gray-900 dark:text-white">
                      {formatDateTime(selectedKey.expires_at)}
                    </span>
                  </div>
                )}
              </div>
            )}
          </div>
        </form>
      </Modal>

      <BulkEditKeysModal
        show={showBulkEditModal}
        selectedKeys={selectedApiKeys.map((key) => ({ id: key.id, name: key.name }))}
        groups={groups}
        quotedGroups={quotedGroups}
        quotesLoading={quotesLoading}
        quotesFailed={quotesFailed}
        onClose={() => setShowBulkEditModal(false)}
        onUpdated={handleBulkUpdated}
        onRetryQuotes={() => void refreshQuotes()}
      />

      <ConfirmDialog
        open={showDeleteDialog}
        title={t('keys.deleteKey')}
        message={t('keys.deleteConfirmMessage', { name: selectedKey?.name })}
        confirmText={t('common.delete')}
        cancelText={t('common.cancel')}
        variant="danger"
        onConfirm={() => void handleDelete()}
        onCancel={() => setShowDeleteDialog(false)}
      />

      <ConfirmDialog
        open={showResetQuotaDialog}
        title={t('keys.resetQuotaTitle')}
        message={t('keys.resetQuotaConfirmMessage', {
          name: selectedKey?.name,
          used: selectedKey?.quota_used?.toFixed(4)
        })}
        confirmText={t('keys.reset')}
        cancelText={t('common.cancel')}
        variant="danger"
        onConfirm={() => void resetQuotaUsed()}
        onCancel={() => setShowResetQuotaDialog(false)}
      />

      <ConfirmDialog
        open={showResetRateLimitDialog}
        title={t('keys.resetRateLimitTitle')}
        message={t('keys.resetRateLimitConfirmMessage', { name: selectedKey?.name })}
        confirmText={t('keys.reset')}
        cancelText={t('common.cancel')}
        variant="danger"
        onConfirm={() => void resetRateLimitUsage()}
        onCancel={() => setShowResetRateLimitDialog(false)}
      />

      <UseKeyModal
        show={showUseKeyModal}
        apiKey={selectedKey?.key || ''}
        baseUrl={publicSettings?.api_base_url || ''}
        platform={selectedKey?.group?.platform || null}
        groupId={selectedKey?.group_id || null}
        conversionEnabled={selectedKey?.conversion_enabled || false}
        defaultClient={selectedKey?.default_client || ''}
        defaultEndpoint={selectedKey?.default_endpoint || ''}
        claudeCodeOnly={selectedKey?.group?.claude_code_only || false}
        allowMessagesDispatch={selectedKey?.group?.allow_messages_dispatch || false}
        onClose={() => {
          setShowUseKeyModal(false)
          setSelectedKey(null)
        }}
      />

      <Modal
        open={showCcsClientSelect}
        title={t('keys.ccsClientSelect.title')}
        onClose={() => {
          setShowCcsClientSelect(false)
          setPendingCcsRow(null)
        }}
        footer={
          <div className="flex justify-end">
            <button className="btn btn-secondary" onClick={() => {
              setShowCcsClientSelect(false)
              setPendingCcsRow(null)
            }}>
              {t('common.cancel')}
            </button>
          </div>
        }
      >
        <div className="space-y-4">
          <p className="text-sm surface-muted">{t('keys.ccsClientSelect.description')}</p>
          <div className="grid grid-cols-2 gap-3">
            <button
              onClick={() => handleCcsClientSelect('claude')}
              className="surface-control flex flex-col items-center gap-2 rounded-xl border-2 border-gray-200 p-4 transition-all hover:border-primary-500 hover:bg-primary-50/25 dark:border-dark-600 dark:hover:bg-primary-900/20"
            >
              <Icon name="terminal" size="xl" className="surface-muted" />
              <span className="font-medium text-gray-900 dark:text-white">{t('keys.ccsClientSelect.claudeCode')}</span>
              <span className="text-xs surface-muted">{t('keys.ccsClientSelect.claudeCodeDesc')}</span>
            </button>
            <button
              onClick={() => handleCcsClientSelect('gemini')}
              className="surface-control flex flex-col items-center gap-2 rounded-xl border-2 border-gray-200 p-4 transition-all hover:border-primary-500 hover:bg-primary-50/25 dark:border-dark-600 dark:hover:bg-primary-900/20"
            >
              <Icon name="sparkles" size="xl" className="surface-muted" />
              <span className="font-medium text-gray-900 dark:text-white">{t('keys.ccsClientSelect.geminiCli')}</span>
              <span className="text-xs surface-muted">{t('keys.ccsClientSelect.geminiCliDesc')}</span>
            </button>
          </div>
        </div>
      </Modal>

      {/* Inline group selector */}
      {groupSelectorKeyId !== null && dropdownPosition && (
        <div
          data-test="key-inline-group-selector"
          ref={dropdownRef}
          className="surface-panel animate-in fade-in slide-in-from-top-2 fixed z-[100000020] w-max max-w-[calc(100vw-16px)] overflow-hidden rounded-xl shadow-lg ring-1 ring-black/5 duration-200 sm:min-w-[380px] dark:ring-white/10"
          style={{
            pointerEvents: 'auto',
            top: dropdownPosition.top !== undefined ? `${dropdownPosition.top}px` : undefined,
            bottom: dropdownPosition.bottom !== undefined ? `${dropdownPosition.bottom}px` : undefined,
            left: `${dropdownPosition.left}px`
          }}
        >
          <div className="border-b border-gray-100 p-2 dark:border-dark-700">
            <div className="relative">
              <svg className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="2">
                <path strokeLinecap="round" strokeLinejoin="round" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
              </svg>
              <input
                value={groupSearchQuery}
                onChange={(event) => setGroupSearchQuery(event.target.value)}
                type="text"
                className="surface-inset w-full rounded-lg border border-gray-200 py-1.5 pl-8 pr-3 text-sm text-gray-900 placeholder-gray-400 outline-none focus:border-primary-300 focus:ring-1 focus:ring-primary-300 dark:border-dark-600 dark:text-white dark:placeholder-gray-500 dark:focus:border-primary-600 dark:focus:ring-primary-600"
                placeholder={t('keys.searchGroup')}
                onClick={(event) => event.stopPropagation()}
              />
            </div>
          </div>
          <div className="max-h-80 overflow-y-auto p-1.5">
            {filteredGroupOptions.map((option) => {
              const isSelected =
                selectedKeyForGroup?.group_id === option.value ||
                (!selectedKeyForGroup?.group_id && option.value === null)
              return (
                <button
                  key={option.value ?? 'null'}
                  className={`flex w-full items-center justify-between rounded-lg border-b border-gray-100 px-3 py-2.5 text-left text-sm transition-colors last:border-0 dark:border-dark-700 ${
                    isSelected ? 'bg-primary-50/[0.15] dark:bg-primary-900/20' : 'hover:surface-hover dark:hover:surface-hover'
                  }`}
                  title={option.description || undefined}
                  onClick={() => moreMenuJob && void changeGroup(moreMenuJob, option.value)}
                >
                  <GroupOptionItem
                    name={option.label}
                    platform={option.platform}
                    subscriptionType={option.subscriptionType}
                    rateMultiplier={option.rate}
                    userRateMultiplier={option.userRate}
                    peakRateEnabled={option.peakRateEnabled}
                    peakStart={option.peakStart}
                    peakEnd={option.peakEnd}
                    peakRateMultiplier={option.peakRateMultiplier}
                    description={option.description}
                    selected={isSelected}
                    rates={
                      <GroupRateSummary group={quoteFor(option.value)} loading={quotesLoading} failed={quotesFailed} compact />
                    }
                  />
                </button>
              )
            })}
            {filteredGroupOptions.length === 0 && (
              <div className="py-4 text-center text-sm surface-muted">{t('keys.noGroupFound')}</div>
            )}
            {quotesFailed && (
              <button type="button" className="btn btn-secondary btn-sm m-2" onClick={(event) => {
                event.stopPropagation()
                void refreshQuotes()
              }}>
                {t('billingRates.retry')}
              </button>
            )}
          </div>
        </div>
      )}
    </TablePageLayout>
  )
}

