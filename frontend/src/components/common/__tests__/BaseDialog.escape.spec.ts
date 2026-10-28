import { afterEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import BaseDialog from '../BaseDialog.vue'

enableAutoUnmount(afterEach)
const pressEscape = () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))

describe('stacked dialog Escape handling', () => {
  // docs/UI_INTERACTION.md: a nested calendar/menu consumes the first Escape;
  // its containing dialog stays open even if the child has already unmounted.
  it('leaves the enclosing dialog open when a child already consumed Escape', () => {
    const parent = mount(BaseDialog, { props: { show: true, title: 'Parent' } })
    const event = new KeyboardEvent('keydown', { key: 'Escape', cancelable: true })
    event.preventDefault()
    document.dispatchEvent(event)
    expect(parent.emitted('close')).toBeUndefined()
    pressEscape()
    expect(parent.emitted('close')).toHaveLength(1)
  })

  it('closes only the most recently opened dialog, then allows the parent to close', async () => {
    const parent = mount(BaseDialog, { props: { show: true, title: 'Parent' } })
    const child = mount(BaseDialog, { props: { show: true, title: 'Child' } })
    pressEscape()
    expect(child.emitted('close')).toHaveLength(1)
    expect(parent.emitted('close')).toBeUndefined()
    await child.setProps({ show: false })
    pressEscape()
    expect(parent.emitted('close')).toHaveLength(1)
  })

  it('does not dismiss a parent behind a child that disallows Escape', () => {
    const parent = mount(BaseDialog, { props: { show: true, title: 'Parent' } })
    const child = mount(BaseDialog, { props: { show: true, title: 'Child', closeOnEscape: false } })
    pressEscape()
    expect(child.emitted('close')).toBeUndefined()
    expect(parent.emitted('close')).toBeUndefined()
    child.unmount()
    pressEscape()
    expect(parent.emitted('close')).toHaveLength(1)
  })
})
