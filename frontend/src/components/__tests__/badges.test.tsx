import { render, screen } from '@testing-library/react'
import { beforeAll, describe, expect, it } from 'vitest'
import { I18nextProvider } from 'react-i18next'
import i18next from 'i18next'
import { initI18n } from '@/i18n'
import {
  GroupBadge,
  GroupCapacityBadge,
  PlatformTypeBadge,
  StatusBadge
} from '../badges'

beforeAll(async () => {
  await initI18n()
})

function wrap(ui: React.ReactNode) {
  return render(<I18nextProvider i18n={i18next}>{ui}</I18nextProvider>)
}

describe('StatusBadge', () => {
  it('maps statuses to their mark colors', () => {
    const { container, unmount } = wrap(<StatusBadge status="active" label="Active" />)
    expect(container.querySelector('.console-data-mark')).toHaveClass('bg-green-500')
    expect(screen.getByText('Active')).toBeInTheDocument()
    unmount()

    const disabled = render(<I18nextProvider i18n={i18next}><StatusBadge status="disabled" label="Disabled" /></I18nextProvider>)
    expect(disabled.container.querySelector('.console-data-mark')).toHaveClass('bg-yellow-500')
    disabled.unmount()

    const error = render(<I18nextProvider i18n={i18next}><StatusBadge status="error" label="Error" /></I18nextProvider>)
    expect(error.container.querySelector('.console-data-mark')).toHaveClass('bg-red-500')
    error.unmount()

    const unknown = render(<I18nextProvider i18n={i18next}><StatusBadge status="whatever" label="Other" /></I18nextProvider>)
    expect(unknown.container.querySelector('.console-data-mark')).toHaveClass('bg-gray-400')
    unknown.unmount()
  })
})

describe('GroupCapacityBadge', () => {
  it('colors the concurrency chip by usage against its limit', () => {
    const { container, unmount } = wrap(
      <GroupCapacityBadge concurrencyUsed={0} concurrencyMax={5} />
    )
    expect(container.querySelector('span')).toHaveClass('surface-inset')
    unmount()

    const partial = render(
      <I18nextProvider i18n={i18next}>
        <GroupCapacityBadge concurrencyUsed={2} concurrencyMax={5} />
      </I18nextProvider>
    )
    expect(partial.container.querySelector('span')).toHaveClass('bg-yellow-100/[0.15]')
    partial.unmount()

    const full = render(
      <I18nextProvider i18n={i18next}>
        <GroupCapacityBadge concurrencyUsed={5} concurrencyMax={5} />
      </I18nextProvider>
    )
    expect(full.container.querySelector('span')).toHaveClass('bg-red-100/[0.15]')
    full.unmount()
  })

  it('renders session and rpm chips only when their limit is positive', () => {
    const { container } = wrap(
      <GroupCapacityBadge
        concurrencyUsed={1}
        concurrencyMax={4}
        sessionsUsed={2}
        sessionsMax={0}
        rpmUsed={3}
        rpmMax={10}
      />
    )
    // Concurrency + rpm chips only.
    expect(container.querySelectorAll('.font-mono')).toHaveLength(4)
  })
})

describe('GroupBadge', () => {
  it('shows the rate label for standard groups', () => {
    wrap(<GroupBadge name="VIP" platform="anthropic" rateMultiplier={2} />)
    expect(screen.getByText('VIP')).toBeInTheDocument()
    expect(screen.getByText('2x')).toBeInTheDocument()
  })

  it('shows remaining days for subscription groups', () => {
    wrap(<GroupBadge name="Monthly" platform="openai" subscriptionType="subscription" daysRemaining={5} />)
    expect(screen.getByText('5d')).toBeInTheDocument()
  })

  it('shows the expired label for lapsed subscriptions', () => {
    wrap(<GroupBadge name="Monthly" platform="openai" subscriptionType="subscription" daysRemaining={0} />)
    expect(screen.getByText('Expired')).toBeInTheDocument()
  })

  it('strikes through the default rate when a custom rate applies', () => {
    const { container } = wrap(
      <GroupBadge name="VIP" platform="anthropic" rateMultiplier={2} userRateMultiplier={1} />
    )
    expect(container.querySelector('.line-through')).toHaveTextContent('2x')
    expect(screen.getByText('1x')).toBeInTheDocument()
  })
})

