import { useEffect, useState, type CSSProperties, type RefObject } from 'react'

/**
 * Keep a dropdown aligned when its trigger lives in a scrolling toolbar.
 * Mirrors the Vue `useAnchoredDropdown` composable.
 */
export function useAnchoredDropdown(anchor: RefObject<HTMLElement | null>, active: boolean): CSSProperties {
  const [style, setStyle] = useState<CSSProperties>({})

  useEffect(() => {
    if (!active) return

    const update = () => {
      if (!anchor.current) return
      const rect = anchor.current.getBoundingClientRect()
      const gutter = 12
      const gap = 6
      const width = Math.min(Math.max(rect.width, 240), window.innerWidth - gutter * 2)
      const below = Math.max(0, window.innerHeight - rect.bottom - gap - gutter)
      const above = Math.max(0, rect.top - gap - gutter)
      const upward = below < 160 && above > below
      setStyle({
        position: 'fixed',
        margin: 0,
        width: `${width}px`,
        right: 'auto',
        left: `${Math.max(gutter, Math.min(rect.left, window.innerWidth - width - gutter))}px`,
        ...(upward ? { bottom: `${window.innerHeight - rect.top + gap}px` } : { top: `${rect.bottom + gap}px` }),
        maxHeight: `${Math.min(240, upward ? above : below)}px`
      })
    }

    update()
    window.addEventListener('resize', update)
    window.addEventListener('scroll', update, { capture: true, passive: true })
    return () => {
      window.removeEventListener('resize', update)
      window.removeEventListener('scroll', update, true)
    }
  }, [active, anchor])

  return style
}

export default useAnchoredDropdown
