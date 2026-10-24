import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import DetailWorkspace from '@/components/common/DetailWorkspace.vue'
import UserErrorRequestsTable from '../UserErrorRequestsTable.vue'
import type { UserErrorRequest } from '@/types'

vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const callbacks: ResizeObserverCallback[] = []
const table = {
  props: ['data'],
  emits: ['rowClick'],
  template: '<div data-testid="table-scroll"><button @click="$emit(\'rowClick\', data[0])">Open error</button></div>',
}
const pagination = {
  props: ['page'],
  emits: ['update:page'],
  template: '<nav data-testid="pagination"><button @click="$emit(\'update:page\', page + 1)">Next</button></nav>',
}
const row = { id: 12, model: 'test-model', client_ip: '203.0.113.1' } as UserErrorRequest

function setup(workspace = true) {
  return mount(UserErrorRequestsTable, {
    attachTo: document.body,
    props: { workspace, rows: [row], total: 120, page: 2, pageSize: 20, loading: false },
    global: { stubs: {
      DataTable: table, Pagination: pagination, IpGeoBatchToolbar: true, Icon: true,
      BaseDialog: { props: ['show'], template: '<div v-if="show" role="dialog"><slot /></div>' },
      UserErrorDetailModal: { props: ['errorId'], template: '<div data-testid="error-detail">{{ errorId }}</div>' },
    } },
  })
}

enableAutoUnmount(afterEach)
beforeEach(() => {
  callbacks.length = 0
  vi.stubGlobal('ResizeObserver', class {
    constructor(callback: ResizeObserverCallback) { callbacks.push(callback) }
    observe() {}
    disconnect() {}
  })
})
afterEach(() => { vi.unstubAllGlobals() })

async function setWidth(width: number) {
  callbacks.forEach(callback => callback([{ contentRect: { width } } as ResizeObserverEntry], {} as ResizeObserver))
  await flushPromises()
}

describe('user error table workspace', () => {
  it('keeps pagination outside the scrolling table while opening, docking and closing error details', async () => {
    const wrapper = setup()
    const scroll = wrapper.get('[data-testid="table-scroll"]').element
    const pager = wrapper.get('[data-testid="pagination"]').element
    const content = wrapper.get('.detail-workspace-content').element
    expect(wrapper.findComponent(DetailWorkspace).props('workspace')).toBe(true)
    expect(wrapper.get('.detail-workspace').classes()).toContain('detail-workspace-contained')
    expect(wrapper.get('[data-testid="table-scroll"]').classes()).toContain('workspace-table-viewport')
    expect(content.contains(scroll)).toBe(true)
    expect(content.contains(pager)).toBe(true)
    expect(scroll.contains(pager)).toBe(false)
    expect(pager.parentElement?.classList.contains('flex-shrink-0')).toBe(true)

    await wrapper.get('[data-testid="table-scroll"] button').trigger('click')
    expect(wrapper.get('[role="dialog"] [data-testid="error-detail"]').text()).toBe('12')
    await setWidth(1600)
    expect(wrapper.get('.detail-workspace').classes()).toContain('detail-workspace-open')
    expect(wrapper.get('aside [data-testid="error-detail"]').text()).toBe('12')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="table-scroll"]').element).toBe(scroll)
    expect(wrapper.get('[data-testid="pagination"]').element).toBe(pager)

    await wrapper.get('[data-testid="pagination"] button').trigger('click')
    expect(wrapper.emitted('update:page')).toEqual([[3]])
    await wrapper.setProps({ page: 3 })
    await setWidth(900)
    expect(wrapper.get('[role="dialog"] [data-testid="error-detail"]').text()).toBe('12')
    expect(wrapper.get('[data-testid="pagination"]').element).toBe(pager)
    await setWidth(1600)
    await wrapper.get('aside button').trigger('click')
    expect(wrapper.find('aside').exists()).toBe(false)
    expect(wrapper.get('[data-testid="table-scroll"]').element).toBe(scroll)
    await wrapper.get('[data-testid="pagination"] button').trigger('click')
    expect(wrapper.emitted('update:page')).toEqual([[3], [4]])
  })

  it('keeps standalone detail layouts outside the contained workspace mode', async () => {
    const wrapper = setup(false)
    expect(wrapper.get('.detail-workspace').classes()).not.toContain('detail-workspace-contained')
    expect(wrapper.get('[data-testid="table-scroll"]').classes()).not.toContain('workspace-table-viewport')
    await wrapper.get('[data-testid="pagination"] button').trigger('click')
    expect(wrapper.emitted('update:page')).toEqual([[3]])
  })
})
