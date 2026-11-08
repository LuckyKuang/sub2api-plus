import { render, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nextProvider } from 'react-i18next'
import i18next from 'i18next'
import { initI18n } from '@/i18n'
import { DataTable, type Column } from '../DataTable'
import { UserTableLayoutProvider } from '@/composables/useUserTableLayout'

beforeAll(async () => {
  await initI18n()
})

const row = { id: 1, model: 'gpt-ui-audit-long-model-name-example', group: 'A long audit group', cost: '$0.001' }
const columns: Column[] = [
  { key: 'model', label: 'Model', sticky: 'left' },
  { key: 'group', label: 'Group' },
  { key: 'cost', label: 'Actual cost' },
  { key: 'actions', label: 'Actions', sticky: 'right' }
]

/**
 * User tables must retain all selected fields without horizontal scrolling,
 * keep original actions and selection working, and leave admin table pinning
 * and presentation unchanged. Geometry is independent input.
 */
function fitGeometry(initialAvailable: number, initialContent: number) {
  let available = initialAvailable
  let content = initialContent
  const observers: Array<{ callback: ResizeObserverCallback; targets: Set<Element> }> = []
  vi.stubGlobal(
    'ResizeObserver',
    class {
      targets = new Set<Element>()
      constructor(public callback: ResizeObserverCallback) {
        observers.push(this)
      }
      observe(element: Element) {
        this.targets.add(element)
      }
      unobserve(element: Element) {
        this.targets.delete(element)
      }
      disconnect() {
        this.targets.clear()
      }
    }
  )
  vi.stubGlobal(
    'matchMedia',
    vi.fn((query: string) => ({
      matches: true,
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn()
    }))
  )
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
    const width = this.matches('table')
      ? Math.max(content, available)
      : this.classList.contains('user-record-cards')
        ? available + 24
        : 80
    return new DOMRect(0, 0, width, 48)
  })

  return {
    setAvailable(value: number) {
      available = value
    },
    setContent(value: number) {
      content = value
    },
    async resize() {
      for (const observer of [...observers]) {
        if (observer.targets.size) observer.callback([], observer as unknown as ResizeObserver)
      }
      await waitFor(() => {})
    },
    restore() {
      vi.restoreAllMocks()
      vi.unstubAllGlobals()
    }
  }
}

function renderUserTable(props: Partial<React.ComponentProps<typeof DataTable>> = {}) {
  return render(
    <I18nextProvider i18n={i18next}>
      <UserTableLayoutProvider value>
        <DataTable
          columns={columns}
          data={[row]}
          selectable
          renderCell={(key, tableRow) => {
            if (key === 'actions') {
              return (
                <div className="table-actions">
                  <button type="button">View</button>
                  <button type="button">Edit</button>
                </div>
              )
            }
            return String((tableRow as typeof row)[key as 'model'])
          }}
          {...props}
        />
      </UserTableLayoutProvider>
    </I18nextProvider>
  )
}

describe('DataTable user fit layout', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('uses cards only when the table cannot fit, retains fields and selection, then restores a table on growth', async () => {
    const geometry = fitGeometry(1200, 1000)
    const onSelectedKeysChange = vi.fn()
    try {
      renderUserTable({ onSelectedKeysChange })
      await waitFor(() => {
        expect(document.querySelector('table')).not.toBeNull()
      })
      expect(document.querySelectorAll('.sticky-col')).toHaveLength(0)

      geometry.setAvailable(800)
      await geometry.resize()
      await waitFor(() => {
        expect(document.querySelector('table')).toBeNull()
      })
      const cards = document.querySelector('.user-record-cards') as HTMLElement
      expect(cards).not.toBeNull()
      expect(document.querySelector('.user-record-fields')).not.toBeNull()
      expect(document.querySelector('.user-record-selection')).not.toBeNull()
      expect(document.querySelector('.user-record-actions')).not.toBeNull()
      for (const text of [row.model, row.group, row.cost, 'Model', 'Group', 'Actual cost', 'View', 'Edit']) {
        expect(cards.textContent).toContain(text)
      }

      const user = userEvent.setup()
      await user.click(document.querySelector('[data-testid="select-row"]') as HTMLElement)
      expect(onSelectedKeysChange.mock.calls.at(-1)?.[0]).toEqual([1])

      // Repeated resize notifications must not toggle a too-wide table back in.
      await geometry.resize()
      expect(document.querySelector('.user-record-cards')).toBe(cards)
      expect(document.querySelector('table')).toBeNull()

      geometry.setAvailable(1500)
      await geometry.resize()
      await waitFor(() => {
        expect(document.querySelector('table')).not.toBeNull()
      })
      expect(document.querySelector('.user-record-cards')).toBeNull()
      expect(document.querySelector('.data-table-fit-probe')).toBeNull()
      expect(document.querySelectorAll('.sticky-col')).toHaveLength(0)
    } finally {
      geometry.restore()
    }
  })

  it('keeps card state for identical polling results and remeasures changed content without a permanent probe', async () => {
    const geometry = fitGeometry(800, 1400)
    try {
      const { rerender } = renderUserTable()
      await waitFor(() => {
        expect(document.querySelector('.user-record-cards')).not.toBeNull()
      })
      const cards = document.querySelector('.user-record-cards') as HTMLElement

      rerender(
        <I18nextProvider i18n={i18next}>
          <UserTableLayoutProvider value>
            <DataTable
              columns={columns}
              data={[{ ...row }]}
              renderCell={(key, tableRow) => String((tableRow as typeof row)[key as 'model'])}
            />
          </UserTableLayoutProvider>
        </I18nextProvider>
      )
      await waitFor(() => {})
      // Identical polling results keep the same card element (and its state).
      expect(document.querySelector('.user-record-cards')).toBe(cards)

      geometry.setContent(700)
      rerender(
        <I18nextProvider i18n={i18next}>
          <UserTableLayoutProvider value>
            <DataTable
              columns={columns}
              data={[{ ...row, model: 'Short model' }]}
              renderCell={(key, tableRow) => String((tableRow as typeof row)[key as 'model'])}
            />
          </UserTableLayoutProvider>
        </I18nextProvider>
      )
      await waitFor(() => {
        expect(document.querySelector('table')).not.toBeNull()
      })
      expect(document.querySelector('.user-record-cards')).toBeNull()
      expect(document.querySelector('.data-table-fit-probe')).toBeNull()
    } finally {
      geometry.restore()
    }
  })

  it('preserves the normal admin table and sticky columns when content is wider than its container', async () => {
    const geometry = fitGeometry(800, 1600)
    try {
      render(
        <I18nextProvider i18n={i18next}>
          <DataTable columns={columns} data={[row]} selectable />
        </I18nextProvider>
      )
      await waitFor(() => {
        expect(document.querySelector('table')).not.toBeNull()
      })
      await geometry.resize()
      expect(document.querySelector('table')).not.toBeNull()
      expect(document.querySelector('.user-record-cards')).toBeNull()
      expect(document.querySelector('thead [data-field="model"]')).toHaveClass('sticky-col-left')
      expect(document.querySelector('thead [data-field="actions"]')).toHaveClass('sticky-col-right')
      expect(document.querySelector('thead th:first-child')).toHaveClass('sticky-col-left')
    } finally {
      geometry.restore()
    }
  })
})
