import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Modal } from '@/components/ui'
import type { PaymentOrder } from '@/types/payment'
import { currencySymbol, formatOrderDateTime } from './paymentOrder'

export interface AdminRefundDialogProps {
  open: boolean
  order: PaymentOrder | null
  submitting?: boolean
  userBalance?: number | null
  requireForce?: boolean
  warning?: string
  onConfirm: (data: { amount: number; reason: string; deduct_balance: boolean; force: boolean }) => void
  onCancel: () => void
}

export function AdminRefundDialog({
  open,
  order,
  submitting = false,
  userBalance = null,
  requireForce = false,
  warning = '',
  onConfirm,
  onCancel
}: AdminRefundDialogProps) {
  const { t } = useTranslation()

  const [amount, setAmount] = useState(0)
  const [reason, setReason] = useState('')
  const [deductBalance, setDeductBalance] = useState(true)
  const [force, setForce] = useState(false)

  const creditedAmountSymbol = currencySymbol('USD')
  const paymentAmountSymbol = currencySymbol(order?.currency)

  // In REFUND_REQUESTED / REFUND_PENDING status, refund_amount is requested/pending, not actually refunded.
  // Only PARTIALLY_REFUNDED / REFUNDED have real refund amounts.
  const actuallyRefunded = (() => {
    if (!order) return 0
    const status = order.status
    if (status === 'PARTIALLY_REFUNDED' || status === 'REFUNDED') return order.refund_amount || 0
    return 0
  })()

  const maxRefundable = order ? order.amount - actuallyRefunded : 0

  const balanceInsufficient = userBalance == null || !order ? false : userBalance < amount

  useEffect(() => {
    if (!open || !order) return
    // For REFUND_REQUESTED, pre-fill with the requested amount
    if (order.status === 'REFUND_REQUESTED' && order.refund_amount) {
      setAmount(order.refund_amount)
    } else {
      setAmount(maxRefundable)
    }
    setReason(order.refund_request_reason || '')
    setDeductBalance(true)
    setForce(false)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, order])

  const formatDateTime = (dateStr: string): string => formatOrderDateTime(dateStr)

  function handleSubmit() {
    if (amount <= 0 || amount > maxRefundable) return
    if (requireForce && !force) return
    onConfirm({ amount, reason, deduct_balance: deductBalance, force })
  }

  return (
    <Modal
      open={open}
      onClose={onCancel}
      title={t('payment.admin.refundOrder')}
      size="md"
      footer={
        <div className="flex justify-end gap-3">
          <Button variant="secondary" onClick={onCancel}>
            {t('common.cancel')}
          </Button>
          <button
            type="submit"
            form="refund-form"
            disabled={submitting || amount <= 0 || (requireForce && !force)}
            className="rounded-md bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700 focus:outline-none focus:ring-2 focus:ring-red-500 focus:ring-offset-2 disabled:opacity-50 dark:focus:ring-offset-dark-800"
          >
            {submitting ? t('common.processing') : t('payment.admin.confirmRefund')}
          </button>
        </div>
      }
    >
      <form
        id="refund-form"
        onSubmit={(event) => {
          event.preventDefault()
          handleSubmit()
        }}
        className="space-y-4"
      >
        {(order?.refund_requested_at || order?.refund_request_reason) && (
          <div className="rounded-lg border border-violet-200 bg-violet-50/[0.15] p-3 dark:border-violet-800 dark:bg-violet-900/20">
            <div className="flex items-center gap-2 text-sm font-medium text-violet-700 dark:text-violet-300">
              <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth="2"
                  d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                />
              </svg>
              {t('payment.admin.refundRequestInfo')}
            </div>
            {order?.refund_requested_at && (
              <div className="mt-2 flex justify-between text-sm">
                <span className="text-violet-600 dark:text-violet-400">{t('payment.admin.refundRequestedAt')}</span>
                <span className="text-violet-800 dark:text-violet-200">
                  {formatDateTime(order.refund_requested_at)}
                </span>
              </div>
            )}
            {order?.refund_request_reason && (
              <div className="mt-1 text-sm">
                <span className="text-violet-600 dark:text-violet-400">{t('payment.admin.refundRequestReason')}:</span>
                <span className="ml-1 text-violet-800 dark:text-violet-200">{order.refund_request_reason}</span>
              </div>
            )}
          </div>
        )}

        <div className="rounded-lg surface-inset p-3">
          <div className="flex justify-between text-sm">
            <span className="surface-muted">{t('payment.orders.orderId')}</span>
            <span className="font-mono text-gray-900 dark:text-white">#{order?.id}</span>
          </div>
          <div className="mt-1 flex justify-between text-sm">
            <span className="surface-muted">{t('payment.orders.creditedAmount')}</span>
            <span className="font-medium text-gray-900 dark:text-white">
              {creditedAmountSymbol}
              {order?.amount?.toFixed(2)}
            </span>
          </div>
          <div className="mt-1 flex justify-between text-sm">
            <span className="surface-muted">{t('payment.orders.payAmount')}</span>
            <span className="font-medium text-gray-900 dark:text-white">
              {paymentAmountSymbol}
              {order?.pay_amount?.toFixed(2)}
            </span>
          </div>
          {actuallyRefunded > 0 && (
            <div className="mt-1 flex justify-between text-sm">
              <span className="surface-muted">{t('payment.admin.alreadyRefunded')}</span>
              <span className="font-medium text-red-600 dark:text-red-400">
                {creditedAmountSymbol}
                {actuallyRefunded.toFixed(2)}
              </span>
            </div>
          )}
        </div>

        <div>
          <div className="flex items-center gap-2">
            <input
              id="deduct-balance"
              checked={deductBalance}
              type="checkbox"
              className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              onChange={(event) => setDeductBalance(event.target.checked)}
            />
            <label htmlFor="deduct-balance" className="text-sm surface-muted">
              {t('payment.admin.deductBalance')}
            </label>
            <span className="text-xs surface-muted">{t('payment.admin.deductBalanceHint')}</span>
          </div>

          {deductBalance && userBalance != null && (
            <div className="mt-3 grid grid-cols-2 gap-3">
              <div className="rounded-lg surface-inset p-3 text-sm">
                <div className="surface-muted">{t('payment.admin.userBalance')}</div>
                <div className="mt-1 font-semibold text-gray-900 dark:text-white">
                  {creditedAmountSymbol}
                  {userBalance.toFixed(2)}
                </div>
              </div>
              <div className="rounded-lg surface-inset p-3 text-sm">
                <div className="surface-muted">{t('payment.admin.orderAmount')}</div>
                <div className="mt-1 font-semibold text-gray-900 dark:text-white">
                  {creditedAmountSymbol}
                  {order?.amount?.toFixed(2)}
                </div>
              </div>
            </div>
          )}

          {deductBalance && balanceInsufficient && (
            <div className="mt-2 rounded-lg bg-amber-50/[0.15] p-3 text-sm text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
              {t('payment.admin.insufficientBalance')}
            </div>
          )}

          {!deductBalance && (
            <div className="mt-2 rounded-lg bg-blue-50/[0.15] p-3 text-sm text-blue-700 dark:bg-blue-900/20 dark:text-blue-300">
              {t('payment.admin.noDeduction')}
            </div>
          )}
        </div>

        <div>
          <label className="input-label">{t('payment.admin.refundAmount')}</label>
          <div className="relative">
            <span className="absolute left-3 top-1/2 -translate-y-1/2 surface-muted">{creditedAmountSymbol}</span>
            <input
              value={amount}
              type="number"
              step="0.01"
              min="0.01"
              max={maxRefundable}
              className="input pl-7"
              required
              onChange={(event) => setAmount(Number(event.target.value))}
            />
          </div>
          <p className="mt-1 text-xs surface-muted">
            {t('payment.admin.maxRefundable')}: {creditedAmountSymbol}
            {maxRefundable.toFixed(2)}
          </p>
        </div>

        <div>
          <label className="input-label">{t('payment.admin.refundReason')}</label>
          <textarea
            value={reason}
            rows={3}
            className="input"
            placeholder={t('payment.admin.refundReasonPlaceholder')}
            required
            onChange={(event) => setReason(event.target.value)}
          />
        </div>

        {warning && (
          <div className="rounded-lg bg-yellow-50/[0.15] p-3 text-sm text-yellow-700 dark:bg-yellow-900/20 dark:text-yellow-300">
            {warning}
          </div>
        )}

        {requireForce && (
          <div className="flex items-center gap-2">
            <input
              id="force-refund"
              checked={force}
              type="checkbox"
              className="h-4 w-4 rounded border-gray-300 text-red-600 focus:ring-red-500"
              onChange={(event) => setForce(event.target.checked)}
            />
            <label htmlFor="force-refund" className="text-sm font-medium text-red-600 dark:text-red-400">
              {t('payment.admin.forceRefund')}
            </label>
          </div>
        )}
      </form>
    </Modal>
  )
}

export default AdminRefundDialog
