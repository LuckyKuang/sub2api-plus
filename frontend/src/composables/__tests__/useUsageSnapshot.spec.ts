import { describe, expect, it } from 'vitest'
import { useUsageSnapshot } from '../useUsageSnapshot'

describe('usage history insertion boundary', () => {
  it('keeps later pages stable until filters change or the reader refreshes', () => {
    const snapshot = useUsageSnapshot()
    expect(snapshot.forFilters({ model: 'a' })).toBe('new')
    snapshot.accept({ snapshot_max_id: 40, items: [{ id: 35 }] }, true)
    expect(snapshot.forFilters({ model: 'a' })).toBe(40)
    // Page two is older; its first row must not become the newest-record marker.
    snapshot.accept({ snapshot_max_id: 40, items: [{ id: 20 }] }, false)
    expect(snapshot.observe(35)).toBe(false)
    expect(snapshot.observe(41)).toBe(true)
    expect(snapshot.forFilters({ model: 'a' })).toBe(40)
    expect(snapshot.forFilters({ model: 'b' })).toBe('new')
    snapshot.accept({ snapshot_max_id: 80, items: [] }, false)
    snapshot.reset()
    expect(snapshot.forFilters({ model: 'b' })).toBe('new')
  })
})
