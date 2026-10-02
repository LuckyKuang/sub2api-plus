import { afterEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import type { UserAnnouncement } from '@/types'
import { setAdminSupportContext } from '@/utils/adminSupportContext'
const { list, markRead } = vi.hoisted(() => ({ list: vi.fn(), markRead: vi.fn() }))
vi.mock('@/api', () => ({ announcementsAPI: { list, markRead } }))
import { useAnnouncementStore } from '../announcements'

let store: ReturnType<typeof useAnnouncementStore>
afterEach(() => { store?.$dispose(); setAdminSupportContext(null); vi.clearAllMocks() })
describe('assistance announcements', () => {
  it('shows and dismisses announcements without writing read receipts', async () => {
    setAdminSupportContext({ actorId: 1, userId: 42 })
    setActivePinia(createPinia())
    list.mockResolvedValue([{ id: 1, title: 'User announcement', read_at: null, notify_mode: 'popup' }])
    store = useAnnouncementStore()
    await store.fetchAnnouncements()
    expect(store.currentPopup?.id).toBe(1)
    await store.dismissPopup()
    await store.markAsRead(1)
    await store.markAllAsRead()
    expect(markRead).not.toHaveBeenCalled()
    expect(store.announcements[0]?.read_at).toBeNull()
    setAdminSupportContext(null)
    expect(store.announcements).toEqual([])
    expect(list).toHaveBeenCalledTimes(1)
  })
  it('discards previous-user data and stale responses when switching targets', async () => {
    setAdminSupportContext({ actorId: 1, userId: 42 })
    setActivePinia(createPinia())
    let resolve!: (items: UserAnnouncement[]) => void
    list.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    list.mockResolvedValueOnce([{ id: 2, title: 'Second user', notify_mode: 'silent', read_at: null }])
    store = useAnnouncementStore()
    const previous = store.fetchAnnouncements()
    setAdminSupportContext({ actorId: 1, userId: 43 })
    await flushPromises()
    resolve([{ id: 1, title: 'Previous user' } as UserAnnouncement])
    await previous
    expect(store.announcements.map(item => item.id)).toEqual([2])
  })
})
