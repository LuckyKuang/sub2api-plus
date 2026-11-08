import { useTranslation } from 'react-i18next'
import { hslForPct } from '../useChannelMonitorFormat'

export interface MonitorAvailabilityRowProps {
  windowLabel: string
  value: number | null
  samplesLabel?: string
}

export function MonitorAvailabilityRow({ windowLabel, value, samplesLabel }: MonitorAvailabilityRowProps) {
  const { t } = useTranslation()

  const displayValue = value === null || Number.isNaN(value) ? t('monitorCommon.latencyEmpty') : value.toFixed(2)
  const colour = hslForPct(value)

  return (
    <>
      <div className="mt-3 flex items-end justify-between">
        <div className="text-[11px] uppercase tracking-widest surface-muted">{windowLabel}</div>
        <div className="flex items-baseline gap-0.5">
          <span className="text-3xl font-bold tabular-nums leading-none" style={{ color: colour ?? 'rgb(156 163 175)' }}>
            {displayValue}
          </span>
          <span className="text-base font-semibold leading-none" style={{ color: colour ?? 'rgb(156 163 175)' }}>
            %
          </span>
        </div>
      </div>
      {samplesLabel && <div className="mt-1 text-right text-[11px] surface-muted">{samplesLabel}</div>}
    </>
  )
}

export default MonitorAvailabilityRow
