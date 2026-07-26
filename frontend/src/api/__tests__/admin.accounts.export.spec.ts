import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, post }
}))

import { exportData, getExportRequirements } from '@/api/admin/accounts'

describe('admin account export API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('reads the export confirmation requirements from the dedicated endpoint', async () => {
    const requirements = { step_up_required: true }
    get.mockResolvedValueOnce({ data: requirements })

    await expect(getExportRequirements()).resolves.toEqual(requirements)
    expect(get).toHaveBeenCalledWith('/admin/accounts/export/requirements')
  })

  it('posts the one-time confirmation and scope without URL query parameters', async () => {
    const payload = { type: 'sub2api-data', version: 1, accounts: [], proxies: [] }
    post.mockResolvedValueOnce({ data: payload })

    await expect(exportData({
      password: 'current-admin-password',
      totpCode: '123456',
      ids: [12, 34],
      filters: { platform: 'openai', sort_by: 'priority', sort_order: 'desc' },
      includeProxies: false
    })).resolves.toEqual(payload)

    expect(post).toHaveBeenCalledWith('/admin/accounts/export', {
      password: 'current-admin-password',
      totp_code: '123456',
      ids: [12, 34],
      filters: { platform: 'openai', sort_by: 'priority', sort_order: 'desc' },
      include_proxies: false
    })
  })
})
