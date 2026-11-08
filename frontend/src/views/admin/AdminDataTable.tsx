import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Icon } from '@/components/Icon'

/**
 * Stopgap data table used by the admin console views until the shared
 * `@/components/DataTable` lands. It reproduces the desktop markup and the
 * `record-table` / `table-*` classes the Vue DataTable rendered so the ported
 * views keep identical structure and styling.
 */
export interface Column {
  key: string
  label: string
  sortable?: boolean
  class?: string
  sticky?: 'left' | 'right'
}

export type SortOrder = 'asc' | 'desc'

export interface AdminDataTableProps<T> {
  columns: Column[]
  data: T[]
  loading?: boolean
  rowKey?: string
  selectable?: boolean
  selectedKeys?: Array<string | number>
  selectionLabel?: (row: T) => string
  actionsCount?: number
  serverSideSort?: boolean
  sortKey?: string
  sortOrder?: SortOrder
  onSort?: (key: string, order: SortOrder) => void
  onSelectedKeysChange?: (keys: Array<string | number>) => void
  renderCell: (key: string, row: T, value: unknown) => React.ReactNode
  renderHeader?: (key: string, column: Column) => React.ReactNode
  empty?: React.ReactNode
  className?: string
}

function resolveRowKey<T>(row: T, index: number, rowKey?: string): string | number {
  if (rowKey) {
    const value = (row as Record<string, unknown>)[rowKey]
    if (value !== undefined && value !== null) return String(value)
  }
  return index
}

/** 根据列数自适应调整内边距 — mirrors DataTable.vue:1015. */
function getAdaptivePaddingClass(columnCount: number): string {
  if (columnCount >= 10) return 'px-2'
  if (columnCount >= 7) return 'px-3'
  if (columnCount >= 5) return 'px-4'
  return 'px-6'
}

/** Mirrors DataTable.vue:979 — a column sticks only when it declares a side. */
function getStickyColumnSide(column: Column): 'left' | 'right' | undefined {
  return column.sticky
}

function SortIndicator({ state }: { state: 'asc' | 'desc' | 'none' }) {
  if (state === 'none') return null
  return (
    <span className="inline-flex h-5 w-4 flex-col items-center justify-center" aria-hidden="true">
      <svg className={cn('h-2.5 w-2.5', state === 'asc' ? 'text-primary-500' : 'text-gray-300')} fill="currentColor" viewBox="0 0 10 10">
        <path d="M5 2L1.5 6.5h7L5 2z" />
      </svg>
      <svg className={cn('-mt-0.5 h-2.5 w-2.5', state === 'desc' ? 'text-primary-500' : 'text-gray-300')} fill="currentColor" viewBox="0 0 10 10">
        <path d="M5 8L1.5 3.5h7L5 8z" />
      </svg>
    </span>
  )
}

