import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: { get },
}))

import * as supportAPI from '@/api/admin/supportView'
import type { AdminSupportAPIKey } from '@/api/admin/supportView'

type Assert<T extends true> = T
type CredentialFieldsExcluded = Assert<
  Extract<
    keyof AdminSupportAPIKey,
    'key' | 'secret' | 'token' | 'authorization' | 'custom_key' | 'ip_whitelist' | 'ip_blacklist'
  > extends never ? true : false
>

const credentialFieldsExcluded: CredentialFieldsExcluded = true

describe('administrator support API', () => {
  beforeEach(() => {
    get.mockReset()
    get.mockResolvedValue({ data: { items: [], pages: 1 } })
  })

  it('keeps the API key contract credential-free at compile time', () => {
    expect(credentialFieldsExcluded).toBe(true)
  })

  it('uses only GET requests in the explicit target namespace', async () => {
    get
      .mockResolvedValueOnce({ data: { id: 42 } })
      .mockResolvedValueOnce({ data: { items: [], pages: 1 } })
      .mockResolvedValueOnce({ data: { items: [], has_more: false } })
      .mockResolvedValueOnce({ data: { task_id: 'imgtask_1' } })
      .mockResolvedValueOnce({ data: { total_requests: 0, total_cost: 0, total_tokens: 0 } })
      .mockResolvedValueOnce({ data: { items: [], group_rates: {} } })
      .mockResolvedValueOnce({ data: { items: [] } })
      .mockResolvedValueOnce({ data: { id: 3, models: [] } })
      .mockResolvedValueOnce({ data: [] })
      .mockResolvedValueOnce({ data: { items: [], pages: 1 } })
      .mockResolvedValueOnce({ data: { id: 9 } })

    await supportAPI.getProfile(42)
    await supportAPI.listAPIKeys(42)
    await supportAPI.listAsyncImages(42)
    await supportAPI.getAsyncImage(42, 'imgtask/1')
    await supportAPI.getUsage(42)
    await supportAPI.listChannels(42)
    await supportAPI.listChannelStatus(42)
    await supportAPI.getChannelStatus(42, 3)
    await supportAPI.listSubscriptions(42)
    await supportAPI.listOrders(42)
    await supportAPI.getOrder(42, 9)

    expect(get.mock.calls.map(([url]) => url)).toEqual([
      '/admin/support/users/42',
      '/admin/support/users/42/api-keys',
      '/admin/support/users/42/async-images',
      '/admin/support/users/42/async-images/imgtask%2F1',
      '/admin/support/users/42/usage',
      '/admin/support/users/42/channels',
      '/admin/support/users/42/channel-status',
      '/admin/support/users/42/channel-status/3',
      '/admin/support/users/42/subscriptions',
      '/admin/support/users/42/orders',
      '/admin/support/users/42/orders/9',
    ])
  })
})
