import { expect, test } from '@playwright/test'

// Approved UI audit: 16/24px on narrow screens; 94%, capped at 1600px,
// centered within the desktop main viewport regardless of sidebar width.
// Exercise the real AppLayout, sidebar, header, Select and production CSS.
test.beforeEach(async ({ page }) => {
  await page.route('**/api/v1/**', route => route.fulfill({ json: { code: 0, data: { items: [], total: 0, pages: 0, enabled: false } } }))
})

test('user content stays centered with natural page scrolling and portal theme', async ({ page }, info) => {
  test.setTimeout(120_000)
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  const cases = [[320, 16], [390, 16], [767, 16], [768, 24], [1023, 24], [1024, 0], [1440, 0], [1920, 0], [3072, 0]] as const
  for (const [width, inset] of cases) {
    await page.setViewportSize({ width, height: 600 })
    await page.goto(`/layout-tests/index.html?view=user-layout&workspace=0&theme=${theme}&locale=${locale}`)
    await expect(page.getByTestId('page-table')).toBeVisible({ timeout: 60_000 })
    const assertGeometry = async () => {
      const geometry = await page.evaluate(() => {
        const sidebar = document.querySelector('.sidebar')!.getBoundingClientRect()
        const main = document.querySelector('.app-main') as HTMLElement
        const rect = main.getBoundingClientRect()
        return {
          leftOrigin: innerWidth >= 1024 ? sidebar.right : 0,
          // A classic scrollbar reserves space inside this same content viewport.
          rightOrigin: innerWidth - (main.offsetWidth - main.clientWidth),
          mainLeft: rect.left + main.clientLeft,
          mainWidth: main.clientWidth,
          edges: ['page-title', 'page-filter', 'card-group', 'page-table', 'page-pagination'].map(id => {
            const r = document.querySelector(`[data-testid="${id}"]`)!.getBoundingClientRect()
            return { left: r.left, right: r.right }
          }),
          overflow: document.documentElement.scrollWidth > innerWidth,
        }
      })
      expect(geometry.overflow).toBe(false)
      const expectedWidth = Math.min(geometry.mainWidth * .94, 1600)
      const desktopMargin = (geometry.mainWidth - expectedWidth) / 2
      for (const edge of geometry.edges) {
        if (width >= 1024) {
          expect(Math.abs(edge.right - edge.left - expectedWidth)).toBeLessThanOrEqual(1)
          expect(Math.abs(edge.left - geometry.mainLeft - desktopMargin)).toBeLessThanOrEqual(1)
          expect(Math.abs(geometry.mainLeft + geometry.mainWidth - edge.right - desktopMargin)).toBeLessThanOrEqual(1)
        } else {
          expect(Math.abs(edge.left - geometry.leftOrigin - inset)).toBeLessThanOrEqual(1)
          expect(Math.abs(geometry.rightOrigin - edge.right - inset)).toBeLessThanOrEqual(1)
        }
      }
    }
    await assertGeometry()
    if (width >= 1024) {
      const main = page.locator('.app-main')
      expect(await main.evaluate(el => el.scrollHeight > el.clientHeight)).toBe(true)
      const tableViewport = page.locator('.workspace-table-viewport')
      expect(await tableViewport.evaluate(el => el.scrollHeight - el.clientHeight)).toBeLessThanOrEqual(1)
      const before = await page.getByTestId('page-pagination').boundingBox()
      await main.evaluate(el => { el.scrollTop = el.scrollHeight })
      await expect(page.getByTestId('page-pagination')).toBeInViewport()
      expect((await page.getByTestId('page-pagination').boundingBox())!.y).toBeLessThan(before!.y)
      await main.evaluate(el => { el.scrollTop = 0 })
      await page.getByTestId('collapse-sidebar').click()
      await expect.poll(() => page.locator('.sidebar').evaluate(el => Math.round(el.getBoundingClientRect().width))).toBe(72)
      await assertGeometry()
      if (width >= 1920) expect(await page.getByTestId('page-table').evaluate(el => el.getBoundingClientRect().width)).toBe(1600)
    }
    const trigger = page.getByTestId('page-filter').locator('.select-trigger')
    await trigger.click()
    const panel = page.locator('.select-dropdown-portal')
    await expect(panel).toBeVisible()
    expect(await panel.evaluate(el => el.parentElement === document.body)).toBe(true)
    expect(await panel.evaluate(el => getComputedStyle(el).getPropertyValue('--surface-text').trim())).toBe(theme === 'dark' ? '#f1f1f3' : '#202124')
    await page.keyboard.press('Escape')
  }
})

