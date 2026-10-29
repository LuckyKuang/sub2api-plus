import type { ChannelCatalog } from '@/api/channels'
import { shallowRef } from 'vue'

const CACHE_TTL_MS = 180_000
const MAX_CACHED_CATALOGS = 4
const catalogs = new Map<string, { catalog: ChannelCatalog; expiresAt: number }>()
const sessionGeneration = shallowRef(0)

export function availableChannelCatalogGeneration(): number {
  return sessionGeneration.value
}

function discardExpired(): void {
  const now = Date.now()
  for (const [key, entry] of catalogs) {
    if (now >= entry.expiresAt) catalogs.delete(key)
  }
}

// These authorized read models live only in memory. The page supplies a key
// scoped to the signed-in actor, assistance target (if any), and API locale.
export function readAvailableChannelCatalog(key: string): ChannelCatalog | undefined {
  discardExpired()
  const entry = catalogs.get(key)
  if (entry) {
    catalogs.delete(key)
    catalogs.set(key, entry)
  }
  return entry?.catalog
}

export function cacheAvailableChannelCatalog(key: string, catalog: ChannelCatalog): void {
  discardExpired()
  catalogs.delete(key)
  if (catalog.user_rate_status === 'loaded') {
    catalogs.set(key, { catalog, expiresAt: Date.now() + CACHE_TTL_MS })
    if (catalogs.size > MAX_CACHED_CATALOGS) {
      const oldest = catalogs.keys().next().value
      if (oldest !== undefined) catalogs.delete(oldest)
    }
  }
}

export function invalidateAvailableChannelCatalog(key: string): void {
  catalogs.delete(key)
}

export function clearAvailableChannelCatalogCache(): void {
  catalogs.clear()
  sessionGeneration.value += 1
}
