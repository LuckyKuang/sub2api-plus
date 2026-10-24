import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AppearanceSettingsCard from '../AppearanceSettingsCard.vue'
import { defaultConsoleAppearance } from '@/types/appearance'

const mocks = vi.hoisted(() => ({
  get: vi.fn(), save: vi.fn(), prepare: vi.fn(),
  store: { consoleAppearancePreview: null as unknown, applyConsoleAppearance: vi.fn(), showSuccess: vi.fn() }
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/appearance', () => ({ getConsoleAppearance: mocks.get, saveConsoleAppearance: mocks.save }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks.store }))
vi.mock('@/utils/consoleBackground', () => ({ prepareConsoleBackground: mocks.prepare, BackgroundImageError: class extends Error {} }))
enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.clearAllMocks()
  mocks.store.consoleAppearancePreview = null
  mocks.get.mockResolvedValue(defaultConsoleAppearance())
  mocks.prepare.mockResolvedValue({ blob: new Blob(['compressed'], { type: 'image/jpeg' }), dataURL: 'data:image/jpeg;base64,Y29tcHJlc3NlZA==' })
  vi.stubGlobal('URL', class extends URL {
    static createObjectURL = vi.fn(() => 'blob:' + location.origin + '/preview-image')
    static revokeObjectURL = vi.fn()
  })
})
afterEach(() => vi.unstubAllGlobals())
const button = (wrapper: VueWrapper, suffix: string) => wrapper.findAll('button').find(item => item.text() === 'admin.settings.appearance.' + suffix)!
const published = { background_url: '/api/v1/settings/console-background/' + 'a'.repeat(64), light_mask: 30, dark_mask: 48, card_transparency: 45 }

// Requirements: docs/UI_INTERACTION.md. Draft changes and reset are local;
// explicit save alone publishes, failures retain the draft, navigation discards
// preview, and existing transparency values never affect solid content surfaces.
describe('appearance settings draft and publication', () => {
  it('shows solid previews and canvas-mask controls without editable content transparency', async () => {
    const wrapper = mount(AppearanceSettingsCard, { props: { active: true } })
    await flushPromises()
    expect(wrapper.find('input[type=checkbox]').exists()).toBe(false)
    expect(wrapper.find('#console-appearance-transparency').exists()).toBe(false)
    expect(wrapper.findAll('input[type=range]')).toHaveLength(2)
    expect(wrapper.findAll<HTMLElement>('.appearance-sample-card').map(item => item.element.style.backgroundColor))
      .toEqual(['rgb(248, 250, 249)', 'rgb(38, 44, 45)'])
    expect(button(wrapper, 'save').attributes('disabled')).toBeDefined()
    expect(mocks.store.consoleAppearancePreview).toBeNull()
  })

  it('previews uploads and masks locally, then publishes them without assigning content transparency', async () => {
    const wrapper = mount(AppearanceSettingsCard, { props: { active: true } })
    await flushPromises()
    const file = new File(['local'], 'wallpaper.png', { type: 'image/png' })
    const input = wrapper.get('input[type=file]')
    Object.defineProperty(input.element, 'files', { value: [file] })
    await input.trigger('change')
    await flushPromises()
    expect(mocks.prepare).toHaveBeenCalledWith(file)
    expect(mocks.store.consoleAppearancePreview).toMatchObject({ card_transparency: null, background_url: 'blob:' + location.origin + '/preview-image' })
    expect(mocks.save).not.toHaveBeenCalled()
    await wrapper.get('#console-appearance-light_mask').setValue('20')
    expect(mocks.store.consoleAppearancePreview).toMatchObject({ card_transparency: null, light_mask: 20 })
    mocks.save.mockResolvedValue({ ...published, card_transparency: null, light_mask: 20 })
    await button(wrapper, 'save').trigger('click'); await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith({ background_image: 'data:image/jpeg;base64,Y29tcHJlc3NlZA==', light_mask: 20, dark_mask: 48, card_transparency: null })
    expect(mocks.store.applyConsoleAppearance).toHaveBeenCalledWith({ ...published, card_transparency: null, light_mask: 20 })
    expect(mocks.store.consoleAppearancePreview).toBeNull()
    expect(URL.revokeObjectURL).toHaveBeenCalled()
  })

  it.each([0, 45, 100])('keeps previews solid and round-trips stored transparency %s without exposing an edit', async cardTransparency => {
    mocks.get.mockResolvedValue({ ...published, card_transparency: cardTransparency })
    const wrapper = mount(AppearanceSettingsCard, { props: { active: true } })
    await flushPromises()
    expect(wrapper.find('#console-appearance-transparency').exists()).toBe(false)
    expect(wrapper.findAll<HTMLElement>('.appearance-sample-card').map(item => item.element.style.backgroundColor))
      .toEqual(['rgb(248, 250, 249)', 'rgb(38, 44, 45)'])
    await wrapper.get('#console-appearance-light_mask').setValue('20')
    mocks.save.mockResolvedValue({ ...published, card_transparency: cardTransparency, light_mask: 20 })
    await button(wrapper, 'save').trigger('click'); await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith({ light_mask: 20, dark_mask: 48, card_transparency: cardTransparency })
  })

  it('keeps restore-defaults as a draft until Save and retains it after a failed save', async () => {
    mocks.get.mockResolvedValue(published)
    const wrapper = mount(AppearanceSettingsCard, { props: { active: true } })
    await flushPromises()
    const restored = { ...defaultConsoleAppearance(), card_transparency: 45 }
    await button(wrapper, 'restore').trigger('click')
    expect(mocks.store.consoleAppearancePreview).toEqual(restored)
    expect(mocks.save).not.toHaveBeenCalled()
    mocks.save.mockRejectedValueOnce(new Error('offline'))
    await button(wrapper, 'save').trigger('click'); await flushPromises()
    expect(wrapper.get('[role=alert]').text()).toContain('saveFailed')
    expect(mocks.store.applyConsoleAppearance).not.toHaveBeenCalled()
    expect(mocks.store.consoleAppearancePreview).toEqual(restored)
    mocks.save.mockResolvedValue(restored)
    await button(wrapper, 'save').trigger('click'); await flushPromises()
    expect(mocks.save).toHaveBeenLastCalledWith({ background_image: '', light_mask: 30, dark_mask: 48, card_transparency: 45 })
    expect(mocks.store.applyConsoleAppearance).toHaveBeenCalledWith(restored)
  })

  it('ends preview on tab navigation and discards edits without publishing', async () => {
    mocks.get.mockResolvedValue(published)
    const wrapper = mount(AppearanceSettingsCard, { props: { active: true } })
    await flushPromises()
    await wrapper.get('#console-appearance-light_mask').setValue('70')
    expect(mocks.store.consoleAppearancePreview).toMatchObject({ light_mask: 70 })
    await wrapper.setProps({ active: false })
    expect(mocks.store.consoleAppearancePreview).toBeNull()
    await wrapper.setProps({ active: true })
    await button(wrapper, 'discard').trigger('click')
    expect(wrapper.get<HTMLInputElement>('#console-appearance-light_mask').element.value).toBe('30')
    expect(mocks.save).not.toHaveBeenCalled()
    await button(wrapper, 'preview').trigger('click')
    wrapper.unmount()
    expect(mocks.store.consoleAppearancePreview).toBeNull()
  })

  it('makes a failed load retryable without enabling saves', async () => {
    mocks.get.mockRejectedValueOnce(new Error('offline'))
    const wrapper = mount(AppearanceSettingsCard, { props: { active: true } })
    await flushPromises()
    expect(wrapper.get('[role=alert]').text()).toContain('loadFailed')
    expect(wrapper.find('input[type=range]').exists()).toBe(false)
    await button(wrapper, 'retry').trigger('click'); await flushPromises()
    expect(button(wrapper, 'save').attributes('disabled')).toBeDefined()
  })
})
