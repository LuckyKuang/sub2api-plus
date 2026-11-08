import { useEffect, type RefObject } from 'react'
import { getAdminSupportContext, supportRequestGeneration } from '@/utils/adminSupportContext'

/**
 * React port of the Vue `v-support-readonly` directive: while an admin is
 * browsing on behalf of a user, mutation controls inside the element are
 * blocked and disabled, while browse/filter/copy controls keep working.
 *
 * The React adminSupportContext module is a plain singleton exposing only a
 * generation counter, so the directive's watchEffect is replaced by a light
 * generation poll.
 */
export function useSupportReadonly(elementRef: RefObject<HTMLElement | null>): void {
  useEffect(() => {
    const element = elementRef.current
    if (!element) return

    const block = (event: Event) => {
      if (!getAdminSupportContext()) return
      event.preventDefault()
      event.stopImmediatePropagation()
    }
    for (const type of ['click', 'submit', 'change', 'input']) {
      element.addEventListener(type, block, true)
    }

    const changed = new Map<HTMLElement, Set<string>>()
    const lock = () => {
      if (!getAdminSupportContext()) {
        for (const [control, attributes] of changed) {
          for (const attribute of attributes) control.removeAttribute(attribute)
        }
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
        if (
          (control instanceof HTMLInputElement &&
            !['checkbox', 'radio', 'file', 'submit', 'button'].includes(control.type)) ||
          control instanceof HTMLTextAreaElement
        ) {
          add(control, 'readonly')
        } else if (control.matches('button,input,select') && !control.hasAttribute('disabled')) {
          add(control, 'disabled')
        }
      }
    }

    const observer = new MutationObserver(lock)
    observer.observe(element, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ['disabled', 'readonly']
    })

    let lastGeneration = supportRequestGeneration()
    lock()
    const poll = setInterval(() => {
      const generation = supportRequestGeneration()
      if (generation === lastGeneration) return
      lastGeneration = generation
      lock()
    }, 500)

    return () => {
      clearInterval(poll)
      observer.disconnect()
      for (const type of ['click', 'submit', 'change', 'input']) {
        element.removeEventListener(type, block, true)
      }
      for (const [control, attributes] of changed) {
        for (const attribute of attributes) control.removeAttribute(attribute)
      }
      changed.clear()
    }
  }, [elementRef])
}
