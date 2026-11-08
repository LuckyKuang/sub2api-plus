export interface MonitorMetricPairProps {
  primaryLabel: string
  primaryValue: string
  primaryUnit: string
  primaryIcon: 'bolt' | 'globe' | 'clock' | 'link'
  secondaryLabel: string
  secondaryValue: string
  secondaryUnit: string
  secondaryIcon: 'bolt' | 'globe' | 'clock' | 'link'
}

export function MonitorMetricPair({
  primaryLabel,
  primaryValue,
  primaryUnit,
  primaryIcon,
  secondaryLabel,
  secondaryValue,
  secondaryUnit,
  secondaryIcon
}: MonitorMetricPairProps) {
  return (
    <div className="mt-5 grid grid-cols-2 gap-2">
      {[
        { label: primaryLabel, value: primaryValue, unit: primaryUnit, icon: primaryIcon },
        { label: secondaryLabel, value: secondaryValue, unit: secondaryUnit, icon: secondaryIcon }
      ].map((metric) => (
        <div
          key={metric.label}
          className="rounded-xl border border-gray-100 p-3 surface-inset dark:border-dark-700/50"
        >
          <div className="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider surface-muted">
            <Icon name={metric.icon} size="xs" />
            <span>{metric.label}</span>
          </div>
          <div className="mt-1.5 font-mono text-lg font-bold tabular-nums text-gray-900 dark:text-gray-100">
            {metric.value}
            <span className="ml-0.5 text-xs font-normal surface-muted">{metric.unit}</span>
          </div>
        </div>
      ))}
    </div>
  )
}

import { Icon } from '../icons'
