import { defineStore } from 'pinia'
import { ref } from 'vue'
import { adminAPI } from '@/api'
import * as supportViewAPI from '@/api/admin/supportView'
import type { AdminUser } from '@/types'
import type { AdminSupportUser } from '@/api/admin/supportView'

export const useAdminSupportViewStore = defineStore('adminSupportView', () => {
  const accounts = ref<AdminUser[]>([])
  const accountsLoaded = ref(false)
  const accountsLoading = ref(false)
  const accountsError = ref('')
  const target = ref<AdminSupportUser | null>(null)
  let accountsRequest: Promise<void> | null = null
  let targetRequestSequence = 0

  async function loadAccounts(force = false): Promise<void> {
    if (accountsLoaded.value && !force) return
    if (accountsRequest) return accountsRequest

    accountsRequest = (async () => {
      accountsLoading.value = true
      accountsError.value = ''
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
        accounts.value = [...unique.values()]
        accountsLoaded.value = true
      } catch (error) {
        accountsError.value = error instanceof Error ? error.message : String(error)
        throw error
      } finally {
        accountsLoading.value = false
        accountsRequest = null
      }
    })()
    return accountsRequest
  }

  async function loadTarget(userId: number): Promise<AdminSupportUser> {
    const requestSequence = ++targetRequestSequence
    const user = await supportViewAPI.getProfile(userId)
    if (requestSequence === targetRequestSequence) target.value = user
    return user
  }

  function clearTarget(): void {
    targetRequestSequence += 1
    target.value = null
  }

  return {
    accounts,
    accountsLoaded,
    accountsLoading,
    accountsError,
    target,
    loadAccounts,
    loadTarget,
    clearTarget
  }
})
