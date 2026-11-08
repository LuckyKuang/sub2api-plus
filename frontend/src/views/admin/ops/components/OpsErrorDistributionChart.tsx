import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { chartTooltipStyle } from '@/utils/chartTooltip'
import { EmptyState, HelpTooltip } from '@/components/ui'
import type { OpsErrorDistributionResponse } from '@/api/admin/ops'
import { useDocumentDarkMode } from '@/views/admin/shared'
import { ChartCanvas } from '../../parts/ChartCanvas'
import type { ChartState } from '../types'

export interface OpsErrorDistributionChartProps {
  data: OpsErrorDistributionResponse | null
  loading: boolean
  onOpenDetails?: () => void
}

interface ErrorCategory {
  label: string
  count: number
  color: string
}

export function OpsErrorDistributionChart({ data, loading, onOpenDetails }: OpsErrorDistributionChartProps) {
  const { t } = useTranslation()
  const isDarkMode = useDocumentDarkMode()

  const colors = {
    blue: '#3b82f6',
    red: '#ef4444',
    orange: '#f59e0b',
    gray: '#9ca3af'
  }

  const totalSlaErrors = (data?.items ?? []).reduce((total, item) => total + Number(item.sla || 0), 0)
  const hasData = totalSlaErrors > 0
  const state: ChartState = hasData ? 'ready' : loading ? 'loading' : 'empty'

  const categories = useMemo<ErrorCategory[]>(() => {
    if (!data) return []

    let upstream = 0 // 502, 503, 504
    let client = 0 // 4xx
    let system = 0 // 500
    let other = 0

    for (const item of data.items || []) {
      const code = Number(item.status_code || 0)
      const count = Number(item.sla || 0)
      if (!Number.isFinite(code) || !Number.isFinite(count)) continue

      if ([502, 503, 504].includes(code)) upstream += count
      else if (code >= 400 && code < 500) client += count
      else if (code === 500) system += count
      else other += count
    }

    const out: ErrorCategory[] = []
    if (upstream > 0) out.push({ label: t('admin.ops.upstream'), count: upstream, color: colors.orange })
    if (client > 0) out.push({ label: t('admin.ops.client'), count: client, color: colors.blue })
    if (system > 0) out.push({ label: t('admin.ops.system'), count: system, color: colors.red })
    if (other > 0) out.push({ label: t('admin.ops.other'), count: other, color: colors.gray })
    return out
  }, [data, t, colors.blue, colors.red, colors.orange, colors.gray])

  const topReason = useMemo(() => {
    if (categories.length === 0) return null
    return categories.reduce((prev, cur) => (cur.count > prev.count ? cur : prev))
  }, [categories])

  const chartData = useMemo(() => {
    if (!hasData || categories.length === 0) return null
    return {
      labels: categories.map((item) => item.label),
      datasets: [
        {
          data: categories.map((item) => item.count),
          backgroundColor: categories.map((item) => item.color),
          borderWidth: 0
        }
      ]
    }
  }, [hasData, categories])

  const options = useMemo(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: { display: false },
        tooltip: chartTooltipStyle(isDarkMode)
      }
    }),
    [isDarkMode]
  )

  return (
    <div className="glass-surface flex h-full flex-col p-6 ring-1 ring-gray-900/5 dark:ring-dark-700">
      <div className="mb-4 flex items-center justify-between">
        <h3 className="flex items-center gap-2 text-sm font-bold text-gray-900 dark:text-white">
          <svg className="h-4 w-4 text-red-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth="2"
              d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
            />
          </svg>
          {t('admin.ops.errorDistribution')}
          <HelpTooltip content={t('admin.ops.tooltips.errorDistribution')} />
        </h3>
        <button
          type="button"
          className="inline-flex items-center rounded-lg border border-gray-200 px-2 py-1 text-[11px] font-semibold hover:surface-hover disabled:opacity-50 dark:border-dark-700 surface-control surface-muted"
          disabled={state !== 'ready'}
          title={t('admin.ops.errorTrend')}
          onClick={() => onOpenDetails?.()}
        >
          {t('admin.ops.requestDetails.details')}
        </button>
      </div>

      <div className="relative min-h-0 flex-1">
        {state === 'ready' && chartData ? (
          <div className="flex h-full flex-col">
            <div className="flex-1">
              <ChartCanvas kind="doughnut" data={chartData as never} options={{ ...options, cutout: '65%' } as never} />
            </div>
            <div className="mt-4 flex flex-col items-center gap-2">
              {topReason && (
                <div className="text-xs font-bold text-gray-900 dark:text-white">
                  {t('admin.ops.top')}: <span style={{ color: topReason.color }}>{topReason.label}</span>
                </div>
              )}
              <div className="flex flex-wrap justify-center gap-3">
                {categories.map((item) => (
                  <div key={item.label} className="flex items-center gap-1.5 text-xs">
                    <span className="h-2 w-2 rounded-full" style={{ backgroundColor: item.color }} />
                    <span className="surface-muted">
                      {item.label} {item.count}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          </div>
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

export default OpsErrorDistributionChart
