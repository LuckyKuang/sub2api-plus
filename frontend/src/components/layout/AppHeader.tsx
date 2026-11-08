import { useMemo } from 'react'
import { Link, useLocation, useMatches } from 'react-router-dom'
import { BookOpen, LayoutGrid, Menu as MenuIcon, Moon, Sun, User as UserIcon, Wallet } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { AnnouncementBell } from '@/components/AnnouncementBell'
import { LocaleSwitcher } from '@/components/LocaleSwitcher'
import { SubscriptionProgressMini } from '@/components/SubscriptionProgressMini'
import { VersionBadge } from '@/components/VersionBadge'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { resolveRouteMetaKeys, type RouteLike } from '@/router/title'
import { resolveSiteBillingMode } from '@/utils/siteBillingMode'

export interface AppHeaderProps {
  siteLogo?: string
  userName?: string
  isAdmin?: boolean
  onToggleMobile: () => void
  onToggleTheme: () => void
  theme: 'light' | 'dark'
  onLogout: () => void
  /** Dense toolbars (ops workspace) drop the secondary entries. */
  compactHeader?: boolean
}

/**
 * Console header. Mirrors the Vue AppHeader: the page title and description on
 * the left, then announcements, docs, model plaza, language, subscription
 * progress, balance and the account menu on the right.
 */
export function AppHeader({
  siteLogo,
  userName,
  onToggleMobile,
  onToggleTheme,
  theme,
  onLogout,
  compactHeader = false
}: AppHeaderProps) {
  const { t } = useTranslation()
  const location = useLocation()
  const matches = useMatches()
  const appStore = useAppStore()
  const authStore = useAuthStore()

  const settings = appStore.cachedPublicSettings
  const user = authStore.user

  const routeLike = useMemo<RouteLike>(
    () => ({
      pathname: location.pathname,
      handle: matches.at(-1)?.handle,
      params: readCustomPageId(location.pathname)
    }),
    [location.pathname, matches]
  )

  const billingMode = resolveSiteBillingMode(settings)
  const { titleKey, descriptionKey } = resolveRouteMetaKeys(routeLike, {
    billingMode: billingMode === 'recharge_only' || billingMode === 'subscription_only' ? billingMode : undefined
  })
  const pageTitle = (titleKey && t(titleKey)) || ''
  const pageDescription = (descriptionKey && t(descriptionKey)) || ''

  const modelPlazaEnabled = settings?.model_plaza_enabled !== false
  const subscriptionFeatureEnabled = settings?.subscription_enabled !== false
  const balance = user?.balance

  return (
    <header className="app-header shell-surface">
      <div className="app-header-inner flex items-center justify-between gap-2">
        {/* Left: mobile toggle + page title */}
        <div className="flex min-w-0 flex-1 items-center gap-2 sm:gap-4">
          <button
            type="button"
            onClick={onToggleMobile}
            className="btn-ghost btn-icon lg:hidden surface-control"
            aria-label={t('common.toggleMenu')}
            aria-controls="app-navigation"
          >
            <MenuIcon className="h-5 w-5" />
          </button>

          {appStore.publicSettingsLoaded && (
            <img
              src={siteLogo || '/logo.svg'}
              alt=""
              className="header-mobile-logo h-7 w-7 shrink-0 rounded-lg object-contain lg:hidden"
            />
          )}

          <div className="min-w-0">
            <h1
              className="truncate text-sm font-semibold text-gray-900 dark:text-white sm:text-lg"
              title={pageTitle}
            >
              {pageTitle}
            </h1>
            {pageDescription && !compactHeader && (
              <p className="truncate text-xs surface-muted" title={pageDescription}>
                {pageDescription}
              </p>
            )}
          </div>
        </div>

        {/* Right: announcements, docs, plaza, language, subscription, balance, account */}
        <div className="flex shrink-0 items-center gap-1 sm:gap-3">
          {user && <AnnouncementBell />}

          {appStore.docUrl && !compactHeader && (
            <a
              href={appStore.docUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="hidden items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-sm font-medium transition-colors hover:surface-hover hover:text-gray-900 dark:hover:text-white sm:flex surface-muted"
            >
              <BookOpen className="h-4 w-4" />
              <span className="hidden sm:inline">{t('nav.docs')}</span>
            </a>
          )}

          {user && modelPlazaEnabled && !compactHeader && (
            <Link
              to={{ pathname: '/model-plaza', search: '?embedded=1' }}
              title={t('nav.modelPlaza')}
              aria-label={t('nav.modelPlaza')}
              className="flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-sm font-medium text-gray-600 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
            >
              <LayoutGrid className="h-4 w-4" />
              <span className="hidden sm:inline">{t('nav.modelPlaza')}</span>
            </Link>
          )}

          {!compactHeader && <LocaleSwitcher />}

          {user && subscriptionFeatureEnabled && !compactHeader && <SubscriptionProgressMini />}

          {user && !compactHeader && (
            <div className="group relative hidden items-center gap-2 rounded-xl bg-primary-50/[0.15] px-3 py-1.5 dark:bg-primary-900/20 sm:flex">
              <Wallet className="h-4 w-4 text-primary-600 dark:text-primary-400" />
              <span className="text-sm font-semibold text-primary-700 dark:text-primary-300">
                ${formatBalance(balance)}
              </span>
            </div>
          )}

          <button
            type="button"
            onClick={onToggleTheme}
            aria-label={t('common.toggleTheme')}
            title={theme === 'dark' ? t('nav.lightMode') : t('nav.darkMode')}
            className="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 dark:hover:bg-dark-800"
          >
            {theme === 'dark' ? <Sun className="h-5 w-5" /> : <Moon className="h-5 w-5" />}
          </button>

          <div className="relative">
            <button
              type="button"
              onClick={onLogout}
              className="flex items-center gap-2 rounded-xl px-2 py-1.5 transition-colors hover:bg-gray-100 dark:hover:bg-dark-800"
              aria-label={userName || t('nav.profile')}
            >
              <span className="flex h-8 w-8 items-center justify-center rounded-full bg-primary-100 text-primary-600 dark:bg-primary-900/30 dark:text-primary-400">
                <UserIcon className="h-4 w-4" />
              </span>
              <span className="hidden max-w-[120px] truncate text-sm font-medium text-gray-700 sm:block dark:text-gray-200">
                {userName}
              </span>
            </button>
          </div>

          {!compactHeader && (
            <div className="hidden md:block">
              <VersionBadge />
            </div>
          )}
        </div>
      </div>
    </header>
  )
}

function formatBalance(value: number | undefined): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '0.00'
  return value.toFixed(2)
}

function readCustomPageId(pathname: string): Record<string, string | undefined> {
  const match = /^\/custom\/([^/]+)/.exec(pathname)
  return match ? { id: decodeURIComponent(match[1]) } : {}
}

export default AppHeader
