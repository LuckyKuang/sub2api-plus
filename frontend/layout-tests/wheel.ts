import { expect, type Locator, type Page } from '@playwright/test'

/** Establish painted input readiness, then deliver exactly one native wheel.
 * Callers retain their independent scroll, fixed-chrome and paging assertions.
 */
export async function wheelScroll(page: Page, scrollport: Locator, deltaY: number, target: Locator = scrollport) {
  await expect(scrollport).toBeVisible()
  await expect.poll(() => scrollport.evaluate(element => element.scrollHeight - element.clientHeight)).toBeGreaterThan(0)
  await target.hover()
  await target.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))))
  await expect.poll(async () => {
    const box = await target.boundingBox()
    if (!box) return false
    return scrollport.evaluate((element, point) => {
      const hit = document.elementFromPoint(point.x, point.y)
      return !!hit && element.contains(hit)
    }, { x: box.x + box.width / 2, y: box.y + box.height / 2 })
  }).toBe(true)
  await scrollport.evaluate(element => {
    element.setAttribute('data-wheel-event', 'null')
    document.addEventListener('wheel', event => {
      queueMicrotask(() => element.setAttribute('data-wheel-event', JSON.stringify({
        trusted: event.isTrusted,
        targetInside: event.target instanceof Node && element.contains(event.target),
        defaultPrevented: event.defaultPrevented,
      })))
    }, { once: true, capture: true })
  })
  try {
    await page.mouse.wheel(0, deltaY)
    await expect.poll(() => scrollport.evaluate(element => JSON.parse(element.getAttribute('data-wheel-event')!))).toEqual({
      trusted: true, targetInside: true, defaultPrevented: false,
    })
  } finally {
    await scrollport.evaluate(element => element.removeAttribute('data-wheel-event'))
  }
}
