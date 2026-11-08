import { useTranslation } from 'react-i18next'
import { formatCurrency, formatNumber } from '@/utils/format'
import type { WindowStats } from '@/types'

export interface AccountTodayStatsCellProps {
  stats?: WindowStats | null
  loading?: boolean
  error?: string | null
}

// Format large token numbers (e.g., 1234567 -> 1.23M)
const formatTokens = (tokens: number): string => {
  if (tokens >= 1000000) {
    return `${(tokens / 1000000).toFixed(2)}M`
  } else if (tokens >= 1000) {
    return `${(tokens / 1000).toFixed(1)}K`
  }
  return tokens.toString()
}

export function AccountTodayStatsCell({ stats = null, loading = false, error = null }: AccountTodayStatsCellProps) {
  const { t } = useTranslation()

  if (loading && !stats) {
    return (
      <div className="space-y-0.5">
        <div className="h-3 w-12 animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
        <div className="h-3 w-16 animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
        <div className="h-3 w-10 animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
      </div>
    )
  }

  if (error && !stats) {
    return <div className="text-xs text-red-500">{error}</div>
  }

  if (stats) {
    return (
      <div className="space-y-0.5 text-xs">
        {/* Requests */}
        <div className="flex items-center gap-1">
          <span className="surface-muted">{t('admin.accounts.stats.requests')}:</span>
          <span className="font-medium surface-muted">{formatNumber(stats.requests)}</span>
        </div>
        {/* Tokens */}
        <div className="flex items-center gap-1">
          <span className="surface-muted">{t('admin.accounts.stats.tokens')}:</span>
          <span className="font-medium surface-muted">{formatTokens(stats.tokens)}</span>
        </div>
        {/* Cost (Account) */}
        <div className="flex items-center gap-1">
          <span className="surface-muted">{t('usage.accountBilled')}:</span>
          <span className="font-medium text-emerald-600 dark:text-emerald-400">
            {formatCurrency(stats.cost)}
          </span>
        </div>
        {/* Cost (User/API Key) */}
        {stats.user_cost != null && (
          <div className="flex items-center gap-1">
            <span className="surface-muted">{t('usage.userBilled')}:</span>
            <span className="font-medium surface-muted">{formatCurrency(stats.user_cost)}</span>
          </div>
        )}
      </div>
    )
  }

  return <div className="text-xs surface-muted">-</div>
}

export default AccountTodayStatsCell
