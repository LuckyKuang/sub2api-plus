import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Pencil, Plus, RefreshCw, Search, Trash2 } from 'lucide-react'
import { extractApiErrorMessage } from '@/utils/apiError'
import { adminAPI } from '@/api/admin'
import channelsAPI from '@/api/admin/channels'
import type {
  AccountStatsPricingRule,
  Channel,
  ChannelModelPricing,
  CreateChannelRequest,
  UpdateChannelRequest
} from '@/api/admin/channels'
import type { AdminGroup, GroupPlatform } from '@/types'
import { platformBadgeLightClass, platformLabel as catalogPlatformLabel, platformTextClass } from '@/utils/platformColors'
import { listPlatforms } from '@/utils/platformCatalog'
import { useAppStore } from '@/stores/app'
import { Button, EmptyState, Input, Modal, Pagination, Select, Toggle } from '@/components/ui'
import { AdminDataTable, type Column } from './AdminDataTable'
import AdminTablePageLayout from './AdminTablePageLayout'
import { PricingEntryCard } from './ChannelPricingEntryCard'
import { GroupModelRateLink } from './GroupModelRateLink'
import {
  apiIntervalsToForm,
  apiTimePricingToForm,
  buildSyncedPricingEntries,
  createDefaultTimePricingForm,
  filterAlreadyCoveredModels,
  findModelConflict,
  formIntervalsToAPI,
  formReasoningEffortMultipliersToAPI,
  formTimePricingToAPI,
  isValidPositiveMultiplier,
  mTokToPerToken,
  perTokenToMTok,
  referenceToPricingRule,
  validateIntervals,
  validateReasoningEffortMultipliers,
  validateTimePricing,
  type PricingFormEntry
} from './channelTypes'

// Channel-level Web Search emulation controls. Rendering and submission share
// this list; account-level overrides retain their provider-specific contract.
const WEB_SEARCH_EMULATION_PLATFORMS: ReadonlySet<string> = new Set([
  'anthropic',
  'openai',
  'command_code',
  'opencode_go',
  'minimax',
  'zhipu'
])

// ── Form-level pricing rule type (per-platform) ──
interface FormPricingRule {
  name: string
  group_ids: number[]
  account_ids: number[]
  pricing: PricingFormEntry[]
}

// ── Platform Section type ──
interface PlatformSection {
  platform: GroupPlatform
  enabled: boolean
  collapsed: boolean
  group_ids: number[]
  model_mapping: Record<string, string>
  model_pricing: PricingFormEntry[]
  web_search_emulation: boolean
  codex_image_generation_bridge: boolean
  bedrock_cc_compat: boolean
  account_stats_pricing_rules: FormPricingRule[]
}

interface SimpleAccount {
  id: number
  name: string
  platform: string
}

interface ChannelFormState {
  name: string
  description: string
  status: string
  restrict_models: boolean
  billing_model_source: string
  platforms: PlatformSection[]
  apply_pricing_to_account_stats: boolean
}

const EMPTY_FORM: ChannelFormState = {
  name: '',
  description: '',
  status: 'active',
  restrict_models: false,
  billing_model_source: 'channel_mapped',
  platforms: [],
  apply_pricing_to_account_stats: false
}

