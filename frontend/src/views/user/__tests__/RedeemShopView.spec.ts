import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import RedeemShopView from '../RedeemShopView.vue'
const getCheckoutInfo = vi.hoisted(() => vi.fn())
vi.mock('@/api/payment', () => ({ paymentAPI: { getCheckoutInfo } }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)
beforeEach(() => vi.clearAllMocks())
const render = () => mount(RedeemShopView, { global: { stubs: {
  AppLayout: { template: '<main><slot /></main>' },
  RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' }
} } })
describe('embedded redeem purchase', () => {
  it('embeds the saved URL without site credentials and offers explicit navigation', async () => {
    const url = 'https://shop.example/code?product=one#buy'
    getCheckoutInfo.mockResolvedValue({ data: { redeem_purchase_enabled: true, redeem_purchase_mode: 'iframe', redeem_purchase_url: url } })
    const wrapper = render(); await flushPromises()
    expect(wrapper.get('iframe').attributes('src')).toBe(url)
    expect(wrapper.get('iframe').attributes('referrerpolicy')).toBe('no-referrer')
    // Approved embedding contract: checkout scripts/forms and payment popups
    // work, but the third-party frame cannot navigate the enclosing site.
    expect(wrapper.get('iframe').attributes('sandbox').split(' ').sort()).toEqual([
      'allow-forms', 'allow-popups', 'allow-popups-to-escape-sandbox', 'allow-same-origin', 'allow-scripts'
    ])
    const external = wrapper.get('a[target="_blank"]')
    expect(external.attributes('href')).toBe(url)
    expect(external.attributes('rel')).toBe('noopener noreferrer')
    expect(wrapper.get('a[href="/redeem"]').exists()).toBe(true)
    expect(wrapper.get('a[href="/purchase"]').exists()).toBe(true)
  })
  it.each([
    { redeem_purchase_enabled: false, redeem_purchase_mode: 'iframe', redeem_purchase_url: 'https://shop.example' },
    { redeem_purchase_enabled: true, redeem_purchase_mode: 'new_tab', redeem_purchase_url: 'https://shop.example' },
  ])('does not embed when the entry or iframe mode is disabled', async data => {
    getCheckoutInfo.mockResolvedValue({ data })
    const wrapper = render(); await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.find('a[target="_blank"]').exists()).toBe(false)
    expect(wrapper.get('[role="alert"]').text()).toContain('purchaseUnavailable')
  })
})
