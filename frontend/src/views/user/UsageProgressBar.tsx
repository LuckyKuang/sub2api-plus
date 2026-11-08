import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { WindowStats } from '@/types'
import { formatCompactNumber } from '@/utils/format'

export interface UsageProgressBarProps {
  label: string
  utilization: number // Percentage (0-100+)
  resetsAt?: string | null
  color: 'indigo' | 'emerald' | 'purple' | 'amber'
  windowStats?: WindowStats | null
  estimatedTotalCost?: number | null
  showNowWhenIdle?: boolean
  remainingCapacity?: boolean
  /** fixed: 定宽居中徽章（账号页纵向对齐）；auto: 限宽截断左对齐（监控页组合标签） */
  labelWidth?: 'fixed' | 'auto'
  dataTestId?: string
  title?: string
}

export function UsageProgressBar({
  label,
  utilization,
  resetsAt = null,
  color,
  windowStats = null,
  estimatedTotalCost = null,
  showNowWhenIdle = false,
  remainingCapacity = false,
  labelWidth = 'fixed',
  dataTestId,
  title
}: UsageProgressBarProps) {
  const { t } = useTranslation()
  const [now, setNow] = useState(() => new Date())

  // Reactive clock for countdown — only runs when a reset time is shown,
  // to avoid creating many idle timers across large account lists.
  useEffect(() => {
    if (!resetsAt) return
    const timer = setInterval(() => setNow(new Date()), 60_000)
    return () => clearInterval(timer)
  }, [resetsAt])

  useEffect(() => {
    if (!resetsAt) return
    const onVisibilityChange = () => {
      if (!document.hidden) setNow(new Date())
    }
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () => document.removeEventListener('visibilitychange', onVisibilityChange)
  }, [resetsAt])

  const labelClass = {
    indigo: 'bg-indigo-100/[0.15] text-indigo-700 dark:bg-indigo-900/40 dark:text-indigo-300',
    emerald: 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300',
    purple: 'bg-purple-100/[0.15] text-purple-700 dark:bg-purple-900/40 dark:text-purple-300',
    amber: 'bg-amber-100/[0.15] text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
  }[color]

  // Label badge width mode: fixed 定宽保证账号页纵向对齐；auto 限宽截断适配
  // 监控页「Pro/7 天」类组合标签。百分比列在两种模式下保持不变。
  const labelSizeClass =
    labelWidth === 'auto'
      ? 'max-w-[72px] shrink-0 truncate rounded px-1 text-left text-[10px] font-medium'
      : 'w-[32px] shrink-0 rounded px-1 text-center text-[10px] font-medium'

  const barClass = (() => {
    if (remainingCapacity) {
      if (utilization <= 20) return 'bg-red-500'
      if (utilization <= 50) return 'bg-amber-500'
      return 'bg-green-500'
    }
    if (utilization >= 90) return 'bg-red-500'
    if (utilization >= 75) return 'bg-amber-500'
    return 'bg-green-500'
  })()

  const textClass = (() => {
    if (remainingCapacity) {
      if (utilization <= 20) return 'text-red-600 dark:text-red-400'
      if (utilization <= 50) return 'text-amber-600 dark:text-amber-400'
      return 'surface-muted'
    }
    if (utilization >= 90) return 'text-red-600 dark:text-red-400'
    if (utilization >= 75) return 'text-amber-600 dark:text-amber-400'
    return 'surface-muted'
  })()

  const barWidth = `${Math.min(Math.max(utilization, 0), 100)}%`

  const displayPercent = (() => {
    const percent = Math.round(remainingCapacity ? Math.min(Math.max(utilization, 0), 100) : utilization)
    return percent > 999 ? '>999%' : `${percent}%`
  })()

  const shouldShowResetTime = resetsAt ? true : Boolean(showNowWhenIdle && utilization <= 0)

  const formatResetTime = (() => {
    if (!resetsAt) {
      // Idle rolling window without a known reset time: treat as immediately available.
      return showNowWhenIdle && utilization <= 0 ? t('usage.resetNow') : '-'
    }

    const date = new Date(resetsAt)
    const diffMs = date.getTime() - now.getTime()

    // resetsAt 已过期：utilization>0 说明后端窗口数据还没刷新（active poll 没回写），
    // 显示「待刷新」以区别于真正可用的「现在」。
    if (diffMs <= 0) {
      return utilization > 0 ? t('usage.resetPending') : t('usage.resetNow')
    }

    const diffHours = Math.floor(diffMs / (1000 * 60 * 60))
    const diffMins = Math.floor((diffMs % (1000 * 60 * 60)) / (1000 * 60))

    if (diffHours >= 24) {
      const days = Math.floor(diffHours / 24)
      return `${days}d ${diffHours % 24}h`
    }
    if (diffHours > 0) {
      return `${diffHours}h ${diffMins}m`
    }
    return `${diffMins}m`
  })()

  return (
    <div data-testid={dataTestId} title={title}>
      {windowStats && (windowStats.requests > 0 || windowStats.tokens > 0) && (
        <div className="mb-0.5 flex items-center">
          <div className="flex items-center gap-1.5 text-[9px] surface-muted">
            <span className="rounded surface-inset px-1.5 py-0.5">
              {formatCompactNumber(windowStats.requests, { allowBillions: false })} req
            </span>
            <span className="rounded surface-inset px-1.5 py-0.5">{formatCompactNumber(windowStats.tokens)}</span>
            <span className="rounded surface-inset px-1.5 py-0.5" title={t('usage.accountBilled')}>
              A ${windowStats.cost.toFixed(2)}
            </span>
            {windowStats.user_cost != null && (
              <span className="rounded surface-inset px-1.5 py-0.5" title={t('usage.userBilled')}>
                U ${windowStats.user_cost.toFixed(2)}
              </span>
            )}
            {estimatedTotalCost != null && (
              <span
                data-test="estimated-total-cost"
                className="rounded surface-inset px-1.5 py-0.5"
                title={t('admin.accounts.usageWindow.estimatedTotalCostTooltip')}
              >
                {t('admin.accounts.usageWindow.estimatedTotalCost', { cost: estimatedTotalCost.toFixed(2) })}
              </span>
            )}
          </div>
        </div>
      )}

      <div className="flex items-center gap-1">
        <span className={[labelSizeClass, labelClass].join(' ')}>{label}</span>

        <div className="h-1.5 w-8 shrink-0 overflow-hidden rounded-full bg-gray-200 dark:bg-gray-700">
          <div className={`console-data-mark h-full transition-all duration-300 ${barClass}`} style={{ width: barWidth }} />
        </div>

        <span className={`w-[32px] shrink-0 text-right text-[10px] font-medium ${textClass}`}>{displayPercent}</span>

        {shouldShowResetTime && <span className="shrink-0 text-[10px] surface-muted">{formatResetTime}</span>}
      </div>
    </div>
  )
}

export default UsageProgressBar
