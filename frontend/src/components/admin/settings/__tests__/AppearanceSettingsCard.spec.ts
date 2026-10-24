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
const published = { ...defaultConsoleAppearance(), background_url: '/api/v1/settings/console-background/' + 'a'.repeat(64), card_transparency: 45 }

// Requirements: docs/UI_INTERACTION.md. Draft changes and reset are local;
// explicit save alone publishes, failures retain the draft, navigation discards
// preview. Accent color and 0–100% background transparency are independently
// editable and resettable; text/outline transparency is owned by shared styles.
describe('appearance settings draft and publication', () => {
  it('starts with the current green accent and 0% transparency, without publishing a draft', async () => {
    const wrapper = mount(AppearanceSettingsCard, { props: { active: true } })
    await flushPromises()
    expect(wrapper.find('input[type=checkbox]').exists()).toBe(false)
    expect(wrapper.get<HTMLInputElement>('#console-appearance-accent').element.value).toBe('#14b8a6')
    expect(wrapper.get<HTMLInputElement>('#console-appearance-accent-hex').element.value).toBe('#14B8A6')
    expect(wrapper.get<HTMLInputElement>('#console-appearance-transparency').element.value).toBe('0')
    expect(wrapper.get<HTMLInputElement>('#console-appearance-transparency-number').element.value).toBe('0')
    expect(wrapper.findAll('input[type=range]')).toHaveLength(3)
    expect(wrapper.findAll<HTMLElement>('.appearance-sample-card').map(item => item.element.style.backgroundColor))
      .toEqual(['rgb(248, 250, 249)', 'rgb(38, 44, 45)'])
    expect(button(wrapper, 'save').attributes('disabled')).toBeDefined()
    expect(mocks.store.consoleAppearancePreview).toBeNull()
  })

  it('previews uploads and masks locally, then publishes the current appearance values', async () => {
    const wrapper = mount(AppearanceSettingsCard, { props: { active: true } })
    await flushPromises()
    const file = new File(['local'], 'wallpaper.png', { type: 'image/png' })
    const input = wrapper.get('input[type=file]')
    Object.defineProperty(input.element, 'files', { value: [file] })
    await input.trigger('change')
    await flushPromises()
    expect(mocks.prepare).toHaveBeenCalledWith(file)
    expect(mocks.store.consoleAppearancePreview).toMatchObject({ accent_color: '#14b8a6', card_transparency: 0, background_url: 'blob:' + location.origin + '/preview-image' })
    expect(mocks.save).not.toHaveBeenCalled()
    await wrapper.get('#console-appearance-light_mask').setValue('20')
    expect(mocks.store.consoleAppearancePreview).toMatchObject({ card_transparency: 0, light_mask: 20 })
    mocks.save.mockResolvedValue({ ...published, card_transparency: 0, light_mask: 20 })
    await button(wrapper, 'save').trigger('click'); await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith({ background_image: 'data:image/jpeg;base64,Y29tcHJlc3NlZA==', light_mask: 20, dark_mask: 48, accent_color: '#14b8a6', card_transparency: 0 })
    expect(mocks.store.applyConsoleAppearance).toHaveBeenCalledWith({ ...published, card_transparency: 0, light_mask: 20 })
    expect(mocks.store.consoleAppearancePreview).toBeNull()
    expect(URL.revokeObjectURL).toHaveBeenCalled()
  })

  it.each([0, 45, 100])('shows and publishes stored transparency %s without changing its value', async cardTransparency => {
    mocks.get.mockResolvedValue({ ...published, card_transparency: cardTransparency })
    const wrapper = mount(AppearanceSettingsCard, { props: { active: true } })
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('#console-appearance-transparency').element.value).toBe(String(cardTransparency))
    expect(wrapper.get<HTMLInputElement>('#console-appearance-transparency-number').element.value).toBe(String(cardTransparency))
    await wrapper.get('#console-appearance-light_mask').setValue('20')
    mocks.save.mockResolvedValue({ ...published, card_transparency: cardTransparency, light_mask: 20 })
    await button(wrapper, 'save').trigger('click'); await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith({ light_mask: 20, dark_mask: 48, accent_color: '#14b8a6', card_transparency: cardTransparency })
  })

  it('keeps a legacy null transparency and missing accent at the default controls until edited', async () => {
    mocks.get.mockResolvedValue({ background_url: '', light_mask: 30, dark_mask: 48, card_transparency: null })
    const wrapper = mount(AppearanceSettingsCard, { props: { active: true } })
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('#console-appearance-accent').element.value).toBe('#14b8a6')
    expect(wrapper.get<HTMLInputElement>('#console-appearance-transparency').element.value).toBe('0')
    expect(button(wrapper, 'save').attributes('disabled')).toBeDefined()
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it('previews color and transparency together, then saves them through the appearance endpoint', async () => {
    const wrapper = mount(AppearanceSettingsCard, { props: { active: true } })
    await flushPromises()
    await wrapper.get('#console-appearance-accent-hex').setValue('#A855F7')
    await wrapper.get('#console-appearance-transparency').setValue('100')
    expect(wrapper.get<HTMLInputElement>('#console-appearance-accent').element.value).toBe('#a855f7')
    expect(wrapper.get<HTMLInputElement>('#console-appearance-transparency-number').element.value).toBe('100')
    expect(mocks.store.consoleAppearancePreview).toMatchObject({ accent_color: '#a855f7', card_transparency: 100 })
    expect(mocks.save).not.toHaveBeenCalled()
    const result = { ...defaultConsoleAppearance(), accent_color: '#a855f7', card_transparency: 100 }
    mocks.save.mockResolvedValue(result)
    await button(wrapper, 'save').trigger('click'); await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith({ light_mask: 30, dark_mask: 48, accent_color: '#a855f7', card_transparency: 100 })
    expect(mocks.store.applyConsoleAppearance).toHaveBeenCalledWith(result)
    expect(button(wrapper, 'save').attributes('disabled')).toBeDefined()
    expect(mocks.store.consoleAppearancePreview).toBeNull()
  })

  it('resets color and transparency independently while retaining the background and masks', async () => {
    mocks.get.mockResolvedValue({ ...published, accent_color: '#f59e0b' })
    const wrapper = mount(AppearanceSettingsCard, { props: { active: true } })
    await flushPromises()
    await wrapper.get('[data-testid="appearance-reset-accent"]').trigger('click')
    expect(mocks.store.consoleAppearancePreview).toEqual({ ...published, accent_color: '#14b8a6' })
    await wrapper.get('[data-testid="appearance-reset-transparency"]').trigger('click')
    expect(mocks.store.consoleAppearancePreview).toEqual({ ...published, accent_color: '#14b8a6', card_transparency: 0 })
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it('retains both new draft values after a failed save and discards them without publishing', async () => {
    mocks.get.mockResolvedValue(published)
    const wrapper = mount(AppearanceSettingsCard, { props: { active: true } })
    await flushPromises()
    await wrapper.get('#console-appearance-accent-hex').setValue('#F59E0B')
    await wrapper.get('#console-appearance-transparency-number').setValue('100')
    mocks.save.mockRejectedValueOnce(new Error('offline'))
    await button(wrapper, 'save').trigger('click'); await flushPromises()
    expect(mocks.store.applyConsoleAppearance).not.toHaveBeenCalled()
    expect(mocks.store.consoleAppearancePreview).toEqual({ ...published, accent_color: '#f59e0b', card_transparency: 100 })
    expect(wrapper.get('[role=alert]').text()).toContain('saveFailed')
    await button(wrapper, 'discard').trigger('click')
    expect(wrapper.get<HTMLInputElement>('#console-appearance-accent-hex').element.value).toBe('#14B8A6')
    expect(wrapper.get<HTMLInputElement>('#console-appearance-transparency-number').element.value).toBe('45')
    expect(mocks.store.consoleAppearancePreview).toBeNull()
    expect(mocks.save).toHaveBeenCalledTimes(1)
  })

  it('prevents invalid HEX values from being previewed or saved and allows correction with a preset', async () => {
    const wrapper = mount(AppearanceSettingsCard, { props: { active: true } })
    await flushPromises()
    await wrapper.get('#console-appearance-accent-hex').setValue('#XYZ123')
    expect(wrapper.get('#console-appearance-accent-hex').attributes('aria-invalid')).toBe('true')
    expect(button(wrapper, 'save').attributes('disabled')).toBeDefined()
    expect(mocks.store.consoleAppearancePreview).toBeNull()
    await button(wrapper, 'save').trigger('click')
    expect(mocks.save).not.toHaveBeenCalled()
    await wrapper.findAll('.appearance-accent-swatch')[1]!.trigger('click')
    expect(wrapper.get('#console-appearance-accent-hex').attributes('aria-invalid')).toBe('false')
    expect(wrapper.get<HTMLInputElement>('#console-appearance-accent-hex').element.value).toBe('#3B82F6')
    expect(mocks.store.consoleAppearancePreview).toMatchObject({ accent_color: '#3b82f6' })
  })

  it('bounds numeric transparency and blocks Enter from submitting the surrounding settings form', async () => {
    const wrapper = mount(AppearanceSettingsCard, { props: { active: true } })
    await flushPromises()
    const input = wrapper.get<HTMLInputElement>('#console-appearance-transparency-number')
    await input.setValue('125')
    expect(wrapper.get<HTMLInputElement>('#console-appearance-transparency').element.value).toBe('100')
    expect(mocks.store.consoleAppearancePreview).toMatchObject({ card_transparency: 100 })
    await input.setValue('-10')
    expect(mocks.store.consoleAppearancePreview).toMatchObject({ card_transparency: 0 })
    for (const field of ['#console-appearance-accent-hex', '#console-appearance-transparency-number']) {
      const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
      wrapper.get(field).element.dispatchEvent(event)
      expect(event.defaultPrevented).toBe(true)
    }
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it('keeps restore-defaults as a draft until Save and retains it after a failed save', async () => {
    mocks.get.mockResolvedValue(published)
    const wrapper = mount(AppearanceSettingsCard, { props: { active: true } })
    await flushPromises()
    const restored = { ...defaultConsoleAppearance(), card_transparency: 45 }
    await wrapper.get('[data-testid="appearance-reset-background"]').trigger('click')
    expect(mocks.store.consoleAppearancePreview).toEqual(restored)
    expect(mocks.save).not.toHaveBeenCalled()
    mocks.save.mockRejectedValueOnce(new Error('offline'))
    await button(wrapper, 'save').trigger('click'); await flushPromises()
    expect(wrapper.get('[role=alert]').text()).toContain('saveFailed')
    expect(mocks.store.applyConsoleAppearance).not.toHaveBeenCalled()
    expect(mocks.store.consoleAppearancePreview).toEqual(restored)
    mocks.save.mockResolvedValue(restored)
    await button(wrapper, 'save').trigger('click'); await flushPromises()
    expect(mocks.save).toHaveBeenLastCalledWith({ background_image: '', light_mask: 30, dark_mask: 48, accent_color: '#14b8a6', card_transparency: 45 })
    expect(mocks.store.applyConsoleAppearance).toHaveBeenCalledWith(restored)
  })

  it('ends preview on tab navigation and discards edits without publishing', async () => {
    mocks.get.mockResolvedValue(published)
    const wrapper = mount(AppearanceSettingsCard, { props: { active: true } })
    await flushPromises()
    await wrapper.get('#console-appearance-light_mask').setValue('70')
    await wrapper.get('#console-appearance-accent-hex').setValue('#F59E0B')
    await wrapper.get('#console-appearance-transparency').setValue('100')
    expect(mocks.store.consoleAppearancePreview).toMatchObject({ light_mask: 70 })
    await wrapper.setProps({ active: false })
    expect(mocks.store.consoleAppearancePreview).toBeNull()
    await wrapper.setProps({ active: true })
    await button(wrapper, 'discard').trigger('click')
    expect(wrapper.get<HTMLInputElement>('#console-appearance-light_mask').element.value).toBe('30')
    expect(wrapper.get<HTMLInputElement>('#console-appearance-accent-hex').element.value).toBe('#14B8A6')
    expect(wrapper.get<HTMLInputElement>('#console-appearance-transparency').element.value).toBe('45')
    expect(mocks.save).not.toHaveBeenCalled()
    await button(wrapper, 'preview').trigger('click')
    wrapper.unmount()
    expect(mocks.store.consoleAppearancePreview).toBeNull()
  })

  it.each([false, true])('does not restart an upload preview after navigation (return before completion: %s)', async returnBeforeCompletion => {
    let finishUpload!: (result: { blob: Blob; dataURL: string }) => void
    mocks.prepare.mockImplementationOnce(() => new Promise(resolve => { finishUpload = resolve }))
    const wrapper = mount(AppearanceSettingsCard, { props: { active: true } })
    await flushPromises()
    const input = wrapper.get('input[type=file]')
    Object.defineProperty(input.element, 'files', { value: [new File(['local'], 'wallpaper.png', { type: 'image/png' })] })
    await input.trigger('change')
    await wrapper.setProps({ active: false })
    if (returnBeforeCompletion) await wrapper.setProps({ active: true })
    finishUpload({ blob: new Blob(['compressed'], { type: 'image/jpeg' }), dataURL: 'data:image/jpeg;base64,Y29tcHJlc3NlZA==' })
    await flushPromises()
    expect(mocks.store.consoleAppearancePreview).toBeNull()
    await wrapper.setProps({ active: true })
    expect(mocks.store.consoleAppearancePreview).toBeNull()
    expect(mocks.save).not.toHaveBeenCalled()
    expect(button(wrapper, 'save').attributes('disabled')).toBeUndefined()
    await button(wrapper, 'preview').trigger('click')
    expect(mocks.store.consoleAppearancePreview).toMatchObject({ background_url: 'blob:' + location.origin + '/preview-image' })
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
