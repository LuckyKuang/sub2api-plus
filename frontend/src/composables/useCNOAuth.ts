import { onBeforeUnmount, ref } from 'vue'
import { cnOAuthRequest, type CNOAuthAccountInput, type CNOAuthPlatform, type CNOAuthSession } from '@/api/admin/cnOAuth'

export function useCNOAuth(platform: CNOAuthPlatform) {
  const session = ref<CNOAuthSession | null>(null)
  const busy = ref(false)
  const failed = ref(false)
  const expired = ref(false)
  let generation = 0
  let timer: ReturnType<typeof setTimeout> | undefined
  const clearTimer = () => { if (timer) clearTimeout(timer); timer = undefined }
  const discard = (id: string) => { void cnOAuthRequest(platform, 'cancel', { session_id: id }).catch(() => {}) }

  function cancel() {
    generation++
    clearTimer()
    if (session.value && session.value.status !== 'completed') discard(session.value.session_id)
    session.value = null
    busy.value = false
    failed.value = false
    expired.value = false
  }
  function schedule() {
    clearTimer()
    const current = session.value
    if (!current || (current.status !== 'pending' && current.status !== 'ready')) return
    const remaining = Date.parse(current.expires_at) - Date.now()
    if (remaining <= 0) { expired.value = true; return }
    timer = setTimeout(() => {
      if (Date.now() >= Date.parse(current.expires_at)) { expired.value = true; return }
      if (platform === 'deepseek' || platform === 'stepfun' || current.status === 'ready') schedule()
      else void advance()
    }, Math.min(remaining, Math.max(1, current.interval_seconds) * 1000))
  }
  async function start(input: { region: string; proxy_id?: number; account_id?: number }) {
    cancel()
    const run = generation
    busy.value = true
    try {
      const result = await cnOAuthRequest(platform, 'start', input)
      if (run !== generation) { discard(result.session_id); return }
      session.value = result
      schedule()
    } catch { if (run === generation) failed.value = true }
    finally { if (run === generation) busy.value = false }
  }
  async function advance(callback?: string) {
    if (!session.value || busy.value || expired.value) return
    const run = generation
    clearTimer()
    busy.value = true
    failed.value = false
    try {
      const result = await cnOAuthRequest(platform, callback ? 'exchange' : 'poll', {
        session_id: session.value.session_id, ...(callback ? { callback } : {})
      })
      if (run !== generation) return
      session.value = result
      if (result.status === 'cancelled') failed.value = true
    } catch { if (run === generation) failed.value = true }
    finally { if (run === generation) { busy.value = false; schedule() } }
  }
  async function complete(input: CNOAuthAccountInput): Promise<number | undefined> {
    if (!session.value || session.value.status !== 'ready' || busy.value || expired.value) return
    const run = generation
    busy.value = true
    failed.value = false
    try {
      const result = await cnOAuthRequest(platform, 'complete', { ...input, session_id: session.value.session_id })
      if (run !== generation) return
      session.value = result
      clearTimer()
      return result.account_id
    } catch { if (run === generation) failed.value = true }
    finally { if (run === generation) busy.value = false }
  }
  onBeforeUnmount(cancel)
  return { session, busy, failed, expired, start, advance, complete, cancel }
}
