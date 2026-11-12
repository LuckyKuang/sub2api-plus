import { expect, test, type Locator, type Page, type TestInfo } from '@playwright/test'
import { wheelScroll } from './wheel'
import { group, offer } from '../src/components/channels/__tests__/catalogFixtures'

// Acceptance source: the approved Chrome user-page audit requires fixed page
// chrome, independent record/card scrolling, aligned rounded panels, 10 invited
// users per page, and a three-column/nine-card subscription page. These tests
// mount production views and intercept only their HTTP boundary; they never
// inject CSS or replacement DOM to obtain the expected geometry.
const user = {
  id: 9999, role: 'user', status: 'active', email: 'review@example.test', username: 'Reviewer',
  balance: 260, concurrency: 6, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
}

// docs/AVAILABLE_CHANNELS.md: real production list rows must fit 320px phones,
// use two columns on wide screens, and preserve quotes, keyboard focus and cache.
test('available-channel list fits both themes and supports quote details, filtering and copying', async ({ page }, info) => {
  test.setTimeout(120_000)
  const { theme, locale } = presentation(info)
  const longName = 'model-00-' + 'long-id-'.repeat(16)
  const catalog = {
    groups: [
      group(1, Array.from({ length: 12 }, (_, index) => offer(index === 0 ? longName : `model-${String(index).padStart(2, '0')}`))),
      group(2, [offer('anthropic-model', { platform: 'anthropic' })]),
      group(3, [offer('model-01')]),
    ],
    user_rate_status: 'loaded',
  }
  let reads = 0
  const writes: string[] = []
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    if (request.method() !== 'GET') writes.push(url.pathname)
    let data: unknown = []
    if (url.pathname === '/api/v1/channels/available') {
      expect(url.searchParams.get('view')).toBe('catalog')
      reads++
      data = catalog
    } else if (url.pathname === '/api/v1/auth/me' || url.pathname === '/api/v1/user/profile') data = user
    await route.fulfill({ json: { code: 0, data } })
  })
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async (text: string) => { sessionStorage.setItem('copied-model', text) } } })
  })
  for (const width of [320, 768, 1920]) {
    await page.setViewportSize({ width, height: 900 })
    await page.goto(`/layout-tests/index.html?view=user-layout&auditPage=channels&layout=list&product=1&theme=${theme}&locale=${locale}`)
    // The first production-view import on a WSL bind mount can be cold. Wait
    // for the page itself before applying the normal data/geometry assertions.
    await expect(page.getByTestId('catalog-results')).toBeVisible({ timeout: 60_000 })
    const list = page.getByTestId('catalog-model-list')
    await expect(list.locator('h3')).toHaveCount(13)
    // Shared filter contract: 240px search and 200px selection fields on
    // desktop; full-width stacked fields below 1024px, with natural actions.
    const filterBar = page.getByTestId('catalog-filters').locator('.filter-bar')
    const searchBox = (await filterBar.locator('.filter-field--search').boundingBox())!
    const sortBox = (await filterBar.locator('.filter-field:not(.filter-field--search)').boundingBox())!
    if (width >= 1024) {
      expect(searchBox.width).toBeCloseTo(240, 0)
      expect(sortBox.width).toBeCloseTo(200, 0)
      expect(sortBox.y).toBeCloseTo(searchBox.y, 0)
    } else {
      const barBox = (await filterBar.boundingBox())!
      expect(searchBox.width).toBeCloseTo(barBox.width, 0)
      expect(sortBox.width).toBeCloseTo(barBox.width, 0)
      expect(sortBox.y).toBeGreaterThanOrEqual(searchBox.y + searchBox.height)
    }
    expect((await filterBar.locator('input[type="search"]').boundingBox())!.width).toBeCloseTo(searchBox.width, 0)
    expect((await filterBar.locator('.select-trigger').boundingBox())!.width).toBeCloseTo(sortBox.width, 0)
    const reset = filterBar.getByRole('button').last()
    expect(await reset.evaluate(element => element.closest('.filter-field') === null)).toBe(true)
    expect((await reset.boundingBox())!.width).toBeLessThan(200)
    const geometry = await list.evaluate(element => ({
      overflow: document.documentElement.scrollWidth > innerWidth,
      clipped: [...element.querySelectorAll('.catalog-list-row')].some(row => row.scrollWidth > row.clientWidth + 1),
      columns: getComputedStyle(element.querySelector('.catalog-list-rows')!).gridTemplateColumns.split(' ').length,
    }))
    expect(geometry.overflow).toBe(false)
    expect(geometry.clipped).toBe(false)
    expect(geometry.columns).toBe(width === 1920 ? 2 : 1)
    // Café Shop reference and the user's acceptance requirement: the row's
    // empty area and prices open details, not just its model-name text.
    const row = list.locator('.catalog-list-row').first()
    const box = (await row.boundingBox())!
    await row.click({ position: { x: box.width - 8, y: box.height - 8 } })
    await expect(page.getByRole('dialog')).toBeVisible()
    await expect(page.getByRole('dialog')).toHaveAccessibleName('anthropic-model')
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toBeHidden()
    // Hover is a visible, non-intercepting card overlay, aligned with the
    // complete row in each theme/viewport (not just a model-name text color).
    await row.hover()
    const highlight = list.locator('.catalog-list-highlight.is-active')
    await expect(highlight).toHaveCount(1)
    await expect(highlight).toHaveCSS('opacity', '1')
    await expect(highlight).toHaveCSS('pointer-events', 'none')
    const markBox = (await highlight.boundingBox())!
    const rowBox = (await row.boundingBox())!
    for (const key of ['x', 'y', 'width', 'height'] as const) expect(Math.abs(markBox[key] - rowBox[key])).toBeLessThanOrEqual(1)
    await expect(highlight).toHaveCSS('transition-duration', '0s')
    const otherRow = list.locator('.catalog-list-row').nth(1)
    await otherRow.hover()
    const movedBox = (await list.locator('.catalog-list-highlight.is-active').boundingBox())!
    const otherBox = (await otherRow.boundingBox())!
    for (const key of ['x', 'y', 'width', 'height'] as const) expect(Math.abs(movedBox[key] - otherBox[key])).toBeLessThanOrEqual(1)
    await row.scrollIntoViewIfNeeded()
    const prices = row.locator('.font-mono.tabular-nums:not([aria-hidden])').first()
    await expect(prices).toContainText('$2')
    const priceBox = (await prices.boundingBox())!
    await page.mouse.click(priceBox.x + priceBox.width / 2, priceBox.y + priceBox.height / 2)
    await expect(page.getByRole('dialog')).toBeVisible()
    // Clicking the read-only dialog's backdrop is another supported close path.
    await page.getByRole('dialog').click({ position: { x: 1, y: 1 } })
    await expect(page.getByRole('dialog')).toBeHidden()
  }
  const readsBeforeFilter = reads
  await page.locator('input[type="search"]').fill('model-01')
  const list = page.getByTestId('catalog-model-list')
  await expect(list.locator('h3')).toHaveCount(1)
  const trigger = list.locator('.catalog-list-open')
  await trigger.focus()
  await page.keyboard.press('Enter')
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expect(dialog).toHaveAccessibleName('model-01')
  await expect(dialog.locator('.catalog-comparison tbody tr')).toHaveCount(2)
  await dialog.locator('.catalog-copy-model').click()
  await expect.poll(() => page.evaluate(() => sessionStorage.getItem('copied-model'))).toBe('model-01')
  await expect(dialog).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(trigger).toBeFocused()
  await page.mouse.move(0, 0)
  await expect(list.locator('.catalog-list-highlight.is-active')).toHaveCount(1)
  await page.keyboard.press('Space')
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: 'Close modal' }).click()
  await expect(dialog).toBeHidden()
  await list.locator('.catalog-copy-model').click()
  await expect(dialog).toBeHidden()
  await expect.poll(() => page.evaluate(() => sessionStorage.getItem('copied-model'))).toBe('model-01')
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await list.locator('.catalog-list-row').hover()
  await expect(list.locator('.catalog-list-highlight.is-active')).toHaveCSS('transition-duration', '0.18s, 0.18s, 0.18s, 0.12s')
  expect(reads).toBe(readsBeforeFilter)
  expect(writes).toEqual([])
})
const invitees = Array.from({ length: 32 }, (_, index) => ({
  user_id: index + 1, email: `invitee-${index + 1}@example.test`, username: `Invitee ${String(index + 1).padStart(2, '0')}`,
  total_rebate: index + 1, created_at: '2026-10-01T12:00:00Z',
}))
const subscriptions = Array.from({ length: 28 }, (_, index) => ({
  id: index + 1, user_id: 9999, group_id: index + 1, status: 'active',
  starts_at: '2026-01-01T00:00:00Z', expires_at: '2099-12-31T00:00:00Z',
  created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
  daily_usage_usd: 2, weekly_usage_usd: 5, monthly_usage_usd: 10, five_hour_usage_usd: 1,
  daily_window_start: null, weekly_window_start: null, monthly_window_start: null, five_hour_window_start: null,
  group: {
    id: index + 1, name: `Audit plan ${String(index + 1).padStart(2, '0')}`, platform: 'openai',
    description: 'Monthly production-compatible subscription fixture', rate_multiplier: 1,
    status: 'active', is_exclusive: false, subscription_type: 'subscription',
    five_hour_limit_usd: 10, daily_limit_usd: 25, weekly_limit_usd: 125, monthly_limit_usd: 400,
    peak_rate_enabled: false,
  },
}))

