import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { adminAPI } from '@/api'
import type { Account, AccountUsageInfo, GeminiCredentials, WindowStats } from '@/types'
import { buildOpenAIUsageRefreshKey } from '@/utils/accountUsageRefresh'
import { enqueueUsageRequest } from '@/utils/usageLoadQueue'
import { formatCompactNumber } from '@/utils/format'
import { isOfficialGeminiBaseUrl } from '@/utils/geminiExplicitCache'
import { supportsAccountQuota } from '@/utils/accountQuota'
import {
  cnBalanceCellVisible as cnBalanceCellVisibleFn,
  cnQuotaCellVisible as cnQuotaCellVisibleFn,
  isMultiProtocolApiKeyPlatform
} from './credentialsBuilder'
import { UsageProgressBar } from './UsageProgressBar'
import { AccountQuotaInfo } from './AccountQuotaInfo'
import { ClaudeResetCreditsCell } from './ClaudeResetCreditsCell'
import { OpenAIQuotaResetCell } from './OpenAIQuotaResetCell'
import { GrokQuotaProbeCell } from './GrokQuotaProbeCell'
import { CNProviderQuotaCell } from './CNProviderQuotaCell'
import { CNProviderBalanceCell } from './CNProviderBalanceCell'
import { OllamaCloudUsageCell } from './OllamaCloudUsageCell'
import { ZhipuStartPlanCell } from './ZhipuStartPlanCell'
import { TriggerTooltip } from './TriggerTooltip'
import { useMediaQuery } from './shared'

// Module-level cache shared across all AccountUsageCell instances
const _usageCache = new Map<number, { data: AccountUsageInfo; ts: number }>()
const USAGE_CACHE_TTL = 5 * 60 * 1000 // 5 minutes

export interface AccountUsageCellProps {
  account: Account
  todayStats?: WindowStats | null
  todayStatsLoading?: boolean
  manualRefreshToken?: number
  batchedUsage?: AccountUsageInfo | null
  batchedUsageError?: string | null
  batchedUsageLoading?: boolean
  requestBatchedUsage?: ((account: Account, options?: { force?: boolean }) => void) | null
  onAccountUpdated: (account: Account) => void
  onUsageLoaded: (usage: AccountUsageInfo) => void
}

interface AntigravityUsageResult {
  utilization: number
  resetTime: string | null
}

interface QuotaBarInfo {
  utilization: number
  resetsAt: string | null
}

interface GrokQuotaBarInfo {
  utilization: number
  resetsAt: string | null
  windowStats?: WindowStats | null
}

