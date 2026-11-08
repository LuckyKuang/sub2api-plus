import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { adminAPI } from '@/api/admin'
import type {
  ContentModerationAPIKeyLoad,
  ContentModerationAPIKeyStatus,
  ContentModerationConfig,
  ContentModerationEndpoint,
  ContentModerationEndpointStatus,
  ContentModerationLog,
  ContentModerationModelFilter,
  ContentModerationModelFilterType,
  ContentModerationRuntimeStatus,
  ContentModerationSessionBlock,
  ContentModerationTestAuditResult,
  KeywordBlockingMode,
  ModerationMode,
  UpdateContentModerationConfig
} from '@/api/admin/riskControl'
import type { AdminGroup, Proxy } from '@/types'
import {
  Button,
  ConfirmDialog,
  Modal,
  Pagination,
  Select,
  Toggle,
  toast,
  type SelectOption
} from '@/components/ui'
import { ProxySelector } from '@/components/ProxySelector'
import { Icon } from '@/components/Icon'
import { WorkspaceTabs } from '@/components/WorkspaceTabs'
import { formatDateTime as formatDateTimeValue } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'
import { DateTimePicker } from './parts/DateTimePicker'
import { ModelWhitelistSelector } from './parts/ModelWhitelistSelector'

type SettingsTab =
  | 'basic'
  | 'scope'
  | 'runtime'
  | 'response'
  | 'riskThresholds'
  | 'retention'
  | 'keywords'
  | 'sessionBlocks'
type WorkerSlotState = 'active' | 'idle' | 'disabled'
type APIKeysWriteMode = 'append' | 'replace'
type OverviewIcon = 'shield' | 'key' | 'users' | 'document'

interface OverviewItem {
  key: string
  label: string
  value: string
  meta: string
  icon: OverviewIcon
  iconClass: string
  badge?: string
  badgeClass?: string
}

interface ModerationScoreRow {
  category: string
  score: number
  threshold: number
  hit: boolean
}

interface RiskThresholdRow {
  category: string
  value: number
  defaultValue: number
}

type ModerationEndpointDraft = ContentModerationEndpoint & { api_keys_text: string; persisted: boolean }

interface ConfigFormState {
  enabled: boolean
  mode: ModerationMode
  base_url: string
  model: string
  proxy_id: number | null
  api_keys_text: string
  api_key_configured: boolean
  api_key_masked: string
  api_key_count: number
  api_key_masks: string[]
  api_key_statuses: ContentModerationAPIKeyStatus[]
  endpoints: ModerationEndpointDraft[]
  api_keys_mode: APIKeysWriteMode
  clear_api_key: boolean
  timeout_ms: number
  retry_count: number
  sample_rate: number
  all_groups: boolean
  group_ids: number[]
  record_non_hits: boolean
  worker_count: number
  queue_size: number
  block_status: number
  block_message: string
  email_on_hit: boolean
  auto_ban_enabled: boolean
  cyber_policy_auto_ban_enabled: boolean
  cyber_policy_exclude_from_ban_count: boolean
  ban_threshold: number
  violation_window_hours: number
  hit_retention_days: number
  non_hit_retention_days: number
  pre_hash_check_enabled: boolean
  session_block_enabled: boolean
  session_block_ttl_seconds: number
  thresholds: Record<string, number>
  blocked_keywords_text: string
  keyword_blocking_mode: KeywordBlockingMode
  text_api_mode: ContentModerationConfig['text_api_mode']
  model_filter_type: ContentModerationModelFilterType
  model_filter_models: string[]
}

interface KeywordNoticeView {
  title: string
  description: string
  icon: 'infoCircle' | 'exclamationTriangle'
  toneClass: string
  iconClass: string
  titleClass: string
}

const maxModerationTestImages = 16
const maxModerationTestImageSize = 8 * 1024 * 1024
const maxVisibleApiKeyRows = 3
const blockedKeywordMax = 10000
const riskThresholdDefaults: Record<string, number> = {
  harassment: 98,
  'harassment/threatening': 90,
  hate: 65,
  'hate/threatening': 65,
  illicit: 95,
  'illicit/violent': 95,
  'self-harm': 65,
  'self-harm/intent': 85,
  'self-harm/instructions': 65,
  sexual: 65,
  'sexual/minors': 65,
  violence: 95,
  'violence/graphic': 95
}
const riskThresholdCategories = Object.keys(riskThresholdDefaults)

const keywordNoticeTones = {
  info: {
    icon: 'infoCircle' as const,
    toneClass: 'border-primary-100 bg-primary-50/60 dark:border-primary-900/40 dark:bg-primary-900/10',
    iconClass: 'mt-0.5 flex-shrink-0 text-primary-500 dark:text-primary-300',
    titleClass: 'text-primary-700 dark:text-primary-200'
  },
  warning: {
    icon: 'exclamationTriangle' as const,
    toneClass: 'border-amber-200 bg-amber-50/[0.15] dark:border-amber-900/40 dark:bg-amber-900/20',
    iconClass: 'mt-0.5 flex-shrink-0 text-amber-500 dark:text-amber-300',
    titleClass: 'text-amber-700 dark:text-amber-200'
  }
}

const statusLabelByValue: Record<ContentModerationAPIKeyStatus['status'], string> = {
  ok: 'admin.riskControl.apiKeyStatusOk',
  error: 'admin.riskControl.apiKeyStatusError',
  frozen: 'admin.riskControl.apiKeyStatusFrozen',
  unknown: 'admin.riskControl.apiKeyStatusUnknown'
}

const statusBadgeClassByValue: Record<ContentModerationAPIKeyStatus['status'], string> = {
  ok: 'bg-emerald-50/[0.15] text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300',
  error: 'bg-amber-50/[0.15] text-amber-700 dark:bg-amber-900/20 dark:text-amber-300',
  frozen: 'bg-red-50/[0.15] text-red-700 dark:bg-red-900/20 dark:text-red-300',
  unknown: 'surface-inset surface-muted dark:surface-inset'
}

const statusDotClassByValue: Record<ContentModerationAPIKeyStatus['status'], string> = {
  ok: 'bg-emerald-500',
  error: 'bg-amber-500',
  frozen: 'bg-red-500',
  unknown: 'bg-gray-400'
}

function clampPercent(value: unknown): number {
  const numeric = Number(value)
  if (!Number.isFinite(numeric)) return 0
  return Math.min(100, Math.max(0, numeric))
}

function parseApiKeys(value: string): string[] {
  return value
    .split(/\r?\n/)
    .map((item) => item.trim())
    .filter((item, index, arr) => item && arr.indexOf(item) === index)
}

function parseBlockedKeywords(value: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const line of value.split(/\r?\n/)) {
    const keyword = line.trim()
    if (!keyword) continue
    const key = keyword.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    out.push(keyword)
  }
  return out
}

function normalizeKeywordBlockingMode(value: unknown): KeywordBlockingMode {
  if (value === 'keyword_only' || value === 'api_only' || value === 'keyword_and_api') return value
  return 'keyword_and_api'
}

function normalizeModelFilterType(value: unknown): ContentModerationModelFilterType {
  if (value === 'include' || value === 'exclude' || value === 'all') return value
  return 'all'
}

function normalizeModelNames(models: unknown): string[] {
  if (!Array.isArray(models)) return []
  const seen = new Set<string>()
  const out: string[] = []
  for (const item of models) {
    const model = String(item ?? '').trim()
    if (!model) continue
    const key = model.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    out.push(model)
  }
  return out
}

function normalizeModelFilter(value: unknown): ContentModerationModelFilter {
  if (!value || typeof value !== 'object') return { type: 'all', models: [] }
  const raw = value as Partial<ContentModerationModelFilter>
  const type = normalizeModelFilterType(raw.type)
  const models = type === 'all' ? [] : normalizeModelNames(raw.models)
  return { type, models }
}

function riskThresholdsFromConfig(thresholds: Record<string, number> | null | undefined): Record<string, number> {
  const out: Record<string, number> = { ...riskThresholdDefaults }
  for (const category of riskThresholdCategories) {
    const value = thresholds?.[category]
    if (Number.isFinite(value)) {
      out[category] = clampPercent(Number(value) * 100)
    }
  }
  return out
}

function normalizeDateTimeLocal(value: string): string | undefined {
  if (!value) return undefined
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return undefined
  return date.toISOString()
}

function fileToDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result || ''))
    reader.onerror = () => reject(reader.error)
    reader.readAsDataURL(file)
  })
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value)
}

