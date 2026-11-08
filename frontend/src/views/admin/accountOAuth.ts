import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { adminAPI } from '@/api'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage, extractI18nErrorMessage } from '@/utils/apiError'

export type AddMethod = 'oauth' | 'setup-token'
export type AuthInputMethod =
  | 'manual'
  | 'device_code'
  | 'cookie'
  | 'refresh_token'
  | 'mobile_refresh_token'
  | 'session_token'
  | 'access_token'
  | 'codex_session'
  | 'agent_identity'
  | 'codex_pat'
  | 'sso_cookie'
  | 'email_password'

export interface TokenInfo {
  org_uuid?: string
  account_uuid?: string
  email_address?: string
  [key: string]: unknown
}

export interface OpenAITokenInfo {
  access_token?: string
  refresh_token?: string
  client_id?: string
  id_token?: string
  token_type?: string
  expires_in?: number
  expires_at?: number
  scope?: string
  email?: string
  name?: string
  plan_type?: string
  subscription_expires_at?: string
  privacy_mode?: string
  // OpenAI specific IDs (extracted from ID Token)
  chatgpt_account_id?: string
  chatgpt_user_id?: string
  organization_id?: string
  [key: string]: unknown
}

export interface GeminiTokenInfo {
  access_token?: string
  refresh_token?: string
  token_type?: string
  scope?: string
  expires_at?: number | string
  project_id?: string
  oauth_type?: string
  tier_id?: string
  extra?: Record<string, unknown>
  [key: string]: unknown
}

interface OAuthBaseState {
  authUrl: string
  sessionId: string
  loading: boolean
  error: string
}

function useOAuthErrorReporter() {
  const showError = useAppStore((state) => state.showError)
  return showError
}

