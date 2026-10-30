import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, post } }))
import { getCodexCLIRuntime, installCodexCLI } from '@/api/admin/codexCliRuntime'

describe('Codex CLI runtime API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('reads the release-pinned runtime without invoking installation', async () => {
    const status = { target_version: '0.165.0', installed: false, execution_available: false }
    get.mockResolvedValueOnce({ data: status })
    await expect(getCodexCLIRuntime()).resolves.toEqual(status)
    expect(get).toHaveBeenCalledExactlyOnceWith('/admin/settings/codex-cli')
    expect(post).not.toHaveBeenCalled()
  })

  it('requests the server-pinned installation without a client-controlled version', async () => {
    const status = { target_version: '0.165.0', installed: true, execution_available: true }
    post.mockResolvedValueOnce({ data: status })
    await expect(installCodexCLI()).resolves.toEqual(status)
    expect(post).toHaveBeenCalledExactlyOnceWith(
      '/admin/settings/codex-cli/install',
      undefined,
      { timeout: 310000 }
    )
    expect(get).not.toHaveBeenCalled()
  })

  it('propagates installation failure without automatically retrying', async () => {
    const error = { status: 503, reason: 'DOWNLOAD_UNAVAILABLE' }
    post.mockRejectedValueOnce(error)
    await expect(installCodexCLI()).rejects.toBe(error)
    expect(post).toHaveBeenCalledTimes(1)
    expect(get).not.toHaveBeenCalled()
  })
})
