import { watchEffect, type ObjectDirective } from 'vue'
import { adminSupportContext } from '@/utils/adminSupportContext'

const cleanup = new WeakMap<HTMLElement, () => void>()

// Explicitly mark mutation controls/forms. Browse, filter, copy and download
// controls are unmarked and retain their ordinary behavior, including teleports.
export const supportReadonly: ObjectDirective<HTMLElement> = {
  mounted(element) {
    const block = (event: Event) => {
      if (!adminSupportContext.value) return
      event.preventDefault()
      event.stopImmediatePropagation()
    }
    for (const type of ['click', 'submit', 'change', 'input']) element.addEventListener(type, block, true)
    const changed = new Map<HTMLElement, Set<string>>()
    const lock = () => {
      if (!adminSupportContext.value) {
        for (const [control, attributes] of changed) for (const attribute of attributes) control.removeAttribute(attribute)
        changed.clear()
        return
      }
      const add = (control: HTMLElement, attribute: string) => {
        if (control.hasAttribute(attribute)) return
        if (!changed.has(control)) changed.set(control, new Set())
        changed.get(control)!.add(attribute)
        control.setAttribute(attribute, '')
      }
      const controls = [element, ...element.querySelectorAll<HTMLElement>('button, input, textarea, select')]
      for (const control of controls) {
        if (control instanceof HTMLInputElement && !['checkbox', 'radio', 'file', 'submit', 'button'].includes(control.type) || control instanceof HTMLTextAreaElement) {
          add(control, 'readonly')
        } else if (control.matches('button,input,select') && !control.hasAttribute('disabled')) {
          add(control, 'disabled')
        }
      }
    }
    const observer = new MutationObserver(lock)
    observer.observe(element, { childList: true, subtree: true, attributes: true, attributeFilter: ['disabled', 'readonly'] })
    const stop = watchEffect(lock)
    cleanup.set(element, () => {
      stop()
      observer.disconnect()
      for (const type of ['click', 'submit', 'change', 'input']) element.removeEventListener(type, block, true)
    })
  },
  unmounted(element) { cleanup.get(element)?.(); cleanup.delete(element) }
}
