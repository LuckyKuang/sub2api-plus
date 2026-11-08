import { create } from 'zustand'
import { adminAPI } from '@/api'
import * as supportViewAPI from '@/api/admin/supportView'
import type { AdminUser } from '@/types'
import type { AdminSupportUser } from '@/api/admin/supportView'
import { onSupportContextChange, syncSupportContext } from './supportContext'

interface AdminSupportViewState {
  accounts: AdminUser[]
  accountsLoaded: boolean
  accountsLoading: boolean
  accountsError: string
  target: AdminSupportUser | null
  loadAccounts: (force?: boolean) => Promise<void>
  loadTarget: (userId: number) => Promise<AdminSupportUser>
  clearTarget: () => void
}

export const useAdminSupportViewStore = create<AdminSupportViewState>()((set, get) => {
  let accountsRequest: Promise<void> | null = null
  let targetRequestSequence = 0

  async function loadAccounts(force = false): Promise<void> {
    syncSupportContext()
    if (get().accountsLoaded && !force) return
    if (accountsRequest) return accountsRequest

    accountsRequest = (async () => {
      set({ accountsLoading: true, accountsError: '' })
      try {
        const collected: AdminUser[] = []
        let page = 1
        let pages = 1
        do {
          const response = await adminAPI.users.list(page, 1000, {
            include_subscriptions: false,
            sort_by: 'created_at',
            sort_order: 'desc'
          })
          collected.push(...response.items.filter((user) => !user.deleted_at))
          pages = Math.max(1, response.pages || 1)
          page += 1
        } while (page <= pages)

        const unique = new Map<number, AdminUser>()
        for (const user of collected) unique.set(user.id, user)
        set({ accounts: [...unique.values()], accountsLoaded: true })
      } catch (error) {
        set({ accountsError: error instanceof Error ? error.message : String(error) })
        throw error
      } finally {
        set({ accountsLoading: false })
        accountsRequest = null
      }
    })()
    return accountsRequest
  }

  async function loadTarget(userId: number): Promise<AdminSupportUser> {
    syncSupportContext()
    const requestSequence = ++targetRequestSequence
    const user = await supportViewAPI.getProfile(userId)
    if (requestSequence === targetRequestSequence) set({ target: user })
    return user
  }

  function clearTarget(): void {
    targetRequestSequence += 1
    set({ target: null })
  }

  // Replaces the Vue `watch(adminSupportContext, clearTarget, { flush: 'sync' })`.
  onSupportContextChange(clearTarget)

  return {
    accounts: [],
    accountsLoaded: false,
    accountsLoading: false,
    accountsError: '',
    target: null,
    loadAccounts,
    loadTarget,
    clearTarget
  }
})
