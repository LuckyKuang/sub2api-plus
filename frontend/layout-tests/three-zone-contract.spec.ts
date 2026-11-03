import { expect, test, type Locator, type Page } from '@playwright/test'
import { wheelScroll } from './wheel'

// The console shell is one contract across every client: four regions (brand
// top-left, header, sidebar, middle), and the middle split into three zones
// where only the middle zone scrolls. Because a scrollport clips its content
// to its own radius, the visible corner is always the outermost clipping
// ancestor's curve; these assertions pin that relationship so a corner can
// never be cut mid-arc while the body scrolls.
const zoneTop = '.table-page-layout > .layout-section-fixed:not(.table-page-pagination), .workspace-page > .workspace-fixed'
const zoneMiddle = '.table-page-layout > .layout-section-scrollable, .workspace-page > .workspace-scroll, .workspace-page > .workspace-table-card'
const zoneBottom = '.table-page-layout > .table-page-pagination, .workspace-page > .pagination-card'

async function visible(page: Page, selector: string): Promise<Locator> {
  const located = page.locator(selector).filter({ visible: true })
  await expect(located.first(), selector).toBeVisible()
  return located.first()
}

test.describe('three-zone console shell', () => {
  test('desktop shell keeps three ordered zones and only the middle one scrolls', async ({ page }, info) => {
    const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
    const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.goto(`/layout-tests/index.html?view=admin-layout&theme=${theme}&locale=${locale}`)

    // Four regions exist.
    await expect(page.locator('.sidebar-header').first()).toBeVisible()
    await expect(page.locator('.app-header')).toBeVisible()
    await expect(page.locator('.sidebar')).toBeVisible()
    await expect(page.locator('.app-main .app-page')).toBeVisible()

    // The admin console keeps its page at the full width of the main viewport.
    // Only the user console narrows its page and opts into a content cap.
    const mainWidth = await page.locator('.app-main').evaluate(element => element.clientWidth)
    const adminPage = await page.locator('.app-main .app-page').evaluate(element => ({
      width: element.clientWidth,
      maxWidth: getComputedStyle(element).maxWidth,
    }))
    expect(adminPage.maxWidth).toBe('none')
    expect(adminPage.width).toBe(mainWidth)

    const top = await visible(page, zoneTop)
    const middle = await visible(page, zoneMiddle)
    const bottom = await visible(page, zoneBottom)
    const topBox = (await top.boundingBox())!
    const middleBox = (await middle.boundingBox())!
    const bottomBox = (await bottom.boundingBox())!

    // The three zones are siblings in that order, separated by the shared gap.
    expect(middleBox.y).toBeGreaterThanOrEqual(topBox.y + topBox.height - 1)
    expect(bottomBox.y).toBeGreaterThanOrEqual(middleBox.y + middleBox.height - 1)
    expect(Math.abs(middleBox.y - topBox.y - topBox.height - 16)).toBeLessThanOrEqual(1)
    expect(Math.abs(bottomBox.y - middleBox.y - middleBox.height - 16)).toBeLessThanOrEqual(1)

    // Every zone container shares the shell page radius, and it is never
    // smaller than a card nested inside it.
    for (const zone of [top, middle, bottom]) {
      const measured = await zone.evaluate(element => ({
        radius: parseFloat(getComputedStyle(element).borderRadius),
        cards: [...element.querySelectorAll('.card')].map(card => parseFloat(getComputedStyle(card).borderRadius)),
      }))
      expect(measured.radius).toBe(24)
      for (const card of measured.cards) expect(card).toBeLessThanOrEqual(measured.radius)
    }

    // Only the middle zone scrolls: no other zone may scroll, and the
    // document itself must not. A table viewport nested inside the middle
    // zone is part of that zone, so it is allowed.
    const scrolling = await page.evaluate(() => {
      const middle = document.querySelector('.table-page-layout > .layout-section-scrollable, .workspace-page > .workspace-scroll')
      const offenders: string[] = []
      for (const element of document.querySelectorAll('.app-page *')) {
        const overflowY = getComputedStyle(element).overflowY
        if (overflowY !== 'auto' && overflowY !== 'scroll') continue
        if (!middle || !middle.contains(element)) offenders.push(element.className.toString())
      }
      return offenders
    })
    expect(scrolling).toEqual([])
    expect(await page.evaluate(() => document.documentElement.scrollHeight <= window.innerHeight)).toBe(true)
  })

  test('the middle zone keeps its corners rounded while the body scrolls', async ({ page }, info) => {
    const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
    const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.goto(`/layout-tests/index.html?view=admin-layout&count=60&theme=${theme}&locale=${locale}`)
    const middle = await visible(page, zoneMiddle)
    const viewport = page.locator('.record-table-scroll').filter({ visible: true }).first()

    const measure = async () => {
      const frame = page.locator('.static-table-frame').filter({ visible: true }).first()
      const frameExists = await frame.count()
      const clipping = frameExists ? frame : middle
      const geometry = await clipping.evaluate(element => {
        const style = getComputedStyle(element)
        return {
          radius: parseFloat(style.borderRadius),
          corners: ['borderTopLeftRadius', 'borderTopRightRadius', 'borderBottomLeftRadius', 'borderBottomRightRadius']
            .map(property => parseFloat(style.getPropertyValue(property.replace(/[A-Z]/g, letter => `-${letter.toLowerCase()}`)))),
          overflow: style.overflow,
        }
      })
      const scrollport = await viewport.evaluate(element => parseFloat(getComputedStyle(element).borderRadius))
      const railBefore = await page.locator('.static-table-header-rail').filter({ visible: true }).first().boundingBox().catch(() => null)
      return { geometry, scrollport, railBefore }
    }

    await wheelScroll(page, viewport, 600)
    const atMiddle = await measure()
    // The outermost clipping ancestor owns the whole curve, so every corner
    // agrees; the scrollport owns none of it.
    expect(atMiddle.geometry.radius).toBe(24)
    expect(atMiddle.geometry.corners.every(corner => corner === 24)).toBe(true)
    expect(atMiddle.scrollport).toBe(0)

    await wheelScroll(page, viewport, 5000)
    const atBottom = await measure()
    expect(atBottom.geometry.radius).toBe(24)
    expect(atBottom.geometry.corners.every(corner => corner === 24)).toBe(true)
    expect(atBottom.scrollport).toBe(0)
    // The header rail is a separate box and must not move with the body.
    if (atMiddle.railBefore && atBottom.railBefore) expect(atBottom.railBefore).toEqual(atMiddle.railBefore)
  })

  test('user client keeps the same three zones with its narrower content width', async ({ page }, info) => {
    const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
    const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.goto(`/layout-tests/index.html?view=user-layout&theme=${theme}&locale=${locale}`)
    await expect(page.locator('.app-layout-user')).toBeVisible()

    const top = await visible(page, zoneTop)
    const middle = await visible(page, zoneMiddle)
    const bottom = await visible(page, zoneBottom)
    const mainWidth = await page.locator('.app-main').evaluate(element => element.clientWidth)
    const pageWidth = await page.locator('.app-page').evaluate(element => element.clientWidth)
    // Only the user console opts into a cap; the ratio and ceiling are its own.
    expect(Math.abs(pageWidth - Math.min(mainWidth * 0.94, 1600))).toBeLessThanOrEqual(1)
    for (const zone of [top, middle, bottom]) {
      expect(await zone.evaluate(element => parseFloat(getComputedStyle(element).borderRadius))).toBe(24)
    }
  })

  test('mobile clients scroll the document and frame no zone', async ({ page }, info) => {
    const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
    const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto(`/layout-tests/index.html?view=admin-layout&theme=${theme}&locale=${locale}`)

    // The shell does not clip on a phone: the document is the scroll container.
    expect(await page.locator('.app-main').evaluate(element => getComputedStyle(element).overflowY)).toBe('visible')
    const innerScrollers = await page.evaluate(() => {
      const offenders: string[] = []
      for (const element of document.querySelectorAll('.app-page *')) {
        const overflowY = getComputedStyle(element).overflowY
        if (overflowY === 'auto' || overflowY === 'scroll') offenders.push(element.className.toString())
      }
      return offenders
    })
    expect(innerScrollers).toEqual([])

    // No card inside the page may outgrow the zone container clipping it, so
    // every radius is a ladder value and none exceeds the shell page radius.
    const zoneRadius = await page.locator('.app-main').evaluate(element => parseFloat(getComputedStyle(element).borderRadius))
    const cardRadii = await page.locator('.app-page .card:visible').evaluateAll(elements =>
      [...new Set(elements.map(element => parseFloat(getComputedStyle(element).borderRadius)))])
    expect(cardRadii.length).toBeGreaterThan(0)
    expect(cardRadii.every(radius => radius <= zoneRadius)).toBe(true)
    expect(cardRadii.every(radius => [14, 20].includes(radius))).toBe(true)
    const inset = await (await visible(page, zoneTop)).evaluate(element => Math.round(element.getBoundingClientRect().left))
    expect(inset).toBe(12)

    await page.goto(`/layout-tests/index.html?view=user-layout&theme=${theme}&locale=${locale}`)
    const userInset = await page.locator('.app-page > *').first().evaluate(element => Math.round(element.getBoundingClientRect().left))
    expect(userInset).toBe(12)
    const userScrollers = await page.evaluate(() => {
      const offenders: string[] = []
      for (const element of document.querySelectorAll('.app-page *')) {
        const overflowY = getComputedStyle(element).overflowY
        if (overflowY === 'auto' || overflowY === 'scroll') offenders.push(element.className.toString())
      }
      return offenders
    })
    expect(userScrollers).toEqual([])
  })
})
