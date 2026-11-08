import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { useAppStore } from '@/stores/app'
import { formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import type { GroupPlatform, SubscriptionType } from '@/types'
import { PlatformIcon } from './PlatformIcon'

export interface GroupBadgeProps {
  name: string
  platform?: GroupPlatform
  subscriptionType?: SubscriptionType
  rateMultiplier?: number
  userRateMultiplier?: number | null
  peakRateEnabled?: boolean
  peakStart?: string
  peakEnd?: string
  peakRateMultiplier?: number
  showRate?: boolean
  /** Allow full group names to wrap in layouts that have room for multiple lines. */
  truncate?: boolean
  daysRemaining?: number | null
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
  const serverUtcOffset = useAppStore((s) => s.cachedPublicSettings?.server_utc_offset)

  const isSubscription = subscriptionType === 'subscription'

  const hasCustomRate =
    userRateMultiplier !== null &&
    userRateMultiplier !== undefined &&
    rateMultiplier !== undefined &&
    userRateMultiplier !== rateMultiplier

  const hasPeak = Boolean(showRate && peakRateEnabled && peakStart && peakEnd)

  const peakRateText = useMemo(
    () =>
      formatPeakRateWindow(
        {
          peak_rate_enabled: peakRateEnabled,
          peak_start: peakStart,
          peak_end: peakEnd,
          peak_rate_multiplier: peakRateMultiplier
        },
        serverTimezoneLabel(serverUtcOffset)
      ),
    [peakRateEnabled, peakStart, peakEnd, peakRateMultiplier, serverUtcOffset]
  )

  const peakRateTitle = t('common.peakRateTooltip', { window: peakRateText })

  const showLabel =
    showRate && (isSubscription || rateMultiplier !== undefined || hasCustomRate)

  const rateLabel = rateMultiplier !== undefined ? `${rateMultiplier}x` : ''
  let labelText = rateLabel
  if (isSubscription && !alwaysShowRate) {
    if (daysRemaining !== null && daysRemaining !== undefined) {
      labelText = daysRemaining <= 0 ? t('admin.users.expired') : t('admin.users.daysRemaining', { days: daysRemaining })
    } else {
      labelText = t('groups.subscription')
    }
  }

  const labelClass = useMemo(() => {
    const base = 'shrink-0 whitespace-nowrap px-1.5 py-0.5 rounded text-[10px] font-semibold'
    if (!isSubscription) {
      return `${base} bg-black/10 dark:bg-white/10`
    }
    if (daysRemaining !== null && daysRemaining !== undefined) {
      if (daysRemaining <= 0 || daysRemaining <= 3) {
        return `${base} bg-red-200/80 text-red-800 dark:bg-red-800/50 dark:text-red-300`
      }
      if (daysRemaining <= 7) {
        return `${base} bg-amber-200/80 text-amber-800 dark:bg-amber-800/50 dark:text-amber-300`
      }
    }
    const theme: Record<string, string> = {
      anthropic: 'bg-orange-200/60 text-orange-800 dark:bg-orange-800/40 dark:text-orange-300',
      openai: 'bg-emerald-200/60 text-emerald-800 dark:bg-emerald-800/40 dark:text-emerald-300',
      gemini: 'bg-blue-200/60 text-blue-800 dark:bg-blue-800/40 dark:text-blue-300',
      antigravity: 'bg-purple-200/60 text-purple-800 dark:bg-purple-800/40 dark:text-purple-300',
      grok: 'bg-zinc-300/70 text-zinc-800 dark:bg-zinc-700/60 dark:text-zinc-200',
      kimi: 'bg-pink-200/60 text-pink-800 dark:bg-pink-800/40 dark:text-pink-300',
      zhipu: 'bg-indigo-200/60 text-indigo-800 dark:bg-indigo-800/40 dark:text-indigo-300',
      deepseek: 'bg-teal-200/60 text-teal-800 dark:bg-teal-800/40 dark:text-teal-300',
      stepfun: 'bg-cyan-200/60 text-cyan-800 dark:bg-cyan-800/40 dark:text-cyan-300',
      minimax: 'bg-rose-200/60 text-rose-800 dark:bg-rose-800/40 dark:text-rose-300',
      composite: 'bg-cyan-200/70 text-cyan-900 dark:bg-cyan-900/50 dark:text-cyan-300'
    }
    return `${base} ${theme[platform ?? ''] ?? 'bg-violet-200/60 text-violet-800 dark:bg-violet-800/40 dark:text-violet-300'}`
  }, [isSubscription, daysRemaining, platform])

  const badgeClass = useMemo(() => {
    const themes: Record<string, [string, string]> = {
      anthropic: ['bg-orange-100/[0.15] text-orange-700 dark:bg-orange-900/30 dark:text-orange-400', 'bg-amber-50/[0.15] text-amber-700 dark:bg-amber-900/20 dark:text-amber-400'],
      openai: ['bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400', 'bg-green-50/[0.15] text-green-700 dark:bg-green-900/20 dark:text-green-400'],
      gemini: ['bg-blue-100/[0.15] text-blue-700 dark:bg-blue-900/30 dark:text-blue-400', 'bg-sky-50/[0.15] text-sky-700 dark:bg-sky-900/20 dark:text-sky-400'],
      antigravity: ['bg-purple-100/[0.15] text-purple-700 dark:bg-purple-900/30 dark:text-purple-400', 'bg-fuchsia-50/[0.15] text-fuchsia-700 dark:bg-fuchsia-900/20 dark:text-fuchsia-400'],
      grok: ['bg-zinc-200 text-zinc-800 dark:bg-zinc-700 dark:text-zinc-100', 'bg-zinc-100 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-200'],
      kimi: ['bg-pink-100/[0.15] text-pink-700 dark:bg-pink-900/30 dark:text-pink-400', 'bg-pink-50/[0.15] text-pink-700 dark:bg-pink-900/20 dark:text-pink-400'],
      zhipu: ['bg-indigo-100/[0.15] text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-400', 'bg-indigo-50/[0.15] text-indigo-700 dark:bg-indigo-900/20 dark:text-indigo-400'],
      deepseek: ['bg-teal-100/[0.15] text-teal-700 dark:bg-teal-900/30 dark:text-teal-400', 'bg-teal-50/[0.15] text-teal-700 dark:bg-teal-900/20 dark:text-teal-400'],
      stepfun: ['bg-cyan-100/[0.15] text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-400', 'bg-cyan-50/[0.15] text-cyan-700 dark:bg-cyan-900/20 dark:text-cyan-400'],
      minimax: ['bg-rose-100/[0.15] text-rose-700 dark:bg-rose-900/30 dark:text-rose-400', 'bg-rose-50/[0.15] text-rose-700 dark:bg-rose-900/20 dark:text-rose-400'],
      composite: ['bg-cyan-100/[0.15] text-cyan-800 dark:bg-cyan-900/30 dark:text-cyan-300', 'bg-cyan-50/[0.15] text-cyan-800 dark:bg-cyan-900/20 dark:text-cyan-300']
    }
    const theme = themes[platform ?? '']
    if (theme) return isSubscription ? theme[0] : theme[1]
    return isSubscription
      ? 'bg-violet-100/[0.15] text-violet-700 dark:bg-violet-900/30 dark:text-violet-400'
      : 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
  }, [platform, isSubscription])

  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium transition-colors',
        badgeClass,
        className
      )}
    >
      {platform && <PlatformIcon platform={platform} size="sm" />}
      <span className={truncate ? 'truncate' : 'min-w-0 whitespace-normal [overflow-wrap:anywhere]'}>{name}</span>
      {showLabel && (
        <span className={labelClass}>
          {hasCustomRate ? (
            <>
              <span className="line-through opacity-50 mr-0.5">{rateMultiplier}x</span>
              <span className="font-bold">{userRateMultiplier}x</span>
            </>
          ) : (
            labelText
          )}
        </span>
      )}
      {hasPeak && (
        <span
          className="px-1.5 py-0.5 rounded text-[10px] font-semibold bg-amber-100/[0.15] text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
          title={peakRateTitle}
        >
          {peakRateText}
        </span>
      )}
    </span>
  )
}

export default GroupBadge
