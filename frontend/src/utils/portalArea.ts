export type PortalArea = 'admin' | 'user'

export const USER_PORTAL_HOME = '/dashboard'
export const ADMIN_PORTAL_HOME = '/admin/dashboard'

export interface PortalAreaInput {
  isAdmin: boolean
  path: string
  routeName?: string | null
  supportResource?: unknown
  // Explicit route ownership for shared entrypoints that do not use the
  // production path prefixes (for example the independent layout fixtures).
  portalArea?: PortalArea | null
  customId?: string | null
  userCustomIds?: Iterable<string>
  adminCustomIds?: Iterable<string>
}

function includesId(ids: Iterable<string> | undefined, id: string): boolean {
  if (!ids) return false
  for (const value of ids) {
    if (value === id) return true
  }
  return false
}

function customPageIdFromPath(path: string): string | null {
  if (!path.startsWith('/custom/')) return null
  const id = path.slice('/custom/'.length).split(/[/?#]/)[0]
  return id || null
}

// A page belongs to the admin console unless it is an authenticated personal
// page. Support routes are the target user's personal pages even though their
// path sits under /admin. Shared custom pages follow the menu visibility that
// makes them reachable.
export function resolvePortalArea(input: PortalAreaInput): PortalArea {
  if (!input.isAdmin) return 'user'
  if (input.supportResource) return 'user'
  if (input.portalArea === 'user') return 'user'
  if (input.portalArea === 'admin') return 'admin'
  if (input.path === '/admin' || input.path.startsWith('/admin/')) return 'admin'

  const customId = input.customId ?? customPageIdFromPath(input.path)
  if (input.routeName === 'CustomPage' || customId) {
    if (customId && includesId(input.userCustomIds, customId)) return 'user'
    if (customId && includesId(input.adminCustomIds, customId)) return 'admin'
    return 'user'
  }

  return 'user'
}
