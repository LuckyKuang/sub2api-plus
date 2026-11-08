import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeAll, describe, expect, it, vi } from 'vitest'
import { I18nextProvider } from 'react-i18next'
import i18next from 'i18next'
import { initI18n } from '@/i18n'
import { CheckboxDropdown } from '../CheckboxDropdown'
import { WorkspaceTabs } from '../WorkspaceTabs'
import { MonitorQuotaView } from '../MonitorQuotaView'
import { AutoRefreshButton } from '../AutoRefreshButton'
import type { MonitorQuotaSnapshot } from '@/api/admin/channelMonitor'

beforeAll(async () => {
  await initI18n()
})

function wrap(ui: React.ReactNode) {
  return render(<I18nextProvider i18n={i18next}>{ui}</I18nextProvider>)
}

describe('CheckboxDropdown', () => {
  it('shows the joined selection on the trigger and toggles values', async () => {
    const user = userEvent.setup()
    const onValueChange = vi.fn()
    wrap(
      <CheckboxDropdown
        value={[1]}
        options={[
          { value: 1, label: 'Alpha' },
          { value: 2, label: 'Beta' }
        ]}
        label="Pick"
        onValueChange={onValueChange}
      />
    )

    const trigger = screen.getByRole('button', { name: 'Pick' })
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
    await user.click(trigger)
    expect(trigger).toHaveAttribute('aria-expanded', 'true')

    const beta = screen.getByRole('checkbox', { name: 'Beta' }) as HTMLInputElement
    expect((screen.getByRole('checkbox', { name: 'Alpha' }) as HTMLInputElement).checked).toBe(true)
    expect(beta.checked).toBe(false)

    await user.click(beta)
    expect(onValueChange).toHaveBeenCalledWith([1, 2])
  })

  it('closes on Escape and does not open when disabled', async () => {
    const user = userEvent.setup()
    const { unmount } = wrap(
      <CheckboxDropdown value={[]} options={[{ value: 1, label: 'Alpha' }]} label="Pick" />
    )
    await user.click(screen.getByRole('button', { name: 'Pick' }))
    await user.keyboard('{Escape}')
    await waitFor(() => {
      expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
    })
    unmount()

    wrap(
      <CheckboxDropdown
        value={[]}
        options={[{ value: 1, label: 'Alpha' }]}
        label="Pick"
        disabled
      />
    )
    await user.click(screen.getByRole('button', { name: 'Pick' }))
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
  })
})

describe('WorkspaceTabs', () => {
  it('moves selection with arrow keys and reports the new value', async () => {
    const user = userEvent.setup()
    const onValueChange = vi.fn()
    wrap(
      <WorkspaceTabs
        value="first"
        label="Workspace"
        onValueChange={onValueChange}
        tabs={[
          { value: 'first', label: 'First' },
          { value: 'second', label: 'Second' },
          { value: 'third', label: 'Third', disabled: true }
        ]}
      />
    )

    const first = screen.getByRole('tab', { name: 'First' })
    expect(first).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tab', { name: 'Third' })).toBeDisabled()

    first.focus()
    await user.keyboard('{ArrowRight}')
    expect(onValueChange).toHaveBeenCalledWith('second')

    await user.keyboard('{End}')
    // End lands on the last enabled tab.
    expect(onValueChange).toHaveBeenLastCalledWith('second')
  })
})

describe('AutoRefreshButton', () => {
  it('shows the countdown while enabled and the paused hint', async () => {
    const user = userEvent.setup()
    const onEnabledChange = vi.fn()
    const { unmount } = wrap(
      <AutoRefreshButton
        enabled
        countdown={7}
        intervalSeconds={30}
        intervals={[5, 30]}
        onEnabledChange={onEnabledChange}
      />
    )
    expect(screen.getByTitle('Auto Refresh')).toHaveTextContent('Auto refresh: 7s')
    unmount()

    const paused = render(
      <I18nextProvider i18n={i18next}>
        <AutoRefreshButton enabled paused countdown={0} intervalSeconds={30} intervals={[5, 30]} />
      </I18nextProvider>
    )
    expect(paused.getByTitle('Auto Refresh')).toHaveTextContent('Paused')
    paused.unmount()

    const intervalOnly = render(
      <I18nextProvider i18n={i18next}>
        <AutoRefreshButton enabled={false} countdown={0} intervalSeconds={30} intervals={[5, 30]} intervalOnly />
      </I18nextProvider>
    )
    expect(intervalOnly.getByTitle('Auto Refresh')).toHaveTextContent('30 seconds')
    intervalOnly.unmount()
    void user
  })
})

describe('MonitorQuotaView', () => {
  it('renders nothing without a snapshot', () => {
    const { container } = wrap(<MonitorQuotaView snapshot={null} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('maps tier window/label tokens through i18n and colors utilization', () => {
    const snapshot: MonitorQuotaSnapshot = {
      source: 'usage',
      success: true,
      fetched_at: '2026-08-18T00:00:00Z',
      tiers: [
        { window: '5h', used_percent: 42.4 },
        { window: '7d', label: 'pro', used_percent: 80 },
        { window: 'weekly', label: 'unknown-token', used_percent: 95 }
      ]
    }
    wrap(<MonitorQuotaView snapshot={snapshot} />)

    const rows = screen.getAllByTestId('monitor-quota-tier')
    expect(rows).toHaveLength(3)
    const text = document.body.textContent ?? ''
    // Known window token goes through i18n.
    expect(text).toContain('5h')
    expect(text).toContain('Pro')
    // Unknown label token passes through unchanged.
    expect(text).toContain('unknown-token/Weekly')
    // Utilization thresholds pick the bar color.
    expect(rows[1].querySelector('.bg-amber-500')).not.toBeNull()
    expect(rows[2].querySelector('.bg-red-500')).not.toBeNull()
  })

  it('shows the unlimited marker and multi-currency balances', () => {
    const snapshot: MonitorQuotaSnapshot = {
      source: 'cn_quota',
      success: true,
      fetched_at: '2026-08-18T00:00:00Z',
      tiers: [{ window: 'daily', unlimited: true, used_percent: 0 }],
      balances: [
        { currency: 'CNY', balance: 12.5 },
        { currency: 'USD', balance: -1 }
      ]
    }
    wrap(<MonitorQuotaView snapshot={snapshot} />)
    expect(screen.getByTestId('monitor-quota-unlimited')).toBeInTheDocument()
    expect(screen.getByText('12.50 CNY')).toBeInTheDocument()
    expect(screen.getByText('-1.00 USD')).toHaveClass('text-red-600')
  })

  it('truncates the error message for failed snapshots', () => {
    const snapshot: MonitorQuotaSnapshot = {
      source: 'usage',
      success: false,
      fetched_at: '2026-08-18T00:00:00Z',
      error: 'x'.repeat(60)
    }
    wrap(<MonitorQuotaView snapshot={snapshot} />)
    const error = screen.getByTestId('monitor-quota-error')
    expect(error.textContent).toHaveLength(49)
    expect(error.textContent?.endsWith('…')).toBe(true)
  })
})
