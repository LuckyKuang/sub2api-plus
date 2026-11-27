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
    // Zone containers carry the shell page radius, never the card radius.
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
    expect(palette.text).toBe(theme === 'dark' ? '#f2f0ed' : '#221f1c')
    expect(palette.canvas).toBe(theme === 'dark' ? '#1c1a18' : '#f4f1ed')
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

test('mobile prompt audit configuration keeps long group choices and save controls inside the page', async ({ page }, info) => {
  // Acceptance source: every mobile admin page and its configuration tabs must
  // fit the phone. A selected-group policy with a long name must not create an
  // implicit min-content grid track wider than the configuration panel.
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  const name = `PromptAuditGroup${'UnbrokenRoutingGroup'.repeat(6)}`
  const writes: string[] = []
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    if (request.method() !== 'GET') writes.push(`${request.method()} ${url.pathname}`)
    let data: unknown = { items: [], total: 0, pages: 0, enabled: false }
    if (url.pathname === '/api/v1/admin/prompt-audit/config') data = {
      enabled: false, blocking_enabled: false, store_pass_events: false, effective_mode: 'off', strategy: 'priority',
      worker_count: 4, queue_capacity: 100, scanners: ['prompt_injection'], all_groups: false, group_ids: [1],
      endpoints: [], config_version: 1, updated_at: '2026-07-16T00:00:00Z', updated_by: 1, change_summary: '{}',
    }
    else if (url.pathname === '/api/v1/admin/groups/all') data = [{ id: 1, name, platform: 'openai', status: 'active' }]
    else if (url.pathname === '/api/v1/admin/prompt-audit/runtime') data = {
      process_status: 'disabled', effective_mode: 'off', expected_config_version: 1, active_config_version: 1,
      worker_total: 0, worker_active: 0, queue_capacity: 100,
      queue: { staging: 0, queued: 0, processing: 0, retry: 0, done: 0, failed: 0, active: 0 },
      processed_total: 0, failed_total: 0, enqueued_total: 0, dropped_total: 0,
      extraction_attempted: 0, extraction_succeeded: 0, extraction_empty: 0, extraction_failed: 0,
      database_status: 'ok', redis_status: 'ok', endpoints: {},
    }
    await route.fulfill({ json: { code: 0, data } })
  })
  for (const width of [320, 390]) {
    await page.setViewportSize({ width, height: 844 })
    await page.goto(`/layout-tests/index.html?view=admin-layout&promptAudit=1&theme=${theme}&locale=${locale}`)
    await page.locator('[data-test="tab-config"]').click()
    const panel = page.locator('[data-test="tab-panel-config"]')
    const choice = panel.getByRole('checkbox', { name: new RegExp(name) })
    await expect(choice).toBeChecked()
    await expect(panel.getByText(name, { exact: true })).toBeVisible()
    const geometry = await panel.evaluate(element => ({
      panelOverflow: element.scrollWidth - element.clientWidth,
      documentOverflow: document.documentElement.scrollWidth - innerWidth,
      fieldsets: [...element.querySelectorAll('fieldset')].map(fieldset => fieldset.scrollWidth - fieldset.clientWidth),
    }))
    expect(geometry.panelOverflow).toBeLessThanOrEqual(1)
    expect(geometry.documentOverflow).toBeLessThanOrEqual(1)
    expect(geometry.fieldsets.every(overflow => overflow <= 1)).toBe(true)
    await choice.uncheck()
    await expect(choice).not.toBeChecked()
    const save = page.locator('[data-test="save-config"]')
    await expect(save).toBeEnabled()
    await save.scrollIntoViewIfNeeded()
    expect(await save.evaluate(element => {
      const rect = element.getBoundingClientRect()
      return rect.left >= 0 && rect.right <= innerWidth
        && element.contains(document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2))
    })).toBe(true)
  }
  expect(writes).toEqual([])
})

