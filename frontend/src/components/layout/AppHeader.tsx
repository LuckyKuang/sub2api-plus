import { useTranslation } from 'react-i18next'
import { Languages, LogOut, Menu as MenuIcon, Moon, Sun, User as UserIcon } from 'lucide-react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useState } from 'react'
import { availableLocales, setLocale } from '@/i18n'
import { useTranslation as useT } from 'react-i18next'
import { cn } from '@/lib/utils'

export interface AppHeaderProps {
  siteName: string
  siteLogo?: string
  userName?: string
  isAdmin?: boolean
  balance?: number
  onToggleMobile: () => void
  onToggleTheme: () => void
  theme: 'light' | 'dark'
  onLogout: () => void
}

export function AppHeader({
  siteName,
  siteLogo,
  userName,
  onToggleMobile,
  onToggleTheme,
  theme,
  onLogout
}: AppHeaderProps) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const [menuOpen, setMenuOpen] = useState(false)
  const [localeOpen, setLocaleOpen] = useState(false)

  return (
    <header className="shell-header surface-shell sticky top-0 z-30 flex h-16 items-center gap-3 border-b px-4 lg:px-6" style={{ borderColor: 'var(--surface-edge)' }}>
      <button
        type="button"
        onClick={onToggleMobile}
        aria-label="Menu"
        className="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 lg:hidden dark:hover:bg-dark-800"
      >
        <MenuIcon className="h-5 w-5" />
      </button>

      <div className="min-w-0 flex-1">
        <h1 className="truncate text-base font-semibold text-gray-900 dark:text-white">{siteName}</h1>
      </div>

      <div className="relative">
        <button
          type="button"
          onClick={() => {
            setLocaleOpen((value) => !value)
            setMenuOpen(false)
          }}
          aria-label="Language"
          className="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 dark:hover:bg-dark-800"
        >
          <Languages className="h-5 w-5" />
        </button>
        {localeOpen && (
          <div className="dropdown surface-panel header-user-dropdown absolute right-0 top-full mt-2 w-40">
            {availableLocales.map((locale) => (
              <button
                key={locale.code}
                type="button"
                onClick={() => {
                  void setLocale(locale.code)
                  setLocaleOpen(false)
                }}
                className={cn(
                  'dropdown-item w-full',
                  i18n.language === locale.code && 'surface-selected'
                )}
              >
                <span>{locale.flag}</span>
                <span>{locale.name}</span>
              </button>
            ))}
          </div>
        )}
      </div>

      <button
        type="button"
        onClick={onToggleTheme}
        aria-label={t('common.toggleTheme')}
        className="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 dark:hover:bg-dark-800"
      >
        {theme === 'dark' ? <Sun className="h-5 w-5" /> : <Moon className="h-5 w-5" />}
      </button>

      <div className="relative">
        <button
          type="button"
          onClick={() => {
            setMenuOpen((value) => !value)
            setLocaleOpen(false)
          }}
          className="flex items-center gap-2 rounded-xl px-2 py-1.5 transition-colors hover:bg-gray-100 dark:hover:bg-dark-800"
        >
          <span className="flex h-8 w-8 items-center justify-center rounded-full bg-primary-100 text-primary-600 dark:bg-primary-900/30 dark:text-primary-400">
            <UserIcon className="h-4 w-4" />
          </span>
          <span className="hidden max-w-[120px] truncate text-sm font-medium text-gray-700 sm:block dark:text-gray-200">
            {userName}
          </span>
        </button>
        {menuOpen && (
          <div className="dropdown surface-panel header-user-dropdown absolute right-0 top-full mt-2 w-48">
            <button
              type="button"
              onClick={() => {
                setMenuOpen(false)
                navigate('/profile')
              }}
              className="dropdown-item w-full"
            >
              <UserIcon className="h-4 w-4" />
              <span>{t('nav.profile')}</span>
            </button>
            <button
              type="button"
              onClick={() => {
                setMenuOpen(false)
                onLogout()
              }}
              className="dropdown-item w-full"
            >
              <LogOut className="h-4 w-4" />
              <span>{t('nav.logout')}</span>
            </button>
          </div>
        )}
      </div>

      {siteLogo && location.pathname === '/home' && <img src={siteLogo} alt="" className="hidden h-8 w-8 rounded-lg" />}
    </header>
  )
}

export function useLocaleLabel() {
  const { t } = useT()
  return t
}
