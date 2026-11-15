import { computed, ref, watch } from 'vue'
import { getImageTaskModes, type ImageTaskModes } from '@/api/imageTasks'
import { useAuthStore } from '@/stores/auth'
import { adminSupportContext } from '@/utils/adminSupportContext'

// Capability is owned by the backend switches; the console only renders what the
// server reports so a hidden menu can never stand in for authorization.
const modes = ref<ImageTaskModes | null>(null)
const loading = ref(false)
let pending: Promise<void> | null = null
// Navigation re-reads the capability so an administrator's switch change shows
// up without a full reload, but a short cache keeps ordinary page changes cheap.
const CACHE_TTL_MS = 30_000
let loadedAt = 0
const scopeKey = ref('')
let generation = 0
let authenticated = false

async function refreshImageTaskModes(force = false): Promise<void> {
  if (!authenticated) return
  if (!force && modes.value && Date.now() - loadedAt < CACHE_TTL_MS) return
  if (pending) return pending
  const requestGeneration = generation
  loading.value = true
  pending = getImageTaskModes()
    .then((value) => {
      if (requestGeneration !== generation) return
      modes.value = value
      loadedAt = Date.now()
    })
    .catch(() => {
      if (requestGeneration !== generation) return
      modes.value = null
    })
    .finally(() => {
      if (requestGeneration !== generation) return
      loading.value = false
      pending = null
    })
  return pending
}

export function useImageTaskModes() {
  const auth = useAuthStore()
  watch(() => JSON.stringify([auth.isAuthenticated ? auth.user?.id : null, adminSupportContext.value]), (next) => {
    authenticated = auth.isAuthenticated
    if (scopeKey.value === next) return
    generation++
    scopeKey.value = next
    modes.value = null
    pending = null
    loadedAt = 0
    loading.value = false
    if (auth.isAuthenticated) void refreshImageTaskModes()
  }, { immediate: true, flush: 'sync' })
  return {
    modes,
    loading,
    scopeKey,
    // Panels, cards and the sidebar follow can_view: history stays reachable
    // after an administrator switches new submissions off.
    ordinaryAvailable: computed(() => modes.value?.ordinary.can_view === true),
    batchAvailable: computed(() => modes.value?.batch.can_view === true),
    anyAvailable: computed(() => modes.value?.ordinary.can_view === true || modes.value?.batch.can_view === true),
    ordinarySubmittable: computed(() => modes.value?.ordinary.can_submit === true),
    batchSubmittable: computed(() => modes.value?.batch.can_submit === true),
    refreshImageTaskModes,
  }
}