test('user workspace scrolls rows with the wheel while pagination stays in a separate bottom card', async ({ page }) => {
  // Approved audit applies fixed chrome to every explicitly opted-in user
  // workspace, including pages without the record-workspace marker.
  for (const [width, recordWorkspace] of [[3072, 1], [1440, 0]]) {
    await page.setViewportSize({ width, height: 600 })
    await page.goto(`/layout-tests/index.html?view=user-layout&recordWorkspace=${recordWorkspace}`)
    const viewport = page.locator('.workspace-table-viewport')
    const pagination = page.getByTestId('page-pagination')
    await expect(pagination).toBeInViewport()
    const card = await pagination.evaluate(el => ({
      fill: getComputedStyle(el).backgroundColor,
      radius: parseFloat(getComputedStyle(el).borderRadius),
      border: parseFloat(getComputedStyle(el).borderTopWidth),
      bottom: el.getBoundingClientRect().bottom,
      mainBottom: el.closest('.app-main')!.getBoundingClientRect().bottom,
    }))
    expect(card.fill).not.toBe('rgba(0, 0, 0, 0)')
    expect(card.radius).toBe(16)
    expect(card.border).toBe(1)
    expect(Math.abs(card.bottom - card.mainBottom)).toBeLessThanOrEqual(1)
    const before = await pagination.boundingBox()
    await viewport.hover()
    await page.mouse.wheel(0, 500)
    await expect.poll(() => viewport.evaluate(el => el.scrollTop)).toBeGreaterThan(0)
    expect(await pagination.boundingBox()).toEqual(before)
    await page.mouse.wheel(0, -1000)
    await expect.poll(() => viewport.evaluate(el => el.scrollTop)).toBe(0)
  }
})

