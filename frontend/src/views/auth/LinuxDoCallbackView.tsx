import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { PendingOAuthCallbackPanel } from '@/components/auth/PendingOAuthCallbackPanel'
import type { PendingOAuthCreateAccountPayload } from '@/components/auth/PendingOAuthCreateAccountForm'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { apiClient } from '@/api/client'
import {
  completeLinuxDoOAuthRegistration,
  exchangePendingOAuthCompletion,
  getOAuthCompletionKind,
  isOAuthLoginCompletion,
  login2FA,
  persistOAuthTokenContext,
  type OAuthAdoptionDecision,
  type PendingOAuthExchangeResponse
} from '@/api/auth'
import {
  clearAllAffiliateReferralCodes,
  loadOAuthAffiliateCode,
  oauthAffiliatePayload
} from '@/utils/oauthAffiliate'
import {
  LINUXDO_STATE_ALIASES,
  extractPendingAccountEmail,
  getRequestErrorMessage,
  isCreateAccountRecoveryError,
  parseFragmentParams,
  readLegacyFragmentLogin,
  resolvePendingAccountAction,
  sanitizeRedirectPath,
  serializeAdoptionDecision,
  type PendingAccountAction,
  type PendingOAuthCompletionFields
} from '@/components/auth/pendingOAuthShared'
import '@/views/authViews.css'
import { AuthLayout } from '@/components/layout/AuthLayout'

type LinuxDoPendingActionResponse = PendingOAuthExchangeResponse & PendingOAuthCompletionFields

