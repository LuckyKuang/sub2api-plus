import { expect, test, type Page } from '@playwright/test'

// Browser audit v5: real production styles retain Vue's root duration, dialogs
// fade/move continuously for 200/140ms, menus for 150/100ms, and calendars never
// transform. Samples observe actual frames and DOM lifetime, not class presence
// alone. The previous Tailwind-purged root duration was 0s and ended at ~38ms.
test.beforeEach(async ({ page }, info) => {
  await page.route('**/api/v1/**', route => route.fulfill({ json: { code: 0, data:
    new URL(route.request().url()).pathname.endsWith('/admin/system/check-updates')
      ? { current_version: '0.1.0', latest_version: '0.1.0', has_update: false, build_type: 'source', release_info: null }
      : new URL(route.request().url()).pathname.endsWith('/subscriptions/active')
      ? [{ id: 1, group_id: 1, status: 'active', expires_at: null, group: { name: 'Motion subscription' } }]
      : [],
  } }))
  await page.goto(`/layout-tests/motion.html?theme=${info.project.use.colorScheme}`)
  await expect(page.getByTestId('motion-ready')).toBeVisible()
})

async function frames(page: Page, trigger: string, rootSelector: string, milliseconds: number) {
  return page.evaluate(async ({ trigger, rootSelector, milliseconds }) => {
    const samples: Array<{ time: number; connected: boolean; active: boolean; duration: number; properties: string; animation: string; opacity: number; x: number; y: number; scale: number }> = []
    const start = performance.now()
    const button = document.querySelector<HTMLElement>(trigger)!
    button.focus()
    button.click()
    do {
      await new Promise<void>(resolve => requestAnimationFrame(() => resolve()))
      const root = document.querySelector<HTMLElement>(rootSelector)
      const panel = root?.querySelector<HTMLElement>(':scope > .modal-content, :scope > .glass-surface') ?? root
      const rootStyle = root ? getComputedStyle(root) : null
      const style = panel ? getComputedStyle(panel) : null
      const matrix = style && style.transform !== 'none' ? new DOMMatrix(style.transform) : new DOMMatrix()
      samples.push({
        time: performance.now() - start,
        connected: !!root,
        active: !!root && /(?:enter|leave)-active/.test(root.className),
        duration: rootStyle ? parseFloat(rootStyle.transitionDuration) : 0,
        properties: rootStyle?.transitionProperty ?? '',
        animation: rootStyle?.animationName ?? '',
        opacity: style ? parseFloat(style.opacity) : 0,
        x: matrix.m41, y: matrix.m42, scale: matrix.m11,
      })
    } while (performance.now() - start < milliseconds)
    return samples
  }, { trigger, rootSelector, milliseconds })
}

for (const kind of ['dialog', 'announcement'] as const) {
  test(`production ${kind} retains root timing and does not snap before its panel completes`, async ({ page }) => {
    const root = kind === 'dialog' ? '.modal-overlay' : '.surface-overlay'
    const trigger = kind === 'dialog' ? '[data-testid="open-motion-dialog"]' : '[data-testid="open-motion-announcement"]'
    const enter = await frames(page, trigger, root, 320)
    expect(enter.some(frame => frame.active && frame.duration === .2)).toBe(true)
    const intermediate = enter.filter(frame => frame.time >= 60 && frame.time <= 150)
    expect(intermediate.some(frame => frame.active && frame.opacity > 0 && frame.opacity < 1 && frame.y > 0 && frame.scale < 1)).toBe(true)
    expect(enter.at(-1)).toMatchObject({ connected: true, active: false, opacity: 1, x: 0, y: 0, scale: 1 })

    const close = kind === 'dialog' ? '.modal-header button' : '[data-testid="announcement-popup-dismiss"]'
    const leave = await frames(page, close, root, 260)
    expect(leave.some(frame => frame.active && frame.duration === .14)).toBe(true)
    expect(leave.some(frame => frame.time >= 60 && frame.connected && frame.opacity > 0 && frame.opacity < 1)).toBe(true)
    expect(leave.find(frame => !frame.connected)!.time).toBeGreaterThanOrEqual(120)
    expect(leave.at(-1)!.connected).toBe(false)
    if (kind === 'dialog') await expect(page.getByTestId('open-motion-dialog')).toBeFocused()
  })
}

test('the right drawer moves horizontally while retaining the common overlay duration', async ({ page }) => {
  const enter = await frames(page, '[data-testid="open-motion-drawer"]', '.modal-overlay', 320)
  expect(enter.some(frame => frame.active && frame.duration === .2 && frame.x > 0 && frame.y === 0 && frame.scale === 1)).toBe(true)
  expect(enter.at(-1)).toMatchObject({ connected: true, active: false, x: 0, y: 0, scale: 1 })
})

test('header user, subscription and announcement surfaces use the shared transitions', async ({ page }) => {
  for (const [trigger, panel] of [
    ['.app-header button[aria-label="User menu"]', '.header-user-dropdown'],
    ['.app-header button[title="View subscription details"]', '.app-header button[title="View subscription details"] + .glass-surface'],
  ]) {
    await expect(page.locator(trigger)).toBeVisible()
    const enter = await frames(page, trigger, panel, 260)
    expect(enter.some(frame => frame.active && frame.duration === .15)).toBe(true)
    expect(enter.filter(frame => frame.connected).every(frame => frame.animation === 'none')).toBe(true)
    const leave = await frames(page, '[data-testid="outside-control"]', panel, 220)
    expect(leave.some(frame => frame.active && frame.duration === .1)).toBe(true)
    expect(leave.at(-1)!.connected).toBe(false)
  }
  const enter = await frames(page, '.app-header button[aria-label="Announcements"]', '.surface-overlay', 320)
  expect(enter.some(frame => frame.active && frame.duration === .2 && frame.y > 0 && frame.opacity > 0 && frame.opacity < 1)).toBe(true)
  const leave = await frames(page, '.surface-overlay button[aria-label="Close"]', '.surface-overlay', 260)
  expect(leave.some(frame => frame.active && frame.duration === .14)).toBe(true)
  expect(leave.at(-1)!.connected).toBe(false)
})

