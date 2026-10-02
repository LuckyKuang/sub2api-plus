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
  | 'batch-images'
  | 'purchase'
  | 'redeem'
  | 'affiliate'
  | 'custom'

export const selfPaths: Record<AdminSupportResource, string> = {
  overview: '/dashboard',
  'api-keys': '/keys',
  'async-images': '/async-image',
  usage: '/usage',
  channels: '/available-channels',
  'channel-status': '/monitor',
  subscriptions: '/subscriptions',
  orders: '/orders',
  profile: '/profile',
  'batch-images': '/batch-image',
  purchase: '/purchase',
  redeem: '/redeem',
  affiliate: '/affiliate',
  custom: '/custom/:id'
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
  const match = path.match(/^\/admin\/support\/users\/\d+\/(overview|api-keys|async-images|usage|channels|channel-status|subscriptions|orders|profile|batch-images|purchase|redeem|affiliate|custom)(?:\/|$)/)
  return (match?.[1] as AdminSupportResource | undefined) ?? null
}

export function supportResourceForPersonalPath(path: string): AdminSupportResource {
  if (path === '/batch-image' || path.startsWith('/batch-image/')) return 'batch-images'
  if (path === '/purchase' || path.startsWith('/purchase/')) return 'purchase'
  if (path === '/redeem' || path.startsWith('/redeem/')) return 'redeem'
  if (path === '/affiliate' || path.startsWith('/affiliate/')) return 'affiliate'
  if (path.startsWith('/custom/')) return 'custom'
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
    return currentSupportResource ? currentSupportResource === 'custom' ? '/custom/' + currentPath.split('/custom/')[1] : selfPathForSupportResource(currentSupportResource) : null
  }
  const resource = supportResourceForPersonalPath(currentPath)
  return adminSupportPath(targetUserId, resource) + (resource === 'custom' ? '/' + currentPath.split('/custom/')[1] : '')
}

export function supportPathForPersonalPath(userId: number, path: string): string | null {
  const entry = Object.entries(selfPaths).find(([, self]) => self === path || (self === '/custom/:id' && path.startsWith('/custom/')))
  if (!entry) return null
  const resource = entry[0] as AdminSupportResource
  return adminSupportPath(userId, resource) + (resource === 'custom' ? '/' + path.slice('/custom/'.length) : '')
}
