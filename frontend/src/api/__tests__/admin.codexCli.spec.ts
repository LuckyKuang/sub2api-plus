import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, put } = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, put } }))

import { getCodexCLIAccount, updateCodexCLIAccount } from '@/api/admin/codexCli'

// The dedicated account API must preserve the initiating owner and secret PATCH
// semantics: omission keeps, a string replaces, and null explicitly clears.
describe('Codex CLI account API', () => {
  beforeEach(() => {
    get.mockReset()
    put.mockReset()
  })

  it('loads only the dedicated redacted view and supports cancellation', async () => {
    const signal = new AbortController().signal
    const view = { config_toml: 'sandbox_mode = "danger-full-access"', auth_status: { has_access_token: true } }
    get.mockResolvedValueOnce({ data: view })

    await expect(getCodexCLIAccount(24, { signal })).resolves.toEqual(view)
    expect(get).toHaveBeenCalledExactlyOnceWith('/admin/accounts/24/codex-cli', { signal })
    expect(put).not.toHaveBeenCalled()
  })

  it('keeps omission, replacement and explicit clearing intact', async () => {
    const payload = {
      expected_owner: 'a'.repeat(64),
      config_toml: '',
      auth: { tokens: { access_token: 'replacement', id_token: null } },
    }
    put.mockResolvedValueOnce({ data: { expected_owner: 'b'.repeat(64) } })

    await expect(updateCodexCLIAccount(24, payload)).resolves.toEqual({ expected_owner: 'b'.repeat(64) })
    expect(put).toHaveBeenCalledExactlyOnceWith('/admin/accounts/24/codex-cli', payload)
    expect(put.mock.calls[0][1].auth.tokens).not.toHaveProperty('refresh_token')
  })

  it.each(['', 'a'.repeat(63), 'A'.repeat(64), ` ${'a'.repeat(64)}`, 'g'.repeat(64)])(
    'rejects invalid owner %s before sending credentials', async owner => {
      await expect(updateCodexCLIAccount(24, {
        expected_owner: owner,
        config_toml: '',
        auth: { tokens: { access_token: 'pending-secret' } },
      })).rejects.toThrow('reopen the account and retry')
      expect(put).not.toHaveBeenCalled()
    },
  )

  it('does not retry an owner conflict against a freshly fetched account', async () => {
    const error = { status: 409, code: 'CODEX_CLI_OWNER_CHANGED' }
    put.mockRejectedValueOnce(error)

    await expect(updateCodexCLIAccount(24, {
      expected_owner: 'c'.repeat(64), config_toml: 'approval_policy = "never"',
    })).rejects.toBe(error)
    expect(put).toHaveBeenCalledTimes(1)
    expect(get).not.toHaveBeenCalled()
  })
})
