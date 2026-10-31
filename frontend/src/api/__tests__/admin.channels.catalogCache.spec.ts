import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { create, update, remove } from '@/api/admin/channels'
import {
  availableChannelCatalogGeneration,
  cacheAvailableChannelCatalog,
  clearAvailableChannelCatalogCache,
  readAvailableChannelCatalog
} from '@/utils/availableChannelCatalogCache'

const client = vi.hoisted(() => ({ post: vi.fn(), put: vi.fn(), delete: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: client }))

const mutations = [
  { name: 'create', call: () => create({ name: 'New channel' }), request: client.post },
  { name: 'update pricing', call: () => update(7, { model_pricing: [] }), request: client.put },
  { name: 'delete', call: () => remove(7), request: client.delete }
]

beforeEach(() => {
  vi.resetAllMocks()
  clearAvailableChannelCatalogCache()
  for (const key of ['own-en', 'assisted-zh']) {
    cacheAvailableChannelCatalog(key, { groups: [], user_rate_status: 'loaded' })
  }
})
afterEach(clearAvailableChannelCatalogCache)

describe('channel changes invalidate available-model catalogs', () => {
  // A successful admin save must discard the previous model list in every
  // cached scope, including assisted views. Failed saves change no catalog.
  it.each(mutations)('discards cached catalogs only after successful $name', async ({ call, request }) => {
    let complete!: (value: unknown) => void
    request.mockImplementationOnce(() => new Promise(resolve => { complete = resolve }))
    const generation = availableChannelCatalogGeneration()
    const pending = call()
    expect(readAvailableChannelCatalog('own-en')).toBeDefined()

    complete({ data: { id: 7 } })
    await pending

    expect(readAvailableChannelCatalog('own-en')).toBeUndefined()
    expect(readAvailableChannelCatalog('assisted-zh')).toBeUndefined()
    expect(availableChannelCatalogGeneration()).toBe(generation + 1)
  })

  it.each(mutations)('retains catalogs after failed $name', async ({ call, request }) => {
    request.mockRejectedValueOnce(new Error('save failed'))
    const generation = availableChannelCatalogGeneration()

    await expect(call()).rejects.toThrow('save failed')

    expect(readAvailableChannelCatalog('own-en')).toBeDefined()
    expect(readAvailableChannelCatalog('assisted-zh')).toBeDefined()
    expect(availableChannelCatalogGeneration()).toBe(generation)
  })
})
