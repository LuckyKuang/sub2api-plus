import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { EmptyState, Spinner } from '@/components/ui'
import { formatDateTime } from '@/utils/format'
import { usageMoney } from '@/utils/usageInspection'
import type { UsageLog } from '@/types'
import { formatTokenShare, sumTokenBuckets } from '@/utils/tokenShare'
import { Icon } from '../icons'
import { cn } from '@/lib/utils'

export interface UserDashboardRecentUsageProps {
  data: UsageLog[]
  loading: boolean
  startDate?: string
  endDate?: string
  className?: string
}

function requestCacheTokens(log: UsageLog): number {
  return sumTokenBuckets(log.cache_creation_tokens, log.cache_read_tokens)
}

function requestTokenTotal(log: UsageLog): number {
  return sumTokenBuckets(log.input_tokens, log.output_tokens, log.cache_creation_tokens, log.cache_read_tokens)
}

function requestTokenShare(log: UsageLog, tokens: number | null | undefined): string | null {
  return formatTokenShare(tokens, requestTokenTotal(log))
}

export function UserDashboardRecentUsage({ data, loading, startDate, endDate, className }: UserDashboardRecentUsageProps) {
  const { t } = useTranslation()

  return (
    <div className={cn('card', className)}>
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-gray-100 px-6 py-4 dark:border-dark-700">
        <h2 className="text-lg font-semibold text-gray-900 dark:text-white">{t('dashboard.recentUsage')}</h2>
        <span className="badge badge-gray">
          {startDate && endDate ? `${startDate} – ${endDate}` : t('dashboard.last7Days')}
        </span>
      </div>
      <div className="p-6">
        {loading && data.length === 0 ? (
          <div className="flex items-center justify-center py-12">
            <Spinner className="h-8 w-8 text-primary-500" />
          </div>
        ) : data.length === 0 ? (
          <div className="py-8">
            <EmptyState title={t('dashboard.noUsageRecords')} description={t('dashboard.startUsingApi')} />
          </div>
        ) : (
          <div className="space-y-3">
            {data.map((log) => (
              <div
                key={log.id}
                className="surface-inset flex flex-wrap items-center justify-between gap-3 rounded-xl p-4 transition-colors hover:surface-hover"
              >
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-gray-900 dark:text-white" title={log.model}>
                    {log.model}
                  </p>
                  <p className="text-xs surface-muted">{formatDateTime(log.created_at)}</p>
                </div>
                <div className="text-right">
                  <p className="text-sm font-semibold">
                    <span className="text-green-600 dark:text-green-400" title={t('dashboard.actual')}>
                      {usageMoney(log.actual_cost)}
                    </span>
                    <span className="font-normal surface-muted" title={t('dashboard.standard')}>
                      {' '}
                      / {usageMoney(log.total_cost)}
                    </span>
                  </p>
                  <p className="text-xs surface-muted">
                    {requestTokenTotal(log).toLocaleString()} Token
                    {requestTokenShare(log, log.input_tokens) && (
                      <> · {t('dashboard.input')} {requestTokenShare(log, log.input_tokens)}</>
                    )}
                    {requestTokenShare(log, log.output_tokens) && (
                      <> · {t('dashboard.output')} {requestTokenShare(log, log.output_tokens)}</>
                    )}
                    {requestTokenShare(log, requestCacheTokens(log)) && (
                      <> · {t('dashboard.cache')} {requestTokenShare(log, requestCacheTokens(log))}</>
                    )}
                  </p>
                </div>
              </div>
            ))}

            <Link
              to="/usage"
              className="flex items-center justify-center gap-2 py-3 text-sm font-medium text-primary-600 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
            >
              {t('dashboard.viewAllUsage')}
              <Icon name="arrowRight" size="sm" />
            </Link>
          </div>
        )}
      </div>
    </div>
  )
}

export default UserDashboardRecentUsage
