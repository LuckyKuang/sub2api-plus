import { expect, test, type Locator, type Page } from '@playwright/test'

// Requirements: docs/UI_INTERACTION.md and the user's full-interface acceptance
// condition: controls must remain visible, unclipped and reachable after styling.
// Browser geometry and hit testing establish this independently of CSS values.
async function expectReachable(target: Locator): Promise<void> {
  await expect(target).toBeVisible()
  await expect.poll(() => target.evaluate(element => {
    const rect = element.getBoundingClientRect()
    const insetX = Math.min(20, rect.width / 3)
    const insetY = Math.min(20, rect.height / 3)
    const points = [
      [rect.left + rect.width / 2, rect.top + rect.height / 2],
      [rect.left + insetX, rect.top + insetY],
      [rect.right - insetX, rect.top + insetY],
      [rect.left + insetX, rect.bottom - insetY],
      [rect.right - insetX, rect.bottom - insetY],
    ]
    return {
      withinViewport: rect.width > 0 && rect.height > 0 && rect.left >= -0.5 && rect.top >= -0.5 && rect.right <= innerWidth + 0.5 && rect.bottom <= innerHeight + 0.5,
      receivesPointer: points.every(([x, y]) => element.contains(document.elementFromPoint(x!, y!))),
    }
  })).toEqual({ withinViewport: true, receivesPointer: true })
}

async function expectInside(target: Locator, container: Locator): Promise<void> {
  const outer = await container.boundingBox()
  expect(outer).not.toBeNull()
  await expect.poll(() => target.evaluate((element, box) => {
    const rect = element.getBoundingClientRect()
    return rect.left >= box.x - 1 && rect.top >= box.y - 1 && rect.right <= box.x + box.width + 1 && rect.bottom <= box.y + box.height + 1
  }, outer!)).toBe(true)
}

async function scrollToBottom(scrollport: Locator): Promise<void> {
  await scrollport.evaluate(element => { element.scrollTop = element.scrollHeight })
}

async function tabTo(page: Page, target: Locator): Promise<void> {
  for (let step = 0; step < 16; step++) {
    if (await target.evaluate(element => element === document.activeElement)) return
    await page.keyboard.press('Tab')
  }
  await expect(target).toBeFocused()
}

test('checkbox dropdown retains selections and supports narrow translated menus', async ({ page }) => {
  const field = page.getByTestId('checkbox-dropdown-field')
  const trigger = field.locator('button')
  await trigger.scrollIntoViewIfNeeded()
  await expectReachable(trigger)
  await trigger.click()
  const panel = page.locator(`#${await trigger.getAttribute('aria-controls')}`)
  await expectReachable(panel)
  const options = panel.locator('input[type="checkbox"]')
  await options.first().check()
  await options.nth(1).check()
  await expect(options.first()).toBeChecked()
  await expect(options.nth(1)).toBeChecked()
  await options.last().scrollIntoViewIfNeeded()
  await expectReachable(options.last())
  await options.last().check()
  await expect(page.getByTestId('checkbox-selection')).toHaveText('1,2,24')
  await options.last().focus()
  await page.keyboard.press('Escape')
  await expect(panel).toHaveCount(0)
  await expect(trigger).toBeFocused()
})

test('checkbox dropdown supports keyboard entry, repeated choices and form tab order', async ({ page }) => {
  const trigger = page.getByTestId('checkbox-dropdown-field').locator('button')
  await tabTo(page, trigger)
  await page.keyboard.press('Enter')
  const panel = page.locator(`#${await trigger.getAttribute('aria-controls')}`)
  const options = panel.locator('input[type="checkbox"]')
  await expect(options.first()).toBeFocused()
  await page.keyboard.press('Space')
  await page.keyboard.press('Tab')
  await expect(options.nth(1)).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page.getByTestId('checkbox-selection')).toHaveText('1,2')
  await page.keyboard.press('End')
  await expect(options.last()).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(panel).toHaveCount(0)
  await expect(page.getByTestId('edge-date-field').locator('.date-picker-trigger')).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(trigger).toBeFocused()
  await page.keyboard.press('ArrowDown')
  await expect(options.first()).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(panel).toHaveCount(0)
  await expect(trigger).toBeFocused()
  await page.keyboard.press('ArrowUp')
  await expect(options.last()).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(panel).toHaveCount(0)
  await expect(trigger).toBeFocused()
})

