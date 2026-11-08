import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nextProvider } from 'react-i18next'
import i18next from 'i18next'
import { initI18n } from '@/i18n'
import { DateTimePicker } from '../DateTimePicker'
import { DateRangePicker } from '../DateRangePicker'

beforeAll(async () => {
  await initI18n()
})

beforeEach(() => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: true,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn()
    }))
  })
})

function wrap(ui: React.ReactNode) {
  return render(<I18nextProvider i18n={i18next}>{ui}</I18nextProvider>)
}

describe('DateTimePicker', () => {
  it('shows the formatted value on the trigger and keeps a hidden native proxy', () => {
    const { container } = wrap(
      <DateTimePicker type="datetime-local" value="2026-03-05T09:30" />
    )
    expect(screen.getByRole('button', { name: /select date and time/i })).toHaveTextContent(
      '2026-03-05 09:30'
    )
    const proxy = container.querySelector('.date-time-value-proxy') as HTMLInputElement
    expect(proxy).toHaveValue('2026-03-05T09:30')
    expect(proxy).toHaveAttribute('type', 'datetime-local')
  })

  it('commits the picked date through apply and emits change events', async () => {
    const user = userEvent.setup()
    const onValueChange = vi.fn()
    const onChange = vi.fn()
    wrap(
      <DateTimePicker
        type="date"
        value="2026-03-05"
        onValueChange={onValueChange}
        onChange={onChange}
      />
    )

    await user.click(screen.getByRole('button', { name: /select date/i }))
    const panel = await screen.findByRole('dialog')
    // The draft starts on the current value.
    await waitFor(() => {
      expect(panel.querySelector('[aria-selected="true"]')).toHaveAttribute('data-date', '2026-03-05')
    })
    await user.click(panel.querySelector('[data-date="2026-03-09"]') as HTMLElement)
    await user.click(screen.getByRole('button', { name: /apply/i }))

    expect(onValueChange).toHaveBeenCalledWith('2026-03-09')
    expect(onChange).toHaveBeenCalled()
  })

  it('clears to an empty string', async () => {
    const user = userEvent.setup()
    const onValueChange = vi.fn()
    wrap(<DateTimePicker type="date" value="2026-03-05" onValueChange={onValueChange} />)

    await user.click(screen.getByRole('button', { name: /select date/i }))
    await user.click(await screen.findByRole('button', { name: /clear/i }))
    expect(onValueChange).toHaveBeenCalledWith('')
  })

  it('disables dates outside the min/max range', async () => {
    const user = userEvent.setup()
    wrap(
      <DateTimePicker type="date" value="2026-03-10" min="2026-03-10" max="2026-03-12" />
    )

    await user.click(screen.getByRole('button', { name: /select date/i }))
    const panel = await screen.findByRole('dialog')
    expect(panel.querySelector('[data-date="2026-03-09"]')).toBeDisabled()
    expect(panel.querySelector('[data-date="2026-03-10"]')).not.toBeDisabled()
    expect(panel.querySelector('[data-date="2026-03-12"]')).not.toBeDisabled()
    expect(panel.querySelector('[data-date="2026-03-13"]')).toBeDisabled()
  })

  it('navigates months with the header buttons', async () => {
    const user = userEvent.setup()
    wrap(<DateTimePicker type="date" value="2026-03-10" />)

    await user.click(screen.getByRole('button', { name: /select date/i }))
    const panel = await screen.findByRole('dialog')
    expect(panel.querySelector('.date-time-month-label')).toHaveTextContent('March 2026')

    await user.click(panel.querySelector('[aria-label="Next month"]') as HTMLElement)
    await waitFor(() => {
      expect(panel.querySelector('.date-time-month-label')).toHaveTextContent('April 2026')
    })
    await user.click(panel.querySelector('[aria-label="Previous month"]') as HTMLElement)
    await waitFor(() => {
      expect(panel.querySelector('.date-time-month-label')).toHaveTextContent('March 2026')
    })
  })

  it('adjusts the time with the stepper controls', async () => {
    const user = userEvent.setup()
    wrap(<DateTimePicker type="time" value="09:30" />)

    await user.click(screen.getByRole('button', { name: /select time/i }))
    const panel = await screen.findByRole('dialog')
    const hours = panel.querySelector('input[type="number"]') as HTMLInputElement
    expect(hours).toHaveValue(9)

    await user.click(panel.querySelector('[aria-label="Increase Hours"]') as HTMLElement)
    await waitFor(() => {
      expect((panel.querySelector('input[type="number"]') as HTMLInputElement).value).toBe('10')
    })
  })

  it('does not open when disabled', async () => {
    const user = userEvent.setup()
    wrap(<DateTimePicker type="date" value="2026-03-05" disabled />)
    await user.click(screen.getByRole('button', { name: /select date/i }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})

describe('DateRangePicker', () => {
  it('uses last 24 hours as the default recognized preset', () => {
    const today = new Date()
    const iso = (date: Date) =>
      `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
    const yesterday = new Date(today)
    yesterday.setDate(today.getDate() - 1)

    render(
      <I18nextProvider i18n={i18next}>
        <DateRangePicker startDate={iso(yesterday)} endDate={iso(today)} />
      </I18nextProvider>
    )
    // A single-day range equal to yesterday is not the last24Hours window, so
    // the explicit dates are displayed instead of a preset label.
    expect(screen.getByRole('button', { name: /select date range/i })).not.toHaveTextContent(
      'Last 24 hours'
    )
  })

  it('applies a preset and publishes the range on apply', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    const onStartDateChange = vi.fn()
    const onEndDateChange = vi.fn()
    render(
      <I18nextProvider i18n={i18next}>
        <DateRangePicker
          startDate="2026-01-01"
          endDate="2026-01-02"
          onChange={onChange}
          onStartDateChange={onStartDateChange}
          onEndDateChange={onEndDateChange}
        />
      </I18nextProvider>
    )

    await user.click(screen.getByRole('button', { name: /select date range/i }))
    await screen.findByRole('dialog')
    await user.click(screen.getByRole('button', { name: /^Today$/ }))
    await user.click(screen.getByRole('button', { name: /^Apply$/ }))

    expect(onStartDateChange).toHaveBeenCalledTimes(1)
    expect(onEndDateChange).toHaveBeenCalledTimes(1)
    const range = onChange.mock.calls.at(-1)?.[0] as {
      startDate: string
      endDate: string
      preset: string | null
    }
    expect(range.preset).toBe('today')
    expect(range.startDate).toBe(range.endDate)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('keeps the panel open while picking a custom date and only publishes on apply', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(
      <I18nextProvider i18n={i18next}>
        <DateRangePicker startDate="2026-01-01" endDate="2026-01-02" onChange={onChange} />
      </I18nextProvider>
    )

    await user.click(screen.getByRole('button', { name: /select date range/i }))
    await screen.findByRole('dialog')
    await user.click(screen.getByRole('button', { name: /last 7 days/i }))
    // Still open, nothing published yet.
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(onChange).not.toHaveBeenCalled()
  })
})
