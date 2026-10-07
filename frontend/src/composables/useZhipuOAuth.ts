import { onScopeDispose, ref, shallowRef } from 'vue'
import {
  createZhipuAccountFromLink,
  exchangeZhipuLink,
  getZhipuOAuthCapabilities,
  pollZhipuLink,
  startZhipuLink,
  type ZhipuCreateAccountRequest,
  type ZhipuLinkPoll,
  type ZhipuLinkSession,
  type ZhipuLinkToken,
  type ZhipuOAuthCapabilities,
  type ZhipuPlanKind,
  type ZhipuProvider
} from '@/api/admin/zhipu'

/** Human labels for the plan kinds, kept next to the wire values. */
export const zhipuPlanKindLabels: Record<ZhipuPlanKind, string> = {
  'individual-coding-plan': 'Individual Coding Plan',
  'team-coding-plan': 'Team Coding Plan',
  'start-plan': 'Start Plan',
  'off-peak': 'Off-peak Idle Plan'
}

/** Plan kinds whose credential derivation needs an organization and project. */
export function zhipuPlanNeedsTeamScope(planKind: ZhipuPlanKind): boolean {
  return planKind === 'team-coding-plan'
}

/**
 * Drives the GLM account link.
 *
 * The server owns the poll credential, so this composable only forwards the
 * session handle and stops polling as soon as the platform reports the
 * authorization is ready or the session expires. Polling is timer-based rather
 * than reactive so a slow platform interval never stacks requests, and it is
 * disposed with the owning component scope.
 */
