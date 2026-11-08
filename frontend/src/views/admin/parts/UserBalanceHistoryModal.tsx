import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/Icon'
import { Modal } from '@/components/ui'
import { Select, type SelectOption } from '@/components/ui'
import { adminAPI, type BalanceHistoryItem } from '@/api/admin'
import { formatDateTime } from '@/utils/format'
import type { AdminUser } from '@/types'

export interface UserBalanceHistoryModalProps {
  show: boolean
  user: AdminUser | null
  hideActions?: boolean
  onClose?: () => void
  onDeposit?: () => void
  onWithdraw?: () => void
}

const PAGE_SIZE = 15

export function UserBalanceHistoryModal({
  show,
  user,
  hideActions = false,
  onClose,
  onDeposit,
  onWithdraw
}: UserBalanceHistoryModalProps) {
  const { t } = useTranslation()

  const [history, setHistory] = useState<BalanceHistoryItem[]>([])
  const [loading, setLoading] = useState(false)
  const [currentPage, setCurrentPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [totalRecharged, setTotalRecharged] = useState(0)
  const [typeFilter, setTypeFilter] = useState('')

  const totalPages = Math.ceil(total / PAGE_SIZE) || 1

  const typeOptions: SelectOption[] = [
    { value: '', label: t('admin.users.allTypes') },
    { value: 'balance', label: t('admin.users.typeBalance') },
    { value: 'affiliate_balance', label: t('admin.users.typeAffiliateBalance') },
    { value: 'admin_balance', label: t('admin.users.typeAdminBalance') },
    { value: 'concurrency', label: t('admin.users.typeConcurrency') },
    { value: 'admin_concurrency', label: t('admin.users.typeAdminConcurrency') },
    { value: 'subscription', label: t('admin.users.typeSubscription') }
  ]

  useEffect(() => {
    if (show && user) {
      setTypeFilter('')
      void loadHistory(1)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [show, user])

  const loadHistory = async (page: number) => {
    if (!user) return
    setLoading(true)
    setCurrentPage(page)
    try {
      const res = await adminAPI.users.getUserBalanceHistory(user.id, page, PAGE_SIZE, typeFilter || undefined)
      setHistory(res.items || [])
      setTotal(res.total || 0)
      setTotalRecharged(res.total_recharged || 0)
    } catch (error) {
      console.error('Failed to load balance history:', error)
    } finally {
      setLoading(false)
    }
  }

  // Helper: check if admin type
  const isAdminType = (type: string) => type === 'admin_balance' || type === 'admin_concurrency'

  // Helper: check if balance type (includes admin_balance)
  const isBalanceType = (type: string) =>
    type === 'balance' || type === 'admin_balance' || type === 'affiliate_balance'

  // Helper: check if subscription type
  const isSubscriptionType = (type: string) => type === 'subscription'

  const getIconName = (item: BalanceHistoryItem): 'dollar' | 'badge' | 'bolt' => {
    if (isBalanceType(item.type)) return 'dollar'
    if (isSubscriptionType(item.type)) return 'badge'
    return 'bolt' // concurrency
  }

  const getIconBg = (item: BalanceHistoryItem): string => {
    if (isBalanceType(item.type)) {
      return item.value >= 0 ? 'bg-emerald-100/[0.15] dark:bg-emerald-900/30' : 'bg-red-100/[0.15] dark:bg-red-900/30'
    }
    if (isSubscriptionType(item.type)) return 'bg-purple-100/[0.15] dark:bg-purple-900/30'
    return item.value >= 0 ? 'bg-blue-100/[0.15] dark:bg-blue-900/30' : 'bg-orange-100/[0.15] dark:bg-orange-900/30'
  }

  const getIconColor = (item: BalanceHistoryItem): string => {
    if (isBalanceType(item.type)) {
      return item.value >= 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400'
    }
    if (isSubscriptionType(item.type)) return 'text-purple-600 dark:text-purple-400'
    return item.value >= 0 ? 'text-blue-600 dark:text-blue-400' : 'text-orange-600 dark:text-orange-400'
  }

  const getValueColor = (item: BalanceHistoryItem): string => {
    if (isBalanceType(item.type)) {
      return item.value >= 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400'
    }
    if (isSubscriptionType(item.type)) return 'text-purple-600 dark:text-purple-400'
    return item.value >= 0 ? 'text-blue-600 dark:text-blue-400' : 'text-orange-600 dark:text-orange-400'
  }

  const getItemTitle = (item: BalanceHistoryItem): string => {
    switch (item.type) {
      case 'balance':
        return t('redeem.balanceAddedRedeem')
      case 'affiliate_balance':
        return t('redeem.balanceAddedAffiliate')
      case 'admin_balance':
        return item.value >= 0 ? t('redeem.balanceAddedAdmin') : t('redeem.balanceDeductedAdmin')
      case 'concurrency':
        return t('redeem.concurrencyAddedRedeem')
      case 'admin_concurrency':
        return item.value >= 0 ? t('redeem.concurrencyAddedAdmin') : t('redeem.concurrencyReducedAdmin')
      case 'subscription':
        return t('redeem.subscriptionAssigned')
      default:
        return t('common.unknown')
    }
  }

  const formatValue = (item: BalanceHistoryItem): string => {
    if (isBalanceType(item.type)) {
      const sign = item.value >= 0 ? '+' : ''
      return `${sign}$${item.value.toFixed(2)}`
    }
    if (isSubscriptionType(item.type)) {
      const days = item.validity_days || Math.round(item.value)
      const groupName = item.group?.name || ''
      return groupName ? `${days}d - ${groupName}` : `${days}d`
    }
    // concurrency types
    const sign = item.value >= 0 ? '+' : ''
    return `${sign}${item.value}`
  }

  return (
    <Modal
      open={show}
      onClose={onClose}
      title={t('admin.users.balanceHistoryTitle')}
      size="full"
      closeOnOverlayClick
    >
      {user && (
        <div className="space-y-4">
          {/* User header: two-row layout with full user info */}
          <div className="rounded-xl surface-inset p-4">
            {/* Row 1: avatar + email/username/created_at (left) + current balance (right) */}
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-full bg-primary-100/[0.15] dark:bg-primary-900/30">
                <span className="text-lg font-medium text-primary-700 dark:text-primary-300">
                  {user.email.charAt(0).toUpperCase()}
                </span>
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <p className="truncate font-medium text-gray-900 dark:text-white">{user.email}</p>
                  {user.deleted_at && (
                    <span className="flex-shrink-0 inline-flex items-center rounded bg-rose-100/[0.15] px-1 py-px text-[10px] font-medium leading-tight text-rose-600 ring-1 ring-inset ring-rose-200 dark:bg-rose-500/20 dark:text-rose-400 dark:ring-rose-500/30">
                      {t('admin.usage.userDeletedBadge')}
                    </span>
                  )}
                  {user.username && (
                    <span className="flex-shrink-0 rounded bg-primary-50/[0.15] px-1.5 py-0.5 text-xs text-primary-600 dark:bg-primary-900/20 dark:text-primary-400">
                      {user.username}
                    </span>
                  )}
                </div>
                <p className="text-xs surface-muted">
                  {t('admin.users.createdAt')}: {formatDateTime(user.created_at)}
                </p>
              </div>
              {/* Current balance: prominent display on the right */}
              <div className="flex-shrink-0 text-right">
                <p className="text-xs surface-muted">{t('admin.users.currentBalance')}</p>
                <p className="text-xl font-bold text-gray-900 dark:text-white">
                  ${user.balance?.toFixed(2) || '0.00'}
                </p>
              </div>
            </div>
            {/* Row 2: notes + total recharged */}
            <div className="mt-2.5 flex items-center justify-between border-t border-gray-200/60 pt-2.5 dark:border-dark-600/60">
              <p className="min-w-0 flex-1 truncate text-xs surface-muted" title={user.notes || ''}>
                {user.notes ? `${t('admin.users.notes')}: ${user.notes}` : '\u00A0'}
              </p>
              <p className="ml-4 flex-shrink-0 text-xs surface-muted">
                {t('admin.users.totalRecharged')}:{' '}
                <span className="font-semibold text-emerald-600 dark:text-emerald-400">${totalRecharged.toFixed(2)}</span>
              </p>
            </div>
          </div>

          {/* Type filter + Action buttons */}
          <div className="flex items-center gap-3">
            <Select
              value={typeFilter}
              options={typeOptions}
              className="w-56"
              onValueChange={(value) => {
                setTypeFilter(String(value ?? ''))
                void loadHistory(1)
              }}
            />
            {!hideActions && (
              <button
                type="button"
                onClick={onDeposit}
                className="flex items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm transition-colors hover:surface-hover dark:border-dark-600 surface-control surface-muted"
              >
                <Icon name="plus" size="sm" className="text-emerald-500" strokeWidth={2} />
                {t('admin.users.deposit')}
              </button>
            )}
            {!hideActions && (
              <button
                type="button"
                onClick={onWithdraw}
                className="flex items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm transition-colors hover:surface-hover dark:border-dark-600 surface-control surface-muted"
              >
                <svg className="h-4 w-4 text-amber-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M20 12H4" />
                </svg>
                {t('admin.users.withdraw')}
              </button>
            )}
          </div>

          {loading && (
            <div className="flex justify-center py-8">
              <svg className="h-8 w-8 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                <path
                  className="opacity-75"
                  fill="currentColor"
                  d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                />
              </svg>
            </div>
          )}

          {!loading && history.length === 0 && (
            <div className="py-8 text-center">
              <p className="text-sm surface-muted">{t('admin.users.noBalanceHistory')}</p>
            </div>
          )}

          {!loading && history.length > 0 && (
            <div className="max-h-[28rem] space-y-3 overflow-y-auto">
              {history.map((item) => (
                <div
                  key={item.id}
                  className="rounded-xl border border-gray-200 surface-inset p-4 dark:border-dark-600"
                >
                  <div className="flex items-start justify-between">
                    <div className="flex items-start gap-3">
                      <div
                        className={`flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-lg ${getIconBg(item)}`}
                      >
                        <Icon name={getIconName(item)} size="sm" className={getIconColor(item)} />
                      </div>
                      <div>
                        <p className="text-sm font-medium text-gray-900 dark:text-white">{getItemTitle(item)}</p>
                        {item.notes && (
                          <p className="mt-0.5 text-xs surface-muted" title={item.notes}>
                            {item.notes.length > 60 ? `${item.notes.substring(0, 55)}...` : item.notes}
                          </p>
                        )}
                        <p className="mt-0.5 text-xs surface-muted">
                          {formatDateTime(item.used_at || item.created_at)}
                        </p>
                      </div>
                    </div>
                    <div className="text-right">
                      <p className={`text-sm font-semibold ${getValueColor(item)}`}>{formatValue(item)}</p>
                      {isAdminType(item.type) ? (
                        <p className="text-xs surface-muted">{t('redeem.adminAdjustment')}</p>
                      ) : (
                        <p className="font-mono text-xs surface-muted">{item.code.slice(0, 8)}...</p>
                      )}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}

          {totalPages > 1 && (
            <div className="pagination-card card flex items-center justify-center gap-2 px-4 py-3">
              <button
                type="button"
                disabled={currentPage <= 1}
                className="btn btn-secondary px-3 py-1 text-sm"
                onClick={() => void loadHistory(currentPage - 1)}
              >
                {t('pagination.previous')}
              </button>
              <span className="text-sm surface-muted">
                {currentPage} / {totalPages}
              </span>
              <button
                type="button"
                disabled={currentPage >= totalPages}
                className="btn btn-secondary px-3 py-1 text-sm"
                onClick={() => void loadHistory(currentPage + 1)}
              >
                {t('pagination.next')}
              </button>
            </div>
          )}
        </div>
      )}
    </Modal>
  )
}

export default UserBalanceHistoryModal
