import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'

const { updateAccountMock, checkMixedChannelRiskMock, authIsSimpleMode, showErrorMock } = vi.hoisted(() => ({
  updateAccountMock: vi.fn(),
  showErrorMock: vi.fn(),
  checkMixedChannelRiskMock: vi.fn(),
  authIsSimpleMode: { value: true }
}))

vi.mock('@/components/account/OutboundIdentityEditor.vue', () => ({ default: { template: '<div />' } }))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: showErrorMock,
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    get isSimpleMode() {
      return authIsSimpleMode.value
    }
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      update: updateAccountMock,
      checkMixedChannelRisk: checkMixedChannelRiskMock
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    },
    tlsFingerprintProfiles: {
      list: vi.fn().mockResolvedValue([])
    }
  }
}))

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

import EditAccountModal from '../EditAccountModal.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: {
      type: Boolean,
      default: false
    }
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

function buildGrokOAuthAccount(
  credentials: Record<string, unknown> = {},
  extra: Record<string, unknown> = {}
) {
  return {
    id: 5,
    name: 'Grok OAuth',
    notes: '',
    platform: 'grok',
    type: 'oauth',
    credentials: {
      expires_at: '2027-01-01T00:00:00Z',
      token_type: 'Bearer',
      ...credentials
    },
    credentials_status: { has_access_token: true, has_refresh_token: true },
    extra,
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    rate_multiplier: 1,
    status: 'active',
    group_ids: [],
    expires_at: null,
    auto_pause_on_expired: false
  } as any
}

function mountModal(account: any) {
  return mount(EditAccountModal, {
    props: {
      show: true,
      account,
      proxies: [],
      groups: []
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: true,
        Icon: true,
        ProxySelector: true,
        GroupSelector: true,
        ModelWhitelistSelector: true
      }
    }
  })
}