export function useZhipuOAuth() {
  const capabilities = shallowRef<ZhipuOAuthCapabilities>()
  const session = ref<ZhipuLinkSession>()
  const ready = shallowRef<ZhipuLinkToken>()
  const loading = ref(false)
  const polling = ref(false)
  const error = ref('')

  let timer: ReturnType<typeof setTimeout> | undefined
  let expiryTimer: ReturnType<typeof setTimeout> | undefined
  let generation = 0
  let disposed = false
  let pollRequest: Promise<ZhipuLinkPoll | undefined> | undefined
  const expired = ref(false)

  function stopPolling() {
    generation += 1
    polling.value = false
    loading.value = false
    pollRequest = undefined
    if (timer) clearTimeout(timer)
    timer = undefined
  }

  function cancelLink() {
    stopPolling()
    if (expiryTimer) clearTimeout(expiryTimer)
    expiryTimer = undefined
    session.value = undefined
    ready.value = undefined
    expired.value = false
    error.value = ''
  }

  function expireSession(): boolean {
    if (!session.value || Date.parse(session.value.expires_at) > Date.now()) return false
    cancelLink()
    expired.value = true
    return true
  }

  function scheduleExpiry() {
    if (expiryTimer) clearTimeout(expiryTimer)
    if (session.value) expiryTimer = setTimeout(expireSession, Math.max(0, Date.parse(session.value.expires_at) - Date.now()))
  }

  async function loadCapabilities(): Promise<ZhipuOAuthCapabilities | undefined> {
    try {
      const result = await getZhipuOAuthCapabilities()
      if (disposed) return undefined
      capabilities.value = result
      return result
    } catch (err) {
      if (!disposed) error.value = describeError(err, 'Failed to load GLM link capabilities')
      return undefined
    }
  }

  async function startLink(provider: ZhipuProvider, proxyId?: number): Promise<boolean> {
    cancelLink()
    const run = generation
    loading.value = true
    try {
      const result = await startZhipuLink({ provider, ...(proxyId ? { proxy_id: proxyId } : {}) })
      if (disposed || run !== generation) return false
      session.value = result
      if (!Number.isFinite(Date.parse(result.expires_at)) || expireSession()) {
        cancelLink()
        expired.value = true
        return false
      }
      scheduleExpiry()
      schedulePoll()
      return true
    } catch (err) {
      if (run === generation) error.value = describeError(err, 'Failed to start the GLM authorization')
      return false
    } finally {
      if (run === generation) loading.value = false
    }
  }

  function schedulePoll() {
    if (timer) clearTimeout(timer)
    const current = session.value
    if (!current || ready.value || expireSession()) return
    const run = generation
    polling.value = true
    timer = setTimeout(async () => {
      if (run !== generation) return
      await pollOnce()
      if (run === generation && session.value && !ready.value) schedulePoll()
    }, Math.max(1, current.interval_seconds || 2) * 1000)
  }

  function pollOnce(): Promise<ZhipuLinkPoll | undefined> {
    const current = session.value
    if (!current || loading.value || expireSession()) return Promise.resolve(undefined)
    if (ready.value) return Promise.resolve({ pending: false, ready: ready.value })
    if (pollRequest) return pollRequest
    const run = generation
    pollRequest = (async () => {
      try {
        const result = await pollZhipuLink(current.session_id)
        if (run !== generation || expireSession()) return undefined
        if (result.ready) {
          ready.value = result.ready
          polling.value = false
          if (timer) clearTimeout(timer)
          timer = undefined
        }
        error.value = ''
        return result
      } catch (err) {
        if (run !== generation || expireSession()) return undefined
        // The server uses 400 for expired/denied sessions; 5xx is retryable.
        if ((err as { response?: { status?: number } }).response?.status === 400) cancelLink()
        error.value = describeError(err, 'Failed to check the GLM authorization')
        return { pending: true }
      } finally {
        if (run === generation) pollRequest = undefined
      }
    })()
    return pollRequest
  }

  async function exchangeLink(callback: string, proxyId?: number): Promise<boolean> {
    const current = session.value
    if (loading.value || expireSession()) return false
    if (!current || !callback.trim()) {
      error.value = 'An authorization code or callback URL is required'
      return false
    }
    stopPolling()
    const run = generation
    loading.value = true
    error.value = ''
    try {
      const result = await exchangeZhipuLink(current.session_id, callback.trim(), proxyId)
      if (run !== generation || expireSession()) return false
      if (result.ready) {
        ready.value = result.ready
        return true
      }
      error.value = 'The authorization did not return a usable credential'
      return false
    } catch (err) {
      if (run === generation) error.value = describeError(err, 'Failed to exchange the GLM authorization code')
      return false
    } finally {
      if (run === generation) {
        loading.value = false
        if (!ready.value) schedulePoll()
      }
    }
  }

  async function createAccount(payload: Omit<ZhipuCreateAccountRequest, 'session_id' | 'provider'>): Promise<boolean> {
    const current = session.value
    const token = ready.value
    if (loading.value || expireSession()) return false
    if (!current || !token) {
      error.value = 'Authorize the account before creating it'
      return false
    }
    stopPolling()
    const run = generation
    loading.value = true
    error.value = ''
    // Once submitted, let the server decide whether consumption preceded expiry.
    // A successful create must still be reported when its response arrives later.
    if (expiryTimer) clearTimeout(expiryTimer)
    expiryTimer = undefined
    try {
      await createZhipuAccountFromLink({
        ...payload,
        session_id: current.session_id,
        provider: token.provider,
        access_token: token.access_token,
        ...(token.refresh_token ? { refresh_token: token.refresh_token } : {}),
        ...(token.zcode_jwt_token ? { zcode_jwt_token: token.zcode_jwt_token } : {})
      })
      if (run !== generation) return false
      cancelLink()
      return true
    } catch (err) {
      if (run === generation) error.value = describeError(err, 'Failed to create the GLM account')
      return false
    } finally {
      if (run === generation) {
        loading.value = false
        if (!expireSession()) scheduleExpiry()
      }
    }
  }

  onScopeDispose(() => { disposed = true; cancelLink() })

  return {
    capabilities,
    session,
    ready,
    loading,
    polling,
    error,
    loadCapabilities,
    startLink,
    pollOnce,
    exchangeLink,
    createAccount,
    stopPolling,
    cancelLink,
    expired
  }
}

function describeError(err: unknown, fallback: string): string {
  const message = (err as { response?: { data?: { message?: string } } })?.response?.data?.message
  if (typeof message === 'string' && message.trim()) return message
  if (err instanceof Error && err.message) return err.message
  return fallback
}
