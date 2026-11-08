import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nextProvider } from 'react-i18next'
import i18next from 'i18next'
import { initI18n } from '@/i18n'
import { AppHeader } from '../AppHeader'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingStore } from '@/stores/onboarding'

// These three query the API on mount. Their own suites cover that; here they
// would only turn a 401 into a jsdom navigation error on stderr.
vi.mock('@/components/AnnouncementBell', () => ({ AnnouncementBell: () => null }))
vi.mock('@/components/SubscriptionProgressMini', () => ({ SubscriptionProgressMini: () => null }))
vi.mock('@/components/VersionBadge', () => ({ VersionBadge: () => null }))

beforeAll(async () => {
  await initI18n()
})

function renderHeader(role: 'admin' | 'user') {
  useAuthStore.setState({
    user: {
      id: 7,
      username: 'kelly',
      email: 'kelly@example.com',
      role,
      balance: 12.5,
      frozen_balance: 2.5,
      concurrency: 1,
      status: 'active',
      allowed_groups: null,
      balance_notify_enabled: false,
      balance_notify_threshold: null,
      balance_notify_extra_emails: [],
      created_at: '2026-01-01T00:00:00Z'
    },
    runMode: 'standard'
  })
  useAppStore.setState({
    docUrl: 'https://docs.example.com',
    contactInfo: 'ops@example.com'
  })

  return render(
    <I18nextProvider i18n={i18next}>
      <RouterProvider
        router={createMemoryRouter(
          [
            {
              path: '/admin/dashboard',
              element: (
                <AppHeader
                  siteLogo="/logo.svg"
                  userName="kelly"
                  isAdmin={role === 'admin'}
                  onToggleMobile={() => {}}
                  onToggleTheme={() => {}}
                  theme="light"
                  onLogout={() => {}}
                />
              )
            }
          ],
          { initialEntries: ['/admin/dashboard'] }
        )}
      />
    </I18nextProvider>
  )
}

describe('AppHeader account menu', () => {
  beforeEach(() => {
    useOnboardingStore.getState().setReplayCallback(null)
  })

  it('exposes the shortcuts an administrator reaches', async () => {
    const user = userEvent.setup()
    renderHeader('admin')

    await user.click(screen.getByRole('button', { name: 'User menu' }))

    const menu = document.querySelector('.header-user-dropdown')
    expect(menu).not.toBeNull()
    // The header block identifies the signed-in account.
    expect(menu).toHaveTextContent('kelly')
    expect(menu).toHaveTextContent('kelly@example.com')

    for (const label of ['Profile', 'API Keys', 'GitHub', 'Restart Onboarding Tour', 'Logout']) {
      expect(screen.getByText(label)).toBeInTheDocument()
    }
    // The configured contact channel is offered for support.
    expect(screen.getByText('Contact Support:')).toBeInTheDocument()
  })

  it('hides the administrator-only entries from a regular user', async () => {
    const user = userEvent.setup()
    renderHeader('user')

    await user.click(screen.getByRole('button', { name: 'User menu' }))

    expect(screen.queryByText('GitHub')).not.toBeInTheDocument()
    expect(screen.queryByText('Restart Onboarding Tour')).not.toBeInTheDocument()
    expect(screen.getByText('Profile')).toBeInTheDocument()
  })

  it('reports available, frozen and total balance in the hover breakdown', () => {
    const { container } = renderHeader('admin')

    const chip = container.querySelector('.app-header .group.relative')
    expect(chip).not.toBeNull()
    expect(chip).toHaveTextContent('$12.50')
    // A frozen balance is surfaced on the chip itself, not only in the breakdown.
    expect(chip).toHaveTextContent('Frozen balance $2.50')

    const breakdown = chip!.querySelector('div')
    expect(breakdown).toHaveTextContent('Available balance')
    expect(breakdown).toHaveTextContent('$12.50')
    expect(breakdown).toHaveTextContent('$2.50')
    expect(breakdown).toHaveTextContent('Total balance')
    expect(breakdown).toHaveTextContent('$15.00')
  })
})
