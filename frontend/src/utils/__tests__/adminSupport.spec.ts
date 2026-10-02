import { describe, expect, it } from 'vitest'

import {
  accountSelectionDestination,
  adminSupportPath,
  parseAdminSupportTargetId,
  selfPathForSupportResource,
  supportResourceForPersonalPath,
  supportResourceFromPath,
  supportPathForPersonalPath,
  type AdminSupportResource,
} from '@/utils/adminSupport'

describe('parseAdminSupportTargetId', () => {
  it('accepts only positive safe integer route IDs', () => {
    expect(parseAdminSupportTargetId('42')).toBe(42)
    expect(parseAdminSupportTargetId(['7'])).toBe(7)
    expect(parseAdminSupportTargetId('0')).toBeNull()
    expect(parseAdminSupportTargetId('-1')).toBeNull()
    expect(parseAdminSupportTargetId('1e2')).toBeNull()
    expect(parseAdminSupportTargetId('not-a-user')).toBeNull()
    expect(parseAdminSupportTargetId(String(Number.MAX_SAFE_INTEGER + 1))).toBeNull()
  })
})

const resourceCases: Array<[AdminSupportResource, string]> = [
  ['overview', '/dashboard'],
  ['api-keys', '/keys'],
  ['async-images', '/async-image'],
  ['usage', '/usage'],
  ['channels', '/available-channels'],
  ['channel-status', '/monitor'],
  ['subscriptions', '/subscriptions'],
  ['orders', '/orders'],
  ['profile', '/profile'],
  ['batch-images', '/batch-image'],
  ['purchase', '/purchase'],
  ['redeem', '/redeem'],
  ['affiliate', '/affiliate'],
]

describe('administrator support route selection', () => {
  it.each(resourceCases)('maps %s between personal and target routes', (resource, personalPath) => {
    const supportPath = adminSupportPath(42, resource)
    expect(supportResourceFromPath(supportPath)).toBe(resource)
    expect(supportResourceForPersonalPath(personalPath)).toBe(resource)
    expect(selfPathForSupportResource(resource)).toBe(personalPath)
  })

  it('keeps the current administrator on personal routes', () => {
    expect(accountSelectionDestination('/keys', 7, 7)).toBeNull()
    expect(accountSelectionDestination('/admin/users', 7, 7)).toBeNull()
  })

  it.each(resourceCases)('returns from target %s view to the matching personal page', (resource, personalPath) => {
    expect(accountSelectionDestination(adminSupportPath(42, resource), 7, 7)).toBe(personalPath)
  })

  it('uses strict ID comparison even when the other account may also be an administrator', () => {
    expect(accountSelectionDestination('/keys', 7, 8)).toBe('/admin/support/users/8/api-keys')
    expect(accountSelectionDestination('/admin/users', 7, 8)).toBe('/admin/support/users/8/overview')
  })
  it('keeps custom page IDs when switching targets or returning to the administrator', () => {
    expect(supportPathForPersonalPath(42, '/custom/help')).toBe('/admin/support/users/42/custom/help')
    expect(accountSelectionDestination('/admin/support/users/42/custom/help', 7, 43)).toBe('/admin/support/users/43/custom/help')
    expect(accountSelectionDestination('/admin/support/users/42/custom/help', 7, 7)).toBe('/custom/help')
    expect(supportPathForPersonalPath(42, '/admin/users')).toBeNull()
  })
})