test('real usage records support wheel scrolling and reachable server pagination', async ({ page }, info) => {
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const requests: Array<{ page: number; size: number }> = []
  await page.addInitScript(() => localStorage.setItem('table-page-size', '20'))
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    let data: unknown = { items: [], total: 0, pages: 0, enabled: false }
    if (url.pathname === '/api/v1/usage') {
      const current = Number(url.searchParams.get('page') || 1)
      const size = Number(url.searchParams.get('page_size') || 20)
      requests.push({ page: current, size })
      data = { total: 48, page: current, page_size: size, pages: 3, items: Array.from({ length: Math.min(size, 48 - (current - 1) * size) }, (_, index) => {
        const id = (current - 1) * size + index + 1
        return { id, user_id: 9999, api_key_id: 1, account_id: null, request_id: `audit-request-${id}`, model: `audit-model-${id}`,
          api_key: { id: 1, name: 'UI audit application integration key' }, group: { id: 1, name: 'OpenAI standard production audit group', platform: 'openai' }, group_id: 1,
          inbound_endpoint: '/v1/responses', upstream_endpoint: '/v1/responses', ip_address: `192.0.2.${id}`,
          reasoning_effort: 'high', request_fast_enabled: id % 2 === 1,
          input_tokens: 1200, output_tokens: 300, cache_creation_tokens: 0, cache_read_tokens: 0,
          input_cost: .01, output_cost: .02, total_cost: .03, actual_cost: .03, rate_multiplier: 1,
          billing_type: 0, billing_mode: 'token', stream: true, request_type: 'stream', duration_ms: 1000,
          first_token_ms: 100, created_at: '2026-10-29T10:00:00Z', user_agent: 'Audit/1.0' }
      }) }
    } else if (url.pathname === '/api/v1/keys') data = { items: [{ id: 1, name: 'UI audit application integration key' }], total: 1, pages: 1 }
    else if (url.pathname.includes('/groups')) data = [{ id: 1, name: 'OpenAI standard production audit group', platform: 'openai' }]
    else if (url.pathname.includes('/stats')) data = { total_requests: 48, total_tokens: 72000, total_cost: 1.44, total_actual_cost: 1.44 }
    else if (url.pathname === '/api/v1/auth/me') data = { id: 9999, role: 'user', email: 'review@example.test', balance: 12.34 }
    await route.fulfill({ json: { code: 0, data } })
  })
  for (const width of [3072, 1440, 390]) {
    await page.setViewportSize({ width, height: 600 })
    await page.goto(`/layout-tests/index.html?view=user-layout&usage=1&theme=${theme}&locale=${locale}`)
    await expect(page.locator('.app-layout-record-workspace')).toBeVisible({ timeout: 60_000 })
    const panel = page.getByTestId('usage-records-panel')
    await expect(panel.getByText('audit-model-1', { exact: true })).toBeVisible()
    const metadata = panel.getByTestId('usage-model-metadata').first()
    await expect(metadata).toContainText('High')
    await expect(metadata).toContainText(locale === 'zh' ? '已开启' : 'Enabled')
    await expect(panel.locator('th[data-field="reasoning_effort"]')).toHaveCount(0)
    await expect(page.locator('.data-table-fit-probe')).toHaveCount(0)
    const viewport = panel.locator('.workspace-table-viewport').filter({ visible: true })
    if (width >= 1024) {
      await expect(viewport).toBeVisible()
      if (width === 3072) {
        await expect(viewport.locator('table.record-table')).toBeVisible()
        const header = panel.locator('.static-table-header-table')
        await expect(header).toBeVisible()
        await expect(header).toHaveCSS('table-layout', 'fixed')
        await expect.poll(() => header.locator('thead th').evaluateAll(cells => cells.length > 0 && cells.every(cell => {
          const style = getComputedStyle(cell)
          return style.textAlign === 'left' && style.whiteSpace === 'nowrap' && style.fontSize === '12px'
            && style.paddingTop === '10px' && style.paddingBottom === '10px'
            && style.paddingLeft === '6px' && style.paddingRight === '6px'
        }))).toBe(true)
        await expect.poll(() => panel.evaluate(el => {
          const headings = [...el.querySelectorAll('.static-table-header-table thead th')]
          const cells = [...el.querySelectorAll('table.record-table tbody tr:not([aria-hidden="true"])')][0]?.querySelectorAll('td')
          return !!cells && headings.length > 0 && headings.length === cells.length && headings.every((heading, index) => {
            const head = heading.getBoundingClientRect()
            const body = cells[index].getBoundingClientRect()
            return Math.abs(head.left - body.left) <= 2 && Math.abs(head.width - body.width) <= 2
          })
        })).toBe(true)
      }
      else await expect(viewport).toHaveClass(/user-record-cards/)
      const pagination = panel.locator('.pagination-card')
      await expect(pagination).toBeInViewport()
      const before = await pagination.boundingBox()
      await panel.getByText('audit-model-1', { exact: true }).hover()
      await page.mouse.wheel(0, 500)
      await expect.poll(() => viewport.evaluate(el => el.scrollTop)).toBeGreaterThan(0)
      expect(await pagination.boundingBox()).toEqual(before)
      await page.mouse.wheel(0, -2000)
      await expect.poll(() => viewport.evaluate(el => el.scrollTop)).toBe(0)
    } else {
      await panel.getByText('audit-model-1', { exact: true }).hover()
      await page.mouse.wheel(0, 20000)
      await expect(panel.locator('.pagination-card')).toBeInViewport()
    }
    const next = panel.locator('.pagination-card button:visible').filter({ hasText: locale === 'zh' ? '下一页' : 'Next' })
    const nextButton = width < 1024 ? next : panel.locator('.pagination-card').getByRole('button', { name: locale === 'zh' ? '下一页' : 'Next', exact: true }).filter({ visible: true })
    const response = page.waitForResponse(response => {
      const url = new URL(response.url())
      return url.pathname === '/api/v1/usage' && url.searchParams.get('page') === '2' && url.searchParams.get('page_size') === '20'
    })
    await nextButton.click()
    await response
    await expect(panel.getByText('audit-model-21', { exact: true })).toHaveCount(1)
    await expect(panel.getByText('audit-model-1', { exact: true })).toHaveCount(0)
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
  }
  expect(requests.filter(request => request.page === 2 && request.size === 20)).toHaveLength(3)
})

