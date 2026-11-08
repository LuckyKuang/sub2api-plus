import { useEffect, useId, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/views/admin/AccountIcon'
import { DateTimePicker } from './DateTimePicker'

interface DatePreset {
  labelKey: string
  value: string
  getRange: () => { start: string; end: string }
}

function formatDateToString(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

export interface DateRangePickerProps {
  startDate: string
  endDate: string
  className?: string
  onStartDateChange: (value: string) => void
  onEndDateChange: (value: string) => void
  onChange: (range: { startDate: string; endDate: string; preset: string | null }) => void
}

export function DateRangePicker({
  startDate,
  endDate,
  className,
  onStartDateChange,
  onEndDateChange,
  onChange
}: DateRangePickerProps) {
  const { t, i18n } = useTranslation()
  const reactId = useId()
  const panelId = `date-range-${reactId.replace(/[:]/g, '')}`
  const [isOpen, setIsOpen] = useState(false)
  const [localStartDate, setLocalStartDate] = useState(startDate)
  const [localEndDate, setLocalEndDate] = useState(endDate)
  const [activePreset, setActivePreset] = useState<string | null>('last24Hours')
  const [dropdownStyle, setDropdownStyle] = useState<React.CSSProperties>({})
  const containerRef = useRef<HTMLDivElement>(null)
  const dropdownRef = useRef<HTMLDivElement>(null)

  const today = formatDateToString(new Date())

  // Tomorrow's date - used for max date to handle timezone differences
  // When user is in a timezone behind the server, "today" on server might be "tomorrow" locally
  const tomorrow = (() => {
    const d = new Date()
    d.setDate(d.getDate() + 1)
    return formatDateToString(d)
  })()

  const presets: DatePreset[] = [
    {
      labelKey: 'dates.today',
      value: 'today',
      getRange: () => {
        const value = today
        return { start: value, end: value }
      }
    },
    {
      labelKey: 'dates.yesterday',
      value: 'yesterday',
      getRange: () => {
        const d = new Date()
        d.setDate(d.getDate() - 1)
        const yesterday = formatDateToString(d)
        return { start: yesterday, end: yesterday }
      }
    },
    {
      labelKey: 'dates.last24Hours',
      value: 'last24Hours',
      getRange: () => {
        const end = new Date()
        const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
        return { start: formatDateToString(start), end: formatDateToString(end) }
      }
    },
    {
      labelKey: 'dates.last7Days',
      value: '7days',
      getRange: () => {
        const end = today
        const d = new Date()
        d.setDate(d.getDate() - 6)
        return { start: formatDateToString(d), end }
      }
    },
    {
      labelKey: 'dates.last14Days',
      value: '14days',
      getRange: () => {
        const end = today
        const d = new Date()
        d.setDate(d.getDate() - 13)
        return { start: formatDateToString(d), end }
      }
    },
    {
      labelKey: 'dates.last30Days',
      value: '30days',
      getRange: () => {
        const end = today
        const d = new Date()
        d.setDate(d.getDate() - 29)
        return { start: formatDateToString(d), end }
      }
    },
    {
      labelKey: 'dates.thisMonth',
      value: 'thisMonth',
      getRange: () => {
        const now = new Date()
        const start = formatDateToString(new Date(now.getFullYear(), now.getMonth(), 1))
        return { start, end: today }
      }
    },
    {
      labelKey: 'dates.lastMonth',
      value: 'lastMonth',
      getRange: () => {
        const now = new Date()
        const start = formatDateToString(new Date(now.getFullYear(), now.getMonth() - 1, 1))
        const end = formatDateToString(new Date(now.getFullYear(), now.getMonth(), 0))
        return { start, end }
      }
    }
  ]

  const formatDate = (dateStr: string): string => {
    const date = new Date(dateStr + 'T00:00:00')
    const dateLocale = i18n.language === 'zh' ? 'zh-CN' : 'en-US'
    return date.toLocaleDateString(dateLocale, { month: 'short', day: 'numeric' })
  }

  const displayValue = (() => {
    if (activePreset) {
      const preset = presets.find((entry) => entry.value === activePreset)
      if (preset) return t(preset.labelKey)
    }

    if (localStartDate && localEndDate) {
      if (localStartDate === localEndDate) {
        return formatDate(localStartDate)
      }
      return `${formatDate(localStartDate)} - ${formatDate(localEndDate)}`
    }

    return t('dates.selectDateRange')
  })()

  const onDateChange = (nextStart: string, nextEnd: string) => {
    // Check if current dates match any preset
    let matched: string | null = null
    for (const preset of presets) {
      const range = preset.getRange()
      if (range.start === nextStart && range.end === nextEnd) {
        matched = preset.value
        break
      }
    }
    setActivePreset(matched)
  }

  function positionDropdown() {
    if (!containerRef.current || !dropdownRef.current) return
    const anchor = containerRef.current.getBoundingClientRect()
    const dropdown = dropdownRef.current
    const viewport = window.visualViewport
    const offsetLeft = viewport?.offsetLeft ?? 0
    const offsetTop = viewport?.offsetTop ?? 0
    const width = viewport?.width ?? window.innerWidth
    const height = viewport?.height ?? window.innerHeight
    const margin = 12
    const gap = 8
    const maxWidth = Math.max(0, width - margin * 2)
    // Measure after updating the width constraint: a menu previously narrowed
    // by the keyboard or zoom can grow again when the visible viewport expands.
    dropdown.style.maxWidth = `${maxWidth}px`
    const panel = dropdown.getBoundingClientRect()
    const viewportTop = offsetTop + margin
    const viewportBottom = Math.max(viewportTop, offsetTop + height - margin)
    const below = Math.max(viewportTop, Math.min(anchor.bottom + gap, viewportBottom))
    const above = Math.max(viewportTop, Math.min(anchor.top - gap, viewportBottom))
    const spaceBelow = viewportBottom - below
    const spaceAbove = above - viewportTop
    // The scroll height retains the full menu size after a smaller viewport has
    // constrained it, so scrolling/resizing can still choose the better side.
    const fullHeight = Math.max(panel.height, dropdown.scrollHeight + dropdown.offsetHeight - dropdown.clientHeight)
    const openAbove = fullHeight > spaceBelow && spaceAbove > spaceBelow
    const maxHeight = openAbove ? spaceAbove : spaceBelow
    const top = openAbove ? above - Math.min(fullHeight, maxHeight) : below
    const left = Math.max(offsetLeft + margin, Math.min(anchor.left, offsetLeft + width - panel.width - margin))
    setDropdownStyle({
      left: `${left}px`,
      top: `${top}px`,
      maxWidth: `${maxWidth}px`,
      maxHeight: `${maxHeight}px`
    })
  }

  // Sync local state with props
  useEffect(() => {
    setLocalStartDate(startDate)
    onDateChange(startDate, endDate)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [startDate])

  useEffect(() => {
    setLocalEndDate(endDate)
     
  }, [endDate])

  useEffect(() => {
    if (!isOpen) return
    const raf = requestAnimationFrame(() => positionDropdown())
    window.addEventListener('resize', positionDropdown)
    window.addEventListener('scroll', positionDropdown, true)
    window.visualViewport?.addEventListener('resize', positionDropdown)
    window.visualViewport?.addEventListener('scroll', positionDropdown)
    return () => {
      cancelAnimationFrame(raf)
      window.removeEventListener('resize', positionDropdown)
      window.removeEventListener('scroll', positionDropdown, true)
      window.visualViewport?.removeEventListener('resize', positionDropdown)
      window.visualViewport?.removeEventListener('scroll', positionDropdown)
    }
     
  }, [isOpen])

  // Outside click + focus trap, mirroring useDialogFocus with lockScroll disabled.
  useEffect(() => {
    if (!isOpen) return
    const previous = document.activeElement as HTMLElement | null
    dropdownRef.current?.focus()
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setIsOpen(false)
        previous?.focus()
      }
    }
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node | null
      if (target && !containerRef.current?.contains(target) && !dropdownRef.current?.contains(target)) {
        setIsOpen(false)
      }
    }
    document.addEventListener('click', handleClickOutside)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('click', handleClickOutside)
      document.removeEventListener('keydown', onKeyDown)
      previous?.focus()
    }
  }, [isOpen])

  function toggle() {
    setIsOpen(!isOpen)
  }

  function selectPreset(preset: DatePreset) {
    const range = preset.getRange()
    setLocalStartDate(range.start)
    setLocalEndDate(range.end)
    setActivePreset(preset.value)
  }

  function apply() {
    onStartDateChange(localStartDate)
    onEndDateChange(localEndDate)
    onChange({ startDate: localStartDate, endDate: localEndDate, preset: activePreset })
    setIsOpen(false)
  }

  return (
    <div className={`relative ${className ?? ''}`} ref={containerRef}>
      <button
        type="button"
        onClick={toggle}
        aria-expanded={isOpen}
        aria-controls={isOpen ? panelId : undefined}
        aria-haspopup="dialog"
        className={`date-picker-trigger surface-control ${isOpen ? 'date-picker-trigger-open' : ''}`}
      >
        <span className="date-picker-icon">
          <Icon name="calendar" size="sm" />
        </span>
        <span className="date-picker-value">{displayValue}</span>
        <span className="date-picker-chevron">
          <Icon
            name="chevronDown"
            size="sm"
            className={`transition-transform duration-200 ${isOpen ? 'rotate-180' : ''}`}
          />
        </span>
      </button>

      {isOpen &&
        createPortal(
          <div
            id={panelId}
            ref={dropdownRef}
            className="date-picker-dropdown surface-panel"
            style={dropdownStyle}
            role="dialog"
            aria-label={t('dates.selectDateRange')}
            tabIndex={-1}
            onClick={(event) => event.stopPropagation()}
            onMouseDown={(event) => event.stopPropagation()}
          >
            <div className="date-picker-presets">
              {presets.map((preset) => (
                <button
                  key={preset.value}
                  type="button"
                  className={`date-picker-preset ${activePreset === preset.value ? 'date-picker-preset-active' : ''}`}
                  onClick={() => selectPreset(preset)}
                >
                  {t(preset.labelKey)}
                </button>
              ))}
            </div>

            <div className="date-picker-divider" />

            <div className="date-picker-custom">
              <div className="date-picker-field">
                <label className="date-picker-label">{t('dates.startDate')}</label>
                <DateTimePicker
                  ariaLabel={t('dates.startDate')}
                  type="date"
                  value={localStartDate}
                  max={localEndDate || tomorrow}
                  className="date-picker-input"
                  onValueChange={(next) => {
                    setLocalStartDate(next)
                    onDateChange(next, localEndDate)
                  }}
                />
              </div>
              <div className="date-picker-separator">
                <Icon name="arrowRight" size="sm" className="text-gray-400" />
              </div>
              <div className="date-picker-field">
                <label className="date-picker-label">{t('dates.endDate')}</label>
                <DateTimePicker
                  ariaLabel={t('dates.endDate')}
                  type="date"
                  value={localEndDate}
                  min={localStartDate}
                  max={tomorrow}
                  className="date-picker-input"
                  onValueChange={(next) => {
                    setLocalEndDate(next)
                    onDateChange(localStartDate, next)
                  }}
                />
              </div>
            </div>

            <div className="date-picker-actions">
              <button type="button" onClick={apply} className="date-picker-apply surface-control">
                {t('dates.apply')}
              </button>
            </div>
          </div>,
          document.body
        )}
    </div>
  )
}

export default DateRangePicker
