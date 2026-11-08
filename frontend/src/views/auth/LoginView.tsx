import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/auth/Icon'
import { CaptchaChallenge, type CaptchaChallengeHandle } from '@/components/auth/CaptchaChallenge'
import { LoginAgreementPrompt } from '@/components/auth/LoginAgreementPrompt'
import { TotpLoginModal } from '@/components/auth/TotpLoginModal'
import { EmailOAuthButtons } from '@/components/auth/EmailOAuthButtons'
import { LinuxDoOAuthSection, DingTalkOAuthSection } from '@/components/auth/ProviderOAuthSections'
import { WechatOAuthSection } from '@/components/auth/WechatOAuthSection'
import { OidcOAuthSection } from '@/components/auth/OidcOAuthSection'
import { useLoginAgreement } from '@/components/auth/useLoginAgreement'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import {
  buildOAuthLoginStartURL,
  getPublicSettings,
  isTotp2FARequired,
  isWeChatWebOAuthEnabled,
  startOAuthLogin,
  type OAuthLoginStart
} from '@/api/auth'
import type { ActionCaptchaRequestProof, LoginRequest, TotpLoginResponse } from '@/types'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { clearAllAffiliateReferralCodes } from '@/utils/oauthAffiliate'
import { cn } from '@/lib/utils'
import '@/views/authViews.css'

