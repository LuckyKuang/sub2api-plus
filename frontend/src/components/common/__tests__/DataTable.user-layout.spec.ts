import { computed, defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import DataTable from '../DataTable.vue'
import { USER_TABLE_LAYOUT_KEY } from '@/composables/useUserTableLayout'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

// UI audit requirements: user tables must retain all selected fields without
// horizontal scrolling, keep original actions and selection working, and leave
// admin table pinning and presentation unchanged. Geometry is independent input.
function fitGeometry(initialAvailable: number, initialContent: number) {
  let available = initialAvailable
  let content = initialContent
  const observers: Array<{ callback: ResizeObserverCallback; targets: Set<Element> }> = []
  vi.stubGlobal('ResizeObserver', class {
    targets = new Set<Element>()
    constructor(public callback: ResizeObserverCallback) { observers.push(this) }
    observe(element: Element) { this.targets.add(element) }
    unobserve(element: Element) { this.targets.delete(element) }
    disconnect() { this.targets.clear() }
  })
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({
    matches: true, media: query, addEventListener: vi.fn(), removeEventListener: vi.fn(),
    addListener: vi.fn(), removeListener: vi.fn(),
  })))
  vi.spyOn(Element.prototype, 'clientWidth', 'get').mockImplementation(function (this: Element) {
    return this.classList.contains('table-wrapper') ? available : available + 24
  })
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockImplementation(function (this: HTMLElement) {
    return this.classList.contains('table-wrapper') ? available + 24 : 80
  })
  vi.spyOn(Element.prototype, 'scrollWidth', 'get').mockImplementation(function (this: Element) {
    return this.matches('table') ? Math.max(content, available) : available
  })
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
    const width = this.matches('table') ? Math.max(content, available)
      : this.classList.contains('user-record-cards') ? available + 24 : 80
    return new DOMRect(0, 0, width, 48)
  })
  return {
    setAvailable(value: number) { available = value },
    setContent(value: number) { content = value },
    async resize() {
      for (const observer of [...observers]) if (observer.targets.size) observer.callback([], observer as unknown as ResizeObserver)
      await flushPromises()
    },
    restore() { vi.restoreAllMocks(); vi.unstubAllGlobals() },
  }
}

const row = { id: 1, model: 'gpt-ui-audit-long-model-name-example', group: 'A long audit group', cost: '$0.001' }
const columns = [
  { key: 'model', label: 'Model', sticky: 'left' as const },
  { key: 'group', label: 'Group' },
  { key: 'cost', label: 'Actual cost' },
  { key: 'actions', label: 'Actions', sticky: 'right' as const },
]
const userProvider = { [USER_TABLE_LAYOUT_KEY as symbol]: computed(() => true) }

describe('DataTable user fit layout', () => {
  it('uses cards only when the table cannot fit, retains fields and selection, then restores a table on growth', async () => {
    const geometry = fitGeometry(1200, 1000)
    const wrapper = mount(DataTable, {
      attachTo: document.body,
      props: { columns, data: [row], selectable: true },
      global: { provide: userProvider },
      slots: { 'cell-actions': '<div class="table-actions"><button>View</button><button>Edit</button></div>' },
    })
    try {
      await flushPromises()
      expect(wrapper.find('table').exists()).toBe(true)
      expect(wrapper.findAll('.sticky-col')).toHaveLength(0)
      expect(wrapper.vm.scrollViewportEl).toBe(wrapper.get('.table-wrapper').element)

      geometry.setAvailable(800)
      await geometry.resize()
      expect(wrapper.find('table').exists()).toBe(false)
      const cards = wrapper.get('.user-record-cards').element
      expect(wrapper.vm.tableWrapperEl).toBeNull()
      expect(wrapper.vm.scrollViewportEl).toBe(cards)
      expect(wrapper.find('.user-record-fields').exists()).toBe(true)
      expect(wrapper.find('.user-record-selection').exists()).toBe(true)
      expect(wrapper.find('.user-record-actions').exists()).toBe(true)
      for (const text of [row.model, row.group, row.cost, 'Model', 'Group', 'Actual cost', 'View', 'Edit']) {
        expect(cards.textContent).toContain(text)
      }
      await wrapper.get('[data-test="select-row"]').setValue(true)
      expect(wrapper.emitted('update:selectedKeys')).toEqual([[[1]]])

      // Repeated resize notifications must not toggle a too-wide table back in.
      await geometry.resize()
      expect(wrapper.get('.user-record-cards').element).toBe(cards)
      expect(wrapper.find('table').exists()).toBe(false)

      geometry.setAvailable(1500)
      await geometry.resize()
      expect(wrapper.find('table').exists()).toBe(true)
      expect(wrapper.find('.user-record-cards').exists()).toBe(false)
      expect(wrapper.find('.data-table-fit-probe').exists()).toBe(false)
      expect(wrapper.findAll('.sticky-col')).toHaveLength(0)
      expect(wrapper.vm.scrollViewportEl).toBe(wrapper.get('.table-wrapper').element)
    } finally { wrapper.unmount(); geometry.restore() }
  })

  it('keeps card state for identical polling results and remeasures changed content without a permanent probe', async () => {
    const geometry = fitGeometry(800, 1400)
    const mountedCell = vi.fn()
    const cell = defineComponent({ setup() { mountedCell(); return {} }, template: '<span>Audit model</span>' })
    const wrapper = mount(DataTable, {
      attachTo: document.body,
      props: { columns, data: [row] }, global: { provide: userProvider },
      slots: { 'cell-model': cell },
    })
    try {
      await flushPromises()
      const cards = wrapper.get('.user-record-cards').element
      const previousMounts = mountedCell.mock.calls.length
      await wrapper.setProps({ data: [{ ...row }] })
      await flushPromises()
      expect(wrapper.get('.user-record-cards').element).toBe(cards)
      expect(mountedCell).toHaveBeenCalledTimes(previousMounts)

      geometry.setContent(700)
      await wrapper.setProps({ data: [{ ...row, model: 'Short model' }] })
      await flushPromises()
      expect(wrapper.find('table').exists()).toBe(true)
      expect(wrapper.find('.user-record-cards').exists()).toBe(false)
      expect(wrapper.find('.data-table-fit-probe').exists()).toBe(false)
    } finally { wrapper.unmount(); geometry.restore() }
  })

  it('preserves the normal admin table and sticky columns when content is wider than its container', async () => {
    const geometry = fitGeometry(800, 1600)
    const wrapper = mount(DataTable, { props: { columns, data: [row], selectable: true } })
    try {
      await flushPromises()
      await geometry.resize()
      expect(wrapper.find('table').exists()).toBe(true)
      expect(wrapper.find('.user-record-cards').exists()).toBe(false)
      expect(wrapper.get('thead [data-field="model"]').classes()).toContain('sticky-col-left')
      expect(wrapper.get('thead [data-field="actions"]').classes()).toContain('sticky-col-right')
      expect(wrapper.get('thead th:first-child').classes()).toContain('sticky-col-left')
    } finally { wrapper.unmount(); geometry.restore() }
  })
})
