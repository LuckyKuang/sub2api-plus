import { useEffect, useRef } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useLocation } from 'react-router-dom'
import { AppRouter } from '@/router'
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

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      refetchOnWindowFocus: false,
      retry: 1
    }
  }
})

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

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <AppRouter />
      <ConsoleChrome />
    </QueryClientProvider>
  )
}

export default App
