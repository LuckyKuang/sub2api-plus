import { getAdminSupportContext, supportRequestGeneration } from '@/utils/adminSupportContext'

/**
 * Cross-store reaction helper for administrator support ("assistance") context changes.
 *
 * The Vue stores watched a reactive `adminSupportContext` ref. The React
 * `@/utils/adminSupportContext` module is a plain singleton that exposes only a generation
 * counter, so stores registered here observe the change by comparing generations at the entry
 * of their public actions: `setAdminSupportContext()` bumps the generation, and the next store
 * action runs the pending reactions before doing anything else.
 */

type SupportContextListener = () => void

const listeners = new Set<SupportContextListener>()
let observedGeneration = supportRequestGeneration()

/** Run pending support-context reactions when the shared generation advanced. */
export function syncSupportContext(): void {
  const generation = supportRequestGeneration()
  if (generation === observedGeneration) {
    return
  }

  observedGeneration = generation
  for (const listener of [...listeners]) {
    listener()
  }
}

/** Register a reaction invoked when the administrator support context changes. */
export function onSupportContextChange(listener: SupportContextListener): void {
  listeners.add(listener)
}

/** Current administrator support context, or null outside an assistance view. */
export function currentSupportContext(): ReturnType<typeof getAdminSupportContext> {
  return getAdminSupportContext()
}