async function interceptAuditAPI(page: Page) {
  const writes: string[] = []
  const historyRequests: Array<{ page: number; size: number }> = []
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    if (request.method() !== 'GET') writes.push(`${request.method()} ${url.pathname}`)
    let data: unknown = { items: [], total: 0, pages: 0, enabled: false }
    if (url.pathname === '/api/v1/redeem/history') {
      const current = Number(url.searchParams.get('page') || 1)
      const size = Number(url.searchParams.get('page_size') || 20)
      historyRequests.push({ page: current, size })
      const count = Math.min(size, Math.max(0, 61 - (current - 1) * size))
      data = { total: 61, page: current, page_size: size, pages: Math.ceil(61 / size), items: Array.from({ length: count }, (_, index) => {
        const id = (current - 1) * size + index + 1
        return { id, code: `R${String(id).padStart(6, '0')}-CODE`, type: 'balance', value: id, status: 'used',
          used_at: '2026-10-01T12:00:00Z', created_at: '2026-10-01T12:00:00Z', notes: `Redeemed record ${id}` }
      }) }
    } else if (url.pathname === '/api/v1/user/aff') {
      data = { user_id: 9999, aff_code: 'AUDIT-INVITE', inviter_id: null, aff_count: 32, aff_quota: 12.5,
        aff_frozen_quota: 2.5, aff_history_quota: 30, effective_rebate_rate_percent: 10, invitees }
    } else if (url.pathname === '/api/v1/subscriptions') data = subscriptions
    else if (url.pathname === '/api/v1/subscriptions/active' || url.pathname === '/api/v1/subscriptions/progress') data = []
    else if (url.pathname === '/api/v1/subscriptions/summary') data = { active_count: 0, subscriptions: [] }
    else if (url.pathname === '/api/v1/auth/me' || url.pathname === '/api/v1/user/profile') data = user
    else if (url.pathname === '/api/v1/settings/public') data = { contact_info: 'support@example.test' }
    await route.fulfill({ json: { code: 0, data } })
  })
  return { writes, historyRequests }
}

