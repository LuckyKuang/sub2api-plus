import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import Pagination from '../Pagination.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

function mountPagination() {
  return mount(Pagination, {
    props: { total: 200, page: 1, pageSize: 20, showJump: true, showPageSizeSelector: false },
    global: { stubs: { Icon: true } }
  })
}

describe('pagination jump input', () => {
  // Approved user audit keeps pagination visible even when no records match.
  it('keeps an empty list on page one with both directions disabled', async () => {
    const wrapper = mountPagination()
    await wrapper.setProps({ total: 0 })
    const directions = wrapper.findAll('button').filter(button =>
      button.attributes('aria-label') === 'pagination.previous' || button.attributes('aria-label') === 'pagination.next'
    )
    expect(directions).toHaveLength(2)
    expect(directions.every(button => (button.element as HTMLButtonElement).disabled)).toBe(true)
    const current = wrapper.get('button[aria-current="page"]')
    expect(current.text()).toBe('1')
    await wrapper.get('input').setValue('9')
    await wrapper.get('.btn').trigger('click')
    expect(wrapper.emitted('update:page')).toBeUndefined()
  })

  it.each(['click', 'enter'])('jumps to the entered numeric page using %s', async (action) => {
    const wrapper = mountPagination()
    const input = wrapper.get('input[type="number"]')
    await input.setValue('3')
    if (action === 'click') await wrapper.get('.btn').trigger('click')
    else await input.trigger('keyup', { key: 'Enter' })
    expect(wrapper.emitted('update:page')).toEqual([[3]])
    expect((input.element as HTMLInputElement).value).toBe('')
  })

  it('clamps an entered page to the last page', async () => {
    const wrapper = mountPagination()
    await wrapper.get('input').setValue('99')
    await wrapper.get('.btn').trigger('click')
    expect(wrapper.emitted('update:page')).toEqual([[10]])
  })

  it('ignores an empty jump input', async () => {
    const wrapper = mountPagination()
    await wrapper.get('.btn').trigger('click')
    expect(wrapper.emitted('update:page')).toBeUndefined()
  })
})