// Acceptance: administrator usage adopts the user lists' separate rounded
// controls, record body and footer cards, with 16px gaps. It retains the admin
// fluid width, frozen columns, horizontal scrolling and paged-record semantics.
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
    // Every zone surface shares the shell page radius, never the card radius.
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
  const controls = page.getByTestId('usage-query-card')
  await expect(panel.getByText('admin-history-model-1', { exact: true })).toBeVisible({ timeout: 60_000 })
  for (const tab of ['usage', 'errors']) {
    await page.locator(`[data-tab="${tab}"]`).click()
    const workspace = panel.locator(tab === 'usage' ? '.usage-history-workspace' : '.usage-errors-workspace')
    const body = workspace.locator('.card.workspace-table-card')
    const viewport = body.locator('.workspace-table-viewport').filter({ visible: true })
    await expect(viewport.locator('table.record-table tbody tr:not([aria-hidden="true"])').first()).toContainText(`admin-${tab === 'usage' ? 'history' : 'error'}-model-1`)
    const surfaces = [controls, body]
    const pagination = workspace.locator('.pagination-card')
    surfaces.push(pagination)
    await expect(viewport.locator('tbody tr:not([aria-hidden="true"])')).toHaveCount(20)
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
    await pagination.getByRole('button', { name: locale === 'zh' ? '下一页' : 'Next', exact: true }).filter({ visible: true }).click()
    await expect(viewport.locator('tbody tr:not([aria-hidden="true"])').first()).toContainText(`admin-${tab === 'usage' ? 'history' : 'error'}-model-21`)
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
    await page.locator(`[data-tab="${tab}"]`).click()
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
  for (const tab of ['usage', 'errors']) {
    await page.locator(`[data-tab="${tab}"]`).click()
    const workspace = panel.locator(tab === 'usage' ? '.usage-history-workspace' : '.usage-errors-workspace')
    const body = workspace.locator('.card.workspace-table-card')
    await expect(body).toBeVisible()
    for (const surface of [page.getByTestId('usage-query-card'), body]) {
      const box = (await surface.boundingBox())!
      expect(Math.abs(box.x - 12)).toBeLessThanOrEqual(1)
      expect(Math.abs(390 - box.x - box.width - 12)).toBeLessThanOrEqual(1)
    }
    const footer = workspace.locator('.pagination-card')
    await footer.scrollIntoViewIfNeeded()
    await expect(footer).toBeInViewport()
    await footer.getByRole('button', { name: locale === 'zh' ? '下一页' : 'Next', exact: true }).filter({ visible: true }).click()
    await expect(body.getByText(`admin-${tab === 'usage' ? 'history' : 'error'}-model-21`, { exact: true })).toHaveCount(1)
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
  }
})

test('expanded admin filter controls stack full width on a phone', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await page.setViewportSize({ width: 375, height: 812 })
  await page.goto(`/layout-tests/index.html?view=admin-layout&groups=1&theme=${theme}&locale=${locale}`)
  await expect(page.locator('.app-layout-admin')).toBeVisible({ timeout: 60_000 })
  // The documented mobile query row starts folded; field geometry applies
  // after the user expands it, while page actions stay available throughout.
  const toggle = page.locator('.mobile-filters-toggle')
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  await toggle.click()
  await expect(toggle).toHaveAttribute('aria-expanded', 'true')
  const bar = page.locator('.filter-bar').first()
  await expect(bar).toBeVisible()
  const fields = await bar.locator('.filter-field').evaluateAll(elements => elements.map(element => {
    const box = element.getBoundingClientRect()
    return { width: Math.round(box.width), top: Math.round(box.top), height: Math.round(box.height) }
  }).filter(field => field.width > 0))
  expect(fields.length).toBeGreaterThan(1)
  const barWidth = Math.round((await bar.boundingBox())!.width)
  for (const field of fields) {
    expect(field.width).toBe(barWidth)
    expect(field.height).toBeGreaterThan(0)
  }
  // Stacked, never squeezed side by side.
  for (let index = 1; index < fields.length; index += 1) {
    expect(fields[index]!.top).toBeGreaterThan(fields[index - 1]!.top)
  }
})