async function chooseDate(page: Page, field: Locator, afterOpen?: () => Promise<void>): Promise<void> {
  const trigger = field.locator('.date-picker-trigger')
  await trigger.scrollIntoViewIfNeeded()
  await expectReachable(trigger)
  await trigger.click()
  const popup = page.locator('.date-picker-dropdown')
  await expectReachable(popup)
  if (afterOpen) {
    await afterOpen()
    await expectReachable(popup)
  }
  const start = popup.locator('.date-time-trigger').first()
  await expectReachable(start)
  await start.click()
  const calendar = page.locator('.date-time-panel')
  await expectReachable(calendar)
  const day = calendar.locator('[data-date="2024-04-02"]')
  await expectReachable(day)
  await day.click()
  const calendarApply = calendar.locator('.date-time-actions .btn-primary')
  await expectReachable(calendarApply)
  await calendarApply.click()
  await expect(calendar).toBeHidden()
  await expect(popup).toBeVisible()
  const rangeApply = popup.locator('.date-picker-apply')
  await rangeApply.scrollIntoViewIfNeeded()
  await expectReachable(rangeApply)
  await rangeApply.click()
  await expect(popup).toBeHidden()
}

test.beforeEach(async ({ page }, info) => {
  const locale = String(info.project.use.locale).startsWith('zh') ? 'zh' : 'en'
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  await page.goto(`/layout-tests/index.html?locale=${locale}&theme=${theme}`)
  await expect(page.getByTestId('layout-ready')).toBeVisible()
})

test('date range escapes a clipped scrolling card at the bottom/right edge, including its nested calendar', async ({ page }) => {
  const scrollport = page.getByTestId('card-scroll')
  await scrollToBottom(scrollport)
  const field = page.getByTestId('edge-date-field')
  const anchor = await field.boundingBox()
  const viewport = page.viewportSize()!
  expect(anchor!.y + anchor!.height).toBeGreaterThan(viewport.height - 150)
  expect(anchor!.x + anchor!.width).toBeGreaterThan(viewport.width - 40)
  await chooseDate(page, field, async () => {
    await scrollport.evaluate(element => { element.scrollTop -= 16 })
  })
  await expect(page.getByTestId('edge-date-result')).toHaveText('2024-04-02')
})

test('plain and searchable selection menus stay reachable at screen edges and inside a dialog', async ({ page }) => {
  await scrollToBottom(page.getByTestId('card-scroll'))
  await page.getByTestId('edge-select-field').locator('.select-trigger').click()
  const menu = page.getByRole('listbox')
  await expectReachable(menu)
  const finalOption = menu.getByRole('option').last()
  await finalOption.scrollIntoViewIfNeeded()
  await expectReachable(finalOption)
  await finalOption.click()
  await expect(page.getByTestId('edge-select-result')).toHaveText('24')
  await expect(menu).toHaveCount(0)

  await page.getByTestId('open-dialog').click()
  const dialog = page.locator('.modal-overlay')
  await scrollToBottom(dialog.locator('.modal-body'))
  await dialog.getByTestId('dialog-select-field').locator('.select-trigger').click()
  await expectReachable(menu)
  const search = menu.locator('.select-search-input')
  await expectReachable(search)
  await search.fill('24')
  await expect(menu.getByRole('option')).toHaveCount(1)
  await expectReachable(menu.getByRole('option'))
  await menu.getByRole('option').click()
  await expect(page.getByTestId('dialog-select-result')).toHaveText('24')
  await expect(dialog).toBeVisible()
})

