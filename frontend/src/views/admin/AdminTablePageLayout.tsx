import { useEffect, useRef, useState } from 'react'

/**
 * Stand-in for the Vue `TablePageLayout` until the shared layout component
 * lands. Keeps the same section structure (`table-page-layout workspace-page`
 * with fixed filter / scrollable table / fixed pagination zones) and the same
 * desktop height calculation so the ported admin views scroll identically.
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
  const [availableHeight, setAvailableHeight] = useState('auto')

  useEffect(() => {
    const check = () => {
      if (window.innerWidth < 1024 || !layoutRef.current) {
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
      className="table-page-layout workspace-page"
      style={{ '--table-page-height': availableHeight } as React.CSSProperties}
    >
      {actions && <div className="layout-section-fixed workspace-fixed">{actions}</div>}
      {filters && <div className="layout-section-fixed workspace-fixed">{filters}</div>}
      <div className="layout-section-scrollable workspace-scroll">
        <div className="card table-scroll-container workspace-shell workspace-table-card">
          {table ?? children}
        </div>
      </div>
      {pagination && <div className="layout-section-fixed table-page-pagination workspace-fixed">{pagination}</div>}
    </div>
  )
}

export default AdminTablePageLayout
