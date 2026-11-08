/**
 * Application State Store
 * Manages global UI state including sidebar, loading indicators, and toast notifications
 */

import { create } from 'zustand'
import i18n from '@/i18n'
import type { Toast, ToastType, PublicSettings } from '@/types'
import {
  checkUpdates as checkUpdatesAPI,
  type VersionInfo,
  type ReleaseInfo
} from '@/api/admin/system'
import { getPublicSettings as fetchPublicSettingsAPI } from '@/api/auth'
import { defaultConsoleAppearance, type ConsoleAppearance } from '@/types/appearance'

export type ChannelsLayout = 'list' | 'card'

interface AppState {
  // State
  sidebarCollapsed: boolean
  mobileOpen: boolean
  sidebarScrollTop: number
  loading: boolean
  toasts: Toast[]

  // Public settings state
  publicSettingsLoaded: boolean
  publicSettingsLoading: boolean
  siteName: string
  siteLogo: string
  siteVersion: string
  contactInfo: string
  apiBaseUrl: string
  docUrl: string
  cachedPublicSettings: PublicSettings | null
  consoleAppearancePreview: ConsoleAppearance | null
  consoleAppearance: ConsoleAppearance

  // Version state
  versionLoaded: boolean
  versionLoading: boolean
  currentVersion: string
  latestVersion: string
  hasUpdate: boolean
  buildType: string
  releaseInfo: ReleaseInfo | null

  // Computed
  hasActiveToasts: () => boolean
  availableChannelsLayout: () => ChannelsLayout
  backendModeEnabled: () => boolean

  // Actions
  toggleSidebar: () => void
  setSidebarCollapsed: (collapsed: boolean) => void
  toggleMobileSidebar: () => void
  setMobileOpen: (open: boolean) => void
  setLoading: (isLoading: boolean) => void
  showToast: (type: ToastType, message: string, duration?: number) => string
  showSuccess: (message: string, duration?: number) => string
  showError: (message: string, duration?: number) => string
  showInfo: (message: string, duration?: number) => string
  showWarning: (message: string, duration?: number) => string
  hideToast: (id: string) => void
  clearAllToasts: () => void
  withLoading: <T>(operation: () => Promise<T>) => Promise<T>
  withLoadingAndError: <T>(operation: () => Promise<T>, errorMessage?: string) => Promise<T | null>
  reset: () => void

  // Version actions
  fetchVersion: (force?: boolean) => Promise<VersionInfo | null>
  invalidateVersionCheck: () => void
  clearVersionCache: () => void

  // Public settings actions
  fetchPublicSettings: (force?: boolean) => Promise<PublicSettings | null>
  clearPublicSettingsCache: () => void
  initFromInjectedConfig: () => boolean
  applyConsoleAppearance: (appearance: ConsoleAppearance) => void
}