export function AccountUsageCell({
  account,
  todayStats = null,
  todayStatsLoading = false,
  manualRefreshToken = 0,
  batchedUsage = null,
  batchedUsageError = null,
  batchedUsageLoading = false,
  requestBatchedUsage = null,
  onAccountUpdated,
  onUsageLoaded
}: AccountUsageCellProps) {
  const { t } = useTranslation()
  const rootRef = useRef<HTMLDivElement | null>(null)
  const unmountedRef = useRef(false)
  const isDesktopViewport = useMediaQuery('(min-width: 768px)')
  const [hasEnteredViewport, setHasEnteredViewport] = useState(false)
  const pendingAutoLoad = useRef(false)
  const pendingAutoLoadSource = useRef<'passive' | 'active' | undefined>(undefined)
  const activeUsageRequestID = useRef(0)

  const [loading, setLoading] = useState(false)
  const [activeQueryLoading, setActiveQueryLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [usageInfo, setUsageInfo] = useState<AccountUsageInfo | null>(null)
  const [linkCopied, setLinkCopied] = useState(false)

  useEffect(() => {
    unmountedRef.current = false
    return () => {
      unmountedRef.current = true
    }
  }, [])

  useEffect(() => {
    if (usageInfo) onUsageLoaded(usageInfo)
  }, [usageInfo])

  // Gemini 第三方上游 API Key：没有 AI Studio 等级与本地模拟配额
  const isGeminiThirdPartyApiKey = (() => {
    if (account.platform !== 'gemini' || account.type !== 'apikey') return false
    const creds = account.credentials as GeminiCredentials | undefined
    return !isOfficialGeminiBaseUrl(creds?.base_url)
  })()

  const isGeminiVertex = account.platform === 'gemini' && account.type === 'service_account'

  // Show usage windows for OAuth and Setup Token accounts
  const showUsageWindows = (() => {
    // Gemini: we can always compute local usage windows from DB logs (simulated quotas).
    // 第三方上游 API Key 没有模拟配额，按普通 Key 账号展示今日统计与额度进度条。
    if (account.platform === 'gemini') return !isGeminiThirdPartyApiKey
    // 多协议 API Key 供应商：apikey 账号也有滚动用量窗口（coding plan / 订阅）或余额，
    // 由 CNProviderQuotaCell / CNProviderBalanceCell 自行探测与展示。
    if (isMultiProtocolApiKeyPlatform(account.platform)) {
      return true
    }
    return account.type === 'oauth' || account.type === 'setup-token'
  })()

  const shouldFetchUsage = (() => {
    if (account.platform === 'anthropic') {
      return account.type === 'oauth' || account.type === 'setup-token'
    }
    if (account.platform === 'gemini') {
      return !isGeminiThirdPartyApiKey && !isGeminiVertex
    }
    if (account.platform === 'antigravity') {
      return account.type === 'oauth'
    }
    if (account.platform === 'grok') {
      return account.type === 'oauth'
    }
    if (account.platform === 'openai') {
      return account.type === 'oauth'
    }
    return false
  })()

  // CN 供应商子单元格可见性（与 CNProviderQuotaCell / CNProviderBalanceCell 共用
  // credentialsBuilder 的单一实现）：都不可见时显示 `-` 占位符。
  const cnQuotaCellVisible = cnQuotaCellVisibleFn(account)
  const cnBalanceCellVisible = cnBalanceCellVisibleFn(account)
  const zhipuStartPlanVisible =
    account.platform === 'zhipu' &&
    account.type === 'oauth' &&
    account.credentials?.plan_kind === 'start-plan' &&
    ['bigmodel', 'zai'].includes(String(account.credentials?.oauth_provider))

  const isBatchManaged = typeof requestBatchedUsage === 'function'

  // Vertex 没有本地模拟配额，用量窗口展示今日统计
  const showGeminiTodayStats = isGeminiVertex

  const geminiUsageAvailable =
    !!usageInfo?.gemini_shared_daily ||
    !!usageInfo?.gemini_pro_daily ||
    !!usageInfo?.gemini_flash_daily ||
    !!usageInfo?.gemini_shared_minute ||
    !!usageInfo?.gemini_pro_minute ||
    !!usageInfo?.gemini_flash_minute

  const hasOpenAIUsageFallback =
    account.platform === 'openai' && account.type === 'oauth' && (!!usageInfo?.five_hour || !!usageInfo?.seven_day)

  const openAISevenDayEstimatedTotalCost = (() => {
    const sevenDay = usageInfo?.seven_day
    const utilization = sevenDay?.utilization
    const currentCost = sevenDay?.window_stats?.cost
    if (
      typeof utilization !== 'number' ||
      typeof currentCost !== 'number' ||
      !Number.isFinite(utilization) ||
      !Number.isFinite(currentCost) ||
      utilization <= 0 ||
      currentCost <= 0
    ) {
      return null
    }

    const estimate = (currentCost * 100) / utilization
    return Number.isFinite(estimate) && estimate > 0 ? estimate : null
  })()

  const openAIUsageRefreshKey = buildOpenAIUsageRefreshKey(account)

  const shouldAutoLoadUsageOnMount = shouldFetchUsage

  const shouldLazyLoadOnMobile = shouldFetchUsage && !isDesktopViewport

  // ===== Antigravity quota groups (usageInfo.antigravity_quota_groups) =====
  // The summary reports each group's real 5-hour and weekly window. When present it replaces the
  // per-model bars below, which show only whichever window is tighter for each model.
  const antigravityQuotaGroupBars = useMemo(() => {
    const groups = usageInfo?.antigravity_quota_groups
    if (!groups || groups.length === 0) return []
    const bars: Array<{
      key: string
      label: string
      title: string
      utilization: number
      resetsAt: string | null
      color: 'indigo' | 'emerald' | 'purple' | 'amber'
    }> = []
    for (const group of groups) {
      // 'gemini' = Gemini models, 'gemini-argon-wtu' = the separate Argon quota of Pro plans,
      // '3p' (or anything else) = Antigravity's Claude and GPT models.
      const family = group.key === 'gemini' ? 'Gemini' : group.key.startsWith('gemini-argon') ? 'Argon' : 'Other'
      for (const window of group.windows || []) {
        const weekly = window.window === 'weekly'
        const labelKey = `admin.accounts.usageWindow.antigravity${family}${weekly ? 'Weekly' : '5h'}`
        const color: 'indigo' | 'emerald' | 'purple' | 'amber' =
          family === 'Gemini' ? (weekly ? 'indigo' : 'emerald') : family === 'Argon' ? 'indigo' : weekly ? 'purple' : 'amber'
        bars.push({
          key: `${group.key}-${window.window}`,
          label: t(labelKey),
          title: group.name,
          utilization: Math.round(window.utilization ?? 0),
          resetsAt: window.resets_at ?? null,
          color
        })
      }
    }
    return bars
  }, [usageInfo?.antigravity_quota_groups, t])

  // ===== Antigravity quota from API (usageInfo.antigravity_quota) =====

  // 检查是否有从 API 获取的配额数据
  const hasAntigravityQuotaFromAPI = !!usageInfo?.antigravity_quota && Object.keys(usageInfo.antigravity_quota).length > 0

  // 从 API 配额数据中获取使用率（多模型取最高使用率）
  const getAntigravityUsageFromAPI = (modelNames: string[]): AntigravityUsageResult | null => {
    const quota = usageInfo?.antigravity_quota
    if (!quota) return null

    let maxUtilization = 0
    let earliestReset: string | null = null

    for (const model of modelNames) {
      const modelQuota = quota[model]
      if (!modelQuota) continue

      if (modelQuota.utilization > maxUtilization) {
        maxUtilization = modelQuota.utilization
      }
      if (modelQuota.reset_time) {
        if (!earliestReset || modelQuota.reset_time < earliestReset) {
          earliestReset = modelQuota.reset_time
        }
      }
    }

    // 如果没有找到任何匹配的模型
    if (maxUtilization === 0 && earliestReset === null) {
      const hasAnyData = modelNames.some((m) => quota[m])
      if (!hasAnyData) return null
    }

    return {
      utilization: maxUtilization,
      resetTime: earliestReset
    }
  }

  // Gemini 3 Pro from API
  const antigravity3ProUsageFromAPI = getAntigravityUsageFromAPI(['gemini-3-pro-low', 'gemini-3-pro-high', 'gemini-3-pro-preview'])

  // Gemini 3 Flash from API
  const antigravity3FlashUsageFromAPI = getAntigravityUsageFromAPI(['gemini-3-flash'])

  // Gemini Image from API
  const antigravity3ImageUsageFromAPI = getAntigravityUsageFromAPI(['gemini-2.5-flash-image', 'gemini-3.1-flash-image', 'gemini-3-pro-image'])

  // Claude from API (all Claude model variants)
  const antigravityClaudeUsageFromAPI = getAntigravityUsageFromAPI([
    'claude-fable-5-1',
    'claude-fable-5',
    'claude-sonnet-4-5',
    'claude-opus-4-5-thinking',
    'claude-sonnet-4-6',
    'claude-opus-4-6',
    'claude-opus-4-6-thinking',
    'claude-opus-4-7',
    'claude-opus-4-8'
  ])

  const aiCreditsDisplay = (() => {
    const credits = usageInfo?.ai_credits
    if (!credits || credits.length === 0) return null
    const total = credits.reduce((sum, credit) => sum + (credit.amount ?? 0), 0)
    if (total <= 0) return null
    return total.toFixed(0)
  })()

  // Antigravity 账户类型（从 load_code_assist 响应中提取）
  const antigravityTier = (() => {
    const extra = account.extra as Record<string, unknown> | undefined
    if (!extra) return null

    const loadCodeAssist = extra.load_code_assist as Record<string, unknown> | undefined
    if (!loadCodeAssist) return null

    // 优先取 paidTier，否则取 currentTier
    const paidTier = loadCodeAssist.paidTier as Record<string, unknown> | undefined
    if (paidTier && typeof paidTier.id === 'string') {
      return paidTier.id
    }

    const currentTier = loadCodeAssist.currentTier as Record<string, unknown> | undefined
    if (currentTier && typeof currentTier.id === 'string') {
      return currentTier.id
    }

    return null
  })()

  // Gemini 账户类型（从 credentials 中提取）
  const geminiTier = (() => {
    if (account.platform !== 'gemini') return null
    const creds = account.credentials as GeminiCredentials | undefined
    return creds?.tier_id || null
  })()

  const geminiOAuthType = (() => {
    if (account.platform !== 'gemini') return null
    const creds = account.credentials as GeminiCredentials | undefined
    return (creds?.oauth_type || '').trim() || null
  })()

  const showGeminiQuotaPolicy = !isGeminiVertex

  // Gemini 是否为 Code Assist OAuth
  const isGeminiCodeAssist = (() => {
    if (account.platform !== 'gemini') return false
    const creds = account.credentials as GeminiCredentials | undefined
    return creds?.oauth_type === 'code_assist' || (!creds?.oauth_type && !!creds?.project_id)
  })()

  const geminiChannelShort = ((): 'ai studio' | 'vertex' | 'gcp' | 'google one' | 'client' | null => {
    if (account.platform !== 'gemini') return null

    if (isGeminiVertex) return 'vertex'
    // API Key accounts are AI Studio.
    if (account.type === 'apikey') return 'ai studio'

    if (geminiOAuthType === 'google_one') return 'google one'
    if (isGeminiCodeAssist) return 'gcp'
    if (geminiOAuthType === 'ai_studio') return 'client'

    // Fallback (unknown legacy data): treat as AI Studio.
    return 'ai studio'
  })()

  const geminiUserLevel = ((): string | null => {
    if (account.platform !== 'gemini') return null
    if (isGeminiVertex) return null

    const tier = (geminiTier || '').toString().trim()
    const tierLower = tier.toLowerCase()
    const tierUpper = tier.toUpperCase()

    // Google One: free / pro / ultra
    if (geminiOAuthType === 'google_one') {
      if (tierLower === 'google_one_free') return 'free'
      if (tierLower === 'google_ai_pro') return 'pro'
      if (tierLower === 'google_ai_ultra') return 'ultra'

      // Backward compatibility (legacy tier markers)
      if (tierUpper === 'AI_PREMIUM' || tierUpper === 'GOOGLE_ONE_STANDARD') return 'pro'
      if (tierUpper === 'GOOGLE_ONE_UNLIMITED') return 'ultra'
      if (tierUpper === 'FREE' || tierUpper === 'GOOGLE_ONE_BASIC' || tierUpper === 'GOOGLE_ONE_UNKNOWN' || tierUpper === '') return 'free'

      return null
    }

    // GCP Code Assist: standard / enterprise
    if (isGeminiCodeAssist) {
      if (tierLower === 'gcp_enterprise') return 'enterprise'
      if (tierLower === 'gcp_standard') return 'standard'

      // Backward compatibility
      if (tierUpper.includes('ULTRA') || tierUpper.includes('ENTERPRISE')) return 'enterprise'
      return 'standard'
    }

    // AI Studio (API Key) and Client OAuth: free / paid
    if (account.type === 'apikey' || geminiOAuthType === 'ai_studio') {
      if (tierLower === 'aistudio_paid') return 'paid'
      if (tierLower === 'aistudio_free') return 'free'

      // Backward compatibility
      if (tierUpper.includes('PAID') || tierUpper.includes('PAYG') || tierUpper.includes('PAY')) return 'paid'
      if (tierUpper.includes('FREE')) return 'free'
      if (account.type === 'apikey') return 'free'
      return null
    }

    return null
  })()

  // Gemini 认证类型（按要求：授权方式简称 + 用户等级）
  const geminiAuthTypeLabel = (() => {
    if (account.platform !== 'gemini') return null
    if (!geminiChannelShort) return null
    return geminiUserLevel ? `${geminiChannelShort} ${geminiUserLevel}` : geminiChannelShort
  })()

  // Gemini 账户类型徽章样式（统一样式）
  const geminiTierClass = (() => {
    // Use channel+level to choose a stable color without depending on raw tier_id variants.
    const channel = geminiChannelShort
    const level = geminiUserLevel

    if (channel === 'client' || channel === 'ai studio' || channel === 'vertex') {
      return 'bg-blue-100/[0.15] text-blue-600 dark:bg-blue-900/40 dark:text-blue-300'
    }

    if (channel === 'google one') {
      if (level === 'ultra') return 'bg-purple-100/[0.15] text-purple-600 dark:bg-purple-900/40 dark:text-purple-300'
      if (level === 'pro') return 'bg-blue-100/[0.15] text-blue-600 dark:bg-blue-900/40 dark:text-blue-300'
      return 'surface-inset surface-muted dark:surface-inset'
    }

    if (channel === 'gcp') {
      if (level === 'enterprise') return 'bg-purple-100/[0.15] text-purple-600 dark:bg-purple-900/40 dark:text-purple-300'
      return 'bg-blue-100/[0.15] text-blue-600 dark:bg-blue-900/40 dark:text-blue-300'
    }

    return ''
  })()

  // Gemini 配额政策信息
  const geminiQuotaPolicyChannel = (() => {
    if (geminiOAuthType === 'google_one') {
      return t('admin.accounts.gemini.quotaPolicy.rows.googleOne.channel')
    }
    if (isGeminiCodeAssist) {
      return t('admin.accounts.gemini.quotaPolicy.rows.gcp.channel')
    }
    return t('admin.accounts.gemini.quotaPolicy.rows.aiStudio.channel')
  })()

  const geminiQuotaPolicyLimits = (() => {
    const tierLower = (geminiTier || '').toString().trim().toLowerCase()

    if (geminiOAuthType === 'google_one') {
      if (tierLower === 'google_ai_ultra' || geminiUserLevel === 'ultra') {
        return t('admin.accounts.gemini.quotaPolicy.rows.googleOne.limitsUltra')
      }
      if (tierLower === 'google_ai_pro' || geminiUserLevel === 'pro') {
        return t('admin.accounts.gemini.quotaPolicy.rows.googleOne.limitsPro')
      }
      return t('admin.accounts.gemini.quotaPolicy.rows.googleOne.limitsFree')
    }

    if (isGeminiCodeAssist) {
      if (tierLower === 'gcp_enterprise' || geminiUserLevel === 'enterprise') {
        return t('admin.accounts.gemini.quotaPolicy.rows.gcp.limitsEnterprise')
      }
      return t('admin.accounts.gemini.quotaPolicy.rows.gcp.limitsStandard')
    }

    // AI Studio (API Key / custom OAuth)
    if (tierLower === 'aistudio_paid' || geminiUserLevel === 'paid') {
      return t('admin.accounts.gemini.quotaPolicy.rows.aiStudio.limitsPaid')
    }
    return t('admin.accounts.gemini.quotaPolicy.rows.aiStudio.limitsFree')
  })()

  const geminiQuotaPolicyDocsUrl =
    geminiOAuthType === 'google_one' || isGeminiCodeAssist
      ? 'https://developers.google.com/gemini-code-assist/resources/quotas'
      : 'https://ai.google.dev/pricing'

  const geminiUsesSharedDaily = (() => {
    if (account.platform !== 'gemini') return false
    // Per requirement: Google One & GCP are shared RPD pools (no per-model breakdown).
    return (
      !!usageInfo?.gemini_shared_daily ||
      !!usageInfo?.gemini_shared_minute ||
      geminiOAuthType === 'google_one' ||
      isGeminiCodeAssist
    )
  })()

  const geminiUsageBars = useMemo(() => {
    if (account.platform !== 'gemini') return []
    if (!usageInfo) return []

    const bars: Array<{
      key: string
      label: string
      utilization: number
      resetsAt: string | null
      windowStats?: WindowStats | null
      color: 'indigo' | 'emerald'
    }> = []

    if (geminiUsesSharedDaily) {
      const sharedDaily = usageInfo.gemini_shared_daily
      if (sharedDaily) {
        bars.push({
          key: 'shared_daily',
          label: '1d',
          utilization: sharedDaily.utilization,
          resetsAt: sharedDaily.resets_at,
          windowStats: sharedDaily.window_stats,
          color: 'indigo'
        })
      }
      return bars
    }

    const pro = usageInfo.gemini_pro_daily
    if (pro) {
      bars.push({
        key: 'pro_daily',
        label: 'pro',
        utilization: pro.utilization,
        resetsAt: pro.resets_at,
        windowStats: pro.window_stats,
        color: 'indigo'
      })
    }

    const flash = usageInfo.gemini_flash_daily
    if (flash) {
      bars.push({
        key: 'flash_daily',
        label: 'flash',
        utilization: flash.utilization,
        resetsAt: flash.resets_at,
        windowStats: flash.window_stats,
        color: 'emerald'
      })
    }

    return bars
  }, [account.platform, usageInfo, geminiUsesSharedDaily])

  const grokBilling = usageInfo?.grok_billing || null
  const grokLocalUsage7d = usageInfo?.grok_local_usage_7d || usageInfo?.seven_day?.window_stats || null
  const grokLocalUsageMonthly = usageInfo?.grok_local_usage_monthly || usageInfo?.thirty_day?.window_stats || null
  const grokWeeklyBillingBar: GrokQuotaBarInfo | null = (() => {
    const billing = grokBilling
    if (billing?.period_type?.toLowerCase() !== 'weekly' || billing.usage_percent == null) {
      return null
    }
    return {
      utilization: Math.min(100, Math.max(0, billing.usage_percent)),
      resetsAt: billing.period_end || null,
      windowStats: grokLocalUsage7d
    }
  })()
  // Monthly used/limit % from billing probe (used_percent or derived from cents).
  const grokMonthlyBillingBar: GrokQuotaBarInfo | null = (() => {
    const billing = grokBilling
    if (!billing) return null
    let utilization: number | null = null
    if (billing.used_percent != null && Number.isFinite(billing.used_percent)) {
      utilization = billing.used_percent
    } else if (billing.monthly_limit_cents != null && billing.monthly_limit_cents > 0 && billing.used_cents != null) {
      utilization = (billing.used_cents / billing.monthly_limit_cents) * 100
    }
    if (utilization == null) return null
    // Avoid duplicating the weekly bar when period_type is weekly-only without monthly.
    if (billing.period_type?.toLowerCase() === 'weekly' && billing.monthly_limit_cents == null) {
      return null
    }
    return {
      utilization: Math.min(100, Math.max(0, utilization)),
      resetsAt: billing.billing_period_end || billing.period_end || null,
      windowStats: grokLocalUsageMonthly
    }
  })()
  const formatGrokMoney = (value?: number | null) => {
    if (value == null || Number.isNaN(value)) return '0'
    if (value >= 1000) return formatCompactNumber(value)
    if (value >= 100) return value.toFixed(0)
    if (value >= 10) return value.toFixed(1)
    return value.toFixed(2)
  }
  // Prepaid chip only when there is a positive prepaid balance.
  // Used/limit only when monthly limit is a positive number (0 means unlimited / unset).
  const grokPrepaidMoneyLine = (() => {
    const billing = grokBilling
    if (!billing) return null
    const prepaid = billing.prepaid_balance
    const showPrepaid = prepaid != null && Number.isFinite(prepaid) && prepaid > 0
    const limitRaw =
      billing.monthly_limit != null
        ? billing.monthly_limit
        : billing.monthly_limit_cents != null
          ? billing.monthly_limit_cents / 100
          : null
    const showUsedLimit = limitRaw != null && Number.isFinite(limitRaw) && limitRaw > 0
    if (!showPrepaid && !showUsedLimit) return null
    const used =
      billing.monthly_used != null
        ? billing.monthly_used
        : billing.used_cents != null
          ? billing.used_cents / 100
          : 0
    return {
      showPrepaid,
      showUsedLimit,
      prepaid: showPrepaid ? formatGrokMoney(prepaid) : null,
      used: showUsedLimit ? formatGrokMoney(used) : null,
      limit: showUsedLimit ? formatGrokMoney(limitRaw) : null
    }
  })()
  const grokPlanLabelIsFree = (value: string) => value.includes('free') || value.includes('basic')
  const grokPlanLabelIsPaid = (value: string) => value !== '' && !grokPlanLabelIsFree(value) && !value.includes('unknown')
  const grokIsFree = (() => {
    if (account.platform !== 'grok' || account.type !== 'oauth') return false
    const billing = grokBilling
    const plan = (billing?.plan || '').trim().toLowerCase()
    const tier = (usageInfo?.subscription_tier || '').trim().toLowerCase()
    const entitlement = (usageInfo?.grok_entitlement_status || '').toLowerCase()
    if (grokPlanLabelIsFree(tier)) return true
    if (grokPlanLabelIsPaid(tier)) return false
    if (
      billing?.usage_percent != null ||
      billing?.used_percent != null ||
      (billing?.monthly_limit_cents != null && billing.monthly_limit_cents > 0)
    )
      return false
    if (grokPlanLabelIsPaid(plan)) return false
    if (grokPlanLabelIsFree(plan) || grokPlanLabelIsFree(entitlement)) return true
    return billing != null
  })()
  const grokFreeQuotaUsage = usageInfo?.grok_local_usage_24h || null
  const grokFreeTokenBar = (() => {
    if (!grokIsFree || !grokFreeQuotaUsage) return null
    const limit = usageInfo?.grok_free_token_limit
    if (typeof limit !== 'number' || limit <= 0) return null
    const used = Math.max(0, grokFreeQuotaUsage.tokens || 0)
    return { utilization: Math.min(100, (used / limit) * 100), limit }
  })()
  const grokQuotaUnknown = (() => {
    if (account.platform !== 'grok') return false
    if (grokIsFree) {
      return !grokFreeTokenBar
    }
    if (grokWeeklyBillingBar || grokMonthlyBillingBar || grokPrepaidMoneyLine) {
      return false
    }
    return usageInfo?.grok_quota_snapshot_state !== 'observed'
  })()
  const grokQuotaUnknownLabel =
    usageInfo?.grok_quota_snapshot_state === 'no_headers'
      ? t('admin.accounts.usageWindow.grokNoHeaders')
      : t('admin.accounts.usageWindow.grokUnknown')
  const grokRetryAfterLabel = (() => {
    const seconds = usageInfo?.grok_retry_after_seconds
    if (seconds == null || seconds <= 0) return null
    if (seconds < 60) return `${seconds}s`
    const minutes = Math.ceil(seconds / 60)
    return `${minutes}m`
  })()

  // 账户类型显示标签
  const antigravityTierLabel = (() => {
    switch (antigravityTier) {
      case 'free-tier':
        return t('admin.accounts.tier.free')
      case 'g1-pro-tier':
        return t('admin.accounts.tier.pro')
      case 'g1-ultra-tier':
        return t('admin.accounts.tier.ultra')
      default:
        return null
    }
  })()

  // 账户类型徽章样式
  const antigravityTierClass = (() => {
    switch (antigravityTier) {
      case 'free-tier':
        return 'surface-inset surface-muted dark:surface-inset'
      case 'g1-pro-tier':
        return 'bg-blue-100/[0.15] text-blue-600 dark:bg-blue-900/40 dark:text-blue-300'
      case 'g1-ultra-tier':
        return 'bg-purple-100/[0.15] text-purple-600 dark:bg-purple-900/40 dark:text-purple-300'
      default:
        return ''
    }
  })()

  // 检测账户是否有不合格状态（ineligibleTiers）
  const hasIneligibleTiers = (() => {
    const extra = account.extra as Record<string, unknown> | undefined
    if (!extra) return false

    const loadCodeAssist = extra.load_code_assist as Record<string, unknown> | undefined
    if (!loadCodeAssist) return false

    const ineligibleTiers = loadCodeAssist.ineligibleTiers as unknown[] | undefined
    return Array.isArray(ineligibleTiers) && ineligibleTiers.length > 0
  })()

  // Antigravity 403 forbidden 状态
  const isForbidden = !!usageInfo?.is_forbidden
  const forbiddenType = usageInfo?.forbidden_type || 'forbidden'
  const validationURL = usageInfo?.validation_url || ''

  // 需要重新授权（401）
  const needsReauth = !!usageInfo?.needs_reauth

  // 降级错误标签（rate_limited / network_error）
  const usageErrorLabel = usageInfo?.error_code === 'rate_limited' ? t('admin.accounts.rateLimited') : t('admin.accounts.usageError')

  const forbiddenLabel = (() => {
    switch (forbiddenType) {
      case 'validation':
        return t('admin.accounts.forbiddenValidation')
      case 'violation':
        return t('admin.accounts.forbiddenViolation')
      default:
        return t('admin.accounts.forbidden')
    }
  })()

  const forbiddenBadgeClass =
    forbiddenType === 'validation'
      ? 'bg-yellow-100/[0.15] text-yellow-700 dark:bg-yellow-900/40 dark:text-yellow-300'
      : 'bg-red-100/[0.15] text-red-700 dark:bg-red-900/40 dark:text-red-300'

  const copyValidationURL = async () => {
    if (!validationURL) return
    try {
      await navigator.clipboard.writeText(validationURL)
      setLinkCopied(true)
      setTimeout(() => setLinkCopied(false), 2000)
    } catch {
      // fallback: ignore
    }
  }

  const isAnthropicOAuthOrSetupToken =
    account.platform === 'anthropic' && (account.type === 'oauth' || account.type === 'setup-token')

  const requestParentBatchUsage = (options?: { force?: boolean }) => {
    if (!isBatchManaged || !shouldFetchUsage) return
    requestBatchedUsage?.(account, options)
  }

  const syncManagedUsageState = () => {
    if (!isBatchManaged) return
    setUsageInfo(batchedUsage ?? null)
    setError(batchedUsageError ?? null)
    setLoading(batchedUsageLoading === true)
  }

  const loadUsage = useCallback(
    async (options?: { source?: 'passive' | 'active'; bypassCache?: boolean }) => {
      if (!shouldFetchUsage) return
      if (isBatchManaged) {
        requestParentBatchUsage({ force: options?.bypassCache === true })
        return
      }

      // Check cache
      if (!options?.bypassCache) {
        const cached = _usageCache.get(account.id)
        if (cached && Date.now() - cached.ts < USAGE_CACHE_TTL) {
          setUsageInfo(cached.data)
          setLoading(false)
          return
        }
      }

      setLoading(true)
      setError(null)

      try {
        const fetchFn = () =>
          options?.source
            ? adminAPI.accounts.getUsage(account.id, options.source, options.bypassCache === true)
            : adminAPI.accounts.getUsage(account.id)
        const result = await enqueueUsageRequest(account, fetchFn)
        if (!unmountedRef.current) {
          setUsageInfo(result)
          _usageCache.set(account.id, { data: result, ts: Date.now() })
        }
      } catch (e) {
        if (!unmountedRef.current) {
          setError(t('common.error'))
          console.error('Failed to load usage:', e)
        }
      } finally {
        if (!unmountedRef.current) setLoading(false)
      }
    },
    // shouldFetchUsage/isBatchManaged are derived from the account row identity.
    [account.id, account.platform, account.type, account.credentials, isBatchManaged, shouldFetchUsage, requestBatchedUsage, t]
  )

  const flushPendingAutoLoad = () => {
    if (!pendingAutoLoad.current) return
    const source = pendingAutoLoadSource.current
    pendingAutoLoad.current = false
    pendingAutoLoadSource.current = undefined
    void loadUsage({ source }).catch((e) => {
      console.error('Failed to load deferred usage:', e)
    })
  }

  const requestAutoLoad = (source?: 'passive' | 'active') => {
    if (!shouldFetchUsage) return
    if (shouldLazyLoadOnMobile && !hasEnteredViewport) {
      pendingAutoLoad.current = true
      pendingAutoLoadSource.current = source
      return
    }
    void loadUsage({ source }).catch((e) => {
      console.error('Failed to auto load usage:', e)
    })
  }

  // Mobile lazy loading: observe visibility so off-screen rows do not fetch.
  useEffect(() => {
    if (!shouldLazyLoadOnMobile || hasEnteredViewport) return
    if (typeof window === 'undefined' || typeof IntersectionObserver === 'undefined') {
      setHasEnteredViewport(true)
      flushPendingAutoLoad()
      return
    }
    if (!rootRef.current) return

    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((entry) => entry.isIntersecting)) return
        setHasEnteredViewport(true)
        observer.disconnect()
        flushPendingAutoLoad()
      },
      { root: null, rootMargin: '200px 0px', threshold: 0.01 }
    )
    observer.observe(rootRef.current)
    return () => observer.disconnect()
  }, [shouldLazyLoadOnMobile, hasEnteredViewport])

  useEffect(() => {
    if (isDesktopViewport) {
      setHasEnteredViewport(true)
      flushPendingAutoLoad()
    } else {
      setHasEnteredViewport(false)
    }
  }, [isDesktopViewport])

  useEffect(() => {
    activeUsageRequestID.current += 1
    setActiveQueryLoading(false)
  }, [account.id, account.platform, account.type])

  const loadActiveUsage = async () => {
    const requestID = ++activeUsageRequestID.current
    const accountID = account.id
    setActiveQueryLoading(true)
    try {
      const result = await adminAPI.accounts.getUsage(accountID, 'active', true)
      if (!unmountedRef.current && requestID === activeUsageRequestID.current) setUsageInfo(result)
    } catch (e) {
      if (!unmountedRef.current && requestID === activeUsageRequestID.current) console.error('Failed to load active usage:', e)
    } finally {
      if (!unmountedRef.current && requestID === activeUsageRequestID.current) setActiveQueryLoading(false)
    }
  }

  // The probe persists upstream quota state; refresh this cell so its compact
  // bars and entitlement status reflect the newly observed snapshot.
  const handleGrokProbed = async () => {
    await loadUsage({ source: 'active', bypassCache: true })
  }

  // ===== API Key quota progress bars =====

  const makeQuotaBar = (used: number, limit: number, startKey?: string): QuotaBarInfo => {
    const utilization = limit > 0 ? (used / limit) * 100 : 0
    let resetsAt: string | null = null
    if (startKey) {
      const extra = account.extra as Record<string, unknown> | undefined
      const isDaily = startKey.includes('daily')
      const mode = isDaily
        ? ((extra?.quota_daily_reset_mode as string) || 'rolling')
        : ((extra?.quota_weekly_reset_mode as string) || 'rolling')

      if (mode === 'fixed') {
        // Use pre-computed next reset time for fixed mode
        const resetAtKey = isDaily ? 'quota_daily_reset_at' : 'quota_weekly_reset_at'
        resetsAt = (extra?.[resetAtKey] as string) || null
      } else {
        // Rolling mode: compute from start + period
        const startStr = extra?.[startKey] as string | undefined
        if (startStr) {
          const startDate = new Date(startStr)
          const periodMs = isDaily ? 24 * 60 * 60 * 1000 : 7 * 24 * 60 * 60 * 1000
          resetsAt = new Date(startDate.getTime() + periodMs).toISOString()
        }
      }
    }
    return { utilization, resetsAt }
  }

  const hasApiKeyQuota = (() => {
    if (!supportsAccountQuota(account.type)) return false
    return (
      (account.quota_daily_limit ?? 0) > 0 || (account.quota_weekly_limit ?? 0) > 0 || (account.quota_limit ?? 0) > 0
    )
  })()

  const quotaDailyBar: QuotaBarInfo | null = (() => {
    const limit = account.quota_daily_limit ?? 0
    if (limit <= 0) return null
    return makeQuotaBar(account.quota_daily_used ?? 0, limit, 'quota_daily_start')
  })()

  const quotaWeeklyBar: QuotaBarInfo | null = (() => {
    const limit = account.quota_weekly_limit ?? 0
    if (limit <= 0) return null
    return makeQuotaBar(account.quota_weekly_used ?? 0, limit, 'quota_weekly_start')
  })()

  const quotaTotalBar: QuotaBarInfo | null = (() => {
    const limit = account.quota_limit ?? 0
    if (limit <= 0) return null
    return makeQuotaBar(account.quota_used ?? 0, limit)
  })()

  const handleQuotaResetAccountUpdated = (next: Account) => {
    onAccountUpdated(next)
  }

  const handleOllamaCloudUsageUpdated = (state: NonNullable<Account['ollama_cloud_usage']>) => {
    onAccountUpdated({ ...account, ollama_cloud_usage: state })
  }

  // ===== Key account today stats formatters =====

  const formatKeyRequests = todayStats ? formatCompactNumber(todayStats.requests, { allowBillions: false }) : ''
  const formatKeyTokens = todayStats ? formatCompactNumber(todayStats.tokens) : ''
  const formatKeyCost = todayStats ? todayStats.cost.toFixed(2) : '0.00'
  const formatKeyUserCost = todayStats && todayStats.user_cost != null ? todayStats.user_cost.toFixed(2) : '0.00'

  // Batch-managed state sync (equivalent of the Vue deep watcher).
  useEffect(() => {
    syncManagedUsageState()
    // Batched props are the parent's source of truth for managed rows.
  }, [isBatchManaged, batchedUsage, batchedUsageError, batchedUsageLoading])

  useEffect(() => {
    if (isBatchManaged) {
      syncManagedUsageState()
      requestParentBatchUsage()
    }
    // Row identity change re-arms the batch request.
  }, [account.id, account.platform, account.type, isBatchManaged])

  // Auto load on mount (desktop, or mobile once visible).
  useEffect(() => {
    if (isBatchManaged) {
      syncManagedUsageState()
      requestParentBatchUsage()
      return
    }
    if (!shouldAutoLoadUsageOnMount) return
    const source = isAnthropicOAuthOrSetupToken ? 'passive' : undefined
    requestAutoLoad(source)
    // Mount-only auto load; subsequent changes go through the watchers below.
  }, [])

  const prevOpenAIRefreshKey = useRef(openAIUsageRefreshKey)
  useEffect(() => {
    const prevKey = prevOpenAIRefreshKey.current
    prevOpenAIRefreshKey.current = openAIUsageRefreshKey
    if (!prevKey || openAIUsageRefreshKey === prevKey) return
    if (account.platform !== 'openai' || account.type !== 'oauth') return

    if (isBatchManaged) {
      requestParentBatchUsage({ force: true })
      return
    }

    _usageCache.delete(account.id)
    requestAutoLoad()
  }, [openAIUsageRefreshKey, account.platform, account.type, account.id, isBatchManaged])

  const prevManualRefreshToken = useRef(manualRefreshToken)
  useEffect(() => {
    const prevToken = prevManualRefreshToken.current
    prevManualRefreshToken.current = manualRefreshToken
    if (manualRefreshToken === prevToken) return
    if (!shouldFetchUsage) return

    if (isBatchManaged) {
      requestParentBatchUsage({ force: true })
      return
    }

    const source = isAnthropicOAuthOrSetupToken ? 'passive' : undefined
    _usageCache.delete(account.id)
    void loadUsage({ source, bypassCache: true }).catch((e) => {
      console.error('Failed to refresh usage after manual refresh:', e)
    })
  }, [manualRefreshToken, shouldFetchUsage, isBatchManaged, isAnthropicOAuthOrSetupToken, account.id, loadUsage])

  const todayStatsRow = (showToday: boolean) => {
    if (!showToday) return null
    if (todayStats) {
      return (
        <div className="mb-0.5 flex items-center">
          <div className="flex items-center gap-1.5 text-[9px] surface-muted">
            <span className="rounded surface-inset px-1.5 py-0.5">{formatKeyRequests} req</span>
            <span className="rounded surface-inset px-1.5 py-0.5">{formatKeyTokens}</span>
            <span className="rounded surface-inset px-1.5 py-0.5" title={t('usage.accountBilled')}>
              A ${formatKeyCost}
            </span>
            {todayStats.user_cost != null && (
              <span className="rounded surface-inset px-1.5 py-0.5" title={t('usage.userBilled')}>
                U ${formatKeyUserCost}
              </span>
            )}
          </div>
        </div>
      )
    }
    if (todayStatsLoading) {
      return (
        <div className="mb-0.5 flex items-center gap-1">
          <div className="h-3 w-10 animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
          <div className="h-3 w-8 animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
          <div className="h-3 w-12 animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
        </div>
      )
    }
    return null
  }

  const activeQueryButton = (
    <button
      type="button"
      className="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium text-blue-600 hover:bg-blue-50/25 dark:text-blue-400 dark:hover:bg-blue-900/30 transition-colors disabled:cursor-not-allowed disabled:opacity-50 surface-control"
      disabled={activeQueryLoading}
      onClick={() => void loadActiveUsage()}
    >
      <svg className={cn('h-2.5 w-2.5', activeQueryLoading && 'animate-spin')} fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
      </svg>
      {t('admin.accounts.usageWindow.activeQuery')}
    </button>
  )

  const renderUsageWindows = () => (
    <>
      {/* Anthropic OAuth and Setup Token accounts: fetch real usage data */}
      {account.platform === 'anthropic' && (account.type === 'oauth' || account.type === 'setup-token') && (
        <>
          {/* Loading state */}
          {loading ? (
            <div className="space-y-1.5">
              {/* OAuth: 3 rows, Setup Token: 1 row */}
              <div className="flex items-center gap-1">
                <div className="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
                <div className="h-1.5 w-8 animate-pulse rounded-full bg-gray-200 dark:bg-gray-700" />
                <div className="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
              </div>
              {account.type === 'oauth' && (
                <>
                  <div className="flex items-center gap-1">
                    <div className="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
                    <div className="h-1.5 w-8 animate-pulse rounded-full bg-gray-200 dark:bg-gray-700" />
                    <div className="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
                  </div>
                  <div className="flex items-center gap-1">
                    <div className="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
                    <div className="h-1.5 w-8 animate-pulse rounded-full bg-gray-200 dark:bg-gray-700" />
                    <div className="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
                  </div>
                </>
              )}
            </div>
          ) : error ? (
            /* Error state */
            <div className="text-xs text-red-500">{error}</div>
          ) : usageInfo ? (
            /* Usage data */
            <div className="space-y-1">
              {/* API error (degraded response) */}
              {usageInfo.error && (
                <div
                  className="text-xs text-amber-600 dark:text-amber-400 truncate max-w-[200px]"
                  title={usageInfo.error}
                >
                  {usageInfo.error}
                </div>
              )}
              {/* 5h Window */}
              {usageInfo.five_hour && (
                <UsageProgressBar
                  label="5h"
                  utilization={usageInfo.five_hour.utilization}
                  resetsAt={usageInfo.five_hour.resets_at}
                  windowStats={usageInfo.five_hour.window_stats}
                  color="indigo"
                />
              )}

              {/* 7d Window (OAuth only) */}
              {usageInfo.seven_day && (
                <UsageProgressBar
                  label="7d"
                  utilization={usageInfo.seven_day.utilization}
                  resetsAt={usageInfo.seven_day.resets_at}
                  color="emerald"
                />
              )}

              {/* 7d Sonnet Window (OAuth only) */}
              {usageInfo.seven_day_sonnet && (
                <UsageProgressBar
                  label="7d S"
                  utilization={usageInfo.seven_day_sonnet.utilization}
                  resetsAt={usageInfo.seven_day_sonnet.resets_at}
                  color="purple"
                />
              )}

              {/* 7d Fable Window (7d_oi) */}
              {usageInfo.seven_day_fable && (
                <UsageProgressBar
                  label="7d F"
                  utilization={usageInfo.seven_day_fable.utilization}
                  resetsAt={usageInfo.seven_day_fable.resets_at}
                  color="amber"
                />
              )}
            </div>
          ) : (
            /* No data yet */
            <div className="space-y-1">
              <div className="text-xs surface-muted">-</div>
            </div>
          )}
          {/*
            One stable instance for every usage state, so a reset-credit query started
            while usage is still loading survives the usage response. The local query
            button shares its row once usage data exists.
          */}
          <ClaudeResetCreditsCell
            account={account}
            className="mt-1"
            onRedeemed={() => void loadActiveUsage()}
            preActions={
              usageInfo ? (
                <>
                  {usageInfo.source === 'passive' && (
                    <span className="text-[9px] italic surface-muted">{t('admin.accounts.usageWindow.passiveSampled')}</span>
                  )}
                  {activeQueryButton}
                </>
              ) : null
            }
          />
        </>
      )}

      {/* OpenAI OAuth accounts: single source from /usage API */}
      {account.platform === 'openai' && account.type === 'oauth' && (
        <>
          {hasOpenAIUsageFallback ? (
            <div className="space-y-1">
              {usageInfo?.five_hour && (
                <UsageProgressBar
                  label="5h"
                  utilization={usageInfo.five_hour.utilization}
                  resetsAt={usageInfo.five_hour.resets_at}
                  windowStats={usageInfo.five_hour.window_stats}
                  showNowWhenIdle
                  color="indigo"
                />
              )}
              {usageInfo?.seven_day && (
                <UsageProgressBar
                  label="7d"
                  utilization={usageInfo.seven_day.utilization}
                  resetsAt={usageInfo.seven_day.resets_at}
                  windowStats={usageInfo.seven_day.window_stats}
                  estimatedTotalCost={openAISevenDayEstimatedTotalCost}
                  showNowWhenIdle
                  color="emerald"
                />
              )}
              {/*
                Upstream codex /wham/usage quota query + reset. The local active-sampling
                refresh button is rendered via the pre-actions slot so the user sees a
                single row of related buttons instead of two stacked rows.
              */}
              <OpenAIQuotaResetCell account={account} onAccountUpdated={handleQuotaResetAccountUpdated} preActions={activeQueryButton} />
            </div>
          ) : loading ? (
            <div className="space-y-1.5">
              <div className="flex items-center gap-1">
                <div className="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
                <div className="h-1.5 w-8 animate-pulse rounded-full bg-gray-200 dark:bg-gray-700" />
                <div className="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
              </div>
              <div className="flex items-center gap-1">
                <div className="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
                <div className="h-1.5 w-8 animate-pulse rounded-full bg-gray-200 dark:bg-gray-700" />
                <div className="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
              </div>
            </div>
          ) : (
            <div>
              <div className="text-xs surface-muted">-</div>
              {/* Always allow on-demand upstream quota query, even before local data exists. */}
              <OpenAIQuotaResetCell account={account} className="mt-1" onAccountUpdated={handleQuotaResetAccountUpdated} />
            </div>
          )}
        </>
      )}

      {/* Antigravity OAuth accounts: fetch usage from API */}
      {account.platform === 'antigravity' && account.type === 'oauth' && (
        <>
          {/* 账户类型徽章 */}
          {antigravityTierLabel && (
            <div className="mb-1 flex items-center gap-1">
              <span className={cn('inline-block rounded px-1.5 py-0.5 text-[10px] font-medium', antigravityTierClass)}>
                {antigravityTierLabel}
              </span>
              {/* 不合格账户警告图标 */}
              {hasIneligibleTiers && (
                <TriggerTooltip content={t('admin.accounts.ineligibleWarning')} widthClass="w-80">
                  <svg className="h-3.5 w-3.5 text-red-500" fill="currentColor" viewBox="0 0 20 20">
                    <path
                      fillRule="evenodd"
                      d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7 4a1 1 0 11-2 0 1 1 0 012 0zm-1-9a1 1 0 00-1 1v4a1 1 0 102 0V6a1 1 0 00-1-1z"
                      clipRule="evenodd"
                    />
                  </svg>
                </TriggerTooltip>
              )}
            </div>
          )}

          {/* Forbidden state (403) */}
          {isForbidden ? (
            <div className="space-y-1">
              <span className={cn('inline-block rounded px-1.5 py-0.5 text-[10px] font-medium', forbiddenBadgeClass)}>
                {forbiddenLabel}
              </span>
              {validationURL && (
                <div className="flex items-center gap-1">
                  <a
                    href={validationURL}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-[10px] text-blue-600 hover:text-blue-800 hover:underline dark:text-blue-400 dark:hover:text-blue-300"
                    title={t('admin.accounts.openVerification')}
                  >
                    {t('admin.accounts.openVerification')}
                  </a>
                  <button
                    type="button"
                    className="text-[10px] hover:text-gray-700 dark:hover:text-gray-200 surface-control surface-muted"
                    title={t('admin.accounts.copyLink')}
                    onClick={() => void copyValidationURL()}
                  >
                    {linkCopied ? t('admin.accounts.linkCopied') : t('admin.accounts.copyLink')}
                  </button>
                </div>
              )}
            </div>
          ) : needsReauth ? (
            /* Needs reauth (401) */
            <div className="space-y-1">
              <span className="inline-block rounded px-1.5 py-0.5 text-[10px] font-medium bg-orange-100/[0.15] text-orange-700 dark:bg-orange-900/40 dark:text-orange-300">
                {t('admin.accounts.needsReauth')}
              </span>
            </div>
          ) : usageInfo?.error ? (
            /* Degraded error (non-403, non-401) */
            <div className="space-y-1">
              <span className="inline-block rounded px-1.5 py-0.5 text-[10px] font-medium bg-amber-100/[0.15] text-amber-700 dark:bg-amber-900/40 dark:text-amber-300">
                {usageErrorLabel}
              </span>
            </div>
          ) : loading ? (
            /* Loading state */
            <div className="space-y-1.5">
              <div className="flex items-center gap-1">
                <div className="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
                <div className="h-1.5 w-8 animate-pulse rounded-full bg-gray-200 dark:bg-gray-700" />
                <div className="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
              </div>
            </div>
          ) : error ? (
            /* Error state */
            <div className="text-xs text-red-500">{error}</div>
          ) : antigravityQuotaGroupBars.length > 0 ? (
            /* Real 5-hour and weekly windows per quota group (retrieveUserQuotaSummary) */
            <div className="space-y-1">
              {antigravityQuotaGroupBars.map((bar) => (
                <UsageProgressBar
                  key={bar.key}
                  label={bar.label}
                  title={bar.title}
                  utilization={bar.utilization}
                  resetsAt={bar.resetsAt}
                  color={bar.color}
                />
              ))}

              {aiCreditsDisplay && (
                <div className="mt-1 text-[10px] text-gray-500 dark:text-gray-400">
                  💳 {t('admin.accounts.aiCreditsBalance')}: {aiCreditsDisplay}
                </div>
              )}
            </div>
          ) : hasAntigravityQuotaFromAPI ? (
            /* Usage data from API */
            <div className="space-y-1">
              {/* Gemini 3 Pro */}
              {antigravity3ProUsageFromAPI !== null && (
                <UsageProgressBar
                  label={t('admin.accounts.usageWindow.gemini3Pro')}
                  utilization={antigravity3ProUsageFromAPI.utilization}
                  resetsAt={antigravity3ProUsageFromAPI.resetTime}
                  color="indigo"
                />
              )}

              {/* Gemini 3 Flash */}
              {antigravity3FlashUsageFromAPI !== null && (
                <UsageProgressBar
                  label={t('admin.accounts.usageWindow.gemini3Flash')}
                  utilization={antigravity3FlashUsageFromAPI.utilization}
                  resetsAt={antigravity3FlashUsageFromAPI.resetTime}
                  color="emerald"
                />
              )}

              {/* Gemini 3 Image */}
              {antigravity3ImageUsageFromAPI !== null && (
                <UsageProgressBar
                  label={t('admin.accounts.usageWindow.gemini3Image')}
                  utilization={antigravity3ImageUsageFromAPI.utilization}
                  resetsAt={antigravity3ImageUsageFromAPI.resetTime}
                  color="purple"
                />
              )}

              {/* Claude */}
              {antigravityClaudeUsageFromAPI !== null && (
                <UsageProgressBar
                  label={t('admin.accounts.usageWindow.claude')}
                  utilization={antigravityClaudeUsageFromAPI.utilization}
                  resetsAt={antigravityClaudeUsageFromAPI.resetTime}
                  color="amber"
                />
              )}

              {aiCreditsDisplay && (
                <div className="mt-1 text-[10px] surface-muted">
                  💳 {t('admin.accounts.aiCreditsBalance')}: {aiCreditsDisplay}
                </div>
              )}
            </div>
          ) : aiCreditsDisplay ? (
            <div className="text-[10px] surface-muted">
              💳 {t('admin.accounts.aiCreditsBalance')}: {aiCreditsDisplay}
            </div>
          ) : (
            <div className="text-xs surface-muted">-</div>
          )}
        </>
      )}

      {/* Grok OAuth accounts: passive xAI quota headers + local Sub2API usage */}
      {account.platform === 'grok' && account.type === 'oauth' && (
        <>
          {loading ? (
            <div className="space-y-1.5">
              <div className="flex items-center gap-1">
                <div className="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
                <div className="h-1.5 w-8 animate-pulse rounded-full bg-gray-200 dark:bg-gray-700" />
                <div className="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
              </div>
            </div>
          ) : error ? (
            <div className="text-xs text-red-500">{error}</div>
          ) : needsReauth ? (
            <div className="space-y-1">
              <span className="inline-block rounded px-1.5 py-0.5 text-[10px] font-medium bg-orange-100/[0.15] text-orange-700 dark:bg-orange-900/40 dark:text-orange-300">
                {t('admin.accounts.needsReauth')}
              </span>
            </div>
          ) : isForbidden ? (
            <div className="space-y-1">
              <span className="inline-block rounded px-1.5 py-0.5 text-[10px] font-medium bg-red-100/[0.15] text-red-700 dark:bg-red-900/40 dark:text-red-300">
                {usageInfo?.grok_entitlement_status || t('admin.accounts.forbidden')}
              </span>
            </div>
          ) : usageInfo ? (
            <div className="space-y-1">
              {/* Free: only rolling 24h soft-gate bar. Paid: 7d + 30d + prepaid money. */}
              {grokIsFree ? (
                <>
                  {grokFreeTokenBar ? (
                    <UsageProgressBar
                      label="24h"
                      title={t('admin.accounts.usageWindow.grokFreeQuota24hHint', {
                        limit: formatCompactNumber(grokFreeTokenBar.limit)
                      })}
                      utilization={grokFreeTokenBar.utilization}
                      windowStats={grokFreeQuotaUsage}
                      showNowWhenIdle
                      color="emerald"
                    />
                  ) : grokQuotaUnknown ? (
                    <div className="text-[10px] surface-muted">{grokQuotaUnknownLabel}</div>
                  ) : null}
                </>
              ) : (
                <>
                  {grokWeeklyBillingBar && (
                    <UsageProgressBar
                      label="7d"
                      utilization={grokWeeklyBillingBar.utilization}
                      resetsAt={grokWeeklyBillingBar.resetsAt}
                      windowStats={grokWeeklyBillingBar.windowStats}
                      showNowWhenIdle
                      color="indigo"
                    />
                  )}
                  {grokMonthlyBillingBar && (
                    <UsageProgressBar
                      label="30d"
                      utilization={grokMonthlyBillingBar.utilization}
                      resetsAt={grokMonthlyBillingBar.resetsAt}
                      windowStats={grokMonthlyBillingBar.windowStats}
                      showNowWhenIdle
                      color="indigo"
                    />
                  )}
                  {grokPrepaidMoneyLine && (
                    <div className="flex flex-wrap items-center gap-1 text-[10px] surface-muted">
                      {grokPrepaidMoneyLine.showPrepaid && (
                        <span
                          className="rounded bg-emerald-50/[0.15] px-1 py-0.5 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300"
                          title={t('admin.accounts.usageWindow.grokPrepaid')}
                        >
                          {t('admin.accounts.usageWindow.grokPrepaid')} ${grokPrepaidMoneyLine.prepaid}
                        </span>
                      )}
                      {grokPrepaidMoneyLine.showUsedLimit && (
                        <span title={t('admin.accounts.usageWindow.grokMonthlyLimit')}>
                          {t('admin.accounts.usageWindow.grokUsed')}
                          {grokPrepaidMoneyLine.used}/{grokPrepaidMoneyLine.limit}
                        </span>
                      )}
                    </div>
                  )}
                  {grokQuotaUnknown && <div className="text-[10px] surface-muted">{grokQuotaUnknownLabel}</div>}
                </>
              )}
              {usageInfo.error && (
                <div className="truncate text-xs text-amber-600 dark:text-amber-400 max-w-[200px]" title={usageInfo.error}>
                  {usageErrorLabel}
                </div>
              )}
              {grokRetryAfterLabel && (
                <div className="text-[10px] text-amber-600 dark:text-amber-400">
                  {t('admin.accounts.usageWindow.grokRetryAfter', { time: grokRetryAfterLabel })}
                </div>
              )}
              <GrokQuotaProbeCell account={account} compact onProbed={() => void handleGrokProbed()} />
            </div>
          ) : (
            <div className="space-y-1">
              <div className="text-xs surface-muted">-</div>
              <GrokQuotaProbeCell account={account} compact onProbed={() => void handleGrokProbed()} />
            </div>
          )}
        </>
      )}

      {/* Multi-protocol API-key providers (CN vendors, OpenCode, Command Code):
          coding-plan / subscription quota windows or balance */}
      {isMultiProtocolApiKeyPlatform(account.platform) && (
        <>
          {/* 挂在 CN 平台下的 Ollama Cloud 账号（资格由后端下发 eligible）：用量由
              Ollama 用量窗口负责。这类账号不是国产厂商订阅，CN 的额度/余额探测端点由
              base_url 衍生，对 ollama.com 会被后端出站 URL 白名单拒绝，渲染出来只会
              给用户一行探测报错，因此不再渲染 CN 子单元格与占位符。 */}
          {account.ollama_cloud_usage?.eligible ? (
            <OllamaCloudUsageCell account={account} onUpdated={handleOllamaCloudUsageUpdated} />
          ) : (
            <div className="space-y-1">
              {/* 子单元格各自按 模式×平台 判定可见；两者都不可见时（智谱 payg 无公开
                  余额端点、coding 探测也不适用）才回落到占位符。 */}
              {!cnQuotaCellVisible && !cnBalanceCellVisible && !zhipuStartPlanVisible && (
                <div className="text-xs surface-muted" title={t('admin.accounts.cnProviders.noBalanceEndpoint')}>
                  -
                </div>
              )}
              <CNProviderQuotaCell account={account} />
              <CNProviderBalanceCell account={account} />
              <ZhipuStartPlanCell account={account} />
            </div>
          )}
        </>
      )}

      {/* Gemini platform: show quota + local usage window */}
      {account.platform === 'gemini' && (
        <>
          {/* Auth Type + Tier Badge (first line) */}
          {geminiAuthTypeLabel && (
            <div className="mb-1 flex items-center gap-1">
              <span className={cn('inline-block rounded px-1.5 py-0.5 text-[10px] font-medium', geminiTierClass)}>
                {geminiAuthTypeLabel}
              </span>
              {/* Help icon */}
              {showGeminiQuotaPolicy && (
                <TriggerTooltip
                  widthClass="w-80"
                  content={
                    <>
                      <div className="font-semibold mb-1">{t('admin.accounts.gemini.quotaPolicy.title')}</div>
                      <div className="mb-2 surface-muted">{t('admin.accounts.gemini.quotaPolicy.note')}</div>
                      <div className="space-y-1">
                        <div>
                          <strong>{geminiQuotaPolicyChannel}:</strong>
                        </div>
                        <div className="pl-2">• {geminiQuotaPolicyLimits}</div>
                        <div className="mt-2">
                          <a
                            href={geminiQuotaPolicyDocsUrl}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-blue-400 hover:text-blue-300 underline"
                          >
                            {t('admin.accounts.gemini.quotaPolicy.columns.docs')} →
                          </a>
                        </div>
                      </div>
                    </>
                  }
                >
                  <svg className="h-3.5 w-3.5 text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300" fill="currentColor" viewBox="0 0 20 20">
                    <path
                      fillRule="evenodd"
                      d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-8-3a1 1 0 00-.867.5 1 1 0 11-1.731-1A3 3 0 0113 8a3.001 3.001 0 01-2 2.83V11a1 1 0 11-2 0v-1a1 1 0 011-1 1 1 0 100-2zm0 8a1 1 0 100-2 1 1 0 000 2z"
                      clipRule="evenodd"
                    />
                  </svg>
                </TriggerTooltip>
              )}
            </div>
          )}

          {/* Usage data or unlimited flow */}
          <div className="space-y-1">
            {showGeminiTodayStats && todayStats && (
              <div className="mb-0.5 flex items-center">{todayStatsRow(true)}</div>
            )}
            {showGeminiTodayStats && !todayStats && todayStatsLoading && (
              <div className="mb-0.5 flex items-center gap-1">
                <div className="h-3 w-10 animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
                <div className="h-3 w-8 animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
                <div className="h-3 w-12 animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
              </div>
            )}
            {/* Vertex 设置了配额上限：显示配额进度条 */}
            {isGeminiVertex && (
              <>
                {quotaDailyBar && (
                  <UsageProgressBar label="1d" utilization={quotaDailyBar.utilization} resetsAt={quotaDailyBar.resetsAt} color="indigo" />
                )}
                {quotaWeeklyBar && (
                  <UsageProgressBar label="7d" utilization={quotaWeeklyBar.utilization} resetsAt={quotaWeeklyBar.resetsAt} color="emerald" />
                )}
                {quotaTotalBar && <UsageProgressBar label="total" utilization={quotaTotalBar.utilization} color="purple" />}
              </>
            )}
            {loading ? (
              <div className="space-y-1">
                <div className="flex items-center gap-1">
                  <div className="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
                  <div className="h-1.5 w-8 animate-pulse rounded-full bg-gray-200 dark:bg-gray-700" />
                  <div className="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
                </div>
              </div>
            ) : error ? (
              <div className="text-xs text-red-500">{error}</div>
            ) : geminiUsageAvailable ? (
              /* Gemini: show daily usage bars when available */
              <div className="space-y-1">
                {geminiUsageBars.map((bar) => (
                  <UsageProgressBar
                    key={bar.key}
                    label={bar.label}
                    utilization={bar.utilization}
                    resetsAt={bar.resetsAt}
                    windowStats={bar.windowStats}
                    color={bar.color}
                  />
                ))}
                <p className="mt-1 text-[9px] leading-tight italic surface-muted">
                  * {t('admin.accounts.gemini.quotaPolicy.simulatedNote') || 'Simulated quota'}
                </p>
              </div>
            ) : !showGeminiTodayStats ? (
              /* AI Studio Client OAuth: show unlimited flow (no usage tracking) */
              <div className="text-xs surface-muted">{t('admin.accounts.gemini.rateLimit.unlimited')}</div>
            ) : null}
          </div>
        </>
      )}

      {/* Other accounts: no usage window */}
      {account.platform !== 'anthropic' &&
        account.platform !== 'openai' &&
        account.platform !== 'antigravity' &&
        account.platform !== 'grok' &&
        account.platform !== 'gemini' &&
        !isMultiProtocolApiKeyPlatform(account.platform) && <div className="text-xs surface-muted">-</div>}
    </>
  )

  return (
    <div ref={rootRef}>
      {showUsageWindows ? (
        renderUsageWindows()
      ) : (
        <div>
          {/* Gemini API Key accounts: show quota info */}
          {account.platform === 'gemini' && !isGeminiThirdPartyApiKey ? (
            <AccountQuotaInfo account={account} />
          ) : (
            /* Key/Bedrock accounts: show today stats + optional quota bars */
            <div className="space-y-1">
              {account.ollama_cloud_usage?.eligible && (
                <OllamaCloudUsageCell account={account} onUpdated={handleOllamaCloudUsageUpdated} />
              )}
              {/* Today stats row (requests, tokens, cost, user_cost) */}
              {todayStatsRow(false)}

              {/* API Key accounts with quota limits: show progress bars */}
              {quotaDailyBar && (
                <UsageProgressBar label="1d" utilization={quotaDailyBar.utilization} resetsAt={quotaDailyBar.resetsAt} color="indigo" />
              )}
              {quotaWeeklyBar && (
                <UsageProgressBar label="7d" utilization={quotaWeeklyBar.utilization} resetsAt={quotaWeeklyBar.resetsAt} color="emerald" />
              )}
              {quotaTotalBar && <UsageProgressBar label="total" utilization={quotaTotalBar.utilization} color="purple" />}

              {/* No data at all */}
              {!todayStats && !todayStatsLoading && !hasApiKeyQuota && !account.ollama_cloud_usage?.eligible && (
                <div className="text-xs surface-muted">-</div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}


export default AccountUsageCell
