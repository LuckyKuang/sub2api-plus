import { useAuthStore } from '@/stores/auth'
import { adminSupportContext } from '@/utils/adminSupportContext'

/** Migrate legacy display preferences once; never store row data. */
export function useColumnPreferenceKey(base: string): string {
  const auth = useAuthStore()
  const key = `${base}:${auth.user?.id ?? 'anonymous'}:${adminSupportContext.value?.userId ?? 'self'}`
  try {
    if (localStorage.getItem(key) == null) {
      const legacy = localStorage.getItem(base)
      if (legacy != null && Array.isArray(JSON.parse(legacy))) localStorage.setItem(key, legacy)
    }
  } catch { /* Restricted storage must not prevent reading usage. */ }
  return key
}
