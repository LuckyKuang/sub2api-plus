import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { PendingOAuthCreateAccountForm, type PendingOAuthCreateAccountPayload } from '@/components/auth/PendingOAuthCreateAccountForm'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { apiClient } from '@/api/client'
import {
  completeWeChatOAuthRegistration,
  exchangePendingOAuthCompletion,
  getAuthToken,
  getOAuthCompletionKind,
  hasExplicitWeChatOAuthCapabilities,
  isOAuthLoginCompletion,
  login2FA,
  persistOAuthTokenContext,
  prepareOAuthBindAccessTokenCookie,
  resolveWeChatOAuthStartStrict,
  type OAuthAdoptionDecision,
  type PendingOAuthExchangeResponse
} from '@/api/auth'
import {
  clearAllAffiliateReferralCodes,
  loadOAuthAffiliateCode,
  oauthAffiliatePayload
} from '@/utils/oauthAffiliate'
import {
  WECHAT_STATE_ALIASES,
  getRequestErrorMessage,
  isCreateAccountRecoveryError,
  normalizedPendingState,
  parseFragmentParams,
  readLegacyFragmentLogin,
  sanitizeRedirectPath,
  serializeAdoptionDecision,
  type PendingOAuthCompletionFields
} from '@/components/auth/pendingOAuthShared'
import '@/views/authViews.css'

type PendingWeChatCompletion = PendingOAuthExchangeResponse & PendingOAuthCompletionFields

/** WeChat keeps its own 'choice' action name instead of the shared chooser alias. */
type WeChatPendingAccountAction = 'none' | 'choice' | 'create_account' | 'bind_login'

