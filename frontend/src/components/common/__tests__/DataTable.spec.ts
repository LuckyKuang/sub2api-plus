import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import DataTable from '../DataTable.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

const stubDesktopMatchMedia = () => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: true,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn()
    }))
  })
}

const stubMobileMatchMedia = () => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn()
    }))
  })
}

describe('DataTable', () => {
  beforeEach(() => {
    stubDesktopMatchMedia()
    localStorage.clear()
  })

  it('pins an explicitly declared prefix using measured widths in both header and records', async () => {
    const widths: Record<string, number> = { user: 187.5, api_key: 104.25, account: 143.75 }
    const measure = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      return new DOMRect(0, 0, widths[this.dataset.field ?? ''] ?? 40, 48)
    })
    const callbacks: ResizeObserverCallback[] = []
    const originalObserver = globalThis.ResizeObserver
    vi.stubGlobal('ResizeObserver', class {
      constructor(callback: ResizeObserverCallback) { callbacks.push(callback) }
      observe() {}
      unobserve() {}
      disconnect() {}
    })
    const columns = [
      { key: 'user', label: 'User', sticky: 'left' as const },
      { key: 'api_key', label: 'API Key', sticky: 'left' as const },
      { key: 'account', label: 'Account', sticky: 'left' as const },
      { key: 'cost', label: 'Cost' }
    ]
    const wrapper = mount(DataTable, {
      props: { columns, data: [{ id: 1, user: 'User', api_key: 'Key', account: 'Account' }] }
    })
    try {
      await flushPromises()
      for (const selector of ['thead', 'tbody']) {
        const account = wrapper.get(`${selector} [data-field="account"]`)
        expect(account.classes()).toContain('sticky-col-left')
        expect((account.element as HTMLElement).style.left).toBe('291.75px')
        expect(wrapper.get(`${selector} [data-field="cost"]`).classes()).not.toContain('sticky-col')
      }

      // Hiding the optional middle column and resizing the first column both
      // change the pin offset; translated labels need no hard-coded widths.
      await wrapper.setProps({ columns: columns.filter(column => column.key !== 'api_key') })
      await flushPromises()
      expect((wrapper.get('thead [data-field="account"]').element as HTMLElement).style.left).toBe('187.5px')
      widths.user = 221
      callbacks.forEach(callback => callback([], {} as ResizeObserver))
      await wrapper.vm.$nextTick()
      for (const selector of ['thead', 'tbody']) {
        expect((wrapper.get(`${selector} [data-field="account"]`).element as HTMLElement).style.left).toBe('221px')
      }
    } finally {
      wrapper.unmount()
      measure.mockRestore()
      vi.stubGlobal('ResizeObserver', originalObserver)
    }
  })

  it('preserves default first/actions pinning and lets a column explicitly opt out', async () => {
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }, { key: 'cost', label: 'Cost' }, { key: 'actions', label: 'Actions' }],
        data: [{ id: 1, name: 'Name' }]
      }
    })
    await flushPromises()
    for (const selector of ['thead', 'tbody']) {
      expect(wrapper.get(`${selector} [data-field="name"]`).classes()).toContain('sticky-col-left')
      expect(wrapper.get(`${selector} [data-field="actions"]`).classes()).toContain('sticky-col-right')
      expect(wrapper.get(`${selector} [data-field="cost"]`).classes()).not.toContain('sticky-col')
    }
    await wrapper.setProps({ columns: [{ key: 'name', label: 'Name', sticky: false }, { key: 'actions', label: 'Actions', sticky: false }] })
    expect(wrapper.findAll('.sticky-col')).toHaveLength(0)
    wrapper.unmount()
  })

  it('refreshes measurement refs and sticky offsets when the shared header layout replaces its hidden copy', async () => {
    let firstWidth = 120
    const measure = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      return new DOMRect(0, 0, this.dataset.field === 'user' ? firstWidth : 80, 48)
    })
    const observed: Element[] = []
    const originalObserver = globalThis.ResizeObserver
    vi.stubGlobal('ResizeObserver', class {
      observe(element: Element) { observed.push(element) }
      unobserve() {}
      disconnect() {}
    })
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'user', label: 'User', sticky: 'left' }, { key: 'account', label: 'Account', sticky: 'left' }],
        data: [{ id: 1, user: 'User', account: 'Account' }],
      },
    })
    try {
      await flushPromises()
      const realHead = wrapper.get('thead').element
      const copy = realHead.cloneNode(true) as HTMLTableSectionElement
      realHead.replaceWith(copy)
      observed.length = 0
      firstWidth = 221
      wrapper.element.dispatchEvent(new Event('static-table-header-measured'))
      await flushPromises()
      expect(observed).toContain(copy.rows[0]!.cells[0])
      expect((wrapper.get('tbody [data-field="account"]').element as HTMLElement).style.left).toBe('221px')
      expect((realHead.querySelector('[data-field="account"]') as HTMLElement).style.left).toBe('221px')
    } finally {
      wrapper.unmount()
      measure.mockRestore()
      vi.stubGlobal('ResizeObserver', originalObserver)
    }
  })

  it('includes the measured selection control width in the pinned first-column offset', async () => {
    const measure = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      return new DOMRect(0, 0, this.dataset.field ? 180 : 46, 48)
    })
    const wrapper = mount(DataTable, {
      props: { selectable: true, columns: [{ key: 'name', label: 'Name' }], data: [{ id: 1, name: 'Name' }] }
    })
    try {
      await flushPromises()
      expect(wrapper.get('thead th:first-child').classes()).toContain('sticky-col-left')
      for (const selector of ['thead', 'tbody']) {
        expect((wrapper.get(`${selector} [data-field="name"]`).element as HTMLElement).style.left).toBe('46px')
      }
    } finally {
      wrapper.unmount()
      measure.mockRestore()
    }
  })

  it.each(['desktop', 'mobile'])('keeps existing rows mounted during background refresh on %s', async viewport => {
    if (viewport === 'mobile') stubMobileMatchMedia()
    const wrapper = mount(DataTable, {
      props: { columns: [{ key: 'name', label: 'Name' }], data: [{ id: 1, name: 'Existing request' }] },
      slots: { 'cell-name': '<span data-record>Existing request</span>' }
    })
    const original = wrapper.get('[data-record]').element
    await wrapper.setProps({ loading: true })
    expect(wrapper.get('[data-record]').element).toBe(original)
    expect(wrapper.attributes('aria-busy')).toBe('true')
    expect(wrapper.find('.animate-pulse').exists()).toBe(false)
    wrapper.unmount()
  })

  it('lets row controls act without also opening the detail', async () => {
    const row = { id: 1, name: 'Request' }
    const wrapper = mount(DataTable, {
      props: { columns: [{ key: 'name', label: 'Name' }], data: [row], clickableRows: true },
      slots: { 'cell-name': '<div><span data-label>Request</span><button>Copy</button><a href="#detail">Link</a><input /></div>' }
    })
    for (const selector of ['button', 'a', 'input']) await wrapper.get(`tbody ${selector}`).trigger('click')
    expect(wrapper.emitted('rowClick')).toBeUndefined()
    await wrapper.get('[data-label]').trigger('click')
    expect(wrapper.emitted('rowClick')).toEqual([[row]])
    wrapper.unmount()
  })

  it('renders paired sort arrows and highlights the active direction', async () => {
    const wrapper = mount(DataTable, {
      props: {
        columns: [
          { key: 'name', label: 'Name', sortable: true },
          { key: 'created_at', label: 'Created', sortable: true }
        ],
        data: [
          { id: 1, name: 'Beta', created_at: '2026-01-02T00:00:00Z' },
          { id: 2, name: 'Alpha', created_at: '2026-01-01T00:00:00Z' }
        ],
        defaultSortKey: 'name',
        defaultSortOrder: 'asc'
      },
      slots: {
        'header-name': '<span data-test="custom-name-header">Name</span>'
      }
    })

    await wrapper.vm.$nextTick()

    const nameHeader = wrapper.findAll('th')[0]
    expect(nameHeader.find('[data-test="custom-name-header"]').exists()).toBe(true)
    expect(nameHeader.attributes('aria-sort')).toBe('ascending')
    expect(nameHeader.findAll('svg')).toHaveLength(2)
    expect(nameHeader.findAll('svg')[0].classes()).toContain('text-primary-600')
    expect(nameHeader.findAll('svg')[1].classes()).toContain('text-gray-300')

    await nameHeader.trigger('click')
    await wrapper.vm.$nextTick()

    expect(nameHeader.attributes('aria-sort')).toBe('descending')
    expect(nameHeader.findAll('svg')[0].classes()).toContain('text-gray-300')
    expect(nameHeader.findAll('svg')[1].classes()).toContain('text-primary-600')
  })

  it('renders every row with no virtual padding spacer for small datasets (virtualization off)', async () => {
    const data = Array.from({ length: 8 }, (_, i) => ({ id: i + 1, name: `Row ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data
      }
    })

    await wrapper.vm.$nextTick()

    // Virtualization is OFF for a small list…
    expect((wrapper.vm as any).shouldVirtualize).toBe(false)
    // …every row is in the DOM…
    expect(wrapper.findAll('tbody tr[data-index]')).toHaveLength(data.length)
    // …and there are no aria-hidden virtual padding spacer rows.
    expect(wrapper.findAll('tbody tr[aria-hidden="true"]')).toHaveLength(0)
  })

  it('switches to windowed rendering once row count exceeds virtualizeThreshold', async () => {
    const data = Array.from({ length: 12 }, (_, i) => ({ id: i + 1, name: `Row ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data,
        virtualizeThreshold: 3
      }
    })

    await wrapper.vm.$nextTick()

    // Virtualization is ON: the mode-switch decision flipped…
    expect((wrapper.vm as any).shouldVirtualize).toBe(true)
    // …and the virtualizer drives off the full row count.
    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    expect(instance.options.count).toBe(data.length)
  })

  it('keys the virtualizer size cache by row identity, not index (avoids stale heights on sort/filter)', async () => {
    const data = Array.from({ length: 12 }, (_, i) => ({ id: 100 + i, name: `Row ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data,
        rowKey: 'id',
        virtualizeThreshold: 3
      }
    })

    await wrapper.vm.$nextTick()

    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    // getItemKey must resolve to the row's stable key (id), not the positional index.
    expect(instance.options.getItemKey(0)).toBe(100)
    expect(instance.options.getItemKey(5)).toBe(105)
  })

  it('clears stale row and element caches when pagination replaces the row ID set', async () => {
    const firstPage = Array.from({ length: 100 }, (_, i) => ({ id: i + 1, name: `First ${i + 1}` }))
    const secondPage = Array.from({ length: 100 }, (_, i) => ({ id: i + 101, name: `Second ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data: firstPage,
        rowKey: 'id',
        virtualizeThreshold: 1
      }
    })

    await wrapper.vm.$nextTick()

    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    const firstPageIDs = firstPage.map(row => row.id)
    ;(instance as any).itemSizeCache = new Map(firstPageIDs.map(id => [id, 156]))
    instance.elementsCache.clear()
    for (const id of firstPageIDs) {
      instance.elementsCache.set(id, document.createElement('tr'))
    }
    const measureElementSpy = vi.spyOn(instance, 'measureElement')

    await wrapper.setProps({ data: secondPage })
    await wrapper.vm.$nextTick()

    const sizeCache = (instance as any).itemSizeCache as Map<number, number>
    expect(sizeCache.size).toBeLessThanOrEqual(secondPage.length)
    expect(instance.elementsCache.size).toBeLessThanOrEqual(secondPage.length)
    expect(firstPageIDs.some(id => sizeCache.has(id))).toBe(false)
    expect(firstPageIDs.some(id => instance.elementsCache.has(id))).toBe(false)
    expect(measureElementSpy.mock.calls.some(([node]) => node === null)).toBe(true)
  })

  it('clears stale caches when equal-length pages replace rows without stable keys', async () => {
    const firstPage = Array.from({ length: 12 }, (_, i) => ({ name: `First ${i + 1}` }))
    const secondPage = Array.from({ length: 12 }, (_, i) => ({ name: `Second ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data: firstPage,
        virtualizeThreshold: 1
      }
    })

    await wrapper.vm.$nextTick()

    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    const measureElementSpy = vi.spyOn(instance, 'measureElement')

    await wrapper.setProps({ data: secondPage })
    await wrapper.vm.$nextTick()

    expect(measureElementSpy.mock.calls.some(([node]) => node === null)).toBe(true)
  })

  it('conservatively clears caches when duplicate row-key multiplicity changes', async () => {
    const firstPage = [
      { id: 1, name: 'First A' },
      { id: 1, name: 'First B' },
      { id: 2, name: 'First C' }
    ]
    const secondPage = [
      { id: 1, name: 'Second A' },
      { id: 2, name: 'Second B' },
      { id: 2, name: 'Second C' }
    ]
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data: firstPage,
        rowKey: 'id',
        virtualizeThreshold: 1
      }
    })

    await wrapper.vm.$nextTick()

    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    const measureElementSpy = vi.spyOn(instance, 'measureElement')

    await wrapper.setProps({ data: secondPage })
    await wrapper.vm.$nextTick()

    expect(measureElementSpy.mock.calls.some(([node]) => node === null)).toBe(true)
  })

  it('preserves cache when rows without stable keys only reorder the same objects', async () => {
    const data = Array.from({ length: 12 }, (_, i) => ({ name: `Row ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data,
        virtualizeThreshold: 1
      }
    })

    await wrapper.vm.$nextTick()

    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    const measureSpy = vi.spyOn(instance, 'measure')

    await wrapper.setProps({ data: [...data].reverse() })
    await wrapper.vm.$nextTick()

    expect(measureSpy).not.toHaveBeenCalled()
  })

  it('preserves stable row height cache when the same row IDs are only reordered', async () => {
    const data = Array.from({ length: 100 }, (_, i) => ({ id: i + 1, name: `Row ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data,
        rowKey: 'id',
        virtualizeThreshold: 1
      }
    })

    await wrapper.vm.$nextTick()

    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    ;(instance as any).itemSizeCache = new Map(data.map(row => [row.id, 156]))
    const measureSpy = vi.spyOn(instance, 'measure')

    await wrapper.setProps({ data: [...data].reverse() })
    await wrapper.vm.$nextTick()

    const sizeCache = (instance as any).itemSizeCache as Map<number, number>
    expect(measureSpy).not.toHaveBeenCalled()
    expect(sizeCache.size).toBe(100)
  })

  it('emits controlled current-page selection while preserving off-page keys', async () => {
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data: [
          { id: 1, name: 'One' },
          { id: 2, name: 'Two' }
        ],
        rowKey: 'id',
        selectable: true,
        selectedKeys: [99]
      }
    })

    await wrapper.get('[data-test="select-all"]').setValue(true)

    const selectedAll = wrapper.emitted('update:selectedKeys')?.at(-1)?.[0]
    expect(selectedAll).toEqual([99, 1, 2])

    await wrapper.setProps({ selectedKeys: selectedAll as number[] })
    const rowCheckboxes = wrapper.findAll<HTMLInputElement>('[data-test="select-row"]')
    expect(rowCheckboxes.every((checkbox) => checkbox.element.checked)).toBe(true)

    await rowCheckboxes[0].setValue(false)

    expect(wrapper.emitted('update:selectedKeys')?.at(-1)?.[0]).toEqual([99, 2])
    expect(wrapper.emitted('selectionChange')?.at(-1)?.[0]).toEqual([99, 2])
  })

  it('keeps the single usage field shrinkable in a 320px mobile card', () => {
    stubMobileMatchMedia()
    const viewport = document.createElement('div')
    viewport.style.width = '320px'
    document.body.appendChild(viewport)
    const wrapper = mount(DataTable, {
      attachTo: viewport,
      props: {
        columns: [{ key: 'usage', label: 'Usage' }],
        data: [{ id: 1, usage: 'snapshot' }],
        rowKey: 'id'
      },
      slots: {
        'cell-usage': '<div data-test="usage-cell">snapshot</div>'
      }
    })

    expect(viewport.style.width).toBe('320px')
    expect(wrapper.findAll('[data-field="usage"]')).toHaveLength(1)
    expect(wrapper.find('[data-field="ollama_cloud_usage"]').exists()).toBe(false)
    const field = wrapper.get('[data-field="usage"]')
    expect(field.classes()).toContain('min-w-0')
    expect(field.get('div').classes()).toEqual(expect.arrayContaining(['min-w-0', 'max-w-full']))
    expect(wrapper.findAll('[data-test="usage-cell"]')).toHaveLength(1)

    wrapper.unmount()
    viewport.remove()
  })

  it('offers current-page select all in the mobile card layout', async () => {
    stubMobileMatchMedia()
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data: [
          { id: 1, name: 'One' },
          { id: 2, name: 'Two' }
        ],
        rowKey: 'id',
        selectable: true,
        selectedKeys: [99]
      }
    })

    await wrapper.get('[data-test="select-all-mobile"]').setValue(true)

    expect(wrapper.emitted('update:selectedKeys')?.at(-1)?.[0]).toEqual([99, 1, 2])
  })
})