test('long dialog titles and footer actions fit while the final content and date picker remain reachable', async ({ page }) => {
  await page.getByTestId('open-dialog').click()
  const dialog = page.locator('.modal-overlay')
  const panel = dialog.locator('.modal-content')
  await expectReachable(panel)
  const title = panel.locator('.modal-title')
  await expectInside(title, panel)
  await expect.poll(() => title.evaluate(element => element.scrollWidth <= element.clientWidth + 1 && element.scrollHeight <= element.clientHeight + 1)).toBe(true)
  await expectReachable(panel.getByRole('button', { name: 'Close modal', exact: true }))
  for (const action of ['dialog-cancel', 'dialog-save']) {
    await expectInside(panel.getByTestId(action), panel)
    await expectReachable(panel.getByTestId(action))
  }
  const body = panel.locator('.modal-body')
  await expect.poll(() => body.evaluate(element => element.scrollHeight > element.clientHeight)).toBe(true)
  await scrollToBottom(body)
  const last = page.getByTestId('last-content')
  await expectInside(last, body)
  await expectReachable(last)
  await last.click()
  await expect(page.getByTestId('last-content-result')).toHaveText('1')
  await chooseDate(page, page.getByTestId('dialog-date-field'))
  await expect(page.getByTestId('dialog-date-result')).toHaveText('2024-04-02')
  await expectReachable(page.getByTestId('dialog-save'))
  await page.getByTestId('dialog-save').click()
  await expect(dialog).toBeHidden()
  await expect(page.getByTestId('saved-result')).toHaveText('1')
})

test('keyboard reaches date controls and apply while Escape closes only the topmost calendar, range, then parent', async ({ page }) => {
  const launch = page.getByTestId('open-dialog')
  await launch.click()
  const dialog = page.locator('.modal-overlay')
  await scrollToBottom(dialog.locator('.modal-body'))
  const trigger = page.getByTestId('dialog-date-field').locator('.date-picker-trigger')
  await trigger.click()
  const range = page.locator('.date-picker-dropdown')
  await expect(range).toBeVisible()
  const start = range.locator('.date-time-trigger').first()
  await tabTo(page, start)
  await expectReachable(start)
  await page.keyboard.press('Enter')
  const calendar = page.locator('.date-time-panel')
  await expectReachable(calendar)
  await page.keyboard.press('Escape')
  await expect(calendar).toBeHidden()
  await expect(range).toBeVisible()
  await expect(dialog).toBeVisible()
  await expect(start).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(range).toBeHidden()
  await expect(dialog).toBeVisible()
  await expect(trigger).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(launch).toBeFocused()

  await page.keyboard.press('Enter')
  await scrollToBottom(dialog.locator('.modal-body'))
  await trigger.click()
  await expect(range).toBeVisible()
  const apply = range.locator('.date-picker-apply')
  await tabTo(page, apply)
  await expectReachable(apply)
  await page.keyboard.press('Enter')
  await expect(range).toBeHidden()
  await expect(dialog).toBeVisible()
})

test('an exceptionally long title remains readable without displacing actions in a short mobile window', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 480 })
  await page.getByTestId('open-extreme-dialog').click()
  const panel = page.locator('.modal-content')
  const title = panel.locator('.modal-title')
  await expectReachable(panel)
  await expect.poll(() => title.evaluate(element => element.scrollHeight > element.clientHeight)).toBe(true)
  await expect(title).toHaveAttribute('tabindex', '0')
  await expectReachable(panel.getByRole('button', { name: 'Close modal', exact: true }))
  for (const action of ['dialog-cancel', 'dialog-save']) {
    await expectInside(panel.getByTestId(action), panel)
    await expectReachable(panel.getByTestId(action))
  }
  await tabTo(page, title)
  await page.keyboard.press('End')
  await expect.poll(() => title.evaluate(element => element.scrollTop + element.clientHeight >= element.scrollHeight - 1)).toBe(true)
  const body = panel.locator('.modal-body')
  await expect.poll(() => body.evaluate(element => element.clientHeight > 32)).toBe(true)
  await scrollToBottom(body)
  await expectReachable(page.getByTestId('last-content'))
  await page.getByTestId('last-content').click()
  await expect(page.getByTestId('last-content-result')).toHaveText('1')
  await panel.getByTestId('dialog-save').click()
  await expect(panel).toBeHidden()
})