function presentation(info: TestInfo) {
  return { theme: info.project.use.colorScheme === 'dark' ? 'dark' : 'light', locale: info.project.use.locale?.startsWith('zh') ? 'zh' : 'en' }
}

async function openAuditPage(page: Page, name: string, info: TestInfo, width: number) {
  const { theme, locale } = presentation(info)
  await page.setViewportSize({ width, height: width === 2560 ? 1205 : 900 })
  await page.goto(`/layout-tests/index.html?view=user-layout&auditPage=${name}&theme=${theme}&locale=${locale}`)
  await expect(page.locator('.app-layout-user.app-layout-workspace')).toBeVisible({ timeout: 60_000 })
}

async function assertFooterAtWorkspaceBottom(footer: Locator, anchor: Locator) {
  await expect(footer).toBeInViewport()
  const content = (await anchor.boundingBox())!
  const values = await footer.evaluate(footerElement => {
    const main = document.querySelector('.app-main') as HTMLElement
    const rect = footerElement.getBoundingClientRect()
    const box = main.getBoundingClientRect()
    const style = getComputedStyle(footerElement)
    return {
      left: rect.left, right: rect.right, bottom: rect.bottom - box.bottom,
      radius: [style.borderTopLeftRadius, style.borderTopRightRadius, style.borderBottomLeftRadius, style.borderBottomRightRadius].map(parseFloat),
      mainOverflow: main.scrollHeight - main.clientHeight,
      documentOverflow: document.documentElement.scrollWidth - innerWidth,
      hit: footerElement.contains(document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2)),
    }
  })
  values.left -= content.x
  values.right -= content.x + content.width
  expect(Math.abs(values.left)).toBeLessThanOrEqual(1)
  expect(Math.abs(values.right)).toBeLessThanOrEqual(1)
  expect(Math.abs(values.bottom)).toBeLessThanOrEqual(1)
  expect(values.radius.every(radius => radius >= 14)).toBe(true)
  expect(values.mainOverflow).toBeLessThanOrEqual(1)
  expect(values.documentOverflow).toBeLessThanOrEqual(1)
  expect(values.hit).toBe(true)
}

async function assertEqualPairAndFourGuides(page: Page, pairSelector: string, guidesSelector: string) {
  const pair = await page.locator(pairSelector).evaluateAll(elements => elements.map(el => {
    const r = el.getBoundingClientRect()
    return { top: r.top, left: r.left, right: r.right, height: r.height, width: r.width }
  }))
  expect(pair).toHaveLength(2)
  expect(Math.abs(pair[0]!.top - pair[1]!.top)).toBeLessThanOrEqual(1)
  expect(Math.abs(pair[0]!.height - pair[1]!.height)).toBeLessThanOrEqual(1)
  expect(Math.abs(pair[0]!.width - pair[1]!.width)).toBeLessThanOrEqual(1)
  expect(Math.abs(pair[1]!.left - pair[0]!.right - 16)).toBeLessThanOrEqual(1)
  const guides = await page.locator(guidesSelector).evaluateAll(elements => elements.map(el => {
    const r = el.getBoundingClientRect()
    return { top: r.top, left: r.left, right: r.right, height: r.height }
  }))
  expect(guides).toHaveLength(4)
  expect(guides.every(rect => Math.abs(rect.top - guides[0]!.top) <= 1)).toBe(true)
  expect(guides.every(rect => Math.abs(rect.height - guides[0]!.height) <= 1)).toBe(true)
  expect(Math.abs(guides[0]!.left - pair[0]!.left)).toBeLessThanOrEqual(1)
  expect(Math.abs(guides[3]!.right - pair[1]!.right)).toBeLessThanOrEqual(1)
}

async function wheelBodyWithoutMovingFooter(page: Page, viewport: Locator, footer: Locator) {
  const originalFooter = await footer.boundingBox()
  await wheelScroll(page, viewport, 600)
  await expect.poll(() => viewport.evaluate(el => el.scrollTop)).toBeGreaterThan(0)
  expect(await footer.boundingBox()).toEqual(originalFooter)
}

