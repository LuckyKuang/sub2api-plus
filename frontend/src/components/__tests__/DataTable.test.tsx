import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nextProvider } from 'react-i18next'
import { createRef } from 'react'
import i18next from 'i18next'
import { initI18n } from '@/i18n'
import { DataTable, type Column, type DataTableHandle } from '../DataTable'

beforeAll(async () => {
  await initI18n()
})

function stubMatchMedia(matches: boolean) {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches,
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

class ResizeObserverStub {
  static instances: ResizeObserverStub[] = []
  observe = vi.fn()
  unobserve = vi.fn()
  disconnect = vi.fn()
  constructor(private callback: ResizeObserverCallback) {
    ResizeObserverStub.instances.push(this)
  }
}

beforeEach(() => {
  stubMatchMedia(true)
  localStorage.clear()
  ResizeObserverStub.instances = []
  vi.stubGlobal('ResizeObserver', ResizeObserverStub)
})

describe('DataTable', () => {
  it('renders paired sort arrows and highlights the active direction', async () => {
    const columns: Column[] = [
      { key: 'name', label: 'Name', sortable: true },
      { key: 'created_at', label: 'Created', sortable: true }
    ]
    render(
      <I18nextProvider i18n={i18next}>
        <DataTable
          columns={columns}
          data={[
            { id: 1, name: 'Beta', created_at: '2026-01-02T00:00:00Z' },
            { id: 2, name: 'Alpha', created_at: '2026-01-01T00:00:00Z' }
          ]}
          defaultSortKey="name"
          defaultSortOrder="asc"
          renderHeader={(key) => <span data-testid={`custom-${key}-header`}>{key}</span>}
        />
      </I18nextProvider>
    )

    await waitFor(() => {
      expect(screen.getByTestId('custom-name-header')).toBeInTheDocument()
    })

    const nameHeader = screen.getByRole('columnheader', { name: /name/i })
    expect(nameHeader).toHaveAttribute('aria-sort', 'ascending')
    const arrows = nameHeader.querySelectorAll('svg')
    expect(arrows).toHaveLength(2)
    expect(arrows[0]).toHaveClass('text-primary-600')
    expect(arrows[1]).toHaveClass('text-gray-300')

    await userEvent.click(nameHeader)
    await waitFor(() => {
      expect(screen.getByRole('columnheader', { name: /name/i })).toHaveAttribute('aria-sort', 'descending')
    })
    const toggledArrows = screen.getByRole('columnheader', { name: /name/i }).querySelectorAll('svg')
    expect(toggledArrows[0]).toHaveClass('text-gray-300')
    expect(toggledArrows[1]).toHaveClass('text-primary-600')
  })

  it('sorts rows client-side by default and emits sort events in server-side mode', async () => {
    const onSort = vi.fn()
    const data = [
      { id: 1, name: 'Beta' },
      { id: 2, name: 'Alpha' },
      { id: 3, name: 'Gamma' }
    ]
    render(
      <I18nextProvider i18n={i18next}>
        <DataTable
          columns={[{ key: 'name', label: 'Name', sortable: true }]}
          data={data}
          defaultSortKey="name"
          defaultSortOrder="asc"
          serverSideSort
          onSort={onSort}
        />
      </I18nextProvider>
    )

    await userEvent.click(screen.getByRole('columnheader', { name: /name/i }))
    expect(onSort).toHaveBeenCalledWith('name', 'desc')
    // Server-side mode never reorders locally.
    const cells = screen.getAllByRole('row').slice(1).map((row) => row.textContent)
    expect(cells).toEqual(['Beta', 'Alpha', 'Gamma'])
  })

  it('sorts numerically and keeps equal values in their original order', async () => {
    render(
      <I18nextProvider i18n={i18next}>
        <DataTable
          columns={[{ key: 'cost', label: 'Cost', sortable: true }]}
          data={[
            { id: 1, cost: '10' },
            { id: 2, cost: '2' },
            { id: 3, cost: '2' }
          ]}
          defaultSortKey="cost"
          defaultSortOrder="asc"
        />
      </I18nextProvider>
    )
    await waitFor(() => {
      expect(screen.getAllByRole('row').slice(1).map((row) => row.textContent)).toEqual(['2', '2', '10'])
    })
  })

  it('emits controlled current-page selection while preserving off-page keys', async () => {
    const onSelectedKeysChange = vi.fn()
    const onSelectionChange = vi.fn()
    const { rerender } = render(
      <I18nextProvider i18n={i18next}>
        <DataTable
          columns={[{ key: 'name', label: 'Name' }]}
          data={[
            { id: 1, name: 'One' },
            { id: 2, name: 'Two' }
          ]}
          rowKey="id"
          selectable
          selectedKeys={[99]}
          onSelectedKeysChange={onSelectedKeysChange}
          onSelectionChange={onSelectionChange}
        />
      </I18nextProvider>
    )

    await userEvent.click(screen.getByTestId('select-all'))
    const selectedAll = onSelectedKeysChange.mock.calls.at(-1)?.[0]
    expect(selectedAll).toEqual([99, 1, 2])

    rerender(
      <I18nextProvider i18n={i18next}>
        <DataTable
          columns={[{ key: 'name', label: 'Name' }]}
          data={[
            { id: 1, name: 'One' },
            { id: 2, name: 'Two' }
          ]}
          rowKey="id"
          selectable
          selectedKeys={selectedAll as number[]}
          onSelectedKeysChange={onSelectedKeysChange}
          onSelectionChange={onSelectionChange}
        />
      </I18nextProvider>
    )

    const rowCheckboxes = screen.getAllByTestId('select-row') as HTMLInputElement[]
    expect(rowCheckboxes.every((checkbox) => checkbox.checked)).toBe(true)

    await userEvent.click(rowCheckboxes[0])
    expect(onSelectedKeysChange.mock.calls.at(-1)?.[0]).toEqual([99, 2])
    expect(onSelectionChange.mock.calls.at(-1)?.[0]).toEqual([99, 2])
  })

  it('lets row controls act without also opening the detail', async () => {
    const onRowClick = vi.fn()
    const row = { id: 1, name: 'Request' }
    render(
      <I18nextProvider i18n={i18next}>
        <DataTable
          columns={[{ key: 'name', label: 'Name' }]}
          data={[row]}
          clickableRows
          onRowClick={onRowClick}
          renderCell={(_key, row) => (
            <div>
              <span data-testid="row-label">{(row as { name: string }).name}</span>
              <button type="button">Copy</button>
              <a href="#detail">Link</a>
              <input aria-label="field" />
            </div>
          )}
        />
      </I18nextProvider>
    )

    await userEvent.click(screen.getByRole('button', { name: 'Copy' }))
    await userEvent.click(screen.getByRole('link', { name: 'Link' }))
    await userEvent.click(screen.getByLabelText('field'))
    expect(onRowClick).not.toHaveBeenCalled()

    await userEvent.click(screen.getByTestId('row-label'))
    expect(onRowClick).toHaveBeenCalledWith(row)
  })

  it('keeps existing rows mounted during background refresh', async () => {
    const { rerender } = render(
      <I18nextProvider i18n={i18next}>
        <DataTable
          columns={[{ key: 'name', label: 'Name' }]}
          data={[{ id: 1, name: 'Existing request' }]}
          renderCell={() => <span data-testid="existing">Existing request</span>}
        />
      </I18nextProvider>
    )
    const original = screen.getByTestId('existing')

    rerender(
      <I18nextProvider i18n={i18next}>
        <DataTable
          columns={[{ key: 'name', label: 'Name' }]}
          data={[{ id: 1, name: 'Existing request' }]}
          loading
          renderCell={() => <span data-testid="existing">Existing request</span>}
        />
      </I18nextProvider>
    )

    expect(screen.getByTestId('existing')).toBe(original)
    expect(screen.getByRole('table').closest('[aria-busy]')).toHaveAttribute('aria-busy', 'true')
    expect(document.querySelector('.animate-pulse')).toBeNull()
  })

  it('shows the empty state and the loading skeleton', () => {
    const { unmount } = render(
      <I18nextProvider i18n={i18next}>
        <DataTable columns={[{ key: 'name', label: 'Name' }]} data={[]} />
      </I18nextProvider>
    )
    expect(screen.getByText('No data found')).toBeInTheDocument()
    unmount()

    render(
      <I18nextProvider i18n={i18next}>
        <DataTable columns={[{ key: 'name', label: 'Name' }]} data={[]} loading />
      </I18nextProvider>
    )
    expect(document.querySelectorAll('.animate-pulse').length).toBeGreaterThan(0)
    expect(screen.queryByText('No data found')).not.toBeInTheDocument()
  })

  it('renders every row with no virtual padding spacer for small datasets', async () => {
    const data = Array.from({ length: 8 }, (_, i) => ({ id: i + 1, name: `Row ${i + 1}` }))
    const ref = createRef<DataTableHandle>()
    render(
      <I18nextProvider i18n={i18next}>
        <DataTable ref={ref} columns={[{ key: 'name', label: 'Name' }]} data={data} />
      </I18nextProvider>
    )
    await waitFor(() => {
      expect(screen.getAllByRole('row')).toHaveLength(data.length + 1)
    })
    expect(document.querySelectorAll('tbody tr[aria-hidden="true"]')).toHaveLength(0)
    expect(ref.current?.shouldVirtualize).toBe(false)
  })

  it('switches to windowed rendering once row count exceeds virtualizeThreshold', async () => {
    const data = Array.from({ length: 12 }, (_, i) => ({ id: 100 + i, name: `Row ${i + 1}` }))
    const ref = createRef<DataTableHandle>()
    render(
      <I18nextProvider i18n={i18next}>
        <DataTable
          ref={ref}
          columns={[{ key: 'name', label: 'Name' }]}
          data={data}
          rowKey="id"
          virtualizeThreshold={3}
        />
      </I18nextProvider>
    )

    await waitFor(() => {
      expect(ref.current?.shouldVirtualize).toBe(true)
    })
    expect(ref.current?.virtualizer?.options.count).toBe(data.length)
    expect(ref.current?.virtualizer?.options.getItemKey(0)).toBe(100)
    expect(ref.current?.virtualizer?.options.getItemKey(5)).toBe(105)
    // The virtualizer drives the rendered window, so never more rows than data.
    expect(screen.getAllByRole('row').length).toBeLessThanOrEqual(data.length + 1)
  })

  it('releases stale virtualizer caches when a page replaces the row id set', async () => {
    const firstPage = Array.from({ length: 6 }, (_, i) => ({ id: i + 1, name: `First ${i + 1}` }))
    const secondPage = Array.from({ length: 6 }, (_, i) => ({ id: i + 101, name: `Second ${i + 1}` }))
    const ref = createRef<DataTableHandle>()
    const { rerender } = render(
      <I18nextProvider i18n={i18next}>
        <DataTable
          ref={ref}
          columns={[{ key: 'name', label: 'Name' }]}
          data={firstPage}
          rowKey="id"
          virtualizeThreshold={1}
        />
      </I18nextProvider>
    )
    await waitFor(() => expect(ref.current?.shouldVirtualize).toBe(true))

    const virtualizer = ref.current?.virtualizer as unknown as {
      itemSizeCache: Map<number, number>
      elementsCache: Map<unknown, Element>
      measureElement: (node: Element | null) => void
    }
    firstPage.forEach((row) => virtualizer.itemSizeCache.set(row.id, 156))
    const detached = document.createElement('tr')
    firstPage.forEach((row) => virtualizer.elementsCache.set(row.id, detached))

    rerender(
      <I18nextProvider i18n={i18next}>
        <DataTable
          ref={ref}
          columns={[{ key: 'name', label: 'Name' }]}
          data={secondPage}
          rowKey="id"
          virtualizeThreshold={1}
        />
      </I18nextProvider>
    )

    await waitFor(() => {
      expect(firstPage.some((row) => virtualizer.itemSizeCache.has(row.id))).toBe(false)
    })
    expect(firstPage.some((row) => virtualizer.elementsCache.has(row.id))).toBe(false)
  })

  it('preserves default first/actions pinning and lets a column explicitly opt out', async () => {
    const { rerender } = render(
      <I18nextProvider i18n={i18next}>
        <DataTable
          columns={[
            { key: 'name', label: 'Name' },
            { key: 'cost', label: 'Cost' },
            { key: 'actions', label: 'Actions' }
          ]}
          data={[{ id: 1, name: 'Name' }]}
        />
      </I18nextProvider>
    )
    for (const section of ['thead', 'tbody']) {
      const header = document.querySelector(`${section} [data-field="name"]`)
      expect(header).toHaveClass('sticky-col-left')
      expect(document.querySelector(`${section} [data-field="actions"]`)).toHaveClass('sticky-col-right')
      expect(document.querySelector(`${section} [data-field="cost"]`)).not.toHaveClass('sticky-col')
    }

    rerender(
      <I18nextProvider i18n={i18next}>
        <DataTable
          columns={[
            { key: 'name', label: 'Name', sticky: false },
            { key: 'actions', label: 'Actions', sticky: false }
          ]}
          data={[{ id: 1, name: 'Name' }]}
        />
      </I18nextProvider>
    )
    expect(document.querySelectorAll('.sticky-col')).toHaveLength(0)
  })

  it('includes the measured selection control width in the pinned first-column offset', async () => {
    const measure = vi
      .spyOn(HTMLElement.prototype, 'getBoundingClientRect')
      .mockImplementation(function (this: HTMLElement) {
        return new DOMRect(0, 0, this.dataset.field ? 180 : 46, 48)
      })
    try {
      render(
        <I18nextProvider i18n={i18next}>
          <DataTable
            columns={[{ key: 'name', label: 'Name' }]}
            data={[{ id: 1, name: 'Name' }]}
            selectable
          />
        </I18nextProvider>
      )
      await waitFor(() => {
        expect(document.querySelector('thead [data-field="name"]')).toHaveStyle({ left: '46px' })
      })
      expect(document.querySelector('thead th:first-child')).toHaveClass('sticky-col-left')
      expect(document.querySelector('tbody [data-field="name"]')).toHaveStyle({ left: '46px' })
    } finally {
      measure.mockRestore()
    }
  })

  it('offers current-page select all in the mobile card layout', async () => {
    stubMatchMedia(false)
    const onSelectedKeysChange = vi.fn()
    render(
      <I18nextProvider i18n={i18next}>
        <DataTable
          columns={[{ key: 'name', label: 'Name' }]}
          data={[
            { id: 1, name: 'One' },
            { id: 2, name: 'Two' }
          ]}
          rowKey="id"
          selectable
          selectedKeys={[99]}
          onSelectedKeysChange={onSelectedKeysChange}
        />
      </I18nextProvider>
    )

    expect(document.querySelector('table')).toBeNull()
    const cards = document.querySelector('.record-cards')
    expect(cards).not.toBeNull()

    await userEvent.click(screen.getByTestId('select-all-mobile'))
    expect(onSelectedKeysChange.mock.calls.at(-1)?.[0]).toEqual([99, 1, 2])

    // Cards render every column label plus the formatted value.
    expect(within(cards as HTMLElement).getAllByText('Name')).toHaveLength(2)
    expect(within(cards as HTMLElement).getByText('One')).toBeInTheDocument()
    expect(within(cards as HTMLElement).getByText('Two')).toBeInTheDocument()
  })

  it('persists and restores sort state through the storage key', async () => {
    const { unmount } = render(
      <I18nextProvider i18n={i18next}>
        <DataTable
          columns={[
            { key: 'name', label: 'Name', sortable: true },
            { key: 'cost', label: 'Cost', sortable: true }
          ]}
          data={[{ id: 1, name: 'Alpha', cost: 1 }]}
          sortStorageKey="test-sort-key"
        />
      </I18nextProvider>
    )
    await userEvent.click(screen.getByRole('columnheader', { name: /cost/i }))
    await waitFor(() => {
      expect(localStorage.getItem('test-sort-key')).toBe(JSON.stringify({ key: 'cost', order: 'asc' }))
    })
    unmount()

    render(
      <I18nextProvider i18n={i18next}>
        <DataTable
          columns={[
            { key: 'name', label: 'Name', sortable: true },
            { key: 'cost', label: 'Cost', sortable: true }
          ]}
          data={[{ id: 1, name: 'Alpha', cost: 1 }]}
          sortStorageKey="test-sort-key"
        />
      </I18nextProvider>
    )
    await waitFor(() => {
      expect(screen.getByRole('columnheader', { name: /cost/i })).toHaveAttribute('aria-sort', 'ascending')
    })
  })
})