/** Claude (Anthropic) OAuth / setup-token / cookie authorization. */
export function useAccountOAuth() {
  const showError = useOAuthErrorReporter()
  const [authUrl, setAuthUrl] = useState('')
  const [authCode, setAuthCode] = useState('')
  const [sessionId, setSessionId] = useState('')
  const [sessionKey, setSessionKey] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const resetState = useCallback(() => {
    setAuthUrl('')
    setAuthCode('')
    setSessionId('')
    setSessionKey('')
    setLoading(false)
    setError('')
  }, [])

  const generateAuthUrl = useCallback(
    async (addMethod: AddMethod, proxyId?: number | null): Promise<boolean> => {
      setLoading(true)
      setAuthUrl('')
      setSessionId('')
      setError('')

      try {
        const proxyConfig = proxyId ? { proxy_id: proxyId } : {}
        const endpoint =
          addMethod === 'oauth'
            ? '/admin/accounts/generate-auth-url'
            : '/admin/accounts/generate-setup-token-url'

        const response = await adminAPI.accounts.generateAuthUrl(endpoint, proxyConfig)
        setAuthUrl(response.auth_url)
        setSessionId(response.session_id)
        return true
      } catch (err) {
        const message = (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail || 'Failed to generate auth URL'
        setError(message)
        showError(message)
        return false
      } finally {
        setLoading(false)
      }
    },
    [showError]
  )

  const exchangeAuthCode = useCallback(
    async (addMethod: AddMethod, proxyId?: number | null): Promise<TokenInfo | null> => {
      if (!authCode.trim() || !sessionId) {
        setError('Missing auth code or session ID')
        return null
      }

      setLoading(true)
      setError('')

      try {
        const proxyConfig = proxyId ? { proxy_id: proxyId } : {}
        const endpoint =
          addMethod === 'oauth' ? '/admin/accounts/exchange-code' : '/admin/accounts/exchange-setup-token-code'

        const tokenInfo = await adminAPI.accounts.exchangeCode(endpoint, {
          session_id: sessionId,
          code: authCode.trim(),
          ...proxyConfig
        })

        return tokenInfo as TokenInfo
      } catch (err) {
        const message = (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail || 'Failed to exchange auth code'
        setError(message)
        showError(message)
        return null
      } finally {
        setLoading(false)
      }
    },
    [authCode, sessionId, showError]
  )

  const cookieAuth = useCallback(
    async (addMethod: AddMethod, sessionKeyValue: string, proxyId?: number | null): Promise<TokenInfo | null> => {
      if (!sessionKeyValue.trim()) {
        setError('Please enter sessionKey')
        return null
      }

      setLoading(true)
      setError('')

      try {
        const proxyConfig = proxyId ? { proxy_id: proxyId } : {}
        const endpoint =
          addMethod === 'oauth' ? '/admin/accounts/cookie-auth' : '/admin/accounts/setup-token-cookie-auth'

        const tokenInfo = await adminAPI.accounts.exchangeCode(endpoint, {
          session_id: '',
          code: sessionKeyValue.trim(),
          ...proxyConfig
        })

        return tokenInfo as TokenInfo
      } catch (err) {
        setError((err as { response?: { data?: { detail?: string } } })?.response?.data?.detail || 'Cookie authorization failed')
        return null
      } finally {
        setLoading(false)
      }
    },
    []
  )

  const parseSessionKeys = (input: string): string[] => {
    return input
      .split('\n')
      .map((k) => k.trim())
      .filter((k) => k)
  }

  const buildExtraInfo = (tokenInfo: TokenInfo): Record<string, string> | undefined => {
    const extra: Record<string, string> = {}
    if (tokenInfo.org_uuid) extra.org_uuid = tokenInfo.org_uuid
    if (tokenInfo.account_uuid) extra.account_uuid = tokenInfo.account_uuid
    if (tokenInfo.email_address) extra.email_address = tokenInfo.email_address
    return Object.keys(extra).length > 0 ? extra : undefined
  }

  return {
    authUrl,
    authCode,
    sessionId,
    sessionKey,
    loading,
    error,
    setAuthCode,
    setSessionKey,
    setLoading,
    resetState,
    generateAuthUrl,
    exchangeAuthCode,
    cookieAuth,
    parseSessionKeys,
    buildExtraInfo
  }
}

export interface OpenAIOAuthController extends OAuthBaseState {
  oauthState: string
  resetState: () => void
  generateAuthUrl: (proxyId?: number | null, redirectUri?: string, accountId?: number) => Promise<boolean>
  startDeviceCode: (
    proxyId?: number | null
  ) => Promise<{ session_id: string; user_code: string; verification_url: string; interval_seconds: number } | null>
  pollDeviceCode: (currentSessionId: string) => Promise<OpenAITokenInfo | { pending: true } | null>
  exchangeAuthCode: (code: string, currentSessionId: string, state: string, proxyId?: number | null) => Promise<OpenAITokenInfo | null>
  validateRefreshToken: (refreshToken: string, proxyId?: number | null, clientId?: string) => Promise<OpenAITokenInfo | null>
  buildCredentials: (tokenInfo: OpenAITokenInfo) => Record<string, unknown>
  buildExtraInfo: (tokenInfo: OpenAITokenInfo) => Record<string, string> | undefined
}

/** OpenAI (Codex) OAuth authorization. */
export function useOpenAIOAuth(): OpenAIOAuthController {
  const showError = useOAuthErrorReporter()
  const { t } = useTranslation()
  const endpointPrefix = '/admin/openai'

  const [authUrl, setAuthUrl] = useState('')
  const [sessionId, setSessionId] = useState('')
  const [oauthState, setOAuthState] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const generation = useRef(0)

  // A reset revokes pending results as well as clearing visible state.
  const resetState = useCallback(() => {
    generation.current += 1
    setAuthUrl('')
    setSessionId('')
    setOAuthState('')
    setLoading(false)
    setError('')
  }, [])

  useEffect(() => resetState, [resetState])

  const generateAuthUrl = useCallback(
    async (proxyId?: number | null, redirectUri?: string, accountId?: number): Promise<boolean> => {
      const run = ++generation.current
      setLoading(true)
      setAuthUrl('')
      setSessionId('')
      setOAuthState('')
      setError('')

      try {
        const payload: Record<string, unknown> = {}
        if (proxyId) payload.proxy_id = proxyId
        if (redirectUri) payload.redirect_uri = redirectUri
        if (accountId) payload.account_id = accountId

        const response = await adminAPI.accounts.generateAuthUrl(`${endpointPrefix}/generate-auth-url`, payload)
        if (run !== generation.current) return false
        setAuthUrl(response.auth_url)
        setSessionId(response.session_id)
        try {
          const parsed = new URL(response.auth_url)
          setOAuthState(parsed.searchParams.get('state') || '')
        } catch {
          setOAuthState('')
        }
        return true
      } catch (err) {
        if (run !== generation.current) return false
        const message = extractApiErrorMessage(err, t('admin.accounts.oauth.openai.failedToGenerateUrl'))
        setError(message)
        showError(message)
        return false
      } finally {
        if (run === generation.current) setLoading(false)
      }
    },
    [showError, t]
  )

  const startDeviceCode = useCallback(
    async (proxyId?: number | null) => {
      const run = ++generation.current
      setLoading(true)
      setError('')
      try {
        const payload: { proxy_id?: number } = {}
        if (proxyId) payload.proxy_id = proxyId
        const result = await adminAPI.accounts.startOpenAIDeviceCode(`${endpointPrefix}/device-code/start`, payload)
        if (run !== generation.current) return null
        setSessionId(result.session_id)
        return result
      } catch (err) {
        if (run !== generation.current) return null
        const message = extractApiErrorMessage(err, t('admin.accounts.oauth.openai.failedToStartDeviceCode'))
        setError(message)
        showError(message)
        return null
      } finally {
        if (run === generation.current) setLoading(false)
      }
    },
    [showError, t]
  )

  const pollDeviceCode = useCallback(
    async (currentSessionId: string): Promise<OpenAITokenInfo | { pending: true } | null> => {
      const run = generation.current
      setError('')
      try {
        const result = await adminAPI.accounts.pollOpenAIDeviceCode(`${endpointPrefix}/device-code/poll`, currentSessionId)
        if (run !== generation.current) return null
        if (result && result.pending === true) {
          return { pending: true }
        }
        return result as OpenAITokenInfo
      } catch (err) {
        if (run !== generation.current) return null
        const message = extractI18nErrorMessage(
          err,
          t,
          'admin.accounts.oauth.openai.errors',
          t('admin.accounts.oauth.openai.failedToPollDeviceCode')
        )
        setError(message)
        showError(message)
        return null
      }
    },
    [showError, t]
  )

  const exchangeAuthCode = useCallback(
    async (code: string, currentSessionId: string, state: string, proxyId?: number | null): Promise<OpenAITokenInfo | null> => {
      if (!code.trim() || !currentSessionId || !state.trim()) {
        setError('Missing auth code, session ID, or state')
        return null
      }

      const run = ++generation.current
      setLoading(true)
      setError('')

      try {
        const payload: { session_id: string; code: string; state: string; proxy_id?: number } = {
          session_id: currentSessionId,
          code: code.trim(),
          state: state.trim()
        }
        if (proxyId) payload.proxy_id = proxyId

        const tokenInfo = await adminAPI.accounts.exchangeCode(`${endpointPrefix}/exchange-code`, payload)
        return run === generation.current ? (tokenInfo as OpenAITokenInfo) : null
      } catch (err) {
        if (run !== generation.current) return null
        const message = extractI18nErrorMessage(
          err,
          t,
          'admin.accounts.oauth.openai.errors',
          t('admin.accounts.oauth.openai.failedToExchangeCode')
        )
        setError(message)
        showError(message)
        return null
      } finally {
        if (run === generation.current) setLoading(false)
      }
    },
    [showError, t]
  )

  // Validate refresh token and get full token info
  // clientId: 指定 OAuth client_id（用于第三方渠道获取的 RT，如 app_LlGpXReQgckcGGUo2JrYvtJK）
  const validateRefreshToken = useCallback(
    async (refreshToken: string, proxyId?: number | null, clientId?: string): Promise<OpenAITokenInfo | null> => {
      if (!refreshToken.trim()) {
        setError('Missing refresh token')
        return null
      }

      const run = ++generation.current
      setLoading(true)
      setError('')

      try {
        const tokenInfo = await adminAPI.accounts.refreshOpenAIToken(
          refreshToken.trim(),
          proxyId,
          `${endpointPrefix}/refresh-token`,
          clientId
        )
        return run === generation.current ? (tokenInfo as OpenAITokenInfo) : null
      } catch (err) {
        if (run !== generation.current) return null
        const message = extractI18nErrorMessage(
          err,
          t,
          'admin.accounts.oauth.openai.errors',
          t('admin.accounts.oauth.openai.failedToValidateRT')
        )
        setError(message)
        showError(message)
        return null
      } finally {
        if (run === generation.current) setLoading(false)
      }
    },
    [showError, t]
  )

  // Build credentials for OpenAI OAuth account (aligned with backend BuildAccountCredentials)
  const buildCredentials = (tokenInfo: OpenAITokenInfo): Record<string, unknown> => {
    const creds: Record<string, unknown> = {
      access_token: tokenInfo.access_token,
      expires_at: tokenInfo.expires_at
    }

    // 仅在返回了新的 refresh_token 时才写入，防止用空值覆盖已有令牌
    if (tokenInfo.refresh_token) creds.refresh_token = tokenInfo.refresh_token
    if (tokenInfo.id_token) creds.id_token = tokenInfo.id_token
    if (tokenInfo.email) creds.email = tokenInfo.email
    if (tokenInfo.chatgpt_account_id) creds.chatgpt_account_id = tokenInfo.chatgpt_account_id
    if (tokenInfo.chatgpt_user_id) creds.chatgpt_user_id = tokenInfo.chatgpt_user_id
    if (tokenInfo.organization_id) creds.organization_id = tokenInfo.organization_id
    if (tokenInfo.plan_type) creds.plan_type = tokenInfo.plan_type
    if (tokenInfo.subscription_expires_at) creds.subscription_expires_at = tokenInfo.subscription_expires_at
    if (tokenInfo.client_id) creds.client_id = tokenInfo.client_id

    return creds
  }

  const buildExtraInfo = (tokenInfo: OpenAITokenInfo): Record<string, string> | undefined => {
    const extra: Record<string, string> = {}
    if (tokenInfo.email) extra.email = tokenInfo.email
    if (tokenInfo.name) extra.name = tokenInfo.name
    if (tokenInfo.privacy_mode) extra.privacy_mode = tokenInfo.privacy_mode
    return Object.keys(extra).length > 0 ? extra : undefined
  }

  return {
    authUrl,
    sessionId,
    oauthState,
    loading,
    error,
    resetState,
    generateAuthUrl,
    startDeviceCode,
    pollDeviceCode,
    exchangeAuthCode,
    validateRefreshToken,
    buildCredentials,
    buildExtraInfo
  }
}

export interface GeminiOAuthController extends OAuthBaseState {
  state: string
  resetState: () => void
  generateAuthUrl: (
    proxyId: number | null | undefined,
    projectId?: string | null,
    oauthType?: string,
    tierId?: string
  ) => Promise<boolean>
  exchangeAuthCode: (params: {
    code: string
    sessionId: string
    state: string
    proxyId?: number | null
    oauthType?: string
    tierId?: string
  }) => Promise<GeminiTokenInfo | null>
  buildCredentials: (tokenInfo: GeminiTokenInfo) => Record<string, unknown>
  buildExtraInfo: (tokenInfo: GeminiTokenInfo) => Record<string, unknown> | undefined
  getCapabilities: () => Promise<unknown>
}

/** Gemini OAuth authorization. */
export function useGeminiOAuth(): GeminiOAuthController {
  const showError = useOAuthErrorReporter()
  const { t } = useTranslation()

  const [authUrl, setAuthUrl] = useState('')
  const [sessionId, setSessionId] = useState('')
  const [state, setState] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const resetState = useCallback(() => {
    setAuthUrl('')
    setSessionId('')
    setState('')
    setLoading(false)
    setError('')
  }, [])

  const generateAuthUrl = useCallback(
    async (proxyId: number | null | undefined, projectId?: string | null, oauthType?: string, tierId?: string): Promise<boolean> => {
      setLoading(true)
      setAuthUrl('')
      setSessionId('')
      setState('')
      setError('')

      try {
        const payload: Record<string, unknown> = {}
        if (proxyId) payload.proxy_id = proxyId
        const trimmedProjectID = projectId?.trim()
        if (trimmedProjectID) payload.project_id = trimmedProjectID
        if (oauthType) payload.oauth_type = oauthType
        const trimmedTierID = tierId?.trim()
        if (trimmedTierID) payload.tier_id = trimmedTierID

        const response = await adminAPI.gemini.generateAuthUrl(payload)
        setAuthUrl(response.auth_url)
        setSessionId(response.session_id)
        setState(response.state)
        return true
      } catch (err) {
        const message = extractApiErrorMessage(err, t('admin.accounts.oauth.gemini.failedToGenerateUrl'))
        setError(message)
        showError(message)
        return false
      } finally {
        setLoading(false)
      }
    },
    [showError, t]
  )

  const exchangeAuthCode = useCallback(
    async (params: {
      code: string
      sessionId: string
      state: string
      proxyId?: number | null
      oauthType?: string
      tierId?: string
    }): Promise<GeminiTokenInfo | null> => {
      const code = params.code?.trim()
      if (!code || !params.sessionId || !params.state) {
        setError(t('admin.accounts.oauth.gemini.missingExchangeParams'))
        return null
      }

      setLoading(true)
      setError('')

      try {
        const payload: Record<string, unknown> = {
          session_id: params.sessionId,
          state: params.state,
          code
        }
        if (params.proxyId) payload.proxy_id = params.proxyId
        if (params.oauthType) payload.oauth_type = params.oauthType
        const trimmedTierID = params.tierId?.trim()
        if (trimmedTierID) payload.tier_id = trimmedTierID

        const tokenInfo = await adminAPI.gemini.exchangeCode(payload as unknown as Parameters<typeof adminAPI.gemini.exchangeCode>[0])
        return tokenInfo as GeminiTokenInfo
      } catch (err) {
        // Check for specific missing project_id error
        const errorMessage = (err as { message?: string; response?: { data?: { message?: string } } })?.message ||
          (err as { response?: { data?: { message?: string } } })?.response?.data?.message ||
          ''
        if (errorMessage.includes('missing project_id')) {
          setError(t('admin.accounts.oauth.gemini.missingProjectId'))
        } else {
          setError(errorMessage || t('admin.accounts.oauth.gemini.failedToExchangeCode'))
        }
        showError(error)
        return null
      } finally {
        setLoading(false)
      }
    },
    [showError, t]
  )

  const buildCredentials = (tokenInfo: GeminiTokenInfo): Record<string, unknown> => {
    let expiresAt: string | undefined
    if (typeof tokenInfo.expires_at === 'number' && Number.isFinite(tokenInfo.expires_at)) {
      expiresAt = Math.floor(tokenInfo.expires_at).toString()
    } else if (typeof tokenInfo.expires_at === 'string' && tokenInfo.expires_at.trim()) {
      expiresAt = tokenInfo.expires_at.trim()
    }

    return {
      access_token: tokenInfo.access_token,
      refresh_token: tokenInfo.refresh_token,
      token_type: tokenInfo.token_type,
      expires_at: expiresAt,
      scope: tokenInfo.scope,
      project_id: tokenInfo.project_id,
      oauth_type: tokenInfo.oauth_type,
      tier_id: tokenInfo.tier_id
    }
  }

  const buildExtraInfo = (tokenInfo: GeminiTokenInfo): Record<string, unknown> | undefined => {
    if (!tokenInfo.extra || typeof tokenInfo.extra !== 'object') return undefined
    return tokenInfo.extra
  }

  const getCapabilities = async (): Promise<unknown> => {
    try {
      return await adminAPI.gemini.getCapabilities()
    } catch {
      // Capabilities are optional for older servers; don't block the UI.
      return null
    }
  }

  return {
    authUrl,
    sessionId,
    state,
    loading,
    error,
    resetState,
    generateAuthUrl,
    exchangeAuthCode,
    buildCredentials,
    buildExtraInfo,
    getCapabilities
  }
}

export interface AntigravityOAuthController extends OAuthBaseState {
  state: string
  resetState: () => void
  generateAuthUrl: (proxyId: number | null | undefined) => Promise<boolean>
  exchangeAuthCode: (params: {
    code: string
    sessionId: string
    state: string
    proxyId?: number | null
  }) => Promise<Record<string, unknown> | null>
  validateRefreshToken: (refreshToken: string, proxyId?: number | null) => Promise<Record<string, unknown> | null>
  buildCredentials: (tokenInfo: Record<string, unknown>, fallbackRefreshToken?: string) => Record<string, unknown>
}

/** Antigravity OAuth authorization. */
export function useAntigravityOAuth(): AntigravityOAuthController {
  const showError = useOAuthErrorReporter()
  const { t } = useTranslation()

  const [authUrl, setAuthUrl] = useState('')
  const [sessionId, setSessionId] = useState('')
  const [state, setState] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const resetState = useCallback(() => {
    setAuthUrl('')
    setSessionId('')
    setState('')
    setLoading(false)
    setError('')
  }, [])

  const generateAuthUrl = useCallback(
    async (proxyId: number | null | undefined): Promise<boolean> => {
      setLoading(true)
      setAuthUrl('')
      setSessionId('')
      setState('')
      setError('')

      try {
        const payload: Record<string, unknown> = {}
        if (proxyId) payload.proxy_id = proxyId

        const response = await adminAPI.antigravity.generateAuthUrl(payload)
        setAuthUrl(response.auth_url)
        setSessionId(response.session_id)
        setState(response.state)
        return true
      } catch (err) {
        const message =
          (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          t('admin.accounts.oauth.antigravity.failedToGenerateUrl')
        setError(message)
        showError(message)
        return false
      } finally {
        setLoading(false)
      }
    },
    [showError, t]
  )

  const exchangeAuthCode = useCallback(
    async (params: { code: string; sessionId: string; state: string; proxyId?: number | null }) => {
      const code = params.code?.trim()
      if (!code || !params.sessionId || !params.state) {
        setError(t('admin.accounts.oauth.antigravity.missingExchangeParams'))
        return null
      }

      setLoading(true)
      setError('')

      try {
        const payload: Record<string, unknown> = {
          session_id: params.sessionId,
          state: params.state,
          code
        }
        if (params.proxyId) payload.proxy_id = params.proxyId

        return await adminAPI.antigravity.exchangeCode(payload as unknown as Parameters<typeof adminAPI.antigravity.exchangeCode>[0])
      } catch (err) {
        const message =
          (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          t('admin.accounts.oauth.antigravity.failedToExchangeCode')
        setError(message)
        showError(message)
        return null
      } finally {
        setLoading(false)
      }
    },
    [showError, t]
  )

  const validateRefreshToken = useCallback(
    async (refreshToken: string, proxyId?: number | null) => {
      if (!refreshToken.trim()) {
        setError(t('admin.accounts.oauth.antigravity.pleaseEnterRefreshToken'))
        return null
      }

      setLoading(true)
      setError('')

      try {
        return await adminAPI.antigravity.refreshAntigravityToken(refreshToken.trim(), proxyId)
      } catch (err) {
        setError(
          (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
            t('admin.accounts.oauth.antigravity.failedToValidateRT')
        )
        // Don't show global error toast for batch validation to avoid spamming
        return null
      } finally {
        setLoading(false)
      }
    },
    [t]
  )

  const buildCredentials = (tokenInfo: Record<string, unknown>, fallbackRefreshToken?: string): Record<string, unknown> => {
    let expiresAt: string | undefined
    const rawExpires = tokenInfo.expires_at
    if (typeof rawExpires === 'number' && Number.isFinite(rawExpires)) {
      expiresAt = Math.floor(rawExpires).toString()
    } else if (typeof rawExpires === 'string' && rawExpires.trim()) {
      expiresAt = rawExpires.trim()
    }
    const refreshTokenRaw = typeof tokenInfo.refresh_token === 'string' ? tokenInfo.refresh_token.trim() : ''
    const refreshToken = refreshTokenRaw ? tokenInfo.refresh_token : fallbackRefreshToken

    return {
      access_token: tokenInfo.access_token,
      refresh_token: refreshToken,
      token_type: tokenInfo.token_type,
      expires_at: expiresAt,
      project_id: tokenInfo.project_id,
      email: tokenInfo.email,
      ...(tokenInfo.plan_type ? { plan_type: tokenInfo.plan_type } : {})
    }
  }

  return {
    authUrl,
    sessionId,
    state,
    loading,
    error,
    resetState,
    generateAuthUrl,
    exchangeAuthCode,
    validateRefreshToken,
    buildCredentials
  }
}
