import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Check, ChevronDown, Loader2, Plus, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Icon } from '@/components/Icon'

export interface SelectOption {
  value: string | number | boolean | null
  label: string
  disabled?: boolean
  [key: string]: unknown
}

type Option = SelectOption | Record<string, unknown>
type Value = SelectOption['value']

const DROPDOWN_MIN_WIDTH = 200
const DROPDOWN_MAX_WIDTH = 480

/**
 * The expanded menu is floored by the same width the trigger takes, so expanding
 * never shrinks below the unified control. Read the token so a stylesheet change
 * cannot silently desynchronise the floor.
 */
function resolveDropdownMinimumWidth(): number {
  if (typeof window === 'undefined') return DROPDOWN_MIN_WIDTH
  const raw = getComputedStyle(document.documentElement).getPropertyValue('--filter-control-width')
  const parsed = Number.parseFloat(raw)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : DROPDOWN_MIN_WIDTH
}

export interface SearchableSelectProps {
  value?: Value | undefined
  /** Accepts any option-shaped object; only `valueKey`/`labelKey` are read. */
  options: readonly unknown[]
  placeholder?: string
  disabled?: boolean
  error?: boolean
  searchable?: boolean | 'auto'
  searchPlaceholder?: string
  emptyText?: string
  valueKey?: string
  labelKey?: string
  creatable?: boolean
  creatablePrefix?: string
  clearable?: boolean
  id?: string
  ariaLabel?: string
  ariaDescribedby?: string
  remote?: boolean
  loading?: boolean
  triggerClass?: string
  className?: string
  renderOption?: (option: Option, state: { selected: boolean }) => React.ReactNode
  renderSelected?: (option: Option | null) => React.ReactNode
  onValueChange?: (value: Value) => void
  onChange?: (value: Value, option: Option | null) => void
  onSearch?: (query: string) => void
}

function readField(option: Option, key: string): unknown {
  return (option as Record<string, unknown>)[key]
}

