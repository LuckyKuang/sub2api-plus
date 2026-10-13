import { onScopeDispose } from 'vue'

const LOCALE_CHANGED = 'sub2api:locale-changed'

/** Notify read models after the API language header has changed. */
export function notifyLocaleChanged(): void {
  window.dispatchEvent(new Event(LOCALE_CHANGED))
}

/** Refresh translated server data without remounting forms or replaying writes. */
export function useLocaleRefresh(refresh: () => void | Promise<unknown>): void {
  let active = true
  const listener = () => {
    void Promise.resolve().then(() => active ? refresh() : undefined).catch(() => {
      console.error('Failed to refresh translated data')
    })
  }
  window.addEventListener(LOCALE_CHANGED, listener)
  onScopeDispose(() => {
    active = false
    window.removeEventListener(LOCALE_CHANGED, listener)
  })
}
