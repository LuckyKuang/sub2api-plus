/**
 * Authentication Store
 * Manages user authentication state, login/logout, token refresh, and token persistence
 */

import { create } from 'zustand'
import { authAPI, isTotp2FARequired, passkeyAPI, type LoginResponse } from '@/api'
import { setAdminSupportContext } from '@/utils/adminSupportContext'
import { clearAvailableChannelCatalogCache } from '@/utils/availableChannelCatalogCache'
import { useAppStore } from './app'
import type {
  User,
  LoginRequest,
  RegisterRequest,
  AuthResponse,
  ActionCaptchaRequestProof
} from '@/types'

const AUTH_TOKEN_KEY = 'auth_token'
const AUTH_USER_KEY = 'auth_user'
const REFRESH_TOKEN_KEY = 'refresh_token'
const TOKEN_EXPIRES_AT_KEY = 'token_expires_at' // 存储过期时间戳而非有效期
const PENDING_AUTH_SESSION_KEY = 'pending_auth_session'
const AUTO_REFRESH_INTERVAL = 60 * 1000 // 60 seconds for user data refresh
const TOKEN_REFRESH_BUFFER = 120 * 1000 // 120 seconds before expiry to refresh token

type PendingAuthTokenField = 'pending_auth_token' | 'pending_oauth_token'

export interface PendingAuthSessionSummary {
  token: string
  token_field: PendingAuthTokenField
  provider: string
  redirect?: string
  adoption_required?: boolean
  suggested_display_name?: string
  suggested_avatar_url?: string
}

interface AuthState {
  user: User | null
  token: string | null
  /** Cached image-management capability driving the async/batch-image nav entries. */
  hasManageableImageKey: boolean
  /** Read-only for callers: derived from the authenticated user's profile. */
  runMode: 'standard' | 'simple'
  /** Read-only for callers: maintained through setPendingAuthSession / clearPendingAuthSession. */
  pendingAuthSession: PendingAuthSessionSummary | null

  // Computed
  isAuthenticated: () => boolean
  isAdmin: () => boolean
  isSimpleMode: () => boolean
  hasPendingAuthSession: () => boolean

  // Actions
  checkAuth: () => void
  startAutoRefresh: () => void
  stopAutoRefresh: () => void
  scheduleTokenRefreshAt: (expiresAtMs: number) => void
  scheduleTokenRefresh: (expiresInSeconds: number) => void
  performTokenRefresh: () => Promise<void>
  stopTokenRefresh: () => void
  login: (credentials: LoginRequest) => Promise<LoginResponse>
  login2FA: (tempToken: string, totpCode: string) => Promise<User>
  loginWithPasskey: (proof?: ActionCaptchaRequestProof) => Promise<User>
  register: (userData: RegisterRequest) => Promise<User>
  setToken: (newToken: string) => Promise<User>
  logout: () => Promise<void>
  refreshUser: () => Promise<User>
  setPendingAuthSession: (session: PendingAuthSessionSummary | null) => void
  clearPendingAuthSession: () => void
}

function normalizePendingAuthTokenField(value: unknown): PendingAuthTokenField {
  return value === 'pending_oauth_token' ? 'pending_oauth_token' : 'pending_auth_token'
}

function getPersistedPendingAuthSession(): PendingAuthSessionSummary | null {
  const raw = localStorage.getItem(PENDING_AUTH_SESSION_KEY)
  if (!raw) {
    return null
  }

  try {
    const parsed = JSON.parse(raw) as Partial<PendingAuthSessionSummary> | null
    const provider = typeof parsed?.provider === 'string' ? parsed.provider.trim() : ''
    if (!provider) {
      localStorage.removeItem(PENDING_AUTH_SESSION_KEY)
      return null
    }
    return {
      token: typeof parsed?.token === 'string' ? parsed.token : '',
      token_field: normalizePendingAuthTokenField(parsed?.token_field),
      provider,
      redirect: typeof parsed?.redirect === 'string' ? parsed.redirect : undefined,
      adoption_required: typeof parsed?.adoption_required === 'boolean' ? parsed.adoption_required : undefined,
      suggested_display_name: typeof parsed?.suggested_display_name === 'string' ? parsed.suggested_display_name : undefined,
      suggested_avatar_url: typeof parsed?.suggested_avatar_url === 'string' ? parsed.suggested_avatar_url : undefined
    }
  } catch {
    localStorage.removeItem(PENDING_AUTH_SESSION_KEY)
    return null
  }
}

function persistPendingAuthSession(session: PendingAuthSessionSummary): void {
  localStorage.setItem(PENDING_AUTH_SESSION_KEY, JSON.stringify(session))
}

