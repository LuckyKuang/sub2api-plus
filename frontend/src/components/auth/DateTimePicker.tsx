import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from './Icon'

// Dialogs and the date picker panel share one stack and one body scroll lock.
const openDialogs = new Set<symbol>()
const scrollLocks = new Set<symbol>()

export interface UseDialogFocusOptions {
  lockScroll?: boolean
  restoreFocus?: () => boolean
}

function useDialogFocus(
  visible: boolean,
  panel: React.RefObject<HTMLElement | null>,
  close: () => void,
  closeOnEscape: () => boolean = () => true,
  options: UseDialogFocusOptions = {}
) {
  const idRef = useRef<symbol | null>(null)
  if (idRef.current === null) {
    idRef.current = Symbol('dialog')
  }
  const id = idRef.current

  const previousRef = useRef<HTMLElement | null>(null)
  const revisionRef = useRef(0)
  const optionsRef = useRef({ options, close, closeOnEscape })
  optionsRef.current = { options, close, closeOnEscape }

  const isTop = useCallback(() => [...openDialogs].pop() === id, [id])

  const release = useCallback((): HTMLElement | null => {
    const restore =
      isTop() && (optionsRef.current.options.restoreFocus?.() ?? true) ? previousRef.current : null
    openDialogs.delete(id)
    scrollLocks.delete(id)
    document.body.classList.toggle('modal-open', scrollLocks.size > 0)
    previousRef.current = null
    return restore
  }, [id, isTop])

  const focusable = useCallback((): HTMLElement[] => {
    return [
      ...((panel.current?.querySelectorAll<HTMLElement>(
        'button:not([disabled]), a[href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), summary, [tabindex="0"]'
      ) ?? []) as unknown as HTMLElement[])
    ].filter((element) => {
      if (
        element.matches(':disabled') ||
        (element.hasAttribute('tabindex') && element.tabIndex < 0) ||
        element.closest('[inert], [hidden], [aria-hidden="true"]')
      ) {
        return false
      }
      for (let ancestor: HTMLElement | null = element; ancestor; ancestor = ancestor.parentElement) {
        const style = getComputedStyle(ancestor)
        if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse') {
          return false
        }
      }
      return true
    })
  }, [panel])

  const onKey = useCallback(
    (event: KeyboardEvent) => {
      if (event.defaultPrevented || !visible || !isTop()) return
      if (event.key === 'Escape' && optionsRef.current.closeOnEscape()) {
        event.preventDefault()
        event.stopPropagation()
        optionsRef.current.close()
      }
      if (event.key !== 'Tab') return
      const elements = focusable()
      const first = elements[0]
      const last = elements[elements.length - 1]
      if (!first) {
        event.preventDefault()
        panel.current?.focus()
        return
      }
      if (!panel.current?.contains(document.activeElement) || document.activeElement === panel.current) {
        event.preventDefault()
        const target = event.shiftKey ? last : first
        target?.focus()
      } else if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last?.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    },
    [focusable, isTop, panel, visible]
  )

  useEffect(() => {
    const current = ++revisionRef.current
    if (visible) {
      previousRef.current = document.activeElement as HTMLElement | null
      openDialogs.add(id)
      if (optionsRef.current.options.lockScroll !== false) scrollLocks.add(id)
      document.body.classList.toggle('modal-open', scrollLocks.size > 0)
      const frame = requestAnimationFrame(() => {
        if (current === revisionRef.current && visible && isTop()) {
          ;(focusable()[0] ?? panel.current)?.focus({ preventScroll: true })
        }
      })
      return () => cancelAnimationFrame(frame)
    }

    const restore = release()
    const frame = requestAnimationFrame(() => {
      if (current === revisionRef.current && restore?.isConnected && !restore.closest('[inert]')) {
        restore.focus({ preventScroll: true })
      }
    })
    return () => cancelAnimationFrame(frame)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visible])

  useEffect(() => {
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('keydown', onKey)
    }
  }, [onKey])

  useEffect(
    () => () => {
      revisionRef.current += 1
      const restore = release()
      if (restore?.isConnected && !restore.closest('[inert]')) {
        restore.focus({ preventScroll: true })
      }
    },
    [release]
  )
}

type TimePart = 'hours' | 'minutes' | 'seconds'

