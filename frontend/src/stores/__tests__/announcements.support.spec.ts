import { afterEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import type { UserAnnouncement } from '@/types'
import { setAdminSupportContext } from '@/utils/adminSupportContext'
const { list, markRead } = vi.hoisted(() => ({ list: vi.fn(), markRead: vi.fn() }))
vi.mock('@/api', () => ({ announcementsAPI: { list, markRead } }))
import { useAnnouncementStore } from '../announcements'

let store: ReturnType<typeof useAnnouncementStore>
afterEach(() => { store?.$dispose(); setAdminSupportContext(null); vi.clearAllMocks(); vi.useRealTimers() })
describe('assistance announcements', () => {
  it('keeps announcements read-only in the list without showing popups', async () => {
    setAdminSupportContext({ actorId: 1, userId: 42 })
    setActivePinia(createPinia())
    list.mockResolvedValue([{ id: 1, title: 'User announcement', read_at: null, notify_mode: 'popup' }])
    store = useAnnouncementStore()
    await store.fetchAnnouncements()
    expect(store.announcements.map(item => item.id)).toEqual([1])
    expect(store.currentPopup).toBeNull()
    expect(store.unreadCount).toBe(1)
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
    list.mockResolvedValueOnce([{ id: 2, title: 'Second user', notify_mode: 'popup', read_at: null }])
    store = useAnnouncementStore()
    const previous = store.fetchAnnouncements()
    setAdminSupportContext({ actorId: 1, userId: 43 })
    await flushPromises()
    resolve([{ id: 1, title: 'Previous user' } as UserAnnouncement])
    await previous
    expect(store.announcements.map(item => item.id)).toEqual([2])
    expect(store.currentPopup).toBeNull()
    expect(markRead).not.toHaveBeenCalled()
  })

  it('clears existing and delayed popups on entry and restores normal user behavior on exit', async () => {
    vi.useFakeTimers()
    setActivePinia(createPinia())
    const notices = [1, 2].map(id => ({ id, title: `Notice ${id}`, notify_mode: 'popup', read_at: null }))
    list.mockResolvedValue(notices)
    markRead.mockResolvedValue(undefined)
    store = useAnnouncementStore()
    await store.fetchAnnouncements()
    expect(store.currentPopup?.id).toBe(1)
    await store.dismissPopup()
    expect(markRead.mock.calls).toEqual([[1]])

    markRead.mockClear()
    setAdminSupportContext({ actorId: 1, userId: 42 })
    await Promise.resolve()
    await vi.advanceTimersByTimeAsync(300)
    expect(store.currentPopup).toBeNull()
    expect(store.announcements.map(item => item.id)).toEqual([1, 2])
    expect(markRead).not.toHaveBeenCalled()

    setAdminSupportContext(null)
    await store.fetchAnnouncements()
    expect(store.currentPopup?.id).toBe(2)
    await store.dismissPopup()
    expect(markRead.mock.calls).toEqual([[2]])
    expect(store.announcements[1]?.read_at).toBeTruthy()
  })

  it('discards an ordinary user popup response arriving after assistance starts', async () => {
    setActivePinia(createPinia())
    let resolve!: (items: UserAnnouncement[]) => void
    list.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    list.mockResolvedValueOnce([{ id: 2, title: 'Assisted user', notify_mode: 'popup', read_at: null }])
    store = useAnnouncementStore()
    const previous = store.fetchAnnouncements()
    setAdminSupportContext({ actorId: 1, userId: 42 })
    await flushPromises()
    resolve([{ id: 1, title: 'Ordinary user', notify_mode: 'popup', read_at: null } as UserAnnouncement])
    await previous
    expect(store.announcements.map(item => item.id)).toEqual([2])
    expect(store.currentPopup).toBeNull()
    expect(markRead).not.toHaveBeenCalled()
  })
})
