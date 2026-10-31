import { expect, test, type Locator, type Page } from '@playwright/test'
import { wheelScroll } from './wheel'

test.beforeEach(async ({ page }) => {
  await page.route('**/api/v1/**', route => route.fulfill({ json: { code: 0, data: { items: [], total: 0, pages: 0, enabled: false } } }))
})

test('admin keeps original horizontal reservations and fixed record chrome', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await page.setViewportSize({ width: 1440, height: 800 })
  await page.goto(`/layout-tests/index.html?view=admin-layout&theme=${theme}&locale=${locale}`)
  const toolbar = page.locator('.table-page-layout > .layout-section-fixed').first()
  const pagination = page.locator('.pagination-card')
  const table = page.locator('.record-table-scroll')
  await expect(pagination).toBeVisible()
  const assertInset = async () => {
    const side = await page.locator('.sidebar').boundingBox()
    const card = await toolbar.boundingBox()
    // Existing desktop shell contract is exactly 24px beyond either sidebar.
    expect(Math.abs(card!.x - side!.x - side!.width - 24)).toBeLessThanOrEqual(1)
    expect(Math.abs(1440 - card!.x - card!.width - 24)).toBeLessThanOrEqual(1)
  }
  await assertInset()
  await page.getByTestId('admin-collapse').click()
  await expect.poll(() => page.locator('.sidebar').evaluate(el => Math.round(el.getBoundingClientRect().width))).toBe(72)
  await assertInset()
  for (const surface of [toolbar, pagination]) {
    const style = await surface.evaluate(el => ({ radius: parseFloat(getComputedStyle(el).borderRadius), border: parseFloat(getComputedStyle(el).borderTopWidth), fill: getComputedStyle(el).backgroundColor }))
    expect(style.radius).toBe(16)
    expect(style.border).toBe(1)
    expect(style.fill).not.toBe('rgba(0, 0, 0, 0)')
  }
  const fixedToolbar = await toolbar.boundingBox()
  const fixedPagination = await pagination.boundingBox()
  expect(await table.evaluate(el => el.scrollHeight > el.clientHeight)).toBe(true)
  expect(await table.evaluate(el => el.scrollWidth > el.clientWidth)).toBe(true)
  const first = page.locator('table.record-table tbody tr td[data-field="name"]').first()
  const action = page.locator('table.record-table tbody tr td[data-field="actions"]').first()
  expect(await first.evaluate(el => getComputedStyle(el).position)).toBe('sticky')
  expect(await action.evaluate(el => getComputedStyle(el).position)).toBe('sticky')
  const firstX = (await first.boundingBox())!.x
  const actionX = (await action.boundingBox())!.x
  await table.evaluate(el => { el.scrollLeft = el.scrollWidth })
  expect(Math.abs((await first.boundingBox())!.x - firstX)).toBeLessThanOrEqual(1)
  expect(Math.abs((await action.boundingBox())!.x - actionX)).toBeLessThanOrEqual(1)
  await action.getByRole('button', { name: 'Edit record', exact: true }).click()
  await expect(page.getByTestId('admin-last-action')).toHaveText('1:Edit record')
  await wheelScroll(page, table, 700)
  await expect.poll(() => table.evaluate(el => el.scrollTop)).toBeGreaterThan(0)
  expect(await toolbar.boundingBox()).toEqual(fixedToolbar)
  expect(await pagination.boundingBox()).toEqual(fixedPagination)
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
})

test('admin neutral theme reaches portalled controls at every transparency', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  for (const alpha of [0, 0.5, 1]) {
    await page.setViewportSize({ width: 1440, height: 800 })
    await page.goto(`/layout-tests/index.html?view=admin-layout&theme=${theme}&locale=${locale}&alpha=${alpha}`)
    await expect(page.locator('.app-layout-admin')).toBeVisible()
    await expect(page.locator('html')).toHaveAttribute('data-product-ui', '')
    const palette = await page.locator('.app-layout-admin').evaluate(el => ({ text: getComputedStyle(el).getPropertyValue('--surface-text').trim(), canvas: getComputedStyle(el).getPropertyValue('--console-canvas').trim(), alpha: getComputedStyle(el).getPropertyValue('--console-card-alpha').trim() }))
    expect(palette.text).toBe(theme === 'dark' ? '#f1f1f3' : '#202124')
    expect(palette.canvas).toBe(theme === 'dark' ? '#191a1d' : '#f6f6f7')
    expect(Number(palette.alpha)).toBe(alpha)
    const trigger = page.getByTestId('admin-select')
    await trigger.focus()
    await page.keyboard.press('Enter')
    const panel = page.locator('.select-dropdown-portal')
    await expect(panel).toBeVisible()
    expect(await panel.evaluate(el => el.parentElement === document.body)).toBe(true)
    expect(await panel.evaluate(el => getComputedStyle(el).getPropertyValue('--surface-text').trim())).toBe(palette.text)
    await page.keyboard.press('Escape')
    await expect(panel).toHaveCount(0)
    await expect(trigger).toBeFocused()
  }
})