export interface DateTimePickerProps {
  value?: string | null
  type?: 'date' | 'datetime-local' | 'time'
  min?: string
  max?: string
  step?: string | number
  disabled?: boolean
  readOnly?: boolean
  required?: boolean
  name?: string
  id?: string
  placeholder?: string
  className?: string
  style?: React.CSSProperties
  'aria-label'?: string
  onValueChange?: (value: string) => void
}

function pad(value: number): string {
  const [integer, decimals] = String(value).split('.')
  return `${integer!.padStart(2, '0')}${decimals === undefined ? '' : `.${decimals}`}`
}

function dateValue(date: Date): string {
  return `${String(date.getFullYear()).padStart(4, '0')}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

function localDate(value: string): Date {
  const [year, month, day] = value.split('-').map(Number)
  const result = new Date(0)
  result.setFullYear(year, month - 1, day)
  result.setHours(12, 0, 0, 0)
  return result
}

function compareDates(left: string, right: string): number {
  const a = left.split('T')[0]!.split('-').map(Number)
  const b = right.split('T')[0]!.split('-').map(Number)
  for (let index = 0; index < 3; index++) {
    const difference = a[index]! - b[index]!
    if (difference) return difference
  }
  return 0
}

function millisecondValue(value: string): string {
  return value.replace(/\.(\d{1,2})$/, (_, digits: string) => `.${digits.padEnd(3, '0')}`)
}

export function DateTimePicker({
  value,
  type = 'datetime-local',
  min,
  max,
  step,
  disabled = false,
  readOnly = false,
  required = false,
  name,
  id,
  placeholder,
  className,
  style,
  'aria-label': ariaLabel,
  onValueChange
}: DateTimePickerProps) {
  const { t, i18n } = useTranslation()
  const reactId = useId()
  const panelId = `date-time-${reactId.replace(/[^a-zA-Z0-9_-]/g, '')}`

  const rootRef = useRef<HTMLDivElement | null>(null)
  const triggerRef = useRef<HTMLButtonElement | null>(null)
  const panelRef = useRef<HTMLDivElement | null>(null)
  const valueInputRef = useRef<HTMLInputElement | null>(null)

  const [opened, setOpened] = useState(false)
  const restoreFocusRef = useRef(true)
  const [draftDate, setDraftDate] = useState('')
  const [focusedDate, setFocusedDate] = useState('')
  const [hours, setHours] = useState('00')
  const [minutes, setMinutes] = useState('00')
  const [seconds, setSeconds] = useState('00')
  const [monthYear, setMonthYear] = useState(() => new Date().getFullYear())
  const [monthIndex, setMonthIndex] = useState(() => new Date().getMonth())
  const [panelStyle, setPanelStyle] = useState<React.CSSProperties>({})

  const todayValue = dateValue(new Date())
  const placeholderText =
    placeholder ||
    t(
      type === 'time'
        ? 'dates.selectTime'
        : type === 'date'
          ? 'dates.selectDate'
          : 'dates.selectDateTime'
    )
  const displayValue = String(value ?? '').replace('T', ' ')

  const includeSeconds =
    type !== 'date' &&
    (step === 'any' ||
      (Number(step) > 0 && Number(step) < 60) ||
      [value, min, max].some((candidate) => /:\d{2}:\d{2}/.test(`T${candidate ?? ''}`)))

  const fractionalSeconds = step === 'any' || (Number(step) > 0 && Number(step) < 1)

  const timeParts = useMemo(() => {
    const parts: Array<{ key: TimePart; label: string; max: number | undefined }> = [
      { key: 'hours', label: t('dates.hours'), max: 23 },
      { key: 'minutes', label: t('dates.minutes'), max: 59 }
    ]
    if (includeSeconds) {
      parts.push({
        key: 'seconds',
        label: t('dates.seconds'),
        max: fractionalSeconds ? undefined : 59
      })
    }
    return parts
  }, [fractionalSeconds, includeSeconds, t])

  const monthDate = useMemo(() => {
    const date = new Date(0)
    date.setFullYear(monthYear, monthIndex, 1)
    return date
  }, [monthYear, monthIndex])

  const monthLabel = useMemo(
    () => new Intl.DateTimeFormat(i18n.language, { year: 'numeric', month: 'long' }).format(monthDate),
    [i18n.language, monthDate]
  )

  const weekdays = useMemo(
    () =>
      Array.from(
        { length: 7 },
        (_, index) =>
          new Intl.DateTimeFormat(i18n.language, { weekday: 'short' }).format(new Date(2026, 5, 8 + index))
      ),
    [i18n.language]
  )

  const dateDisabled = useCallback(
    (candidate: string) =>
      Boolean(
        (min && compareDates(candidate, min) < 0) || (max && compareDates(candidate, max) > 0)
      ),
    [max, min]
  )

  const calendarDays = useMemo(() => {
    const first = localDate(dateValue(monthDate))
    first.setDate(1 - (first.getDay() + 6) % 7)
    return Array.from({ length: 42 }, (_, index) => {
      const day = new Date(first)
      day.setDate(first.getDate() + index)
      const cellValue = dateValue(day)
      return {
        value: cellValue,
        number: day.getDate(),
        outside: day.getMonth() !== monthIndex,
        disabled: day.getFullYear() < 1 || dateDisabled(cellValue),
        label: new Intl.DateTimeFormat(i18n.language, {
          year: 'numeric',
          month: 'long',
          day: 'numeric'
        }).format(day)
      }
    })
  }, [dateDisabled, i18n.language, monthDate, monthIndex])

  const calendarWeeks = useMemo(
    () => Array.from({ length: 6 }, (_, index) => calendarDays.slice(index * 7, index * 7 + 7)),
    [calendarDays]
  )

  const clock = `${pad(Number(hours))}:${pad(Number(minutes))}${
    includeSeconds ? `:${pad(Number(seconds))}` : ''
  }`
  const draftValue = type === 'date' ? draftDate : type === 'time' ? clock : `${draftDate}T${clock}`

  const timeValue = useCallback(
    (part: TimePart): string => ({ hours, minutes, seconds } as Record<TimePart, string>)[part],
    [hours, minutes, seconds]
  )

  // Let the browser enforce the original input's calendar, range and step rules,
  // without displaying its platform-specific popup or converting to UTC.
  const draftValid = useMemo(() => {
    if (
      type !== 'date' &&
      timeParts.some((part) => {
        const pattern =
          part.key === 'seconds' && fractionalSeconds ? /^\d+(?:\.\d+)?$/ : /^\d+$/
        const current = timeValue(part.key)
        const parsed = Number(current)
        return !pattern.test(current) || parsed < 0 || parsed >= (part.key === 'hours' ? 24 : 60)
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
  }, [draftValue, fractionalSeconds, max, min, step, timeParts, type, timeValue])

  function canMoveMonth(delta: number): boolean {
    const date = new Date(monthDate)
    date.setMonth(date.getMonth() + delta)
    if (date.getFullYear() < 1) return false
    const first = dateValue(date)
    date.setMonth(date.getMonth() + 1, 0)
    return (
      !(min && compareDates(dateValue(date), min) < 0) && !(max && compareDates(first, max) > 0)
    )
  }

  function moveMonth(delta: number) {
    if (!canMoveMonth(delta)) return
    const date = new Date(monthDate)
    date.setMonth(date.getMonth() + delta)
    setMonthYear(date.getFullYear())
    setMonthIndex(date.getMonth())
    setFocusedDate(dateValue(date))
  }

  function selectDay(cellValue: string) {
    setDraftDate(cellValue)
    setFocusedDate(cellValue)
    const date = localDate(cellValue)
    setMonthYear(date.getFullYear())
    setMonthIndex(date.getMonth())
  }

  function onDayKey(event: React.KeyboardEvent<HTMLButtonElement>, cellValue: string) {
    const offsets: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 }
    const date = localDate(cellValue)
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
      panelRef.current?.querySelector<HTMLButtonElement>(`[data-date="${next}"]`)?.focus()
    })
  }

  function initializeDraft() {
    const current = value ?? ''
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
    const parsed = localDate(date)
    setMonthYear(parsed.getFullYear())
    setMonthIndex(parsed.getMonth())
  }

  function editable(): boolean {
    return (
      !disabled &&
      !readOnly &&
      !triggerRef.current?.disabled &&
      !valueInputRef.current?.readOnly &&
      !valueInputRef.current?.disabled
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

  function close() {
    restoreFocusRef.current = true
    setOpened(false)
  }

  function commit(next: string) {
    if (!editable()) {
      close()
      return
    }
    const changed = next !== String(value ?? '')
    close()
    if (!changed) return
    if (valueInputRef.current) valueInputRef.current.value = next
    onValueChange?.(next)
    for (const eventType of ['input', 'change'] as const) {
      const event = new Event(eventType, { bubbles: true })
      valueInputRef.current?.dispatchEvent(event)
    }
  }

  function apply() {
    if (draftValid) commit(draftValue)
  }

  function onInvalid() {
    void open()
    triggerRef.current?.focus()
  }

  const positionPanel = useCallback(() => {
    if (!opened || !panelRef.current || !triggerRef.current) return
    const anchor = triggerRef.current.getBoundingClientRect()
    const popup = panelRef.current.getBoundingClientRect()
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
  }, [opened])

  useDialogFocus(opened, panelRef, close, () => true, {
    lockScroll: false,
    restoreFocus: () => restoreFocusRef.current
  })

  useEffect(() => {
    if (disabled || readOnly) close()
  }, [disabled, readOnly])

  useEffect(() => {
    requestAnimationFrame(positionPanel)
  }, [draftDate, includeSeconds, positionPanel])

  useEffect(() => {
    function onOutside(event: MouseEvent) {
      const target = event.target as Node | null
      if (!opened || (target && (rootRef.current?.contains(target) || panelRef.current?.contains(target)))) {
        return
      }
      restoreFocusRef.current = false
      setOpened(false)
    }

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
  }, [opened, positionPanel])

  function adjustTime(part: TimePart, delta: number) {
    const limit = part === 'hours' ? 24 : 60
    const amount = part === 'seconds' && Number(step) > 0 && Number(step) < 60 ? Number(step) : 1
    // Native time values have millisecond precision; integer arithmetic avoids
    // introducing binary floating-point digits when a fractional second wraps.
    const units = limit * 1000
    const current = Number(timeValue(part) || 0)
    const next = Math.round(current * 1000) + delta * Math.round(amount * 1000)
    const nextValue = pad(((next % units + units) % units) / 1000)
    if (part === 'hours') setHours(nextValue)
    else if (part === 'minutes') setMinutes(nextValue)
    else setSeconds(nextValue)
  }

  function normalizeTime(part: TimePart) {
    const current = timeValue(part)
    if (current !== '') {
      const normalized = pad(Number(current))
      if (part === 'hours') setHours(normalized)
      else if (part === 'minutes') setMinutes(normalized)
      else setSeconds(normalized)
    }
  }

  return (
    <div
      ref={rootRef}
      className={[
        'date-time-picker input',
        className,
        disabled ? 'date-time-picker-disabled' : ''
      ]
        .filter(Boolean)
        .join(' ')}
      style={style}
    >
      {/* Keep native value, constraint validation and form submission semantics.
          This proxy never opens a browser-owned picker. */}
      <input
        ref={valueInputRef}
        className="date-time-value-proxy"
        type={type}
        value={value ?? ''}
        name={name}
        min={min}
        max={max}
        step={step}
        required={required}
        disabled={disabled}
        readOnly={readOnly}
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
        onFocus={() => triggerRef.current?.focus()}
        onChange={() => undefined}
      />
      <button
        id={id}
        ref={triggerRef}
        type="button"
        disabled={disabled}
        aria-readonly={readOnly || undefined}
        aria-required={required || undefined}
        aria-expanded={opened}
        aria-controls={opened ? panelId : undefined}
        aria-haspopup="dialog"
        aria-label={ariaLabel || placeholderText}
        className={`date-time-trigger${value ? '' : ' date-time-trigger-empty'}`}
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
            ref={panelRef}
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
                    <div className="date-time-week" role="row" key={index}>
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
                          className={[
                            'date-time-day',
                            day.outside ? 'date-time-day-outside' : '',
                            day.value === draftDate ? 'date-time-day-selected' : '',
                            day.value === todayValue ? 'date-time-day-today' : ''
                          ]
                            .filter(Boolean)
                            .join(' ')}
                          onClick={() => selectDay(day.value)}
                          onKeyDown={(event) => onDayKey(event, day.value)}
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
                  <div className="date-time-clock-part" key={part.key}>
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
                        onChange={(event) => {
                          const next = event.target.value
                          if (part.key === 'hours') setHours(next)
                          else if (part.key === 'minutes') setMinutes(next)
                          else setSeconds(next)
                        }}
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
              <button type="button" className="btn btn-ghost btn-sm date-time-clear" onClick={() => commit('')}>
                {t('common.clear')}
              </button>
              <button type="button" className="btn btn-secondary btn-sm" onClick={close}>
                {t('common.cancel')}
              </button>
              <button type="button" className="btn btn-primary btn-sm" disabled={!draftValid} onClick={apply}>
                {t('dates.apply')}
              </button>
            </div>
          </div>,
          document.body
        )}
    </div>
  )
}

export default DateTimePicker