export default function ChannelsView() {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)
  const showWarning = useAppStore((state) => state.showWarning)

  const [channels, setChannels] = useState<Channel[]>([])
  const [loading, setLoading] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [pagination, setPagination] = useState({ page: 1, page_size: 20, total: 0 })
  const [sortState, setSortState] = useState({ sort_by: 'created_at', sort_order: 'desc' as 'asc' | 'desc' })

  const [showDialog, setShowDialog] = useState(false)
  const [editingChannel, setEditingChannel] = useState<Channel | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [deletingChannel, setDeletingChannel] = useState<Channel | null>(null)
  const [activeTab, setActiveTab] = useState<string>('basic')

  const [allGroups, setAllGroups] = useState<AdminGroup[]>([])
  const [groupsLoading, setGroupsLoading] = useState(false)

  // All channels for group-conflict detection (independent of current page)
  const [allChannelsForConflict, setAllChannelsForConflict] = useState<Channel[]>([])

  const [form, setForm] = useState<ChannelFormState>(EMPTY_FORM)
  const [compositeSupplier, setCompositeSupplier] = useState<GroupPlatform>('openai')

  // Web Search global enabled state (loaded once on mount)
  const [webSearchGlobalEnabled, setWebSearchGlobalEnabled] = useState(false)

  const abortController = useRef<AbortController | null>(null)
  const searchTimeout = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [syncingPlatform, setSyncingPlatform] = useState<string | null>(null)

  // Account search for pricing rules
  const [ruleAccountSearchKeyword, setRuleAccountSearchKeyword] = useState<Record<string, string>>({})
  const [ruleAccountSearchResults, setRuleAccountSearchResults] = useState<Record<string, SimpleAccount[]>>({})
  const [showRuleAccountDropdown, setShowRuleAccountDropdown] = useState<Record<string, boolean>>({})
  // Cache: account ID → name, populated when search results are selected
  const ruleAccountNameCache = useRef<Record<number, string>>({})
  const ruleAccountSearchTimers = useRef<Record<string, ReturnType<typeof setTimeout>>>({})
  const ruleAccountSearchAborts = useRef<Record<string, AbortController>>({})

  const platformOrder = useMemo<GroupPlatform[]>(() => ['composite', ...listPlatforms().map((spec) => spec.id)] as GroupPlatform[], [])
  const supplierOptions = useMemo(() => listPlatforms().map((spec) => ({ value: spec.id, label: catalogPlatformLabel(spec.id) })), [])

  const compositeSection = form.platforms.find((section) => section.enabled && section.platform === 'composite')
  const compositeOnly = !!compositeSection && !form.platforms.some((section) => section.enabled && section.platform !== 'composite' && section.group_ids.length > 0)
  const activePlatforms = form.platforms.filter((s) => s.enabled).map((s) => s.platform)

  const columns: Column[] = useMemo(
    () => [
      { key: 'name', label: t('admin.channels.columns.name', 'Name'), sortable: true },
      { key: 'description', label: t('admin.channels.columns.description', 'Description'), sortable: false },
      { key: 'status', label: t('admin.channels.columns.status', 'Status'), sortable: true },
      { key: 'group_count', label: t('admin.channels.columns.groups', 'Groups'), sortable: false },
      { key: 'pricing_count', label: t('admin.channels.columns.pricing', 'Pricing'), sortable: false },
      { key: 'created_at', label: t('admin.channels.columns.createdAt', 'Created'), sortable: true },
      { key: 'actions', label: t('admin.channels.columns.actions', 'Actions'), sortable: false }
    ],
    [t]
  )

  const statusFilterOptions = useMemo(
    () => [
      { value: '', label: t('admin.channels.allStatus', 'All Status') },
      { value: 'active', label: t('admin.channels.statusActive', 'Active') },
      { value: 'disabled', label: t('admin.channels.statusDisabled', 'Disabled') }
    ],
    [t]
  )

  const statusEditOptions = useMemo(
    () => [
      { value: 'active', label: t('admin.channels.statusActive', 'Active') },
      { value: 'disabled', label: t('admin.channels.statusDisabled', 'Disabled') }
    ],
    [t]
  )

  const billingModelSourceOptions = useMemo(
    () => [
      { value: 'channel_mapped', label: t('admin.channels.form.billingModelSourceChannelMapped', 'Bill by channel-mapped model') },
      { value: 'requested', label: t('admin.channels.form.billingModelSourceRequested', 'Bill by requested model') },
      { value: 'upstream', label: t('admin.channels.form.billingModelSourceUpstream', 'Bill by final upstream model') },
      { value: 'response_model', label: t('admin.channels.form.billingModelSourceResponse', 'Bill by upstream response model') }
    ],
    [t]
  )

  const deleteConfirmMessage = deletingChannel?.name
    ? t('admin.channels.deleteConfirm', {
        name: deletingChannel.name,
        defaultValue: `Are you sure you want to delete channel "${deletingChannel.name}"? This action cannot be undone.`
      })
    : ''

  // Latest-state refs so every entry point shares one load path.
  const queryRef = useRef({ pagination, statusFilter, searchQuery, sortState })
  queryRef.current = { pagination, statusFilter, searchQuery, sortState }

  const loadChannels = useCallback(async () => {
    if (abortController.current) abortController.current.abort()
    const ctrl = new AbortController()
    abortController.current = ctrl
    const { pagination: page, statusFilter: status, searchQuery: search, sortState: sort } = queryRef.current
    setLoading(true)

    try {
      const response = await adminAPI.channels.list(
        page.page,
        page.page_size,
        {
          status: status || undefined,
          search: search || undefined,
          sort_by: sort.sort_by,
          sort_order: sort.sort_order
        },
        { signal: ctrl.signal }
      )

      if (ctrl.signal.aborted || abortController.current !== ctrl) return
      setChannels(response.items || [])
      setPagination((prev) => ({ ...prev, total: response.total }))
    } catch (error) {
      const e = error as { name?: string; code?: string }
      if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
      showError(extractApiErrorMessage(error, t('admin.channels.loadError', 'Failed to load channels')))
    } finally {
      if (abortController.current === ctrl) {
        setLoading(false)
        abortController.current = null
      }
    }
  }, [showError, t])

  const loadGroups = useCallback(async () => {
    setGroupsLoading(true)
    try {
      setAllGroups(await adminAPI.groups.getAll())
    } catch (error) {
      console.error('Error loading groups:', error)
    } finally {
      setGroupsLoading(false)
    }
  }, [])

  const loadAllChannelsForConflict = useCallback(async () => {
    try {
      const response = await adminAPI.channels.list(1, 1000)
      setAllChannelsForConflict(response.items || [])
    } catch {
      // Fallback to current page data
      setAllChannelsForConflict(channels)
    }
  }, [channels])

  const loadWebSearchGlobalState = useCallback(async () => {
    try {
      const cfg = await adminAPI.settings.getWebSearchEmulationConfig()
      setWebSearchGlobalEnabled(cfg?.enabled === true && (cfg?.providers?.length ?? 0) > 0)
    } catch (err: unknown) {
      console.warn('Failed to load web search global state:', err)
      setWebSearchGlobalEnabled(false)
    }
  }, [])

  const [reloadKey, setReloadKey] = useState(0)
  useEffect(() => {
    void loadChannels()
  }, [loadChannels, reloadKey])

  useEffect(() => {
    void loadGroups()
    void loadWebSearchGlobalState()
  }, [loadGroups, loadWebSearchGlobalState])

  useEffect(() => {
    const handleRuleAccountClickOutside = (event: MouseEvent) => {
      const target = event.target as HTMLElement
      if (!target.closest('.rule-account-search-container')) {
        setShowRuleAccountDropdown((prev) => {
          const next: Record<string, boolean> = {}
          for (const key of Object.keys(prev)) next[key] = false
          return next
        })
      }
    }
    document.addEventListener('click', handleRuleAccountClickOutside)
    return () => document.removeEventListener('click', handleRuleAccountClickOutside)
  }, [])

  useEffect(() => {
    const timers = ruleAccountSearchTimers.current
    const aborts = ruleAccountSearchAborts.current
    return () => {
      if (searchTimeout.current) clearTimeout(searchTimeout.current)
      abortController.current?.abort()
      for (const timer of Object.values(timers)) clearTimeout(timer)
      for (const controller of Object.values(aborts)) controller.abort()
    }
  }, [])

  const formatDate = (value: string): string => {
    if (!value) return '-'
    return new Date(value).toLocaleDateString()
  }

  // ── Platform section helpers ──
  const addPlatformSection = (platform: GroupPlatform) => {
    setForm((prev) => ({
      ...prev,
      platforms: [
        ...prev.platforms,
        {
          platform,
          enabled: true,
          collapsed: false,
          group_ids: [],
          model_mapping: {},
          model_pricing: [],
          web_search_emulation: false,
          codex_image_generation_bridge: false,
          bedrock_cc_compat: false,
          account_stats_pricing_rules: []
        }
      ]
    }))
  }

  const selectCompositeSupplier = (value: string | number | boolean | null) => {
    if (!value || !listPlatforms().some((spec) => spec.id === value)) return
    const platform = value as GroupPlatform
    setCompositeSupplier(platform)
    if (!form.platforms.some((section) => section.platform === platform)) addPlatformSection(platform)
    else {
      setForm((prev) => ({
        ...prev,
        platforms: prev.platforms.map((section) => (section.platform === platform ? { ...section, enabled: true } : section))
      }))
    }
  }

  const togglePlatform = (platform: GroupPlatform) => {
    const section = form.platforms.find((s) => s.platform === platform)
    if (section) {
      setForm((prev) => ({
        ...prev,
        platforms: prev.platforms.map((entry) => (entry.platform === platform ? { ...entry, enabled: !entry.enabled } : entry))
      }))
      if (!section.enabled && activeTab === platform) setActiveTab('basic')
    } else {
      addPlatformSection(platform)
    }
    if (platform === 'composite' && compositeSection) selectCompositeSupplier(compositeSupplier)
  }

  const getGroupsForPlatform = (platform: GroupPlatform): AdminGroup[] => allGroups.filter((g) => g.platform === platform)

  const sectionGroupIds = (section: PlatformSection): number[] => [
    ...new Set([...section.group_ids, ...(compositeSection?.group_ids ?? [])])
  ]

  // ── Group helpers ──
  const groupToChannelMap = useMemo(() => {
    const map = new Map<number, Channel>()
    for (const ch of allChannelsForConflict) {
      if (editingChannel && ch.id === editingChannel.id) continue
      for (const gid of ch.group_ids || []) {
        map.set(gid, ch)
      }
    }
    return map
  }, [allChannelsForConflict, editingChannel])

  const isGroupInOtherChannel = (groupId: number): boolean => groupToChannelMap.has(groupId)

  const getGroupChannelName = (groupId: number): string => groupToChannelMap.get(groupId)?.name || ''

  const getGroupInOtherChannelLabel = (groupId: number): string => {
    const name = getGroupChannelName(groupId)
    return t('admin.channels.form.inOtherChannel', { name, defaultValue: `In "${name}"` })
  }

  const toggleGroupInSection = (sectionIdx: number, groupId: number) => {
    setForm((prev) => ({
      ...prev,
      platforms: prev.platforms.map((section, idx) => {
        if (idx !== sectionIdx) return section
        const at = section.group_ids.indexOf(groupId)
        const group_ids = at >= 0 ? section.group_ids.filter((id) => id !== groupId) : [...section.group_ids, groupId]
        return { ...section, group_ids }
      })
    }))
  }

  // ── Pricing helpers ──
  const emptyPricingEntry = (): PricingFormEntry => ({
    models: [],
    billing_mode: 'token',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_write_1h_price: null,
    cache_read_price: null,
    fast_multiplier: null,
    flex_multiplier: null,
    reasoning_effort_multipliers: null,
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
    time_pricing: createDefaultTimePricingForm()
  })

  const addPricingEntry = (sectionIdx: number) => {
    setForm((prev) => ({
      ...prev,
      platforms: prev.platforms.map((section, idx) =>
        idx === sectionIdx ? { ...section, model_pricing: [...section.model_pricing, emptyPricingEntry()] } : section
      )
    }))
  }

  const syncLatestModels = async (sectionIdx: number) => {
    const section = form.platforms[sectionIdx]
    if (!section || syncingPlatform || !listPlatforms().some((spec) => spec.id === section.platform)) return
    const platform = section.platform
    setSyncingPlatform(platform)
    try {
      const result = await adminAPI.channels.syncPricingModels(platform)
      // 只新建本平台还没有覆盖的型号：精确匹配与通配符规则都要算，否则重复同步
      // 或已有 `claude-*` 规则时会建出重复条目。
      const newModels = filterAlreadyCoveredModels(
        result.models.map((ref) => ref.model),
        form.platforms[sectionIdx].model_pricing
      )
      if (newModels.length === 0) {
        showSuccess(t('admin.channels.form.syncModelsAlreadyUpToDate'))
        return
      }

      const newModelSet = new Set(newModels)
      // 每个模型一条独立规则，各自带自己的官方价（同步每次都会新鲜返回）。
      const rules = buildSyncedPricingEntries(result.models.filter((ref) => newModelSet.has(ref.model)))
      setForm((prev) => ({
        ...prev,
        platforms: prev.platforms.map((entry, idx) =>
          idx === sectionIdx
            ? { ...entry, model_pricing: [...entry.model_pricing, ...rules.map((rule) => rule.entry)] }
            : entry
        )
      }))

      const priced = rules.filter((rule) => rule.reference.status === 'priced').length
      const needsPricing = rules.length - priced
      if (needsPricing > 0) {
        showSuccess(t('admin.channels.form.syncModelsPartial', { count: newModels.length, priced, needsPricing }))
      } else {
        showSuccess(t('admin.channels.form.syncModelsSuccess', { count: newModels.length }))
      }
      if (result.refresh_status === 'stale' || result.warning_code) {
        showWarning(t('admin.channels.form.syncModelsStaleCatalog'))
      }
    } catch (error) {
      showError(extractApiErrorMessage(error, t('admin.channels.form.syncModelsError')))
    } finally {
      setSyncingPlatform(null)
    }
  }

  const updatePricingEntry = (sectionIdx: number, idx: number, updated: PricingFormEntry) => {
    setForm((prev) => ({
      ...prev,
      platforms: prev.platforms.map((section, sIdx) =>
        sIdx !== sectionIdx
          ? section
          : { ...section, model_pricing: section.model_pricing.map((entry, i) => (i === idx ? updated : entry)) }
      )
    }))
  }

  const removePricingEntry = (sectionIdx: number, idx: number) => {
    setForm((prev) => ({
      ...prev,
      platforms: prev.platforms.map((section, sIdx) =>
        sIdx !== sectionIdx ? section : { ...section, model_pricing: section.model_pricing.filter((_, i) => i !== idx) }
      )
    }))
  }

  /**
   * 一次粘贴多个模型时，把多出来的模型拆成独立规则。
   * 不拆分就只能给它们填同一套价（一条规则本来就是一套价）。
   * 拆分出的每个模型都要独立查一次参考价：早期实现只查第一个型号，
   * 其余型号拿到的是第一条规则的空价或第一个型号的价。
   */
  const splitPricingEntry = async (sectionIdx: number, idx: number, models: string[]) => {
    const entries = form.platforms[sectionIdx].model_pricing
    const source = entries[idx]
    if (!source || !models.length) return
    const platform = form.platforms[sectionIdx].platform
    const additions = await Promise.all(models.map((model) => resolveSplitEntry(platform, model)))
    setForm((prev) => ({
      ...prev,
      platforms: prev.platforms.map((section, sIdx) => {
        if (sIdx !== sectionIdx) return section
        const next = [...section.model_pricing]
        next.splice(idx + 1, 0, ...additions)
        return { ...section, model_pricing: next }
      })
    }))
  }

  const splitRulePricingEntry = async (platform: string, ruleIndex: number, sectionIdx: number, pIdx: number, models: string[]) => {
    const rule = form.platforms[sectionIdx]?.account_stats_pricing_rules?.[ruleIndex]
    const source = rule?.pricing[pIdx]
    if (!rule || !source || !models.length) return
    const additions = await Promise.all(models.map((model) => resolveSplitEntry(platform, model)))
    setForm((prev) => ({
      ...prev,
      platforms: prev.platforms.map((section, sIdx) => {
        if (sIdx !== sectionIdx) return section
        return {
          ...section,
          account_stats_pricing_rules: section.account_stats_pricing_rules.map((entry, rIdx) => {
            if (rIdx !== ruleIndex) return entry
            const next = [...entry.pricing]
            next.splice(pIdx + 1, 0, ...additions)
            return { ...entry, pricing: next }
          })
        }
      })
    }))
  }

  /**
   * 为拆分出来的单个模型查参考价并生成独立规则。
   * manual/unsupported：保留空规则但带上参考价元数据，卡片上显示原因；
   * 查询失败：同样留空，由运营者手填（不阻断拆分本身）。
   */
  const resolveSplitEntry = async (platform: string, model: string): Promise<PricingFormEntry> => {
    const base = emptyPricingEntry()
    base.models = [model]
    if (!platform) return base
    try {
      const reference = await channelsAPI.getModelDefaultPricing(platform, model)
      const { entry } = referenceToPricingRule(reference)
      return entry
    } catch {
      return base
    }
  }

  // ── Model Mapping helpers ──
  const addMappingEntry = (sectionIdx: number) => {
    setForm((prev) => ({
      ...prev,
      platforms: prev.platforms.map((section, idx) => {
        if (idx !== sectionIdx) return section
        const mapping = { ...section.model_mapping }
        let key = ''
        let i = 1
        while (key === '' || key in mapping) {
          key = `model-${i}`
          i++
        }
        mapping[key] = ''
        return { ...section, model_mapping: mapping }
      })
    }))
  }

  const removeMappingEntry = (sectionIdx: number, key: string) => {
    setForm((prev) => ({
      ...prev,
      platforms: prev.platforms.map((section, idx) => {
        if (idx !== sectionIdx) return section
        const mapping = { ...section.model_mapping }
        delete mapping[key]
        return { ...section, model_mapping: mapping }
      })
    }))
  }

  const renameMappingKey = (sectionIdx: number, oldKey: string, newKeyRaw: string) => {
    const newKey = newKeyRaw.trim()
    if (!newKey || newKey === oldKey) return
    setForm((prev) => ({
      ...prev,
      platforms: prev.platforms.map((section, idx) => {
        if (idx !== sectionIdx) return section
        const mapping = { ...section.model_mapping }
        if (newKey in mapping) return section
        const value = mapping[oldKey]
        delete mapping[oldKey]
        mapping[newKey] = value
        return { ...section, model_mapping: mapping }
      })
    }))
  }

  const setMappingValue = (sectionIdx: number, key: string, value: string) => {
    setForm((prev) => ({
      ...prev,
      platforms: prev.platforms.map((section, idx) =>
        idx !== sectionIdx ? section : { ...section, model_mapping: { ...section.model_mapping, [key]: value } }
      )
    }))
  }

  // ── Account Stats Pricing helpers ──
  const addAccountStatsRule = (sectionIdx: number) => {
    setForm((prev) => ({
      ...prev,
      platforms: prev.platforms.map((section, idx) =>
        idx !== sectionIdx
          ? section
          : {
              ...section,
              account_stats_pricing_rules: [
                ...section.account_stats_pricing_rules,
                { name: '', group_ids: [], account_ids: [], pricing: [] }
              ]
            }
      )
    }))
  }

  const addRulePricingEntry = (sectionIdx: number, ruleIndex: number) => {
    setForm((prev) => ({
      ...prev,
      platforms: prev.platforms.map((section, sIdx) =>
        sIdx !== sectionIdx
          ? section
          : {
              ...section,
              account_stats_pricing_rules: section.account_stats_pricing_rules.map((rule, rIdx) =>
                rIdx !== ruleIndex ? rule : { ...rule, pricing: [...rule.pricing, emptyPricingEntry()] }
              )
            }
      )
    }))
  }

  const removeAccountStatsRule = (sectionIdx: number, ruleIndex: number) => {
    setForm((prev) => ({
      ...prev,
      platforms: prev.platforms.map((section, sIdx) =>
        sIdx !== sectionIdx
          ? section
          : { ...section, account_stats_pricing_rules: section.account_stats_pricing_rules.filter((_, i) => i !== ruleIndex) }
      )
    }))
    // Clear all search state since indices shift after removal
    for (const timer of Object.values(ruleAccountSearchTimers.current)) clearTimeout(timer)
    ruleAccountSearchTimers.current = {}
    clearAllRuleAccountSearchState()
  }

  const removeRulePricingEntry = (sectionIdx: number, ruleIndex: number, pricingIndex: number) => {
    setForm((prev) => ({
      ...prev,
      platforms: prev.platforms.map((section, sIdx) =>
        sIdx !== sectionIdx
          ? section
          : {
              ...section,
              account_stats_pricing_rules: section.account_stats_pricing_rules.map((rule, rIdx) =>
                rIdx !== ruleIndex ? rule : { ...rule, pricing: rule.pricing.filter((_, i) => i !== pricingIndex) }
              )
            }
      )
    }))
  }

  const updateRuleField = (sectionIdx: number, ruleIndex: number, patch: Partial<FormPricingRule>) => {
    setForm((prev) => ({
      ...prev,
      platforms: prev.platforms.map((section, sIdx) =>
        sIdx !== sectionIdx
          ? section
          : {
              ...section,
              account_stats_pricing_rules: section.account_stats_pricing_rules.map((rule, rIdx) =>
                rIdx !== ruleIndex ? rule : { ...rule, ...patch }
              )
            }
      )
    }))
  }

  const updateRulePricingEntry = (sectionIdx: number, ruleIndex: number, pricingIndex: number, updated: PricingFormEntry) => {
    setForm((prev) => ({
      ...prev,
      platforms: prev.platforms.map((section, sIdx) =>
        sIdx !== sectionIdx
          ? section
          : {
              ...section,
              account_stats_pricing_rules: section.account_stats_pricing_rules.map((rule, rIdx) =>
                rIdx !== ruleIndex
                  ? rule
                  : { ...rule, pricing: rule.pricing.map((entry, i) => (i === pricingIndex ? updated : entry)) }
              )
            }
      )
    }))
  }

  const getGroupNameById = (groupId: number): string => {
    const group = allGroups.find((g) => g.id === groupId)
    return group ? group.name : `#${groupId}`
  }

  const triggerRuleAccountSearch = (key: string, keyword: string) => {
    if (ruleAccountSearchTimers.current[key]) clearTimeout(ruleAccountSearchTimers.current[key])
    ruleAccountSearchAborts.current[key]?.abort()
    ruleAccountSearchTimers.current[key] = setTimeout(async () => {
      const controller = new AbortController()
      ruleAccountSearchAborts.current[key] = controller
      try {
        const platform = key.split('-')[0]
        const res = await adminAPI.accounts.list(1, 20, { platform, search: keyword }, { signal: controller.signal })
        setRuleAccountSearchResults((prev) => ({
          ...prev,
          [key]: res.items.map((a) => ({ id: a.id, name: a.name, platform: a.platform }))
        }))
      } catch {
        setRuleAccountSearchResults((prev) => ({ ...prev, [key]: [] }))
      }
    }, 300)
  }

  const onRuleAccountSearchInput = (platform: string, ruleIndex: number, keyword: string) => {
    const key = `${platform}-${ruleIndex}`
    setRuleAccountSearchKeyword((prev) => ({ ...prev, [key]: keyword }))
    setShowRuleAccountDropdown((prev) => ({ ...prev, [key]: true }))
    triggerRuleAccountSearch(key, keyword)
  }

  const onRuleAccountSearchFocus = (platform: string, ruleIndex: number) => {
    const key = `${platform}-${ruleIndex}`
    setShowRuleAccountDropdown((prev) => ({ ...prev, [key]: true }))
    if (!ruleAccountSearchResults[key]?.length) {
      triggerRuleAccountSearch(key, ruleAccountSearchKeyword[key] || '')
    }
  }

  const selectRuleAccount = (
    rule: FormPricingRule,
    account: SimpleAccount,
    platform: string,
    ruleIndex: number,
    sectionIdx: number
  ) => {
    if (!rule.account_ids.includes(account.id)) {
      ruleAccountNameCache.current[account.id] = account.name
      updateRuleField(sectionIdx, ruleIndex, { account_ids: [...rule.account_ids, account.id] })
    }
    const key = `${platform}-${ruleIndex}`
    setRuleAccountSearchKeyword((prev) => ({ ...prev, [key]: '' }))
    setShowRuleAccountDropdown((prev) => ({ ...prev, [key]: false }))
  }

  const removeRuleAccount = (rule: FormPricingRule, accountId: number, sectionIdx: number, ruleIndex: number) => {
    updateRuleField(sectionIdx, ruleIndex, { account_ids: rule.account_ids.filter((id) => id !== accountId) })
  }

  const getRuleAccountLabel = (accountId: number): string => {
    const name = ruleAccountNameCache.current[accountId]
    return name ? `${name} #${accountId}` : `#${accountId}`
  }

  const clearAllRuleAccountSearchState = () => {
    setRuleAccountSearchKeyword({})
    setRuleAccountSearchResults({})
    setShowRuleAccountDropdown({})
  }

  const accountStatsRulesToAPI = (): AccountStatsPricingRule[] => {
    const rules: AccountStatsPricingRule[] = []
    for (const section of form.platforms) {
      if (!section.enabled) continue
      for (const rule of section.account_stats_pricing_rules) {
        rules.push({
          name: rule.name,
          group_ids: rule.group_ids,
          account_ids: rule.account_ids,
          pricing: rule.pricing
            .filter((p) => p.models.length > 0)
            .map((p) => ({
              platform: section.platform,
              models: p.models,
              billing_mode: p.billing_mode,
              input_price: mTokToPerToken(p.input_price),
              output_price: mTokToPerToken(p.output_price),
              cache_write_price: mTokToPerToken(p.cache_write_price),
              cache_write_1h_price: mTokToPerToken(p.cache_write_1h_price),
              cache_read_price: mTokToPerToken(p.cache_read_price),
              reasoning_effort_multipliers: formReasoningEffortMultipliersToAPI(p.reasoning_effort_multipliers),
              image_input_price: mTokToPerToken(p.image_input_price),
              image_output_price: mTokToPerToken(p.image_output_price),
              per_request_price: p.per_request_price != null && p.per_request_price !== '' ? Number(p.per_request_price) : null,
              intervals: formIntervalsToAPI(p.intervals || []),
              time_pricing: null
            }))
        })
      }
    }
    return rules
  }

  // ── Form ↔ API conversion ──
  const formToAPI = (): {
    group_ids: number[]
    model_pricing: ChannelModelPricing[]
    model_mapping: Record<string, Record<string, string>>
    features_config: Record<string, unknown>
  } => {
    const group_ids: number[] = []
    const model_pricing: ChannelModelPricing[] = []
    const model_mapping: Record<string, Record<string, string>> = {}
    // Preserve existing features_config fields not managed by the form
    const featuresConfig: Record<string, unknown> = editingChannel?.features_config
      ? { ...editingChannel.features_config }
      : {}

    for (const section of form.platforms) {
      if (!section.enabled) continue
      group_ids.push(...section.group_ids)
      if (section.platform === 'composite') continue

      // Model mapping per platform
      if (Object.keys(section.model_mapping).length > 0) {
        model_mapping[section.platform] = { ...section.model_mapping }
      }

      // Model pricing with platform tag
      for (const entry of section.model_pricing) {
        if (entry.models.length === 0) continue
        model_pricing.push({
          platform: section.platform,
          models: entry.models,
          billing_mode: entry.billing_mode,
          input_price: mTokToPerToken(entry.input_price),
          output_price: mTokToPerToken(entry.output_price),
          cache_write_price: mTokToPerToken(entry.cache_write_price),
          cache_write_1h_price: mTokToPerToken(entry.cache_write_1h_price),
          cache_read_price: mTokToPerToken(entry.cache_read_price),
          fast_multiplier: entry.fast_multiplier != null && entry.fast_multiplier !== '' ? Number(entry.fast_multiplier) : null,
          flex_multiplier: entry.flex_multiplier != null && entry.flex_multiplier !== '' ? Number(entry.flex_multiplier) : null,
          reasoning_effort_multipliers: formReasoningEffortMultipliersToAPI(entry.reasoning_effort_multipliers),
          image_input_price: mTokToPerToken(entry.image_input_price),
          image_output_price: mTokToPerToken(entry.image_output_price),
          per_request_price: entry.per_request_price != null && entry.per_request_price !== '' ? Number(entry.per_request_price) : null,
          intervals: formIntervalsToAPI(entry.intervals || []),
          time_pricing: formTimePricingToAPI(entry.time_pricing)
        })
      }
    }
    const uniqueGroupIds = Array.from(new Set(group_ids))

    // Preserve dimensions without an editable section. Every displayed platform
    // writes an explicit value, including false when its section is disabled.
    const wsEmulation: Record<string, boolean> = {
      ...(featuresConfig.web_search_emulation as Record<string, boolean> | undefined)
    }
    for (const section of form.platforms) {
      if (!WEB_SEARCH_EMULATION_PLATFORMS.has(section.platform)) continue
      wsEmulation[section.platform] = section.enabled ? !!section.web_search_emulation : false
    }
    if (Object.keys(wsEmulation).length > 0) {
      featuresConfig.web_search_emulation = wsEmulation
    } else {
      delete featuresConfig.web_search_emulation
    }

    const codexImageGenerationBridge: Record<string, boolean> = {}
    for (const section of form.platforms) {
      if (!section.enabled) continue
      if (section.platform === 'openai') {
        codexImageGenerationBridge[section.platform] = !!section.codex_image_generation_bridge
      }
    }
    if (Object.keys(codexImageGenerationBridge).length > 0) {
      featuresConfig.codex_image_generation_bridge = codexImageGenerationBridge
    } else {
      delete featuresConfig.codex_image_generation_bridge
    }

    const bedrockCCCompat: Record<string, boolean> = {}
    for (const section of form.platforms) {
      if (!section.enabled) continue
      if (section.platform === 'anthropic') {
        bedrockCCCompat[section.platform] = !!section.bedrock_cc_compat
      }
    }
    if (Object.keys(bedrockCCCompat).length > 0) {
      featuresConfig.bedrock_cc_compat = bedrockCCCompat
    } else {
      delete featuresConfig.bedrock_cc_compat
    }

    return { group_ids: uniqueGroupIds, model_pricing, model_mapping, features_config: featuresConfig }
  }

  const apiToForm = (channel: Channel): PlatformSection[] => {
    // Build a map: groupID → platform
    const groupPlatformMap = new Map<number, GroupPlatform>()
    for (const g of allGroups) {
      groupPlatformMap.set(g.id, g.platform)
    }

    // Determine which platforms are active (from groups + pricing + mapping)
    const active = new Set<GroupPlatform>()
    for (const gid of channel.group_ids || []) {
      const p = groupPlatformMap.get(gid)
      if (p) active.add(p)
    }
    for (const p of channel.model_pricing || []) {
      if (p.platform) active.add(p.platform as GroupPlatform)
    }
    for (const p of Object.keys(channel.model_mapping || {})) {
      if (platformOrder.includes(p as GroupPlatform)) active.add(p as GroupPlatform)
    }

    // Build sections in platform order
    const sections: PlatformSection[] = []
    for (const platform of platformOrder) {
      if (!active.has(platform)) continue

      const groupIds = (channel.group_ids || []).filter((gid) => groupPlatformMap.get(gid) === platform)
      const mapping = (channel.model_mapping || {})[platform] || {}
      const pricing = (channel.model_pricing || [])
        .filter((p) => (p.platform || 'anthropic') === platform)
        .map(
          (p) =>
            ({
              models: p.models || [],
              billing_mode: p.billing_mode,
              input_price: perTokenToMTok(p.input_price),
              output_price: perTokenToMTok(p.output_price),
              cache_write_price: perTokenToMTok(p.cache_write_price),
              cache_write_1h_price: perTokenToMTok(p.cache_write_1h_price),
              cache_read_price: perTokenToMTok(p.cache_read_price),
              fast_multiplier: p.fast_multiplier,
              flex_multiplier: p.flex_multiplier,
              reasoning_effort_multipliers: p.reasoning_effort_multipliers ? { ...p.reasoning_effort_multipliers } : null,
              image_input_price: perTokenToMTok(p.image_input_price),
              image_output_price: perTokenToMTok(p.image_output_price),
              per_request_price: p.per_request_price,
              intervals: apiIntervalsToForm(p.intervals || []),
              time_pricing: apiTimePricingToForm(p.time_pricing)
            }) as PricingFormEntry
        )

      // Read web_search_emulation from features_config
      const fc = channel.features_config
      const wsEmulation = fc?.web_search_emulation as Record<string, boolean> | undefined
      const webSearchEnabled = wsEmulation?.[platform] === true
      const codexImageGenerationBridge = fc?.codex_image_generation_bridge as Record<string, boolean> | undefined
      const codexImageGenerationBridgeEnabled = codexImageGenerationBridge?.[platform] === true
      const bedrockCCCompatEnabled = fc?.bedrock_cc_compat === true

      sections.push({
        platform,
        enabled: true,
        collapsed: false,
        group_ids: groupIds,
        model_mapping: { ...mapping },
        model_pricing: pricing,
        web_search_emulation: webSearchEnabled,
        codex_image_generation_bridge: codexImageGenerationBridgeEnabled,
        bedrock_cc_compat: bedrockCCCompatEnabled,
        account_stats_pricing_rules: []
      })
    }

    return sections
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

  // ── Dialog ──
  const resetForm = () => {
    setForm(EMPTY_FORM)
    setActiveTab('basic')
    for (const timer of Object.values(ruleAccountSearchTimers.current)) clearTimeout(timer)
    ruleAccountSearchTimers.current = {}
    clearAllRuleAccountSearchState()
    ruleAccountNameCache.current = {}
  }

  const openCreateDialog = async () => {
    setEditingChannel(null)
    resetForm()
    await Promise.all([loadGroups(), loadAllChannelsForConflict()])
    if (compositeOnly) selectCompositeSupplier(form.platforms.find((section) => section.platform !== 'composite')?.platform || 'openai')
    setShowDialog(true)
  }

  /** Distribute flat channel-level rules into the matching platform section based on group_ids */
  const distributeRulesToPlatforms = (apiRules: AccountStatsPricingRule[], sections: PlatformSection[]): PlatformSection[] => {
    // Build groupID → platform lookup
    const groupPlatformMap = new Map<number, GroupPlatform>()
    for (const g of allGroups) {
      groupPlatformMap.set(g.id, g.platform)
    }

    const next = sections.map((section) => ({ ...section }))

    for (const apiRule of apiRules) {
      // Infer platform from group_ids
      const platforms = new Set<GroupPlatform>()
      for (const gid of apiRule.group_ids || []) {
        const p = groupPlatformMap.get(gid)
        if (p && p !== 'composite') platforms.add(p)
      }
      // If pricing has a platform field, use that as fallback
      if (platforms.size === 0 && apiRule.pricing?.length > 0) {
        const p = apiRule.pricing[0].platform as GroupPlatform | undefined
        if (p) platforms.add(p)
      }
      const targetPlatform = platforms.size >= 1 ? [...platforms][0] : null
      if (!targetPlatform) continue

      const section = next.find((s) => s.platform === targetPlatform)
      if (!section) continue

      const formRule: FormPricingRule = {
        name: apiRule.name || '',
        group_ids: [...(apiRule.group_ids || [])],
        account_ids: [...(apiRule.account_ids || [])],
        pricing: (apiRule.pricing || []).map(
          (p) =>
            ({
              models: [...(p.models || [])],
              billing_mode: p.billing_mode,
              input_price: perTokenToMTok(p.input_price),
              output_price: perTokenToMTok(p.output_price),
              cache_write_price: perTokenToMTok(p.cache_write_price),
              cache_write_1h_price: perTokenToMTok(p.cache_write_1h_price),
              cache_read_price: perTokenToMTok(p.cache_read_price),
              reasoning_effort_multipliers: p.reasoning_effort_multipliers ? { ...p.reasoning_effort_multipliers } : null,
              image_input_price: perTokenToMTok(p.image_input_price),
              image_output_price: perTokenToMTok(p.image_output_price),
              per_request_price: p.per_request_price,
              intervals: apiIntervalsToForm(p.intervals || []),
              time_pricing: createDefaultTimePricingForm()
            }) as PricingFormEntry
        )
      }
      section.account_stats_pricing_rules.push(formRule)
    }

    return next
  }

  /** Populate ruleAccountNameCache by fetching account details for all account_ids in rules */
  const populateRuleAccountNameCache = async (sections: PlatformSection[]) => {
    const allAccountIds = new Set<number>()
    for (const section of sections) {
      for (const rule of section.account_stats_pricing_rules) {
        for (const id of rule.account_ids) {
          allAccountIds.add(id)
        }
      }
    }
    if (allAccountIds.size === 0) return

    // Fetch account details in parallel (batch of individual getById calls)
    const ids = [...allAccountIds]
    const results = await Promise.allSettled(ids.map((id) => adminAPI.accounts.getById(id)))
    for (let i = 0; i < ids.length; i++) {
      const result = results[i]
      if (result.status === 'fulfilled') {
        ruleAccountNameCache.current[ids[i]] = result.value.name
      }
      // If rejected, the cache won't have the name, so it'll show "#ID" which is acceptable
    }
  }

  const openEditDialog = async (channel: Channel) => {
    setEditingChannel(channel)
    setForm({
      name: channel.name,
      description: channel.description || '',
      status: channel.status,
      restrict_models: channel.restrict_models || false,
      billing_model_source: channel.billing_model_source || 'channel_mapped',
      platforms: [],
      apply_pricing_to_account_stats: channel.apply_pricing_to_account_stats || false
    })
    // Must load groups first so apiToForm can map groupID → platform
    await Promise.all([loadGroups(), loadAllChannelsForConflict()])
    const sections = distributeRulesToPlatforms(channel.account_stats_pricing_rules || [], apiToForm(channel))
    setForm((prev) => ({ ...prev, platforms: sections }))

    await populateRuleAccountNameCache(sections)

    if (compositeOnly) selectCompositeSupplier(sections.find((section) => section.platform !== 'composite')?.platform || 'openai')
    setShowDialog(true)
  }

  const closeDialog = () => {
    setShowDialog(false)
    setEditingChannel(null)
    resetForm()
  }

  const handleSubmit = async () => {
    if (submitting) return
    if (!form.name.trim()) {
      showError(t('admin.channels.nameRequired', 'Please enter a channel name'))
      return
    }

    // Check for pricing entries with empty models (would be silently skipped)
    for (const section of form.platforms.filter((s) => s.enabled)) {
      if (section.group_ids.length === 0 && (section.platform === 'composite' || !compositeSection?.group_ids.length)) {
        const platformLabel = t('admin.groups.platforms.' + section.platform, catalogPlatformLabel(section.platform))
        showError(t('admin.channels.noGroupsSelected', { platform: platformLabel }))
        setActiveTab(section.platform)
        return
      }
      for (const entry of section.model_pricing) {
        if (entry.models.length === 0) {
          const platformLabel = t('admin.groups.platforms.' + section.platform, catalogPlatformLabel(section.platform))
          showError(t('admin.channels.emptyModelsInPricing', { platform: platformLabel }))
          setActiveTab(section.platform)
          return
        }
      }
    }

    // Check model pattern conflicts per platform (duplicate / wildcard overlap)
    for (const section of form.platforms.filter((s) => s.enabled)) {
      // Collect all pricing models for this platform
      const allModels: string[] = []
      for (const entry of section.model_pricing) {
        allModels.push(...entry.models)
      }
      const pricingConflict = findModelConflict(allModels)
      if (pricingConflict) {
        showError(t('admin.channels.modelConflict', { model1: pricingConflict[0], model2: pricingConflict[1] }))
        setActiveTab(section.platform)
        return
      }
      // Check model mapping source pattern conflicts
      const mappingKeys = Object.keys(section.model_mapping)
      if (mappingKeys.length > 0) {
        const mappingConflict = findModelConflict(mappingKeys)
        if (mappingConflict) {
          showError(t('admin.channels.mappingConflict', { model1: mappingConflict[0], model2: mappingConflict[1] }))
          setActiveTab(section.platform)
          return
        }
      }
    }

    // 校验 per_request/image 模式必须有价格 (只校验启用的平台)
    for (const section of form.platforms.filter((s) => s.enabled)) {
      for (const entry of section.model_pricing) {
        if (entry.models.length === 0) continue
        if (
          (entry.billing_mode === 'per_request' || entry.billing_mode === 'image') &&
          (entry.per_request_price == null || entry.per_request_price === '') &&
          (!entry.intervals || entry.intervals.length === 0)
        ) {
          showError(t('admin.channels.form.perRequestPriceRequired'))
          return
        }
      }
    }

    // 思考等级倍率同时适用于渠道计价和独立的账号统计计价规则。
    for (const section of form.platforms.filter((s) => s.enabled)) {
      const entries = [...section.model_pricing, ...section.account_stats_pricing_rules.flatMap((rule) => rule.pricing)]
      for (const entry of entries) {
        const error = validateReasoningEffortMultipliers(entry.reasoning_effort_multipliers, t)
        if (!error) continue
        const platformLabel = t('admin.groups.platforms.' + section.platform, catalogPlatformLabel(section.platform))
        const modelLabel = entry.models.join(', ') || t('admin.channels.form.unnamed')
        showError(`${platformLabel} - ${modelLabel}: ${error}`)
        setActiveTab(section.platform)
        return
      }
    }

    // 校验区间合法性（范围、重叠等）
    for (const section of form.platforms.filter((s) => s.enabled)) {
      for (const entry of section.model_pricing) {
        if (!isValidPositiveMultiplier(entry.fast_multiplier) || !isValidPositiveMultiplier(entry.flex_multiplier)) {
          const platformLabel = t('admin.groups.platforms.' + section.platform, catalogPlatformLabel(section.platform))
          const modelLabel = entry.models.join(', ') || t('admin.channels.form.unnamed')
          showError(`${platformLabel} - ${modelLabel}: ${t('admin.channels.form.multiplierPositive')}`)
          setActiveTab(section.platform)
          return
        }
        if (!entry.intervals || entry.intervals.length === 0) continue
        const intervalErr = validateIntervals(entry.intervals, entry.billing_mode, t)
        if (intervalErr) {
          const platformLabel = t('admin.groups.platforms.' + section.platform, catalogPlatformLabel(section.platform))
          const modelLabel = entry.models.join(', ') || t('admin.channels.form.unnamed')
          showError(`${platformLabel} - ${modelLabel}: ${intervalErr}`)
          setActiveTab(section.platform)
          return
        }
      }
    }

    // 校验时间段定价，并切换到对应平台便于修正
    for (const section of form.platforms.filter((s) => s.enabled)) {
      for (const entry of section.model_pricing) {
        const timePricingError = validateTimePricing(entry.time_pricing, t)
        if (timePricingError) {
          const platformLabel = t('admin.groups.platforms.' + section.platform, catalogPlatformLabel(section.platform))
          const modelLabel = entry.models.join(', ') || t('admin.channels.form.unnamed')
          showError(`${platformLabel} - ${modelLabel}: ${timePricingError}`)
          setActiveTab(section.platform)
          return
        }
      }
    }

    const { group_ids, model_pricing, model_mapping, features_config } = formToAPI()

    setSubmitting(true)
    try {
      if (editingChannel) {
        const req: UpdateChannelRequest = {
          name: form.name.trim(),
          description: form.description.trim() || undefined,
          status: form.status,
          group_ids,
          model_pricing,
          model_mapping: Object.keys(model_mapping).length > 0 ? model_mapping : {},
          billing_model_source: form.billing_model_source,
          restrict_models: form.restrict_models,
          features_config,
          apply_pricing_to_account_stats: form.apply_pricing_to_account_stats,
          account_stats_pricing_rules: accountStatsRulesToAPI()
        }
        await adminAPI.channels.update(editingChannel.id, req)
        showSuccess(t('admin.channels.updateSuccess', 'Channel updated'))
      } else {
        const req: CreateChannelRequest = {
          name: form.name.trim(),
          description: form.description.trim() || undefined,
          group_ids,
          model_pricing,
          model_mapping: Object.keys(model_mapping).length > 0 ? model_mapping : {},
          billing_model_source: form.billing_model_source,
          restrict_models: form.restrict_models,
          features_config,
          apply_pricing_to_account_stats: form.apply_pricing_to_account_stats,
          account_stats_pricing_rules: accountStatsRulesToAPI()
        }
        await adminAPI.channels.create(req)
        showSuccess(t('admin.channels.createSuccess', 'Channel created'))
      }
      closeDialog()
      setReloadKey((k) => k + 1)
    } catch (error) {
      showError(
        extractApiErrorMessage(
          error,
          editingChannel
            ? t('admin.channels.updateError', 'Failed to update channel')
            : t('admin.channels.createError', 'Failed to create channel')
        )
      )
    } finally {
      setSubmitting(false)
    }
  }

  // ── Toggle status ──
  const toggleChannelStatus = async (channel: Channel) => {
    const newStatus = channel.status === 'active' ? 'disabled' : 'active'
    try {
      await adminAPI.channels.update(channel.id, { status: newStatus })
      if (statusFilter && statusFilter !== newStatus) {
        // Item no longer matches the active filter — reload list
        setReloadKey((k) => k + 1)
      } else {
        setChannels((prev) => prev.map((entry) => (entry.id === channel.id ? { ...entry, status: newStatus } : entry)))
      }
    } catch (error) {
      showError(t('admin.channels.updateError', 'Failed to update channel'))
      console.error('Error toggling channel status:', error)
    }
  }

  // ── Delete ──
  const handleDelete = (channel: Channel) => {
    setDeletingChannel(channel)
    setShowDeleteDialog(true)
  }

  const confirmDelete = async () => {
    if (!deletingChannel) return

    try {
      await adminAPI.channels.remove(deletingChannel.id)
      showSuccess(t('admin.channels.deleteSuccess', 'Channel deleted'))
      setShowDeleteDialog(false)
      setDeletingChannel(null)
      setReloadKey((k) => k + 1)
    } catch (error) {
      showError(extractApiErrorMessage(error, t('admin.channels.deleteError', 'Failed to delete channel')))
    }
  }

  const platformLabel = (platform: string) => t('admin.groups.platforms.' + platform, catalogPlatformLabel(platform))

  return (
    <>
      <AdminTablePageLayout
        filters={
          <div className="flex flex-col justify-between gap-4 lg:flex-row lg:items-start">
            <div className="filter-bar flex-1">
              <div className="filter-field filter-field--search relative">
                <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 surface-muted" />
                <Input
                  value={searchQuery}
                  onValueChange={(value) => {
                    setSearchQuery(value)
                    handleSearch()
                  }}
                  type="text"
                  placeholder={t('admin.channels.searchChannels', 'Search channels...')}
                  className="pl-10"
                />
              </div>

              <div className="filter-field">
                <Select
                  value={statusFilter}
                  options={statusFilterOptions}
                  placeholder={t('admin.channels.allStatus', 'All Status')}
                  onValueChange={(value) => {
                    setStatusFilter(String(value ?? ''))
                    setPagination((prev) => ({ ...prev, page: 1 }))
                    setReloadKey((k) => k + 1)
                  }}
                />
              </div>
            </div>

            <div className="flex w-full flex-shrink-0 flex-wrap items-center justify-end gap-3 lg:w-auto">
              <Button
                variant="secondary"
                size="icon"
                disabled={loading}
                onClick={() => setReloadKey((k) => k + 1)}
                title={t('common.refresh', 'Refresh')}
              >
                <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
              </Button>
              <Button variant="primary" onClick={openCreateDialog}>
                <Plus className="mr-2 h-4 w-4" />
                {t('admin.channels.createChannel', 'Create Channel')}
              </Button>
            </div>
          </div>
        }
        table={
          <AdminDataTable<Channel>
            columns={columns}
            data={channels}
            loading={loading}
            rowKey="id"
            sortKey={sortState.sort_by}
            sortOrder={sortState.sort_order}
            onSort={handleSort}
            renderCell={(key, row) => {
              if (key === 'name') {
                return <span className="font-medium text-gray-900 dark:text-white">{row.name}</span>
              }
              if (key === 'description') {
                return <span className="text-sm surface-muted">{row.description || '-'}</span>
              }
              if (key === 'status') {
                return <Toggle checked={row.status === 'active'} onCheckedChange={() => toggleChannelStatus(row)} />
              }
              if (key === 'group_count') {
                return (
                  <span className="inline-flex items-center rounded surface-inset px-2 py-0.5 text-xs font-medium text-gray-800 dark:text-gray-300">
                    {(row.group_ids || []).length} {t('admin.channels.groupsUnit', 'groups')}
                  </span>
                )
              }
              if (key === 'pricing_count') {
                return (
                  <span className="inline-flex items-center rounded surface-inset px-2 py-0.5 text-xs font-medium text-gray-800 dark:text-gray-300">
                    {(row.model_pricing || []).length} {t('admin.channels.pricingUnit', 'pricing rules')}
                  </span>
                )
              }
              if (key === 'created_at') {
                return <span className="text-sm surface-muted">{formatDate(row.created_at)}</span>
              }
              if (key === 'actions') {
                return (
                  <div className="table-actions">
                    <button className="table-action" onClick={() => openEditDialog(row)}>
                      <Pencil className="h-3.5 w-3.5" />
                      <span className="text-xs">{t('common.edit', 'Edit')}</span>
                    </button>
                    <button className="table-action table-action-danger" onClick={() => handleDelete(row)}>
                      <Trash2 className="h-3.5 w-3.5" />
                      <span className="text-xs">{t('common.delete', 'Delete')}</span>
                    </button>
                  </div>
                )
              }
              return null
            }}
            empty={
              <EmptyState
                title={t('admin.channels.noChannelsYet', 'No Channels Yet')}
                description={t('admin.channels.createFirstChannel', 'Create your first channel to manage model pricing')}
                action={
                  <Button variant="primary" onClick={openCreateDialog}>
                    {t('admin.channels.createChannel', 'Create Channel')}
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

      <Modal
        open={showDialog}
        onClose={closeDialog}
        title={editingChannel ? t('admin.channels.editChannel', 'Edit Channel') : t('admin.channels.createChannel', 'Create Channel')}
        size="full"
        footer={
          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={closeDialog}>
              {t('common.cancel', 'Cancel')}
            </Button>
            <Button variant="primary" loading={submitting} onClick={handleSubmit}>
              {submitting
                ? t('common.submitting', 'Submitting...')
                : editingChannel
                  ? t('common.update', 'Update')
                  : t('common.create', 'Create')}
            </Button>
          </div>
        }
      >
        <div className="channel-dialog-body">
          <div className="card-tabs flex-shrink-0" role="tablist" aria-label={t('admin.channels.form.basicSettings')}>
            <button
              type="button"
              role="tab"
              onClick={() => setActiveTab('basic')}
              className="card-tab"
              aria-selected={activeTab === 'basic'}
            >
              {t('admin.channels.form.basicSettings')}
            </button>
            {form.platforms
              .filter((s) => s.enabled && (!compositeOnly || s.platform === 'composite'))
              .map((section) => (
                <button
                  key={section.platform}
                  type="button"
                  role="tab"
                  onClick={() => setActiveTab(section.platform)}
                  className="card-tab group"
                  aria-selected={activeTab === section.platform}
                >
                  <span className={platformTextClass(section.platform)}>{platformLabel(section.platform)}</span>
                </button>
              ))}
          </div>

          <div className="flex-1 overflow-y-auto pt-4 space-y-5">
            {activeTab === 'basic' && (
              <div className="space-y-5">
                <div>
                  <label className="input-label">
                    {t('admin.channels.form.name', 'Name')} <span className="text-red-500">*</span>
                  </label>
                  <Input
                    type="text"
                    required
                    value={form.name}
                    onValueChange={(value) => setForm((prev) => ({ ...prev, name: value }))}
                    placeholder={t('admin.channels.form.namePlaceholder', 'Enter channel name')}
                  />
                </div>

                <div>
                  <label className="input-label">{t('admin.channels.form.description', 'Description')}</label>
                  <textarea
                    rows={2}
                    value={form.description}
                    onChange={(event) => setForm((prev) => ({ ...prev, description: event.target.value }))}
                    className="input"
                    placeholder={t('admin.channels.form.descriptionPlaceholder', 'Optional description')}
                  />
                </div>

                {editingChannel && (
                  <div>
                    <label className="input-label">{t('admin.channels.form.status', 'Status')}</label>
                    <Select
                      value={form.status}
                      options={statusEditOptions}
                      onValueChange={(value) => setForm((prev) => ({ ...prev, status: String(value ?? '') }))}
                    />
                  </div>
                )}

                <div>
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={form.restrict_models}
                      onChange={(event) => setForm((prev) => ({ ...prev, restrict_models: event.target.checked }))}
                      className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                    />
                    <span className="input-label mb-0">{t('admin.channels.form.restrictModels', 'Restrict Models')}</span>
                  </label>
                  <p className="mt-1 ml-6 text-xs surface-muted">
                    {t('admin.channels.form.restrictModelsHint', 'Require channel, system, or platform fallback pricing. Group and account rules still control model access.')}
                  </p>
                </div>

                <div>
                  <label className="input-label">{t('admin.channels.form.billingModelSource', 'Billing Basis')}</label>
                  <Select
                    value={form.billing_model_source}
                    options={billingModelSourceOptions}
                    onValueChange={(value) => setForm((prev) => ({ ...prev, billing_model_source: String(value ?? '') }))}
                  />
                  <p className="mt-1 text-xs surface-muted">
                    {t('admin.channels.form.billingModelSourceHint', 'Controls which model name is used for pricing lookup')}
                  </p>
                </div>

                <div className="space-y-3">
                  <label className="input-label mb-0">{t('admin.channels.form.platformConfig')}</label>
                  <div className="flex flex-wrap gap-2">
                    {platformOrder.map((p) => (
                      <label
                        key={p}
                        className={`inline-flex cursor-pointer items-center gap-1.5 rounded-md border px-3 py-1.5 text-sm transition-colors ${
                          activePlatforms.includes(p)
                            ? 'bg-primary-50/[0.15] border-primary-300 dark:bg-primary-900/20 dark:border-primary-700'
                            : 'border-gray-200 hover:surface-hover dark:border-dark-600 dark:hover:surface-hover'
                        }`}
                      >
                        <input
                          type="checkbox"
                          checked={activePlatforms.includes(p)}
                          onChange={() => togglePlatform(p)}
                          className="h-3.5 w-3.5 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                        />
                        <span className={platformTextClass(p)}>{platformLabel(p)}</span>
                      </label>
                    ))}
                  </div>
                </div>

                <div className="border-t border-gray-200 pt-4 dark:border-dark-700">
                  <div className="flex items-center justify-between">
                    <div>
                      <label className="text-sm font-medium surface-muted">{t('admin.channels.form.applyPricingToAccountStats')}</label>
                      <p className="mt-0.5 text-xs surface-muted">{t('admin.channels.form.applyPricingToAccountStatsDesc')}</p>
                    </div>
                    <Toggle
                      checked={form.apply_pricing_to_account_stats}
                      onCheckedChange={(checked) => setForm((prev) => ({ ...prev, apply_pricing_to_account_stats: checked }))}
                    />
                  </div>
                </div>
              </div>
            )}

            {form.platforms.map((section, sIdx) => {
              const visible =
                section.enabled && (activeTab === section.platform || (activeTab === 'composite' && section.platform === compositeSupplier))
              if (!visible) return null
              return (
                <div key={`tab-${section.platform}`} className="space-y-4">
                  {section.platform === 'composite' && (
                    <div className="space-y-3">
                      <p className="input-hint">{t('nativeAccess.channelHint')}</p>
                      <label>
                        <span className="input-label">{t('billingRates.editor.choosePlatform')}</span>
                        <Select
                          value={compositeSupplier}
                          options={supplierOptions}
                          onValueChange={(value) => selectCompositeSupplier(value)}
                        />
                      </label>
                    </div>
                  )}

                  {(section.platform === 'composite' || !compositeOnly) && (
                    <div>
                      <label className="input-label text-xs">
                        {t('admin.channels.form.groups', 'Associated Groups')} <span className="text-red-500">*</span>
                        {section.group_ids.length > 0 && (
                          <span className="ml-1 font-normal surface-muted">
                            ({t('admin.channels.form.selectedCount', { count: section.group_ids.length })})
                          </span>
                        )}
                      </label>
                      <div className="max-h-40 overflow-auto rounded-lg border border-gray-200 surface-inset p-2 dark:border-dark-600">
                        {groupsLoading ? (
                          <div className="py-2 text-center text-xs surface-muted">{t('common.loading', 'Loading...')}</div>
                        ) : getGroupsForPlatform(section.platform).length === 0 ? (
                          <div className="py-2 text-center text-xs surface-muted">{t('admin.channels.form.noGroupsAvailable', 'No groups available')}</div>
                        ) : (
                          <div className="flex flex-wrap gap-1">
                            {getGroupsForPlatform(section.platform).map((group) => (
                              <label
                                key={group.id}
                                className={`inline-flex cursor-pointer items-center gap-1.5 rounded-md border border-gray-200 px-2 py-1 text-xs transition-colors hover:surface-hover dark:border-dark-600 ${
                                  section.group_ids.includes(group.id) ? 'bg-primary-50/[0.15] border-primary-300 dark:bg-primary-900/20 dark:border-primary-700' : ''
                                } ${isGroupInOtherChannel(group.id) ? 'opacity-40' : ''}`}
                              >
                                <input
                                  type="checkbox"
                                  checked={section.group_ids.includes(group.id)}
                                  disabled={isGroupInOtherChannel(group.id)}
                                  onChange={() => toggleGroupInSection(sIdx, group.id)}
                                  className="h-3 w-3 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                                />
                                <span className={`font-medium ${platformTextClass(group.platform)}`}>{group.name}</span>
                                <span className={`rounded-full px-1 py-0 text-[10px] ${platformBadgeLightClass(group.platform)}`}>
                                  {group.rate_multiplier}x
                                </span>
                                <span className="text-[10px] surface-muted">{group.account_count || 0}</span>
                                {isGroupInOtherChannel(group.id) && (
                                  <span className="text-[10px] surface-muted">{getGroupInOtherChannelLabel(group.id)}</span>
                                )}
                              </label>
                            ))}
                          </div>
                        )}
                      </div>
                    </div>
                  )}

                  {section.platform !== 'composite' && (
                    <>
                      {/* Web Search Emulation (platform whitelist, hidden when global disabled) */}
                      {WEB_SEARCH_EMULATION_PLATFORMS.has(section.platform) && webSearchGlobalEnabled && (
                        <div className="border-t border-gray-200 pt-3 dark:border-dark-600">
                          <div className="flex items-center justify-between gap-4">
                            <div>
                              <label className="text-xs font-medium surface-muted">{t('admin.channels.form.webSearchEmulation')}</label>
                              <p className="mt-0.5 text-[11px] text-red-500 dark:text-red-400">{t('admin.channels.form.webSearchEmulationHint')}</p>
                            </div>
                            <Toggle
                              checked={section.web_search_emulation}
                              onCheckedChange={(checked) =>
                                setForm((prev) => ({
                                  ...prev,
                                  platforms: prev.platforms.map((entry, idx) =>
                                    idx === sIdx ? { ...entry, web_search_emulation: checked } : entry
                                  )
                                }))
                              }
                            />
                          </div>
                        </div>
                      )}

                      {/* Codex Image Generation Bridge (OpenAI only) */}
                      {section.platform === 'openai' && (
                        <div className="border-t border-gray-200 pt-3 dark:border-dark-600">
                          <div className="flex items-center justify-between gap-4">
                            <div>
                              <label className="text-xs font-medium surface-muted">{t('admin.channels.form.codexImageGenerationBridge')}</label>
                              <p className="mt-0.5 text-[11px] text-amber-600 dark:text-amber-400">{t('admin.channels.form.codexImageGenerationBridgeHint')}</p>
                            </div>
                            <Toggle
                              checked={section.codex_image_generation_bridge}
                              onCheckedChange={(checked) =>
                                setForm((prev) => ({
                                  ...prev,
                                  platforms: prev.platforms.map((entry, idx) =>
                                    idx === sIdx ? { ...entry, codex_image_generation_bridge: checked } : entry
                                  )
                                }))
                              }
                            />
                          </div>
                        </div>
                      )}

                      {/* Bedrock CC Compatibility (Anthropic only) */}
                      {section.platform === 'anthropic' && (
                        <div className="border-t border-gray-200 pt-3 dark:border-dark-600">
                          <div className="flex items-center justify-between gap-4">
                            <div>
                              <label className="text-xs font-medium surface-muted">{t('admin.channels.form.bedrockCCCompat')}</label>
                              <p className="mt-0.5 text-[11px] text-amber-600 dark:text-amber-400">{t('admin.channels.form.bedrockCCCompatHint')}</p>
                            </div>
                            <Toggle
                              checked={section.bedrock_cc_compat}
                              onCheckedChange={(checked) =>
                                setForm((prev) => ({
                                  ...prev,
                                  platforms: prev.platforms.map((entry, idx) =>
                                    idx === sIdx ? { ...entry, bedrock_cc_compat: checked } : entry
                                  )
                                }))
                              }
                            />
                          </div>
                        </div>
                      )}

                      {/* Model Mapping */}
                      <div>
                        <div className="mb-1 flex items-center justify-between">
                          <label className="input-label text-xs mb-0">{t('admin.channels.form.modelMapping', 'Model Mapping')}</label>
                          <button type="button" onClick={() => addMappingEntry(sIdx)} className="text-xs text-primary-600 hover:text-primary-700 surface-control">
                            + {t('common.add', 'Add')}
                          </button>
                        </div>
                        {Object.keys(section.model_mapping).length === 0 ? (
                          <div className="rounded border border-dashed border-gray-300 p-2 text-center text-xs dark:border-dark-500 surface-muted">
                            {t('admin.channels.form.noMappingRules', 'No mapping rules. Click "Add" to create one.')}
                          </div>
                        ) : (
                          <div className="space-y-1">
                            {Object.keys(section.model_mapping).map((srcModel) => (
                              <div key={srcModel} className="flex items-center gap-2">
                                <MappingSourceInput
                                  srcModel={srcModel}
                                  className={`flex-1 text-xs ${platformTextClass(section.platform)}`}
                                  placeholder={t('admin.channels.form.mappingSource', 'Source model')}
                                  onRename={(newKey) => renameMappingKey(sIdx, srcModel, newKey)}
                                />
                                <span className="text-xs surface-muted">→</span>
                                <Input
                                  value={section.model_mapping[srcModel]}
                                  type="text"
                                  className={`flex-1 text-xs ${platformTextClass(section.platform)}`}
                                  placeholder={t('admin.channels.form.mappingTarget', 'Target model')}
                                  onValueChange={(value) => setMappingValue(sIdx, srcModel, value)}
                                />
                                <button
                                  type="button"
                                  onClick={() => removeMappingEntry(sIdx, srcModel)}
                                  className="rounded p-0.5 hover:text-red-500 surface-control surface-muted"
                                >
                                  <Trash2 className="h-4 w-4" />
                                </button>
                              </div>
                            ))}
                          </div>
                        )}
                      </div>

                      {/* Model Pricing */}
                      <div>
                        <div className="mb-1 flex items-center justify-between">
                          <label className="input-label text-xs mb-0">{t('admin.channels.form.modelPricing', 'Model Pricing')}</label>
                          <div className="flex items-center gap-2">
                            {listPlatforms().some((spec) => spec.id === section.platform) && (
                              <button
                                type="button"
                                onClick={() => syncLatestModels(sIdx)}
                                disabled={!!syncingPlatform}
                                className="text-xs hover:text-primary-600 disabled:opacity-50 surface-control surface-muted"
                              >
                                {syncingPlatform === section.platform ? t('admin.channels.form.syncingModels') : t('admin.channels.form.syncLatestModels')}
                              </button>
                            )}
                            <button type="button" onClick={() => addPricingEntry(sIdx)} className="text-xs text-primary-600 hover:text-primary-700 surface-control">
                              + {t('common.add', 'Add')}
                            </button>
                          </div>
                        </div>
                        {section.model_pricing.length === 0 ? (
                          <div className="rounded border border-dashed border-gray-300 p-2 text-center text-xs dark:border-dark-500 surface-muted">
                            {t('admin.channels.form.noPricingRules', 'No pricing rules yet. Click "Add" to create one.')}
                          </div>
                        ) : (
                          <div className="space-y-2">
                            {section.model_pricing.map((entry, idx) => (
                              <PricingEntryCard
                                key={`pricing-${idx}`}
                                entry={entry}
                                platform={section.platform}
                                enableTimePricing
                                enableTierMultipliers
                                onUpdate={(updated) => updatePricingEntry(sIdx, idx, updated)}
                                onRemove={() => removePricingEntry(sIdx, idx)}
                                onSplit={(models) => splitPricingEntry(sIdx, idx, models)}
                              />
                            ))}
                          </div>
                        )}
                      </div>

                      <GroupModelRateLink groups={allGroups.filter((group) => section.group_ids.includes(group.id))} />

                      {/* Account Stats Pricing Rules (per-platform, always visible) */}
                      <div className="mt-4 border-t border-gray-200 pt-4 dark:border-dark-700 space-y-3">
                        <div className="flex items-center justify-between">
                          <h4 className="text-sm font-medium surface-muted">{t('admin.channels.form.accountStatsPricingRules')}</h4>
                          <button
                            type="button"
                            onClick={() => addAccountStatsRule(sIdx)}
                            className="rounded-lg border border-primary-300 px-3 py-1 text-xs font-medium text-primary-600 hover:bg-primary-50/25 dark:border-primary-600 dark:text-primary-400 dark:hover:bg-primary-900/20 surface-control"
                          >
                            + {t('admin.channels.form.addRule')}
                          </button>
                        </div>

                        {section.account_stats_pricing_rules.length === 0 && (
                          <p className="text-xs italic surface-muted">{t('admin.channels.form.noRulesConfigured')}</p>
                        )}

                        {section.account_stats_pricing_rules.map((rule, ruleIndex) => (
                          <div key={`rule-${ruleIndex}`} className="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-600">
                            <div className="flex items-center justify-between">
                              <Input
                                value={rule.name}
                                onValueChange={(value) => updateRuleField(sIdx, ruleIndex, { name: value })}
                                placeholder={t('admin.channels.form.ruleName')}
                                className="bg-transparent text-sm font-medium"
                              />
                              <button
                                type="button"
                                onClick={() => removeAccountStatsRule(sIdx, ruleIndex)}
                                className="text-xs text-red-500 hover:text-red-700 surface-control"
                              >
                                {t('common.delete')}
                              </button>
                            </div>

                            <div>
                              <label className="text-xs surface-muted">{t('admin.channels.form.ruleGroups')}</label>
                              <div className="mt-1 flex flex-wrap gap-1">
                                {sectionGroupIds(section).map((gid) => (
                                  <label
                                    key={gid}
                                    className={`inline-flex cursor-pointer items-center gap-1 rounded-md border px-2 py-1 text-xs transition-colors ${
                                      rule.group_ids.includes(gid)
                                        ? 'border-primary-300 bg-primary-50/[0.15] dark:border-primary-700 dark:bg-primary-900/20'
                                        : 'border-gray-200 hover:surface-hover dark:border-dark-600 dark:hover:surface-hover'
                                    }`}
                                  >
                                    <input
                                      type="checkbox"
                                      checked={rule.group_ids.includes(gid)}
                                      onChange={(event) =>
                                        updateRuleField(sIdx, ruleIndex, {
                                          group_ids: event.target.checked
                                            ? [...rule.group_ids, gid]
                                            : rule.group_ids.filter((id) => id !== gid)
                                        })
                                      }
                                      className="h-3 w-3 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                                    />
                                    <span className={`font-medium ${platformTextClass(section.platform)}`}>{getGroupNameById(gid)}</span>
                                  </label>
                                ))}
                              </div>
                              {section.group_ids.length === 0 && (
                                <p className="mt-1 text-xs surface-muted">{t('admin.channels.form.noGroupsInChannel')}</p>
                              )}
                            </div>

                            <div>
                              <label className="text-xs surface-muted">{t('admin.channels.form.ruleAccounts')}</label>
                              <div className="mt-1 flex flex-wrap gap-1">
                                {rule.account_ids.map((accountId) => (
                                  <span
                                    key={accountId}
                                    className="inline-flex items-center gap-1 rounded-md border border-primary-300 bg-primary-50/[0.15] px-2 py-0.5 text-xs dark:border-primary-700 dark:bg-primary-900/20"
                                  >
                                    <span className={`font-medium ${platformTextClass(section.platform)}`}>{getRuleAccountLabel(accountId)}</span>
                                    <button
                                      type="button"
                                      onClick={() => removeRuleAccount(rule, accountId, sIdx, ruleIndex)}
                                      className="hover:text-red-500 surface-control surface-muted"
                                    >
                                      <X className="h-3 w-3" />
                                    </button>
                                  </span>
                                ))}
                              </div>
                              <div className="relative mt-1 rule-account-search-container">
                                <Input
                                  value={ruleAccountSearchKeyword[`${section.platform}-${ruleIndex}`] ?? ''}
                                  onValueChange={(value) => onRuleAccountSearchInput(section.platform, ruleIndex, value)}
                                  onFocus={() => onRuleAccountSearchFocus(section.platform, ruleIndex)}
                                  type="text"
                                  className="text-sm"
                                  placeholder={t('admin.channels.form.searchAccountPlaceholder')}
                                />
                                {showRuleAccountDropdown[`${section.platform}-${ruleIndex}`] &&
                                  (ruleAccountSearchResults[`${section.platform}-${ruleIndex}`]?.length ?? 0) > 0 && (
                                    <div className="absolute z-50 mt-1 max-h-48 w-full overflow-auto rounded-lg border shadow-lg dark:border-dark-600 surface-panel">
                                      {ruleAccountSearchResults[`${section.platform}-${ruleIndex}`].map((account) => (
                                        <button
                                          key={account.id}
                                          type="button"
                                          onClick={() => selectRuleAccount(rule, account, section.platform, ruleIndex, sIdx)}
                                          className={`w-full px-3 py-2 text-left text-sm hover:surface-hover surface-control ${
                                            rule.account_ids.includes(account.id) ? 'opacity-50' : ''
                                          }`}
                                          disabled={rule.account_ids.includes(account.id)}
                                        >
                                          <span className={platformTextClass(account.platform)}>{account.name}</span>
                                          <span className="ml-2 text-xs surface-muted">#{account.id}</span>
                                        </button>
                                      ))}
                                    </div>
                                  )}
                              </div>
                              <p className="mt-1 text-xs surface-muted">{t('admin.channels.form.ruleAccountsHint')}</p>
                            </div>

                            <div>
                              <div className="mb-1 flex items-center justify-between">
                                <label className="text-xs surface-muted">{t('admin.channels.form.ruleModelPricing')}</label>
                                <button
                                  type="button"
                                  onClick={() => addRulePricingEntry(sIdx, ruleIndex)}
                                  className="text-xs text-primary-600 hover:text-primary-700 surface-control"
                                >
                                  + {t('common.add')}
                                </button>
                              </div>
                              {rule.pricing.length === 0 ? (
                                <div className="rounded border border-dashed border-gray-300 p-2 text-center text-xs dark:border-dark-500 surface-muted">
                                  {t('admin.channels.form.noPricingRules')}
                                </div>
                              ) : (
                                <div className="space-y-2">
                                  {rule.pricing.map((entry, pIdx) => (
                                    <PricingEntryCard
                                      key={`rule-pricing-${pIdx}`}
                                      entry={entry}
                                      platform={section.platform}
                                      onUpdate={(updated) => updateRulePricingEntry(sIdx, ruleIndex, pIdx, updated)}
                                      onRemove={() => removeRulePricingEntry(sIdx, ruleIndex, pIdx)}
                                      onSplit={(models) => splitRulePricingEntry(section.platform, ruleIndex, sIdx, pIdx, models)}
                                    />
                                  ))}
                                </div>
                              )}
                            </div>
                          </div>
                        ))}
                      </div>
                    </>
                  )}
                </div>
              )
            })}
          </div>
        </div>
      </Modal>

      <Modal
        open={showDeleteDialog}
        onClose={() => setShowDeleteDialog(false)}
        title={t('admin.channels.deleteChannel', 'Delete Channel')}
        size="sm"
        footer={
          <>
            <Button variant="ghost" onClick={() => setShowDeleteDialog(false)}>
              {t('common.cancel', 'Cancel')}
            </Button>
            <Button variant="danger" onClick={confirmDelete}>
              {t('common.delete', 'Delete')}
            </Button>
          </>
        }
      >
        <div className="text-sm text-gray-600 dark:text-gray-300">{deleteConfirmMessage}</div>
      </Modal>
    </>
  )
}

function X({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
      <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
    </svg>
  )
}

/**
 * Source-model key of a mapping row. The key itself is the identity, so edits
 * are drafted locally and only committed (renamed) on blur.
 */
function MappingSourceInput({
  srcModel,
  className,
  placeholder,
  onRename
}: {
  srcModel: string
  className?: string
  placeholder?: string
  onRename: (newKey: string) => void
}) {
  const [draft, setDraft] = useState(srcModel)
  useEffect(() => {
    setDraft(srcModel)
  }, [srcModel])

  return (
    <Input
      value={draft}
      type="text"
      className={className}
      placeholder={placeholder}
      onValueChange={setDraft}
      onBlur={() => onRename(draft)}
    />
  )
}
