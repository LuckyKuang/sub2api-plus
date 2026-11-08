import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Icon } from '@/components/Icon'
import { Select } from './Select'
import {
  getConfiguredTablePageSizeOptions,
  normalizeTablePageSize
} from '@/utils/tablePreferences'
import { setPersistedPageSize } from '@/composables/usePersistedPageSize'

export interface PaginationProps {
  page: number
  pageSize: number
  total: number
  onPageChange?: (page: number) => void
  onPageSizeChange?: (pageSize: number) => void
  pageSizeOptions?: number[]
  showJump?: boolean
  className?: string
}

const MAX_VISIBLE_PAGES = 7

export function Pagination({
  page,
  pageSize,
  total,
  onPageChange,
  onPageSizeChange,
  pageSizeOptions,
  showJump = false,
  className
}: PaginationProps) {
  const { t } = useTranslation()
  const [jumpPage, setJumpPage] = useState('')

  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const fromItem = total === 0 ? 0 : (page - 1) * pageSize + 1
  const toItem = Math.min(page * pageSize, total)

  const pageSizeSelectOptions = Array.from(
    new Set([...getConfiguredTablePageSizeOptions(), normalizeTablePageSize(pageSize)])
  )
    .sort((a, b) => a - b)
    .map((size) => ({ value: size, label: String(size) }))

  const goToPage = (next: number) => {
    if (next >= 1 && next <= totalPages && next !== page) onPageChange?.(next)
  }

  const handlePageSizeChange = (value: string | number | boolean | null) => {
    if (value === null || typeof value === 'boolean') return
    const next = normalizeTablePageSize(typeof value === 'string' ? Number.parseInt(value, 10) : value)
    setPersistedPageSize(next)
    onPageSizeChange?.(next)
  }

  const submitJump = () => {
    const raw = jumpPage.trim()
    if (!raw) return
    const parsed = Number.parseInt(raw, 10)
    if (Number.isNaN(parsed)) return
    setJumpPage('')
    goToPage(Math.min(Math.max(parsed, 1), totalPages))
  }

  return (
    <div className={cn('pagination-card pagination-container card min-w-0 px-4 py-3 sm:px-6', className)}>
      {/* Mobile: previous / "page X of Y" / next */}
      <div className="pagination-mobile">
        <PageButton onClick={() => goToPage(page - 1)} disabled={page <= 1} className="px-4" label={t('pagination.previous')}>
          {t('pagination.previous')}
        </PageButton>
        <span className="pagination-page-label text-sm surface-muted">
          {t('pagination.pageOf', { page, total: totalPages })}
        </span>
        <PageButton onClick={() => goToPage(page + 1)} disabled={page >= totalPages} className="px-4" label={t('pagination.next')}>
          {t('pagination.next')}
        </PageButton>
      </div>

      <div className="pagination-desktop">
        <div className="pagination-info flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2">
          <p className="whitespace-nowrap text-sm surface-muted">
            {t('pagination.showing')} <span className="font-medium">{fromItem}</span> {t('pagination.to')}{' '}
            <span className="font-medium">{toItem}</span> {t('pagination.of')}{' '}
            <span className="font-medium">{total}</span> {t('pagination.results')}
          </p>

          {onPageSizeChange && (
            <div className="flex shrink-0 items-center space-x-2 whitespace-nowrap">
              <span className="text-sm surface-muted">{t('pagination.perPage')}:</span>
              <div className="page-size-select w-24">
                <Select
                  value={pageSize}
                  options={pageSizeOptions ? pageSizeOptions.map((size) => ({ value: size, label: String(size) })) : pageSizeSelectOptions}
                  searchable={false}
                  ariaLabel={t('pagination.perPage')}
                  onValueChange={handlePageSizeChange}
                />
              </div>
            </div>
          )}

          {showJump && (
            <div className="flex shrink-0 items-center space-x-2 whitespace-nowrap">
              <span className="text-sm surface-muted">{t('pagination.jumpTo')}</span>
              <input
                value={jumpPage}
                onChange={(event) => setJumpPage(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') submitJump()
                }}
                type="number"
                min={1}
                max={totalPages}
                className="input w-20 text-sm"
                placeholder={t('pagination.jumpPlaceholder')}
                aria-label={t('pagination.jumpTo')}
              />
              <button type="button" className="btn btn-ghost btn-sm" onClick={submitJump}>
                {t('pagination.jumpAction')}
              </button>
            </div>
          )}
        </div>

        <nav
          className="relative z-0 inline-flex flex-wrap items-center justify-end gap-1.5 self-end"
          aria-label="Pagination"
        >
          <PageButton onClick={() => goToPage(page - 1)} disabled={page <= 1} label={t('pagination.previous')}>
            <Icon name="chevronLeft" size="md" />
          </PageButton>

          {visiblePages(page, totalPages).map((entry, index) =>
            typeof entry === 'number' ? (
              <PageButton
                key={`${entry}-${index}`}
                active={entry === page}
                onClick={() => goToPage(entry)}
                label={t('pagination.goToPage', { page: entry })}
                ariaCurrent={entry === page}
                className="px-3"
              >
                {entry}
              </PageButton>
            ) : (
              <span key={`gap-${index}`} className="px-1.5 text-sm text-gray-400">
                …
              </span>
            )
          )}

          <PageButton onClick={() => goToPage(page + 1)} disabled={page >= totalPages} label={t('pagination.next')}>
            <Icon name="chevronRight" size="md" />
          </PageButton>
        </nav>
      </div>
    </div>
  )
}

function PageButton({
  children,
  onClick,
  disabled,
  active,
  label,
  ariaCurrent,
  className
}: {
  children?: React.ReactNode
  onClick: () => void
  disabled?: boolean
  active?: boolean
  label: string
  ariaCurrent?: boolean
  className?: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      aria-current={ariaCurrent ? 'page' : undefined}
      className={cn(
        'pagination-button relative inline-flex items-center border text-sm font-medium',
        'disabled:cursor-not-allowed disabled:opacity-50',
        active
          ? 'z-10 border-primary-500 bg-primary-50/[0.15] text-primary-600 dark:bg-primary-900/30 dark:text-primary-400'
          : 'border-gray-300 surface-inset text-gray-700 hover:surface-hover dark:border-dark-600 dark:surface-inset dark:text-gray-300 dark:hover:surface-hover',
        className
      )}
    >
      {children}
    </button>
  )
}

/** Build a compact page list with ellipses, e.g. 1 … 4 5 6 … 20 */
function visiblePages(page: number, totalPages: number): Array<number | '…'> {
  if (totalPages <= MAX_VISIBLE_PAGES) {
    return Array.from({ length: totalPages }, (_, index) => index + 1)
  }

  const pages: Array<number | '…'> = [1]
  const start = Math.max(2, page - 2)
  const end = Math.min(totalPages - 1, page + 2)

  if (start > 2) pages.push('…')
  for (let value = start; value <= end; value += 1) pages.push(value)
  if (end < totalPages - 1) pages.push('…')
  pages.push(totalPages)
  return pages
}

export default Pagination
