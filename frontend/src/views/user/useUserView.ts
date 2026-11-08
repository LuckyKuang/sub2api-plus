import { useAuthStore } from '@/stores/auth'
import { useAdminSupportViewStore } from '@/stores/adminSupportView'
import { getAdminSupportContext } from '@/utils/adminSupportContext'
import type { User } from '@/types'

/**
 * Presentation identity for end-user screens. The authentication store and its
 * persisted session always retain the real administrator; in assistance mode
 * the screens show the target user instead.
 */
export function useUserView() {
  const auth = useAuthStore()

  return {
    get user(): User | null {
      const context = getAdminSupportContext()
      if (!context) return auth.user
      const target = useAdminSupportViewStore.getState().target
      return target?.id === context.userId ? target : null
    },
    get isAdmin(): boolean {
      const context = getAdminSupportContext()
      if (!context) return auth.isAdmin()
      return useAdminSupportViewStore.getState().target?.role === 'admin'
    },
    get isAuthenticated(): boolean {
      return auth.isAuthenticated()
    },
    get isSimpleMode(): boolean {
      return auth.isSimpleMode()
    },
    async refreshUser(): Promise<User> {
      const context = getAdminSupportContext()
      if (!context) return auth.refreshUser()
      return useAdminSupportViewStore.getState().loadTarget(context.userId)
    }
  }
}
