import { h, reactive } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import UsageRecordWorkspace from '../UsageRecordWorkspace.vue'
import UsageRecordDetail from '../UsageRecordDetail.vue'
import type { AdminUsageLog } from '@/types'

const { getUser, getAdmin } = vi.hoisted(() => ({ getUser: vi.fn(), getAdmin: vi.fn() }))
vi.mock('@/api/usage', () => ({ usageAPI: { getById: getUser } }))
vi.mock('@/api/admin/usage', () => ({ adminUsageAPI: { getById: getAdmin } }))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const row = (id: number) => ({ id, model: `model-${id}`, request_id: `request-${id}`, account_id: 88, upstream_model: 'secret-upstream',
  input_tokens: 1, output_tokens: 2, duration_ms: 1000, timing_version: 1, first_token_ms: 10, actual_cost: 0.00000004,
  completion_status: 'completed', created_at: '2026-10-21T10:00:00Z' } as AdminUsageLog)
const workspace = {
  props: ['show'], emits: ['close'],
  template: '<div><slot /><div v-if="show"><button data-close @click="$emit(\'close\')">close</button><slot name="detail" /></div></div>',
}
beforeEach(() => { getUser.mockReset(); getAdmin.mockReset() })
async function setup(query = '') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/usage', component: { template: '<div />' } }] })
  await router.push('/usage' + query); await router.isReady()
  const rows = reactive([row(1), row(2)])
  const wrapper = mount(UsageRecordWorkspace, {
    props: { rows, mode: 'user' },
    slots: { default: ({ select }) => rows.map(r => h('button', { 'data-row': r.id, onClick: () => select(r) }, String(r.id))) },
    global: { plugins: [router], stubs: { DetailWorkspace: workspace } },
  })
  return { wrapper, router }
}
describe('usage detail navigation and isolation', () => {
  it('rejects a late A response after the reader has selected B', async () => {
    let a!: (value: AdminUsageLog) => void
    let b!: (value: AdminUsageLog) => void
    getUser.mockImplementation((id: number) => new Promise(resolve => { if (id === 1) a = resolve; else b = resolve }))
    const { wrapper, router } = await setup('?filter=keep')
    await wrapper.get('[data-row="1"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-row="2"]').trigger('click'); await flushPromises()
    b(row(2)); await flushPromises(); a(row(1)); await flushPromises()
    expect(wrapper.findComponent(UsageRecordDetail).props('row').id).toBe(2)
    expect(getAdmin).not.toHaveBeenCalled()
    expect(router.currentRoute.value.query.filter).toBe('keep')
    await wrapper.get('[data-close]').trigger('click'); await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ filter: 'keep' })
    expect(wrapper.findComponent(UsageRecordDetail).exists()).toBe(false)
    wrapper.unmount()
  })
  it('preserves filters changed while a detail is open', async () => {
    getUser.mockImplementation((id: number) => Promise.resolve(row(id)))
    const { wrapper, router } = await setup('?filter=before#results')
    await wrapper.get('[data-row="1"]').trigger('click'); await flushPromises()
    expect(router.currentRoute.value.hash).toBe('#results')
    await router.replace({ query: { ...router.currentRoute.value.query, filter: 'after' }, hash: '#results' })
    await wrapper.get('[data-close]').trigger('click'); await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ filter: 'after' })
    expect(router.currentRoute.value.hash).toBe('#results')
    wrapper.unmount()
  })
  it('does not restore a closed detail when its response arrives', async () => {
    let resolve!: (value: AdminUsageLog) => void
    getUser.mockImplementation(() => new Promise(done => { resolve = done }))
    const { wrapper, router } = await setup('?record=1')
    await wrapper.get('[data-close]').trigger('click'); await flushPromises()
    resolve(row(1)); await flushPromises()
    expect(router.currentRoute.value.query.record).toBeUndefined()
    expect(wrapper.findComponent(UsageRecordDetail).exists()).toBe(false)
    wrapper.unmount()
  })
  it('keeps admin-only routing fields out of the user detail', () => {
    const wrapper = mount(UsageRecordDetail, { props: { row: row(1), admin: false } })
    expect(wrapper.text()).not.toContain('secret-upstream')
    expect(wrapper.text()).not.toContain('inspection.accountCost')
    expect(wrapper.text()).toContain('$4.00e-8')
    wrapper.unmount()
  })
})
