import { forwardRef, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { adminAPI } from '@/api'
import type { AccountPlatform } from '@/types'
import type { AddMethod, AuthInputMethod } from './accountOAuth'
import { Icon } from './AccountIcon'
import { useClipboard } from './stepUp'

export interface OAuthAuthorizationFlowHandle {
  authCode: string
  oauthState: string
  projectId: string
  sessionKey: string
  refreshToken: string
  sessionToken: string
  codexSession: string
  codexPAT: string
  ssoCookie: string
  emailPassword: string
  inputMethod: AuthInputMethod
  reset: () => void
}

export interface OAuthAuthorizationFlowProps {
  addMethod: AddMethod
  authUrl?: string
  sessionId?: string
  loading?: boolean
  error?: string
  showHelp?: boolean
  showProxyWarning?: boolean
  allowMultiple?: boolean
  methodLabel?: string
  showCookieOption?: boolean
  showRefreshTokenOption?: boolean
  showMobileRefreshTokenOption?: boolean
  showSessionTokenOption?: boolean
  showAccessTokenOption?: boolean
  showCodexSessionImportOption?: boolean
  showAgentIdentityOption?: boolean
  showCodexPatOption?: boolean
  showSsoOption?: boolean
  /** Grok email + password login (admin; password never persisted). */
  showEmailPasswordOption?: boolean
  showManualOption?: boolean
  showDeviceCodeOption?: boolean
  deviceUserCode?: string
  deviceVerificationUrl?: string
  deviceCodePolling?: boolean
  initialInputMethod?: AuthInputMethod
  /** Prefill for Grok email + password reauth. Password is never stored. */
  initialEmailPassword?: string
  platform?: AccountPlatform
  showProjectId?: boolean
  onGenerateUrl: () => void
  onExchangeCode: (code: string) => void
  onCookieAuth: (sessionKey: string) => void
  onValidateRefreshToken: (refreshToken: string) => void
  onValidateMobileRefreshToken: (refreshToken: string) => void
  onValidateSessionToken: (sessionToken: string) => void
  onImportAccessToken: (accessToken: string) => void
  onImportCodexSession: (content: string) => void
  onImportCodexPat: (accessToken: string) => void
  onImportSso: (content: string) => void
  onAuthorizePassword: (emailPasswordInput: string) => void
  onStartDeviceCode: () => void
  onInputMethodChange: (method: AuthInputMethod) => void
}

const Spinner = ({ className }: { className?: string }) => (
  <svg className={cn('-ml-1 mr-2 h-4 w-4 animate-spin', className)} fill="none" viewBox="0 0 24 24">
    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
    <path
      className="opacity-75"
      fill="currentColor"
      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
    />
  </svg>
)

export const OAuthAuthorizationFlow = forwardRef<OAuthAuthorizationFlowHandle, OAuthAuthorizationFlowProps>(
  function OAuthAuthorizationFlow(
    {
      authUrl = '',
      loading = false,
      error = '',
      showHelp = true,
      showProxyWarning = true,
      allowMultiple = false,
      methodLabel = 'Authorization Method',
      showCookieOption = true,
      showRefreshTokenOption = false,
      showMobileRefreshTokenOption = false,
      showSessionTokenOption = false,
      showAccessTokenOption = false,
      showCodexSessionImportOption = false,
      showAgentIdentityOption = false,
      showCodexPatOption = false,
      showSsoOption = false,
      showEmailPasswordOption = false,
      showManualOption = true,
      showDeviceCodeOption = false,
      deviceUserCode = '',
      deviceVerificationUrl = '',
      deviceCodePolling = false,
      initialInputMethod = 'manual',
      initialEmailPassword = '',
      platform = 'anthropic',
      showProjectId = true,
      onGenerateUrl,
      onCookieAuth,
      onValidateRefreshToken,
      onValidateMobileRefreshToken,
      onValidateSessionToken,
      onImportAccessToken,
      onImportCodexSession,
      onImportCodexPat,
      onImportSso,
      onAuthorizePassword,
      onStartDeviceCode,
      onInputMethodChange
    },
    ref
  ) {
    const { t } = useTranslation()
    const { copied, copyToClipboard } = useClipboard()

    const [passwordAuthEnabled, setPasswordAuthEnabled] = useState(false)
    const emailPasswordOptionEnabled = showEmailPasswordOption && platform === 'grok' && passwordAuthEnabled

    const showLocalCallbackNotice = platform === 'openai' || platform === 'grok'

    const [inputMethod, setInputMethod] = useState<AuthInputMethod>(initialInputMethod)
    const isAgentIdentityInput = inputMethod === 'agent_identity'
    const [authCodeInput, setAuthCodeInput] = useState('')
    const [sessionKeyInput, setSessionKeyInput] = useState('')
    const [refreshTokenInput, setRefreshTokenInput] = useState('')
    const [sessionTokenInput, setSessionTokenInput] = useState('')
    const [codexSessionInput, setCodexSessionInput] = useState('')
    const [codexPATInput, setCodexPATInput] = useState('')
    const [ssoCookieInput, setSsoCookieInput] = useState('')
    const [emailPasswordInput, setEmailPasswordInput] = useState(initialEmailPassword || '')
    const [showHelpDialog, setShowHelpDialog] = useState(false)
    const [oauthState, setOAuthState] = useState('')
    const [projectId, setProjectId] = useState('')

    // Latest-value mirror for the imperative handle.
    const stateRef = useRef({
      authCodeInput,
      oauthState,
      projectId,
      sessionKeyInput,
      refreshTokenInput,
      sessionTokenInput,
      codexSessionInput,
      codexPATInput,
      ssoCookieInput,
      emailPasswordInput,
      inputMethod
    })
    stateRef.current = {
      authCodeInput,
      oauthState,
      projectId,
      sessionKeyInput,
      refreshTokenInput,
      sessionTokenInput,
      codexSessionInput,
      codexPATInput,
      ssoCookieInput,
      emailPasswordInput,
      inputMethod
    }

    useImperativeHandle<OAuthAuthorizationFlowHandle, OAuthAuthorizationFlowHandle>(
      ref,
      () => ({
        get authCode() {
          return stateRef.current.authCodeInput
        },
        get oauthState() {
          return stateRef.current.oauthState
        },
        get projectId() {
          return stateRef.current.projectId
        },
        get sessionKey() {
          return stateRef.current.sessionKeyInput
        },
        get refreshToken() {
          return stateRef.current.refreshTokenInput
        },
        get sessionToken() {
          return stateRef.current.sessionTokenInput
        },
        get codexSession() {
          return stateRef.current.codexSessionInput
        },
        get codexPAT() {
          return stateRef.current.codexPATInput
        },
        get ssoCookie() {
          return stateRef.current.ssoCookieInput
        },
        get emailPassword() {
          return stateRef.current.emailPasswordInput
        },
        get inputMethod() {
          return stateRef.current.inputMethod
        },
        reset: () => {
          setAuthCodeInput('')
          setOAuthState('')
          setProjectId('')
          setSessionKeyInput('')
          setRefreshTokenInput('')
          setSessionTokenInput('')
          setCodexSessionInput('')
          setCodexPATInput('')
          setSsoCookieInput('')
          setEmailPasswordInput('')
          setInputMethod(initialInputMethod)
          setShowHelpDialog(false)
        }
      }),
      [initialInputMethod]
    )

    // Get translation key based on platform
    const getOAuthKey = (key: string) => {
      if (platform === 'openai') return `admin.accounts.oauth.openai.${key}`
      if (platform === 'gemini') return `admin.accounts.oauth.gemini.${key}`
      if (platform === 'antigravity') return `admin.accounts.oauth.antigravity.${key}`
      if (platform === 'grok') return `admin.accounts.oauth.grok.${key}`
      return `admin.accounts.oauth.${key}`
    }

    const oauthTitle = t(getOAuthKey('title'))
    const oauthFollowSteps = t(getOAuthKey('followSteps'))
    const oauthStep1GenerateUrl = t(getOAuthKey('step1GenerateUrl'))
    const oauthGenerateAuthUrl = t(getOAuthKey('generateAuthUrl'))
    const oauthStep2OpenUrl = t(getOAuthKey('step2OpenUrl'))
    const oauthOpenUrlDesc = t(getOAuthKey('openUrlDesc'))
    const oauthStep3EnterCode = t(getOAuthKey('step3EnterCode'))
    const oauthAuthCodeDesc = t(getOAuthKey('authCodeDesc'))
    const oauthAuthCode = t(getOAuthKey('authCode'))
    const oauthAuthCodePlaceholder = t(getOAuthKey('authCodePlaceholder'))
    const oauthAuthCodeHint = t(getOAuthKey('authCodeHint'))
    const oauthImportantNotice = (() => {
      if (platform === 'openai') return t('admin.accounts.oauth.openai.importantNotice')
      if (platform === 'antigravity') return t('admin.accounts.oauth.antigravity.importantNotice')
      if (platform === 'grok') return t('admin.accounts.oauth.grok.importantNotice')
      return ''
    })()

    useEffect(() => {
      let cancelled = false
      setPasswordAuthEnabled(false)
      if (platform !== 'grok' || !showEmailPasswordOption) return
      void (async () => {
        try {
          const capabilities = await adminAPI.grok.getCapabilities()
          if (!cancelled) setPasswordAuthEnabled(capabilities.password_auth_enabled)
        } catch {
          // Fail closed; the backend enforces the same capability.
        }
      })()
      return () => {
        cancelled = true
      }
    }, [platform, showEmailPasswordOption])

    useEffect(() => {
      if (!emailPasswordOptionEnabled && inputMethod === 'email_password') setInputMethod('manual')
    }, [emailPasswordOptionEnabled, inputMethod])

    useEffect(() => {
      setInputMethod(initialInputMethod)
    }, [initialInputMethod])

    useEffect(() => {
      // Only prefill when the field is empty so we never overwrite operator input.
      if (initialEmailPassword && !emailPasswordInput.trim()) setEmailPasswordInput(initialEmailPassword)
    }, [initialEmailPassword])

    useEffect(() => {
      onInputMethodChange(inputMethod)
    }, [inputMethod])

    // Auto-extract code from callback URL (OpenAI/Gemini/Antigravity/Grok)
    // e.g., http://localhost:8085/callback?code=xxx...&state=...
    useEffect(() => {
      if (platform !== 'openai' && platform !== 'gemini' && platform !== 'antigravity' && platform !== 'grok') return

      const trimmed = authCodeInput.trim()
      // Check if it looks like a URL with code parameter
      if (trimmed.includes('code=')) {
        try {
          const url = trimmed.includes('?')
            ? new URL(trimmed)
            : new URL(`http://localhost/callback?${trimmed.replace(/^\?/, '')}`)
          const code = url.searchParams.get('code')
          const stateParam = url.searchParams.get('state')
          if (stateParam) setOAuthState(stateParam)
          if (code && code !== trimmed) {
            setAuthCodeInput(code)
          }
        } catch {
          // If URL parsing fails, try regex extraction
          const match = trimmed.match(/[?&]code=([^&]+)/)
          const stateMatch = trimmed.match(/[?&]state=([^&]+)/)
          if (stateMatch && stateMatch[1]) setOAuthState(stateMatch[1])
          if (match && match[1] && match[1] !== trimmed) {
            setAuthCodeInput(match[1])
          }
        }
      }
    }, [authCodeInput, platform])

    // Computed: show method selection only when there is something to choose.
    const methodOptionCount = useMemo(
      () =>
        [
          showManualOption,
          showDeviceCodeOption,
          showCookieOption,
          showRefreshTokenOption,
          showMobileRefreshTokenOption,
          showSessionTokenOption,
          showAccessTokenOption,
          showCodexSessionImportOption,
          showAgentIdentityOption,
          showCodexPatOption,
          showSsoOption,
          emailPasswordOptionEnabled
        ].filter(Boolean).length,
      [
        showManualOption,
        showDeviceCodeOption,
        showCookieOption,
        showRefreshTokenOption,
        showMobileRefreshTokenOption,
        showSessionTokenOption,
        showAccessTokenOption,
        showCodexSessionImportOption,
        showAgentIdentityOption,
        showCodexPatOption,
        showSsoOption,
        emailPasswordOptionEnabled
      ]
    )
    const showMethodSelection = methodOptionCount > 1

    const parsedKeyCount = sessionKeyInput.split('\n').map((k) => k.trim()).filter((k) => k).length
    const parsedRefreshTokenCount = refreshTokenInput.split('\n').map((rt) => rt.trim()).filter((rt) => rt).length
    const parsedCodexSessionCount = (() => {
      const trimmed = codexSessionInput.trim()
      if (!trimmed) return 0
      if (trimmed.startsWith('{') || trimmed.startsWith('[')) return 1
      return trimmed.split('\n').map((item) => item.trim()).filter((item) => item).length
    })()
    const parsedSSOCount = ssoCookieInput.split('\n').map((item) => item.trim()).filter((item) => item).length
    const parsedEmailPasswordCount = emailPasswordInput
      .split('\n')
      .map((item) => item.trim())
      .filter((item) => item && item.includes('----')).length

    const handleAuthorizePassword = () => {
      if (emailPasswordInput.trim()) {
        onAuthorizePassword(emailPasswordInput)
      }
    }

    const handleGenerateUrl = () => {
      onGenerateUrl()
    }

    const handleStartDeviceCode = () => {
      onStartDeviceCode()
    }

    const handleCopyUrl = () => {
      if (authUrl) {
        void copyToClipboard(authUrl, 'URL copied to clipboard')
      }
    }

    const handleRegenerate = () => {
      setAuthCodeInput('')
      onGenerateUrl()
    }

    const handleCookieAuth = () => {
      if (sessionKeyInput.trim()) {
        onCookieAuth(sessionKeyInput)
      }
    }

    const handleValidateRefreshToken = () => {
      if (refreshTokenInput.trim()) {
        if (inputMethod === 'mobile_refresh_token') {
          onValidateMobileRefreshToken(refreshTokenInput.trim())
        } else {
          onValidateRefreshToken(refreshTokenInput.trim())
        }
      }
    }

    const handleImportCodexSession = () => {
      if (codexSessionInput.trim()) {
        onImportCodexSession(codexSessionInput.trim())
      }
    }

    const handleImportCodexPAT = () => {
      if (codexPATInput.trim()) {
        onImportCodexPat(codexPATInput.trim())
      }
    }

    const handleImportSSO = () => {
      if (ssoCookieInput.trim()) {
        onImportSso(ssoCookieInput.trim())
      }
    }

    const radioClass = 'text-blue-600 focus:ring-blue-500'
    const errorBox = (
      <div className="mb-4 rounded-lg border border-red-200 bg-red-50/[0.15] p-3 dark:border-red-700 dark:bg-red-900/30">
        <p className="whitespace-pre-line text-sm text-red-600 dark:text-red-400">{error}</p>
      </div>
    )

    return (
      <div className="rounded-lg border border-blue-200 bg-blue-50/[0.15] p-4 dark:border-blue-700 dark:bg-blue-900/30">
        <div className="flex items-start gap-4">
          <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg bg-blue-500">
            <Icon name="link" size="md" className="text-white" />
          </div>
          <div className="flex-1">
            <h4 className="mb-3 font-semibold text-blue-900 dark:text-blue-200">{oauthTitle}</h4>

            {/* Auth Method Selection */}
            {showMethodSelection && (
              <div className="mb-4">
                <label className="mb-2 block text-sm font-medium text-blue-800 dark:text-blue-300">{methodLabel}</label>
                <div className="flex flex-wrap gap-4">
                  {showManualOption && (
                    <label className="flex cursor-pointer items-center gap-2">
                      <input
                        checked={inputMethod === 'manual'}
                        onChange={() => setInputMethod('manual')}
                        type="radio"
                        value="manual"
                        className={radioClass}
                      />
                      <span className="text-sm text-blue-900 dark:text-blue-200">{t('admin.accounts.oauth.manualAuth')}</span>
                    </label>
                  )}
                  {showDeviceCodeOption && (
                    <label className="flex cursor-pointer items-center gap-2">
                      <input
                        checked={inputMethod === 'device_code'}
                        onChange={() => setInputMethod('device_code')}
                        type="radio"
                        value="device_code"
                        className={radioClass}
                      />
                      <span className="text-sm text-blue-900 dark:text-blue-200">
                        {t('admin.accounts.oauth.openai.deviceCodeAuth')}
                      </span>
                    </label>
                  )}
                  {showCookieOption && (
                    <label className="flex cursor-pointer items-center gap-2">
                      <input
                        checked={inputMethod === 'cookie'}
                        onChange={() => setInputMethod('cookie')}
                        type="radio"
                        value="cookie"
                        className={radioClass}
                      />
                      <span className="text-sm text-blue-900 dark:text-blue-200">
                        {t('admin.accounts.oauth.cookieAutoAuth')}
                      </span>
                    </label>
                  )}
                  {showRefreshTokenOption && (
                    <label className="flex cursor-pointer items-center gap-2">
                      <input
                        checked={inputMethod === 'refresh_token'}
                        onChange={() => setInputMethod('refresh_token')}
                        type="radio"
                        value="refresh_token"
                        className={radioClass}
                      />
                      <span className="text-sm text-blue-900 dark:text-blue-200">{t(getOAuthKey('refreshTokenAuth'))}</span>
                    </label>
                  )}
                  {showSsoOption && (
                    <label className="flex cursor-pointer items-center gap-2">
                      <input
                        checked={inputMethod === 'sso_cookie'}
                        onChange={() => setInputMethod('sso_cookie')}
                        type="radio"
                        value="sso_cookie"
                        className={radioClass}
                      />
                      <span className="text-sm text-blue-900 dark:text-blue-200">{t(getOAuthKey('ssoCookieAuth'))}</span>
                    </label>
                  )}
                  {emailPasswordOptionEnabled && (
                    <label className="flex cursor-pointer items-center gap-2">
                      <input
                        checked={inputMethod === 'email_password'}
                        onChange={() => setInputMethod('email_password')}
                        type="radio"
                        value="email_password"
                        className={radioClass}
                      />
                      <span className="text-sm text-blue-900 dark:text-blue-200">{t(getOAuthKey('emailPasswordAuth'))}</span>
                    </label>
                  )}
                  {showMobileRefreshTokenOption && (
                    <label className="flex cursor-pointer items-center gap-2">
                      <input
                        checked={inputMethod === 'mobile_refresh_token'}
                        onChange={() => setInputMethod('mobile_refresh_token')}
                        type="radio"
                        value="mobile_refresh_token"
                        className={radioClass}
                      />
                      <span className="text-sm text-blue-900 dark:text-blue-200">
                        {t('admin.accounts.oauth.openai.mobileRefreshTokenAuth')}
                      </span>
                    </label>
                  )}
                  {showSessionTokenOption && (
                    <label className="flex cursor-pointer items-center gap-2">
                      <input
                        checked={inputMethod === 'session_token'}
                        onChange={() => setInputMethod('session_token')}
                        type="radio"
                        value="session_token"
                        className={radioClass}
                      />
                      <span className="text-sm text-blue-900 dark:text-blue-200">{t(getOAuthKey('sessionTokenAuth'))}</span>
                    </label>
                  )}
                  {showAccessTokenOption && (
                    <label className="flex cursor-pointer items-center gap-2">
                      <input
                        checked={inputMethod === 'access_token'}
                        onChange={() => setInputMethod('access_token')}
                        type="radio"
                        value="access_token"
                        className={radioClass}
                      />
                      <span className="text-sm text-blue-900 dark:text-blue-200">
                        {t('admin.accounts.oauth.openai.accessTokenAuth')}
                      </span>
                    </label>
                  )}
                  {showCodexSessionImportOption && (
                    <label className="flex cursor-pointer items-center gap-2">
                      <input
                        checked={inputMethod === 'codex_session'}
                        onChange={() => setInputMethod('codex_session')}
                        type="radio"
                        value="codex_session"
                        className={radioClass}
                      />
                      <span className="text-sm text-blue-900 dark:text-blue-200">
                        {t('admin.accounts.oauth.openai.codexSessionAuth')}
                      </span>
                    </label>
                  )}
                  {showAgentIdentityOption && (
                    <label className="flex cursor-pointer items-center gap-2">
                      <input
                        checked={inputMethod === 'agent_identity'}
                        onChange={() => setInputMethod('agent_identity')}
                        type="radio"
                        value="agent_identity"
                        className={radioClass}
                      />
                      <span className="text-sm text-blue-900 dark:text-blue-200">
                        {t('admin.accounts.oauth.openai.agentIdentityAuth')}
                      </span>
                    </label>
                  )}
                  {showCodexPatOption && (
                    <label className="flex cursor-pointer items-center gap-2">
                      <input
                        checked={inputMethod === 'codex_pat'}
                        onChange={() => setInputMethod('codex_pat')}
                        type="radio"
                        value="codex_pat"
                        className={radioClass}
                      />
                      <span className="text-sm text-blue-900 dark:text-blue-200">
                        {t('admin.accounts.oauth.openai.codexPatAuth')}
                      </span>
                    </label>
                  )}
                </div>
              </div>
            )}

            {/* Refresh Token Input (OpenAI / Antigravity / Mobile RT) */}
            {(inputMethod === 'refresh_token' || inputMethod === 'mobile_refresh_token') && (
              <div className="space-y-4">
                <div className="rounded-lg border border-blue-300 surface-inset p-4 dark:border-blue-600">
                  <p className="mb-3 text-sm text-blue-700 dark:text-blue-300">{t(getOAuthKey('refreshTokenDesc'))}</p>

                  {/* Refresh Token Input */}
                  <div className="mb-4">
                    <label className="mb-2 flex items-center gap-2 text-sm font-semibold surface-muted">
                      <Icon name="key" size="sm" className="text-blue-500" />
                      Refresh Token
                      {parsedRefreshTokenCount > 1 && (
                        <span className="rounded-full bg-blue-500 px-2 py-0.5 text-xs text-white">
                          {t('admin.accounts.oauth.keysCount', { count: parsedRefreshTokenCount })}
                        </span>
                      )}
                    </label>
                    <textarea
                      value={refreshTokenInput}
                      onChange={(event) => setRefreshTokenInput(event.target.value)}
                      rows={3}
                      className="input w-full resize-y font-mono text-sm"
                      placeholder={t(getOAuthKey('refreshTokenPlaceholder'))}
                    />
                    {parsedRefreshTokenCount > 1 && (
                      <p className="mt-1 text-xs text-blue-600 dark:text-blue-400">
                        {t('admin.accounts.oauth.batchCreateAccounts', { count: parsedRefreshTokenCount })}
                      </p>
                    )}
                  </div>

                  {/* Error Message */}
                  {error && errorBox}

                  {/* Validate Button */}
                  <button
                    type="button"
                    className="btn btn-primary w-full"
                    disabled={loading || !refreshTokenInput.trim()}
                    onClick={handleValidateRefreshToken}
                  >
                    {loading ? <Spinner /> : <Icon name="sparkles" size="sm" className="mr-2" />}
                    {loading ? t(getOAuthKey('validating')) : t(getOAuthKey('validateAndCreate'))}
                  </button>
                </div>
              </div>
            )}

            {/* SSO Cookie Input (Grok Web -> Grok Build) */}
            {inputMethod === 'sso_cookie' && (
              <div className="space-y-4">
                <div className="rounded-lg border border-blue-300 surface-inset p-4 dark:border-blue-600">
                  <p className="mb-3 text-sm text-blue-700 dark:text-blue-300">{t(getOAuthKey('ssoCookieDesc'))}</p>

                  <div className="mb-4">
                    <label className="mb-2 flex items-center gap-2 text-sm font-semibold surface-muted">
                      <Icon name="key" size="sm" className="text-blue-500" />
                      {t(getOAuthKey('ssoCookieLabel'))}
                      {parsedSSOCount > 1 && (
                        <span className="rounded-full bg-blue-500 px-2 py-0.5 text-xs text-white">
                          {t('admin.accounts.oauth.keysCount', { count: parsedSSOCount })}
                        </span>
                      )}
                    </label>
                    <textarea
                      value={ssoCookieInput}
                      onChange={(event) => setSsoCookieInput(event.target.value)}
                      rows={5}
                      className="input w-full resize-y font-mono text-sm"
                      placeholder={t(getOAuthKey('ssoCookiePlaceholder'))}
                      spellCheck={false}
                    />
                    <p className="mt-1 text-xs text-blue-600 dark:text-blue-400">{t(getOAuthKey('ssoCookieHint'))}</p>
                  </div>

                  {error && errorBox}

                  <button
                    type="button"
                    className="btn btn-primary w-full"
                    disabled={loading || !ssoCookieInput.trim()}
                    onClick={handleImportSSO}
                  >
                    {loading ? <Spinner /> : <Icon name="sparkles" size="sm" className="mr-2" />}
                    {loading ? t(getOAuthKey('convertingSSO')) : t(getOAuthKey('convertSSOAndCreate'))}
                  </button>
                </div>
              </div>
            )}

            {/* Grok email + password → ephemeral SSO → Build OAuth (password never stored) */}
            {inputMethod === 'email_password' && (
              <div className="space-y-4">
                <div className="rounded-lg border border-blue-300 surface-inset p-4 dark:border-blue-600">
                  <p className="mb-3 text-sm text-blue-700 dark:text-blue-300">{t(getOAuthKey('emailPasswordDesc'))}</p>
                  <div className="mb-4">
                    <label className="mb-2 flex items-center gap-2 text-sm font-semibold surface-muted">
                      <Icon name="user" size="sm" className="text-blue-500" />
                      {t(getOAuthKey('emailPasswordInputLabel'))}
                      {parsedEmailPasswordCount > 1 && (
                        <span className="rounded-full bg-blue-500 px-2 py-0.5 text-xs text-white">
                          {t('admin.accounts.oauth.keysCount', { count: parsedEmailPasswordCount })}
                        </span>
                      )}
                    </label>
                    <textarea
                      value={emailPasswordInput}
                      onChange={(event) => setEmailPasswordInput(event.target.value)}
                      rows={4}
                      className="input w-full resize-y font-mono text-sm"
                      placeholder={t(getOAuthKey('emailPasswordPlaceholder'))}
                      spellCheck={false}
                      autoComplete="off"
                    />
                    <p className="mt-1 text-xs text-blue-600 dark:text-blue-400">{t(getOAuthKey('emailPasswordHint'))}</p>
                  </div>
                  {error && errorBox}
                  <button
                    type="button"
                    className="btn btn-primary w-full"
                    disabled={loading || !emailPasswordInput.trim()}
                    onClick={handleAuthorizePassword}
                  >
                    {loading ? <Spinner /> : <Icon name="sparkles" size="sm" className="mr-2" />}
                    {loading ? t(getOAuthKey('validating')) : t(getOAuthKey('validateAndCreate'))}
                  </button>
                </div>
              </div>
            )}

            {/* Codex auth.json / session credential batch import */}
            {(inputMethod === 'codex_session' || inputMethod === 'agent_identity') && (
              <div className="space-y-4">
                <div className="rounded-lg border border-blue-300 surface-inset p-4 dark:border-blue-600">
                  <p className="mb-3 text-sm text-blue-700 dark:text-blue-300">
                    {t(
                      isAgentIdentityInput
                        ? 'admin.accounts.oauth.openai.agentIdentityDesc'
                        : 'admin.accounts.oauth.openai.codexSessionDesc'
                    )}
                  </p>

                  <div className="mb-4">
                    <label className="mb-2 flex items-center gap-2 text-sm font-semibold surface-muted">
                      <Icon name="key" size="sm" className="text-blue-500" />
                      {t(
                        isAgentIdentityInput
                          ? 'admin.accounts.oauth.openai.agentIdentityInputLabel'
                          : 'admin.accounts.oauth.openai.codexSessionInputLabel'
                      )}
                      {parsedCodexSessionCount > 1 && (
                        <span className="rounded-full bg-blue-500 px-2 py-0.5 text-xs text-white">
                          {t('admin.accounts.oauth.keysCount', { count: parsedCodexSessionCount })}
                        </span>
                      )}
                    </label>
                    <textarea
                      value={codexSessionInput}
                      onChange={(event) => setCodexSessionInput(event.target.value)}
                      rows={8}
                      className="input w-full resize-y font-mono text-sm"
                      placeholder={t(
                        isAgentIdentityInput
                          ? 'admin.accounts.oauth.openai.agentIdentityPlaceholder'
                          : 'admin.accounts.oauth.openai.codexSessionPlaceholder'
                      )}
                      spellCheck={false}
                    />
                    <p className="mt-1 text-xs text-blue-600 dark:text-blue-400">
                      {t(
                        isAgentIdentityInput
                          ? 'admin.accounts.oauth.openai.agentIdentityHint'
                          : 'admin.accounts.oauth.openai.codexSessionHint'
                      )}
                    </p>
                  </div>

                  {error && errorBox}

                  <button
                    type="button"
                    className="btn btn-primary w-full"
                    disabled={loading || !codexSessionInput.trim()}
                    onClick={handleImportCodexSession}
                  >
                    {loading ? <Spinner /> : <Icon name="sparkles" size="sm" className="mr-2" />}
                    {loading
                      ? t('admin.accounts.oauth.openai.validating')
                      : t('admin.accounts.oauth.openai.codexSessionImportAndCreate')}
                  </button>
                </div>
              </div>
            )}

            {/* Codex Personal Access Token */}
            {inputMethod === 'codex_pat' && (
              <div className="space-y-4">
                <div className="rounded-lg border border-blue-300 surface-inset p-4 dark:border-blue-600">
                  <p className="mb-3 text-sm text-blue-700 dark:text-blue-300">
                    {t('admin.accounts.oauth.openai.codexPatDesc')}
                  </p>

                  <div className="mb-4">
                    <label className="mb-2 flex items-center gap-2 text-sm font-semibold surface-muted">
                      <Icon name="key" size="sm" className="text-blue-500" />
                      {t('admin.accounts.oauth.openai.codexPatInputLabel')}
                    </label>
                    <textarea
                      value={codexPATInput}
                      onChange={(event) => setCodexPATInput(event.target.value)}
                      rows={3}
                      className="input w-full resize-y font-mono text-sm"
                      placeholder={t('admin.accounts.oauth.openai.codexPatPlaceholder')}
                      spellCheck={false}
                    />
                    <p className="mt-1 text-xs text-blue-600 dark:text-blue-400">
                      {t('admin.accounts.oauth.openai.codexPatHint')}
                    </p>
                  </div>

                  {error && errorBox}

                  <button
                    type="button"
                    className="btn btn-primary w-full"
                    disabled={loading || !codexPATInput.trim()}
                    onClick={handleImportCodexPAT}
                  >
                    {loading ? <Spinner /> : <Icon name="sparkles" size="sm" className="mr-2" />}
                    {loading
                      ? t('admin.accounts.oauth.openai.validating')
                      : t('admin.accounts.oauth.openai.codexPatImportAndCreate')}
                  </button>
                </div>
              </div>
            )}

            {/* Cookie Auto-Auth Form */}
            {inputMethod === 'cookie' && (
              <div className="space-y-4">
                <div className="rounded-lg border border-blue-300 surface-inset p-4 dark:border-blue-600">
                  <p className="mb-3 text-sm text-blue-700 dark:text-blue-300">
                    {t('admin.accounts.oauth.cookieAutoAuthDesc')}
                  </p>

                  {/* sessionKey Input */}
                  <div className="mb-4">
                    <label className="mb-2 flex items-center gap-2 text-sm font-semibold surface-muted">
                      <Icon name="key" size="sm" className="text-blue-500" />
                      {t('admin.accounts.oauth.sessionKey')}
                      {parsedKeyCount > 1 && allowMultiple && (
                        <span className="rounded-full bg-blue-500 px-2 py-0.5 text-xs text-white">
                          {t('admin.accounts.oauth.keysCount', { count: parsedKeyCount })}
                        </span>
                      )}
                      {showHelp && (
                        <button
                          type="button"
                          className="text-blue-500 hover:text-blue-600 surface-control"
                          onClick={() => setShowHelpDialog((prev) => !prev)}
                        >
                          <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="1.5">
                            <path
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              d="M9.879 7.519c1.171-1.025 3.071-1.025 4.242 0 1.172 1.025 1.172 2.687 0 3.712-.203.179-.43.326-.67.442-.745.361-1.45.999-1.45 1.827v.75M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9 5.25h.008v.008H12v-.008z"
                            />
                          </svg>
                        </button>
                      )}
                    </label>
                    <textarea
                      value={sessionKeyInput}
                      onChange={(event) => setSessionKeyInput(event.target.value)}
                      rows={3}
                      className="input w-full resize-y font-mono text-sm"
                      placeholder={
                        allowMultiple
                          ? t('admin.accounts.oauth.sessionKeyPlaceholder')
                          : t('admin.accounts.oauth.sessionKeyPlaceholderSingle')
                      }
                    />
                    {parsedKeyCount > 1 && allowMultiple && (
                      <p className="mt-1 text-xs text-blue-600 dark:text-blue-400">
                        {t('admin.accounts.oauth.batchCreateAccounts', { count: parsedKeyCount })}
                      </p>
                    )}
                  </div>

                  {/* Help Section */}
                  {showHelpDialog && showHelp && (
                    <div className="mb-4 rounded-lg border border-amber-200 bg-amber-50/[0.15] p-3 dark:border-amber-700 dark:bg-amber-900/30">
                      <h5 className="mb-2 font-semibold text-amber-800 dark:text-amber-200">
                        {t('admin.accounts.oauth.howToGetSessionKey')}
                      </h5>
                      <ol className="list-inside list-decimal space-y-1 text-xs text-amber-700 dark:text-amber-300">
                        <li>{t('admin.accounts.oauth.step1')}</li>
                        <li>{t('admin.accounts.oauth.step2')}</li>
                        <li>{t('admin.accounts.oauth.step3')}</li>
                        <li>{t('admin.accounts.oauth.step4')}</li>
                        <li>{t('admin.accounts.oauth.step5')}</li>
                        <li>{t('admin.accounts.oauth.step6')}</li>
                      </ol>
                      <p className="mt-2 text-xs text-amber-600 dark:text-amber-400">
                        {t('admin.accounts.oauth.sessionKeyFormat')}
                      </p>
                    </div>
                  )}

                  {/* Error Message */}
                  {error && errorBox}

                  {/* Auth Button */}
                  <button
                    type="button"
                    className="btn btn-primary w-full"
                    disabled={loading || !sessionKeyInput.trim()}
                    onClick={handleCookieAuth}
                  >
                    {loading ? <Spinner /> : <Icon name="sparkles" size="sm" className="mr-2" />}
                    {loading ? t('admin.accounts.oauth.authorizing') : t('admin.accounts.oauth.startAutoAuth')}
                  </button>
                </div>
              </div>
            )}

            {inputMethod === 'device_code' && (
              <div className="space-y-4">
                <p className="mb-4 text-sm text-blue-800 dark:text-blue-300">
                  {t('admin.accounts.oauth.openai.deviceCodeHint')}
                </p>
                <div className="rounded-lg border border-blue-300 surface-inset p-4 dark:border-blue-600">
                  {!deviceUserCode ? (
                    <button type="button" disabled={loading} className="btn btn-primary text-sm" onClick={handleStartDeviceCode}>
                      {loading ? t('admin.accounts.oauth.generating') : t('admin.accounts.oauth.openai.startDeviceCode')}
                    </button>
                  ) : (
                    <div className="space-y-3">
                      <div>
                        <p className="mb-1 text-xs font-medium text-blue-800 dark:text-blue-300">
                          {t('admin.accounts.oauth.openai.deviceCodeVerificationUrl')}
                        </p>
                        <a
                          href={deviceVerificationUrl}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="break-all text-sm text-blue-600 hover:text-blue-700 dark:text-blue-400"
                        >
                          {deviceVerificationUrl}
                        </a>
                      </div>
                      <div>
                        <p className="mb-1 text-xs font-medium text-blue-800 dark:text-blue-300">
                          {t('admin.accounts.oauth.openai.deviceCodeUserCode')}
                        </p>
                        <div className="flex items-center gap-2">
                          <input
                            value={deviceUserCode}
                            readOnly
                            type="text"
                            className="input flex-1 surface-inset font-mono text-lg tracking-widest"
                          />
                          <button
                            type="button"
                            className="btn btn-secondary p-2"
                            onClick={() =>
                              void copyToClipboard(deviceUserCode, t('admin.accounts.oauth.openai.deviceCodeUserCode'))
                            }
                          >
                            {copied ? (
                              <Icon name="check" size="sm" className="text-green-500" strokeWidth={2} />
                            ) : (
                              <Icon name="link" size="sm" />
                            )}
                          </button>
                        </div>
                      </div>
                      {deviceCodePolling && (
                        <p className="text-sm text-blue-700 dark:text-blue-300">
                          {t('admin.accounts.oauth.openai.deviceCodePolling')}
                        </p>
                      )}
                      <button
                        type="button"
                        className="text-xs text-blue-600 hover:text-blue-700 dark:text-blue-400 surface-control"
                        onClick={handleStartDeviceCode}
                      >
                        {t('admin.accounts.oauth.openai.deviceCodeRestart')}
                      </button>
                    </div>
                  )}
                </div>
              </div>
            )}

            {/* Manual Authorization Flow */}
            {inputMethod === 'manual' && (
              <div className="space-y-4">
                <p className="mb-4 text-sm text-blue-800 dark:text-blue-300">{oauthFollowSteps}</p>

                {/* Step 1: Generate Auth URL */}
                <div className="rounded-lg border border-blue-300 surface-inset p-4 dark:border-blue-600">
                  <div className="flex items-start gap-3">
                    <div className="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-blue-600 text-xs font-bold text-white">
                      1
                    </div>
                    <div className="flex-1">
                      <p className="mb-2 font-medium text-blue-900 dark:text-blue-200">{oauthStep1GenerateUrl}</p>
                      {showProjectId && platform === 'gemini' && (
                        <div className="mb-3">
                          <label className="input-label flex items-center gap-2">
                            {t('admin.accounts.oauth.gemini.projectIdLabel')}
                            <a
                              href="https://console.cloud.google.com/"
                              target="_blank"
                              rel="noopener noreferrer"
                              className="inline-flex items-center gap-1 text-xs font-normal text-blue-500 hover:text-blue-600 dark:text-blue-400"
                            >
                              <svg className="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="1.5">
                                <path
                                  strokeLinecap="round"
                                  strokeLinejoin="round"
                                  d="M9.879 7.519c1.171-1.025 3.071-1.025 4.242 0 1.172 1.025 1.172 2.687 0 3.712-.203.179-.43.326-.67.442-.745.361-1.45.999-1.45 1.827v.75M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9 5.25h.008v.008H12v-.008z"
                                />
                              </svg>
                              {t('admin.accounts.oauth.gemini.howToGetProjectId')}
                            </a>
                          </label>
                          <input
                            value={projectId}
                            onChange={(event) => setProjectId(event.target.value)}
                            type="text"
                            className="input w-full font-mono text-sm"
                            placeholder={t('admin.accounts.oauth.gemini.projectIdPlaceholder')}
                          />
                          <p className="mt-1 text-xs surface-muted">{t('admin.accounts.oauth.gemini.projectIdHint')}</p>
                        </div>
                      )}
                      {!authUrl ? (
                        <button type="button" disabled={loading} className="btn btn-primary text-sm" onClick={handleGenerateUrl}>
                          {loading ? <Spinner /> : <Icon name="link" size="sm" className="mr-2" />}
                          {loading ? t('admin.accounts.oauth.generating') : oauthGenerateAuthUrl}
                        </button>
                      ) : (
                        <div className="space-y-3">
                          <div className="flex items-center gap-2">
                            <input value={authUrl} readOnly type="text" className="input flex-1 surface-inset font-mono text-xs" />
                            <button type="button" className="btn btn-secondary p-2" title="Copy URL" onClick={handleCopyUrl}>
                              {!copied ? (
                                <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="1.5">
                                  <path
                                    strokeLinecap="round"
                                    strokeLinejoin="round"
                                    d="M15.666 3.888A2.25 2.25 0 0013.5 2.25h-3c-1.03 0-1.9.693-2.166 1.638m7.332 0c.055.194.084.4.084.612v0a.75.75 0 01-.75.75H9a.75.75 0 01-.75-.75v0c0-.212.03-.418.084-.612m7.332 0c.646.049 1.288.11 1.927.184 1.1.128 1.907 1.077 1.907 2.185V19.5a2.25 2.25 0 01-2.25 2.25H6.75A2.25 2.25 0 014.5 19.5V6.257c0-1.108.806-2.057 1.907-2.185a48.208 48.208 0 011.927-.184"
                                  />
                                </svg>
                              ) : (
                                <Icon name="check" size="sm" className="text-green-500" strokeWidth={2} />
                              )}
                            </button>
                          </div>
                          <button
                            type="button"
                            className="text-xs text-blue-600 hover:text-blue-700 dark:text-blue-400 surface-control"
                            onClick={handleRegenerate}
                          >
                            <Icon name="refresh" size="xs" className="mr-1 inline" />
                            {t('admin.accounts.oauth.regenerate')}
                          </button>
                        </div>
                      )}
                    </div>
                  </div>
                </div>

                {/* Step 2: Open URL and authorize */}
                <div className="rounded-lg border border-blue-300 surface-inset p-4 dark:border-blue-600">
                  <div className="flex items-start gap-3">
                    <div className="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-blue-600 text-xs font-bold text-white">
                      2
                    </div>
                    <div className="flex-1">
                      <p className="mb-2 font-medium text-blue-900 dark:text-blue-200">{oauthStep2OpenUrl}</p>
                      <p className="text-sm text-blue-700 dark:text-blue-300">{oauthOpenUrlDesc}</p>
                      {/* Local callback notice */}
                      {showLocalCallbackNotice ? (
                        <div className="mt-2 rounded border border-amber-300 bg-amber-50/[0.15] p-3 dark:border-amber-700 dark:bg-amber-900/30">
                          <p className="text-xs text-amber-800 dark:text-amber-300">{oauthImportantNotice}</p>
                        </div>
                      ) : showProxyWarning ? (
                        /* Proxy Warning (for non-OpenAI) */
                        <div className="mt-2 rounded border border-yellow-300 bg-yellow-50/[0.15] p-3 dark:border-yellow-700 dark:bg-yellow-900/30">
                          <p className="text-xs text-yellow-800 dark:text-yellow-300">{t('admin.accounts.oauth.proxyWarning')}</p>
                        </div>
                      ) : null}
                    </div>
                  </div>
                </div>

                {/* Step 3: Enter authorization code */}
                <div className="rounded-lg border border-blue-300 surface-inset p-4 dark:border-blue-600">
                  <div className="flex items-start gap-3">
                    <div className="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-blue-600 text-xs font-bold text-white">
                      3
                    </div>
                    <div className="flex-1">
                      <p className="mb-2 font-medium text-blue-900 dark:text-blue-200">{oauthStep3EnterCode}</p>
                      <p className="mb-3 text-sm text-blue-700 dark:text-blue-300">{oauthAuthCodeDesc}</p>
                      <div>
                        <label className="input-label">
                          <Icon name="key" size="sm" className="mr-1 inline text-blue-500" />
                          {oauthAuthCode}
                        </label>
                        <textarea
                          value={authCodeInput}
                          onChange={(event) => setAuthCodeInput(event.target.value)}
                          rows={3}
                          className="input w-full resize-none font-mono text-sm"
                          placeholder={oauthAuthCodePlaceholder}
                        />
                        <p className="mt-2 text-xs surface-muted">
                          <Icon name="infoCircle" size="xs" className="mr-1 inline" />
                          {oauthAuthCodeHint}
                        </p>

                        {/* Gemini-specific state parameter warning */}
                        {platform === 'gemini' && (
                          <div className="mt-3 rounded-lg border-2 border-amber-400 bg-amber-50/[0.15] p-3 dark:border-amber-600 dark:bg-amber-900/30">
                            <div className="flex items-start gap-2">
                              <Icon
                                name="exclamationTriangle"
                                size="md"
                                className="flex-shrink-0 text-amber-600 dark:text-amber-400"
                                strokeWidth={2}
                              />
                              <div className="text-sm text-amber-800 dark:text-amber-300">
                                <p className="font-semibold">{t('admin.accounts.oauth.gemini.stateWarningTitle')}</p>
                                <p className="mt-1">{t('admin.accounts.oauth.gemini.stateWarningDesc')}</p>
                              </div>
                            </div>
                          </div>
                        )}
                      </div>

                      {/* Error Message */}
                      {error && (
                        <div className="mt-3 rounded-lg border border-red-200 bg-red-50/[0.15] p-3 dark:border-red-700 dark:bg-red-900/30">
                          <p className="whitespace-pre-line text-sm text-red-600 dark:text-red-400">{error}</p>
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              </div>
            )}

            {/* Access token import (non-OpenAI providers) */}
            {inputMethod === 'access_token' && (
              <div className="space-y-4">
                <div className="rounded-lg border border-blue-300 surface-inset p-4 dark:border-blue-600">
                  <p className="mb-3 text-sm text-blue-700 dark:text-blue-300">{t(getOAuthKey('accessTokenDesc'))}</p>
                  <div className="mb-4">
                    <label className="mb-2 flex items-center gap-2 text-sm font-semibold surface-muted">
                      <Icon name="key" size="sm" className="text-blue-500" />
                      {t(getOAuthKey('accessTokenLabel'))}
                    </label>
                    <textarea
                      value={sessionTokenInput}
                      onChange={(event) => setSessionTokenInput(event.target.value)}
                      rows={3}
                      className="input w-full resize-y font-mono text-sm"
                      placeholder={t(getOAuthKey('accessTokenPlaceholder'))}
                      spellCheck={false}
                    />
                  </div>
                  {error && errorBox}
                  <button
                    type="button"
                    className="btn btn-primary w-full"
                    disabled={loading || !sessionTokenInput.trim()}
                    onClick={() => {
                      if (sessionTokenInput.trim()) onImportAccessToken(sessionTokenInput.trim())
                    }}
                  >
                    {loading ? <Spinner /> : <Icon name="sparkles" size="sm" className="mr-2" />}
                    {loading ? t(getOAuthKey('validating')) : t(getOAuthKey('importAndCreate'))}
                  </button>
                </div>
              </div>
            )}

            {/* Session token import */}
            {inputMethod === 'session_token' && (
              <div className="space-y-4">
                <div className="rounded-lg border border-blue-300 surface-inset p-4 dark:border-blue-600">
                  <p className="mb-3 text-sm text-blue-700 dark:text-blue-300">{t(getOAuthKey('sessionTokenDesc'))}</p>
                  <div className="mb-4">
                    <label className="mb-2 flex items-center gap-2 text-sm font-semibold surface-muted">
                      <Icon name="key" size="sm" className="text-blue-500" />
                      {t(getOAuthKey('sessionTokenLabel'))}
                    </label>
                    <textarea
                      value={sessionTokenInput}
                      onChange={(event) => setSessionTokenInput(event.target.value)}
                      rows={3}
                      className="input w-full resize-y font-mono text-sm"
                      placeholder={t(getOAuthKey('sessionTokenPlaceholder'))}
                      spellCheck={false}
                    />
                  </div>
                  {error && errorBox}
                  <button
                    type="button"
                    className="btn btn-primary w-full"
                    disabled={loading || !sessionTokenInput.trim()}
                    onClick={() => {
                      if (sessionTokenInput.trim()) onValidateSessionToken(sessionTokenInput.trim())
                    }}
                  >
                    {loading ? <Spinner /> : <Icon name="sparkles" size="sm" className="mr-2" />}
                    {loading ? t(getOAuthKey('validating')) : t(getOAuthKey('validateAndCreate'))}
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>
      </div>
    )
  }
)

export default OAuthAuthorizationFlow
