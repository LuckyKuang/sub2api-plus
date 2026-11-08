import { accountsAPI } from '@/api/admin/accounts'

// Keep the map local to one details refresh: account renames, deletion and
// session changes must not reuse a stale module-wide cache.
const ACCOUNT_NAME_PAGE_SIZE = 1000

export async function getAccountNameMap(signal?: AbortSignal): Promise<Map<number, string>> {
  const map = new Map<number, string>()
  let page = 1
  let total = Number.POSITIVE_INFINITY
  while (map.size < total) {
    const res = await accountsAPI.list(page, ACCOUNT_NAME_PAGE_SIZE, {
      lite: '1',
      include_scheduler_score: '0'
    }, { signal })
    total = res.total || 0
    const items = res.items || []
    if (items.length === 0) break
    for (const item of items) map.set(item.id, item.name)
    page++
  }

  return map
}