describe('EditAccountModal Grok OAuth upstream config', () => {
  beforeEach(() => {
    authIsSimpleMode.value = true
    updateAccountMock.mockReset()
    showErrorMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
  })

  it('enabling the custom base URL toggle and saving persists base_url', async () => {
    const account = buildGrokOAuthAccount({ base_url: 'https://cli-chat-proxy.grok.com/v1' })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    // 官方地址 → 开关初始为关（视同未定制）
    const toggle = wrapper.get('[data-testid="grok-custom-base-url-toggle"]')
    await toggle.trigger('click')

    const input = wrapper.get('[data-testid="grok-custom-base-url-input"]')
    await input.setValue('https://my-relay.example.com')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await vi.waitFor(() => expect(updateAccountMock).toHaveBeenCalledTimes(1))

    const payload = updateAccountMock.mock.calls[0]?.[1]
    expect(payload?.credentials?.base_url).toBe('https://my-relay.example.com')
  })

  it('accepts the official API host as a manual endpoint switch and persists it', async () => {
    const account = buildGrokOAuthAccount({ base_url: 'https://cli-chat-proxy.grok.com/v1' })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('[data-testid="grok-custom-base-url-toggle"]').trigger('click')
    await wrapper.get('[data-testid="grok-custom-base-url-input"]').setValue('https://api.x.ai/v1')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await vi.waitFor(() => expect(updateAccountMock).toHaveBeenCalledTimes(1))

    const payload = updateAccountMock.mock.calls[0]?.[1]
    expect(payload?.credentials?.base_url).toBe('https://api.x.ai/v1')
  })

  it('echoes a stored official API endpoint with the toggle on', async () => {
    const account = buildGrokOAuthAccount({ base_url: 'https://us-west-2.api.x.ai/v1' })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    const input = wrapper.get('[data-testid="grok-custom-base-url-input"]')
    expect((input.element as HTMLInputElement).value).toBe('https://us-west-2.api.x.ai/v1')
  })

  it('fills the input from an endpoint preset chip', async () => {
    const account = buildGrokOAuthAccount({ base_url: 'https://cli-chat-proxy.grok.com/v1' })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('[data-testid="grok-custom-base-url-toggle"]').trigger('click')
    const presets = wrapper.findAll('[data-testid="grok-base-url-preset"]')
    expect(presets.length).toBe(5)

    // 第二个预设为官方 API (api.x.ai/v1)
    await presets[1].trigger('click')
    const input = wrapper.get('[data-testid="grok-custom-base-url-input"]')
    expect((input.element as HTMLInputElement).value).toBe('https://api.x.ai/v1')
  })

  it('loads an existing custom base_url with the toggle on and keeps it on save', async () => {
    const account = buildGrokOAuthAccount({ base_url: 'https://my-relay.example.com' })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    const input = wrapper.get('[data-testid="grok-custom-base-url-input"]')
    expect((input.element as HTMLInputElement).value).toBe('https://my-relay.example.com')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await vi.waitFor(() => expect(updateAccountMock).toHaveBeenCalledTimes(1))

    const payload = updateAccountMock.mock.calls[0]?.[1]
    expect(payload?.credentials?.base_url).toBe('https://my-relay.example.com')
  })

  it('keeps stored header overrides intact on an untouched save', async () => {
    const account = buildGrokOAuthAccount({
      header_override_enabled: true,
      header_overrides: {
        'x-route': 'primary',
        'x-xai-token-auth': 'xai-grok-cli'
      }
    })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await vi.waitFor(() => expect(updateAccountMock).toHaveBeenCalledTimes(1))

    const payload = updateAccountMock.mock.calls[0]?.[1]
    expect(payload?.credentials?.header_override_enabled).toBe(true)
    expect(payload?.credentials?.header_overrides).toEqual({
      'x-route': 'primary',
      'x-xai-token-auth': 'xai-grok-cli'
    })
  })

  it('requires removal of legacy identity overrides before saving and preserves ordinary headers', async () => {
    const account = buildGrokOAuthAccount({
      header_override_enabled: true,
      header_overrides: { 'X-Stainless-Package-Version': '999.0.0', 'x-route': 'primary' }
    })
    updateAccountMock.mockResolvedValue(account)
    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await vi.waitFor(() => expect(showErrorMock).toHaveBeenCalledWith('admin.accounts.headerOverride.blockedName'))
    expect(updateAccountMock).not.toHaveBeenCalled()

    wrapper.findComponent({ name: 'HeaderOverrideEditor' }).vm.$emit('update:rows', [
      { name: 'x-route', value: 'primary' }
    ])
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await vi.waitFor(() => expect(updateAccountMock).toHaveBeenCalledTimes(1))
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.header_overrides).toEqual({ 'x-route': 'primary' })
  })

  it('does not expose the retired tool-cache control on Grok OAuth or API Key', () => {
    const oauth = mountModal(buildGrokOAuthAccount())
    expect(oauth.find('[data-testid="grok-client-tool-cache-toggle"]').exists()).toBe(false)
    const apiKey = mountModal({ ...buildGrokOAuthAccount(), type: 'apikey', credentials: { api_key: 'test-key' } })
    expect(apiKey.find('[data-testid="grok-client-tool-cache-toggle"]').exists()).toBe(false)
  })

  it('saving a Grok account does not recreate the retired cache configuration', async () => {
    const account = buildGrokOAuthAccount({}, { custom_setting: 'keep-me' })
    updateAccountMock.mockResolvedValue(account)
    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await vi.waitFor(() => expect(updateAccountMock).toHaveBeenCalledTimes(1))
    const payload = updateAccountMock.mock.calls[0]?.[1]
    expect(payload?.extra).toEqual({ custom_setting: 'keep-me' })
    expect(payload?.extra).not.toHaveProperty('grok_client_tool_cache_enabled')
    expect(account.extra).toEqual({ custom_setting: 'keep-me' })
  })
})
