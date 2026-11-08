import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import type { PaymentOrder } from '@/types/payment'
import { AdminDataTable, type Column } from '@/views/admin/AdminDataTable'
import { OrderStatusBadge } from './OrderStatusBadge'
import { currencySymbol } from './paymentOrder'

export interface OrderTableProps {
  orders: PaymentOrder[]
  loading: boolean
  showUser?: boolean
  renderActions?: (row: PaymentOrder) => React.ReactNode
}

export function OrderTable({ orders, loading, showUser = false, renderActions }: OrderTableProps) {
  const { t } = useTranslation()

  const creditedAmountSymbol = currencySymbol('USD')
  const paymentAmountSymbol = (order: PaymentOrder): string => currencySymbol(order.currency)

  const formatDate = (dateStr: string): string => new Date(dateStr).toLocaleString()

  const columns = useMemo<Column[]>(() => {
    const cols: Column[] = [
      { key: 'id', label: t('payment.orders.orderId') },
      { key: 'out_trade_no', label: t('payment.orders.orderNo') }
    ]
    if (showUser) {
      cols.push({ key: 'user_email', label: t('payment.admin.colUser') })
    }
    cols.push(
      { key: 'order_source', label: t('featureExtensions.orderSource') },
      { key: 'order_type', label: t('featureExtensions.orderType') },
      { key: 'pay_amount', label: t('payment.orders.payAmount') },
      { key: 'payment_type', label: t('payment.orders.paymentMethod') },
      { key: 'status', label: t('payment.orders.status') },
      { key: 'created_at', label: t('payment.orders.createdAt') },
      { key: 'actions', label: t('common.actions') }
    )
    return cols
  }, [t, showUser])

  return (
    <AdminDataTable
      columns={columns}
      data={orders}
      loading={loading}
      renderCell={(key, row, value) => {
        const order = row as PaymentOrder
        if (key === 'id') {
          return <span className="font-mono text-sm">#{String(value ?? '')}</span>
        }
        if (key === 'out_trade_no') {
          return <span className="break-all text-sm text-gray-900 dark:text-white">{String(value ?? '')}</span>
        }
        if (key === 'user_email') {
          return (
            <div className="min-w-0 break-words text-sm">
              <span className="text-gray-900 dark:text-white">
                {String(value ?? '') || (order as { user_name?: string }).user_name || '#' + order.user_id}
              </span>
              {(order as { user_notes?: string }).user_notes && <span className="ml-1 text-xs surface-muted">({(order as { user_notes?: string }).user_notes})</span>}
            </div>
          )
        }
        if (key === 'pay_amount') {
          const amount = Number(value ?? 0)
          return (
            <div className="text-sm">
              <span className="font-medium text-gray-900 dark:text-white">
                {paymentAmountSymbol(order)}
                {amount.toFixed(2)}
              </span>
              {order.fee_rate > 0 && (
                <span
                  className="ml-1 text-xs surface-muted"
                  title={t('payment.orders.fee') + ': ' + order.fee_rate + '%'}
                >
                  ({t('payment.orders.fee')} {order.fee_rate}%)
                </span>
              )}
              {((order.order_source === 'redeem' && order.order_type === 'balance') ||
                order.amount !== order.pay_amount ||
                (order.bonus_amount ?? 0) > 0) && (
                <div className="text-xs surface-muted">
                  {t('payment.orders.creditedAmount')}: {creditedAmountSymbol}
                  {order.amount.toFixed(2)}
                  {(order.bonus_amount ?? 0) > 0 && (
                    <span className="ml-1 text-amber-600 dark:text-amber-400">
                      ({t('payment.orders.bonusIncluded', { amount: creditedAmountSymbol + (order.bonus_amount ?? 0).toFixed(2) })})
                    </span>
                  )}
                </div>
              )}
            </div>
          )
        }
        if (key === 'order_source') {
          return (
            <span className="badge badge-gray">
              {t(order.order_source === 'redeem' ? 'featureExtensions.redeemOrder' : 'featureExtensions.paymentOrder')}
            </span>
          )
        }
        if (key === 'order_type') {
          return (
            <span>
              {t(order.order_type === 'subscription' ? 'payment.tabSubscribe' : 'payment.tabTopUp')}
              {order.plan_name && <p className="mt-1 text-xs surface-muted">{order.plan_name}</p>}
            </span>
          )
        }
        if (key === 'payment_type') {
          return (
            <span className="text-sm surface-muted">
              {value === 'redeem' ? t('featureExtensions.redeemOrder') : t('payment.methods.' + String(value ?? ''), String(value ?? ''))}
            </span>
          )
        }
        if (key === 'status') {
          return <OrderStatusBadge status={value as never} />
        }
        if (key === 'created_at') {
          return <span className="text-xs surface-muted">{formatDate(String(value ?? ''))}</span>
        }
        if (key === 'actions') {
          return renderActions ? renderActions(order) : null
        }
        return <span>{value == null ? '' : String(value)}</span>
      }}
    />
  )
}

export default OrderTable
