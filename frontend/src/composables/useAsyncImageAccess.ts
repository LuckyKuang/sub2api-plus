import { keysAPI } from '@/api/keys'
import { useAuthStore } from '@/stores/auth'
import { supportRequestGeneration } from '@/utils/adminSupportContext'
import type { ApiKey } from '@/types'

export function keyCanManageAsyncImage(key: ApiKey): boolean {
  return key.status === 'active' || key.status === 'quota_exhausted' || key.status === 'expired'
}

// Administrators need the entry even when they have not created a personal API
// key, so they can verify the feature and manage the group-level permission.
// Any non-disabled user key may own history after its status, group, or platform
// changes, so management access cannot be gated on current submission support.
export function canViewAsyncImage(isAdmin: boolean, hasManagementKey: boolean): boolean {
  return isAdmin || hasManagementKey
}

export async function loadAsyncImageAccess(): Promise<boolean> {
  const authStore = useAuthStore.getState()
  if (!authStore.isAuthenticated()) {
    return false
  }

  const scope = supportRequestGeneration()
  const pageSize = 100
  let page = 1
  while (true) {
    const response = await keysAPI.list(page, pageSize, { sort_by: 'created_at', sort_order: 'desc' })
    if (scope !== supportRequestGeneration()) return false
    if ((response.items || []).some(keyCanManageAsyncImage)) return true
    const pages = Number.isFinite(response.pages) && response.pages > 0 ? response.pages : 1
    if (page >= pages || (response.items || []).length === 0) return false
    page += 1
  }
}

/** Refresh the auth store's cached image-management capability. */
export async function refreshAsyncImageAccess(): Promise<boolean> {
  const canManage = await loadAsyncImageAccess()
  useAuthStore.setState({ hasManageableImageKey: canManage })
  return canManage
}
