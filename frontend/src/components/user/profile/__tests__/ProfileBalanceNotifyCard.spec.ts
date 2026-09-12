import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ProfileBalanceNotifyCard from '../ProfileBalanceNotifyCard.vue'

const { updateProfile, toggleNotifyEmail, showError, auth } = vi.hoisted(() => ({
  updateProfile: vi.fn(),
  toggleNotifyEmail: vi.fn(),
  showError: vi.fn(),
  auth: { user: null as unknown },
}))
vi.mock('@/api', () => ({ userAPI: { updateProfile, toggleNotifyEmail } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

const props = {
  enabled: false, threshold: null, extraEmails: [], systemDefaultThreshold: 10, userEmail: '',
}

describe('balance notification switches', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    auth.user = null
  })

  it.each([false, true])('saves the newly selected state once, starting from %s', async enabled => {
    const updated = { balance_notify_enabled: !enabled }
    updateProfile.mockResolvedValue(updated)
    const wrapper = mount(ProfileBalanceNotifyCard, { props: { ...props, enabled } })
    await wrapper.get('[role="switch"]').trigger('click')
    await flushPromises()
    expect(updateProfile).toHaveBeenCalledExactlyOnceWith({ balance_notify_enabled: !enabled })
    expect(auth.user).toBe(updated)
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe(String(!enabled))
    wrapper.unmount()
  })

  it('restores the previous state when saving fails', async () => {
    updateProfile.mockRejectedValueOnce(new Error('save failed'))
    const wrapper = mount(ProfileBalanceNotifyCard, { props })
    await wrapper.get('[role="switch"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    expect(showError).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('keeps an email enabled until the server accepts disabling it', async () => {
    const entry = { email: 'notify@example.test', verified: true, disabled: false }
    let resolveSave!: (value: unknown) => void
    toggleNotifyEmail.mockReturnValue(new Promise(resolve => { resolveSave = resolve }))
    const wrapper = mount(ProfileBalanceNotifyCard, { props: { ...props, enabled: true, extraEmails: [entry] } })
    const button = wrapper.get('[aria-label="notify@example.test"]')
    await button.trigger('click')
    expect(toggleNotifyEmail).toHaveBeenCalledExactlyOnceWith(entry.email, true)
    expect(button.attributes('aria-checked')).toBe('true')
    resolveSave({ balance_notify_extra_emails: [{ ...entry, disabled: true }] })
    await flushPromises()
    expect(button.attributes('aria-checked')).toBe('false')
    wrapper.unmount()
  })
})
