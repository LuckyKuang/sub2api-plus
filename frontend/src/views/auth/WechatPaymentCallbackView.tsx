import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useProductAppearance } from '@/components/auth/useProductAppearance'
import { useAppStore } from '@/stores/app'
import '@/views/authViews.css'

function readQueryString(params: URLSearchParams, key: string): string {
  const value = params.get(key)
  if (value === null) return ''
  return Array.isArray(value) ? (typeof value[0] === 'string' ? value[0] : '') : value
}

function parseFragmentParams(): URLSearchParams {
  const raw = typeof window !== 'undefined' ? window.location.hash : ''
  const hash = raw.startsWith('#') ? raw.slice(1) : raw
  return new URLSearchParams(hash)
}

function normalizeRedirectPath(path: string | null | undefined): string {
  const value = (path || '').trim()
  if (!value) return '/purchase'
  if (!value.startsWith('/')) return '/purchase'
  if (value.startsWith('//') || value.includes('://')) return '/purchase'
  if (value === '/payment') return '/purchase'
  if (value.startsWith('/payment?')) return '/purchase' + value.slice('/payment'.length)
  return value
}

function appendQueryParam(query: Record<string, string>, key: string, value: string) {
  if (value) {
    query[key] = value
  }
}

export default function WechatPaymentCallbackView() {
  const { t } = useTranslation()
  const appStore = useAppStore()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()

  useProductAppearance()

  const [errorMessage, setErrorMessage] = useState('')

  useEffect(() => {
    if (errorMessage) {
      appStore.showError(errorMessage)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [errorMessage])

  const callbackProcessingText = t('auth.wechatPayment.callbackProcessing')
  const callbackTitleText = t('auth.wechatPayment.callbackTitle')
  const backToPaymentText = t('auth.wechatPayment.backToPayment')

  function goBackToPayment() {
    void navigate('/purchase', { replace: true })
  }

  useEffect(() => {
    let cancelled = false

    void (async () => {
      const fragment = parseFragmentParams()
      const readParam = (key: string) => fragment.get(key) || readQueryString(searchParams, key)

      const error = readParam('error') || readParam('err_msg') || readParam('errmsg')
      const errorDescription = readParam('error_description') || readParam('message')

      if (error) {
        if (!cancelled) setErrorMessage(errorDescription || error)
        return
      }

      const resumeToken = readParam('wechat_resume_token')
      const openid = readParam('openid')
      const state = readParam('state')
      const scope = readParam('scope')
      const paymentType = readParam('payment_type')
      const amount = readParam('amount')
      const orderType = readParam('order_type')
      const planId = readParam('plan_id')
      const redirectURL = new URL(normalizeRedirectPath(readParam('redirect')), window.location.origin)

      if (!resumeToken && !openid) {
        if (!cancelled) setErrorMessage(t('auth.wechatPayment.callbackMissingResumeToken'))
        return
      }

      const query: Record<string, string> = {
        ...Object.fromEntries(redirectURL.searchParams.entries()),
        wechat_resume: '1'
      }

      if (resumeToken) {
        query.wechat_resume_token = resumeToken
      } else {
        query.openid = openid
        appendQueryParam(query, 'state', state)
        appendQueryParam(query, 'scope', scope)
        appendQueryParam(query, 'payment_type', paymentType)
        appendQueryParam(query, 'amount', amount)
        appendQueryParam(query, 'order_type', orderType)
        appendQueryParam(query, 'plan_id', planId)
      }

      await navigate({ pathname: redirectURL.pathname, search: new URLSearchParams(query).toString() }, { replace: true })
    })()

    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <main className="min-h-screen bg-gray-50 px-4 py-8 dark:bg-dark-950 sm:py-12">
      <section
        className="card mx-auto max-w-2xl p-5 sm:p-8"
        aria-labelledby="wechat-payment-callback-title"
        aria-busy={!errorMessage}
      >
        <h1 id="wechat-payment-callback-title" className="text-lg font-semibold text-gray-900 dark:text-white">
          {callbackTitleText}
        </h1>
        <p className="mt-2 text-sm surface-muted">{errorMessage || callbackProcessingText}</p>

        {!errorMessage ? (
          <div className="mt-6 flex items-center justify-center py-10">
            <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary-500 border-t-transparent" />
          </div>
        ) : (
          <div className="mt-6 rounded-lg border border-gray-200 surface-inset p-4 dark:border-dark-700">
            <p className="text-sm surface-muted">{errorMessage}</p>
            <button className="btn btn-primary mt-4" type="button" onClick={goBackToPayment}>
              {backToPaymentText}
            </button>
          </div>
        )}
      </section>
    </main>
  )
}
