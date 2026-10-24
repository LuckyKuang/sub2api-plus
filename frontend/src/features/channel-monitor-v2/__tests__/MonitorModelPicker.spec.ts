import { DOMWrapper, enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import MonitorModelPicker from '../MonitorModelPicker.vue'

vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
enableAutoUnmount(afterEach)
afterEach(() => { document.body.innerHTML = ''; vi.unstubAllGlobals(); vi.restoreAllMocks() })
async function picker() {
  const anchor = document.createElement('button')
  document.body.append(anchor)
  anchor.focus()
  const wrapper = mount(MonitorModelPicker, { attachTo: document.body, props: {
    open: false, anchor, title: 'Add models',
    candidates: [{ model: 'Alpha', source: 'observed' }, { model: 'alpha', source: 'preset' }, { model: 'existing', source: 'configured' }],
    unavailable: ['existing'],
  }, global: { stubs: { teleport: false } } })
  await wrapper.setProps({ open: true }); await flushPromises()
  return { wrapper, anchor, dialog: () => new DOMWrapper(document.querySelector<HTMLElement>('[data-testid="model-picker"]')!) }
}

describe('single composite model picker', () => {
  it('repositions a resized panel and clamps it when the trigger scrolls outside the viewport', async () => {
    let resize!: () => void
    const disconnect = vi.fn()
    vi.stubGlobal('ResizeObserver', class {
      constructor(callback: () => void) { resize = callback }
      observe() {}
      disconnect = disconnect
    })
    vi.stubGlobal('innerWidth', 1024)
    vi.stubGlobal('innerHeight', 800)
    const { wrapper, anchor, dialog } = await picker()
    let height = 240
    vi.spyOn(anchor, 'getBoundingClientRect').mockReturnValue({ top: 900, bottom: 920, left: 1100, right: 1120, width: 20, height: 20, x: 1100, y: 900, toJSON() {} })
    vi.spyOn(dialog().element, 'getBoundingClientRect').mockImplementation(() => ({ top: 0, bottom: height, left: 0, right: 440, width: 440, height, x: 0, y: 0, toJSON() {} }))
    resize(); await flushPromises()
    expect(dialog().attributes('style')).toContain('left: 568px')
    expect(dialog().attributes('style')).toContain('top: 544px')
    height = 500
    resize(); await flushPromises()
    expect(dialog().attributes('style')).toContain('top: 284px')
    wrapper.unmount()
    expect(disconnect).toHaveBeenCalled()
  })

  it('keeps candidate search and checkboxes together, commits only on Apply, and cancels local selection', async () => {
    const { wrapper, dialog } = await picker()
    expect(dialog().find('input[type="search"]').exists()).toBe(true)
    expect(dialog().findAll('input[type="checkbox"]')).toHaveLength(2)
    await dialog().get('input[type="search"]').setValue('Alpha')
    await dialog().findAll('input[type="checkbox"]')[0]!.setValue(true)
    expect(wrapper.emitted('apply')).toBeUndefined()
    await dialog().get('[data-testid="picker-apply"]').trigger('click')
    expect(wrapper.emitted('apply')).toEqual([[['Alpha']]])
    await wrapper.setProps({ open: false }); await wrapper.setProps({ open: true }); await flushPromises()
    expect(dialog().get('[data-testid="picker-apply"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('parses pasted real newlines and commas, preserves exact case, and skips existing IDs', async () => {
    const { wrapper, dialog } = await picker()
    await dialog().findAll('button').find(button => button.text() === 'channelMonitorV2.settings.picker.bulk')!.trigger('click')
    expect(dialog().find('input[type="search"]').exists()).toBe(false)
    await dialog().get('textarea').setValue(' gpt-render\nNOVA\r\nNOVA，nova, existing, __other__ ')
    await dialog().get('[data-testid="picker-apply"]').trigger('click')
    expect(wrapper.emitted('apply')).toEqual([[['NOVA', 'gpt-render', 'nova']]])
    wrapper.unmount()
  })

  it('traps keyboard focus, cancels with Escape or outside click, and restores focus to the trigger', async () => {
    const { wrapper, anchor, dialog } = await picker()
    const first = dialog().findAll('button')[0]!.element as HTMLElement
    const last = dialog().get('[data-testid="picker-apply"]').element as HTMLButtonElement
    await dialog().findAll('input[type="checkbox"]')[0]!.setValue(true)
    last.focus()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
    expect(document.activeElement).toBe(first)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.emitted('apply')).toBeUndefined()
    await wrapper.setProps({ open: false }); await flushPromises()
    expect(document.activeElement).toBe(anchor)
    await wrapper.setProps({ open: true }); await flushPromises()
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    expect(wrapper.emitted('close')).toHaveLength(2)
    wrapper.unmount()
  })
})
