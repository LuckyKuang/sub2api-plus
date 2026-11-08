import { useMemo, useRef } from 'react'

interface UsageSnapshotData {
  snapshot_max_id?: number
  items: Array<{ id: number }>
}

/** Keep newly inserted records out of subsequent pages until an explicit refresh. */
export function useUsageSnapshot() {
  const boundary = useRef<number | undefined>(undefined)
  const filterKey = useRef('')
  const latestHead = useRef<number | undefined>(undefined)

  return useMemo(
    () => ({
      forFilters(filters: unknown): number | 'new' {
        const key = JSON.stringify(filters)
        if (key !== filterKey.current) {
          boundary.current = undefined
          latestHead.current = undefined
          filterKey.current = key
        }
        return boundary.current ?? 'new'
      },
      accept(data: UsageSnapshotData, isLatest: boolean) {
        if (data.snapshot_max_id != null) boundary.current = data.snapshot_max_id
        if (isLatest) latestHead.current = data.items[0]?.id
      },
      observe(head: number | undefined) {
        const changed = latestHead.current != null && head != null && head !== latestHead.current
        latestHead.current = head
        return changed
      },
      reset() {
        boundary.current = undefined
        latestHead.current = undefined
      }
    }),
    []
  )
}

const PAGE_SIZE_STORAGE_KEY = 'table-page-size'

/** Read the persisted table page size (mirrors the Vue composable). */
export function getPersistedPageSize(fallback = 20): number {
  if (typeof window !== 'undefined') {
    try {
      const stored = window.localStorage.getItem(PAGE_SIZE_STORAGE_KEY)
      if (stored !== null) {
        const parsed = Number(stored)
        if (Number.isFinite(parsed)) return parsed
      }
    } catch (error) {
      console.warn('Failed to read persisted page size:', error)
    }
  }
  return fallback
}

/** Persist the table page size. */
export function setPersistedPageSize(size: number): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(PAGE_SIZE_STORAGE_KEY, String(size))
  } catch (error) {
    console.warn('Failed to persist page size:', error)
  }
}
