import { createBrowserRouter, Navigate, RouterProvider, useLocation } from 'react-router-dom'
import { Suspense, type ReactNode } from 'react'
import { AppLayout } from '@/components/layout/AppLayout'
import { AuthLayout } from '@/components/layout/AuthLayout'
import { routes, type RouteMeta } from './routes'
import { resolveGuardRedirect, type AuthSnapshot } from './guards'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { LoadingState } from '@/components/ui'

/**
 * Wraps a route element with the auth/admin/payment guard. The previous Vue
 * Router guard ran before the component resolved; React Router resolves the
 * component first, so the guard redirects during render instead.
 */
function Guarded({ children, meta }: { children: ReactNode; meta: RouteMeta }) {
  const location = useLocation()
  const appStore = useAppStore()
  const authStore = useAuthStore()

  const settings = appStore.cachedPublicSettings
  const snapshot: AuthSnapshot = {
    isAuthenticated: authStore.isAuthenticated(),
    isAdmin: authStore.isAdmin(),
    hasPendingAuthSession: authStore.hasPendingAuthSession(),
    backendModeEnabled: settings?.backend_mode_enabled ?? false,
    publicSettingsLoaded: appStore.publicSettingsLoaded,
    settings: settings ?? undefined
  }

  const redirect = resolveGuardRedirect(location.pathname, meta, snapshot)

  if (redirect) {
    return <Navigate to={redirect} replace />
  }

  return <>{children}</>
}

function withGuard(element: ReactNode, meta: RouteMeta | undefined) {
  return (
    <Suspense fallback={<LoadingState />}>
      <Guarded meta={meta ?? {}}>{element}</Guarded>
    </Suspense>
  )
}

const PUBLIC_PATHS = new Set([
  '/setup',
  '/home',
  '/login',
  '/register',
  '/email-verify',
  '/forgot-password',
  '/reset-password',
  '/key-usage',
  '/model-plaza'
])

function isPublicRoute(path?: string): boolean {
  if (!path) return false
  if (PUBLIC_PATHS.has(path)) return true
  return (
    path.startsWith('/auth/') ||
    path.startsWith('/legal/') ||
    path.startsWith('/payment/')
  )
}

const shellRoutes = [
  {
    element: (
      <Suspense fallback={<LoadingState />}>
        <AppLayout appearance="admin" />
      </Suspense>
    ),
    children: routes
      .filter((route) => !isPublicRoute(route.path))
      .map((route) => ({
        ...route,
        element: withGuard(route.element, route.handle as RouteMeta | undefined)
      }))
  },
  {
    element: (
      <Suspense fallback={<LoadingState />}>
        <AuthLayout />
      </Suspense>
    ),
    children: routes
      .filter((route) => isPublicRoute(route.path))
      .map((route) => ({
        ...route,
        element: withGuard(route.element, route.handle as RouteMeta | undefined)
      }))
  }
]

export const router = createBrowserRouter(shellRoutes)

export function AppRouter() {
  return <RouterProvider router={router} />
}
