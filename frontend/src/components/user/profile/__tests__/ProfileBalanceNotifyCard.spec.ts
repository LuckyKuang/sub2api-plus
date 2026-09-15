import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ProfileBalanceNotifyCard from '../ProfileBalanceNotifyCard.vue'

const {
  updateProfile,
  toggleNotifyEmail,
  sendNotifyEmailCode,
  verifyNotifyEmail,
  getProfile,
  showError,
  showSuccess,
  auth,
} = vi.hoisted(() => ({
  updateProfile: vi.fn(),
  toggleNotifyEmail: vi.fn(),
  sendNotifyEmailCode: vi.fn(),
  verifyNotifyEmail: vi.fn(),
  getProfile: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  auth: { user: null as unknown },
}))

vi.mock('@/api', () => ({
  userAPI: { updateProfile, toggleNotifyEmail, sendNotifyEmailCode, verifyNotifyEmail, getProfile },
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

enableAutoUnmount(afterEach)

const props = {
  enabled: false, threshold: null, extraEmails: [], systemDefaultThreshold: 10, userEmail: '',
}

const deferred = () => {
  let resolve!: () => void
  const promise = new Promise<void>((done) => { resolve = done })
  return { promise, resolve }
}

const pendingRows = (wrapper: VueWrapper) => wrapper.findAll('.bg-yellow-50')
const pendingEmails = (wrapper: VueWrapper) => pendingRows(wrapper).map(row => row.get('span').text())

const button = (wrapper: VueWrapper, text: string) =>
  wrapper.findAll('button').find(item => item.text() === text)!

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
  })

  it('restores the previous state when saving fails', async () => {
    updateProfile.mockRejectedValueOnce(new Error('save failed'))
    const wrapper = mount(ProfileBalanceNotifyCard, { props })
    await wrapper.get('[role="switch"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    expect(showError).toHaveBeenCalledOnce()
  })

  it('keeps an email enabled until the server accepts disabling it', async () => {
    const entry = { email: 'notify@example.test', verified: true, disabled: false }
    let resolveSave!: (value: unknown) => void
    toggleNotifyEmail.mockReturnValue(new Promise(resolve => { resolveSave = resolve }))
    const wrapper = mount(ProfileBalanceNotifyCard, { props: { ...props, enabled: true, extraEmails: [entry] } })
    const toggle = wrapper.get('[aria-label="notify@example.test"]')
    await toggle.trigger('click')
    expect(toggleNotifyEmail).toHaveBeenCalledExactlyOnceWith(entry.email, true)
    expect(toggle.attributes('aria-checked')).toBe('true')
    resolveSave({ balance_notify_extra_emails: [{ ...entry, disabled: true }] })
    await flushPromises()
    expect(toggle.attributes('aria-checked')).toBe('false')
  })
})

describe('ProfileBalanceNotifyCard', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.resetAllMocks()
    sendNotifyEmailCode.mockResolvedValue({})
    getProfile.mockResolvedValue({ balance_notify_extra_emails: [] })
    auth.user = null
  })

  afterEach(() => {
    vi.clearAllTimers()
    vi.useRealTimers()
  })

  it.each([0, 1])('removes only verified emails when request %i finishes first', async (first) => {
    const emails = ['first@example.com', 'second@example.com', 'third@example.com']
    const requests = [deferred(), deferred()]
    verifyNotifyEmail.mockImplementation((email: string) => requests[emails.indexOf(email)]!.promise)
    const wrapper = mount(ProfileBalanceNotifyCard, {
      props: { enabled: true, threshold: null, extraEmails: [], systemDefaultThreshold: 5, userEmail: '' }
    })

    for (const email of emails) {
      await wrapper.get('input[type="email"]').setValue(email)
      await button(wrapper, 'common.add').trigger('click')
    }
    for (const row of pendingRows(wrapper)) {
      await row.findAll('button').find(item => item.text() === 'profile.balanceNotify.sendCode')!.trigger('click')
    }
    await flushPromises()
    for (const row of pendingRows(wrapper)) {
      await row.get('input').setValue('123456')
    }
    for (const row of pendingRows(wrapper).slice(0, 2)) {
      await row.findAll('button').find(item => item.text() === 'profile.balanceNotify.verify')!.trigger('click')
    }
    expect(verifyNotifyEmail.mock.calls).toEqual(emails.slice(0, 2).map(email => [email, '123456']))

    requests[first]!.resolve()
    await flushPromises()
    expect(pendingEmails(wrapper)).toEqual(emails.filter((_, index) => index !== first))

    requests[1 - first]!.resolve()
    await flushPromises()
    expect(pendingEmails(wrapper)).toEqual([emails[2]])
    expect((pendingRows(wrapper)[0]!.get('input').element as HTMLInputElement).value).toBe('123456')
    expect(getProfile).toHaveBeenCalledTimes(2)
  })
})
