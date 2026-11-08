import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/auth/Icon'
import { CaptchaChallenge, type CaptchaChallengeHandle } from '@/components/auth/CaptchaChallenge'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import {
  getPublicSettings,
  isOAuthLoginCompletion,
  persistOAuthTokenContext,
  sendPendingOAuthVerifyCode,
  sendVerifyCode,
  type PendingOAuthSendVerifyCodeResponse
} from '@/api/auth'
import { apiClient } from '@/api/client'
import { buildAuthErrorMessage } from '@/utils/authError'
import { extractApiErrorCode } from '@/utils/apiError'
import {
  formatRegistrationEmailSuffixWhitelistForMessage,
  isRegistrationEmailSuffixAllowed,
  normalizeRegistrationEmailSuffixWhitelist
} from '@/utils/registrationEmailPolicy'
import { clearAllAffiliateReferralCodes, loadAffiliateReferralCode, oauthAffiliatePayload } from '@/utils/oauthAffiliate'
import type { PendingAuthSessionSummary } from '@/stores/auth'
import type { SendVerifyCodeRequest } from '@/types'
import { cn } from '@/lib/utils'
import '@/views/authViews.css'
import { AuthLayout } from '@/components/layout/AuthLayout'

type PendingOAuthCreateAccountResponse = {
  auth_result?: string
  access_token: string
  refresh_token?: string
  expires_in?: number
  token_type?: string
  provider?: string
  redirect?: string
}

interface RegisterData {
  email?: string
  password?: string
  turnstile_token?: string
  tencent_captcha_ticket?: string
  tencent_captcha_randstr?: string
  promo_code?: string
  invitation_code?: string
  aff_code?: string
  pending_auth_token?: string
  pending_auth_token_field?: 'pending_auth_token' | 'pending_oauth_token'
  pending_provider?: string
  pending_redirect?: string
  pending_adoption_decision?: { adopt_display_name?: boolean; adopt_avatar?: boolean }
}

