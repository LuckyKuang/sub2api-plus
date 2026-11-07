import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { describe, expect, it } from 'vitest'
import WorkspaceTabs from '../WorkspaceTabs.vue'

const tabs = [
  { value: 'charts', label: '图表统计', panelId: 'charts-panel' },
  { value: 'unavailable', label: 'Unavailable', disabled: true },
  { value: 'records', label: 'Usage records', panelId: 'records-panel' },
]

describe('workspace navigation', () => {
  it('drops its own card surface when embedded in a parent card', () => {
    // Pages that merge the tabs into their header card must not keep a second
    // rounded surface, while every other caller keeps the standalone card.
    const standalone = mount(WorkspaceTabs, { props: { modelValue: 'charts', tabs, label: 'Usage' } })
    expect(standalone.get('[role=tablist]').classes()).toContain('card')
    expect(standalone.get('[role=tablist]').classes()).toContain('workspace-tabs')
    standalone.unmount()

    const embedded = mount(WorkspaceTabs, { props: { modelValue: 'charts', tabs, label: 'Usage', embedded: true } })
    expect(embedded.get('[role=tablist]').classes()).not.toContain('card')
    expect(embedded.get('[role=tablist]').classes()).toContain('workspace-tabs')
    expect(embedded.get('[role=tablist]').classes()).toContain('workspace-tabs--embedded')
    expect(embedded.get('[role=tablist]').classes()).toContain('workspace-fixed')
    embedded.unmount()
  })

  it('keeps navigation inside a form from submitting or changing state before its owner accepts it', async () => {
    const wrapper = mount(WorkspaceTabs, { props: { modelValue: 'charts', tabs, label: 'Usage' } })
    expect(wrapper.get('[role=tablist]').attributes('aria-label')).toBe('Usage')
    const records = wrapper.findAll('button')[2]
    await records.trigger('click')
    expect(records.attributes('type')).toBe('button')
    expect(wrapper.emitted('update:modelValue')).toEqual([['records']])
    expect(records.attributes('aria-selected')).toBe('false')
    await wrapper.setProps({ modelValue: 'records' })
    expect(records.attributes('aria-selected')).toBe('true')
    expect(records.attributes('tabindex')).toBe('0')
    expect(records.attributes('aria-controls')).toBe('records-panel')
    expect(wrapper.findAll('button')[0].attributes('tabindex')).toBe('-1')
    wrapper.unmount()
  })

  it('supports arrow, Home and End keys, skips disabled tabs and leaves Tab to the browser', async () => {
    const wrapper = mount(WorkspaceTabs, { attachTo: document.body, props: { modelValue: 'charts', tabs, label: 'Usage' } })
    const buttons = wrapper.findAll('button')
    await buttons[0].trigger('keydown', { key: 'ArrowRight' })
    await nextTick()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['records'])
    expect(document.activeElement).toBe(buttons[2].element)
    await buttons[2].trigger('keydown', { key: 'ArrowRight' })
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['charts'])
    await buttons[0].trigger('keydown', { key: 'End' })
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['records'])
    await buttons[2].trigger('keydown', { key: 'Home' })
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['charts'])
    const count = wrapper.emitted('update:modelValue')?.length
    await buttons[0].trigger('keydown', { key: 'Tab' })
    expect(wrapper.emitted('update:modelValue')).toHaveLength(count!)
    wrapper.unmount()
  })
})
