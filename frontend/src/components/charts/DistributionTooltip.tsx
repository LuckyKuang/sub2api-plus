import type { TooltipContentProps } from 'recharts'
import { chartTooltipStyle } from '@/utils/chartTooltip'

export type DistributionMetric = 'tokens' | 'actual_cost'

function formatTokens(value: number): string {
  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`
  } else if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`
  } else if (value >= 1_000) {
    return `${(value / 1_000).toFixed(2)}K`
  }
  return value.toLocaleString()
}

function formatCost(value: number): string {
  if (value >= 1000) {
    return (value / 1000).toFixed(2) + 'K'
  } else if (value >= 1) {
    return value.toFixed(2)
  } else if (value >= 0.01) {
    return value.toFixed(3)
  }
  return value.toFixed(4)
}

export interface DistributionTooltipProps extends TooltipContentProps {
  metric: DistributionMetric
  isDarkMode: boolean
  /** Total across every slice, so the hovered slice shows its share. */
  total: number
}

/**
 * Mirrors the chart.js doughnut tooltip callback:
 * `${label}: ${formattedValue} (${percentage}%)`
 */
export function DistributionTooltip({
  active,
  payload,
  metric,
  isDarkMode,
  total
}: DistributionTooltipProps) {
  if (!active || !payload || payload.length === 0) return null
  const entry = payload[0]
  const value = Number(entry.value ?? 0)
  const percentage = total > 0 ? ((value / total) * 100).toFixed(1) : '0.0'
  const formattedValue = metric === 'actual_cost' ? `$${formatCost(value)}` : formatTokens(value)
  const style = chartTooltipStyle(isDarkMode)

  return (
    <div
      style={{
        backgroundColor: style.backgroundColor,
        border: `${style.borderWidth}px solid ${style.borderColor}`,
        borderRadius: style.cornerRadius,
        padding: style.padding
      }}
    >
      <p style={{ color: style.bodyColor, fontSize: 12 }}>
        {entry.name}: {formattedValue} ({percentage}%)
      </p>
    </div>
  )
}