export function SearchableSelect({
  value,
  options,
  placeholder = 'Select...',
  disabled = false,
  error = false,
  searchable = 'auto',
  searchPlaceholder = 'Search...',
  emptyText = 'No options',
  valueKey = 'value',
  labelKey = 'label',
  creatable = false,
  creatablePrefix = '',
  clearable = false,
  id,
  ariaLabel,
  ariaDescribedby,
  remote = false,
  loading = false,
  triggerClass,
  className,
  renderOption,
  renderSelected,
  onValueChange,
  onChange,
  onSearch
}: SearchableSelectProps) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [focusedIndex, setFocusedIndex] = useState(-1)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const dropdownRef = useRef<HTMLDivElement>(null)
  const searchInputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  const items = useMemo(() => options as Option[], [options])

  const isSearchable =
    remote || creatable || searchable === true || (searchable === 'auto' && items.length > 5)

  const selectedOption = useMemo(() => {
    if (value === undefined || value === null) return null
    return items.find((option) => readField(option, valueKey) === value) ?? null
  }, [items, value, valueKey])

  const filteredOptions = useMemo(() => {
    if (remote || !query.trim()) return items
    const needle = query.trim().toLowerCase()
    return items.filter((option) => String(readField(option, labelKey) ?? '').toLowerCase().includes(needle))
  }, [items, query, labelKey, remote])

  const canCreate =
    creatable &&
    query.trim().length > 0 &&
    !items.some((option) => String(readField(option, labelKey) ?? '').toLowerCase() === query.trim().toLowerCase())

  const close = useCallback(() => {
    setOpen(false)
    setQuery('')
    setFocusedIndex(-1)
  }, [])

  const commit = useCallback(
    (next: Value, option: Option | null) => {
      onValueChange?.(next)
      onChange?.(next, option)
      close()
      triggerRef.current?.focus()
    },
    [close, onChange, onValueChange]
  )

  // Position the dropdown in a portal so it is never clipped by scroll ports.
  useLayoutEffect(() => {
    if (!open) return
    const place = () => {
      const trigger = triggerRef.current
      const dropdown = dropdownRef.current
      if (!trigger || !dropdown) return
      const rect = trigger.getBoundingClientRect()
      const dropdownHeight = dropdown.offsetHeight || 260
      const spaceBelow = window.innerHeight - rect.bottom
      const placeAbove = spaceBelow < dropdownHeight + 8 && rect.top > dropdownHeight + 8
      const floor = resolveDropdownMinimumWidth()
      // Vue sizes the menu to its content between a floor and a cap rather than
      // pinning it to the trigger width, so a long option label is never
      // clipped by the control. Mirror that with min/max and no explicit width.
      const minWidth = Math.min(Math.max(rect.width, floor), DROPDOWN_MAX_WIDTH)
      const viewportLeft = 8
      const viewportRight = Math.max(viewportLeft, window.innerWidth - 8)
      const left = Math.min(Math.max(viewportLeft, rect.left), Math.max(viewportLeft, viewportRight - minWidth))
      const availableWidth = Math.max(0, viewportRight - left)
      dropdown.style.minWidth = `${minWidth}px`
      dropdown.style.maxWidth = `${Math.min(DROPDOWN_MAX_WIDTH, availableWidth)}px`
      dropdown.style.width = ''
      dropdown.style.left = `${left}px`
      dropdown.style.top = placeAbove ? `${rect.top - dropdownHeight - 4}px` : `${rect.bottom + 4}px`
    }
    place()
    window.addEventListener('resize', place)
    window.addEventListener('scroll', place, true)
    return () => {
      window.removeEventListener('resize', place)
      window.removeEventListener('scroll', place, true)
    }
  }, [open, filteredOptions.length])

  useEffect(() => {
    if (!open) return
    const onPointerDown = (event: MouseEvent) => {
      const target = event.target as Node
      if (triggerRef.current?.contains(target) || dropdownRef.current?.contains(target)) return
      close()
    }
    document.addEventListener('mousedown', onPointerDown)
    return () => document.removeEventListener('mousedown', onPointerDown)
  }, [open, close])

  useEffect(() => {
    if (open && isSearchable) {
      searchInputRef.current?.focus()
    }
  }, [open, isSearchable])

  useEffect(() => {
    if (!open) return
    const active = listRef.current?.querySelector<HTMLElement>('[data-select-option-focused="true"]')
    active?.scrollIntoView({ block: 'nearest' })
  }, [open, focusedIndex])

  const totalRows = filteredOptions.length + (canCreate ? 1 : 0)

  const onTriggerKeyDown = (event: React.KeyboardEvent) => {
    if (disabled) return
    if (event.key === 'Enter' || event.key === ' ' || event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      setOpen(true)
      setFocusedIndex(0)
    } else if (event.key === 'Escape') {
      close()
    }
  }

  const onListKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      setFocusedIndex((index) => (totalRows === 0 ? -1 : (index + 1) % totalRows))
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      setFocusedIndex((index) => (totalRows === 0 ? -1 : (index - 1 + totalRows) % totalRows))
    } else if (event.key === 'Enter') {
      event.preventDefault()
      if (canCreate && focusedIndex === filteredOptions.length) {
        commit(creatablePrefix ? `${creatablePrefix}${query.trim()}` : query.trim(), null)
        return
      }
      const option = filteredOptions[focusedIndex]
      if (option) commit(readField(option, valueKey) as Value, option)
    } else if (event.key === 'Escape') {
      event.preventDefault()
      close()
      triggerRef.current?.focus()
    } else if (event.key === 'Tab') {
      close()
    }
  }

  return (
    <>
      <div className="relative w-full min-w-0">
        <button
          ref={triggerRef}
          id={id}
          type="button"
          disabled={disabled}
          aria-haspopup="listbox"
          aria-expanded={open}
          aria-label={ariaLabel}
          aria-describedby={ariaDescribedby}
          onClick={() => {
            if (disabled) return
            if (open) {
              close()
              return
            }
            setOpen(true)
            setFocusedIndex(Math.max(0, items.findIndex((option) => readField(option, valueKey) === value)))
          }}
          onKeyDown={onTriggerKeyDown}
          className={cn(
            'select-trigger',
            'flex items-center justify-between gap-2',
            error && 'select-trigger-error',
            open && 'select-trigger-open',
            disabled && 'select-trigger-disabled',
            triggerClass,
            className
          )}
        >
          <span className={cn('min-w-0 flex-1 truncate text-left', !selectedOption && 'text-gray-500 dark:text-dark-400')}>
            {selectedOption ? (
              renderSelected ? (
                renderSelected(selectedOption)
              ) : (
                String(readField(selectedOption, labelKey) ?? '')
              )
            ) : (
              placeholder
            )}
          </span>
          <span className="flex shrink-0 items-center gap-1">
            {loading && <Loader2 className="h-3.5 w-3.5 animate-spin text-gray-400" />}
            {clearable && selectedOption && !disabled && (
              <span
                role="button"
                tabIndex={-1}
                aria-label="Clear"
                onClick={(event) => {
                  event.stopPropagation()
                  commit(null, null)
                }}
                className="rounded p-0.5 text-gray-400 transition-colors hover:text-gray-600 dark:hover:text-gray-200"
              >
                <X className="h-3.5 w-3.5" />
              </span>
            )}
            <ChevronDown className={cn('h-5 w-5 text-gray-400 transition-transform duration-200', open && 'rotate-180')} />
          </span>
        </button>
      </div>

      {open &&
        createPortal(
          <div
            ref={dropdownRef}
            className="select-dropdown-portal surface-panel fixed z-[80] max-h-72 overflow-hidden rounded-xl border"
            style={{ top: 0, left: 0 }}
            onKeyDown={onListKeyDown}
          >
            {isSearchable && (
              <div className="select-search">
                <Icon name="search" size="sm" className="text-gray-400" />
                <input
                  ref={searchInputRef}
                  value={query}
                  onChange={(event) => {
                    setQuery(event.target.value)
                    setFocusedIndex(0)
                    onSearch?.(event.target.value)
                  }}
                  type="text"
                  placeholder={searchPlaceholder}
                  aria-label={searchPlaceholder}
                  className="select-search-input"
                />
              </div>
            )}
            <div ref={listRef} className="select-options" role="listbox">
              {filteredOptions.length === 0 && !canCreate && (
                <div className="px-3 py-6 text-center text-sm text-gray-500 dark:text-dark-400">
                  {loading ? searchPlaceholder : emptyText}
                </div>
              )}
              {filteredOptions.map((option, index) => {
                const selected = readField(option, valueKey) === value
                return (
                  <div
                    key={String(readField(option, valueKey))}
                    role="option"
                    aria-selected={selected}
                    data-select-option-focused={index === focusedIndex ? 'true' : 'false'}
                    onMouseEnter={() => setFocusedIndex(index)}
                    onClick={() => {
                      if (readField(option, 'disabled')) return
                      commit(readField(option, valueKey) as Value, option)
                    }}
                    className={cn(
                      'select-option flex cursor-pointer items-center gap-2 text-sm',
                      (readField(option, 'disabled') as boolean | undefined) && 'select-option-disabled'
                    )}
                  >
                    <span className="select-option-label">
                      {renderOption ? renderOption(option, { selected }) : String(readField(option, labelKey) ?? '')}
                    </span>
                    {selected && <Check className="h-4 w-4 shrink-0 text-primary-500" />}
                  </div>
                )
              })}
              {canCreate && (
                <div
                  role="option"
                  aria-selected={false}
                  data-select-option-focused={focusedIndex === filteredOptions.length ? 'true' : 'false'}
                  onMouseEnter={() => setFocusedIndex(filteredOptions.length)}
                  onClick={() => commit(creatablePrefix ? `${creatablePrefix}${query.trim()}` : query.trim(), null)}
                  className="select-option flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-sm"
                >
                  <Plus className="h-4 w-4 shrink-0" />
                  <span className="min-w-0 flex-1 truncate">{query.trim()}</span>
                </div>
              )}
            </div>
          </div>,
          document.body
        )}
    </>
  )
}

export interface SelectProps extends Omit<SearchableSelectProps, 'searchable'> {
  searchable?: boolean | 'auto'
}

export function Select({ searchable = 'auto', ...rest }: SelectProps) {
  return <SearchableSelect searchable={searchable} {...rest} />
}
