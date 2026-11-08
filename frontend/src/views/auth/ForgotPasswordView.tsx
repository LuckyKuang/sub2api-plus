import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/auth/Icon'
import { CaptchaChallenge, type CaptchaChallengeHandle } from '@/components/auth/CaptchaChallenge'
import { useAppStore } from '@/stores/app'
import { forgotPassword, getPublicSettings } from '@/api/auth'
import { cn } from '@/lib/utils'
import '@/views/authViews.css'
import { AuthLayout } from '@/components/layout/AuthLayout'

export default function ForgotPasswordView() {
  const { t } = useTranslation()
  const appStore = useAppStore()

  const [isLoading, setIsLoading] = useState(false)
  const [isSubmitted, setIsSubmitted] = useState(false)

  const [email, setEmail] = useState('')

  // Public settings
  const [turnstileEnabled, setTurnstileEnabled] = useState(false)
  const [turnstileSiteKey, setTurnstileSiteKey] = useState('')
  const [tencentCaptchaEnabled, setTencentCaptchaEnabled] = useState(false)
  const [tencentCaptchaAppId, setTencentCaptchaAppId] = useState('')
  const [tencentCaptchaRegion, setTencentCaptchaRegion] = useState('cn')
  const [aliyunCaptchaEnabled, setAliyunCaptchaEnabled] = useState(false)
  const [aliyunCaptchaSceneId, setAliyunCaptchaSceneId] = useState('')
  const [aliyunCaptchaPrefix, setAliyunCaptchaPrefix] = useState('')
  const [aliyunCaptchaRegion, setAliyunCaptchaRegion] = useState('cn')

  const [turnstileToken, setTurnstileToken] = useState('')
  const [tencentCaptchaRandstr, setTencentCaptchaRandstr] = useState('')
  const captchaRef = useRef<CaptchaChallengeHandle | null>(null)

  const [errors, setErrors] = useState({ email: '', turnstile: '' })

  const aliyunCaptchaReady =
    aliyunCaptchaEnabled && Boolean(aliyunCaptchaSceneId) && Boolean(aliyunCaptchaPrefix)
  // 动作触发式验证码（腾讯/阿里云）：提交时弹窗验证
  const actionCaptchaEnabled =
    (tencentCaptchaEnabled && Boolean(tencentCaptchaAppId)) || aliyunCaptchaReady
  const captchaEnabled = (turnstileEnabled && Boolean(turnstileSiteKey)) || actionCaptchaEnabled

  const validationToastMessage = errors.email || errors.turnstile || ''

  // Validation messages surface as toasts, matching the Vue watcher.
  useEffect(() => {
    if (validationToastMessage) {
      appStore.showError(validationToastMessage)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [validationToastMessage])

  // ==================== Lifecycle ====================

  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const settings = await getPublicSettings()
        if (cancelled) return
        setTurnstileEnabled(settings.turnstile_enabled)
        setTurnstileSiteKey(settings.turnstile_site_key || '')
        setTencentCaptchaEnabled(settings.tencent_captcha_enabled === true)
        setTencentCaptchaAppId(settings.tencent_captcha_app_id || '')
        setTencentCaptchaRegion(settings.tencent_captcha_region || 'cn')
        setAliyunCaptchaEnabled(settings.aliyun_captcha_enabled === true)
        setAliyunCaptchaSceneId(settings.aliyun_captcha_scene_id || '')
        setAliyunCaptchaPrefix(settings.aliyun_captcha_prefix || '')
        setAliyunCaptchaRegion(settings.aliyun_captcha_region || 'cn')
      } catch (error) {
        console.error('Failed to load public settings:', error)
      }
    })()

    return () => {
      cancelled = true
    }
  }, [])

  // ==================== Captcha Handlers ====================

  function onTurnstileVerify(token: string, randstr = ''): void {
    setTurnstileToken(token)
    setTencentCaptchaRandstr(randstr)
    setErrors((previous) => ({ ...previous, turnstile: '' }))
  }

  function onTurnstileExpire(): void {
    setTurnstileToken('')
    setTencentCaptchaRandstr('')
    setErrors((previous) => ({ ...previous, turnstile: t('auth.turnstileExpired') }))
  }

  function onTurnstileError(): void {
    setTurnstileToken('')
    setTencentCaptchaRandstr('')
    setErrors((previous) => ({ ...previous, turnstile: t('auth.turnstileFailed') }))
  }

  function resetCaptchaProof(): void {
    captchaRef.current?.reset()
    setTurnstileToken('')
    setTencentCaptchaRandstr('')
    setErrors((previous) => ({ ...previous, turnstile: '' }))
  }

  async function acquireActionProof(): Promise<boolean> {
    if (!actionCaptchaEnabled) return true

    const proof = await captchaRef.current?.verifyAction()
    if (!proof) return false

    setTurnstileToken(proof.token)
    setTencentCaptchaRandstr(proof.randstr)
    return true
  }

  // ==================== Validation ====================

  function validateForm(): boolean {
    setErrors({ email: '', turnstile: '' })

    let isValid = true

    // Email validation
    if (!email.trim()) {
      setErrors((previous) => ({ ...previous, email: t('auth.emailRequired') }))
      isValid = false
    } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
      setErrors((previous) => ({ ...previous, email: t('auth.invalidEmail') }))
      isValid = false
    }

    // Turnstile validation
    if (turnstileEnabled && !turnstileToken) {
      setErrors((previous) => ({ ...previous, turnstile: t('auth.completeVerification') }))
      isValid = false
    }

    return isValid
  }

  // ==================== Form Handlers ====================

  async function handleSubmit(event: React.FormEvent): Promise<void> {
    event.preventDefault()

    if (!validateForm()) {
      return
    }

    if (!(await acquireActionProof())) {
      return
    }

    setIsLoading(true)

    try {
      await forgotPassword({
        email,
        turnstile_token: turnstileEnabled || aliyunCaptchaEnabled ? turnstileToken : undefined,
        tencent_captcha_ticket: tencentCaptchaEnabled ? turnstileToken : undefined,
        tencent_captcha_randstr: tencentCaptchaEnabled ? tencentCaptchaRandstr : undefined
      })

      setIsSubmitted(true)
      appStore.showSuccess(t('auth.resetEmailSent'))
    } catch (error: unknown) {
      const err = error as { message?: string; response?: { data?: { detail?: string } } }

      if (err.response?.data?.detail) {
        appStore.showError(err.response.data.detail)
      } else if (err.message) {
        appStore.showError(err.message)
      } else {
        appStore.showError(t('auth.sendResetLinkFailed'))
      }
    } finally {
      if (captchaEnabled) {
        resetCaptchaProof()
      }
      setIsLoading(false)
    }
  }

  return (
    <AuthLayout
      footer={
        <>
        <p className="surface-muted">
          {t('auth.rememberedPassword')}
          <Link
            to="/login"
            className="font-medium text-primary-600 transition-colors hover:text-primary-500 dark:text-primary-400 dark:hover:text-primary-300"
          >
            {t('auth.signIn')}
          </Link>
        </p>
        </>
      }
    >
      <div className="space-y-6">
        {/* Title */}
        <div className="text-center">
          <h2 className="text-2xl font-bold text-gray-900 dark:text-white">{t('auth.forgotPasswordTitle')}</h2>
          <p className="mt-2 text-sm surface-muted">{t('auth.forgotPasswordHint')}</p>
        </div>

        {/* Success State */}
        {isSubmitted ? (
          <div className="space-y-6">
            <div className="rounded-xl border border-green-200 bg-green-50/[0.15] p-6 dark:border-green-800/50 dark:bg-green-900/20">
              <div className="flex flex-col items-center gap-4 text-center">
                <div className="flex h-12 w-12 items-center justify-center rounded-full bg-green-100/[0.15] dark:bg-green-800/50">
                  <Icon name="checkCircle" size="lg" className="text-green-600 dark:text-green-400" />
                </div>
                <div>
                  <h3 className="text-lg font-semibold text-green-800 dark:text-green-200">
                    {t('auth.resetEmailSent')}
                  </h3>
                  <p className="mt-2 text-sm text-green-700 dark:text-green-300">{t('auth.resetEmailSentHint')}</p>
                </div>
              </div>
            </div>

            <div className="text-center">
              <Link
                to="/login"
                className="inline-flex items-center gap-2 font-medium text-primary-600 transition-colors hover:text-primary-500 dark:text-primary-400 dark:hover:text-primary-300"
              >
                <Icon name="arrowLeft" size="sm" />
                {t('auth.backToLogin')}
              </Link>
            </div>
          </div>
        ) : (
          /* Form State */
          <form onSubmit={handleSubmit} className="space-y-5">
            {/* Email Input */}
            <div>
              <label htmlFor="email" className="input-label">
                {t('auth.emailLabel')}
              </label>
              <div className="relative">
                <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
                  <Icon name="mail" size="md" className="surface-muted" />
                </div>
                <input
                  id="email"
                  value={email}
                  onChange={(event) => setEmail(event.target.value)}
                  type="email"
                  required
                  autoFocus
                  autoComplete="email"
                  disabled={isLoading}
                  className={cn('input pl-11', errors.email && 'input-error')}
                  placeholder={t('auth.emailPlaceholder')}
                />
              </div>
            </div>

            {/* Turnstile Widget */}
            {captchaEnabled && (
              <div>
                <CaptchaChallenge
                  ref={captchaRef}
                  turnstileEnabled={turnstileEnabled}
                  turnstileSiteKey={turnstileSiteKey}
                  tencentEnabled={tencentCaptchaEnabled}
                  tencentAppId={tencentCaptchaAppId}
                  tencentRegion={tencentCaptchaRegion}
                  aliyunEnabled={aliyunCaptchaEnabled}
                  aliyunSceneId={aliyunCaptchaSceneId}
                  aliyunPrefix={aliyunCaptchaPrefix}
                  aliyunRegion={aliyunCaptchaRegion}
                  onVerify={onTurnstileVerify}
                  onExpire={onTurnstileExpire}
                  onError={onTurnstileError}
                />
              </div>
            )}

            {/* Submit Button */}
            <button
              type="submit"
              disabled={isLoading || (turnstileEnabled && !turnstileToken)}
              className="btn btn-primary w-full"
            >
              {isLoading ? (
                <svg className="-ml-1 mr-2 h-4 w-4 animate-spin text-white" fill="none" viewBox="0 0 24 24">
                  <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                  <path
                    className="opacity-75"
                    fill="currentColor"
                    d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                  />
                </svg>
              ) : (
                <Icon name="mail" size="md" className="mr-2" />
              )}
              {isLoading ? t('auth.sendingResetLink') : t('auth.sendResetLink')}
            </button>
          </form>
        )}
      </div>

      {/* Footer */}
    </AuthLayout>
  )
}
