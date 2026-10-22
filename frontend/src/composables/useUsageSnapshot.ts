/** Keep newly inserted records out of subsequent pages until an explicit refresh. */
export function useUsageSnapshot() {
  let boundary: number | undefined
  let filterKey = ''
  let latestHead: number | undefined
  return {
    forFilters(filters: unknown): number | 'new' {
      const key = JSON.stringify(filters)
      if (key !== filterKey) { boundary = undefined; latestHead = undefined; filterKey = key }
      return boundary ?? 'new'
    },
    accept(data: { snapshot_max_id?: number; items: { id: number }[] }, isLatest: boolean) {
      if (data.snapshot_max_id != null) boundary = data.snapshot_max_id
      if (isLatest) latestHead = data.items[0]?.id
    },
    observe(head: number | undefined) {
      const changed = latestHead != null && head != null && head !== latestHead
      latestHead = head
      return changed
    },
    reset() { boundary = undefined; latestHead = undefined },
  }
}
