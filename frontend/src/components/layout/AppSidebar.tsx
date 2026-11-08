import { useMemo, useState } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { ChevronDown, PanelLeftClose, PanelLeftOpen } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { navSections, type NavContext, type NavItem } from './navConfig'

export interface AppSidebarProps extends NavContext {
  collapsed: boolean
  onToggleCollapsed: () => void
  mobileOpen: boolean
  onCloseMobile: () => void
}

export function AppSidebar({
  collapsed,
  onToggleCollapsed,
  mobileOpen,
  onCloseMobile,
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
        <div className="sidebar-header">
          <BrandMark collapsed={collapsed} siteName={ctx.t('app.name')} />
          <button
            type="button"
            onClick={onToggleCollapsed}
            aria-label={collapsed ? t('common.expand') : t('common.collapse')}
            className="ml-auto hidden rounded-lg p-1.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 lg:block dark:hover:bg-dark-800"
          >
            {collapsed ? <PanelLeftOpen className="h-4 w-4" /> : <PanelLeftClose className="h-4 w-4" />}
          </button>
        </div>

        <nav className="sidebar-nav">
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
      </aside>
    </>
  )
}

function BrandMark({ collapsed, siteName }: { collapsed: boolean; siteName: string }) {
  const location = useLocation()
  const logoUrl = siteName && siteName !== 'Sub2API Plus' ? siteName : '/logo.svg'
  return (
    <NavLink to="/home" className="flex min-w-0 items-center gap-2.5">
      <img
        src={logoUrl}
        alt=""
        className="h-8 w-8 shrink-0 rounded-lg"
        onError={(event) => {
          event.currentTarget.src = '/logo.svg'
        }}
      />
      {!collapsed && (
        <span className="truncate text-[15px] font-semibold text-gray-900 dark:text-white">
          {location.pathname.startsWith('/admin') ? 'Admin' : 'Sub2API'}
        </span>
      )}
    </NavLink>
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
