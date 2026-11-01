import { expect, test, type Locator, type Page } from '@playwright/test'

// User acceptance: every column, including frozen columns, paints the same
// outlined record card in normal, hover and selected states. Pixels establish
// visible output rather than treating computed declarations as proof of paint.
interface PixelSamples { field: string | null; outline: number[]; fill: number[]; gap: number[]; corner: number[]; left: number; right: number }

async function rowPixels(page: Page, row: Locator): Promise<PixelSamples[]> {
  // The production static-header owner splits newly mounted live tables on a
  // subsequent animation frame. Sample only after font and geometry settle.
  await row.evaluate(async element => {
    await document.fonts.ready
    let previous = '', stable = 0
    for (let frame = 0; frame < 30 && stable < 4; frame++) {
      await new Promise<void>(resolve => requestAnimationFrame(() => resolve()))
      const rect = element.getBoundingClientRect()
      const signature = [rect.x, rect.y, rect.width, rect.height].join(',')
      stable = signature === previous ? stable + 1 : 0
      previous = signature
    }
  })
  const boxes = await row.locator('td').evaluateAll(elements => elements.map(element => {
    const r = element.getBoundingClientRect()
    const viewport = element.closest<HTMLElement>('.record-table-scroll')!.getBoundingClientRect()
    return { x: r.x, y: r.y, width: r.width, height: r.height, field: element.getAttribute('data-field'),
      visible: r.left >= viewport.left && r.right <= viewport.right,
    }
  }).filter(b => b.visible))
  const screenshot = (await page.screenshot()).toString('base64')
  return page.evaluate(async ({ boxes, screenshot }) => {
    const image = new Image()
    image.src = `data:image/png;base64,${screenshot}`
    await image.decode()
    const canvas = document.createElement('canvas')
    canvas.width = image.width; canvas.height = image.height
    const ctx = canvas.getContext('2d')!
    ctx.drawImage(image, 0, 0)
    const pixel = (x: number, y: number) => [...ctx.getImageData(Math.round(x), Math.round(y), 1, 1).data]
    return boxes.map(b => ({ field: b.field, outline: pixel(b.x + b.width / 2, b.y + 4),
      fill: pixel(b.x + b.width / 2, b.y + 8), gap: pixel(b.x + b.width / 2, b.y + 1),
      corner: pixel(b.x + 1, b.y + 5), left: b.x, right: b.x + b.width }))
  }, { boxes, screenshot })
}

function difference(a: number[], b: number[]) { return Math.max(...a.slice(0, 3).map((v, i) => Math.abs(v - b[i]!))) }

async function assertSharedPaint(page: Page, row: Locator, frozenFields: Array<string | null>, scrolled = false) {
  const samples = await rowPixels(page, row)
  for (const field of frozenFields) expect(samples.find(sample => sample.field === field), `${field} frozen column is visible`).toBeTruthy()
  const ordinary = samples.find(sample => !frozenFields.includes(sample.field))!
  expect(ordinary, 'an ordinary visible cell provides an independent continuity reference').toBeTruthy()
  for (const sample of samples.filter(sample => frozenFields.includes(sample.field))) {
    expect(difference(sample.outline, ordinary.outline), `${sample.field} outlined card continues through frozen cell`).toBeLessThanOrEqual(3)
    expect(difference(sample.fill, ordinary.fill), `${sample.field} row tint continues through frozen cell`).toBeLessThanOrEqual(3)
    expect(difference(sample.gap, ordinary.gap), `${sample.field} transparent row gap`).toBeLessThanOrEqual(3)
    expect(difference(sample.outline, sample.fill), `${sample.field} visible outline`).toBeGreaterThan(5)
    if (!scrolled && sample === samples[0]) {
      expect(difference(sample.corner, sample.gap), 'outer rounded cutout remains transparent').toBeLessThanOrEqual(3)
    }
  }
  return samples
}

for (const alpha of [0, .5, 1]) test(`frozen cells retain card paint, rounded transparent gaps and selection at alpha ${alpha} while scrolling`, async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await page.route('**/api/v1/**', route => route.fulfill({ json: { code: 0, data: { items: [], total: 0 } } }))
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto(`/layout-tests/index.html?view=admin-layout&theme=${theme}&locale=${locale}&alpha=${alpha}`)
  const row = page.locator('table.record-table tbody tr[data-row-id]').first()
  await expect(row).toBeVisible()
  await page.mouse.move(0, 0)
  await assertSharedPaint(page, row, [null, 'name', 'actions'])
  await row.locator('td').first().hover()
  await assertSharedPaint(page, row, [null, 'name', 'actions'])
  await row.locator('[data-test=select-row]').check()
  await page.mouse.move(0, 0)
  const selected = await assertSharedPaint(page, row, [null, 'name', 'actions'])
  const frozen = selected.find(sample => sample.field === 'name')!
  const viewport = page.locator('.record-table-scroll')
  await viewport.evaluate(element => { element.scrollLeft = 140 })
  await assertSharedPaint(page, row, [null, 'name', 'actions'], true)
  expect(Math.abs((await row.locator('[data-field=name]').boundingBox())!.x - frozen.left)).toBeLessThanOrEqual(1)
  await row.locator('td').first().hover()
  await assertSharedPaint(page, row, [null, 'name', 'actions'], true)
})