test('ordinary pages, empty groups and admin defaults retain their layout ownership', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 768 })
  for (const count of [0, 1, 3]) {
    await page.goto(`/layout-tests/index.html?view=user-layout&workspace=0&count=${count}`)
    const title = await page.getByTestId('page-title').boundingBox()
    const table = await page.getByTestId('page-table').boundingBox()
    expect(table?.x).toBe(title?.x)
    expect(table?.width).toBe(title?.width)
  }
  await page.goto('/layout-tests/index.html?view=user-layout&admin=1')
  await expect(page.locator('html')).not.toHaveAttribute('data-product-ui')
  const sidebar = await page.locator('.sidebar').boundingBox()
  const title = await page.getByTestId('page-title').boundingBox()
  expect(title!.x - sidebar!.x - sidebar!.width).toBe(24)
  const pagination = await page.getByTestId('page-pagination').boundingBox()
  const viewport = page.locator('.workspace-table-viewport')
  expect(await viewport.evaluate(el => el.scrollHeight > el.clientHeight)).toBe(true)
  await viewport.evaluate(el => { el.scrollTop = el.scrollHeight })
  expect(await page.getByTestId('page-pagination').boundingBox()).toEqual(pagination)
})

test('user tables keep full single-line names and headings with wrapped actions and complete row cards', async ({ page }, info) => {
  // User audit requirements: no horizontal scrolling at the reviewed desktop
  // size; long model/group names remain single-line; action groups may wrap;
  // hover and empty-state cards must reach the first column.
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await page.setViewportSize({ width: 1920, height: 900 })
  await page.goto(`/layout-tests/index.html?view=user-layout&records=1&theme=${theme}&locale=${locale}`)
  const table = page.getByTestId('page-table').locator('table.record-table')
  await expect(table.locator('tbody tr.table-data-row')).toHaveCount(3)
  const tableGeometry = await table.evaluate(el => {
    const parent = el.parentElement!
    const cells = Array.from(el.querySelectorAll('th,td'))
    return {
      layout: getComputedStyle(el).tableLayout,
      overflow: parent.scrollWidth - parent.clientWidth,
      cells: cells.map(cell => ({ alignment: getComputedStyle(cell).textAlign })),
      headers: Array.from(el.querySelectorAll('th')).map(cell => getComputedStyle(cell).whiteSpace),
      names: Array.from(el.querySelectorAll('td[data-field="model"],td[data-field="group"]')).map(cell => ({
        wrapping: getComputedStyle(cell).whiteSpace,
        scrollWidth: cell.scrollWidth,
        width: cell.clientWidth,
        textOverflow: getComputedStyle(cell.firstElementChild!).textOverflow,
      })),
    }
  })
  expect(tableGeometry.layout).toBe('auto')
  expect(tableGeometry.overflow).toBeLessThanOrEqual(1)
  expect(tableGeometry.cells.every(cell => cell.alignment === 'left')).toBe(true)
  expect(tableGeometry.headers.every(value => value === 'nowrap')).toBe(true)
  for (const name of tableGeometry.names) {
    expect(name.wrapping).toBe('nowrap')
    expect(name.scrollWidth - name.width).toBeLessThanOrEqual(1)
    expect(name.textOverflow).not.toBe('ellipsis')
  }

  const row = table.locator('tbody tr.table-data-row').first()
  const actions = row.locator('.table-actions')
  const actionGeometry = await actions.evaluate(el => ({
    wrap: getComputedStyle(el).flexWrap,
    lines: new Set(Array.from(el.querySelectorAll('button')).map(button => Math.round(button.getBoundingClientRect().top))).size,
    buttons: Array.from(el.querySelectorAll('button')).map(button => ({
      height: button.getBoundingClientRect().height,
      border: getComputedStyle(button).borderTopStyle,
    })),
  }))
  expect(actionGeometry.wrap).toBe('wrap')
  expect(actionGeometry.lines).toBeGreaterThan(1)
  expect(actionGeometry.buttons.every(button => button.height >= 28 && button.border === 'solid')).toBe(true)

  const first = row.locator('td').first()
  const beforeHover = await first.evaluate(el => getComputedStyle(el).backgroundImage)
  await row.locator('[data-field="name"]').hover()
  const hover = await row.evaluate(el => Array.from(el.querySelectorAll('td')).map(cell => ({
    background: getComputedStyle(cell).backgroundImage,
    shadow: getComputedStyle(cell).boxShadow,
    radius: getComputedStyle(cell).borderTopLeftRadius,
    pseudo: getComputedStyle(cell, '::before').content,
    position: getComputedStyle(cell).position,
  })))
  expect(hover[0]!.background).not.toBe(beforeHover)
  expect(hover.every(cell => cell.background === hover[0]!.background)).toBe(true)
  expect(hover[0]!.shadow).not.toBe('none')
  expect(parseFloat(hover[0]!.radius)).toBeGreaterThan(0)
  expect(hover[0]!.pseudo).toBe('none')
  expect(hover[0]!.position).toBe('static')
  await page.getByTestId('page-filter').hover()
  await expect.poll(() => first.evaluate(el => getComputedStyle(el).backgroundImage)).toBe(beforeHover)

  await page.getByTestId('toggle-records').click()
  await expect(table.locator('tbody td[colspan]')).toHaveCount(1)
  const empty = await table.evaluate(el => {
    const cell = el.querySelector('tbody td[colspan]') as HTMLTableCellElement
    const rect = cell.getBoundingClientRect()
    const tableRect = el.getBoundingClientRect()
    return {
      colspan: cell.colSpan,
      columns: el.querySelectorAll('thead th').length,
      left: rect.left - tableRect.left,
      right: tableRect.right - rect.right,
      leftRadius: parseFloat(getComputedStyle(cell).borderTopLeftRadius),
      rightRadius: parseFloat(getComputedStyle(cell).borderTopRightRadius),
      shadow: getComputedStyle(cell).boxShadow,
      overflow: el.parentElement!.scrollWidth - el.parentElement!.clientWidth,
    }
  })
  expect(empty.colspan).toBe(empty.columns)
  expect(Math.abs(empty.left)).toBeLessThanOrEqual(1)
  expect(Math.abs(empty.right)).toBeLessThanOrEqual(1)
  expect(empty.leftRadius).toBeGreaterThan(0)
  expect(empty.rightRadius).toBeGreaterThan(0)
  expect(empty.shadow).not.toBe('none')
  expect(empty.overflow).toBeLessThanOrEqual(1)
})

