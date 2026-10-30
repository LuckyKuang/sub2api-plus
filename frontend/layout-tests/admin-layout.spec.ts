import { expect, test } from '@playwright/test'
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