test('admin mobile cards retain every field, selection and actions within original shell insets', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  // console.css authorizes 12px below 768px and 16px from 768px to 1023px.
  for (const [width, inset] of [[320, 12], [390, 12], [768, 16]]) {
    await page.setViewportSize({ width, height: 900 })
    await page.goto(`/layout-tests/index.html?view=admin-layout&theme=${theme}&locale=${locale}`)
    const toolbar = page.locator('.table-page-layout > .layout-section-fixed').first()
    await expect(toolbar).toBeVisible()
    const box = (await toolbar.boundingBox())!
    expect(Math.abs(box.x - inset)).toBeLessThanOrEqual(1)
    expect(Math.abs(width - box.x - box.width - inset)).toBeLessThanOrEqual(1)
    if (width === 768) {
      await expect(page.locator('table.record-table')).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
      continue
    }
    const card = page.locator('.record-card-fields').first()
    await expect(card).toBeVisible()
    for (const [field, value] of [
      ['name', 'Admin record 1'], ['model', 'gpt-5.4-admin-layout-long-model-name-complete'],
      ['group', '管理端完整分组名称 Admin complete routing group'],
      ['endpoint', '/v1/responses/admin-layout-independent-regression'],
      ['request_id', 'admin-layout-request-1-independent-fixture'],
    ]) {
      await expect(card.locator(`[data-field="${field}"]`)).toContainText(value)
      const fieldBox = (await card.locator(`[data-field="${field}"]`).boundingBox())!
      expect(fieldBox.x).toBeGreaterThanOrEqual(inset)
      expect(fieldBox.x + fieldBox.width).toBeLessThanOrEqual(width - inset + 1)
    }
    await card.locator('input[data-test="select-row"]').check()
    await expect(card.locator('input[data-test="select-row"]')).toBeChecked()
    await card.getByRole('button', { name: 'Inspect usage', exact: true }).click()
    await expect(page.getByTestId('admin-last-action')).toHaveText('1:Inspect usage')
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
  }
})

// Acceptance: administrator usage adopts the user lists' separate rounded
// controls, record body and footer cards, with 16px gaps. It retains the admin
// fluid width, frozen columns, horizontal scrolling and live-window semantics.
async function mockAdminUsage(page: Page, empty = false) {
  await page.addInitScript(() => localStorage.setItem('table-page-size', '20'))
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    const current = Number(url.searchParams.get('page') || 1)
    const size = Number(url.searchParams.get('page_size') || 20)
    const total = empty ? 0 : 48
    const ids = Array.from({ length: Math.max(0, Math.min(size, total - (current - 1) * size)) }, (_, index) => (current - 1) * size + index + 1)
    let data: unknown = { items: [], total: 0, pages: 0, enabled: false }
    if (url.pathname === '/api/v1/admin/usage') {
      data = { total, page: current, page_size: size, pages: Math.ceil(total / size), items: ids.map(id => ({
        id, user_id: 9998, api_key_id: 1, account_id: 1, request_id: `admin-usage-${id}`, model: `admin-history-model-${id}`,
        user: { id: 9998, email: 'administrator-layout-record@example.test' },
        api_key: { id: 1, name: 'Admin layout key' }, account: { id: 1, name: 'Admin provider account' },
        group: { id: 1, name: 'Admin routing group', platform: 'openai' }, group_id: 1,
        inbound_endpoint: '/v1/responses', upstream_endpoint: '/v1/responses', reasoning_effort: 'high',
        input_tokens: 1200, output_tokens: 300, cache_creation_tokens: 0, cache_read_tokens: 0,
        input_cost: .01, output_cost: .02, total_cost: .03, actual_cost: .03, rate_multiplier: 1,
        billing_type: 0, billing_mode: 'token', stream: true, request_type: 'stream', duration_ms: 1000,
        first_token_ms: 100, created_at: '2026-10-29T10:00:00Z', user_agent: 'Admin-Layout/1.0',
      })) }
    } else if (url.pathname === '/api/v1/admin/ops/errors') {
      data = { total, page: current, page_size: size, items: ids.map(id => ({
        id, user_id: 9998, user_email: 'administrator-layout-record@example.test', account_id: 1,
        account_name: 'Admin provider account', api_key_id: 1, api_key_name: 'Admin layout key',
        requested_model: `admin-error-model-${id}`, model: `admin-error-model-${id}`,
        phase: 'upstream', type: 'upstream_error', status_code: 429, message: `Admin error record ${id}`,
        request_type: 2, created_at: '2026-10-29T10:00:00Z',
      })) }
    } else if (url.pathname === '/api/v1/admin/usage/live') {
      const now = new Date().toISOString()
      data = { server_now: now, window_seconds: 300, limit: 200, truncated: false, poll_after_seconds: 30,
        items: Array.from({ length: empty ? 0 : 48 }, (_, index) => ({
          id: `admin-live-${index + 1}`, version: 1, user_id: 9998, api_key_id: 1, key_name: 'Admin layout key',
          model: `admin-live-model-${index + 1}`, group_name: 'Admin routing group', endpoint: '/v1/responses',
          request_type: 'stream', status: 'completed', started_at: now, updated_at: now, finished_at: now,
          elapsed_ms: 1000, duration_ms: 1000, first_token_ms: 100, input_tokens: 1200, output_tokens: 300, actual_cost: .03,
        })) }
    } else if (url.pathname === '/api/v1/admin/usage/stats') {
      data = { total_requests: total, total_input_tokens: 0, total_output_tokens: 0, total_cache_tokens: 0,
        total_cache_creation_tokens: 0, total_cache_read_tokens: 0, total_tokens: 0, total_cost: 0,
        total_actual_cost: 0, total_account_cost: 0, average_duration_ms: 0 }
    } else if (url.pathname === '/api/v1/admin/dashboard/models') data = { models: [] }
    else if (url.pathname === '/api/v1/admin/dashboard/snapshot-v2') data = { trend: [], models: [], groups: [] }
    await route.fulfill({ json: { code: 0, data } })
  })
}

