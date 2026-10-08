import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import LoginView from '@/views/auth/LoginView.vue'

const { getPublicSettingsMock, pushMock, loginMock, showErrorMock } = vi.hoisted(() => ({
  getPublicSettingsMock: vi.fn(),
  pushMock: vi.fn(),
  loginMock: vi.fn(),
  showErrorMock: vi.fn()
}))

const publicSettings = {
  registration_enabled: true,
  turnstile_enabled: false,
  turnstile_site_key: '',
  tencent_captcha_enabled: false,
  tencent_captcha_app_id: '',
  aliyun_captcha_enabled: false,
  aliyun_captcha_scene_id: '',
  aliyun_captcha_prefix: '',
  linuxdo_oauth_enabled: false,
  dingtalk_oauth_enabled: false,
  wechat_oauth_enabled: false,
  backend_mode_enabled: false,
  oidc_oauth_enabled: false,
  oidc_oauth_provider_name: 'OIDC',
  github_oauth_enabled: false,
  google_oauth_enabled: false,
  password_reset_enabled: false,
  passkey_enabled: false,
  login_agreement_enabled: false,
  login_agreement_documents: []
}

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: pushMock,
    currentRoute: { value: { query: {} } }
  })
}))

vi.mock('vue-i18n', () => ({
  createI18n: () => ({
    global: {
      t: (key: string) => key
    }
  }),
  useI18n: () => ({
    t: (key: string) => key
  })
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({
    login: loginMock,
    loginWithPasskey: vi.fn(),
    login2FA: vi.fn()
  }),
  useAppStore: () => ({
    showError: showErrorMock,
    showSuccess: vi.fn(),
    showWarning: vi.fn()
  })
}))

vi.mock('@/api/auth', () => ({
  buildOAuthLoginStartURL: vi.fn(),
  getPublicSettings: (...args: unknown[]) => getPublicSettingsMock(...args),
  isTotp2FARequired: vi.fn(() => false),
  isWeChatWebOAuthEnabled: vi.fn(() => false),
  startOAuthLogin: vi.fn()
}))

function mountLogin() {
  return mount(LoginView, {
    global: {
      stubs: {
        AuthLayout: { template: '<div><slot /><slot name="footer" /></div>' },
        DingTalkOAuthSection: true,
        EmailOAuthButtons: true,
        Icon: true,
        LinuxDoOAuthSection: true,
        LoginAgreementPrompt: true,
        OidcOAuthSection: true,
        RouterLink: { template: '<a><slot /></a>' },
        TotpLoginModal: true,
        TurnstileWidget: true,
        WechatOAuthSection: true,
        transition: false
      }
    }
  })
}

describe('LoginView', () => {
  beforeEach(() => {
    getPublicSettingsMock.mockReset()
    pushMock.mockReset()
    loginMock.mockReset().mockResolvedValue({})
    showErrorMock.mockReset()
    getPublicSettingsMock.mockResolvedValue(publicSettings)
  })

  it.each(['中中中', '😀😀', 'a'.repeat(72)])('authenticates a valid setup password %s without character-count restrictions', async (password) => {
    const wrapper = mountLogin()
    try {
      await flushPromises()
      await wrapper.get('#email').setValue('owner@example.com')
      await wrapper.get('#password').setValue(password)
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      expect(loginMock).toHaveBeenCalledWith(expect.objectContaining({ email: 'owner@example.com', password }))
      expect(pushMock).toHaveBeenCalled()
    } finally { wrapper.unmount() }
  })

  it('keeps empty passwords local and shows server rejection for an incorrect nonempty password', async () => {
    const wrapper = mountLogin()
    try {
      await flushPromises()
      await wrapper.get('#email').setValue('owner@example.com')
      await wrapper.get('form').trigger('submit')
      expect(loginMock).not.toHaveBeenCalled()
      expect(wrapper.get('#password').classes()).toContain('input-error')
      loginMock.mockRejectedValueOnce({ response: { data: { message: 'Invalid credentials' } } })
      await wrapper.get('#password').setValue('wrong')
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      expect(loginMock).toHaveBeenCalledOnce()
      expect(pushMock).not.toHaveBeenCalled()
      expect(showErrorMock).toHaveBeenCalledWith('Invalid credentials')
    } finally { wrapper.unmount() }
  })

  it('shows the registration entry when registration is enabled', async () => {
    const wrapper = mountLogin()
    await flushPromises()

    expect(wrapper.text()).toContain('auth.signUp')
  })

  it('hides the registration entry when registration is disabled', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      registration_enabled: false
    })

    const wrapper = mountLogin()
    await flushPromises()

    expect(wrapper.text()).not.toContain('auth.signUp')
  })
})
