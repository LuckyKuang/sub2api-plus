import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Check, CircleAlert } from 'lucide-react'
import type { Stripe, StripeElements } from '@stripe/stripe-js'
import { paymentAPI } from '@/api/payment'
import { usePaymentStore } from '@/stores/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { isMobileDevice } from '@/utils/device'
import { formatPaymentAmount, normalizePaymentCurrency } from './paymentCurrency'
import {
  PAYMENT_RECOVERY_STORAGE_KEY,
  readPaymentRecoverySnapshot,
} from './paymentFlow'
import type { PaymentOrder } from '@/types/payment'

interface WechatPayPaymentIntent {
  status: string
  next_action?: { wechat_pay_display_qr_code?: { image_data_url?: string } }
}

interface StripeWithWechatPay {
  confirmWechatPayPayment(
    clientSecret: string,
    options: Record<string, unknown>,
  ): Promise<{ paymentIntent?: WechatPayPaymentIntent; error?: { message?: string } }>
}

export default function StripePaymentView() {
  const { t, i18n } = useTranslation()
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const paymentStore = usePaymentStore()

  // 弹窗模式：指定支付宝或微信方式时跳过 AppLayout
  const isPopup = !!searchParams.get('method')

  const [loading, setLoading] = useState(true)
  const [initError, setInitError] = useState('')
  const [stripeError, setStripeError] = useState('')
  const [stripeSubmitting, setStripeSubmitting] = useState(false)
  const [stripeSuccess, setStripeSuccess] = useState(false)
  const [stripeReady, setStripeReady] = useState(false)
  const [order, setOrder] = useState<PaymentOrder | null>(null)
  const [currency, setCurrency] = useState('CNY')
  const [wechatQrUrl, setWechatQrUrl] = useState('')
  const [redirecting, setRedirecting] = useState(false)
  const [showPaymentElement, setShowPaymentElement] = useState(false)

  const disposed = useRef(false)
  const stripeInstance = useRef<Stripe | null>(null)
  const elementsInstance = useRef<StripeElements | null>(null)
  const redirectTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const pollTimer = useRef<ReturnType<typeof setInterval> | null>(null)
  const pendingPaymentElement = useRef<{ stripe: Stripe; clientSecret: string } | null>(null)

  const localeCode = i18n.resolvedLanguage || i18n.language

  function formatGatewayAmount(value: number): string {
    return formatPaymentAmount(value, currency, localeCode)
  }

  function mountPaymentElement(stripe: Stripe, clientSecret: string) {
    if (disposed.current) return
    const isDark = document.documentElement.classList.contains('dark')
    const elements = stripe.elements({
      clientSecret,
      appearance: { theme: isDark ? 'night' : 'stripe', variables: { borderRadius: '8px' } },
    })
    elementsInstance.current = elements
    const paymentElement = elements.create('payment', {
      layout: 'tabs',
      paymentMethodOrder: ['alipay', 'wechat_pay', 'card', 'link'],
    } as Record<string, unknown>)
    paymentElement.mount('#stripe-payment-element')
    paymentElement.on('ready', () => { setStripeReady(true) })
  }

  async function confirmAlipay(stripe: Stripe, clientSecret: string, orderId: number) {
    setRedirecting(true)
    const returnUrl = window.location.origin + '/payment/result?order_id=' + orderId + '&status=success'
    const { error } = await stripe.confirmAlipayPayment(clientSecret, { return_url: returnUrl })
    if (error) {
      setRedirecting(false)
      setStripeError(error.message || t('payment.result.failed'))
    }
    // 无错误时 Stripe 会自动跳转
  }

  async function confirmWechatPay(stripe: Stripe, clientSecret: string) {
    const { paymentIntent, error } = await (stripe as Stripe & StripeWithWechatPay).confirmWechatPayPayment(clientSecret, {
      payment_method_options: { wechat_pay: { client: isMobileDevice() ? 'mobile_web' : 'web' } },
    })

    if (error) {
      setStripeError(error.message || t('payment.result.failed'))
      return
    }

    // 从 next_action 中提取二维码
    const qrData = paymentIntent?.next_action?.wechat_pay_display_qr_code?.image_data_url
    if (qrData) {
      setWechatQrUrl(qrData)
      // 轮询支付完成状态
      startPolling()
    } else if (paymentIntent?.status === 'succeeded') {
      setStripeSuccess(true)
      scheduleClose()
    } else {
      setStripeError(t('payment.result.failed'))
    }
  }

  function startPolling() {
    const orderId = Number(searchParams.get('order_id'))
    if (!orderId || disposed.current) return
    pollTimer.current = setInterval(async () => {
      const o = await paymentStore.pollOrderStatus(orderId)
      if (!o) return
      if (o.status === 'COMPLETED' || o.status === 'PAID') {
        if (pollTimer.current) { clearInterval(pollTimer.current); pollTimer.current = null }
        setStripeSuccess(true)
        setWechatQrUrl('')
        scheduleClose()
      }
    }, 3000)
  }

  function scheduleClose() {
    if (disposed.current) return
    if (window.opener) {
      redirectTimer.current = setTimeout(() => { window.close() }, 2000)
    } else {
      redirectTimer.current = setTimeout(() => {
        void navigate({ pathname: '/payment/result', search: `order_id=${searchParams.get('order_id') || ''}&status=success` })
      }, 2000)
    }
  }

  async function handleGenericPay() {
    if (!stripeInstance.current || !elementsInstance.current || stripeSubmitting) return
    setStripeSubmitting(true)
    setStripeError('')
    try {
      const { error } = await stripeInstance.current.confirmPayment({
        elements: elementsInstance.current,
        confirmParams: {
          return_url: window.location.origin + '/payment/result?order_id=' + searchParams.get('order_id') + '&status=success',
        },
        redirect: 'if_required',
      })
      if (error) {
        setStripeError(error.message || t('payment.result.failed'))
      } else {
        setStripeSuccess(true)
        scheduleClose()
      }
    } catch (err: unknown) {
      setStripeError(extractI18nErrorMessage(err, t, 'payment.errors', t('payment.result.failed')))
    } finally {
      setStripeSubmitting(false)
    }
  }

  useEffect(() => {
    let cancelled = false
    async function init() {
      const orderId = Number(searchParams.get('order_id'))
      const method = String(searchParams.get('method') || '')
      const resumeToken = searchParams.get('resume_token') ?? undefined
      // client_secret 只从本浏览器下单时写入的恢复快照读取，且快照订单必须与 URL 订单一致：
      // 不信任 URL 中的 client_secret，避免页面展示的订单与实际收款的 PaymentIntent 被拼接错绑。
      const restored = typeof window !== 'undefined'
        ? readPaymentRecoverySnapshot(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY), { resumeToken })
        : null
      const clientSecret = orderId && restored?.orderId === orderId ? restored.clientSecret : ''

      if (!orderId || !clientSecret) {
        setLoading(false)
        setInitError(t('payment.stripeMissingParams'))
        return
      }

      try {
        setCurrency(normalizePaymentCurrency(restored?.currency))
        const res = await paymentAPI.getOrder(orderId)
        if (cancelled) return
        setOrder(res.data)
        if (res.data.currency) {
          setCurrency(normalizePaymentCurrency(res.data.currency))
        }

        await paymentStore.fetchConfig()
        const publishableKey = paymentStore.config?.stripe_publishable_key
        if (!publishableKey) { setInitError(t('payment.stripeNotConfigured')); return }

        const { loadStripe } = await import('@stripe/stripe-js/pure')
        const stripe = await loadStripe(publishableKey)
        if (disposed.current || cancelled) return
        if (!stripe) { setInitError(t('payment.stripeLoadFailed')); return }

        stripeInstance.current = stripe
        setLoading(false)

        // 指定方式直接确认，无需渲染完整 Payment Element
        if (method === 'alipay') {
          await confirmAlipay(stripe, clientSecret, orderId)
        } else if (method === 'wechat_pay') {
          await confirmWechatPay(stripe, clientSecret)
        } else {
          // 未指定方式时渲染完整 Payment Element
          setShowPaymentElement(true)
          pendingPaymentElement.current = { stripe, clientSecret }
        }
      } catch (err: unknown) {
        setInitError(extractI18nErrorMessage(err, t, 'payment.errors', t('payment.stripeLoadFailed')))
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void init()
    return () => { cancelled = true }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => () => {
    disposed.current = true
    if (redirectTimer.current) clearTimeout(redirectTimer.current)
    if (pollTimer.current) clearInterval(pollTimer.current)
  }, [])

  // The element container only exists after the render that reveals it.
  useEffect(() => {
    const pending = pendingPaymentElement.current
    if (!showPaymentElement || !pending) return
    pendingPaymentElement.current = null
    mountPaymentElement(pending.stripe, pending.clientSecret)
  }, [showPaymentElement])

  const body = loading ? (
    <div className="flex items-center justify-center py-20">
      <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary-500 border-t-transparent"></div>
    </div>
  ) : initError ? (
    <div className="card p-8 text-center">
      <div className="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-red-100/[0.15] dark:bg-red-900/30">
        <CircleAlert size={32} strokeWidth={1.5} className="text-red-500" />
      </div>
      <h3 className="text-lg font-semibold text-gray-900 dark:text-white">{t('payment.stripeLoadFailed')}</h3>
      <p className="mt-2 text-sm surface-muted">{initError}</p>
      <button className="btn btn-primary mt-6" onClick={() => { void navigate('/purchase') }}>{t('payment.result.backToRecharge')}</button>
    </div>
  ) : (
    <>
      {/* 金额头部 */}
      {order && (
        <div className="card overflow-hidden">
          <div className="bg-gradient-to-br from-[#635bff] to-[#4f46e5] px-6 py-6 text-center">
            <p className="text-sm font-medium text-indigo-200">{t('payment.actualPay')}</p>
            <p className="mt-1 text-3xl font-bold text-white">{formatGatewayAmount(order.pay_amount)}</p>
          </div>
        </div>
      )}

      {/* 微信二维码展示 */}
      {wechatQrUrl ? (
        <>
          <div className="card p-6">
            <div className="flex flex-col items-center space-y-4">
              <p className="text-lg font-semibold text-gray-900 dark:text-white">{t('payment.qr.scanWxpay')}</p>
              <div className="relative max-w-full rounded-lg border-2 border-[#2BB741] bg-green-50/[0.15] p-4 dark:border-[#2BB741]/70 dark:bg-green-950/20">
                <img src={wechatQrUrl} alt="WeChat Pay QR" className="aspect-square h-auto w-56 max-w-full rounded" />
                <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
                  <span className="rounded-full bg-[#2BB741] p-2 shadow ring-2 ring-white">
                    <svg className="h-5 w-5 text-white" viewBox="0 0 24 24" fill="currentColor"><path d="M8.691 2.188C3.891 2.188 0 5.476 0 9.53c0 2.212 1.17 4.203 3.002 5.55a.59.59 0 0 1 .213.665l-.39 1.48c-.019.07-.048.141-.048.213 0 .163.13.295.29.295a.326.326 0 0 0 .167-.054l1.903-1.114a.864.864 0 0 1 .717-.098 10.16 10.16 0 0 0 2.837.403c.276 0 .543-.027.811-.05-.857-2.578.157-4.972 1.932-6.446 1.703-1.415 3.882-1.98 5.853-1.838-.576-3.583-4.196-6.348-8.596-6.348zM5.785 5.991c.642 0 1.162.529 1.162 1.18a1.17 1.17 0 0 1-1.162 1.178A1.17 1.17 0 0 1 4.623 7.17c0-.651.52-1.18 1.162-1.18zm5.813 0c.642 0 1.162.529 1.162 1.18a1.17 1.17 0 0 1-1.162 1.178 1.17 1.17 0 0 1-1.162-1.178c0-.651.52-1.18 1.162-1.18zm3.636 4.35c-2.084 0-3.993.672-5.363 1.844-1.188.982-2.004 2.308-2.004 3.862 0 1.207.546 2.355 1.483 3.285.114.113.238.213.358.321l-.105.42c-.021.084-.042.17-.042.253 0 .168.126.258.282.258.065 0 .126-.025.18-.058l1.27-.765a.69.69 0 0 1 .58-.086c.96.282 1.99.437 3.043.437 2.633 0 5.03-.972 6.4-2.5.782-.87 1.258-1.901 1.258-3.006 0-3.328-3.325-6.006-7.34-6.006zm-3.21 3.09c.52 0 .94.429.94.957a.949.949 0 0 1-.94.955.949.949 0 0 1-.94-.955c0-.528.42-.957.94-.957zm4.739 0c.52 0 .94.429.94.957a.949.949 0 0 1-.94.955.949.949 0 0 1-.94-.955c0-.528.42-.957.94-.957z"/></svg>
                  </span>
                </div>
              </div>
              <p className="text-center text-sm surface-muted">{t('payment.qr.scanWxpayHint')}</p>
            </div>
          </div>
          <div className="card p-4 text-center">
            <p className="text-sm surface-muted">{t('payment.qr.waitingPayment')}</p>
          </div>
        </>
      ) : redirecting ? (
        /* 支付宝跳转状态 */
        <div className="card p-6">
          <div className="flex flex-col items-center space-y-4 py-4">
            <div className="h-10 w-10 animate-spin rounded-full border-4 border-[#00AEEF] border-t-transparent"></div>
            <p className="text-sm surface-muted">{t('payment.qr.payInNewWindowHint')}</p>
          </div>
        </div>
      ) : stripeSuccess ? (
        /* 成功状态 */
        <div className="card p-6 text-center">
          <div className="flex flex-col items-center gap-3 py-4">
            <div className="flex h-16 w-16 items-center justify-center rounded-full bg-green-100/[0.15] dark:bg-green-900/30">
              <Check size={24} strokeWidth={1.5} className="text-green-500" />
            </div>
            <p className="text-lg font-bold text-gray-900 dark:text-white">{t('payment.result.success')}</p>
            <p className="text-sm surface-muted">{t('payment.stripeSuccessProcessing')}</p>
          </div>
        </div>
      ) : showPaymentElement ? (
        /* 无指定方式或未知方式时展示完整 Payment Element */
        <>
          <div className="card p-6">
            <div id="stripe-payment-element" className="min-h-[200px]"></div>
            {stripeError && <p className="mt-4 text-sm text-red-600 dark:text-red-400">{stripeError}</p>}
            <button className="btn btn-stripe mt-6 w-full py-3 text-base" disabled={stripeSubmitting || !stripeReady} onClick={() => { void handleGenericPay() }}>
              {stripeSubmitting ? (
                <span className="flex items-center justify-center gap-2">
                  <span className="h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent"></span>
                  {t('common.processing')}
                </span>
              ) : (
                <span>{t('payment.stripePay')}</span>
              )}
            </button>
          </div>
          <div className="text-center">
            <button className="btn btn-secondary" onClick={() => { void navigate('/purchase') }}>{t('payment.result.backToRecharge')}</button>
          </div>
        </>
      ) : null}
      {/* 错误状态 */}
      {stripeError && !showPaymentElement && (
        <div className="card p-4">
          <p className="text-sm text-red-600 dark:text-red-400">{stripeError}</p>
          <button className="btn btn-secondary mt-3 w-full" onClick={() => { void navigate('/purchase') }}>{t('payment.result.backToRecharge')}</button>
        </div>
      )}
    </>
  )

  if (isPopup) {
    return <div className="min-h-screen bg-gray-50 dark:bg-dark-900">{body}</div>
  }

  return (
    <div className="mx-auto max-w-lg space-y-6 py-8">
      {body}
    </div>
  )
}