for (const width of [1440, 2560]) {
  test(`real redemption layout keeps equal cards, four guidance panels and fixed paging at ${width}px`, async ({ page }, info) => {
    const { writes, historyRequests } = await interceptAuditAPI(page)
    await openAuditPage(page, 'redeem', info, width)
    const root = page.locator('.redeem-workspace')
    const footer = page.locator('.redeem-history-pagination')
    await expect(page.getByText('Redeemed record 1', { exact: true })).toBeVisible()
    await assertEqualPairAndFourGuides(page, '.redeem-top-row > .card', '.redeem-guide-grid > li')
    await assertFooterAtWorkspaceBottom(footer, root)
    await wheelBodyWithoutMovingFooter(page, page.locator('.redeem-history'), footer)
    const labels = presentation(info).locale === 'zh' ? { next: '下一页', previous: '上一页' } : { next: 'Next', previous: 'Previous' }
    await footer.getByRole('button', { name: labels.next, exact: true }).click()
    await expect(page.getByText('Redeemed record 21', { exact: true })).toBeVisible()
    await expect(page.getByText('Redeemed record 1', { exact: true })).toHaveCount(0)
    expect(historyRequests).toContainEqual({ page: 2, size: 20 })
    await expect.poll(() => page.locator('.redeem-history').evaluate(el => el.scrollTop)).toBe(0)
    await expect(footer.getByRole('button', { name: labels.previous, exact: true })).toBeEnabled()
    await assertFooterAtWorkspaceBottom(footer, root)
    expect(writes).toEqual([])
  })

  test(`real affiliate layout keeps its table independently scrollable with ten actual rows at ${width}px`, async ({ page }, info) => {
    const { writes } = await interceptAuditAPI(page)
    await openAuditPage(page, 'affiliate', info, width)
    const root = page.locator('.affiliate-workspace')
    const footer = page.locator('.affiliate-pagination')
    const rows = page.locator('.affiliate-invitees table.record-table tbody tr')
    await expect(rows).toHaveCount(10)
    await expect(rows.first()).toContainText('Invitee 01')
    await assertEqualPairAndFourGuides(page, '.affiliate-top-row > .card', '.affiliate-guide-grid > li')
    await assertFooterAtWorkspaceBottom(footer, root)
    // The production App.vue table adapter owns the header rail and native body.
    const viewport = page.locator('.affiliate-invitees [data-static-table-body-viewport]')
    await expect(viewport).toBeVisible()
    const header = page.locator('.affiliate-invitees .static-table-header-rail')
    const usableBody = await viewport.evaluate(element => {
      const style = getComputedStyle(element)
      return {
        height: element.clientHeight - parseFloat(style.paddingTop) - parseFloat(style.paddingBottom),
        twoRows: [...element.querySelectorAll('tbody tr')].slice(0, 2).reduce((height, row) => height + row.getBoundingClientRect().height, 0),
      }
    })
    expect(usableBody.height).toBeGreaterThanOrEqual(usableBody.twoRows)
    const frame = page.locator('.affiliate-invitees .static-table-frame')
    expect(await frame.evaluate(element => element.scrollHeight - element.clientHeight)).toBeLessThanOrEqual(1)
    // The frame owns all four outer corners and shares the zone card's curve,
    // so scrolling the body never reveals a cut or mismatched corner.
    const zoneCard = page.locator('.affiliate-invitees')
    const [frameRadius, zoneRadius, viewportRadius] = await Promise.all([
      frame.evaluate(element => parseFloat(getComputedStyle(element).borderRadius)),
      zoneCard.evaluate(element => parseFloat(getComputedStyle(element).borderRadius)),
      viewport.evaluate(element => parseFloat(getComputedStyle(element).borderRadius)),
    ])
    expect(frameRadius).toBe(zoneRadius)
    expect(viewportRadius).toBe(0)
    const headerBefore = await header.boundingBox()
    await wheelBodyWithoutMovingFooter(page, viewport, footer)
    expect(await header.boundingBox()).toEqual(headerBefore)
    expect(await frame.evaluate(element => element.scrollTop)).toBe(0)
    const labels = presentation(info).locale === 'zh' ? { next: '下一页', pageFour: '跳转到第 4 页' } : { next: 'Next', pageFour: 'Go to page 4' }
    await footer.getByRole('button', { name: labels.next, exact: true }).click()
    await expect(rows).toHaveCount(10)
    await expect(rows.first()).toContainText('Invitee 11')
    await expect.poll(() => viewport.evaluate(el => el.scrollTop)).toBe(0)
    await footer.getByRole('button', { name: labels.pageFour, exact: true }).click()
    await expect(rows).toHaveCount(2)
    await expect(rows.first()).toContainText('Invitee 31')
    await expect(footer.getByRole('button', { name: labels.next, exact: true })).toBeDisabled()
    await assertFooterAtWorkspaceBottom(footer, root)
    expect(await page.locator('.affiliate-invitation button').count()).toBe(2)
    await expect(page.locator('.affiliate-transfer button')).toBeEnabled()
    expect(writes).toEqual([])
  })

  test(`real subscriptions keep three columns, nine cards, aligned transparent scroll and fixed footer at ${width}px`, async ({ page }, info) => {
    const { writes } = await interceptAuditAPI(page)
    const { theme, locale } = presentation(info)
    await page.setViewportSize({ width, height: width === 2560 ? 1205 : 900 })
    // Exercise full transparency: the scrollport must not clip its final cards
    // or reserve a blank strip between the cards and the fixed chrome.
    await page.goto(`/layout-tests/index.html?view=user-layout&auditPage=subscriptions&theme=${theme}&locale=${locale}&alpha=0`)
    const header = page.getByTestId('subscriptions-heading')
    const footer = page.locator('.subscriptions-pagination')
    const cards = page.getByTestId('subscription-card')
    const frame = page.getByTestId('subscriptions-activity')
    const viewport = frame.locator('.user-card-scroll-viewport')
    await expect(cards).toHaveCount(9)
    await expect(cards.first()).toContainText('Audit plan 01')
    const cardBoxes = await cards.evaluateAll(elements => elements.map(el => {
      const r = el.getBoundingClientRect()
      return { left: r.left, right: r.right, top: r.top }
    }))
    expect(cardBoxes.slice(0, 3).every(rect => Math.abs(rect.top - cardBoxes[0]!.top) <= 1)).toBe(true)
    expect(cardBoxes[3]!.top).toBeGreaterThan(cardBoxes[0]!.top)
    expect(cardBoxes[6]!.top).toBeGreaterThan(cardBoxes[3]!.top)
    const headingBox = (await header.boundingBox())!
    expect(Math.abs(cardBoxes[0]!.left - headingBox.x)).toBeLessThanOrEqual(1)
    // The scroll viewport widens itself by its scrollbar so the reserved gutter
    // never shrinks the cards; that re-measure settles a frame after the cards
    // appear, so wait for it and then compare the settled geometry.
    await expect.poll(async () => {
      const heading = (await header.boundingBox())!
      const rights = await cards.evaluateAll(elements => elements.map(element => element.getBoundingClientRect().right))
      return Math.abs(rights[2]! - heading.x - heading.width)
    }).toBeLessThanOrEqual(1)
    const settled = await cards.evaluateAll(elements => elements.map(el => {
      const r = el.getBoundingClientRect()
      return { left: r.left, right: r.right }
    }))
    expect(Math.abs(settled[2]!.right - headingBox.x - headingBox.width)).toBeLessThanOrEqual(1)
    const frameBox = (await frame.boundingBox())!
    expect(Math.abs(frameBox.x - headingBox.x)).toBeLessThanOrEqual(1)
    expect(Math.abs(frameBox.width - headingBox.width)).toBeLessThanOrEqual(1)
    expect(await viewport.evaluate(el => ['borderTopLeftRadius', 'borderTopRightRadius', 'borderBottomLeftRadius', 'borderBottomRightRadius'].every(property => parseFloat(getComputedStyle(el).getPropertyValue(property.replace(/[A-Z]/g, letter => `-${letter.toLowerCase()}`))) >= 14))).toBe(true)
    await assertFooterAtWorkspaceBottom(footer, page.locator('.subscriptions-page'))
    const beforeHeader = await header.boundingBox()
    await wheelBodyWithoutMovingFooter(page, viewport, footer)
    expect(await header.boundingBox()).toEqual(beforeHeader)
    const labels = locale === 'zh' ? { next: '下一页', pageFour: '跳转到第 4 页' } : { next: 'Next', pageFour: 'Go to page 4' }
    await footer.getByRole('button', { name: labels.next, exact: true }).filter({ visible: true }).click()
    await expect(cards).toHaveCount(9)
    await expect(cards.first()).toContainText('Audit plan 10')
    await expect.poll(() => viewport.evaluate(el => el.scrollTop)).toBe(0)
    await footer.getByRole('button', { name: labels.pageFour, exact: true }).filter({ visible: true }).click()
    await expect(cards).toHaveCount(1)
    await expect(cards.first()).toContainText('Audit plan 28')
    await expect(footer.getByRole('button', { name: labels.next, exact: true }).filter({ visible: true })).toBeDisabled()
    await assertFooterAtWorkspaceBottom(footer, page.locator('.subscriptions-page'))
    expect(writes).toEqual([])
  })
}

