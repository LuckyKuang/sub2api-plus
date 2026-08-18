import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const { listUsers, getProfile } = vi.hoisted(() => ({
  listUsers: vi.fn(),
  getProfile: vi.fn(),
}))

vi.mock('@/api', () => ({
  adminAPI: { users: { list: listUsers } },
}))

vi.mock('@/api/admin/supportView', () => ({
  getProfile,
}))

import { useAdminSupportViewStore } from '@/stores/adminSupportView'

describe('administrator support target store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    listUsers.mockReset()
    getProfile.mockReset()
  })

  it('loads every page and excludes soft-deleted accounts', async () => {
    listUsers
      .mockResolvedValueOnce({
        items: [{ id: 1, email: 'admin@example.com' }, { id: 2, email: 'deleted@example.com', deleted_at: '2026-01-01' }],
        pages: 2,
      })
      .mockResolvedValueOnce({
        items: [{ id: 3, email: 'disabled@example.com', status: 'disabled' }],
        pages: 2,
      })

    const store = useAdminSupportViewStore()
    await store.loadAccounts()

    const expectedFilters = {
      include_subscriptions: false,
      sort_by: 'created_at',
      sort_order: 'desc',
    }
    expect(listUsers).toHaveBeenNthCalledWith(1, 1, 1000, expectedFilters)
    expect(listUsers).toHaveBeenNthCalledWith(2, 2, 1000, expectedFilters)
    expect(store.accounts.map((user) => user.id)).toEqual([1, 3])
  })

  it('stores target display metadata without any authentication state', async () => {
    getProfile.mockResolvedValue({ id: 8, email: 'target@example.com' })
    const store = useAdminSupportViewStore()

    await expect(store.loadTarget(8)).resolves.toMatchObject({ id: 8 })
    expect(store.target).toMatchObject({ id: 8, email: 'target@example.com' })
    expect(Object.keys(store)).not.toContain('token')
    expect(Object.keys(store)).not.toContain('user')
  })

  it('does not let an older target response replace the latest selection', async () => {
    let resolveFirst: ((value: { id: number; email: string }) => void) | undefined
    getProfile
      .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve }))
      .mockResolvedValueOnce({ id: 9, email: 'latest@example.com' })

    const store = useAdminSupportViewStore()
    const first = store.loadTarget(8)
    await store.loadTarget(9)
    resolveFirst?.({ id: 8, email: 'stale@example.com' })
    await first

    expect(store.target).toMatchObject({ id: 9, email: 'latest@example.com' })
  })

  it('revalidates a cached target before each support page load', async () => {
    getProfile
      .mockResolvedValueOnce({ id: 8, email: 'before@example.com' })
      .mockResolvedValueOnce({ id: 8, email: 'after@example.com' })

    const store = useAdminSupportViewStore()
    await store.loadTarget(8)
    await store.loadTarget(8)

    expect(getProfile).toHaveBeenCalledTimes(2)
    expect(store.target).toMatchObject({ id: 8, email: 'after@example.com' })
  })
})
