import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { chartTooltipStyle } from '@/utils/chartTooltip'
import { formatNumber } from '@/utils/format'
import { EmptyState, HelpTooltip } from '@/components/ui'
import type {
  OpsThroughputGroupBreakdownItem,
  OpsThroughputPlatformBreakdownItem,
  OpsThroughputTrendPoint
} from '@/api/admin/ops'
import { useDocumentDarkMode } from '@/views/admin/shared'
import { formatHistoryLabel, sumNumbers } from '../utils/opsFormatters'
import { ChartCanvas } from '../../parts/ChartCanvas'
import type { ChartState } from '../types'

export interface OpsThroughputTrendChartProps {
  points: OpsThroughputTrendPoint[]
  loading: boolean
  timeRange: string
  byPlatform?: OpsThroughputPlatformBreakdownItem[]
  topGroups?: OpsThroughputGroupBreakdownItem[]
  fullscreen?: boolean
  onSelectPlatform?: (platform: string) => void
  onSelectGroup?: (groupId: number) => void
  onOpenDetails?: () => void
}

export function OpsThroughputTrendChart({
  points,
  loading,
  timeRange,
  byPlatform = [],
  topGroups = [],
  fullscreen = false,
  onSelectPlatform,
  onSelectGroup,
  onOpenDetails
}: OpsThroughputTrendChartProps) {
  const { t } = useTranslation()
  const isDarkMode = useDocumentDarkMode()

  const colors = {
    blue: '#3b82f6',
    blueAlpha: '#3b82f620',
    green: '#10b981',
    greenAlpha: '#10b98120',
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
          label: 'QPS',
          data: points.map((point) => point.qps ?? 0),
          borderColor: colors.blue,
          backgroundColor: colors.blueAlpha,
          fill: true,
          tension: 0.4,
          pointRadius: 0,
          pointHitRadius: 10
        },
        {
          label: t('admin.ops.tpsK'),
          data: points.map((point) => (point.tps ?? 0) / 1000),
          borderColor: colors.green,
          backgroundColor: colors.greenAlpha,
          fill: true,
          tension: 0.4,
          pointRadius: 0,
          pointHitRadius: 10,
          yAxisID: 'y1'
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
            label: (context: { dataset: { label?: string }; parsed: { y: number } }) => {
              let label = context.dataset.label || ''
              if (label) label += ': '
              if (context.parsed?.y != null) label += context.parsed.y.toFixed(1)
              return label
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
          ticks: { color: colors.text, font: { size: 10 } }
        },
        y1: {
          type: 'linear' as const,
          display: true,
          position: 'right' as const,
          grid: { display: false },
          ticks: { color: colors.green, font: { size: 10 } }
        }
      }
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [isDarkMode, colors.grid, colors.text]
  )

  return (
    <div className="glass-surface flex h-full min-w-0 flex-col p-6 ring-1 ring-gray-900/5 dark:ring-dark-700">
      <div
        data-testid="throughput-chart-header"
        className="mb-4 flex shrink-0 flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"
      >
        <h3 className="flex min-w-0 items-center gap-2 text-sm font-bold text-gray-900 dark:text-white">
          <svg className="h-4 w-4 text-blue-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M13 7h8m0 0v8m0-8l-8 8-4-4-6 6" />
          </svg>
          {t('admin.ops.throughputTrend')}
          {!fullscreen && <HelpTooltip content={t('admin.ops.tooltips.throughputTrend')} />}
        </h3>
        <div
          data-testid="throughput-chart-toolbar"
          className="flex w-full min-w-0 flex-wrap items-center gap-2 text-xs sm:w-auto sm:justify-end surface-muted"
        >
          <span className="flex shrink-0 items-center gap-1">
            <span className="console-data-mark h-2 w-2 rounded-full bg-blue-500" />
            QPS
          </span>
          <span className="flex shrink-0 items-center gap-1">
            <span className="console-data-mark h-2 w-2 rounded-full bg-green-500" />
            {t('admin.ops.tpsK')}
          </span>
          {!fullscreen && (
            <button
              type="button"
              className="inline-flex shrink-0 items-center rounded-lg border border-gray-200 px-2 py-1 text-[11px] font-semibold hover:surface-hover disabled:opacity-50 dark:border-dark-700 surface-control surface-muted"
              disabled={state !== 'ready'}
              title={t('admin.ops.requestDetails.title')}
              onClick={() => onOpenDetails?.()}
            >
              {t('admin.ops.requestDetails.details')}
            </button>
          )}
        </div>
      </div>

      {/* Drilldown chips (baseline interaction: click to set global filter) */}
      {topGroups.length > 0 ? (
        <div className="mb-3 flex flex-wrap gap-2">
          {topGroups.map((group) => (
            <button
              key={group.group_id}
              type="button"
              className="inline-flex items-center gap-2 rounded-full border border-gray-200 px-3 py-1 text-[11px] font-semibold hover:surface-hover dark:border-dark-700 surface-control surface-muted"
              onClick={() => onSelectGroup?.(group.group_id)}
            >
              <span className="max-w-[180px] truncate">{group.group_name || `#${group.group_id}`}</span>
              <span className="surface-muted">{formatNumber(group.request_count)}</span>
            </button>
          ))}
        </div>
      ) : byPlatform.length > 0 ? (
        <div className="mb-3 flex flex-wrap gap-2">
          {byPlatform.map((entry) => (
            <button
              key={entry.platform}
              type="button"
              className="inline-flex items-center gap-2 rounded-full border border-gray-200 px-3 py-1 text-[11px] font-semibold hover:surface-hover dark:border-dark-700 surface-control surface-muted"
              onClick={() => onSelectPlatform?.(entry.platform)}
            >
              <span className="uppercase">{entry.platform}</span>
              <span className="surface-muted">{formatNumber(entry.request_count)}</span>
            </button>
          ))}
        </div>
      ) : null}

      <div className="min-h-0 min-w-0 flex-1">
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

export default OpsThroughputTrendChart
