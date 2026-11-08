import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'

const PANEL_ID = 'title-tooltip'

interface Owner {
  element: Element
  originalTitle: string
  addedLabel: boolean
  labelValue: string
}

/**
 * Delegates legacy `title` attributes to the shared tooltip card, so tables and
 * menus rendered with `title=` get the same treatment as the rest of the console
 * without changing their DOM. Mirrors the Vue TitleTooltip: the native attribute
 * is suppressed while the card is shown, and restored on release.
 */
export function DocumentTitleTooltip() {
  const panelRef = useRef<HTMLDivElement>(null)
  const ownerRef = useRef<Owner | null>(null)
  const [content, setContent] = useState('')
  const [position, setPosition] = useState({ left: '0px', top: '0px' })
  const hoveredRef = useRef(false)
  const focusedRef = useRef(false)
  const dismissedRef = useRef(false)

  useEffect(() => {
    const observer = new MutationObserver(() => {
      const owner = ownerRef.current
      if (!owner) return
      const title = owner.element.getAttribute('title') ?? ''
      owner.originalTitle = title
      observer.disconnect()
      owner.element.setAttribute('title', '')
      observer.observe(owner.element, { attributes: true, attributeFilter: ['title'] })
      if (owner.addedLabel && title) {
        owner.labelValue = title
        owner.element.setAttribute('aria-label', title)
      }
      if (!title.trim()) dismiss()
      else if (!dismissedRef.current) updatePosition()
    })
    const removalObserver = new MutationObserver(() => {
      const owner = ownerRef.current
      if (owner && !owner.element.isConnected) release()
    })

    const clearDescription = () => {
      const owner = ownerRef.current
      if (!owner) return
      const descriptions = (owner.element.getAttribute('aria-describedby') ?? '')
        .split(/\s+/)
        .filter((value) => value && value !== PANEL_ID)
      if (descriptions.length) owner.element.setAttribute('aria-describedby', descriptions.join(' '))
      else owner.element.removeAttribute('aria-describedby')
    }

    const release = () => {
      observer?.disconnect()
      removalObserver?.disconnect()
      const owner = ownerRef.current
      if (owner) {
        if (owner.element.getAttribute('title') === '') {
          if (owner.originalTitle === null) owner.element.removeAttribute('title')
          else owner.element.setAttribute('title', owner.originalTitle)
        }
        clearDescription()
        if (owner.addedLabel && owner.element.getAttribute('aria-label') === owner.labelValue) {
          owner.element.removeAttribute('aria-label')
        }
      }
      ownerRef.current = null
      hoveredRef.current = false
      focusedRef.current = false
      dismissedRef.current = false
      setContent('')
    }

    const updatePosition = () => {
      const owner = ownerRef.current
      const panel = panelRef.current
      if (!owner?.element.isConnected) {
        release()
        return
      }
      if (!content || !panel) return
      const anchor = owner.element.getBoundingClientRect()
      if (anchor.bottom <= 0 || anchor.top >= window.innerHeight || anchor.right <= 0 || anchor.left >= window.innerWidth) {
        release()
        return
      }
      const bounds = panel.getBoundingClientRect()
      const inset = 12
      const left = Math.max(
        inset,
        Math.min(anchor.left + (anchor.width - bounds.width) / 2, window.innerWidth - bounds.width - inset)
      )
      const top =
        anchor.top >= bounds.height + inset + 8
          ? anchor.top - bounds.height - 8
          : Math.min(anchor.bottom + 8, window.innerHeight - bounds.height - inset)
      setPosition({ left: `${left}px`, top: `${Math.max(inset, top)}px` })
    }

    const describe = () => {
      const owner = ownerRef.current
      if (!owner || dismissedRef.current) return
      const descriptions = new Set(
        (owner.element.getAttribute('aria-describedby') ?? '').split(/\s+/).filter(Boolean)
      )
      descriptions.add(PANEL_ID)
      owner.element.setAttribute('aria-describedby', [...descriptions].join(' '))
      requestAnimationFrame(updatePosition)
    }

    const activate = (target: EventTarget | null, mode: 'hover' | 'focus') => {
      if (!(target instanceof Element)) return
      if (target.closest('[role="tooltip"], .help-tooltip-trigger')) {
        release()
        return
      }
      const candidate = target.closest('[title]')
      const current = ownerRef.current
      if (current?.element.contains(target) && (!candidate || candidate === current.element || !candidate.getAttribute('title')?.trim())) {
        if (mode === 'hover') hoveredRef.current = true
        else focusedRef.current = true
        describe()
        return
      }
      if (!candidate) return
      const title = candidate.getAttribute('title') ?? ''
      if (!title.trim()) return
      release()
      const accessibleContent = candidate.cloneNode(true) as Element
      accessibleContent.querySelectorAll('[aria-hidden="true"], [hidden]').forEach((element) => element.remove())
      const hasLabel = 'labels' in candidate && !!(candidate as HTMLInputElement).labels?.length
      const addedLabel =
        candidate.matches('button, a, input, [role="button"]') &&
        !candidate.hasAttribute('aria-label') &&
        !candidate.hasAttribute('aria-labelledby') &&
        !hasLabel &&
        !accessibleContent.textContent?.trim()
      ownerRef.current = { element: candidate, originalTitle: title, addedLabel, labelValue: title }
      candidate.setAttribute('title', '')
      if (addedLabel) candidate.setAttribute('aria-label', title)
      if (mode === 'hover') hoveredRef.current = true
      else focusedRef.current = true
      observer.observe(candidate, { attributes: true, attributeFilter: ['title'] })
      removalObserver.observe(document.body, { childList: true, subtree: true })
      setContent(title)
      requestAnimationFrame(updatePosition)
    }

    const onOver = (event: PointerEvent) => {
      if (event.pointerType !== 'touch') activate(event.target, 'hover')
    }
    const onOut = (event: PointerEvent) => {
      const owner = ownerRef.current
      if (!owner || (event.relatedTarget instanceof Node && owner.element.contains(event.relatedTarget))) return
      hoveredRef.current = false
      if (!focusedRef.current) release()
    }
    const onFocus = (event: FocusEvent) => activate(event.target, 'focus')
    const onBlur = (event: FocusEvent) => {
      const owner = ownerRef.current
      if (!owner || (event.relatedTarget instanceof Node && owner.element.contains(event.relatedTarget))) return
      focusedRef.current = false
      if (!hoveredRef.current) release()
    }
    const dismiss = () => {
      dismissedRef.current = true
      setContent('')
      clearDescription()
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') dismiss()
    }

    document.addEventListener('pointerover', onOver, true)
    document.addEventListener('pointerout', onOut, true)
    document.addEventListener('focusin', onFocus, true)
    document.addEventListener('focusout', onBlur, true)
    document.addEventListener('keydown', onKey)
    document.addEventListener('click', dismiss, true)
    window.addEventListener('scroll', updatePosition, true)
    window.addEventListener('resize', updatePosition)

    return () => {
      release()
      document.removeEventListener('pointerover', onOver, true)
      document.removeEventListener('pointerout', onOut, true)
      document.removeEventListener('focusin', onFocus, true)
      document.removeEventListener('focusout', onBlur, true)
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('click', dismiss, true)
      window.removeEventListener('scroll', updatePosition, true)
      window.removeEventListener('resize', updatePosition)
    }
  }, [content])

  if (!content) return null

  return createPortal(
    <div
      id={PANEL_ID}
      ref={panelRef}
      role="tooltip"
      className="title-tooltip surface-panel"
      style={position}
    >
      {content}
    </div>,
    document.body
  )
}

export default DocumentTitleTooltip