async function assertAdminSurfaceStack(page: Page, surfaces: Locator[]) {
  const sidebar = (await page.locator('.sidebar').boundingBox())!
  const width = page.viewportSize()!.width
  let bottom: number | undefined
  for (const surface of surfaces) {
    await expect(surface).toBeInViewport()
    const box = (await surface.boundingBox())!
    expect(Math.abs(box.x - sidebar.x - sidebar.width - 24)).toBeLessThanOrEqual(1)
    expect(Math.abs(width - box.x - box.width - 24)).toBeLessThanOrEqual(1)
    const style = await surface.evaluate(el => {
      const css = getComputedStyle(el)
      return { radius: parseFloat(css.borderRadius), border: parseFloat(css.borderTopWidth), fill: css.backgroundColor }
    })
    expect(style.radius).toBe(16)
    expect(style.border).toBe(1)
    expect(style.fill).not.toBe('rgba(0, 0, 0, 0)')
    if (bottom !== undefined) expect(Math.abs(box.y - bottom - 16)).toBeLessThanOrEqual(1)
    bottom = box.y + box.height
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
}

test('real admin usage lists separate controls, scrolling records and paging without narrowing the page', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await mockAdminUsage(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto(`/layout-tests/index.html?view=admin-layout&usage=1&theme=${theme}&locale=${locale}`)
  const panel = page.getByTestId('usage-records-panel')
  const controls = panel.locator('.usage-record-controls')
  await expect(panel.getByText('admin-history-model-1', { exact: true })).toBeVisible({ timeout: 60_000 })
  for (const tab of ['usage', 'errors', 'live']) {
    await panel.locator(`[data-tab="${tab}"]`).click()
    const workspace = panel.locator(tab === 'usage' ? '.usage-history-workspace' : tab === 'errors' ? '.usage-errors-workspace' : '.live-admin-workspace')
    const body = workspace.locator('.card.workspace-table-card')
    const viewport = body.locator('.workspace-table-viewport').filter({ visible: true })
    await expect(viewport.locator('table.record-table tbody tr:not([aria-hidden="true"])').first()).toContainText(`admin-${tab === 'usage' ? 'history' : tab === 'errors' ? 'error' : 'live'}-model-1`)
    const surfaces = [controls]
    if (tab === 'usage') surfaces.push(workspace.locator('.usage-history-refresh'))
    if (tab === 'live') surfaces.push(workspace.locator('.card.workspace-fixed'))
    surfaces.push(body)
    const pagination = workspace.locator('.pagination-card')
    if (tab !== 'live') surfaces.push(pagination)
    else {
      await expect(pagination).toHaveCount(0)
      await expect(viewport.locator('tbody tr:not([aria-hidden="true"])')).toHaveCount(48)
    }
    await assertAdminSurfaceStack(page, surfaces)
    const fixedBoxes = await Promise.all(surfaces.filter(surface => surface !== body).map(surface => surface.boundingBox()))
    await wheelScroll(page, viewport, 500)
    await expect.poll(() => viewport.evaluate(el => el.scrollTop)).toBeGreaterThan(0)
    expect(await Promise.all(surfaces.filter(surface => surface !== body).map(surface => surface.boundingBox()))).toEqual(fixedBoxes)
    if (tab === 'usage') {
      expect(await viewport.evaluate(el => el.scrollWidth > el.clientWidth)).toBe(true)
      const frozen = viewport.locator('tbody tr:not([aria-hidden="true"])').first().locator('td[data-field="user"]')
      await expect(frozen).toHaveCSS('position', 'sticky')
      const left = (await frozen.boundingBox())!.x
      await viewport.evaluate(el => { el.scrollLeft = el.scrollWidth })
      expect(Math.abs((await frozen.boundingBox())!.x - left)).toBeLessThanOrEqual(1)
    }
    if (tab !== 'live') {
      await pagination.getByRole('button', { name: locale === 'zh' ? '下一页' : 'Next', exact: true }).filter({ visible: true }).click()
      await expect(viewport.locator('tbody tr:not([aria-hidden="true"])').first()).toContainText(`admin-${tab === 'usage' ? 'history' : 'error'}-model-21`)
    }
  }
})

test('empty admin history and errors keep separate visible pagination cards', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await mockAdminUsage(page, true)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto(`/layout-tests/index.html?view=admin-layout&usage=1&theme=${theme}&locale=${locale}`)
  const panel = page.getByTestId('usage-records-panel')
  for (const tab of ['usage', 'errors']) {
    await panel.locator(`[data-tab="${tab}"]`).click()
    const workspace = panel.locator(tab === 'usage' ? '.usage-history-workspace' : '.usage-errors-workspace')
    const footer = workspace.locator('.pagination-card')
    await expect(footer).toBeInViewport()
    await expect(footer.getByRole('button', { name: locale === 'zh' ? '下一页' : 'Next', exact: true }).filter({ visible: true })).toBeDisabled()
    expect(await footer.evaluate(el => el.closest('.workspace-table-card'))).toBeNull()
    await expect(workspace.locator('.workspace-table-viewport .empty-state')).toBeVisible()
  }
})

