import { computed, ref } from 'vue'
import { keysAPI } from '@/api/keys'
import { useAuthStore } from '@/stores/auth'
import type { ApiKey } from '@/types'

const loaded = ref(false)
const loading = ref(false)
const hasAllowedAsyncImageKey = ref(false)
let pendingLoad: Promise<boolean> | null = null
const pageSize = 100

export function keyAllowsAsyncImage(key: ApiKey): boolean {
  return key.status === 'active'
    && (key.group?.platform === 'openai' || key.group?.platform === 'grok')
    && key.group?.allow_image_generation === true
}

// Administrators need the entry even when they have not created a personal API
// key, so they can verify the feature and manage the group-level permission.
// Users remain gated by an active key whose group explicitly allows images.
export function canViewAsyncImage(isAdmin: boolean, hasAllowedKey: boolean): boolean {
  return isAdmin || hasAllowedKey
}

async function loadAsyncImageAccess(force = false): Promise<boolean> {
  const authStore = useAuthStore()
  if (!authStore.isAuthenticated) {
    loaded.value = true
    hasAllowedAsyncImageKey.value = false
    return false
  }
  if (loaded.value && !force) return hasAllowedAsyncImageKey.value
  if (pendingLoad && !force) return pendingLoad

  loading.value = true
  pendingLoad = (async () => {
    let page = 1
    while (true) {
      const response = await keysAPI.list(page, pageSize, { status: 'active', sort_by: 'created_at', sort_order: 'desc' })
      if ((response.items || []).some(keyAllowsAsyncImage)) {
        hasAllowedAsyncImageKey.value = true
        loaded.value = true
        return true
      }
      if (page >= response.pages || (response.items || []).length === 0) {
        hasAllowedAsyncImageKey.value = false
        loaded.value = true
        return false
      }
      page += 1
    }
  })()
    .catch(() => {
      hasAllowedAsyncImageKey.value = false
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
    canUseAsyncImage: computed(() => canViewAsyncImage(authStore.isAdmin, hasAllowedAsyncImageKey.value)),
    asyncImageAccessLoaded: computed(() => loaded.value),
    asyncImageAccessLoading: computed(() => loading.value),
    refreshAsyncImageAccess: loadAsyncImageAccess,
  }
}