describe('PlatformTypeBadge', () => {
  it('labels oauth, token, key and bedrock account types', () => {
    const { unmount } = wrap(<PlatformTypeBadge platform="openai" type="oauth" />)
    expect(screen.getByText('OAuth')).toBeInTheDocument()
    unmount()

    const token = render(<I18nextProvider i18n={i18next}><PlatformTypeBadge platform="anthropic" type="setup-token" /></I18nextProvider>)
    expect(token.getByText('Token')).toBeInTheDocument()
    token.unmount()

    const key = render(<I18nextProvider i18n={i18next}><PlatformTypeBadge platform="gemini" type="apikey" /></I18nextProvider>)
    expect(key.getByText('Key')).toBeInTheDocument()
    key.unmount()

    const aws = render(<I18nextProvider i18n={i18next}><PlatformTypeBadge platform="anthropic" type="bedrock" /></I18nextProvider>)
    expect(aws.getByText('AWS')).toBeInTheDocument()
    aws.unmount()
  })

  it('labels agent identity and PAT OpenAI OAuth modes', () => {
    const agent = render(<I18nextProvider i18n={i18next}><PlatformTypeBadge platform="openai" type="oauth" authMode="agent_identity" /></I18nextProvider>)
    expect(agent.getByText('Agent Identity')).toBeInTheDocument()
    agent.unmount()

    const pat = render(<I18nextProvider i18n={i18next}><PlatformTypeBadge platform="openai" type="oauth" authMode="personal_access_token" /></I18nextProvider>)
    expect(pat.getByText('PAT')).toBeInTheDocument()
    pat.unmount()
  })

  it('labels ChatGPT plan tiers for OpenAI accounts', () => {
    const { unmount } = wrap(<PlatformTypeBadge platform="openai" type="oauth" planType="pro" />)
    expect(screen.getByText('Pro 200')).toBeInTheDocument()
    unmount()

    const lite = render(<I18nextProvider i18n={i18next}><PlatformTypeBadge platform="openai" type="oauth" planType="prolite" /></I18nextProvider>)
    expect(lite.getByText('Pro 100')).toBeInTheDocument()
    lite.unmount()

    const team = render(<I18nextProvider i18n={i18next}><PlatformTypeBadge platform="openai" type="oauth" planType="team" /></I18nextProvider>)
    expect(team.getByText('Business')).toBeInTheDocument()
    team.unmount()

    const business = render(<I18nextProvider i18n={i18next}><PlatformTypeBadge platform="openai" type="oauth" planType="self_serve_business_prolite" /></I18nextProvider>)
    expect(business.getByText('Business Premium')).toBeInTheDocument()
    business.unmount()

    const plus = render(<I18nextProvider i18n={i18next}><PlatformTypeBadge platform="openai" type="oauth" planType="plus" /></I18nextProvider>)
    expect(plus.getByText('Plus')).toBeInTheDocument()
    plus.unmount()

    const free = render(<I18nextProvider i18n={i18next}><PlatformTypeBadge platform="openai" type="oauth" planType="free" /></I18nextProvider>)
    expect(free.getByText('Free')).toBeInTheDocument()
    free.unmount()
  })

  it('labels Grok free plans with the Grok Free icon and paid tiers with the bolt mark', () => {
    const { unmount } = wrap(<PlatformTypeBadge platform="grok" type="oauth" planType="free" />)
    expect(screen.getByText('Grok Free')).toBeInTheDocument()
    expect(screen.getByTestId('grok-free-plan-icon')).toBeInTheDocument()
    expect(screen.queryByTestId('grok-plan-icon')).not.toBeInTheDocument()
    unmount()

    const heavy = render(<I18nextProvider i18n={i18next}><PlatformTypeBadge platform="grok" type="oauth" planType="supergrokheavy" /></I18nextProvider>)
    expect(heavy.getByTestId('grok-plan-icon')).toBeInTheDocument()
    expect(heavy.queryByTestId('grok-free-plan-icon')).not.toBeInTheDocument()
    heavy.unmount()
  })

  it('renders the privacy badge for OpenAI OAuth privacy modes only', () => {
    const { unmount } = wrap(<PlatformTypeBadge platform="openai" type="oauth" privacyMode="training_off" />)
    expect(screen.getByText('Private')).toBeInTheDocument()
    unmount()

    const blocked = render(<I18nextProvider i18n={i18next}><PlatformTypeBadge platform="openai" type="oauth" privacyMode="training_set_cf_blocked" /></I18nextProvider>)
    expect(blocked.getByText('CF')).toBeInTheDocument()
    blocked.unmount()

    const failed = render(<I18nextProvider i18n={i18next}><PlatformTypeBadge platform="openai" type="oauth" privacyMode="training_set_failed" /></I18nextProvider>)
    expect(failed.getByText('Fail')).toBeInTheDocument()
    failed.unmount()

    const ignored = render(<I18nextProvider i18n={i18next}><PlatformTypeBadge platform="gemini" type="oauth" privacyMode="training_off" /></I18nextProvider>)
    expect(ignored.queryByText('Private')).not.toBeInTheDocument()
    ignored.unmount()
  })

  it('shows the subscription expiry only for paid plans', () => {
    const { unmount } = wrap(
      <PlatformTypeBadge platform="openai" type="oauth" planType="plus" subscriptionExpiresAt="2026-03-05T00:00:00Z" />
    )
    expect(screen.getByText(/Expires 2026-03-05/)).toBeInTheDocument()
    unmount()

    const free = render(<I18nextProvider i18n={i18next}><PlatformTypeBadge platform="openai" type="oauth" planType="free" subscriptionExpiresAt="2026-03-05T00:00:00Z" /></I18nextProvider>)
    expect(free.queryByText(/Expires/)).not.toBeInTheDocument()
    free.unmount()
  })
})