// The accepted header has three distinct rows, with no second frame around tabs.
test('risk heading places tabs between title and right aligned actions', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await page.route('**/api/v1/admin/**', route => {
    const path = new URL(route.request().url()).pathname
    const data = path.endsWith('/all') ? [] : path.endsWith('/config') ? {
      enabled: false, mode: 'async', endpoints: [], thresholds: {}, all_groups: true, group_ids: [],
    } : path.endsWith('/status') ? {
      enabled: false, mode: 'async', endpoints: [], worker_count: 4, active_workers: 0, idle_workers: 4,
      queue_size: 100, queue_length: 0, processed_count: 0, error_count: 0,
    } : { items: [], total: 0, page: 1, page_size: 20 }
    return route.fulfill({ json: { code: 0, data } })
  })
  for (const width of [390, 1024, 1440]) {
    await page.setViewportSize({ width, height: 800 })
    await page.goto(`/layout-tests/index.html?view=admin-layout&riskControl=1&theme=${theme}&locale=${locale}`)
    const header = page.getByTestId('risk-page-heading')
    await expect(header).toBeVisible({ timeout: 60_000 })
    const title = header.locator('h1')
    const tabs = header.getByRole('tablist')
    const actions = page.getByTestId('risk-page-actions')
    await expect(actions).toBeVisible()
    const titleBox = (await title.boundingBox())!
    const tabBox = (await tabs.boundingBox())!
    const actionBox = (await actions.boundingBox())!
    expect(tabBox.y).toBeGreaterThan(titleBox.y + titleBox.height)
    expect(actionBox.y).toBeGreaterThanOrEqual(tabBox.y + tabBox.height)
    const buttonBox = (await actions.getByRole('button').last().boundingBox())!
    expect(Math.abs(buttonBox.x + buttonBox.width - actionBox.x - actionBox.width)).toBeLessThanOrEqual(1)
    const tabStyle = await tabs.evaluate(el => {
      const style = getComputedStyle(el)
      return { border: style.borderTopWidth, fill: style.backgroundColor, padding: style.paddingLeft, shadow: style.boxShadow }
    })
    expect(tabStyle).toEqual({ border: '0px', fill: 'rgba(0, 0, 0, 0)', padding: '0px', shadow: 'none' })
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
  }
})

test('prompt actions follow the end time field in the same right aligned filter flow', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await page.route('**/api/v1/admin/prompt-audit/config', route => route.fulfill({ json: { code: 0, data: {
    enabled: false, blocking_enabled: false, store_pass_events: false, scanners: [], endpoints: [],
    all_groups: true, group_ids: [], config_version: 1,
  } } }))
  for (const width of [1024, 1280, 1818]) {
    await page.setViewportSize({ width, height: 800 })
    await page.goto(`/layout-tests/index.html?view=admin-layout&promptAudit=1&theme=${theme}&locale=${locale}`)
    await page.locator('[data-test="tab-events"]').click()
    const form = page.locator('.event-filters')
    const actions = form.locator('.event-actions')
    await expect(actions).toBeVisible()
    expect(await actions.evaluate(el => el.previousElementSibling?.tagName)).toBe('LABEL')
    expect(await actions.evaluate(el => el.parentElement?.lastElementChild === el)).toBe(true)
    const end = (await form.locator('label').last().boundingBox())!
    const group = (await actions.boundingBox())!
    const formBox = (await form.boundingBox())!
    expect(group.y).toBeGreaterThanOrEqual(end.y)
    expect(group.y > end.y + end.height - 1 || group.x >= end.x + end.width).toBe(true)
    expect(Math.abs(group.x + group.width - formBox.x - formBox.width)).toBeLessThanOrEqual(1)
    const last = (await actions.getByRole('button').last().boundingBox())!
    expect(Math.abs(last.x + last.width - group.x - group.width)).toBeLessThanOrEqual(1)
    const controls = await form.locator('.input, .select-trigger, .event-actions > button').evaluateAll(elements =>
      elements.map(el => Math.round(el.getBoundingClientRect().height)).filter(height => height > 0))
    expect(new Set(controls)).toEqual(new Set([32]))
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
  }
})

