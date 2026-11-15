import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, reactive, type EffectScope } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { useImageTaskModes } from '../useImageTaskModes'
import { adminSupportContext } from '@/utils/adminSupportContext'
import type { ImageTaskModes } from '@/api/imageTasks'

const { getModes } = vi.hoisted(() => ({ getModes: vi.fn() }))
const auth = reactive({ isAuthenticated: true, user: { id: 1 } })
vi.mock('@/api/imageTasks', () => ({ getImageTaskModes: getModes }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))

const allowed: ImageTaskModes = {
  ordinary: { enabled: true, can_view: true, can_submit: true },
  batch: { enabled: true, can_view: true, can_submit: true },
}
const denied: ImageTaskModes = {
  ordinary: { enabled: false, can_view: false, can_submit: false, reason: 'disabled' },
  batch: { enabled: false, can_view: false, can_submit: false, reason: 'disabled' },
}

let scope: EffectScope
function access() {
  scope = effectScope()
  return scope.run(() => useImageTaskModes())!
}

// R3/R4 require capabilities and history to follow the authenticated identity.
describe('image mode capability identity isolation', () => {
  beforeEach(() => {
    getModes.mockReset()
    auth.isAuthenticated = true
    auth.user = { id: auth.user.id + 1 }
    adminSupportContext.value = null
  })
  afterEach(() => scope?.stop())

  it('rejects a late response from the previous signed-in user', async () => {
    let resolvePrevious!: (value: ImageTaskModes) => void
    getModes.mockReturnValueOnce(new Promise<ImageTaskModes>(resolve => { resolvePrevious = resolve }))
      .mockResolvedValueOnce(denied)
    const result = access()
    auth.user = { id: auth.user.id + 1 }
    expect(result.modes.value).toBeNull()
    await flushPromises()
    expect(result.modes.value).toEqual(denied)
    resolvePrevious(allowed)
    await flushPromises()
    expect(result.modes.value).toEqual(denied)
  })

  it('clears cached capabilities on logout and makes no anonymous request', async () => {
    getModes.mockResolvedValue(allowed)
    const result = access()
    await flushPromises()
    expect(result.anyAvailable.value).toBe(true)
    auth.isAuthenticated = false
    expect(result.modes.value).toBeNull()
    await result.refreshImageTaskModes(true)
    expect(getModes).toHaveBeenCalledTimes(1)
  })

  it('clears the previous scope when entering an administrator assistance view', async () => {
    getModes.mockResolvedValueOnce(allowed).mockResolvedValueOnce(denied)
    const result = access()
    await flushPromises()
    const originalScope = result.scopeKey.value
    adminSupportContext.value = { userId: 789, actorId: auth.user.id }
    expect(result.modes.value).toBeNull()
    expect(result.scopeKey.value).not.toBe(originalScope)
    await flushPromises()
    expect(result.anyAvailable.value).toBe(false)
  })
})
