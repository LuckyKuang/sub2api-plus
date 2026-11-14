import { computed, ref } from 'vue'
import { getImageTaskModes, type ImageTaskModes } from '@/api/imageTasks'

// Capability is owned by the backend switches; the console only renders what the
// server reports so a hidden menu can never stand in for authorization.
const modes = ref<ImageTaskModes | null>(null)
const loading = ref(false)
let pending: Promise<void> | null = null

async function refreshImageTaskModes(force = false): Promise<void> {
  if (!force && modes.value) return
  if (pending && !force) return pending
  loading.value = true
  pending = getImageTaskModes()
    .then((value) => {
      modes.value = value
    })
    .catch(() => {
      modes.value = null
    })
    .finally(() => {
      loading.value = false
      pending = null
    })
  return pending
}

export function useImageTaskModes() {
  return {
    modes,
    loading,
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
