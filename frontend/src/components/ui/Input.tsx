import { forwardRef, useId } from 'react'
import { cn } from '@/lib/utils'

export interface InputProps
  extends Omit<React.InputHTMLAttributes<HTMLInputElement>, 'onChange' | 'value' | 'prefix'> {
  label?: string
  error?: string
  hint?: string
  value?: string | number | null
  onValueChange?: (value: string) => void
  prefix?: React.ReactNode
  suffix?: React.ReactNode
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { label, error, hint, value, onValueChange, prefix, suffix, className, id, disabled, readOnly, ...rest },
  ref
) {
  const generatedId = useId()
  const inputId = id || generatedId

  return (
    <div className="w-full">
      {label && (
        <label htmlFor={inputId} className="input-label mb-1.5 block">
          {label}
          {rest.required && <span className="text-red-500">*</span>}
        </label>
      )}
      <div className="relative">
        {prefix && (
          <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5 surface-muted">
            {prefix}
          </div>
        )}
        <input
          id={inputId}
          ref={ref}
          value={value ?? ''}
          disabled={disabled}
          readOnly={readOnly}
          aria-invalid={error ? 'true' : undefined}
          className={cn(
            'input w-full transition-all duration-200',
            prefix && 'pl-11',
            suffix && 'pr-11',
            error && 'input-error ring-2 ring-red-500/20',
            disabled && 'cursor-not-allowed opacity-60',
            className
          )}
          onChange={(event) => onValueChange?.(event.target.value)}
          {...rest}
        />
        {suffix && (
          <div className="absolute inset-y-0 right-0 flex items-center pr-3 surface-muted">{suffix}</div>
        )}
      </div>
      {error ? (
        <p className="input-error-text mt-1.5">{error}</p>
      ) : hint ? (
        <p className="input-hint mt-1.5">{hint}</p>
      ) : null}
    </div>
  )
})

export interface TextAreaProps extends Omit<React.TextareaHTMLAttributes<HTMLTextAreaElement>, 'onChange'> {
  label?: string
  error?: string
  hint?: string
  value?: string
  onValueChange?: (value: string) => void
}

export const TextArea = forwardRef<HTMLTextAreaElement, TextAreaProps>(function TextArea(
  { label, error, hint, value, onValueChange, className, id, ...rest },
  ref
) {
  const generatedId = useId()
  const areaId = id || generatedId

  return (
    <div className="w-full">
      {label && (
        <label htmlFor={areaId} className="input-label mb-1.5 block">
          {label}
          {rest.required && <span className="text-red-500">*</span>}
        </label>
      )}
      <textarea
        id={areaId}
        ref={ref}
        value={value ?? ''}
        aria-invalid={error ? 'true' : undefined}
        className={cn('input w-full', error && 'input-error ring-2 ring-red-500/20', className)}
        onChange={(event) => onValueChange?.(event.target.value)}
        {...rest}
      />
      {error ? (
        <p className="input-error-text mt-1.5">{error}</p>
      ) : hint ? (
        <p className="input-hint mt-1.5">{hint}</p>
      ) : null}
    </div>
  )
})
