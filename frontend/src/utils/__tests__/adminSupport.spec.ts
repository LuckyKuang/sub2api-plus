import { describe, expect, it } from 'vitest'

import {
  ADMIN_SUPPORT_SAFE_LANDING,
  accountSelectionDestination,
  adminSupportPath,
  parseAdminSupportTargetId,
  resolveAdminSupportRouteDecision,
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
  it('keeps the unified image menu and account selection inside the target support view', () => {
    expect(supportPathForPersonalPath(42, '/image-tasks')).toBe('/admin/support/users/42/async-images')
    expect(accountSelectionDestination('/image-tasks', 7, 42)).toBe('/admin/support/users/42/async-images')
  })
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

describe('assistance route decisions', () => {
  it('never falls back to the caller personal page for an invalid target', () => {
    expect(resolveAdminSupportRouteDecision({
      resource: 'api-keys', targetUserId: null, actorUserId: 7,
    })).toEqual({ kind: 'safe-landing', path: ADMIN_SUPPORT_SAFE_LANDING })
  })

  it('normalizes a valid self target to the personal page', () => {
    expect(resolveAdminSupportRouteDecision({
      resource: 'api-keys', targetUserId: 7, actorUserId: 7,
    })).toEqual({ kind: 'self', path: '/keys' })
    expect(resolveAdminSupportRouteDecision({
      resource: 'custom', targetUserId: 7, actorUserId: 7, customId: 'help',
    })).toEqual({ kind: 'self', path: '/custom/help' })
    expect(resolveAdminSupportRouteDecision({
      resource: 'custom', targetUserId: 7, actorUserId: 7,
    })).toEqual({ kind: 'safe-landing', path: ADMIN_SUPPORT_SAFE_LANDING })
  })

  it('treats every other account as a read-only support target', () => {
    expect(resolveAdminSupportRouteDecision({
      resource: 'api-keys', targetUserId: 8, actorUserId: 7,
    })).toEqual({ kind: 'support', userId: 8 })
    expect(resolveAdminSupportRouteDecision({
      resource: 'api-keys', targetUserId: 8, actorUserId: 7, customId: 'help',
    })).toEqual({ kind: 'support', userId: 8 })
  })
})
