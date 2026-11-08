import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router-dom'
import OrderStatusBadge from './OrderStatusBadge'
import { useProductAppearance } from '@/components/auth/useProductAppearance'
import {
  PAYMENT_RECOVERY_STORAGE_KEY,
  clearPaymentRecoverySnapshot,
  readPaymentRecoverySnapshot,
} from './paymentFlow'
import { usePaymentStore } from '@/stores/payment'
import { useAuthStore } from '@/stores/auth'
import { paymentAPI } from '@/api/payment'
import type { PublicOrderVerifyResult } from '@/api/payment'
import type { OrderStatus, PaymentOrder } from '@/types/payment'
import { formatPaymentAmount, normalizePaymentCurrency } from './paymentCurrency'
import { normalizePaymentMethodForDisplay, paymentMethodI18nKey } from './paymentUx'

type ResolvedOrder = PaymentOrder | PublicOrderVerifyResult

interface ReturnInfo {
  outTradeNo: string
  money: string
  type: string
  tradeStatus: string
}

const SUCCESS_STATUSES = new Set(['COMPLETED', 'PAID', 'RECHARGING'])
const PENDING_STATUSES = new Set(['PENDING', 'CREATED', 'WAITING', 'PROCESSING'])
const STATUS_REFRESH_INTERVAL_MS = 2000
const STATUS_REFRESH_MAX_ATTEMPTS = 15

