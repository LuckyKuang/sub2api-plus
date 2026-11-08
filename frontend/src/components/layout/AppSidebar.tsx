import { useMemo, useState } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { ChevronDown, ChevronsLeft, ChevronsRight, Moon, Sun, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { navSections, type NavContext, type NavItem } from './navConfig'
import { VersionBadge } from '@/components/VersionBadge'

/** Anchors the interactive tour highlights. */
const TOUR_ANCHOR_IDS: Record<string, string> = {
  '/admin/accounts': 'sidebar-channel-manage',
  '/admin/groups': 'sidebar-group-manage',
  '/admin/redeem': 'sidebar-wallet'
}

export interface AppSidebarProps extends NavContext {
  collapsed: boolean
  onToggleCollapsed: () => void
  mobileOpen: boolean
  onCloseMobile: () => void
  /** Current theme, for the footer toggle's icon and label. */
  theme: 'light' | 'dark'
  onToggleTheme: () => void
  /** Only the desktop shell offers the collapse control. */
  isDesktop: boolean
  siteName: string
  siteLogo: string
}

export function AppSidebar({
  collapsed,
  onToggleCollapsed,
  mobileOpen,
  onCloseMobile,
  theme,
  onToggleTheme,
  isDesktop,
  siteName,
  siteLogo,
  ...ctx
}: AppSidebarProps) {
  const { t } = useTranslation()
  const location = useLocation()
  const sections = useMemo(() => navSections(ctx), [ctx])

  const isGroupActive = (item: NavItem) =>
    item.path === location.pathname ||
    (item.children ?? []).some((child) => child.path === location.pathname)

  return (
    <>
      {mobileOpen && (
        <div
          className="fixed inset-0 z-40 bg-black/40 lg:hidden"
          onClick={onCloseMobile}
          aria-hidden="true"
        />
      )}
      <aside
        className={cn(
          'sidebar surface-shell fixed inset-y-0 left-0 z-50 flex flex-col border-r transition-all duration-200 lg:static lg:z-auto',
          collapsed ? 'lg:w-[76px]' : 'lg:w-[248px]',
          mobileOpen ? 'w-[248px] translate-x-0' : '-translate-x-full lg:translate-x-0'
        )}
        style={{ borderColor: 'var(--surface-edge)' }}
      >
        <div className={cn('sidebar-header shell-surface', collapsed && 'sidebar-header-collapsed')}>
          <a
            href="/home"
            className="sidebar-logo flex h-9 w-9 items-center justify-center overflow-hidden rounded-xl shadow-glow transition-opacity hover:opacity-80"
          >
            <img src={siteLogo || '/logo.svg'} alt="Logo" className="h-full w-full object-contain" />
          </a>
          <div className={cn('sidebar-brand', collapsed && 'sidebar-brand-collapsed')}>
            <a
              href="/home"
              className="sidebar-brand-title text-lg font-bold text-gray-900 transition-colors hover:text-primary-600 dark:text-white dark:hover:text-primary-400"
            >
              {siteName}
            </a>
            {ctx.isAdmin && <VersionBadge />}
          </div>
          {mobileOpen && (
            <button
              type="button"
              className="sidebar-mobile-close btn-ghost btn-icon surface-control"
              aria-label={t('common.close')}
              onClick={onCloseMobile}
            >
              <X className="h-5 w-5" />
            </button>
          )}
        </div>

        <div className="sidebar-panel shell-surface flex min-h-0 flex-1 flex-col">
        <nav className="sidebar-nav scrollbar-hide">
          {sections.map((section, sectionIndex) => (
            <div key={sectionIndex} className="sidebar-section">
              {section.items.map((item) => (
                <SidebarLink
                  key={item.path}
                  item={item}
                  collapsed={collapsed}
                  active={isGroupActive(item)}
                  onNavigate={onCloseMobile}
                />
              ))}
            </div>
          ))}
        </nav>

        <div className="sidebar-footer mt-auto border-t border-gray-100 p-3 dark:border-dark-800">
          <button
            type="button"
            onClick={onToggleTheme}
            className={cn('sidebar-link theme-toggle mb-2 w-full surface-control', collapsed && 'sidebar-link-collapsed')}
            title={collapsed ? (theme === 'dark' ? t('nav.lightMode') : t('nav.darkMode')) : undefined}
          >
            {theme === 'dark' ? <Sun className="h-5 w-5 flex-shrink-0" /> : <Moon className="h-5 w-5 flex-shrink-0" />}
            <span className={cn('sidebar-label', collapsed && 'sidebar-label-collapsed')}>
              {theme === 'dark' ? t('nav.lightMode') : t('nav.darkMode')}
            </span>
          </button>

          {isDesktop && (
            <button
              type="button"
              onClick={onToggleCollapsed}
              className={cn('sidebar-link w-full surface-control', collapsed && 'sidebar-link-collapsed')}
              title={collapsed ? t('nav.expand') : t('nav.collapse')}
            >
              {collapsed ? <ChevronsRight className="h-5 w-5 flex-shrink-0" /> : <ChevronsLeft className="h-5 w-5 flex-shrink-0" />}
              <span className={cn('sidebar-label', collapsed && 'sidebar-label-collapsed')}>{t('nav.collapse')}</span>
            </button>
          )}
        </div>
        </div>
      </aside>
    </>
  )
}


function SidebarLink({
  item,
  collapsed,
  active,
  onNavigate
}: {
  item: NavItem
  collapsed: boolean
  active: boolean
  onNavigate: () => void
}) {
  const location = useLocation()
  const hasChildren = (item.children?.length ?? 0) > 0
  const childActive = hasChildren && item.children!.some((child) => child.path === location.pathname)
  const [open, setOpen] = useState(childActive)

  if (hasChildren) {
    return (
      <div>
        <button
          type="button"
          onClick={() => setOpen((value) => !value)}
          className={cn('sidebar-link w-full', active && 'sidebar-link-active')}
          title={collapsed ? item.label : undefined}
        >
          <span className="shrink-0">{item.icon}</span>
          {!collapsed && (
            <>
              <span className="min-w-0 flex-1 truncate text-left">{item.label}</span>
              <ChevronDown className={cn('h-4 w-4 shrink-0 transition-transform', open && 'rotate-180')} />
            </>
          )}
        </button>
        {open && !collapsed && (
          <div className="mt-1 space-y-0.5 pl-[26px]">
            {item.children!.map((child) => (
              <NavLink
                key={child.path}
                to={child.path}
                onClick={onNavigate}
                className={({ isActive }) =>
                  cn(
                    'sidebar-link !py-2 text-[13px]',
                    isActive && 'sidebar-link-active'
                  )
                }
              >
                <span className="shrink-0">{child.icon}</span>
                <span className="min-w-0 flex-1 truncate">{child.label}</span>
              </NavLink>
            ))}
          </div>
        )}
      </div>
    )
  }

  return (
    <NavLink
      to={item.path}
      onClick={onNavigate}
      title={collapsed ? item.label : undefined}
      id={TOUR_ANCHOR_IDS[item.path]}
      className={({ isActive }) => cn('sidebar-link', isActive && 'sidebar-link-active')}
    >
      <span className="shrink-0">
        {item.iconSvg ? (
          <img src={item.iconSvg} alt="" className="h-[18px] w-[18px]" />
        ) : (
          item.icon
        )}
      </span>
      {!collapsed && <span className="min-w-0 flex-1 truncate">{item.label}</span>}
    </NavLink>
  )
}
