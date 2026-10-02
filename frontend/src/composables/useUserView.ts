import { useAuthStore } from '@/stores/auth'
import { useAdminSupportViewStore } from '@/stores/adminSupportView'
import { adminSupportContext } from '@/utils/adminSupportContext'
import type { User } from '@/types'

// Presentation identity only. The authentication store and its persisted
// session always retain the real administrator.
export function useUserView() {
  const auth = useAuthStore()
  const support = () => useAdminSupportViewStore()
  return {
    get user(): User | null {
      const context = adminSupportContext.value
      return context ? support().target?.id === context.userId ? support().target : null : auth.user
    },
    set user(value: User | null) {
      if (!adminSupportContext.value) auth.user = value
    },
    get isAdmin() { return adminSupportContext.value ? support().target?.role === 'admin' : auth.isAdmin },
    get isAuthenticated() { return auth.isAuthenticated },
    get isSimpleMode() { return auth.isSimpleMode },
    // Embedded external pages must not receive the administrator's JWT.
    get token() { return adminSupportContext.value ? null : auth.token },
    async refreshUser(): Promise<User> {
      const context = adminSupportContext.value
      return context ? support().loadTarget(context.userId) : auth.refreshUser()
    }
  }
}
