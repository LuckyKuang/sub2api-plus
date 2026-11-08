import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type CSSProperties
} from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Icon } from './Icon'
import { useDialogFocus } from '@/composables/useDialogFocus'

type TimePart = 'hours' | 'minutes' | 'seconds'

export interface DateTimePickerProps {
  value?: string | null
  type?: 'date' | 'datetime-local' | 'time'
  min?: string
  max?: string
  step?: string | number
  disabled?: boolean
  readonly?: boolean
  required?: boolean
  name?: string
  id?: string
  placeholder?: string
  className?: string
  style?: CSSProperties
  'aria-label'?: string
  onValueChange?: (value: string) => void
  onInput?: (event: Event) => void
  onChange?: (event: Event) => void
}

function pad(value: number | string) {
  const [integer, decimals] = String(value).split('.')
  return `${integer!.padStart(2, '0')}${decimals === undefined ? '' : `.${decimals}`}`
}

function dateValue(date: Date) {
  return `${String(date.getFullYear()).padStart(4, '0')}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

function localDate(value: string): Date {
  const [year, month, day] = value.split('-').map(Number)
  const result = new Date(0)
  result.setFullYear(year, month - 1, day)
  result.setHours(12, 0, 0, 0)
  return result
}

function millisecondValue(value: string) {
  return value.replace(/\.(\d{1,2})$/, (_, digits: string) => `.${digits.padEnd(3, '0')}`)
}

function compareDates(left: string, right: string) {
  const a = left.split('T')[0]!.split('-').map(Number)
  const b = right.split('T')[0]!.split('-').map(Number)
  for (let index = 0; index < 3; index++) {
    const difference = a[index]! - b[index]!
    if (difference) return difference
  }
  return 0
}

export function DateTimePicker({
  value,
  type = 'datetime-local',
  min,
  max,
  step,
  disabled = false,
  readonly = false,
  required = false,
  name,
  id,
  placeholder,
  className,
  style,
  'aria-label': ariaLabel,
  onValueChange,
  onInput,
  onChange
}: DateTimePickerProps) {
  const { t, i18n } = useTranslation()
  const locale = i18n.language
  const reactId = useId()
  const panelId = `date-time-${reactId.replace(/:/g, '')}`

  const root = useRef<HTMLDivElement | null>(null)
  const trigger = useRef<HTMLButtonElement | null>(null)
  const panel = useRef<HTMLDivElement | null>(null)
  const valueInput = useRef<HTMLInputElement | null>(null)

  const [opened, setOpened] = useState(false)
  const openedRef = useRef(opened)
  openedRef.current = opened
  const restoreFocusRef = useRef(true)
  const [draftDate, setDraftDate] = useState('')
  const [focusedDate, setFocusedDate] = useState('')
  const [hours, setHours] = useState('00')
  const [minutes, setMinutes] = useState('00')
  const [seconds, setSeconds] = useState('00')
  const [monthYear, setMonthYear] = useState(() => new Date().getFullYear())
  const [monthIndex, setMonthIndex] = useState(() => new Date().getMonth())
  const [panelStyle, setPanelStyle] = useState<CSSProperties>({})

  const modelValue = value ?? ''
  const modelValueRef = useRef(modelValue)
  modelValueRef.current = modelValue

  const close = useCallback(() => {
    restoreFocusRef.current = true
    setOpened(false)
  }, [])

  useDialogFocus(
    opened,
    panel,
    close,
    () => true,
    { lockScroll: false, restoreFocus: () => restoreFocusRef.current }
  )

  const todayValue = dateValue(new Date())
  const placeholderText = placeholder || t(type === 'time' ? 'dates.selectTime' : type === 'date' ? 'dates.selectDate' : 'dates.selectDateTime')
  const displayValue = String(modelValue ?? '').replace('T', ' ')
  const includeSeconds =
    type !== 'date' &&
    (step === 'any' ||
      (Number(step) > 0 && Number(step) < 60) ||
      [modelValue, min, max].some((entry) => /:\d{2}:\d{2}/.test(`T${entry ?? ''}`)))
  const fractionalSeconds = step === 'any' || (Number(step) > 0 && Number(step) < 1)

  const timeParts: Array<{ key: TimePart; label: string; max: number | undefined }> = [
    { key: 'hours', label: t('dates.hours'), max: 23 },
    { key: 'minutes', label: t('dates.minutes'), max: 59 },
    ...(includeSeconds
      ? [{ key: 'seconds' as const, label: t('dates.seconds'), max: fractionalSeconds ? undefined : 59 }]
      : [])
  ]

  const monthDate = (() => {
    const d = new Date(0)
    d.setFullYear(monthYear, monthIndex, 1)
    return d
  })()
  const monthLabel = new Intl.DateTimeFormat(locale, { year: 'numeric', month: 'long' }).format(monthDate)
  const weekdays = Array.from(
    { length: 7 },
    (_, i) => new Intl.DateTimeFormat(locale, { weekday: 'short' }).format(new Date(2026, 5, 8 + i))
  )

  function dateDisabled(entry: string) {
    return Boolean(
      (min && compareDates(entry, min) < 0) || (max && compareDates(entry, max) > 0)
    )
  }

  const calendarDays = (() => {
    const first = localDate(dateValue(monthDate))
    first.setDate(1 - (first.getDay() + 6) % 7)
    return Array.from({ length: 42 }, (_, index) => {
      const day = new Date(first)
      day.setDate(first.getDate() + index)
      const entry = dateValue(day)
      return {
        value: entry,
        number: day.getDate(),
        outside: day.getMonth() !== monthIndex,
        disabled: day.getFullYear() < 1 || dateDisabled(entry),
        label: new Intl.DateTimeFormat(locale, {
          year: 'numeric',
          month: 'long',
          day: 'numeric'
        }).format(day)
      }
    })
  })()
  const calendarWeeks = Array.from({ length: 6 }, (_, index) =>
    calendarDays.slice(index * 7, index * 7 + 7)
  )

  const draftValue = (() => {
    if (type === 'date') return draftDate
    const clock = `${pad(Number(hours))}:${pad(Number(minutes))}${includeSeconds ? `:${pad(Number(seconds))}` : ''}`
    return type === 'time' ? clock : `${draftDate}T${clock}`
  })()

  // Let the browser enforce the original input's calendar, range and step rules,
  // without displaying its platform-specific popup or converting to UTC.
  const draftValid = (() => {
    if (
      type !== 'date' &&
      timeParts.some((part) => {
        const pattern =
          part.key === 'seconds' && fractionalSeconds ? /^\d+(?:\.\d+)?$/ : /^\d+$/
        const entry = timeValue(part.key)
        return (
          !pattern.test(entry) ||
          Number(entry) < 0 ||
          Number(entry) >= (part.key === 'hours' ? 24 : 60)
        )
      })
    ) {
      return false
    }
    const input = document.createElement('input')
    input.type = type
    // Compare fractional values in full milliseconds so equivalent .9 and .900
    // times use the same numeric range and step semantics in DOM implementations.
    if (min) input.min = millisecondValue(min)
    if (max) input.max = millisecondValue(max)
    if (step !== undefined) input.step = String(step)
    input.value = millisecondValue(draftValue)
    return Boolean(input.value) && input.validity.valid
  })()

  function timeValue(part: TimePart) {
    return ({ hours, minutes, seconds })[part]
  }

  function setTime(part: TimePart, entry: string) {
    const setter = { hours: setHours, minutes: setMinutes, seconds: setSeconds }[part]
    setter(entry)
  }

  function normalizeTime(part: TimePart) {
    const current = timeValue(part)
    if (current !== '') setTime(part, pad(Number(current)))
  }

  function adjustTime(part: TimePart, delta: number) {
    const limit = part === 'hours' ? 24 : 60
    const amount = part === 'seconds' && Number(step) > 0 && Number(step) < 60 ? Number(step) : 1
    // Native time values have millisecond precision; integer arithmetic avoids
    // introducing binary floating-point digits when a fractional second wraps.
    const units = limit * 1000
    const next = Math.round(Number(timeValue(part) || 0) * 1000) + delta * Math.round(amount * 1000)
    setTime(part, pad(((next % units + units) % units) / 1000))
  }

  function canMoveMonth(delta: number) {
    const date = new Date(monthDate)
    date.setMonth(date.getMonth() + delta)
    if (date.getFullYear() < 1) return false
    const first = dateValue(date)
    date.setMonth(date.getMonth() + 1, 0)
    return !(min && compareDates(dateValue(date), min) < 0) && !(max && compareDates(first, max) > 0)
  }

  function moveMonth(delta: number) {
    if (!canMoveMonth(delta)) return
    const date = new Date(monthDate)
    date.setMonth(date.getMonth() + delta)
    setMonthYear(date.getFullYear())
    setMonthIndex(date.getMonth())
    setFocusedDate(dateValue(date))
  }

  function selectDay(entry: string) {
    setDraftDate(entry)
    setFocusedDate(entry)
    const date = localDate(entry)
    setMonthYear(date.getFullYear())
    setMonthIndex(date.getMonth())
  }

  async function onDayKey(event: React.KeyboardEvent, entry: string) {
    const offsets: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 }
    const date = localDate(entry)
    if (event.key in offsets) date.setDate(date.getDate() + offsets[event.key]!)
    else if (event.key === 'Home') date.setDate(date.getDate() - (date.getDay() + 6) % 7)
    else if (event.key === 'End') date.setDate(date.getDate() + 6 - (date.getDay() + 6) % 7)
    else if (event.key === 'PageUp' || event.key === 'PageDown') {
      const day = date.getDate()
      date.setDate(1)
      date.setMonth(date.getMonth() + (event.key === 'PageUp' ? -1 : 1))
      const lastDay = new Date(date)
      lastDay.setMonth(lastDay.getMonth() + 1, 0)
      date.setDate(Math.min(day, lastDay.getDate()))
    } else return
    event.preventDefault()
    const next = dateValue(date)
    if (date.getFullYear() < 1 || dateDisabled(next)) return
    setFocusedDate(next)
    setMonthYear(date.getFullYear())
    setMonthIndex(date.getMonth())
    requestAnimationFrame(() => {
      panel.current?.querySelector<HTMLButtonElement>(`[data-date="${next}"]`)?.focus()
    })
  }

  function initializeDraft() {
    const current = modelValueRef.current ?? ''
    const datePart = type === 'time' ? '' : current.split('T')[0]
    const clock = type === 'time' ? current : current.split('T')[1] || ''
    const match = /^(\d{2}):(\d{2})(?::(\d{2})(\.\d+)?)?$/.exec(clock)
    setHours(match?.[1] ?? '00')
    setMinutes(match?.[2] ?? '00')
    setSeconds(`${match?.[3] ?? '00'}${match?.[4] ?? ''}`)
    let date = datePart && /^\d{4,}-\d{2}-\d{2}$/.test(datePart) ? datePart : todayValue
    if (type !== 'time' && dateDisabled(date)) date = (min || max)?.split('T')[0] || date
    setDraftDate(date)
    setFocusedDate(date)
    const currentDate = localDate(date)
    setMonthYear(currentDate.getFullYear())
    setMonthIndex(currentDate.getMonth())
  }

  function editable() {
    return (
      !disabled &&
      !readonly &&
      !trigger.current?.disabled &&
      !valueInput.current?.readOnly &&
      !valueInput.current?.disabled
    )
  }

  function toggle() {
    if (opened) close()
    else void open()
  }

  async function open() {
    if (!editable() || opened) return
    initializeDraft()
    restoreFocusRef.current = true
    setOpened(true)
    requestAnimationFrame(positionPanel)
  }

  function commit(next: string) {
    if (!editable()) {
      close()
      return
    }
    const changed = next !== String(modelValueRef.current ?? '')
    close()
    if (!changed) return
    if (valueInput.current) valueInput.current.value = next
    onValueChange?.(next)
    for (const eventType of ['input', 'change'] as const) {
      const event = new Event(eventType, { bubbles: true })
      valueInput.current?.dispatchEvent(event)
      if (eventType === 'input') onInput?.(event)
      else onChange?.(event)
    }
  }

  function apply() {
    if (draftValid) commit(draftValue)
  }

  function onInvalid() {
    void open()
    trigger.current?.focus()
  }

  const positionPanel = useCallback(() => {
    if (!openedRef.current || !panel.current || !trigger.current) return
    const anchor = trigger.current.getBoundingClientRect()
    const popup = panel.current.getBoundingClientRect()
    const viewport = window.visualViewport
    const offsetLeft = viewport?.offsetLeft ?? 0
    const offsetTop = viewport?.offsetTop ?? 0
    const width = viewport?.width ?? window.innerWidth
    const height = viewport?.height ?? window.innerHeight
    const margin = 12
    const left = Math.max(offsetLeft + margin, Math.min(anchor.left, offsetLeft + width - popup.width - margin))
    const bottom = offsetTop + height - margin
    const top =
      anchor.bottom + 8 + popup.height <= bottom
        ? anchor.bottom + 8
        : Math.max(offsetTop + margin, Math.min(anchor.top - popup.height - 8, bottom - popup.height))
    setPanelStyle({
      left: `${left}px`,
      top: `${top}px`,
      maxWidth: `${Math.max(0, width - margin * 2)}px`,
      maxHeight: `${Math.max(0, height - margin * 2)}px`
    })
  }, [])

  function onOutside(event: MouseEvent) {
    const target = event.target as Node | null
    if (!openedRef.current || (target && (root.current?.contains(target) || panel.current?.contains(target)))) {
      return
    }
    restoreFocusRef.current = false
    setOpened(false)
  }

  useEffect(() => {
    if (disabled || readonly) close()
  }, [disabled, readonly, close])

  // 展开操作按钮后，列宽可能变化。
  useEffect(() => {
    if (!opened) return
    requestAnimationFrame(positionPanel)
  }, [draftDate, includeSeconds, opened, positionPanel])

  useEffect(() => {
    document.addEventListener('click', onOutside, true)
    window.addEventListener('resize', positionPanel)
    window.addEventListener('scroll', positionPanel, true)
    window.visualViewport?.addEventListener('resize', positionPanel)
    window.visualViewport?.addEventListener('scroll', positionPanel)
    return () => {
      document.removeEventListener('click', onOutside, true)
      window.removeEventListener('resize', positionPanel)
      window.removeEventListener('scroll', positionPanel, true)
      window.visualViewport?.removeEventListener('resize', positionPanel)
      window.visualViewport?.removeEventListener('scroll', positionPanel)
    }
  }, [positionPanel])

  return (
    <div
      ref={root}
      className={cn('date-time-picker input', className, disabled && 'date-time-picker-disabled')}
      style={style}
    >
      {/* Keep native value, constraint validation and form submission semantics.
          This proxy never opens a browser-owned picker. */}
      <input
        ref={valueInput}
        className="date-time-value-proxy"
        type={type}
        value={modelValue ?? ''}
        name={name}
        min={min}
        max={max}
        step={step}
        required={required}
        disabled={disabled}
        readOnly={readonly}
        tabIndex={-1}
        aria-hidden="true"
        onInvalid={(event) => {
          event.preventDefault()
          onInvalid()
        }}
        onClick={(event) => {
          event.preventDefault()
          event.stopPropagation()
          void open()
        }}
        onFocus={() => trigger.current?.focus()}
      />
      <button
        id={id}
        ref={trigger}
        type="button"
        disabled={disabled}
        aria-readonly={readonly || undefined}
        aria-required={required || undefined}
        aria-expanded={opened}
        aria-controls={opened ? panelId : undefined}
        aria-haspopup="dialog"
        aria-label={ariaLabel || placeholderText}
        className={cn('date-time-trigger', !modelValue && 'date-time-trigger-empty')}
        onClick={toggle}
        onKeyDown={(event) => {
          if (event.key === 'ArrowDown') {
            event.preventDefault()
            void open()
          }
        }}
      >
        <span className="date-time-trigger-value">{displayValue || placeholderText}</span>
        <Icon name={type === 'time' ? 'clock' : 'calendar'} size="sm" className="shrink-0 surface-muted" />
      </button>
      {opened &&
        createPortal(
          <div
            id={panelId}
            ref={panel}
            className="date-time-panel surface-panel"
            style={panelStyle}
            role="dialog"
            aria-label={placeholderText}
            tabIndex={-1}
            onClick={(event) => event.stopPropagation()}
            onMouseDown={(event) => event.stopPropagation()}
          >
            {type !== 'time' && (
              <div className="date-time-calendar">
                <div className="date-time-month-nav">
                  <button
                    type="button"
                    className="date-time-icon-button"
                    aria-label={t('dates.previousMonth')}
                    disabled={!canMoveMonth(-1)}
                    onClick={() => moveMonth(-1)}
                  >
                    <Icon name="chevronLeft" size="sm" />
                  </button>
                  <span className="date-time-month-label" aria-live="polite">
                    {monthLabel}
                  </span>
                  <button
                    type="button"
                    className="date-time-icon-button"
                    aria-label={t('dates.nextMonth')}
                    disabled={!canMoveMonth(1)}
                    onClick={() => moveMonth(1)}
                  >
                    <Icon name="chevronRight" size="sm" />
                  </button>
                </div>
                <div className="date-time-weekdays" aria-hidden="true">
                  {weekdays.map((day) => (
                    <span key={day}>{day}</span>
                  ))}
                </div>
                <div className="date-time-days" role="grid" aria-label={monthLabel}>
                  {calendarWeeks.map((week, index) => (
                    <div key={index} className="date-time-week" role="row">
                      {week.map((day) => (
                        <button
                          key={day.value}
                          type="button"
                          role="gridcell"
                          data-date={day.value}
                          aria-label={day.label}
                          aria-selected={day.value === draftDate}
                          aria-current={day.value === todayValue ? 'date' : undefined}
                          disabled={day.disabled}
                          tabIndex={day.value === focusedDate ? 0 : -1}
                          className={cn(
                            'date-time-day',
                            day.outside && 'date-time-day-outside',
                            day.value === draftDate && 'date-time-day-selected',
                            day.value === todayValue && 'date-time-day-today'
                          )}
                          onClick={() => selectDay(day.value)}
                          onKeyDown={(event) => void onDayKey(event, day.value)}
                        >
                          {day.number}
                        </button>
                      ))}
                    </div>
                  ))}
                </div>
              </div>
            )}
            {type !== 'date' && (
              <div className="date-time-clock" aria-label={t('dates.time')}>
                {timeParts.map((part) => (
                  <div key={part.key} className="date-time-clock-part">
                    <label htmlFor={`${panelId}-${part.key}`}>{part.label}</label>
                    <div className="date-time-stepper">
                      <button
                        type="button"
                        aria-label={t('dates.decrease', { unit: part.label })}
                        onClick={() => adjustTime(part.key, -1)}
                      >
                        <svg
                          className="h-4 w-4"
                          fill="none"
                          viewBox="0 0 24 24"
                          stroke="currentColor"
                          strokeWidth="1.5"
                          aria-hidden="true"
                        >
                          <path strokeLinecap="round" d="M5 12h14" />
                        </svg>
                      </button>
                      <input
                        id={`${panelId}-${part.key}`}
                        type="number"
                        inputMode={part.key === 'seconds' && fractionalSeconds ? 'decimal' : 'numeric'}
                        min="0"
                        max={part.max}
                        step={
                          part.key === 'seconds' && (step === 'any' || (Number(step) > 0 && Number(step) < 60))
                            ? step
                            : 1
                        }
                        value={timeValue(part.key)}
                        aria-label={part.label}
                        onChange={(event) => setTime(part.key, event.target.value)}
                        onBlur={() => normalizeTime(part.key)}
                      />
                      <button
                        type="button"
                        aria-label={t('dates.increase', { unit: part.label })}
                        onClick={() => adjustTime(part.key, 1)}
                      >
                        <Icon name="plus" size="sm" />
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}
            {!draftValid && (
              <p className="date-time-validation" role="status">
                {t('dates.outOfRange')}
              </p>
            )}
            <div className="date-time-actions">
              <button
                type="button"
                className="btn btn-ghost btn-sm date-time-clear"
                onClick={() => commit('')}
              >
                {t('common.clear')}
              </button>
              <button type="button" className="btn btn-secondary btn-sm" onClick={close}>
                {t('common.cancel')}
              </button>
              <button
                type="button"
                className="btn btn-primary btn-sm"
                disabled={!draftValid}
                onClick={apply}
              >
                {t('dates.apply')}
              </button>
            </div>
          </div>,
          document.body
        )
      }
    </div>
  )
}
