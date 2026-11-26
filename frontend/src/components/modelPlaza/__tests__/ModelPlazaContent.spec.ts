import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, shallowMount } from '@vue/test-utils'
import { nextTick } from 'vue'
import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'
import ModelPlazaContent from '../ModelPlazaContent.vue'
import PlazaFilterBar from '../PlazaFilterBar.vue'
import PlazaModelCard from '../PlazaModelCard.vue'
import PlazaModelDetail from '../PlazaModelDetail.vue'
import PlazaGroupSection from '../PlazaGroupSection.vue'

vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAuthenticated: true }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)

function model(name: string, platform = 'openai'): PlazaModel {
  return { name, platform, pricing: null, official_pricing: null }
}

function group(id: number, name: string, models: PlazaModel[], overrides: Partial<ModelPlazaGroup> = {}): ModelPlazaGroup {
  return {
    id, name, models, description: '', platform: 'openai', rate_multiplier: 1,
    subscription_type: 'standard', is_exclusive: false, peak_rate_enabled: false,
    peak_start: '', peak_end: '', peak_rate_multiplier: 1, long_context_pricing_enabled: true,
    image_rate_independent: false, image_rate_multiplier: 1, video_rate_independent: false,
    video_rate_multiplier: 1, ...overrides
  }
}

const plus = group(1, 'Plus', [model('provider/Mix'), model('plus-only')], { user_rate_multiplier: 0.2 })
const pro = group(2, 'Pro', [model('provider/Mix'), model('pro-only')], { rate_multiplier: 0.5 })
const composite = group(3, 'Composite', [model('provider/Mix', 'anthropic')], { platform: 'composite' })

function catalog() {
  return shallowMount(ModelPlazaContent, {
    props: { loading: false, embedded: true, response: { description: '', groups: [pro, composite, plus] } }
  })
}

describe('Model Plaza browsing', () => {
  it('quotes one visible group per exact platform/model identity and compares every authorized offer', async () => {
    const page = catalog()
    const cards = page.findAllComponents(PlazaModelCard)
    expect(cards).toHaveLength(4)
    const shared = cards.filter(card => card.props('model').name === 'provider/Mix')
    expect(shared.map(card => [card.props('group').name, card.props('groupCount')]))
      .toEqual([['Plus', 2], ['Composite', 1]])

    const filters = page.getComponent(PlazaFilterBar)
    filters.vm.$emit('update:groupId', pro.id)
    filters.vm.$emit('update:search', '  MIX  ')
    await nextTick()
    const [card] = page.findAllComponents(PlazaModelCard)
    expect(page.findAllComponents(PlazaModelCard)).toHaveLength(1)
    expect(card.props('group').name).toBe('Pro')
    card.vm.$emit('open')
    await nextTick()
    const detail = page.getComponent(PlazaModelDetail)
    expect(detail.props('group').name).toBe('Pro')
    expect(detail.props('offers').map(offer => offer.group.name)).toEqual(['Plus', 'Pro'])
    detail.vm.$emit('close')
    filters.vm.$emit('update:view', 'table')
    await nextTick()
    const section = page.getComponent(PlazaGroupSection)
    expect(section.props('group').models.map(m => m.name)).toEqual(['provider/Mix'])
    section.vm.$emit('open', pro.models[0])
    await nextTick()
    expect(page.getComponent(PlazaModelDetail).props('model').name).toBe('provider/Mix')
  })

  it('retains the group through repricing and drops details when that group loses visibility', async () => {
    const page = catalog()
    const filters = page.getComponent(PlazaFilterBar)
    filters.vm.$emit('update:groupId', plus.id)
    filters.vm.$emit('update:rate', 0.2)
    await nextTick()
    page.getComponent(PlazaModelCard).vm.$emit('open')
    await nextTick()
    await page.setProps({ response: { description: '', groups: [{ ...plus, user_rate_multiplier: 0.4 }, pro] } })
    expect(filters.props('groupId')).toBe(1)
    expect(filters.props('rate')).toBe('all')
    expect(page.getComponent(PlazaModelDetail).props('group').user_rate_multiplier).toBe(0.4)
    await page.setProps({ response: { description: '', groups: [pro] } })
    expect(page.findComponent(PlazaModelDetail).exists()).toBe(false)
    expect(filters.props('groupId')).toBe('all')
    expect(page.getComponent(PlazaModelCard).props('groupCount')).toBe(1)
  })

  it('can clear an empty search and preserves the configured Markdown notes', async () => {
    const page = catalog()
    await page.setProps({ response: { description: '**Some pricing notes**', groups: [plus] } })
    expect(page.get('details strong').text()).toBe('Some pricing notes')
    const filters = page.getComponent(PlazaFilterBar)
    filters.vm.$emit('update:search', 'absent')
    await nextTick()
    expect(page.get('[role="status"]').text()).toBe('modelPlaza.noSearchResult')
    await page.get('button').trigger('click')
    expect(filters.props('search')).toBe('')
    expect(page.findAllComponents(PlazaModelCard)).toHaveLength(2)
  })
})