export function AdminDataTable<T>({
  columns,
  data,
  loading = false,
  rowKey,
  selectable = false,
  selectedKeys,
  selectionLabel,
  sortKey,
  sortOrder,
  onSort,
  onSelectedKeysChange,
  renderCell,
  renderHeader,
  empty,
  className
}: AdminDataTableProps<T>) {
  const { t } = useTranslation()
  const selectedSet = useMemo(() => new Set((selectedKeys ?? []).map((key) => String(key))), [selectedKeys])
  const allVisibleSelected = data.length > 0 && data.every((row, index) => selectedSet.has(String(resolveRowKey(row, index, rowKey))))
  const someVisibleSelected = data.some((row, index) => selectedSet.has(String(resolveRowKey(row, index, rowKey))))
  const tableColumnCount = columns.length + (selectable ? 1 : 0)

  const toggleAllVisible = (checked: boolean) => {
    if (!onSelectedKeysChange) return
    const keys = data.map((row, index) => resolveRowKey(row, index, rowKey))
    if (checked) {
      const next = new Set(selectedKeys ?? [])
      for (const key of keys) next.add(key)
      onSelectedKeysChange([...next])
    } else {
      const remove = new Set(keys)
      onSelectedKeysChange((selectedKeys ?? []).filter((key) => !remove.has(String(key))))
    }
  }

  const toggleRowSelection = (row: T, index: number, checked: boolean) => {
    if (!onSelectedKeysChange) return
    const key = resolveRowKey(row, index, rowKey)
    onSelectedKeysChange(
      checked ? [...(selectedKeys ?? []), key] : (selectedKeys ?? []).filter((entry) => String(entry) !== String(key))
    )
  }

  // Sticky columns need their measured offset, recomputed whenever the column
  // set changes — mirrors the Vue `computeStickyOffsets` watcher.
  const tableRef = useRef<HTMLTableElement | null>(null)
  const [stickyOffsets, setStickyOffsets] = useState<Record<string, number>>({})
  const columnsSignature = columns.map((column) => column.key).join('|')

  useLayoutEffect(() => {
    const table = tableRef.current
    if (!table) return
    const hasSticky = columns.some((column) => getStickyColumnSide(column))
    if (!hasSticky) {
      setStickyOffsets((current) => (Object.keys(current).length ? {} : current))
      return
    }
    const offsets: Record<string, number> = {}
    let leftAccum = 0
    let rightAccum = 0
    for (const column of columns) {
      const cell = table.querySelector<HTMLElement>(`th[data-field="${column.key}"]`)
      const width = cell?.getBoundingClientRect().width ?? 0
      const side = getStickyColumnSide(column)
      if (side === 'left') {
        offsets[column.key] = leftAccum
        leftAccum += width
      } else if (side === 'right') {
        rightAccum += width
        offsets[column.key] = rightAccum
      }
    }
    setStickyOffsets((current) =>
      JSON.stringify(current) === JSON.stringify(offsets) ? current : offsets
    )
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [columnsSignature, data.length])

  const stickyStyle = (column: Column) => {
    const side = getStickyColumnSide(column)
    return side ? { [side]: `${stickyOffsets[column.key] ?? 0}px` } : undefined
  }
  const stickyClass = (column: Column) => {
    const side = getStickyColumnSide(column)
    return side ? `sticky-col sticky-col-${side}` : ''
  }

  const adaptivePadding = getAdaptivePaddingClass(columns.length)

  // Below 768px the Vue DataTable swaps the table for stacked record cards.
  const [isDesktopViewport, setIsDesktopViewport] = useState(
    () => typeof window === 'undefined' || window.matchMedia('(min-width: 768px)').matches
  )
  useEffect(() => {
    const query = window.matchMedia('(min-width: 768px)')
    const onChange = (event: MediaQueryListEvent) => setIsDesktopViewport(event.matches)
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [])

  const hasActionsColumn = columns.some((column) => column.key === 'actions')
  const dataColumns = hasActionsColumn ? columns.filter((column) => column.key !== 'actions') : columns

  if (!isDesktopViewport) {
    return (
      <div className={cn('record-cards space-y-3', className)} aria-busy={loading}>
        {loading && data.length === 0 && (
          Array.from({ length: 5 }).map((_, rowIndex) => (
            <div key={`skeleton-${rowIndex}`} className="glass-inset rounded-2xl border border-gray-200 p-4 dark:border-dark-700">
              <div className="space-y-3">
                {dataColumns.map((column) => (
                  <div key={column.key} className="flex justify-between">
                    <div className="h-4 w-20 animate-pulse rounded bg-gray-200 dark:bg-dark-700" />
                    <div className="h-4 w-32 animate-pulse rounded bg-gray-200 dark:bg-dark-700" />
                  </div>
                ))}
                {hasActionsColumn && (
                  <div className="border-t border-gray-200 pt-3 dark:border-dark-700">
                    <div className="h-8 w-full animate-pulse rounded bg-gray-200 dark:bg-dark-700" />
                  </div>
                )}
              </div>
            </div>
          ))
        )}

        {!(loading && data.length === 0) && data.length === 0 && (
          <div className="glass-inset rounded-2xl border border-gray-200 p-12 text-center dark:border-dark-700">
            {empty ?? (
              <div className="flex flex-col items-center">
                <Icon name="inbox" size="xl" className="mb-4 h-12 w-12 surface-muted" />
                <p className="text-lg font-medium text-gray-900 dark:text-gray-100">{t('empty.noData')}</p>
              </div>
            )}
          </div>
        )}

        {data.length > 0 && (
          <>
            {selectable && (
              <div className="flex items-center justify-end gap-2 px-1">
                <label className="flex items-center gap-2 text-sm font-medium surface-muted">
                  <input
                    type="checkbox"
                    className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800"
                    checked={allVisibleSelected}
                    ref={(element) => {
                      if (element) element.indeterminate = !allVisibleSelected && someVisibleSelected
                    }}
                    data-test="select-all-mobile"
                    onChange={(event) => toggleAllVisible(event.target.checked)}
                  />
                  <span>{t('common.selectAll')}</span>
                </label>
              </div>
            )}
            {data.map((row, rowIndex) => {
              const key = resolveRowKey(row, rowIndex, rowKey)
              const isSelected = selectedSet.has(String(key))
              return (
                <div
                  key={key}
                  className={cn(
                    'glass-inset rounded-2xl border border-gray-200 p-4 dark:border-dark-700',
                    isSelected && 'table-row-selected'
                  )}
                >
                  <div className="record-card-fields space-y-3">
                    {selectable && (
                      <div className="record-card-selection flex justify-end">
                        <input
                          type="checkbox"
                          className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800"
                          checked={isSelected}
                          aria-label={selectionLabel ? selectionLabel(row) : undefined}
                          data-test="select-row"
                          onChange={(event) => toggleRowSelection(row, rowIndex, event.target.checked)}
                        />
                      </div>
                    )}
                    {dataColumns.map((column) => (
                      <div key={column.key} data-field={column.key} className="flex min-w-0 items-start justify-between gap-4">
                        <span className="text-xs font-medium uppercase tracking-wider surface-muted">{column.label}</span>
                        <div className="min-w-0 max-w-full text-right text-sm text-gray-900 dark:text-gray-100">
                          {renderCell(column.key, row, (row as Record<string, unknown>)[column.key])}
                        </div>
                      </div>
                    ))}
                    {hasActionsColumn && (
                      <div className="record-card-actions border-t border-gray-200 pt-3 dark:border-dark-700">
                        {renderCell('actions', row, undefined)}
                      </div>
                    )}
                  </div>
                </div>
              )
            })}
          </>
        )}
      </div>
    )
  }

  return (
    <div className={cn('table-wrapper table-container record-table-scroll', className)} aria-busy={loading}>
      <table ref={tableRef} className="record-table w-full min-w-max divide-y divide-gray-200 dark:divide-dark-700">
        <thead className="table-header surface-inset">
          <tr>
            {selectable && (
              <th scope="col" className="sticky-header-cell w-11 min-w-11 px-3 py-3 text-center">
                <input
                  type="checkbox"
                  className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800"
                  checked={allVisibleSelected}
                  ref={(element) => {
                    if (element) element.indeterminate = !allVisibleSelected && someVisibleSelected
                  }}
                  aria-label="Select all"
                  data-test="select-all"
                  onChange={(event) => toggleAllVisible(event.target.checked)}
                />
              </th>
            )}
            {columns.map((column) => {
              const state: 'asc' | 'desc' | 'none' =
                column.sortable && sortKey === column.key ? (sortOrder === 'asc' ? 'asc' : 'desc') : 'none'
              return (
                <th
                  key={column.key}
                  data-field={column.key}
                  scope="col"
                  data-sticky={getStickyColumnSide(column)}
                  style={stickyStyle(column)}
                  aria-sort={column.sortable ? (state === 'none' ? 'none' : state === 'asc' ? 'ascending' : 'descending') : undefined}
                  className={cn(
                    'sticky-header-cell py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted',
                    adaptivePadding,
                    column.sortable && 'cursor-pointer hover:surface-hover dark:hover:surface-hover',
                    stickyClass(column),
                    column.class
                  )}
                  onClick={column.sortable ? () => onSort?.(column.key, state === 'asc' ? 'desc' : 'asc') : undefined}
                >
                  <div className="flex items-center space-x-1">
                    {renderHeader ? renderHeader(column.key, column) : <span>{column.label}</span>}
                    {column.sortable && <SortIndicator state={state} />}
                  </div>
                </th>
              )
            })}
          </tr>
        </thead>
        <tbody className="table-body divide-y divide-gray-200 dark:divide-dark-700">
          {loading && data.length === 0 && (
            Array.from({ length: 5 }).map((_, rowIndex) => (
              <tr key={`skeleton-${rowIndex}`}>
                {selectable && (
                  <td className="w-11 min-w-11 px-3 py-4">
                    <div className="mx-auto h-4 w-4 animate-pulse rounded bg-gray-200 dark:bg-dark-700" />
                  </td>
                )}
                {columns.map((column) => (
                  <td key={column.key} className={cn('whitespace-nowrap py-4', adaptivePadding)}>
                    <div className="animate-pulse">
                      <div className="h-4 w-3/4 rounded bg-gray-200 dark:bg-dark-700" />
                    </div>
                  </td>
                ))}
              </tr>
            ))
          )}

          {!(loading && data.length === 0) && data.length === 0 && (
            <tr>
              <td colSpan={tableColumnCount} className={cn('empty-state-cell', adaptivePadding)}>
                <div className="empty-state-fill surface-muted">
                  {empty ?? (
                    <div className="flex flex-col items-center">
                      <p className="text-lg font-medium text-gray-900 dark:text-gray-100">No data</p>
                    </div>
                  )}
                </div>
              </td>
            </tr>
          )}

          {data.map((row, rowIndex) => {
            const key = resolveRowKey(row, rowIndex, rowKey)
            const isSelected = selectedSet.has(String(key))
            return (
              <tr
                key={key}
                data-row-id={key}
                data-index={rowIndex}
                className={cn('table-data-row', isSelected && 'table-row-selected')}
              >
                {selectable && (
                  <td className="w-11 min-w-11 px-3 py-4 text-center">
                    <input
                      type="checkbox"
                      className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800"
                      checked={isSelected}
                      aria-label={selectionLabel ? selectionLabel(row) : undefined}
                      data-test="select-row"
                      onChange={(event) => toggleRowSelection(row, rowIndex, event.target.checked)}
                    />
                  </td>
                )}
                {columns.map((column) => (
                  <td
                    key={column.key}
                    data-field={column.key}
                    data-sticky={getStickyColumnSide(column)}
                    style={stickyStyle(column)}
                    className={cn(
                      'whitespace-nowrap py-4 text-sm text-gray-900 dark:text-gray-100',
                      adaptivePadding,
                      stickyClass(column),
                      column.class
                    )}
                  >
                    {renderCell(column.key, row, (row as Record<string, unknown>)[column.key])}
                  </td>
                ))}
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

export default AdminDataTable
