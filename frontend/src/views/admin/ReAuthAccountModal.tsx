import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { adminAPI } from '@/api'
import { useAppStore } from '@/stores/app'
import type { Account } from '@/types'
import { Modal } from '@/components/ui'
import { Icon } from './AccountIcon'
import {
  OAuthAuthorizationFlow,
  type OAuthAuthorizationFlowHandle
} from './OAuthAuthorizationFlow'
import {
  useAccountOAuth,
  useAntigravityOAuth,
  useGeminiOAuth,
  useOpenAIOAuth,
  type AddMethod,
  type AuthInputMethod
} from './accountOAuth'

export interface ReAuthAccountModalProps {
  show: boolean
  account: Account | null
  onClose: () => void
  onReauthorized: () => void
}

export function ReAuthAccountModal({ show, account, onClose, onReauthorized }: ReAuthAccountModalProps) {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)

  // OAuth hooks
  const claudeOAuth = useAccountOAuth()
  const openaiOAuth = useOpenAIOAuth()
  const geminiOAuth = useGeminiOAuth()
  const antigravityOAuth = useAntigravityOAuth()

  const oauthFlowRef = useRef<OAuthAuthorizationFlowHandle | null>(null)

  const [addMethod, setAddMethod] = useState<AddMethod>('oauth')
  const [geminiOAuthType, setGeminiOAuthType] = useState<'code_assist' | 'google_one' | 'ai_studio'>('code_assist')

  const isOpenAI = account?.platform === 'openai'
  const isOpenAILike = isOpenAI
  const isGemini = account?.platform === 'gemini'
  const isAnthropic = account?.platform === 'anthropic'
  const isAntigravity = account?.platform === 'antigravity'

  // Computed - current OAuth state based on platform
  const currentAuthUrl = isOpenAILike
    ? openaiOAuth.authUrl
    : isGemini
      ? geminiOAuth.authUrl
      : isAntigravity
        ? antigravityOAuth.authUrl
        : claudeOAuth.authUrl
  const currentSessionId = isOpenAILike
    ? openaiOAuth.sessionId
    : isGemini
      ? geminiOAuth.sessionId
      : isAntigravity
        ? antigravityOAuth.sessionId
        : claudeOAuth.sessionId
  const currentLoading = isOpenAILike
    ? openaiOAuth.loading
    : isGemini
      ? geminiOAuth.loading
      : isAntigravity
        ? antigravityOAuth.loading
        : claudeOAuth.loading
  const currentError = isOpenAILike
    ? openaiOAuth.error
    : isGemini
      ? geminiOAuth.error
      : isAntigravity
        ? antigravityOAuth.error
        : claudeOAuth.error

  const inputMethod: AuthInputMethod = oauthFlowRef.current?.inputMethod ?? 'manual'
  const isManualInputMethod =
    // OpenAI/Gemini/Antigravity always use manual input (no cookie auth option)
    isOpenAILike || isGemini || isAntigravity || inputMethod === 'manual'

  const canExchangeCode = Boolean(
    (oauthFlowRef.current?.authCode || '').trim() && currentSessionId && !currentLoading
  )

  useEffect(() => {
    if (!show || !account) {
      resetState()
      return
    }
    // Initialize addMethod based on current account type (Claude only)
    if (isAnthropic && (account.type === 'oauth' || account.type === 'setup-token')) {
      setAddMethod(account.type as AddMethod)
    }
    if (isGemini) {
      const creds = (account.credentials || {}) as Record<string, unknown>
      setGeminiOAuthType(
        creds.oauth_type === 'google_one'
          ? 'google_one'
          : creds.oauth_type === 'ai_studio'
            ? 'ai_studio'
            : 'code_assist'
      )
    }
  }, [show, account])

  const resetState = () => {
    setAddMethod('oauth')
    setGeminiOAuthType('code_assist')
    claudeOAuth.resetState()
    openaiOAuth.resetState()
    geminiOAuth.resetState()
    antigravityOAuth.resetState()
    oauthFlowRef.current?.reset()
  }

  const handleClose = () => {
    onClose()
  }

  const handleGenerateUrl = async () => {
    if (!account) return

    if (isOpenAILike) {
      await openaiOAuth.generateAuthUrl(account.proxy_id, undefined, account.id)
    } else if (isGemini) {
      const creds = (account.credentials || {}) as Record<string, unknown>
      const tierId = typeof creds.tier_id === 'string' ? creds.tier_id : undefined
      const projectId = geminiOAuthType === 'code_assist' ? oauthFlowRef.current?.projectId : undefined
      await geminiOAuth.generateAuthUrl(account.proxy_id, projectId, geminiOAuthType, tierId)
    } else if (isAntigravity) {
      await antigravityOAuth.generateAuthUrl(account.proxy_id)
    } else {
      await claudeOAuth.generateAuthUrl(addMethod, account.proxy_id)
    }
  }

  const handleExchangeCode = async () => {
    if (!account) return
    const expectedOwner = account.oauth_credential_owner || ''

    const authCode = oauthFlowRef.current?.authCode || ''
    if (!authCode.trim()) return

    if (isOpenAILike) {
      // OpenAI OAuth flow
      const sessionId = openaiOAuth.sessionId
      if (!sessionId) return
      const stateToUse = (oauthFlowRef.current?.oauthState || openaiOAuth.oauthState || '').trim()
      if (!stateToUse) {
        showError(t('admin.accounts.oauth.authFailed'))
        return
      }

      const tokenInfo = await openaiOAuth.exchangeAuthCode(authCode.trim(), sessionId, stateToUse, account.proxy_id)
      if (!tokenInfo) return

      // Build credentials and extra info
      const credentials = openaiOAuth.buildCredentials(tokenInfo)
      const extra = openaiOAuth.buildExtraInfo(tokenInfo)

      try {
        // Update account with new credentials
        await adminAPI.accounts.applyOAuthCredentials(account.id, {
          expected_owner: expectedOwner,
          type: 'oauth', // OpenAI OAuth is always 'oauth' type
          credentials,
          extra
        })

        // Clear error status after successful re-authorization
        await adminAPI.accounts.clearError(account.id)

        showSuccess(t('admin.accounts.reAuthorizedSuccess'))
        onReauthorized()
        handleClose()
      } catch (error) {
        const message =
          (error as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          t('admin.accounts.oauth.authFailed')
        showError(message)
      }
    } else if (isGemini) {
      const sessionId = geminiOAuth.sessionId
      if (!sessionId) return

      const stateFromInput = oauthFlowRef.current?.oauthState || ''
      const stateToUse = stateFromInput || geminiOAuth.state
      if (!stateToUse) return

      const tokenInfo = await geminiOAuth.exchangeAuthCode({
        code: authCode.trim(),
        sessionId,
        state: stateToUse,
        proxyId: account.proxy_id,
        oauthType: geminiOAuthType,
        tierId: typeof (account.credentials as Record<string, unknown> | undefined)?.tier_id === 'string'
          ? ((account.credentials as Record<string, unknown>).tier_id as string)
          : undefined
      })
      if (!tokenInfo) return

      const credentials = geminiOAuth.buildCredentials(tokenInfo)

      try {
        await adminAPI.accounts.update(account.id, {
          type: 'oauth',
          credentials
        })
        await adminAPI.accounts.clearError(account.id)
        showSuccess(t('admin.accounts.reAuthorizedSuccess'))
        onReauthorized()
        handleClose()
      } catch (error) {
        const message =
          (error as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          t('admin.accounts.oauth.authFailed')
        showError(message)
      }
    } else if (isAntigravity) {
      // Antigravity OAuth flow
      const sessionId = antigravityOAuth.sessionId
      if (!sessionId) return

      const stateFromInput = oauthFlowRef.current?.oauthState || ''
      const stateToUse = stateFromInput || antigravityOAuth.state
      if (!stateToUse) return

      const tokenInfo = await antigravityOAuth.exchangeAuthCode({
        code: authCode.trim(),
        sessionId,
        state: stateToUse,
        proxyId: account.proxy_id
      })
      if (!tokenInfo) return

      const credentials = antigravityOAuth.buildCredentials(tokenInfo)

      try {
        await adminAPI.accounts.update(account.id, {
          type: 'oauth',
          credentials
        })
        await adminAPI.accounts.clearError(account.id)
        showSuccess(t('admin.accounts.reAuthorizedSuccess'))
        onReauthorized()
        handleClose()
      } catch (error) {
        const message =
          (error as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          t('admin.accounts.oauth.authFailed')
        showError(message)
      }
    } else {
      // Claude OAuth flow
      const sessionId = claudeOAuth.sessionId
      if (!sessionId) return

      try {
        const proxyConfig = account.proxy_id ? { proxy_id: account.proxy_id } : {}
        const endpoint =
          addMethod === 'oauth' ? '/admin/accounts/exchange-code' : '/admin/accounts/exchange-setup-token-code'

        const tokenInfo = await adminAPI.accounts.exchangeCode(endpoint, {
          session_id: sessionId,
          code: authCode.trim(),
          ...proxyConfig
        })

        const extra = claudeOAuth.buildExtraInfo(tokenInfo)

        // Update account with new credentials and type
        await adminAPI.accounts.update(account.id, {
          type: addMethod, // Update type based on selected method
          credentials: tokenInfo,
          extra
        })

        // Clear error status after successful re-authorization
        await adminAPI.accounts.clearError(account.id)

        showSuccess(t('admin.accounts.reAuthorizedSuccess'))
        onReauthorized()
        handleClose()
      } catch (error) {
        const message =
          (error as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          t('admin.accounts.oauth.authFailed')
        showError(message)
      } finally {
        claudeOAuth.setLoading(false)
      }
    }
  }

  const handleCookieAuth = async (sessionKey: string) => {
    if (!account || isOpenAILike) return

    try {
      const proxyConfig = account.proxy_id ? { proxy_id: account.proxy_id } : {}
      const endpoint =
        addMethod === 'oauth' ? '/admin/accounts/cookie-auth' : '/admin/accounts/setup-token-cookie-auth'

      const tokenInfo = await adminAPI.accounts.exchangeCode(endpoint, {
        session_id: '',
        code: sessionKey.trim(),
        ...proxyConfig
      })

      const extra = claudeOAuth.buildExtraInfo(tokenInfo)

      // Update account with new credentials and type
      await adminAPI.accounts.update(account.id, {
        type: addMethod, // Update type based on selected method
        credentials: tokenInfo,
        extra
      })

      // Clear error status after successful re-authorization
      await adminAPI.accounts.clearError(account.id)

      showSuccess(t('admin.accounts.reAuthorizedSuccess'))
      onReauthorized()
      handleClose()
    } catch (error) {
      const message =
        (error as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
        t('admin.accounts.oauth.cookieAuthFailed')
      showError(message)
    } finally {
      claudeOAuth.setLoading(false)
    }
  }

  return (
    <Modal open={show} onClose={handleClose} title={t('admin.accounts.reAuthorizeAccount')} size="lg">
      {account && (
        <div className="space-y-4">
          {/* Account Info */}
          <div className="rounded-lg border border-gray-200 surface-inset p-4 dark:border-dark-600">
            <div className="flex items-center gap-3">
              <div
                className={[
                  'flex h-10 w-10 items-center justify-center rounded-lg bg-gradient-to-br',
                  isOpenAILike
                    ? 'from-green-500 to-green-600'
                    : isGemini
                      ? 'from-blue-500 to-blue-600'
                      : isAntigravity
                        ? 'from-purple-500 to-purple-600'
                        : 'from-orange-500 to-orange-600'
                ].join(' ')}
              >
                <Icon name="sparkles" size="md" className="text-white" />
              </div>
              <div>
                <span className="block font-semibold text-gray-900 dark:text-white">{account.name}</span>
                <span className="text-sm surface-muted">
                  {isOpenAI
                    ? t('admin.accounts.openaiAccount')
                    : isGemini
                      ? t('admin.accounts.geminiAccount')
                      : isAntigravity
                        ? t('admin.accounts.antigravityAccount')
                        : t('admin.accounts.claudeCodeAccount')}
                </span>
              </div>
            </div>
          </div>

          {/* Add Method Selection (Claude only) */}
          {isAnthropic && (
            <fieldset className="border-0 p-0">
              <legend className="input-label">{t('admin.accounts.oauth.authMethod')}</legend>
              <div className="mt-2 flex gap-4">
                <label className="flex cursor-pointer items-center">
                  <input
                    checked={addMethod === 'oauth'}
                    onChange={() => setAddMethod('oauth')}
                    type="radio"
                    value="oauth"
                    className="mr-2 text-primary-600 focus:ring-primary-500"
                  />
                  <span className="text-sm surface-muted">{t('admin.accounts.types.oauth')}</span>
                </label>
                <label className="flex cursor-pointer items-center">
                  <input
                    checked={addMethod === 'setup-token'}
                    onChange={() => setAddMethod('setup-token')}
                    type="radio"
                    value="setup-token"
                    className="mr-2 text-primary-600 focus:ring-primary-500"
                  />
                  <span className="text-sm surface-muted">{t('admin.accounts.setupTokenLongLived')}</span>
                </label>
              </div>
            </fieldset>
          )}

          {/* Gemini OAuth Type Display (read-only) */}
          {isGemini && (
            <div className="rounded-lg border border-gray-200 surface-inset p-4 dark:border-dark-600">
              <div className="mb-2 text-sm font-medium surface-muted">
                {t('admin.accounts.oauth.gemini.oauthTypeLabel')}
              </div>
              <div className="flex items-center gap-3">
                <div
                  className={[
                    'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg',
                    geminiOAuthType === 'google_one'
                      ? 'bg-purple-500 text-white'
                      : geminiOAuthType === 'code_assist'
                        ? 'bg-blue-500 text-white'
                        : 'bg-amber-500 text-white'
                  ].join(' ')}
                >
                  {geminiOAuthType === 'google_one' ? (
                    <Icon name="user" size="sm" />
                  ) : geminiOAuthType === 'code_assist' ? (
                    <Icon name="cloud" size="sm" />
                  ) : (
                    <Icon name="sparkles" size="sm" />
                  )}
                </div>
                <div>
                  <span className="block text-sm font-medium text-gray-900 dark:text-white">
                    {geminiOAuthType === 'google_one'
                      ? 'Google One'
                      : geminiOAuthType === 'code_assist'
                        ? t('admin.accounts.gemini.oauthType.builtInTitle')
                        : t('admin.accounts.gemini.oauthType.customTitle')}
                  </span>
                  <span className="text-xs surface-muted">
                    {geminiOAuthType === 'google_one'
                      ? t('admin.accounts.gemini.oauthType.googleOneDesc')
                      : geminiOAuthType === 'code_assist'
                        ? t('admin.accounts.gemini.oauthType.builtInDesc')
                        : t('admin.accounts.gemini.oauthType.customDesc')}
                  </span>
                </div>
              </div>
            </div>
          )}

          <OAuthAuthorizationFlow
            ref={oauthFlowRef}
            addMethod={addMethod}
            authUrl={currentAuthUrl}
            sessionId={currentSessionId}
            loading={currentLoading}
            error={currentError}
            showHelp={isAnthropic}
            showProxyWarning={isAnthropic}
            showCookieOption={isAnthropic}
            allowMultiple={false}
            methodLabel={t('admin.accounts.inputMethod')}
            platform={isOpenAI ? 'openai' : isGemini ? 'gemini' : isAntigravity ? 'antigravity' : 'anthropic'}
            showProjectId={isGemini && geminiOAuthType === 'code_assist'}
            onGenerateUrl={() => void handleGenerateUrl()}
            onExchangeCode={() => void handleExchangeCode()}
            onCookieAuth={(sessionKey) => void handleCookieAuth(sessionKey)}
            onValidateRefreshToken={() => undefined}
            onValidateMobileRefreshToken={() => undefined}
            onValidateSessionToken={() => undefined}
            onImportAccessToken={() => undefined}
            onImportCodexSession={() => undefined}
            onImportCodexPat={() => undefined}
            onImportSso={() => undefined}
            onAuthorizePassword={() => undefined}
            onStartDeviceCode={() => undefined}
            onInputMethodChange={() => undefined}
          />
        </div>
      )}

      <div className="flex justify-between gap-3">
        <button type="button" className="btn btn-secondary" onClick={handleClose}>
          {t('common.cancel')}
        </button>
        {isManualInputMethod && (
          <button type="button" disabled={!canExchangeCode} className="btn btn-primary" onClick={() => void handleExchangeCode()}>
            {currentLoading && (
              <svg className="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                <path
                  className="opacity-75"
                  fill="currentColor"
                  d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                />
              </svg>
            )}
            {currentLoading ? t('admin.accounts.oauth.verifying') : t('admin.accounts.oauth.completeAuth')}
          </button>
        )}
      </div>
    </Modal>
  )
}

export default ReAuthAccountModal
