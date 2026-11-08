import { useId } from 'react'
import { cn } from '@/lib/utils'

export interface CheckboxProps extends Omit<React.InputHTMLAttributes<HTMLInputElement>, 'onChange' | 'type'> {
  checked?: boolean
  indeterminate?: boolean
  onCheckedChange?: (checked: boolean) => void
  label?: React.ReactNode
}

export function Checkbox({
  checked = false,
  indeterminate = false,
  onCheckedChange,
  label,
  className,
  disabled,
  ...rest
}: CheckboxProps) {
  const id = useId()
  return (
    <div className={cn('flex items-center gap-2', className)}>
      <input
        id={id}
        type="checkbox"
        checked={checked}
        disabled={disabled}
        ref={(node) => {
          if (node) node.indeterminate = indeterminate && !checked
        }}
        onChange={(event) => onCheckedChange?.(event.target.checked)}
        className={cn(
          'h-4 w-4 shrink-0 rounded border-gray-300 dark:border-dark-600',
          'accent-[rgb(var(--console-accent-500,20_184_166))]',
          disabled && 'cursor-not-allowed opacity-60'
        )}
        {...rest}
      />
      {label && (
        <label htmlFor={id} className="cursor-pointer select-none text-sm text-gray-700 dark:text-gray-300">
          {label}
        </label>
      )}
    </div>
  )
}

export interface ToggleProps {
  checked?: boolean
  onCheckedChange?: (checked: boolean) => void
  disabled?: boolean
  label?: React.ReactNode
  className?: string
  id?: string
  /** Names the switch for assistive tech when no visible label is rendered. */
  ariaLabel?: string
  /** Test hook rendered as a `data-test` attribute on the switch. */
  dataTest?: string
}

export function Toggle({
  checked = false,
  onCheckedChange,
  disabled,
  label,
  className,
  id,
  ariaLabel,
  dataTest
}: ToggleProps) {
  const generatedId = useId()
  const toggleId = id || generatedId
  return (
    <div className={cn('flex items-center gap-2.5', className)}>
      <button
        id={toggleId}
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={ariaLabel}
        data-test={dataTest}
        disabled={disabled}
        onClick={() => onCheckedChange?.(!checked)}
        className={cn(
          'relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors duration-200',
          'focus:outline-none focus:ring-2 focus:ring-primary-500/50 focus:ring-offset-2',
          checked ? 'bg-primary-500' : 'bg-gray-300 dark:bg-dark-600',
          disabled && 'cursor-not-allowed opacity-60'
        )}
      >
        <span
          className={cn(
            'inline-block h-5 w-5 transform rounded-full bg-white shadow-sm transition-transform duration-200',
            checked ? 'translate-x-[22px]' : 'translate-x-0.5'
          )}
        />
      </button>
      {label && (
        <label htmlFor={toggleId} className="cursor-pointer select-none text-sm text-gray-700 dark:text-gray-300">
          {label}
        </label>
      )}
    </div>
  )
}
