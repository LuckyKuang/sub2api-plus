import { useTranslation } from 'react-i18next'
import type { UserMonitorDetail, UserMonitorView } from '@/api/channelMonitor'
import { EmptyState } from '@/components/ui'
import { MonitorCard } from './MonitorCard'

export interface MonitorCardGridProps {
  items: UserMonitorView[]
  window: '7d' | '15d' | '30d'
  countdownSeconds: number
  loading: boolean
  detailCache: Record<number, UserMonitorDetail>
  onCardClick?: (item: UserMonitorView) => void
}

export function MonitorCardGrid({
  items,
  window,
  countdownSeconds,
  loading,
  detailCache,
  onCardClick
}: MonitorCardGridProps) {
  const { t } = useTranslation()

  function resolveAvailability(item: UserMonitorView): number | null {
    if (window === '7d') {
      return item.availability_7d ?? null
    }
    const detail = detailCache[item.id]
    if (!detail) return null
    const primary = detail.models.find((m) => m.model === item.primary_model)
    if (!primary) return null
    return window === '15d' ? primary.availability_15d ?? null : primary.availability_30d ?? null
  }

  if (loading && items.length === 0) {
    return (
      <div className="grid gap-5 grid-cols-1 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
        {Array.from({ length: 6 }, (_, i) => (
          <div key={i} className="glass-surface min-h-[280px] animate-pulse p-5">
            <div className="flex items-start gap-3">
              <div className="h-9 w-9 rounded-xl surface-inset" />
              <div className="flex-1 space-y-2">
                <div className="h-4 w-2/3 rounded bg-gray-200 dark:bg-dark-700" />
                <div className="h-3 w-1/2 rounded bg-gray-200 dark:bg-dark-700" />
              </div>
              <div className="h-6 w-16 rounded-full surface-inset" />
            </div>
            <div className="mt-5 grid grid-cols-2 gap-2">
              <div className="h-16 rounded-xl surface-inset" />
              <div className="h-16 rounded-xl surface-inset" />
            </div>
            <div className="mt-6 h-5 w-full rounded surface-inset" />
          </div>
        ))}
      </div>
    )
  }

  if (items.length === 0) {
    return (
      <EmptyState title={t('channelStatus.empty.title')} description={t('channelStatus.empty.description')} />
    )
  }

  return (
    <div className="grid gap-5 grid-cols-1 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
      {items.map((item) => (
        <MonitorCard
          key={item.id}
          item={item}
          window={window}
          availabilityValue={resolveAvailability(item)}
          countdownSeconds={countdownSeconds}
          onClick={() => onCardClick?.(item)}
        />
      ))}
    </div>
  )
}

export default MonitorCardGrid
