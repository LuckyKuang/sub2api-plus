import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { QRCodeCanvas } from 'qrcode.react'
import { paymentAPI } from '@/api/payment'
import { usePaymentStore } from '@/stores/payment'
import { useAppStore } from '@/stores/app'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { isBuiltInAlipayMethod, isBuiltInWxpayMethod } from './paymentProviderConfig'
import alipayIcon from '@/assets/icons/alipay.svg'
import wxpayIcon from '@/assets/icons/wxpay.svg'

export default function PaymentQRCodeView() {
  const { t } = useTranslation()
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const paymentStore = usePaymentStore()
  const appStore = useAppStore()

  const qrCanvas = useRef<HTMLCanvasElement | null>(null)
  const [qrUrl, setQrUrl] = useState('')
  const [payUrl, setPayUrl] = useState('')
  const [orderId, setOrderId] = useState(0)
  const [remainingSeconds, setRemainingSeconds] = useState(0)
  const [expired, setExpired] = useState(false)
  const [cancelling, setCancelling] = useState(false)
  const [paymentType, setPaymentType] = useState('')

  const pollTimer = useRef<ReturnType<typeof setInterval> | null>(null)
  const countdownTimer = useRef<ReturnType<typeof setInterval> | null>(null)
  const pollInFlight = useRef(false)

  const countdownDisplay = (() => {
    const m = Math.floor(remainingSeconds / 60)
    const s = remainingSeconds % 60
    return m.toString().padStart(2, '0') + ':' + s.toString().padStart(2, '0')
  })()

  const isAlipay = isBuiltInAlipayMethod(paymentType)
  const isWxpay = isBuiltInWxpayMethod(paymentType)

  const scanTitle = isAlipay
    ? t('payment.qr.scanAlipay')
    : isWxpay ? t('payment.qr.scanWxpay') : t('payment.qr.scanToPay')

  const scanHint = isAlipay
    ? t('payment.qr.scanAlipayHint')
    : isWxpay ? t('payment.qr.scanWxpayHint') : ''

  function getLogoForType(): string | null {
    if (isAlipay) return alipayIcon
    if (isWxpay) return wxpayIcon
    return null
  }

  const logoSource = getLogoForType()

  // Draw logo in center of QR code
  useEffect(() => {
    if (!qrUrl || !logoSource) return
    const canvas = qrCanvas.current
    if (!canvas) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return

    const img = new Image()
    img.src = logoSource
    img.onload = () => {
      const logoSize = 48
      const x = (canvas.width - logoSize) / 2
      const y = (canvas.height - logoSize) / 2
      // White background with rounded corners
      const pad = 5
      ctx.fillStyle = '#FFFFFF'
      ctx.beginPath()
      const r = 6
      ctx.moveTo(x - pad + r, y - pad)
      ctx.arcTo(x + logoSize + pad, y - pad, x + logoSize + pad, y + logoSize + pad, r)
      ctx.arcTo(x + logoSize + pad, y + logoSize + pad, x - pad, y + logoSize + pad, r)
      ctx.arcTo(x - pad, y + logoSize + pad, x - pad, y - pad, r)
      ctx.arcTo(x - pad, y - pad, x + logoSize + pad, y - pad, r)
      ctx.fill()
      // Draw logo
      ctx.drawImage(img, x, y, logoSize, logoSize)
    }
  }, [qrUrl, logoSource])

  async function pollStatus(pollOrderId: number) {
    if (!pollOrderId) return
    // 防重入：接口响应慢于 3 秒轮询间隔时避免并发重叠请求与重复跳转。
    if (pollInFlight.current) return
    pollInFlight.current = true
    try {
      const order = await paymentStore.pollOrderStatus(pollOrderId)
      if (!order) return
      // 定时器已被 cleanup 清除时不再执行终态跳转（响应可能在 cleanup 后才回来）。
      if (!pollTimer.current) return
      if (order.status === 'COMPLETED' || order.status === 'PAID') {
        cleanup()
        void navigate({ pathname: '/payment/result', search: `order_id=${pollOrderId}&status=success` })
      } else if (order.status === 'EXPIRED' || order.status === 'CANCELLED' || order.status === 'FAILED') {
        cleanup()
        setExpired(true)
      }
    } finally {
      pollInFlight.current = false
    }
  }

  function startCountdown(seconds: number) {
    setRemainingSeconds(Math.max(0, seconds))
    const deadline = Date.now() + Math.max(0, seconds) * 1000
    if (seconds <= 0) {
      setExpired(true)
      return
    }
    countdownTimer.current = setInterval(() => {
      setRemainingSeconds(Math.max(0, Math.floor((deadline - Date.now()) / 1000)))
      if (deadline - Date.now() <= 0) {
        setExpired(true)
        cleanup()
      }
    }, 1000)
  }

  async function handleCancel() {
    if (!orderId || cancelling) return
    setCancelling(true)
    try {
      await paymentAPI.cancelOrder(orderId)
      cleanup()
      void navigate('/purchase')
    } catch (err: unknown) {
      appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
    } finally {
      setCancelling(false)
    }
  }

  function cleanup() {
    if (pollTimer.current) { clearInterval(pollTimer.current); pollTimer.current = null }
    if (countdownTimer.current) { clearInterval(countdownTimer.current); countdownTimer.current = null }
  }

  useEffect(() => {
    const nextOrderId = Number(searchParams.get('order_id')) || 0
    const nextQrUrl = String(searchParams.get('qr') || '')
    const nextPayUrl = String(searchParams.get('pay_url') || '')
    const nextPaymentType = String(searchParams.get('payment_type') || '')

    setOrderId(nextOrderId)
    setQrUrl(nextQrUrl)
    setPayUrl(nextPayUrl)
    setPaymentType(nextPaymentType)

    // Calculate countdown from expiresAt
    const expiresAtStr = String(searchParams.get('expires_at') || '')
    let seconds = 30 * 60 // fallback: 30 minutes
    if (expiresAtStr) {
      const expiresAt = new Date(expiresAtStr)
      const now = new Date()
      seconds = Math.floor((expiresAt.getTime() - now.getTime()) / 1000)
    }
    startCountdown(seconds)
    pollTimer.current = setInterval(() => { void pollStatus(nextOrderId) }, 3000)
    return () => { cleanup() }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="card mx-auto flex w-full min-w-0 max-w-md flex-col items-center gap-6 p-5 sm:p-8">
      <h2 className="text-xl font-semibold text-gray-900 dark:text-white">
        {qrUrl ? scanTitle : t('payment.qr.payInNewWindow')}
      </h2>
      {qrUrl && (
        <div className="w-full rounded-[var(--surface-radius-panel)] surface-opaque p-3">
          <QRCodeCanvas
            ref={qrCanvas}
            value={qrUrl}
            size={256}
            marginSize={2}
            level={getLogoForType() ? 'M' : 'L'}
            className="mx-auto !h-auto max-w-full"
          />
        </div>
      )}
      {/* Scan prompt for QR code */}
      {qrUrl && !expired && scanHint && (
        <p className="text-center text-sm surface-muted">
          {scanHint}
        </p>
      )}
      {expired ? (
        <div className="text-center">
          <p className="text-lg font-medium text-red-500">{t('payment.qr.expired')}</p>
          <button className="btn btn-primary mt-4" onClick={() => { void navigate('/purchase') }}>{t('payment.result.backToRecharge')}</button>
        </div>
      ) : (
        <div className="text-center">
          <p className="text-sm surface-muted">{qrUrl ? t('payment.qr.expiresIn') : t('payment.qr.payInNewWindowHint')}</p>
          <p className="mt-1 text-2xl font-bold tabular-nums text-gray-900 dark:text-white">{countdownDisplay}</p>
          <p className="mt-2 text-sm surface-muted">{t('payment.qr.waitingPayment')}</p>
        </div>
      )}
      {payUrl && !qrUrl && !expired && (
        <a href={payUrl} target="_blank" rel="noopener noreferrer"
          className="btn btn-primary w-full py-3">
          {t('payment.qr.openPayWindow')}
        </a>
      )}
      {/* Cancel button */}
      {!expired && orderId > 0 && (
        <button className="btn btn-secondary w-full" disabled={cancelling} onClick={() => { void handleCancel() }}>
          {cancelling ? t('common.processing') : t('payment.qr.cancelOrder')}
        </button>
      )}
    </div>
  )
}
