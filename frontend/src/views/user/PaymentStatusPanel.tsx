import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { QRCodeCanvas } from 'qrcode.react'
import { Check, CircleCheck, ExternalLink, Download } from 'lucide-react'
import { cn } from '@/lib/utils'
import { paymentAPI } from '@/api/payment'
import { usePaymentStore } from '@/stores/payment'
import { useAppStore } from '@/stores/app'
import { extractI18nErrorMessage } from '@/utils/apiError'
import type { PaymentOrder } from '@/types/payment'
import { getPaymentPopupFeatures, isBuiltInAlipayMethod, isBuiltInWxpayMethod } from './paymentProviderConfig'
import { currencySymbol, formatPaymentAmount, normalizePaymentCurrency } from './paymentCurrency'
import {
  createAlipayDeepLinkLauncher,
  type AlipayDeepLinkLauncher,
  type AlipayDeepLinkState,
} from './alipayDeepLink'
import alipayIcon from '@/assets/icons/alipay.svg'
import wxpayIcon from '@/assets/icons/wxpay.svg'
import paymentIcon from '@/assets/icons/payment.svg'

type PaymentOutcome = 'success' | 'cancelled' | 'expired'

export interface PaymentStatusPanelProps {
  orderId: number
  amount?: number
  payAmount?: number
  qrCode: string
  expiresAt: string
  paymentType: string
  payUrl?: string
  orderType?: string
  currency?: string
  outTradeNo?: string
  mobileAlipayDeepLink?: boolean
  onDone: () => void
  onSuccess: () => void
  onSettled: (outcome: PaymentOutcome) => void
}

const VERIFY_RETRY_INTERVAL_MS = 15000
const VERIFY_RETRY_MAX_ATTEMPTS = 6