test('proxy filters and wrapping actions share control heights and stay within the toolbar', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  for (const width of [390, 1024, 1280, 1818]) {
    await page.setViewportSize({ width, height: 800 })
    await page.goto(`/layout-tests/index.html?view=admin-layout&proxies=1&theme=${theme}&locale=${locale}`)
    await expect(page.locator('.app-layout-admin')).toBeVisible({ timeout: 60_000 })
    const bar = page.locator('.filter-bar').first()
    const actions = bar.locator('.filter-actions')
    await expect(actions).toBeVisible()
    if (width < 1024) {
      await page.locator('.mobile-filters-toggle').click()
      await expect(bar.locator('.filter-field').first()).toBeVisible()
    }
    const barBox = (await bar.boundingBox())!
    const buttons = await actions.getByRole('button').evaluateAll(elements => elements.map(el => {
      const box = el.getBoundingClientRect()
      return { x: box.x, right: box.right, y: box.y, height: box.height }
    }))
    expect(buttons).toHaveLength(7)
    for (const button of buttons) {
      expect(button.height).toBe(36)
      expect(button.x).toBeGreaterThanOrEqual(barBox.x - 1)
      expect(button.right).toBeLessThanOrEqual(barBox.x + barBox.width + 1)
    }
    const last = buttons.at(-1)!
    expect(Math.abs(last.right - barBox.x - barBox.width)).toBeLessThanOrEqual(1)
    const heights = await bar.locator('.input, .select-trigger').evaluateAll(elements => elements.map(el => el.getBoundingClientRect().height))
    expect(new Set(heights)).toEqual(new Set([36]))
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
  }
})

test('settings save dock remains reachable without covering the final field in both page sizes', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await page.route('**/api/v1/admin/settings', route => route.fulfill({ json: { code: 0, data: {} } }))
  for (const viewport of [{ width: 375, height: 600 }, { width: 1440, height: 700 }]) {
    await page.setViewportSize(viewport)
    await page.goto(`/layout-tests/index.html?view=admin-layout&settings=1&theme=${theme}&locale=${locale}`)
    const dock = page.locator('[data-test="settings-save-bar"]')
    const save = dock.getByRole('button')
    // The first production Settings import may be cold on a WSL bind mount.
    // Keep normal interaction/geometry timeouts after the page has mounted.
    await expect(dock).toBeVisible({ timeout: 60_000 })
    await expect(save).toBeEnabled()
    const lastField = page.locator('form input:visible').last()
    await lastField.scrollIntoViewIfNeeded()
    await expect(lastField).toBeInViewport()
    await expect(save).toBeInViewport()
    for (const control of [lastField, save]) {
      expect(await control.evaluate(element => {
        const rect = element.getBoundingClientRect()
        return element.contains(document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2))
      })).toBe(true)
    }
    const box = await dock.boundingBox()
    expect(box!.x).toBeGreaterThanOrEqual(0)
    expect(box!.x + box!.width).toBeLessThanOrEqual(viewport.width)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    const fill = await dock.evaluate(element => getComputedStyle(element).backgroundColor)
    expect(fill).not.toBe('rgba(0, 0, 0, 0)')
    if (theme === 'dark') expect(fill).not.toContain('255, 255, 255')
    const update = page.waitForRequest(request => request.url().endsWith('/api/v1/admin/settings') && request.method() !== 'GET')
    await save.click()
    await update
  }
})

