import { useEffect, useMemo, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { AppSidebar } from './AppSidebar'
import { AppHeader } from './AppHeader'
import { Toaster } from '@/components/ui'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import { getAdminSupportContext } from '@/utils/adminSupportContext'
import { canViewAsyncImage } from '@/composables/useAsyncImageAccess'

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

  const appStore = useAppStore()
  const authStore = useAuthStore()
  const adminSettingsStore = useAdminSettingsStore()
  const settings = appStore.cachedPublicSettings
  const isAdmin = authStore.user?.role === 'admin'
  const supportUserId = getAdminSupportContext()?.userId ?? null

  useEffect(() => {
    document.documentElement.classList.toggle('dark', theme === 'dark')
    localStorage.setItem(THEME_KEY, theme)
  }, [theme])

  useEffect(() => {
    setMobileOpen(false)
  }, [location.pathname])

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
      />
      <div className="app-content">
        <AppHeader
          siteName={appStore.siteName}
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
            <Outlet />
          </div>
        </main>
      </div>
      <Toaster />
    </div>
  )
}