test('shared navigation, segmented choices and horizontal row actions keep translated controls readable and reachable', async ({ page }, info) => {
  const locale = String(info.project.use.locale).startsWith('zh') ? 'zh' : 'en'
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  await page.goto(`/layout-tests/index.html?locale=${locale}&theme=${theme}&view=controls&accent=%23ec4899`)
  const gallery = page.getByTestId('style-gallery')
  await expect(gallery).toBeVisible()
  for (const alpha of ['1', '.8', '0']) {
    await page.evaluate(value => {
      document.documentElement.style.setProperty('--console-card-alpha', value)
    }, alpha)
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
    for (const inactive of await gallery.locator('.workspace-tab[aria-selected="false"], .card-tab[aria-selected="false"], .segmented-option[aria-pressed="false"]').all()) {
      await expect(inactive).toHaveCSS('border-top-color', 'rgba(0, 0, 0, 0)')
    }
    const tabs = gallery.locator('.workspace-tab')
    await tabs.last().scrollIntoViewIfNeeded()
    await expectReachable(tabs.last())
    await tabs.last().click()
    await expect(tabs.last()).toHaveAttribute('aria-selected', 'true')
    for (const target of await gallery.locator('.card-tab, .segmented-option, .table-action, .btn, .select-trigger, .input').all()) {
      await target.scrollIntoViewIfNeeded()
      await expectReachable(target)
      await expect.poll(() => target.evaluate(element => element.scrollWidth <= element.clientWidth + 1 && element.scrollHeight <= element.clientHeight + 1)).toBe(true)
    }
    const segments = gallery.locator('.segmented-option')
    await segments.last().click()
    await expect(segments.last()).toHaveAttribute('aria-pressed', 'true')
    await expect(segments.first()).toHaveAttribute('aria-pressed', 'false')
    const actions = page.getByTestId('table-actions')
    await expect.poll(() => actions.evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
    const edit = page.getByTestId('table-action-edit')
    await edit.scrollIntoViewIfNeeded()
    await expect.poll(() => edit.evaluate(element => {
      const icon = element.querySelector('svg')!.getBoundingClientRect()
      const label = element.querySelector('span')!.getBoundingClientRect()
      return icon.right <= label.left + 1
    })).toBe(true)
    await expect(page.getByTestId('table-action-disabled')).toBeDisabled()
    await expect(page.getByTestId('table-action-busy')).toBeDisabled()
    await expect(page.getByTestId('table-action-busy')).toHaveAttribute('aria-busy', 'true')
  }
  await page.getByTestId('table-action-edit').click()
  await expect(page.getByTestId('gallery-clicks')).toHaveText('1')
})


test('primary labels stay opaque and legible at opaque, half-transparent and transparent accent fills before and after hover', async ({ page }, info) => {
  const locale = String(info.project.use.locale).startsWith('zh') ? 'zh' : 'en'
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  for (const alpha of [1, .5, 0]) {
    for (const accent of ['ffffff', 'ffff00', '000000', '000080', '14b8a6']) {
      await page.goto(`/layout-tests/index.html?locale=${locale}&theme=${theme}&view=controls&accent=%23${accent}&alpha=${alpha}`)
      const button = page.getByTestId('style-gallery').locator('.btn-primary')
      await button.scrollIntoViewIfNeeded()
      await expectReachable(button)
      // Independent literal expectations for the known theme surfaces. No
      // foreground interpolation is permitted at intermediate transparency.
      const white = alpha === 1 ? ['000000', '000080'].includes(accent)
        : theme === 'dark' && (alpha === 0 || ['000000', '000080', '14b8a6'].includes(accent))
      const renderedColor = () => button.evaluate(element => {
        const canvas = document.createElement('canvas')
        canvas.width = canvas.height = 1
        const context = canvas.getContext('2d')!
        context.fillStyle = getComputedStyle(element).color
        context.fillRect(0, 0, 1, 1)
        return Array.from(context.getImageData(0, 0, 1, 1).data)
      })
      const expectedChannels = white ? [255, 255, 255, 255] : [0, 0, 0, 255]
      await expect.poll(renderedColor).toEqual(expectedChannels)
      await button.hover()
      await expect.poll(renderedColor).toEqual(expectedChannels)
      if (alpha === .5 && accent === '000000') {
        await page.evaluate(() => document.documentElement.classList.toggle('dark'))
        await expect.poll(renderedColor).toEqual(white ? [0, 0, 0, 255] : [255, 255, 255, 255])
        await expectReachable(button)
      }
    }
  }
})
