import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount, enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { useAppStore } from '@/stores/app'
import { getPublicSettings } from '@/api/auth'
import type { PublicSettings } from '@/types'
const flags = vi.hoisted(() => ({ admin: false }))
vi.mock('@/api/auth', () => ({ getPublicSettings: vi.fn() }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAdmin: flags.admin }) }))
vi.mock('../ChannelStatusV1View.vue', () => ({ default: defineComponent({ setup: () => () => h('div', { 'data-testid': 'v1' }) }) }))
vi.mock('../ChannelStatusV2View.vue', () => ({ default: defineComponent({ setup: () => () => h('div', { 'data-testid': 'v2' }) }) }))
vi.mock('../ChannelStatusV3View.vue', () => ({ default: defineComponent({ setup: () => () => h('div', { 'data-testid': 'v3' }) }) }))
import ChannelStatusView from '../ChannelStatusView.vue'
enableAutoUnmount(afterEach)

function settings(mode: 'v1' | 'v2' | 'v3', enabled = true) {
  return { channel_monitor_mode: mode, channel_monitor_enabled: enabled } as PublicSettings
}

describe('ChannelStatusView modes and user visibility', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    flags.admin = false
    vi.mocked(getPublicSettings).mockReset()
    delete window.__APP_CONFIG__
    useAppStore().cachedPublicSettings = settings('v1')
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
  })
  afterEach(() => { vi.restoreAllMocks(); delete window.__APP_CONFIG__ })
  it.each(['v1', 'v2', 'v3'] as const)('renders %s for ordinary users', (mode) => {
    useAppStore().cachedPublicSettings = settings(mode)
    expect(mount(ChannelStatusView).find(`[data-testid="${mode}"]`).exists()).toBe(true)
  })
  it.each(['v1', 'v2', 'v3'] as const)('renders %s for administrators', (mode) => {
    useAppStore().cachedPublicSettings = settings(mode); flags.admin = true
    expect(mount(ChannelStatusView).find(`[data-testid="${mode}"]`).exists()).toBe(true)
  })
  it('does not mount a monitor while disabled', () => {
    useAppStore().cachedPublicSettings = settings('v1', false)
    expect(mount(ChannelStatusView).find('[data-testid]').exists()).toBe(false)
  })

  it.each([
    ['v1', 'v2'], ['v1', 'v3'], ['v2', 'v1'],
    ['v2', 'v3'], ['v3', 'v1'], ['v3', 'v2'],
  ] as const)('replaces %s with saved %s when the existing tab regains focus', async (oldMode, savedMode) => {
    useAppStore().cachedPublicSettings = settings(oldMode)
    const wrapper = mount(ChannelStatusView)
    expect(wrapper.find(`[data-testid="${oldMode}"]`).exists()).toBe(true)
    vi.mocked(getPublicSettings).mockResolvedValue(settings(savedMode))
    window.dispatchEvent(new Event('focus'))
    await flushPromises()
    expect(getPublicSettings).toHaveBeenCalledOnce()
    expect(wrapper.findAll('[data-testid]')).toHaveLength(1)
    expect(wrapper.find(`[data-testid="${savedMode}"]`).exists()).toBe(true)
    expect(wrapper.find(`[data-testid="${oldMode}"]`).exists()).toBe(false)
  })

  it('synchronizes visibility changes, stops disabled monitors and removes listeners on unmount', async () => {
    const wrapper = mount(ChannelStatusView)
    vi.mocked(getPublicSettings).mockResolvedValue(settings('v3', false))
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(wrapper.find('[data-testid]').exists()).toBe(false)
    wrapper.unmount()
    window.dispatchEvent(new Event('focus'))
    document.dispatchEvent(new Event('visibilitychange'))
    expect(getPublicSettings).toHaveBeenCalledOnce()
  })

  it('does not refresh a hidden tab', () => {
    mount(ChannelStatusView)
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
    expect(getPublicSettings).not.toHaveBeenCalled()
  })
})