test('real admin usage cards and paging remain reachable inside phone shell insets', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await mockAdminUsage(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto(`/layout-tests/index.html?view=admin-layout&usage=1&theme=${theme}&locale=${locale}`)
  const panel = page.getByTestId('usage-records-panel')
  await expect(panel.getByText('admin-history-model-1', { exact: true })).toBeVisible({ timeout: 60_000 })
  for (const tab of ['usage', 'errors', 'live']) {
    await panel.locator(`[data-tab="${tab}"]`).click()
    const workspace = panel.locator(tab === 'usage' ? '.usage-history-workspace' : tab === 'errors' ? '.usage-errors-workspace' : '.live-admin-workspace')
    const body = workspace.locator('.card.workspace-table-card')
    await expect(body).toBeVisible()
    for (const surface of [panel.locator('.usage-record-controls'), body]) {
      const box = (await surface.boundingBox())!
      expect(Math.abs(box.x - 12)).toBeLessThanOrEqual(1)
      expect(Math.abs(390 - box.x - box.width - 12)).toBeLessThanOrEqual(1)
    }
    if (tab === 'live') {
      await expect(body.locator('article')).toHaveCount(48)
      const first = body.locator('article').first()
      await first.getByRole('button', { name: locale === 'zh' ? '更多字段' : 'More fields', exact: true }).click()
      await expect(first.locator('dl')).toBeVisible()
      await expect(first.locator('dl')).toContainText('Admin layout key')
      await expect(workspace.locator('.pagination-card')).toHaveCount(0)
    } else {
      const footer = workspace.locator('.pagination-card')
      await footer.scrollIntoViewIfNeeded()
      await expect(footer).toBeInViewport()
      await footer.getByRole('button', { name: locale === 'zh' ? '下一页' : 'Next', exact: true }).filter({ visible: true }).click()
      await expect(body.getByText(`admin-${tab === 'usage' ? 'history' : 'error'}-model-21`, { exact: true })).toHaveCount(1)
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
  }
})
