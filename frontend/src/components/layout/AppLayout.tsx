import { useEffect, useMemo, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { AppSidebar } from './AppSidebar'
import { AppHeader } from './AppHeader'
import { Toaster } from '@/components/ui'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import { useOnboardingStore } from '@/stores/onboarding'
import { getAdminSupportContext } from '@/utils/adminSupportContext'
import { AdminSupportBanner } from '@/views/admin/support/AdminSupportBanner'
import { useConsoleAppearance } from '@/composables/useConsoleAppearance'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { defaultConsoleAppearance } from '@/types/appearance'
import { canViewAsyncImage } from '@/composables/useAsyncImageAccess'
import '@/styles/onboarding.css'

const THEME_KEY = 'theme'

function readTheme(): 'light' | 'dark' {
  const saved = localStorage.getItem(THEME_KEY)
  if (saved === 'dark' || saved === 'light') return saved
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export interface AppLayoutProps {
  appearance?: 'user' | 'admin'
}

export function AppLayout({ appearance = 'admin' }: AppLayoutProps) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const [collapsed, setCollapsed] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)
  const [theme, setTheme] = useState<'light' | 'dark'>(() => readTheme())
  const [isDesktop, setIsDesktop] = useState(() => window.matchMedia('(min-width: 1024px)').matches)

  const appStore = useAppStore()
  const authStore = useAuthStore()
  const adminSettingsStore = useAdminSettingsStore()
  const settings = appStore.cachedPublicSettings
  const consoleAppearance = appStore.consoleAppearance
  const isAdmin = authStore.user?.role === 'admin'
  const supportUserId = getAdminSupportContext()?.userId ?? null

  const { replayTour } = useOnboardingTour({
    storageKey: isAdmin ? 'admin_guide' : 'user_guide',
    autoStart: !supportUserId
  })

  useEffect(() => {
    useOnboardingStore.getState().setReplayCallback(replayTour)
  }, [replayTour])

  useEffect(() => {
    document.documentElement.classList.toggle('dark', theme === 'dark')
    localStorage.setItem(THEME_KEY, theme)
  }, [theme])

  // Accent palette, card transparency and background come from the saved
  // console appearance; the admin preview overrides it while being edited.
  useConsoleAppearance(appStore.consoleAppearancePreview ?? consoleAppearance ?? defaultConsoleAppearance())

  useEffect(() => {
    setMobileOpen(false)
  }, [location.pathname])

  useEffect(() => {
    const query = window.matchMedia('(min-width: 1024px)')
    const onChange = (event: MediaQueryListEvent) => setIsDesktop(event.matches)
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [])

  const navContext = useMemo(
    () => ({
      t: (key: string) => i18n.t(key),
      settings,
      isAdmin,
      isSimpleMode: authStore.runMode === 'simple',
      purchaseNavLabel: t('nav.buySubscription'),
      customMenuItems: [
        ...(settings?.custom_menu_items ?? []),
        ...(isAdmin ? adminSettingsStore.customMenuItems : [])
      ],
      channelMonitorMode: 'v2' as const,
      supportUserId,
      flags: {
        asyncImage: canViewAsyncImage(isAdmin, authStore.hasManageableImageKey),
        batchImage: canViewAsyncImage(isAdmin, authStore.hasManageableImageKey),
        opsMonitoring: adminSettingsStore.opsMonitoringEnabled,
        adminPayment: adminSettingsStore.paymentEnabled
      }
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [
      i18n.language,
      settings,
      isAdmin,
      authStore.runMode,
      authStore.hasManageableImageKey,
      adminSettingsStore.customMenuItems,
      adminSettingsStore.opsMonitoringEnabled,
      adminSettingsStore.paymentEnabled,
      supportUserId,
      t
    ]
  )

  const handleLogout = () => {
    void authStore.logout().then(() => navigate('/login'))
  }

  return (
    <div
      className={[
        'app-layout',
        collapsed && 'sidebar-is-collapsed',
        appearance === 'user' ? 'app-layout-user' : 'app-layout-admin'
      ]
        .filter(Boolean)
        .join(' ')}
    >
      <AppSidebar
        {...navContext}
        collapsed={collapsed}
        onToggleCollapsed={() => setCollapsed((value) => !value)}
        mobileOpen={mobileOpen}
        onCloseMobile={() => setMobileOpen(false)}
        theme={theme}
        onToggleTheme={() => setTheme((value) => (value === 'dark' ? 'light' : 'dark'))}
        isDesktop={isDesktop}
        siteName={appStore.siteName}
        siteLogo={appStore.siteLogo}
      />
      <div className="app-content" inert={mobileOpen || undefined}>
        <AppHeader
          siteLogo={appStore.siteLogo}
          userName={authStore.user?.username || authStore.user?.email}
          isAdmin={isAdmin}
          onToggleMobile={() => setMobileOpen((value) => !value)}
          onToggleTheme={() => setTheme((value) => (value === 'dark' ? 'light' : 'dark'))}
          theme={theme}
          onLogout={handleLogout}
        />
        <main className="app-main">
          <div className="app-page">
            {supportUserId && authStore.user && (
              <AdminSupportBanner target={authStore.user} className="support-banner-spacing" />
            )}
            <Outlet />
          </div>
        </main>
      </div>
      <Toaster />
    </div>
  )
}
