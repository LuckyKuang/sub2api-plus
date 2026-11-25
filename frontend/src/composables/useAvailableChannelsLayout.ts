import { ref } from 'vue'

export type AvailableChannelsLayout = 'list' | 'card'

interface SessionLayout {
  mode: AvailableChannelsLayout
  adminDefault: AvailableChannelsLayout
}

// The user's choice lives for the browser session (module scope), not just the
// mounted component. A stored choice is ignored once the administrator default
// changes, so a later default update takes effect on the next visit.
const sessionLayout = ref<SessionLayout | null>(null)

export function resetAvailableChannelsLayoutSession(): void {
  sessionLayout.value = null
}

export function useAvailableChannelsLayoutSession() {
  return {
    sessionLayout,
    choose(mode: AvailableChannelsLayout, adminDefault: AvailableChannelsLayout) {
      sessionLayout.value = { mode, adminDefault }
    },
    reset() {
      sessionLayout.value = null
    },
  }
}
