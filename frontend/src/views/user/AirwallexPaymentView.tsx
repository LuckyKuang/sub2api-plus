import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { CircleAlert } from 'lucide-react'
import type { Payment as AirwallexPayments, SDKOptions } from '@airwallex/components-sdk'
import {
  PAYMENT_RECOVERY_STORAGE_KEY,
  readPaymentRecoverySnapshot,
  type PaymentRecoverySnapshot,
} from './paymentFlow'

export default function AirwallexPaymentView() {
  const { t, i18n } = useTranslation()
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()

  const [loading, setLoading] = useState(true)
  const [errorMessage, setErrorMessage] = useState('')
  const disposed = useRef(false)

  useEffect(() => () => { disposed.current = true }, [])

  function queryString(key: string): string {
    return searchParams.get(key) || ''
  }

  function buildSuccessUrl(snapshot: PaymentRecoverySnapshot): string {
    const url = new URL('/payment/result', window.location.origin)
    const orderId = queryString('order_id')
    const outTradeNo = queryString('out_trade_no')
    const resumeToken = queryString('resume_token')

    if (orderId || snapshot.orderId > 0) url.searchParams.set('order_id', orderId || String(snapshot.orderId))
    if (outTradeNo || snapshot.outTradeNo) url.searchParams.set('out_trade_no', outTradeNo || snapshot.outTradeNo)
    if (resumeToken || snapshot.resumeToken) url.searchParams.set('resume_token', resumeToken || snapshot.resumeToken)
    return url.toString()
  }

  function restoreAirwallexSnapshot(): PaymentRecoverySnapshot | null {
    if (typeof window === 'undefined') {
      return null
    }

    const orderId = Number(queryString('order_id')) || 0
    const outTradeNo = queryString('out_trade_no')
    const resumeToken = queryString('resume_token')
    const snapshot = readPaymentRecoverySnapshot(
      window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY),
      resumeToken ? { resumeToken } : {},
    )

    if (!snapshot || snapshot.paymentType !== 'airwallex') {
      return null
    }
    if (orderId > 0 && snapshot.orderId !== orderId) {
      return null
    }
    if (outTradeNo && snapshot.outTradeNo !== outTradeNo) {
      return null
    }
    if (!snapshot.intentId || !snapshot.clientSecret) {
      return null
    }
    return snapshot
  }

  useEffect(() => {
    let cancelled = false
    async function init() {
      const snapshot = restoreAirwallexSnapshot()
      const checkoutLocale = (i18n.resolvedLanguage || i18n.language || '').toLowerCase().startsWith('zh') ? 'zh' : 'en'

      if (!snapshot) {
        setLoading(false)
        setErrorMessage(t('payment.airwallexMissingParams'))
        return
      }

      try {
        const airwallex = await import('@airwallex/components-sdk')
        if (disposed.current || cancelled) return
        const result = await airwallex.init({
          env: snapshot.paymentEnv === 'prod' ? 'prod' : 'demo',
          enabledElements: ['payments'],
          locale: checkoutLocale,
        } as SDKOptions)
        if (disposed.current || cancelled) return

        setLoading(false)
        const checkoutOptions = {
          intent_id: snapshot.intentId,
          client_secret: snapshot.clientSecret,
          currency: snapshot.currency || 'CNY',
          country_code: snapshot.countryCode || 'CN',
          successUrl: buildSuccessUrl(snapshot),
        } as AirwallexPayments.HostPaymentPage & { currency: string; country_code: string }
        if (!result.payments) {
          throw new Error(t('payment.airwallexLoadFailed'))
        }
        const redirectResult = result.payments.redirectToCheckout(checkoutOptions)

        if (typeof redirectResult === 'string' && redirectResult) {
          window.location.assign(redirectResult)
        }
      } catch (err: unknown) {
        setLoading(false)
        setErrorMessage(err instanceof Error && err.message
          ? err.message
          : t('payment.airwallexLoadFailed'))
      }
    }
    void init()
    return () => { cancelled = true }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="mx-auto max-w-lg space-y-6 py-8">
      {loading ? (
        <div className="flex items-center justify-center py-20">
          <div className="h-8 w-8 animate-spin rounded-full border-4 border-emerald-500 border-t-transparent"></div>
        </div>
      ) : errorMessage ? (
        <div className="card workspace-shell p-8 text-center">
          <div className="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-red-100/[0.15] dark:bg-red-900/30">
            <CircleAlert size={32} strokeWidth={1.5} className="text-red-500" />
          </div>
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white">{t('payment.airwallexLoadFailed')}</h3>
          <p className="mt-2 text-sm surface-muted">{errorMessage}</p>
          <button className="btn btn-primary mt-6" onClick={() => { void navigate('/purchase') }}>{t('payment.result.backToRecharge')}</button>
        </div>
      ) : (
        <div className="card workspace-shell p-6">
          <div className="flex flex-col items-center space-y-4 py-4">
            <div className="h-10 w-10 animate-spin rounded-full border-4 border-emerald-500 border-t-transparent"></div>
            <p className="text-sm surface-muted">{t('payment.qr.payInNewWindowHint')}</p>
          </div>
        </div>
      )}
    </div>
  )
}
