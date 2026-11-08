import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAppStore } from '@/stores/app'
import subscriptionsAPI from '@/api/subscriptions'
import { Pagination } from '@/components/ui'
import type { UserSubscription } from '@/types'
import { formatDateTimeToMinute } from '@/utils/format'
import {
  getExpirationDateRelation,
  getRemainingDurationParts,
  isOneTimeDailyQuota,
  type RemainingDurationParts
} from '@/utils/subscriptionQuota'
import { hasPeakRate, formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import {
  platformBorderClass,
  platformBadgeClass,
  platformButtonClass,
  platformLabel
} from '@/utils/platformColors'
// Plain subscription helper (returns a disposer) despite its `use*` name, so it
// is imported under a non-hook alias for the rules-of-hooks heuristic.
import { useLocaleRefresh as subscribeLocaleRefresh } from '@/utils/localeRefresh'
import { Icon } from './icons'
import { UserCardScroll } from './UserCardScroll'

const PAGE_SIZE = 9

function platformAccentDotClass(p: string): string {
  switch (p) {
    case 'anthropic':
      return 'bg-orange-500'
    case 'openai':
      return 'bg-emerald-500'
    case 'antigravity':
      return 'bg-purple-500'
    case 'gemini':
      return 'bg-blue-500'
    default:
      return 'bg-gray-400'
  }
}

export default function SubscriptionsView() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const appStore = useAppStore()
  const [subscriptions, setSubscriptions] = useState<UserSubscription[]>([])
  const [loading, setLoading] = useState(true)
  const [page, setPage] = useState(1)
  const subscriptionRequest = useRef(0)

  const pagedSubscriptions = subscriptions.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)

  useEffect(() => {
    setPage((current) => Math.min(current, Math.max(1, Math.ceil(subscriptions.length / PAGE_SIZE))))
  }, [subscriptions.length])

  function subscriptionHasPeakRate(subscription: UserSubscription): boolean {
    return hasPeakRate(subscription.group)
  }

  function subscriptionPeakRateLabel(subscription: UserSubscription): string {
    return formatPeakRateWindow(
      subscription.group,
      serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset)
    )
  }

  const loadSubscriptions = useCallback(async () => {
    const request = ++subscriptionRequest.current
    try {
      setLoading(true)
      const result = await subscriptionsAPI.getMySubscriptions()
      if (request === subscriptionRequest.current) setSubscriptions(result)
    } catch (error) {
      console.error('Failed to load subscriptions:', error)
      useAppStore.getState().showError(t('userSubscriptions.failedToLoad'))
    } finally {
      if (request === subscriptionRequest.current) setLoading(false)
    }
  }, [t])

  // The locale-changed listener always calls the latest loader without
  // re-subscribing when the translation function identity changes.
  const loadSubscriptionsRef = useRef(loadSubscriptions)
  useEffect(() => {
    loadSubscriptionsRef.current = loadSubscriptions
  }, [loadSubscriptions])

  useEffect(() => {
    void loadSubscriptions()
  }, [loadSubscriptions])

  useEffect(
    () =>
      subscribeLocaleRefresh(() => {
        void loadSubscriptionsRef.current()
      }),
    []
  )

  function getProgressWidth(used: number | undefined, limit: number | null | undefined): string {
    if (!limit || limit === 0) return '0%'
    const percentage = Math.min(((used || 0) / limit) * 100, 100)
    return `${percentage}%`
  }

  function getProgressBarClass(used: number | undefined, limit: number | null | undefined): string {
    if (!limit || limit === 0) return 'bg-gray-400'
    const percentage = ((used || 0) / limit) * 100
    if (percentage >= 90) return 'bg-red-500'
    if (percentage >= 70) return 'bg-orange-500'
    return 'bg-green-500'
  }

  function formatExpirationDate(expiresAt: string): string {
    const now = new Date()
    const expires = new Date(expiresAt)
    const diff = expires.getTime() - now.getTime()
    const days = Math.ceil(diff / (1000 * 60 * 60 * 24))
    const relation = getExpirationDateRelation(expires, now)

    if (relation === null) return ''
    if (relation === 'expired') return t('userSubscriptions.status.expired')

    const dateStr = formatDateTimeToMinute(expires)

    if (relation === 'today') return `${dateStr} (${t('common.today')})`
    if (relation === 'tomorrow') return `${dateStr} (${t('common.tomorrow')})`

    return t('userSubscriptions.daysRemaining', { days }) + ` (${dateStr})`
  }

  function getExpirationClass(expiresAt: string): string {
    const now = new Date()
    const expires = new Date(expiresAt)
    const diff = expires.getTime() - now.getTime()
    const days = Math.ceil(diff / (1000 * 60 * 60 * 24))

    if (diff <= 0) return 'font-medium text-red-600 dark:text-red-400'
    if (days <= 3) return 'text-red-600 dark:text-red-400'
    if (days <= 7) return 'text-orange-600 dark:text-orange-400'
    return 'surface-muted'
  }

  function formatDurationParts(parts: RemainingDurationParts): string {
    if (parts.days > 0) return `${parts.days}d ${parts.hours}h`
    if (parts.hours > 0) return `${parts.hours}h ${parts.minutes}m`
    return `${parts.minutes}m`
  }

  function formatDailyUsageWindow(subscription: UserSubscription): string {
    if (isOneTimeDailyQuota(subscription) && subscription.expires_at) {
      const parts = getRemainingDurationParts(subscription.expires_at)
      if (!parts) return t('userSubscriptions.windowNotActive')
      return t('userSubscriptions.quotaEndsIn', { time: formatDurationParts(parts) })
    }

    return t('userSubscriptions.resetIn', {
      time: formatResetTime(subscription.daily_window_start, 24)
    })
  }

  function formatResetTime(windowStart: string | null, windowHours: number): string {
    if (!windowStart) return t('userSubscriptions.windowNotActive')

    const start = new Date(windowStart)
    const end = new Date(start.getTime() + windowHours * 60 * 60 * 1000)
    const parts = getRemainingDurationParts(end)

    return parts ? formatDurationParts(parts) : t('userSubscriptions.windowNotActive')
  }

  return (
    <div className="workspace-page subscriptions-page">
      <header
        className="card workspace-fixed subscriptions-heading flex flex-wrap items-center justify-between gap-4 p-5"
        data-testid="subscriptions-heading"
      >
        <div>
          <h2 className="text-base font-semibold">{t('userSubscriptions.title')}</h2>
          <p className="mt-1 text-xs surface-muted">{t('userSubscriptions.description')}</p>
        </div>
        <span className="surface-muted rounded-lg border px-3 py-2 text-xs">
          {t('userSubscriptions.count', { count: subscriptions.length })} ·{' '}
          {t('userSubscriptions.pageSize', { count: PAGE_SIZE })}
        </span>
      </header>
      <UserCardScroll label={t('userSubscriptions.title')} resetKey={page} data-testid="subscriptions-activity">
        {loading ? (
          <div className="flex justify-center py-12">
            <div className="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent" />
          </div>
        ) : subscriptions.length === 0 ? (
          <div className="card workspace-shell p-12 text-center">
            <div className="surface-inset mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full">
              <Icon name="creditCard" size="xl" className="text-gray-400" />
            </div>
            <h3 className="mb-2 text-lg font-semibold text-gray-900 dark:text-white">
              {t('userSubscriptions.noActiveSubscriptions')}
            </h3>
            <p className="surface-muted">{t('userSubscriptions.noActiveSubscriptionsDesc')}</p>
          </div>
        ) : (
          <div className="subscriptions-grid grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
            {pagedSubscriptions.map((subscription) => (
              <div
                key={subscription.id}
                data-testid="subscription-card"
                className={`glass-surface overflow-hidden border ${platformBorderClass(subscription.group?.platform || '')}`}
              >
                {/* Header */}
                <div className="flex flex-col items-stretch gap-3 border-b border-gray-100 p-4 dark:border-dark-700">
                  <div className="flex min-w-0 flex-1 items-start gap-3">
                    <div
                      className={`console-data-mark h-1.5 w-1.5 shrink-0 rounded-full ${platformAccentDotClass(
                        subscription.group?.platform || ''
                      )}`}
                    />
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <h3 className="break-words font-semibold text-gray-900 dark:text-white [overflow-wrap:anywhere]">
                          {subscription.group?.name || `Group #${subscription.group_id}`}
                        </h3>
                        <span
                          className={`rounded-md border px-2 py-0.5 text-[11px] font-medium ${platformBadgeClass(
                            subscription.group?.platform || ''
                          )}`}
                        >
                          {platformLabel(subscription.group?.platform || '')}
                        </span>
                      </div>
                      {subscription.group?.description && (
                        <p className="mt-0.5 break-words text-xs surface-muted [overflow-wrap:anywhere]">
                          {subscription.group.description}
                        </p>
                      )}
                      <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[11px] surface-muted">
                        <span>
                          {t('payment.planCard.rate')}: ×{subscription.group?.rate_multiplier ?? 1}
                        </span>
                        {subscriptionHasPeakRate(subscription) && (
                          <span className="text-amber-700 dark:text-amber-300">
                            {t('payment.planCard.peakRate')}: {subscriptionPeakRateLabel(subscription)}
                          </span>
                        )}
                      </div>
                    </div>
                  </div>
                  <div className="flex shrink-0 flex-wrap items-center justify-between gap-2">
                    <span
                      className={`rounded-full px-2 py-0.5 text-xs font-medium ${
                        subscription.status === 'active'
                          ? 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
                          : subscription.status === 'expired'
                            ? 'surface-inset text-gray-600 dark:surface-inset dark:text-gray-400'
                            : 'bg-red-100/[0.15] text-red-700 dark:bg-red-900/40 dark:text-red-300'
                      }`}
                    >
                      {t(`userSubscriptions.status.${subscription.status}`)}
                    </span>
                    {subscription.status === 'active' && (
                      <button
                        className={`rounded-lg px-3 py-1.5 text-xs font-semibold text-white transition-colors ${platformButtonClass(
                          subscription.group?.platform || ''
                        )}`}
                        onClick={() =>
                          navigate({
                            pathname: '/purchase',
                            search: `tab=subscription&group=${String(subscription.group_id)}`
                          })
                        }
                      >
                        {t('payment.renewNow')}
                      </button>
                    )}
                  </div>
                </div>

                {/* Usage Progress */}
                <div className="space-y-4 p-4">
                  {subscription.expires_at ? (
                    <div className="flex items-start justify-between gap-3 text-sm">
                      <span className="shrink-0 whitespace-nowrap surface-muted">{t('userSubscriptions.expires')}</span>
                      <span className={`min-w-0 text-right ${getExpirationClass(subscription.expires_at)}`}>
                        {formatExpirationDate(subscription.expires_at)}
                      </span>
                    </div>
                  ) : (
                    <div className="flex items-start justify-between gap-3 text-sm">
                      <span className="shrink-0 whitespace-nowrap surface-muted">{t('userSubscriptions.expires')}</span>
                      <span className="surface-muted">{t('userSubscriptions.noExpiration')}</span>
                    </div>
                  )}

                  {subscription.group?.five_hour_limit_usd && (
                    <div className="space-y-2">
                      <div className="flex items-center justify-between">
                        <span className="text-sm font-medium surface-muted">{t('userSubscriptions.fiveHour')}</span>
                        <span className="text-sm surface-muted">
                          ${(subscription.five_hour_usage_usd || 0).toFixed(2)} / $
                          {subscription.group.five_hour_limit_usd.toFixed(2)}
                        </span>
                      </div>
                      <div className="relative h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                        <div
                          className={`console-data-mark absolute inset-y-0 left-0 rounded-full transition-all duration-300 ${getProgressBarClass(
                            subscription.five_hour_usage_usd,
                            subscription.group.five_hour_limit_usd
                          )}`}
                          style={{
                            width: getProgressWidth(
                              subscription.five_hour_usage_usd,
                              subscription.group.five_hour_limit_usd
                            )
                          }}
                        />
                      </div>
                      {subscription.five_hour_window_start ? (
                        <p className="text-xs surface-muted">
                          {t('userSubscriptions.resetIn', {
                            time: formatResetTime(subscription.five_hour_window_start, 5)
                          })}
                        </p>
                      ) : (
                        <p className="text-xs surface-muted">{t('userSubscriptions.windowNotActive')}</p>
                      )}
                    </div>
                  )}

                  {subscription.group?.daily_limit_usd && (
                    <div className="space-y-2">
                      <div className="flex items-center justify-between">
                        <span className="text-sm font-medium surface-muted">{t('userSubscriptions.daily')}</span>
                        <span className="text-sm surface-muted">
                          ${(subscription.daily_usage_usd || 0).toFixed(2)} / $
                          {subscription.group.daily_limit_usd.toFixed(2)}
                        </span>
                      </div>
                      <div className="relative h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                        <div
                          className={`console-data-mark absolute inset-y-0 left-0 rounded-full transition-all duration-300 ${getProgressBarClass(
                            subscription.daily_usage_usd,
                            subscription.group.daily_limit_usd
                          )}`}
                          style={{
                            width: getProgressWidth(subscription.daily_usage_usd, subscription.group.daily_limit_usd)
                          }}
                        />
                      </div>
                      {subscription.daily_window_start && (
                        <p className="text-xs surface-muted">{formatDailyUsageWindow(subscription)}</p>
                      )}
                    </div>
                  )}

                  {subscription.group?.weekly_limit_usd && (
                    <div className="space-y-2">
                      <div className="flex items-center justify-between">
                        <span className="text-sm font-medium surface-muted">{t('userSubscriptions.weekly')}</span>
                        <span className="text-sm surface-muted">
                          ${(subscription.weekly_usage_usd || 0).toFixed(2)} / $
                          {subscription.group.weekly_limit_usd.toFixed(2)}
                        </span>
                      </div>
                      <div className="relative h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                        <div
                          className={`console-data-mark absolute inset-y-0 left-0 rounded-full transition-all duration-300 ${getProgressBarClass(
                            subscription.weekly_usage_usd,
                            subscription.group.weekly_limit_usd
                          )}`}
                          style={{
                            width: getProgressWidth(subscription.weekly_usage_usd, subscription.group.weekly_limit_usd)
                          }}
                        />
                      </div>
                      {subscription.weekly_window_start && (
                        <p className="text-xs surface-muted">
                          {t('userSubscriptions.resetIn', {
                            time: formatResetTime(subscription.weekly_window_start, 168)
                          })}
                        </p>
                      )}
                    </div>
                  )}

                  {subscription.group?.monthly_limit_usd && (
                    <div className="space-y-2">
                      <div className="flex items-center justify-between">
                        <span className="text-sm font-medium surface-muted">{t('userSubscriptions.monthly')}</span>
                        <span className="text-sm surface-muted">
                          ${(subscription.monthly_usage_usd || 0).toFixed(2)} / $
                          {subscription.group.monthly_limit_usd.toFixed(2)}
                        </span>
                      </div>
                      <div className="relative h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                        <div
                          className={`console-data-mark absolute inset-y-0 left-0 rounded-full transition-all duration-300 ${getProgressBarClass(
                            subscription.monthly_usage_usd,
                            subscription.group.monthly_limit_usd
                          )}`}
                          style={{
                            width: getProgressWidth(
                              subscription.monthly_usage_usd,
                              subscription.group.monthly_limit_usd
                            )
                          }}
                        />
                      </div>
                      {subscription.monthly_window_start && (
                        <p className="text-xs surface-muted">
                          {t('userSubscriptions.resetIn', {
                            time: formatResetTime(subscription.monthly_window_start, 720)
                          })}
                        </p>
                      )}
                    </div>
                  )}

                  {!subscription.group?.daily_limit_usd &&
                    !subscription.group?.weekly_limit_usd &&
                    !subscription.group?.monthly_limit_usd &&
                    !subscription.group?.five_hour_limit_usd && (
                      <div className="flex items-center justify-center rounded-xl bg-gradient-to-r from-emerald-50 to-teal-50 py-6 dark:from-emerald-900/20 dark:to-teal-900/20">
                        <div className="flex items-center gap-3">
                          <span className="text-4xl text-emerald-600 dark:text-emerald-400">∞</span>
                          <div>
                            <p className="text-sm font-medium text-emerald-700 dark:text-emerald-300">
                              {t('userSubscriptions.unlimited')}
                            </p>
                            <p className="text-xs text-emerald-600/70 dark:text-emerald-400/70">
                              {t('userSubscriptions.unlimitedDesc')}
                            </p>
                          </div>
                        </div>
                      </div>
                    )}
                </div>
              </div>
            ))}
          </div>
        )}
      </UserCardScroll>
      {!loading && subscriptions.length > 0 ? (
        <Pagination
          className="subscriptions-pagination workspace-fixed"
          total={subscriptions.length}
          page={page}
          pageSize={PAGE_SIZE}
          onPageChange={setPage}
        />
      ) : (
        <footer className="card workspace-fixed subscriptions-pagination p-5 text-sm surface-muted">
          {loading ? t('common.loading') : t('userSubscriptions.count', { count: 0 })}
        </footer>
      )}
    </div>
  )
}
