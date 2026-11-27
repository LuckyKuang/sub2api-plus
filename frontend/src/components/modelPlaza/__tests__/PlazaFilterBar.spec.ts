import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { chooseSelect } from '@/__tests__/helpers/select'
import PlazaFilterBar from '../PlazaFilterBar.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, args?: { filter: string }) => args?.filter ?? key
  })
}))
enableAutoUnmount(afterEach)

const defaults = {
  platforms: ['openai', 'anthropic'],
  groups: [
    { id: 4, name: 'Codex Plus', platform: 'openai', rate: 0.3 },
    { id: 7, name: 'Codex Pro', platform: 'openai', rate: 0.5 },
    { id: 9, name: 'Claude', platform: 'anthropic', rate: 1 }
  ],
  rates: [0.3, 0.5, 1],
  platform: 'all',
  rate: 'all' as const,
  groupId: 'all' as const,
  search: '',
  view: 'cards' as const
}

describe('model plaza browsing controls', () => {
  it('keeps search and group selection available with advanced filters closed', async () => {
    const w = mount(PlazaFilterBar, { props: defaults })
    expect(w.get('[aria-label="modelPlaza.filters.platformLabel"]').isVisible()).toBe(false)
    expect(w.get('[aria-label="modelPlaza.filters.rateLabel"]').isVisible()).toBe(false)
    await w.get('[role="searchbox"]').setValue('gpt-6')
    expect(w.emitted('update:search')).toEqual([['gpt-6']])
    await chooseSelect(w.get('[aria-label="modelPlaza.filters.groupLabel"]'), 'Codex Pro')
    expect(w.emitted('update:groupId')).toEqual([[7]])

    await w.get('[aria-label="modelPlaza.filters.table"]').trigger('click')
    expect(w.emitted('update:view')).toEqual([['table']])
    expect(w.emitted('update:search')).toHaveLength(1)
    expect(w.emitted('update:groupId')).toHaveLength(1)
  })

  it('prevents incompatible choices and lets users remove a hidden filter', async () => {
    const w = mount(PlazaFilterBar, { props: { ...defaults, platform: 'openai', rate: 0.3 } })
    const toggle = w.get('button[aria-controls][aria-expanded]')
    await toggle.trigger('click')
    const group = w.get('[aria-label="modelPlaza.filters.groupLabel"]')
    await chooseSelect(group, 'Codex Pro')
    await chooseSelect(group, 'Claude')
    expect(w.emitted('update:groupId')).toBeUndefined()
    await chooseSelect(group, 'Codex Plus')
    expect(w.emitted('update:groupId')).toEqual([[4]])

    const rate = w.get('[aria-label="modelPlaza.filters.rateLabel"]')
    await chooseSelect(rate, '1×')
    expect(w.emitted('update:rate')).toBeUndefined()
    await chooseSelect(rate, '0.5×')
    expect(w.emitted('update:rate')).toEqual([[0.5]])

    await toggle.trigger('click')
    expect(rate.isVisible()).toBe(false)
    await w.get('button[aria-label="0.3×"]').trigger('click')
    await w.get('button[aria-label="OpenAI"]').trigger('click')
    expect(w.emitted('update:rate')?.at(-1)).toEqual(['all'])
    expect(w.emitted('update:platform')).toEqual([['all']])
  })
})
