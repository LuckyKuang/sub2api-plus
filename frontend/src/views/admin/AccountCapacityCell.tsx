import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { supportsAccountQuota } from '@/utils/accountQuota'
import type { Account } from '@/types'
import { CapacityBadge, QuotaBadge } from './AccountBadges'

export interface AccountCapacityCellProps {
  account: Account
}

export function AccountCapacityCell({ account }: AccountCapacityCellProps) {
  const { t } = useTranslation()

  // ====== 并发 ======
  const currentConcurrency = account.current_concurrency || 0

  const concurrencyClass = useMemo(() => {
    const current = currentConcurrency
    const max = account.concurrency
    if (current >= max) return 'bg-red-100/[0.15] text-red-700 dark:bg-red-900/30 dark:text-red-400'
    if (current > 0) return 'bg-yellow-100/[0.15] text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
    return 'surface-inset surface-muted dark:surface-inset'
  }, [currentConcurrency, account.concurrency])

  // ====== 窗口费用 ======
  const isAnthropicOAuthOrSetupToken =
    account.platform === 'anthropic' && (account.type === 'oauth' || account.type === 'setup-token')

  const showWindowCost =
    isAnthropicOAuthOrSetupToken && account.window_cost_limit != null && account.window_cost_limit > 0

  const currentWindowCost = account.current_window_cost ?? 0

  const windowCostClass = useMemo(() => {
    if (!showWindowCost) return ''
    const current = currentWindowCost
    const limit = account.window_cost_limit || 0
    const reserve = account.window_cost_sticky_reserve || 10
    if (current >= limit + reserve) return 'bg-red-100/[0.15] text-red-700 dark:bg-red-900/30 dark:text-red-400'
    if (current >= limit) return 'bg-orange-100/[0.15] text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
    if (current >= limit * 0.8) return 'bg-yellow-100/[0.15] text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
    return 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
  }, [showWindowCost, currentWindowCost, account.window_cost_limit, account.window_cost_sticky_reserve])

  const windowCostTooltip = useMemo(() => {
    if (!showWindowCost) return ''
    const current = currentWindowCost
    const limit = account.window_cost_limit || 0
    const reserve = account.window_cost_sticky_reserve || 10
    if (current >= limit + reserve) return t('admin.accounts.capacity.windowCost.blocked')
    if (current >= limit) return t('admin.accounts.capacity.windowCost.stickyOnly')
    return t('admin.accounts.capacity.windowCost.normal')
  }, [showWindowCost, currentWindowCost, account.window_cost_limit, account.window_cost_sticky_reserve, t])

  // ====== 会话限制 ======
  const showSessionLimit =
    isAnthropicOAuthOrSetupToken && account.max_sessions != null && account.max_sessions > 0

  const activeSessions = account.active_sessions ?? 0

  const sessionLimitClass = useMemo(() => {
    if (!showSessionLimit) return ''
    const current = activeSessions
    const max = account.max_sessions || 0
    if (current >= max) return 'bg-red-100/[0.15] text-red-700 dark:bg-red-900/30 dark:text-red-400'
    if (current >= max * 0.8) return 'bg-yellow-100/[0.15] text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
    return 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
  }, [showSessionLimit, activeSessions, account.max_sessions])

  const sessionLimitTooltip = useMemo(() => {
    if (!showSessionLimit) return ''
    const current = activeSessions
    const max = account.max_sessions || 0
    const idle = account.session_idle_timeout_minutes || 5
    if (current >= max) return t('admin.accounts.capacity.sessions.full', { idle })
    return t('admin.accounts.capacity.sessions.normal', { idle })
  }, [showSessionLimit, activeSessions, account.max_sessions, account.session_idle_timeout_minutes, t])

  // ====== RPM ======
  const showRpmLimit = isAnthropicOAuthOrSetupToken && account.base_rpm != null && account.base_rpm > 0

  const currentRPM = account.current_rpm ?? 0
  const rpmStrategy = account.rpm_strategy || 'tiered'
  const rpmStrategyTag = rpmStrategy === 'sticky_exempt' ? '[S]' : '[T]'

  const rpmBuffer =
    account.rpm_sticky_buffer ??
    (account.base_rpm ? Math.max(1, Math.floor(account.base_rpm / 5)) : 0)

  const rpmClass = useMemo(() => {
    if (!showRpmLimit) return ''
    const current = currentRPM
    const base = account.base_rpm ?? 0
    const buffer = rpmBuffer
    if (rpmStrategy === 'tiered') {
      if (current >= base + buffer) return 'bg-red-100/[0.15] text-red-700 dark:bg-red-900/30 dark:text-red-400'
      if (current >= base) return 'bg-orange-100/[0.15] text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
    } else {
      if (current >= base) return 'bg-orange-100/[0.15] text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
    }
    if (current >= base * 0.8) return 'bg-yellow-100/[0.15] text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
    return 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
  }, [showRpmLimit, currentRPM, account.base_rpm, rpmBuffer, rpmStrategy])

  const rpmTooltip = useMemo(() => {
    if (!showRpmLimit) return ''
    const current = currentRPM
    const base = account.base_rpm ?? 0
    const buffer = rpmBuffer
    if (rpmStrategy === 'tiered') {
      if (current >= base + buffer) return t('admin.accounts.capacity.rpm.tieredBlocked', { buffer })
      if (current >= base) return t('admin.accounts.capacity.rpm.tieredStickyOnly', { buffer })
      if (current >= base * 0.8) return t('admin.accounts.capacity.rpm.tieredWarning')
      return t('admin.accounts.capacity.rpm.tieredNormal')
    } else {
      if (current >= base) return t('admin.accounts.capacity.rpm.stickyExemptOver')
      if (current >= base * 0.8) return t('admin.accounts.capacity.rpm.stickyExemptWarning')
      return t('admin.accounts.capacity.rpm.stickyExemptNormal')
    }
  }, [showRpmLimit, currentRPM, account.base_rpm, rpmBuffer, rpmStrategy, t])

  // 格式化费用显示
  const formatCost = (value: number | null | undefined) => {
    if (value === null || value === undefined) return '0'
    return value.toFixed(2)
  }

  // ====== 配额 ======
  const isQuotaEligible = supportsAccountQuota(account.type)

  const showDailyQuota = isQuotaEligible && account.quota_daily_limit != null && account.quota_daily_limit > 0
  const showWeeklyQuota = isQuotaEligible && account.quota_weekly_limit != null && account.quota_weekly_limit > 0
  const showTotalQuota = isQuotaEligible && account.quota_limit != null && account.quota_limit > 0

  return (
    <div className="flex flex-col gap-0.5">
      {/* 并发槽位 */}
      <CapacityBadge colorClass={concurrencyClass} current={currentConcurrency} max={account.concurrency}>
        <svg className="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
          <path strokeLinecap="round" strokeLinejoin="round" d="M3.75 6A2.25 2.25 0 016 3.75h2.25A2.25 2.25 0 0110.5 6v2.25a2.25 2.25 0 01-2.25 2.25H6a2.25 2.25 0 01-2.25-2.25V6zM3.75 15.75A2.25 2.25 0 016 13.5h2.25a2.25 2.25 0 012.25 2.25V18a2.25 2.25 0 01-2.25 2.25H6A2.25 2.25 0 013.75 18v-2.25zM13.5 6a2.25 2.25 0 012.25-2.25H18A2.25 2.25 0 0120.25 6v2.25a2.25 2.25 0 01-2.25 2.25h-2.25A2.25 2.25 0 0113.5 8.25V6zM13.5 15.75a2.25 2.25 0 012.25-2.25H18a2.25 2.25 0 012.25 2.25V18A2.25 2.25 0 0118 20.25h-2.25A2.25 2.25 0 0113.5 18v-2.25z" />
        </svg>
      </CapacityBadge>

      {/* 5h窗口费用限制 */}
      {showWindowCost && (
        <CapacityBadge
          colorClass={windowCostClass}
          tooltip={windowCostTooltip}
          current={'$' + formatCost(currentWindowCost)}
          max={'$' + formatCost(account.window_cost_limit)}
        >
          <svg className="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
            <path strokeLinecap="round" strokeLinejoin="round" d="M12 6v12m-3-2.818l.879.659c1.171.879 3.07.879 4.242 0 1.172-.879 1.172-2.303 0-3.182C13.536 12.219 12.768 12 12 12c-.725 0-1.45-.22-2.003-.659-1.106-.879-1.106-2.303 0-3.182s2.9-.879 4.006 0l.415.33M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
        </CapacityBadge>
      )}

      {/* 会话数量限制 */}
      {showSessionLimit && (
        <CapacityBadge
          colorClass={sessionLimitClass}
          tooltip={sessionLimitTooltip}
          current={activeSessions}
          max={account.max_sessions!}
        >
          <svg className="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
            <path strokeLinecap="round" strokeLinejoin="round" d="M15 19.128a9.38 9.38 0 002.625.372 9.337 9.337 0 004.121-.952 4.125 4.125 0 00-7.533-2.493M15 19.128v-.003c0-1.113-.285-2.16-.786-3.07M15 19.128v.106A12.318 12.318 0 018.624 21c-2.331 0-4.512-.645-6.374-1.766l-.001-.109a6.375 6.375 0 0111.964-3.07M12 6.375a3.375 3.375 0 11-6.75 0 3.375 3.375 0 016.75 0zm8.25 2.25a2.625 2.625 0 11-5.25 0 2.625 2.625 0 015.25 0z" />
          </svg>
        </CapacityBadge>
      )}

      {/* RPM 限制 */}
      {showRpmLimit && (
        <CapacityBadge
          colorClass={rpmClass}
          tooltip={rpmTooltip}
          current={currentRPM}
          max={account.base_rpm!}
          suffix={rpmStrategyTag}
        >
          <svg className="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="1.5">
            <path strokeLinecap="round" strokeLinejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" />
          </svg>
        </CapacityBadge>
      )}

      {/* API Key 账号配额限制 */}
      {showDailyQuota && (
        <QuotaBadge used={account.quota_daily_used ?? 0} limit={account.quota_daily_limit!} label="D" />
      )}
      {showWeeklyQuota && (
        <QuotaBadge used={account.quota_weekly_used ?? 0} limit={account.quota_weekly_limit!} label="W" />
      )}
      {showTotalQuota && <QuotaBadge used={account.quota_used ?? 0} limit={account.quota_limit!} />}
    </div>
  )
}

export default AccountCapacityCell
