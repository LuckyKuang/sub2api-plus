import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { chooseSelect } from '@/__tests__/helpers/select'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import LocaleSwitcher from '../LocaleSwitcher.vue'

const { setLocale } = vi.hoisted(() => ({ setLocale: vi.fn() }))

vi.mock('@/i18n', () => ({
  setLocale,
  availableLocales: [
    { code: 'en', name: 'English', flag: '🇺🇸' },
    { code: 'zh', name: '中文', flag: '🇨🇳' }
  ]
}))

vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: { value: 'zh' }, t: (key: string) => key }) }))

enableAutoUnmount(afterEach)

const reload = vi.fn()

const mountSwitcher = () =>
  mount(LocaleSwitcher, { global: { stubs: { Icon: true, transition: false } } })

const choose = async (wrapper: ReturnType<typeof mountSwitcher>, code: string) => {
  await chooseSelect(wrapper, code === 'en' ? '🇺🇸 English' : '🇨🇳 中文')
  await flushPromises()
}

describe('LocaleSwitcher', () => {
  beforeEach(() => {
    setLocale.mockReset().mockResolvedValue(undefined)
    reload.mockReset()
    vi.stubGlobal('location', { ...window.location, reload })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  // Server-backed read models refresh separately; draft forms stay mounted.
  it('switches language through the shared menu without reloading the page', async () => {
    const wrapper = mountSwitcher()

    await choose(wrapper, 'en')

    expect(setLocale).toHaveBeenCalledWith('en')
    expect(reload).not.toHaveBeenCalled()
    expect(wrapper.get('.select-trigger').attributes('aria-expanded')).toBe('false')
  })

  it('does nothing when the current language is chosen again', async () => {
    const wrapper = mountSwitcher()

    await choose(wrapper, 'zh')

    expect(setLocale).not.toHaveBeenCalled()
    expect(reload).not.toHaveBeenCalled()
  })
})
