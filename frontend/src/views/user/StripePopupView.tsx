import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { buildApiUrl } from '@/api/url'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { isMobileDevice } from '@/utils/device'

interface StripeWithWechatPay {
  confirmWechatPayPayment(clientSecret: string, options: Record<string, unknown>): Promise<{ error?: { message?: string }; paymentIntent?: { status: string } }>
}

const METHOD_COLORS: Record<string, string> = {
  alipay: '#00AEEF',
  wechat_pay: '#07C160',
}
const DEFAULT_METHOD_COLOR = '#635bff'

export default function StripePopupView() {
  const { t } = useTranslation()
  const [searchParams] = useSearchParams()

  const orderId = String(searchParams.get('order_id') || '')
  const method = String(searchParams.get('method') || 'alipay')
  const amount = String(searchParams.get('amount') || '')

  const methodColor = METHOD_COLORS[method] || DEFAULT_METHOD_COLOR

  const [error, setError] = useState('')
  const [success, setSuccess] = useState(false)
  const [hint, setHint] = useState(t('payment.stripePopup.redirecting'))

  const pollTimer = useRef<ReturnType<typeof setInterval> | null>(null)
  const initTimeoutTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const messageHandler = useRef<((event: MessageEvent) => void) | null>(null)

  function closeWindow() { window.close() }

  function clearInitTimeout() {
    if (initTimeoutTimer.current) {
      clearTimeout(initTimeoutTimer.current)
      initTimeoutTimer.current = null
    }
  }

  useEffect(() => {
    const handler = (event: MessageEvent) => {
      if (event.origin !== window.location.origin) return
      const data = event.data as { type?: string; clientSecret?: string; publishableKey?: string } | null
      if (data?.type !== 'STRIPE_POPUP_INIT') return
      // INIT 已到达，取消兜底超时，避免长时间的扫码支付被误判为超时。
      clearInitTimeout()
      if (messageHandler.current) {
        window.removeEventListener('message', messageHandler.current)
        messageHandler.current = null
      }
      void initStripe(data.clientSecret || '', data.publishableKey || '')
    }
    messageHandler.current = handler
    window.addEventListener('message', handler)

    if (window.opener) {
      window.opener.postMessage({ type: 'STRIPE_POPUP_READY' }, window.location.origin)
    }

    // 仅兜底「父窗口始终未发 STRIPE_POPUP_INIT」的场景。
    initTimeoutTimer.current = setTimeout(() => {
      if (!error && !success) {
        setError(t('payment.stripePopup.timeout'))
      }
    }, 15000)

    return () => {
      if (pollTimer.current) { clearInterval(pollTimer.current); pollTimer.current = null }
      clearInitTimeout()
      if (messageHandler.current) {
        window.removeEventListener('message', messageHandler.current)
        messageHandler.current = null
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function initStripe(clientSecret: string, publishableKey: string) {
    if (!clientSecret || !publishableKey) {
      setError(t('payment.stripeMissingParams'))
      return
    }
    try {
      const { loadStripe } = await import('@stripe/stripe-js/pure')
      const stripe = await loadStripe(publishableKey)
      if (!stripe) { setError(t('payment.stripeLoadFailed')); return }

      const returnUrl = window.location.origin + '/payment/result?order_id=' + orderId + '&status=success'

      if (method === 'alipay') {
        // Alipay: redirect this popup to Alipay payment page
        const { error: err } = await stripe.confirmAlipayPayment(clientSecret, { return_url: returnUrl })
        if (err) setError(err.message || t('payment.result.failed'))
      } else if (method === 'wechat_pay') {
        // WeChat: Stripe shows its built-in QR dialog, user scans, promise resolves
        setHint(t('payment.stripePopup.loadingQr'))
        const result = await (stripe as unknown as StripeWithWechatPay).confirmWechatPayPayment(clientSecret, {
          payment_method_options: { wechat_pay: { client: isMobileDevice() ? 'mobile_web' : 'web' } },
        })
        if (result.error) {
          setError(result.error.message || t('payment.result.failed'))
        } else if (result.paymentIntent?.status === 'succeeded') {
          setSuccess(true)
          setTimeout(closeWindow, 2000)
        } else {
          // Payment not completed (user closed QR dialog)
          startPolling()
        }
      }
    } catch (err: unknown) {
      setError(extractI18nErrorMessage(err, t, 'payment.errors', t('payment.stripeLoadFailed')))
    }
  }

  function startPolling() {
    let inFlight = false
    pollTimer.current = setInterval(async () => {
      // 防重入：接口响应慢于轮询间隔时避免并发重叠请求。
      if (inFlight) return
      inFlight = true
      try {
        // access token 存储在 localStorage 的 'auth_token' 键下（见 api/client.ts），
        // 之前误读 'token' 导致轮询请求不带认证、永远 401，支付成功无法被检测到。
        const token = localStorage.getItem('auth_token') || ''
        const res = await fetch(buildApiUrl(`/payment/orders/${orderId}`), {
          headers: token ? { Authorization: 'Bearer ' + token } : {},
          credentials: 'include',
        })
        if (!res.ok) return
        const data = await res.json()
        const status = data?.data?.status
        if (status === 'COMPLETED' || status === 'PAID') {
          if (pollTimer.current) { clearInterval(pollTimer.current); pollTimer.current = null }
          setSuccess(true)
          setTimeout(closeWindow, 2000)
        }
      } catch { /* ignore */ } finally {
        inFlight = false
      }
    }, 3000)
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-canvas p-4 md:p-6 lg:p-8 dark:surface-inset">
      <div className="card w-full min-w-0 max-w-md space-y-4 p-6">
        {/* Amount + Order ID */}
        {amount && (
          <div className="text-center">
            <p className="text-3xl font-bold" style={{ color: methodColor }}>¥{amount}</p>
            {orderId && (
              <p className="mt-1 text-sm dark:text-slate-400 surface-muted">
                {t('payment.orders.orderId')}: {orderId}
              </p>
            )}
          </div>
        )}

        {/* Error */}
        {error ? (
          <div className="space-y-3">
            <div
              className="rounded-lg border border-red-200 bg-red-50/[0.15] p-3 text-sm text-red-600 dark:border-red-700 dark:bg-red-900/30 dark:text-red-400"
            >
              {error}
            </div>
            <button
              className="btn btn-secondary w-full"
              style={{ color: methodColor }}
              onClick={closeWindow}
            >
              {t('common.close')}
            </button>
          </div>
        ) : success ? (
          /* Success */
          <div className="space-y-3 py-4 text-center">
            <div className="text-5xl text-green-600 dark:text-green-400">✓</div>
            <p className="text-sm dark:text-slate-400 surface-muted">{t('payment.result.success')}</p>
            <button
              className="btn btn-secondary"
              style={{ color: methodColor }}
              onClick={closeWindow}
            >
              {t('common.close')}
            </button>
          </div>
        ) : (
          /* Loading / Redirecting */
          <div className="flex items-center justify-center py-8">
            <div
              className="h-8 w-8 animate-spin rounded-full border-2 border-t-transparent"
              style={{ borderColor: methodColor, borderTopColor: 'transparent' }}
            />
            <span className="ml-3 text-sm dark:text-slate-400 surface-muted">{hint}</span>
          </div>
        )}
      </div>
    </div>
  )
}