export default function LoginView() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const authStore = useAuthStore()
  const appStore = useAppStore()
  const agreement = useLoginAgreement()

  const [isLoading, setIsLoading] = useState(false)
  const [passkeyLoading, setPasskeyLoading] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [publicSettingsLoaded, setPublicSettingsLoaded] = useState(false)

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')

  // Public settings
  const [registrationEnabled, setRegistrationEnabled] = useState(false)
  const [turnstileEnabled, setTurnstileEnabled] = useState(false)
  const [turnstileSiteKey, setTurnstileSiteKey] = useState('')
  const [tencentCaptchaEnabled, setTencentCaptchaEnabled] = useState(false)
  const [tencentCaptchaAppId, setTencentCaptchaAppId] = useState('')
  const [tencentCaptchaRegion, setTencentCaptchaRegion] = useState('cn')
  const [aliyunCaptchaEnabled, setAliyunCaptchaEnabled] = useState(false)
  const [aliyunCaptchaSceneId, setAliyunCaptchaSceneId] = useState('')
  const [aliyunCaptchaPrefix, setAliyunCaptchaPrefix] = useState('')
  const [aliyunCaptchaRegion, setAliyunCaptchaRegion] = useState('cn')
  const [linuxdoOAuthEnabled, setLinuxdoOAuthEnabled] = useState(false)
  const [dingtalkOAuthEnabled, setDingtalkOAuthEnabled] = useState(false)
  const [wechatOAuthEnabled, setWechatOAuthEnabled] = useState(false)
  const [backendModeEnabled, setBackendModeEnabled] = useState(false)
  const [oidcOAuthEnabled, setOidcOAuthEnabled] = useState(false)
  const [oidcOAuthProviderName, setOidcOAuthProviderName] = useState('OIDC')
  const [githubOAuthEnabled, setGithubOAuthEnabled] = useState(false)
  const [googleOAuthEnabled, setGoogleOAuthEnabled] = useState(false)
  const [passwordResetEnabled, setPasswordResetEnabled] = useState(false)
  const [passkeyEnabled, setPasskeyEnabled] = useState(false)

  const [turnstileToken, setTurnstileToken] = useState('')
  const [tencentCaptchaRandstr, setTencentCaptchaRandstr] = useState('')
  const captchaRef = useRef<CaptchaChallengeHandle | null>(null)

  const [errors, setErrors] = useState({ email: '', password: '', turnstile: '' })

  // 2FA state
  const [show2FAModal, setShow2FAModal] = useState(false)
  const [totpTempToken, setTotpTempToken] = useState('')
  const [totpUserEmailMasked, setTotpUserEmailMasked] = useState('')
  const [totpVerifying, setTotpVerifying] = useState(false)
  const [totpErrorMessage, setTotpErrorMessage] = useState('')

  const aliyunCaptchaReady =
    aliyunCaptchaEnabled && Boolean(aliyunCaptchaSceneId) && Boolean(aliyunCaptchaPrefix)
  // 动作触发式验证码（腾讯/阿里云）：提交、OAuth 启动、passkey 时弹窗验证
  const actionCaptchaEnabled =
    (tencentCaptchaEnabled && Boolean(tencentCaptchaAppId)) || aliyunCaptchaReady
  const captchaEnabled = (turnstileEnabled && Boolean(turnstileSiteKey)) || actionCaptchaEnabled

  const validationToastMessage = errors.email || errors.password || errors.turnstile || ''

  const agreementGateActive = agreement.enabled && !agreement.accepted

  const authActionDisabled = isLoading || passkeyLoading || !publicSettingsLoaded || agreementGateActive

  const showPasskeyLogin = passkeyEnabled && typeof window.PublicKeyCredential !== 'undefined'

  const showOAuthLogin = useMemo(
    () =>
      !backendModeEnabled &&
      (linuxdoOAuthEnabled ||
        dingtalkOAuthEnabled ||
        wechatOAuthEnabled ||
        oidcOAuthEnabled ||
        githubOAuthEnabled ||
        googleOAuthEnabled),
    [
      backendModeEnabled,
      linuxdoOAuthEnabled,
      dingtalkOAuthEnabled,
      wechatOAuthEnabled,
      oidcOAuthEnabled,
      githubOAuthEnabled,
      googleOAuthEnabled
    ]
  )

  // Validation messages surface as toasts, matching the Vue watcher.
  useEffect(() => {
    if (validationToastMessage) {
      appStore.showError(validationToastMessage)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [validationToastMessage])

  useEffect(() => {
    const expiredFlag = sessionStorage.getItem('auth_expired')
    if (expiredFlag) {
      sessionStorage.removeItem('auth_expired')
      const message = t('auth.reloginRequired')
      appStore.showWarning(message)
    }

    let cancelled = false
    void (async () => {
      try {
        const settings = await getPublicSettings()
        if (cancelled) return
        setRegistrationEnabled(settings.registration_enabled === true)
        setTurnstileEnabled(settings.turnstile_enabled)
        setTurnstileSiteKey(settings.turnstile_site_key || '')
        setTencentCaptchaEnabled(settings.tencent_captcha_enabled === true)
        setTencentCaptchaAppId(settings.tencent_captcha_app_id || '')
        setTencentCaptchaRegion(settings.tencent_captcha_region || 'cn')
        setAliyunCaptchaEnabled(settings.aliyun_captcha_enabled === true)
        setAliyunCaptchaSceneId(settings.aliyun_captcha_scene_id || '')
        setAliyunCaptchaPrefix(settings.aliyun_captcha_prefix || '')
        setAliyunCaptchaRegion(settings.aliyun_captcha_region || 'cn')
        setLinuxdoOAuthEnabled(settings.linuxdo_oauth_enabled)
        setDingtalkOAuthEnabled(settings.dingtalk_oauth_enabled ?? false)
        setWechatOAuthEnabled(isWeChatWebOAuthEnabled(settings))
        setBackendModeEnabled(settings.backend_mode_enabled)
        setOidcOAuthEnabled(settings.oidc_oauth_enabled)
        setOidcOAuthProviderName(settings.oidc_oauth_provider_name || 'OIDC')
        setGithubOAuthEnabled(settings.github_oauth_enabled)
        setGoogleOAuthEnabled(settings.google_oauth_enabled)
        setPasswordResetEnabled(settings.password_reset_enabled)
        setPasskeyEnabled(settings.passkey_enabled === true)
        agreement.applySettings(settings)
      } catch (error) {
        console.error('Failed to load public settings:', error)
        if (cancelled) return
        agreement.applySettings({})
      } finally {
        if (!cancelled) setPublicSettingsLoaded(true)
      }
    })()

    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const redirectTarget = () => searchParams.get('redirect') || '/dashboard'

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
    setErrors({ email: '', password: '', turnstile: '' })

    let isValid = true

    if (agreementGateActive) {
      appStore.showWarning(t('legal.loginAgreementPrompt.loginRequiredWarning'))
      if (agreement.mode !== 'checkbox') {
        agreement.openModal()
      }
      return false
    }

    // Email validation
    if (!email.trim()) {
      setErrors((previous) => ({ ...previous, email: t('auth.emailRequired') }))
      isValid = false
    } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
      setErrors((previous) => ({ ...previous, email: t('auth.invalidEmail') }))
      isValid = false
    }

    // Password validation
    if (!password) {
      setErrors((previous) => ({ ...previous, password: t('auth.passwordRequired') }))
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

  async function handleLogin(event: React.FormEvent): Promise<void> {
    event.preventDefault()

    if (!validateForm()) {
      return
    }

    if (!(await acquireActionProof())) {
      return
    }

    setIsLoading(true)

    try {
      // Call auth store login（阿里云 captchaVerifyParam 复用 turnstile_token 字段）
      const credentials: LoginRequest = {
        email,
        password,
        turnstile_token: turnstileEnabled || aliyunCaptchaEnabled ? turnstileToken : undefined,
        tencent_captcha_ticket: tencentCaptchaEnabled ? turnstileToken : undefined,
        tencent_captcha_randstr: tencentCaptchaEnabled ? tencentCaptchaRandstr : undefined
      }
      const response = await authStore.login(credentials)

      // Check if 2FA is required
      if (isTotp2FARequired(response)) {
        const totpResponse = response as TotpLoginResponse
        setTotpTempToken(totpResponse.temp_token || '')
        setTotpUserEmailMasked(totpResponse.user_email_masked || '')
        setTotpErrorMessage('')
        setShow2FAModal(true)
        setIsLoading(false)
        return
      }

      // Show success toast
      clearAllAffiliateReferralCodes()
      appStore.showSuccess(t('auth.loginSuccess'))

      // Redirect to dashboard or intended route
      await navigate(redirectTarget())
    } catch (error: unknown) {
      const message = extractI18nErrorMessage(error, t, 'auth.errors', t('auth.loginFailed'))
      appStore.showError(message)
    } finally {
      if (captchaEnabled) {
        resetCaptchaProof()
      }
      setIsLoading(false)
    }
  }

  async function handlePasskeyLogin(): Promise<void> {
    if (agreementGateActive) {
      appStore.showWarning(t('legal.loginAgreementPrompt.loginRequiredWarning'))
      if (agreement.mode !== 'checkbox') {
        agreement.openModal()
      }
      return
    }

    setPasskeyLoading(true)
    try {
      let proof: ActionCaptchaRequestProof | undefined
      if (actionCaptchaEnabled) {
        const result = await captchaRef.current?.verifyAction()
        if (!result) return
        proof = tencentCaptchaEnabled
          ? { tencent_captcha_ticket: result.token, tencent_captcha_randstr: result.randstr }
          : { turnstile_token: result.token }
      }

      await authStore.loginWithPasskey(proof)
      clearAllAffiliateReferralCodes()
      appStore.showSuccess(t('auth.loginSuccess'))
      await navigate(redirectTarget())
    } catch (error: unknown) {
      const fallback =
        error instanceof DOMException && error.name === 'NotAllowedError'
          ? t('auth.passkeyCancelled')
          : t('auth.passkeyFailed')
      const message = extractI18nErrorMessage(error, t, 'auth.errors', fallback)
      appStore.showError(message)
    } finally {
      if (actionCaptchaEnabled) {
        resetCaptchaProof()
      }
      setPasskeyLoading(false)
    }
  }

  async function handleOAuthStart(request: OAuthLoginStart): Promise<void> {
    if (authActionDisabled) return

    if (!actionCaptchaEnabled) {
      window.location.href = buildOAuthLoginStartURL(request)
      return
    }

    setIsLoading(true)
    try {
      const proof = await captchaRef.current?.verifyAction()
      if (!proof) return

      const result = await startOAuthLogin(
        request,
        tencentCaptchaEnabled
          ? { tencent_captcha_ticket: proof.token, tencent_captcha_randstr: proof.randstr }
          : { turnstile_token: proof.token }
      )
      window.location.href = result.authorize_url
    } catch (error: unknown) {
      const message = extractI18nErrorMessage(error, t, 'auth.errors', t('auth.turnstileFailed'))
      appStore.showError(message)
    } finally {
      resetCaptchaProof()
      setIsLoading(false)
    }
  }

  // ==================== 2FA Handlers ====================

  async function handle2FAVerify(code: string): Promise<void> {
    setTotpVerifying(true)
    setTotpErrorMessage('')

    try {
      await authStore.login2FA(totpTempToken, code)

      setShow2FAModal(false)
      clearAllAffiliateReferralCodes()
      appStore.showSuccess(t('auth.loginSuccess'))
      await navigate(redirectTarget())
    } catch (error: unknown) {
      const err = error as { message?: string; response?: { data?: { message?: string } } }
      const message = err.response?.data?.message || err.message || t('profile.totp.loginFailed')
      setTotpErrorMessage(message)
    } finally {
      setTotpVerifying(false)
    }
  }

  function handle2FACancel(): void {
    setShow2FAModal(false)
    setTotpTempToken('')
    setTotpUserEmailMasked('')
    setTotpErrorMessage('')
  }

  return (
    <>
      <div className="space-y-6">
        {/* Title */}
        <div className="text-center">
          <h2 className="text-2xl font-bold text-gray-900 dark:text-white">{t('auth.welcomeBack')}</h2>
          <p className="mt-2 text-sm surface-muted">{t('auth.signInToAccount')}</p>
        </div>

        {/* Login Form */}
        <form onSubmit={handleLogin} className="space-y-5">
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
                disabled={authActionDisabled}
                className={cn('input pl-11', errors.email && 'input-error')}
                placeholder={t('auth.emailPlaceholder')}
              />
            </div>
          </div>

          {/* Password Input */}
          <div>
            <label htmlFor="password" className="input-label">
              {t('auth.passwordLabel')}
            </label>
            <div className="relative">
              <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
                <Icon name="lock" size="md" className="surface-muted" />
              </div>
              <input
                id="password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                type={showPassword ? 'text' : 'password'}
                required
                autoComplete="current-password"
                disabled={authActionDisabled}
                className={cn('input pl-11 pr-11', errors.password && 'input-error')}
                placeholder={t('auth.passwordPlaceholder')}
              />
              <button
                type="button"
                onClick={() => setShowPassword((value) => !value)}
                disabled={authActionDisabled}
                className="absolute inset-y-0 right-0 flex items-center pr-3.5 transition-colors hover:text-gray-600 dark:hover:text-dark-300 surface-control surface-muted"
              >
                <Icon name={showPassword ? 'eyeOff' : 'eye'} size="md" />
              </button>
            </div>
            <div className="mt-1 flex items-center justify-between">
              <span />
              {passwordResetEnabled && !backendModeEnabled && (
                <Link
                  to="/forgot-password"
                  className="text-sm font-medium text-primary-600 transition-colors hover:text-primary-500 dark:text-primary-400 dark:hover:text-primary-300"
                >
                  {t('auth.forgotPassword')}
                </Link>
              )}
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
            disabled={authActionDisabled || (turnstileEnabled && !turnstileToken)}
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
              <Icon name="login" size="md" className="mr-2" />
            )}
            {isLoading ? t('auth.signingIn') : t('auth.signIn')}
          </button>

          <LoginAgreementPrompt
            accepted={agreement.accepted}
            documents={agreement.documents}
            mode={agreement.mode}
            updatedAt={agreement.updatedAt}
            visible={agreement.modalVisible}
            onAccept={agreement.accept}
            onReject={() => agreement.reject('legal.loginAgreementPrompt.loginRejectedWarning')}
            onOpen={agreement.openModal}
          />

          {(showPasskeyLogin || showOAuthLogin) && (
            <div className="space-y-3 pt-1">
              <div className="flex items-center gap-3">
                <div className="h-px flex-1 surface-inset" />
                <span className="text-xs surface-muted">{t('auth.oauthOrContinue')}</span>
                <div className="h-px flex-1 surface-inset" />
              </div>

              {showPasskeyLogin && (
                <button
                  type="button"
                  className="btn btn-secondary w-full"
                  disabled={authActionDisabled}
                  onClick={() => void handlePasskeyLogin()}
                >
                  <Icon name="key" size="md" className="mr-2" />
                  {passkeyLoading ? t('auth.passkeySigningIn') : t('auth.passkeySignIn')}
                </button>
              )}

              <EmailOAuthButtons
                disabled={authActionDisabled}
                githubEnabled={githubOAuthEnabled}
                googleEnabled={googleOAuthEnabled}
                showDivider={false}
                onStart={(request) => void handleOAuthStart(request)}
              />

              {linuxdoOAuthEnabled && (
                <LinuxDoOAuthSection
                  disabled={authActionDisabled}
                  showDivider={false}
                  onStart={(request) => void handleOAuthStart(request)}
                />
              )}
              {dingtalkOAuthEnabled && (
                <DingTalkOAuthSection
                  disabled={authActionDisabled}
                  showDivider={false}
                  onStart={(request) => void handleOAuthStart(request)}
                />
              )}
              {wechatOAuthEnabled && (
                <WechatOAuthSection
                  disabled={authActionDisabled}
                  showDivider={false}
                  onStart={(request) => void handleOAuthStart(request)}
                />
              )}
              {oidcOAuthEnabled && (
                <OidcOAuthSection
                  disabled={authActionDisabled}
                  providerName={oidcOAuthProviderName}
                  showDivider={false}
                  onStart={(request) => void handleOAuthStart(request)}
                />
              )}
            </div>
          )}
        </form>
      </div>

      {/* Footer */}
      {!backendModeEnabled && publicSettingsLoaded && registrationEnabled && (
        <div className="mt-6 border-t border-gray-100 pt-4 text-center text-sm surface-inset dark:border-dark-800">
          <p className="surface-muted">
            {t('auth.dontHaveAccount')}
            <Link
              to="/register"
              className="font-medium text-primary-600 transition-colors hover:text-primary-500 dark:text-primary-400 dark:hover:text-primary-300"
            >
              {t('auth.signUp')}
            </Link>
          </p>
        </div>
      )}

      {/* 2FA Modal */}
      <TotpLoginModal
        open={show2FAModal}
        tempToken={totpTempToken}
        userEmailMasked={totpUserEmailMasked}
        verifying={totpVerifying}
        errorMessage={totpErrorMessage}
        onVerify={(code) => void handle2FAVerify(code)}
        onCancel={handle2FACancel}
      />
    </>
  )
}