// CONTRIBUTING console contract: shared dialogs/menus and numbered pagination;
// MODEL_QUALITY_INSPECTION: real billable requests require explicit confirmation.
test('quality inspection shares console controls without viewport overflow', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const zh = info.project.use.locale?.startsWith('zh') ?? false
  const locale = zh ? 'zh' : 'en'
  const question = { id: 'numeric', title: 'Calculation', kind: 'number', prompt: '17 * 19', instructions: '', expected: '323', choices: [], unit: '', rounds: 1, enabled: true }
  const plans = Array.from({ length: 12 }, (_, i) => ({ id: i + 1, name: `Plan ${i + 1}`, group_id: 1, group_name: 'Quality review group', platform: 'openai', model: 'gpt-5.1-codex', endpoint: 'any', effort: 'medium', enabled: true, schedule_enabled: false, interval_minutes: 60, pause_start: '22:00', pause_end: '09:00', timezone: 'Asia/Shanghai', max_accounts: 1, max_saved_rounds: 20, quarantine_enabled: false, quarantine_cooldown_minutes: 60, quarantine_fail_rounds: 2, revision: 1, questions: [question], estimated_requests: 1 }))
  let billableRequests = 0
  await page.route('**/api/v1/admin/quality-inspections**', async route => {
    const url = new URL(route.request().url())
    if (url.pathname.endsWith('/run')) billableRequests++
    await route.fulfill({ json: { code: 0, data: { plans, questions: [question], settings: { enabled: true, quarantine_enabled: false, quarantine_cooldown_minutes: 60, max_requests_per_round: 30, stability_rounds: 5 } } } })
  })
  await page.route('**/api/v1/admin/groups?**', route => route.fulfill({ json: { code: 0, data: { items: Array.from({ length: 7 }, (_, i) => ({ id: i + 1, name: `Quality group ${i + 1} with a complete translated label`, platform: 'openai' })) } } }))
  for (const width of [390, 1440]) {
    await page.setViewportSize({ width, height: 900 })
    await page.goto(`/layout-tests/index.html?view=admin-layout&qualityInspection=1&theme=${theme}&locale=${locale}`)
    const view = page.getByTestId('quality-inspection')
    await expect(view).toBeVisible({ timeout: 30_000 })
    await expect(view.locator('table tbody tr')).toHaveCount(10)
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
    // Numbered page controls must reach records after the first ten.
    await view.locator('.pagination-container').getByRole('button', { name: zh ? '跳转到第 2 页' : 'Go to page 2', exact: true }).filter({ visible: true }).click()
    await expect(view.locator('table tbody tr')).toHaveCount(2)
    await expect(view.locator('table tbody')).toContainText('Plan 11')
    const row = view.locator('table tbody tr').first()
    const more = row.getByRole('button', { name: zh ? '更多' : 'More', exact: true })
    // A person scrolls the narrow table before tapping its off-screen action.
    // Drain that scroll event before opening a menu that closes on scrolling.
    await more.scrollIntoViewIfNeeded()
    await more.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))))
    await more.click()
    const menu = page.getByRole('menu')
    await expect(menu).toBeVisible()
    const menuBounds = await menu.boundingBox()
    expect(menuBounds!.x).toBeGreaterThanOrEqual(0)
    expect(menuBounds!.x + menuBounds!.width).toBeLessThanOrEqual(width)
    await menu.getByRole('menuitem', { name: zh ? '运行一轮' : 'Run one round' }).click()
    const confirmation = page.getByRole('dialog')
    await expect(confirmation).toContainText(zh ? '真实请求并产生费用' : 'billable requests')
    await confirmation.getByRole('button', { name: zh ? '取消' : 'Cancel', exact: true }).click()
    expect(billableRequests).toBe(0)
    await view.getByRole('button', { name: zh ? '新建计划' : 'New plan', exact: true }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
    const groupTrigger = dialog.locator('.select-trigger').first()
    await groupTrigger.click()
    const dropdown = page.locator('.select-dropdown-portal')
    await expect(dropdown).toBeVisible()
    const bounds = await dropdown.boundingBox()
    expect(bounds!.x).toBeGreaterThanOrEqual(0)
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width)
    await dropdown.getByRole('option').last().click()
    await expect(groupTrigger).toContainText('Quality group 7')
    await dialog.getByRole('button', { name: zh ? '取消' : 'Cancel', exact: true }).click()
  }
})
