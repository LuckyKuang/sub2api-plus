import { useEffect } from 'react'
import { useAppStore } from '@/stores/app'
import { sanitizeUrl } from '@/utils/url'
import { useProductAppearance } from '@/components/auth/useProductAppearance'

export interface AuthLayoutProps {
  /** Page content rendered inside the auth card. */
  children?: React.ReactNode
  /** Content rendered below the card, outside it (e.g. a registration link). */
  footer?: React.ReactNode
}

/**
 * Centred single-column shell for the unauthenticated screens. Mirrors the Vue
 * AuthLayout: a brand block, the card that carries the page, an optional footer
 * beneath the card, and the copyright line.
 */
export function AuthLayout({ children, footer }: AuthLayoutProps) {
  useProductAppearance()

  const siteName = useAppStore((s) => s.siteName)
  const siteLogo = useAppStore((s) => s.siteLogo)
  const siteSubtitle = useAppStore((s) => s.cachedPublicSettings?.site_subtitle)
  const publicSettingsLoaded = useAppStore((s) => s.publicSettingsLoaded)
  const fetchPublicSettings = useAppStore((s) => s.fetchPublicSettings)

  // The router renders this shell before the settings request the app kicks off
  // at bootstrap has necessarily resolved, so make sure it is in flight.
  useEffect(() => {
    if (!publicSettingsLoaded) void fetchPublicSettings()
  }, [publicSettingsLoaded, fetchPublicSettings])

  const resolvedName = siteName || 'Sub2API Plus'
  const resolvedLogo = sanitizeUrl(siteLogo || '', { allowRelative: true, allowDataUrl: true })
  const resolvedSubtitle = siteSubtitle || 'Subscription to API Conversion Platform'

  return (
    <div className="auth-layout relative flex min-h-screen items-center justify-center px-4 py-10 sm:px-6">
      <div className="relative z-10 flex w-full max-w-md flex-col gap-6">
        {/* Brand */}
        <div className="auth-brand px-4 pb-2 text-center">
          {publicSettingsLoaded && (
            <>
              <div className="mb-4 inline-flex h-14 w-14 items-center justify-center overflow-hidden rounded-2xl">
                <img src={resolvedLogo || '/logo.svg'} alt="Logo" className="h-full w-full object-contain" />
              </div>
              <h1 className="mb-2 break-words text-2xl font-semibold tracking-tight text-gray-900 dark:text-white">
                {resolvedName}
              </h1>
              <p className="text-sm surface-muted">{resolvedSubtitle}</p>
            </>
          )}
        </div>

        {/* Card */}
        <div className="card auth-card p-6 sm:p-8">{children}</div>

        {footer && <div className="px-2 text-center text-sm surface-muted">{footer}</div>}

        <div className="text-center text-xs surface-muted">
          &copy; {new Date().getFullYear()} {resolvedName}. All rights reserved.
        </div>
      </div>
    </div>
  )
}

export default AuthLayout
