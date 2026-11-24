import { describe, expect, it } from 'vitest'
import { resolvePortalArea } from '@/utils/portalArea'

const base = {
  isAdmin: true,
  path: '/dashboard',
  userCustomIds: ['help', 'shared'],
  adminCustomIds: ['admin-help']
}

describe('portal area classification', () => {
  it('keeps non-admin pages in the user portal even under the admin path prefix', () => {
    expect(resolvePortalArea({ ...base, isAdmin: false, path: '/admin/dashboard' })).toBe('user')
  })

  it('classifies management routes as the admin console', () => {
    expect(resolvePortalArea({ ...base, path: '/admin' })).toBe('admin')
    expect(resolvePortalArea({ ...base, path: '/admin/dashboard' })).toBe('admin')
    expect(resolvePortalArea({ ...base, path: '/admin/users' })).toBe('admin')
  })

  it('classifies personal and assistance routes as the user portal', () => {
    expect(resolvePortalArea({ ...base, path: '/dashboard' })).toBe('user')
    expect(resolvePortalArea({ ...base, path: '/keys' })).toBe('user')
    expect(resolvePortalArea({ ...base, path: '/admin/support/users/42/api-keys', supportResource: 'api-keys' })).toBe('user')
    expect(resolvePortalArea({ ...base, path: '/admin/support/users/42/custom/help', supportResource: 'custom' })).toBe('user')
  })

  it('follows custom menu visibility for shared custom pages', () => {
    expect(resolvePortalArea({ ...base, path: '/custom/help', routeName: 'CustomPage', customId: 'help' })).toBe('user')
    expect(resolvePortalArea({ ...base, path: '/custom/admin-help', routeName: 'CustomPage', customId: 'admin-help' })).toBe('admin')
    expect(resolvePortalArea({ ...base, path: '/custom/unknown', routeName: 'CustomPage', customId: 'unknown' })).toBe('user')
  })

  it('derives the custom page id from the path when the route params are not ready', () => {
    expect(resolvePortalArea({ ...base, path: '/custom/admin-help', customId: null })).toBe('admin')
    expect(resolvePortalArea({ ...base, path: '/custom/help', customId: null })).toBe('user')
  })
})
