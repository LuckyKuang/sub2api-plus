import { computed, ref } from 'vue'
import { keysAPI } from '@/api/keys'
import { useAuthStore } from '@/stores/auth'
import type { ApiKey } from '@/types'

const loaded = ref(false)
const loading = ref(false)
const hasManageableAsyncImageKey = ref(false)
let pendingLoad: Promise<boolean> | null = null
const pageSize = 100

export function keyCanManageAsyncImage(key: ApiKey): boolean {
  return key.status === 'active' || key.status === 'quota_exhausted' || key.status === 'expired'
}

export function keyAllowsAsyncImage(key: ApiKey): boolean {
  return key.status === 'active'
    && (key.group?.platform === 'openai' || key.group?.platform === 'grok')
    && key.group?.allow_image_generation === true
}

// Administrators need the entry even when they have not created a personal API
// key, so they can verify the feature and manage the group-level permission.
// Any non-disabled user key may own history after its status, group, or platform
// changes, so management access cannot be gated on current submission support.
export function canViewAsyncImage(isAdmin: boolean, hasManagementKey: boolean): boolean {
  return isAdmin || hasManagementKey
}

async function loadAsyncImageAccess(force = false): Promise<boolean> {
  const authStore = useAuthStore()
  if (!authStore.isAuthenticated) {
    loaded.value = true
    hasManageableAsyncImageKey.value = false
    return false
  }
  if (loaded.value && !force) return hasManageableAsyncImageKey.value
  if (pendingLoad && !force) return pendingLoad

  loading.value = true
  pendingLoad = (async () => {
    let page = 1
    while (true) {
      const response = await keysAPI.list(page, pageSize, { sort_by: 'created_at', sort_order: 'desc' })
      if ((response.items || []).some(keyCanManageAsyncImage)) {
        hasManageableAsyncImageKey.value = true
        loaded.value = true
        return true
      }
      const pages = Number.isFinite(response.pages) && response.pages > 0 ? response.pages : 1
      if (page >= pages || (response.items || []).length === 0) {
        hasManageableAsyncImageKey.value = false
        loaded.value = true
        return false
      }
      page += 1
    }
  })()
    .catch(() => {
      hasManageableAsyncImageKey.value = false
      loaded.value = true
      return false
    })
    .finally(() => {
      loading.value = false
      pendingLoad = null
    })
  return pendingLoad
}

export function useAsyncImageAccess() {
  const authStore = useAuthStore()

  return {
    canUseAsyncImage: computed(() => canViewAsyncImage(authStore.isAdmin, hasManageableAsyncImageKey.value)),
    asyncImageAccessLoaded: computed(() => loaded.value),
    asyncImageAccessLoading: computed(() => loading.value),
    refreshAsyncImageAccess: loadAsyncImageAccess,
  }
}