async function mockPages(page: Page) {
  const rows = Array.from({ length: 4 }, (_, i) => ({
    id: i + 1, user_id: 9998, api_key_id: 1, account_id: 1, model: 'gpt-5.4',
    user: { id: 9998, email: 'hover-record@example.test' }, account: { id: 1, name: 'Hover account' },
    group: { id: 1, name: 'Hover group', platform: 'openai' },
    input_tokens: 1200, output_tokens: 300, cache_creation_tokens: 0, cache_read_tokens: 0,
    input_cost: .01, output_cost: .02, total_cost: .03, actual_cost: .03, rate_multiplier: 1,
    billing_type: 0, billing_mode: 'token', stream: true, request_type: 'stream', duration_ms: 1000,
    first_token_ms: 100, created_at: '2026-10-29T10:00:00Z',
  }))
  await page.route('**/api/v1/**', async route => {
    const p = new URL(route.request().url()).pathname
    let data: unknown = { items: [], total: 0, enabled: false }
    if (p === '/api/v1/admin/usage') data = { items: rows, total: rows.length, pages: 1, page: 1, page_size: 20 }
    else if (p === '/api/v1/admin/usage/stats') data = { total_requests: 4, total_input_tokens: 4800, total_output_tokens: 1200, total_tokens: 6000, total_actual_cost: .12 }
    else if (p === '/api/v1/admin/usage/live') data = { server_now: '2026-10-29T10:00:05Z', window_seconds: 300, limit: 200, truncated: false, poll_after_seconds: 30,
      items: rows.map(r => ({ ...r, id: `live-${r.id}`, version: 1, key_name: 'Hover key', group_name: 'Hover group', status: 'completed', started_at: '2026-10-29T10:00:00Z', updated_at: '2026-10-29T10:00:01Z', finished_at: '2026-10-29T10:00:01Z' })) }
    else if (p === '/api/v1/admin/dashboard/models') data = { models: [] }
    else if (p === '/api/v1/admin/dashboard/snapshot-v2') data = { trend: [], models: [], groups: [] }
    else if (p === '/api/v1/admin/ip-access-control/failure-states') data = { total: 4, page: 1, page_size: 20, items: rows.map(r => ({ normalized_ip: `203.0.113.${r.id}`, failure_count: 2, failure_threshold: 8, window_started_at: '2026-10-29T10:00:00Z', last_failed_at: '2026-10-29T10:00:02Z', window_expires_at: '2026-10-29T10:15:00Z', active_block_rule: false, runtime_enforcement_enabled: false, suppressed_by_allow_rule: false, emergency_allowlisted: false, effectively_blocked: false, as_of: '2026-10-29T10:00:05Z' })) }
    else if (p === '/api/v1/admin/ip-access-control/settings') data = { enforcement_enabled: false, login_failure_auto_block_enabled: false, login_failure_threshold: 8, login_failure_window_minutes: 15, login_failure_block_minutes: 1440 }
    await route.fulfill({ json: { code: 0, data } })
  })
}

test('reported IP and usage rows share frozen-column card paint', async ({ page }, info) => {
  await mockPages(page)
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto(`/layout-tests/index.html?view=admin-layout&ipAccessControl=1&theme=${theme}&locale=${locale}`)
  await page.locator('[aria-controls=ip-panel-failures]').click()
  for (const target of ['ip', 'usage', 'live']) {
    if (target === 'usage') {
      await page.goto(`/layout-tests/index.html?view=admin-layout&usage=1&theme=${theme}&locale=${locale}`)
      await page.locator('[data-tab=usage]').click()
    }
    if (target === 'live') await page.locator('[data-tab=live]').click()
    const row = page.locator('table.record-table tbody tr[data-row-id]').filter({ visible: true }).first()
    await expect(row).toBeVisible()
    await page.mouse.move(0, 0)
    await assertSharedPaint(page, row, target === 'ip' ? ['normalized_ip', 'actions'] : target === 'usage' ? ['user', 'account'] : ['started_at'])
    await row.locator('td').first().hover()
    await assertSharedPaint(page, row, target === 'ip' ? ['normalized_ip', 'actions'] : target === 'usage' ? ['user', 'account'] : ['started_at'])
  }
})
