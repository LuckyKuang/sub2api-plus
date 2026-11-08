import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { chartTooltipStyle } from '@/utils/chartTooltip'
import { EmptyState, HelpTooltip } from '@/components/ui'
import type { OpsErrorTrendPoint } from '@/api/admin/ops'
import { useDocumentDarkMode } from '@/views/admin/shared'
import { formatHistoryLabel, sumNumbers } from '../utils/opsFormatters'
import { ChartCanvas } from '../../parts/ChartCanvas'
import type { ChartState } from '../types'

export interface OpsErrorTrendChartProps {
  points: OpsErrorTrendPoint[]
  loading: boolean
  timeRange: string
  onOpenRequestErrors?: () => void
  onOpenUpstreamErrors?: () => void
}

export function OpsErrorTrendChart({
  points,
  loading,
  timeRange,
  onOpenRequestErrors,
  onOpenUpstreamErrors
}: OpsErrorTrendChartProps) {
  const { t } = useTranslation()
  const isDarkMode = useDocumentDarkMode()

  const colors = {
    red: '#ef4444',
    redAlpha: '#ef444420',
    purple: '#8b5cf6',
    purpleAlpha: '#8b5cf620',
    gray: '#9ca3af',
    grid: isDarkMode ? '#404040' : '#e5e5e5',
    text: isDarkMode ? '#e5e5e5' : '#525252'
  }

  const totalRequestErrors = sumNumbers(points.map((point) => point.error_count_sla ?? 0))

  const totalUpstreamErrors = sumNumbers(
    points.map(
      (point) =>
        (point.upstream_error_count_excl_429_529 ?? 0) + (point.upstream_429_count ?? 0) + (point.upstream_529_count ?? 0)
    )
  )

  const totalDisplayed = sumNumbers(
    points.map(
      (point) =>
        (point.error_count_sla ?? 0) + (point.upstream_error_count_excl_429_529 ?? 0) + (point.business_limited_count ?? 0)
    )
  )

  const hasRequestErrors = totalRequestErrors > 0
  const hasUpstreamErrors = totalUpstreamErrors > 0

  const chartData = useMemo(() => {
    if (!points.length || totalDisplayed <= 0) return null
    return {
      labels: points.map((point) => formatHistoryLabel(point.bucket_start, timeRange)),
      datasets: [
        {
          label: t('admin.ops.errorsSla'),
          data: points.map((point) => point.error_count_sla ?? 0),
          borderColor: colors.red,
          backgroundColor: colors.redAlpha,
          fill: true,
          tension: 0.35,
          pointRadius: 0,
          pointHitRadius: 10
        },
        {
          label: t('admin.ops.upstreamExcl429529'),
          data: points.map((point) => point.upstream_error_count_excl_429_529 ?? 0),
          borderColor: colors.purple,
          backgroundColor: colors.purpleAlpha,
          fill: true,
          tension: 0.35,
          pointRadius: 0,
          pointHitRadius: 10
        },
        {
          label: t('admin.ops.businessLimited'),
          data: points.map((point) => point.business_limited_count ?? 0),
          borderColor: colors.gray,
          backgroundColor: 'transparent',
          borderDash: [6, 6],
          fill: false,
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
        tooltip: { ...chartTooltipStyle(isDarkMode), displayColors: true }
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
          ticks: { color: colors.text, font: { size: 10 }, precision: 0 }
        }
      }
    }),
    [isDarkMode, colors.grid, colors.text]
  )

  return (
    <div className="glass-surface flex h-full flex-col p-6 ring-1 ring-gray-900/5 dark:ring-dark-700">
      <div className="mb-4 flex shrink-0 items-center justify-between">
        <h3 className="flex items-center gap-2 text-sm font-bold text-gray-900 dark:text-white">
          <svg className="h-4 w-4 text-rose-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth="2"
              d="M13 17h8m0 0V9m0 8l-8-8-4 4-6-6"
            />
          </svg>
          {t('admin.ops.errorTrend')}
          <HelpTooltip content={t('admin.ops.tooltips.errorTrend')} />
        </h3>
        <div className="flex items-center gap-2">
          <button
            type="button"
            className="inline-flex items-center rounded-lg border border-gray-200 px-2 py-1 text-[11px] font-semibold hover:surface-hover disabled:opacity-50 dark:border-dark-700 surface-control surface-muted"
            disabled={!hasRequestErrors}
            onClick={() => onOpenRequestErrors?.()}
          >
            {t('admin.ops.errorDetails.requestErrors')}
          </button>
          <button
            type="button"
            className="inline-flex items-center rounded-lg border border-gray-200 px-2 py-1 text-[11px] font-semibold hover:surface-hover disabled:opacity-50 dark:border-dark-700 surface-control surface-muted"
            disabled={!hasUpstreamErrors}
            onClick={() => onOpenUpstreamErrors?.()}
          >
            {t('admin.ops.errorDetails.upstreamErrors')}
          </button>
        </div>
      </div>

      <div className="min-h-0 flex-1">
        {state === 'ready' && chartData ? (
          <ChartCanvas kind="line" data={chartData as never} options={options as never} />
        ) : (
          <div className="flex h-full items-center justify-center">
            {state === 'loading' ? (
              <div className="animate-pulse text-sm text-gray-400">{t('common.loading')}</div>
            ) : (
              <EmptyState title={t('common.noData')} description={t('admin.ops.charts.emptyError')} />
            )}
          </div>
        )}
      </div>
    </div>
  )
}

export default OpsErrorTrendChart
