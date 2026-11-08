import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { useSubscriptionsStore } from '@/stores/subscriptions'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import type { UserSubscription } from '@/types'
import { Icon } from './Icon'

export interface SubscriptionProgressMiniProps {
  inline?: boolean
}

export function SubscriptionProgressMini({ inline = false }: SubscriptionProgressMiniProps) {
  const { t } = useTranslation()
  const subscriptionStore = useSubscriptionsStore()
  const containerRef = useRef<HTMLDivElement | null>(null)
  const [tooltipOpen, setTooltipOpen] = useState(false)

  // Use store data instead of local state
  const activeSubscriptions = subscriptionStore.activeSubscriptions
  const hasActiveSubscriptions = subscriptionStore.hasActiveSubscriptions()
  // 订阅功能关闭后，即使用户仍持有后台分配的订阅，顶栏也不再露出订阅进度与「查看全部订阅」入口。
  const subscriptionFeatureEnabled = isFeatureFlagEnabled(FeatureFlags.subscription)

  const displaySubscriptions = (() => {
    // Sort by most usage (highest percentage first)
    return [...activeSubscriptions].sort((a, b) => {
      const aMax = getMaxUsagePercentage(a)
      const bMax = getMaxUsagePercentage(b)
      return bMax - aMax
    })
  })()

  function getMaxUsagePercentage(sub: UserSubscription): number {
    const percentages: number[] = []
    if (sub.group?.daily_limit_usd) {
      percentages.push(((sub.daily_usage_usd || 0) / sub.group.daily_limit_usd) * 100)
    }
    if (sub.group?.weekly_limit_usd) {
      percentages.push(((sub.weekly_usage_usd || 0) / sub.group.weekly_limit_usd) * 100)
    }
    if (sub.group?.monthly_limit_usd) {
      percentages.push(((sub.monthly_usage_usd || 0) / sub.group.monthly_limit_usd) * 100)
    }
    if (sub.group?.five_hour_limit_usd) {
      percentages.push(((sub.five_hour_usage_usd || 0) / sub.group.five_hour_limit_usd) * 100)
    }
    return percentages.length > 0 ? Math.max(...percentages) : 0
  }

  function isUnlimited(sub: UserSubscription): boolean {
    return (
      !sub.group?.daily_limit_usd &&
      !sub.group?.weekly_limit_usd &&
      !sub.group?.monthly_limit_usd &&
      !sub.group?.five_hour_limit_usd
    )
  }

  function getProgressDotClass(sub: UserSubscription): string {
    // Unlimited subscriptions get a special color
    if (isUnlimited(sub)) {
      return 'bg-emerald-500'
    }
    const maxPercentage = getMaxUsagePercentage(sub)
    if (maxPercentage >= 90) return 'bg-red-500'
    if (maxPercentage >= 70) return 'bg-orange-500'
    return 'bg-green-500'
  }

  function getProgressBarClass(used: number | undefined, limit: number | null | undefined): string {
    if (!limit || limit === 0) return 'bg-gray-400'
    const percentage = ((used || 0) / limit) * 100
    if (percentage >= 90) return 'bg-red-500'
    if (percentage >= 70) return 'bg-orange-500'
    return 'bg-green-500'
  }

  function getProgressWidth(used: number | undefined, limit: number | null | undefined): string {
    if (!limit || limit === 0) return '0%'
    const percentage = Math.min(((used || 0) / limit) * 100, 100)
    return `${percentage}%`
  }

  function formatUsage(used: number | undefined, limit: number | null | undefined): string {
    const usedValue = (used || 0).toFixed(2)
    const limitValue = limit?.toFixed(2) || '∞'
    return `$${usedValue}/$${limitValue}`
  }

  function formatDaysRemaining(expiresAt: string): string {
    const now = new Date()
    const expires = new Date(expiresAt)
    const diff = expires.getTime() - now.getTime()
    if (diff < 0) return t('subscriptionProgress.expired')
    const days = Math.ceil(diff / (1000 * 60 * 60 * 24))
    if (days === 0) return t('subscriptionProgress.expiresToday')
    if (days === 1) return t('subscriptionProgress.expiresTomorrow')
    return t('subscriptionProgress.daysRemaining', { days })
  }

  function getDaysRemainingClass(expiresAt: string): string {
    const now = new Date()
    const expires = new Date(expiresAt)
    const diff = expires.getTime() - now.getTime()
    const days = Math.ceil(diff / (1000 * 60 * 60 * 24))
    if (days <= 3) return 'text-red-600 dark:text-red-400'
    if (days <= 7) return 'text-orange-600 dark:text-orange-400'
    return 'surface-muted'
  }

  const closeTooltip = () => setTooltipOpen(false)

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        closeTooltip()
      }
    }
    document.addEventListener('click', handleClickOutside)
    return () => document.removeEventListener('click', handleClickOutside)
  }, [])

  useEffect(() => {
    // Trigger initial fetch if not already loaded
    // The actual data loading is handled by App.vue globally
    if (!subscriptionFeatureEnabled) return
    subscriptionStore.fetchActiveSubscriptions().catch((error) => {
      console.error('Failed to load subscriptions in SubscriptionProgressMini:', error)
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  if (!subscriptionFeatureEnabled || !hasActiveSubscriptions) return null

  return (
    <div className="relative" ref={containerRef}>
      {/* Mini Progress Display */}
      <button
        onClick={() => setTooltipOpen((open) => !open)}
        className="flex cursor-pointer items-center gap-2 rounded-xl bg-purple-50/[0.15] px-3 py-1.5 transition-colors hover:bg-purple-100/25 dark:bg-purple-900/20 dark:hover:bg-purple-900/30"
        title={t('subscriptionProgress.viewDetails')}
      >
        <Icon name="creditCard" size="sm" className="text-purple-600 dark:text-purple-400" />
        <div className="flex items-center gap-1.5">
          {/* Combined progress indicator */}
          <div className="flex items-center gap-0.5">
            {displaySubscriptions.slice(0, 3).map((sub, index) => (
              <div
                key={index}
                className={cn('console-data-mark h-2 w-2 rounded-full', getProgressDotClass(sub))}
              ></div>
            ))}
          </div>
          <span className="text-xs font-medium text-purple-700 dark:text-purple-300">
            {activeSubscriptions.length}
          </span>
        </div>
      </button>

      {/* Hover/Click Tooltip */}
      {tooltipOpen && (
        <div
          className={cn(
            'glass-surface z-50 mt-2 overflow-hidden',
            inline ? 'w-full' : 'absolute right-0 w-[340px] max-w-[calc(100vw-24px)]'
          )}
        >
          <div className="border-b border-gray-100 p-3 dark:border-dark-700">
            <h3 className="text-sm font-semibold text-gray-900 dark:text-white">
              {t('subscriptionProgress.title')}
            </h3>
            <p className="mt-0.5 text-xs surface-muted">
              {t('subscriptionProgress.activeCount', { count: activeSubscriptions.length })}
            </p>
          </div>

          <div className="max-h-64 overflow-y-auto">
            {displaySubscriptions.map((subscription) => (
              <div
                key={subscription.id}
                className="border-b border-gray-50 p-3 last:border-b-0 dark:border-dark-700/50"
              >
                <div className="mb-2 flex items-center justify-between">
                  <span className="text-sm font-medium text-gray-900 dark:text-white">
                    {subscription.group?.name || `Group #${subscription.group_id}`}
                  </span>
                  {subscription.expires_at && (
                    <span className={cn('text-xs', getDaysRemainingClass(subscription.expires_at))}>
                      {formatDaysRemaining(subscription.expires_at)}
                    </span>
                  )}
                </div>

                {/* Progress bars or Unlimited badge */}
                <div className="space-y-1.5">
                  {/* Unlimited subscription badge */}
                  {isUnlimited(subscription) ? (
                    <div className="flex items-center gap-2 rounded-lg bg-gradient-to-r from-emerald-50 to-teal-50 px-2.5 py-1.5 dark:from-emerald-900/20 dark:to-teal-900/20">
                      <span className="text-lg text-emerald-600 dark:text-emerald-400">∞</span>
                      <span className="text-xs font-medium text-emerald-700 dark:text-emerald-300">
                        {t('subscriptionProgress.unlimited')}
                      </span>
                    </div>
                  ) : (
                    <>
                      {subscription.group?.five_hour_limit_usd && (
                        <div className="flex items-center gap-2">
                          <span className="w-8 flex-shrink-0 text-[10px] surface-muted">
                            {t('subscriptionProgress.fiveHour')}
                          </span>
                          <div className="h-1.5 min-w-0 flex-1 rounded-full bg-gray-200 dark:bg-dark-600">
                            <div
                              className={cn(
                                'console-data-mark h-1.5 rounded-full transition-all',
                                getProgressBarClass(
                                  subscription.five_hour_usage_usd,
                                  subscription.group?.five_hour_limit_usd
                                )
                              )}
                              style={{
                                width: getProgressWidth(
                                  subscription.five_hour_usage_usd,
                                  subscription.group?.five_hour_limit_usd
                                )
                              }}
                            ></div>
                          </div>
                          <span className="w-24 flex-shrink-0 text-right text-[10px] surface-muted">
                            {formatUsage(
                              subscription.five_hour_usage_usd,
                              subscription.group?.five_hour_limit_usd
                            )}
                          </span>
                        </div>
                      )}

                      {subscription.group?.daily_limit_usd && (
                        <div className="flex items-center gap-2">
                          <span className="w-8 flex-shrink-0 text-[10px] surface-muted">
                            {t('subscriptionProgress.daily')}
                          </span>
                          <div className="h-1.5 min-w-0 flex-1 rounded-full bg-gray-200 dark:bg-dark-600">
                            <div
                              className={cn(
                                'console-data-mark h-1.5 rounded-full transition-all',
                                getProgressBarClass(
                                  subscription.daily_usage_usd,
                                  subscription.group?.daily_limit_usd
                                )
                              )}
                              style={{
                                width: getProgressWidth(
                                  subscription.daily_usage_usd,
                                  subscription.group?.daily_limit_usd
                                )
                              }}
                            ></div>
                          </div>
                          <span className="w-24 flex-shrink-0 text-right text-[10px] surface-muted">
                            {formatUsage(subscription.daily_usage_usd, subscription.group?.daily_limit_usd)}
                          </span>
                        </div>
                      )}

                      {subscription.group?.weekly_limit_usd && (
                        <div className="flex items-center gap-2">
                          <span className="w-8 flex-shrink-0 text-[10px] surface-muted">
                            {t('subscriptionProgress.weekly')}
                          </span>
                          <div className="h-1.5 min-w-0 flex-1 rounded-full bg-gray-200 dark:bg-dark-600">
                            <div
                              className={cn(
                                'console-data-mark h-1.5 rounded-full transition-all',
                                getProgressBarClass(
                                  subscription.weekly_usage_usd,
                                  subscription.group?.weekly_limit_usd
                                )
                              )}
                              style={{
                                width: getProgressWidth(
                                  subscription.weekly_usage_usd,
                                  subscription.group?.weekly_limit_usd
                                )
                              }}
                            ></div>
                          </div>
                          <span className="w-24 flex-shrink-0 text-right text-[10px] surface-muted">
                            {formatUsage(subscription.weekly_usage_usd, subscription.group?.weekly_limit_usd)}
                          </span>
                        </div>
                      )}

                      {subscription.group?.monthly_limit_usd && (
                        <div className="flex items-center gap-2">
                          <span className="w-8 flex-shrink-0 text-[10px] surface-muted">
                            {t('subscriptionProgress.monthly')}
                          </span>
                          <div className="h-1.5 min-w-0 flex-1 rounded-full bg-gray-200 dark:bg-dark-600">
                            <div
                              className={cn(
                                'console-data-mark h-1.5 rounded-full transition-all',
                                getProgressBarClass(
                                  subscription.monthly_usage_usd,
                                  subscription.group?.monthly_limit_usd
                                )
                              )}
                              style={{
                                width: getProgressWidth(
                                  subscription.monthly_usage_usd,
                                  subscription.group?.monthly_limit_usd
                                )
                              }}
                            ></div>
                          </div>
                          <span className="w-24 flex-shrink-0 text-right text-[10px] surface-muted">
                            {formatUsage(subscription.monthly_usage_usd, subscription.group?.monthly_limit_usd)}
                          </span>
                        </div>
                      )}
                    </>
                  )}
                </div>
              </div>
            ))}
          </div>

          <div className="border-t border-gray-100 p-2 dark:border-dark-700">
            <Link
              to="/subscriptions"
              onClick={closeTooltip}
              className="block w-full py-1 text-center text-xs text-primary-600 hover:underline dark:text-primary-400"
            >
              {t('subscriptionProgress.viewAll')}
            </Link>
          </div>
        </div>
      )}
    </div>
  )
}
