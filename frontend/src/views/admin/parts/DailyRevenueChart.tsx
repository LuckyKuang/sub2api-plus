import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { chartTooltipStyle } from '@/utils/chartTooltip'
import type { DailyPaymentStats } from '@/types/payment'
import { useDocumentDarkMode } from '@/views/admin/shared'
import { LoadingSpinner } from '@/views/admin/shared'
import { ChartCanvas } from './ChartCanvas'

const colors: Array<[string, string]> = [
  ['rgb(59, 130, 246)', 'rgba(59, 130, 246, 0.1)'],
  ['rgb(168, 85, 247)', 'rgba(168, 85, 247, 0.1)'],
  ['rgb(245, 158, 11)', 'rgba(245, 158, 11, 0.1)'],
  ['rgb(239, 68, 68)', 'rgba(239, 68, 68, 0.1)']
]

export interface DailyRevenueChartProps {
  data: DailyPaymentStats[]
  loading?: boolean
}

export function DailyRevenueChart({ data, loading = false }: DailyRevenueChartProps) {
  const { t } = useTranslation()
  const isDarkMode = useDocumentDarkMode()

  const chartData = useMemo(() => {
    if (!data || data.length === 0) return null
    const currencies = [...new Set(data.flatMap((day) => Object.keys(day.amount)))].sort()
    return {
      labels: data.map((day) => day.date),
      datasets: [
        ...currencies.map((currency, index) => {
          const [borderColor, backgroundColor] = colors[index % colors.length]
          return {
            label: `${currency} ${t('payment.admin.revenue')}`,
            data: data.map((day) => day.amount[currency] || 0),
            borderColor,
            backgroundColor,
            fill: true,
            tension: 0.3,
            pointRadius: 3,
            pointHoverRadius: 5
          }
        }),
        {
          label: t('payment.admin.orderCount'),
          data: data.map((day) => day.count),
          borderColor: 'rgb(16, 185, 129)',
          backgroundColor: 'rgba(16, 185, 129, 0.1)',
          fill: false,
          tension: 0.3,
          pointRadius: 3,
          pointHoverRadius: 5,
          yAxisID: 'y1'
        }
      ]
    }
  }, [data, t])

  const chartOptions = useMemo(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: 'index' as const, intersect: false },
      scales: {
        y: {
          type: 'linear' as const,
          display: true,
          position: 'left' as const,
          title: { display: true, text: t('payment.admin.revenue') }
        },
        y1: {
          type: 'linear' as const,
          display: true,
          position: 'right' as const,
          title: { display: true, text: t('payment.admin.orderCount') },
          grid: { drawOnChartArea: false }
        }
      },
      plugins: {
        legend: { position: 'top' as const },
        tooltip: chartTooltipStyle(isDarkMode)
      }
    }),
    [t, isDarkMode]
  )

  return (
    <div className="card p-4">
      <h3 className="mb-4 text-sm font-semibold text-gray-900 dark:text-white">{t('payment.admin.dailyRevenue')}</h3>
      <div className="h-64">
        {loading ? (
          <div className="flex h-full items-center justify-center">
            <LoadingSpinner size="md" />
          </div>
        ) : chartData ? (
          <ChartCanvas kind="line" data={chartData as never} options={chartOptions as never} />
        ) : (
          <div className="flex h-full items-center justify-center text-sm surface-muted">{t('payment.admin.noData')}</div>
        )}
      </div>
    </div>
  )
}

export default DailyRevenueChart
