import { useEffect, useRef, useState } from 'react'

export interface UserCardScrollProps {
  label?: string
  resetKey?: string | number
  className?: string
  children?: React.ReactNode
}

export function UserCardScroll({ label, resetKey, className, children }: UserCardScrollProps) {
  const viewport = useRef<HTMLDivElement>(null)
  const content = useRef<HTMLDivElement>(null)
  const [scrollbarWidth, setScrollbarWidth] = useState(0)

  // The viewport is widened by exactly its scrollbar so the reserved gutter never
  // shrinks the content. That gutter only appears once the content overflows, so
  // measuring the viewport alone misses the moment the slotted cards arrive.
  function measureScrollbar() {
    const element = viewport.current
    if (element) setScrollbarWidth(Math.max(0, element.offsetWidth - element.clientWidth))
  }

  useEffect(() => {
    if (viewport.current) viewport.current.scrollTop = 0
  }, [resetKey])

  useEffect(() => {
    measureScrollbar()
    const resize = new ResizeObserver(measureScrollbar)
    if (viewport.current) resize.observe(viewport.current)
    if (content.current) resize.observe(content.current)
    return () => resize.disconnect()
  }, [])

  return (
    <div className={`user-card-scroll-frame ${className ?? ''}`}>
      <div
        ref={viewport}
        className="user-card-scroll-viewport"
        style={{ ['--user-card-scrollbar' as string]: `${scrollbarWidth}px` }}
        aria-label={label}
        role={label ? 'region' : undefined}
        tabIndex={0}
      >
        <div ref={content} className="user-card-scroll-content">
          {children}
        </div>
      </div>
    </div>
  )
}

export default UserCardScroll
