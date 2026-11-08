import { useMemo } from 'react'
import { cn } from '@/lib/utils'

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

  return (
    <div className={cn('table-wrapper table-container record-table-scroll', className)} aria-busy={loading}>
      <table className="record-table w-full min-w-max divide-y divide-gray-200 dark:divide-dark-700">
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
                  aria-sort={column.sortable ? (state === 'none' ? 'none' : state === 'asc' ? 'ascending' : 'descending') : undefined}
                  className={cn(
                    'sticky-header-cell py-3 text-left text-xs font-medium uppercase tracking-wider surface-muted',
                    column.sortable && 'cursor-pointer hover:surface-hover dark:hover:surface-hover',
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
                  <td key={column.key} className="whitespace-nowrap py-4">
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
              <td colSpan={tableColumnCount} className="empty-state-cell">
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
                    className={cn(
                      'whitespace-nowrap py-4 text-sm text-gray-900 dark:text-gray-100',
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