export function PaymentStatusPanel({
  orderId,
  amount,
  payAmount,
  qrCode,
  expiresAt,
  paymentType,
  payUrl,
  orderType,
  currency,
  outTradeNo,
  mobileAlipayDeepLink,
  onDone,
  onSuccess,
  onSettled,
}: PaymentStatusPanelProps) {
  const { t, i18n } = useTranslation()
  const paymentStore = usePaymentStore()
  const appStore = useAppStore()

  const qrCanvas = useRef<HTMLCanvasElement | null>(null)
  const [qrUrl] = useState(qrCode)
  const [remainingSeconds, setRemainingSeconds] = useState(0)
  const [cancelling, setCancelling] = useState(false)
  const [paidOrder, setPaidOrder] = useState<PaymentOrder | null>(null)
  const [deepLinkState, setDeepLinkState] = useState<AlipayDeepLinkState>('idle')
  const [deepLinkFallbackVisible, setDeepLinkFallbackVisible] = useState(false)
  const [outcome, setOutcomeState] = useState<PaymentOutcome | null>(null)

  const outcomeRef = useRef<PaymentOutcome | null>(null)
  const pollTimer = useRef<ReturnType<typeof setInterval> | null>(null)
  const countdownTimer = useRef<ReturnType<typeof setInterval> | null>(null)
  const pollInFlight = useRef(false)
  const verifyAttempts = useRef(0)
  const lastVerifyAt = useRef(0)
  const alipayLauncher = useRef<AlipayDeepLinkLauncher | null>(null)
  const onDoneRef = useRef(onDone)
  const onSuccessRef = useRef(onSuccess)
  const onSettledRef = useRef(onSettled)
  onDoneRef.current = onDone
  onSuccessRef.current = onSuccess
  onSettledRef.current = onSettled

  const paymentCurrency = normalizePaymentCurrency(currency)
  const creditedAmountSymbol = currencySymbol('USD')
  const localeCode = i18n.resolvedLanguage || i18n.language

  const isAlipay = isBuiltInAlipayMethod(paymentType)
  const isWxpay = isBuiltInWxpayMethod(paymentType)
  const isMobileAlipayDeepLink = mobileAlipayDeepLink === true && isAlipay && !!qrUrl
  const showQRCode = !!qrUrl && (!isMobileAlipayDeepLink || deepLinkFallbackVisible)

  const qrBorderClass = isAlipay
    ? 'border-[#00AEEF] bg-blue-50/[0.15] dark:border-[#00AEEF]/70 dark:bg-blue-950/20'
    : isWxpay
      ? 'border-[#2BB741] bg-green-50/[0.15] dark:border-[#2BB741]/70 dark:bg-green-950/20'
      : 'border-gray-200 surface-inset dark:border-dark-600 dark:surface-inset'

  const qrLogoBgClass = isAlipay ? 'bg-[#00AEEF]' : isWxpay ? 'bg-[#2BB741]' : 'bg-gray-400'
  const qrLogoIcon = isAlipay ? alipayIcon : isWxpay ? wxpayIcon : paymentIcon

  const scanTitle = isAlipay
    ? t('payment.qr.scanAlipay')
    : isWxpay ? t('payment.qr.scanWxpay') : t('payment.qr.scanToPay')

  const scanHint = isAlipay
    ? t('payment.qr.scanAlipayHint')
    : isWxpay ? t('payment.qr.scanWxpayHint') : ''

  const m = Math.floor(remainingSeconds / 60)
  const s = remainingSeconds % 60
  const countdownDisplay = m.toString().padStart(2, '0') + ':' + s.toString().padStart(2, '0')

  const displayPaymentAmount = formatPaymentAmount(payAmount || amount || 0, paymentCurrency, localeCode)
  const displayOrderNumber = outTradeNo || `#${orderId}`

  function formatGatewayAmount(value: number, currencyCode?: string | null): string {
    return formatPaymentAmount(value, currencyCode || paymentCurrency, localeCode)
  }

  function isSuccessStatus(status: string | null | undefined): boolean {
    return status === 'COMPLETED' || status === 'PAID' || status === 'RECHARGING'
  }

  function reopenPopup() {
    if (payUrl) {
      const win = window.open(payUrl, 'paymentPopup', getPaymentPopupFeatures())
      if (!win || win.closed) {
        window.location.href = payUrl
      }
    }
  }

  function setOutcome(next: PaymentOutcome) {
    if (outcomeRef.current === next) return
    outcomeRef.current = next
    setOutcomeState(next)
    onSettledRef.current(next)
  }

  function updateDeepLinkState(state: AlipayDeepLinkState) {
    setDeepLinkState(state)
    if (state === 'fallback') {
      setDeepLinkFallbackVisible(true)
    } else if (state === 'backgrounded') {
      setDeepLinkFallbackVisible(false)
    }
  }

  function reopenAlipay() {
    alipayLauncher.current?.launch()
  }

  function saveQRCode() {
    const canvas = qrCanvas.current
    if (!canvas) return
    const link = document.createElement('a')
    link.href = canvas.toDataURL('image/png')
    link.download = `alipay-${outTradeNo || orderId}.png`
    document.body.appendChild(link)
    link.click()
    link.remove()
  }

  async function tryRecoverPendingOrder(order: PaymentOrder): Promise<PaymentOrder> {
    if (!isWxpay && !isAlipay) return order
    const orderOutTradeNo = String(order.out_trade_no || '').trim()
    if (!orderOutTradeNo) return order
    const normalizedStatus = String(order.status || '').trim().toUpperCase()
    if (normalizedStatus !== 'PENDING') return order
    const now = Date.now()
    if (verifyAttempts.current >= VERIFY_RETRY_MAX_ATTEMPTS || now - lastVerifyAt.current < VERIFY_RETRY_INTERVAL_MS) {
      return order
    }

    lastVerifyAt.current = now
    verifyAttempts.current += 1
    try {
      const result = await paymentAPI.verifyOrder(orderOutTradeNo)
      return result.data ?? order
    } catch {
      return order
    }
  }

  function cleanup() {
    if (pollTimer.current) { clearInterval(pollTimer.current); pollTimer.current = null }
    if (countdownTimer.current) { clearInterval(countdownTimer.current); countdownTimer.current = null }
    alipayLauncher.current?.dispose()
    alipayLauncher.current = null
  }

  async function pollStatus() {
    if (!orderId || outcomeRef.current) return
    // 防重入：接口（含 verifyOrder 二次确认）响应慢于 3 秒轮询间隔时避免并发重叠请求。
    if (pollInFlight.current) return
    pollInFlight.current = true
    try {
      let order = await paymentStore.pollOrderStatus(orderId)
      if (!order) return
      // 已进入终态则不再处理迟到的响应。
      if (outcomeRef.current) return
      order = await tryRecoverPendingOrder(order)
      if (outcomeRef.current) return
      if (isSuccessStatus(order.status)) {
        cleanup()
        setPaidOrder(order)
        setOutcome('success')
        onSuccessRef.current()
      } else if (order.status === 'CANCELLED') {
        cleanup()
        setOutcome('cancelled')
      } else if (order.status === 'EXPIRED' || order.status === 'FAILED') {
        cleanup()
        setOutcome('expired')
      }
    } finally {
      pollInFlight.current = false
    }
  }

  function startCountdown(seconds: number) {
    setRemainingSeconds(Math.max(0, seconds))
    const deadline = Date.now() + Math.max(0, seconds) * 1000
    if (seconds <= 0) { setOutcome('expired'); return }
    countdownTimer.current = setInterval(() => {
      setRemainingSeconds(Math.max(0, Math.floor((deadline - Date.now()) / 1000)))
      if (deadline - Date.now() <= 0) { setOutcome('expired'); cleanup() }
    }, 1000)
  }

  async function handleCancel() {
    if (!orderId || cancelling) return
    setCancelling(true)
    try {
      await paymentAPI.cancelOrder(orderId)
      cleanup()
      setOutcome('cancelled')
    } catch (err: unknown) {
      appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
    } finally {
      setCancelling(false)
    }
  }

  function handleDone() { cleanup(); onDoneRef.current() }

  // Initialize on mount
  useEffect(() => {
    verifyAttempts.current = 0
    lastVerifyAt.current = 0
    let seconds = 30 * 60
    if (expiresAt) {
      seconds = Math.floor((new Date(expiresAt).getTime() - Date.now()) / 1000)
    }
    startCountdown(seconds)
    pollTimer.current = setInterval(() => { void pollStatus() }, 3000)
    return () => { cleanup() }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (!mobileAlipayDeepLink || !isAlipay || !qrUrl) return
    const launcher = createAlipayDeepLinkLauncher({
      qrCode: qrUrl,
      document,
      lifecycleTarget: window,
      userAgent: window.navigator.userAgent,
      assignLocation: (url) => window.location.assign(url),
      onStateChange: updateDeepLinkState,
    })
    alipayLauncher.current = launcher
    launcher.launch()
    return () => {
      launcher.dispose()
      alipayLauncher.current = null
    }
  }, [mobileAlipayDeepLink, isAlipay, qrUrl])

  // ═══ Terminal States: show result, user clicks to return ═══
  if (outcome === 'success') {
    return (
      <div className="space-y-4">
        <div className="card workspace-shell p-6">
          <div className="flex flex-col items-center space-y-4 py-4">
            <div className="flex h-16 w-16 items-center justify-center rounded-full bg-green-100/[0.15] dark:bg-green-900/30">
              <Check size={24} strokeWidth={1.5} className="text-green-500" />
            </div>
            <p className="text-lg font-bold text-gray-900 dark:text-white">
              {orderType === 'subscription' ? t('payment.result.subscriptionSuccess') : t('payment.result.success')}
            </p>
            {paidOrder && (
              <div className="w-full rounded-xl surface-inset p-4">
                <div className="space-y-2 text-sm">
                  <div className="flex justify-between">
                    <span className="surface-muted">{t('payment.orders.orderId')}</span>
                    <span className="font-medium text-gray-900 dark:text-white">#{paidOrder.id}</span>
                  </div>
                  {paidOrder.out_trade_no && (
                    <div className="flex justify-between">
                      <span className="surface-muted">{t('payment.orders.orderNo')}</span>
                      <span className="font-medium text-gray-900 dark:text-white">{paidOrder.out_trade_no}</span>
                    </div>
                  )}
                  <div className="flex justify-between">
                    <span className="surface-muted">{t('payment.orders.amount')}</span>
                    <span className="font-medium text-gray-900 dark:text-white">
                      {creditedAmountSymbol}{paidOrder.amount.toFixed(2)}
                      {(paidOrder.bonus_amount ?? 0) > 0 && (
                        <span className="ml-1 text-xs font-normal text-amber-600 dark:text-amber-400">
                          ({t('payment.orders.bonusIncluded', { amount: creditedAmountSymbol + (paidOrder.bonus_amount ?? 0).toFixed(2) })})
                        </span>
                      )}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="surface-muted">{t('payment.orders.payAmount')}</span>
                    <span className="font-medium text-gray-900 dark:text-white">{formatGatewayAmount(paidOrder.pay_amount, paidOrder.currency)}</span>
                  </div>
                </div>
              </div>
            )}
            <button className="btn btn-primary" onClick={handleDone}>{t('common.confirm')}</button>
          </div>
        </div>
      </div>
    )
  }

  if (outcome === 'cancelled') {
    return (
      <div className="space-y-4">
        <div className="card workspace-shell p-6">
          <div className="flex flex-col items-center space-y-4 py-4">
            <div className="flex h-16 w-16 items-center justify-center rounded-full surface-inset">
              <svg className="h-8 w-8 surface-muted" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
              </svg>
            </div>
            <p className="text-lg font-bold text-gray-900 dark:text-white">{t('payment.qr.cancelled')}</p>
            <p className="text-sm surface-muted">{t('payment.qr.cancelledDesc')}</p>
            <button className="btn btn-primary" onClick={handleDone}>{t('common.confirm')}</button>
          </div>
        </div>
      </div>
    )
  }

  if (outcome === 'expired') {
    return (
      <div className="space-y-4">
        <div className="card workspace-shell p-6">
          <div className="flex flex-col items-center space-y-4 py-4">
            <div className="flex h-16 w-16 items-center justify-center rounded-full bg-orange-100/[0.15] dark:bg-orange-900/30">
              <svg className="h-8 w-8 text-orange-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                <path strokeLinecap="round" strokeLinejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
            </div>
            <p className="text-lg font-bold text-gray-900 dark:text-white">{t('payment.qr.expired')}</p>
            <p className="text-sm surface-muted">{t('payment.qr.expiredDesc')}</p>
            <button className="btn btn-primary" onClick={handleDone}>{t('common.confirm')}</button>
          </div>
        </div>
      </div>
    )
  }

  // ═══ Active States: QR or Popup waiting ═══

  // Mobile Alipay app handoff. The QR fallback stays hidden until launch timeout.
  if (isMobileAlipayDeepLink) {
    if (!deepLinkFallbackVisible) {
      return (
        <div className="space-y-4">
          <div className="card workspace-shell p-6">
            <div className="flex flex-col items-center space-y-4 py-4 text-center">
              {deepLinkState === 'launching' ? (
                <div className="h-10 w-10 animate-spin rounded-full border-4 border-[#00AEEF] border-t-transparent"></div>
              ) : (
                <div className="flex h-12 w-12 items-center justify-center rounded-full bg-blue-50/[0.15] dark:bg-blue-950/30">
                  <CircleCheck size={24} strokeWidth={1.5} className="text-[#00AEEF]" />
                </div>
              )}
              <p className="text-lg font-semibold text-gray-900 dark:text-white">
                {deepLinkState === 'backgrounded' ? t('payment.qr.alipayContinueInApp') : t('payment.qr.alipayOpening')}
              </p>
              <p className="text-sm surface-muted">{t('payment.qr.alipayWaitingHint')}</p>
              {deepLinkState === 'backgrounded' && (
                <button
                  data-test="reopen-alipay"
                  className="btn btn-alipay inline-flex items-center gap-2 text-sm"
                  onClick={reopenAlipay}
                >
                  <ExternalLink size={16} strokeWidth={1.5} />
                  {t('payment.qr.reopenAlipay')}
                </button>
              )}
            </div>
          </div>
          <div className="card workspace-shell p-4 text-center">
            <p className="text-sm surface-muted">{t('payment.qr.expiresIn')}</p>
            <p className="mt-1 text-2xl font-bold tabular-nums text-gray-900 dark:text-white">{countdownDisplay}</p>
            <p className="mt-1 text-xs surface-muted">{t('payment.qr.waitingPayment')}</p>
          </div>
        </div>
      )
    }

    return (
      <div className="space-y-4">
        <div data-test="alipay-qr-fallback" className="card workspace-shell p-6">
          <div className="flex flex-col items-center space-y-4">
            <div className="text-center">
              <p className="text-lg font-semibold text-gray-900 dark:text-white">{t('payment.qr.alipayFallbackTitle')}</p>
              <p className="mt-1 text-sm surface-muted">{t('payment.qr.alipayFallbackHint')}</p>
            </div>
            <div className="w-full space-y-2 border-y border-gray-100 py-3 text-sm dark:border-dark-600">
              <div className="flex items-start justify-between gap-4">
                <span className="surface-muted">{t('payment.orders.payAmount')}</span>
                <span className="font-semibold text-gray-900 dark:text-white">{displayPaymentAmount}</span>
              </div>
              <div className="flex items-start justify-between gap-4">
                <span className="surface-muted">{t('payment.orders.orderNo')}</span>
                <span className="max-w-[70%] break-all text-right font-mono text-xs text-gray-900 dark:text-white">
                  {displayOrderNumber}
                </span>
              </div>
              <div className="flex items-start justify-between gap-4">
                <span className="surface-muted">{t('payment.qr.expiresIn')}</span>
                <span className="font-semibold tabular-nums text-gray-900 dark:text-white">{countdownDisplay}</span>
              </div>
            </div>
            <div className={cn('relative rounded-lg border surface-opaque p-4', qrBorderClass)}>
              <QRCodeCanvas
                ref={qrCanvas}
                value={qrUrl}
                size={220}
                marginSize={2}
                level="M"
                className="mx-auto"
              />
              <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
                <span className={cn('rounded-full p-2 shadow ring-2 ring-white', qrLogoBgClass)}>
                  <img src={qrLogoIcon} alt="" className="h-5 w-5 brightness-0 invert" />
                </span>
              </div>
            </div>
            <p className="text-center text-sm leading-6 surface-muted">
              {t('payment.qr.alipaySaveAndScanHint')}
            </p>
            <div className="grid w-full gap-2 sm:grid-cols-2">
              <button
                data-test="reopen-alipay"
                className="btn btn-alipay inline-flex items-center justify-center gap-2"
                onClick={reopenAlipay}
              >
                <ExternalLink size={16} strokeWidth={1.5} />
                {t('payment.qr.reopenAlipay')}
              </button>
              <button
                data-test="save-alipay-qr"
                className="btn btn-secondary inline-flex items-center justify-center gap-2"
                onClick={saveQRCode}
              >
                <Download size={16} strokeWidth={1.5} />
                {t('payment.qr.saveQRCode')}
              </button>
            </div>
            <button className="btn btn-secondary w-full" onClick={handleDone}>
              {t('payment.result.backToRecharge')}
            </button>
          </div>
        </div>
      </div>
    )
  }

  // QR Code Mode
  if (showQRCode) {
    return (
      <div className="space-y-4">
        <div className="card workspace-shell p-6">
          <div className="flex flex-col items-center space-y-4">
            <p className="text-lg font-semibold text-gray-900 dark:text-white">{scanTitle}</p>
            <div className={cn('relative rounded-lg border surface-opaque p-4', qrBorderClass)}>
              <QRCodeCanvas
                ref={qrCanvas}
                value={qrUrl}
                size={220}
                marginSize={2}
                level="M"
                className="mx-auto"
              />
              {/* Brand logo overlay */}
              <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
                <span className={cn('rounded-full p-2 shadow ring-2 ring-white', qrLogoBgClass)}>
                  <img src={qrLogoIcon} alt="" className="h-5 w-5 brightness-0 invert" />
                </span>
              </div>
            </div>
            {scanHint && <p className="text-center text-sm surface-muted">{scanHint}</p>}
            {payUrl && (
              <button className="btn btn-secondary text-sm" onClick={reopenPopup}>
                {t('payment.qr.openPayWindow')}
              </button>
            )}
          </div>
        </div>
        <div className="card workspace-shell p-4 text-center">
          <p className="text-sm surface-muted">{t('payment.qr.expiresIn')}</p>
          <p className="mt-1 text-2xl font-bold tabular-nums text-gray-900 dark:text-white">{countdownDisplay}</p>
          <p className="mt-1 text-xs surface-muted">{t('payment.qr.waitingPayment')}</p>
        </div>
        <button className="btn btn-secondary w-full" disabled={cancelling} onClick={handleCancel}>
          {cancelling ? t('common.processing') : t('payment.qr.cancelOrder')}
        </button>
      </div>
    )
  }

  // Waiting for Popup/Redirect Mode
  return (
    <div className="space-y-4">
      <div className="card workspace-shell p-6">
        <div className="flex flex-col items-center space-y-4 py-4">
          <div className="h-10 w-10 animate-spin rounded-full border-4 border-primary-500 border-t-transparent"></div>
          <p className="text-sm surface-muted">{t('payment.qr.payInNewWindowHint')}</p>
          {payUrl && (
            <button className="btn btn-secondary text-sm" onClick={reopenPopup}>
              {t('payment.qr.openPayWindow')}
            </button>
          )}
        </div>
      </div>
      <div className="card workspace-shell p-4 text-center">
        <p className="mt-1 text-2xl font-bold tabular-nums text-gray-900 dark:text-white">{countdownDisplay}</p>
        <p className="mt-1 text-xs surface-muted">{t('payment.qr.waitingPayment')}</p>
      </div>
      <button className="btn btn-secondary w-full" disabled={cancelling} onClick={handleCancel}>
        {cancelling ? t('common.processing') : t('payment.qr.cancelOrder')}
      </button>
    </div>
  )
}

export default PaymentStatusPanel
