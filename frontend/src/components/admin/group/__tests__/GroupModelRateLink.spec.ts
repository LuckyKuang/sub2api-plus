import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import GroupModelRateLink from '../GroupModelRateLink.vue'
import Select from '@/components/common/Select.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('shared channel pricing group ownership', () => {
  it('requires choosing one bound group and links to that group editor only', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/groups', component: { template: '<div />' } }] })
    await router.push('/admin/groups')
    const w = mount(GroupModelRateLink, { props: { groups: [{ id: 1, name: 'A' }, { id: 2, name: 'B' }] }, global: { plugins: [router] } })
    expect(w.find('a').exists()).toBe(false)
    w.getComponent(Select).vm.$emit('update:modelValue', 2)
    await w.vm.$nextTick()
    expect(w.get('a').attributes('href')).toBe('/admin/groups?edit_group_id=2')
    expect(w.emitted()).toEqual({})
    await w.setProps({ groups: [{ id: 1, name: 'A' }] })
    expect(w.get('a').attributes('href')).toBe('/admin/groups?edit_group_id=1')
  })
})
