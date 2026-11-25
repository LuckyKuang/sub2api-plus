import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useAdminSettingsStore, useAppStore, useAuthStore } from '@/stores'
import { resolvePortalArea, type PortalArea } from '@/utils/portalArea'

// Presentation area for the current route. Authentication always stays with the
// real login; this only selects admin console or personal/user chrome.
export function usePortalArea() {
  const route = useRoute()
  const authStore = useAuthStore()
  const appStore = useAppStore()
  const adminSettingsStore = useAdminSettingsStore()

  return computed<PortalArea>(() => resolvePortalArea({
    isAdmin: authStore.isAdmin,
    path: route.path ?? '',
    routeName: route.name as string | undefined,
    supportResource: route.meta?.adminSupportResource,
    portalArea: route.meta?.portalArea as PortalArea | undefined,
    customId: typeof route.params?.id === 'string' ? route.params.id : null,
    userCustomIds: (appStore.cachedPublicSettings?.custom_menu_items ?? [])
      .filter(item => item.visibility === 'user')
      .map(item => item.id),
    adminCustomIds: adminSettingsStore.customMenuItems
      .filter(item => item.visibility === 'admin')
      .map(item => item.id)
  }))
}
