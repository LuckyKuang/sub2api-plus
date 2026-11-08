import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import type { UserBreakdownItem } from '@/types'
import { formatTokenShare } from '@/utils/tokenShare'
import { Spinner } from '../ui'

export interface UserBreakdownRowsProps {
  items: UserBreakdownItem[]
  loading?: boolean
  showAccountCost?: boolean
  totalTokens?: number
}

function formatTokens(value: number): string {
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(2)}K`
  return value.toLocaleString()
}

function formatCost(value: number | undefined | null): string {
  if (value == null) return '0.0000'
  if (value >= 1000) return (value / 1000).toFixed(2) + 'K'
  else if (value >= 1) return value.toFixed(2)
  else if (value >= 0.01) return value.toFixed(3)
  return value.toFixed(4)
}

export function UserBreakdownRows({
  items,
  loading = false,
  showAccountCost = true,
  totalTokens = 0
}: UserBreakdownRowsProps) {
  const { t } = useTranslation()

  const tokenShare = (tokens: number | null | undefined): string | null =>
    formatTokenShare(tokens, totalTokens)

  const columnCount = showAccountCost ? 6 : 5

  if (loading) {
    return (
      <tr>
        <td
          colSpan={columnCount}
          data-record-edges="b l r bl br"
          className="py-3"
        >
          <div className="flex justify-center">
            <Spinner />
          </div>
        </td>
      </tr>
    )
  }

  if (items.length === 0) {
    return (
      <tr>
        <td
          colSpan={columnCount}
          data-record-edges="b l r bl br"
          className="py-2 text-center text-xs surface-muted"
        >
          {t('admin.dashboard.noDataAvailable')}
        </td>
      </tr>
    )
  }

  return (
    <>
      {items.map((user, index) => (
        <tr key={user.user_id} className="text-xs">
          <td
            data-record-edges={index === items.length - 1 ? 'b l bl' : 'l'}
            className="py-1 pl-6 surface-muted"
          >
            <span className="block max-w-[120px] truncate" title={user.email}>
              {user.email || `User #${user.user_id}`}
            </span>
          </td>
          <td
            data-record-edges={index === items.length - 1 ? 'b' : ''}
            className="py-1 text-right surface-muted"
          >
            {user.requests.toLocaleString()}
          </td>
          <td
            data-record-edges={index === items.length - 1 ? 'b' : ''}
            className="py-1 text-right surface-muted"
          >
            {formatTokens(user.total_tokens)}{' '}
            {tokenShare(user.total_tokens) && <span className="surface-muted">({tokenShare(user.total_tokens)})</span>}
          </td>
          <td
            data-record-edges={index === items.length - 1 ? 'b' : ''}
            className="py-1 text-right text-green-600 dark:text-green-400"
          >
            ${formatCost(user.actual_cost)}
          </td>
          {showAccountCost && (
            <td
              data-record-edges={index === items.length - 1 ? 'b' : ''}
              className="py-1 text-right text-orange-500 dark:text-orange-400"
            >
              ${formatCost(user.account_cost)}
            </td>
          )}
          <td
            data-record-edges={index === items.length - 1 ? 'b r br' : 'r'}
            className={cn('py-1 text-right surface-muted')}
          >
            ${formatCost(user.cost)}
          </td>
        </tr>
      ))}
    </>
  )
}
