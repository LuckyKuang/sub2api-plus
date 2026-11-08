import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import type { OrderStatus } from '@/types/payment'

export interface OrderStatusBadgeProps {
  status: OrderStatus
}

const statusMap: Record<OrderStatus, { key: string; class: string }> = {
  PENDING: { key: 'payment.status.pending', class: 'bg-yellow-100/[0.15] text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-400' },
  PAID: { key: 'payment.status.paid', class: 'bg-blue-100/[0.15] text-blue-800 dark:bg-blue-900/30 dark:text-blue-400' },
  RECHARGING: { key: 'payment.status.recharging', class: 'bg-blue-100/[0.15] text-blue-800 dark:bg-blue-900/30 dark:text-blue-400' },
  COMPLETED: { key: 'payment.status.completed', class: 'bg-green-100/[0.15] text-green-800 dark:bg-green-900/30 dark:text-green-400' },
  EXPIRED: { key: 'payment.status.expired', class: 'surface-inset text-gray-800 dark:surface-inset dark:text-gray-400' },
  CANCELLED: { key: 'payment.status.cancelled', class: 'surface-inset text-gray-800 dark:surface-inset dark:text-gray-400' },
  FAILED: { key: 'payment.status.failed', class: 'bg-red-100/[0.15] text-red-800 dark:bg-red-900/30 dark:text-red-400' },
  REFUND_REQUESTED: { key: 'payment.status.refund_requested', class: 'bg-orange-100/[0.15] text-orange-800 dark:bg-orange-900/30 dark:text-orange-400' },
  REFUNDING: { key: 'payment.status.refunding', class: 'bg-orange-100/[0.15] text-orange-800 dark:bg-orange-900/30 dark:text-orange-400' },
  REFUND_PENDING: { key: 'payment.status.refund_pending', class: 'bg-orange-100/[0.15] text-orange-800 dark:bg-orange-900/30 dark:text-orange-400' },
  REFUNDED: { key: 'payment.status.refunded', class: 'bg-purple-100/[0.15] text-purple-800 dark:bg-purple-900/30 dark:text-purple-400' },
  PARTIALLY_REFUNDED: { key: 'payment.status.partially_refunded', class: 'bg-purple-100/[0.15] text-purple-800 dark:bg-purple-900/30 dark:text-purple-400' },
  REFUND_FAILED: { key: 'payment.status.refund_failed', class: 'bg-red-100/[0.15] text-red-800 dark:bg-red-900/30 dark:text-red-400' },
}

export function OrderStatusBadge({ status }: OrderStatusBadgeProps) {
  const { t } = useTranslation()
  const entry = statusMap[status]

  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium',
        entry?.class ?? 'surface-inset text-gray-800 dark:surface-inset dark:text-gray-400',
      )}
    >
      {entry ? t(entry.key) : status}
    </span>
  )
}

export default OrderStatusBadge
