import { expect, test, type Page } from '@playwright/test'

// Acceptance source: phone records must remain compact and readable. Short
// label/value pairs share a row, long values wrap, and card height follows
// content rather than the viewport or the desktop workspace.
const user = {
  id: 90001, email: 'mobile-review@example.test', username: 'Mobile reviewer',
  role: 'user', status: 'active', balance: 12.5, concurrency: 2,
  created_at: '2026-10-31T10:00:00Z', last_active_at: null, last_used_at: null,
  allowed_groups: [], subscriptions: [],
}

async function intercept(page: Page, count: number) {
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname
    let data: unknown = { items: [], total: 0, pages: 0, enabled: false }
    if (path === '/api/v1/admin/users') data = {
      items: Array.from({ length: count }, (_, index) => ({ ...user, id: user.id + index })),
      total: count, pages: count ? 1 : 0, page: 1, page_size: 20,
    }
    else if (path === '/api/v1/admin/user-attributes' || path === '/api/v1/admin/groups/all') data = []
    else if (path === '/api/v1/admin/groups') data = {
      items: [{ id: 90001, name: 'Compact mobile routing group', platform: 'openai',
        subscription_type: 'standard', rate_multiplier: 1, is_exclusive: false,
        status: 'active', account_count: 4, created_at: '2026-10-31T10:00:00Z' }],
      total: 1, pages: 1, page: 1, page_size: 20,
    }
    else if (path === '/api/v1/admin/groups/usage-summary' || path === '/api/v1/admin/groups/capacity-summary') data = []
    else if (path === '/api/v1/subscriptions/active') data = []
    else if (path === '/api/v1/auth/me') data = { ...user, id: 9998, role: 'admin' }
    return route.fulfill({ json: { code: 0, data } })
  })
}

test('mobile user-management records keep short fields on the same line and natural card heights', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await intercept(page, 3)
  for (const width of [320, 390]) {
    await page.setViewportSize({ width, height: 844 })
    await page.goto(`/layout-tests/index.html?view=admin-layout&users=1&theme=${theme}&locale=${locale}`)
    const fields = page.locator('.record-card-fields')
    await expect(fields).toHaveCount(3)
    const first = fields.first()
    await expect(first.locator('[data-field="email"]')).toContainText(user.email)
    await expect(first.locator('[data-field]')).toHaveCount(9)
    const dimensions = await first.evaluate(element => {
      const card = element.parentElement!.getBoundingClientRect()
      const shortFields = ['id', 'username', 'role', 'status', 'last_active_at', 'last_used_at'].map(key => {
        const row = element.querySelector(`[data-field="${key}"]`)!
        const label = row.firstElementChild!.getBoundingClientRect()
        const value = row.lastElementChild!.getBoundingClientRect()
        return { key, labelTop: label.top, valueTop: value.top, height: row.getBoundingClientRect().height }
      })
      return { height: card.height, shortFields, width: element.clientWidth, contentWidth: element.scrollWidth }
    })
    // A nine-field account summary with selection/actions fits roughly half a
    // phone page. It must not expand into nine stacked mini-sections.
    expect(dimensions.height).toBeLessThanOrEqual(460)
    expect(dimensions.contentWidth).toBeLessThanOrEqual(dimensions.width + 1)
    for (const row of dimensions.shortFields) {
      expect(Math.abs(row.labelTop - row.valueTop), row.key).toBeLessThanOrEqual(2)
      expect(row.height, row.key).toBeLessThanOrEqual(36)
    }
    const cards = await fields.evaluateAll(elements => elements.map(element => element.parentElement!.getBoundingClientRect()).map(rect => ({ top: rect.top, bottom: rect.bottom, height: rect.height })))
    expect(cards[1]!.top - cards[0]!.bottom).toBeGreaterThanOrEqual(8)
    expect(cards[1]!.top - cards[0]!.bottom).toBeLessThanOrEqual(16)
    const height = dimensions.height
    await page.setViewportSize({ width, height: 1100 })
    expect((await first.locator('..').boundingBox())!.height).toBeCloseTo(height, 0)
    const selection = first.locator('[data-test="select-row"]')
    await selection.check()
    await expect(selection).toBeChecked()
    const limits = page.locator('[data-test="bulk-edit-limits"]')
    const deletion = page.locator('[data-test="bulk-delete-users"]')
    const creation = page.getByRole('button', { name: locale === 'zh' ? '创建用户' : 'Create User', exact: true }).first()
    for (const button of [limits, deletion, creation]) {
      await expect(button).toBeVisible()
      const metrics = await button.evaluate(element => ({
        height: element.getBoundingClientRect().height,
        x: element.getBoundingClientRect().x,
        right: element.getBoundingClientRect().right,
        contentWidth: element.scrollWidth,
        width: element.clientWidth,
      }))
      expect(metrics.height).toBeLessThanOrEqual(48)
      expect(metrics.x).toBeGreaterThanOrEqual(12)
      expect(metrics.right).toBeLessThanOrEqual(width - 12 + 1)
      expect(metrics.contentWidth).toBeLessThanOrEqual(metrics.width + 1)
    }
    await first.getByRole('button', { name: locale === 'zh' ? '编辑' : 'Edit', exact: true }).click()
    await expect(page.locator('.modal-overlay')).toBeVisible()
    await page.keyboard.press('Escape')
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
  }
})

test('mobile shared record cards stay compact with few, many and empty records', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await intercept(page, 0)
  for (const count of [0, 1, 12]) {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto(`/layout-tests/index.html?view=admin-layout&count=${count}&theme=${theme}&locale=${locale}`)
    const fields = page.locator('.record-card-fields')
    await expect(fields).toHaveCount(count)
    if (!count) {
      await expect(page.locator('.record-cards')).toContainText(locale === 'zh' ? '暂无数据' : 'No data')
      continue
    }
    const first = fields.first()
    await expect(first.locator('[data-field]')).toHaveCount(5)
    // This fixture deliberately has long, unbroken model/endpoint values.
    expect((await first.locator('..').boundingBox())!.height).toBeLessThanOrEqual(440)
    const overflow = await first.locator('[data-field]').evaluateAll(elements => elements.filter(element => element.scrollWidth > element.clientWidth + 1).length)
    expect(overflow).toBe(0)
    await first.getByRole('button', { name: 'Inspect usage', exact: true }).click()
    await expect(page.getByTestId('admin-last-action')).toHaveText('1:Inspect usage')
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
  }
})

test('mobile group-management keeps nine fields including composite usage in a compact card', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await intercept(page, 0)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto(`/layout-tests/index.html?view=admin-layout&groups=1&theme=${theme}&locale=${locale}`)
  const fields = page.locator('.record-card-fields')
  await expect(fields).toHaveCount(1)
  await expect(fields.locator('[data-field]')).toHaveCount(9)
  await expect(fields.locator('[data-field="name"]')).toContainText('Compact mobile routing group')
  await expect(fields.locator('[data-field="usage"]')).toContainText('$0.00')
  expect((await fields.locator('..').boundingBox())!.height).toBeLessThanOrEqual(490)
  const short = await fields.locator('[data-field="status"]').evaluate(row => ({
    height: row.getBoundingClientRect().height,
    labelTop: row.firstElementChild!.getBoundingClientRect().top,
    valueTop: row.lastElementChild!.getBoundingClientRect().top,
  }))
  expect(short.height).toBeLessThanOrEqual(28)
  expect(Math.abs(short.labelTop - short.valueTop)).toBeLessThanOrEqual(2)
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
})
