import { beforeEach, describe, expect, it, vi } from 'vitest'
const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get } }))
import { getProfile } from '@/api/admin/supportView'

describe('complete user assistance profile', () => {
  beforeEach(() => get.mockReset())
  it('returns the same profile, identity and notification fields without replacing auth', async () => {
    const profile = { id: 42, avatar_url: '/avatar.png', email_bound: true, auth_bindings: { oidc: { bound: true, display_name: 'First User' } }, balance_notify_extra_emails: [{ email: 'notify@example.test', verified: true }] }
    get.mockResolvedValue({ data: profile })
    expect(await getProfile(42)).toEqual(profile)
    expect(get).toHaveBeenCalledWith('/admin/support/users/42/user/profile')
  })
})
