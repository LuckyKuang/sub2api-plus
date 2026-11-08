import { useTranslation } from 'react-i18next'
import type { AdminUsageStatsResponse } from '@/api/admin/usage'
import type { UsageStatsResponse } from '@/types'
import { formatTokenShare, sumTokenBuckets } from '@/utils/tokenShare'

export interface UsageStatsCardsProps {
  stats: (AdminUsageStatsResponse | UsageStatsResponse) | null
  showAccountCost?: boolean
  strikeStandardCost?: boolean
}

function formatDuration(ms: number): string {
  return ms < 1000 ? `${ms.toFixed(0)}ms` : `${(ms / 1000).toFixed(2)}s`
}

function formatTokens(value: number): string {
  if (value >= 1e9) return `${(value / 1e9).toFixed(2)}B`
  if (value >= 1e6) return `${(value / 1e6).toFixed(2)}M`
  if (value >= 1e3) return `${(value / 1e3).toFixed(2)}K`
  return value.toLocaleString()
}

export function UsageStatsCards({
  stats,
  showAccountCost = true,
  strikeStandardCost = false
}: UsageStatsCardsProps) {
  const { t } = useTranslation()

  const totalAccountCost = (stats as (AdminUsageStatsResponse & { total_account_cost?: number }) | null)
    ?.total_account_cost ?? null
  const totalCacheTokens = sumTokenBuckets(stats?.total_cache_creation_tokens, stats?.total_cache_read_tokens)
  const tokenShare = (tokens: number | null | undefined): string | null =>
    formatTokenShare(tokens, stats?.total_tokens)

  return (
    <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
      <div className="card p-4">
        <p className="text-xs font-medium surface-muted">{t('usage.totalRequests')}</p>
        <p className="mt-1 text-2xl font-bold tabular-nums">{stats?.total_requests?.toLocaleString() || '0'}</p>
        <p className="mt-1 text-xs surface-muted">{t('usage.inSelectedRange')}</p>
      </div>
      <div className="card p-4">
        <p className="text-xs font-medium surface-muted">{t('usage.totalTokens')}</p>
        <p className="mt-1 text-2xl font-bold tabular-nums">{formatTokens(stats?.total_tokens || 0)}</p>
        <p className="mt-1 flex flex-wrap items-center gap-x-1 text-xs surface-muted">
          <span>
            {t('usage.in')}: {formatTokens(stats?.total_input_tokens || 0)}{' '}
            {tokenShare(stats?.total_input_tokens) && (
              <span className="surface-muted">({tokenShare(stats?.total_input_tokens)})</span>
            )}
          </span>
          <span>/</span>
          <span>
            {t('usage.out')}: {formatTokens(stats?.total_output_tokens || 0)}{' '}
            {tokenShare(stats?.total_output_tokens) && (
              <span className="surface-muted">({tokenShare(stats?.total_output_tokens)})</span>
            )}
          </span>
          <span>/</span>
          <span className="group relative inline-flex cursor-help items-center gap-0.5" tabIndex={0}>
            <span>
              {t('usage.cacheTotal')}: {formatTokens(totalCacheTokens)}{' '}
              {tokenShare(totalCacheTokens) && <span className="surface-muted">({tokenShare(totalCacheTokens)})</span>}
            </span>
            <svg className="h-3.5 w-3.5 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth="2"
                d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
            <span className="pointer-events-none absolute left-1/2 top-full z-30 mt-2 hidden w-56 -translate-x-1/2 rounded-lg border border-gray-200 p-3 text-left text-xs shadow-lg group-hover:block group-focus:block dark:border-dark-600 surface-panel surface-muted">
              <span className="mb-2 block font-medium text-gray-900 dark:text-white">{t('usage.cacheBreakdown')}</span>
              <span className="flex items-center justify-between gap-3">
                <span>{t('usage.cacheCreationTokensLabel')}</span>
                <span className="tabular-nums">
                  {formatTokens(stats?.total_cache_creation_tokens || 0)}{' '}
                  {tokenShare(stats?.total_cache_creation_tokens) && (
                    <span className="surface-muted">({tokenShare(stats?.total_cache_creation_tokens)})</span>
                  )}
                </span>
              </span>
              <span className="mt-1 flex items-center justify-between gap-3">
                <span>{t('usage.cacheReadTokensLabel')}</span>
                <span className="tabular-nums">
                  {formatTokens(stats?.total_cache_read_tokens || 0)}{' '}
                  {tokenShare(stats?.total_cache_read_tokens) && (
                    <span className="surface-muted">({tokenShare(stats?.total_cache_read_tokens)})</span>
                  )}
                </span>
              </span>
            </span>
          </span>
        </p>
      </div>
      <div className="card p-4">
        <p className="text-xs font-medium surface-muted">{t('usage.totalCost')}</p>
        <p className="mt-1 text-2xl font-bold tabular-nums text-green-600">
          ${(stats?.total_actual_cost || 0).toFixed(4)}
        </p>
        <p className="mt-1 text-xs surface-muted">
          {showAccountCost && totalAccountCost != null && (
            <>
              <span className="text-orange-500">
                {t('usage.accountCost')} ${totalAccountCost.toFixed(4)}
              </span>
              <span> · </span>
            </>
          )}
          <span>
            {t('usage.standardCost')}
            <span className={strikeStandardCost ? 'line-through' : undefined}>
              ${(stats?.total_cost || 0).toFixed(4)}
            </span>
          </span>
        </p>
      </div>
      <div className="card p-4">
        <p className="text-xs font-medium surface-muted">{t('usage.avgDuration')}</p>
        <p className="mt-1 text-2xl font-bold tabular-nums">{formatDuration(stats?.average_duration_ms || 0)}</p>
      </div>
    </div>
  )
}

export default UsageStatsCards
