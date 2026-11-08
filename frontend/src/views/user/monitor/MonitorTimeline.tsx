import { useTranslation } from 'react-i18next'
import type { MonitorTimelinePoint } from '@/api/channelMonitor'
import { useChannelMonitorFormat } from '../useChannelMonitorFormat'

export interface MonitorTimelineProps {
  buckets?: MonitorTimelinePoint[]
  countdownSeconds: number
  length?: number
  maintenance?: boolean
}

interface Bar {
  colorClass: string
  heightPct: number
  title: string
}

// 4 级高度 + 颜色双重编码：高=好+绿，短=坏+红，灰=未测试。
// 长绿(正常) > 中黄(降级) > 短红(失败/系统错误) > 很短灰(未测试)。
const STATUS_HEIGHT: Record<string, number> = {
  operational: 100,
  degraded: 65,
  failed: 35,
  error: 35,
  empty: 15
}

const STATUS_COLOR: Record<string, string> = {
  operational: 'bg-emerald-500',
  degraded: 'bg-amber-500',
  failed: 'bg-red-500',
  error: 'bg-red-500',
  empty: 'bg-gray-300 dark:surface-inset'
}

export function MonitorTimeline({
  buckets = [],
  countdownSeconds,
  length = 60,
  maintenance = false
}: MonitorTimelineProps) {
  const { t } = useTranslation()
  const { statusLabel, formatLatency, formatRelativeTime } = useChannelMonitorFormat()

  const displayBars: Bar[] = (() => {
    // Real points come newest-first; convert to oldest-first so the rightmost
    // bar represents "now". Pad the left with empty placeholders to keep the
    // bar count stable at `length`.
    const real = [...buckets].slice(0, length).reverse()

    const padCount = Math.max(0, length - real.length)
    const bars: Bar[] = []

    for (let i = 0; i < padCount; i += 1) {
      bars.push({
        colorClass: STATUS_COLOR.empty,
        heightPct: STATUS_HEIGHT.empty,
        title: ''
      })
    }

    for (const point of real) {
      const status = point.status as keyof typeof STATUS_HEIGHT
      const colorClass = STATUS_COLOR[status] ?? STATUS_COLOR.empty
      const heightPct = STATUS_HEIGHT[status] ?? STATUS_HEIGHT.empty
      const latency = formatLatency(point.latency_ms)
      const relative = formatRelativeTime(point.checked_at)
      const label = statusLabel(point.status)
      bars.push({
        colorClass,
        heightPct,
        title: `${relative} · ${label} · ${t('monitorCommon.firstText')} ${formatLatency(point.first_text_ms)}ms · ${t('monitorCommon.firstByte')} ${formatLatency(point.first_byte_ms)}ms · ${t('usage.latencyDuration')} ${latency}ms`
      })
    }

    return bars
  })()

  return (
    <div className="mt-4 border-t border-gray-100 pt-3 dark:border-dark-700/60">
      <div className="mb-2 flex justify-between text-[10px] font-semibold uppercase tracking-widest surface-muted">
        <span>{t('monitorCommon.history60pts', { n: length })}</span>
        <span className="tabular-nums">{t('monitorCommon.nextUpdateIn', { n: countdownSeconds })}</span>
      </div>

      {maintenance ? (
        <div className="flex h-5 w-full items-center justify-center rounded border border-dashed border-gray-300 text-[10px] uppercase tracking-widest surface-muted dark:border-dark-600">
          {t('monitorCommon.maintenancePaused')}
        </div>
      ) : (
        <div className="flex h-5 w-full items-end gap-[2px]">
          {displayBars.map((bar, index) => (
            <div
              key={index}
              className={`min-w-0 flex-1 rounded-sm ${bar.colorClass}`}
              style={{ height: `${bar.heightPct}%` }}
              title={bar.title}
            />
          ))}
        </div>
      )}

      <div className="mt-1 flex justify-between text-[9px] uppercase tracking-widest surface-muted">
        <span>{t('monitorCommon.past')}</span>
        <span>{t('monitorCommon.now')}</span>
      </div>
    </div>
  )
}

export default MonitorTimeline
