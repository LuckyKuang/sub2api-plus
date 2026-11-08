import { useState } from 'react'
import { useSearchParams, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { PendingOAuthCreateAccountForm, type PendingOAuthCreateAccountPayload } from '@/components/auth/PendingOAuthCreateAccountForm'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { apiClient } from '@/api/client'
import { persistOAuthTokenContext, type PendingOAuthExchangeResponse } from '@/api/auth'
import { clearAllAffiliateReferralCodes } from '@/utils/oauthAffiliate'
import '@/views/authViews.css'
import { AuthLayout } from '@/components/layout/AuthLayout'

export default function DingTalkEmailCompletionView() {
  const { t } = useTranslation()
  const appStore = useAppStore()
  const authStore = useAuthStore()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()

  const [isSubmitting, setIsSubmitting] = useState(false)
  const [accountActionError, setAccountActionError] = useState('')

  const initialEmail = searchParams.get('email') || ''

  function sanitizeRedirectPath(path: string | null | undefined): string {
    if (!path) return '/dashboard'
    if (!path.startsWith('/')) return '/dashboard'
    if (path.startsWith('//')) return '/dashboard'
    if (path.includes('://')) return '/dashboard'
    if (path.includes('\n') || path.includes('\r')) return '/dashboard'
    return path
  }

  function getRequestErrorMessage(error: unknown, fallback: string): string {
    const err = error as { message?: string; response?: { data?: { detail?: string; message?: string } } }
    return err.response?.data?.detail || err.response?.data?.message || err.message || fallback
  }

  async function handleCreateAccount(payload: PendingOAuthCreateAccountPayload) {
    setAccountActionError('')
    if (!payload.email || !payload.password) return

    setIsSubmitting(true)
    try {
      const { data } = await apiClient.post<
        PendingOAuthExchangeResponse & {
          step?: string
          redirect?: string
          existing_account_bindable?: boolean
        }
      >('/auth/oauth/pending/create-account', {
        email: payload.email,
        password: payload.password,
        verify_code: payload.verifyCode || undefined,
        ...(payload.turnstileToken ? { turnstile_token: payload.turnstileToken } : {}),
        ...(payload.tencentCaptchaTicket
          ? {
              tencent_captcha_ticket: payload.tencentCaptchaTicket,
              tencent_captcha_randstr: payload.tencentCaptchaRandstr
            }
          : {}),
        invitation_code: payload.invitationCode || undefined
      })

      const redirect = sanitizeRedirectPath(data.redirect || searchParams.get('redirect'))

      if (data.access_token) {
        persistOAuthTokenContext(data)
        await authStore.setToken(data.access_token)
        clearAllAffiliateReferralCodes()
        appStore.showSuccess(t('auth.loginSuccess'))
        await navigate(redirect, { replace: true })
        return
      }

      // 后端把 pending session 转到 choice 状态（用户填的 email 已在系统内）→ 跳回 callback view 走绑定流程
      if (data.step === 'choose_account_action_required' || data.existing_account_bindable === true) {
        navigateToBindLogin(payload.email)
        return
      }

      setAccountActionError(t('auth.loginFailed'))
    } catch (error: unknown) {
      // 全局"开放注册"关闭且未开启钉钉企业模式豁免时，引导用户去绑定已有账户而非死路
      const err = error as { response?: { data?: { reason?: string } } }
      if (err.response?.data?.reason === 'REGISTRATION_DISABLED') {
        appStore.showInfo(t('auth.dingtalk.registrationDisabledRedirectToBind'))
        navigateToBindLogin(payload.email)
        return
      }
      setAccountActionError(getRequestErrorMessage(error, t('auth.loginFailed')))
    } finally {
      setIsSubmitting(false)
    }
  }

  function navigateToBindLogin(email: string) {
    const query: Record<string, string> = { bind: '1' }
    if (email) query.email = email
    const redirect = searchParams.get('redirect')
    if (redirect) query.redirect = redirect
    navigate({ pathname: '/auth/dingtalk/callback', search: new URLSearchParams(query).toString() }, { replace: true })
  }

  function handleSwitchToBind(email: string) {
    navigateToBindLogin(email)
  }

  return (
    <AuthLayout>
      <div className="space-y-6">
        <div className="text-center">
          <h2 className="text-2xl font-bold text-gray-900 dark:text-white">
            {t('auth.dingtalk.createAccountTitle')}
          </h2>
          <p className="mt-2 text-sm surface-muted">{t('auth.oauthFlow.createAccountHint')}</p>
        </div>

        <PendingOAuthCreateAccountForm
          testIdPrefix="dingtalk"
          initialEmail={initialEmail}
          isSubmitting={isSubmitting}
          errorMessage={accountActionError}
          onSubmit={(payload) => void handleCreateAccount(payload)}
          onSwitchToBind={handleSwitchToBind}
        />
      </div>
    </AuthLayout>
  )
}
