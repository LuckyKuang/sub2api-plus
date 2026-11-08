import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Area,
  AreaChart,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
  type TooltipContentProps
} from 'recharts'
import { chartTooltipStyle } from '@/utils/chartTooltip'
import { useDocumentDarkMode } from '@/composables/useDocumentDarkMode'
import { cn } from '@/lib/utils'
import type { TrendDataPoint } from '@/types'
import { formatTokenShare, sumTokenBuckets } from '@/utils/tokenShare'
import { Icon } from '../Icon'
import { Spinner } from '../ui'

export interface TokenUsageTrendProps {
  workspace?: boolean
  trendData: TrendDataPoint[]
  loading?: boolean
  className?: string
}

const SERIES = [
  { key: 'Input', color: '#3b82f6' },
  { key: 'Output', color: '#10b981' },
  { key: 'Cache Creation', color: '#f59e0b' },
  { key: 'Cache Read', color: '#06b6d4' }
] as const

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

interface TrendTooltipProps extends TooltipContentProps {
  isDarkMode: boolean
}

function TrendTooltip({ active, payload, isDarkMode }: TrendTooltipProps) {
  if (!active || !payload || payload.length === 0) return null
  const row = payload[0]?.payload as { point: TrendDataPoint } | undefined
  const point = row?.point
  if (!point) return null

  const style = chartTooltipStyle(isDarkMode)
  const totalTokens = sumTokenBuckets(
    point.input_tokens,
    point.output_tokens,
    point.cache_creation_tokens,
    point.cache_read_tokens
  )

  return (
    <div
      style={{
        backgroundColor: style.backgroundColor,
        border: `${style.borderWidth}px solid ${style.borderColor}`,
        borderRadius: style.cornerRadius,
        padding: style.padding
      }}
    >
      <p style={{ color: style.titleColor, fontSize: 12, marginBottom: 4 }}>{point.date}</p>
      {payload.map((entry) => {
        const raw = Number(entry.value ?? 0)
        return (
          <p key={String(entry.dataKey)} style={{ color: style.bodyColor, fontSize: 12 }}>
            {entry.name}: {formatTokens(raw)} ({formatTokenShare(raw, totalTokens) ?? '0.0%'})
          </p>
        )
      })}
      <p style={{ color: style.footerColor, fontSize: 12, marginTop: 4 }}>
        Actual: ${formatCost(point.actual_cost)} | Standard: ${formatCost(point.cost)}
      </p>
    </div>
  )
}

export function TokenUsageTrend({
  workspace = false,
  trendData,
  loading = false,
  className
}: TokenUsageTrendProps) {
  const { t } = useTranslation()
  const isDarkMode = useDocumentDarkMode()

  const chartColors = {
    text: isDarkMode ? '#e5e5e5' : '#525252',
    grid: isDarkMode ? '#404040' : '#e5e5e5'
  }

  const chartData = useMemo(() => {
    if (!trendData?.length) return null
    return trendData.map((point) => ({
      date: point.date,
      Input: point.input_tokens,
      Output: point.output_tokens,
      'Cache Creation': point.cache_creation_tokens,
      'Cache Read': point.cache_read_tokens,
      point
    }))
  }, [trendData])

  return (
    <div className={cn('card p-4', workspace && 'chart-workspace', className)}>
      <h3 className="mb-4 text-sm font-semibold text-gray-900 dark:text-white">
        {t('admin.dashboard.tokenUsageTrend')}
      </h3>
      {loading && trendData.length === 0 ? (
        <div className="flex h-48 items-center justify-center">
          <Spinner />
        </div>
      ) : trendData.length > 0 && chartData ? (
        <div className="h-48">
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={chartData} margin={{ top: 4, right: 4, bottom: 4, left: 4 }}>
              <CartesianGrid stroke={chartColors.grid} />
              <XAxis
                dataKey="date"
                tick={{ fill: chartColors.text, fontSize: 10 }}
                stroke={chartColors.grid}
              />
              <YAxis
                tick={{ fill: chartColors.text, fontSize: 10 }}
                stroke={chartColors.grid}
                tickFormatter={(value: number) => formatTokens(value)}
              />
              <Tooltip
                content={(props) => <TrendTooltip {...props} isDarkMode={isDarkMode} />}
                cursor={{ stroke: chartColors.grid }}
              />
              <Legend
                verticalAlign="top"
                iconType="circle"
                iconSize={8}
                wrapperStyle={{ color: chartColors.text, fontSize: 11, paddingBottom: 15 }}
              />
              {SERIES.map((entry) => (
                <Area
                  key={entry.key}
                  type="natural"
                  dataKey={entry.key}
                  stroke={entry.color}
                  fill={entry.color}
                  fillOpacity={0.125}
                  strokeWidth={2}
                  dot={false}
                  activeDot={{ r: 3 }}
                  isAnimationActive={false}
                />
              ))}
            </AreaChart>
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