// Acceptance source: the desktop page audit requires subscription titles to use
// the card's available width, with complete renewal actions below them and an
// unbroken expiration label. Three cards per row remain part of UI_INTERACTION.
for (const width of [1280, 1920]) {
  test(`desktop subscription titles use the available card width without squeezing their actions at ${width}px`, async ({ page }, info) => {
    const { writes } = await interceptAuditAPI(page)
    const longName = `DesktopSubscription${'LongPlanName'.repeat(8)}`
    await page.route(url => url.pathname === '/api/v1/subscriptions', route => route.fulfill({
      json: { code: 0, data: [{ ...subscriptions[0], group: {
        ...subscriptions[0]!.group, name: longName,
      } }] },
    }))
    await openAuditPage(page, 'subscriptions', info, width)
    const card = page.getByTestId('subscription-card')
    const renewLabel = presentation(info).locale === 'zh' ? '续费' : 'Renew'
    const renew = card.getByRole('button', { name: renewLabel })
    await expect(renew).toBeVisible()
    const geometry = await card.evaluate(element => {
      const heading = element.querySelector('h3')!
      const headingRect = heading.getBoundingClientRect()
      const button = element.querySelector('button')!
      const buttonRect = button.getBoundingClientRect()
      const header = heading.closest('.border-b')!
      const headerRect = header.getBoundingClientRect()
      const expiryRow = element.querySelector('.space-y-4.p-4')!.firstElementChild!
      const label = expiryRow.firstElementChild!
      const labelRect = label.getBoundingClientRect()
      return {
        headerWidth: headerRect.width,
        titleWidth: headingRect.width,
        actionsBelowTitle: buttonRect.top >= headingRect.bottom,
        expiryLabelHeight: labelRect.height,
        expiryLabelLineHeight: parseFloat(getComputedStyle(label).lineHeight),
        actionHit: button.contains(document.elementFromPoint(buttonRect.left + buttonRect.width / 2, buttonRect.top + buttonRect.height / 2)),
        overflow: element.scrollWidth - element.clientWidth,
      }
    })
    expect(geometry.titleWidth).toBeGreaterThan(geometry.headerWidth - 64)
    expect(geometry.actionsBelowTitle).toBe(true)
    expect(geometry.expiryLabelHeight).toBeLessThanOrEqual(geometry.expiryLabelLineHeight + 1)
    expect(geometry.actionHit).toBe(true)
    expect(geometry.overflow).toBeLessThanOrEqual(1)
    expect(writes).toEqual([])
  })
}

