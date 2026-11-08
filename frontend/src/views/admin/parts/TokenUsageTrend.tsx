import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { chartTooltipStyle } from '@/utils/chartTooltip'
import type { TrendDataPoint } from '@/types'
import { formatTokenShare, sumTokenBuckets } from '@/utils/tokenShare'
import { useDocumentDarkMode } from '@/views/admin/shared'
import { LoadingSpinner } from '@/views/admin/shared'
import { Icon } from '@/views/admin/AccountIcon'
import { ChartCanvas } from './ChartCanvas'

function formatTokens(value: number): string {
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(2)}K`
  return value.toLocaleString()
}

function formatCost(value: number): string {
  if (value >= 1000) return (value / 1000).toFixed(2) + 'K'
  if (value >= 1) return value.toFixed(2)
  if (value >= 0.01) return value.toFixed(3)
  return value.toFixed(4)
}

export interface TokenUsageTrendProps {
  workspace?: boolean
  trendData: TrendDataPoint[]
  loading?: boolean
}

export function TokenUsageTrend({ workspace = false, trendData, loading = false }: TokenUsageTrendProps) {
  const { t } = useTranslation()
  const isDarkMode = useDocumentDarkMode()

  const chartColors = {
    text: isDarkMode ? '#e5e5e5' : '#525252',
    grid: isDarkMode ? '#404040' : '#e5e5e5',
    input: '#3b82f6',
    output: '#10b981',
    cacheCreation: '#f59e0b',
    cacheRead: '#06b6d4'
  }

  const chartData = useMemo(() => {
    if (!trendData?.length) return null

    return {
      labels: trendData.map((point) => point.date),
      datasets: [
        {
          label: 'Input',
          data: trendData.map((point) => point.input_tokens),
          borderColor: chartColors.input,
          backgroundColor: `${chartColors.input}20`,
          fill: true,
          tension: 0.3
        },
        {
          label: 'Output',
          data: trendData.map((point) => point.output_tokens),
          borderColor: chartColors.output,
          backgroundColor: `${chartColors.output}20`,
          fill: true,
          tension: 0.3
        },
        {
          label: 'Cache Creation',
          data: trendData.map((point) => point.cache_creation_tokens),
          borderColor: chartColors.cacheCreation,
          backgroundColor: `${chartColors.cacheCreation}20`,
          fill: true,
          tension: 0.3
        },
        {
          label: 'Cache Read',
          data: trendData.map((point) => point.cache_read_tokens),
          borderColor: chartColors.cacheRead,
          backgroundColor: `${chartColors.cacheRead}20`,
          fill: true,
          tension: 0.3
        }
      ]
    }
  }, [trendData, chartColors.input, chartColors.output, chartColors.cacheCreation, chartColors.cacheRead])

  const lineOptions = useMemo(
    () => ({
      animation: false as const,
      responsive: true,
      maintainAspectRatio: false,
      interaction: { intersect: false, mode: 'index' as const },
      plugins: {
        legend: {
          position: 'top' as const,
          labels: {
            color: chartColors.text,
            usePointStyle: true,
            pointStyle: 'circle',
            padding: 15,
            font: { size: 11 }
          }
        },
        tooltip: {
          ...chartTooltipStyle(isDarkMode),
          callbacks: {
            label: (context: { raw: unknown; dataset: { label?: string }; dataIndex: number }) => {
              const point = trendData[context.dataIndex]
              const totalTokens = point
                ? sumTokenBuckets(
                    point.input_tokens,
                    point.output_tokens,
                    point.cache_creation_tokens,
                    point.cache_read_tokens
                  )
                : 0
              return `${context.dataset.label}: ${formatTokens(Number(context.raw))} (${formatTokenShare(context.raw as number, totalTokens) ?? '0.0%'})`
            },
            footer: (tooltipItems: Array<{ dataIndex: number }>) => {
              const dataIndex = tooltipItems[0]?.dataIndex
              if (dataIndex !== undefined && trendData[dataIndex]) {
                const data = trendData[dataIndex]
                return `Actual: $${formatCost(data.actual_cost)} | Standard: $${formatCost(data.cost)}`
              }
              return ''
            }
          }
        }
      },
      scales: {
        x: {
          grid: { color: chartColors.grid },
          ticks: { color: chartColors.text, font: { size: 10 } }
        },
        y: {
          grid: { color: chartColors.grid },
          ticks: { color: chartColors.text, font: { size: 10 }, callback: (value: string | number) => formatTokens(Number(value)) }
        }
      }
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [isDarkMode, trendData]
  )

  return (
    <div className={`card p-4 ${workspace ? 'chart-workspace' : ''}`}>
      <h3 className="mb-4 text-sm font-semibold text-gray-900 dark:text-white">{t('admin.dashboard.tokenUsageTrend')}</h3>
      {loading && !trendData.length ? (
        <div className="flex h-48 items-center justify-center">
          <LoadingSpinner />
        </div>
      ) : trendData.length > 0 && chartData ? (
        <div className="h-48">
          <ChartCanvas kind="line" data={chartData as never} options={lineOptions as never} />
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
