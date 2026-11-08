import { create } from 'zustand'
import { announcementsAPI } from '@/api'
import type { UserAnnouncement } from '@/types'
import { currentSupportContext, onSupportContextChange, syncSupportContext } from './supportContext'
import { supportRequestGeneration } from '@/utils/adminSupportContext'

const THROTTLE_MS = 20 * 60 * 1000 // 20 minutes

interface AnnouncementsState {
  announcements: UserAnnouncement[]
  loading: boolean
  currentPopup: UserAnnouncement | null
  unreadCount: () => number
  fetchAnnouncements: (force?: boolean) => Promise<void>
  dismissPopup: () => Promise<void>
  markAsRead: (id: number) => Promise<boolean | undefined>
  markAllAsRead: () => Promise<void>
  reset: () => void
}

export const useAnnouncementsStore = create<AnnouncementsState>()((set, get) => {
  // Session-scoped dedup set — not reactive, used as plain lookup only
  let shownPopupIds = new Set<number>()

  // Internal bookkeeping kept out of the public store surface, like the Vue refs it replaces.
  let lastFetchTime = 0
  let popupQueue: UserAnnouncement[] = []

  function enqueueNewPopups() {
    if (currentSupportContext()) return
    const newPopups = get().announcements.filter(
      (a) => a.notify_mode === 'popup' && !a.read_at && !shownPopupIds.has(a.id)
    )
    if (newPopups.length === 0) return

    for (const p of newPopups) {
      if (!popupQueue.some((q) => q.id === p.id)) {
        popupQueue.push(p)
      }
    }

    if (!get().currentPopup) {
      showNextPopup()
    }
  }

  function showNextPopup() {
    if (currentSupportContext() || popupQueue.length === 0) {
      set({ currentPopup: null })
      return
    }
    const next = popupQueue.shift()!
    shownPopupIds.add(next.id)
    set({ currentPopup: next })
  }

  async function fetchAnnouncements(force = false) {
    syncSupportContext()
    const scope = supportRequestGeneration()
    const now = Date.now()
    if (!force && lastFetchTime > 0 && now - lastFetchTime < THROTTLE_MS) {
      return
    }

    // Set immediately to prevent concurrent duplicate requests
    lastFetchTime = now

    try {
      set({ loading: true })
      const all = await announcementsAPI.list(false)
      if (scope !== supportRequestGeneration()) return
      set({ announcements: all.slice(0, 20) })
      enqueueNewPopups()
    } catch (err: any) {
      if (scope !== supportRequestGeneration()) return
      // Revert throttle timestamp on failure so retry is allowed
      lastFetchTime = 0
      console.error('Failed to fetch announcements:', err)
    } finally {
      if (scope === supportRequestGeneration()) set({ loading: false })
    }
  }

  async function dismissPopup() {
    syncSupportContext()
    const popup = get().currentPopup
    if (!popup) return
    const id = popup.id
    set({ currentPopup: null })

    // Mark as read (fire-and-forget, UI already updated)
    if (!currentSupportContext()) void markAsRead(id)

    // Show next popup after a short delay
    if (popupQueue.length > 0) {
      const scope = supportRequestGeneration()
      setTimeout(() => { if (scope === supportRequestGeneration()) showNextPopup() }, 300)
    }
  }

  async function markAsRead(id: number) {
    if (currentSupportContext()) return
    try {
      await announcementsAPI.markRead(id)
      set({
        announcements: get().announcements.map((a) =>
          a.id === id ? { ...a, read_at: new Date().toISOString() } : a
        )
      })
      return true
    } catch (err: any) {
      console.error('Failed to mark announcement as read:', err)
      return false
    }
  }

  async function markAllAsRead() {
    if (currentSupportContext()) return
    const unread = get().announcements.filter((a) => !a.read_at)
    if (unread.length === 0) return

    try {
      set({ loading: true })
      const readAt = new Map<number, string>()
      const results = await Promise.allSettled(unread.map(async (a) => {
        await announcementsAPI.markRead(a.id)
        readAt.set(a.id, new Date().toISOString())
      }))
      set({
        announcements: get().announcements.map((a) => {
          const timestamp = readAt.get(a.id)
          return timestamp ? { ...a, read_at: timestamp } : a
        })
      })
      const failure = results.find((result) => result.status === 'rejected')
      if (failure) throw failure.reason
    } catch (err: any) {
      console.error('Failed to mark all as read:', err)
      throw err
    } finally {
      set({ loading: false })
    }
  }

  function reset() {
    set({ announcements: [], loading: false, currentPopup: null })
    lastFetchTime = 0
    shownPopupIds = new Set()
    popupQueue = []
  }

  // Replaces the Vue `watch(adminSupportContext, ...)`: entering assistance drops the
  // previous user's announcements and refetches for the assisted user.
  onSupportContextChange(() => {
    reset()
    if (currentSupportContext()) void fetchAnnouncements(true)
  })

  return {
    // State
    announcements: [],
    loading: false,
    currentPopup: null,
    // Getters
    unreadCount: () => get().announcements.filter((a) => !a.read_at).length,
    // Actions
    fetchAnnouncements,
    dismissPopup,
    markAsRead,
    markAllAsRead,
    reset
  }
})

/** The Vue barrel exported this store as `useAnnouncementStore`. */
export const useAnnouncementStore = useAnnouncementsStore
