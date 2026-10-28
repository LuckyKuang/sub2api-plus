import { nextTick, onBeforeUnmount, onMounted, watch, type Ref } from 'vue'

// Dialogs and the navigation drawer share one stack and one body scroll lock.
const openDialogs = new Set<symbol>()
const scrollLocks = new Set<symbol>()

export function useDialogFocus(
  visible: () => boolean,
  panel: Ref<HTMLElement | null>,
  close: () => void,
  closeOnEscape: () => boolean = () => true,
  options: { lockScroll?: boolean; restoreFocus?: () => boolean } = {}
) {
  const id = Symbol('dialog')
  let previous: HTMLElement | null = null
  let revision = 0
  const isTop = () => [...openDialogs].pop() === id
  function release() {
    const restore = isTop() && (options.restoreFocus?.() ?? true) ? previous : null
    openDialogs.delete(id)
    scrollLocks.delete(id)
    document.body.classList.toggle('modal-open', scrollLocks.size > 0)
    previous = null
    return restore
  }
  function focusable() {
    return [...(panel.value?.querySelectorAll<HTMLElement>(
      'button:not([disabled]), a[href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), summary, [tabindex="0"]'
    ) ?? [])].filter(element => {
      if (element.matches(':disabled') || (element.hasAttribute('tabindex') && element.tabIndex < 0)
        || element.closest('[inert], [hidden], [aria-hidden="true"]')) return false
      for (let ancestor: HTMLElement | null = element; ancestor; ancestor = ancestor.parentElement) {
        const style = getComputedStyle(ancestor)
        if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse') return false
        if (ancestor instanceof HTMLDetailsElement && !ancestor.open) {
          const summary = ancestor.querySelector(':scope > summary')
          if (!summary?.contains(element)) return false
        }
      }
      return true
    })
  }
  function onKey(event: KeyboardEvent) {
    if (event.defaultPrevented || !visible() || !isTop()) return
    if (event.key === 'Escape' && closeOnEscape()) {
      event.preventDefault()
      event.stopPropagation()
      close()
    }
    if (event.key !== 'Tab') return
    const elements = focusable()
    const first = elements[0]
    const last = elements[elements.length - 1]
    if (!first) { event.preventDefault(); panel.value?.focus(); return }
    if (!panel.value?.contains(document.activeElement) || document.activeElement === panel.value) {
      event.preventDefault()
      const target = event.shiftKey ? last : first
      target?.focus()
    } else if (event.shiftKey && document.activeElement === first) {
      event.preventDefault(); last?.focus()
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault(); first.focus()
    }
  }
  watch(visible, async show => {
    const current = ++revision
    if (show) {
      previous = document.activeElement as HTMLElement
      openDialogs.add(id)
      if (options.lockScroll !== false) scrollLocks.add(id)
      document.body.classList.toggle('modal-open', scrollLocks.size > 0)
      await nextTick()
      if (current === revision && visible() && isTop()) (focusable()[0] ?? panel.value)?.focus({ preventScroll: true })
    } else {
      const restore = release()
      await nextTick()
      if (current === revision && restore?.isConnected && !restore.closest('[inert]')) restore.focus({ preventScroll: true })
    }
  }, { immediate: true })
  onMounted(() => document.addEventListener('keydown', onKey))
  onBeforeUnmount(() => {
    ++revision
    document.removeEventListener('keydown', onKey)
    const restore = release()
    if (restore?.isConnected && !restore.closest('[inert]')) restore.focus({ preventScroll: true })
  })
}
