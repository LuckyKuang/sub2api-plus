import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useLocation, useMatches } from 'react-router-dom'
import { BookOpen, LayoutGrid, Menu as MenuIcon, Moon, Sun } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { AnnouncementBell } from '@/components/AnnouncementBell'
import { LocaleSwitcher } from '@/components/LocaleSwitcher'
import { SubscriptionProgressMini } from '@/components/SubscriptionProgressMini'
import { VersionBadge } from '@/components/VersionBadge'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingStore } from '@/stores/onboarding'
import { resolveRouteMetaKeys, type RouteLike } from '@/router/title'
import { resolveSiteBillingMode } from '@/utils/siteBillingMode'
import { sanitizeUrl } from '@/utils/url'

export interface AppHeaderProps {
  siteLogo?: string
  userName?: string
  isAdmin?: boolean
  onToggleMobile: () => void
  onToggleTheme: () => void
  theme: 'light' | 'dark'
  onLogout: () => void
}

/** Below this width the header folds its secondary entries into the dropdown. */
const COMPACT_HEADER_WIDTH = 1000

function formatHeaderMoney(value: number): string {
  if (!Number.isFinite(value)) return '$0.00'
  return `$${value.toFixed(2)}`
}

/**
 * Console header. Mirrors the Vue AppHeader: the page title and description on
 * the left, then announcements, docs, model plaza, language, subscription
 * progress, balance and the account menu on the right.
 */
