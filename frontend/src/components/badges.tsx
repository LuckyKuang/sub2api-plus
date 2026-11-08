import { useTranslation } from 'react-i18next'
import { useAppStore } from '@/stores/app'
import { cn } from '@/lib/utils'
import type { AccountPlatform, AccountType, GroupPlatform, SubscriptionType } from '@/types'
import { platformLabel as sharedPlatformLabel } from '@/utils/platformColors'
import { normalizePlanType, openAIPlanTypeLabel } from '@/utils/planType'
import { formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import { GrokFreeIcon, PlatformIcon } from './icons'
import { Icon } from './Icon'

// ==========================================================================
// StatusBadge
// ==========================================================================

export interface StatusBadgeProps {
  status: string
  label: string
}

export function StatusBadge({ status, label }: StatusBadgeProps) {
  const variantClass = (() => {
    switch (status) {
      case 'active':
      case 'success':
        return 'bg-green-500'
      case 'disabled':
      case 'inactive':
      case 'warning':
        return 'bg-yellow-500'
      case 'error':
      case 'danger':
        return 'bg-red-500'
      default:
        return 'bg-gray-400'
    }
  })()

  return (
    <div className="flex items-center gap-1.5">
      <span className={cn('console-data-mark inline-block h-2 w-2 rounded-full', variantClass)}></span>
      <span className="text-sm surface-muted">{label}</span>
    </div>
  )
}

// ==========================================================================
// GroupCapacityBadge
// ==========================================================================

function capacityClass(used: number, max: number): string {
  if (max > 0 && used >= max) {
    return 'bg-red-100/[0.15] text-red-700 dark:bg-red-900/30 dark:text-red-400'
  }
  if (used > 0) {
    return 'bg-yellow-100/[0.15] text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
  }
  return 'surface-inset surface-muted dark:surface-inset'
}

export interface GroupCapacityBadgeProps {
  concurrencyUsed?: number
  concurrencyMax?: number
  sessionsUsed?: number
  sessionsMax?: number
  rpmUsed?: number
  rpmMax?: number
}

export function GroupCapacityBadge({
  concurrencyUsed = 0,
  concurrencyMax = 0,
  sessionsUsed = 0,
  sessionsMax = 0,
  rpmUsed = 0,
  rpmMax = 0
}: GroupCapacityBadgeProps) {
  return (
    <div className="flex flex-col gap-1">
      {/* 并发槽位 */}
      <div className="flex items-center gap-1">
        <span
          className={cn(
            'inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[10px] font-medium',
            capacityClass(concurrencyUsed, concurrencyMax)
          )}
        >
          <svg className="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              d="M3.75 6A2.25 2.25 0 016 3.75h2.25A2.25 2.25 0 0110.5 6v2.25a2.25 2.25 0 01-2.25 2.25H6a2.25 2.25 0 01-2.25-2.25V6zM3.75 15.75A2.25 2.25 0 016 13.5h2.25a2.25 2.25 0 012.25 2.25V18a2.25 2.25 0 01-2.25 2.25H6A2.25 2.25 0 013.75 18v-2.25zM13.5 6a2.25 2.25 0 012.25-2.25H18A2.25 2.25 0 0120.25 6v2.25a2.25 2.25 0 01-2.25 2.25h-2.25A2.25 2.25 0 0113.5 8.25V6zM13.5 15.75a2.25 2.25 0 012.25-2.25H18a2.25 2.25 0 012.25 2.25V18A2.25 2.25 0 0118 20.25h-2.25A2.25 2.25 0 0113.5 18v-2.25z"
            />
          </svg>
          <span className="font-mono">{concurrencyUsed}</span>
          <span className="surface-muted">/</span>
          <span className="font-mono">{concurrencyMax}</span>
        </span>
      </div>

      {/* 会话数 */}
      {sessionsMax > 0 && (
        <div className="flex items-center gap-1">
          <span
            className={cn(
              'inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[10px] font-medium',
              capacityClass(sessionsUsed, sessionsMax)
            )}
          >
            <svg className="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M15 19.128a9.38 9.38 0 002.625.372 9.337 9.337 0 004.121-.952 4.125 4.125 0 00-7.533-2.493M15 19.128v-.003c0-1.113-.285-2.16-.786-3.07M15 19.128v.106A12.318 12.318 0 018.624 21c-2.331 0-4.512-.645-6.374-1.766l-.001-.109a6.375 6.375 0 0111.964-3.07M12 6.375a3.375 3.375 0 11-6.75 0 3.375 3.375 0 016.75 0zm8.25 2.25a2.625 2.625 0 11-5.25 0 2.625 2.625 0 015.25 0z"
              />
            </svg>
            <span className="font-mono">{sessionsUsed}</span>
            <span className="surface-muted">/</span>
            <span className="font-mono">{sessionsMax}</span>
          </span>
        </div>
      )}

      {/* RPM */}
      {rpmMax > 0 && (
        <div className="flex items-center gap-1">
          <span
            className={cn(
              'inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[10px] font-medium',
              capacityClass(rpmUsed, rpmMax)
            )}
          >
            <svg className="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" strokeWidth="1.5" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" />
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

// ==========================================================================
// GroupBadge
// ==========================================================================

export interface GroupBadgeProps {
  name: string
  platform?: GroupPlatform
  subscriptionType?: SubscriptionType
  rateMultiplier?: number
  userRateMultiplier?: number | null // 用户专属倍率
  peakRateEnabled?: boolean
  peakStart?: string
  peakEnd?: string
  peakRateMultiplier?: number
  showRate?: boolean
  /** Allow full group names to wrap in layouts that have room for multiple lines. */
  truncate?: boolean
  daysRemaining?: number | null // 剩余天数（订阅类型时使用）
  /**
   * 订阅分组默认在右侧 label 展示"订阅"或剩余天数；
   * 开启后订阅分组也改为显示倍率（保留订阅主题色 label，配合可用渠道这类
   * 只关心费率、不关心有效期的场景）。
   */
  alwaysShowRate?: boolean
  className?: string
}

export function GroupBadge({
  name,
  platform,
  subscriptionType = 'standard',
  rateMultiplier,
  userRateMultiplier = null,
  peakRateEnabled = false,
  peakStart,
  peakEnd,
  peakRateMultiplier,
  showRate = true,
  truncate = true,
  daysRemaining = null,
  alwaysShowRate = false,
  className
}: GroupBadgeProps) {
  const { t } = useTranslation()
  const appStore = useAppStore()

  const isSubscription = subscriptionType === 'subscription'

  // 是否有专属倍率（且与默认倍率不同）
  const hasCustomRate =
    userRateMultiplier !== null &&
    userRateMultiplier !== undefined &&
    rateMultiplier !== undefined &&
    userRateMultiplier !== rateMultiplier

  const hasPeakRate = Boolean(showRate && peakRateEnabled && peakStart && peakEnd)

  const peakRateText = formatPeakRateWindow(
    {
      peak_rate_enabled: peakRateEnabled,
      peak_start: peakStart,
      peak_end: peakEnd,
      peak_rate_multiplier: peakRateMultiplier
    },
    serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset)
  )

  const peakRateTitle = t('common.peakRateTooltip', { window: peakRateText })

  // 是否显示右侧标签
  const showLabel = (() => {
    if (!showRate) return false
    // 订阅类型：显示天数或"订阅"
    if (isSubscription) return true
    // 标准类型：显示倍率（包括专属倍率）
    return rateMultiplier !== undefined || hasCustomRate
  })()

  // Label text
  const labelText = (() => {
    const rateLabel = rateMultiplier !== undefined ? `${rateMultiplier}x` : ''
    if (isSubscription && !alwaysShowRate) {
      // 如果有剩余天数，显示天数
      if (daysRemaining !== null && daysRemaining !== undefined) {
        if (daysRemaining <= 0) {
          return t('admin.users.expired')
        }
        return t('admin.users.daysRemaining', { days: daysRemaining })
      }
      // 否则显示"订阅"
      return t('groups.subscription')
    }
    return rateLabel
  })()

  // Label style based on type and days remaining
  const labelClass = (() => {
    const base = 'shrink-0 whitespace-nowrap px-1.5 py-0.5 rounded text-[10px] font-semibold'

    if (!isSubscription) {
      // Standard: subtle background (不再为专属倍率使用不同的背景色)
      return `${base} bg-black/10 dark:bg-white/10`
    }

    // 订阅类型：根据剩余天数显示不同颜色
    if (daysRemaining !== null && daysRemaining !== undefined) {
      if (daysRemaining <= 0 || daysRemaining <= 3) {
        // 已过期或紧急（<=3天）：红色
        return `${base} bg-red-200/80 text-red-800 dark:bg-red-800/50 dark:text-red-300`
      }
      if (daysRemaining <= 7) {
        // 警告（<=7天）：橙色
        return `${base} bg-amber-200/80 text-amber-800 dark:bg-amber-800/50 dark:text-amber-300`
      }
    }

    // 正常状态或无天数：根据平台显示主题色
    if (platform === 'anthropic') {
      return `${base} bg-orange-200/60 text-orange-800 dark:bg-orange-800/40 dark:text-orange-300`
    }
    if (platform === 'openai') {
      return `${base} bg-emerald-200/60 text-emerald-800 dark:bg-emerald-800/40 dark:text-emerald-300`
    }
    if (platform === 'gemini') {
      return `${base} bg-blue-200/60 text-blue-800 dark:bg-blue-800/40 dark:text-blue-300`
    }
    if (platform === 'antigravity') {
      return `${base} bg-purple-200/60 text-purple-800 dark:bg-purple-800/40 dark:text-purple-300`
    }
    if (platform === 'grok') {
      return `${base} bg-zinc-300/70 text-zinc-800 dark:bg-zinc-700/60 dark:text-zinc-200`
    }
    if (platform === 'kimi') {
      return `${base} bg-pink-200/60 text-pink-800 dark:bg-pink-800/40 dark:text-pink-300`
    }
    if (platform === 'zhipu') {
      return `${base} bg-indigo-200/60 text-indigo-800 dark:bg-indigo-800/40 dark:text-indigo-300`
    }
    if (platform === 'deepseek') {
      return `${base} bg-teal-200/60 text-teal-800 dark:bg-teal-800/40 dark:text-teal-300`
    }
    if (platform === 'stepfun') {
      return `${base} bg-cyan-200/60 text-cyan-800 dark:bg-cyan-800/40 dark:text-cyan-300`
    }
    if (platform === 'minimax') {
      return `${base} bg-rose-200/60 text-rose-800 dark:bg-rose-800/40 dark:text-rose-300`
    }
    if (platform === 'composite') {
      return `${base} bg-cyan-200/70 text-cyan-900 dark:bg-cyan-900/50 dark:text-cyan-300`
    }
    return `${base} bg-violet-200/60 text-violet-800 dark:bg-violet-800/40 dark:text-violet-300`
  })()

  const peakRateClass =
    'px-1.5 py-0.5 rounded text-[10px] font-semibold bg-amber-100/[0.15] text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'

  // Badge color based on platform and subscription type
  const badgeClass = (() => {
    if (platform === 'anthropic') {
      // Claude: orange theme
      return isSubscription
        ? 'bg-orange-100/[0.15] text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
        : 'bg-amber-50/[0.15] text-amber-700 dark:bg-amber-900/20 dark:text-amber-400'
    } else if (platform === 'openai') {
      // OpenAI: green theme
      return isSubscription
        ? 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
        : 'bg-green-50/[0.15] text-green-700 dark:bg-green-900/20 dark:text-green-400'
    }
    if (platform === 'gemini') {
      return isSubscription
        ? 'bg-blue-100/[0.15] text-blue-700 dark:bg-blue-900/30 dark:text-blue-400'
        : 'bg-sky-50/[0.15] text-sky-700 dark:bg-sky-900/20 dark:text-sky-400'
    }
    if (platform === 'antigravity') {
      return isSubscription
        ? 'bg-purple-100/[0.15] text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
        : 'bg-fuchsia-50/[0.15] text-fuchsia-700 dark:bg-fuchsia-900/20 dark:text-fuchsia-400'
    }
    if (platform === 'grok') {
      return isSubscription
        ? 'bg-zinc-200 text-zinc-800 dark:bg-zinc-700 dark:text-zinc-100'
        : 'bg-zinc-100 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-200'
    }
    if (platform === 'kimi') {
      return isSubscription
        ? 'bg-pink-100/[0.15] text-pink-700 dark:bg-pink-900/30 dark:text-pink-400'
        : 'bg-pink-50/[0.15] text-pink-700 dark:bg-pink-900/20 dark:text-pink-400'
    }
    if (platform === 'zhipu') {
      return isSubscription
        ? 'bg-indigo-100/[0.15] text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-400'
        : 'bg-indigo-50/[0.15] text-indigo-700 dark:bg-indigo-900/20 dark:text-indigo-400'
    }
    if (platform === 'deepseek') {
      return isSubscription
        ? 'bg-teal-100/[0.15] text-teal-700 dark:bg-teal-900/30 dark:text-teal-400'
        : 'bg-teal-50/[0.15] text-teal-700 dark:bg-teal-900/20 dark:text-teal-400'
    }
    if (platform === 'stepfun') {
      return isSubscription
        ? 'bg-cyan-100/[0.15] text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-400'
        : 'bg-cyan-50/[0.15] text-cyan-700 dark:bg-cyan-900/20 dark:text-cyan-400'
    }
    if (platform === 'minimax') {
      return isSubscription
        ? 'bg-rose-100/[0.15] text-rose-700 dark:bg-rose-900/30 dark:text-rose-400'
        : 'bg-rose-50/[0.15] text-rose-700 dark:bg-rose-900/20 dark:text-rose-400'
    }
    if (platform === 'composite') {
      return isSubscription
        ? 'bg-cyan-100/[0.15] text-cyan-800 dark:bg-cyan-900/30 dark:text-cyan-300'
        : 'bg-cyan-50/[0.15] text-cyan-800 dark:bg-cyan-900/20 dark:text-cyan-300'
    }
    // Fallback: original colors
    return isSubscription
      ? 'bg-violet-100/[0.15] text-violet-700 dark:bg-violet-900/30 dark:text-violet-400'
      : 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
  })()

  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium transition-colors',
        badgeClass,
        className
      )}
    >
      {/* Platform logo */}
      {platform && <PlatformIcon platform={platform} size="sm" />}
      {/* Group name */}
      <span className={truncate ? 'truncate' : 'min-w-0 whitespace-normal [overflow-wrap:anywhere]'}>
        {name}
      </span>
      {/* Right side label */}
      {showLabel && (
        <span className={labelClass}>
          {hasCustomRate ? (
            <>
              {/* 原倍率删除线 + 专属倍率高亮 */}
              <span className="line-through opacity-50 mr-0.5">{rateMultiplier}x</span>
              <span className="font-bold">{userRateMultiplier}x</span>
            </>
          ) : (
            labelText
          )}
        </span>
      )}
      {hasPeakRate && (
        <span className={peakRateClass} title={peakRateTitle}>
          {peakRateText}
        </span>
      )}
    </span>
  )
}

// ==========================================================================
// PlatformTypeBadge
// ==========================================================================

export interface PlatformTypeBadgeProps {
  platform: AccountPlatform
  type: AccountType
  authMode?: string
  planType?: string
  privacyMode?: string
  subscriptionExpiresAt?: string
}

export function PlatformTypeBadge({
  platform,
  type,
  authMode,
  planType,
  privacyMode,
  subscriptionExpiresAt
}: PlatformTypeBadgeProps) {
  const { t } = useTranslation()

  const platformLabel = sharedPlatformLabel(platform)

  const normalizedAuthMode = (authMode || '').trim().toLowerCase().replace(/[\s_-]+/g, '')

  const typeLabel = (() => {
    if (platform === 'openai' && type === 'oauth') {
      if (normalizedAuthMode === 'agentidentity') return 'Agent Identity'
      if (normalizedAuthMode === 'personalaccesstoken') return 'PAT'
    }
    switch (type) {
      case 'oauth':
        return 'OAuth'
      case 'setup-token':
        return 'Token'
      case 'apikey':
        return 'Key'
      case 'bedrock':
        return 'AWS'
      case 'service_account':
        return 'Agent Platform (Vertex)'
      default:
        return type
    }
  })()

  const normalizedPlanType = normalizePlanType(planType)

  const planLabel = (() => {
    if (!normalizedPlanType) return ''
    // ChatGPT 档位命名（Pro 100 / Pro 200 / Pro 500、Business / Business Premium）只适用于
    // OpenAI：Antigravity 与 Grok 各自的 pro/team 沿用下面的通用标签。
    if (platform === 'openai') {
      const label = openAIPlanTypeLabel(planType)
      if (label) return label
    }
    switch (normalizedPlanType) {
      case 'plus':
        return 'Plus'
      case 'team':
        return 'Team'
      case 'chatgptpro':
      case 'pro':
        return 'Pro'
      case 'free':
      case 'basic':
        return platform === 'grok' ? 'Grok Free' : 'Free'
      case 'supergrok':
        return 'SuperGrok'
      case 'supergroklite':
        return 'SuperGrok Lite'
      case 'supergrokplus':
        return 'SuperGrok Plus'
      case 'supergrokheavy':
        return 'SuperGrok Heavy'
      case 'heavy':
        return 'Heavy'
      case 'xbasic':
        return 'X Basic'
      case 'abnormal':
        return t('admin.accounts.subscriptionAbnormal')
      default:
        return planType
    }
  })()

  const isGrokFreePlan =
    platform === 'grok' &&
    (normalizedPlanType === 'free' ||
      normalizedPlanType === 'basic' ||
      normalizedPlanType === 'xbasic')

  const planIconName: 'bolt' | null = (() => {
    if (platform !== 'grok') return null
    // Paid Grok tiers (SuperGrok / Heavy) share the bolt mark; free uses GrokFreeIcon.
    if (
      normalizedPlanType === 'supergrok' ||
      normalizedPlanType === 'supergrokheavy' ||
      normalizedPlanType === 'heavy' ||
      normalizedPlanType.includes('heavy')
    ) {
      return 'bolt'
    }
    return null
  })()

  const platformClass = (() => {
    if (platform === 'anthropic') {
      return 'bg-orange-100/[0.15] text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
    }
    if (platform === 'openai') {
      return 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
    }
    if (platform === 'antigravity') {
      return 'bg-purple-100/[0.15] text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
    }
    if (platform === 'grok') {
      return 'surface-inset surface-muted'
    }
    if (platform === 'kimi') {
      return 'bg-pink-100/[0.15] text-pink-700 dark:bg-pink-900/30 dark:text-pink-400'
    }
    if (platform === 'zhipu') {
      return 'bg-indigo-100/[0.15] text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-400'
    }
    if (platform === 'deepseek') {
      return 'bg-teal-100/[0.15] text-teal-700 dark:bg-teal-900/30 dark:text-teal-400'
    }
    if (platform === 'stepfun') {
      return 'bg-cyan-100/[0.15] text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-400'
    }
    if (platform === 'minimax') {
      return 'bg-rose-100/[0.15] text-rose-700 dark:bg-rose-900/30 dark:text-rose-400'
    }
    return 'bg-blue-100/[0.15] text-blue-700 dark:bg-blue-900/30 dark:text-blue-400'
  })()

  const typeClass = (() => {
    if (platform === 'anthropic') {
      return 'bg-orange-100/[0.15] text-orange-600 dark:bg-orange-900/30 dark:text-orange-400'
    }
    if (platform === 'openai') {
      return 'bg-emerald-100/[0.15] text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-400'
    }
    if (platform === 'antigravity') {
      return 'bg-purple-100/[0.15] text-purple-600 dark:bg-purple-900/30 dark:text-purple-400'
    }
    if (platform === 'grok') {
      return 'surface-inset surface-muted'
    }
    if (platform === 'kimi') {
      return 'bg-pink-100/[0.15] text-pink-600 dark:bg-pink-900/30 dark:text-pink-400'
    }
    if (platform === 'zhipu') {
      return 'bg-indigo-100/[0.15] text-indigo-600 dark:bg-indigo-900/30 dark:text-indigo-400'
    }
    if (platform === 'deepseek') {
      return 'bg-teal-100/[0.15] text-teal-600 dark:bg-teal-900/30 dark:text-teal-400'
    }
    if (platform === 'stepfun') {
      return 'bg-cyan-100/[0.15] text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-400'
    }
    if (platform === 'minimax') {
      return 'bg-rose-100/[0.15] text-rose-600 dark:bg-rose-900/30 dark:text-rose-400'
    }
    return 'bg-blue-100/[0.15] text-blue-600 dark:bg-blue-900/30 dark:text-blue-400'
  })()

  const planBadgeClass = (() => {
    if (normalizedPlanType === 'abnormal') {
      return 'bg-red-100/[0.15] text-red-600 dark:bg-red-900/30 dark:text-red-400'
    }
    // Free stays muted gray; paid Grok tiers get distinct colors.
    if (
      normalizedPlanType === 'free' ||
      normalizedPlanType === 'basic' ||
      normalizedPlanType === 'xbasic'
    ) {
      return 'surface-inset surface-muted dark:surface-inset'
    }
    if (platform === 'grok' && normalizedPlanType) {
      // Heavy / SuperGrok Heavy → purple
      if (normalizedPlanType.includes('heavy')) {
        return 'bg-purple-100/[0.15] text-purple-600 dark:bg-purple-900/30 dark:text-purple-300'
      }
      // SuperGrok → cyan
      if (normalizedPlanType.includes('supergrok')) {
        return 'bg-cyan-100/[0.15] text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-300'
      }
      // Any other non-free Grok plan (future tiers) → amber so it still stands out
      return 'bg-amber-100/[0.15] text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    }
    // OpenAI / other paid plan labels: keep readable distinction from free gray
    if (normalizedPlanType === 'plus') {
      return 'bg-sky-100/[0.15] text-sky-700 dark:bg-sky-900/30 dark:text-sky-300'
    }
    if (
      normalizedPlanType === 'team' ||
      normalizedPlanType === 'selfservebusinessprolite' ||
      (platform === 'openai' &&
        [
          'selfservebusinessusagebased',
          'business',
          'enterprise',
          'ent26',
          'enterprisecbpautomation',
          'enterprisecbpusagebased',
          'edu',
          'eduplus',
          'edupro'
        ].includes(normalizedPlanType))
    ) {
      return 'bg-indigo-100/[0.15] text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-300'
    }
    if (
      normalizedPlanType === 'pro' ||
      normalizedPlanType === 'chatgptpro' ||
      normalizedPlanType === 'prolite' ||
      (platform === 'openai' && normalizedPlanType === 'promax')
    ) {
      return 'bg-violet-100/[0.15] text-violet-700 dark:bg-violet-900/30 dark:text-violet-300'
    }
    return typeClass
  })()

  // Subscription expiration label (non-free only)
  const expiresLabel = (() => {
    if (!subscriptionExpiresAt || !planType) return ''
    if (
      normalizedPlanType === 'free' ||
      normalizedPlanType === 'basic' ||
      normalizedPlanType === 'xbasic'
    ) {
      return ''
    }
    try {
      const d = new Date(subscriptionExpiresAt)
      if (isNaN(d.getTime())) return ''
      const yyyy = d.getFullYear()
      const mm = String(d.getMonth() + 1).padStart(2, '0')
      const dd = String(d.getDate()).padStart(2, '0')
      return `${t('admin.accounts.subscriptionExpires')} ${yyyy}-${mm}-${dd}`
    } catch {
      return ''
    }
  })()

  // Privacy badge — shows different states for OpenAI/Antigravity OAuth privacy setting
  const privacyBadge: { label: string; icon: string; title: string; class: string } | null = (() => {
    if (type !== 'oauth' || !privacyMode) return null
    // 支持 OpenAI 和 Antigravity 平台
    if (platform !== 'openai' && platform !== 'antigravity') return null

    const shieldCheck =
      'M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z'
    const shieldX =
      'M12 9v3.75m0-10.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285zM12 18h.008v.008H12V18z'
    switch (privacyMode) {
      // OpenAI states
      case 'training_off':
        return {
          label: 'Private',
          icon: shieldCheck,
          title: t('admin.accounts.privacyTrainingOff'),
          class: 'bg-green-100/[0.15] text-green-600 dark:bg-green-900/30 dark:text-green-400'
        }
      case 'training_set_cf_blocked':
        return {
          label: 'CF',
          icon: shieldX,
          title: t('admin.accounts.privacyCfBlocked'),
          class: 'bg-yellow-100/[0.15] text-yellow-600 dark:bg-yellow-900/30 dark:text-yellow-400'
        }
      case 'training_set_failed':
        return {
          label: 'Fail',
          icon: shieldX,
          title: t('admin.accounts.privacyFailed'),
          class: 'bg-red-100/[0.15] text-red-600 dark:bg-red-900/30 dark:text-red-400'
        }
      // Antigravity states
      case 'privacy_set':
        return {
          label: 'Private',
          icon: shieldCheck,
          title: t('admin.accounts.privacyAntigravitySet'),
          class: 'bg-green-100/[0.15] text-green-600 dark:bg-green-900/30 dark:text-green-400'
        }
      case 'privacy_set_failed':
        return {
          label: 'Fail',
          icon: shieldX,
          title: t('admin.accounts.privacyAntigravityFailed'),
          class: 'bg-red-100/[0.15] text-red-600 dark:bg-red-900/30 dark:text-red-400'
        }
      default:
        return null
    }
  })()

  return (
    <div className="inline-flex flex-col gap-0.5 text-xs font-medium">
      {/* Row 1: Platform + Type */}
      <div className="inline-flex items-center overflow-hidden rounded-md">
        <span className={cn('inline-flex items-center gap-1 px-2 py-1', platformClass)}>
          <PlatformIcon platform={platform} size="xs" />
          <span>{platformLabel}</span>
        </span>
        <span className={cn('inline-flex items-center gap-1 px-1.5 py-1', typeClass)}>
          {/* OAuth icon */}
          {type === 'oauth' ? (
            <svg className="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z"
              />
            </svg>
          ) : type === 'setup-token' ? (
            /* Setup Token icon */
            <Icon name="shield" size="xs" />
          ) : type === 'service_account' ? (
            /* API Key icon */
            <Icon name="cloud" size="xs" />
          ) : (
            <Icon name="key" size="xs" />
          )}
          <span>{typeLabel}</span>
        </span>
      </div>
      {/* Row 2: Plan type + Privacy mode (only if either exists) */}
      {(planLabel || privacyBadge) && (
        <div className="inline-flex items-center overflow-hidden rounded-md">
          {planLabel && (
            <span className={cn('inline-flex items-center gap-1 px-1.5 py-1', planBadgeClass)}>
              {isGrokFreePlan ? (
                <GrokFreeIcon data-testid="grok-free-plan-icon" />
              ) : (
                planIconName && (
                  <Icon name={planIconName} size="xs" data-testid="grok-plan-icon" />
                )
              )}
              <span>{planLabel}</span>
            </span>
          )}
          {privacyBadge && (
            <span
              className={cn('inline-flex items-center gap-1 px-1.5 py-1', privacyBadge.class)}
              title={privacyBadge.title}
            >
              <svg className="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                <path strokeLinecap="round" strokeLinejoin="round" d={privacyBadge.icon} />
              </svg>
              <span>{privacyBadge.label}</span>
            </span>
          )}
        </div>
      )}
      {/* Row 3: Subscription expiration (non-free paid accounts only) */}
      {expiresLabel && (
        <div className="text-[10px] leading-tight pl-0.5 surface-muted" title={subscriptionExpiresAt}>
          {expiresLabel}
        </div>
      )}
    </div>
  )
}