test('dense user records retain every field when the viewport cannot fit a table', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await page.setViewportSize({ width: 3072, height: 900 })
  await page.goto(`/layout-tests/index.html?view=user-layout&records=1&dense=1&theme=${theme}&locale=${locale}`)
  const panel = page.getByTestId('page-table')
  await expect(panel.locator('table.record-table')).toBeVisible()
  for (const width of [1440, 1024, 390, 3072]) {
    await page.setViewportSize({ width, height: 900 })
    await expect(panel.getByText('gpt-5.4-ui-audit-long-model-name-example', { exact: true })).toBeVisible()
    await expect(panel.getByText('[UI测试] OpenAI 标准审计分组', { exact: true })).toHaveCount(3)
    await expect(panel.getByText('/v1/images/generations', { exact: true })).toHaveCount(3)
    await expect(panel.getByText('UI-Audit-Long-Client/1.0', { exact: true })).toHaveCount(3)
    for (const requestId of ['000001', '000002', '000003']) {
      await expect(panel.getByText(`ui-audit-request-20261029-${requestId}`, { exact: true })).toBeVisible()
    }
    await expect(panel.getByText('$0.123456', { exact: true })).toHaveCount(3)
    await expect(panel.getByRole('button', { name: 'Import to CCS', exact: true })).toHaveCount(3)
    await expect.poll(() => panel.evaluate(el => el.scrollWidth - el.clientWidth)).toBeLessThanOrEqual(1)
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
    const model = panel.getByText('gpt-5.4-ui-audit-long-model-name-example', { exact: true })
    expect(await model.evaluate(el => el.scrollWidth - el.clientWidth)).toBeLessThanOrEqual(1)
    if (width === 1440 || width === 390) {
      const fields = panel.locator('.user-record-fields').first()
      await expect(fields).toBeVisible()
      const layout = await fields.evaluate(el => {
        const bounds = el.getBoundingClientRect()
        const records = [...el.children].filter(child => child.hasAttribute('data-field'))
        return {
          columns: getComputedStyle(el).gridTemplateColumns.split(/\s+/).length,
          fields: records.map(child => ({ left: child.getBoundingClientRect().left, top: child.getBoundingClientRect().top })),
          spans: [...el.querySelectorAll('.user-record-selection, .user-record-actions')].map(child => ({
            left: child.getBoundingClientRect().left - bounds.left,
            width: child.getBoundingClientRect().width,
          })),
          width: bounds.width,
        }
      })
      if (width === 1440) {
        expect(layout.columns).toBeGreaterThanOrEqual(2)
        expect(layout.fields.some((field, index) => layout.fields.slice(index + 1).some(other => Math.abs(field.top - other.top) < 1 && Math.abs(field.left - other.left) > 1))).toBe(true)
      } else {
        expect(layout.columns).toBe(1)
      }
      expect(layout.spans).toHaveLength(2)
      for (const span of layout.spans) {
        expect(Math.abs(span.left)).toBeLessThanOrEqual(1)
        expect(Math.abs(span.width - layout.width)).toBeLessThanOrEqual(1)
      }
    }
  }
  await expect(panel.locator('table.record-table')).toBeVisible()
})

