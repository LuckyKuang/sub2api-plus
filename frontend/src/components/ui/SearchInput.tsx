import { useEffect, useRef, useState } from 'react'
import { Search, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Input } from './Input'

export interface SearchInputProps {
  value?: string
  onValueChange?: (value: string) => void
  onSearch?: (value: string) => void
  placeholder?: string
  className?: string
  disabled?: boolean
  autoFocus?: boolean
}

export function SearchInput({
  value,
  onValueChange,
  onSearch,
  placeholder = 'Search...',
  className,
  disabled,
  autoFocus
}: SearchInputProps) {
  const [inner, setInner] = useState(value ?? '')
  const inputRef = useRef<HTMLInputElement>(null)
  const isControlled = value !== undefined
  const current = isControlled ? value : inner

  useEffect(() => {
    if (isControlled) setInner(value as string)
  }, [isControlled, value])

  useEffect(() => {
    if (autoFocus) inputRef.current?.focus()
  }, [autoFocus])

  return (
    <div className={cn('relative w-full min-w-0', className)}>
      <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
      <Input
        ref={inputRef}
        value={current}
        disabled={disabled}
        placeholder={placeholder}
        onValueChange={(next) => {
          if (!isControlled) setInner(next)
          onValueChange?.(next)
        }}
        onKeyDown={(event) => {
          if (event.key === 'Enter') onSearch?.(current)
        }}
        className="pl-9 pr-9"
      />
      {current && (
        <button
          type="button"
          aria-label="Clear search"
          onClick={() => {
            if (!isControlled) setInner('')
            onValueChange?.('')
            onSearch?.('')
            inputRef.current?.focus()
          }}
          className="absolute right-2.5 top-1/2 -translate-y-1/2 rounded p-0.5 text-gray-400 transition-colors hover:text-gray-600 dark:hover:text-gray-200"
        >
          <X className="h-4 w-4" />
        </button>
      )}
    </div>
  )
}
