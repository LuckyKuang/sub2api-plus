import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/auth/Icon'
import { CaptchaChallenge, type CaptchaChallengeHandle } from '@/components/auth/CaptchaChallenge'
import { LoginAgreementPrompt } from '@/components/auth/LoginAgreementPrompt'
import { EmailOAuthButtons } from '@/components/auth/EmailOAuthButtons'
import { LinuxDoOAuthSection } from '@/components/auth/ProviderOAuthSections'
import { WechatOAuthSection } from '@/components/auth/WechatOAuthSection'
import { OidcOAuthSection } from '@/components/auth/OidcOAuthSection'
import { useLoginAgreement } from '@/components/auth/useLoginAgreement'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import {
  buildOAuthLoginStartURL,
  getPublicSettings,
  isWeChatWebOAuthEnabled,
  startOAuthLogin,
  validateInvitationCode,
  validatePromoCode,
  type OAuthLoginStart
} from '@/api/auth'
import { buildAuthErrorMessage } from '@/utils/authError'
import { extractApiErrorCode, extractI18nErrorMessage } from '@/utils/apiError'
import {
  formatRegistrationEmailSuffixWhitelistForMessage,
  isRegistrationEmailSuffixAllowed,
  normalizeRegistrationEmailSuffixWhitelist
} from '@/utils/registrationEmailPolicy'
import {
  clearAffiliateReferralCode,
  loadAffiliateReferralCode,
  resolveAffiliateReferralCode
} from '@/utils/oauthAffiliate'
import type { RegisterRequest } from '@/types'
import { cn } from '@/lib/utils'
import '@/views/authViews.css'
import { AuthLayout } from '@/components/layout/AuthLayout'

