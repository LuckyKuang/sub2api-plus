import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import ActionMenu, { type ActionMenuItem } from '../ActionMenu.vue'
import { setAdminSupportContext } from '@/utils/adminSupportContext'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const items: ActionMenuItem[] = [
  { key: 'import', label: 'Import', icon: 'upload' },
  { key: 'hidden', label: 'Hidden', hidden: true },
  { key: 'disable', label: 'Disable', icon: 'ban', tone: 'warning', mutates: true },
  { key: 'delete', label: 'Delete', icon: 'trash', tone: 'danger', mutates: true },
]

let wrapper: VueWrapper | undefined

function mountMenu() {
  wrapper = mount(ActionMenu, {
    props: { items },
    attachTo: document.body,
  })
  return wrapper
}

const menu = () => document.querySelector<HTMLElement>('[role="menu"]')
const entries = () => Array.from(document.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'))

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  setAdminSupportContext(null)
  document.body.replaceChildren()
})

describe('ActionMenu', () => {
  it('opens a menu of the visible actions and focuses the first one', async () => {
    const view = mountMenu()
    const trigger = view.get('button')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(menu()).toBeNull()
    await trigger.trigger('click')
    await nextTick()
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(entries().map(entry => entry.textContent?.trim())).toEqual(['Import', 'Disable', 'Delete'])
    expect(document.activeElement).toBe(entries()[0])
    expect(entries()[2]!.classList).toContain('action-menu-item--danger')
  })

  it('emits the chosen action once, closes and returns focus to the trigger', async () => {
    const view = mountMenu()
    await view.get('button').trigger('click')
    await nextTick()
    entries()[2]!.click()
    await nextTick()
    expect(view.emitted('select')).toEqual([['delete']])
    expect(menu()).toBeNull()
    expect(document.activeElement).toBe(view.get('button').element)
  })

  it('moves with arrow keys and closes on Escape', async () => {
    const view = mountMenu()
    await view.get('button').trigger('click')
    await nextTick()
    menu()!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }))
    expect(document.activeElement).toBe(entries()[2])
    menu()!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    expect(document.activeElement).toBe(entries()[0])
    menu()!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    expect(menu()).toBeNull()
    expect(view.emitted('select')).toBeUndefined()
    expect(document.activeElement).toBe(view.get('button').element)
  })

  it('closes on an outside press without choosing anything', async () => {
    const view = mountMenu()
    await view.get('button').trigger('click')
    await nextTick()
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await nextTick()
    expect(menu()).toBeNull()
    expect(view.emitted('select')).toBeUndefined()
  })

  it('locks only mutating actions during read-only assistance', async () => {
    setAdminSupportContext({ actorId: 1, userId: 42 })
    const view = mountMenu()
    await view.get('button').trigger('click')
    await nextTick()
    const [importEntry, disableEntry, deleteEntry] = entries()
    expect(importEntry!.disabled).toBe(false)
    expect(disableEntry!.disabled).toBe(true)
    expect(deleteEntry!.disabled).toBe(true)
    deleteEntry!.click()
    await nextTick()
    expect(view.emitted('select')).toBeUndefined()
  })
})