export const useAppStore = create<AppState>()((set, get) => {
  // Public settings request deduplication: one in-flight request shared by every caller.
  let publicSettingsRequest: Promise<PublicSettings | null> | null = null
  let publicSettingsRequestId = 0

  // Version cache state
  let versionRequestId = 0

  // Auto-incrementing ID for toasts
  let toastIdCounter = 0

  // Loading depth counter kept out of the public store surface, like the Vue ref it replaces.
  let loadingCount = 0

  // ==================== Actions ====================

  /**
   * Toggle sidebar collapsed state
   */
  function toggleSidebar(): void {
    set({ sidebarCollapsed: !get().sidebarCollapsed })
  }

  /**
   * Set sidebar collapsed state explicitly
   * @param collapsed - Whether sidebar should be collapsed
   */
  function setSidebarCollapsed(collapsed: boolean): void {
    set({ sidebarCollapsed: collapsed })
  }

  /**
   * Toggle mobile sidebar open state
   */
  function toggleMobileSidebar(): void {
    set({ mobileOpen: !get().mobileOpen })
  }

  /**
   * Set mobile sidebar open state explicitly
   * @param open - Whether mobile sidebar should be open
   */
  function setMobileOpen(open: boolean): void {
    set({ mobileOpen: open })
  }

  /**
   * Set global loading state
   * @param isLoading - Whether app is in loading state
   */
  function setLoading(isLoading: boolean): void {
    if (isLoading) {
      loadingCount++
    } else {
      loadingCount = Math.max(0, loadingCount - 1)
    }
    set({ loading: loadingCount > 0 })
  }

  /**
   * Show a toast notification
   * @param type - Type of toast (success, error, info, warning)
   * @param message - Toast message content
   * @param duration - Auto-dismiss duration in ms (undefined = no auto-dismiss)
   * @returns Toast ID for manual dismissal
   */
  function showToast(type: ToastType, message: string, duration?: number): string {
    const id = `toast-${++toastIdCounter}`
    const toast: Toast = {
      id,
      type,
      message,
      duration,
      startTime: duration !== undefined ? Date.now() : undefined
    }

    set({ toasts: [...get().toasts, toast] })

    // Auto-dismiss if duration is specified
    if (duration !== undefined) {
      setTimeout(() => {
        hideToast(id)
      }, duration)
    }

    return id
  }

  /**
   * Show a success toast
   * @param message - Success message
   * @param duration - Auto-dismiss duration in ms (default: 3000)
   */
  function showSuccess(message: string, duration: number = 3000): string {
    return showToast('success', message, duration)
  }

  /**
   * Show an error toast
   * @param message - Error message
   * @param duration - Auto-dismiss duration in ms (default: 5000)
   */
  function showError(message: string, duration: number = 5000): string {
    return showToast('error', message, duration)
  }

  /**
   * Show an info toast
   * @param message - Info message
   * @param duration - Auto-dismiss duration in ms (default: 3000)
   */
  function showInfo(message: string, duration: number = 3000): string {
    return showToast('info', message, duration)
  }

  /**
   * Show a warning toast
   * @param message - Warning message
   * @param duration - Auto-dismiss duration in ms (default: 4000)
   */
  function showWarning(message: string, duration: number = 4000): string {
    return showToast('warning', message, duration)
  }

  /**
   * Hide a specific toast by ID
   * @param id - Toast ID to hide
   */
  function hideToast(id: string): void {
    const index = get().toasts.findIndex((t) => t.id === id)
    if (index !== -1) {
      set({ toasts: get().toasts.filter((t) => t.id !== id) })
    }
  }

  /**
   * Clear all toasts
   */
  function clearAllToasts(): void {
    set({ toasts: [] })
  }

  /**
   * Execute an async operation with loading state
   * Automatically manages loading indicator
   * @param operation - Async operation to execute
   * @returns Promise resolving to operation result
   */
  async function withLoading<T>(operation: () => Promise<T>): Promise<T> {
    setLoading(true)
    try {
      return await operation()
    } finally {
      setLoading(false)
    }
  }

  /**
   * Execute an async operation with loading and error handling
   * Shows error toast on failure
   * @param operation - Async operation to execute
   * @param errorMessage - Custom error message (optional)
   * @returns Promise resolving to operation result or null on error
   */
  async function withLoadingAndError<T>(
    operation: () => Promise<T>,
    errorMessage?: string
  ): Promise<T | null> {
    setLoading(true)
    try {
      return await operation()
    } catch (error) {
      const message =
        errorMessage ||
        (error as { message?: string }).message ||
        i18n.t('common.unknownError')
      showError(message)
      return null
    } finally {
      setLoading(false)
    }
  }

  /**
   * Reset app state to defaults
   * Useful for cleanup or testing
   */
  function reset(): void {
    loadingCount = 0
    set({
      sidebarCollapsed: false,
      loading: false,
      toasts: []
    })
  }
  // loadingCount stays a closure counter; the store only mirrors `loading`.

  // ==================== Version Management ====================

  async function currentUserIsAdmin(): Promise<boolean> {
    // Late import avoids a cycle with auth.ts, which clears this cache on logout.
    const { useAuthStore } = await import('./auth')
    return useAuthStore.getState().isAdmin()
  }

  /**
   * Fetch version info (uses cache unless force=true)
   * @param force - Force refresh from API
   */
  async function fetchVersion(force = false): Promise<VersionInfo | null> {
    if (!(await currentUserIsAdmin())) {
      clearVersionCache()
      return null
    }

    // Return cached data if available and not forcing refresh
    if (get().versionLoaded && !force) {
      return {
        current_version: get().currentVersion,
        latest_version: get().latestVersion,
        has_update: get().hasUpdate,
        build_type: get().buildType,
        release_info: get().releaseInfo || undefined,
        cached: true
      }
    }

    // Prevent duplicate requests
    if (get().versionLoading) {
      return null
    }

    const requestId = versionRequestId
    set({ versionLoading: true })
    try {
      const data = await checkUpdatesAPI(force)
      if (requestId !== versionRequestId || !(await currentUserIsAdmin())) {
        return null
      }
      set({
        currentVersion: data.current_version,
        latestVersion: data.latest_version,
        hasUpdate: data.has_update,
        buildType: data.build_type || 'source',
        releaseInfo: data.release_info || null,
        versionLoaded: true
      })
      return data
    } catch (error) {
      console.error('Failed to fetch version:', error)
      return null
    } finally {
      if (requestId === versionRequestId) {
        set({ versionLoading: false })
      }
    }
  }

  /**
   * Drop a completed update check without wiping the visible current version.
   * Used after an in-place update/rollback so the next check refetches.
   */
  function invalidateVersionCheck(): void {
    versionRequestId += 1
    set({
      versionLoaded: false,
      versionLoading: false,
      hasUpdate: false
    })
  }

  /**
   * Clear version cache (e.g., after update, logout, or losing admin).
   */
  function clearVersionCache(): void {
    invalidateVersionCheck()
    set({
      currentVersion: '',
      latestVersion: '',
      buildType: 'source',
      releaseInfo: null
    })
  }

  // ==================== Public Settings Management ====================

  /**
   * Apply settings to store state (internal helper to avoid code duplication)
   */
  function applySettings(config: PublicSettings): void {
    if (typeof window !== 'undefined') {
      window.__APP_CONFIG__ = { ...config }
    }
    set({
      cachedPublicSettings: config,
      consoleAppearance: { ...(config.console_appearance ?? defaultConsoleAppearance()) },
      siteName: config.site_name || 'Sub2API Plus',
      siteLogo: config.site_logo || '',
      siteVersion: config.version || '',
      contactInfo: config.contact_info || '',
      apiBaseUrl: config.api_base_url || '',
      docUrl: config.doc_url || '',
      publicSettingsLoaded: true
    })
  }

  function applyConsoleAppearance(appearance: ConsoleAppearance): void {
    // A public-settings request started before this save must not restore its
    // older appearance when it eventually completes.
    publicSettingsRequestId++
    publicSettingsRequest = null
    set({ publicSettingsLoading: false, consoleAppearance: { ...appearance } })
    const cachedPublicSettings = get().cachedPublicSettings
    if (cachedPublicSettings) {
      set({
        cachedPublicSettings: {
          ...cachedPublicSettings,
          console_appearance: { ...appearance }
        }
      })
    }
    if (typeof window !== 'undefined' && window.__APP_CONFIG__) {
      window.__APP_CONFIG__ = { ...window.__APP_CONFIG__, console_appearance: { ...appearance } }
    }
  }

  /**
   * Fetch public settings (uses cache unless force=true)
   * @param force - Force refresh from API
   */
  function fetchPublicSettings(force = false): Promise<PublicSettings | null> {
    // An active request always wins over cache/force semantics so every caller observes
    // the same refresh result and no older request can overwrite a newer one.
    if (publicSettingsRequest) {
      return publicSettingsRequest
    }

    // Check for injected config from server (eliminates flash)
    if (!get().publicSettingsLoaded && !force && window.__APP_CONFIG__) {
      applySettings(window.__APP_CONFIG__)
      return Promise.resolve(window.__APP_CONFIG__)
    }

    // Return cached data if available and not forcing refresh
    if (get().publicSettingsLoaded && !force) {
      const cachedPublicSettings = get().cachedPublicSettings
      if (cachedPublicSettings) {
        return Promise.resolve({ ...cachedPublicSettings })
      }
      return Promise.resolve<PublicSettings>({
        registration_enabled: false,
        email_verify_enabled: false,
        force_email_on_third_party_signup: false,
        registration_email_suffix_whitelist: [],
        promo_code_enabled: true,
        password_reset_enabled: false,
        invitation_code_enabled: false,
        turnstile_enabled: false,
        turnstile_site_key: '',
        aliyun_captcha_enabled: false,
        aliyun_captcha_scene_id: '',
        aliyun_captcha_prefix: '',
        aliyun_captcha_region: 'cn',
        site_name: get().siteName,
        site_logo: get().siteLogo,
        site_subtitle: '',
        api_base_url: get().apiBaseUrl,
        contact_info: get().contactInfo,
        doc_url: get().docUrl,
        home_content: '',
        compact_home_enabled: false,
        hide_ccs_import_button: false,
        payment_enabled: false,
        table_default_page_size: 20,
        table_page_size_options: [10, 20, 50, 100],
        custom_menu_items: [],
        custom_endpoints: [],
        linuxdo_oauth_enabled: false,
        wechat_oauth_enabled: false,
        wechat_oauth_open_enabled: false,
        wechat_oauth_mp_enabled: false,
        wechat_oauth_mobile_enabled: false,
        oidc_oauth_enabled: false,
        oidc_oauth_provider_name: 'OIDC',
        github_oauth_enabled: false,
        google_oauth_enabled: false,
        backend_mode_enabled: false,
        passkey_enabled: false,
        version: get().siteVersion,
        balance_low_notify_enabled: false,
        account_quota_notify_enabled: false,
        balance_low_notify_threshold: 0,
        channel_monitor_enabled: true,
        channel_monitor_default_interval_seconds: 60,
        available_channels_enabled: false,
        available_channels_layout: 'card',
        subscription_enabled: true,
        payment_balance_disabled: false,
        model_plaza_enabled: false,
        model_plaza_require_auth: true,
        plugin_management_enabled: false,
        risk_control_enabled: false,
        global_ip_access_control_enabled: false,
        service_quota_enabled: false,
        affiliate_enabled: false,
        allow_user_view_error_requests: false,
        mcp_enabled: false,
      })
    }

    const requestId = publicSettingsRequestId
    set({ publicSettingsLoading: true })
    let apiRequest: Promise<PublicSettings>
    try {
      apiRequest = fetchPublicSettingsAPI()
    } catch (error) {
      console.error('Failed to fetch public settings:', error)
      set({ publicSettingsLoading: false })
      return Promise.resolve(null)
    }

    const request = (apiRequest as Promise<PublicSettings>)
      .then((data) => {
        if (requestId !== publicSettingsRequestId) return null
        applySettings(data)
        return data
      })
      .catch((error) => {
        console.error('Failed to fetch public settings:', error)
        return null
      })
      .finally(() => {
        if (publicSettingsRequest === request) {
          publicSettingsRequest = null
          set({ publicSettingsLoading: false })
        }
      })

    publicSettingsRequest = request
    return request
  }

  /**
   * Clear public settings cache
   */
  function clearPublicSettingsCache(): void {
    publicSettingsRequestId += 1
    publicSettingsRequest = null
    set({
      publicSettingsLoading: false,
      publicSettingsLoaded: false,
      cachedPublicSettings: null
    })
    if (typeof window !== 'undefined') delete window.__APP_CONFIG__
  }

  /**
   * Initialize settings from injected config (window.__APP_CONFIG__)
   * This is called synchronously before Vue app mounts to prevent flash
   * @returns true if config was found and applied, false otherwise
   */
  function initFromInjectedConfig(): boolean {
    if (window.__APP_CONFIG__) {
      applySettings(window.__APP_CONFIG__)
      return true
    }
    return false
  }

  // ==================== Return Store API ====================

  return {
    // State
    sidebarCollapsed: false,
    mobileOpen: false,
    sidebarScrollTop: 0,
    loading: false,
    toasts: [],

    // Public settings state
    publicSettingsLoaded: false,
    publicSettingsLoading: false,
    siteName: 'Sub2API Plus',
    siteLogo: '',
    siteVersion: '',
    contactInfo: '',
    apiBaseUrl: '',
    docUrl: '',
    cachedPublicSettings: null,
    consoleAppearancePreview: null,
    consoleAppearance: defaultConsoleAppearance(),

    // Version state
    versionLoaded: false,
    versionLoading: false,
    currentVersion: '',
    latestVersion: '',
    hasUpdate: false,
    buildType: 'source',
    releaseInfo: null,

    // Computed
    hasActiveToasts: () => get().toasts.length > 0,
    availableChannelsLayout: () =>
      get().cachedPublicSettings?.available_channels_layout === 'list' ? 'list' : 'card',
    backendModeEnabled: () => get().cachedPublicSettings?.backend_mode_enabled ?? false,

    // Actions
    toggleSidebar,
    setSidebarCollapsed,
    toggleMobileSidebar,
    setMobileOpen,
    setLoading,
    showToast,
    showSuccess,
    showError,
    showInfo,
    showWarning,
    hideToast,
    clearAllToasts,
    withLoading,
    withLoadingAndError,
    reset,

    // Version actions
    fetchVersion,
    invalidateVersionCheck,
    clearVersionCache,

    // Public settings actions
    fetchPublicSettings,
    clearPublicSettingsCache,
    initFromInjectedConfig,
    applyConsoleAppearance
  }
})
