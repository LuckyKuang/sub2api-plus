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
  let generation = 0

  function stopPolling() {
    generation += 1
    polling.value = false
    if (timer) {
      clearTimeout(timer)
      timer = undefined
    }
  }

  async function loadCapabilities(): Promise<ZhipuOAuthCapabilities | undefined> {
    try {
      capabilities.value = await getZhipuOAuthCapabilities()
      return capabilities.value
    } catch (err) {
      error.value = describeError(err, 'Failed to load GLM link capabilities')
      return undefined
    }
  }

  async function startLink(provider: ZhipuProvider, proxyId?: number): Promise<boolean> {
    stopPolling()
    loading.value = true
    error.value = ''
    ready.value = undefined
    session.value = undefined
    try {
      session.value = await startZhipuLink({ provider, ...(proxyId ? { proxy_id: proxyId } : {}) })
      schedulePoll()
      return true
    } catch (err) {
      error.value = describeError(err, 'Failed to start the GLM authorization')
      return false
    } finally {
      loading.value = false
    }
  }

  function schedulePoll() {
    const current = session.value
    if (!current) return
    const intervalMs = Math.max(1, current.interval_seconds || 2) * 1000
    const expected = ++generation
    polling.value = true
    timer = setTimeout(async () => {
      if (expected !== generation) return
      const result = await pollOnce()
      if (expected !== generation) return
      if (result && !result.pending) {
        polling.value = false
        return
      }
      // A failed poll keeps the session alive: a transient error must not cancel
      // an authorization the operator may already have completed.
      if (session.value) schedulePoll()
    }, intervalMs)
  }

  async function pollOnce(): Promise<ZhipuLinkPoll | undefined> {
    const current = session.value
    if (!current) return undefined
    try {
      const result = await pollZhipuLink(current.session_id)
      if (result.ready) ready.value = result.ready
      error.value = ''
      return result
    } catch (err) {
      error.value = describeError(err, 'Failed to check the GLM authorization')
      return { pending: true }
    }
  }

  async function exchangeLink(callback: string, proxyId?: number): Promise<boolean> {
    const current = session.value
    if (!current || !callback.trim()) {
      error.value = 'An authorization code or callback URL is required'
      return false
    }
    stopPolling()
    loading.value = true
    error.value = ''
    try {
      const result = await exchangeZhipuLink(current.session_id, callback.trim(), proxyId)
      if (result.ready) {
        ready.value = result.ready
        return true
      }
      error.value = 'The authorization did not return a usable credential'
      return false
    } catch (err) {
      error.value = describeError(err, 'Failed to exchange the GLM authorization code')
      return false
    } finally {
      loading.value = false
    }
  }

  async function createAccount(payload: Omit<ZhipuCreateAccountRequest, 'session_id' | 'provider'>): Promise<boolean> {
    const current = session.value
    const token = ready.value
    if (!current || !token) {
      error.value = 'Authorize the account before creating it'
      return false
    }
    loading.value = true
    error.value = ''
    try {
      await createZhipuAccountFromLink({
        ...payload,
        session_id: current.session_id,
        provider: token.provider,
        access_token: token.access_token,
        ...(token.refresh_token ? { refresh_token: token.refresh_token } : {}),
        ...(token.zcode_jwt_token ? { zcode_jwt_token: token.zcode_jwt_token } : {})
      })
      session.value = undefined
      ready.value = undefined
      return true
    } catch (err) {
      error.value = describeError(err, 'Failed to create the GLM account')
      return false
    } finally {
      loading.value = false
    }
  }

  onScopeDispose(stopPolling)

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
    stopPolling
  }
}

function describeError(err: unknown, fallback: string): string {
  const message = (err as { response?: { data?: { message?: string } } })?.response?.data?.message
  if (typeof message === 'string' && message.trim()) return message
  if (err instanceof Error && err.message) return err.message
  return fallback
}
