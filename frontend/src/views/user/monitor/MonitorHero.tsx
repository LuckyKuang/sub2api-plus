import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Icon } from '../icons'
import type { AutoRefreshState } from '../useAutoRefresh'
import { AutoRefreshButton, type MonitorWindow, type OverallStatus } from './AutoRefreshButton'

export type { MonitorWindow, OverallStatus }

export interface MonitorHeroProps {
  overallStatus: OverallStatus
  intervalSeconds: number
  window: MonitorWindow
  loading: boolean
  autoRefresh?: AutoRefreshState
  className?: string
  onWindowChange?: (value: MonitorWindow) => void
  onRefresh?: () => void
}

export function MonitorHero({
  overallStatus,
  window,
  loading,
  autoRefresh,
  className,
  onWindowChange,
  onRefresh
}: MonitorHeroProps) {
  const { t } = useTranslation()

  const windowOptions: Array<{ value: MonitorWindow; label: string }> = [
    { value: '7d', label: t('channelStatus.windowTab.7d') },
    { value: '15d', label: t('channelStatus.windowTab.15d') },
    { value: '30d', label: t('channelStatus.windowTab.30d') }
  ]

  const overallLabel = t(`channelStatus.overall.${overallStatus}`)

  const overallChipClass =
    overallStatus === 'operational'
      ? 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-500/[0.15] dark:text-emerald-300'
      : 'bg-amber-100/[0.15] text-amber-700 dark:bg-amber-500/[0.15] dark:text-amber-300'

  const overallDotClass =
    overallStatus === 'operational' ? 'bg-emerald-500 animate-pulse' : 'bg-amber-500 animate-pulse'

  return (
    <section className={cn('py-3 md:py-4', className)}>
      <div className="flex flex-wrap items-center justify-end gap-3">
        <div
          role="tablist"
          className="inline-flex rounded-xl border border-gray-200/60 p-0.5 text-xs surface-inset dark:border-dark-700/60"
        >
          {windowOptions.map((opt) => (
            <button
              key={opt.value}
              type="button"
              role="tab"
              aria-selected={window === opt.value}
              className={`rounded-lg px-3 py-1 transition-colors surface-control ${
                window === opt.value
                  ? 'surface-field dark:surface-field shadow-sm font-semibold text-gray-900 dark:text-white'
                  : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'
              }`}
              onClick={() => onWindowChange?.(opt.value)}
            >
              {opt.label}
            </button>
          ))}
        </div>

        <span
          className={`inline-flex items-center rounded-full px-2.5 py-1 text-xs font-semibold uppercase tracking-wider ${overallChipClass}`}
        >
          <span className={`console-data-mark mr-1.5 h-1.5 w-1.5 rounded-full ${overallDotClass}`} />
          {overallLabel}
        </span>

        <button
          type="button"
          className="surface-control surface-muted flex h-8 w-8 items-center justify-center rounded-lg transition-colors hover:text-gray-700 hover:surface-hover disabled:opacity-50 dark:hover:text-gray-200"
          disabled={loading}
          title={t('common.refresh')}
          onClick={onRefresh}
        >
          <Icon name="refresh" size="md" className={loading ? 'animate-spin' : ''} />
        </button>

        {autoRefresh && (
          <AutoRefreshButton
            enabled={autoRefresh.enabled}
            intervalSeconds={autoRefresh.intervalSeconds}
            countdown={autoRefresh.countdown}
            intervals={autoRefresh.intervals}
            onEnabledChange={autoRefresh.setEnabled}
            onIntervalChange={autoRefresh.setInterval}
          />
        )}
      </div>
    </section>
  )
}

export default MonitorHero
