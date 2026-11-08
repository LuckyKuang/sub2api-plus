import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/auth/Icon'
import { LocaleSwitcher } from '@/components/auth/LocaleSwitcher'
import { useProductAppearance } from '@/components/auth/useProductAppearance'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import '@/views/authViews.css'

const GITHUB_URL = 'https://github.com/luckykuang/sub2api-plus'

export default function HomeView() {
  const { t } = useTranslation()
  const appStore = useAppStore()
  const authStore = useAuthStore()

  useProductAppearance()

  const isAuthenticated = authStore.isAuthenticated()
  const isAdmin = authStore.isAdmin()
  const userEmail = authStore.user?.email

  // Site settings - directly from appStore (already initialized from injected config)
  const settings = appStore.cachedPublicSettings
  const siteName = settings?.site_name || appStore.siteName || 'Sub2API Plus'
  const siteLogo = sanitizeUrl(settings?.site_logo || appStore.siteLogo || '', {
    allowRelative: true,
    allowDataUrl: true
  })
  const siteSubtitle = settings?.site_subtitle || 'AI API Gateway Platform'
  const docUrl = sanitizeUrl(settings?.doc_url || appStore.docUrl || '')
  const homeContent = settings?.home_content || ''
  const hasHomeContent = homeContent.trim().length > 0
  const compactHomeEnabled = settings?.compact_home_enabled === true
  const modelPlazaEnabled = isFeatureFlagEnabled(FeatureFlags.modelPlaza)

  // Check if homeContent is a URL (for iframe display)
  const isHomeContentUrl = homeContent.trim().startsWith('http://') || homeContent.trim().startsWith('https://')

  // Theme
  const [isDark, setIsDark] = useState(
    () => typeof document !== 'undefined' && document.documentElement.classList.contains('dark')
  )

  const modelPlazaRequiresAuth = settings?.model_plaza_require_auth === true
  const showModelPlazaEntry = modelPlazaEnabled && (isAuthenticated || !modelPlazaRequiresAuth)
  const dashboardPath = isAdmin ? '/admin/dashboard' : '/dashboard'
  const userInitial = userEmail ? userEmail.charAt(0).toUpperCase() : ''

  const currentYear = useMemo(() => new Date().getFullYear(), [])

  function toggleTheme() {
    const next = !isDark
    setIsDark(next)
    document.documentElement.classList.toggle('dark', next)
    localStorage.setItem('theme', next ? 'dark' : 'light')
  }

  function initTheme() {
    const savedTheme = localStorage.getItem('theme')
    if (
      savedTheme === 'dark' ||
      (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
    ) {
      setIsDark(true)
      document.documentElement.classList.add('dark')
    }
  }

  useEffect(() => {
    initTheme()

    // Check auth state
    authStore.checkAuth()

    // Ensure public settings are loaded (will use cache if already loaded from injected config)
    if (!appStore.publicSettingsLoaded) {
      void appStore.fetchPublicSettings()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Custom Home Content: Full Page Mode
  if (hasHomeContent) {
    return (
      <div className="home-page min-h-screen p-4 md:p-6 lg:p-8">
        <div className="card overflow-hidden">
          {isHomeContentUrl ? (
            <iframe
              src={homeContent.trim()}
              className="h-[calc(100dvh-2rem)] w-full border-0 md:h-[calc(100dvh-3rem)] lg:h-[calc(100dvh-4rem)]"
              allowFullScreen
            />
          ) : (
            /* HTML mode - SECURITY: homeContent is admin-only setting, XSS risk is acceptable */
            <div dangerouslySetInnerHTML={{ __html: homeContent }} />
          )}
        </div>
      </div>
    )
  }

  // Compact Home Page
  if (compactHomeEnabled) {
    return (
      <div
        data-testid="compact-home"
        className="home-page home-compact flex min-h-screen flex-col gap-6 p-4 text-gray-900 md:p-6 lg:p-8 dark:text-white"
      >
        <header className="shell-surface px-4 py-4 sm:px-6">
          <nav className="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-3 sm:gap-4">
            <div className="flex min-w-0 flex-1 items-center gap-3">
              <img src={siteLogo || '/logo.svg'} alt="Logo" className="h-9 w-9 shrink-0 rounded-lg object-contain" />
              <span className="min-w-0 truncate text-base font-semibold">{siteName}</span>
            </div>
            <div className="flex max-w-full shrink-0 flex-wrap items-center justify-end gap-2">
              <LocaleSwitcher />
              {docUrl && (
                <a
                  href={docUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg hover:surface-hover surface-muted"
                  title={t('home.viewDocs')}
                >
                  <Icon name="book" size="md" />
                </a>
              )}
              {showModelPlazaEntry && (
                <Link
                  to="/model-plaza"
                  className="flex h-10 shrink-0 items-center gap-1.5 rounded-lg px-2.5 text-sm font-medium text-gray-500 hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
                  title={t('nav.modelPlaza')}
                >
                  <Icon name="grid" size="md" />
                  <span className="hidden sm:inline">{t('nav.modelPlaza')}</span>
                </Link>
              )}
              <button
                className="theme-toggle flex h-10 w-10 shrink-0 items-center justify-center rounded-lg surface-control"
                title={isDark ? t('home.switchToLight') : t('home.switchToDark')}
                onClick={toggleTheme}
              >
                <Icon name={isDark ? 'sun' : 'moon'} size="md" />
              </button>
              <Link
                to={isAuthenticated ? dashboardPath : '/login'}
                className="inline-flex min-h-10 shrink-0 items-center justify-center rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200"
              >
                {isAuthenticated ? t('home.dashboard') : t('home.login')}
              </Link>
            </div>
          </nav>
        </header>

        <main className="flex min-w-0 flex-1 items-center justify-center">
          <div className="card w-full min-w-0 max-w-2xl px-4 py-12 text-center sm:px-8 sm:py-16">
            <img
              src={siteLogo || '/logo.svg'}
              alt="Logo"
              className="mx-auto mb-6 h-20 w-20 rounded-2xl object-contain"
            />
            <h1 className="[overflow-wrap:anywhere] text-3xl font-bold md:text-4xl">{siteName}</h1>
            <p className="mt-4 whitespace-pre-wrap [overflow-wrap:anywhere] text-base surface-muted">{siteSubtitle}</p>
            <Link
              to={isAuthenticated ? dashboardPath : '/login'}
              className="mt-8 inline-flex min-h-10 items-center justify-center rounded-lg bg-primary-500 px-5 py-2.5 text-sm font-medium text-primary-950 hover:bg-primary-400"
            >
              {isAuthenticated ? t('home.goToDashboard') : t('home.login')}
            </Link>
          </div>
        </main>

        <footer className="shell-surface min-w-0 px-4 py-5 text-center text-sm [overflow-wrap:anywhere] sm:px-6 surface-muted">
          &copy; {currentYear} {siteName}
        </footer>
      </div>
    )
  }

  // Default Home Page
  return (
    <div className="home-page home-default relative flex min-h-screen flex-col gap-8 p-4 md:p-6 lg:p-8">
      {/* Header */}
      <header className="home-nav relative z-20 px-2 py-2 sm:px-4">
        <nav className="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-3">
          {/* Logo */}
          <div className="flex min-w-0 items-center gap-3">
            <div className="h-10 w-10 shrink-0 overflow-hidden rounded-xl">
              <img src={siteLogo || '/logo.svg'} alt="Logo" className="h-full w-full object-contain" />
            </div>
            <span className="max-w-[14rem] truncate text-base font-semibold tracking-tight">{siteName}</span>
          </div>

          {/* Nav Actions */}
          <div className="flex max-w-full flex-wrap items-center gap-2 sm:gap-3">
            {/* Language Switcher */}
            <LocaleSwitcher />

            {/* Doc Link */}
            {docUrl && (
              <a
                href={docUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="rounded-lg p-2 transition-colors hover:surface-hover hover:text-gray-700 dark:hover:text-white surface-muted"
                title={t('home.viewDocs')}
              >
                <Icon name="book" size="md" />
              </a>
            )}

            {/* Model Plaza Link */}
            {showModelPlazaEntry && (
              <Link
                to="/model-plaza"
                className="inline-flex items-center gap-1.5 rounded-lg p-2 text-sm text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
                title={t('nav.modelPlaza')}
              >
                <Icon name="grid" size="md" />
                <span className="hidden sm:inline">{t('nav.modelPlaza')}</span>
              </Link>
            )}

            {/* Theme Toggle */}
            <button
              onClick={toggleTheme}
              className="theme-toggle rounded-lg p-2 transition-colors surface-control"
              title={isDark ? t('home.switchToLight') : t('home.switchToDark')}
            >
              <Icon name={isDark ? 'sun' : 'moon'} size="md" />
            </button>

            {/* Login / Dashboard Button */}
            {isAuthenticated ? (
              <Link
                to={dashboardPath}
                className="inline-flex items-center gap-1.5 rounded-full bg-gray-900 py-1 pl-1 pr-2.5 transition-colors hover:bg-gray-800 dark:bg-gray-800 dark:hover:bg-gray-700"
              >
                <span className="flex h-5 w-5 items-center justify-center rounded-full bg-gradient-to-br from-primary-400 to-primary-600 text-[10px] font-semibold text-white">
                  {userInitial}
                </span>
                <span className="text-xs font-medium text-white">{t('home.dashboard')}</span>
                <svg className="h-3 w-3 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M4.5 19.5l15-15m0 0H8.25m11.25 0v11.25" />
                </svg>
              </Link>
            ) : (
              <Link
                to="/login"
                className="inline-flex items-center rounded-full bg-gray-900 px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-gray-800 dark:bg-gray-800 dark:hover:bg-gray-700"
              >
                {t('home.login')}
              </Link>
            )}
          </div>
        </nav>
      </header>

      {/* Main Content */}
      <main className="relative z-10 min-w-0 flex-1">
        <div className="mx-auto flex max-w-6xl flex-col gap-[var(--shell-gap)]">
          {/* Hero Section - Left/Right Layout */}
          <section className="home-hero flex min-w-0 flex-col items-center justify-between gap-10 px-2 py-8 sm:px-6 sm:py-12 lg:flex-row lg:gap-14 lg:py-16">
            {/* Left: Text Content */}
            <div className="min-w-0 flex-1 text-center lg:text-left">
              <h1 className="mb-5 break-words text-4xl font-semibold tracking-tight text-gray-900 dark:text-white md:text-5xl lg:text-6xl">
                {siteName}
              </h1>
              <p className="mb-8 whitespace-pre-wrap text-base leading-relaxed md:text-lg surface-muted">
                {siteSubtitle}
              </p>

              {/* CTA Button */}
              <div>
                <Link to={isAuthenticated ? dashboardPath : '/login'} className="btn btn-primary px-6 py-3 text-sm">
                  {isAuthenticated ? t('home.goToDashboard') : t('home.getStarted')}
                  <Icon name="arrowRight" size="md" className="ml-2" strokeWidth={2} />
                </Link>
              </div>
            </div>

            {/* Right: Terminal Animation */}
            <div className="flex w-full min-w-0 flex-1 justify-center lg:justify-end">
              <div className="terminal-container">
                <div className="terminal-window">
                  {/* Window header */}
                  <div className="terminal-header">
                    <div className="terminal-buttons">
                      <span className="btn-close" />
                      <span className="btn-minimize" />
                      <span className="btn-maximize" />
                    </div>
                    <span className="terminal-title">terminal</span>
                  </div>
                  {/* Terminal content */}
                  <div className="terminal-body">
                    <div className="code-line line-1">
                      <span className="code-prompt">$</span>
                      <span className="code-cmd">curl</span>
                      <span className="code-flag">-X POST</span>
                      <span className="code-url">/v1/messages</span>
                    </div>
                    <div className="code-line line-2">
                      <span className="code-comment"># Routing to upstream...</span>
                    </div>
                    <div className="code-line line-3">
                      <span className="code-success">200 OK</span>
                      <span className="code-response">{' "content": "Hello!" }'}</span>
                    </div>
                    <div className="code-line line-4">
                      <span className="code-prompt">$</span>
                      <span className="cursor" />
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </section>

          {/* Feature Tags - Centered */}
          <div className="flex flex-wrap items-center justify-center gap-3 md:gap-4">
            <div className="inline-flex items-center gap-2.5 rounded-full border border-gray-200/50 surface-inset px-5 py-2.5 shadow-sm dark:border-dark-700/50">
              <Icon name="swap" size="sm" className="text-primary-500" />
              <span className="text-sm font-medium surface-muted">{t('home.tags.subscriptionToApi')}</span>
            </div>
            <div className="inline-flex items-center gap-2.5 rounded-full border border-gray-200/50 surface-inset px-5 py-2.5 shadow-sm dark:border-dark-700/50">
              <Icon name="shield" size="sm" className="text-primary-500" />
              <span className="text-sm font-medium surface-muted">{t('home.tags.stickySession')}</span>
            </div>
            <div className="inline-flex items-center gap-2.5 rounded-full border border-gray-200/50 surface-inset px-5 py-2.5 shadow-sm dark:border-dark-700/50">
              <Icon name="chart" size="sm" className="text-primary-500" />
              <span className="text-sm font-medium surface-muted">{t('home.tags.realtimeBilling')}</span>
            </div>
          </div>

          {/* Features Grid */}
          <div className="grid gap-[var(--shell-gap)] md:grid-cols-3">
            {/* Feature 1: Unified Gateway */}
            <div className="card home-feature p-6">
              <div className="home-feature-icon mb-5 flex h-10 w-10 items-center justify-center rounded-xl">
                <Icon name="server" size="md" />
              </div>
              <h3 className="mb-2 text-lg font-semibold text-gray-900 dark:text-white">
                {t('home.features.unifiedGateway')}
              </h3>
              <p className="text-sm leading-relaxed surface-muted">{t('home.features.unifiedGatewayDesc')}</p>
            </div>

            {/* Feature 2: Account Pool */}
            <div className="card home-feature p-6">
              <div className="home-feature-icon mb-5 flex h-10 w-10 items-center justify-center rounded-xl">
                <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="1.5">
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    d="M18 18.72a9.094 9.094 0 003.741-.479 3 3 0 00-4.682-2.72m.94 3.198l.001.031c0 .225-.012.447-.037.666A11.944 11.944 0 0112 21c-2.17 0-4.207-.576-5.963-1.584A6.062 6.062 0 016 18.719m12 0a5.971 5.971 0 00-.941-3.197m0 0A5.995 5.995 0 0012 12.75a5.995 5.995 0 00-5.058 2.772m0 0a3 3 0 00-4.681 2.72 8.986 8.986 0 003.74.477m.94-3.197a5.971 5.971 0 00-.94 3.197M15 6.75a3 3 0 11-6 0 3 3 0 016 0zm6 3a2.25 2.25 0 11-4.5 0 2.25 2.25 0 014.5 0zm-13.5 0a2.25 2.25 0 11-4.5 0 2.25 2.25 0 014.5 0z"
                  />
                </svg>
              </div>
              <h3 className="mb-2 text-lg font-semibold text-gray-900 dark:text-white">{t('home.features.multiAccount')}</h3>
              <p className="text-sm leading-relaxed surface-muted">{t('home.features.multiAccountDesc')}</p>
            </div>

            {/* Feature 3: Billing & Quota */}
            <div className="card home-feature p-6">
              <div className="home-feature-icon mb-5 flex h-10 w-10 items-center justify-center rounded-xl">
                <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="1.5">
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    d="M2.25 18.75a60.07 60.07 0 0115.797 2.101c.727.198 1.453-.342 1.453-1.096V18.75M3.75 4.5v.75A.75.75 0 013 6h-.75m0 0v-.375c0-.621.504-1.125 1.125-1.125H20.25M2.25 6v9m18-10.5v.75c0 .414.336.75.75.75h.75m-1.5-1.5h.375c.621 0 1.125.504 1.125 1.125v9.75c0 .621-.504 1.125-1.125-1.125h-.375m1.5-1.5H21a.75.75 0 00-.75.75v.75m0 0H3.75m0 0h-.375a1.125 1.125 0 01-1.125-1.125V15m1.5 1.5v-.75A.75.75 0 003 15h-.75M15 10.5a3 3 0 11-6 0 3 3 0 016 0zm3 0h.008v.008H18V10.5zm-12 0h.008v.008H6V10.5z"
                  />
                </svg>
              </div>
              <h3 className="mb-2 text-lg font-semibold text-gray-900 dark:text-white">
                {t('home.features.balanceQuota')}
              </h3>
              <p className="text-sm leading-relaxed surface-muted">{t('home.features.balanceQuotaDesc')}</p>
            </div>
          </div>

          {/* Supported Providers */}
          <section className="card p-5 sm:p-8">
            <div className="mb-6 text-center">
              <h2 className="mb-3 text-2xl font-bold text-gray-900 dark:text-white">{t('home.providers.title')}</h2>
              <p className="text-sm surface-muted">{t('home.providers.description')}</p>
            </div>

            <div className="flex flex-wrap items-center justify-center gap-4">
              {/* Claude - Supported */}
              <div className="flex items-center gap-2 rounded-xl border border-primary-200 surface-inset px-5 py-3 ring-1 ring-primary-500/20 dark:border-primary-800">
                <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-orange-400 to-orange-500">
                  <span className="text-xs font-bold text-white">C</span>
                </div>
                <span className="text-sm font-medium surface-muted">{t('home.providers.claude')}</span>
                <span className="rounded bg-primary-100/[0.15] px-1.5 py-0.5 text-[10px] font-medium text-primary-600 dark:bg-primary-900/30 dark:text-primary-400">
                  {t('home.providers.supported')}
                </span>
              </div>
              {/* GPT - Supported */}
              <div className="flex items-center gap-2 rounded-xl border border-primary-200 surface-inset px-5 py-3 ring-1 ring-primary-500/20 dark:border-primary-800">
                <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-green-500 to-green-600">
                  <span className="text-xs font-bold text-white">G</span>
                </div>
                <span className="text-sm font-medium surface-muted">GPT</span>
                <span className="rounded bg-primary-100/[0.15] px-1.5 py-0.5 text-[10px] font-medium text-primary-600 dark:bg-primary-900/30 dark:text-primary-400">
                  {t('home.providers.supported')}
                </span>
              </div>
              {/* Gemini - Supported */}
              <div className="flex items-center gap-2 rounded-xl border border-primary-200 surface-inset px-5 py-3 ring-1 ring-primary-500/20 dark:border-primary-800">
                <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-blue-500 to-blue-600">
                  <span className="text-xs font-bold text-white">G</span>
                </div>
                <span className="text-sm font-medium surface-muted">{t('home.providers.gemini')}</span>
                <span className="rounded bg-primary-100/[0.15] px-1.5 py-0.5 text-[10px] font-medium text-primary-600 dark:bg-primary-900/30 dark:text-primary-400">
                  {t('home.providers.supported')}
                </span>
              </div>
              {/* Antigravity - Supported */}
              <div className="flex items-center gap-2 rounded-xl border border-primary-200 surface-inset px-5 py-3 ring-1 ring-primary-500/20 dark:border-primary-800">
                <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-rose-500 to-pink-600">
                  <span className="text-xs font-bold text-white">A</span>
                </div>
                <span className="text-sm font-medium surface-muted">{t('home.providers.antigravity')}</span>
                <span className="rounded bg-primary-100/[0.15] px-1.5 py-0.5 text-[10px] font-medium text-primary-600 dark:bg-primary-900/30 dark:text-primary-400">
                  {t('home.providers.supported')}
                </span>
              </div>
              {/* More - Coming Soon */}
              <div className="flex items-center gap-2 rounded-xl border border-gray-200/50 surface-inset px-5 py-3 opacity-60 dark:border-dark-700/50">
                <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-gray-500 to-gray-600">
                  <span className="text-xs font-bold text-white">+</span>
                </div>
                <span className="text-sm font-medium surface-muted">{t('home.providers.more')}</span>
                <span className="rounded surface-inset px-1.5 py-0.5 text-[10px] font-medium surface-muted">
                  {t('home.providers.soon')}
                </span>
              </div>
            </div>
          </section>
        </div>
      </main>

      {/* Footer */}
      <footer className="home-footer relative z-10 px-2 py-6 sm:px-4">
        <div className="mx-auto flex max-w-6xl flex-col items-center justify-center gap-4 text-center sm:flex-row sm:text-left">
          <p className="text-sm surface-muted">
            &copy; {currentYear} {siteName}. {t('home.footer.allRightsReserved')}
          </p>
          <div className="flex items-center gap-4">
            {docUrl && (
              <a
                href={docUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="text-sm transition-colors hover:text-gray-700 dark:hover:text-white surface-muted"
              >
                {t('home.docs')}
              </a>
            )}
            <a
              href={GITHUB_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="text-sm transition-colors hover:text-gray-700 dark:hover:text-white surface-muted"
            >
              GitHub
            </a>
          </div>
        </div>
      </footer>
    </div>
  )
}