export function AppHeader({
  siteLogo,
  onToggleMobile,
  onToggleTheme,
  theme,
  onLogout
}: AppHeaderProps) {
  const { t } = useTranslation()
  const location = useLocation()
  const matches = useMatches()
  const appStore = useAppStore()
  const authStore = useAuthStore()
  const onboardingStore = useOnboardingStore()

  const headerRef = useRef<HTMLElement | null>(null)
  const dropdownRef = useRef<HTMLDivElement | null>(null)
  const userMenuButtonRef = useRef<HTMLButtonElement | null>(null)
  const [dropdownOpen, setDropdownOpen] = useState(false)
  const [headerWidth, setHeaderWidth] = useState(0)
  const compactHeader = headerWidth > 0 && headerWidth < COMPACT_HEADER_WIDTH

  const settings = appStore.cachedPublicSettings
  const user = authStore.user

  useEffect(() => {
    const element = headerRef.current
    if (!element) return
    const observer = new ResizeObserver((entries) => {
      const width = entries[0]?.contentRect.width
      if (typeof width === 'number') setHeaderWidth(width)
    })
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  useEffect(() => {
    if (!dropdownOpen) return
    const onPointerDown = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setDropdownOpen(false)
      }
    }
    document.addEventListener('click', onPointerDown)
    return () => document.removeEventListener('click', onPointerDown)
  }, [dropdownOpen])

  const closeDropdown = useCallback(() => setDropdownOpen(false), [])

  const closeDropdownAndFocus = useCallback(() => {
    closeDropdown()
    userMenuButtonRef.current?.focus()
  }, [closeDropdown])

  useEffect(() => {
    if (!dropdownOpen) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      event.stopPropagation()
      closeDropdownAndFocus()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [dropdownOpen, closeDropdownAndFocus])

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
  const docUrl = sanitizeUrl(appStore.docUrl)
  const siteLogoUrl = sanitizeUrl(siteLogo || '', { allowRelative: true, allowDataUrl: true })

  const availableBalance = Number(user?.balance || 0)
  const frozenBalance = Number(user?.frozen_balance || 0)
  const totalBalance = availableBalance + frozenBalance

  const avatarUrl = user?.avatar_url?.trim() || ''
  const displayName = user?.username || user?.email?.split('@')[0] || ''
  const userInitials = user?.username
    ? user.username.substring(0, 2).toUpperCase()
    : user?.email
      ? user.email.split('@')[0].substring(0, 2).toUpperCase()
      : ''

  const contactInfo = appStore.contactInfo
  const isSimpleMode = authStore.runMode === 'simple'
  const showOnboardingButton = !isSimpleMode && user?.role === 'admin'

  const handleReplayGuide = () => {
    closeDropdown()
    onboardingStore.replay()
  }

  return (
    <header ref={headerRef} className="app-header shell-surface">
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
              src={siteLogoUrl || '/logo.svg'}
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

          {docUrl && !compactHeader && (
            <a
              href={docUrl}
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
              <svg
                className="h-4 w-4 text-primary-600 dark:text-primary-400"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                strokeWidth="1.5"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M2.25 18.75a60.07 60.07 0 0115.797 2.101c.727.198 1.453-.342 1.453-1.096V18.75M3.75 4.5v.75A.75.75 0 013 6h-.75m0 0v-.375c0-.621.504-1.125 1.125-1.125H20.25M2.25 6v9m18-10.5v.75c0 .414.336.75.75.75h.75m-1.5-1.5h.375c.621 0 1.125.504 1.125 1.125v9.75c0 .621-.504 1.125-1.125 1.125h-.375m1.5-1.5H21a.75.75 0 00-.75.75v.75m0 0H3.75m0 0h-.375a1.125 1.125 0 01-1.125-1.125V15m1.5 1.5v-.75A.75.75 0 003 15h-.75M15 10.5a3 3 0 11-6 0 3 3 0 016 0zm3 0h.008v.008H18V10.5zm-12 0h.008v.008H6V10.5z"
                />
              </svg>
              <span className="text-sm font-semibold text-primary-700 dark:text-primary-300">
                {formatHeaderMoney(availableBalance)}
              </span>
              {frozenBalance > 0 && (
                <span className="rounded-full bg-amber-100/[0.15] px-1.5 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/40 dark:text-amber-200">
                  {t('common.frozenBalance')} {formatHeaderMoney(frozenBalance)}
                </span>
              )}
              <div className="pointer-events-none absolute right-0 top-full mt-2 hidden w-56 rounded-lg border border-gray-200 p-3 text-xs shadow-lg group-hover:block dark:border-dark-700 surface-panel">
                <div className="flex items-center justify-between">
                  <span className="surface-muted">{t('common.availableBalance')}</span>
                  <span className="font-medium text-gray-900 dark:text-white">
                    {formatHeaderMoney(availableBalance)}
                  </span>
                </div>
                <div className="mt-2 flex items-center justify-between">
                  <span className="surface-muted">{t('common.frozenBalance')}</span>
                  <span className="font-medium text-amber-700 dark:text-amber-200">
                    {formatHeaderMoney(frozenBalance)}
                  </span>
                </div>
                <div className="mt-2 border-t border-gray-100 pt-2 dark:border-dark-700">
                  <div className="flex items-center justify-between">
                    <span className="surface-muted">{t('common.totalBalance')}</span>
                    <span className="font-semibold text-gray-900 dark:text-white">
                      {formatHeaderMoney(totalBalance)}
                    </span>
                  </div>
                </div>
              </div>
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

          {user && (
            <div className="relative" ref={dropdownRef}>
              <button
                ref={userMenuButtonRef}
                type="button"
                onClick={() => setDropdownOpen((value) => !value)}
                onKeyDown={(event) => {
                  if (event.key === 'Escape') {
                    event.stopPropagation()
                    closeDropdownAndFocus()
                  }
                }}
                className="flex items-center gap-2 rounded-xl p-1.5 transition-colors hover:surface-hover surface-control"
                aria-label={t('common.userMenu')}
                aria-expanded={dropdownOpen}
                aria-haspopup="true"
              >
                <div className="flex h-8 w-8 items-center justify-center overflow-hidden rounded-xl bg-gradient-to-br from-primary-500 to-primary-600 text-sm font-medium text-white shadow-sm">
                  {avatarUrl ? (
                    <img src={avatarUrl} alt={displayName} className="h-full w-full object-cover" />
                  ) : (
                    <span>{userInitials}</span>
                  )}
                </div>
                {!compactHeader && (
                  <div className="max-w-32 text-left">
                    <div className="truncate text-sm font-medium text-gray-900 dark:text-white">
                      {displayName}
                    </div>
                    <div className="text-xs surface-muted">
                      {t(`admin.users.roles.${user.role}`)}
                    </div>
                  </div>
                )}
                {!compactHeader && (
                  <svg
                    className="h-4 w-4 text-gray-400"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    strokeWidth="1.5"
                  >
                    <path strokeLinecap="round" strokeLinejoin="round" d="M19.5 8.25l-7.5 7.5-7.5-7.5" />
                  </svg>
                )}
              </button>

              {dropdownOpen && (
                <div
                  className="dropdown header-user-dropdown right-0 mt-2"
                  onKeyDown={(event) => {
                    if (event.key === 'Escape') {
                      event.stopPropagation()
                      closeDropdownAndFocus()
                    }
                  }}
                >
                  <div className="border-b border-gray-100 px-4 py-3 dark:border-dark-700">
                    <div className="text-sm font-medium text-gray-900 dark:text-white">{displayName}</div>
                    <div className="text-xs surface-muted">{user.email}</div>
                  </div>

                  {compactHeader && (
                    <div className="border-b border-gray-100 px-4 py-2 dark:border-dark-700">
                      <div className="text-xs surface-muted">{t('common.balance')}</div>
                      <div className="text-sm font-semibold text-primary-600 dark:text-primary-400">
                        {formatHeaderMoney(availableBalance)}
                      </div>
                      {frozenBalance > 0 && (
                        <div className="mt-1 text-xs text-amber-600 dark:text-amber-300">
                          {t('common.frozenBalance')} {formatHeaderMoney(frozenBalance)}
                        </div>
                      )}
                    </div>
                  )}

                  <div className="py-1">
                    {compactHeader && (
                      <>
                        {docUrl && (
                          <a
                            href={docUrl}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="dropdown-item"
                            onClick={closeDropdown}
                          >
                            <BookOpen className="h-4 w-4" />
                            {t('nav.docs')}
                          </a>
                        )}
                        {modelPlazaEnabled && (
                          <Link
                            to={{ pathname: '/model-plaza', search: '?embedded=1' }}
                            className="dropdown-item"
                            onClick={closeDropdown}
                          >
                            <LayoutGrid className="h-4 w-4" />
                            {t('nav.modelPlaza')}
                          </Link>
                        )}
                        <div className="flex flex-col items-start gap-3 px-4 py-2">
                          <LocaleSwitcher />
                          {subscriptionFeatureEnabled && <SubscriptionProgressMini inline />}
                        </div>
                      </>
                    )}
                    <Link to="/profile" onClick={closeDropdown} className="dropdown-item">
                      <svg
                        className="h-4 w-4"
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                        strokeWidth="1.5"
                      >
                        <path
                          strokeLinecap="round"
                          strokeLinejoin="round"
                          d="M15.75 6a3.75 3.75 0 11-7.5 0 3.75 3.75 0 017.5 0zM4.501 20.118a7.5 7.5 0 0114.998 0A17.933 17.933 0 0112 21.75c-2.676 0-5.216-.584-7.499-1.632z"
                        />
                      </svg>
                      {t('nav.profile')}
                    </Link>

                    <Link to="/keys" onClick={closeDropdown} className="dropdown-item">
                      <svg
                        className="h-4 w-4"
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                        strokeWidth="1.5"
                      >
                        <path
                          strokeLinecap="round"
                          strokeLinejoin="round"
                          d="M15.75 5.25a3 3 0 013 3m3 0a6 6 0 01-7.029 5.912c-.563-.097-1.159.026-1.563.43L10.5 17.25H8.25v2.25H6v2.25H2.25v-2.818c0-.597.237-1.17.659-1.591l6.499-6.499c.404-.404.527-1 .43-1.563A6 6 0 1121.75 8.25z"
                        />
                      </svg>
                      {t('nav.apiKeys')}
                    </Link>

                    {user.role === 'admin' && (
                      <a
                        href="https://github.com/luckykuang/sub2api-plus"
                        target="_blank"
                        rel="noopener noreferrer"
                        onClick={closeDropdown}
                        className="dropdown-item"
                      >
                        <svg className="h-4 w-4" fill="currentColor" viewBox="0 0 24 24">
                          <path
                            fillRule="evenodd"
                            clipRule="evenodd"
                            d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.17 6.839 9.49.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.604-3.369-1.34-3.369-1.34-.454-1.156-1.11-1.464-1.11-1.464-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.831.092-.646.35-1.086.636-1.336-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.578 9.578 0 0112 6.836c.85.004 1.705.114 2.504.336 1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.203 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.743 0 .267.18.578.688.48C19.138 20.167 22 16.418 22 12c0-5.523-4.477-10-10-10z"
                          />
                        </svg>
                        {t('nav.github')}
                      </a>
                    )}
                  </div>

                  {contactInfo && (
                    <div className="border-t border-gray-100 px-4 py-2.5 dark:border-dark-700">
                      <div className="flex items-center gap-2 text-xs surface-muted">
                        <svg
                          className="h-3.5 w-3.5 flex-shrink-0"
                          fill="none"
                          viewBox="0 0 24 24"
                          stroke="currentColor"
                          strokeWidth="1.5"
                        >
                          <path
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            d="M20.25 8.511c.884.284 1.5 1.128 1.5 2.097v4.286c0 1.136-.847 2.1-1.98 2.193-.34.027-.68.052-1.02.072v3.091l-3-3c-1.354 0-2.694-.055-4.02-.163a2.115 2.115 0 01-.825-.242m9.345-8.334a2.126 2.126 0 00-.476-.095 48.64 48.64 0 00-8.048 0c-1.131.094-1.976 1.057-1.976 2.192v4.286c0 .837.46 1.58 1.155 1.951m9.345-8.334V6.637c0-1.621-1.152-3.026-2.76-3.235A48.455 48.455 0 0011.25 3c-2.115 0-4.198.137-6.24.402-1.608.209-2.76 1.614-2.76 3.235v6.226c0 1.621 1.152 3.026 2.76 3.235.577.075 1.157.14 1.74.194V21l4.155-4.155"
                          />
                        </svg>
                        <span>{t('common.contactSupport')}:</span>
                        <span className="font-medium surface-muted">{contactInfo}</span>
                      </div>
                    </div>
                  )}

                  {showOnboardingButton && (
                    <div className="border-t border-gray-100 py-1 dark:border-dark-700">
                      <button
                        type="button"
                        onClick={handleReplayGuide}
                        className="dropdown-item w-full surface-control"
                      >
                        <svg className="h-4 w-4" fill="currentColor" viewBox="0 0 24 24">
                          <path d="M12 2a10 10 0 100 20 10 10 0 000-20zm0 14a1 1 0 110 2 1 1 0 010-2zm1.07-7.75c0-.6-.49-1.25-1.32-1.25-.7 0-1.22.4-1.43 1.02a1 1 0 11-1.9-.62A3.41 3.41 0 0111.8 5c2.02 0 3.25 1.4 3.25 2.9 0 2-1.83 2.55-2.43 3.12-.43.4-.47.75-.47 1.23a1 1 0 01-2 0c0-1 .16-1.82 1.1-2.7.69-.64 1.82-1.05 1.82-2.06z" />
                        </svg>
                        {t('onboarding.restartTour')}
                      </button>
                    </div>
                  )}

                  <div className="border-t border-gray-100 py-1 dark:border-dark-700">
                    <button
                      type="button"
                      onClick={() => {
                        closeDropdown()
                        onLogout()
                      }}
                      className="dropdown-item w-full text-red-600 hover:bg-red-50/25 dark:text-red-400 dark:hover:bg-red-900/20 surface-control"
                    >
                      <svg
                        className="h-4 w-4"
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                        strokeWidth="1.5"
                      >
                        <path
                          strokeLinecap="round"
                          strokeLinejoin="round"
                          d="M15.75 9V5.25A2.25 2.25 0 0013.5 3h-6a2.25 2.25 0 00-2.25 2.25v13.5A2.25 2.25 0 007.5 21h6a2.25 2.25 0 002.25-2.25V15M12 9l-3 3m0 0l3 3m-3-3h12.75"
                        />
                      </svg>
                      {t('nav.logout')}
                    </button>
                  </div>
                </div>
              )}
            </div>
          )}

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

function readCustomPageId(pathname: string): Record<string, string | undefined> {
  const match = /^\/custom\/([^/]+)/.exec(pathname)
  return match ? { id: decodeURIComponent(match[1]) } : {}
}

export default AppHeader