export default function EmailVerifyView() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const authStore = useAuthStore()
  const appStore = useAppStore()

  const [isLoading, setIsLoading] = useState(false)
  const [isSendingCode, setIsSendingCode] = useState(false)
  const [codeSent, setCodeSent] = useState(false)
  const [verifyCode, setVerifyCode] = useState('')
  const [countdown, setCountdown] = useState(0)

  // Registration data from sessionStorage
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [initialTurnstileToken, setInitialTurnstileToken] = useState('')
  const [initialTencentCaptchaRandstr, setInitialTencentCaptchaRandstr] = useState('')
  const [promoCode, setPromoCode] = useState('')
  const [invitationCode, setInvitationCode] = useState('')
  const [affCode, setAffCode] = useState('')
  const [pendingAuthToken, setPendingAuthToken] = useState('')
  const [pendingAuthTokenField, setPendingAuthTokenField] = useState<'pending_auth_token' | 'pending_oauth_token'>(
    'pending_auth_token'
  )
  const [pendingProvider, setPendingProvider] = useState('')
  const [pendingRedirect, setPendingRedirect] = useState('')
  const [pendingAdoptionDecision, setPendingAdoptionDecision] = useState<{
    adoptDisplayName?: boolean
    adoptAvatar?: boolean
  } | null>(null)
  const [hasRegisterData, setHasRegisterData] = useState(false)

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
  const [siteName, setSiteName] = useState('Sub2API Plus')
  const [registrationEmailSuffixWhitelist, setRegistrationEmailSuffixWhitelist] = useState<string[]>([])
  // 域名限量注册开关：开启时非白名单域名可注册 1 个账户（由后端判定），前端不做白名单预检。
  const [emailDomainQuotaEnabled, setEmailDomainQuotaEnabled] = useState(false)

  // Turnstile for resend
  const captchaRef = useRef<CaptchaChallengeHandle | null>(null)
  const createAccountCaptchaRef = useRef<CaptchaChallengeHandle | null>(null)
  const [resendTurnstileToken, setResendTurnstileToken] = useState('')
  const [resendTencentCaptchaRandstr, setResendTencentCaptchaRandstr] = useState('')
  const [createAccountTurnstileToken, setCreateAccountTurnstileToken] = useState('')
  const [createAccountTencentCaptchaRandstr, setCreateAccountTencentCaptchaRandstr] = useState('')
  const [showResendTurnstile, setShowResendTurnstile] = useState(false)

  const [errors, setErrors] = useState({ code: '', turnstile: '' })

  const countdownTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  /**
   * Async handlers read the live values through this mirror so a field written
   * moments earlier (for example while mounting) is visible immediately, exactly
   * like the Vue refs these handlers replace.
   */
  const liveRef = useRef({
    email: '',
    password: '',
    initialTurnstileToken: '',
    initialTencentCaptchaRandstr: '',
    resendTurnstileToken: '',
    resendTencentCaptchaRandstr: '',
    turnstileEnabled: false,
    aliyunCaptchaEnabled: false,
    tencentCaptchaEnabled: false,
    pendingAuthToken: '',
    pendingAuthTokenField: 'pending_auth_token' as 'pending_auth_token' | 'pending_oauth_token',
    pendingProvider: '',
    emailDomainQuotaEnabled: false,
    registrationEmailSuffixWhitelist: [] as string[]
  })

  useEffect(() => {
    liveRef.current = {
      email,
      password,
      initialTurnstileToken,
      initialTencentCaptchaRandstr,
      resendTurnstileToken,
      resendTencentCaptchaRandstr,
      turnstileEnabled,
      aliyunCaptchaEnabled,
      tencentCaptchaEnabled,
      pendingAuthToken,
      pendingAuthTokenField,
      pendingProvider,
      emailDomainQuotaEnabled,
      registrationEmailSuffixWhitelist
    }
  })

  const aliyunCaptchaReady =
    aliyunCaptchaEnabled && Boolean(aliyunCaptchaSceneId) && Boolean(aliyunCaptchaPrefix)
  // 动作触发式验证码（腾讯/阿里云）：重发验证码、创建账号时弹窗验证
  const actionCaptchaEnabled =
    (tencentCaptchaEnabled && Boolean(tencentCaptchaAppId)) || aliyunCaptchaReady
  const captchaEnabled = (turnstileEnabled && Boolean(turnstileSiteKey)) || actionCaptchaEnabled

  const validationToastMessage = errors.code || errors.turnstile || ''

  const isPendingOAuthFlow = () => Boolean(liveRef.current.pendingProvider.trim())
  const pendingOAuthCreateTurnstileRequired = isPendingOAuthFlow() && turnstileEnabled
  const pendingOAuthCreateCaptchaEnabled = isPendingOAuthFlow() && captchaEnabled

  // Validation messages surface as toasts, matching the Vue watcher.
  useEffect(() => {
    if (validationToastMessage) {
      appStore.showError(validationToastMessage)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [validationToastMessage])

  useEffect(() => {
    const timer = setInterval(() => {
      setCountdown((previous) => {
        if (previous > 0) {
          return previous - 1
        }
        if (countdownTimerRef.current) {
          clearInterval(countdownTimerRef.current)
          countdownTimerRef.current = null
        }
        return previous
      })
    }, 1000)
    countdownTimerRef.current = timer
    return () => {
      if (countdownTimerRef.current) {
        clearInterval(countdownTimerRef.current)
        countdownTimerRef.current = null
      }
    }
  }, [])

  // ==================== Lifecycle ====================

  useEffect(() => {
    let cancelled = false
    let registerDataAvailable = false

    const activePendingSession = authStore.pendingAuthSession as PendingAuthSessionSummary | null

    // Load registration data from sessionStorage
    const registerDataStr = sessionStorage.getItem('register_data')
    if (registerDataStr) {
      try {
        const registerData = JSON.parse(registerDataStr) as RegisterData
        if (cancelled) return
        setEmail(registerData.email || '')
        setPassword(registerData.password || '')
        setInitialTurnstileToken(registerData.tencent_captcha_ticket || registerData.turnstile_token || '')
        setInitialTencentCaptchaRandstr(registerData.tencent_captcha_randstr || '')
        setPromoCode(registerData.promo_code || '')
        setInvitationCode(registerData.invitation_code || '')
        setAffCode(registerData.aff_code || loadAffiliateReferralCode())
        setPendingAuthToken(registerData.pending_auth_token || activePendingSession?.token || '')
        setPendingAuthTokenField(
          registerData.pending_auth_token_field || activePendingSession?.token_field || 'pending_auth_token'
        )
        setPendingProvider(registerData.pending_provider || activePendingSession?.provider || '')
        setPendingRedirect(registerData.pending_redirect || activePendingSession?.redirect || '')
        setPendingAdoptionDecision(
          registerData.pending_adoption_decision
            ? {
                adoptDisplayName: registerData.pending_adoption_decision.adopt_display_name === true,
                adoptAvatar: registerData.pending_adoption_decision.adopt_avatar === true
              }
            : null
        )
        registerDataAvailable = Boolean(registerData.email && registerData.password)
        setHasRegisterData(registerDataAvailable)
      } catch {
        if (!cancelled) setHasRegisterData(false)
      }
    } else if (activePendingSession) {
      setPendingAuthToken(activePendingSession.token)
      setPendingAuthTokenField(activePendingSession.token_field)
      setPendingProvider(activePendingSession.provider)
      setPendingRedirect(activePendingSession.redirect || '')
    }

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
        setSiteName(settings.site_name || 'Sub2API Plus')
        setRegistrationEmailSuffixWhitelist(
          normalizeRegistrationEmailSuffixWhitelist(settings.registration_email_suffix_whitelist || [])
        )
        setEmailDomainQuotaEnabled(settings.registration_email_domain_quota_enabled === true)
      } catch (error) {
        console.error('Failed to load public settings:', error)
      }

      // Auto-send verification code if we have valid data
      if (registerDataAvailable) {
        await sendCode()
      }
    })()

    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // ==================== Captcha Handlers ====================

  function onTurnstileVerify(token: string, randstr = ''): void {
    setResendTurnstileToken(token)
    setResendTencentCaptchaRandstr(randstr)
    setErrors((previous) => ({ ...previous, turnstile: '' }))
  }

  function onTurnstileExpire(): void {
    setResendTurnstileToken('')
    setResendTencentCaptchaRandstr('')
    setErrors((previous) => ({ ...previous, turnstile: t('auth.turnstileExpired') }))
  }

  function onTurnstileError(): void {
    setResendTurnstileToken('')
    setResendTencentCaptchaRandstr('')
    setErrors((previous) => ({ ...previous, turnstile: t('auth.turnstileFailed') }))
  }

  function onCreateAccountTurnstileVerify(token: string, randstr = ''): void {
    setCreateAccountTurnstileToken(token)
    setCreateAccountTencentCaptchaRandstr(randstr)
    setErrors((previous) => ({ ...previous, turnstile: '' }))
  }

  function onCreateAccountTurnstileExpire(): void {
    setCreateAccountTurnstileToken('')
    setCreateAccountTencentCaptchaRandstr('')
    setErrors((previous) => ({ ...previous, turnstile: t('auth.turnstileExpired') }))
  }

  function onCreateAccountTurnstileError(): void {
    setCreateAccountTurnstileToken('')
    setCreateAccountTencentCaptchaRandstr('')
    setErrors((previous) => ({ ...previous, turnstile: t('auth.turnstileFailed') }))
  }

  function resetCreateAccountTurnstile(): void {
    setCreateAccountTurnstileToken('')
    setCreateAccountTencentCaptchaRandstr('')
    createAccountCaptchaRef.current?.reset()
  }

  async function acquireResendActionProof(): Promise<boolean> {
    if (!actionCaptchaEnabled) return true

    const proof = await captchaRef.current?.verifyAction()
    if (!proof) return false

    setResendTurnstileToken(proof.token)
    setResendTencentCaptchaRandstr(proof.randstr)
    return true
  }

  async function acquireCreateAccountActionProof(): Promise<boolean> {
    if (!isPendingOAuthFlow() || !actionCaptchaEnabled) return true

    const proof = await createAccountCaptchaRef.current?.verifyAction()
    if (!proof) return false

    setCreateAccountTurnstileToken(proof.token)
    setCreateAccountTencentCaptchaRandstr(proof.randstr)
    return true
  }

  // 域名限量注册开启时交由后端按额度判定；pending OAuth / 换绑流程沿用后端策略，前端不预检。
  function shouldBypassRegistrationEmailPolicy(): boolean {
    const live = liveRef.current
    return live.emailDomainQuotaEnabled || isPendingOAuthFlow() || Boolean(live.pendingAuthToken.trim())
  }

  function resolvePendingOAuthCallbackRoute(provider: string): string {
    switch (provider.trim().toLowerCase()) {
      case 'linuxdo':
        return '/auth/linuxdo/callback'
      case 'oidc':
        return '/auth/oidc/callback'
      case 'wechat':
        return '/auth/wechat/callback'
      default:
        return '/auth/callback'
    }
  }

  function isPendingOAuthSessionResponse(data: PendingOAuthCreateAccountResponse): boolean {
    return data.auth_result === 'pending_session'
  }

  function getPendingOAuthSendCodeSessionResponse(
    data: PendingOAuthSendVerifyCodeResponse
  ): PendingOAuthSendVerifyCodeResponse | null {
    return data.auth_result === 'pending_session' ? data : null
  }

  function persistPendingOAuthSession(provider: string, redirect?: string): void {
    authStore.setPendingAuthSession({
      token: pendingAuthToken,
      token_field: pendingAuthTokenField,
      provider: provider.trim() || pendingProvider.trim(),
      redirect: redirect || pendingRedirect || undefined
    })
  }

  function buildEmailSuffixNotAllowedMessage(): string {
    const normalizedWhitelist = normalizeRegistrationEmailSuffixWhitelist(registrationEmailSuffixWhitelist)
    if (normalizedWhitelist.length === 0) {
      return t('auth.emailSuffixNotAllowed')
    }
    const separator = String(i18n.language || '').toLowerCase().startsWith('zh') ? '、' : ', '
    return t('auth.emailSuffixNotAllowedWithAllowed', {
      suffixes: formatRegistrationEmailSuffixWhitelistForMessage(normalizedWhitelist, {
        separator,
        more: (count) => t('auth.emailSuffixAllowedMore', { count })
      })
    })
  }

  // ==================== Send Code ====================

  async function sendCode(): Promise<void> {
    setIsSendingCode(true)
    let requestSucceeded = false
    let captchaProofUsed = false

    try {
      const live = liveRef.current
      if (
        !shouldBypassRegistrationEmailPolicy() &&
        !isRegistrationEmailSuffixAllowed(live.email, live.registrationEmailSuffixWhitelist)
      ) {
        const message = buildEmailSuffixNotAllowedMessage()
        appStore.showError(message)
        return
      }

      const requestPayload = {
        email: live.email,
        [live.pendingAuthTokenField]: live.pendingAuthToken || undefined,
        // 优先使用重发时新获取的 token（因为初始 token 可能已被使用）
        turnstile_token:
          live.turnstileEnabled || live.aliyunCaptchaEnabled
            ? live.resendTurnstileToken || live.initialTurnstileToken || undefined
            : undefined,
        tencent_captcha_ticket: live.tencentCaptchaEnabled
          ? live.resendTurnstileToken || live.initialTurnstileToken || undefined
          : undefined,
        tencent_captcha_randstr: live.tencentCaptchaEnabled
          ? live.resendTencentCaptchaRandstr || live.initialTencentCaptchaRandstr || undefined
          : undefined
      } as SendVerifyCodeRequest
      captchaProofUsed = Boolean(requestPayload.turnstile_token || requestPayload.tencent_captcha_ticket)
      const response = isPendingOAuthFlow()
        ? await sendPendingOAuthVerifyCode(requestPayload)
        : await sendVerifyCode(requestPayload)
      requestSucceeded = true

      const pendingSendCodeSession = isPendingOAuthFlow()
        ? getPendingOAuthSendCodeSessionResponse(response as PendingOAuthSendVerifyCodeResponse)
        : null
      if (pendingSendCodeSession) {
        sessionStorage.removeItem('register_data')
        persistPendingOAuthSession(
          pendingSendCodeSession.provider || live.pendingProvider,
          pendingSendCodeSession.redirect
        )
        await navigate(
          resolvePendingOAuthCallbackRoute(pendingSendCodeSession.provider || live.pendingProvider)
        )
        return
      }

      setCodeSent(true)
      setCountdown(response.countdown)

      setShowResendTurnstile(false)
    } catch (error: unknown) {
      const message = buildRegistrationErrorMessage(error, t('auth.sendCodeFailed'))
      appStore.showError(message)
    } finally {
      if (captchaProofUsed) {
        clearStoredCaptchaProof()
        setInitialTurnstileToken('')
        setInitialTencentCaptchaRandstr('')
        setResendTurnstileToken('')
        setResendTencentCaptchaRandstr('')
        captchaRef.current?.reset()
        if (!requestSucceeded && turnstileEnabled) {
          setShowResendTurnstile(true)
        }
      }
      setIsSendingCode(false)
    }
  }

  function clearStoredCaptchaProof(): void {
    const registerDataStr = sessionStorage.getItem('register_data')
    if (!registerDataStr) return

    try {
      const registerData = JSON.parse(registerDataStr) as Record<string, unknown>
      delete registerData.turnstile_token
      delete registerData.tencent_captcha_ticket
      delete registerData.tencent_captcha_randstr
      sessionStorage.setItem('register_data', JSON.stringify(registerData))
    } catch {
      // Invalid registration state is handled by the existing mount parser.
    }
  }

  // ==================== Handlers ====================

  async function handleResendCode(): Promise<void> {
    // Turnstile stays staged; Tencent is acquired from this action.
    if (turnstileEnabled && !showResendTurnstile) {
      setShowResendTurnstile(true)
      return
    }

    if (turnstileEnabled && !resendTurnstileToken) {
      setErrors((previous) => ({ ...previous, turnstile: t('auth.completeVerification') }))
      return
    }

    if (!(await acquireResendActionProof())) {
      return
    }

    await sendCode()
  }

  function validateForm(): boolean {
    setErrors((previous) => ({ ...previous, code: '' }))

    if (!verifyCode.trim()) {
      setErrors((previous) => ({ ...previous, code: t('auth.codeRequired') }))
      return false
    }

    if (!/^\d{6}$/.test(verifyCode.trim())) {
      setErrors((previous) => ({ ...previous, code: t('auth.invalidCode') }))
      return false
    }

    return true
  }

  async function handleVerify(event: React.FormEvent): Promise<void> {
    event.preventDefault()

    if (!validateForm()) {
      return
    }

    if (
      !shouldBypassRegistrationEmailPolicy() &&
      !isRegistrationEmailSuffixAllowed(liveRef.current.email, liveRef.current.registrationEmailSuffixWhitelist)
    ) {
      const message = buildEmailSuffixNotAllowedMessage()
      appStore.showError(message)
      return
    }

    if (!(await acquireCreateAccountActionProof())) {
      return
    }

    setIsLoading(true)

    try {
      if (isPendingOAuthFlow()) {
        const payload: Record<string, unknown> = {
          email,
          password,
          verify_code: verifyCode.trim(),
          ...((turnstileEnabled || aliyunCaptchaEnabled) && createAccountTurnstileToken
            ? { turnstile_token: createAccountTurnstileToken }
            : {}),
          ...(tencentCaptchaEnabled && createAccountTurnstileToken
            ? {
                tencent_captcha_ticket: createAccountTurnstileToken,
                tencent_captcha_randstr: createAccountTencentCaptchaRandstr
              }
            : {}),
          ...oauthAffiliatePayload(affCode || loadAffiliateReferralCode())
        }
        if (invitationCode) {
          payload.invitation_code = invitationCode
        }
        if (pendingAdoptionDecision?.adoptDisplayName !== undefined) {
          payload.adopt_display_name = pendingAdoptionDecision.adoptDisplayName
        }
        if (pendingAdoptionDecision?.adoptAvatar !== undefined) {
          payload.adopt_avatar = pendingAdoptionDecision.adoptAvatar
        }

        const { data } = await apiClient.post<PendingOAuthCreateAccountResponse>(
          '/auth/oauth/pending/create-account',
          payload
        )
        if (isPendingOAuthSessionResponse(data)) {
          sessionStorage.removeItem('register_data')
          persistPendingOAuthSession(data.provider || pendingProvider, data.redirect)
          await navigate(resolvePendingOAuthCallbackRoute(data.provider || pendingProvider))
          return
        }
        if (!isOAuthLoginCompletion(data)) {
          throw new Error(t('auth.verifyFailed'))
        }

        persistOAuthTokenContext(data)
        await authStore.setToken(data.access_token)
        authStore.clearPendingAuthSession()
      } else {
        // Register with verification code
        await authStore.register({
          email,
          password,
          verify_code: verifyCode.trim(),
          turnstile_token:
            turnstileEnabled || aliyunCaptchaEnabled ? initialTurnstileToken || undefined : undefined,
          tencent_captcha_ticket: tencentCaptchaEnabled ? initialTurnstileToken || undefined : undefined,
          tencent_captcha_randstr: tencentCaptchaEnabled ? initialTencentCaptchaRandstr || undefined : undefined,
          promo_code: promoCode || undefined,
          invitation_code: invitationCode || undefined,
          ...(affCode ? { aff_code: affCode } : {})
        })
      }

      // Clear session data
      sessionStorage.removeItem('register_data')
      clearAllAffiliateReferralCodes()

      // Show success toast
      appStore.showSuccess(t('auth.accountCreatedSuccess', { siteName }))

      // Redirect to dashboard
      await navigate(pendingRedirect || '/dashboard')
    } catch (error: unknown) {
      const message = buildRegistrationErrorMessage(error, t('auth.verifyFailed'))
      appStore.showError(message)
    } finally {
      setInitialTurnstileToken('')
      setInitialTencentCaptchaRandstr('')
      if (pendingOAuthCreateCaptchaEnabled) {
        resetCreateAccountTurnstile()
      }
      setIsLoading(false)
    }
  }

  function buildRegistrationErrorMessage(error: unknown, fallback: string): string {
    if (extractApiErrorCode(error) === 'EMAIL_DOMAIN_REGISTRATION_LIMIT') {
      return t('auth.emailDomainRegistrationLimit')
    }
    if (extractApiErrorCode(error) === 'EMAIL_BLOCKED') {
      return t('auth.errors.EMAIL_BLOCKED')
    }
    return buildAuthErrorMessage(error, { fallback })
  }

  function handleBack(): void {
    // Clear session data
    sessionStorage.removeItem('register_data')

    // Go back to registration
    void navigate('/register')
  }

  return (
    <AuthLayout
      footer={
        <>
        <button
          onClick={handleBack}
          className="flex items-center gap-2 transition-colors hover:text-gray-700 dark:hover:text-gray-300 surface-control surface-muted"
        >
          <Icon name="arrowLeft" size="sm" />
          {t('auth.backToRegistration')}
        </button>
        </>
      }
    >
      <div className="space-y-6">
        {/* Title */}
        <div className="text-center">
          <h2 className="text-2xl font-bold text-gray-900 dark:text-white">{t('auth.verifyYourEmail')}</h2>
          <p className="mt-2 text-sm surface-muted">
            {t('auth.sendCodeDesc')}
            <span className="font-medium surface-muted">{email}</span>
          </p>
        </div>

        {/* No Data Warning */}
        {!hasRegisterData && (
          <div className="rounded-xl border border-amber-200 bg-amber-50/[0.15] p-4 dark:border-amber-800/50 dark:bg-amber-900/20">
            <div className="flex items-start gap-3">
              <div className="flex-shrink-0">
                <Icon name="exclamationCircle" size="md" className="text-amber-500" />
              </div>
              <div className="text-sm text-amber-700 dark:text-amber-400">
                <p className="font-medium">{t('auth.sessionExpired')}</p>
                <p className="mt-1">{t('auth.sessionExpiredDesc')}</p>
              </div>
            </div>
          </div>
        )}

        {/* Verification Form */}
        {hasRegisterData && (
          <form onSubmit={handleVerify} className="space-y-5">
            {/* Verification Code Input */}
            <div>
              <label htmlFor="code" className="input-label text-center">
                {t('auth.verificationCode')}
              </label>
              <input
                id="code"
                value={verifyCode}
                onChange={(event) => setVerifyCode(event.target.value)}
                type="text"
                required
                autoComplete="one-time-code"
                inputMode="numeric"
                maxLength={6}
                disabled={isLoading}
                className={cn(
                  'input py-3 text-center font-mono text-xl tracking-[0.5em]',
                  errors.code && 'input-error'
                )}
                placeholder="000000"
              />
              <p className="input-hint text-center">{t('auth.verificationCodeHint')}</p>
            </div>

            {/* Code Status */}
            {codeSent && (
              <div className="rounded-xl border border-green-200 bg-green-50/[0.15] p-4 dark:border-green-800/50 dark:bg-green-900/20">
                <div className="flex items-start gap-3">
                  <div className="flex-shrink-0">
                    <Icon name="checkCircle" size="md" className="text-green-500" />
                  </div>
                  <p className="text-sm text-green-700 dark:text-green-400">{t('auth.codeSentSuccess')}</p>
                </div>
              </div>
            )}

            {/* Turnstile Widget for Resend */}
            {(actionCaptchaEnabled || (turnstileEnabled && showResendTurnstile)) && (
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

            {pendingOAuthCreateCaptchaEnabled && (
              <div className="space-y-2">
                <CaptchaChallenge
                  ref={createAccountCaptchaRef}
                  turnstileEnabled={turnstileEnabled}
                  turnstileSiteKey={turnstileSiteKey}
                  tencentEnabled={tencentCaptchaEnabled}
                  tencentAppId={tencentCaptchaAppId}
                  tencentRegion={tencentCaptchaRegion}
                  aliyunEnabled={aliyunCaptchaEnabled}
                  aliyunSceneId={aliyunCaptchaSceneId}
                  aliyunPrefix={aliyunCaptchaPrefix}
                  aliyunRegion={aliyunCaptchaRegion}
                  onVerify={onCreateAccountTurnstileVerify}
                  onExpire={onCreateAccountTurnstileExpire}
                  onError={onCreateAccountTurnstileError}
                />
              </div>
            )}

            {/* Submit Button */}
            <button
              type="submit"
              disabled={
                isLoading ||
                !verifyCode ||
                (pendingOAuthCreateTurnstileRequired && !createAccountTurnstileToken)
              }
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
                <Icon name="checkCircle" size="md" className="mr-2" />
              )}
              {isLoading ? t('auth.verifying') : t('auth.verifyAndCreate')}
            </button>

            {/* Resend Code */}
            <div className="text-center">
              {countdown > 0 ? (
                <button
                  type="button"
                  disabled
                  className="cursor-not-allowed text-sm surface-control surface-muted"
                >
                  {t('auth.resendCountdown', { countdown })}
                </button>
              ) : (
                <button
                  type="button"
                  onClick={() => void handleResendCode()}
                  disabled={
                    isSendingCode || (turnstileEnabled && showResendTurnstile && !resendTurnstileToken)
                  }
                  className="text-sm text-primary-600 transition-colors hover:text-primary-500 disabled:cursor-not-allowed disabled:opacity-50 dark:text-primary-400 dark:hover:text-primary-300 surface-control"
                >
                  {isSendingCode ? (
                    <span>{t('auth.sendingCode')}</span>
                  ) : captchaEnabled && !showResendTurnstile ? (
                    <span>{t('auth.clickToResend')}</span>
                  ) : (
                    <span>{t('auth.resendCode')}</span>
                  )}
                </button>
              )}
            </div>
          </form>
        )}
      </div>

      {/* Footer */}
    </AuthLayout>
  )
}