// Acceptance source: the mobile page audit requires long user-controlled labels
// and order references to stay inside their cards, with fully reachable actions
// at both a narrow phone width and the reviewed 390px viewport. Checking only
// document overflow misses content clipped by overflow-hidden cards.
for (const width of [320, 390]) {
  test(`mobile subscription names and renewal actions remain inside their cards at ${width}px`, async ({ page }, info) => {
    const { writes } = await interceptAuditAPI(page)
    const longName = `MobileSubscription${'LongGroupName'.repeat(8)}`
    await page.route(url => url.pathname === '/api/v1/subscriptions', route => route.fulfill({
      json: { code: 0, data: [{ ...subscriptions[0], group: {
        ...subscriptions[0]!.group, name: longName, description: 'UnbrokenDescription'.repeat(8),
      } }] },
    }))
    await openAuditPage(page, 'subscriptions', info, width)
    const card = page.getByTestId('subscription-card')
    await expect(card).toHaveCount(1)
    await expect(card.getByRole('heading', { name: longName })).toBeVisible()
    const renewLabel = presentation(info).locale === 'zh' ? '续费' : 'Renew'
    const renew = card.getByRole('button', { name: renewLabel })
    await renew.scrollIntoViewIfNeeded()
    const bounds = await card.evaluate(element => {
      const cardRect = element.getBoundingClientRect()
      const heading = element.querySelector('h3')!
      const button = element.querySelector('button')!
      const headingRect = heading.getBoundingClientRect()
      const buttonRect = button.getBoundingClientRect()
      return {
        overflow: element.scrollWidth - element.clientWidth,
        headingOverflow: heading.scrollWidth - heading.clientWidth,
        headingInside: headingRect.left >= cardRect.left && headingRect.right <= cardRect.right,
        buttonInside: buttonRect.left >= cardRect.left && buttonRect.right <= cardRect.right,
        buttonHit: [0.15, 0.5, 0.85].every(position => button.contains(document.elementFromPoint(
          buttonRect.left + buttonRect.width * position, buttonRect.top + buttonRect.height / 2,
        ))),
        documentOverflow: document.documentElement.scrollWidth - innerWidth,
      }
    })
    expect(bounds.overflow).toBeLessThanOrEqual(1)
    expect(bounds.headingOverflow).toBeLessThanOrEqual(1)
    expect(bounds.headingInside).toBe(true)
    expect(bounds.buttonInside).toBe(true)
    expect(bounds.buttonHit).toBe(true)
    expect(bounds.documentOverflow).toBeLessThanOrEqual(1)
    await renew.click()
    await expect(page).toHaveURL(/\/purchase\?tab=subscription&group=1$/)
    expect(writes).toEqual([])
  })

  for (const resolved of [true, false]) {
    test(`mobile payment result keeps a long ${resolved ? 'resolved' : 'return'} order reference and actions reachable at ${width}px`, async ({ page }, info) => {
      await interceptAuditAPI(page)
      const reference = `MOBILE${'1234567890'.repeat(12)}`
      await page.route('**/api/v1/**/orders/verify', route => route.fulfill(resolved ? {
        json: { code: 0, data: { out_trade_no: reference, status: 'FAILED', paid: false } },
      } : { status: 404, json: { code: 404, message: 'Order not found' } }))
      const { theme, locale } = presentation(info)
      await page.setViewportSize({ width, height: 844 })
      await page.goto(`/layout-tests/index.html?view=user-layout&auditPage=payment-result&theme=${theme}&locale=${locale}&out_trade_no=${reference}&trade_status=TRADE_CLOSED`)
      const orderReference = page.getByText(reference, { exact: true })
      await expect(orderReference).toBeVisible()
      const card = page.locator('.card').filter({ has: orderReference })
      expect(await card.evaluate(element => element.scrollWidth - element.clientWidth)).toBeLessThanOrEqual(1)
      expect(await orderReference.evaluate(element => element.scrollWidth - element.clientWidth)).toBeLessThanOrEqual(1)
      expect(await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)).toBeLessThanOrEqual(1)
      const actions = card.getByRole('button')
      await expect(actions).toHaveCount(2)
      for (const action of await actions.all()) {
        await action.scrollIntoViewIfNeeded()
        expect(await action.evaluate(element => {
          const rect = element.getBoundingClientRect()
          return rect.left >= 0 && rect.right <= innerWidth
            && element.contains(document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2))
        })).toBe(true)
      }
      await actions.last().click()
      await expect(page).toHaveURL(/\/orders$/)
    })
  }
}


// Audit feedback: the aggregate action must be as prominent as the ordinary
// creation action, and every selected group name must remain readable in place.
async function interceptAggregateKeyAPI(page: Page, saved = false) {
  await interceptAuditAPI(page)
  const keyWrites: string[] = []
  page.on('request', request => {
    const path = new URL(request.url()).pathname
    // Bulk usage reads use POST; key configuration must remain unchanged until Save.
    if (request.method() !== 'GET' && /^\/api\/v1\/keys(?:\/|$)/.test(path)) keyWrites.push(`${request.method()} ${path}`)
  })
  const groups = [
    { id: 1, name: 'Claude 生产环境按量分组 · Production messages', platform: 'anthropic' },
    { id: 2, name: 'OpenAI-production-' + 'longscope'.repeat(10), platform: 'openai' },
    { id: 3, name: 'Gemini 按量分组 · Production generate content', platform: 'gemini' },
  ].map(item => ({ ...item, status: 'active', subscription_type: 'standard', rate_multiplier: 1 }))
  const items = saved ? [{ id: 99901, name: 'Saved aggregate colors', key: 'sk-browser-fixture', status: 'active', group_binding_mode: 'custom', group_id: null, aggregate_group_ids: [1, 2, 3], created_at: '2026-01-01T00:00:00Z' }] : []
  await page.route(url => url.pathname === '/api/v1/keys', route => route.fulfill({ json: { code: 0, data: { items, total: items.length, pages: saved ? 1 : 0, page: 1, page_size: 20 } } }))
  await page.route(url => url.pathname === '/api/v1/usage/dashboard/api-keys-usage', route => route.fulfill({ json: { code: 0, data: { stats: {} } } }))
  await page.route(url => url.pathname === '/api/v1/groups/available', route => route.fulfill({ json: { code: 0, data: groups } }))
  await page.route(url => url.pathname === '/api/v1/groups/rates', route => route.fulfill({ json: { code: 0, data: {} } }))
  await page.route(url => url.pathname === '/api/v1/channels/available', route => route.fulfill({ json: { code: 0, data: { groups: [], user_rate_status: 'loaded' } } }))
  return { keyWrites, groups }
}

