import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { paymentAPI } from '@/api/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { EmptyState, Pagination, Select, toast } from '@/components/ui'
import { DataTable, type Column } from '@/components/DataTable'
import type { PaymentOrder } from '@/types/payment'
import { Icon } from './icons'
import { BaseDialog } from './BaseDialog'
import { OrderStatusBadge } from './OrderStatusBadge'
import { currencySymbol } from './paymentCurrency'

export default function UserOrdersView() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [loading, setLoading] = useState(false)
  const [actionLoading, setActionLoading] = useState(false)
  const [orders, setOrders] = useState<PaymentOrder[]>([])
  const [refundEligibleProviders, setRefundEligibleProviders] = useState<Set<string>>(new Set())
  const [currentFilter, setCurrentFilter] = useState('')
  const [sourceFilter, setSourceFilter] = useState('')
  const [typeFilter, setTypeFilter] = useState('')
  const [cancelTargetId, setCancelTargetId] = useState<number | null>(null)
  const [refundTarget, setRefundTarget] = useState<PaymentOrder | null>(null)
  const [refundReason, setRefundReason] = useState('')
  const [pagination, setPagination] = useState({ page: 1, page_size: 20, total: 0 })

  const sourceFilters = [
    { value: '', label: t('featureExtensions.allSources') },
    { value: 'payment', label: t('featureExtensions.paymentOrder') },
    { value: 'redeem', label: t('featureExtensions.redeemOrder') }
  ]
  const typeFilters = [
    { value: '', label: t('common.all') },
    { value: 'balance', label: t('payment.tabTopUp') },
    { value: 'subscription', label: t('payment.tabSubscribe') }
  ]
  const statusFilters = [
    { value: '', label: t('common.all') },
    { value: 'PENDING', label: t('payment.status.pending') },
    { value: 'COMPLETED', label: t('payment.status.completed') },
    { value: 'FAILED', label: t('payment.status.failed') },
    { value: 'REFUNDED', label: t('payment.status.refunded') }
  ]

  const fetchOrders = useCallback(async () => {
    setLoading(true)
    try {
      const res = await paymentAPI.getMyOrders({
        page: pagination.page,
        page_size: pagination.page_size,
        status: currentFilter || undefined,
        order_source: sourceFilter || undefined,
        order_type: typeFilter || undefined
      })
      setOrders(res.data.items || [])
      setPagination((previous) => ({ ...previous, total: res.data.total || 0 }))
    } catch (err: unknown) {
      toast.error(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
    } finally {
      setLoading(false)
    }
  }, [pagination.page, pagination.page_size, currentFilter, sourceFilter, typeFilter, t])

  const handlePageChange = (page: number) => {
    setPagination((previous) => ({ ...previous, page }))
  }

  const handlePageSizeChange = (size: number) => {
    setPagination((previous) => ({ ...previous, page: 1, page_size: size }))
  }

  useEffect(() => {
    void fetchOrders()
  }, [fetchOrders])

  useEffect(() => {
    let disposed = false
    void (async () => {
      try {
        const res = await paymentAPI.getRefundEligibleProviders()
        if (!disposed) setRefundEligibleProviders(new Set(res.data.provider_instance_ids || []))
      } catch {
        /* ignore — default to hiding refund button */
      }
    })()
    return () => {
      disposed = true
    }
  }, [])

  const confirmCancel = async () => {
    if (!cancelTargetId) return
    setActionLoading(true)
    try {
      await paymentAPI.cancelOrder(cancelTargetId)
      toast.success(t('common.success'))
      setCancelTargetId(null)
      await fetchOrders()
    } catch (err: unknown) {
      toast.error(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
    } finally {
      setActionLoading(false)
    }
  }

  const confirmRefund = async () => {
    if (!refundTarget || !refundReason.trim()) return
    setActionLoading(true)
    try {
      await paymentAPI.requestRefund(refundTarget.id, { reason: refundReason.trim() })
      toast.success(t('common.success'))
      setRefundTarget(null)
      setRefundReason('')
      await fetchOrders()
    } catch (err: unknown) {
      toast.error(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
    } finally {
      setActionLoading(false)
    }
  }

  function canRequestRefund(order: PaymentOrder): boolean {
    if (order.order_source === 'redeem') return false
    if (order.status !== 'COMPLETED') return false
    if (!order.provider_instance_id) return false
    return refundEligibleProviders.has(order.provider_instance_id)
  }

  const columns: Column[] = [
    { key: 'id', label: t('payment.orders.orderId') },
    { key: 'out_trade_no', label: t('payment.orders.orderNo') },
    { key: 'order_source', label: t('featureExtensions.orderSource') },
    { key: 'order_type', label: t('featureExtensions.orderType') },
    { key: 'pay_amount', label: t('payment.orders.payAmount') },
    { key: 'payment_type', label: t('payment.orders.paymentMethod') },
    { key: 'status', label: t('payment.orders.status') },
    { key: 'created_at', label: t('payment.orders.createdAt') },
    { key: 'actions', label: t('common.actions') }
  ]

  function formatDate(dateStr: string): string {
    return new Date(dateStr).toLocaleString()
  }

  function renderCell(key: string, row: PaymentOrder, value: unknown): React.ReactNode {
    switch (key) {
      case 'id':
        return <span className="font-mono text-sm">#{String(value)}</span>
      case 'out_trade_no':
        return <span className="break-all text-sm text-gray-900 dark:text-white">{String(value)}</span>
      case 'pay_amount':
        return (
          <div className="text-sm">
            <span className="font-medium text-gray-900 dark:text-white">
              {currencySymbol(row.currency)}
              {row.pay_amount.toFixed(2)}
            </span>
            {row.fee_rate > 0 && (
              <span
                className="ml-1 text-xs surface-muted"
                title={`${t('payment.orders.fee')}: ${row.fee_rate}%`}
              >
                ({t('payment.orders.fee')} {row.fee_rate}%)
              </span>
            )}
            {((row.order_source === 'redeem' && row.order_type === 'balance') ||
              row.amount !== row.pay_amount ||
              (row.bonus_amount ?? 0) > 0) && (
              <div className="text-xs surface-muted">
                {t('payment.orders.creditedAmount')}: {currencySymbol('USD')}
                {row.amount.toFixed(2)}
                {(row.bonus_amount ?? 0) > 0 && (
                  <span className="ml-1 text-amber-600 dark:text-amber-400">
                    (
                    {t('payment.orders.bonusIncluded', {
                      amount: currencySymbol('USD') + (row.bonus_amount ?? 0).toFixed(2)
                    })}
                    )
                  </span>
                )}
              </div>
            )}
          </div>
        )
      case 'order_source':
        return (
          <span className="badge badge-gray">
            {t(row.order_source === 'redeem' ? 'featureExtensions.redeemOrder' : 'featureExtensions.paymentOrder')}
          </span>
        )
      case 'order_type':
        return (
          <>
            <span>{t(row.order_type === 'subscription' ? 'payment.tabSubscribe' : 'payment.tabTopUp')}</span>
            {row.plan_name && <p className="mt-1 text-xs surface-muted">{row.plan_name}</p>}
          </>
        )
      case 'payment_type':
        return (
          <span className="text-sm surface-muted">
            {value === 'redeem'
              ? t('featureExtensions.redeemOrder')
              : t('payment.methods.' + String(value), String(value))}
          </span>
        )
      case 'status':
        return <OrderStatusBadge status={row.status} />
      case 'created_at':
        return <span className="text-xs surface-muted">{formatDate(String(value))}</span>
      case 'actions':
        return (
          <div className="table-actions">
            {row.status === 'PENDING' && (
              <button className="table-action table-action-warning" onClick={() => setCancelTargetId(row.id)}>
                <Icon name="x" size="sm" />
                <span>{t('payment.orders.cancel')}</span>
              </button>
            )}
            {canRequestRefund(row) && (
              <button
                className="table-action"
                onClick={() => {
                  setRefundTarget(row)
                  setRefundReason('')
                }}
              >
                <Icon name="dollar" size="sm" />
                <span>{t('payment.orders.requestRefund')}</span>
              </button>
            )}
          </div>
        )
      default:
        return value === null || value === undefined ? '' : String(value)
    }
  }

  return (
    <div className="workspace-page">
      <div className="card workspace-fixed p-4" data-test="user-orders-filters">
        <div className="filter-bar">
          <div className="filter-field">
            <Select
              value={sourceFilter}
              options={sourceFilters}
              searchable={false}
              onValueChange={(value) => {
                setSourceFilter(value === null ? '' : String(value))
                handlePageChange(1)
              }}
            />
          </div>
          <div className="filter-field">
            <Select
              value={typeFilter}
              options={typeFilters}
              searchable={false}
              onValueChange={(value) => {
                setTypeFilter(value === null ? '' : String(value))
                handlePageChange(1)
              }}
            />
          </div>
          <div className="filter-field">
            <Select
              value={currentFilter}
              options={statusFilters}
              searchable={false}
              onValueChange={(value) => {
                setCurrentFilter(value === null ? '' : String(value))
                handlePageChange(1)
              }}
            />
          </div>
          <div className="flex flex-1 items-center justify-end gap-2">
            <button
              onClick={() => void fetchOrders()}
              disabled={loading}
              className="btn btn-secondary"
              title={t('common.refresh')}
            >
              <Icon name="refresh" size="md" className={loading ? 'animate-spin' : ''} />
            </button>
            <button className="btn btn-primary" onClick={() => navigate('/purchase')}>
              {t('payment.result.backToRecharge')}
            </button>
          </div>
        </div>
      </div>

      <div className="card workspace-table-card workspace-shell" data-test="user-orders-table">
        <DataTable
          className="workspace-table-viewport"
          columns={columns}
          data={orders}
          loading={loading}
          rowKey="id"
          renderCell={(key, row, value) => renderCell(key, row as PaymentOrder, value)}
          renderEmpty={() => <EmptyState title={t('payment.orders.empty')} />}
        />
      </div>

      <Pagination
        className="workspace-fixed"
        page={pagination.page}
        total={pagination.total}
        pageSize={pagination.page_size}
        onPageChange={handlePageChange}
        onPageSizeChange={handlePageSizeChange}
      />

      <BaseDialog
        show={!!cancelTargetId}
        title={t('payment.orders.cancel')}
        width="narrow"
        onClose={() => setCancelTargetId(null)}
        footer={
          <div className="flex justify-end gap-3">
            <button className="btn btn-secondary" onClick={() => setCancelTargetId(null)}>
              {t('common.cancel')}
            </button>
            <button
              className="btn btn-danger"
              disabled={actionLoading}
              onClick={() => void confirmCancel()}
            >
              {actionLoading ? t('common.processing') : t('payment.orders.cancel')}
            </button>
          </div>
        }
      >
        <p className="text-sm surface-muted">{t('payment.confirmCancel')}</p>
      </BaseDialog>

      <BaseDialog
        show={!!refundTarget}
        title={t('payment.orders.requestRefund')}
        onClose={() => setRefundTarget(null)}
        footer={
          <div className="flex justify-end gap-3">
            <button className="btn btn-secondary" onClick={() => setRefundTarget(null)}>
              {t('common.cancel')}
            </button>
            <button
              className="btn btn-primary"
              disabled={actionLoading || !refundReason.trim()}
              onClick={() => void confirmRefund()}
            >
              {actionLoading ? t('common.processing') : t('payment.orders.requestRefund')}
            </button>
          </div>
        }
      >
        {refundTarget && (
          <div className="space-y-4">
            <div className="surface-inset rounded-xl p-4">
              <div className="flex justify-between text-sm">
                <span className="surface-muted">{t('payment.orders.orderId')}</span>
                <span className="font-mono text-gray-900 dark:text-white">#{refundTarget.id}</span>
              </div>
              <div className="mt-2 flex justify-between text-sm">
                <span className="surface-muted">{t('payment.orders.amount')}</span>
                <span className="text-gray-900 dark:text-white">${refundTarget.amount.toFixed(2)}</span>
              </div>
            </div>
            <div>
              <label className="input-label">{t('payment.refundReason')}</label>
              <textarea
                value={refundReason}
                onChange={(event) => setRefundReason(event.target.value)}
                rows={3}
                className="input mt-1 w-full"
                placeholder={t('payment.refundReasonPlaceholder')}
              />
            </div>
          </div>
        )}
      </BaseDialog>
    </div>
  )
}
