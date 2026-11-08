import { useEffect, useRef } from 'react'

// Dialogs and the navigation drawer share one stack and one body scroll lock.
const openDialogs = new Set<symbol>()
const scrollLocks = new Set<symbol>()

export interface UseDialogFocusOptions {
  lockScroll?: boolean
  restoreFocus?: () => boolean
}

/**
 * Focus management for dialogs: traps Tab within the panel, closes on Escape
 * while topmost, restores focus to the previously active element on close, and
 * shares one body scroll lock across every open dialog.
 */
export function useDialogFocus(
  visible: boolean,
  panel: React.RefObject<HTMLElement | null>,
  close: () => void,
  closeOnEscape: () => boolean = () => true,
  options: UseDialogFocusOptions = {}
) {
  const idRef = useRef<symbol | null>(null)
  if (idRef.current === null) idRef.current = Symbol('dialog')
  const id = idRef.current

  const previousRef = useRef<HTMLElement | null>(null)
  const revisionRef = useRef(0)
  const visibleRef = useRef(visible)
  visibleRef.current = visible
  const closeRef = useRef(close)
  closeRef.current = close
  const closeOnEscapeRef = useRef(closeOnEscape)
  closeOnEscapeRef.current = closeOnEscape
  const optionsRef = useRef(options)
  optionsRef.current = options

  const isTop = () => [...openDialogs].pop() === id

  const release = (): HTMLElement | null => {
    const restore =
      isTop() && (optionsRef.current.restoreFocus?.() ?? true) ? previousRef.current : null
    openDialogs.delete(id)
    scrollLocks.delete(id)
    document.body.classList.toggle('modal-open', scrollLocks.size > 0)
    previousRef.current = null
    return restore
  }

  const focusable = () =>
    [
      ...(panel.current?.querySelectorAll<HTMLElement>(
        'button:not([disabled]), a[href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), summary, [tabindex="0"]'
      ) ?? [])
    ].filter((element) => {
      if (
        element.matches(':disabled') ||
        (element.hasAttribute('tabindex') && element.tabIndex < 0) ||
        element.closest('[inert], [hidden], [aria-hidden="true"]')
      ) {
        return false
      }
      for (let ancestor: HTMLElement | null = element; ancestor; ancestor = ancestor.parentElement) {
        const style = getComputedStyle(ancestor)
        if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse') {
          return false
        }
        if (ancestor instanceof HTMLDetailsElement && !ancestor.open) {
          const summary = ancestor.querySelector(':scope > summary')
          if (!summary?.contains(element)) return false
        }
      }
      return true
    })

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.defaultPrevented || !visibleRef.current || !isTop()) return
      if (event.key === 'Escape' && closeOnEscapeRef.current()) {
        event.preventDefault()
        event.stopPropagation()
        closeRef.current()
      }
      if (event.key !== 'Tab') return
      const elements = focusable()
      const first = elements[0]
      const last = elements[elements.length - 1]
      if (!first) {
        event.preventDefault()
        panel.current?.focus()
        return
      }
      if (!panel.current?.contains(document.activeElement) || document.activeElement === panel.current) {
        event.preventDefault()
        const target = event.shiftKey ? last : first
        target?.focus()
      } else if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last?.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => {
      revisionRef.current += 1
      document.removeEventListener('keydown', onKey)
      const restore = release()
      if (restore?.isConnected && !restore.closest('[inert]')) restore.focus({ preventScroll: true })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    const current = ++revisionRef.current
    if (visible) {
      previousRef.current = document.activeElement as HTMLElement | null
      openDialogs.add(id)
      if (optionsRef.current.lockScroll !== false) scrollLocks.add(id)
      document.body.classList.toggle('modal-open', scrollLocks.size > 0)
      const frame = requestAnimationFrame(() => {
        if (current === revisionRef.current && visibleRef.current && isTop()) {
          ;(focusable()[0] ?? panel.current)?.focus({ preventScroll: true })
        }
      })
      return () => cancelAnimationFrame(frame)
    }
    const restore = release()
    const frame = requestAnimationFrame(() => {
      if (current === revisionRef.current && restore?.isConnected && !restore.closest('[inert]')) {
        restore.focus({ preventScroll: true })
      }
    })
    return () => cancelAnimationFrame(frame)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visible])
}
