import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { post } }))

import { applyOAuthCredentials } from '@/api/admin/accounts'

describe('admin account OAuth owner precondition', () => {
  beforeEach(() => {
    post.mockReset()
  })

  it.each([
    ['missing', undefined],
    ['empty', ''],
    ['truncated', 'a'.repeat(63)],
    ['extended', 'a'.repeat(65)],
    ['non-hex', 'g'.repeat(64)],
    ['uppercase', 'A'.repeat(64)],
    ['padded', ` ${'a'.repeat(64)}`],
  ])('rejects %s owner before sending credentials', async (_label, expectedOwner) => {
    await expect(applyOAuthCredentials(81, {
      expected_owner: expectedOwner as string,
      type: 'oauth',
      credentials: { access_token: 'pending-grant' },
    })).rejects.toThrow('reopen the account and retry')
    expect(post).not.toHaveBeenCalled()
  })

  it('sends the initiating owner unchanged with the replacement grant', async () => {
    const owner = '1234567890abcdef'.repeat(4)
    const account = { id: 81, oauth_credential_owner: 'b'.repeat(64) }
    post.mockResolvedValueOnce({ data: account })

    await expect(applyOAuthCredentials(81, {
      expected_owner: owner,
      type: 'oauth',
      credentials: { access_token: 'pending-grant' },
      extra: { quota_tier: 'pro' },
    })).resolves.toEqual(account)
    expect(post).toHaveBeenCalledExactlyOnceWith('/admin/accounts/81/apply-oauth-credentials', {
      expected_owner: owner,
      type: 'oauth',
      credentials: { access_token: 'pending-grant' },
      extra: { quota_tier: 'pro' },
    })
  })

  it('propagates an owner conflict without retrying against a new owner', async () => {
    const conflict = { status: 409, code: 'OAUTH_OWNER_CHANGED' }
    post.mockRejectedValueOnce(conflict)

    await expect(applyOAuthCredentials(81, {
      expected_owner: 'a'.repeat(64),
      type: 'oauth',
      credentials: { access_token: 'stale-grant' },
    })).rejects.toBe(conflict)
    expect(post).toHaveBeenCalledTimes(1)
    expect(post.mock.calls[0][1].expected_owner).toBe('a'.repeat(64))
  })
})
