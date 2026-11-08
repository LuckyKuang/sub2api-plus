import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { chartTooltipStyle } from '@/utils/chartTooltip'
import { EmptyState, HelpTooltip } from '@/components/ui'
import type { OpsThroughputTrendPoint } from '@/api/admin/ops'
import { useDocumentDarkMode } from '@/views/admin/shared'
import { formatHistoryLabel, sumNumbers } from '../utils/opsFormatters'
import { ChartCanvas } from '../../parts/ChartCanvas'
import type { ChartState } from '../types'

export interface OpsSwitchRateTrendChartProps {
  points: OpsThroughputTrendPoint[]
  loading: boolean
  timeRange: string
  fullscreen?: boolean
}

export function OpsSwitchRateTrendChart({ points, loading, timeRange, fullscreen = false }: OpsSwitchRateTrendChartProps) {
  const { t } = useTranslation()
  const isDarkMode = useDocumentDarkMode()

  const colors = {
    teal: '#14b8a6',
    tealAlpha: '#14b8a620',
    grid: isDarkMode ? '#404040' : '#e5e5e5',
    text: isDarkMode ? '#e5e5e5' : '#525252'
  }

  const totalRequests = sumNumbers(points.map((point) => point.request_count))

  const chartData = useMemo(() => {
    if (!points.length || totalRequests <= 0) return null
    return {
      labels: points.map((point) => formatHistoryLabel(point.bucket_start, timeRange)),
      datasets: [
        {
          label: t('admin.ops.switchRate'),
          data: points.map((point) => {
            const requests = point.request_count ?? 0
            const switches = point.switch_count ?? 0
            if (requests <= 0) return 0
            return switches / requests
          }),
          borderColor: colors.teal,
          backgroundColor: colors.tealAlpha,
          fill: true,
          tension: 0.35,
          pointRadius: 0,
          pointHitRadius: 10
        }
      ]
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [points, timeRange, t])

  const state: ChartState = chartData ? 'ready' : loading ? 'loading' : 'empty'

  const options = useMemo(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      interaction: { intersect: false, mode: 'index' as const },
      plugins: {
        legend: {
          position: 'top' as const,
          align: 'end' as const,
          labels: { color: colors.text, usePointStyle: true, boxWidth: 6, font: { size: 10 } }
        },
        tooltip: {
          ...chartTooltipStyle(isDarkMode),
          displayColors: true,
          callbacks: {
            label: (context: { parsed?: { y?: number } }) => {
              const value = typeof context?.parsed?.y === 'number' ? context.parsed.y : 0
              return `${t('admin.ops.switchRate')}: ${value.toFixed(3)}`
            }
          }
        }
      },
      scales: {
        x: {
          type: 'category' as const,
          grid: { display: false },
          ticks: {
            color: colors.text,
            font: { size: 10 },
            maxTicksLimit: 8,
            autoSkip: true,
            autoSkipPadding: 10
          }
        },
        y: {
          type: 'linear' as const,
          display: true,
          position: 'left' as const,
          grid: { color: colors.grid, borderDash: [4, 4] },
          ticks: {
            color: colors.text,
            font: { size: 10 },
            callback: (value: string | number) => Number(value).toFixed(3)
          }
        }
      }
    }),
    [isDarkMode, colors.grid, colors.text, t]
  )

  return (
    <div className="glass-surface flex h-full flex-col p-6 ring-1 ring-gray-900/5 dark:ring-dark-700">
      <div className="mb-4 flex shrink-0 items-center justify-between">
        <h3 className="flex items-center gap-2 text-sm font-bold text-gray-900 dark:text-white">
          <svg className="h-4 w-4 text-teal-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M7 7h10M7 12h6m-6 5h3" />
          </svg>
          {t('admin.ops.switchRateTrend')}
          {!fullscreen && <HelpTooltip content={t('admin.ops.tooltips.switchRateTrend')} />}
        </h3>
      </div>

      <div className="min-h-0 flex-1">
        {state === 'ready' && chartData ? (
          <ChartCanvas kind="line" data={chartData as never} options={options as never} />
        ) : (
          <div className="flex h-full items-center justify-center">
            {state === 'loading' ? (
              <div className="animate-pulse text-sm text-gray-400">{t('common.loading')}</div>
            ) : (
              <EmptyState title={t('common.noData')} description={t('admin.ops.charts.emptyRequest')} />
            )}
          </div>
        )}
      </div>
    </div>
  )
}

export default OpsSwitchRateTrendChart
