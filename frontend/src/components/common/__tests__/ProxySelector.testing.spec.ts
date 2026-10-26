import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import ProxySelector from '../ProxySelector.vue'
import type { Proxy } from '@/types'

const testProxy = vi.hoisted(() => vi.fn())
vi.mock('@/api/admin', () => ({ adminAPI: { proxies: { testProxy } } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => { vi.clearAllMocks(); testProxy.mockReset() })

async function openSelector() {
  const wrapper = mount(ProxySelector, {
    props: { modelValue: null, proxies: [1, 2].map(id => ({
      id, name: `Proxy ${id}`, host: 'localhost', port: 8080, protocol: 'http'
    } as Proxy)) },
    global: { stubs: { Icon: true } }
  })
  await wrapper.get('.select-trigger').trigger('click')
  return wrapper
}

describe('proxy connection tests', () => {
  it('does not restart an individual test when a batch is started', async () => {
    let finish!: (result: object) => void
    testProxy.mockImplementation((id: number) => id === 1
      ? new Promise(resolve => { finish = resolve })
      : Promise.resolve({ success: true, country: 'GB' }))
    const wrapper = await openSelector()
    await wrapper.findAll('.test-btn')[0].trigger('click')
    await wrapper.get('.batch-test-btn').trigger('click')
    await flushPromises()
    expect(testProxy.mock.calls.map(([id]) => id)).toEqual([1, 2])
    expect(wrapper.findAll('.test-btn')[0].attributes('disabled')).toBeDefined()
    finish({ success: true, country: 'US' })
    await flushPromises()
    expect(wrapper.text()).toContain('US')
    expect(wrapper.findAll('.test-btn')[0].attributes('disabled')).toBeUndefined()
  })

  it('shows per-proxy outcomes and allows another batch after a failure', async () => {
    testProxy.mockImplementation((id: number) => id === 1
      ? Promise.reject(new Error('offline'))
      : Promise.resolve({ success: true, country: 'GB' }))
    const wrapper = await openSelector()
    await wrapper.get('.batch-test-btn').trigger('click')
    await flushPromises()
    expect(testProxy).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('admin.proxies.testFailed')
    expect(wrapper.text()).toContain('GB')
    expect(wrapper.get('.batch-test-btn').attributes('disabled')).toBeUndefined()
    await wrapper.get('.batch-test-btn').trigger('click')
    await flushPromises()
    expect(testProxy).toHaveBeenCalledTimes(4)
  })
})

// A proxy ID survives edits and repair. A connectivity badge is evidence only
// for the captured endpoint/auth revision; it must not certify a replacement.
describe('proxy selector observations after route repair', () => {
  it('clears cached connectivity when a saved route changes', async () => {
    testProxy.mockResolvedValueOnce({ success: true, message: 'old', country: 'OLD' })
    const wrapper = await openSelector()
    await wrapper.findAll('.test-btn')[0].trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('OLD')
    await wrapper.setProps({ proxies: [{ ...wrapper.props('proxies')[0], host: 'repaired.example' }] })
    expect(wrapper.text()).not.toContain('OLD')
    expect(wrapper.text()).toContain('repaired.example')
    testProxy.mockResolvedValueOnce({ success: true, message: 'new', country: 'NL' })
    await wrapper.get('.test-btn').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('NL')
  })

  it('ignores an old success and keeps a replacement test pending until its own response', async () => {
    let finishOld!: (value: object) => void
    let finishCurrent!: (value: object) => void
    testProxy.mockImplementationOnce(() => new Promise(resolve => { finishOld = resolve }))
      .mockImplementationOnce(() => new Promise(resolve => { finishCurrent = resolve }))
    const wrapper = await openSelector()
    await wrapper.findAll('.test-btn')[0].trigger('click')
    await wrapper.setProps({ proxies: [{ ...wrapper.props('proxies')[0], password: 'rotated-password', updated_at: '2026-10-25T10:00:00Z' }] })
    expect(wrapper.get('.test-btn').attributes('disabled')).toBeUndefined()
    await wrapper.get('.test-btn').trigger('click')
    expect(testProxy).toHaveBeenCalledTimes(2)
    finishOld({ success: true, message: 'old', country: 'STALE' })
    await flushPromises()
    expect(wrapper.text()).not.toContain('STALE')
    expect(wrapper.get('.test-btn').attributes('disabled')).toBeDefined()
    finishCurrent({ success: true, message: 'new', country: 'NL' })
    await flushPromises()
    expect(wrapper.text()).toContain('NL')
    expect(wrapper.get('.test-btn').attributes('disabled')).toBeUndefined()
  })

  it('does not attach a delayed failure to a deleted and recreated selector option', async () => {
    let failOld!: (error: Error) => void
    testProxy.mockImplementationOnce(() => new Promise((_, reject) => { failOld = reject }))
    const wrapper = await openSelector()
    const first = { ...wrapper.props('proxies')[0] }
    await wrapper.findAll('.test-btn')[0].trigger('click')
    await wrapper.setProps({ proxies: [] })
    await wrapper.setProps({ proxies: [{ ...first, host: 'replacement.example' }] })
    failOld(new Error('old endpoint offline'))
    await flushPromises()
    expect(wrapper.text()).not.toContain('admin.proxies.testFailed')
    expect(wrapper.get('.test-btn').attributes('disabled')).toBeUndefined()
  })
})
