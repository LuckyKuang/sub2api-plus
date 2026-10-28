import { onScopeDispose, ref } from 'vue'
import { getKeyQuotes, type CatalogGroup } from '@/api/channels'

/** An authorized backend quote is the only source of customer selling rates. */
export function useGroupRateQuotes() {
  const groups = ref<CatalogGroup[]>([])
  const loading = ref(false)
  const failed = ref(false)
  let request = 0
  let controller: AbortController | undefined
  async function refresh() {
    const current = ++request
    controller?.abort()
    controller = new AbortController()
    groups.value = []
    loading.value = true
    failed.value = false
    try {
      const catalog = await getKeyQuotes({ signal: controller.signal })
      if (current !== request) return
      if (catalog.user_rate_status === 'unavailable') failed.value = true
      else groups.value = catalog.groups
    } catch {
      if (current === request) failed.value = true
    } finally {
      if (current === request) loading.value = false
    }
  }
  onScopeDispose(() => { request++; controller?.abort() })
  return { groups, loading, failed, refresh }
}
