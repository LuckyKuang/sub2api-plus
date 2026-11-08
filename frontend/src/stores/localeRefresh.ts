/**
 * Locale refresh registration for stores.
 *
 * Stores are module singletons created once outside React's component tree, so they cannot use
 * the `useLocaleRefresh` hook from `@/utils/localeRefresh` (its contract is a per-component
 * subscription with a disposer). They register once for the application lifetime instead, which
 * matches the Vue stores that subscribed while the Pinia instance was alive.
 */

import { useLocaleRefresh as registerLocaleRefresh } from '@/utils/localeRefresh'

/** Refresh translated server data without remounting forms or replaying writes. */
export function onLocaleRefresh(refresh: () => void | Promise<unknown>): void {
  registerLocaleRefresh(refresh)
}
