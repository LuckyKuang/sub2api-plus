import { onScopeDispose } from 'vue'

/** Refresh displayed request-time quotes while a page remains visible. */
export function useQuoteRefresh(refresh: () => void | Promise<unknown>): void {
  let running = false
  let disposed = false
  const update = async () => {
    if (disposed || running || document.visibilityState === 'hidden') return
    running = true
    try { await refresh() } catch { /* The owning page renders its own error state. */ }
    finally { running = false }
  }
  const interval = window.setInterval(() => { void update() }, 60_000)
  window.addEventListener('focus', update)
  document.addEventListener('visibilitychange', update)
  onScopeDispose(() => {
    disposed = true
    window.clearInterval(interval)
    window.removeEventListener('focus', update)
    document.removeEventListener('visibilitychange', update)
  })
}
