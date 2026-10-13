import { flushPromises, mount } from '@vue/test-utils'
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

vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: { value: 'zh' } }) }))

const reload = vi.fn()

const mountSwitcher = () =>
  mount(LocaleSwitcher, { global: { stubs: { Icon: true, transition: false } } })

const choose = async (wrapper: ReturnType<typeof mountSwitcher>, code: string) => {
  await wrapper.get('select').setValue(code)
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
  it('switches the native selector without reloading the page', async () => {
    const wrapper = mountSwitcher()

    await choose(wrapper, 'en')

    expect(setLocale).toHaveBeenCalledWith('en')
    expect(reload).not.toHaveBeenCalled()
    expect(wrapper.find('button').exists()).toBe(false)
  })

  it('does nothing when the current language is chosen again', async () => {
    const wrapper = mountSwitcher()

    await choose(wrapper, 'zh')

    expect(setLocale).not.toHaveBeenCalled()
    expect(reload).not.toHaveBeenCalled()
  })
})
