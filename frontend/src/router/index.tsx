import { createBrowserRouter, Navigate, Outlet, RouterProvider, useLocation, useMatches } from 'react-router-dom'
import { NavigationProgress } from '@/components/NavigationProgress'
import { DocumentTitleTooltip } from '@/components/common/DocumentTitleTooltip'
import { AdminComplianceDialog } from '@/components/admin/AdminComplianceDialog'
import { AnnouncementPopup } from '@/views/admin/parts/AnnouncementPopup'
import { useStaticTableHeaders } from '@/composables/useStaticTableHeaders'
import { useTransparentTableClipping } from '@/composables/useTransparentTableClipping'
import { useAdminComplianceStore } from '@/stores/adminCompliance'
import { useAnnouncementsStore } from '@/stores/announcements'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useSubscriptionsStore } from '@/stores/subscriptions'
import { getSetupStatus } from '@/api/setup'
import { updateFavicon } from '@/utils/branding'
import { Suspense, useEffect, useRef, type ReactNode } from 'react'
import { AppLayout } from '@/components/layout/AppLayout'
import { routes, type RouteMeta } from './routes'
import { resolveGuardRedirect, type AuthSnapshot } from './guards'
import { LoadingState, Toaster } from '@/components/ui'

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

/**
 * Unauthenticated screens render `AuthLayout` themselves, exactly as the Vue
 * views did, so the router only supplies suspense and the shared toaster here.
 */
function PublicShell() {
  return (
    <>
      <Suspense fallback={<LoadingState className="min-h-screen" />}>
        <Outlet />
      </Suspense>
      <Toaster />
    </>
  )
}

/**
 * The console shell. In Vue each view chose its own chrome by wrapping itself in
 * `<AppLayout appearance="user" workspace>`; the single React shell reads that
 * choice off the deepest matched route instead.
 */
function ConsoleShell() {
  const matches = useMatches()
  const meta = matches.at(-1)?.handle as RouteMeta | undefined
  return (
    <Suspense fallback={<LoadingState />}>
      <AppLayout appearance={meta?.appearance ?? 'admin'} workspace={meta?.workspace ?? false} />
    </Suspense>
  )
}

/** One document-wide owner also covers standalone routes and teleported dialogs. */
function useStaticTables() {
  const bodyRef = useRef<HTMLElement>(document.body)
  useStaticTableHeaders(bodyRef)
  useTransparentTableClipping()
}

/**
 * Application chrome that in Vue lived in `App.vue`: the navigation progress bar,
 * the delegated title tooltip, the announcement popup, the blocking admin
 * compliance dialog, and the data sync the shell drives.
 */
function ConsoleChrome() {
  const location = useLocation()
  const appStore = useAppStore()
  const authStore = useAuthStore()
  const subscriptionStore = useSubscriptionsStore()
  const announcementStore = useAnnouncementsStore()
  const adminComplianceStore = useAdminComplianceStore()

  useStaticTables()

  // Announcements refresh on route change, throttled by the store.
  useEffect(() => {
    if (authStore.isAuthenticated()) announcementStore.fetchAnnouncements()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [location.pathname])

  // The favicon follows the configured site logo.
  useEffect(() => {
    if (appStore.siteLogo) updateFavicon(appStore.siteLogo)
  }, [appStore.siteLogo])

  // Subscriptions preload and poll while the feature is on; clear when off.
  useEffect(() => {
    if (!authStore.isAuthenticated()) return
    if (appStore.cachedPublicSettings?.subscription_enabled === false) {
      subscriptionStore.clear()
      return
    }
    subscriptionStore.fetchActiveSubscriptions().catch((error) => {
      console.error('Failed to preload subscriptions:', error)
    })
    subscriptionStore.startPolling()
    return () => subscriptionStore.stopPolling()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [authStore.isAuthenticated(), appStore.cachedPublicSettings?.subscription_enabled])

  // A new login force-refreshes announcements after a short delay.
  useEffect(() => {
    if (!authStore.isAuthenticated()) return
    const timer = setTimeout(() => announcementStore.fetchAnnouncements(true), 3000)
    return () => clearTimeout(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [authStore.user?.id])

  // Admins must acknowledge the compliance document before using the console.
  useEffect(() => {
    if (!authStore.isAuthenticated() || !authStore.isAdmin()) return
    adminComplianceStore.fetchStatus().catch((error) => {
      console.error('Failed to fetch admin compliance status:', error)
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [authStore.user?.id, authStore.user?.role])

  useEffect(() => {
    const onComplianceRequired = (event: Event) => {
      const detail = (event as CustomEvent<Record<string, string>>).detail || {}
      adminComplianceStore.requireAcknowledgement(detail)
    }
    window.addEventListener('admin-compliance-required', onComplianceRequired)
    return () => window.removeEventListener('admin-compliance-required', onComplianceRequired)
  }, [adminComplianceStore])

  // Refetch announcements when the tab becomes visible again.
  useEffect(() => {
    const onVisibilityChange = () => {
      if (document.visibilityState === 'visible' && authStore.isAuthenticated()) {
        announcementStore.fetchAnnouncements()
      }
    }
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () => document.removeEventListener('visibilitychange', onVisibilityChange)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // A fresh install must complete setup before anything else is reachable.
  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const status = await getSetupStatus()
        if (cancelled) return
        if (status.needs_setup && window.location.pathname !== '/setup') {
          window.location.replace('/setup')
        }
      } catch {
        // If setup endpoint fails, assume normal mode and continue
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  const popup = announcementStore.currentPopup

  return (
    <>
      <NavigationProgress />
      <DocumentTitleTooltip />
      {popup && <AnnouncementPopup announcement={popup} onClose={() => void announcementStore.dismissPopup()} />}
      <AdminComplianceDialog />
    </>
  )
}

/**
 * Root layout. The global chrome lives here rather than beside the router so it
 * stays inside the Router context — it needs `useLocation` to refetch
 * announcements on navigation, as `App.vue` did with `router.afterEach`.
 */
function RootLayout() {
  return (
    <>
      <Outlet />
      <ConsoleChrome />
    </>
  )
}

const shellRoutes = [
  {
    element: <RootLayout />,
    children: [
      {
        element: <ConsoleShell />,
        children: routes
          .filter((route) => !isPublicRoute(route.path))
          .map((route) => ({
            ...route,
            element: withGuard(route.element, route.handle as RouteMeta | undefined)
          }))
      },
      {
        element: <PublicShell />,
        children: routes
          .filter((route) => isPublicRoute(route.path))
          .map((route) => ({
            ...route,
            element: withGuard(route.element, route.handle as RouteMeta | undefined)
          }))
      }
    ]
  }
]

export const router = createBrowserRouter(shellRoutes)

export function AppRouter() {
  return <RouterProvider router={router} />
}