export default function RegisterView() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const authStore = useAuthStore()
  const appStore = useAppStore()
  const agreement = useLoginAgreement()

  const [isLoading, setIsLoading] = useState(false)
  const [settingsLoaded, setSettingsLoaded] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [showConfirmPassword, setShowConfirmPassword] = useState(false)

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [promoCode, setPromoCode] = useState('')
  const [invitationCode, setInvitationCode] = useState('')
  const [affCode, setAffCode] = useState('')

  // Public settings
  const [registrationEnabled, setRegistrationEnabled] = useState(true)
  const [emailVerifyEnabled, setEmailVerifyEnabled] = useState(false)
  // Public settings are injected into the app store before the first render. Use that
  // value for the first render so a disabled promo-code field never flashes
  // while the async settings request is still in flight. If injection is
  // unavailable, fail closed until the request explicitly enables the field.
  const [promoCodeEnabled, setPromoCodeEnabled] = useState(
    appStore.cachedPublicSettings?.promo_code_enabled === true
  )
  const [invitationCodeEnabled, setInvitationCodeEnabled] = useState(false)
  const [affiliateEnabled, setAffiliateEnabled] = useState(false)
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
  const [linuxdoOAuthEnabled, setLinuxdoOAuthEnabled] = useState(false)
  const [wechatOAuthEnabled, setWechatOAuthEnabled] = useState(false)
  const [oidcOAuthEnabled, setOidcOAuthEnabled] = useState(false)
  const [oidcOAuthProviderName, setOidcOAuthProviderName] = useState('OIDC')
  const [githubOAuthEnabled, setGithubOAuthEnabled] = useState(false)
  const [googleOAuthEnabled, setGoogleOAuthEnabled] = useState(false)
  const [registrationEmailSuffixWhitelist, setRegistrationEmailSuffixWhitelist] = useState<string[]>([])
  // 域名限量注册开关：开启时非白名单域名可注册 1 个账户（由后端判定），前端不做白名单预检。
  const [emailDomainQuotaEnabled, setEmailDomainQuotaEnabled] = useState(false)

  const [turnstileToken, setTurnstileToken] = useState('')
  const [tencentCaptchaRandstr, setTencentCaptchaRandstr] = useState('')
  const captchaRef = useRef<CaptchaChallengeHandle | null>(null)

  // Promo code validation
  const [promoValidating, setPromoValidating] = useState(false)
  const [promoValidation, setPromoValidation] = useState({
    valid: false,
    invalid: false,
    bonusAmount: null as number | null,
    message: ''
  })

  // Invitation code validation
  const [invitationValidating, setInvitationValidating] = useState(false)
  const [invitationValidation, setInvitationValidation] = useState({
    valid: false,
    invalid: false,
    message: ''
  })

  const [errors, setErrors] = useState({
    email: '',
    password: '',
    confirmPassword: '',
    turnstile: '',
    invitation_code: ''
  })

  const promoValidateTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const invitationValidateTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const aliyunCaptchaReady =
    aliyunCaptchaEnabled && Boolean(aliyunCaptchaSceneId) && Boolean(aliyunCaptchaPrefix)
  // 动作触发式验证码（腾讯/阿里云）：提交、OAuth 启动时弹窗验证
  const actionCaptchaEnabled =
    (tencentCaptchaEnabled && Boolean(tencentCaptchaAppId)) || aliyunCaptchaReady
  const captchaEnabled = (turnstileEnabled && Boolean(turnstileSiteKey)) || actionCaptchaEnabled

  const validationToastMessage = useMemo(
    () =>
      errors.email ||
      errors.password ||
      errors.confirmPassword ||
      (invitationValidation.invalid ? invitationValidation.message : '') ||
      errors.invitation_code ||
      (promoValidation.invalid ? promoValidation.message : '') ||
      errors.turnstile ||
      '',
    [errors, invitationValidation, promoValidation]
  )

  const showOAuthLogin = useMemo(
    () =>
      linuxdoOAuthEnabled ||
      wechatOAuthEnabled ||
      oidcOAuthEnabled ||
      githubOAuthEnabled ||
      googleOAuthEnabled,
    [linuxdoOAuthEnabled, wechatOAuthEnabled, oidcOAuthEnabled, githubOAuthEnabled, googleOAuthEnabled]
  )

  const agreementGateActive = agreement.enabled && !agreement.accepted

  const registrationActionDisabled = isLoading || !settingsLoaded || agreementGateActive

  const showRegistrationDisabled = !registrationEnabled && settingsLoaded

  // Validation messages surface as toasts, matching the Vue watcher.
  useEffect(() => {
    if (validationToastMessage) {
      appStore.showError(validationToastMessage)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [validationToastMessage])

  useEffect(
    () => () => {
      if (promoValidateTimeoutRef.current) {
        clearTimeout(promoValidateTimeoutRef.current)
      }
      if (invitationValidateTimeoutRef.current) {
        clearTimeout(invitationValidateTimeoutRef.current)
      }
    },
    []
  )

  function syncAffiliateReferralCode(): string {
    const code = resolveAffiliateReferralCode(
      searchParams.get('aff'),
      searchParams.get('aff_code')
    )
    if (code) {
      setAffCode(code)
    }
    return code
  }

  // ==================== Lifecycle ====================

  useEffect(() => {
    syncAffiliateReferralCode()

    let cancelled = false
    void (async () => {
      try {
        const settings = await getPublicSettings()
        if (cancelled) return
        setRegistrationEnabled(settings.registration_enabled)
        setEmailVerifyEnabled(settings.email_verify_enabled)
        setPromoCodeEnabled(settings.promo_code_enabled)
        setInvitationCodeEnabled(settings.invitation_code_enabled)
        setAffiliateEnabled(settings.affiliate_enabled)
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
        setLinuxdoOAuthEnabled(settings.linuxdo_oauth_enabled)
        setWechatOAuthEnabled(isWeChatWebOAuthEnabled(settings))
        setOidcOAuthEnabled(settings.oidc_oauth_enabled)
        setOidcOAuthProviderName(settings.oidc_oauth_provider_name || 'OIDC')
        setGithubOAuthEnabled(settings.github_oauth_enabled)
        setGoogleOAuthEnabled(settings.google_oauth_enabled)
        setRegistrationEmailSuffixWhitelist(
          normalizeRegistrationEmailSuffixWhitelist(settings.registration_email_suffix_whitelist || [])
        )
        setEmailDomainQuotaEnabled(settings.registration_email_domain_quota_enabled === true)
        agreement.applySettings(settings)

        // Read promo code from URL parameter only if promo code is enabled
        if (settings.promo_code_enabled) {
          const promoParam = searchParams.get('promo')
          if (promoParam) {
            setPromoCode(promoParam)
            // Validate the promo code from URL
            await validatePromoCodeDebounced(promoParam)
          }
        }
        syncAffiliateReferralCode()
      } catch (error) {
        console.error('Failed to load public settings:', error)
        if (cancelled) return
        agreement.applySettings({})
      } finally {
        if (!cancelled) setSettingsLoaded(true)
      }
    })()

    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // ==================== Promo Code Validation ====================

  function handlePromoCodeInput(): void {
    const code = promoCode.trim()

    // Clear previous validation
    setPromoValidation({ valid: false, invalid: false, bonusAmount: null, message: '' })

    if (!code) {
      setPromoValidating(false)
      return
    }

    // Debounce validation
    if (promoValidateTimeoutRef.current) {
      clearTimeout(promoValidateTimeoutRef.current)
    }

    promoValidateTimeoutRef.current = setTimeout(() => {
      void validatePromoCodeDebounced(code)
    }, 500)
  }

  async function validatePromoCodeDebounced(code: string): Promise<void> {
    if (!code.trim()) return

    setPromoValidating(true)

    try {
      const result = await validatePromoCode(code)

      if (result.valid) {
        setPromoValidation({
          valid: true,
          invalid: false,
          bonusAmount: result.bonus_amount || 0,
          message: ''
        })
      } else {
        setPromoValidation({
          valid: false,
          invalid: true,
          bonusAmount: null,
          // 根据错误码显示对应的翻译
          message: getPromoErrorMessage(result.error_code)
        })
      }
    } catch (error) {
      console.error('Failed to validate promo code:', error)
      setPromoValidation({ valid: false, invalid: true, bonusAmount: null, message: t('auth.promoCodeInvalid') })
    } finally {
      setPromoValidating(false)
    }
  }

  function getPromoErrorMessage(errorCode?: string): string {
    switch (errorCode) {
      case 'PROMO_CODE_NOT_FOUND':
        return t('auth.promoCodeNotFound')
      case 'PROMO_CODE_EXPIRED':
        return t('auth.promoCodeExpired')
      case 'PROMO_CODE_DISABLED':
        return t('auth.promoCodeDisabled')
      case 'PROMO_CODE_MAX_USED':
        return t('auth.promoCodeMaxUsed')
      case 'PROMO_CODE_ALREADY_USED':
        return t('auth.promoCodeAlreadyUsed')
      default:
        return t('auth.promoCodeInvalid')
    }
  }

  // ==================== Invitation Code Validation ====================

  function handleInvitationCodeInput(): void {
    const code = invitationCode.trim()

    // Clear previous validation
    setInvitationValidation({ valid: false, invalid: false, message: '' })
    setErrors((previous) => ({ ...previous, invitation_code: '' }))

    if (!code) {
      return
    }

    // Debounce validation
    if (invitationValidateTimeoutRef.current) {
      clearTimeout(invitationValidateTimeoutRef.current)
    }

    invitationValidateTimeoutRef.current = setTimeout(() => {
      void validateInvitationCodeDebounced(code)
    }, 500)
  }

  async function validateInvitationCodeDebounced(code: string): Promise<boolean> {
    setInvitationValidating(true)

    let valid = false
    try {
      const result = await validateInvitationCode(code)

      if (result.valid) {
        setInvitationValidation({ valid: true, invalid: false, message: '' })
        valid = true
      } else {
        setInvitationValidation({
          valid: false,
          invalid: true,
          message: getInvitationErrorMessage(result.error_code)
        })
      }
    } catch {
      setInvitationValidation({ valid: false, invalid: true, message: t('auth.invitationCodeInvalid') })
    } finally {
      setInvitationValidating(false)
    }
    return valid
  }

  function getInvitationErrorMessage(errorCode?: string): string {
    switch (errorCode) {
      case 'INVITATION_CODE_NOT_FOUND':
        return t('auth.invitationCodeInvalid')
      case 'INVITATION_CODE_INVALID':
        return t('auth.invitationCodeInvalid')
      case 'INVITATION_CODE_USED':
        return t('auth.invitationCodeInvalid')
      case 'INVITATION_CODE_DISABLED':
        return t('auth.invitationCodeInvalid')
      default:
        return t('auth.invitationCodeInvalid')
    }
  }

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

  async function handleOAuthStart(request: OAuthLoginStart): Promise<void> {
    if (registrationActionDisabled) return

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

  // ==================== Validation ====================

  function validateEmail(value: string): boolean {
    const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
    return emailRegex.test(value)
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

  function validateForm(): boolean {
    setErrors({ email: '', password: '', confirmPassword: '', turnstile: '', invitation_code: '' })

    let isValid = true

    if (agreementGateActive) {
      appStore.showWarning(t('legal.loginAgreementPrompt.registerRequiredWarning'))
      if (agreement.mode !== 'checkbox') {
        agreement.openModal()
      }
      return false
    }

    // Email validation
    if (!email.trim()) {
      setErrors((previous) => ({ ...previous, email: t('auth.emailRequired') }))
      isValid = false
    } else if (!validateEmail(email)) {
      setErrors((previous) => ({ ...previous, email: t('auth.invalidEmail') }))
      isValid = false
    } else if (
      !emailDomainQuotaEnabled &&
      !isRegistrationEmailSuffixAllowed(email, registrationEmailSuffixWhitelist)
    ) {
      // 域名限量注册关闭时保持严格白名单预检；开启时交给后端按域名额度判定
      setErrors((previous) => ({ ...previous, email: buildEmailSuffixNotAllowedMessage() }))
      isValid = false
    }

    // Password validation
    if (!password) {
      setErrors((previous) => ({ ...previous, password: t('auth.passwordRequired') }))
      isValid = false
    } else if (password.length < 6) {
      setErrors((previous) => ({ ...previous, password: t('auth.passwordMinLength') }))
      isValid = false
    }

    // Confirm password validation
    if (!confirmPassword) {
      setErrors((previous) => ({ ...previous, confirmPassword: t('auth.confirmPasswordRequired') }))
      isValid = false
    } else if (password !== confirmPassword) {
      setErrors((previous) => ({ ...previous, confirmPassword: t('auth.passwordsDoNotMatch') }))
      isValid = false
    }

    // Invitation code validation (required when enabled)
    if (invitationCodeEnabled) {
      if (!invitationCode.trim()) {
        setErrors((previous) => ({ ...previous, invitation_code: t('auth.invitationCodeRequired') }))
        isValid = false
      }
    }

    // Turnstile validation
    if (turnstileEnabled && !turnstileToken) {
      setErrors((previous) => ({ ...previous, turnstile: t('auth.completeVerification') }))
      isValid = false
    }

    return isValid
  }

  // ==================== Form Handlers ====================

  async function handleRegister(event: React.FormEvent): Promise<void> {
    event.preventDefault()

    if (!validateForm()) {
      return
    }

    // Check promo code validation status
    if (promoCode.trim()) {
      // If promo code is being validated, wait
      if (promoValidating) {
        appStore.showInfo(t('auth.promoCodeValidating'))
        return
      }
      // If promo code is invalid, block submission
      if (promoValidation.invalid) {
        appStore.showError(t('auth.promoCodeInvalidCannotRegister'))
        return
      }
    }

    // Check invitation code validation status (if enabled and code provided)
    if (invitationCodeEnabled) {
      // If still validating, wait
      if (invitationValidating) {
        appStore.showInfo(t('auth.invitationCodeValidating'))
        return
      }
      // If invitation code is invalid, block submission
      if (invitationValidation.invalid) {
        appStore.showError(t('auth.invitationCodeInvalidCannotRegister'))
        return
      }
      // If invitation code is required but not validated yet
      if (invitationCode.trim() && !invitationValidation.valid) {
        appStore.showInfo(t('auth.invitationCodeValidating'))
        // Trigger validation
        const valid = await validateInvitationCodeDebounced(invitationCode.trim())
        if (!valid) {
          appStore.showError(t('auth.invitationCodeInvalidCannotRegister'))
          return
        }
      }
    }

    if (!(await acquireActionProof())) {
      return
    }

    setIsLoading(true)

    try {
      const resolvedAffCode = affCode.trim() || loadAffiliateReferralCode()
      if (resolvedAffCode) {
        setAffCode(resolvedAffCode)
      }

      const captchaFields = {
        turnstile_token: turnstileEnabled || aliyunCaptchaEnabled ? turnstileToken : undefined,
        tencent_captcha_ticket: tencentCaptchaEnabled ? turnstileToken : undefined,
        tencent_captcha_randstr: tencentCaptchaEnabled ? tencentCaptchaRandstr : undefined,
        promo_code: promoCode || undefined,
        invitation_code: invitationCode || undefined,
        ...(resolvedAffCode ? { aff_code: resolvedAffCode } : {})
      }

      // If email verification is enabled, redirect to verification page
      if (emailVerifyEnabled) {
        // Store registration data in sessionStorage
        sessionStorage.setItem(
          'register_data',
          JSON.stringify({
            email,
            password,
            ...captchaFields
          })
        )

        // Navigate to email verification page
        await navigate('/email-verify')
        return
      }

      // Otherwise, directly register
      const payload: RegisterRequest = { email, password, ...captchaFields }
      await authStore.register(payload)
      clearAffiliateReferralCode()

      // Show success toast
      appStore.showSuccess(t('auth.accountCreatedSuccess', { siteName }))

      // Redirect to dashboard
      await navigate('/dashboard')
    } catch (error: unknown) {
      // Handle registration error
      const message = buildRegistrationErrorMessage(error, t('auth.registrationFailed'))
      appStore.showError(message)
    } finally {
      if (captchaEnabled) {
        resetCaptchaProof()
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

  return (
    <AuthLayout
      footer={
        <>
        <p className="surface-muted">
          {t('auth.alreadyHaveAccount')}
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
          <h2 className="text-2xl font-bold text-gray-900 dark:text-white">{t('auth.createAccount')}</h2>
          <p className="mt-2 text-sm surface-muted">{t('auth.signUpToStart', { siteName })}</p>
        </div>

        {/* Registration Disabled Message */}
        {showRegistrationDisabled && (
          <div className="rounded-xl border border-amber-200 bg-amber-50/[0.15] p-4 dark:border-amber-800/50 dark:bg-amber-900/20">
            <div className="flex items-start gap-3">
              <div className="flex-shrink-0">
                <Icon name="exclamationCircle" size="md" className="text-amber-500" />
              </div>
              <p className="text-sm text-amber-700 dark:text-amber-400">{t('auth.registrationDisabled')}</p>
            </div>
          </div>
        )}

        {/* Registration Form */}
        {!showRegistrationDisabled && (
          <form onSubmit={handleRegister} className="space-y-5">
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
                  disabled={registrationActionDisabled}
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
                  autoComplete="new-password"
                  disabled={registrationActionDisabled}
                  className={cn('input pl-11 pr-11', errors.password && 'input-error')}
                  placeholder={t('auth.createPasswordPlaceholder')}
                />
                <button
                  type="button"
                  disabled={registrationActionDisabled}
                  onClick={() => setShowPassword((value) => !value)}
                  className="absolute inset-y-0 right-0 flex items-center pr-3.5 transition-colors hover:text-gray-600 dark:hover:text-dark-300 surface-control surface-muted"
                >
                  <Icon name={showPassword ? 'eyeOff' : 'eye'} size="md" />
                </button>
              </div>
              <p className="input-hint">{t('auth.passwordHint')}</p>
            </div>

            {/* Confirm Password Input */}
            <div>
              <label htmlFor="confirmPassword" className="input-label">
                {t('auth.confirmPassword')}
              </label>
              <div className="relative">
                <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
                  <Icon name="lock" size="md" className="surface-muted" />
                </div>
                <input
                  id="confirmPassword"
                  value={confirmPassword}
                  onChange={(event) => setConfirmPassword(event.target.value)}
                  type={showConfirmPassword ? 'text' : 'password'}
                  required
                  autoComplete="new-password"
                  disabled={registrationActionDisabled}
                  className={cn('input pl-11 pr-11', errors.confirmPassword && 'input-error')}
                  placeholder={t('auth.confirmPasswordPlaceholder')}
                />
                <button
                  type="button"
                  disabled={registrationActionDisabled}
                  onClick={() => setShowConfirmPassword((value) => !value)}
                  className="absolute inset-y-0 right-0 flex items-center pr-3.5 transition-colors hover:text-gray-600 dark:hover:text-dark-300 surface-control surface-muted"
                >
                  <Icon name={showConfirmPassword ? 'eyeOff' : 'eye'} size="md" />
                </button>
              </div>
            </div>

            {/* Invitation Code Input (Required when enabled) */}
            {invitationCodeEnabled && (
              <div>
                <label htmlFor="invitation_code" className="input-label">
                  {t('auth.invitationCodeLabel')}
                </label>
                <div className="relative">
                  <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
                    <Icon
                      name="key"
                      size="md"
                      className={invitationValidation.valid ? 'text-green-500' : 'surface-muted'}
                    />
                  </div>
                  <input
                    id="invitation_code"
                    value={invitationCode}
                    onChange={(event) => {
                      setInvitationCode(event.target.value)
                      handleInvitationCodeInput()
                    }}
                    type="text"
                    disabled={registrationActionDisabled}
                    className={cn(
                      'input pl-11 pr-10',
                      invitationValidation.valid && 'border-green-500 focus:border-green-500 focus:ring-green-500',
                      (invitationValidation.invalid || errors.invitation_code) &&
                        'border-red-500 focus:border-red-500 focus:ring-red-500'
                    )}
                    placeholder={t('auth.invitationCodePlaceholder')}
                  />
                  {/* Validation indicator */}
                  {invitationValidating && (
                    <div className="absolute inset-y-0 right-0 flex items-center pr-3.5">
                      <svg className="h-4 w-4 animate-spin text-gray-400" fill="none" viewBox="0 0 24 24">
                        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                        <path
                          className="opacity-75"
                          fill="currentColor"
                          d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                        />
                      </svg>
                    </div>
                  )}
                  {!invitationValidating && invitationValidation.valid && (
                    <div className="absolute inset-y-0 right-0 flex items-center pr-3.5">
                      <Icon name="checkCircle" size="md" className="text-green-500" />
                    </div>
                  )}
                  {!invitationValidating &&
                    (invitationValidation.invalid || errors.invitation_code) && (
                      <div className="absolute inset-y-0 right-0 flex items-center pr-3.5">
                        <Icon name="exclamationCircle" size="md" className="text-red-500" />
                      </div>
                    )}
                </div>
                {/* Invitation code validation result */}
                {invitationValidation.valid && (
                  <div className="mt-2 flex items-center gap-2 rounded-lg bg-green-50/[0.15] px-3 py-2 dark:bg-green-900/20">
                    <Icon name="checkCircle" size="sm" className="text-green-600 dark:text-green-400" />
                    <span className="text-sm text-green-700 dark:text-green-400">
                      {t('auth.invitationCodeValid')}
                    </span>
                  </div>
                )}
              </div>
            )}

            {/* Affiliate Invitation Code Input (Optional) */}
            {!invitationCodeEnabled && affiliateEnabled && (
              <div data-testid="affiliate-invitation-field">
                <label htmlFor="affiliate_code" className="input-label">
                  {t('auth.invitationCodeLabel')}
                  <span className="ml-1 text-xs font-normal surface-muted">({t('common.optional')})</span>
                </label>
                <div className="relative">
                  <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
                    <Icon name="key" size="md" className="surface-muted" />
                  </div>
                  <input
                    id="affiliate_code"
                    value={affCode}
                    onChange={(event) => setAffCode(event.target.value)}
                    type="text"
                    disabled={registrationActionDisabled}
                    className="input pl-11"
                    placeholder={t('auth.invitationCodePlaceholder')}
                  />
                </div>
              </div>
            )}

            {/* Promo Code Input (Optional) */}
            {promoCodeEnabled && (
              <div>
                <label htmlFor="promo_code" className="input-label">
                  {t('auth.promoCodeLabel')}
                  <span className="ml-1 text-xs font-normal surface-muted">({t('common.optional')})</span>
                </label>
                <div className="relative">
                  <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
                    <Icon
                      name="gift"
                      size="md"
                      className={promoValidation.valid ? 'text-green-500' : 'surface-muted'}
                    />
                  </div>
                  <input
                    id="promo_code"
                    value={promoCode}
                    onChange={(event) => {
                      setPromoCode(event.target.value)
                      handlePromoCodeInput()
                    }}
                    type="text"
                    disabled={registrationActionDisabled}
                    className={cn(
                      'input pl-11 pr-10',
                      promoValidation.valid && 'border-green-500 focus:border-green-500 focus:ring-green-500',
                      promoValidation.invalid && 'border-red-500 focus:border-red-500 focus:ring-red-500'
                    )}
                    placeholder={t('auth.promoCodePlaceholder')}
                  />
                  {/* Validation indicator */}
                  {promoValidating && (
                    <div className="absolute inset-y-0 right-0 flex items-center pr-3.5">
                      <svg className="h-4 w-4 animate-spin text-gray-400" fill="none" viewBox="0 0 24 24">
                        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                        <path
                          className="opacity-75"
                          fill="currentColor"
                          d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                        />
                      </svg>
                    </div>
                  )}
                  {!promoValidating && promoValidation.valid && (
                    <div className="absolute inset-y-0 right-0 flex items-center pr-3.5">
                      <Icon name="checkCircle" size="md" className="text-green-500" />
                    </div>
                  )}
                  {!promoValidating && promoValidation.invalid && (
                    <div className="absolute inset-y-0 right-0 flex items-center pr-3.5">
                      <Icon name="exclamationCircle" size="md" className="text-red-500" />
                    </div>
                  )}
                </div>
                {/* Promo code validation result */}
                {promoValidation.valid && (
                  <div className="mt-2 flex items-center gap-2 rounded-lg bg-green-50/[0.15] px-3 py-2 dark:bg-green-900/20">
                    <Icon name="gift" size="sm" className="text-green-600 dark:text-green-400" />
                    <span className="text-sm text-green-700 dark:text-green-400">
                      {t('auth.promoCodeValid', { amount: promoValidation.bonusAmount?.toFixed(2) })}
                    </span>
                  </div>
                )}
              </div>
            )}

            {/* Turnstile Widget */}
            {captchaEnabled && (
              <div data-testid="registration-turnstile">
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

            <LoginAgreementPrompt
              accepted={agreement.accepted}
              documents={agreement.documents}
              mode={agreement.mode}
              updatedAt={agreement.updatedAt}
              visible={agreement.modalVisible}
              onAccept={agreement.accept}
              onReject={() => agreement.reject('legal.loginAgreementPrompt.registerRejectedWarning')}
              onOpen={agreement.openModal}
            />

            {/* Submit Button */}
            <button
              type="submit"
              disabled={registrationActionDisabled || (turnstileEnabled && !turnstileToken)}
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
                <Icon name="userPlus" size="md" className="mr-2" />
              )}
              {isLoading
                ? t('auth.processing')
                : emailVerifyEnabled
                  ? t('auth.continue')
                  : t('auth.createAccount')}
            </button>
          </form>
        )}

        {showOAuthLogin && (
          <div className="space-y-3 pt-1">
            <div className="flex items-center gap-3">
              <div className="h-px flex-1 surface-inset" />
              <span className="text-xs surface-muted">{t('auth.oauthOrContinue')}</span>
              <div className="h-px flex-1 surface-inset" />
            </div>

            <EmailOAuthButtons
              disabled={registrationActionDisabled}
              affCode={affCode}
              promoCode={promoCode}
              githubEnabled={githubOAuthEnabled}
              googleEnabled={googleOAuthEnabled}
              showDivider={false}
              onStart={(request) => void handleOAuthStart(request)}
            />

            {linuxdoOAuthEnabled && (
              <LinuxDoOAuthSection
                disabled={registrationActionDisabled}
                affCode={affCode}
                promoCode={promoCode}
                showDivider={false}
                onStart={(request) => void handleOAuthStart(request)}
              />
            )}
            {wechatOAuthEnabled && (
              <WechatOAuthSection
                disabled={registrationActionDisabled}
                affCode={affCode}
                showDivider={false}
                onStart={(request) => void handleOAuthStart(request)}
              />
            )}
            {oidcOAuthEnabled && (
              <OidcOAuthSection
                disabled={registrationActionDisabled}
                providerName={oidcOAuthProviderName}
                affCode={affCode}
                showDivider={false}
                onStart={(request) => void handleOAuthStart(request)}
              />
            )}
          </div>
        )}
      </div>

      {/* Footer */}
    </AuthLayout>
  )
}
