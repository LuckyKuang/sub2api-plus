import { shallowRef } from 'vue'

export interface SupportContext { userId: number; actorId: number }
export const adminSupportContext = shallowRef<SupportContext | null>(null)
let generation = 0
const imageKeys = new Map<string, number>()
const pendingImageReads = new Set<AbortController>()

export function setAdminSupportContext(context: SupportContext | null): void {
  if (context?.userId === adminSupportContext.value?.userId && context?.actorId === adminSupportContext.value?.actorId) return
  generation += 1
  imageKeys.clear()
  for (const controller of pendingImageReads) controller.abort()
  pendingImageReads.clear()
  adminSupportContext.value = context
}

export function supportRequestGeneration(): number { return generation }
export function supportReadOnlyError(): Error {
  return Object.assign(new Error('This user assistance view is read-only.'), { code: 'ADMIN_SUPPORT_READ_ONLY', status: 403 })
}

const personalReads = [
  /^\/user\/(profile|platform-quotas|aff|passkeys|totp\/(status|verification-method))$/,
  /^\/user\/api-keys\/\d+\/usage\/daily$/,
  /^\/keys(?:\/\d+)?$/,
  /^\/groups\/(available|rates)$/,
  /^\/channels\/available$/,
  /^\/usage(?:\/(stats|errors(?:\/\d+)?|\d+|dashboard\/(stats|trend|models|snapshot-v2|api-keys-usage)))?$/,
  /^\/subscriptions(?:\/(active|progress|summary))?$/,
  /^\/redeem\/history$/,
  /^\/announcements$/,
  /^\/channel-monitors(?:\/\d+\/status)?$/,
  /^\/channel-monitor-v2\/(dimensions|snapshot|models|matrix|errors|users)$/,
  /^\/channel-monitor-v3\/snapshot$/,
  /^\/payment\/(config|checkout-info|plans|limits|orders\/(my|\d+|refund-eligible-providers))$/,
]

export function supportReadURL(url: string, userId: number): string | null {
  const path = url.split('?')[0]
  return personalReads.some(pattern => pattern.test(path)) ? `/admin/support/users/${userId}${url}` : null
}

export function rememberSupportImageKeys(keys: Array<{ id: number; key: string }>): void {
  if (!adminSupportContext.value) return
  for (const key of keys) imageKeys.set(key.key, key.id)
}

// Image APIs normally authenticate with the user's key. In assistance, use the
// administrator JWT and explicit target/key IDs, never the user's credential.
export async function supportImageFetch(
  path: string, apiKey: string, init: RequestInit, gatewayURL: (path: string) => string, apiURL: (path: string) => string
): Promise<Response> {
  const context = adminSupportContext.value
  if (!context) return fetch(gatewayURL(path), init)
  if ((init.method || 'GET').toUpperCase() !== 'GET') throw supportReadOnlyError()
  const keyId = imageKeys.get(apiKey)
  if (!keyId) throw new Error('The selected API key is not in the current assistance view.')
  const scope = generation
  const controller = new AbortController()
  pendingImageReads.add(controller)
  const supportPath = path.split('?')[0] === '/v1/models' ? path.replace('/v1/models', '/images/models') : path.replace(/^\/v1/, '')
  const separator = supportPath.includes('?') ? '&' : '?'
  const abort = () => controller.abort()
  try {
    init.signal?.addEventListener('abort', abort, { once: true })
    if (init.signal?.aborted) controller.abort()
    const response = await fetch(apiURL(`/admin/support/users/${context.userId}${supportPath}${separator}api_key_id=${keyId}`), {
      ...init,
      signal: controller.signal,
      credentials: 'include',
      headers: { Authorization: `Bearer ${localStorage.getItem('auth_token') || ''}`, 'X-Admin-UI-Request': '1' }
    })
    if (scope !== generation) throw new DOMException('Assistance target changed', 'AbortError')
    return response
  } finally { init.signal?.removeEventListener('abort', abort); pendingImageReads.delete(controller) }
}
