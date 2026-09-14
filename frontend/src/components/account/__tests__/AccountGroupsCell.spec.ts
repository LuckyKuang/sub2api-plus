import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import AccountGroupsCell from '../AccountGroupsCell.vue'

const groups = Array.from({ length: 6 }, (_, index) => ({
  id: index + 1,
  name: `Group ${index + 1}`,
  platform: 'openai' as const,
  subscription_type: 'standard' as const
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: { count?: number }) => `${key}:${params?.count ?? ''}`
    })
  }
})

afterEach(() => {
  document.body.innerHTML = ''
})

describe('AccountGroupsCell', () => {
  it('keeps the summary to two lines and exposes every group from the overflow button', async () => {
    const wrapper = mount(AccountGroupsCell, {
      props: { groups },
      global: {
        stubs: {
          GroupBadge: {
            props: ['name'],
            template: '<span data-test="group-badge">{{ name }}</span>'
          },
          Icon: true
        }
      }
    })

    const summary = wrapper.get('[data-test="group-summary"]')
    expect(summary.classes()).toEqual(expect.arrayContaining(['max-h-14', 'overflow-hidden']))
    expect(summary.findAll('[data-test="group-badge"]')).toHaveLength(3)
    expect(summary.text()).toContain('+3')

    await summary.get('button').trigger('click')

    const popover = document.body.querySelector('[role="dialog"]')
    expect(popover).not.toBeNull()
    expect(popover?.querySelectorAll('[data-test="group-badge"]')).toHaveLength(groups.length)
    expect(popover?.textContent).toContain(groups.at(-1)?.name)
    wrapper.unmount()
  })

  it('renders the empty placeholder when the account has no groups', () => {
    const wrapper = mount(AccountGroupsCell, { props: { groups: [] } })

    expect(wrapper.text()).toBe('-')
  })
})
