import { afterEach, describe, expect, it, vi } from 'vitest'
import type { User } from '@/types'
import { setAdminSupportContext } from '@/utils/adminSupportContext'

const { auth, support } = vi.hoisted(() => ({
  auth: { user: { id: 1, role: 'admin', balance: 99 } as User, isAdmin: true, isAuthenticated: true, isSimpleMode: false, token: 'real-admin-session', refreshUser: vi.fn() },
  support: { target: { id: 42, role: 'user', balance: 12 } as User, loadTarget: vi.fn() }
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/adminSupportView', () => ({ useAdminSupportViewStore: () => support }))
import { useUserView } from '../useUserView'

afterEach(() => setAdminSupportContext(null))
describe('user presentation identity', () => {
  it('uses target balances and role without replacing the administrator session', () => {
    setAdminSupportContext({ actorId: 1, userId: 42 })
    const view = useUserView()
    expect(view.user).toBe(support.target)
    expect(view.isAdmin).toBe(false)
    expect(view.token).toBeNull()
    view.user = { id: 43 } as User
    expect(auth.user.id).toBe(1)
    expect(auth.token).toBe('real-admin-session')
    setAdminSupportContext({ actorId: 1, userId: 43 })
    expect(view.user).toBeNull()
    setAdminSupportContext(null)
    expect(view.user).toBe(auth.user)
    expect(view.token).toBe('real-admin-session')
  })
})
