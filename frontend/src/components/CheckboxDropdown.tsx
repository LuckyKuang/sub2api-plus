import { useCallback, useEffect, useId, useRef, useState, type CSSProperties } from 'react'
import { createPortal } from 'react-dom'
import { cn } from '@/lib/utils'
import { getFloatingPanelPosition } from '@/utils/floatingPanel'

export interface CheckboxDropdownOption<T extends string | number> {
  value: T
  label: string
}

export interface CheckboxDropdownProps<T extends string | number> {
  value: T[]
  options: Array<CheckboxDropdownOption<T>>
  label: string
  disabled?: boolean
  className?: string
  onValueChange?: (value: T[]) => void
}

export function CheckboxDropdown<T extends string | number>({
  value,
  options,
  label,
  disabled = false,
  className,
  onValueChange
}: CheckboxDropdownProps<T>) {
  const reactId = useId()
  const panelID = `checkbox-dropdown-${reactId.replace(/:/g, '')}`
  const trigger = useRef<HTMLButtonElement | null>(null)
  const panel = useRef<HTMLDivElement | null>(null)
  const [open, setOpen] = useState(false)
  const openRef = useRef(open)
  openRef.current = open
  const [rect, setRect] = useState<DOMRect | null>(null)
  const valueRef = useRef(value)
  valueRef.current = value

  const updatePosition = useCallback(() => {
    if (openRef.current) setRect(trigger.current?.getBoundingClientRect() ?? null)
  }, [])

  useEffect(() => {
    window.addEventListener('resize', updatePosition)
    window.addEventListener('scroll', updatePosition, true)
    const onPointerDown = (event: MouseEvent) => {
      const target = event.target as Node | null
      if (
        openRef.current &&
        target &&
        !trigger.current?.contains(target) &&
        !panel.current?.contains(target)
      ) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', onPointerDown)
    return () => {
      window.removeEventListener('resize', updatePosition)
      window.removeEventListener('scroll', updatePosition, true)
      document.removeEventListener('mousedown', onPointerDown)
    }
  }, [updatePosition])

  useEffect(() => {
    if (disabled) setOpen(false)
  }, [disabled])

  const close = useCallback(() => {
    setOpen(false)
    trigger.current?.focus()
  }, [])

  const checkboxOptions = useCallback(
    () =>
      Array.from(
        panel.current?.querySelectorAll<HTMLInputElement>('input[type="checkbox"]:not(:disabled)') ?? []
      ),
    []
  )

  const openAndFocus = async (last = false) => {
    if (disabled) return
    setOpen(true)
    updatePosition()
    await new Promise((resolve) => requestAnimationFrame(resolve))
    if (!openRef.current) return
    const items = checkboxOptions()
    items[last ? items.length - 1 : 0]?.focus()
  }

  const toggleOpen = () => {
    if (open) close()
    else void openAndFocus()
  }

  const onPanelKeydown = (event: React.KeyboardEvent) => {
    if (event.nativeEvent.isComposing) return
    const items = checkboxOptions()
    const index = items.indexOf(document.activeElement as HTMLInputElement)
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      close()
    } else if (
      event.key === 'Tab' &&
      ((event.shiftKey && index === 0) || (!event.shiftKey && index === items.length - 1))
    ) {
      // Restore the trigger's place in the form's tab order before the browser
      // advances focus; the portal panel lives at the end of the document.
      if (event.shiftKey) event.preventDefault()
      close()
    } else if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key) && items.length) {
      event.preventDefault()
      const next =
        event.key === 'Home'
          ? 0
          : event.key === 'End'
            ? items.length - 1
            : (index + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length
      items[next]?.focus()
    } else if (event.key === 'Enter' && index >= 0) {
      event.preventDefault()
      items[index]?.click()
    }
  }

  const panelStyle: CSSProperties = (() => {
    if (!rect) return {}
    const position = getFloatingPanelPosition(rect, window.innerWidth, window.innerHeight, {
      maxWidth: rect.width,
      viewportPadding: 8,
      gap: 4
    })
    return {
      position: 'fixed' as const,
      zIndex: 100000020,
      width: `${position.width}px`,
      minWidth: '0',
      left: `${position.left}px`,
      top: position.top == null ? undefined : `${position.top}px`,
      bottom: position.bottom == null ? undefined : `${position.bottom}px`,
      maxHeight: `${position.maxHeight}px`
    }
  })()

  const selectedLabel = options
    .filter((option) => value.includes(option.value))
    .map((option) => option.label)
    .join(', ')

  const toggle = (optionValue: T, checked: boolean) => {
    onValueChange?.(checked ? [...valueRef.current, optionValue] : valueRef.current.filter((item) => item !== optionValue))
  }

  return (
    <div className={className}>
      <button
        ref={trigger}
        type="button"
        className={cn('select-trigger flex items-center gap-2', open && 'select-trigger-open')}
        aria-label={label}
        aria-expanded={open}
        aria-controls={open ? panelID : undefined}
        disabled={disabled}
        onClick={toggleOpen}
        onKeyDown={(event) => {
          if (event.key === 'ArrowDown') {
            event.preventDefault()
            void openAndFocus()
          } else if (event.key === 'ArrowUp') {
            event.preventDefault()
            void openAndFocus(true)
          } else if (event.key === 'Escape') {
            event.stopPropagation()
            event.preventDefault()
            setOpen(false)
          }
        }}
      >
        <span className="min-w-0 flex-1 truncate text-left">{selectedLabel || label}</span>
        <span className="shrink-0 surface-muted" aria-hidden="true">
          ⌄
        </span>
      </button>
      {open &&
        createPortal(
          <div
            id={panelID}
            ref={panel}
            className="select-dropdown-portal"
            style={panelStyle}
            onKeyDown={onPanelKeydown}
          >
            <div className="select-options">
              {options.map((option) => (
                <label
                  key={String(option.value)}
                  className={cn(
                    'select-option hover:surface-hover',
                    value.includes(option.value) && 'select-option-selected'
                  )}
                >
                  <input
                    className="shrink-0"
                    type="checkbox"
                    value={String(option.value)}
                    checked={value.includes(option.value)}
                    disabled={disabled}
                    onChange={(event) => toggle(option.value, event.target.checked)}
                  />
                  <span className="min-w-0 flex-1 break-words">{option.label}</span>
                </label>
              ))}
            </div>
          </div>,
          document.body
        )}
    </div>
  )
}