test('aggregate key creation is as prominent as ordinary key creation', async ({ page }, info) => {
  await interceptAggregateKeyAPI(page)
  await openAuditPage(page, 'keys', info, 1440)
  const aggregate = page.getByTestId('create-aggregate-key')
  const ordinary = page.locator('[data-tour="keys-create-btn"]')
  await expect(aggregate).toBeVisible()
  const appearance = (element: Element) => {
    const style = getComputedStyle(element)
    return { fill: style.backgroundColor, gradient: style.backgroundImage, border: style.borderColor, foreground: style.color }
  }
  expect(await aggregate.evaluate(appearance)).toEqual(await ordinary.evaluate(appearance))
})

test('aggregate group selections show every full name and keep the open menu clear of the growing field', async ({ page }, info) => {
  const { keyWrites, groups } = await interceptAggregateKeyAPI(page)
  const locale = presentation(info).locale
  for (const width of [375, 768, 1440]) {
    await openAuditPage(page, 'keys', info, width)
    await page.getByTestId('create-aggregate-key').click()
    await page.locator('#key-aggregate-mode').click()
    await page.getByRole('option', { name: locale === 'zh' ? '自定义分组' : 'Custom groups', exact: true }).click()
    const field = page.getByTestId('aggregate-key-fields')
    await expect(field).not.toContainText('Composite 和订阅分组继续使用原有 Key 入口。')
    await expect(field).not.toContainText('Composite and subscription groups use the original key flow.')
    const trigger = field.getByRole('button', { name: locale === 'zh' ? '选择按量分组' : 'Select pay-as-you-go groups', exact: true })
    await trigger.click()
    const panel = page.locator(`#${await trigger.getAttribute('aria-controls')}`)
    const colors = []
    for (const group of groups) {
      const checkbox = panel.getByRole('checkbox', { name: `${group.name} · ×1`, exact: true })
      const option = checkbox.locator('..').getByTestId('aggregate-group-option')
      const color = await option.evaluate(element => getComputedStyle(element).color)
      colors.push(color)
      await checkbox.check()
      await expect(trigger).toContainText(group.name)
      const badge = trigger.getByTestId('aggregate-selected-group').filter({ hasText: group.name })
      expect(await badge.evaluate(element => getComputedStyle(element).color)).toBe(color)
    }
    expect(new Set(colors).size).toBe(3)
    const expectFullSelection = async () => {
      await expect.poll(() => trigger.evaluate(element => {
        const label = element.querySelector('span')!
        const bounds = label.getBoundingClientRect()
        const range = document.createRange()
        range.selectNodeContents(label)
        const field = element.getBoundingClientRect()
        const dialog = element.closest('.modal-content')!.getBoundingClientRect()
        return {
          allNamesFit: label.scrollWidth <= label.clientWidth + 1 && label.scrollHeight <= label.clientHeight + 1 && [...range.getClientRects()].every(rect => rect.left >= bounds.left - 1 && rect.right <= bounds.right + 1 && rect.top >= bounds.top - 1 && rect.bottom <= bounds.bottom + 1),
          fieldFitsDialog: field.left >= dialog.left && field.right <= dialog.right,
        }
      })).toEqual({ allNamesFit: true, fieldFitsDialog: true })
    }
    await expectFullSelection()
    await expect.poll(async () => {
      const menu = (await panel.boundingBox())!
      const anchor = (await trigger.boundingBox())!
      return menu.y + menu.height <= anchor.y || menu.y >= anchor.y + anchor.height
    }).toBe(true)
    await page.keyboard.press('Escape')
    await expect(panel).toHaveCount(0)
    await expectFullSelection()
    await trigger.click()
    for (const group of groups) await expect(panel.getByRole('checkbox', { name: `${group.name} · ×1`, exact: true })).toBeChecked()
    await panel.getByRole('checkbox', { name: `${groups[1]!.name} · ×1`, exact: true }).uncheck()
    await expect(trigger).not.toContainText(groups[1]!.name)
    await expect(trigger).toContainText(groups[0]!.name)
    await expect(trigger).toContainText(groups[2]!.name)
    await page.keyboard.press('Escape')
    await expectFullSelection()
  }
  expect(keyWrites).toEqual([])
})

