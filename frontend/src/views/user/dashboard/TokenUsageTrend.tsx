import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { chartTooltipStyle } from '@/utils/chartTooltip'
import type { TrendDataPoint } from '@/types'
import { formatTokenShare, sumTokenBuckets } from '@/utils/tokenShare'
import { Icon } from '../icons'
import { useDocumentDarkMode } from '../useDocumentDarkMode'

export interface TokenUsageTrendProps {
  workspace?: boolean
  trendData: TrendDataPoint[]
  loading?: boolean
}

const SERIES = [
  { key: 'input_tokens', label: 'Input', color: '#3b82f6' },
  { key: 'output_tokens', label: 'Output', color: '#10b981' },
  { key: 'cache_creation_tokens', label: 'Cache Creation', color: '#f59e0b' },
  { key: 'cache_read_tokens', label: 'Cache Read', color: '#06b6d4' }
] as const

const formatTokens = (value: number): string => {
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(2)}K`
  return value.toLocaleString()
}

const formatCost = (value: number): string => {
  if (value >= 1000) return `${(value / 1000).toFixed(2)}K`
  if (value >= 1) return value.toFixed(2)
  if (value >= 0.01) return value.toFixed(3)
  return value.toFixed(4)
}

export function TokenUsageTrend({ workspace = false, trendData, loading = false }: TokenUsageTrendProps) {
  const { t } = useTranslation()
  const isDarkMode = useDocumentDarkMode()

  const chartColors = {
    text: isDarkMode ? '#e5e5e5' : '#525252',
    grid: isDarkMode ? '#404040' : '#e5e5e5'
  }

  const chartData = trendData?.length ? trendData.map((d) => ({ ...d })) : null

  const tooltipStyle = useMemo(() => {
    const base = chartTooltipStyle(isDarkMode)
    return {
      contentStyle: {
        backgroundColor: base.backgroundColor,
        border: `${base.borderWidth}px solid ${base.borderColor}`,
        borderRadius: base.cornerRadius,
        padding: base.padding,
        color: base.bodyColor
      },
      itemStyle: { color: base.bodyColor },
      labelStyle: { color: base.titleColor },
      footerStyle: { color: base.footerColor }
    }
  }, [isDarkMode])

  return (
    <div className={`card p-4 ${workspace ? 'chart-workspace' : ''}`}>
      <h3 className="mb-4 text-sm font-semibold text-gray-900 dark:text-white">{t('admin.dashboard.tokenUsageTrend')}</h3>
      {loading && !trendData.length ? (
        <div className="flex h-48 items-center justify-center">
          <span className="spinner h-6 w-6 text-primary-500" aria-hidden="true" />
        </div>
      ) : trendData.length > 0 && chartData ? (
        <div className="h-48">
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={chartData} margin={{ top: 5, right: 8, left: 0, bottom: 0 }}>
              <CartesianGrid stroke={chartColors.grid} strokeDasharray="3 3" vertical={false} />
              <XAxis
                dataKey="date"
                stroke={chartColors.grid}
                tick={{ fill: chartColors.text, fontSize: 10 }}
                tickMargin={8}
                minTickGap={24}
              />
              <YAxis
                stroke={chartColors.grid}
                tick={{ fill: chartColors.text, fontSize: 10 }}
                width={48}
                tickFormatter={(value: number) => formatTokens(value)}
              />
              <Tooltip
                {...tooltipStyle}
                content={({ active, payload }: any) => {
                  if (!active || !payload?.length) return null
                  const point = payload[0]?.payload as TrendDataPoint | undefined
                  if (!point) return null
                  const totalTokens = sumTokenBuckets(
                    point.input_tokens,
                    point.output_tokens,
                    point.cache_creation_tokens,
                    point.cache_read_tokens
                  )
                  return (
                    <div style={tooltipStyle.contentStyle as React.CSSProperties}>
                      <p style={tooltipStyle.labelStyle as React.CSSProperties}>{point.date}</p>
                      {payload.map((entry: any) => (
                        <p key={entry.dataKey} style={tooltipStyle.itemStyle as React.CSSProperties}>
                          {entry.name}: {formatTokens(Number(entry.value))} (
                          {formatTokenShare(Number(entry.value), totalTokens) ?? '0.0%'})
                        </p>
                      ))}
                      <p style={tooltipStyle.footerStyle as React.CSSProperties}>
                        Actual: ${formatCost(point.actual_cost)} | Standard: ${formatCost(point.cost)}
                      </p>
                    </div>
                  )
                }}
              />
              {SERIES.map((series) => (
                <Line
                  key={series.key}
                  type="monotone"
                  dataKey={series.key}
                  name={series.label}
                  stroke={series.color}
                  strokeWidth={2}
                  fill={series.color}
                  fillOpacity={0.12}
                  dot={false}
                  activeDot={{ r: 3 }}
                  isAnimationActive={false}
                />
              ))}
            </LineChart>
          </ResponsiveContainer>
        </div>
      ) : (
        <div className="flex h-48 flex-col items-center justify-center gap-2 surface-muted">
          <Icon name="chart" size="lg" className="opacity-60" />
          <span className="text-sm">{t('admin.dashboard.noDataAvailable')}</span>
        </div>
      )}
    </div>
  )
}

export default TokenUsageTrend
