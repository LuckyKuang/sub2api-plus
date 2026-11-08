/**
 * Subscription Store
 * Global state management for user subscriptions with caching and deduplication
 */

import { create } from 'zustand'
import subscriptionsAPI from '@/api/subscriptions'
import type { UserSubscription } from '@/types'
import { onLocaleRefresh } from './localeRefresh'
import { onSupportContextChange, syncSupportContext } from './supportContext'

// Cache TTL: 60 seconds
const CACHE_TTL_MS = 60_000

// Request generation counter to invalidate stale in-flight responses
let requestGeneration = 0

interface SubscriptionsState {
  activeSubscriptions: UserSubscription[]
  loading: boolean
  hasActiveSubscriptions: () => boolean
  fetchActiveSubscriptions: (force?: boolean) => Promise<UserSubscription[]>
  startPolling: () => void
  stopPolling: () => void
  clear: () => void
  invalidateCache: () => void
}

export const useSubscriptionsStore = create<SubscriptionsState>()((set, get) => {
  // Internal bookkeeping kept out of the public store surface, like the Vue refs it replaces.
  let loaded = false
  let lastFetchedAt: number | null = null

  // In-flight request deduplication
  let activePromise: Promise<UserSubscription[]> | null = null

  // Auto-refresh interval
  let pollerInterval: ReturnType<typeof setInterval> | null = null

  /**
   * Fetch active subscriptions with caching and deduplication
   * @param force - Force refresh even if cache is valid
   */
  async function fetchActiveSubscriptions(force = false): Promise<UserSubscription[]> {
    syncSupportContext()
    const now = Date.now()

    // Return cached data if valid
    if (
      !force &&
      loaded &&
      lastFetchedAt &&
      now - lastFetchedAt < CACHE_TTL_MS
    ) {
      return get().activeSubscriptions
    }

    // Return in-flight request if exists (deduplication)
    if (activePromise && !force) {
      return activePromise
    }

    const currentGeneration = ++requestGeneration

    // Start new request
    set({ loading: true })
    const requestPromise = subscriptionsAPI
      .getActiveSubscriptions()
      .then((data) => {
        if (currentGeneration === requestGeneration) {
          loaded = true
          lastFetchedAt = Date.now()
          set({ activeSubscriptions: data })
        }
        return data
      })
      .catch((error) => {
        console.error('Failed to fetch active subscriptions:', error)
        throw error
      })
      .finally(() => {
        if (activePromise === requestPromise) {
          set({ loading: false })
          activePromise = null
        }
      })

    activePromise = requestPromise

    return activePromise
  }

  /**
   * Start auto-refresh polling
   */
  function startPolling() {
    syncSupportContext()
    if (pollerInterval) return

    pollerInterval = setInterval(() => {
      fetchActiveSubscriptions(true).catch((error) => {
        console.error('Subscription polling failed:', error)
      })
    }, 5 * 60 * 1000)
  }

  /**
   * Stop auto-refresh polling
   */
  function stopPolling() {
    if (pollerInterval) {
      clearInterval(pollerInterval)
      pollerInterval = null
    }
  }

  /**
   * Clear all subscription data and stop polling
   */
  function clear() {
    requestGeneration++
    activePromise = null
    set({ loading: false, activeSubscriptions: [] })
    loaded = false
    lastFetchedAt = null
    stopPolling()
  }

  /**
   * Invalidate cache (force next fetch to reload)
   */
  function invalidateCache() {
    lastFetchedAt = null
  }

  // Replaces the Vue `watch(adminSupportContext, clear, { flush: 'sync' })`.
  onSupportContextChange(clear)

  // Replaces the Vue `useLocaleRefresh(...)` registration.
  onLocaleRefresh(() => {
    if (loaded || activePromise) return fetchActiveSubscriptions(true)
    invalidateCache()
  })

  return {
    // State
    activeSubscriptions: [],
    loading: false,
    hasActiveSubscriptions: () => get().activeSubscriptions.length > 0,

    // Actions
    fetchActiveSubscriptions,
    startPolling,
    stopPolling,
    clear,
    invalidateCache
  }
})

/** The Vue barrel exported this store as `useSubscriptionStore`. */
export const useSubscriptionStore = useSubscriptionsStore