export default function PaymentResultView() {
  const { t, i18n } = useTranslation()
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const paymentStore = usePaymentStore()
  const authStore = useAuthStore()

  const [order, setOrder] = useState<ResolvedOrder | null>(null)
  const [loading, setLoading] = useState(true)
  const [currency, setCurrency] = useState('CNY')
  const [returnInfo, setReturnInfo] = useState<ReturnInfo | null>(null)

  const statusRefreshTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const userBalanceRefreshStarted = useRef(false)
  const refreshAttempts = useRef(0)
  const orderRef = useRef<ResolvedOrder | null>(null)

  // The root owns the palette so body-portalled controls share the page theme.
  useProductAppearance()

  const localeCode = i18n.resolvedLanguage || i18n.language

  /** 充值金额 = pay_amount / (1 + fee_rate/100)，fee_rate=0 时等于 pay_amount */
  const baseAmount = (() => {
    if (!hasAmountFields(order)) return 0
    const feeRate = Number(order.fee_rate) || 0
    if (feeRate <= 0) return order.pay_amount ?? 0
    return Math.round((order.pay_amount / (1 + feeRate / 100)) * 100) / 100
  })()

  /** 手续费 = pay_amount - baseAmount */
  const feeAmount = (() => {
    if (!hasAmountFields(order)) return 0
    const feeRate = Number(order.fee_rate) || 0
    if (feeRate <= 0) return 0
    return Math.round((order.pay_amount - baseAmount) * 100) / 100
  })()

  const isSuccess = isSuccessStatus(order?.status)
  const isPending = isPendingStatus(order?.status)

  const statusTitle = isSuccess
    ? t('payment.result.success')
    : isPending ? t('payment.result.processing') : t('payment.result.failed')

  function normalizedOrderPaymentType(paymentType: string): string {
    return normalizePaymentMethodForDisplay(paymentType || '') || paymentType || ''
  }

  function formatGatewayAmount(value: number): string {
    return formatPaymentAmount(value, currency, localeCode)
  }

  function setResolvedOrder(nextOrder: ResolvedOrder | null): void {
    orderRef.current = nextOrder
    setOrder(nextOrder)
    if (nextOrder && 'currency' in nextOrder && nextOrder.currency) {
      setCurrency(normalizePaymentCurrency(nextOrder.currency))
    }
    refreshUserBalanceForSuccessfulOrder(nextOrder)
  }

  function refreshUserBalanceForSuccessfulOrder(nextOrder: ResolvedOrder | null): void {
    if (!nextOrder || userBalanceRefreshStarted.current || normalizeOrderStatus(nextOrder.status) !== 'COMPLETED') {
      return
    }
    if ('order_type' in nextOrder && nextOrder.order_type !== 'balance') {
      return
    }

    userBalanceRefreshStarted.current = true
    void authStore.refreshUser().catch(() => {
      // The order result remains authoritative even if refreshing profile data fails.
    })
  }

  /** 充值赠送额度（USD）；老接口/订阅订单没有该字段时视为 0 */
  function orderBonusAmount(target: unknown): number {
    const value = (target as { bonus_amount?: unknown } | null | undefined)?.bonus_amount
    return typeof value === 'number' && Number.isFinite(value) && value > 0 ? value : 0
  }

  function clearStatusRefreshTimer(): void {
    if (statusRefreshTimer.current !== null) {
      clearTimeout(statusRefreshTimer.current)
      statusRefreshTimer.current = null
    }
  }

  function clearRecoverySnapshot(): void {
    if (typeof window === 'undefined') return
    clearPaymentRecoverySnapshot(window.localStorage, PAYMENT_RECOVERY_STORAGE_KEY)
  }

  function clearRecoverySnapshotForTerminalStatus(status: string | null | undefined): void {
    if (!status) return
    if (!isPendingStatus(status)) {
      clearRecoverySnapshot()
    }
  }

  function scheduleStatusRefresh(refreshOrder: (() => Promise<ResolvedOrder | null>) | null): void {
    clearStatusRefreshTimer()
    if (!refreshOrder || !isPendingStatus(orderRef.current?.status) || refreshAttempts.current >= STATUS_REFRESH_MAX_ATTEMPTS) {
      return
    }

    statusRefreshTimer.current = setTimeout(() => {
      void (async () => {
        refreshAttempts.current += 1
        const refreshedOrder = await refreshOrder()
        if (refreshedOrder) {
          setResolvedOrder(refreshedOrder)
          clearRecoverySnapshotForTerminalStatus(refreshedOrder.status)
        }

        if (isPendingStatus(orderRef.current?.status)) {
          scheduleStatusRefresh(refreshOrder)
        }
      })()
    }, STATUS_REFRESH_INTERVAL_MS)
  }

  useEffect(() => {
    let cancelled = false
    async function resolveOrderFromResumeToken(resumeToken: string): Promise<ResolvedOrder | null> {
      try {
        const result = await paymentAPI.resolveOrderPublicByResumeToken(resumeToken)
        return result.data
      } catch {
        return null
      }
    }

    async function resolveOrderFromOutTradeNo(outTradeNo: string): Promise<ResolvedOrder | null> {
      try {
        const result = await paymentAPI.verifyOrder(outTradeNo)
        return result.data
      } catch {
        try {
          const result = await paymentAPI.verifyOrderPublic(outTradeNo)
          return result.data
        } catch {
          return null
        }
      }
    }

    async function run() {
      const resumeToken = searchParams.get('resume_token') || ''
      const routeOrderId = Number(searchParams.get('order_id')) || 0
      let outTradeNo = searchParams.get('out_trade_no') || ''
      let orderId = 0
      let resumeTokenLookupFailed = false

      const restored = restoreRecoverySnapshot({
        resumeToken,
        routeOrderId,
        routeOutTradeNo: outTradeNo,
      })
      if (restored?.orderId) {
        orderId = restored.orderId
      }
      if (restored?.currency) {
        setCurrency(normalizePaymentCurrency(restored.currency))
      }
      if (!outTradeNo && restored?.outTradeNo) {
        outTradeNo = restored.outTradeNo
      }

      if (resumeToken) {
        const resolvedOrder = await resolveOrderFromResumeToken(resumeToken)
        if (resolvedOrder) {
          setResolvedOrder(resolvedOrder)
          if (!orderId) {
            orderId = hasOrderId(resolvedOrder) ? resolvedOrder.id : 0
          }
        } else if (routeOrderId > 0) {
          resumeTokenLookupFailed = true
          orderId = routeOrderId
        } else {
          resumeTokenLookupFailed = true
        }
      } else if (routeOrderId > 0) {
        orderId = routeOrderId
      }

      const hasLegacyFallbackContext = (searchParams.get('trade_status') || '').trim() !== ''
      const shouldUsePublicOutTradeNo = outTradeNo !== '' && (hasLegacyFallbackContext || routeOrderId > 0 || orderId > 0)

      if (!orderRef.current && orderId && (!resumeToken || routeOrderId > 0)) {
        try {
          setResolvedOrder(await paymentStore.pollOrderStatus(orderId))
        } catch {
          // Order lookup failed, will try legacy fallback below when possible.
        }
      }

      if (!orderRef.current && shouldUsePublicOutTradeNo && (!resumeToken || resumeTokenLookupFailed)) {
        const legacyOrder = await resolveOrderFromOutTradeNo(outTradeNo)
        if (legacyOrder) {
          setResolvedOrder(legacyOrder)
          if (!orderId) {
            orderId = hasOrderId(legacyOrder) ? legacyOrder.id : 0
          }
        }
      }

      let resolvedReturnInfo: ReturnInfo | null = null
      if (!orderRef.current && !orderId && outTradeNo && hasLegacyFallbackContext) {
        resolvedReturnInfo = {
          outTradeNo,
          money: searchParams.get('money') || '',
          type: searchParams.get('type') || '',
          tradeStatus: searchParams.get('trade_status') || '',
        }
        setReturnInfo(resolvedReturnInfo)
      }

      const refreshOrder = async (): Promise<ResolvedOrder | null> => {
        if (resumeToken) {
          const resolvedOrder = await resolveOrderFromResumeToken(resumeToken)
          if (resolvedOrder) {
            return resolvedOrder
          }
        }

        if (orderId) {
          try {
            return await paymentStore.pollOrderStatus(orderId)
          } catch {
            // Fall through to legacy public verification when order polling is unavailable.
          }
        }

        if (shouldUsePublicOutTradeNo) {
          return await resolveOrderFromOutTradeNo(outTradeNo)
        }

        return null
      }

      if (isPendingStatus(orderRef.current?.status)) {
        scheduleStatusRefresh(refreshOrder)
      } else if (orderRef.current) {
        clearRecoverySnapshotForTerminalStatus(orderRef.current.status)
      } else if (resolvedReturnInfo) {
        clearRecoverySnapshot()
      }
      if (!cancelled) setLoading(false)
    }

    void run()
    return () => {
      cancelled = true
      clearStatusRefreshTimer()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-canvas p-4 md:p-6 lg:p-8 dark:surface-inset">
        <div className="card w-full min-w-0 max-w-md space-y-6 p-5 sm:p-8">
          <div className="flex items-center justify-center py-20">
            <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary-500 border-t-transparent"></div>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-canvas p-4 md:p-6 lg:p-8 dark:surface-inset">
      <div className="card w-full min-w-0 max-w-md space-y-6 p-5 sm:p-8">
        {/* Status Icon */}
        <div className="text-center">
          {isSuccess ? (
            <div className="mx-auto flex h-20 w-20 items-center justify-center rounded-full bg-green-100/[0.15] dark:bg-green-900/30">
              <svg className="h-10 w-10 text-green-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
              </svg>
            </div>
          ) : isPending ? (
            <div className="mx-auto flex h-20 w-20 items-center justify-center rounded-full bg-yellow-100/[0.15] dark:bg-yellow-900/30">
              <div className="h-10 w-10 animate-spin rounded-full border-4 border-yellow-500 border-t-transparent"></div>
            </div>
          ) : (
            <div className="mx-auto flex h-20 w-20 items-center justify-center rounded-full bg-red-100/[0.15] dark:bg-red-900/30">
              <svg className="h-10 w-10 text-red-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
              </svg>
            </div>
          )}
          <h2 className="mt-4 text-2xl font-bold text-gray-900 dark:text-white">
            {statusTitle}
          </h2>
          {isPending && (
            <p className="mt-2 text-sm surface-muted">
              {t('payment.result.processingHint')}
            </p>
          )}
        </div>
        {/* Order Info */}
        {order ? (
          <div className="rounded-[var(--surface-radius-panel)] surface-inset p-4">
            <div className="space-y-3 text-sm">
              {hasOrderId(order) && (
                <div className="flex justify-between">
                  <span className="surface-muted">{t('payment.orders.orderId')}</span>
                  <span className="font-medium text-gray-900 dark:text-white">#{order.id}</span>
                </div>
              )}
              {order.out_trade_no && (
                <div className="flex justify-between gap-3">
                  <span className="shrink-0 surface-muted">{t('payment.orders.orderNo')}</span>
                  <span className="min-w-0 break-all text-right font-medium text-gray-900 dark:text-white">{order.out_trade_no}</span>
                </div>
              )}
              {hasAmountFields(order) && (
                <>
                  <div className="flex justify-between">
                    <span className="surface-muted">{t('payment.orders.baseAmount')}</span>
                    <span className="font-medium text-gray-900 dark:text-white">{formatGatewayAmount(baseAmount)}</span>
                  </div>
                  {order.fee_rate > 0 && (
                    <div className="flex justify-between">
                      <span className="surface-muted">{t('payment.orders.fee')} ({order.fee_rate}%)</span>
                      <span className="font-medium text-gray-900 dark:text-white">{formatGatewayAmount(feeAmount)}</span>
                    </div>
                  )}
                  <div className="flex justify-between">
                    <span className="surface-muted">{t('payment.orders.payAmount')}</span>
                    <span className="font-bold text-primary-600 dark:text-primary-400">{formatGatewayAmount(order.pay_amount)}</span>
                  </div>
                  {orderBonusAmount(order) > 0 && (
                    <div className="flex justify-between">
                      <span className="surface-muted">{t('payment.orders.bonusAmount')}</span>
                      <span className="font-medium text-amber-600 dark:text-amber-400">+${orderBonusAmount(order).toFixed(2)}</span>
                    </div>
                  )}
                  {(order.amount !== order.pay_amount || orderBonusAmount(order) > 0) && (
                    <div className="flex justify-between">
                      <span className="surface-muted">{t('payment.orders.creditedAmount')}</span>
                      <span className="font-medium text-gray-900 dark:text-white">{order.order_type === 'balance' ? '$' + order.amount.toFixed(2) : formatGatewayAmount(order.amount)}</span>
                    </div>
                  )}
                </>
              )}
              {hasPaymentType(order) && (
                <div className="flex justify-between">
                  <span className="surface-muted">{t('payment.orders.paymentMethod')}</span>
                  <span className="font-medium text-gray-900 dark:text-white">{t(paymentMethodI18nKey(order.payment_type), normalizedOrderPaymentType(order.payment_type))}</span>
                </div>
              )}
              <div className="flex justify-between">
                <span className="surface-muted">{t('payment.orders.status')}</span>
                <OrderStatusBadge status={displayOrderStatus(order.status)} />
              </div>
            </div>
          </div>
        ) : returnInfo ? (
          /* EasyPay return info (when no order loaded) */
          <div className="rounded-[var(--surface-radius-panel)] surface-inset p-4">
            <div className="space-y-3 text-sm">
              {returnInfo.outTradeNo && (
                <div className="flex justify-between gap-3">
                  <span className="shrink-0 surface-muted">{t('payment.orders.orderId')}</span>
                  <span className="min-w-0 break-all text-right font-medium text-gray-900 dark:text-white">{returnInfo.outTradeNo}</span>
                </div>
              )}
              {returnInfo.money && (
                <div className="flex justify-between">
                  <span className="surface-muted">{t('payment.orders.payAmount')}</span>
                  <span className="font-medium text-gray-900 dark:text-white">{formatGatewayAmount(Number(returnInfo.money) || 0)}</span>
                </div>
              )}
              {returnInfo.type && (
                <div className="flex justify-between">
                  <span className="surface-muted">{t('payment.orders.paymentMethod')}</span>
                  <span className="font-medium text-gray-900 dark:text-white">{t(paymentMethodI18nKey(returnInfo.type), normalizedOrderPaymentType(returnInfo.type))}</span>
                </div>
              )}
            </div>
          </div>
        ) : null}
        {/* Actions */}
        <div className="flex flex-col gap-3 sm:flex-row">
          <button className="btn btn-secondary flex-1" onClick={() => { void navigate('/purchase') }}>{t('payment.result.backToRecharge')}</button>
          <button className="btn btn-primary flex-1" onClick={() => { void navigate('/orders') }}>{t('payment.result.viewOrders')}</button>
        </div>
      </div>
    </div>
  )
}

function hasOrderId(nextOrder: ResolvedOrder | null): nextOrder is PaymentOrder {
  return !!nextOrder && 'id' in nextOrder && typeof nextOrder.id === 'number'
}

function hasAmountFields(nextOrder: ResolvedOrder | null): nextOrder is PaymentOrder {
  return !!nextOrder && 'pay_amount' in nextOrder && typeof nextOrder.pay_amount === 'number' && 'amount' in nextOrder && typeof nextOrder.amount === 'number'
}

function hasPaymentType(nextOrder: ResolvedOrder | null): nextOrder is PaymentOrder {
  return !!nextOrder && 'payment_type' in nextOrder && typeof nextOrder.payment_type === 'string' && nextOrder.payment_type.trim() !== ''
}

function normalizeOrderStatus(status: string | null | undefined): string {
  return String(status || '').trim().toUpperCase()
}

function displayOrderStatus(status: string): OrderStatus {
  return normalizeOrderStatus(status) as OrderStatus
}

function isSuccessStatus(status: string | null | undefined): boolean {
  return SUCCESS_STATUSES.has(normalizeOrderStatus(status))
}

function isPendingStatus(status: string | null | undefined): boolean {
  return PENDING_STATUSES.has(normalizeOrderStatus(status))
}

function restoreRecoverySnapshot(context: {
  resumeToken: string
  routeOrderId: number
  routeOutTradeNo: string
}) {
  if (typeof window === 'undefined') {
    return null
  }

  const rawSnapshot = window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)
  if (!rawSnapshot) {
    return null
  }

  if (context.resumeToken) {
    return readPaymentRecoverySnapshot(rawSnapshot, {
      resumeToken: context.resumeToken,
    })
  }

  if (!context.routeOrderId && !context.routeOutTradeNo) {
    return null
  }

  const restored = readPaymentRecoverySnapshot(rawSnapshot)
  if (!restored) {
    return null
  }

  if (context.routeOrderId > 0 && restored.orderId !== context.routeOrderId) {
    return null
  }

  if (context.routeOutTradeNo && restored.outTradeNo !== context.routeOutTradeNo) {
    return null
  }

  return restored
}