export default function LinuxDoCallbackView() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const authStore = useAuthStore()
  const appStore = useAppStore()

  const providerName = 'LinuxDo'
  const bindSuccessMessage = t('profile.authBindings.bindSuccess')

  const [isProcessing, setIsProcessing] = useState(true)

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
  const [pendingAccountAction, setPendingAccountAction] = useState<PendingAccountAction>('none')
  const [pendingAccountEmail, setPendingAccountEmail] = useState('')
  const [bindLoginEmail, setBindLoginEmail] = useState('')
  const [bindLoginPassword, setBindLoginPassword] = useState('')
  const legacyPendingOAuthTokenRef = useRef('')
  const [accountActionError, setAccountActionError] = useState('')
  const [canReturnToCreateAccount, setCanReturnToCreateAccount] = useState(false)
  const [needsTotpChallenge, setNeedsTotpChallenge] = useState(false)
  const [totpTempToken, setTotpTempToken] = useState('')
  const [totpCode, setTotpCode] = useState('')
  const [totpError, setTotpError] = useState('')
  const [totpUserEmailMasked, setTotpUserEmailMasked] = useState('')
  const [errorMessage, setErrorMessage] = useState('')

  const needsCreateAccount = pendingAccountAction === 'create_account'
  const needsChooser = pendingAccountAction === 'choose_account_action'
  const needsBindLogin = pendingAccountAction === 'bind_login'

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
      provider: 'linuxdo',
      redirect: sanitizeRedirectPath(redirect || redirectTo)
    })
  }

  function clearPendingAuthSession() {
    authStore.clearPendingAuthSession()
  }

  const currentAdoptionDecision = useCallback(
    (): OAuthAdoptionDecision => ({ adoptDisplayName, adoptAvatar }),
    [adoptDisplayName, adoptAvatar]
  )

  function applyAdoptionSuggestionState(completion: {
    adoption_required?: boolean
    suggested_display_name?: string
    suggested_avatar_url?: string
  }) {
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

  function applyPendingAccountAction(completion: LinuxDoPendingActionResponse): PendingAccountAction {
    const action = resolvePendingAccountAction(completion, LINUXDO_STATE_ALIASES)
    setPendingAccountAction(action)
    setAccountActionError('')
    setNeedsTotpChallenge(false)
    setTotpTempToken('')
    setTotpCode('')
    setTotpError('')
    setTotpUserEmailMasked('')

    const email = extractPendingAccountEmail(completion, LINUXDO_STATE_ALIASES)
    if (action === 'choose_account_action') {
      setPendingAccountEmail(email)
      setBindLoginEmail(email)
      setBindLoginPassword('')
      setCanReturnToCreateAccount(false)
      return action
    }

    if (action === 'create_account') {
      setPendingAccountEmail(email)
      setCanReturnToCreateAccount(true)
      return action
    }

    if (action === 'bind_login') {
      setBindLoginEmail(email)
      setBindLoginPassword('')
      setCanReturnToCreateAccount(false)
      return action
    }

    setCanReturnToCreateAccount(false)
    return action
  }

  function applyTotpChallenge(completion: LinuxDoPendingActionResponse): boolean {
    if (completion.requires_2fa !== true || !completion.temp_token) {
      return false
    }

    setPendingAccountAction('none')
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
    setBindLoginEmail(bindLoginEmail.trim() || nextEmail?.trim() || pendingAccountEmail.trim())
    setBindLoginPassword('')
    setAccountActionError('')
    setCanReturnToCreateAccount(true)
  }

  function switchToCreateAccountMode() {
    setPendingAccountAction('create_account')
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
      throw new Error(t('auth.linuxdo.callbackMissingToken'))
    }

    persistOAuthTokenContext(completion)
    await authStore.setToken(completion.access_token)
    clearAllAffiliateReferralCodes()
    appStore.showSuccess(t('auth.loginSuccess'))
    await navigate(redirect, { replace: true })
  }

  async function finalizePendingAccountResponse(completion: LinuxDoPendingActionResponse) {
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
      const completion: LinuxDoPendingActionResponse = legacyPendingOAuthTokenRef.current
        ? (
            await apiClient.post<LinuxDoPendingActionResponse>('/auth/oauth/linuxdo/complete-registration', {
              pending_oauth_token: legacyPendingOAuthTokenRef.current,
              invitation_code: invitationCode.trim(),
              ...oauthAffiliatePayload(affCode),
              ...serializeAdoptionDecision(decision)
            })
          ).data
        : affCode
          ? await completeLinuxDoOAuthRegistration(invitationCode.trim(), decision, affCode)
          : await completeLinuxDoOAuthRegistration(invitationCode.trim(), decision)
      await finalizePendingAccountResponse(completion)
    } catch (error: unknown) {
      const err = error as { message?: string; response?: { data?: { message?: string } } }
      setInvitationError(
        err.response?.data?.message || err.message || t('auth.linuxdo.completeRegistrationFailed')
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
      )) as LinuxDoPendingActionResponse
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
      const { data } = await apiClient.post<LinuxDoPendingActionResponse>('/auth/oauth/pending/create-account', {
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
      const { data } = await apiClient.post<LinuxDoPendingActionResponse>('/auth/oauth/pending/bind-login', {
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

  useEffect(() => {
    let cancelled = false

    void (async () => {
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

        const completion = await exchangePendingOAuthCompletion()
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

        if (applyTotpChallenge(completion as LinuxDoPendingActionResponse)) {
          persistPendingAuthSession(completionRedirect)
          return
        }

        const action = applyPendingAccountAction(completion as LinuxDoPendingActionResponse)
        if (action !== 'none') {
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

  return (
    <AuthLayout>
      <div className="space-y-6">
        <div className="text-center">
          <h2 className="text-2xl font-bold text-gray-900 dark:text-white">
            {t('auth.linuxdo.callbackTitle')}
          </h2>
          <p className="mt-2 text-sm surface-muted">
            {isProcessing ? t('auth.linuxdo.callbackProcessing') : t('auth.linuxdo.callbackHint')}
          </p>
        </div>

        <PendingOAuthCallbackPanel
          providerName={providerName}
          needsInvitation={needsInvitation}
          needsAdoptionConfirmation={needsAdoptionConfirmation}
          needsChooser={needsChooser}
          needsCreateAccount={needsCreateAccount}
          needsBindLogin={needsBindLogin}
          needsTotpChallenge={needsTotpChallenge}
          isSubmitting={isSubmitting}
          adoptionRequired={adoptionRequired}
          suggestedDisplayName={suggestedDisplayName}
          suggestedAvatarUrl={suggestedAvatarUrl}
          adoptDisplayName={adoptDisplayName}
          adoptAvatar={adoptAvatar}
          onAdoptDisplayNameChange={setAdoptDisplayName}
          onAdoptAvatarChange={setAdoptAvatar}
          invitationCode={invitationCode}
          onInvitationCodeChange={setInvitationCode}
          onSubmitInvitation={() => void handleSubmitInvitation()}
          invitationRequiredText={t('auth.linuxdo.invitationRequired')}
          invitationCompletingText={t('auth.linuxdo.completing')}
          invitationCompleteText={t('auth.linuxdo.completeRegistration')}
          onContinueLogin={() => void handleContinueLogin()}
          pendingAccountEmail={pendingAccountEmail}
          onSwitchToBindLogin={switchToBindLoginMode}
          onSwitchToCreateAccount={() => switchToCreateAccountMode()}
          onCreateAccount={(payload) => void handleCreateAccount(payload)}
          accountActionError={accountActionError}
          createAccountTestIdPrefix="linuxdo"
          canReturnToCreateAccount={canReturnToCreateAccount}
          bindLoginEmail={bindLoginEmail}
          bindLoginPassword={bindLoginPassword}
          onBindLoginEmailChange={setBindLoginEmail}
          onBindLoginPasswordChange={setBindLoginPassword}
          onBindLogin={() => void handleBindLogin()}
          bindLoginTestIdPrefix="linuxdo"
          bindLoginHintText={t('auth.oauthFlow.bindLoginHint', { providerName })}
          totpCode={totpCode}
          onTotpCodeChange={setTotpCode}
          onSubmitTotpChallenge={() => void handleSubmitTotpChallenge()}
          totpUserEmailMasked={totpUserEmailMasked}
          totpTestIdPrefix="linuxdo"
        />
      </div>
    </AuthLayout>
  )
}
