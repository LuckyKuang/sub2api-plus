import { expect, test } from '@playwright/test'

// Regression: the empty state must stay inside the visible scrollport of a wide
// table (centered across the visible columns, pinned left columns included),
// instead of centering across the full min-w-max table width and drifting off
// screen. Covers the admin /usage "暂无数据" report and every DataTable.
test('wide-table empty state covers the visible columns, not the full table width', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto(`/layout-tests/index.html?view=admin-layout&emptyWide=1&theme=${theme}&locale=${locale}`)

  const scrollport = page.locator('.record-table-scroll').first()
  await expect(scrollport).toBeVisible()
  const fill = page.locator('.empty-state-fill').first()
  await expect(fill).toBeVisible()

  // The table must genuinely overflow its scrollport for this to be meaningful.
  const metrics = await scrollport.evaluate(el => ({ client: el.clientWidth, scroll: el.scrollWidth }))
  expect(metrics.scroll).toBeGreaterThan(metrics.client + 1)

  // The empty-state layer spans the visible scrollport, not the full table width.
  const fillBox = (await fill.boundingBox())!
  expect(fillBox.width).toBeLessThanOrEqual(metrics.client + 2)

  // The message is horizontally centered within the visible scrollport.
  const view = (await scrollport.boundingBox())!
  const text = (await page.locator('.empty-state-fill p').boundingBox())!
  const viewCenter = view.x + view.width / 2
  const textCenter = text.x + text.width / 2
  expect(Math.abs(textCenter - viewCenter)).toBeLessThanOrEqual(2)
})
