export type AdminSupportResource =
  | 'overview'
  | 'api-keys'
  | 'async-images'
  | 'usage'
  | 'channels'
  | 'channel-status'
  | 'subscriptions'
  | 'orders'
  | 'profile'

const selfPaths: Record<AdminSupportResource, string> = {
  overview: '/keys',
  'api-keys': '/keys',
  'async-images': '/async-image',
  usage: '/usage',
  channels: '/available-channels',
  'channel-status': '/monitor',
  subscriptions: '/subscriptions',
  orders: '/orders',
  profile: '/profile'
}

export function parseAdminSupportTargetId(value: unknown): number | null {
  const raw = Array.isArray(value) ? value[0] : value
  if (typeof raw !== 'string' && typeof raw !== 'number') return null
  const normalized = String(raw).trim()
  if (!/^\d+$/.test(normalized)) return null
  const userId = Number(normalized)
  return Number.isSafeInteger(userId) && userId > 0 ? userId : null
}

export function adminSupportPath(userId: number, resource: AdminSupportResource): string {
  return `/admin/support/users/${userId}/${resource}`
}

export function selfPathForSupportResource(resource: AdminSupportResource): string {
  return selfPaths[resource]
}

export function supportResourceFromPath(path: string): AdminSupportResource | null {
  const match = path.match(/^\/admin\/support\/users\/\d+\/(overview|api-keys|async-images|usage|channels|channel-status|subscriptions|orders|profile)(?:\/|$)/)
  return (match?.[1] as AdminSupportResource | undefined) ?? null
}

export function supportResourceForPersonalPath(path: string): AdminSupportResource {
  if (path === '/async-image' || path.startsWith('/async-image/')) return 'async-images'
  if (path === '/usage' || path.startsWith('/usage/')) return 'usage'
  if (path === '/available-channels' || path.startsWith('/available-channels/')) return 'channels'
  if (path === '/monitor' || path.startsWith('/monitor/')) return 'channel-status'
  if (path === '/subscriptions' || path.startsWith('/subscriptions/')) return 'subscriptions'
  if (path === '/orders' || path.startsWith('/orders/')) return 'orders'
  if (path === '/profile' || path.startsWith('/profile/')) return 'profile'
  if (path === '/keys' || path.startsWith('/keys/')) return 'api-keys'
  return supportResourceFromPath(path) ?? 'overview'
}

export function accountSelectionDestination(
  currentPath: string,
  actorUserId: number,
  targetUserId: number
): string | null {
  const currentSupportResource = supportResourceFromPath(currentPath)
  if (targetUserId === actorUserId) {
    return currentSupportResource ? selfPathForSupportResource(currentSupportResource) : null
  }
  return adminSupportPath(targetUserId, supportResourceForPersonalPath(currentPath))
}
