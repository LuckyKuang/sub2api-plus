import { useEffect, useRef, useState } from 'react'
import { cn } from '@/lib/utils'

/**
 * Stand-in for the Vue `TablePageLayout` until the shared layout component
 * lands. Keeps the same section structure (`table-page-layout workspace-page`
 * with fixed filter / scrollable table / fixed pagination zones) and the same
 * desktop height calculation so the ported admin views scroll identically.
 *
 * `table` is the scrollable table zone. `children` is rendered outside every
 * zone: the Vue original exposes no default slot, so children are overlays
 * (dialogs), not table content.
 */
export interface AdminTablePageLayoutProps {
  filters?: React.ReactNode
  actions?: React.ReactNode
  table?: React.ReactNode
  pagination?: React.ReactNode
  children?: React.ReactNode
}

export function AdminTablePageLayout({ filters, actions, table, pagination, children }: AdminTablePageLayoutProps) {
  const layoutRef = useRef<HTMLDivElement>(null)
  const [isMobile, setIsMobile] = useState(() => window.innerWidth < 1024)
  const [availableHeight, setAvailableHeight] = useState('auto')

  useEffect(() => {
    const check = () => {
      const mobile = window.innerWidth < 1024
      setIsMobile(mobile)
      if (mobile || !layoutRef.current) {
        setAvailableHeight('auto')
        return
      }
      const top = layoutRef.current.getBoundingClientRect().top + window.scrollY
      const main = layoutRef.current.closest('main')
      const shell = layoutRef.current.closest('.app-layout')
      let padding = 24
      if (shell) {
        padding = parseFloat(getComputedStyle(shell).paddingBottom) || 24
      } else if (main) {
        padding = parseFloat(getComputedStyle(main).paddingBottom) || 24
      }
      setAvailableHeight(`${Math.max(320, window.innerHeight - top - padding)}px`)
    }

    check()
    window.addEventListener('resize', check)
    let observer: ResizeObserver | undefined
    if (typeof ResizeObserver !== 'undefined' && layoutRef.current?.parentElement) {
      observer = new ResizeObserver(check)
      observer.observe(layoutRef.current.parentElement)
    }
    return () => {
      window.removeEventListener('resize', check)
      observer?.disconnect()
    }
  }, [])

  return (
    <div
      ref={layoutRef}
      className={cn('table-page-layout workspace-page', isMobile && 'mobile-mode')}
      style={{ '--table-page-height': availableHeight } as React.CSSProperties}
    >
      {actions && <div className="layout-section-fixed workspace-fixed">{actions}</div>}
      {filters && <div className="layout-section-fixed workspace-fixed">{filters}</div>}
      <div className="layout-section-scrollable workspace-scroll">
        <div className="card table-scroll-container workspace-shell workspace-table-card">
          {table}
        </div>
      </div>
      {pagination && <div className="layout-section-fixed table-page-pagination workspace-fixed">{pagination}</div>}
      {/* The Vue TablePageLayout has named slots only and no default slot, so
          anything passed as children (typically dialogs) is an overlay rather
          than table content. It renders outside the scroll zone; the shared
          Modal portals to the body, so placement here does not affect layout. */}
      {children}
    </div>
  )
}

export default AdminTablePageLayout
