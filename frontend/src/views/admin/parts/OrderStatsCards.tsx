import { useTranslation } from 'react-i18next'
import type { CurrencyAmounts, DashboardStats } from '@/types/payment'
import { Icon } from '@/views/admin/AccountIcon'

function sortedAmounts(amounts: CurrencyAmounts): [string, number][] {
  return Object.entries(amounts).sort(([left], [right]) => left.localeCompare(right))
}

function formatMoney(currency: string, amount: number): string {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(amount)
}

export interface OrderStatsCardsProps {
  stats: DashboardStats
}

export function OrderStatsCards({ stats }: OrderStatsCardsProps) {
  const { t } = useTranslation()

  return (
    <div className="grid grid-cols-1 gap-4 min-[375px]:grid-cols-2 lg:grid-cols-4">
      <div className="card min-w-0 p-4">
        <div className="flex min-w-0 flex-col items-start gap-3 sm:flex-row sm:items-center">
          <div className="rounded-lg bg-green-100/[0.15] p-2 dark:bg-green-900/30">
            <Icon name="dollar" size="md" className="text-green-600 dark:text-green-400" strokeWidth={2} />
          </div>
          <div className="min-w-0 max-w-full [overflow-wrap:anywhere]">
            <p className="text-xs font-medium surface-muted">{t('payment.admin.todayRevenue')}</p>
            {sortedAmounts(stats.today_amount).map(([currency, amount]) => (
              <p key={currency} className="text-xl font-bold text-gray-900 dark:text-white">
                {formatMoney(currency, amount)}
              </p>
            ))}
            <p className="text-xs surface-muted">
              {stats.today_count} {t('payment.admin.orders')}
            </p>
          </div>
        </div>
      </div>

      <div className="card min-w-0 p-4">
        <div className="flex min-w-0 flex-col items-start gap-3 sm:flex-row sm:items-center">
          <div className="rounded-lg bg-blue-100/[0.15] p-2 dark:bg-blue-900/30">
            <Icon name="creditCard" size="md" className="text-blue-600 dark:text-blue-400" strokeWidth={2} />
          </div>
          <div className="min-w-0 max-w-full [overflow-wrap:anywhere]">
            <p className="text-xs font-medium surface-muted">{t('payment.admin.totalRevenue')}</p>
            {sortedAmounts(stats.total_amount).map(([currency, amount]) => (
              <p key={currency} className="text-xl font-bold text-gray-900 dark:text-white">
                {formatMoney(currency, amount)}
              </p>
            ))}
            <p className="text-xs surface-muted">
              {stats.total_count} {t('payment.admin.orders')}
            </p>
          </div>
        </div>
      </div>

      <div className="card min-w-0 p-4">
        <div className="flex min-w-0 flex-col items-start gap-3 sm:flex-row sm:items-center">
          <div className="rounded-lg bg-purple-100/[0.15] p-2 dark:bg-purple-900/30">
            <Icon name="chart" size="md" className="text-purple-600 dark:text-purple-400" strokeWidth={2} />
          </div>
          <div className="min-w-0 max-w-full [overflow-wrap:anywhere]">
            <p className="text-xs font-medium surface-muted">{t('payment.admin.todayOrders')}</p>
            <p className="text-xl font-bold text-gray-900 dark:text-white">{stats.today_count}</p>
          </div>
        </div>
      </div>

      <div className="card min-w-0 p-4">
        <div className="flex min-w-0 flex-col items-start gap-3 sm:flex-row sm:items-center">
          <div className="rounded-lg bg-amber-100/[0.15] p-2 dark:bg-amber-900/30">
            <Icon name="chart" size="md" className="text-amber-600 dark:text-amber-400" strokeWidth={2} />
          </div>
          <div className="min-w-0 max-w-full [overflow-wrap:anywhere]">
            <p className="text-xs font-medium surface-muted">{t('payment.admin.avgAmount')}</p>
            {sortedAmounts(stats.avg_amount).map(([currency, amount]) => (
              <p key={currency} className="text-xl font-bold text-gray-900 dark:text-white">
                {formatMoney(currency, amount)}
              </p>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}

export default OrderStatsCards