export default function WechatCallbackView() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const authStore = useAuthStore()
  const appStore = useAppStore()

  const providerName = t('auth.wechatProviderName')
  const bindSuccessMessage = t('profile.authBindings.bindSuccess')

  const [isProcessing, setIsProcessing] = useState(true)
  const [errorMessage, setErrorMessage] = useState('')
  const [needsInvitation, setNeedsInvitation] = useState(false)
  const [invitationCode, setInvitationCode] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [invitationError, setInvitationError] = useState('')
  const [redirectTo, setRedirectTo] = useState('/dashboard')
  const [adoptionRequired, setAdoptionRequired] = useState(false)
  const [suggestedDisplayName, setSuggestedDisplayName] = useState('')
  const [suggestedAvatarUrl, setSuggestedAvatarUrl] = useState('')
  const [adoptDisplayName, setAdoptDisplayName] = useState(true)
  const [adoptAvatar, setAdoptAvatar] = useState(true)
  const [needsAdoptionConfirmation, setNeedsAdoptionConfirmation] = useState(false)
  const [pendingAccountAction, setPendingAccountAction] = useState<WeChatPendingAccountAction>('none')
  const [needsChooser, setNeedsChooser] = useState(false)
  const [pendingAccountEmail, setPendingAccountEmail] = useState('')
  const [existingAccountEmail, setExistingAccountEmail] = useState('')
  const [bindLoginEmail, setBindLoginEmail] = useState('')
  const [bindLoginPassword, setBindLoginPassword] = useState('')
  const legacyPendingOAuthTokenRef = useRef('')
  const [accountActionError, setAccountActionError] = useState('')
  const [needsTotpChallenge, setNeedsTotpChallenge] = useState(false)
  const [totpTempToken, setTotpTempToken] = useState('')
  const [totpCode, setTotpCode] = useState('')
  const [totpError, setTotpError] = useState('')
  const [totpUserEmailMasked, setTotpUserEmailMasked] = useState('')

  const needsCreateAccount = pendingAccountAction === 'create_account'
  const needsBindLogin = pendingAccountAction === 'bind_login'
  const showBackToChooser = pendingAccountAction === 'create_account' || pendingAccountAction === 'bind_login'
  const hasCurrentAuthToken = Boolean(getAuthToken())

  useEffect(() => {
    if (invitationError) appStore.showError(invitationError)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [invitationError])

  useEffect(() => {
    if (accountActionError) appStore.showError(accountActionError)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [accountActionError])

  useEffect(() => {
    if (totpError) appStore.showError(totpError)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [totpError])

  useEffect(() => {
    if (errorMessage) appStore.showError(errorMessage)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [errorMessage])

  function persistPendingAuthSession(redirect?: string) {
    authStore.setPendingAuthSession({
      token: '',
      token_field: 'pending_oauth_token',
      provider: 'wechat',
      redirect: sanitizeRedirectPath(redirect || redirectTo)
    })
  }

  function clearPendingAuthSession() {
    authStore.clearPendingAuthSession()
  }

  async function ensurePublicSettingsLoaded(): Promise<void> {
    if (hasExplicitWeChatOAuthCapabilities(appStore.cachedPublicSettings)) {
      return
    }

    if (appStore.publicSettingsLoaded) {
      return
    }

    await appStore.fetchPublicSettings()
  }

  function resolveConfiguredWeChatOAuthMode(): 'open' | 'mp' | null {
    if (!hasExplicitWeChatOAuthCapabilities(appStore.cachedPublicSettings)) {
      return null
    }

    return resolveWeChatOAuthStartStrict(appStore.cachedPublicSettings).mode
  }

  function resolveWeChatOAuthUnavailableMessage(): string {
    const resolved = resolveWeChatOAuthStartStrict(appStore.cachedPublicSettings)

    switch (resolved.unavailableReason) {
      case 'capability_unknown':
        return t('auth.oauthFlow.wechatAvailabilityUnknown')
      case 'external_browser_required':
        return t('auth.oauthFlow.wechatSystemBrowserOnly')
      case 'wechat_browser_required':
        return t('auth.oauthFlow.wechatBrowserOnly')
      case 'native_app_required':
        return 'This WeChat sign-in flow is only available from the native mobile app.'
      case 'not_configured':
        return t('auth.oauthFlow.wechatNotConfigured')
      default:
        return t('auth.loginFailed')
    }
  }

  function resolveRuntimeWeChatOAuthMode(): 'open' | 'mp' {
    if (typeof navigator === 'undefined') {
      return 'open'
    }
    return /MicroMessenger/i.test(navigator.userAgent) ? 'mp' : 'open'
  }

  function normalizeWeChatOAuthMode(value: unknown): 'open' | 'mp' | null {
    return value === 'open' || value === 'mp' ? value : null
  }

  function resolveRequestedWeChatOAuthMode(): 'open' | 'mp' | null {
    const configuredMode = resolveConfiguredWeChatOAuthMode()
    if (configuredMode) {
      return configuredMode
    }

    const queryMode = normalizeWeChatOAuthMode(searchParams.get('mode'))
    if (queryMode) {
      return queryMode
    }

    return resolveRuntimeWeChatOAuthMode()
  }

  function resolveRedirectTarget(): string {
    return sanitizeRedirectPath(searchParams.get('redirect') || redirectTo || '/dashboard')
  }

  function resolveWeChatStartURL(intent: 'bind_current_user' | 'adopt_existing_user_by_email'): string | null {
    const apiBase = (import.meta.env.VITE_API_BASE_URL as string | undefined) || '/api/v1'
    const normalized = apiBase.replace(/\/$/, '')
    const mode = resolveRequestedWeChatOAuthMode()
    if (!mode) {
      return null
    }
    const params = new URLSearchParams({
      mode,
      redirect: resolveRedirectTarget(),
      intent
    })

    return `${normalized}/auth/oauth/wechat/bind/start?${params.toString()}`
  }

  function buildExistingAccountResumePath(emailOverride?: string): string | null {
    const mode = resolveRequestedWeChatOAuthMode()
    if (!mode) {
      return null
    }

    const params = new URLSearchParams({
      wechat_bind_existing: '1',
      redirect: resolveRedirectTarget(),
      mode
    })

    const email = (emailOverride ?? existingAccountEmail).trim()
    if (email) {
      params.set('email', email)
    }

    return `/auth/wechat/callback?${params.toString()}`
  }

  const currentAdoptionDecision = useCallback(
    (): OAuthAdoptionDecision => ({ adoptDisplayName, adoptAvatar }),
    [adoptDisplayName, adoptAvatar]
  )

  function resolveResumeEmail(): string {
    const email = searchParams.get('email')
    return typeof email === 'string' ? email.trim() : ''
  }

  function applyAdoptionSuggestionState(completion: PendingOAuthExchangeResponse) {
    setAdoptionRequired(completion.adoption_required === true)
    setSuggestedDisplayName(completion.suggested_display_name || '')
    setSuggestedAvatarUrl(completion.suggested_avatar_url || '')

    if (!completion.suggested_display_name) {
      setAdoptDisplayName(false)
    }
    if (!completion.suggested_avatar_url) {
      setAdoptAvatar(false)
    }
  }

  function extractWeChatPendingAccountEmail(completion: PendingWeChatCompletion): string {
    return (
      completion.pending_email ||
      completion.existing_account_email ||
      completion.resolved_email ||
      completion.email ||
      resolveResumeEmail() ||
      ''
    ).trim()
  }

  function applyPendingAccountAction(completion: PendingWeChatCompletion): WeChatPendingAccountAction {
    const raw = normalizedPendingState(
      completion.step || completion.status || completion.state || completion.error || completion.intent
    )
    let action: WeChatPendingAccountAction = 'none'
    if (
      raw === 'choice' ||
      raw === 'choose_account_action_required' ||
      raw === 'choose_account_action' ||
      raw === 'choose_account' ||
      raw === 'choose'
    ) {
      action = 'choice'
    } else if (raw === 'email_required' || raw === 'create_account_required' || raw === 'create_account') {
      action = 'create_account'
    } else if (WECHAT_STATE_ALIASES.bindLoginStates.includes(raw)) {
      action = 'bind_login'
    }

    setPendingAccountAction(action)
    setAccountActionError('')
    setNeedsChooser(false)
    setNeedsTotpChallenge(false)
    setTotpTempToken('')
    setTotpCode('')
    setTotpError('')
    setTotpUserEmailMasked('')

    const email = extractWeChatPendingAccountEmail(completion)
    setPendingAccountEmail(email)
    if (action === 'create_account') {
      return action
    }

    if (action === 'bind_login') {
      setBindLoginEmail(email)
      setBindLoginPassword('')
      return action
    }

    if (action === 'choice') {
      setNeedsChooser(true)
      setBindLoginPassword('')
    }

    return action
  }

  function applyTotpChallenge(completion: PendingWeChatCompletion): boolean {
    if (completion.requires_2fa !== true || !completion.temp_token) {
      return false
    }

    setPendingAccountAction('none')
    setNeedsChooser(false)
    setNeedsInvitation(false)
    setNeedsAdoptionConfirmation(false)
    setNeedsTotpChallenge(true)
    setTotpTempToken(completion.temp_token)
    setTotpCode('')
    setTotpError('')
    setTotpUserEmailMasked(completion.user_email_masked || '')
    setIsProcessing(false)
    return true
  }

  function switchToBindLoginMode(nextEmail?: string) {
    setPendingAccountAction('bind_login')
    setNeedsChooser(false)
    setBindLoginEmail(bindLoginEmail.trim() || nextEmail?.trim() || pendingAccountEmail.trim())
    setBindLoginPassword('')
    setAccountActionError('')
  }

  function switchToCreateAccountMode() {
    setPendingAccountAction('create_account')
    setNeedsChooser(false)
    setPendingAccountEmail(pendingAccountEmail.trim() || bindLoginEmail.trim())
    setAccountActionError('')
  }

  async function finalizeCompletion(completion: PendingOAuthExchangeResponse, redirect: string) {
    if (getOAuthCompletionKind(completion) === 'bind') {
      const bindRedirect = sanitizeRedirectPath(completion.redirect || '/profile')
      clearPendingAuthSession()
      clearAllAffiliateReferralCodes()
      appStore.showSuccess(bindSuccessMessage)
      await navigate(bindRedirect, { replace: true })
      return
    }

    if (!isOAuthLoginCompletion(completion)) {
      throw new Error(t('auth.oidc.callbackMissingToken'))
    }

    persistOAuthTokenContext(completion)
    await authStore.setToken(completion.access_token)
    clearAllAffiliateReferralCodes()
    appStore.showSuccess(t('auth.loginSuccess'))
    await navigate(redirect, { replace: true })
  }

  async function finalizePendingAccountResponse(completion: PendingWeChatCompletion) {
    applyAdoptionSuggestionState(completion)
    const redirect = sanitizeRedirectPath(completion.redirect || redirectTo)

    if (completion.error === 'invitation_required') {
      setPendingAccountAction('none')
      setNeedsInvitation(true)
      setNeedsAdoptionConfirmation(false)
      setIsProcessing(false)
      persistPendingAuthSession(redirect)
      return
    }

    if (applyTotpChallenge(completion)) {
      persistPendingAuthSession(redirect)
      return
    }

    const action = applyPendingAccountAction(completion)
    if (action !== 'none') {
      setNeedsInvitation(false)
      setNeedsAdoptionConfirmation(false)
      setIsProcessing(false)
      persistPendingAuthSession(redirect)
      return
    }

    if (completion.auth_result === 'pending_session') {
      setNeedsInvitation(false)
      setNeedsAdoptionConfirmation(false)
      setIsProcessing(false)
      persistPendingAuthSession(redirect)
      return
    }

    await finalizeCompletion(completion, redirect)
  }

  async function handleSubmitInvitation() {
    setInvitationError('')
    if (!invitationCode.trim()) return

    setIsSubmitting(true)
    try {
      const affCode = loadOAuthAffiliateCode()
      const decision = currentAdoptionDecision()
      const completion: PendingWeChatCompletion = legacyPendingOAuthTokenRef.current
        ? (
            await apiClient.post<PendingWeChatCompletion>('/auth/oauth/wechat/complete-registration', {
              pending_oauth_token: legacyPendingOAuthTokenRef.current,
              invitation_code: invitationCode.trim(),
              ...oauthAffiliatePayload(affCode),
              ...serializeAdoptionDecision(decision)
            })
          ).data
        : affCode
          ? await completeWeChatOAuthRegistration(invitationCode.trim(), decision, affCode)
          : await completeWeChatOAuthRegistration(invitationCode.trim(), decision)
      await finalizePendingAccountResponse(completion)
    } catch (error: unknown) {
      const err = error as { message?: string; response?: { data?: { message?: string } } }
      setInvitationError(
        err.response?.data?.message || err.message || t('auth.oidc.completeRegistrationFailed')
      )
    } finally {
      setIsSubmitting(false)
    }
  }

  async function handleContinueLogin() {
    setIsSubmitting(true)
    try {
      const completion = (await exchangePendingOAuthCompletion(
        currentAdoptionDecision()
      )) as PendingWeChatCompletion
      await finalizePendingAccountResponse(completion)
    } catch (error: unknown) {
      setErrorMessage(getRequestErrorMessage(error, t('auth.loginFailed')))
      setNeedsAdoptionConfirmation(false)
    } finally {
      setIsSubmitting(false)
    }
  }

  async function handleCreateAccount(payload: PendingOAuthCreateAccountPayload) {
    setAccountActionError('')
    if (!payload.email || !payload.password) return

    setIsSubmitting(true)
    try {
      const { data } = await apiClient.post<PendingWeChatCompletion>('/auth/oauth/pending/create-account', {
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
        invitation_code: payload.invitationCode || undefined,
        ...oauthAffiliatePayload(loadOAuthAffiliateCode()),
        ...serializeAdoptionDecision(currentAdoptionDecision())
      })
      await finalizePendingAccountResponse(data)
    } catch (error: unknown) {
      if (isCreateAccountRecoveryError(error)) {
        switchToBindLoginMode(payload.email.trim())
        return
      }
      setAccountActionError(getRequestErrorMessage(error, t('auth.loginFailed')))
    } finally {
      setIsSubmitting(false)
    }
  }

  async function handleBindLogin() {
    setAccountActionError('')
    const email = bindLoginEmail.trim()
    const password = bindLoginPassword
    if (!email || !password) return

    setIsSubmitting(true)
    try {
      const { data } = await apiClient.post<PendingWeChatCompletion>('/auth/oauth/pending/bind-login', {
        email,
        password,
        ...serializeAdoptionDecision(currentAdoptionDecision())
      })
      await finalizePendingAccountResponse(data)
    } catch (error: unknown) {
      setAccountActionError(getRequestErrorMessage(error, t('auth.loginFailed')))
    } finally {
      setIsSubmitting(false)
    }
  }

  async function handleSubmitTotpChallenge() {
    setTotpError('')
    const code = totpCode.trim()
    if (!totpTempToken || code.length !== 6) return

    setIsSubmitting(true)
    try {
      const completion = await login2FA({ temp_token: totpTempToken, totp_code: code })
      persistOAuthTokenContext(completion)
      await authStore.setToken(completion.access_token)
      clearAllAffiliateReferralCodes()
      appStore.showSuccess(t('auth.loginSuccess'))
      await navigate(redirectTo, { replace: true })
    } catch (error: unknown) {
      setTotpError(getRequestErrorMessage(error, t('auth.loginFailed')))
    } finally {
      setIsSubmitting(false)
    }
  }

  async function handleBindCurrentAccount() {
    const unavailableMessage =
      resolveConfiguredWeChatOAuthMode() === null ? resolveWeChatOAuthUnavailableMessage() : ''

    const startURL = resolveWeChatStartURL('bind_current_user')
    if (!startURL) {
      setErrorMessage(unavailableMessage || resolveWeChatOAuthUnavailableMessage())
      return
    }

    try {
      await prepareOAuthBindAccessTokenCookie()
      window.location.href = startURL
    } catch (error: unknown) {
      setErrorMessage(getRequestErrorMessage(error, t('auth.loginFailed')))
    }
  }

  async function handleExistingAccountBinding() {
    if (getAuthToken()) {
      await handleBindCurrentAccount()
      return
    }

    const resumePath = buildExistingAccountResumePath()
    if (!resumePath) {
      setErrorMessage(resolveWeChatOAuthUnavailableMessage())
      return
    }

    const params = new URLSearchParams({ redirect: resumePath })
    const email = existingAccountEmail.trim()
    if (email) {
      params.set('email', email)
    }
    await navigate(`/login?${params.toString()}`, { replace: true })
  }

  useEffect(() => {
    let cancelled = false

    void (async () => {
      try {
        await ensurePublicSettingsLoaded()
      } catch {
        // Binding recovery requires confirmed capability flags. Use the strict guard below.
      }

      if (cancelled) return

      const resumeEmail = searchParams.get('email')
      if (typeof resumeEmail === 'string') {
        const email = resumeEmail.trim()
        setExistingAccountEmail(email)
        setBindLoginEmail(email)
        setPendingAccountEmail(email)
      }

      if (searchParams.get('wechat_bind_existing') === '1') {
        if (getAuthToken()) {
          await handleBindCurrentAccount()
          return
        }

        const resumePath = buildExistingAccountResumePath(resumeEmail ?? undefined)
        if (!resumePath) {
          setErrorMessage(resolveWeChatOAuthUnavailableMessage())
          setIsProcessing(false)
          return
        }

        const loginParams = new URLSearchParams({ redirect: resumePath })
        const email = (resumeEmail || '').trim()
        if (email) {
          loginParams.set('email', email)
        }
        await navigate(`/login?${loginParams.toString()}`, { replace: true })
        return
      }

      const params = parseFragmentParams()
      const legacyLogin = readLegacyFragmentLogin(params)
      const legacyPendingToken = params.get('pending_oauth_token')?.trim() || ''
      const error = params.get('error')
      const errorDesc = params.get('error_description') || params.get('error_message') || ''
      const redirect = sanitizeRedirectPath(
        params.get('redirect') || searchParams.get('redirect') || '/dashboard'
      )

      try {
        if (legacyLogin) {
          persistOAuthTokenContext(legacyLogin)
          await authStore.setToken(legacyLogin.access_token)
          clearAllAffiliateReferralCodes()
          appStore.showSuccess(t('auth.loginSuccess'))
          await navigate(redirect, { replace: true })
          return
        }

        if (error === 'invitation_required' && legacyPendingToken) {
          legacyPendingOAuthTokenRef.current = legacyPendingToken
          setRedirectTo(redirect)
          setNeedsInvitation(true)
          setIsProcessing(false)
          return
        }

        if (error) {
          setErrorMessage(errorDesc || error)
          setIsProcessing(false)
          return
        }

        const completion = (await exchangePendingOAuthCompletion()) as PendingWeChatCompletion
        if (cancelled) return
        const completionRedirect = sanitizeRedirectPath(
          completion.redirect || searchParams.get('redirect') || '/dashboard'
        )
        applyAdoptionSuggestionState(completion)
        setRedirectTo(completionRedirect)

        if (completion.error === 'invitation_required') {
          setNeedsInvitation(true)
          setIsProcessing(false)
          persistPendingAuthSession(completionRedirect)
          return
        }

        if (applyTotpChallenge(completion)) {
          persistPendingAuthSession(completionRedirect)
          return
        }

        const action = applyPendingAccountAction(completion)
        if (action !== 'none') {
          setIsProcessing(false)
          persistPendingAuthSession(completionRedirect)
          return
        }

        if (completion.auth_result === 'pending_session') {
          setIsProcessing(false)
          persistPendingAuthSession(completionRedirect)
          return
        }

        if (
          completion.adoption_required === true &&
          (completion.suggested_display_name || completion.suggested_avatar_url)
        ) {
          setNeedsAdoptionConfirmation(true)
          setIsProcessing(false)
          persistPendingAuthSession(completionRedirect)
          return
        }

        await finalizeCompletion(completion, completionRedirect)
      } catch (error: unknown) {
        clearPendingAuthSession()
        setErrorMessage(getRequestErrorMessage(error, t('auth.loginFailed')))
        setIsProcessing(false)
      }
    })()

    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const showBody =
    needsInvitation ||
    needsChooser ||
    needsAdoptionConfirmation ||
    needsCreateAccount ||
    needsBindLogin ||
    needsTotpChallenge

  return (
    <>
      <div className="space-y-6">
        <div className="text-center">
          <h2 className="text-2xl font-bold text-gray-900 dark:text-white">
            {t('auth.oidc.callbackTitle', { providerName })}
          </h2>
          <p className="mt-2 text-sm surface-muted">
            {isProcessing ? t('auth.oidc.callbackProcessing', { providerName }) : t('auth.oidc.callbackHint')}
          </p>
        </div>

        {showBody && (
          <div className="space-y-4">
            {adoptionRequired && (suggestedDisplayName || suggestedAvatarUrl) && (
              <div className="rounded-xl border border-gray-200 surface-inset p-4 dark:border-dark-600">
                <div className="space-y-3">
                  <div className="space-y-1">
                    <p className="text-sm font-medium text-gray-900 dark:text-white">
                      {t('auth.oauthFlow.profileDetailsTitle', { providerName })}
                    </p>
                    <p className="text-xs surface-muted">
                      {t('auth.oauthFlow.profileDetailsDescription', { providerName })}
                    </p>
                  </div>

                  {suggestedDisplayName && (
                    <label className="flex items-start gap-3 rounded-lg border border-gray-200 surface-inset p-3 text-sm dark:border-dark-600">
                      <input
                        type="checkbox"
                        className="mt-1 h-4 w-4"
                        checked={adoptDisplayName}
                        onChange={(event) => setAdoptDisplayName(event.target.checked)}
                      />
                      <span className="space-y-1">
                        <span className="block font-medium text-gray-900 dark:text-white">
                          {t('auth.oauthFlow.useDisplayName')}
                        </span>
                        <span className="block surface-muted">{suggestedDisplayName}</span>
                      </span>
                    </label>
                  )}

                  {suggestedAvatarUrl && (
                    <label className="flex items-start gap-3 rounded-lg border border-gray-200 surface-inset p-3 text-sm dark:border-dark-600">
                      <input
                        type="checkbox"
                        className="mt-1 h-4 w-4"
                        checked={adoptAvatar}
                        onChange={(event) => setAdoptAvatar(event.target.checked)}
                      />
                      <img
                        src={suggestedAvatarUrl}
                        alt={t('auth.oauthFlow.avatarAlt', { providerName })}
                        className="h-10 w-10 rounded-full border border-gray-200 object-cover dark:border-dark-600"
                      />
                      <span className="space-y-1">
                        <span className="block font-medium text-gray-900 dark:text-white">
                          {t('auth.oauthFlow.useAvatar')}
                        </span>
                        <span className="block break-all surface-muted">{suggestedAvatarUrl}</span>
                      </span>
                    </label>
                  )}
                </div>
              </div>
            )}

            {needsInvitation && (
              <>
                <p className="text-sm surface-muted">{t('auth.oidc.invitationRequired', { providerName })}</p>
                <div>
                  <input
                    value={invitationCode}
                    type="text"
                    className="input w-full"
                    placeholder={t('auth.invitationCodePlaceholder')}
                    disabled={isSubmitting}
                    onChange={(event) => setInvitationCode(event.target.value)}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter') void handleSubmitInvitation()
                    }}
                  />
                </div>
                <button
                  className="btn btn-primary w-full"
                  disabled={isSubmitting || !invitationCode.trim()}
                  onClick={() => void handleSubmitInvitation()}
                >
                  {isSubmitting ? t('auth.oidc.completing') : t('auth.oidc.completeRegistration')}
                </button>

                <div className="rounded-xl border border-gray-200 surface-inset p-4 dark:border-dark-600">
                  <div className="space-y-3">
                    <div className="space-y-1">
                      <p className="text-sm font-medium text-gray-900 dark:text-white">
                        {t('auth.alreadyHaveAccount')}
                      </p>
                      <p className="text-xs surface-muted">
                        {hasCurrentAuthToken
                          ? t('auth.oauthFlow.bindCurrentAccountDescription', { providerName })
                          : t('auth.oauthFlow.signInThenBindDescription', { providerName })}
                      </p>
                    </div>

                    {!hasCurrentAuthToken && (
                      <input
                        value={existingAccountEmail}
                        data-testid="existing-account-email"
                        type="email"
                        className="input w-full"
                        placeholder={t('auth.emailPlaceholder')}
                        disabled={isSubmitting}
                        onChange={(event) => setExistingAccountEmail(event.target.value)}
                      />
                    )}

                    <button
                      data-testid="existing-account-submit"
                      type="button"
                      className="btn btn-secondary w-full"
                      disabled={isSubmitting}
                      onClick={() => void handleExistingAccountBinding()}
                    >
                      {hasCurrentAuthToken ? t('auth.oauthFlow.bindCurrentAccount') : t('auth.signIn')}
                    </button>
                  </div>
                </div>
              </>
            )}

            {!needsInvitation && needsChooser && (
              <div className="rounded-xl border border-gray-200 surface-inset p-4 dark:border-dark-600">
                <div className="space-y-4">
                  <div className="space-y-1">
                    <p className="text-sm font-medium text-gray-900 dark:text-white">
                      {t('auth.oauthFlow.chooseHowToContinue')}
                    </p>
                    <p className="text-xs surface-muted">{t('auth.oauthFlow.chooseAccountActionHint')}</p>
                  </div>

                  <button
                    data-testid="wechat-choice-bind-existing"
                    type="button"
                    className="btn btn-primary w-full"
                    disabled={isSubmitting}
                    onClick={() => switchToBindLoginMode()}
                  >
                    {t('auth.oauthFlow.bindExistingAccount')}
                  </button>

                  <button
                    data-testid="wechat-choice-create-account"
                    type="button"
                    className="btn btn-secondary w-full"
                    disabled={isSubmitting}
                    onClick={() => switchToCreateAccountMode()}
                  >
                    {t('auth.oauthFlow.createNewAccount')}
                  </button>
                </div>
              </div>
            )}

            {!needsInvitation && !needsChooser && needsAdoptionConfirmation && (
              <>
                <p className="text-sm surface-muted">
                  {t('auth.oauthFlow.reviewProfileBeforeContinue', { providerName })}
                </p>
                <button
                  className="btn btn-primary w-full"
                  disabled={isSubmitting}
                  onClick={() => void handleContinueLogin()}
                >
                  {isSubmitting ? t('common.processing') : t('auth.continue')}
                </button>
              </>
            )}

            {!needsInvitation && !needsChooser && !needsAdoptionConfirmation && needsCreateAccount && (
              <>
                <p className="text-sm surface-muted">{t('auth.oauthFlow.createAccountHint')}</p>
                <PendingOAuthCreateAccountForm
                  testIdPrefix="wechat"
                  initialEmail={pendingAccountEmail}
                  isSubmitting={isSubmitting}
                  errorMessage={accountActionError}
                  onSubmit={(payload) => void handleCreateAccount(payload)}
                  onSwitchToBind={switchToBindLoginMode}
                />
                {showBackToChooser && (
                  <button
                    className="btn btn-secondary w-full"
                    disabled={isSubmitting}
                    onClick={() => switchToCreateAccountMode()}
                  >
                    {t('auth.oauthFlow.createNewAccount')}
                  </button>
                )}
              </>
            )}

            {!needsInvitation &&
              !needsChooser &&
              !needsAdoptionConfirmation &&
              !needsCreateAccount &&
              needsBindLogin && (
                <>
                  <p className="text-sm surface-muted">
                    {t('auth.oauthFlow.bindSignInToExistingAccount', { providerName })}
                  </p>
                  {hasCurrentAuthToken ? (
                    <div className="rounded-xl border border-gray-200 surface-inset p-4 dark:border-dark-600">
                      <div className="space-y-3">
                        <div className="space-y-1">
                          <p className="text-sm font-medium text-gray-900 dark:text-white">
                            {t('auth.oauthFlow.bindCurrentAccountTitle')}
                          </p>
                          <p className="text-xs surface-muted">
                            {t('auth.oauthFlow.bindCurrentAccountDescription', { providerName })}
                          </p>
                        </div>

                        <button
                          data-testid="existing-account-submit"
                          type="button"
                          className="btn btn-primary w-full"
                          disabled={isSubmitting}
                          onClick={() => void handleBindCurrentAccount()}
                        >
                          {isSubmitting ? t('common.processing') : t('auth.oauthFlow.bindCurrentAccount')}
                        </button>
                      </div>
                    </div>
                  ) : (
                    <div className="space-y-3">
                      <input
                        value={bindLoginEmail}
                        data-testid="wechat-bind-login-email"
                        type="email"
                        className="input w-full"
                        placeholder={t('auth.emailPlaceholder')}
                        disabled={isSubmitting}
                        onChange={(event) => setBindLoginEmail(event.target.value)}
                        onKeyDown={(event) => {
                          if (event.key === 'Enter') void handleBindLogin()
                        }}
                      />
                      <input
                        value={bindLoginPassword}
                        data-testid="wechat-bind-login-password"
                        type="password"
                        className="input w-full"
                        placeholder={t('auth.passwordPlaceholder')}
                        disabled={isSubmitting}
                        onChange={(event) => setBindLoginPassword(event.target.value)}
                        onKeyDown={(event) => {
                          if (event.key === 'Enter') void handleBindLogin()
                        }}
                      />
                      <button
                        data-testid="wechat-bind-login-submit"
                        className="btn btn-primary w-full"
                        disabled={isSubmitting || !bindLoginEmail.trim() || !bindLoginPassword}
                        onClick={() => void handleBindLogin()}
                      >
                        {isSubmitting ? t('common.processing') : t('auth.oauthFlow.logInAndBind')}
                      </button>
                    </div>
                  )}
                  {showBackToChooser && (
                    <button
                      className="btn btn-secondary w-full"
                      disabled={isSubmitting}
                      onClick={() => switchToCreateAccountMode()}
                    >
                      {t('auth.oauthFlow.createNewAccount')}
                    </button>
                  )}
                </>
              )}

            {!needsInvitation &&
              !needsChooser &&
              !needsAdoptionConfirmation &&
              !needsCreateAccount &&
              !needsBindLogin &&
              needsTotpChallenge && (
                <>
                  <p className="text-sm surface-muted">
                    {t('auth.oauthFlow.totpHint', {
                      providerName,
                      account: totpUserEmailMasked || t('auth.oauthFlow.yourAccount')
                    })}
                  </p>
                  <div className="space-y-3">
                    <input
                      value={totpCode}
                      data-testid="wechat-bind-login-totp"
                      type="text"
                      inputMode="numeric"
                      maxLength={6}
                      className="input w-full"
                      placeholder="123456"
                      disabled={isSubmitting}
                      onChange={(event) => setTotpCode(event.target.value)}
                      onKeyDown={(event) => {
                        if (event.key === 'Enter') void handleSubmitTotpChallenge()
                      }}
                    />
                    <button
                      data-testid="wechat-bind-login-totp-submit"
                      className="btn btn-primary w-full"
                      disabled={isSubmitting || totpCode.trim().length !== 6}
                      onClick={() => void handleSubmitTotpChallenge()}
                    >
                      {isSubmitting ? t('common.processing') : t('auth.oauthFlow.verifyAndContinue')}
                    </button>
                  </div>
                </>
              )}
          </div>
        )}
      </div>
    </>
  )
}
