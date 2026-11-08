import { useEffect } from 'react'
import { useLocation } from 'react-router-dom'
import { routes, type RouteMeta } from './routes'

const BACKEND_MODE_ALLOWED_PATHS = ['/login', '/key-usage', '/setup', '/payment/result', '/payment/airwallex', '/legal']
const BACKEND_MODE_CALLBACK_PATHS = [
  '/auth/callback',
  '/auth/linuxdo/callback',
  '/auth/dingtalk/callback',
  '/auth/dingtalk/email-completion',
  '/auth/oidc/callback',
  '/auth/wechat/callback',
  '/auth/wechat/payment/callback'
]
const BACKEND_MODE_PENDING_AUTH_PATHS = ['/register', '/email-verify']

export function isBackendModePublicRouteAllowed(path: string, hasPendingAuthSession: boolean): boolean {
  if (BACKEND_MODE_ALLOWED_PATHS.some((allowedPath) => path === allowedPath || path.startsWith(allowedPath))) {
    return true
  }
  if (BACKEND_MODE_CALLBACK_PATHS.some((callbackPath) => path === callbackPath)) {
    return true
  }
  if (hasPendingAuthSession && BACKEND_MODE_PENDING_AUTH_PATHS.some((allowedPath) => path === allowedPath)) {
    return true
  }
  return false
}

export interface AuthSnapshot {
  isAuthenticated: boolean
  isAdmin: boolean
  hasPendingAuthSession: boolean
  backendModeEnabled: boolean
  publicSettingsLoaded: boolean
  settings?: {
    model_plaza_enabled?: boolean
    model_plaza_require_auth?: boolean
    payment_enabled?: boolean
    risk_control_enabled?: boolean
    global_ip_access_control_enabled?: boolean
    subscription_enabled?: boolean
    channel_monitor_enabled?: boolean
  }
}

/**
 * Decide where a navigation should land before the guarded screen renders.
 * Mirrors the previous Vue Router beforeEach contract exactly.
 */
export function resolveGuardRedirect(
  pathname: string,
  meta: RouteMeta,
  auth: AuthSnapshot
): string | null {
  const home = auth.isAdmin ? '/admin/dashboard' : '/dashboard'
  const requiresAuth = meta.requiresAuth !== false
  const requiresAdmin = meta.requiresAdmin === true

  if (pathname === '/') return '/home'

  if (!requiresAuth) {
    if (auth.isAuthenticated && (pathname === '/login' || pathname === '/register')) {
      if (auth.backendModeEnabled && !auth.isAdmin) return null
      return home
    }
    if (pathname === '/model-plaza') {
      if (auth.publicSettingsLoaded && auth.settings?.model_plaza_enabled === false) {
        return auth.isAuthenticated ? home : '/home'
      }
      if (auth.settings?.model_plaza_require_auth === true && !auth.isAuthenticated) {
        return `/login?redirect=${encodeURIComponent(pathname)}`
      }
      if (auth.backendModeEnabled && auth.isAuthenticated && !auth.isAdmin) return '/login'
    }
    if (auth.backendModeEnabled && !auth.isAuthenticated) {
      if (!isBackendModePublicRouteAllowed(pathname, auth.hasPendingAuthSession)) return '/login'
    }
    return null
  }

  if (!auth.isAuthenticated) {
    return `/login?redirect=${encodeURIComponent(pathname)}`
  }

  if (requiresAdmin && !auth.isAdmin) return '/dashboard'

  if (meta.requiresPayment && auth.publicSettingsLoaded && auth.settings?.payment_enabled === false) return home
  if (meta.requiresRiskControl && auth.publicSettingsLoaded && auth.settings?.risk_control_enabled === false) {
    return auth.isAdmin ? '/admin/settings' : '/dashboard'
  }
  if (meta.requiresIpAccessControl && auth.publicSettingsLoaded && auth.settings?.global_ip_access_control_enabled === false) {
    return auth.isAdmin ? '/admin/settings' : '/dashboard'
  }
  if (meta.requiresSubscription && auth.publicSettingsLoaded && auth.settings?.subscription_enabled === false) return home

  if (pathname === '/monitor' || meta.adminSupportResource === 'channel-status') {
    if (auth.publicSettingsLoaded && auth.settings?.channel_monitor_enabled === false) return home
  }

  return null
}

/** Applies the resolved document title for the current location. */
export function useDocumentTitle(resolve: (pathname: string) => string): void {
  const location = useLocation()
  useEffect(() => {
    document.title = resolve(location.pathname)
  }, [location.pathname, resolve])
}

export { routes }
