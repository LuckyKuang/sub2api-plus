import { cn } from '@/lib/utils'

export interface PaginationProps {
  page: number
  pageSize: number
  total: number
  onPageChange?: (page: number) => void
  onPageSizeChange?: (pageSize: number) => void
  pageSizeOptions?: number[]
  className?: string
}

export function Pagination({
  page,
  pageSize,
  total,
  onPageChange,
  onPageSizeChange,
  pageSizeOptions = [10, 20, 50, 100],
  className
}: PaginationProps) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const from = total === 0 ? 0 : (page - 1) * pageSize + 1
  const to = Math.min(page * pageSize, total)

  const go = (next: number) => {
    const clamped = Math.min(Math.max(1, next), totalPages)
    if (clamped !== page) onPageChange?.(clamped)
  }

  const pages = usePageWindow(page, totalPages)

  return (
    <div className={cn('flex flex-wrap items-center justify-between gap-3 px-1 py-3', className)}>
      <div className="flex items-center gap-2 text-sm text-gray-500 dark:text-dark-400">
        <span>
          {from}-{to} / {total}
        </span>
        {onPageSizeChange && (
          <label className="flex items-center gap-1.5">
            <select
              value={pageSize}
              onChange={(event) => onPageSizeChange?.(Number(event.target.value))}
              className="input !w-auto py-1 text-xs"
              aria-label="Page size"
            >
              {pageSizeOptions.map((size) => (
                <option key={size} value={size}>
                  {size}
                </option>
              ))}
            </select>
          </label>
        )}
      </div>
      <div className="flex items-center gap-1">
        <PageButton disabled={page <= 1} onClick={() => go(1)} label="«" />
        <PageButton disabled={page <= 1} onClick={() => go(page - 1)} label="‹" />
        {pages.map((entry, index) =>
          entry === '…' ? (
            <span key={`gap-${index}`} className="px-1.5 text-sm text-gray-400">
              …
            </span>
          ) : (
            <PageButton
              key={entry}
              active={entry === page}
              onClick={() => go(entry as number)}
              label={String(entry)}
            />
          )
        )}
        <PageButton disabled={page >= totalPages} onClick={() => go(page + 1)} label="›" />
        <PageButton disabled={page >= totalPages} onClick={() => go(totalPages)} label="»" />
      </div>
    </div>
  )
}

function PageButton({
  label,
  onClick,
  disabled,
  active
}: {
  label: string
  onClick: () => void
  disabled?: boolean
  active?: boolean
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-current={active ? 'page' : undefined}
      className={cn(
        'min-w-[32px] rounded-lg px-2 py-1 text-sm transition-colors',
        'disabled:cursor-not-allowed disabled:opacity-40',
        active
          ? 'bg-primary-500 font-medium text-white'
          : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-800'
      )}
    >
      {label}
    </button>
  )
}

/** Build a compact page list with ellipses, e.g. 1 … 4 5 6 … 20 */
function usePageWindow(page: number, totalPages: number): Array<number | '…'> {
  if (totalPages <= 7) return Array.from({ length: totalPages }, (_, index) => index + 1)

  const pages: Array<number | '…'> = [1]
  const start = Math.max(2, page - 1)
  const end = Math.min(totalPages - 1, page + 1)

  if (start > 2) pages.push('…')
  for (let value = start; value <= end; value += 1) pages.push(value)
  if (end < totalPages - 1) pages.push('…')
  pages.push(totalPages)
  return pages
}
