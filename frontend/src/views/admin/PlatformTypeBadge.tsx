import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { platformLabel } from '@/utils/platformColors'
import { normalizePlanType, openAIPlanTypeLabel } from '@/utils/planType'
import type { AccountPlatform, AccountType } from '@/types'
import { GrokFreeIcon, PlatformIcon } from './AccountBadges'
import { Icon } from './AccountIcon'

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

  const platformLabelText = platformLabel(platform)

  const normalizedAuthMode = (authMode || '').trim().toLowerCase().replace(/[\s_-]+/g, '')

  const typeLabel = useMemo(() => {
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
  }, [platform, type, normalizedAuthMode])

  const normalizedPlanTypeValue = normalizePlanType(planType)

  const planLabel = useMemo(() => {
    if (!normalizedPlanTypeValue) return ''
    // ChatGPT 档位命名（Pro 100 / Pro 200 / Pro 500、Business / Business Premium）只适用于
    // OpenAI：Antigravity 与 Grok 各自的 pro/team 沿用下面的通用标签。
    if (platform === 'openai') {
      const label = openAIPlanTypeLabel(planType)
      if (label) return label
    }
    switch (normalizedPlanTypeValue) {
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
  }, [normalizedPlanTypeValue, platform, planType, t])

  const isGrokFreePlan =
    platform === 'grok' &&
    (normalizedPlanTypeValue === 'free' ||
      normalizedPlanTypeValue === 'basic' ||
      normalizedPlanTypeValue === 'xbasic')

  const planIconName = useMemo<'bolt' | null>(() => {
    if (platform !== 'grok') return null
    // Paid Grok tiers (SuperGrok / Heavy) share the bolt mark; free uses GrokFreeIcon.
    if (
      normalizedPlanTypeValue === 'supergrok' ||
      normalizedPlanTypeValue === 'supergrokheavy' ||
      normalizedPlanTypeValue === 'heavy' ||
      normalizedPlanTypeValue.includes('heavy')
    ) {
      return 'bolt'
    }
    return null
  }, [platform, normalizedPlanTypeValue])

  const platformClass = useMemo(() => {
    switch (platform) {
      case 'anthropic':
        return 'bg-orange-100/[0.15] text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
      case 'openai':
        return 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
      case 'antigravity':
        return 'bg-purple-100/[0.15] text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
      case 'grok':
        return 'surface-inset surface-muted'
      case 'kimi':
        return 'bg-pink-100/[0.15] text-pink-700 dark:bg-pink-900/30 dark:text-pink-400'
      case 'zhipu':
        return 'bg-indigo-100/[0.15] text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-400'
      case 'deepseek':
        return 'bg-teal-100/[0.15] text-teal-700 dark:bg-teal-900/30 dark:text-teal-400'
      case 'stepfun':
        return 'bg-cyan-100/[0.15] text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-400'
      case 'minimax':
        return 'bg-rose-100/[0.15] text-rose-700 dark:bg-rose-900/30 dark:text-rose-400'
      default:
        return 'bg-blue-100/[0.15] text-blue-700 dark:bg-blue-900/30 dark:text-blue-400'
    }
  }, [platform])

  const typeClass = useMemo(() => {
    switch (platform) {
      case 'anthropic':
        return 'bg-orange-100/[0.15] text-orange-600 dark:bg-orange-900/30 dark:text-orange-400'
      case 'openai':
        return 'bg-emerald-100/[0.15] text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-400'
      case 'antigravity':
        return 'bg-purple-100/[0.15] text-purple-600 dark:bg-purple-900/30 dark:text-purple-400'
      case 'grok':
        return 'surface-inset surface-muted'
      case 'kimi':
        return 'bg-pink-100/[0.15] text-pink-600 dark:bg-pink-900/30 dark:text-pink-400'
      case 'zhipu':
        return 'bg-indigo-100/[0.15] text-indigo-600 dark:bg-indigo-900/30 dark:text-indigo-400'
      case 'deepseek':
        return 'bg-teal-100/[0.15] text-teal-600 dark:bg-teal-900/30 dark:text-teal-400'
      case 'stepfun':
        return 'bg-cyan-100/[0.15] text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-400'
      case 'minimax':
        return 'bg-rose-100/[0.15] text-rose-600 dark:bg-rose-900/30 dark:text-rose-400'
      default:
        return 'bg-blue-100/[0.15] text-blue-600 dark:bg-blue-900/30 dark:text-blue-400'
    }
  }, [platform])

  const planBadgeClass = useMemo(() => {
    if (normalizedPlanTypeValue === 'abnormal') {
      return 'bg-red-100/[0.15] text-red-600 dark:bg-red-900/30 dark:text-red-400'
    }
    // Free stays muted gray; paid Grok tiers get distinct colors.
    if (
      normalizedPlanTypeValue === 'free' ||
      normalizedPlanTypeValue === 'basic' ||
      normalizedPlanTypeValue === 'xbasic'
    ) {
      return 'surface-inset surface-muted dark:surface-inset'
    }
    if (platform === 'grok' && normalizedPlanTypeValue) {
      // Heavy / SuperGrok Heavy → purple
      if (normalizedPlanTypeValue.includes('heavy')) {
        return 'bg-purple-100/[0.15] text-purple-600 dark:bg-purple-900/30 dark:text-purple-300'
      }
      // SuperGrok → cyan
      if (normalizedPlanTypeValue.includes('supergrok')) {
        return 'bg-cyan-100/[0.15] text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-300'
      }
      // Any other non-free Grok plan (future tiers) → amber so it still stands out
      return 'bg-amber-100/[0.15] text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    }
    // OpenAI / other paid plan labels: keep readable distinction from free gray
    if (normalizedPlanTypeValue === 'plus') {
      return 'bg-sky-100/[0.15] text-sky-700 dark:bg-sky-900/30 dark:text-sky-300'
    }
    if (
      normalizedPlanTypeValue === 'team' ||
      normalizedPlanTypeValue === 'selfservebusinessprolite' ||
      (platform === 'openai' &&
        ['selfservebusinessusagebased', 'business', 'enterprise', 'ent26', 'enterprisecbpautomation', 'enterprisecbpusagebased', 'edu', 'eduplus', 'edupro'].includes(normalizedPlanTypeValue))
    ) {
      return 'bg-indigo-100/[0.15] text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-300'
    }
    if (
      normalizedPlanTypeValue === 'pro' ||
      normalizedPlanTypeValue === 'chatgptpro' ||
      normalizedPlanTypeValue === 'prolite' ||
      (platform === 'openai' && normalizedPlanTypeValue === 'promax')
    ) {
      return 'bg-violet-100/[0.15] text-violet-700 dark:bg-violet-900/30 dark:text-violet-300'
    }
    return typeClass
  }, [normalizedPlanTypeValue, platform, typeClass])

  // Subscription expiration label (non-free only)
  const expiresLabel = useMemo(() => {
    if (!subscriptionExpiresAt || !planType) return ''
    if (
      normalizedPlanTypeValue === 'free' ||
      normalizedPlanTypeValue === 'basic' ||
      normalizedPlanTypeValue === 'xbasic'
    )
      return ''
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
  }, [subscriptionExpiresAt, planType, normalizedPlanTypeValue, t])

  // Privacy badge — shows different states for OpenAI/Antigravity OAuth privacy setting
  const privacyBadge = useMemo(() => {
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
        return { label: 'Private', icon: shieldCheck, title: t('admin.accounts.privacyTrainingOff'), class: 'bg-green-100/[0.15] text-green-600 dark:bg-green-900/30 dark:text-green-400' }
      case 'training_set_cf_blocked':
        return { label: 'CF', icon: shieldX, title: t('admin.accounts.privacyCfBlocked'), class: 'bg-yellow-100/[0.15] text-yellow-600 dark:bg-yellow-900/30 dark:text-yellow-400' }
      case 'training_set_failed':
        return { label: 'Fail', icon: shieldX, title: t('admin.accounts.privacyFailed'), class: 'bg-red-100/[0.15] text-red-600 dark:bg-red-900/30 dark:text-red-400' }
      // Antigravity states
      case 'privacy_set':
        return { label: 'Private', icon: shieldCheck, title: t('admin.accounts.privacyAntigravitySet'), class: 'bg-green-100/[0.15] text-green-600 dark:bg-green-900/30 dark:text-green-400' }
      case 'privacy_set_failed':
        return { label: 'Fail', icon: shieldX, title: t('admin.accounts.privacyAntigravityFailed'), class: 'bg-red-100/[0.15] text-red-600 dark:bg-red-900/30 dark:text-red-400' }
      default:
        return null
    }
  }, [type, privacyMode, platform, t])

  return (
    <div className="inline-flex flex-col gap-0.5 text-xs font-medium">
      {/* Row 1: Platform + Type */}
      <div className="inline-flex items-center overflow-hidden rounded-md">
        <span className={cn('inline-flex items-center gap-1 px-2 py-1', platformClass)}>
          <PlatformIcon platform={platform} size="xs" />
          <span>{platformLabelText}</span>
        </span>
        <span className={cn('inline-flex items-center gap-1 px-1.5 py-1', typeClass)}>
          {type === 'oauth' ? (
            <svg className="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z" />
            </svg>
          ) : type === 'setup-token' ? (
            <Icon name="shield" size="xs" />
          ) : type === 'service_account' ? (
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
                <GrokFreeIcon />
              ) : planIconName ? (
                <Icon name={planIconName} size="xs" />
              ) : null}
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

export default PlatformTypeBadge
