import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ChannelCatalog } from '@/api/channels'
import { cacheAvailableChannelCatalog, clearAvailableChannelCatalogCache, readAvailableChannelCatalog } from '../availableChannelCatalogCache'

const success = (): ChannelCatalog => ({ groups: [], user_rate_status: 'loaded' })

beforeEach(clearAvailableChannelCatalogCache)

describe('available channel catalog memory limit', () => {
  it('retains at most four scopes and evicts the least recently viewed catalog', () => {
    for (let user = 1; user <= 4; user++) cacheAvailableChannelCatalog(`actor-1-target-${user}-en`, success())
    expect(readAvailableChannelCatalog('actor-1-target-1-en')).toBeDefined()

    cacheAvailableChannelCatalog('actor-1-target-5-en', success())

    expect(readAvailableChannelCatalog('actor-1-target-2-en')).toBeUndefined()
    for (const user of [1, 3, 4, 5]) expect(readAvailableChannelCatalog(`actor-1-target-${user}-en`)).toBeDefined()
  })

  it('never persists authorized responses in browser storage', () => {
    const storageWrite = vi.spyOn(Storage.prototype, 'setItem')
    try {
      cacheAvailableChannelCatalog('actor-1-self-en', success())
      expect(readAvailableChannelCatalog('actor-1-self-en')).toBeDefined()
      clearAvailableChannelCatalogCache()
      expect(storageWrite).not.toHaveBeenCalled()
    } finally { storageWrite.mockRestore() }
  })
})
