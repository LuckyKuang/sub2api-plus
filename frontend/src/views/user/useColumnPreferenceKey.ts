import { useAuthStore } from '@/stores/auth'
import { getAdminSupportContext } from '@/utils/adminSupportContext'

/** Migrate legacy display preferences once; never store row data. */
export function useColumnPreferenceKey(base: string): string {
  const userId = useAuthStore.getState().user?.id ?? 'anonymous'
  const key = `${base}:${userId}:${getAdminSupportContext()?.userId ?? 'self'}`
  try {
    if (localStorage.getItem(key) == null) {
      const legacy = localStorage.getItem(base)
      if (legacy != null && Array.isArray(JSON.parse(legacy))) localStorage.setItem(key, legacy)
    }
  } catch {
    /* Restricted storage must not prevent reading usage. */
  }
  return key
}
