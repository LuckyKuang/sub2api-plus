import { describe, expect, it, vi, beforeEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
const flags = vi.hoisted(() => ({ mode: 'v1', enabled: true, admin: false }))
vi.mock('@/utils/featureFlags', () => ({ getChannelMonitorMode: () => flags.mode, isChannelMonitorRouteEnabled: () => flags.enabled }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAdmin: flags.admin }) }))
vi.mock('../ChannelStatusV1View.vue', () => ({ default: defineComponent({ setup: () => () => h('div', { 'data-testid': 'v1' }) }) }))
vi.mock('../ChannelStatusV2View.vue', () => ({ default: defineComponent({ setup: () => () => h('div', { 'data-testid': 'v2' }) }) }))
vi.mock('../ChannelStatusV3View.vue', () => ({ default: defineComponent({ setup: () => () => h('div', { 'data-testid': 'v3' }) }) }))
import ChannelStatusView from '../ChannelStatusView.vue'
describe('ChannelStatusView modes and privacy', () => {
  beforeEach(() => { flags.mode = 'v1'; flags.enabled = true; flags.admin = false })
  it.each(['v1', 'v3'])('renders %s for users', (mode) => {
    flags.mode = mode
    expect(mount(ChannelStatusView).find(`[data-testid="${mode}"]`).exists()).toBe(true)
  })
  it('never mounts private V2 for ordinary users', () => {
    flags.mode = 'v2'
    expect(mount(ChannelStatusView).find('[data-testid]').exists()).toBe(false)
  })
  it('allows administrators to view V2', () => {
    flags.mode = 'v2'; flags.admin = true
    expect(mount(ChannelStatusView).find('[data-testid="v2"]').exists()).toBe(true)
  })
  it('does not mount a monitor while disabled', () => {
    flags.enabled = false
    expect(mount(ChannelStatusView).find('[data-testid]').exists()).toBe(false)
  })
})
