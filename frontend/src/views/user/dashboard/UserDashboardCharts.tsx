import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Cell, Pie, PieChart, ResponsiveContainer, Tooltip } from 'recharts'
import { DateRangePicker } from '@/components/DateRangePicker'
import { Select, Spinner } from '@/components/ui'
import { chartTooltipStyle } from '@/utils/chartTooltip'
import type { TrendDataPoint, ModelStat } from '@/types'
import { formatCostFixed as formatCost, formatNumberLocaleString as formatNumber, formatTokensK as formatTokens } from '@/utils/format'
import { formatTokenShare } from '@/utils/tokenShare'
import { TokenUsageTrend } from './TokenUsageTrend'

export interface UserDashboardChartsProps {
  workspace?: boolean
  showToolbar?: boolean
  loading: boolean
  startDate: string
  endDate: string
  granularity: string
  trend: TrendDataPoint[]
  models: ModelStat[]
  onStartDateChange?: (value: string) => void
  onEndDateChange?: (value: string) => void
  onGranularityChange?: (value: string) => void
  onDateRangeChange?: (range: { startDate: string; endDate: string; preset: string | null }) => void
  onGranularityChanged?: () => void
  onRefresh?: () => void
}

const MODEL_COLORS = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899', '#06b6d4', '#84cc16']

function useDocumentDarkMode(): boolean {
  const [isDark, setIsDark] = useState(
    () => typeof document !== 'undefined' && document.documentElement.classList.contains('dark')
  )

  useEffect(() => {
    if (typeof document === 'undefined') return
    const observer = new MutationObserver(() => {
      setIsDark(document.documentElement.classList.contains('dark'))
    })
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => observer.disconnect()
  }, [])

  return isDark
}

export function UserDashboardCharts({
  workspace = false,
  showToolbar,
  loading,
  startDate,
  endDate,
  granularity,
  trend,
  models,
  onStartDateChange,
  onEndDateChange,
  onGranularityChange,
  onDateRangeChange,
  onGranularityChanged,
  onRefresh
}: UserDashboardChartsProps) {
  const { t } = useTranslation()
  const isDarkMode = useDocumentDarkMode()
  const showToolbarVisible = showToolbar !== false

  const modelData = useMemo(
    () =>
      !models?.length
        ? null
        : models.map((m: ModelStat) => ({ name: m.model, value: m.total_tokens })),
    [models]
  )

  const modelTokenTotal = useMemo(
    () => models.reduce((sum, model) => sum + Number(model.total_tokens || 0), 0),
    [models]
  )

  const modelTokenShare = (tokens: number | null | undefined): string | null =>
    formatTokenShare(tokens, modelTokenTotal)

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
      labelStyle: { color: base.titleColor }
    }
  }, [isDarkMode])

  return (
    <div className={workspace ? 'workspace-body user-dashboard-charts' : 'space-y-6'}>
      {showToolbarVisible && (
        <div className="card p-4">
          <div className="flex flex-wrap items-center gap-4">
            <div className="flex items-center gap-2">
              <span className="text-sm font-medium surface-muted">{t('dashboard.timeRange')}:</span>
              <DateRangePicker
                startDate={startDate}
                endDate={endDate}
                onStartDateChange={onStartDateChange}
                onEndDateChange={onEndDateChange}
                onChange={onDateRangeChange}
              />
            </div>
            <button onClick={onRefresh} disabled={loading} className="btn btn-secondary">
              {t('common.refresh')}
            </button>
            <div className="ml-auto flex items-center gap-2">
              <span className="text-sm font-medium surface-muted">{t('dashboard.granularity')}:</span>
              <div className="w-28">
                <Select
                  value={granularity}
                  searchable={false}
                  options={[
                    { value: 'day', label: t('dashboard.day') },
                    { value: 'hour', label: t('dashboard.hour') }
                  ]}
                  onValueChange={(value) => {
                    onGranularityChange?.(String(value))
                    onGranularityChanged?.()
                  }}
                />
              </div>
            </div>
          </div>
        </div>
      )}

      <div
        className={`grid grid-cols-1 gap-6 lg:grid-cols-2 ${workspace ? 'workspace-body user-chart-grid' : ''}`}
      >
        <div className={`card relative overflow-hidden p-4 ${workspace ? 'user-model-chart' : ''}`}>
          {loading && models.length === 0 && (
            <div className="surface-inset absolute inset-0 z-10 flex items-center justify-center">
              <Spinner className="h-6 w-6 text-primary-500" />
            </div>
          )}
          <h3 className="mb-4 text-sm font-semibold text-gray-900 dark:text-white">{t('dashboard.modelDistribution')}</h3>
          <div className="model-distribution-body flex flex-col items-center gap-4">
            <div className="model-distribution-doughnut shrink-0">
              {modelData ? (
                <ResponsiveContainer width="100%" height="100%">
                  <PieChart>
                    <Pie
                      data={modelData}
                      dataKey="value"
                      nameKey="name"
                      innerRadius="55%"
                      outerRadius="80%"
                      isAnimationActive={false}
                    >
                      {modelData.map((entry) => (
                        <Cell key={entry.name} fill={MODEL_COLORS[modelData.indexOf(entry) % MODEL_COLORS.length]} />
                      ))}
                    </Pie>
                    <Tooltip
                      {...tooltipStyle}
                      formatter={(value, name) => [
                        `${formatTokens(Number(value))} tokens (${formatTokenShare(Number(value), modelTokenTotal) ?? '0.0%'})`,
                        String(name)
                      ]}
                    />
                  </PieChart>
                </ResponsiveContainer>
              ) : (
                <div className="flex h-full items-center justify-center text-sm surface-muted">
                  {t('dashboard.noDataAvailable')}
                </div>
              )}
            </div>
            <div className="model-distribution-records record-table-scroll w-full min-w-0">
              <table className="model-distribution-table record-table record-table-compact w-full text-xs">
                <thead>
                  <tr className="surface-muted">
                    <th className="pb-2 text-left">{t('dashboard.model')}</th>
                    <th className="pb-2 text-left">{t('dashboard.requests')}</th>
                    <th className="pb-2 text-left">{t('dashboard.tokens')}</th>
                    <th className="pb-2 text-left">{t('dashboard.actual')}</th>
                    <th className="pb-2 text-left">{t('dashboard.standard')}</th>
                  </tr>
                </thead>
                <tbody>
                  {models.map((model) => (
                    <tr key={model.model} className="border-t border-gray-100 dark:border-dark-700">
                      <td className="py-1.5 font-medium text-gray-900 dark:text-white" title={model.model}>
                        {model.model}
                      </td>
                      <td className="surface-muted py-1.5 text-left" data-label={t('dashboard.requests')}>
                        {formatNumber(model.requests)}
                      </td>
                      <td className="surface-muted py-1.5 text-left" data-label={t('dashboard.tokens')}>
                        {formatTokens(model.total_tokens)}{' '}
                        {modelTokenShare(model.total_tokens) && <span className="surface-muted">({modelTokenShare(model.total_tokens)})</span>}
                      </td>
                      <td className="py-1.5 text-left text-green-600 dark:text-green-400" data-label={t('dashboard.actual')}>
                        ${formatCost(model.actual_cost)}
                      </td>
                      <td className="surface-muted py-1.5 text-left" data-label={t('dashboard.standard')}>
                        ${formatCost(model.cost)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>

        <TokenUsageTrend workspace={workspace} trendData={trend} loading={loading} />
      </div>
    </div>
  )
}

export default UserDashboardCharts
