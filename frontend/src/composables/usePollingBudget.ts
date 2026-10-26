import { onMounted, onBeforeUnmount, ref } from 'vue'

/** Divide the live polling budget among visible tabs for the same identity. */
export function usePollingBudget(scope: string) {
  const peers = ref(1)
  const prefix = `polling-tab:${scope}:`
  const ownKey = prefix + (globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`)
  let timer: ReturnType<typeof setInterval> | undefined
  function update() {
    try {
      if (document.hidden) localStorage.removeItem(ownKey)
      else localStorage.setItem(ownKey, String(Date.now() + 15000))
      let count = 0
      for (let i = localStorage.length - 1; i >= 0; i--) {
        const key = localStorage.key(i)
        if (!key?.startsWith(prefix)) continue
        if (Number(localStorage.getItem(key)) <= Date.now()) localStorage.removeItem(key)
        else count++
      }
      peers.value = Math.max(1, count)
    } catch { peers.value = 2 } // Restricted storage: use a conservative interval.
  }
  onMounted(() => { update(); timer = setInterval(update, 5000); document.addEventListener('visibilitychange', update) })
  onBeforeUnmount(() => {
    clearInterval(timer)
    document.removeEventListener('visibilitychange', update)
    try { localStorage.removeItem(ownKey) } catch { /* restricted storage */ }
  })
  return peers
}
