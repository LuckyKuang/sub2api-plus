import { describe, expect, it, vi } from 'vitest'
import { effectScope } from 'vue'
import { useGroupRateQuotes } from '../useGroupRateQuotes'
import { getKeyQuotes } from '@/api/channels'
import { group } from '@/components/channels/__tests__/catalogFixtures'

vi.mock('@/api/channels', () => ({ getKeyQuotes: vi.fn() }))

describe('authorized group rate quotes', () => {
  it('ignores late results and clears stale values while refreshing or after failure', async () => {
    let first!: (value: Awaited<ReturnType<typeof getKeyQuotes>>) => void
    vi.mocked(getKeyQuotes).mockImplementationOnce(() => new Promise(resolve => { first = resolve }))
    vi.mocked(getKeyQuotes).mockResolvedValueOnce({ groups: [group(2)], user_rate_status: 'loaded' })
    const scope = effectScope()
    const state = scope.run(() => useGroupRateQuotes())!
    const old = state.refresh()
    await state.refresh()
    first({ groups: [group(1)], user_rate_status: 'loaded' })
    await old
    expect(state.groups.value.map(g => g.id)).toEqual([2])
    vi.mocked(getKeyQuotes).mockRejectedValueOnce(new Error('network unavailable'))
    await state.refresh()
    expect(state.groups.value).toEqual([])
    expect(state.failed.value).toBe(true)
    scope.stop()
  })
  it('discards all quotes if personal rate resolution is unavailable', async () => {
    vi.mocked(getKeyQuotes).mockResolvedValueOnce({ groups: [group()], user_rate_status: 'unavailable' })
    const scope = effectScope()
    const state = scope.run(() => useGroupRateQuotes())!
    await state.refresh()
    expect(state.groups.value).toEqual([])
    expect(state.failed.value).toBe(true)
    scope.stop()
  })
})
