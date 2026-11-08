import { render, screen } from '@testing-library/react'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { beforeAll, describe, expect, it, vi } from 'vitest'
import { I18nextProvider } from 'react-i18next'
import i18next from 'i18next'
import { initI18n } from '@/i18n'
import { AppSidebar } from '../AppSidebar'
import type { NavContext } from '../navConfig'

vi.mock('@/components/VersionBadge', () => ({ VersionBadge: () => null }))

beforeAll(async () => {
  await initI18n()
})

function renderSidebar() {
  const ctx: NavContext = {
    t: (key: string) => i18next.t(key),
    settings: { affiliate_enabled: true },
    isAdmin: true,
    isSimpleMode: false,
    purchaseNavLabel: i18next.t('nav.buySubscription'),
    customMenuItems: [],
    channelMonitorMode: 'v2',
    supportUserId: null,
    flags: { asyncImage: false, batchImage: false, opsMonitoring: false, adminPayment: false }
  }

  return render(
    <I18nextProvider i18n={i18next}>
      <RouterProvider
        router={createMemoryRouter(
          [
            {
              path: '/admin/dashboard',
              element: (
                <AppSidebar
                  {...ctx}
                  collapsed={false}
                  onToggleCollapsed={() => {}}
                  mobileOpen={false}
                  onCloseMobile={() => {}}
                  theme="light"
                  onToggleTheme={() => {}}
                  isDesktop
                  siteName="Sub2API Plus"
                  siteLogo=""
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

describe('AppSidebar tour anchors', () => {
  it('exposes the ids the interactive tour highlights', () => {
    const { container } = renderSidebar()

    // The tour's group and channel steps target these entries directly.
    expect(container.querySelector('#sidebar-group-manage')).not.toBeNull()
    expect(container.querySelector('#sidebar-channel-manage')).not.toBeNull()
    expect(container.querySelector('#sidebar-wallet')).not.toBeNull()
  })

  it('still renders the console entries the admin menu leads with', () => {
    const { container } = renderSidebar()

    // The admin section precedes the personal one, as the Vue sidebar does.
    const links = [...container.querySelectorAll('.sidebar-link')].map((e) => e.textContent?.trim())
    expect(links[0]).toContain('Dashboard')
    expect(links.indexOf('Users')).toBeGreaterThan(links.indexOf('Dashboard'))
    // The affiliate entry renders its translated label, not the i18n key.
    expect(screen.getAllByText('Affiliate Rebates').length).toBeGreaterThan(0)
    expect(container.textContent).not.toMatch(/\bnav\.affiliate/)
    // The personal section closes with the console settings entry.
    expect(links).toContain('Settings')
  })
})
