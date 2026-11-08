import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Select } from '@/components/ui'
import { adminPaymentAPI } from '@/api/admin/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { toast } from '@/components/ui'
import type { CurrencyAmounts, DashboardStats, TopUserPaymentStats } from '@/types/payment'
import { Icon } from '@/views/admin/AccountIcon'
import { LoadingSpinner } from '@/views/admin/shared'
import { DailyRevenueChart } from '../parts/DailyRevenueChart'
import { OrderStatsCards } from '../parts/OrderStatsCards'

const DAYS_OPTIONS = [7, 30, 90] as const

function methodColor(type: string): string {
  const c: Record<string, string> = {
    alipay: 'bg-blue-500',
    wxpay: 'bg-green-500',
    alipay_direct: 'bg-blue-400',
    wxpay_direct: 'bg-green-400',
    stripe: 'bg-purple-500'
  }
  return c[type] || 'bg-gray-400'
}

function rankClass(idx: number): string {
  if (idx === 0) return 'bg-yellow-100/[0.15] text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
  if (idx === 1) return 'surface-inset surface-muted dark:surface-inset'
  if (idx === 2) return 'bg-amber-100/[0.15] text-amber-700 dark:bg-amber-900/30 dark:text-amber-400'
  return 'surface-inset surface-muted dark:surface-inset'
}

function sortedAmounts(amounts: CurrencyAmounts): [string, number][] {
  return Object.entries(amounts).sort(([left], [right]) => left.localeCompare(right))
}

function sortedTopUsers(usersByCurrency: Record<string, TopUserPaymentStats[]>): [string, TopUserPaymentStats[]][] {
  return Object.entries(usersByCurrency).sort(([left], [right]) => left.localeCompare(right))
}

function hasTopUsers(usersByCurrency: Record<string, TopUserPaymentStats[]>): boolean {
  return Object.values(usersByCurrency).some((users) => users.length > 0)
}

function formatMoney(currency: string, amount: number): string {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(amount)
}

export default function AdminPaymentDashboardView() {
  const { t } = useTranslation()

  const [days, setDays] = useState<number>(30)
  const [sourceFilter, setSourceFilter] = useState('')
  const [loading, setLoading] = useState(false)
  const [stats, setStats] = useState<DashboardStats | null>(null)

  const sourceFilters = [
    { value: '', label: t('featureExtensions.allSources') },
    { value: 'payment', label: t('featureExtensions.paymentOrder') },
    { value: 'redeem', label: t('featureExtensions.redeemOrder') }
  ]

  useEffect(() => {
    void loadDashboard()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    void loadDashboard()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [days, sourceFilter])

  async function loadDashboard() {
    setLoading(true)
    try {
      const res = await adminPaymentAPI.getDashboard(days, sourceFilter)
      setStats(res.data)
    } catch (err: unknown) {
      toast.error(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-end">
        <div className="flex flex-wrap items-center justify-end gap-2">
          <Select
            value={sourceFilter}
            options={sourceFilters}
            searchable={false}
            className="w-36"
            onValueChange={(next) => setSourceFilter(String(next ?? ''))}
          />
          <div className="flex rounded-lg border border-gray-200 dark:border-dark-600">
            {DAYS_OPTIONS.map((option) => (
              <button
                key={option}
                type="button"
                className={`px-3 py-1.5 text-xs font-medium transition-colors first:rounded-l-lg last:rounded-r-lg surface-control ${
                  days === option
                    ? 'bg-primary-600 text-white'
                    : 'text-gray-600 hover:surface-hover dark:text-gray-300 dark:hover:surface-hover'
                }`}
                onClick={() => setDays(option)}
              >
                {option}
                {t('payment.admin.daySuffix')}
              </button>
            ))}
          </div>
          <Button variant="secondary" onClick={() => void loadDashboard()} disabled={loading} title={t('common.refresh')}>
            <Icon name="refresh" size="md" className={loading ? 'animate-spin' : ''} />
          </Button>
        </div>
      </div>

      {loading ? (
        <div className="flex items-center justify-center py-12">
          <LoadingSpinner />
        </div>
      ) : (
        stats && (
          <>
            <OrderStatsCards stats={stats} />
            <DailyRevenueChart data={stats.daily_series || []} loading={loading} />
            <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
              <div className="card p-4">
                <h3 className="mb-4 text-sm font-semibold text-gray-900 dark:text-white">
                  {t('payment.admin.paymentDistribution')}
                </h3>
                {!stats.payment_methods?.length ? (
                  <div className="flex h-32 items-center justify-center text-sm surface-muted">
                    {t('payment.admin.noData')}
                  </div>
                ) : (
                  <div className="space-y-3">
                    {stats.payment_methods.map((method) => (
                      <div key={method.type} className="flex items-center justify-between">
                        <div className="flex items-center gap-2">
                          <span
                            className={`console-data-mark inline-block h-3 w-3 rounded-full ${methodColor(method.type)}`}
                          />
                          <span className="text-sm surface-muted">
                            {t('payment.methods.' + method.type, method.type)}
                          </span>
                        </div>
                        <div className="space-y-1 text-right">
                          {sortedAmounts(method.amount).map(([currency, amount]) => (
                            <span
                              key={currency}
                              className="block text-sm font-medium text-gray-900 dark:text-white"
                            >
                              {formatMoney(currency, amount)}
                            </span>
                          ))}
                          <span className="ml-2 text-xs surface-muted">({method.count})</span>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
              <div className="card p-4">
                <h3 className="mb-4 text-sm font-semibold text-gray-900 dark:text-white">{t('payment.admin.topUsers')}</h3>
                {!hasTopUsers(stats.top_users) ? (
                  <div className="flex h-32 items-center justify-center text-sm surface-muted">
                    {t('payment.admin.noData')}
                  </div>
                ) : (
                  <div className="space-y-2">
                    {sortedTopUsers(stats.top_users).map(([currency, users]) => (
                      <div key={currency} className="space-y-2">
                        <p className="text-xs font-semibold surface-muted">{currency}</p>
                        {users.map((user, idx) => (
                          <div
                            key={user.user_id}
                            className="flex flex-wrap items-center justify-between gap-2 rounded-lg px-3 py-2 hover:surface-hover"
                          >
                            <div className="flex min-w-0 flex-1 items-center gap-3">
                              <span
                                className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-xs font-bold ${rankClass(idx)}`}
                              >
                                {idx + 1}
                              </span>
                              <span className="min-w-0 break-all text-sm surface-muted">{user.email}</span>
                            </div>
                            <span className="text-sm font-medium text-gray-900 dark:text-white">
                              {formatMoney(currency, user.amount)}
                            </span>
                          </div>
                        ))}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          </>
        )
      )}
    </div>
  )
}
