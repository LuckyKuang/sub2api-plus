import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { chartTooltipStyle } from '@/utils/chartTooltip'
import { EmptyState, HelpTooltip } from '@/components/ui'
import type { OpsLatencyHistogramResponse } from '@/api/admin/ops'
import { useDocumentDarkMode } from '@/views/admin/shared'
import { ChartCanvas } from '../../parts/ChartCanvas'
import type { ChartState } from '../types'

export interface OpsLatencyChartProps {
  latencyData: OpsLatencyHistogramResponse | null
  loading: boolean
}

export function OpsLatencyChart({ latencyData, loading }: OpsLatencyChartProps) {
  const { t } = useTranslation()
  const isDarkMode = useDocumentDarkMode()

  const colors = {
    blue: '#3b82f6',
    grid: isDarkMode ? '#404040' : '#e5e5e5',
    text: isDarkMode ? '#e5e5e5' : '#525252'
  }

  const hasData = (latencyData?.total_requests ?? 0) > 0

  const state: ChartState = hasData ? 'ready' : loading ? 'loading' : 'empty'

  const chartData = useMemo(() => {
    if (!latencyData || !hasData) return null
    return {
      labels: latencyData.buckets.map((bucket) => bucket.range),
      datasets: [
        {
          label: t('admin.ops.requests'),
          data: latencyData.buckets.map((bucket) => bucket.count),
          backgroundColor: colors.blue,
          borderRadius: 4,
          barPercentage: 0.6
        }
      ]
    }
  }, [latencyData, hasData, t, colors.blue])

  const options = useMemo(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: { display: false },
        tooltip: chartTooltipStyle(isDarkMode)
      },
      scales: {
        x: {
          grid: { display: false },
          ticks: { color: colors.text, font: { size: 10 } }
        },
        y: {
          beginAtZero: true,
          grid: { color: colors.grid, borderDash: [4, 4] },
          ticks: { color: colors.text, font: { size: 10 } }
        }
      }
    }),
    [isDarkMode, colors.grid, colors.text]
  )

  return (
    <div className="glass-surface flex h-full flex-col p-6 ring-1 ring-gray-900/5 dark:ring-dark-700">
      <div className="mb-4 flex items-center justify-between">
        <h3 className="flex items-center gap-2 text-sm font-bold text-gray-900 dark:text-white">
          <svg className="h-4 w-4 text-purple-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth="2"
              d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
            />
          </svg>
          {t('admin.ops.latencyHistogram')}
          <HelpTooltip content={t('admin.ops.tooltips.latencyHistogram')} />
        </h3>
      </div>

      <div className="min-h-0 flex-1">
        {state === 'ready' && chartData ? (
          <ChartCanvas kind="bar" data={chartData as never} options={options as never} />
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

export default OpsLatencyChart