test('dashboard shows all sections, four permanent performance cards and complete model records', async ({ page }, info) => {
  // Approved audit: chart/quotas/recent sections share one page; performance
  // has no disclosure and sits 16px above the platform card. Long model names
  // remain complete at wide, ordinary desktop and mobile widths.
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  const longModel = 'gpt-5.4-ui-audit-long-model-name-example'
  const stats = {
    timezone: 'UTC', total_api_keys: 4, active_api_keys: 1,
    total_requests: 168, total_input_tokens: 800000, total_output_tokens: 200000,
    total_cache_creation_tokens: 100000, total_cache_read_tokens: 200000,
    total_tokens: 1300000, total_cost: 4.5, total_actual_cost: 3.5,
    today_requests: 24, today_input_tokens: 10000, today_output_tokens: 2000,
    today_cache_creation_tokens: 1000, today_cache_read_tokens: 2000,
    today_tokens: 15000, today_cost: 0.5, today_actual_cost: 0.4,
    average_duration_ms: 5000, rpm: 2, tpm: 2000,
    by_platform: [{ platform: 'openai', total_requests: 168, total_tokens: 1300000,
      total_actual_cost: 3.5, today_requests: 24, today_tokens: 15000, today_actual_cost: 0.4 }],
  }
  await page.route('**/api/v1/**', async route => {
    const pathname = new URL(route.request().url()).pathname
    let data: unknown = { items: [], total: 0, pages: 0, enabled: false }
    if (pathname === '/api/v1/auth/me') data = { id: 9999, role: 'user', email: 'review@example.test', username: 'Reviewer', balance: 12.34 }
    else if (pathname === '/api/v1/usage/dashboard/stats') data = stats
    else if (pathname === '/api/v1/usage/dashboard/models') data = { models: [
      { model: longModel, requests: 21, total_tokens: 193700, actual_cost: 0.5632, cost: 0.5632 },
      { model: 'claude-opus-4-6', requests: 21, total_tokens: 205300, actual_cost: 0.4683, cost: 0.5853 },
    ] }
    else if (pathname === '/api/v1/usage/dashboard/trend') data = { trend: [
      { date: '2026-10-28', requests: 12, input_tokens: 10000, output_tokens: 2000, total_tokens: 12000, cost: 0.5, actual_cost: 0.4 },
      { date: '2026-10-29', requests: 24, input_tokens: 20000, output_tokens: 4000, total_tokens: 24000, cost: 1, actual_cost: 0.8 },
    ] }
    else if (pathname === '/api/v1/user/platform-quotas') data = { platform_quotas: [] }
    await route.fulfill({ json: { code: 0, data } })
  })
  await page.setViewportSize({ width: 3072, height: 900 })
  await page.goto(`/layout-tests/index.html?view=user-layout&dashboard=1&theme=${theme}&locale=${locale}`)
  for (const width of [3072, 1440, 390]) {
    await page.setViewportSize({ width, height: 900 })
    for (const section of ['dashboard-charts-panel', 'dashboard-quotas-panel', 'dashboard-recent-panel']) {
      await expect(page.getByTestId(section)).toBeVisible()
    }
    if (width >= 1024) {
      const toolbar = page.locator('.dashboard-toolbar')
      const viewport = page.locator('.dashboard-activity .user-card-scroll-viewport')
      const edges = await page.locator('.dashboard-activity').evaluate(frame => {
        const toolbar = document.querySelector('.dashboard-toolbar')!.getBoundingClientRect()
        const content = frame.querySelector('.user-card-scroll-content')!.getBoundingClientRect()
        return {
          background: getComputedStyle(frame).backgroundColor,
          left: content.left - toolbar.left,
          right: content.right - toolbar.right,
        }
      })
      expect(edges.background).toBe('rgba(0, 0, 0, 0)')
      expect(Math.abs(edges.left)).toBeLessThanOrEqual(1)
      expect(Math.abs(edges.right)).toBeLessThanOrEqual(1)
      const fixed = await toolbar.boundingBox()
      await viewport.hover()
      await page.mouse.wheel(0, 500)
      await expect.poll(() => viewport.evaluate(el => el.scrollTop)).toBeGreaterThan(0)
      expect(await toolbar.boundingBox()).toEqual(fixed)
      await viewport.evaluate(el => { el.scrollTop = 0 })
    }
    await expect(page.locator('.user-dashboard > [role="tablist"]')).toHaveCount(0)
    const performance = page.getByTestId('dashboard-performance')
    await expect(performance.getByRole('heading', { name: locale === 'zh' ? '性能指标' : 'Performance', exact: true })).toBeVisible()
    await expect(performance.locator('summary,details')).toHaveCount(0)
    await expect(performance.getByTestId('performance-card')).toHaveCount(4)
    const gap = await performance.evaluate(el => el.nextElementSibling!.getBoundingClientRect().top - el.getBoundingClientRect().bottom)
    expect(Math.abs(gap - 16)).toBeLessThanOrEqual(1)
    const model = page.locator('.model-distribution-table td').filter({ hasText: longModel })
    await expect(model).toHaveText(longModel)
    const layout = await model.evaluate(el => ({
      wrapping: getComputedStyle(el).whiteSpace,
      alignment: getComputedStyle(el).textAlign,
      overflow: el.scrollWidth - el.clientWidth,
      recordsOverflow: el.closest('.record-table-scroll')!.scrollWidth - el.closest('.record-table-scroll')!.clientWidth,
      width: innerWidth,
      recordsWidth: el.closest('.record-table-scroll')!.clientWidth,
    }))
    expect(layout.wrapping).toBe('nowrap')
    expect(layout.alignment).toBe('left')
    expect(layout.overflow, JSON.stringify(layout)).toBeLessThanOrEqual(1)
    expect(layout.recordsOverflow, JSON.stringify(layout)).toBeLessThanOrEqual(1)
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), `${width}px dashboard document width`).toBe(false)
    if (width === 390) {
      // A desktop canvas keeps this inline width until Chart.js handles its
      // ResizeObserver on the next frame. It must fit during that interval too.
      const staleCanvas = await page.locator('.user-chart-grid .chart-workspace canvas').evaluate(canvas => {
        const element = canvas as HTMLCanvasElement
        const originalWidth = element.style.width
        try {
          element.style.width = '1005px'
          return {
            width: element.getBoundingClientRect().width,
            available: element.parentElement!.clientWidth,
            documentOverflow: document.documentElement.scrollWidth > innerWidth,
          }
        } finally {
          element.style.width = originalWidth
        }
      })
      expect(staleCanvas.width).toBeLessThanOrEqual(staleCanvas.available)
      expect(staleCanvas.documentOverflow).toBe(false)
    }
  }
})
