import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { redeemAPI, authAPI, type RedeemHistoryItem } from '@/api'
import { useSubscriptionStore } from '@/stores/subscriptions'
import { Select, toast } from '@/components/ui'
import { formatDateTime } from '@/utils/format'
import { useLocaleRefresh as subscribeLocaleRefresh } from '@/utils/localeRefresh'
import { Icon } from './icons'
import { useUserView } from './useUserView'

interface RedeemResult {
  message: string
  type: string
  value: number
  new_balance?: number
  new_concurrency?: number
  group_name?: string
  validity_days?: number
}

const redeemRules = [
  { title: 'redeem.ruleTitles.once', body: 'redeem.codeRule1', icon: 'shield' },
  { title: 'redeem.ruleTitles.benefits', body: 'redeem.codeRule2', icon: 'gift' },
  { title: 'redeem.ruleTitles.support', body: 'redeem.codeRule3', icon: 'chat' },
  { title: 'redeem.ruleTitles.instant', body: 'redeem.codeRule4', icon: 'checkCircle' }
] as const

export default function RedeemView() {
  const { t } = useTranslation()
  const authStore = useUserView()
  const user = authStore.user

  const [redeemCode, setRedeemCode] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [redeemResult, setRedeemResult] = useState<RedeemResult | null>(null)
  const [errorMessage, setErrorMessage] = useState('')

  const [history, setHistory] = useState<RedeemHistoryItem[]>([])
  const [loadingHistory, setLoadingHistory] = useState(false)
  const [historyPage, setHistoryPage] = useState(1)
  const [historyPageSize, setHistoryPageSize] = useState(20)
  const [historyTotal, setHistoryTotal] = useState(0)
  const historyRequest = useRef(0)
  const loadedHistoryPageSize = useRef(20)
  const [contactInfo, setContactInfo] = useState('')
  const historyViewport = useRef<HTMLDivElement>(null)

  const historyPageCount = Math.max(1, Math.ceil(historyTotal / historyPageSize))

  const isBalanceType = (type: string) => type === 'balance' || type === 'admin_balance'
  const isSubscriptionType = (type: string) => type === 'subscription'
  const isAdminAdjustment = (type: string) => type === 'admin_balance' || type === 'admin_concurrency'

  const getHistoryItemTitle = (item: RedeemHistoryItem) => {
    if (item.type === 'balance') return t('redeem.balanceAddedRedeem')
    if (item.type === 'admin_balance') {
      return item.value >= 0 ? t('redeem.balanceAddedAdmin') : t('redeem.balanceDeductedAdmin')
    }
    if (item.type === 'concurrency') return t('redeem.concurrencyAddedRedeem')
    if (item.type === 'admin_concurrency') {
      return item.value >= 0 ? t('redeem.concurrencyAddedAdmin') : t('redeem.concurrencyReducedAdmin')
    }
    if (item.type === 'subscription') return t('redeem.subscriptionAssigned')
    return t('common.unknown')
  }

  const formatHistoryValue = (item: RedeemHistoryItem) => {
    if (isBalanceType(item.type)) {
      const sign = item.value >= 0 ? '+' : ''
      return `${sign}$${item.value.toFixed(2)}`
    }
    if (isSubscriptionType(item.type)) {
      // 订阅类型显示有效天数和分组名称
      const days = item.validity_days || Math.round(item.value)
      const groupName = item.group?.name || ''
      return groupName ? `${days}${t('redeem.days')} - ${groupName}` : `${days}${t('redeem.days')}`
    }
    const sign = item.value >= 0 ? '+' : ''
    return `${sign}${item.value} ${t('redeem.requests')}`
  }

  const fetchHistory = async (page = 1) => {
    const request = ++historyRequest.current
    const pageSize = historyPageSize
    setLoadingHistory(true)
    try {
      const result = await redeemAPI.getHistory(page, pageSize)
      if (request !== historyRequest.current) return
      setHistory(result.items)
      setHistoryTotal(result.total)
      setHistoryPage(page)
      setHistoryPageSize(pageSize)
      loadedHistoryPageSize.current = pageSize
      if (request === historyRequest.current && historyViewport.current) historyViewport.current.scrollTop = 0
    } catch (error) {
      if (request !== historyRequest.current) return
      setHistoryPageSize(loadedHistoryPageSize.current)
      toast.error(t('redeem.historyLoadFailed'))
      console.error('Failed to fetch history:', error)
    } finally {
      if (request === historyRequest.current) setLoadingHistory(false)
    }
  }

  const handleRedeem = async () => {
    if (!redeemCode.trim()) {
      toast.error(t('redeem.pleaseEnterCode'))
      return
    }

    setSubmitting(true)
    setErrorMessage('')
    setRedeemResult(null)

    try {
      const result = await redeemAPI.redeem(redeemCode.trim())

      setRedeemResult(result)

      // Refresh user data to get updated balance/concurrency
      try {
        await authStore.refreshUser()
      } catch (error) {
        console.error('Failed to refresh user after redeem:', error)
        toast.warning(t('redeem.userRefreshFailed'))
      }

      // If subscription type, immediately refresh subscription status
      if (result.type === 'subscription') {
        try {
          await useSubscriptionStore.getState().fetchActiveSubscriptions(true) // force refresh
        } catch (error) {
          console.error('Failed to refresh subscriptions after redeem:', error)
          toast.warning(t('redeem.subscriptionRefreshFailed'))
        }
      }

      // Clear the input
      setRedeemCode('')

      // Refresh history
      await fetchHistory()

      // Show success toast
      toast.success(t('redeem.codeRedeemSuccess'))
    } catch (error: unknown) {
      setErrorMessage(
        ((error as { response?: { data?: { detail?: string } } }).response?.data?.detail ??
          t('redeem.failedToRedeem')) as string
      )

      toast.error(t('redeem.redeemFailed'))
    } finally {
      setSubmitting(false)
    }
  }

  useEffect(() => {
    void fetchHistory()
    void (async () => {
      try {
        const settings = await authAPI.getPublicSettings()
        setContactInfo(settings.contact_info || '')
      } catch (error) {
        console.error('Failed to load contact info:', error)
      }
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Plain subscription helper (returns a disposer) despite its `use*` name.
  const fetchHistoryRef = useRef(fetchHistory)
  fetchHistoryRef.current = fetchHistory
  const historyPageRef = useRef(historyPage)
  historyPageRef.current = historyPage
  useEffect(
    () =>
      subscribeLocaleRefresh(() => fetchHistoryRef.current(historyPageRef.current)),
    []
  )

  const iconClassFor = (item: RedeemHistoryItem) => {
    if (isBalanceType(item.type)) {
      return item.value >= 0
        ? 'bg-emerald-100/[0.15] dark:bg-emerald-900/30'
        : 'bg-red-100/[0.15] dark:bg-red-900/30'
    }
    if (isSubscriptionType(item.type)) return 'bg-purple-100/[0.15] dark:bg-purple-900/30'
    return item.value >= 0 ? 'bg-blue-100/[0.15] dark:bg-blue-900/30' : 'bg-orange-100/[0.15] dark:bg-orange-900/30'
  }

  const iconColorClass = (item: RedeemHistoryItem) => {
    if (isBalanceType(item.type)) {
      return item.value >= 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400'
    }
    if (isSubscriptionType(item.type)) return 'text-purple-600 dark:text-purple-400'
    return item.value >= 0 ? 'text-blue-600 dark:text-blue-400' : 'text-orange-600 dark:text-orange-400'
  }

  return (
    <div className="workspace-page redeem-workspace">
      <div className="redeem-top-row workspace-fixed">
        <div className="card redeem-balance-card overflow-hidden">
          <div className="redeem-balance">
            <div className="surface-inset mb-4 inline-flex h-16 w-16 items-center justify-center rounded-2xl">
              <Icon name="creditCard" size="xl" className="text-primary-600 dark:text-primary-400" />
            </div>
            <p className="text-sm font-medium surface-muted">{t('redeem.currentBalance')}</p>
            <p className="mt-2 text-4xl font-bold">${user?.balance?.toFixed(2) || '0.00'}</p>
            <p className="mt-2 text-sm surface-muted">
              {t('redeem.concurrency')}: {user?.concurrency || 0} {t('redeem.requests')}
            </p>
          </div>
        </div>

        <div className="card redeem-form-card">
          <div className="p-6">
            <form
              onSubmit={(event) => {
                event.preventDefault()
                void handleRedeem()
              }}
              className="space-y-5"
            >
              <div>
                <label htmlFor="code" className="input-label">
                  {t('redeem.redeemCodeLabel')}
                </label>
                <div className="relative mt-1">
                  <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-4">
                    <Icon name="gift" size="md" className="surface-muted" />
                  </div>
                  <input
                    id="code"
                    value={redeemCode}
                    onChange={(event) => setRedeemCode(event.target.value)}
                    type="text"
                    required
                    placeholder={t('redeem.redeemCodePlaceholder')}
                    disabled={submitting}
                    className="input py-3 pl-12 text-lg"
                  />
                </div>
                <p className="input-hint">{t('redeem.redeemCodeHint')}</p>
              </div>

              <button type="submit" disabled={!redeemCode || submitting} className="btn btn-primary w-full py-3">
                {submitting ? (
                  <svg className="-ml-1 mr-2 h-5 w-5 animate-spin" fill="none" viewBox="0 0 24 24">
                    <circle
                      className="opacity-25"
                      cx="12"
                      cy="12"
                      r="10"
                      stroke="currentColor"
                      strokeWidth="4"
                    />
                    <path
                      className="opacity-75"
                      fill="currentColor"
                      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                    />
                  </svg>
                ) : (
                  <Icon name="checkCircle" size="md" className="mr-2" />
                )}
                {submitting ? t('redeem.redeeming') : t('redeem.redeemButton')}
              </button>
            </form>
          </div>
        </div>
      </div>

      {redeemResult && (
        <div className="card workspace-fixed redeem-feedback border-emerald-200 bg-emerald-50/[0.15] dark:border-emerald-800/50 dark:bg-emerald-900/20">
          <div className="p-6">
            <div className="flex items-start gap-4">
              <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl bg-emerald-100/[0.15] dark:bg-emerald-900/30">
                <Icon name="checkCircle" size="md" className="text-emerald-600 dark:text-emerald-400" />
              </div>
              <div className="flex-1">
                <h3 className="text-sm font-semibold text-emerald-800 dark:text-emerald-300">
                  {t('redeem.redeemSuccess')}
                </h3>
                <div className="mt-2 text-sm text-emerald-700 dark:text-emerald-400">
                  <p>{redeemResult.message}</p>
                  <div className="mt-3 space-y-1">
                    {redeemResult.type === 'balance' && (
                      <p className="font-medium">
                        {t('redeem.added')}: ${redeemResult.value.toFixed(2)}
                      </p>
                    )}
                    {redeemResult.type === 'concurrency' && (
                      <p className="font-medium">
                        {t('redeem.added')}: {redeemResult.value} {t('redeem.concurrentRequests')}
                      </p>
                    )}
                    {redeemResult.type === 'subscription' && (
                      <p className="font-medium">
                        {t('redeem.subscriptionAssigned')}
                        {redeemResult.group_name && <> - {redeemResult.group_name}</>}
                        {redeemResult.validity_days && (
                          <>({t('redeem.subscriptionDays', { days: redeemResult.validity_days })})</>
                        )}
                      </p>
                    )}
                    {redeemResult.new_balance !== undefined && (
                      <p>
                        {t('redeem.newBalance')}:
                        <span className="font-semibold">${redeemResult.new_balance.toFixed(2)}</span>
                      </p>
                    )}
                    {redeemResult.new_concurrency !== undefined && (
                      <p>
                        {t('redeem.newConcurrency')}:
                        <span className="font-semibold">
                          {redeemResult.new_concurrency} {t('redeem.requests')}
                        </span>
                      </p>
                    )}
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}

      {errorMessage && (
        <div className="card workspace-fixed redeem-feedback border-red-200 bg-red-50/[0.15] dark:border-red-800/50 dark:bg-red-900/20">
          <div className="p-6">
            <div className="flex items-start gap-4">
              <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl bg-red-100/[0.15] dark:bg-red-900/30">
                <Icon name="exclamationCircle" size="md" className="text-red-600 dark:text-red-400" />
              </div>
              <div className="flex-1">
                <h3 className="text-sm font-semibold text-red-800 dark:text-red-300">{t('redeem.redeemFailed')}</h3>
                <p className="mt-2 text-sm text-red-700 dark:text-red-400">{errorMessage}</p>
              </div>
            </div>
          </div>
        </div>
      )}

      <section className="workspace-fixed redeem-guidance" aria-label={t('redeem.aboutCodes')}>
        <h3 className="mb-3 text-sm font-semibold">{t('redeem.aboutCodes')}</h3>
        <ul className="redeem-guide-grid">
          {redeemRules.map((rule, index) => (
            <li key={rule.body} className="card redeem-guide-card">
              <div className="redeem-guide-top">
                <span className="redeem-guide-icon">
                  <Icon name={rule.icon} size="md" />
                </span>
                <span className="redeem-guide-step">{String(index + 1).padStart(2, '0')}</span>
              </div>
              <h4 className="text-sm font-semibold">{t(rule.title)}</h4>
              <p className="text-[13px] leading-relaxed surface-muted">
                {t(rule.body)}
                {index === 2 && contactInfo && (
                  <span className="surface-inset ml-1.5 inline-flex rounded-md px-2 py-0.5 text-xs font-medium">
                    {contactInfo}
                  </span>
                )}
              </p>
            </li>
          ))}
        </ul>
      </section>

      <div className="card workspace-table-card redeem-history-card">
        <div className="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white">{t('redeem.recentActivity')}</h2>
        </div>
        <div ref={historyViewport} className="redeem-history workspace-table-viewport p-6">
          {loadingHistory ? (
            <div className="flex items-center justify-center py-8">
              <svg className="h-6 w-6 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                <path
                  className="opacity-75"
                  fill="currentColor"
                  d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                />
              </svg>
            </div>
          ) : history.length > 0 ? (
            <div className="space-y-3">
              {history.map((item) => (
                <div key={item.id} className="surface-inset flex items-center justify-between rounded-xl p-4">
                  <div className="flex items-center gap-4">
                    <div
                      className={`flex h-10 w-10 items-center justify-center rounded-xl ${iconClassFor(item)}`}
                    >
                      {isBalanceType(item.type) ? (
                        <Icon name="dollar" size="md" className={iconColorClass(item)} />
                      ) : isSubscriptionType(item.type) ? (
                        <Icon name="badge" size="md" className="text-purple-600 dark:text-purple-400" />
                      ) : (
                        <Icon name="bolt" size="md" className={iconColorClass(item)} />
                      )}
                    </div>
                    <div>
                      <p className="text-sm font-medium text-gray-900 dark:text-white">{getHistoryItemTitle(item)}</p>
                      <p className="text-xs surface-muted">{formatDateTime(item.used_at)}</p>
                    </div>
                  </div>
                  <div className="text-right">
                    <p className={`text-sm font-semibold ${iconColorClass(item)}`}>{formatHistoryValue(item)}</p>
                    {!isAdminAdjustment(item.type) ? (
                      <p className="font-mono text-xs surface-muted">{item.code.slice(0, 8)}...</p>
                    ) : (
                      <p className="text-xs surface-muted">{t('redeem.adminAdjustment')}</p>
                    )}
                    {item.notes && (
                      <p
                        className="surface-muted mt-1 max-w-[200px] truncate text-xs italic"
                        title={item.notes}
                      >
                        {item.notes}
                      </p>
                    )}
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className="empty-state py-8">
              <div className="surface-inset mb-4 flex h-16 w-16 items-center justify-center rounded-2xl">
                <Icon name="clock" size="xl" className="surface-muted" />
              </div>
              <p className="text-sm surface-muted">{t('redeem.historyWillAppear')}</p>
            </div>
          )}
        </div>
      </div>

      <nav
        className="card workspace-fixed redeem-history-pagination"
        aria-label={t('redeem.recentActivity')}
      >
        <div className="redeem-pagination-info">
          <span>
            {t('common.total')}: {historyTotal} {t('pagination.results')}
          </span>
          <label>
            {t('pagination.perPage')}
            <Select
              value={historyPageSize}
              className="w-24"
              disabled={loadingHistory || submitting}
              searchable={false}
              options={[20, 50, 100].map((size) => ({ value: size, label: String(size) }))}
              onValueChange={(value) => {
                setHistoryPageSize(Number(value))
                void fetchHistory(1)
              }}
            />
          </label>
        </div>
        <div className="redeem-pagination-actions">
          <span className="text-xs surface-muted">
            {t('pagination.pageOf', { page: historyPage, total: historyPageCount })}
          </span>
          <button
            className="btn btn-secondary"
            disabled={loadingHistory || submitting || historyPage <= 1}
            onClick={() => void fetchHistory(historyPage - 1)}
          >
            {t('pagination.previous')}
          </button>
          <button
            className="btn btn-secondary"
            disabled={loadingHistory || submitting || historyPage * historyPageSize >= historyTotal}
            onClick={() => void fetchHistory(historyPage + 1)}
          >
            {t('pagination.next')}
          </button>
        </div>
      </nav>
    </div>
  )
}