test('aggregate editing retains the ordinary key platform colors for saved group selections', async ({ page }, info) => {
  const pageErrors: string[] = []
  page.on('pageerror', error => pageErrors.push(error.message))
  const { keyWrites, groups } = await interceptAggregateKeyAPI(page, true)
  const locale = presentation(info).locale
  await openAuditPage(page, 'keys', info, 1440)
  // Read the existing ordinary-key option as the reference; no replacement
  // palette or class names in this test can define what "matches" means.
  await page.locator('[data-tour="keys-create-btn"]').click()
  await page.locator('[data-tour="key-form-group"]').click()
  const reference = page.getByRole('option').filter({ hasText: groups[0]!.name }).locator('.groupOptionItemBadge')
  await expect(reference).toBeVisible()
  const ordinaryColor = await reference.evaluate(element => getComputedStyle(element).color)
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: 'Close modal', exact: true }).click()

  await page.getByRole('button', { name: locale === 'zh' ? '编辑' : 'Edit', exact: true }).click()
  const field = page.getByTestId('aggregate-key-fields')
  const trigger = field.getByRole('button', { name: locale === 'zh' ? '选择按量分组' : 'Select pay-as-you-go groups', exact: true })
  await expect(trigger.getByTestId('aggregate-selected-group')).toHaveCount(3)
  const colors = []
  for (const group of groups) {
    const badge = trigger.getByTestId('aggregate-selected-group').filter({ hasText: group.name })
    colors.push(await badge.evaluate(element => getComputedStyle(element).color))
  }
  expect(colors[0]).toBe(ordinaryColor)
  expect(new Set(colors).size).toBe(3)
  await trigger.click()
  const panel = page.locator(`#${await trigger.getAttribute('aria-controls')}`)
  for (const [index, group] of groups.entries()) {
    const checkbox = panel.getByRole('checkbox', { name: `${group.name} · ×1`, exact: true })
    await expect(checkbox).toBeChecked()
    expect(await checkbox.locator('..').getByTestId('aggregate-group-option').evaluate(element => getComputedStyle(element).color)).toBe(colors[index])
  }
  await panel.getByRole('checkbox', { name: `${groups[1]!.name} · ×1`, exact: true }).uncheck()
  await expect(trigger.getByTestId('aggregate-selected-group')).toHaveCount(2)
  expect(keyWrites).toEqual([])
  expect(pageErrors).toEqual([])
})

// The user must be able to read and copy CLI configuration in either theme.
// Contrast is computed from the rendered production code and composited surface,
// independently of the classes chosen by the component.
for (const width of [1280, 1920]) for (const platform of ['openai', 'gemini']) {
  test(`API key instructions keep readable code and copy controls at ${width}px for ${platform}`, async ({ page }, info) => {
    await interceptAuditAPI(page)
    await page.route(url => url.pathname === '/api/v1/keys', route => route.fulfill({ json: { code: 0, data: {
      items: [{ id: 99901, name: 'Readability review key', key: 'sk-read-only-browser-fixture', status: 'active',
        created_at: '2026-01-01T00:00:00Z', group_id: 99901, group: {
          id: 99901, name: 'CLI readability review', platform, status: 'active', rate_multiplier: 1,
        } }], total: 1, pages: 1, page: 1, page_size: 20,
    } } }))
    await page.route(url => url.pathname === '/api/v1/usage/dashboard/api-keys-usage', route => route.fulfill({ json: { code: 0, data: { stats: {} } } }))
    await openAuditPage(page, 'keys', info, width)
    const accented = new URL(page.url())
    accented.searchParams.set('accent', '#ec4899')
    await page.goto(accented.href)
    const locale = presentation(info).locale
    await page.getByRole('button', { name: locale === 'zh' ? '使用密钥' : 'Use Key', exact: true }).click()
    const code = page.locator('.modal-body pre').first()
    await expect(code).toBeVisible()
    await expect(code).toContainText(platform === 'openai' ? 'model_provider' : 'GOOGLE_GEMINI_BASE_URL')
    const readContrast = () => code.evaluate(element => {
      type Color = [number, number, number, number]
      const pixel = document.createElement('canvas')
      pixel.width = pixel.height = 1
      const context = pixel.getContext('2d')!
      const parse = (value: string): Color => {
        context.clearRect(0, 0, 1, 1)
        context.fillStyle = value
        context.fillRect(0, 0, 1, 1)
        const rgba = context.getImageData(0, 0, 1, 1).data
        return [rgba[0]!, rgba[1]!, rgba[2]!, rgba[3]! / 255]
      }
      const over = (top: Color, bottom: Color): Color => {
        const alpha = top[3] + bottom[3] * (1 - top[3])
        return [0, 1, 2].map(i => (top[i]! * top[3] + bottom[i]! * bottom[3] * (1 - top[3])) / alpha).concat(alpha) as Color
      }
      const background = (target: Element): Color => {
        const ancestors: Element[] = []
        for (let current: Element | null = target; current; current = current.parentElement) ancestors.unshift(current)
        return ancestors.reduce((color, node) => over(parse(getComputedStyle(node).backgroundColor), color), [255, 255, 255, 1] as Color)
      }
      const luminance = (color: Color) => {
        const linear = color.slice(0, 3).map(value => { const channel = value / 255; return channel <= .04045 ? channel / 12.92 : ((channel + .055) / 1.055) ** 2.4 })
        return .2126 * linear[0]! + .7152 * linear[1]! + .0722 * linear[2]!
      }
      const ratio = (target: Element) => {
        const bg = background(target), fg = over(parse(getComputedStyle(target).color), bg)
        const a = luminance(bg), b = luminance(fg)
        return (Math.max(a, b) + .05) / (Math.min(a, b) + .05)
      }
      const blocks = [...element.closest('.modal-body')!.querySelectorAll('pre')]
      const targets = blocks.flatMap(block => [block, ...block.querySelectorAll('span'), block.parentElement!.querySelector('button')!])
      return Math.min(...targets.filter(target => target.textContent?.trim()).map(ratio))
    })
    expect(await readContrast()).toBeGreaterThanOrEqual(4.5)
    const copy = code.locator('..').getByRole('button')
    await copy.hover()
    await copy.evaluate(element => Promise.all(element.getAnimations().map(animation => animation.finished.catch(() => undefined))))
    expect(await readContrast()).toBeGreaterThanOrEqual(4.5)
    await copy.click()
    await expect(copy).toHaveText(locale === 'zh' ? '已复制' : 'Copied')
    await copy.evaluate(element => Promise.all(element.getAnimations().map(animation => animation.finished.catch(() => undefined))))
    expect(await readContrast()).toBeGreaterThanOrEqual(4.5)
    await expect(copy).toHaveText(locale === 'zh' ? '已复制' : 'Copied')
  })
}
