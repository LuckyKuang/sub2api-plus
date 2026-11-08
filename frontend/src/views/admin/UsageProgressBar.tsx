import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { formatCompactNumber } from '@/utils/format'
import type { WindowStats } from '@/types'

export interface UsageProgressBarProps {
  label: string
  /** Percentage (0-100+) */
  utilization: number
  resetsAt?: string | null
  color: 'indigo' | 'emerald' | 'purple' | 'amber'
  windowStats?: WindowStats | null
  estimatedTotalCost?: number | null
  showNowWhenIdle?: boolean
  remainingCapacity?: boolean
  /** fixed: 定宽居中徽章（账号页纵向对齐）；auto: 限宽截断左对齐（监控页组合标签） */
  labelWidth?: 'fixed' | 'auto'
  title?: string
}

const LABEL_COLORS: Record<UsageProgressBarProps['color'], string> = {
  indigo: 'bg-indigo-100/[0.15] text-indigo-700 dark:bg-indigo-900/40 dark:text-indigo-300',
  emerald: 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300',
  purple: 'bg-purple-100/[0.15] text-purple-700 dark:bg-purple-900/40 dark:text-purple-300',
  amber: 'bg-amber-100/[0.15] text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
}

export function UsageProgressBar({
  label,
  utilization,
  resetsAt,
  color,
  windowStats,
  estimatedTotalCost,
  showNowWhenIdle,
  remainingCapacity,
  labelWidth = 'fixed',
  title
}: UsageProgressBarProps) {
  const { t } = useTranslation()
  const [now, setNow] = useState(() => new Date())
  const nowRef = useRef(now)
  nowRef.current = now

  // Reactive clock for countdown — only runs when a reset time is shown,
  // to avoid creating many idle timers across large account lists.
  useEffect(() => {
    if (!resetsAt) return
    setNow(new Date())
    const timer = setInterval(() => setNow(new Date()), 60_000)
    return () => clearInterval(timer)
  }, [resetsAt])

  useEffect(() => {
    if (!resetsAt) return
    const refreshClockWhenVisible = () => {
      if (!document.hidden && resetsAt) setNow(new Date())
    }
    document.addEventListener('visibilitychange', refreshClockWhenVisible)
    return () => document.removeEventListener('visibilitychange', refreshClockWhenVisible)
  }, [resetsAt])

  const labelClass = LABEL_COLORS[color]

  // Label badge width mode: fixed 定宽保证账号页纵向对齐；auto 限宽截断适配
  // 监控页「Pro/7 天」类组合标签。百分比列在两种模式下保持不变。
  const labelSizeClass =
    labelWidth === 'auto'
      ? 'max-w-[72px] shrink-0 truncate rounded px-1 text-left text-[10px] font-medium'
      : 'w-[32px] shrink-0 rounded px-1 text-center text-[10px] font-medium'

  // Progress bar color based on utilization
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

  // Text color based on utilization
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

  // Bar width (capped at 100%)
  const barWidth = `${Math.min(Math.max(utilization, 0), 100)}%`

  // Display percentage (cap at 999% for readability)
  const displayPercent = (() => {
    const percent = Math.round(remainingCapacity ? Math.min(Math.max(utilization, 0), 100) : utilization)
    return percent > 999 ? '>999%' : `${percent}%`
  })()

  const shouldShowResetTime = resetsAt ? true : Boolean(showNowWhenIdle && utilization <= 0)

  // Format reset time
  const formatResetTime = useMemo(() => {
    if (!resetsAt) {
      // Idle rolling window without a known reset time: treat as immediately available.
      return showNowWhenIdle && utilization <= 0 ? t('usage.resetNow') : '-'
    }

    const date = new Date(resetsAt)
    const diffMs = date.getTime() - nowRef.current.getTime()

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
    } else if (diffHours > 0) {
      return `${diffHours}h ${diffMins}m`
    } else {
      return `${diffMins}m`
    }
  }, [resetsAt, utilization, showNowWhenIdle, now, t])

  // Window stats formatters
  const formatRequests = windowStats ? formatCompactNumber(windowStats.requests, { allowBillions: false }) : ''
  const formatTokens = windowStats ? formatCompactNumber(windowStats.tokens) : ''
  const formatAccountCost = windowStats ? windowStats.cost.toFixed(2) : '0.00'
  const formatUserCost = windowStats && windowStats.user_cost != null ? windowStats.user_cost.toFixed(2) : '0.00'

  return (
    <div title={title}>
      {/* Window stats row (above progress bar) */}
      {windowStats && (windowStats.requests > 0 || windowStats.tokens > 0) && (
        <div className="mb-0.5 flex items-center">
          <div className="flex items-center gap-1.5 text-[9px] surface-muted">
            <span className="rounded surface-inset px-1.5 py-0.5">{formatRequests} req</span>
            <span className="rounded surface-inset px-1.5 py-0.5">{formatTokens}</span>
            <span className="rounded surface-inset px-1.5 py-0.5" title={t('usage.accountBilled')}>
              A ${formatAccountCost}
            </span>
            {windowStats.user_cost != null && (
              <span className="rounded surface-inset px-1.5 py-0.5" title={t('usage.userBilled')}>
                U ${formatUserCost}
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

      {/* Progress bar row */}
      <div className="flex items-center gap-1">
        <span className={cn(labelSizeClass, labelClass)}>{label}</span>

        <div className="h-1.5 w-8 shrink-0 overflow-hidden rounded-full bg-gray-200 dark:bg-gray-700">
          <div className={cn('console-data-mark h-full transition-all duration-300', barClass)} style={{ width: barWidth }} />
        </div>

        <span className={cn('w-[32px] shrink-0 text-right text-[10px] font-medium', textClass)}>{displayPercent}</span>

        {shouldShowResetTime && <span className="shrink-0 text-[10px] surface-muted">{formatResetTime}</span>}
      </div>
    </div>
  )
}

export default UsageProgressBar
