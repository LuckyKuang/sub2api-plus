import { useMemo } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { sanitizeUrl } from '@/utils/url'
import { cn } from '@/lib/utils'

export interface PlazaNavBarProps {
  className?: string
}

export function PlazaNavBar({ className }: PlazaNavBarProps) {
  const { t } = useTranslation()
  const settings = useAppStore((state) => state.cachedPublicSettings)
  const appStoreSiteName = useAppStore((state) => state.siteName)
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated())
  const isAdmin = useAuthStore((state) => state.isAdmin())

  const siteName = settings?.site_name || appStoreSiteName || 'Sub2API Plus'
  const siteLogo = sanitizeUrl(settings?.site_logo || '', { allowRelative: true, allowDataUrl: true })
  const backTarget = isAdmin ? '/admin/dashboard' : '/dashboard'

  const buttonClass = useMemo(
    () =>
      cn(
        'inline-flex flex-shrink-0 items-center justify-center rounded-xl bg-gradient-to-r from-primary-500 to-primary-600 px-4 py-2 text-sm font-semibold text-white shadow-md shadow-primary-500/25 transition-all duration-200 hover:from-primary-600 hover:to-primary-700 hover:shadow-lg hover:shadow-primary-500/30 active:scale-[0.98] dark:shadow-primary-500/20'
      ),
    []
  )

  return (
    <header className={cn('shell-surface sticky top-[var(--shell-inset-top)] z-30', className)}>
      <div className="mx-auto flex max-w-7xl items-center justify-between gap-4 px-4 py-3.5 sm:px-6">
        {/* 左:站点 logo + 名称 */}
        <div className="flex min-w-0 items-center gap-3">
          {settings ? (
            <>
              <span className="flex h-9 w-9 flex-shrink-0 items-center justify-center overflow-hidden rounded-xl surface-inset shadow-sm ring-1 ring-gray-200 dark:ring-dark-700">
                <img src={siteLogo || '/logo.svg'} alt="Logo" className="h-full w-full object-contain" />
              </span>
              <span className="truncate text-base font-semibold text-gray-950 dark:text-white">{siteName}</span>
            </>
          ) : (
            <>
              <span className="h-9 w-9 flex-shrink-0 animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700" aria-hidden="true" />
              <span className="h-5 w-28 animate-pulse rounded bg-gray-200 dark:bg-dark-700" aria-hidden="true" />
            </>
          )}
        </div>

        {/* 右:登录 / 回到后台 */}
        {isAuthenticated ? (
          <Link to={backTarget} className={buttonClass}>
            {t('modelPlaza.nav.backToDashboard')}
          </Link>
        ) : (
          <Link to={{ pathname: '/login', search: '?redirect=/model-plaza' }} className={buttonClass}>
            {t('modelPlaza.nav.login')}
          </Link>
        )}
      </div>
    </header>
  )
}

export default PlazaNavBar