export default function RiskControlView() {
  const { t } = useTranslation()
  const showError = useCallback((message: string) => toast.error(message), [])
  const showSuccess = useCallback((message: string) => toast.success(message), [])

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [logsLoading, setLogsLoading] = useState(false)
  const [statusLoading, setStatusLoading] = useState(false)
  const [apiKeyTesting, setApiKeyTesting] = useState(false)
  const [endpointTestingID, setEndpointTestingID] = useState<string | null>(null)
  const [hashActionLoading, setHashActionLoading] = useState(false)
  const [unbanningUserID, setUnbanningUserID] = useState<number | null>(null)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [policyHelpOpen, setPolicyHelpOpen] = useState(false)
  const [activeSettingsTab, setActiveSettingsTab] = useState<SettingsTab>('basic')
  const [activePageTab, setActivePageTab] = useState('overview')
  const [groupSearch, setGroupSearch] = useState('')
  const [flaggedHashInput, setFlaggedHashInput] = useState('')
  const [sessionActionLoading, setSessionActionLoading] = useState(false)
  const [sessionBlocksLoading, setSessionBlocksLoading] = useState(false)
  const [sessionBlocks, setSessionBlocks] = useState<ContentModerationSessionBlock[]>([])
  const [sessionFilters, setSessionFilters] = useState({ search: '' })
  const [sessionPagination, setSessionPagination] = useState({ page: 1, page_size: 20, total: 0, pages: 1 })
  const [groups, setGroups] = useState<AdminGroup[]>([])
  const [proxies, setProxies] = useState<Proxy[]>([])
  const [logs, setLogs] = useState<ContentModerationLog[]>([])
  const [status, setStatus] = useState<ContentModerationRuntimeStatus | null>(null)
  const [testedApiKeyStatuses, setTestedApiKeyStatuses] = useState<ContentModerationAPIKeyStatus[]>([])
  const [pendingDeleteApiKeyHashes, setPendingDeleteApiKeyHashes] = useState<string[]>([])
  const [apiKeyRowsExpanded, setApiKeyRowsExpanded] = useState(false)
  const [moderationTestPrompt, setModerationTestPrompt] = useState('')
  const [moderationTestImages, setModerationTestImages] = useState<string[]>([])
  const [moderationTestResult, setModerationTestResult] = useState<ContentModerationTestAuditResult | null>(null)
  const [inputDetailRow, setInputDetailRow] = useState<ContentModerationLog | null>(null)
  const [clearHashesConfirmOpen, setClearHashesConfirmOpen] = useState(false)
  const [clearSessionsConfirmOpen, setClearSessionsConfirmOpen] = useState(false)
  const sessionPaginationRef = useRef(sessionPagination)
  sessionPaginationRef.current = sessionPagination

  const defaultBlockMessage = useCallback(() => t('admin.riskControl.defaultBlockMessage'), [t])

  const [configForm, setConfigForm] = useState<ConfigFormState>(() => ({
    enabled: false,
    mode: 'pre_block',
    base_url: 'https://api.openai.com',
    model: 'omni-moderation-latest',
    proxy_id: null,
    api_keys_text: '',
    api_key_configured: false,
    api_key_masked: '',
    api_key_count: 0,
    api_key_masks: [],
    api_key_statuses: [],
    endpoints: [],
    api_keys_mode: 'append',
    clear_api_key: false,
    timeout_ms: 3000,
    retry_count: 2,
    sample_rate: 100,
    all_groups: true,
    group_ids: [],
    record_non_hits: false,
    worker_count: 4,
    queue_size: 32768,
    block_status: 403,
    block_message: defaultBlockMessage(),
    email_on_hit: true,
    auto_ban_enabled: true,
    cyber_policy_auto_ban_enabled: false,
    cyber_policy_exclude_from_ban_count: false,
    ban_threshold: 10,
    violation_window_hours: 720,
    hit_retention_days: 180,
    non_hit_retention_days: 3,
    pre_hash_check_enabled: false,
    session_block_enabled: false,
    session_block_ttl_seconds: 2592000,
    thresholds: { ...riskThresholdDefaults },
    blocked_keywords_text: '',
    keyword_blocking_mode: 'keyword_and_api',
    text_api_mode: 'blocking',
    model_filter_type: 'all',
    model_filter_models: []
  }))
  // Saving reads the live form without re-creating the callback on every edit.
  const configFormRef = useRef(configForm)
  configFormRef.current = configForm

  const [pagination, setPagination] = useState({ page: 1, page_size: 20, total: 0, pages: 1 })
  const paginationRef = useRef(pagination)
  paginationRef.current = pagination
  const [filters, setFilters] = useState({
    result: '',
    group_id: 0,
    endpoint: '',
    search: '',
    from: '',
    to: ''
  })
  const filtersRef = useRef(filters)
  filtersRef.current = filters

  const patchConfig = useCallback((partial: Partial<ConfigFormState>) => {
    setConfigForm((prev) => ({ ...prev, ...partial }))
  }, [])

  const patchFilters = useCallback((partial: Partial<typeof filters>) => {
    setFilters((prev) => ({ ...prev, ...partial }))
  }, [])

  const pageTabs = useMemo(
    () => [
      { value: 'overview', label: t('workspace.riskControl'), panelId: 'risk-overview-panel' },
      { value: 'records', label: t('workspace.auditRecords'), panelId: 'risk-records-panel' }
    ],
    [t]
  )

  const settingsTabs = useMemo<Array<{ id: SettingsTab; label: string }>>(
    () => [
      { id: 'basic', label: t('admin.riskControl.tabs.basic') },
      { id: 'scope', label: t('admin.riskControl.tabs.scope') },
      { id: 'runtime', label: t('admin.riskControl.tabs.runtime') },
      { id: 'response', label: t('admin.riskControl.tabs.response') },
      { id: 'riskThresholds', label: t('admin.riskControl.tabs.riskThresholds') },
      { id: 'keywords', label: t('admin.riskControl.tabs.keywords') },
      { id: 'sessionBlocks', label: t('admin.riskControl.tabs.sessionBlocks') },
      { id: 'retention', label: t('admin.riskControl.tabs.retention') }
    ],
    [t]
  )

  const modeOptions = useMemo<SelectOption[]>(
    () => [
      { value: 'pre_block', label: t('admin.riskControl.modePreBlock') },
      { value: 'observe', label: t('admin.riskControl.modeObserve') },
      { value: 'off', label: t('admin.riskControl.modeOff') }
    ],
    [t]
  )

  const modeLabel = useCallback(
    (mode: ModerationMode): string => {
      const found = modeOptions.find((option) => option.value === mode)
      return found?.label ?? mode
    },
    [modeOptions]
  )

  const modeDescription = useCallback(
    (mode: ModerationMode): string => {
      const descriptions: Record<ModerationMode, string> = {
        pre_block: t('admin.riskControl.modePreBlockDesc'),
        observe: t('admin.riskControl.modeObserveDesc'),
        off: t('admin.riskControl.modeOffDesc')
      }
      return descriptions[mode] ?? ''
    },
    [t]
  )

  const textAPIModeOptions = useMemo<SelectOption[]>(
    () => [
      {
        value: 'auto',
        label: t(
          configForm.mode === 'observe'
            ? 'admin.riskControl.textApiModes.autoObserve'
            : 'admin.riskControl.textApiModes.auto'
        )
      },
      {
        value: 'blocking',
        label: t(
          configForm.mode === 'observe'
            ? 'admin.riskControl.textApiModes.normalObserve'
            : 'admin.riskControl.textApiModes.blocking'
        )
      },
      { value: 'observe', label: t('admin.riskControl.textApiModes.observe') },
      { value: 'off', label: t('admin.riskControl.textApiModes.off') }
    ],
    [t, configForm.mode]
  )

  const effectiveTextPolicyDescription = useMemo(() => {
    const mode = configForm.mode === 'off' ? 'off' : configForm.mode
    const policy = mode === 'off' ? 'any' : configForm.text_api_mode
    return t(`admin.riskControl.policyHelp.combinations.${mode}.${policy}`)
  }, [configForm.mode, configForm.text_api_mode, t])

  const policyMatrixRows = useMemo(() => {
    const combinations = [
      ['pre_block', 'auto', 'depends'],
      ['pre_block', 'blocking', 'yes'],
      ['pre_block', 'observe', 'textNoImageYes'],
      ['pre_block', 'off', 'textNo'],
      ['observe', 'auto', 'no'],
      ['observe', 'blocking', 'no'],
      ['observe', 'observe', 'no'],
      ['observe', 'off', 'no'],
      ['off', 'any', 'no']
    ] as const
    return combinations.map(([mode, policy, block]) => ({
      mode,
      policy,
      modeLabel: t(`admin.riskControl.policyHelp.modeLabels.${mode}`),
      policyLabel: t(`admin.riskControl.policyHelp.policyLabels.${policy}`),
      behavior: t(`admin.riskControl.policyHelp.combinations.${mode}.${policy}`),
      canBlock: t(`admin.riskControl.policyHelp.blockLabels.${block}`)
    }))
  }, [t])

  const keywordBlockingModeOptions = useMemo(
    () => [
      {
        value: 'keyword_and_api' as KeywordBlockingMode,
        label: t('admin.riskControl.keywordModeKeywordAndApi'),
        description: t('admin.riskControl.keywordModeKeywordAndApiDesc')
      },
      {
        value: 'keyword_only' as KeywordBlockingMode,
        label: t('admin.riskControl.keywordModeKeywordOnly'),
        description: t('admin.riskControl.keywordModeKeywordOnlyDesc')
      },
      {
        value: 'api_only' as KeywordBlockingMode,
        label: t('admin.riskControl.keywordModeApiOnly'),
        description: t('admin.riskControl.keywordModeApiOnlyDesc')
      }
    ],
    [t]
  )

  const modelFilterOptions = useMemo(
    () => [
      {
        value: 'all' as ContentModerationModelFilterType,
        label: t('admin.riskControl.modelFilterAll'),
        description: t('admin.riskControl.modelFilterAllDesc')
      },
      {
        value: 'include' as ContentModerationModelFilterType,
        label: t('admin.riskControl.modelFilterInclude'),
        description: t('admin.riskControl.modelFilterIncludeDesc')
      },
      {
        value: 'exclude' as ContentModerationModelFilterType,
        label: t('admin.riskControl.modelFilterExclude'),
        description: t('admin.riskControl.modelFilterExcludeDesc')
      }
    ],
    [t]
  )

  const keywordNotice = useMemo<KeywordNoticeView>(() => {
    const strategy = configForm.keyword_blocking_mode
    if (strategy === 'api_only') {
      return {
        ...keywordNoticeTones.info,
        title: t('admin.riskControl.keywordModeApiOnlyNotice'),
        description: t('admin.riskControl.keywordModeApiOnlyDesc')
      }
    }
    if (configForm.mode !== 'pre_block') {
      return {
        ...keywordNoticeTones.warning,
        title: t('admin.riskControl.blockedKeywordsModeWarning', { mode: modeLabel(configForm.mode) }),
        description: t('admin.riskControl.blockedKeywordsDescription')
      }
    }
    if (strategy === 'keyword_only') {
      return {
        ...keywordNoticeTones.info,
        title: t('admin.riskControl.keywordModeKeywordOnlyNotice'),
        description: t('admin.riskControl.keywordModeKeywordOnlyDesc')
      }
    }
    return {
      ...keywordNoticeTones.info,
      title: t('admin.riskControl.blockedKeywordsPreBlockHint'),
      description: t('admin.riskControl.blockedKeywordsDescription')
    }
  }, [configForm.keyword_blocking_mode, configForm.mode, modeLabel, t])

  const resultOptions = useMemo<SelectOption[]>(
    () => [
      { value: '', label: t('admin.riskControl.result.all') },
      { value: 'hit', label: t('admin.riskControl.result.hit') },
      { value: 'blocked', label: t('admin.riskControl.result.blocked') },
      { value: 'pass', label: t('admin.riskControl.result.pass') },
      { value: 'error', label: t('admin.riskControl.result.error') }
    ],
    [t]
  )

  const endpointOptions = useMemo<SelectOption[]>(
    () => [
      { value: '', label: t('admin.riskControl.filters.allEndpoints') },
      { value: '/v1/messages', label: '/v1/messages' },
      { value: '/v1/responses', label: '/v1/responses' },
      { value: '/v1/chat/completions', label: '/v1/chat/completions' },
      { value: '/v1beta/models', label: '/v1beta/models' },
      { value: '/v1/images/generations', label: '/v1/images/generations' },
      { value: '/v1/images/edits', label: '/v1/images/edits' }
    ],
    [t]
  )

  const groupFilterOptions = useMemo<SelectOption[]>(
    () => [
      { value: 0, label: t('admin.riskControl.filters.allGroups') },
      ...groups.map((group) => ({ value: group.id, label: `${group.name} (${group.platform})` }))
    ],
    [groups, t]
  )

  const modelFilterModelCount = configForm.model_filter_models.length

  const modelFilterSummary = useMemo(() => {
    if (configForm.model_filter_type === 'include') {
      return t('admin.riskControl.modelFilterIncludeSummary', { count: modelFilterModelCount })
    }
    if (configForm.model_filter_type === 'exclude') {
      return t('admin.riskControl.modelFilterExcludeSummary', { count: modelFilterModelCount })
    }
    return t('admin.riskControl.modelFilterAllSummary')
  }, [configForm.model_filter_type, modelFilterModelCount, t])

  const modelFilterPreviewModels = configForm.model_filter_models.slice(0, 6)
  const hiddenModelFilterModelCount = Math.max(
    0,
    configForm.model_filter_models.length - modelFilterPreviewModels.length
  )

  const filteredGroups = useMemo(() => {
    const keyword = groupSearch.trim().toLowerCase()
    if (!keyword) return groups
    return groups.filter((group) => {
      return group.name.toLowerCase().includes(keyword) || String(group.platform).toLowerCase().includes(keyword)
    })
  }, [groups, groupSearch])

  const inputApiKeyCount = parseApiKeys(configForm.api_keys_text).length
  const blockedKeywordList = parseBlockedKeywords(configForm.blocked_keywords_text)
  const blockedKeywordCount = blockedKeywordList.length
  const pendingDeletedApiKeyCount = pendingDeleteApiKeyHashes.length
  const effectiveStoredApiKeyCount = Math.max(0, configForm.api_key_count - pendingDeletedApiKeyCount)

  const apiKeysPlaceholder =
    configForm.api_keys_mode === 'replace'
      ? t('admin.riskControl.apiKeysPlaceholderReplace')
      : t('admin.riskControl.apiKeysPlaceholder')

  const apiKeysModeHint =
    configForm.api_keys_mode === 'replace'
      ? t('admin.riskControl.apiKeysModeReplaceHint')
      : t('admin.riskControl.apiKeysModeAppendHint')

  const hasModerationAuditInput = moderationTestPrompt.trim() !== '' || moderationTestImages.length > 0
  const isFlaggedHashInputValid = /^[a-fA-F0-9]{64}$/.test(flaggedHashInput.trim())

  const storedApiKeyTestButtonText = useMemo(() => {
    if (apiKeyTesting) return t('admin.riskControl.testingApiKeys')
    if (hasModerationAuditInput) return t('admin.riskControl.testContentWithStoredApiKey')
    return t('admin.riskControl.testStoredApiKeys')
  }, [apiKeyTesting, hasModerationAuditInput, t])

  const savedApiKeyRows = useMemo<ContentModerationAPIKeyStatus[]>(() => {
    const rows = status?.api_key_statuses?.length ? status.api_key_statuses : configForm.api_key_statuses
    return Array.isArray(rows) ? rows : []
  }, [status, configForm.api_key_statuses])

  const apiKeyRows = useMemo<ContentModerationAPIKeyStatus[]>(
    () => [...savedApiKeyRows, ...testedApiKeyStatuses],
    [savedApiKeyRows, testedApiKeyStatuses]
  )

  const visibleApiKeyRows = useMemo<ContentModerationAPIKeyStatus[]>(
    () => (apiKeyRowsExpanded ? apiKeyRows : apiKeyRows.slice(0, maxVisibleApiKeyRows)),
    [apiKeyRows, apiKeyRowsExpanded]
  )

  const hiddenApiKeyRowCount = Math.max(0, apiKeyRows.length - visibleApiKeyRows.length)
  const canToggleApiKeyRows = apiKeyRows.length > maxVisibleApiKeyRows

  const isStoredApiKeyPendingDelete = useCallback(
    (row: ContentModerationAPIKeyStatus): boolean =>
      row.configured && row.key_hash !== '' && pendingDeleteApiKeyHashes.includes(row.key_hash),
    [pendingDeleteApiKeyHashes]
  )

  const activeSavedApiKeyRows = useMemo(
    () => savedApiKeyRows.filter((row) => !isStoredApiKeyPendingDelete(row)),
    [savedApiKeyRows, isStoredApiKeyPendingDelete]
  )

  const apiKeyHealthBadges = useMemo(() => {
    const counts: Record<ContentModerationAPIKeyStatus['status'], number> = {
      ok: 0,
      error: 0,
      frozen: 0,
      unknown: 0
    }
    for (const row of activeSavedApiKeyRows) {
      counts[row.status] = (counts[row.status] ?? 0) + 1
    }
    if (activeSavedApiKeyRows.length === 0 && effectiveStoredApiKeyCount > 0) {
      counts.unknown = effectiveStoredApiKeyCount
    }
    return (['ok', 'frozen', 'error', 'unknown'] as Array<ContentModerationAPIKeyStatus['status']>)
      .map((item) => ({ status: item, count: counts[item] }))
      .filter((item) => item.count > 0)
  }, [activeSavedApiKeyRows, effectiveStoredApiKeyCount])

  const apiKeyHealthSummary = useMemo(() => {
    if (!configForm.api_key_configured) return ''
    if (apiKeyHealthBadges.length === 0) return t('admin.riskControl.apiKeyStatusUnknown')
    return apiKeyHealthBadges.map((badge) => `${t(statusLabelByValue[badge.status])} ${badge.count}`).join(' · ')
  }, [configForm.api_key_configured, apiKeyHealthBadges, t])

  const runtimeBadgeText = useMemo(() => {
    if (!status?.risk_control_enabled) return t('admin.riskControl.riskSwitchOff')
    if (!configForm.enabled || configForm.mode === 'off') return t('admin.riskControl.overview.disabled')
    return t('admin.riskControl.overview.enabled')
  }, [status, configForm.enabled, configForm.mode, t])

  const runtimeBadgeClass = useMemo(() => {
    if (!status?.risk_control_enabled || !configForm.enabled || configForm.mode === 'off') {
      return 'surface-inset surface-muted dark:surface-inset'
    }
    return 'bg-emerald-50/[0.15] text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'
  }, [status, configForm.enabled, configForm.mode])

  const overviewItems = useMemo<OverviewItem[]>(
    () => [
      {
        key: 'status',
        label: t('admin.riskControl.overview.status'),
        value: configForm.enabled ? t('admin.riskControl.overview.enabled') : t('admin.riskControl.overview.disabled'),
        meta: modeLabel(configForm.mode),
        icon: 'shield',
        iconClass: configForm.enabled
          ? 'bg-emerald-50/[0.15] text-emerald-600 dark:bg-emerald-900/20 dark:text-emerald-300'
          : 'surface-inset surface-muted dark:surface-inset',
        badge: runtimeBadgeText,
        badgeClass: runtimeBadgeClass
      },
      {
        key: 'api-key',
        label: t('admin.riskControl.overview.apiKey'),
        value: configForm.api_key_configured
          ? t('admin.riskControl.apiKeyCount', { count: configForm.api_key_count })
          : t('admin.riskControl.notConfigured'),
        meta: configForm.api_key_configured ? apiKeyHealthSummary || configForm.model || '-' : configForm.model || '-',
        icon: 'key',
        iconClass: 'bg-sky-50/[0.15] text-sky-600 dark:bg-sky-900/20 dark:text-sky-300'
      },
      {
        key: 'scope',
        label: t('admin.riskControl.overview.groupScope'),
        value: configForm.all_groups ? t('admin.riskControl.allGroups') : String(configForm.group_ids.length),
        meta: modelFilterSummary,
        icon: 'users',
        iconClass: 'bg-violet-50/[0.15] text-violet-600 dark:bg-violet-900/20 dark:text-violet-300'
      },
      {
        key: 'logs',
        label: t('admin.riskControl.overview.logs'),
        value: formatNumber(pagination.total),
        meta: t('admin.riskControl.overview.currentFilter'),
        icon: 'document',
        iconClass: 'bg-amber-50/[0.15] text-amber-600 dark:bg-amber-900/20 dark:text-amber-300'
      }
    ],
    [
      t,
      configForm.enabled,
      configForm.mode,
      configForm.api_key_configured,
      configForm.api_key_count,
      configForm.model,
      configForm.all_groups,
      configForm.group_ids.length,
      apiKeyHealthSummary,
      modelFilterSummary,
      modeLabel,
      pagination.total,
      runtimeBadgeText,
      runtimeBadgeClass
    ]
  )

  const moderationScoreRows = useMemo<ModerationScoreRow[]>(() => {
    const result = moderationTestResult
    if (!result) return []
    return Object.entries(result.category_scores || {})
      .map(([category, score]) => {
        const threshold = result.thresholds?.[category] ?? 1
        return { category, score, threshold, hit: score >= threshold }
      })
      .sort((a, b) => b.score - a.score)
  }, [moderationTestResult])

  const riskThresholdRows = useMemo<RiskThresholdRow[]>(
    () =>
      riskThresholdCategories.map((category) => ({
        category,
        value: configForm.thresholds[category] ?? riskThresholdDefaults[category],
        defaultValue: riskThresholdDefaults[category]
      })),
    [configForm.thresholds]
  )

  const inputDetailText = useMemo(() => {
    if (!inputDetailRow) return '-'
    return inputDetailRow.input_content || inputDetailRow.input_excerpt || inputDetailRow.error || '-'
  }, [inputDetailRow])

  const queueUsagePercent = `${Math.min(100, Math.max(0, status?.queue_usage_percent ?? 0)).toFixed(1)}%`
  const queueUsageStyle = { width: queueUsagePercent }

  const runtimeMode: ModerationMode = status?.mode ?? configForm.mode
  const showPreBlockRuntimeCard = runtimeMode === 'pre_block'
  const showWorkerRuntimeCard = runtimeMode === 'observe'

  const extractionMetricItems = useMemo(
    () => [
      {
        key: 'attempted',
        label: t('admin.riskControl.extractionAttempted'),
        value: formatNumber(status?.extraction_attempted ?? 0),
        valueClass: 'text-gray-900 dark:text-white'
      },
      {
        key: 'succeeded',
        label: t('admin.riskControl.extractionSucceeded'),
        value: formatNumber(status?.extraction_succeeded ?? 0),
        valueClass: 'text-emerald-700 dark:text-emerald-300'
      },
      {
        key: 'empty',
        label: t('admin.riskControl.extractionEmpty'),
        value: formatNumber(status?.extraction_empty ?? 0),
        valueClass: 'text-gray-900 dark:text-white'
      },
      {
        key: 'failed',
        label: t('admin.riskControl.extractionFailed'),
        value: formatNumber(status?.extraction_failed ?? 0),
        valueClass: (status?.extraction_failed ?? 0) > 0 ? 'text-red-700 dark:text-red-300' : 'text-gray-900 dark:text-white'
      }
    ],
    [status, t]
  )

  const preBlockMetricItems = useMemo(
    () => [
      {
        key: 'active',
        label: t('admin.riskControl.preBlockActive'),
        value: formatNumber(status?.pre_block_active ?? 0),
        meta: t('admin.riskControl.preBlockActiveHint'),
        class: 'bg-sky-50/[0.15] dark:bg-sky-900/10',
        valueClass: 'text-sky-700 dark:text-sky-300'
      },
      {
        key: 'checked',
        label: t('admin.riskControl.preBlockChecked'),
        value: formatNumber(status?.pre_block_checked ?? 0),
        meta: t('admin.riskControl.preBlockCheckedHint'),
        class: 'surface-inset dark:surface-inset',
        valueClass: 'text-gray-900 dark:text-white'
      },
      {
        key: 'allowed',
        label: t('admin.riskControl.preBlockAllowed'),
        value: formatNumber(status?.pre_block_allowed ?? 0),
        meta: t('admin.riskControl.preBlockAllowedHint'),
        class: 'bg-emerald-50/[0.15] dark:bg-emerald-900/10',
        valueClass: 'text-emerald-700 dark:text-emerald-300'
      },
      {
        key: 'blocked',
        label: t('admin.riskControl.preBlockBlocked'),
        value: formatNumber(status?.pre_block_blocked ?? 0),
        meta: t('admin.riskControl.preBlockBlockedHint'),
        class: 'bg-rose-50/[0.15] dark:bg-rose-900/10',
        valueClass: 'text-rose-700 dark:text-rose-300'
      },
      {
        key: 'errors',
        label: t('admin.riskControl.preBlockErrors'),
        value: formatNumber(status?.pre_block_errors ?? 0),
        meta: t('admin.riskControl.preBlockErrorsHint'),
        class: 'bg-amber-50/[0.15] dark:bg-amber-900/10',
        valueClass: 'text-amber-700 dark:text-amber-300'
      },
      {
        key: 'latency',
        label: t('admin.riskControl.preBlockAvgLatency'),
        value: `${formatNumber(status?.pre_block_avg_latency_ms ?? 0)} ms`,
        meta: t('admin.riskControl.preBlockAvgLatencyHint'),
        class: 'bg-violet-50/[0.15] dark:bg-violet-900/10',
        valueClass: 'text-violet-700 dark:text-violet-300'
      }
    ],
    [status, t]
  )

  const preBlockAPIKeyLoads = useMemo<ContentModerationAPIKeyLoad[]>(
    () => [...(status?.pre_block_api_key_loads ?? [])].sort((a, b) => a.index - b.index),
    [status]
  )

  const preBlockAPIKeyMaxTotal = Math.max(1, ...preBlockAPIKeyLoads.map((item) => item.total || 0))

  const preBlockAPIKeyLoadSummaryText = t('admin.riskControl.preBlockAPIKeyLoadSummary', {
    active: formatNumber(status?.pre_block_api_key_active ?? 0),
    available: formatNumber(status?.pre_block_api_key_available_count ?? 0),
    total: formatNumber(status?.pre_block_api_key_total_calls ?? 0),
    workerActive: formatNumber(status?.active_workers ?? 0),
    workerTotal: formatNumber(status?.worker_count ?? configForm.worker_count)
  })

  const preBlockAPIKeyLoadWidth = (total: number): string =>
    `${Math.min(100, Math.max(0, (total / preBlockAPIKeyMaxTotal) * 100)).toFixed(1)}%`

  const workerSlots = useMemo(() => {
    const total = Math.max(0, status?.worker_count ?? configForm.worker_count)
    const active = Math.max(0, status?.active_workers ?? 0)
    const enabled = Boolean(status?.risk_control_enabled && status?.enabled && status?.mode !== 'off')
    return Array.from({ length: total }, (_, index) => ({
      id: index + 1,
      state: (!enabled ? 'disabled' : index < active ? 'active' : 'idle') as WorkerSlotState,
      label: !enabled
        ? t('admin.riskControl.workerDisabled')
        : index < active
          ? t('admin.riskControl.workerActive')
          : t('admin.riskControl.workerIdle')
    }))
  }, [status, configForm.worker_count, t])

  const applyConfig = useCallback(
    (config: ContentModerationConfig) => {
      setConfigForm({
        enabled: config.enabled,
        mode: config.mode,
        base_url: config.base_url || 'https://api.openai.com',
        model: config.model || 'omni-moderation-latest',
        proxy_id: config.proxy_id || null,
        api_keys_text: '',
        api_key_configured: config.api_key_configured,
        api_key_masked: config.api_key_masked || '',
        api_key_count: config.api_key_count || 0,
        api_key_masks: Array.isArray(config.api_key_masks) ? [...config.api_key_masks] : [],
        api_key_statuses: Array.isArray(config.api_key_statuses) ? [...config.api_key_statuses] : [],
        endpoints: (Array.isArray(config.endpoints) ? config.endpoints : []).map((endpoint) => ({
          ...endpoint,
          proxy_id: endpoint.proxy_id ?? null,
          api_key_masks: [...(endpoint.api_key_masks || [])],
          api_key_statuses: [...(endpoint.api_key_statuses || [])],
          api_keys_text: '',
          persisted: true
        })),
        api_keys_mode: 'append',
        clear_api_key: false,
        timeout_ms: config.timeout_ms || 3000,
        retry_count: config.retry_count ?? 2,
        sample_rate: config.sample_rate ?? 100,
        all_groups: config.all_groups,
        group_ids: Array.isArray(config.group_ids) ? [...config.group_ids] : [],
        record_non_hits: config.record_non_hits,
        worker_count: config.worker_count || 4,
        queue_size: config.queue_size || 32768,
        block_status: config.block_status || 403,
        block_message: config.block_message || defaultBlockMessage(),
        email_on_hit: config.email_on_hit ?? true,
        auto_ban_enabled: config.auto_ban_enabled ?? true,
        cyber_policy_auto_ban_enabled: config.cyber_policy_auto_ban_enabled ?? false,
        cyber_policy_exclude_from_ban_count: config.cyber_policy_exclude_from_ban_count ?? false,
        ban_threshold: config.ban_threshold || 10,
        violation_window_hours: config.violation_window_hours || 720,
        hit_retention_days: config.hit_retention_days || 180,
        non_hit_retention_days: Math.min(Math.max(config.non_hit_retention_days || 3, 1), 3),
        pre_hash_check_enabled: config.pre_hash_check_enabled ?? false,
        session_block_enabled: config.session_block_enabled ?? false,
        session_block_ttl_seconds: config.session_block_ttl_seconds || 2592000,
        thresholds: riskThresholdsFromConfig(config.thresholds),
        blocked_keywords_text: Array.isArray(config.blocked_keywords) ? config.blocked_keywords.join('\n') : '',
        keyword_blocking_mode: normalizeKeywordBlockingMode(config.keyword_blocking_mode),
        text_api_mode: config.text_api_mode || 'blocking',
        model_filter_type: normalizeModelFilter(config.model_filter).type,
        model_filter_models: normalizeModelFilter(config.model_filter).models
      })
      setPendingDeleteApiKeyHashes([])
      setTestedApiKeyStatuses([])
      setApiKeyRowsExpanded(false)
    },
    [defaultBlockMessage]
  )

  const prunePendingDeleteAPIKeyHashes = useCallback(() => {
    const currentHashes = new Set(savedApiKeyRows.map((row) => row.key_hash).filter(Boolean))
    setPendingDeleteApiKeyHashes((prev) => prev.filter((hash) => currentHashes.has(hash)))
  }, [savedApiKeyRows])

  const mergeEndpointRuntime = useCallback((endpoints: ContentModerationEndpoint[] | undefined) => {
    if (!Array.isArray(endpoints)) return
    const runtimeByID = new Map(endpoints.map((endpoint) => [endpoint.id, endpoint]))
    setConfigForm((prev) => ({
      ...prev,
      endpoints: prev.endpoints.map((endpoint) => {
        const latest = runtimeByID.get(endpoint.id)
        return latest
          ? { ...endpoint, runtime: latest.runtime, manual_paused: latest.manual_paused, enabled: latest.enabled }
          : endpoint
      })
    }))
  }, [])

  const loadStatus = useCallback(
    async (silent = true) => {
      setStatusLoading(true)
      try {
        const runtimeStatus = await adminAPI.riskControl.getStatus()
        setStatus(runtimeStatus)
        if (Array.isArray(runtimeStatus.api_key_statuses)) {
          setConfigForm((prev) => ({ ...prev, api_key_statuses: [...runtimeStatus.api_key_statuses] }))
          prunePendingDeleteAPIKeyHashes()
        }
        mergeEndpointRuntime(runtimeStatus.endpoints)
      } catch (err: unknown) {
        if (!silent) {
          showError(extractApiErrorMessage(err, t('admin.riskControl.statusFailed')))
        }
      } finally {
        setStatusLoading(false)
      }
    },
    [prunePendingDeleteAPIKeyHashes, mergeEndpointRuntime, showError, t]
  )

  const loadLogs = useCallback(async () => {
    setLogsLoading(true)
    try {
      const current = filtersRef.current
      const result = await adminAPI.riskControl.listLogs({
        page: paginationRef.current.page,
        page_size: paginationRef.current.page_size,
        result: current.result || undefined,
        group_id: current.group_id || undefined,
        endpoint: current.endpoint || undefined,
        search: current.search || undefined,
        from: normalizeDateTimeLocal(current.from),
        to: normalizeDateTimeLocal(current.to)
      })
      setLogs(result.items)
      setPagination({ page: result.page, page_size: result.page_size, total: result.total, pages: result.pages })
    } catch (err: unknown) {
      showError(extractApiErrorMessage(err, t('admin.riskControl.logsFailed')))
    } finally {
      setLogsLoading(false)
    }
  }, [showError, t])

  const loadSessionBlocks = useCallback(async () => {
    setSessionBlocksLoading(true)
    try {
      const result = await adminAPI.riskControl.listSessionBlocks({
        page: sessionPaginationRef.current.page,
        page_size: sessionPaginationRef.current.page_size,
        search: sessionFilters.search || undefined
      })
      setSessionBlocks(result.items || [])
      setSessionPagination((prev) => ({
        ...prev,
        total: result.total,
        pages: result.pages,
        page: result.page,
        page_size: result.page_size
      }))
    } catch (err: unknown) {
      showError(extractApiErrorMessage(err, t('admin.riskControl.sessionBlockDeleteFailed')))
    } finally {
      setSessionBlocksLoading(false)
    }
  }, [sessionFilters.search, showError, t])

  const loadAll = useCallback(async () => {
    setLoading(true)
    try {
      const [config, groupItems, runtimeStatus, proxyItems] = await Promise.all([
        adminAPI.riskControl.getConfig(),
        adminAPI.groups.getAll(),
        adminAPI.riskControl.getStatus(),
        // 代理列表加载失败不阻塞风控页面（仅影响下拉可选项）
        adminAPI.proxies.getAll().catch(() => [] as Proxy[])
      ])
      applyConfig(config)
      setGroups(groupItems)
      setStatus(runtimeStatus)
      setProxies(proxyItems)
      if (Array.isArray(runtimeStatus.api_key_statuses)) {
        setConfigForm((prev) => ({ ...prev, api_key_statuses: [...runtimeStatus.api_key_statuses] }))
        prunePendingDeleteAPIKeyHashes()
      }
      mergeEndpointRuntime(runtimeStatus.endpoints)
      await Promise.all([loadLogs(), loadSessionBlocks()])
    } catch (err: unknown) {
      showError(extractApiErrorMessage(err, t('admin.riskControl.loadFailed')))
    } finally {
      setLoading(false)
    }
  }, [applyConfig, prunePendingDeleteAPIKeyHashes, mergeEndpointRuntime, loadLogs, loadSessionBlocks, showError, t])

  useEffect(() => {
    void loadAll()
    const timer = window.setInterval(() => {
      void loadStatus(true)
    }, 15000)
    return () => window.clearInterval(timer)
  }, [loadAll, loadStatus])

  const buildModelFilterPayload = (): ContentModerationModelFilter => {
    const type = normalizeModelFilterType(configForm.model_filter_type)
    if (type === 'all') return { type: 'all', models: [] }
    return { type, models: normalizeModelNames(configForm.model_filter_models) }
  }

  const buildRiskThresholdPayload = (): Record<string, number> => {
    const payload: Record<string, number> = {}
    for (const category of riskThresholdCategories) {
      payload[category] = Number((clampPercent(configFormRef.current.thresholds[category]) / 100).toFixed(4))
    }
    return payload
  }

  const saveConfig = async () => {
    setSaving(true)
    try {
      const modelFilterPayload = buildModelFilterPayload()
      if (modelFilterPayload.type !== 'all' && modelFilterPayload.models.length === 0) {
        showError(t('admin.riskControl.modelFilterModelsRequired'))
        return
      }
      const payload: UpdateContentModerationConfig = {
        enabled: configForm.enabled,
        mode: configForm.mode,
        base_url: configForm.base_url,
        model: configForm.model,
        // 后端语义：0 清除代理（直连），>0 指定代理
        proxy_id: configForm.proxy_id ?? 0,
        timeout_ms: Number(configForm.timeout_ms) || 3000,
        retry_count: Number(configForm.retry_count) || 0,
        sample_rate: Number(configForm.sample_rate) || 0,
        all_groups: configForm.all_groups,
        group_ids: configForm.all_groups ? [] : [...configForm.group_ids],
        record_non_hits: configForm.record_non_hits,
        clear_api_key: configForm.clear_api_key,
        worker_count: Number(configForm.worker_count) || 4,
        queue_size: Number(configForm.queue_size) || 32768,
        block_status: Number(configForm.block_status) || 403,
        block_message: configForm.block_message || defaultBlockMessage(),
        email_on_hit: configForm.email_on_hit,
        auto_ban_enabled: configForm.auto_ban_enabled,
        cyber_policy_auto_ban_enabled: configForm.cyber_policy_auto_ban_enabled,
        cyber_policy_exclude_from_ban_count: configForm.cyber_policy_exclude_from_ban_count,
        ban_threshold: Number(configForm.ban_threshold) || 10,
        violation_window_hours: Number(configForm.violation_window_hours) || 720,
        hit_retention_days: Number(configForm.hit_retention_days) || 180,
        non_hit_retention_days: Math.min(Math.max(Number(configForm.non_hit_retention_days) || 3, 1), 3),
        pre_hash_check_enabled: configForm.pre_hash_check_enabled,
        session_block_enabled: configForm.session_block_enabled,
        session_block_ttl_seconds: Number(configForm.session_block_ttl_seconds) || 2592000,
        thresholds: buildRiskThresholdPayload(),
        blocked_keywords: blockedKeywordList,
        keyword_blocking_mode: configForm.keyword_blocking_mode,
        text_api_mode: configForm.text_api_mode,
        model_filter: modelFilterPayload,
        endpoints: configForm.endpoints.map((endpoint, index) => ({
          id: endpoint.id,
          name: endpoint.name,
          enabled: endpoint.enabled,
          priority: index + 1,
          base_url: endpoint.base_url,
          model: endpoint.model,
          proxy_id: endpoint.proxy_id ?? null,
          api_keys: parseApiKeys(endpoint.api_keys_text),
          timeout_ms: Number(endpoint.timeout_ms) || 3000,
          cooldown_seconds: Number(endpoint.cooldown_seconds) || 60,
          failure_threshold: Number(endpoint.failure_threshold) || 1,
          manual_paused: endpoint.manual_paused
        }))
      }
      const keys = parseApiKeys(configForm.api_keys_text)
      if (!payload.clear_api_key && configForm.api_keys_mode === 'replace' && keys.length === 0) {
        showError(t('admin.riskControl.apiKeysReplaceNoInput'))
        return
      }
      if (keys.length > 0) {
        payload.api_keys = keys
        payload.api_keys_mode = configForm.api_keys_mode
        payload.clear_api_key = false
      }
      if (!payload.clear_api_key && configForm.api_keys_mode !== 'replace' && pendingDeleteApiKeyHashes.length > 0) {
        payload.delete_api_key_hashes = [...pendingDeleteApiKeyHashes]
      }

      const updated = await adminAPI.riskControl.updateConfig(payload)
      applyConfig(updated)
      setSettingsOpen(false)
      showSuccess(t('admin.riskControl.saved'))
      await Promise.all([loadStatus(true), loadLogs()])
    } catch (err: unknown) {
      showError(extractApiErrorMessage(err, t('admin.riskControl.saveFailed')))
    } finally {
      setSaving(false)
    }
  }

  const canUnbanRow = (row: ContentModerationLog): boolean =>
    Boolean(row.auto_banned && row.user_id && row.user_status === 'disabled')

  const inputSummaryText = (row: ContentModerationLog): string => row.input_excerpt || row.error || '-'

  const unbanUser = async (row: ContentModerationLog) => {
    if (!row.user_id || unbanningUserID !== null) return
    setUnbanningUserID(row.user_id)
    try {
      const result = await adminAPI.riskControl.unbanUser(row.user_id)
      setLogs((prev) =>
        prev.map((item) => (item.user_id !== row.user_id ? item : { ...item, user_status: result.status }))
      )
      showSuccess(t('admin.riskControl.unbanSuccess'))
    } catch (err: unknown) {
      showError(extractApiErrorMessage(err, t('admin.riskControl.unbanFailed')))
    } finally {
      setUnbanningUserID(null)
    }
  }

  const deleteFlaggedHash = async () => {
    if (!isFlaggedHashInputValid || hashActionLoading) return
    setHashActionLoading(true)
    try {
      const result = await adminAPI.riskControl.deleteFlaggedHash(flaggedHashInput)
      setFlaggedHashInput('')
      await loadStatus(true)
      showSuccess(result.deleted ? t('admin.riskControl.flaggedHashDeleted') : t('admin.riskControl.flaggedHashNotFound'))
    } catch (err: unknown) {
      showError(extractApiErrorMessage(err, t('admin.riskControl.flaggedHashDeleteFailed')))
    } finally {
      setHashActionLoading(false)
    }
  }

  const clearFlaggedHashes = async () => {
    if (hashActionLoading) return
    setHashActionLoading(true)
    try {
      const result = await adminAPI.riskControl.clearFlaggedHashes()
      await loadStatus(true)
      showSuccess(t('admin.riskControl.flaggedHashesCleared', { count: result.deleted }))
    } catch (err: unknown) {
      showError(extractApiErrorMessage(err, t('admin.riskControl.flaggedHashesClearFailed')))
    } finally {
      setHashActionLoading(false)
    }
  }

  const reloadSessionBlocksFromFirstPage = () => {
    setSessionPagination((prev) => ({ ...prev, page: 1 }))
    void loadSessionBlocks()
  }

  const onSessionPageChange = (page: number) => {
    setSessionPagination((prev) => ({ ...prev, page }))
    void loadSessionBlocks()
  }

  const onSessionPageSizeChange = (pageSize: number) => {
    setSessionPagination((prev) => ({ ...prev, page_size: pageSize, page: 1 }))
    void loadSessionBlocks()
  }

  const deleteSessionBlock = async (blockKey: string) => {
    if (!blockKey || sessionActionLoading) return
    setSessionActionLoading(true)
    try {
      const result = await adminAPI.riskControl.deleteSessionBlock(blockKey)
      await loadSessionBlocks()
      await loadStatus(true)
      showSuccess(
        result.deleted
          ? t('admin.riskControl.sessionBlockDeleted', { count: 1 })
          : t('admin.riskControl.sessionBlockNotFound')
      )
    } catch (err: unknown) {
      showError(extractApiErrorMessage(err, t('admin.riskControl.sessionBlockDeleteFailed')))
    } finally {
      setSessionActionLoading(false)
    }
  }

  const clearSessionBlocks = async () => {
    if (sessionActionLoading) return
    setSessionActionLoading(true)
    try {
      const result = await adminAPI.riskControl.clearSessionBlocks()
      await loadSessionBlocks()
      await loadStatus(true)
      showSuccess(t('admin.riskControl.sessionBlocksCleared', { count: result.deleted }))
    } catch (err: unknown) {
      showError(extractApiErrorMessage(err, t('admin.riskControl.sessionBlocksClearFailed')))
    } finally {
      setSessionActionLoading(false)
    }
  }

  const openSettings = () => {
    setActiveSettingsTab('basic')
    setSettingsOpen(true)
    void loadSessionBlocks()
  }

  const addModerationEndpoint = () => {
    const id = `endpoint-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`
    setConfigForm((prev) => ({
      ...prev,
      endpoints: [
        ...prev.endpoints,
        {
          id,
          name: t('admin.riskControl.endpointPool.defaultName', { index: prev.endpoints.length + 1 }),
          enabled: true,
          priority: prev.endpoints.length + 1,
          base_url: 'https://api.openai.com',
          model: 'omni-moderation-latest',
          proxy_id: null,
          api_key_configured: false,
          api_key_count: 0,
          api_key_masks: [],
          api_key_statuses: [],
          timeout_ms: 3000,
          cooldown_seconds: 60,
          failure_threshold: 1,
          manual_paused: false,
          runtime: { status: 'healthy', failure_count: 0, last_error: '', half_open: false },
          api_keys_text: '',
          persisted: false
        }
      ]
    }))
  }

  const removeModerationEndpoint = (index: number) => {
    setConfigForm((prev) => {
      if (prev.endpoints.length <= 1) return prev
      return { ...prev, endpoints: prev.endpoints.filter((_, item) => item !== index) }
    })
  }

  const moveModerationEndpoint = (index: number, direction: -1 | 1) => {
    setConfigForm((prev) => {
      const next = index + direction
      if (next < 0 || next >= prev.endpoints.length) return prev
      const endpoints = [...prev.endpoints]
      const [endpoint] = endpoints.splice(index, 1)
      if (endpoint) endpoints.splice(next, 0, endpoint)
      return { ...prev, endpoints }
    })
  }

  const patchEndpoint = (index: number, partial: Partial<ModerationEndpointDraft>) => {
    setConfigForm((prev) => ({
      ...prev,
      endpoints: prev.endpoints.map((endpoint, item) => (item === index ? { ...endpoint, ...partial } : endpoint))
    }))
  }

  const effectiveEndpointStatus = (endpoint: ModerationEndpointDraft): ContentModerationEndpointStatus =>
    endpoint.manual_paused ? 'manual_pause' : endpoint.runtime.status

  const endpointStatusLabel = (endpoint: ModerationEndpointDraft): string =>
    t(`admin.riskControl.endpointPool.status.${effectiveEndpointStatus(endpoint)}`)

  const endpointStatusBadgeClass = (endpoint: ModerationEndpointDraft): string => {
    const status = effectiveEndpointStatus(endpoint)
    if (status === 'healthy') return 'bg-emerald-50/[0.15] text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    if (status === 'error') return 'bg-red-50/[0.15] text-red-700 dark:bg-red-900/30 dark:text-red-300'
    if (status === 'disabled') return 'surface-inset surface-muted dark:surface-inset'
    return 'bg-amber-50/[0.15] text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
  }

  const endpointStatusDotClass = (endpoint: ModerationEndpointDraft): string => {
    const status = effectiveEndpointStatus(endpoint)
    if (status === 'healthy') return 'bg-emerald-500'
    if (status === 'error') return 'bg-red-500'
    if (status === 'disabled') return 'bg-gray-400'
    return 'bg-amber-500'
  }

  const testModerationEndpoint = async (endpoint: ModerationEndpointDraft) => {
    const keys = parseApiKeys(endpoint.api_keys_text)
    if (!endpoint.api_key_configured && keys.length === 0) {
      showError(t('admin.riskControl.endpointPool.keysRequired'))
      return
    }
    setEndpointTestingID(endpoint.id)
    try {
      await adminAPI.riskControl.testAPIKeys({
        endpoint_id: endpoint.persisted ? endpoint.id : undefined,
        api_keys: keys,
        base_url: endpoint.base_url,
        model: endpoint.model,
        timeout_ms: Number(endpoint.timeout_ms) || 3000,
        proxy_id: endpoint.proxy_id ?? 0,
        prompt: moderationTestPrompt,
        images: moderationTestImages
      })
      showSuccess(t('admin.riskControl.endpointPool.testSuccess', { name: endpoint.name }))
    } catch (err: unknown) {
      showError(extractApiErrorMessage(err, t('admin.riskControl.endpointPool.testFailed')))
    } finally {
      setEndpointTestingID(null)
    }
  }

  const toggleEndpointPause = async (endpoint: ModerationEndpointDraft) => {
    if (!endpoint.persisted) {
      const index = configForm.endpoints.findIndex((item) => item.id === endpoint.id)
      if (index >= 0) patchEndpoint(index, { manual_paused: !endpoint.manual_paused })
      return
    }
    try {
      const updated = await adminAPI.riskControl.setEndpointPaused(endpoint.id, !endpoint.manual_paused)
      applyConfig(updated)
      showSuccess(
        t(endpoint.manual_paused ? 'admin.riskControl.endpointPool.resumed' : 'admin.riskControl.endpointPool.paused')
      )
    } catch (err: unknown) {
      showError(extractApiErrorMessage(err, t('admin.riskControl.endpointPool.pauseFailed')))
    }
  }

  const reloadLogsFromFirstPage = () => {
    setPagination((prev) => ({ ...prev, page: 1 }))
    void loadLogs()
  }

  const onPageChange = (page: number) => {
    setPagination((prev) => ({ ...prev, page }))
    void loadLogs()
  }

  const onPageSizeChange = (pageSize: number) => {
    setPagination((prev) => ({ ...prev, page: 1, page_size: pageSize }))
    void loadLogs()
  }

  const toggleClearApiKey = () => {
    const nextClear = !configForm.clear_api_key
    patchConfig({ clear_api_key: nextClear })
    if (nextClear) {
      patchConfig({ api_keys_text: '', api_keys_mode: 'append' })
      setTestedApiKeyStatuses([])
      setPendingDeleteApiKeyHashes([])
    }
  }

  const setAPIKeysMode = (mode: APIKeysWriteMode) => {
    patchConfig({ api_keys_mode: mode })
    if (mode === 'replace') {
      setPendingDeleteApiKeyHashes([])
    }
  }

  const setModelFilterType = (type: ContentModerationModelFilterType) => {
    patchConfig({ model_filter_type: type })
    if (type === 'all') {
      patchConfig({ model_filter_models: [] })
    }
  }

  const mergeConfiguredAPIKeyStatuses = (items: ContentModerationAPIKeyStatus[]) => {
    if (!hasModerationAuditInput || configFormRef.current.api_key_statuses.length === 0) {
      setConfigForm((prev) => ({ ...prev, api_key_statuses: items }))
      return
    }
    const updates = new Map(items.map((item) => [item.key_hash, item]))
    setConfigForm((prev) => ({
      ...prev,
      api_key_statuses: prev.api_key_statuses.map((item) => updates.get(item.key_hash) ?? item)
    }))
  }

  const testApiKeys = async (useInputKeys: boolean) => {
    const keys = useInputKeys ? parseApiKeys(configForm.api_keys_text) : []
    if (useInputKeys && keys.length === 0) {
      showError(t('admin.riskControl.apiKeyTestNoInput'))
      return
    }
    setApiKeyTesting(true)
    try {
      const result = await adminAPI.riskControl.testAPIKeys({
        api_keys: keys,
        base_url: configForm.base_url,
        model: configForm.model,
        timeout_ms: Number(configForm.timeout_ms) || 3000,
        // 与保存语义一致：0 强制直连，>0 指定代理，确保测试与实际审计走同一条链路
        proxy_id: configForm.proxy_id ?? 0,
        prompt: moderationTestPrompt,
        images: moderationTestImages
      })
      setModerationTestResult(result.audit_result ?? null)
      if (useInputKeys) {
        setTestedApiKeyStatuses(result.items.map((item) => ({ ...item, configured: false })))
      } else {
        setTestedApiKeyStatuses([])
        mergeConfiguredAPIKeyStatuses(result.items)
        await loadStatus(true)
      }
      showSuccess(t('admin.riskControl.apiKeyTestDone', { count: result.items.length }))
    } catch (err: unknown) {
      showError(extractApiErrorMessage(err, t('admin.riskControl.apiKeyTestFailed')))
    } finally {
      setApiKeyTesting(false)
    }
  }

  const toggleDeleteStoredApiKey = (row: ContentModerationAPIKeyStatus) => {
    if (!row.configured || !row.key_hash) return
    setPendingDeleteApiKeyHashes((prev) => {
      const index = prev.indexOf(row.key_hash)
      if (index >= 0) {
        return prev.filter((_, item) => item !== index)
      }
      return [...prev, row.key_hash]
    })
  }

  const clearModerationTestInput = () => {
    setModerationTestPrompt('')
    setModerationTestImages([])
    setModerationTestResult(null)
  }

  const removeModerationTestImage = (index: number) => {
    setModerationTestImages((prev) => prev.filter((_, item) => item !== index))
  }

  const addModerationTestFiles = async (files: FileList | File[] | null) => {
    if (!files) return
    const items = Array.from(files).filter((file) => file.type.startsWith('image/'))
    for (const file of items) {
      if (moderationTestImages.length >= maxModerationTestImages) {
        showError(t('admin.riskControl.auditTestImageLimit', { count: maxModerationTestImages }))
        return
      }
      if (file.size > maxModerationTestImageSize) {
        showError(t('admin.riskControl.auditTestImageTooLarge'))
        continue
      }
      try {
        const dataUrl = await fileToDataURL(file)
        setModerationTestImages((prev) => [...prev, dataUrl])
      } catch {
        showError(t('admin.riskControl.auditTestImageReadFailed'))
      }
    }
  }

  const handleModerationImageUpload = async (event: React.ChangeEvent<HTMLInputElement>) => {
    await addModerationTestFiles(event.target.files)
    event.target.value = ''
  }

  const handleModerationImageDrop = async (event: React.DragEvent<HTMLDivElement>) => {
    event.preventDefault()
    await addModerationTestFiles(event.dataTransfer?.files ?? null)
  }

  const handleModerationImagePaste = async (event: React.ClipboardEvent<HTMLDivElement>) => {
    const files = Array.from(event.clipboardData?.files ?? []).filter((file) => file.type.startsWith('image/'))
    if (files.length === 0) return
    event.preventDefault()
    await addModerationTestFiles(files)
  }

  const toggleGroup = (groupID: number) => {
    setConfigForm((prev) => ({
      ...prev,
      group_ids: prev.group_ids.includes(groupID)
        ? prev.group_ids.filter((item) => item !== groupID)
        : [...prev.group_ids, groupID]
    }))
  }

  const isGroupSelected = (groupID: number): boolean => configForm.group_ids.includes(groupID)

  const resultLabel = (row: ContentModerationLog): string => {
    if (row.action === 'cyber_policy') return t('admin.riskControl.action.cyberPolicy')
    if (row.action === 'keyword_block') return t('admin.riskControl.action.keywordBlock')
    if (row.action === 'hash_block') return t('admin.riskControl.action.hashBlock')
    if (row.action === 'session_block') return t('admin.riskControl.action.sessionBlock')
    if (row.action === 'shadow') return t('admin.riskControl.action.shadow')
    if (row.action === 'block') return t('admin.riskControl.action.block')
    if (row.action === 'error' || row.error) return t('admin.riskControl.action.error')
    if (row.flagged) return t('admin.riskControl.result.hit')
    return t('admin.riskControl.result.pass')
  }

  const resultBadgeClass = (row: ContentModerationLog): string => {
    if (
      row.action === 'block' ||
      row.action === 'keyword_block' ||
      row.action === 'hash_block' ||
      row.action === 'session_block' ||
      row.action === 'cyber_policy'
    ) {
      return 'bg-red-100/[0.15] text-red-700 dark:bg-red-900/30 dark:text-red-300'
    }
    if (row.action === 'error' || row.error) return 'bg-amber-100/[0.15] text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    if (row.flagged) return 'bg-pink-100/[0.15] text-pink-700 dark:bg-pink-900/30 dark:text-pink-300'
    return 'bg-green-100/[0.15] text-green-700 dark:bg-green-900/30 dark:text-green-300'
  }

  const workerSlotClass = (state: WorkerSlotState): string => {
    if (state === 'active') {
      return 'border-sky-200 bg-sky-50/[0.15] text-sky-700 dark:border-sky-900/60 dark:bg-sky-900/20 dark:text-sky-300'
    }
    if (state === 'idle') {
      return 'border-emerald-200 bg-emerald-50/[0.15] text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-900/20 dark:text-emerald-300'
    }
    return 'border-gray-100 surface-inset surface-muted dark:border-dark-700 dark:surface-inset'
  }

  const workerDotClass = (state: WorkerSlotState): string => {
    if (state === 'active') return 'bg-sky-500'
    if (state === 'idle') return 'bg-emerald-500'
    return 'bg-gray-300 dark:bg-dark-500'
  }

  const percent = (value: number): string => {
    if (!Number.isFinite(value)) return '-'
    return `${(value * 100).toFixed(1)}%`
  }

  const percentWidth = (value: number): string => {
    if (!Number.isFinite(value)) return '0%'
    return `${Math.min(100, Math.max(0, value * 100)).toFixed(1)}%`
  }

  const latencyText = (value: number | null): string => {
    if (value === null || value === undefined) return '-'
    return `${value} ms`
  }

  const apiKeyRowKey = (row: ContentModerationAPIKeyStatus, index: number): string =>
    `${row.configured ? 'saved' : 'test'}-${row.key_hash || index}`

  const apiKeyStatusLabel = (statusValue: ContentModerationAPIKeyStatus['status']): string =>
    t(statusLabelByValue[statusValue] ?? statusLabelByValue.unknown)

  const apiKeyStatusBadgeClass = (statusValue: ContentModerationAPIKeyStatus['status']): string =>
    statusBadgeClassByValue[statusValue] ?? statusBadgeClassByValue.unknown

  const apiKeyStatusDotClass = (statusValue: ContentModerationAPIKeyStatus['status']): string =>
    statusDotClassByValue[statusValue] ?? statusDotClassByValue.unknown

  const formatDateTime = (value: string): string => formatDateTimeValue(value) || '-'

  const apiKeyStatusMeta = (row: ContentModerationAPIKeyStatus): string => {
    const parts: string[] = []
    parts.push(t('admin.riskControl.apiKeyFailureCount', { count: row.failure_count || 0 }))
    if (row.last_latency_ms > 0) parts.push(t('admin.riskControl.apiKeyLatency', { ms: row.last_latency_ms }))
    if (row.last_http_status > 0) parts.push(t('admin.riskControl.apiKeyHTTPStatus', { status: row.last_http_status }))
    if (row.frozen_until) {
      parts.push(t('admin.riskControl.apiKeyFrozenUntil', { time: formatDateTime(row.frozen_until) }))
    } else if (row.last_checked_at) {
      parts.push(t('admin.riskControl.apiKeyLastChecked', { time: formatDateTime(row.last_checked_at) }))
    } else {
      parts.push(t('admin.riskControl.apiKeyNotTested'))
    }
    return parts.join(' / ')
  }

  const violationCountText = (row: ContentModerationLog): string => {
    if (!row.flagged) return '-'
    if (row.violation_count === 0) return t('admin.riskControl.violationNotCounted')
    return t('admin.riskControl.violationCount', { count: row.violation_count || 1 })
  }

  const resetRiskThresholds = () => {
    patchConfig({ thresholds: { ...riskThresholdDefaults } })
  }

  const formatThresholdPercent = (value: number): string => `${clampPercent(value).toFixed(1)}%`

  if (loading) {
    return (
      <div className="workspace-page">
        <div className="flex items-center justify-center py-16">
          <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600" />
        </div>
      </div>
    )
  }

  return (
    <div className="workspace-page">
      <div className="card workspace-fixed flex flex-col gap-3 px-6 py-4" data-testid="risk-page-heading">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 dark:text-white">{t('admin.riskControl.title')}</h1>
          <p className="mt-1 text-sm surface-muted">{t('admin.riskControl.description')}</p>
        </div>
        {/* Tabs live inside the header card so the page keeps one surface. */}
        <div className="w-full">
          <WorkspaceTabs
            value={activePageTab}
            tabs={pageTabs}
            label={t('admin.riskControl.title')}
            embedded
            onValueChange={setActivePageTab}
          />
        </div>
        <div className="flex w-full flex-wrap items-center justify-end gap-2" data-testid="risk-page-actions">
          <button
            type="button"
            className="btn btn-secondary inline-flex items-center gap-2"
            disabled={statusLoading}
            onClick={() => void loadStatus(false)}
          >
            <Icon name="refresh" size="sm" className={statusLoading ? 'animate-spin' : undefined} />
            {t('admin.riskControl.refreshStatus')}
          </button>
          <button type="button" className="btn btn-primary inline-flex items-center gap-2" onClick={openSettings}>
            <Icon name="cog" size="sm" />
            {t('admin.riskControl.openSettings')}
          </button>
        </div>
      </div>

      {activePageTab === 'overview' && (
        <div
          id="risk-overview-panel"
          role="tabpanel"
          aria-label={t('workspace.riskControl')}
          className="workspace-shell workspace-scroll"
          data-test="risk-overview"
        >
          <div className="workspace-stack space-y-6">
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4">
              {overviewItems.map((item) => (
                <div
                  key={item.key}
                  data-test={`risk-overview-${item.key}`}
                  className="risk-overview-card border border-gray-100 surface-inset px-4 py-3 shadow-sm dark:border-dark-700"
                >
                  <div className="flex min-w-0 items-center gap-3">
                    <div className={`flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-lg ${item.iconClass}`}>
                      <Icon name={item.icon} size="sm" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex min-w-0 items-center justify-between gap-2">
                        <p className="truncate text-xs font-medium surface-muted">{item.label}</p>
                        {item.badge && (
                          <span
                            className={`inline-flex flex-shrink-0 items-center rounded-full px-2 py-0.5 text-xs font-medium ${item.badgeClass}`}
                          >
                            {item.badge}
                          </span>
                        )}
                      </div>
                      <div className="mt-1 flex min-w-0 items-baseline gap-2">
                        <p className="truncate text-xl font-semibold leading-7 text-gray-900 dark:text-white">{item.value}</p>
                        {item.meta && <p className="truncate text-xs surface-muted">{item.meta}</p>}
                      </div>
                    </div>
                  </div>
                </div>
              ))}
            </div>

            <section data-test="content-extraction-runtime" className="card px-4 py-4">
              <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
                <div className="max-w-xl">
                  <h2 className="text-sm font-semibold text-gray-900 dark:text-white">
                    {t('admin.riskControl.extractionStatus')}
                  </h2>
                  <p className="mt-1 text-xs leading-5 surface-muted">{t('admin.riskControl.extractionStatusHint')}</p>
                </div>
                <dl className="grid grid-cols-2 gap-x-8 gap-y-3 sm:grid-cols-4">
                  {extractionMetricItems.map((item) => (
                    <div key={item.key} data-test={`content-extraction-metric-${item.key}`} className="min-w-[7rem]">
                      <dt className="text-xs surface-muted">{item.label}</dt>
                      <dd className={`mt-1 text-lg font-semibold tabular-nums ${item.valueClass}`}>{item.value}</dd>
                    </div>
                  ))}
                </dl>
              </div>
            </section>

            {showPreBlockRuntimeCard && (
              <div
                data-test="pre-block-runtime-cards"
                className="grid grid-cols-1 gap-6 xl:grid-cols-[minmax(0,520px)_minmax(0,1fr)]"
              >
                <div data-test="pre-block-sync-card" className="card">
                  <div className="flex flex-col gap-4 border-b border-gray-100 px-6 py-4 dark:border-dark-700 lg:flex-row lg:items-center lg:justify-between">
                    <div>
                      <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
                        {t('admin.riskControl.preBlockSyncStatus')}
                      </h2>
                      <p className="mt-1 text-sm surface-muted">{t('admin.riskControl.preBlockSyncHint')}</p>
                    </div>
                    <span className="inline-flex w-fit items-center rounded-full surface-inset px-2.5 py-1 text-xs font-medium surface-muted">
                      {modeLabel(status?.mode ?? configForm.mode)}
                    </span>
                  </div>

                  <div className="p-6">
                    <div data-test="pre-block-metric-grid" className="grid grid-cols-2 gap-3 md:grid-cols-3">
                      {preBlockMetricItems.map((item) => (
                        <div key={item.key} className={`rounded-lg p-4 ${item.class}`}>
                          <p className="text-xs surface-muted">{item.label}</p>
                          <p className={`mt-2 truncate text-2xl font-semibold leading-8 ${item.valueClass}`}>{item.value}</p>
                          {item.meta && <p className="mt-1 truncate text-xs surface-muted">{item.meta}</p>}
                        </div>
                      ))}
                    </div>
                  </div>
                </div>

                <div data-test="pre-block-api-key-load-card" className="card">
                  <div className="flex flex-col gap-4 border-b border-gray-100 px-6 py-4 dark:border-dark-700 lg:flex-row lg:items-center lg:justify-between">
                    <div>
                      <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
                        {t('admin.riskControl.preBlockAPIKeyLoad')}
                      </h2>
                      <p className="mt-1 text-sm surface-muted">{t('admin.riskControl.preBlockAPIKeyLoadHint')}</p>
                    </div>
                    <span className="inline-flex w-fit items-center rounded-full surface-inset px-2.5 py-1 text-xs font-medium surface-muted">
                      {preBlockAPIKeyLoadSummaryText}
                    </span>
                  </div>

                  <div className="p-6">
                    {preBlockAPIKeyLoads.length > 0 ? (
                      <div data-test="pre-block-api-key-load-list" className="max-h-[280px] space-y-3 overflow-y-auto pr-1">
                        {preBlockAPIKeyLoads.map((item) => (
                          <div key={item.key_hash || item.index} className="rounded-lg surface-inset p-3">
                            <div className="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                              <div className="min-w-0">
                                <div className="flex min-w-0 items-center gap-2">
                                  <span className="font-mono text-sm font-semibold text-gray-900 dark:text-white">
                                    #{item.index + 1}
                                  </span>
                                  <span className="truncate font-mono text-sm surface-muted">{item.masked || '-'}</span>
                                  <span
                                    className={`console-data-mark h-2 w-2 flex-shrink-0 rounded-full ${apiKeyStatusDotClass(item.status)}`}
                                  />
                                </div>
                                <p className="mt-1 text-xs surface-muted">
                                  {t('admin.riskControl.preBlockAPIKeyTotals', {
                                    total: formatNumber(item.total),
                                    success: formatNumber(item.success),
                                    errors: formatNumber(item.errors)
                                  })}
                                </p>
                              </div>
                              <div className="grid grid-cols-4 gap-2 text-right text-xs sm:min-w-[280px] surface-muted">
                                <div>
                                  <p>{t('admin.riskControl.preBlockKeyActiveShort')}</p>
                                  <p className="mt-1 text-sm font-semibold text-sky-700 dark:text-sky-300">
                                    {formatNumber(item.active)}
                                  </p>
                                </div>
                                <div>
                                  <p>{t('admin.riskControl.preBlockKeyTotalShort')}</p>
                                  <p className="mt-1 text-sm font-semibold text-gray-900 dark:text-white">
                                    {formatNumber(item.total)}
                                  </p>
                                </div>
                                <div>
                                  <p>{t('admin.riskControl.preBlockKeyAvgShort')}</p>
                                  <p className="mt-1 text-sm font-semibold text-gray-900 dark:text-white">
                                    {formatNumber(item.avg_latency_ms)} ms
                                  </p>
                                </div>
                                <div>
                                  <p>{t('admin.riskControl.preBlockKeyLastShort')}</p>
                                  <p className="mt-1 text-sm font-semibold text-gray-900 dark:text-white">
                                    {formatNumber(item.last_latency_ms)} ms
                                  </p>
                                </div>
                              </div>
                            </div>
                            <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-white dark:bg-dark-900">
                              <div
                                className="console-data-mark h-full rounded-full bg-sky-500"
                                style={{ width: preBlockAPIKeyLoadWidth(item.total) }}
                              />
                            </div>
                          </div>
                        ))}
                      </div>
                    ) : (
                      <p className="rounded-lg surface-inset p-4 text-sm surface-muted">
                        {t('admin.riskControl.preBlockAPIKeyLoadEmpty')}
                      </p>
                    )}
                  </div>
                </div>
              </div>
            )}

            {showWorkerRuntimeCard && (
              <div className="card">
                <div className="flex flex-col gap-4 border-b border-gray-100 px-6 py-4 dark:border-dark-700 lg:flex-row lg:items-center lg:justify-between">
                  <div>
                    <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
                      {t('admin.riskControl.workerStatus')}
                    </h2>
                    <p className="mt-1 text-sm surface-muted">{t('admin.riskControl.workerStatusHint')}</p>
                  </div>
                  <div className="flex flex-wrap items-center gap-2 text-sm surface-muted">
                    <span>{t('admin.riskControl.autoRefresh')}</span>
                    {status?.last_cleanup_at && (
                      <span>{t('admin.riskControl.lastCleanup', { time: formatDateTime(status.last_cleanup_at) })}</span>
                    )}
                  </div>
                </div>

                <div className="grid grid-cols-1 gap-6 p-6 xl:grid-cols-[minmax(0,360px)_1fr]">
                  <div className="space-y-4">
                    <div className="rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                      <div className="flex items-center justify-between gap-3">
                        <div>
                          <p className="text-sm font-medium text-gray-900 dark:text-white">{t('admin.riskControl.queueUsage')}</p>
                          <p className="mt-1 text-xs surface-muted">
                            {formatNumber(status?.queue_length ?? 0)} /{' '}
                            {formatNumber(status?.queue_size ?? configForm.queue_size)}
                          </p>
                        </div>
                        <span className="text-sm font-semibold text-gray-900 dark:text-white">{queueUsagePercent}</span>
                      </div>
                      <div className="mt-4 h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
                        <div
                          className="console-data-mark h-full rounded-full bg-primary-500 transition-all duration-300"
                          style={queueUsageStyle}
                        />
                      </div>
                    </div>

                    <div className="grid grid-cols-2 gap-3">
                      <div className="rounded-lg surface-inset p-4">
                        <p className="text-xs surface-muted">{t('admin.riskControl.activeWorkers')}</p>
                        <p className="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
                          {status?.active_workers ?? 0}
                        </p>
                      </div>
                      <div className="rounded-lg bg-emerald-50/[0.15] p-4 dark:bg-emerald-900/10">
                        <p className="text-xs surface-muted">{t('admin.riskControl.idleWorkers')}</p>
                        <p className="mt-2 text-2xl font-semibold text-emerald-700 dark:text-emerald-300">
                          {status?.idle_workers ?? configForm.worker_count}
                        </p>
                      </div>
                      <div className="rounded-lg surface-inset p-4">
                        <p className="text-xs surface-muted">{t('admin.riskControl.processed')}</p>
                        <p className="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
                          {formatNumber(status?.processed ?? 0)}
                        </p>
                      </div>
                      <div className="rounded-lg surface-inset p-4">
                        <p className="text-xs surface-muted">{t('admin.riskControl.droppedErrors')}</p>
                        <p className="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
                          {formatNumber((status?.dropped ?? 0) + (status?.errors ?? 0))}
                        </p>
                      </div>
                    </div>
                  </div>

                  <div>
                    <div className="mb-3 flex items-center justify-between gap-3">
                      <div>
                        <p className="text-sm font-medium text-gray-900 dark:text-white">{t('admin.riskControl.workerPool')}</p>
                        <p className="mt-1 text-xs surface-muted">
                          {t('admin.riskControl.workerPoolMeta', {
                            active: status?.active_workers ?? 0,
                            idle: status?.idle_workers ?? configForm.worker_count,
                            total: status?.worker_count ?? configForm.worker_count
                          })}
                        </p>
                      </div>
                      <span className="inline-flex items-center rounded-full surface-inset px-2.5 py-1 text-xs font-medium surface-muted">
                        {modeLabel(status?.mode ?? configForm.mode)}
                      </span>
                    </div>
                    <div className="grid grid-cols-2 gap-2 sm:grid-cols-4 md:grid-cols-6 xl:grid-cols-8 2xl:grid-cols-10">
                      {workerSlots.map((worker) => (
                        <div
                          key={worker.id}
                          className={`flex h-12 items-center justify-between rounded-lg border px-3 transition-colors ${workerSlotClass(worker.state)}`}
                          title={worker.label}
                        >
                          <span className="text-sm font-semibold">#{worker.id}</span>
                          <span className={`console-data-mark h-2.5 w-2.5 rounded-full ${workerDotClass(worker.state)}`} />
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {activePageTab === 'records' && (
        <div
          id="risk-records-panel"
          role="tabpanel"
          aria-label={t('workspace.auditRecords')}
          className="card workspace-table-card workspace-shell"
          data-test="risk-records"
        >
          <div className="risk-records-toolbar workspace-fixed flex flex-col gap-4 border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <div className="risk-records-heading flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
              <div className="min-w-0">
                <h2 className="text-lg font-semibold text-gray-900 dark:text-white">{t('admin.riskControl.records')}</h2>
                <p className="mt-1 text-sm surface-muted">{t('admin.riskControl.recordsHint')}</p>
              </div>
              <button
                type="button"
                className="btn btn-secondary inline-flex shrink-0 items-center gap-2"
                disabled={logsLoading}
                onClick={() => void loadLogs()}
              >
                <Icon name="refresh" size="sm" className={logsLoading ? 'animate-spin' : undefined} />
                {t('admin.riskControl.refresh')}
              </button>
            </div>

            <div className="flex flex-col gap-2 rounded-lg border border-gray-100 surface-inset px-3 py-2 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex min-w-0 items-center gap-2 text-sm surface-muted">
                <Icon name="filter" size="sm" className="flex-shrink-0 text-gray-400" />
                <span className="font-medium">{t('admin.riskControl.modelFilter')}</span>
                <span className="truncate surface-muted">{modelFilterSummary}</span>
              </div>
              {modelFilterPreviewModels.length > 0 && (
                <div className="flex flex-wrap gap-1.5">
                  {modelFilterPreviewModels.map((model) => (
                    <span
                      key={model}
                      className="inline-flex max-w-[180px] items-center truncate rounded-md surface-inset px-2 py-1 font-mono text-xs shadow-sm surface-muted"
                    >
                      {model}
                    </span>
                  ))}
                  {hiddenModelFilterModelCount > 0 && (
                    <span className="inline-flex rounded-md surface-inset px-2 py-1 text-xs shadow-sm surface-muted">
                      +{hiddenModelFilterModelCount}
                    </span>
                  )}
                </div>
              )}
            </div>

            <div
              className="risk-records-filters grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6"
              data-test="risk-records-filters"
            >
              <Select
                value={filters.result}
                options={resultOptions}
                onValueChange={(value) => {
                  patchFilters({ result: String(value ?? '') })
                  reloadLogsFromFirstPage()
                }}
              />
              <Select
                value={filters.group_id}
                options={groupFilterOptions}
                onValueChange={(value) => {
                  patchFilters({ group_id: Number(value ?? 0) })
                  reloadLogsFromFirstPage()
                }}
              />
              <Select
                value={filters.endpoint}
                options={endpointOptions}
                onValueChange={(value) => {
                  patchFilters({ endpoint: String(value ?? '') })
                  reloadLogsFromFirstPage()
                }}
              />
              <input
                value={filters.search}
                type="search"
                className="input"
                placeholder={t('admin.riskControl.filters.search')}
                onChange={(event) => patchFilters({ search: event.target.value })}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') reloadLogsFromFirstPage()
                }}
              />
              <DateTimePicker
                ariaLabel={t('admin.riskControl.filters.from')}
                value={filters.from}
                type="datetime-local"
                className="input"
                onValueChange={(value) => {
                  patchFilters({ from: value })
                  reloadLogsFromFirstPage()
                }}
              />
              <DateTimePicker
                ariaLabel={t('admin.riskControl.filters.to')}
                value={filters.to}
                type="datetime-local"
                className="input"
                onValueChange={(value) => {
                  patchFilters({ to: value })
                  reloadLogsFromFirstPage()
                }}
              />
            </div>
          </div>

          <div className="record-table-scroll table-container workspace-table-viewport" data-test="risk-records-scroll">
            <table className="record-table min-w-full divide-y divide-gray-200 dark:divide-dark-700">
              <thead className="surface-inset">
                <tr>
                  <th className="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted">
                    {t('admin.riskControl.table.time')}
                  </th>
                  <th className="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted">
                    {t('admin.riskControl.table.group')}
                  </th>
                  <th className="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted">
                    {t('admin.riskControl.table.user')}
                  </th>
                  <th className="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted">
                    {t('admin.riskControl.table.apiKey')}
                  </th>
                  <th className="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted">
                    {t('admin.riskControl.table.moderationPlatform')}
                  </th>
                  <th className="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted">
                    {t('admin.riskControl.table.endpoint')}
                  </th>
                  <th className="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted">
                    {t('admin.riskControl.table.result')}
                  </th>
                  <th className="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted">
                    {t('admin.riskControl.table.highest')}
                  </th>
                  <th className="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted">
                    {t('admin.riskControl.table.actionMeta')}
                  </th>
                  <th className="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted">
                    {t('admin.riskControl.table.latency')}
                  </th>
                  <th className="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted">
                    {t('admin.riskControl.table.input')}
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100 surface-inset dark:divide-dark-800">
                {logsLoading && (
                  <tr>
                    <td colSpan={11} className="px-5 py-12 text-center text-sm surface-muted">
                      {t('common.loading')}
                    </td>
                  </tr>
                )}
                {!logsLoading && logs.length === 0 && (
                  <tr>
                    <td colSpan={11} className="px-5 py-12 text-center text-sm surface-muted">
                      {t('admin.riskControl.emptyLogs')}
                    </td>
                  </tr>
                )}
                {!logsLoading &&
                  logs.map((row) => (
                    <tr key={row.id} className="hover:surface-hover">
                      <td className="whitespace-nowrap px-5 py-4 text-sm surface-muted">{formatDateTime(row.created_at)}</td>
                      <td className="whitespace-nowrap px-5 py-4 text-sm surface-muted">{row.group_name || '-'}</td>
                      <td className="whitespace-nowrap px-5 py-4 text-sm surface-muted">
                        <div>{row.user_email || '-'}</div>
                        {row.user_id && <div className="text-xs surface-muted">UID {row.user_id}</div>}
                      </td>
                      <td className="whitespace-nowrap px-5 py-4 text-sm surface-muted">{row.api_key_name || '-'}</td>
                      <td className="whitespace-nowrap px-5 py-4 text-sm surface-muted">
                        {row.moderation_endpoint_name || '-'}
                      </td>
                      <td className="whitespace-nowrap px-5 py-4 text-sm surface-muted">
                        <div>{row.endpoint || '-'}</div>
                        <div className="text-xs surface-muted">
                          {row.provider || '-'} / {row.model || '-'}
                        </div>
                      </td>
                      <td className="whitespace-nowrap px-5 py-4">
                        <span className={`inline-flex rounded-md px-2 py-1 text-xs font-medium ${resultBadgeClass(row)}`}>
                          {resultLabel(row)}
                        </span>
                      </td>
                      <td className="whitespace-nowrap px-5 py-4 text-sm surface-muted">
                        <div>{row.highest_category || '-'}</div>
                        <div className="text-xs surface-muted">{percent(row.highest_score)}</div>
                        {row.matched_keyword && (
                          <div
                            className="mt-0.5 text-xs font-medium text-red-600 dark:text-red-300"
                            title={`${t('admin.riskControl.matchedKeyword')}: ${row.matched_keyword}`}
                          >
                            {t('admin.riskControl.matchedKeyword')}: {row.matched_keyword}
                          </div>
                        )}
                      </td>
                      <td className="whitespace-nowrap px-5 py-4 text-sm surface-muted">
                        <div>{violationCountText(row)}</div>
                        <div className="text-xs surface-muted">
                          {row.email_sent ? t('admin.riskControl.emailSent') : t('admin.riskControl.emailNotSent')}
                          {row.auto_banned && <> / {t('admin.riskControl.autoBanned')}</>}
                        </div>
                        {canUnbanRow(row) && (
                          <button
                            type="button"
                            className="mt-2 inline-flex items-center gap-1 rounded-md border border-emerald-200 bg-emerald-50/[0.15] px-2 py-1 text-xs font-medium text-emerald-700 transition-colors hover:bg-emerald-100/25 disabled:cursor-not-allowed disabled:opacity-60 dark:border-emerald-900/60 dark:bg-emerald-900/20 dark:text-emerald-300 dark:hover:bg-emerald-900/30"
                            disabled={unbanningUserID === row.user_id}
                            onClick={() => void unbanUser(row)}
                          >
                            <Icon
                              name="checkCircle"
                              size="xs"
                              className={unbanningUserID === row.user_id ? 'animate-spin' : undefined}
                            />
                            {unbanningUserID === row.user_id ? t('common.processing') : t('admin.riskControl.unbanUser')}
                          </button>
                        )}
                      </td>
                      <td className="whitespace-nowrap px-5 py-4 text-sm surface-muted">
                        <div>{latencyText(row.upstream_latency_ms)}</div>
                        {row.queue_delay_ms !== null && row.queue_delay_ms !== undefined && (
                          <div className="text-xs surface-muted">
                            {t('admin.riskControl.queueDelay', { ms: row.queue_delay_ms })}
                          </div>
                        )}
                      </td>
                      <td className="w-[320px] max-w-sm px-5 py-4 text-sm surface-muted">
                        <button
                          type="button"
                          className="group flex w-full min-w-0 items-center gap-2 rounded-lg px-2 py-1.5 text-left transition-colors hover:surface-hover surface-control"
                          title={inputSummaryText(row)}
                          onClick={() => setInputDetailRow(row)}
                        >
                          <span className="min-w-0 flex-1 truncate">{inputSummaryText(row)}</span>
                          <Icon
                            name="eye"
                            size="xs"
                            className="flex-shrink-0 text-gray-300 transition-colors group-hover:text-primary-500 dark:text-gray-500"
                          />
                        </button>
                      </td>
                    </tr>
                  ))}
              </tbody>
            </table>
          </div>

          {pagination.total > 0 && (
            <Pagination
              className="risk-records-pagination workspace-fixed"
              page={pagination.page}
              total={pagination.total}
              pageSize={pagination.page_size}
              onPageChange={onPageChange}
              onPageSizeChange={onPageSizeChange}
            />
          )}
        </div>
      )}

      <Modal
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
        title={t('admin.riskControl.settingsTitle')}
        size="full"
        footer={
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setSettingsOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" disabled={saving} onClick={() => void saveConfig()}>
              <Icon name={saving ? 'refresh' : 'check'} size="sm" className={saving ? 'animate-spin' : undefined} />
              {saving ? t('common.saving') : t('admin.riskControl.saveConfig')}
            </Button>
          </div>
        }
      >
        <div className="space-y-6">
          <div className="card-tabs overflow-x-auto pb-3" role="tablist" aria-label={t('admin.riskControl.settingsTitle')}>
            {settingsTabs.map((tab) => (
              <button
                key={tab.id}
                type="button"
                role="tab"
                className="card-tab"
                aria-selected={activeSettingsTab === tab.id}
                onClick={() => setActiveSettingsTab(tab.id)}
              >
                {tab.label}
              </button>
            ))}
          </div>

          {activeSettingsTab === 'basic' && (
            <div className="space-y-5">
              <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
                <div className="flex items-center justify-between rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                  <div>
                    <p className="text-sm font-medium text-gray-900 dark:text-white">{t('admin.riskControl.enabled')}</p>
                    <p className="mt-1 text-xs surface-muted">{t('admin.riskControl.enabledHint')}</p>
                  </div>
                  <Toggle
                    checked={configForm.enabled}
                    onCheckedChange={(checked) => patchConfig({ enabled: checked })}
                    ariaLabel={t('admin.riskControl.enabled')}
                  />
                </div>
                <div>
                  <label className="input-label">{t('admin.riskControl.mode')}</label>
                  <Select
                    value={configForm.mode}
                    options={modeOptions}
                    onValueChange={(value) => patchConfig({ mode: (value as ModerationMode) ?? 'pre_block' })}
                  />
                  <p className="mt-2 text-xs leading-5 surface-muted">{modeDescription(configForm.mode)}</p>
                </div>
                <div className="lg:col-span-2">
                  <div className="mb-1 flex items-center gap-1.5">
                    <label className="input-label mb-0">{t('admin.riskControl.textApiMode')}</label>
                    <button
                      type="button"
                      className="inline-flex h-6 w-6 items-center justify-center hover:text-gray-700 focus:outline-none focus:ring-2 focus:ring-primary-500 dark:hover:text-gray-200 surface-control surface-muted"
                      aria-label={t('admin.riskControl.policyHelp.open')}
                      onClick={() => setPolicyHelpOpen(true)}
                    >
                      <Icon name="exclamationCircle" size="sm" />
                    </button>
                  </div>
                  <Select
                    value={configForm.text_api_mode}
                    options={textAPIModeOptions}
                    disabled={configForm.mode === 'off'}
                    onValueChange={(value) =>
                      patchConfig({ text_api_mode: (value as ContentModerationConfig['text_api_mode']) ?? 'blocking' })
                    }
                  />
                  <p data-test="moderation-effective-behavior" className="mt-2 text-xs leading-5 surface-muted">
                    {t('admin.riskControl.policyHelp.current')}
                    {effectiveTextPolicyDescription}
                  </p>
                </div>
                {configForm.endpoints.length === 0 && (
                  <div>
                    <label className="input-label">{t('admin.riskControl.baseUrl')}</label>
                    <input
                      value={configForm.base_url}
                      type="url"
                      className="input"
                      placeholder="https://api.openai.com"
                      onChange={(event) => patchConfig({ base_url: event.target.value })}
                    />
                  </div>
                )}
                {configForm.endpoints.length === 0 && (
                  <div>
                    <label className="input-label">{t('admin.riskControl.model')}</label>
                    <input
                      value={configForm.model}
                      type="text"
                      className="input"
                      placeholder="omni-moderation-latest"
                      onChange={(event) => patchConfig({ model: event.target.value })}
                    />
                  </div>
                )}
                {configForm.endpoints.length === 0 && (
                  <div>
                    <label className="input-label">{t('admin.riskControl.timeoutMs')}</label>
                    <input
                      value={configForm.timeout_ms}
                      type="number"
                      min={500}
                      max={30000}
                      className="input"
                      onChange={(event) => patchConfig({ timeout_ms: Number(event.target.value) })}
                    />
                  </div>
                )}
                <div>
                  <label className="input-label">{t('admin.riskControl.retryCount')}</label>
                  <input
                    value={configForm.retry_count}
                    type="number"
                    min={0}
                    max={5}
                    className="input"
                    onChange={(event) => patchConfig({ retry_count: Number(event.target.value) })}
                  />
                </div>
                <div>
                  <label className="input-label">{t('admin.riskControl.sampleRate')}</label>
                  <div className="relative">
                    <input
                      value={configForm.sample_rate}
                      type="number"
                      min={0}
                      max={100}
                      step={1}
                      className="input pr-8"
                      onChange={(event) => patchConfig({ sample_rate: Number(event.target.value) })}
                    />
                    <span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 surface-muted">%</span>
                  </div>
                </div>
                {configForm.endpoints.length === 0 && (
                  <div>
                    <label className="input-label">{t('admin.riskControl.proxy')}</label>
                    <ProxySelector
                      value={configForm.proxy_id}
                      proxies={proxies}
                      onValueChange={(value) => patchConfig({ proxy_id: value })}
                    />
                    <p className="mt-2 text-xs leading-5 surface-muted">{t('admin.riskControl.proxyHint')}</p>
                  </div>
                )}
              </div>

              <section data-test="moderation-endpoint-pool" className="border-t border-gray-200 pt-5 dark:border-dark-700">
                <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                  <div>
                    <h3 className="text-base font-semibold text-gray-900 dark:text-white">
                      {t('admin.riskControl.endpointPool.title')}
                    </h3>
                    <p className="mt-1 text-xs leading-5 surface-muted">{t('admin.riskControl.endpointPool.hint')}</p>
                  </div>
                  <button
                    type="button"
                    className="btn btn-secondary inline-flex items-center gap-2"
                    onClick={addModerationEndpoint}
                  >
                    <Icon name="plus" size="sm" />
                    {t('admin.riskControl.endpointPool.add')}
                  </button>
                </div>

                <div className="space-y-4">
                  {configForm.endpoints.map((endpoint, index) => (
                    <div key={endpoint.id} className="border border-gray-200 p-4 dark:border-dark-700">
                      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                        <div className="flex min-w-0 items-center gap-3">
                          <span className="flex h-8 w-8 flex-shrink-0 items-center justify-center surface-inset text-sm font-semibold surface-muted">
                            {index + 1}
                          </span>
                          <div className="min-w-0">
                            <p className="truncate text-sm font-semibold text-gray-900 dark:text-white">
                              {endpoint.name || t('admin.riskControl.endpointPool.unnamed')}
                            </p>
                            <span
                              className={`mt-1 inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium ${endpointStatusBadgeClass(endpoint)}`}
                            >
                              <span
                                className={`console-data-mark h-1.5 w-1.5 rounded-full ${endpointStatusDotClass(endpoint)}`}
                              />
                              {endpointStatusLabel(endpoint)}
                            </span>
                          </div>
                        </div>
                        <div className="flex flex-wrap items-center gap-2">
                          <button
                            type="button"
                            className="btn btn-secondary"
                            disabled={endpointTestingID === endpoint.id}
                            onClick={() => void testModerationEndpoint(endpoint)}
                          >
                            <Icon name="beaker" size="sm" />
                            {t('admin.riskControl.endpointPool.test')}
                          </button>
                          <button
                            type="button"
                            className="btn btn-secondary"
                            disabled={index === 0}
                            aria-label={t('admin.riskControl.endpointPool.moveUp')}
                            onClick={() => moveModerationEndpoint(index, -1)}
                          >
                            <Icon name="arrowUp" size="sm" />
                          </button>
                          <button
                            type="button"
                            className="btn btn-secondary"
                            disabled={index === configForm.endpoints.length - 1}
                            aria-label={t('admin.riskControl.endpointPool.moveDown')}
                            onClick={() => moveModerationEndpoint(index, 1)}
                          >
                            <Icon name="arrowDown" size="sm" />
                          </button>
                          <button
                            type="button"
                            className="btn btn-secondary"
                            onClick={() => void toggleEndpointPause(endpoint)}
                          >
                            {endpoint.manual_paused
                              ? t('admin.riskControl.endpointPool.resume')
                              : t('admin.riskControl.endpointPool.pause')}
                          </button>
                          <button
                            type="button"
                            className="btn btn-secondary text-red-600"
                            disabled={configForm.endpoints.length === 1}
                            aria-label={t('admin.riskControl.endpointPool.delete')}
                            onClick={() => removeModerationEndpoint(index)}
                          >
                            <Icon name="trash" size="sm" />
                          </button>
                          <Toggle
                            checked={endpoint.enabled}
                            onCheckedChange={(checked) => patchEndpoint(index, { enabled: checked })}
                            ariaLabel={t('admin.riskControl.endpointPool.name')}
                          />
                        </div>
                      </div>

                      <div className="mt-4 grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
                        <div>
                          <label className="input-label">{t('admin.riskControl.endpointPool.name')}</label>
                          <input
                            value={endpoint.name}
                            className="input"
                            type="text"
                            onChange={(event) => patchEndpoint(index, { name: event.target.value })}
                          />
                        </div>
                        <div className="md:col-span-2">
                          <label className="input-label">{t('admin.riskControl.baseUrl')}</label>
                          <input
                            value={endpoint.base_url}
                            className="input"
                            type="url"
                            onChange={(event) => patchEndpoint(index, { base_url: event.target.value })}
                          />
                        </div>
                        <div>
                          <label className="input-label">{t('admin.riskControl.model')}</label>
                          <input
                            value={endpoint.model}
                            className="input"
                            type="text"
                            onChange={(event) => patchEndpoint(index, { model: event.target.value })}
                          />
                        </div>
                        <div>
                          <label className="input-label">{t('admin.riskControl.timeoutMs')}</label>
                          <input
                            value={endpoint.timeout_ms}
                            className="input"
                            type="number"
                            min={500}
                            max={30000}
                            onChange={(event) => patchEndpoint(index, { timeout_ms: Number(event.target.value) })}
                          />
                        </div>
                        <div>
                          <label className="input-label">{t('admin.riskControl.endpointPool.cooldown')}</label>
                          <input
                            value={endpoint.cooldown_seconds}
                            className="input"
                            type="number"
                            min={1}
                            max={86400}
                            onChange={(event) => patchEndpoint(index, { cooldown_seconds: Number(event.target.value) })}
                          />
                        </div>
                        <div>
                          <label className="input-label">{t('admin.riskControl.endpointPool.failureThreshold')}</label>
                          <input
                            value={endpoint.failure_threshold}
                            className="input"
                            type="number"
                            min={1}
                            max={20}
                            onChange={(event) => patchEndpoint(index, { failure_threshold: Number(event.target.value) })}
                          />
                        </div>
                        <div>
                          <label className="input-label">{t('admin.riskControl.proxy')}</label>
                          <ProxySelector
                            value={endpoint.proxy_id}
                            proxies={proxies}
                            onValueChange={(value) => patchEndpoint(index, { proxy_id: value })}
                          />
                        </div>
                        <div className="md:col-span-2 xl:col-span-4">
                          <label className="input-label">{t('admin.riskControl.endpointPool.apiKeys')}</label>
                          <textarea
                            value={endpoint.api_keys_text}
                            className="input min-h-20 resize-y font-mono text-sm"
                            placeholder={
                              endpoint.api_key_count
                                ? t('admin.riskControl.endpointPool.keysKeep', { count: endpoint.api_key_count })
                                : t('admin.riskControl.endpointPool.keysRequired')
                            }
                            onChange={(event) => patchEndpoint(index, { api_keys_text: event.target.value })}
                          />
                          <p className="mt-1 text-xs surface-muted">{t('admin.riskControl.endpointPool.keysHint')}</p>
                        </div>
                      </div>
                      {endpoint.runtime.last_error && (
                        <p className="mt-3 break-words text-xs text-red-600 dark:text-red-300">
                          {endpoint.runtime.last_error}
                        </p>
                      )}
                    </div>
                  ))}
                </div>
              </section>

              {configForm.endpoints.length === 0 && (
                <div className="overflow-hidden rounded-xl border border-gray-100 surface-inset shadow-sm dark:border-dark-700">
                  <div className="flex flex-col gap-4 border-b border-gray-100 surface-inset px-4 py-4 dark:border-dark-700 lg:flex-row lg:items-center lg:justify-between">
                    <div className="flex items-start gap-3">
                      <span className="mt-0.5 flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg bg-primary-50/[0.15] text-primary-600 dark:bg-primary-900/30 dark:text-primary-300">
                        <Icon name="key" size="md" />
                      </span>
                      <div>
                        <label className="text-sm font-semibold text-gray-900 dark:text-white">
                          {t('admin.riskControl.apiKeys')}
                        </label>
                        <p className="mt-1 max-w-3xl text-xs leading-5 surface-muted">
                          {t('admin.riskControl.apiKeysHint', { count: configForm.api_key_count })}
                        </p>
                      </div>
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                      <button
                        type="button"
                        className="btn btn-secondary inline-flex items-center gap-2"
                        disabled={apiKeyTesting || inputApiKeyCount === 0 || configForm.clear_api_key}
                        onClick={() => void testApiKeys(true)}
                      >
                        <Icon name="beaker" size="sm" className={apiKeyTesting ? 'animate-pulse' : undefined} />
                        {apiKeyTesting ? t('admin.riskControl.testingApiKeys') : t('admin.riskControl.testInputApiKeys')}
                      </button>
                      <button
                        type="button"
                        className="btn btn-secondary inline-flex items-center gap-2"
                        disabled={
                          apiKeyTesting ||
                          effectiveStoredApiKeyCount === 0 ||
                          pendingDeletedApiKeyCount > 0 ||
                          configForm.clear_api_key ||
                          configForm.api_keys_mode === 'replace'
                        }
                        onClick={() => void testApiKeys(false)}
                      >
                        <Icon name="shield" size="sm" />
                        {storedApiKeyTestButtonText}
                      </button>
                      {configForm.api_key_configured && (
                        <button
                          type="button"
                          className="btn btn-secondary inline-flex items-center gap-2"
                          onClick={toggleClearApiKey}
                        >
                          <Icon name={configForm.clear_api_key ? 'x' : 'trash'} size="sm" />
                          {configForm.clear_api_key ? t('admin.riskControl.keepApiKey') : t('admin.riskControl.clearApiKey')}
                        </button>
                      )}
                    </div>
                  </div>

                  <div className="grid grid-cols-1 gap-4 p-4 xl:grid-cols-[minmax(0,1fr)_minmax(360px,440px)]">
                    <div className="space-y-3">
                      <div className="flex flex-col gap-2 rounded-lg border border-gray-100 surface-inset p-2 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between">
                        <div className="text-xs leading-5 surface-muted">
                          <span className="font-medium surface-muted">{t('admin.riskControl.apiKeysWriteMode')}</span>
                          <span className="ml-2">{apiKeysModeHint}</span>
                        </div>
                        <div className="inline-flex rounded-lg surface-inset p-1 shadow-sm">
                          <button
                            type="button"
                            className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors surface-control ${
                              configForm.api_keys_mode === 'append'
                                ? 'bg-primary-500 text-white shadow-sm'
                                : 'text-gray-600 hover:surface-hover dark:text-gray-300 dark:hover:surface-hover'
                            }`}
                            disabled={configForm.clear_api_key}
                            onClick={() => setAPIKeysMode('append')}
                          >
                            {t('admin.riskControl.apiKeysModeAppend')}
                          </button>
                          <button
                            type="button"
                            className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors surface-control ${
                              configForm.api_keys_mode === 'replace'
                                ? 'bg-amber-500 text-white shadow-sm'
                                : 'text-gray-600 hover:surface-hover dark:text-gray-300 dark:hover:surface-hover'
                            }`}
                            disabled={configForm.clear_api_key}
                            onClick={() => setAPIKeysMode('replace')}
                          >
                            {t('admin.riskControl.apiKeysModeReplace')}
                          </button>
                        </div>
                      </div>
                      <textarea
                        value={configForm.api_keys_text}
                        className="input min-h-44 resize-y font-mono text-sm"
                        placeholder={apiKeysPlaceholder}
                        autoComplete="new-password"
                        disabled={configForm.clear_api_key}
                        onChange={(event) => patchConfig({ api_keys_text: event.target.value })}
                      />
                      <div className="flex flex-wrap items-center gap-2 text-xs surface-muted">
                        <span className="inline-flex rounded-md surface-inset px-2 py-1">
                          {t('admin.riskControl.inputApiKeyCount', { count: inputApiKeyCount })}
                        </span>
                        {configForm.api_key_configured && (
                          <span className="inline-flex rounded-md surface-inset px-2 py-1">
                            {t('admin.riskControl.storedApiKeyCount', { count: configForm.api_key_count })}
                          </span>
                        )}
                        {configForm.clear_api_key && (
                          <span className="inline-flex rounded-md bg-red-50/[0.15] px-2 py-1 text-red-700 dark:bg-red-900/20 dark:text-red-300">
                            {t('admin.riskControl.apiKeyWillClear')}
                          </span>
                        )}
                        {!configForm.clear_api_key && pendingDeletedApiKeyCount > 0 && (
                          <span className="inline-flex rounded-md bg-amber-50/[0.15] px-2 py-1 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
                            {t('admin.riskControl.apiKeyPendingDeleteCount', { count: pendingDeletedApiKeyCount })}
                          </span>
                        )}
                        {configForm.api_keys_mode === 'replace' && (
                          <span className="inline-flex rounded-md bg-amber-50/[0.15] px-2 py-1 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
                            {t('admin.riskControl.apiKeysReplaceWarning')}
                          </span>
                        )}
                      </div>

                      <div
                        className="rounded-lg border border-gray-100 surface-inset p-3 dark:border-dark-700"
                        onPaste={(event) => void handleModerationImagePaste(event)}
                      >
                        <div className="mb-3 flex items-center justify-between gap-3">
                          <div>
                            <p className="text-sm font-semibold text-gray-900 dark:text-white">
                              {t('admin.riskControl.auditTestInput')}
                            </p>
                            <p className="mt-1 text-xs surface-muted">{t('admin.riskControl.auditTestInputHint')}</p>
                          </div>
                          {(moderationTestPrompt || moderationTestImages.length > 0 || moderationTestResult) && (
                            <button
                              type="button"
                              className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium hover:surface-hover hover:text-gray-900 dark:hover:text-white surface-control surface-muted"
                              onClick={clearModerationTestInput}
                            >
                              <Icon name="x" size="xs" />
                              {t('admin.riskControl.clearAuditTest')}
                            </button>
                          )}
                        </div>
                        <textarea
                          value={moderationTestPrompt}
                          className="input min-h-24 resize-y text-sm"
                          placeholder={t('admin.riskControl.auditTestPromptPlaceholder')}
                          onChange={(event) => setModerationTestPrompt(event.target.value)}
                        />
                        <div
                          className="mt-3 rounded-lg border border-dashed border-gray-200 surface-inset p-3 dark:border-dark-700"
                          onDragOver={(event) => event.preventDefault()}
                          onDrop={(event) => void handleModerationImageDrop(event)}
                        >
                          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                            <div className="flex items-start gap-2">
                              <Icon name="upload" size="md" className="mt-0.5 text-gray-400" />
                              <div>
                                <p className="text-sm font-medium text-gray-800 dark:text-gray-100">
                                  {t('admin.riskControl.auditTestImages')}
                                </p>
                                <p className="mt-1 text-xs surface-muted">{t('admin.riskControl.auditTestImagesHint')}</p>
                              </div>
                            </div>
                            <label className="btn btn-secondary inline-flex cursor-pointer items-center gap-2">
                              <Icon name="plus" size="sm" />
                              {t('admin.riskControl.addAuditTestImage')}
                              <input
                                type="file"
                                accept="image/*"
                                multiple
                                className="sr-only"
                                onChange={(event) => void handleModerationImageUpload(event)}
                              />
                            </label>
                          </div>
                          {moderationTestImages.length > 0 && (
                            <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
                              {moderationTestImages.map((image, index) => (
                                <div
                                  key={`${image.slice(0, 64)}-${index}`}
                                  className="group relative aspect-square overflow-hidden rounded-lg border border-gray-100 surface-inset dark:border-dark-700"
                                >
                                  <img src={image} alt="" className="h-full w-full object-cover" />
                                  <button
                                    type="button"
                                    className="absolute right-1.5 top-1.5 flex h-7 w-7 items-center justify-center rounded-full bg-black/60 text-white opacity-0 transition-opacity group-hover:opacity-100 surface-control"
                                    onClick={() => removeModerationTestImage(index)}
                                  >
                                    <Icon name="x" size="xs" strokeWidth={2} />
                                  </button>
                                </div>
                              ))}
                            </div>
                          )}
                        </div>
                      </div>
                    </div>

                    <div className="rounded-lg border border-gray-100 surface-inset p-3 dark:border-dark-700">
                      <div className="mb-3 flex items-start justify-between gap-3">
                        <div className="min-w-0">
                          <p className="text-sm font-semibold text-gray-900 dark:text-white">
                            {t('admin.riskControl.apiKeyHealth')}
                          </p>
                          <p className="mt-1 text-xs surface-muted">{t('admin.riskControl.apiKeyFreezeRule')}</p>
                        </div>
                        <span className="inline-flex shrink-0 items-center whitespace-nowrap rounded-full surface-inset px-2 py-0.5 text-[11px] font-medium leading-5 shadow-sm surface-muted">
                          {t('admin.riskControl.apiKeyRows', { count: apiKeyRows.length })}
                        </span>
                      </div>

                      {apiKeyRows.length === 0 ? (
                        <div className="flex min-h-32 flex-col items-center justify-center rounded-lg border border-dashed border-gray-200 surface-inset px-4 py-6 text-center dark:border-dark-700">
                          <Icon name="infoCircle" size="lg" className="text-gray-300 dark:text-dark-500" />
                          <p className="mt-2 text-sm font-medium surface-muted">{t('admin.riskControl.apiKeyHealthEmpty')}</p>
                          <p className="mt-1 text-xs surface-muted">{t('admin.riskControl.apiKeyHealthEmptyHint')}</p>
                        </div>
                      ) : (
                        <div className="space-y-2">
                          <div className={`space-y-2 ${apiKeyRowsExpanded ? 'max-h-72 overflow-y-auto pr-1' : ''}`}>
                            {visibleApiKeyRows.map((row, index) => (
                              <div
                                key={apiKeyRowKey(row, index)}
                                className={`rounded-lg border surface-inset p-2.5 shadow-sm ${
                                  isStoredApiKeyPendingDelete(row)
                                    ? 'border-amber-200 opacity-70 dark:border-amber-800/60'
                                    : 'border-gray-100 dark:border-dark-700'
                                }`}
                              >
                                <div className="flex items-start justify-between gap-2">
                                  <div className="min-w-0">
                                    <div className="flex min-w-0 flex-wrap items-center gap-2">
                                      <span className="truncate font-mono text-sm font-semibold text-gray-900 dark:text-white">
                                        {row.masked || '-'}
                                      </span>
                                      <span
                                        className={`inline-flex rounded-md px-1.5 py-0.5 text-[11px] font-medium ${
                                          row.configured
                                            ? 'bg-primary-50/[0.15] text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
                                            : 'bg-purple-50/[0.15] text-purple-700 dark:bg-purple-900/30 dark:text-purple-300'
                                        }`}
                                      >
                                        {isStoredApiKeyPendingDelete(row)
                                          ? t('admin.riskControl.apiKeyPendingDelete')
                                          : row.configured
                                            ? t('admin.riskControl.apiKeyConfigured')
                                            : t('admin.riskControl.apiKeyTemporary')}
                                      </span>
                                    </div>
                                    <p className="mt-1 text-xs leading-5 surface-muted">{apiKeyStatusMeta(row)}</p>
                                  </div>
                                  <div className="flex flex-shrink-0 items-center gap-1.5">
                                    <span
                                      className={`inline-flex items-center gap-1.5 whitespace-nowrap rounded-full px-2 py-0.5 text-xs font-medium ${apiKeyStatusBadgeClass(row.status)}`}
                                    >
                                      <span
                                        className={`console-data-mark h-1.5 w-1.5 rounded-full ${apiKeyStatusDotClass(row.status)}`}
                                      />
                                      {apiKeyStatusLabel(row.status)}
                                    </span>
                                    {row.configured && !configForm.clear_api_key && (
                                      <button
                                        type="button"
                                        className="inline-flex h-7 w-7 items-center justify-center rounded-md transition-colors hover:surface-hover hover:text-gray-700 dark:hover:text-gray-200 surface-control surface-muted"
                                        title={
                                          isStoredApiKeyPendingDelete(row)
                                            ? t('admin.riskControl.undoDeleteApiKey')
                                            : t('admin.riskControl.deleteApiKey')
                                        }
                                        onClick={() => toggleDeleteStoredApiKey(row)}
                                      >
                                        <Icon name={isStoredApiKeyPendingDelete(row) ? 'refresh' : 'trash'} size="xs" />
                                      </button>
                                    )}
                                  </div>
                                </div>
                                {row.last_error && (
                                  <p className="mt-1.5 rounded-md bg-amber-50/[0.15] px-2 py-1.5 text-xs leading-5 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
                                    {row.last_error}
                                  </p>
                                )}
                              </div>
                            ))}
                          </div>

                          {canToggleApiKeyRows && (
                            <div className="flex items-center justify-between gap-3 rounded-lg border border-dashed border-gray-200 surface-inset px-3 py-2 text-xs dark:border-dark-700 surface-muted">
                              <span className="min-w-0 truncate">
                                {apiKeyRowsExpanded
                                  ? t('admin.riskControl.apiKeyRowsExpanded', { count: apiKeyRows.length })
                                  : t('admin.riskControl.apiKeyRowsCollapsed', { count: hiddenApiKeyRowCount })}
                              </span>
                              <button
                                type="button"
                                className="inline-flex shrink-0 items-center gap-1 rounded-md px-2 py-1 font-medium text-primary-600 transition-colors hover:bg-primary-50/25 hover:text-primary-700 dark:text-primary-300 dark:hover:bg-primary-900/20 surface-control"
                                onClick={() => setApiKeyRowsExpanded((prev) => !prev)}
                              >
                                <Icon name={apiKeyRowsExpanded ? 'chevronUp' : 'chevronDown'} size="xs" />
                                {apiKeyRowsExpanded
                                  ? t('admin.riskControl.collapseApiKeyRows')
                                  : t('admin.riskControl.expandApiKeyRows')}
                              </button>
                            </div>
                          )}
                        </div>
                      )}

                      {moderationTestResult && (
                        <div className="mt-4 rounded-lg border border-gray-100 surface-inset p-3 dark:border-dark-700">
                          <div className="flex items-start justify-between gap-3">
                            <div>
                              <p className="text-sm font-semibold text-gray-900 dark:text-white">
                                {t('admin.riskControl.auditTestResult')}
                              </p>
                              <p className="mt-1 text-xs surface-muted">
                                {t('admin.riskControl.auditTestHighest', {
                                  category: moderationTestResult.highest_category || '-',
                                  score: percent(moderationTestResult.highest_score)
                                })}
                              </p>
                            </div>
                            <span
                              className={`inline-flex rounded-full px-2 py-1 text-xs font-medium ${
                                moderationTestResult.flagged
                                  ? 'bg-red-50/[0.15] text-red-700 dark:bg-red-900/20 dark:text-red-300'
                                  : 'bg-emerald-50/[0.15] text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'
                              }`}
                            >
                              {moderationTestResult.flagged
                                ? t('admin.riskControl.auditTestFlagged')
                                : t('admin.riskControl.auditTestPassed')}
                            </span>
                          </div>
                          <div className="mt-3">
                            <div className="mb-2 flex items-center justify-between text-xs surface-muted">
                              <span>{t('admin.riskControl.auditTestComposite')}</span>
                              <span className="font-semibold text-gray-900 dark:text-white">
                                {percent(moderationTestResult.composite_score)}
                              </span>
                            </div>
                            <div className="h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
                              <div
                                className={`console-data-mark h-full rounded-full ${
                                  moderationTestResult.flagged ? 'bg-red-500' : 'bg-emerald-500'
                                }`}
                                style={{ width: percentWidth(moderationTestResult.composite_score) }}
                              />
                            </div>
                          </div>
                          <div className="mt-3 max-h-52 space-y-2 overflow-y-auto pr-1">
                            {moderationScoreRows.map((score) => (
                              <div key={score.category}>
                                <div className="mb-1 flex items-center justify-between gap-3 text-xs">
                                  <span className="truncate surface-muted">{score.category}</span>
                                  <span className="font-mono surface-muted">
                                    {percent(score.score)} / {percent(score.threshold)}
                                  </span>
                                </div>
                                <div className="h-1.5 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
                                  <div
                                    className={`console-data-mark h-full rounded-full ${
                                      score.hit ? 'bg-red-500' : 'bg-primary-500'
                                    }`}
                                    style={{ width: percentWidth(score.score) }}
                                  />
                                </div>
                              </div>
                            ))}
                          </div>
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              )}
            </div>
          )}

          {activeSettingsTab === 'scope' && (
            <div className="space-y-5">
              <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
                <div>
                  <h3 className="text-base font-semibold text-gray-900 dark:text-white">{t('admin.riskControl.groupScope')}</h3>
                  <p className="mt-1 text-sm surface-muted">{t('admin.riskControl.groupScopeHint')}</p>
                </div>
                <div className="inline-flex rounded-lg surface-inset p-1">
                  <button
                    type="button"
                    className={`rounded-md px-3 py-1.5 text-sm font-medium transition-colors surface-control ${
                      configForm.all_groups
                        ? 'surface-field text-gray-900 shadow-sm dark:surface-field dark:text-white'
                        : 'surface-muted'
                    }`}
                    onClick={() => patchConfig({ all_groups: true })}
                  >
                    {t('admin.riskControl.allGroups')}
                  </button>
                  <button
                    type="button"
                    className={`rounded-md px-3 py-1.5 text-sm font-medium transition-colors surface-control ${
                      !configForm.all_groups
                        ? 'surface-field text-gray-900 shadow-sm dark:surface-field dark:text-white'
                        : 'surface-muted'
                    }`}
                    onClick={() => patchConfig({ all_groups: false })}
                  >
                    {t('admin.riskControl.selectedGroups')}
                  </button>
                </div>
              </div>

              {!configForm.all_groups && (
                <div className="space-y-4">
                  <div className="relative">
                    <Icon
                      name="search"
                      size="sm"
                      className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"
                    />
                    <input
                      value={groupSearch}
                      type="search"
                      className="input pl-9"
                      placeholder={t('admin.riskControl.searchGroups')}
                      onChange={(event) => setGroupSearch(event.target.value)}
                    />
                  </div>
                  <div className="grid max-h-[420px] grid-cols-1 gap-3 overflow-y-auto pr-1 md:grid-cols-2 xl:grid-cols-3">
                    {filteredGroups.map((group) => (
                      <button
                        key={group.id}
                        type="button"
                        className={`flex min-h-20 items-center justify-between rounded-lg border p-4 text-left transition-colors surface-control ${
                          isGroupSelected(group.id)
                            ? 'border-primary-300 bg-primary-50/[0.15] dark:border-primary-700 dark:bg-primary-900/20'
                            : 'border-gray-100 hover:surface-hover dark:border-dark-700 dark:hover:surface-hover'
                        }`}
                        onClick={() => toggleGroup(group.id)}
                      >
                        <span className="min-w-0">
                          <span className="block truncate text-sm font-semibold text-gray-900 dark:text-white">
                            {group.name}
                          </span>
                          <span className="mt-1 inline-flex rounded-md surface-inset px-2 py-0.5 text-xs surface-muted">
                            {group.platform}
                          </span>
                        </span>
                        <span
                          className={`flex h-5 w-5 flex-shrink-0 items-center justify-center rounded-full border ${
                            isGroupSelected(group.id)
                              ? 'border-primary-500 bg-primary-500 text-white'
                              : 'border-gray-300 text-transparent dark:border-dark-500'
                          }`}
                        >
                          <Icon name="check" size="xs" strokeWidth={2} />
                        </span>
                      </button>
                    ))}
                    {filteredGroups.length === 0 && <p className="text-sm surface-muted">{t('admin.riskControl.noGroups')}</p>}
                  </div>
                </div>
              )}

              <div className="space-y-4 rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div className="flex flex-col gap-2 lg:flex-row lg:items-center lg:justify-between">
                  <div>
                    <h3 className="text-base font-semibold text-gray-900 dark:text-white">
                      {t('admin.riskControl.modelFilter')}
                    </h3>
                    <p className="mt-1 text-sm surface-muted">{t('admin.riskControl.modelFilterHint')}</p>
                  </div>
                  <span className="inline-flex w-fit rounded-md surface-inset px-2.5 py-1 text-xs font-medium surface-muted">
                    {modelFilterSummary}
                  </span>
                </div>

                <div className="grid grid-cols-1 gap-2 md:grid-cols-3">
                  {modelFilterOptions.map((option) => (
                    <button
                      key={option.value}
                      type="button"
                      className={`rounded-lg border p-3 text-left transition-colors surface-control ${
                        configForm.model_filter_type === option.value
                          ? 'border-primary-300 bg-primary-50/[0.15] text-primary-900 shadow-sm dark:border-primary-700 dark:bg-primary-900/20 dark:text-primary-100'
                          : 'border-gray-100 hover:surface-hover dark:border-dark-700 dark:hover:surface-hover'
                      }`}
                      onClick={() => setModelFilterType(option.value)}
                    >
                      <div className="flex items-center justify-between gap-2">
                        <span className="text-sm font-semibold">{option.label}</span>
                        <span
                          className={`flex h-4 w-4 flex-shrink-0 items-center justify-center rounded-full border ${
                            configForm.model_filter_type === option.value
                              ? 'border-primary-500 bg-primary-500 text-white'
                              : 'border-gray-300 text-transparent dark:border-dark-500'
                          }`}
                        >
                          <Icon name="check" size="xs" strokeWidth={2} />
                        </span>
                      </div>
                      <p className="mt-1 text-xs leading-5 surface-muted">{option.description}</p>
                    </button>
                  ))}
                </div>

                {configForm.model_filter_type !== 'all' && (
                  <div className="space-y-2">
                    <label className="input-label">{t('admin.riskControl.modelFilterModels')}</label>
                    <ModelWhitelistSelector
                      value={configForm.model_filter_models}
                      onValueChange={(models) => patchConfig({ model_filter_models: models })}
                    />
                    <p className="text-xs surface-muted">
                      {t('admin.riskControl.modelFilterModelCount', { count: modelFilterModelCount })}
                    </p>
                  </div>
                )}
              </div>
            </div>
          )}

          {activeSettingsTab === 'runtime' && (
            <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
              <div>
                <label className="input-label">{t('admin.riskControl.workerCount')}</label>
                <input
                  value={configForm.worker_count}
                  type="number"
                  min={1}
                  max={32}
                  className="input"
                  onChange={(event) => patchConfig({ worker_count: Number(event.target.value) })}
                />
              </div>
              <div>
                <label className="input-label">{t('admin.riskControl.queueSize')}</label>
                <input
                  value={configForm.queue_size}
                  type="number"
                  min={100}
                  max={100000}
                  className="input"
                  onChange={(event) => patchConfig({ queue_size: Number(event.target.value) })}
                />
              </div>
              <div className="flex items-center justify-between rounded-lg border border-gray-100 p-4 dark:border-dark-700 lg:col-span-2">
                <div>
                  <p className="text-sm font-medium text-gray-900 dark:text-white">{t('admin.riskControl.recordNonHits')}</p>
                  <p className="mt-1 text-xs surface-muted">{t('admin.riskControl.recordNonHitsHint')}</p>
                </div>
                <Toggle
                  checked={configForm.record_non_hits}
                  onCheckedChange={(checked) => patchConfig({ record_non_hits: checked })}
                  ariaLabel={t('admin.riskControl.recordNonHits')}
                />
              </div>
              <div className="space-y-4 rounded-lg border border-gray-100 p-4 dark:border-dark-700 lg:col-span-2">
                <div className="flex items-center justify-between gap-4">
                  <div>
                    <p className="text-sm font-medium text-gray-900 dark:text-white">{t('admin.riskControl.preHashCheck')}</p>
                    <p className="mt-1 text-xs surface-muted">{t('admin.riskControl.preHashCheckHint')}</p>
                  </div>
                  <Toggle
                    checked={configForm.pre_hash_check_enabled}
                    onCheckedChange={(checked) => patchConfig({ pre_hash_check_enabled: checked })}
                    ariaLabel={t('admin.riskControl.preHashCheck')}
                  />
                </div>
                <div className="rounded-lg surface-inset p-3">
                  <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
                    <div>
                      <p className="text-sm font-medium text-gray-900 dark:text-white">
                        {t('admin.riskControl.flaggedHashCount', {
                          count: formatNumber(status?.flagged_hash_count ?? 0)
                        })}
                      </p>
                      <p className="mt-1 text-xs surface-muted">{t('admin.riskControl.flaggedHashHint')}</p>
                    </div>
                    <button
                      type="button"
                      className="btn btn-secondary inline-flex items-center justify-center gap-2 text-red-600 hover:text-red-700 dark:text-red-300"
                      disabled={hashActionLoading || (status?.flagged_hash_count ?? 0) === 0}
                      onClick={() => setClearHashesConfirmOpen(true)}
                    >
                      <Icon name="trash" size="sm" className={hashActionLoading ? 'animate-pulse' : undefined} />
                      {t('admin.riskControl.clearFlaggedHashes')}
                    </button>
                  </div>
                  <div className="mt-3 flex flex-col gap-2 sm:flex-row">
                    <input
                      value={flaggedHashInput}
                      type="text"
                      className="input font-mono text-sm"
                      placeholder={t('admin.riskControl.flaggedHashPlaceholder')}
                      onChange={(event) => setFlaggedHashInput(event.target.value)}
                    />
                    <button
                      type="button"
                      className="btn btn-secondary inline-flex items-center justify-center gap-2"
                      disabled={hashActionLoading || !isFlaggedHashInputValid}
                      onClick={() => void deleteFlaggedHash()}
                    >
                      <Icon name="trash" size="sm" />
                      {t('admin.riskControl.deleteFlaggedHash')}
                    </button>
                  </div>
                </div>
              </div>
            </div>
          )}

          {activeSettingsTab === 'response' && (
            <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
              <div>
                <label className="input-label">{t('admin.riskControl.blockStatus')}</label>
                <input
                  value={configForm.block_status}
                  type="number"
                  min={400}
                  max={599}
                  className="input"
                  onChange={(event) => patchConfig({ block_status: Number(event.target.value) })}
                />
              </div>
              <div>
                <label className="input-label">{t('admin.riskControl.blockMessage')}</label>
                <input
                  value={configForm.block_message}
                  type="text"
                  className="input"
                  onChange={(event) => patchConfig({ block_message: event.target.value })}
                />
              </div>
              <div className="flex items-center justify-between rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div>
                  <p className="text-sm font-medium text-gray-900 dark:text-white">{t('admin.riskControl.emailOnHit')}</p>
                  <p className="mt-1 text-xs surface-muted">{t('admin.riskControl.emailOnHitHint')}</p>
                </div>
                <Toggle
                  checked={configForm.email_on_hit}
                  onCheckedChange={(checked) => patchConfig({ email_on_hit: checked })}
                  ariaLabel={t('admin.riskControl.emailOnHit')}
                />
              </div>
              <div className="flex items-center justify-between rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div>
                  <p className="text-sm font-medium text-gray-900 dark:text-white">{t('admin.riskControl.autoBan')}</p>
                  <p className="mt-1 text-xs surface-muted">{t('admin.riskControl.autoBanHint')}</p>
                </div>
                <Toggle
                  checked={configForm.auto_ban_enabled}
                  onCheckedChange={(checked) => patchConfig({ auto_ban_enabled: checked })}
                  ariaLabel={t('admin.riskControl.autoBan')}
                />
              </div>
              <div className="flex items-center justify-between rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div>
                  <p className="text-sm font-medium text-gray-900 dark:text-white">
                    {t('admin.riskControl.cyberPolicyAutoBan')}
                  </p>
                  <p className="mt-1 text-xs surface-muted">{t('admin.riskControl.cyberPolicyAutoBanHint')}</p>
                </div>
                <Toggle
                  checked={configForm.cyber_policy_auto_ban_enabled}
                  onCheckedChange={(checked) => patchConfig({ cyber_policy_auto_ban_enabled: checked })}
                  ariaLabel={t('admin.riskControl.cyberPolicyAutoBan')}
                />
              </div>
              <div className="flex items-center justify-between rounded-lg border border-gray-100 p-4 dark:border-dark-700 lg:col-span-2">
                <div>
                  <p className="text-sm font-medium text-gray-900 dark:text-white">
                    {t('admin.riskControl.cyberPolicyExcludeBan')}
                  </p>
                  <p className="mt-1 text-xs surface-muted">{t('admin.riskControl.cyberPolicyExcludeBanHint')}</p>
                </div>
                <Toggle
                  checked={configForm.cyber_policy_exclude_from_ban_count}
                  onCheckedChange={(checked) => patchConfig({ cyber_policy_exclude_from_ban_count: checked })}
                  ariaLabel={t('admin.riskControl.cyberPolicyExcludeBan')}
                />
              </div>
              <div>
                <label className="input-label">{t('admin.riskControl.banThreshold')}</label>
                <input
                  value={configForm.ban_threshold}
                  type="number"
                  min={1}
                  max={1000}
                  className="input"
                  onChange={(event) => patchConfig({ ban_threshold: Number(event.target.value) })}
                />
              </div>
              <div>
                <label className="input-label">{t('admin.riskControl.violationWindowHours')}</label>
                <input
                  value={configForm.violation_window_hours}
                  type="number"
                  min={1}
                  max={8760}
                  className="input"
                  onChange={(event) => patchConfig({ violation_window_hours: Number(event.target.value) })}
                />
              </div>
              <div className="flex items-center justify-between rounded-lg border border-gray-100 p-4 dark:border-dark-700 lg:col-span-2">
                <div>
                  <p className="text-sm font-medium text-gray-900 dark:text-white">{t('admin.riskControl.sessionBlock')}</p>
                  <p className="mt-1 text-xs surface-muted">{t('admin.riskControl.sessionBlockHint')}</p>
                </div>
                <Toggle
                  checked={configForm.session_block_enabled}
                  onCheckedChange={(checked) => patchConfig({ session_block_enabled: checked })}
                  ariaLabel={t('admin.riskControl.sessionBlock')}
                />
              </div>
              <div>
                <label className="input-label">{t('admin.riskControl.sessionBlockTTL')}</label>
                <input
                  value={configForm.session_block_ttl_seconds}
                  type="number"
                  min={60}
                  max={7776000}
                  className="input"
                  onChange={(event) => patchConfig({ session_block_ttl_seconds: Number(event.target.value) })}
                />
                <p className="mt-1 text-xs surface-muted">{t('admin.riskControl.sessionBlockTTLHint')}</p>
              </div>
            </div>
          )}

          {activeSettingsTab === 'riskThresholds' && (
            <div className="space-y-5">
              <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                <div>
                  <h3 className="text-base font-semibold text-gray-900 dark:text-white">
                    {t('admin.riskControl.riskThresholds')}
                  </h3>
                  <p className="mt-1 text-sm surface-muted">{t('admin.riskControl.riskThresholdsHint')}</p>
                </div>
                <button
                  type="button"
                  className="btn btn-secondary inline-flex items-center justify-center gap-2"
                  onClick={resetRiskThresholds}
                >
                  <Icon name="refresh" size="sm" />
                  {t('admin.riskControl.riskThresholdReset')}
                </button>
              </div>

              <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
                {riskThresholdRows.map((row) => (
                  <div
                    key={row.category}
                    className="rounded-lg border border-gray-100 surface-inset p-4 dark:border-dark-700"
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <label
                          className="block truncate text-sm font-semibold text-gray-900 dark:text-white"
                          htmlFor={`risk-threshold-${row.category}`}
                        >
                          {row.category}
                        </label>
                        <p className="mt-1 text-xs surface-muted">
                          {t('admin.riskControl.riskThresholdDefault', {
                            value: formatThresholdPercent(row.defaultValue)
                          })}
                        </p>
                      </div>
                      <span className="inline-flex shrink-0 rounded-md surface-inset px-2 py-1 font-mono text-xs font-medium shadow-sm surface-muted">
                        {formatThresholdPercent(row.value)}
                      </span>
                    </div>
                    <div className="mt-3">
                      <label className="sr-only" htmlFor={`risk-threshold-${row.category}`}>
                        {t('admin.riskControl.riskThresholdPercent')}
                      </label>
                      <div className="relative">
                        <input
                          id={`risk-threshold-${row.category}`}
                          value={configForm.thresholds[row.category] ?? riskThresholdDefaults[row.category]}
                          data-test={`risk-threshold-${row.category}`}
                          type="number"
                          min={0}
                          max={100}
                          step="0.1"
                          className="input pr-8 font-mono"
                          onChange={(event) =>
                            patchConfig({
                              thresholds: {
                                ...configForm.thresholds,
                                [row.category]: Number(event.target.value)
                              }
                            })
                          }
                        />
                        <span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 surface-muted">%</span>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {activeSettingsTab === 'keywords' && (
            <div className="space-y-5">
              <div className={`flex items-start gap-3 rounded-lg border p-4 ${keywordNotice.toneClass}`}>
                <Icon name={keywordNotice.icon} size="md" className={keywordNotice.iconClass} />
                <div className="text-sm leading-6">
                  <p className={`font-medium ${keywordNotice.titleClass}`}>{keywordNotice.title}</p>
                  <p className="mt-1 text-xs surface-muted">{keywordNotice.description}</p>
                </div>
              </div>

              <div className="space-y-2">
                <label className="input-label">{t('admin.riskControl.keywordBlockingMode')}</label>
                <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
                  {keywordBlockingModeOptions.map((option) => (
                    <button
                      key={option.value}
                      type="button"
                      className={`rounded-lg border p-3 text-left transition-colors surface-control ${
                        configForm.keyword_blocking_mode === option.value
                          ? 'border-primary-300 bg-primary-50/[0.15] text-primary-900 shadow-sm dark:border-primary-700 dark:bg-primary-900/20 dark:text-primary-100'
                          : 'border-gray-100 hover:surface-hover dark:border-dark-700 dark:hover:surface-hover'
                      }`}
                      onClick={() => patchConfig({ keyword_blocking_mode: option.value })}
                    >
                      <div className="flex items-center justify-between gap-2">
                        <span className="text-sm font-semibold">{option.label}</span>
                        <span
                          className={`flex h-4 w-4 flex-shrink-0 items-center justify-center rounded-full border ${
                            configForm.keyword_blocking_mode === option.value
                              ? 'border-primary-500 bg-primary-500 text-white'
                              : 'border-gray-300 text-transparent dark:border-dark-500'
                          }`}
                        >
                          <Icon name="check" size="xs" strokeWidth={2} />
                        </span>
                      </div>
                      <p className="mt-1 text-xs leading-5 surface-muted">{option.description}</p>
                    </button>
                  ))}
                </div>
              </div>

              <div>
                <div className="mb-2 flex items-center justify-between">
                  <label className="input-label mb-0">{t('admin.riskControl.blockedKeywords')}</label>
                  <span className="inline-flex rounded-md surface-inset px-2 py-1 text-xs surface-muted">
                    {t('admin.riskControl.blockedKeywordCount', { count: blockedKeywordCount })}
                  </span>
                </div>
                <textarea
                  value={configForm.blocked_keywords_text}
                  className="input min-h-52 resize-y font-mono text-sm"
                  placeholder={t('admin.riskControl.blockedKeywordsPlaceholder')}
                  disabled={configForm.keyword_blocking_mode === 'api_only'}
                  onChange={(event) => patchConfig({ blocked_keywords_text: event.target.value })}
                />
                <p className="mt-2 text-xs surface-muted">
                  {t('admin.riskControl.blockedKeywordsLimit', { max: blockedKeywordMax })}
                </p>
              </div>
            </div>
          )}

          {activeSettingsTab === 'sessionBlocks' && (
            <div className="space-y-5">
              <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                <div>
                  <h3 className="text-base font-semibold text-gray-900 dark:text-white">
                    {t('admin.riskControl.sessionBlocks')}
                  </h3>
                  <p className="mt-1 text-sm surface-muted">{t('admin.riskControl.sessionBlocksHint')}</p>
                </div>
                <button
                  type="button"
                  className="btn btn-secondary inline-flex items-center justify-center gap-2 text-red-600 hover:text-red-700 dark:text-red-300"
                  disabled={sessionActionLoading || sessionBlocks.length === 0}
                  onClick={() => setClearSessionsConfirmOpen(true)}
                >
                  <Icon name="trash" size="sm" />
                  {t('admin.riskControl.clearSessionBlocks')}
                </button>
              </div>
              <p className="text-sm surface-muted">
                {t('admin.riskControl.sessionBlockCount', {
                  count: formatNumber(status?.blocked_session_count ?? sessionPagination.total)
                })}
              </p>
              <div className="flex flex-col gap-2 sm:flex-row">
                <input
                  value={sessionFilters.search}
                  type="search"
                  className="input min-w-0 flex-1"
                  placeholder={t('admin.riskControl.sessionBlockSearch')}
                  onChange={(event) => setSessionFilters({ search: event.target.value })}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') reloadSessionBlocksFromFirstPage()
                  }}
                />
                <button
                  type="button"
                  className="btn btn-secondary shrink-0 whitespace-nowrap"
                  disabled={sessionBlocksLoading}
                  onClick={reloadSessionBlocksFromFirstPage}
                >
                  {t('admin.riskControl.refresh')}
                </button>
              </div>
              <div className="record-table-scroll record-table-dialog-scroll table-container overflow-x-auto">
                <table className="record-table w-full min-w-[760px] divide-y divide-gray-200 dark:divide-dark-700">
                  <thead className="surface-inset [&_th]:whitespace-nowrap">
                    <tr>
                      <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted">
                        {t('admin.riskControl.table.time')}
                      </th>
                      <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted">
                        session_id
                      </th>
                      <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted">
                        {t('admin.riskControl.table.user')}
                      </th>
                      <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted">
                        {t('admin.riskControl.table.apiKey')}
                      </th>
                      <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted">
                        {t('admin.riskControl.sessionBlockExpires')}
                      </th>
                      <th className="px-4 py-3" />
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-gray-100 surface-inset dark:divide-dark-800">
                    {sessionBlocksLoading && (
                      <tr>
                        <td colSpan={6} className="px-4 py-10 text-center text-sm surface-muted">
                          {t('common.loading')}
                        </td>
                      </tr>
                    )}
                    {!sessionBlocksLoading && sessionBlocks.length === 0 && (
                      <tr>
                        <td colSpan={6} className="px-4 py-10 text-center text-sm surface-muted">
                          {t('admin.riskControl.sessionBlockEmpty')}
                        </td>
                      </tr>
                    )}
                    {!sessionBlocksLoading &&
                      sessionBlocks.map((row) => (
                        <tr key={row.id} className="hover:surface-hover">
                          <td className="whitespace-nowrap px-4 py-3 text-sm surface-muted">
                            {formatDateTime(row.created_at)}
                          </td>
                          <td className="px-4 py-3 font-mono text-sm surface-muted">{row.session_id}</td>
                          <td className="whitespace-nowrap px-4 py-3 text-sm surface-muted">{row.user_email || '-'}</td>
                          <td className="whitespace-nowrap px-4 py-3 text-sm surface-muted">{row.api_key_name || '-'}</td>
                          <td className="whitespace-nowrap px-4 py-3 text-sm surface-muted">
                            {formatDateTime(row.expires_at)}
                          </td>
                          <td className="px-4 py-3 text-right">
                            <button
                              type="button"
                              className="btn btn-secondary text-xs"
                              disabled={sessionActionLoading}
                              onClick={() => void deleteSessionBlock(row.block_key)}
                            >
                              {t('admin.riskControl.deleteSessionBlock')}
                            </button>
                          </td>
                        </tr>
                      ))}
                  </tbody>
                </table>
              </div>
              {sessionPagination.total > 0 && (
                <Pagination
                  page={sessionPagination.page}
                  total={sessionPagination.total}
                  pageSize={sessionPagination.page_size}
                  onPageChange={onSessionPageChange}
                  onPageSizeChange={onSessionPageSizeChange}
                />
              )}
            </div>
          )}

          {activeSettingsTab === 'retention' && (
            <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
              <div>
                <label className="input-label">{t('admin.riskControl.hitRetentionDays')}</label>
                <input
                  value={configForm.hit_retention_days}
                  type="number"
                  min={1}
                  max={3650}
                  className="input"
                  onChange={(event) => patchConfig({ hit_retention_days: Number(event.target.value) })}
                />
              </div>
              <div>
                <label className="input-label">{t('admin.riskControl.nonHitRetentionDays')}</label>
                <input
                  value={configForm.non_hit_retention_days}
                  type="number"
                  min={1}
                  max={3}
                  className="input"
                  onChange={(event) => patchConfig({ non_hit_retention_days: Number(event.target.value) })}
                />
              </div>
              <div className="rounded-lg border border-gray-100 p-4 text-sm dark:border-dark-700 lg:col-span-2 surface-muted">
                <div className="flex flex-wrap items-center gap-3">
                  <Icon name="database" size="md" className="text-gray-400" />
                  <span>
                    {t('admin.riskControl.cleanupStats', {
                      hit: status?.last_cleanup_deleted_hit ?? 0,
                      nonHit: status?.last_cleanup_deleted_non_hit ?? 0
                    })}
                  </span>
                </div>
              </div>
            </div>
          )}
        </div>
      </Modal>

      <Modal
        open={policyHelpOpen}
        onClose={() => setPolicyHelpOpen(false)}
        title={t('admin.riskControl.policyHelp.title')}
        size="full"
        footer={
          <Button variant="primary" onClick={() => setPolicyHelpOpen(false)}>
            {t('common.close')}
          </Button>
        }
      >
        <div className="space-y-5">
          <div className="rounded-xl border border-primary-500 surface-inset px-4 py-3">
            <p className="text-sm surface-muted">{t('admin.riskControl.policyHelp.summary')}</p>
            <p className="mt-2 text-sm font-semibold text-gray-900 dark:text-white">
              {t('admin.riskControl.policyHelp.current')}
              {effectiveTextPolicyDescription}
            </p>
          </div>
          <div className="record-table-scroll record-table-dialog-scroll table-container overflow-x-auto">
            <table className="record-table min-w-[900px] w-full text-left text-sm">
              <thead className="border-b border-gray-200 text-xs dark:border-dark-700 surface-muted">
                <tr>
                  <th className="px-3 py-2">{t('admin.riskControl.policyHelp.globalMode')}</th>
                  <th className="px-3 py-2">{t('admin.riskControl.policyHelp.textPolicy')}</th>
                  <th className="px-3 py-2">{t('admin.riskControl.policyHelp.behavior')}</th>
                  <th className="px-3 py-2">{t('admin.riskControl.policyHelp.canBlock')}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100 dark:divide-dark-700">
                {policyMatrixRows.map((row) => (
                  <tr key={`${row.mode}-${row.policy}`}>
                    <td className="px-3 py-3 font-medium text-gray-900 dark:text-white">{row.modeLabel}</td>
                    <td className="px-3 py-3 surface-muted">{row.policyLabel}</td>
                    <td className="px-3 py-3 leading-5 surface-muted">{row.behavior}</td>
                    <td className="px-3 py-3 surface-muted">{row.canBlock}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </Modal>

      <Modal
        open={inputDetailRow !== null}
        onClose={() => setInputDetailRow(null)}
        title={t('admin.riskControl.inputDetailTitle')}
        size="lg"
        footer={
          <div className="flex justify-end">
            <Button variant="secondary" onClick={() => setInputDetailRow(null)}>
              {t('common.close')}
            </Button>
          </div>
        }
      >
        {inputDetailRow && (
          <div className="space-y-5">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
              <div className="rounded-lg border border-gray-100 surface-inset p-4 dark:border-dark-700">
                <p className="text-xs font-medium surface-muted">{t('admin.riskControl.table.time')}</p>
                <p className="mt-1 truncate text-sm font-semibold text-gray-900 dark:text-white">
                  {formatDateTime(inputDetailRow.created_at)}
                </p>
              </div>
              <div className="rounded-lg border border-gray-100 surface-inset p-4 dark:border-dark-700">
                <p className="text-xs font-medium surface-muted">{t('admin.riskControl.table.user')}</p>
                <p className="mt-1 truncate text-sm font-semibold text-gray-900 dark:text-white">
                  {inputDetailRow.user_email || '-'}
                </p>
              </div>
              <div className="rounded-lg border border-gray-100 surface-inset p-4 dark:border-dark-700">
                <p className="text-xs font-medium surface-muted">{t('admin.riskControl.table.result')}</p>
                <span
                  className={`mt-1 inline-flex rounded-md px-2 py-1 text-xs font-medium ${resultBadgeClass(inputDetailRow)}`}
                >
                  {resultLabel(inputDetailRow)}
                </span>
              </div>
              <div className="rounded-lg border border-gray-100 surface-inset p-4 dark:border-dark-700">
                <p className="text-xs font-medium surface-muted">{t('admin.riskControl.table.highest')}</p>
                <p className="mt-1 truncate text-sm font-semibold text-gray-900 dark:text-white">
                  {inputDetailRow.highest_category || '-'} / {percent(inputDetailRow.highest_score)}
                </p>
              </div>
              <div className="rounded-lg border border-gray-100 surface-inset p-4 dark:border-dark-700">
                <p className="text-xs font-medium surface-muted">{t('admin.riskControl.table.moderationPlatform')}</p>
                <p
                  className="mt-1 truncate text-sm font-semibold text-gray-900 dark:text-white"
                  title={inputDetailRow.moderation_endpoint_name || ''}
                >
                  {inputDetailRow.moderation_endpoint_name || '-'}
                </p>
              </div>
              {inputDetailRow.matched_keyword && (
                <div className="rounded-lg border border-red-100 bg-red-50/[0.15] p-4 dark:border-red-900/60 dark:bg-red-900/20">
                  <p className="text-xs font-medium text-red-500 dark:text-red-300">
                    {t('admin.riskControl.matchedKeyword')}
                  </p>
                  <p
                    className="mt-1 truncate text-sm font-semibold text-red-700 dark:text-red-200"
                    title={inputDetailRow.matched_keyword}
                  >
                    {inputDetailRow.matched_keyword}
                  </p>
                </div>
              )}
            </div>

            <div className="rounded-xl border border-gray-100 surface-inset p-4 shadow-sm dark:border-dark-700">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <p className="text-sm font-semibold text-gray-900 dark:text-white">
                    {t('admin.riskControl.inputDetailContent')}
                  </p>
                  <p className="mt-1 text-xs surface-muted">
                    {inputDetailRow.endpoint || '-'} · {inputDetailRow.provider || '-'} / {inputDetailRow.model || '-'}
                  </p>
                  {inputDetailRow.input_content_truncated && (
                    <p className="mt-2 text-xs text-amber-700 dark:text-amber-300">
                      {t('admin.riskControl.inputDetailTruncated')}
                    </p>
                  )}
                </div>
                {inputDetailRow.group_name && (
                  <span className="inline-flex rounded-md bg-sky-50/[0.15] px-2.5 py-1 text-xs font-medium text-sky-700 dark:bg-sky-900/20 dark:text-sky-300">
                    {inputDetailRow.group_name}
                  </span>
                )}
              </div>
              <pre className="mt-4 max-h-[420px] overflow-auto whitespace-pre-wrap break-words rounded-lg surface-inset p-4 text-sm leading-6 text-gray-100 shadow-inner dark:bg-black/50">
                {inputDetailText}
              </pre>
            </div>
          </div>
        )}
      </Modal>

      <ConfirmDialog
        open={clearHashesConfirmOpen}
        title={t('admin.riskControl.clearFlaggedHashes')}
        message={t('admin.riskControl.clearFlaggedHashesConfirm')}
        confirmText={t('admin.riskControl.clearFlaggedHashes')}
        variant="danger"
        onConfirm={() => {
          setClearHashesConfirmOpen(false)
          void clearFlaggedHashes()
        }}
        onCancel={() => setClearHashesConfirmOpen(false)}
      />

      <ConfirmDialog
        open={clearSessionsConfirmOpen}
        title={t('admin.riskControl.clearSessionBlocks')}
        message={t('admin.riskControl.clearSessionBlocksConfirm')}
        confirmText={t('admin.riskControl.clearSessionBlocks')}
        variant="danger"
        onConfirm={() => {
          setClearSessionsConfirmOpen(false)
          void clearSessionBlocks()
        }}
        onCancel={() => setClearSessionsConfirmOpen(false)}
      />
    </div>
  )
}
