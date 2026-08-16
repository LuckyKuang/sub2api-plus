import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick } from 'vue'

import VersionBadge from '../VersionBadge.vue'
import { useAppStore, useAuthStore } from '@/stores'

const {
  checkUpdates,
  performUpdate,
  restartService,
  getRollbackVersions,
  rollback,
} = vi.hoisted(() => ({
  checkUpdates: vi.fn(),
  performUpdate: vi.fn(),
  restartService: vi.fn(),
  getRollbackVersions: vi.fn(),
  rollback: vi.fn(),
}))

vi.mock('@/api/admin/system', () => ({
  checkUpdates,
  performUpdate,
  restartService,
  getRollbackVersions,
  rollback,
  default: {
    checkUpdates,
    performUpdate,
    restartService,
    getRollbackVersions,
    rollback,
  },
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const IconStub = defineComponent({
  props: { name: { type: String, default: '' } },
  template: '<span class="icon" />',
})

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../VersionBadge.vue')
const componentSource = readFileSync(componentPath, 'utf8')

function mountBadge() {
  return mount(VersionBadge, {
    global: {
      plugins: [createPinia()],
      stubs: { Icon: IconStub, transition: false },
    },
  })
}

describe('VersionBadge dropdown width', () => {
  it('uses a fixed 20rem width for long custom version strings', () => {
    expect(componentSource).toContain('mt-2 w-80 overflow-hidden')
    expect(componentSource).not.toContain("'w-64'")
  })

  it('does not render a static version for non-admins', () => {
    expect(componentSource).not.toContain('Non-admin: Simple static version text')
    expect(componentSource).not.toContain('v-else-if="version"')
    expect(componentSource).toContain('v-if="isAdmin"')
  })
})

describe('VersionBadge admin-only rendering', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    checkUpdates.mockReset()
    checkUpdates.mockResolvedValue({
      current_version: '0.1.177',
      latest_version: '0.1.177',
      has_update: false,
      build_type: 'release',
      cached: false,
    })
  })

  it('renders nothing and does not fetch updates for a regular user', async () => {
    const wrapper = mountBadge()
    const authStore = useAuthStore()
    const appStore = useAppStore()
    authStore.user = {
      id: 1,
      username: 'user',
      email: 'user@example.com',
      role: 'user',
      balance: 0,
      concurrency: 1,
      status: 'active',
      allowed_groups: null,
      created_at: '2026-01-01',
      updated_at: '2026-01-01',
    } as never
    appStore.siteVersion = '9.9.9'
    await nextTick()
    await flushPromises()

    expect(wrapper.text()).not.toContain('v9.9.9')
    expect(wrapper.find('button').exists()).toBe(false)
    expect(checkUpdates).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('fetches and shows the admin version badge only after the user is an admin', async () => {
    const wrapper = mountBadge()
    const authStore = useAuthStore()
    await nextTick()
    expect(wrapper.find('button').exists()).toBe(false)

    authStore.user = {
      id: 2,
      username: 'admin',
      email: 'admin@example.com',
      role: 'admin',
      balance: 0,
      concurrency: 1,
      status: 'active',
      allowed_groups: null,
      created_at: '2026-01-01',
      updated_at: '2026-01-01',
    } as never
    await flushPromises()

    expect(checkUpdates).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('v0.1.177')
    expect(wrapper.find('button').exists()).toBe(true)
    wrapper.unmount()
  })

  it('removes the badge and clears cached version state when the user loses admin', async () => {
    const wrapper = mountBadge()
    const authStore = useAuthStore()
    const appStore = useAppStore()
    authStore.user = {
      id: 2,
      username: 'admin',
      email: 'admin@example.com',
      role: 'admin',
      balance: 0,
      concurrency: 1,
      status: 'active',
      allowed_groups: null,
      created_at: '2026-01-01',
      updated_at: '2026-01-01',
    } as never
    await flushPromises()
    expect(wrapper.find('button').exists()).toBe(true)
    expect(appStore.currentVersion).toBe('0.1.177')

    authStore.user = {
      id: 2,
      username: 'admin',
      email: 'admin@example.com',
      role: 'user',
      balance: 0,
      concurrency: 1,
      status: 'active',
      allowed_groups: null,
      created_at: '2026-01-01',
      updated_at: '2026-01-01',
    } as never
    await nextTick()

    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('v0.1.177')
    expect(appStore.currentVersion).toBe('')
    expect(appStore.versionLoaded).toBe(false)
    wrapper.unmount()
  })

  it('does not call update or restart APIs after losing admin', async () => {
    const wrapper = mountBadge()
    const authStore = useAuthStore()
    authStore.user = {
      id: 2,
      username: 'admin',
      email: 'admin@example.com',
      role: 'admin',
      balance: 0,
      concurrency: 1,
      status: 'active',
      allowed_groups: null,
      created_at: '2026-01-01',
      updated_at: '2026-01-01',
    } as never
    await flushPromises()

    const vm = wrapper.vm as unknown as {
      handleUpdate: () => Promise<void>
      handleRestart: () => Promise<void>
    }
    authStore.user = {
      id: 2,
      username: 'admin',
      email: 'admin@example.com',
      role: 'user',
      balance: 0,
      concurrency: 1,
      status: 'active',
      allowed_groups: null,
      created_at: '2026-01-01',
      updated_at: '2026-01-01',
    } as never
    await nextTick()

    await vm.handleUpdate()
    await vm.handleRestart()

    expect(performUpdate).not.toHaveBeenCalled()
    expect(restartService).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