test('the real administrator version menu retains 150/100ms timing without legacy utility overrides', async ({ page }) => {
  await page.getByTestId('enable-admin-motion').click()
  const trigger = '[data-testid="version-control"] > div > button'
  await expect(page.locator(trigger)).toContainText('v0.1.0')
  const enter = await frames(page, trigger, '.version-dropdown', 260)
  expect(enter.some(frame => frame.active && frame.duration === .15)).toBe(true)
  expect(enter.filter(frame => frame.active).every(frame => !frame.properties.includes('all'))).toBe(true)
  // This component deliberately ignores outside button clicks; close it using
  // its original toggle rather than changing its hit-testing contract.
  const leave = await frames(page, trigger, '.version-dropdown', 220)
  expect(leave.some(frame => frame.active && frame.duration === .1)).toBe(true)
  expect(leave.at(-1)!.connected).toBe(false)
})

test('plain select, checkbox and refresh menus retain a real leave transition and their original values', async ({ page }) => {
  for (const [trigger, panel] of [
    ['button[aria-label="Motion select"]', '.select-dropdown-portal'],
    ['button[aria-label="Motion checkboxes"]', '.select-dropdown-portal'],
    ['button[title="Auto Refresh"]', 'main .surface-panel.absolute'],
  ]) {
    const enter = await frames(page, trigger, panel, 260)
    expect(enter.some(frame => frame.active && frame.duration === .15)).toBe(true)
    expect(enter.at(-1)).toMatchObject({ connected: true, active: false, opacity: 1, x: 0, y: 0 })
    expect(enter.filter(frame => frame.active).every(frame => !frame.properties.includes('all'))).toBe(true)
    const leave = await frames(page, '[data-testid="outside-control"]', panel, 220)
    expect(leave.some(frame => frame.active && frame.duration === .1 && frame.connected)).toBe(true)
    expect(leave.find(frame => !frame.connected)!.time).toBeGreaterThanOrEqual(80)
    expect(leave.at(-1)!.connected).toBe(false)
  }
  await expect(page.getByTestId('motion-selection')).toHaveText('first /')
})

test('both calendar panels fade without moving and keep their position stable while opening and closing', async ({ page }) => {
  for (const [trigger, panel] of [
    ['.date-picker-trigger', '.date-picker-dropdown'],
    ['button[aria-label="Motion calendar"]', '.date-time-panel'],
  ]) {
    const enter = await frames(page, trigger, panel, 260)
    expect(enter.some(frame => frame.active && frame.duration === .15 && frame.opacity > 0 && frame.opacity < 1)).toBe(true)
    expect(enter.every(frame => frame.x === 0 && frame.y === 0 && frame.scale === 1)).toBe(true)
    const geometry = await page.locator(panel).evaluate(el => {
      const rect = el.getBoundingClientRect()
      return { left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom }
    })
    expect(enter.filter(frame => frame.active).every(frame => frame.properties === 'opacity')).toBe(true)
    expect(geometry.left).toBeGreaterThanOrEqual(0)
    expect(geometry.right).toBeLessThanOrEqual(1440)
    expect(geometry.top).toBeGreaterThanOrEqual(0)
    expect(geometry.bottom).toBeLessThanOrEqual(900)
    const leave = await frames(page, '[data-testid="outside-control"]', panel, 220)
    expect(leave.some(frame => frame.active && frame.duration === .1)).toBe(true)
    expect(leave.find(frame => !frame.connected)!.time).toBeGreaterThanOrEqual(80)
    expect(leave.every(frame => frame.x === 0 && frame.y === 0 && frame.scale === 1)).toBe(true)
  }
})

test('reduced motion covers body portals and overlay children without suppressing focus restoration', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  for (const [trigger, root] of [
    ['[data-testid="open-motion-dialog"]', '.modal-overlay'],
    ['[data-testid="open-motion-announcement"]', '.surface-overlay'],
    ['button[aria-label="Motion select"]', '.select-dropdown-portal'],
    ['button[aria-label="Motion checkboxes"]', '.select-dropdown-portal'],
    ['.date-picker-trigger', '.date-picker-dropdown'],
    ['button[aria-label="Motion calendar"]', '.date-time-panel'],
  ]) {
    await test.step(`reduced motion: ${root}`, async () => {
      const enter = await frames(page, trigger, root, 80)
      expect(enter.some(frame => frame.active && frame.duration <= .001)).toBe(true)
      expect(enter.every(frame => frame.x === 0 && frame.y === 0 && frame.scale === 1)).toBe(true)
      if (root === '.modal-overlay') {
        await page.locator('.modal-header button').click({ timeout: 5_000 })
        await expect(page.getByTestId('open-motion-dialog')).toBeFocused()
      } else if (root === '.surface-overlay') {
        await page.getByTestId('announcement-popup-dismiss').click({ timeout: 5_000 })
      } else {
        // Calendar panels legitimately cover the following outside control.
        // Escape uses the real keyboard path without clicking through a panel.
        await page.keyboard.press('Escape')
      }
      await expect(page.locator(root)).toHaveCount(0)
    })
  }
})
