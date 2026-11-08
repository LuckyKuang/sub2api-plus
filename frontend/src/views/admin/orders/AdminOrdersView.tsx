import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Modal, Pagination, Select, toast } from '@/components/ui'
import { adminPaymentAPI } from '@/api/admin/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import type { PaymentOrder } from '@/types/payment'
import { Icon } from '@/views/admin/AccountIcon'
import { AdminRefundDialog } from '../parts/AdminRefundDialog'
import { OrderStatusBadge } from '../parts/OrderStatusBadge'
import { OrderTable } from '../parts/OrderTable'
import { currencySymbol, formatOrderDateTime } from '../parts/paymentOrder'

interface AuditLog {
  id: number
  action: string
  detail: string | null
  operator: string | null
  created_at: string
}

export default function AdminOrdersView() {
  const { t } = useTranslation()

  const [ordersLoading, setOrdersLoading] = useState(false)
  const [orders, setOrders] = useState<PaymentOrder[]>([])
  const [orderSearch, setOrderSearch] = useState('')
  const [orderFilters, setOrderFilters] = useState({ status: '', payment_type: '', order_type: '', order_source: '' })
  const [orderPage, setOrderPage] = useState(1)
  const [orderPageSize, setOrderPageSize] = useState(20)
  const [orderTotal, setOrderTotal] = useState(0)

  const [selectedOrder, setSelectedOrder] = useState<PaymentOrder | null>(null)
  const [showDetailDialog, setShowDetailDialog] = useState(false)
  const [showRefundDialog, setShowRefundDialog] = useState(false)
  const [refundSubmitting, setRefundSubmitting] = useState(false)
  const [refundRequireForce, setRefundRequireForce] = useState(false)
  const [refundWarning, setRefundWarning] = useState('')
  const [refundQueryingIds, setRefundQueryingIds] = useState<Set<number>>(new Set())
  const [orderAuditLogs, setOrderAuditLogs] = useState<AuditLog[]>([])

  const debounceTimer = useRef<number | null>(null)
  const stateRef = useRef({ orderPage, orderPageSize, orderSearch, orderFilters })
  stateRef.current = { orderPage, orderPageSize, orderSearch, orderFilters }

  const creditedAmountSymbol = currencySymbol('USD')
  const paymentAmountSymbol = (order: PaymentOrder | null | undefined): string => currencySymbol(order?.currency)

  const sourceFilters = [
    { value: '', label: t('featureExtensions.allSources') },
    { value: 'payment', label: t('featureExtensions.paymentOrder') },
    { value: 'redeem', label: t('featureExtensions.redeemOrder') }
  ]

  const statusFilterOptions = [
    { value: '', label: t('payment.admin.allStatuses') },
    { value: 'PENDING', label: t('payment.status.pending') },
    { value: 'PAID', label: t('payment.status.paid') },
    { value: 'COMPLETED', label: t('payment.status.completed') },
    { value: 'EXPIRED', label: t('payment.status.expired') },
    { value: 'CANCELLED', label: t('payment.status.cancelled') },
    { value: 'FAILED', label: t('payment.status.failed') },
    { value: 'REFUNDED', label: t('payment.status.refunded') },
    { value: 'REFUND_REQUESTED', label: t('payment.status.refund_requested') },
    { value: 'REFUND_PENDING', label: t('payment.status.refund_pending') },
    { value: 'REFUND_FAILED', label: t('payment.status.refund_failed') }
  ]

  const paymentTypeFilterOptions = [
    { value: '', label: t('payment.admin.allPaymentTypes') },
    { value: 'alipay', label: t('payment.methods.alipay') },
    { value: 'wxpay', label: t('payment.methods.wxpay') },
    { value: 'stripe', label: t('payment.methods.stripe') },
    { value: 'airwallex', label: t('payment.methods.airwallex') }
  ]

  const orderTypeFilterOptions = [
    { value: '', label: t('payment.admin.allOrderTypes') },
    { value: 'balance', label: t('payment.admin.balanceOrder') },
    { value: 'subscription', label: t('payment.admin.subscriptionOrder') }
  ]

  async function loadOrders() {
    const current = stateRef.current
    setOrdersLoading(true)
    try {
      const res = await adminPaymentAPI.getOrders({
        page: current.orderPage,
        page_size: current.orderPageSize,
        keyword: current.orderSearch || undefined,
        status: current.orderFilters.status || undefined,
        payment_type: current.orderFilters.payment_type || undefined,
        order_type: current.orderFilters.order_type || undefined,
        order_source: current.orderFilters.order_source || undefined
      })
      setOrders(res.data.items || [])
      setOrderTotal(res.data.total || 0)
    } catch (err: unknown) {
      toast.error(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
    } finally {
      setOrdersLoading(false)
    }
  }

  useEffect(() => {
    void loadOrders()
    return () => {
      if (debounceTimer.current) window.clearTimeout(debounceTimer.current)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function debounceLoadOrders() {
    if (debounceTimer.current) window.clearTimeout(debounceTimer.current)
    debounceTimer.current = window.setTimeout(() => void loadOrders(), 300)
  }

  async function showOrderDetail(order: PaymentOrder) {
    setSelectedOrder(order)
    setOrderAuditLogs([])
    setShowDetailDialog(true)
    try {
      const res = await adminPaymentAPI.getOrder(order.id)
      const data = res.data as unknown as Record<string, unknown>
      if (data.order) setSelectedOrder(data.order as PaymentOrder)
      setOrderAuditLogs(((data.auditLogs || data.audit_logs || []) as unknown) as AuditLog[])
    } catch {
      /* keep cached order data */
    }
  }

  async function handleCancelOrder(order: PaymentOrder) {
    try {
      await adminPaymentAPI.cancelOrder(order.id)
      toast.success(t('payment.admin.orderCancelled'))
      void loadOrders()
    } catch (err: unknown) {
      toast.error(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
    }
  }

  async function handleRetryOrder(order: PaymentOrder) {
    try {
      await adminPaymentAPI.retryRecharge(order.id)
      toast.success(t('payment.admin.retrySuccess'))
      void loadOrders()
    } catch (err: unknown) {
      toast.error(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
    }
  }

  function openRefundDialog(order: PaymentOrder) {
    setSelectedOrder(order)
    setRefundRequireForce(false)
    setRefundWarning('')
    setShowRefundDialog(true)
  }

  function closeRefundDialog() {
    setShowRefundDialog(false)
    setRefundRequireForce(false)
    setRefundWarning('')
  }

  function isRefundPendingWarning(warning: string | undefined): boolean {
    return /pending|处理中|待/.test(String(warning || '').toLowerCase())
  }

  async function handleRefund(data: { amount: number; reason: string; deduct_balance: boolean; force: boolean }) {
    if (!selectedOrder) return
    setRefundSubmitting(true)
    try {
      const res = await adminPaymentAPI.refundOrder(selectedOrder.id, {
        amount: data.amount,
        reason: data.reason,
        deduct_balance: data.deduct_balance,
        force: data.force
      })
      if (res.data.success) {
        toast.success(t('payment.admin.refundSuccess'))
        closeRefundDialog()
        void loadOrders()
        return
      }
      if (isRefundPendingWarning(res.data.warning)) {
        toast.success(t('payment.admin.refundPending'))
        closeRefundDialog()
        void loadOrders()
        return
      }
      if (res.data.require_force) {
        // Backend needs an explicit force confirmation (e.g. the user spent their
        // balance after requesting the refund). Keep the dialog open and surface
        // the force checkbox instead of dropping the admin back to the list.
        setRefundRequireForce(true)
        setRefundWarning(res.data.warning || '')
        return
      }
      toast.error(res.data.warning || t('common.error'))
    } catch (err: unknown) {
      toast.error(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
    } finally {
      setRefundSubmitting(false)
    }
  }

  async function handleQueryRefund(order: PaymentOrder) {
    setRefundQueryingIds((previous) => new Set(previous).add(order.id))
    try {
      const res = await adminPaymentAPI.queryRefund(order.id)
      if (res.data.success) {
        toast.success(t('payment.admin.refundSuccess'))
      } else if (isRefundPendingWarning(res.data.warning)) {
        toast.success(t('payment.admin.refundPending'))
      } else {
        toast.error(res.data.warning || t('common.error'))
      }
      void loadOrders()
    } catch (err: unknown) {
      toast.error(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
    } finally {
      setRefundQueryingIds((previous) => {
        const next = new Set(previous)
        next.delete(order.id)
        return next
      })
    }
  }

  const formatDateTime = (dateStr: string): string => formatOrderDateTime(dateStr)

  return (
    <div className="workspace-page">
      <div className="card workspace-fixed p-4" data-test="admin-orders-filters">
        <div className="filter-bar">
          <div className="filter-field filter-field--search">
            <input
              value={orderSearch}
              type="text"
              placeholder={t('payment.admin.searchOrders')}
              className="input"
              onChange={(event) => {
                setOrderSearch(event.target.value)
                debounceLoadOrders()
              }}
            />
          </div>
          <div className="filter-field">
            <Select
              value={orderFilters.order_source}
              options={sourceFilters}
              searchable={false}
              onValueChange={(next) => {
                setOrderFilters({ ...orderFilters, order_source: String(next ?? '') })
                setOrderPage(1)
                void loadOrders()
              }}
            />
          </div>
          <div className="filter-field">
            <Select
              value={orderFilters.status}
              options={statusFilterOptions}
              searchable={false}
              onValueChange={(next) => {
                setOrderFilters({ ...orderFilters, status: String(next ?? '') })
                void loadOrders()
              }}
            />
          </div>
          <div className="filter-field">
            <Select
              value={orderFilters.payment_type}
              options={paymentTypeFilterOptions}
              searchable={false}
              onValueChange={(next) => {
                setOrderFilters({ ...orderFilters, payment_type: String(next ?? '') })
                void loadOrders()
              }}
            />
          </div>
          <div className="filter-field">
            <Select
              value={orderFilters.order_type}
              options={orderTypeFilterOptions}
              searchable={false}
              onValueChange={(next) => {
                setOrderFilters({ ...orderFilters, order_type: String(next ?? '') })
                void loadOrders()
              }}
            />
          </div>
          <div className="flex flex-1 flex-wrap items-center justify-end gap-2">
            <Button
              variant="secondary"
              onClick={() => void loadOrders()}
              disabled={ordersLoading}
              title={t('common.refresh')}
            >
              <Icon name="refresh" size="md" className={ordersLoading ? 'animate-spin' : ''} />
            </Button>
          </div>
        </div>
      </div>

      <div className="card workspace-table-card workspace-shell" data-test="admin-orders-table">
        <OrderTable
          orders={orders}
          loading={ordersLoading}
          showUser
          renderActions={(row) => (
            <div className="table-actions">
              <button type="button" className="table-action table-action-neutral" onClick={() => void showOrderDetail(row)}>
                <Icon name="eye" size="sm" />
                {t('common.view')}
              </button>
              {row.status === 'PENDING' && (
                <button
                  type="button"
                  className="table-action table-action-warning"
                  onClick={() => void handleCancelOrder(row)}
                >
                  <Icon name="x" size="sm" />
                  {t('payment.orders.cancel')}
                </button>
              )}
              {row.status === 'FAILED' && (
                <button type="button" className="table-action" onClick={() => void handleRetryOrder(row)}>
                  <Icon name="refresh" size="sm" />
                  {t('payment.admin.retry')}
                </button>
              )}
              {row.order_source !== 'redeem' && row.status === 'REFUND_REQUESTED' && (
                <>
                  {row.refund_amount && (
                    <span className="rounded-full bg-purple-100/[0.15] px-1.5 py-0.5 text-xs font-medium text-purple-700 dark:bg-purple-900/30 dark:text-purple-300">
                      {creditedAmountSymbol}
                      {row.refund_amount.toFixed(2)}
                    </span>
                  )}
                  <button type="button" className="table-action" onClick={() => openRefundDialog(row)}>
                    <Icon name="check" size="sm" />
                    {t('payment.admin.approveRefund')}
                  </button>
                </>
              )}
              {row.order_source !== 'redeem' && row.status === 'REFUND_FAILED' && (
                <button type="button" className="table-action" onClick={() => openRefundDialog(row)}>
                  <Icon name="refresh" size="sm" />
                  {t('payment.admin.retryRefund')}
                </button>
              )}
              {row.order_source !== 'redeem' && row.status === 'REFUND_PENDING' && (
                <button
                  type="button"
                  className="table-action table-action-warning"
                  disabled={refundQueryingIds.has(row.id)}
                  onClick={() => void handleQueryRefund(row)}
                >
                  <Icon name="refresh" size="sm" className={refundQueryingIds.has(row.id) ? 'animate-spin' : ''} />
                  {t('payment.admin.queryRefundStatus')}
                </button>
              )}
              {row.order_source !== 'redeem' && (row.status === 'COMPLETED' || row.status === 'PARTIALLY_REFUNDED') && (
                <button
                  type="button"
                  className="table-action table-action-danger"
                  onClick={() => openRefundDialog(row)}
                >
                  <Icon name="dollar" size="sm" />
                  {t('payment.admin.refund')}
                </button>
              )}
            </div>
          )}
        />
      </div>

      {orderTotal > 0 && (
        <Pagination
          className="workspace-fixed"
          page={orderPage}
          total={orderTotal}
          pageSize={orderPageSize}
          onPageChange={(page) => {
            setOrderPage(page)
            void loadOrders()
          }}
          onPageSizeChange={(size) => {
            setOrderPageSize(size)
            setOrderPage(1)
            void loadOrders()
          }}
        />
      )}

      <Modal
        open={showDetailDialog}
        onClose={() => setShowDetailDialog(false)}
        title={t('payment.admin.orderDetail')}
        size="lg"
      >
        {selectedOrder && (
          <div className="space-y-4">
            <div className="grid grid-cols-1 gap-4 break-words sm:grid-cols-2">
              <div>
                <p className="text-xs surface-muted">{t('featureExtensions.orderSource')}</p>
                <p>
                  {t(
                    selectedOrder.order_source === 'redeem'
                      ? 'featureExtensions.redeemOrder'
                      : 'featureExtensions.paymentOrder'
                  )}
                </p>
              </div>
              {selectedOrder.plan_name && (
                <div>
                  <p className="text-xs surface-muted">{t('featureExtensions.orderPlan')}</p>
                  <p>{selectedOrder.plan_name}</p>
                </div>
              )}
              <div>
                <p className="text-xs surface-muted">{t('payment.orders.orderId')}</p>
                <p className="font-mono text-sm font-medium text-gray-900 dark:text-white">#{selectedOrder.id}</p>
              </div>
              <div className="min-w-0">
                <p className="text-xs surface-muted">{t('payment.orders.orderNo')}</p>
                <p className="break-all text-sm font-medium text-gray-900 dark:text-white">
                  {selectedOrder.out_trade_no}
                </p>
              </div>
              <div>
                <p className="text-xs surface-muted">{t('payment.orders.status')}</p>
                <OrderStatusBadge status={selectedOrder.status} />
              </div>
              <div>
                <p className="text-xs surface-muted">{t('payment.orders.amount')}</p>
                <p className="text-sm font-medium text-gray-900 dark:text-white">
                  {selectedOrder.order_source === 'redeem' && selectedOrder.order_type === 'subscription'
                    ? paymentAmountSymbol(selectedOrder)
                    : creditedAmountSymbol}
                  {selectedOrder.amount.toFixed(2)}
                </p>
              </div>
              <div>
                <p className="text-xs surface-muted">{t('payment.orders.payAmount')}</p>
                <p className="text-sm font-medium text-gray-900 dark:text-white">
                  {paymentAmountSymbol(selectedOrder)}
                  {selectedOrder.pay_amount.toFixed(2)}
                </p>
              </div>
              <div>
                <p className="text-xs surface-muted">{t('payment.orders.paymentMethod')}</p>
                <p className="text-sm surface-muted">
                  {t('payment.methods.' + selectedOrder.payment_type, selectedOrder.payment_type)}
                </p>
              </div>
              <div>
                <p className="text-xs surface-muted">{t('payment.admin.feeRate')}</p>
                <p className="text-sm surface-muted">{selectedOrder.fee_rate}%</p>
              </div>
              <div>
                <p className="text-xs surface-muted">{t('payment.orders.createdAt')}</p>
                <p className="text-sm surface-muted">{formatDateTime(selectedOrder.created_at)}</p>
              </div>
              <div>
                <p className="text-xs surface-muted">{t('payment.admin.expiresAt')}</p>
                <p className="text-sm surface-muted">{formatDateTime(selectedOrder.expires_at)}</p>
              </div>
              {selectedOrder.paid_at && (
                <div>
                  <p className="text-xs surface-muted">{t('payment.admin.paidAt')}</p>
                  <p className="text-sm surface-muted">{formatDateTime(selectedOrder.paid_at)}</p>
                </div>
              )}
              {selectedOrder.refund_amount && (
                <div>
                  <p className="text-xs surface-muted">{t('payment.admin.refundAmount')}</p>
                  <p className="text-sm font-medium text-red-600 dark:text-red-400">
                    {creditedAmountSymbol}
                    {selectedOrder.refund_amount.toFixed(2)}
                  </p>
                </div>
              )}
              {selectedOrder.refund_reason && (
                <div className="sm:col-span-2">
                  <p className="text-xs surface-muted">{t('payment.admin.refundReason')}</p>
                  <p className="text-sm surface-muted">{selectedOrder.refund_reason}</p>
                </div>
              )}
              {selectedOrder.refund_requested_at && (
                <div className="border-t border-gray-200 pt-3 sm:col-span-2 dark:border-dark-600">
                  <p className="mb-2 text-xs font-medium text-purple-600 dark:text-purple-400">
                    {t('payment.admin.refundRequestInfo')}
                  </p>
                  <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                    <div>
                      <p className="text-xs surface-muted">{t('payment.admin.refundRequestedAt')}</p>
                      <p className="text-sm surface-muted">{formatDateTime(selectedOrder.refund_requested_at)}</p>
                    </div>
                    <div>
                      <p className="text-xs surface-muted">{t('payment.admin.refundRequestedBy')}</p>
                      <p className="text-sm surface-muted">#{selectedOrder.refund_requested_by}</p>
                    </div>
                    <div className="sm:col-span-2">
                      <p className="text-xs surface-muted">{t('payment.admin.refundRequestReason')}</p>
                      <p className="text-sm surface-muted">{selectedOrder.refund_request_reason}</p>
                    </div>
                  </div>
                </div>
              )}
            </div>

            {orderAuditLogs.length > 0 && (
              <div className="border-t border-gray-200 pt-4 dark:border-dark-600">
                <p className="mb-2 text-xs font-medium surface-muted">{t('payment.admin.auditLogs')}</p>
                <div className="max-h-48 space-y-2 overflow-y-auto">
                  {orderAuditLogs.map((log) => (
                    <div
                      key={log.id}
                      className="rounded-lg border border-gray-100 surface-inset p-2.5 dark:border-dark-600"
                    >
                      <div className="flex items-center justify-between">
                        <span className="text-xs font-medium surface-muted">{log.action}</span>
                        <span className="text-xs surface-muted">{formatDateTime(log.created_at)}</span>
                      </div>
                      {log.detail && <div className="mt-1 break-all text-xs surface-muted">{log.detail}</div>}
                      {log.operator && (
                        <div className="mt-1 text-xs surface-muted">
                          {t('payment.admin.operator')}: {log.operator}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}
      </Modal>

      <AdminRefundDialog
        open={showRefundDialog}
        order={selectedOrder}
        submitting={refundSubmitting}
        requireForce={refundRequireForce}
        warning={refundWarning}
        onConfirm={(data) => void handleRefund(data)}
        onCancel={closeRefundDialog}
      />
    </div>
  )
}