function clearPendingAuthSessionStorage(): void {
  localStorage.removeItem(PENDING_AUTH_SESSION_KEY)
}

export const useAuthStore = create<AuthState>()((set, get) => {
  // Module-level session handles stay outside the reactive state, exactly like the Vue refs they replace.
  let refreshTokenValue: string | null = null
  let tokenExpiresAt: number | null = null // 过期时间戳（毫秒）
  let refreshIntervalId: ReturnType<typeof setInterval> | null = null
  let tokenRefreshTimeoutId: ReturnType<typeof setTimeout> | null = null

  /**
   * Commit a new user and keep the version cache coherent.
   * Replaces the Vue `watch(isAdmin, ...)`: losing admin rights drops the cached update check.
   */
  function commitUser(nextUser: User | null): void {
    const wasAdmin = get().isAdmin()
    set({ user: nextUser })
    if (wasAdmin && !get().isAdmin()) {
      useAppStore.getState().clearVersionCache()
    }
  }

  /**
   * Initialize auth state from localStorage
   * Call this on app startup to restore session
   * Also starts auto-refresh and immediately fetches latest user data
   */
  function checkAuth(): void {
    const savedToken = localStorage.getItem(AUTH_TOKEN_KEY)
    const savedUser = localStorage.getItem(AUTH_USER_KEY)
    const savedRefreshToken = localStorage.getItem(REFRESH_TOKEN_KEY)
    const savedExpiresAt = localStorage.getItem(TOKEN_EXPIRES_AT_KEY)
    set({ pendingAuthSession: getPersistedPendingAuthSession() })

    if (savedToken && savedUser) {
      try {
        set({
          token: savedToken,
          user: JSON.parse(savedUser) as User
        })
        refreshTokenValue = savedRefreshToken
        tokenExpiresAt = savedExpiresAt ? parseInt(savedExpiresAt, 10) : null

        // Immediately refresh user data from backend (async, don't block)
        refreshUser().catch((error) => {
          console.error('Failed to refresh user on init:', error)
        })

        // Start auto-refresh interval for user data
        startAutoRefresh()

        // Start proactive token refresh if we have refresh token and expiry info
        // Note: use !== null to handle case when tokenExpiresAt is 0 (expired)
        if (savedRefreshToken && tokenExpiresAt !== null) {
          scheduleTokenRefreshAt(tokenExpiresAt)
        }
      } catch (error) {
        console.error('Failed to parse saved user data:', error)
        clearAuth({ preservePendingAuthSession: true })
      }
    }
  }

  /**
   * Start auto-refresh interval for user data
   * Refreshes user data every 60 seconds
   */
  function startAutoRefresh(): void {
    // Clear existing interval if any
    stopAutoRefresh()

    refreshIntervalId = setInterval(() => {
      if (get().token) {
        refreshUser().catch((error) => {
          console.error('Auto-refresh user failed:', error)
        })
      }
    }, AUTO_REFRESH_INTERVAL)
  }

  /**
   * Stop auto-refresh interval
   */
  function stopAutoRefresh(): void {
    if (refreshIntervalId) {
      clearInterval(refreshIntervalId)
      refreshIntervalId = null
    }
  }

  /**
   * Schedule proactive token refresh before expiry (based on expiry timestamp)
   * @param expiresAtMs - Token expiry timestamp in milliseconds
   */
  function scheduleTokenRefreshAt(expiresAtMs: number): void {
    // Clear any existing timeout
    if (tokenRefreshTimeoutId) {
      clearTimeout(tokenRefreshTimeoutId)
      tokenRefreshTimeoutId = null
    }

    // Calculate remaining time until refresh (buffer time before expiry)
    const now = Date.now()
    const refreshInMs = Math.max(0, expiresAtMs - now - TOKEN_REFRESH_BUFFER)

    if (refreshInMs <= 0) {
      // Token is about to expire or already expired, refresh immediately
      performTokenRefresh()
      return
    }

    tokenRefreshTimeoutId = setTimeout(() => {
      performTokenRefresh()
    }, refreshInMs)
  }

  /**
   * Schedule proactive token refresh before expiry (based on expires_in seconds)
   * @param expiresInSeconds - Token expiry time in seconds from now
   */
  function scheduleTokenRefresh(expiresInSeconds: number): void {
    const expiresAtMs = Date.now() + expiresInSeconds * 1000
    tokenExpiresAt = expiresAtMs
    localStorage.setItem(TOKEN_EXPIRES_AT_KEY, String(expiresAtMs))
    scheduleTokenRefreshAt(expiresAtMs)
  }

  /**
   * Perform the actual token refresh
   */
  async function performTokenRefresh(): Promise<void> {
    if (!refreshTokenValue) {
      return
    }

    try {
      const response = await authAPI.refreshToken()

      // Update state
      refreshTokenValue = response.refresh_token
      set({ token: response.access_token })

      // Schedule next refresh (this also updates tokenExpiresAt and localStorage)
      scheduleTokenRefresh(response.expires_in)
    } catch (error) {
      console.error('Token refresh failed:', error)
      // Don't clear auth here - the interceptor will handle 401 errors
    }
  }

  /**
   * Stop token refresh timeout
   */
  function stopTokenRefresh(): void {
    if (tokenRefreshTimeoutId) {
      clearTimeout(tokenRefreshTimeoutId)
      tokenRefreshTimeoutId = null
    }
  }

  /**
   * User login
   * @param credentials - Login credentials (email and password)
   * @returns Promise resolving to the login response (may require 2FA)
   * @throws Error if login fails
   */
  async function login(credentials: LoginRequest): Promise<LoginResponse> {
    try {
      const response = await authAPI.login(credentials)

      // If 2FA is required, return the response without setting auth state
      if (isTotp2FARequired(response)) {
        return response
      }

      // Set auth state from the response
      setAuthFromResponse(response)

      return response
    } catch (error) {
      // Clear any partial state on error
      clearAuth({ preservePendingAuthSession: get().pendingAuthSession !== null })
      throw error
    }
  }

  /**
   * Complete login with 2FA code
   * @param tempToken - Temporary token from initial login
   * @param totpCode - 6-digit TOTP code
   * @returns Promise resolving to the authenticated user
   * @throws Error if 2FA verification fails
   */
  async function login2FA(tempToken: string, totpCode: string): Promise<User> {
    try {
      const response = await authAPI.login2FA({ temp_token: tempToken, totp_code: totpCode })
      setAuthFromResponse(response)
      return get().user!
    } catch (error) {
      clearAuth({ preservePendingAuthSession: get().pendingAuthSession !== null })
      throw error
    }
  }

  async function loginWithPasskey(proof?: ActionCaptchaRequestProof): Promise<User> {
    try {
      const response = await passkeyAPI.login(proof)
      setAuthFromResponse(response)
      return get().user!
    } catch (error) {
      clearAuth({ preservePendingAuthSession: get().pendingAuthSession !== null })
      throw error
    }
  }

  /**
   * Set auth state from an AuthResponse
   * Internal helper function
   */
  function setAuthFromResponse(response: AuthResponse): void {
    clearAvailableChannelCatalogCache()
    // Store token and user
    set({ token: response.access_token })

    // Store refresh token if present
    if (response.refresh_token) {
      refreshTokenValue = response.refresh_token
      localStorage.setItem(REFRESH_TOKEN_KEY, response.refresh_token)
    }

    // Extract run_mode if present
    if (response.user.run_mode) {
      set({ runMode: response.user.run_mode })
    }
    const { run_mode: _run_mode, ...userData } = response.user
    commitUser(userData)

    // Persist to localStorage
    localStorage.setItem(AUTH_TOKEN_KEY, response.access_token)
    localStorage.setItem(AUTH_USER_KEY, JSON.stringify(userData))
    clearPendingAuthSession()

    // Start auto-refresh interval for user data
    startAutoRefresh()

    // Start proactive token refresh if we have refresh token and expiry info
    // scheduleTokenRefresh will also store the expiry timestamp
    if (response.refresh_token && response.expires_in) {
      scheduleTokenRefresh(response.expires_in)
    }
  }

  /**
   * User registration
   * @param userData - Email/password and optional verification, CAPTCHA and referral fields
   * @returns Promise resolving to the newly registered and authenticated user
   * @throws Error if registration fails
   */
  async function register(userData: RegisterRequest): Promise<User> {
    try {
      const response = await authAPI.register(userData)

      // Use the common helper to set auth state
      setAuthFromResponse(response)

      return get().user!
    } catch (error) {
      // Clear any partial state on error
      clearAuth({ preservePendingAuthSession: get().pendingAuthSession !== null })
      throw error
    }
  }

  /**
   * 直接设置 token（用于 OAuth/SSO 回调），并加载当前用户信息。
   * 会自动读取 localStorage 中已设置的 refresh_token 和 token_expires_in
   * @param newToken - 后端签发的 JWT access token
   */
  async function setToken(newToken: string): Promise<User> {
    // Clear any previous state first (avoid mixing sessions)
    // Note: Don't clear localStorage here as OAuth callback may have set refresh_token
    stopAutoRefresh()
    stopTokenRefresh()
    set({ token: null })
    clearAvailableChannelCatalogCache()
    setAdminSupportContext(null)
    commitUser(null)

    set({ token: newToken })
    localStorage.setItem(AUTH_TOKEN_KEY, newToken)

    // Read refresh token and expires_at from localStorage if set by OAuth callback
    const savedRefreshToken = localStorage.getItem(REFRESH_TOKEN_KEY)
    const savedExpiresAt = localStorage.getItem(TOKEN_EXPIRES_AT_KEY)

    if (savedRefreshToken) {
      refreshTokenValue = savedRefreshToken
    }
    if (savedExpiresAt) {
      tokenExpiresAt = parseInt(savedExpiresAt, 10)
    }

    try {
      const userData = await refreshUser()
      startAutoRefresh()

      // Start proactive token refresh if we have refresh token and expiry info
      // Note: use !== null to handle case when tokenExpiresAt is 0 (expired)
      if (savedRefreshToken && tokenExpiresAt !== null) {
        scheduleTokenRefreshAt(tokenExpiresAt)
      }

      clearPendingAuthSession()
      return userData
    } catch (error) {
      clearAuth({ preservePendingAuthSession: get().pendingAuthSession !== null })
      throw error
    }
  }

  function setPendingAuthSession(session: PendingAuthSessionSummary | null): void {
    set({ pendingAuthSession: session })

    if (session) {
      persistPendingAuthSession(session)
      return
    }

    clearPendingAuthSessionStorage()
  }

  function clearPendingAuthSession(): void {
    setPendingAuthSession(null)
  }

  /**
   * User logout
   * Clears all authentication state and persisted data
   */
  async function logout(): Promise<void> {
    try {
      // Call API logout (revokes refresh token on server)
      await authAPI.logout()
    } catch (err) {
      // 服务端吊销失败（网络/5xx/超时）不应阻止本地登出，否则用户点了退出仍处于登录态。
      console.warn('Logout API call failed, clearing local session anyway', err)
    } finally {
      // Always clear local state (tokens, user data, refresh timers)
      clearAuth()
    }
  }

  /**
   * Refresh current user data
   * Fetches latest user info from the server
   * @returns Promise resolving to the updated user
   * @throws Error if not authenticated or request fails
   */
  async function refreshUser(): Promise<User> {
    if (!get().token) {
      throw new Error('Not authenticated')
    }

    try {
      const response = await authAPI.getCurrentUser()
      if (response.data.run_mode) {
        set({ runMode: response.data.run_mode })
      }
      const { run_mode: _run_mode, ...userData } = response.data
      commitUser(userData)

      // Update localStorage
      localStorage.setItem(AUTH_USER_KEY, JSON.stringify(userData))

      return userData
    } catch (error) {
      // If refresh fails with 401, clear auth state
      if ((error as { status?: number }).status === 401) {
        clearAuth({ preservePendingAuthSession: get().pendingAuthSession !== null })
      }
      throw error
    }
  }

  /**
   * Clear all authentication state
   * Internal helper function
   */
  function clearAuth(options?: { preservePendingAuthSession?: boolean }): void {
    // Stop auto-refresh
    stopAutoRefresh()
    // Stop token refresh
    stopTokenRefresh()

    set({ token: null })
    clearAvailableChannelCatalogCache()
    refreshTokenValue = null
    tokenExpiresAt = null
    setAdminSupportContext(null)
    commitUser(null)
    localStorage.removeItem(AUTH_TOKEN_KEY)
    localStorage.removeItem(AUTH_USER_KEY)
    localStorage.removeItem(REFRESH_TOKEN_KEY)
    localStorage.removeItem(TOKEN_EXPIRES_AT_KEY)

    if (options?.preservePendingAuthSession) {
      set({ pendingAuthSession: getPersistedPendingAuthSession() })
      return
    }

    set({ pendingAuthSession: null })
    clearPendingAuthSessionStorage()
  }

  return {
    // State
    user: null,
    token: null,
    hasManageableImageKey: false,
    runMode: 'standard',
    pendingAuthSession: null,

    // Computed
    isAuthenticated: () => !!get().token && !!get().user,
    isAdmin: () => get().user?.role === 'admin',
    isSimpleMode: () => get().runMode === 'simple',
    hasPendingAuthSession: () => get().pendingAuthSession !== null,

    // Actions
    checkAuth,
    startAutoRefresh,
    stopAutoRefresh,
    scheduleTokenRefreshAt,
    scheduleTokenRefresh,
    performTokenRefresh,
    stopTokenRefresh,
    login,
    login2FA,
    loginWithPasskey,
    register,
    setToken,
    logout,
    refreshUser,
    setPendingAuthSession,
    clearPendingAuthSession
  }
})
