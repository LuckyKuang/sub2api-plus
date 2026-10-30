import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { Account } from '@/types'
import type { CodexCLIAccountView } from '@/api/admin/codexCli'

const { updateAccount, getCLI, updateCLI, getRuntime, showError, showSuccess } = vi.hoisted(() => ({
  updateAccount: vi.fn(), getCLI: vi.fn(), updateCLI: vi.fn(), getRuntime: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(),
}))
vi.mock('@/components/account/OutboundIdentityEditor.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess, showInfo: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: true }) }))
vi.mock('@/api/admin/codexCli', () => ({ getCodexCLIAccount: getCLI, updateCodexCLIAccount: updateCLI }))
vi.mock('@/api/admin/codexCliRuntime', () => ({ getCodexCLIRuntime: getRuntime }))
vi.mock('@/api/admin/accounts', () => ({ getAntigravityDefaultModelMapping: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: {
  accounts: { update: updateAccount, checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false }) },
  settings: {
    getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
    getSettings: vi.fn().mockResolvedValue({}),
  },
  tlsFingerprintProfiles: { list: vi.fn().mockResolvedValue([]) },
} }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

import EditAccountModal from '../EditAccountModal.vue'
import Select from '@/components/common/Select.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'

const owner = 'c'.repeat(64)
const defaultConfig = 'sandbox_mode = "danger-full-access"\napproval_policy = "never"\ncli_auth_credentials_store = "file"\n'
function account(id = 41): Account {
  return {
    id, name: 'Codex account', notes: '', platform: 'openai', type: 'oauth', parent_account_id: null,
    credentials: { expires_at: '2027-01-01T00:00:00Z' },
    credentials_status: { has_access_token: true, has_refresh_token: true },
    oauth_credential_owner: owner, extra: {}, proxy_id: null, concurrency: 1, priority: 1,
    rate_multiplier: 1, status: 'active', group_ids: [], expires_at: null, auto_pause_on_expired: false,
  } as Account
}
function cliView(): CodexCLIAccountView {
  return {
    enabled: false,
    config_toml: defaultConfig,
    auth: { auth_mode: 'chatgpt', tokens: { account_id: 'team-a' } },
    auth_status: { has_access_token: true, has_refresh_token: true, has_id_token: false },
    expected_owner: owner,
    oauth_credential_owner: owner,
    availability: { available: true, code: 'exec_task_only', reason: 'task endpoint' },
  }
}
const BaseDialog = defineComponent({
  props: { show: Boolean },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})
const wrappers: VueWrapper[] = []
function modal(initialAccount = account()) {
  const wrapper = mount(EditAccountModal, {
    props: { show: true, account: initialAccount, proxies: [], groups: [] },
    global: { stubs: {
      BaseDialog, Icon: true, ProxySelector: true, GroupSelector: true, ModelWhitelistSelector: true,
    } },
  })
  wrappers.push(wrapper)
  return wrapper
}

// Account Save must not overwrite a newly replaced CLI grant with its old
// redacted form. Cancel must remain free of writes; partial success is explicit.
describe('EditAccountModal Codex CLI save workflow', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    updateAccount.mockReset().mockResolvedValue(account())
    getCLI.mockReset().mockResolvedValue(cliView())
    updateCLI.mockReset().mockResolvedValue(cliView())
    getRuntime.mockReset().mockResolvedValue({ execution_available: true, bridge_enabled: true })
  })
  afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

  it.each(['off', 'unknown', 'failed'])('hides official CLI editing when global bridge is %s', async state => {
    if (state === 'off') getRuntime.mockResolvedValue({ execution_available: true, bridge_enabled: false })
    else if (state === 'unknown') getRuntime.mockResolvedValue({ execution_available: true })
    else getRuntime.mockRejectedValue(new Error('private-runtime-diagnostic'))
    const wrapper = modal()
    expect(wrapper.find('[data-testid="codex-cli-editor"]').exists()).toBe(false)
    await flushPromises()
    expect(wrapper.find('[data-testid="codex-cli-editor"]').exists()).toBe(false)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccount).toHaveBeenCalledTimes(1)
    expect(updateCLI).not.toHaveBeenCalled()
    expect(wrapper.html()).not.toContain('private-runtime-diagnostic')
  })

  it('preserves existing custom configuration when opening and saving unrelated edits', async () => {
    const custom = 'sandbox_mode = "workspace-write"\napproval_policy = "never"\nmodel = "gpt-6.1-sol"\n'
    getCLI.mockResolvedValue({ ...cliView(), config_toml: custom })
    const wrapper = modal()
    await flushPromises()
    const userAgentGroup = wrapper.get('[data-testid="edit-openai-account-user-agent"]').element.parentElement!
    expect(userAgentGroup.nextElementSibling).toBe(wrapper.get('[data-testid="codex-cli-editor"]').element)
    expect(userAgentGroup.nextElementSibling?.nextElementSibling?.textContent).toContain('admin.accounts.openai.oauthSessionSharing')
    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="codex-cli-config"]').element.value).toBe(custom)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateCLI).not.toHaveBeenCalled()
  })

  it('saves unrelated account edits without writing CLI defaults or credentials', async () => {
    const wrapper = modal()
    await flushPromises()
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccount).toHaveBeenCalledTimes(1)
    expect(updateCLI).not.toHaveBeenCalled()
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.accountUpdated')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('saves the general account before replacing a secret through the dedicated API', async () => {
    const calls: string[] = []
    updateAccount.mockImplementationOnce(async () => { calls.push('account'); return account() })
    updateCLI.mockImplementationOnce(async () => { calls.push('cli'); return cliView() })
    const wrapper = modal()
    await flushPromises()
    const action = wrapper.findAllComponents(Select).find(component => component.props('id') === 'codex-cli-access_token-action-41')!
    action.vm.$emit('update:modelValue', 'replace')
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-access_token-value"]').setValue('new-owner-secret')
    expect(updateAccount).not.toHaveBeenCalled()
    expect(updateCLI).not.toHaveBeenCalled()
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(calls).toEqual(['account', 'cli'])
    expect(updateCLI).toHaveBeenCalledExactlyOnceWith(41, {
      expected_owner: owner, config_toml: defaultConfig,
      auth: { auth_mode: 'chatgpt', tokens: { access_token: 'new-owner-secret' } },
    })
    expect(JSON.stringify(updateAccount.mock.calls)).not.toContain('new-owner-secret')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('does not write either resource when closing a modified form', async () => {
    const wrapper = modal()
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-config"]').setValue('draft-only')
    const cancel = wrapper.findAll('button').find(button => button.text() === 'common.cancel')!
    await cancel.trigger('click')
    expect(updateAccount).not.toHaveBeenCalled()
    expect(updateCLI).not.toHaveBeenCalled()
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it.each([
    { originalProxy: null, selectedProxy: 7 },
    { originalProxy: 7, selectedProxy: null },
  ])('rejects changing proxy $originalProxy to $selectedProxy with CLI edits before either write', async ({ originalProxy, selectedProxy }) => {
    const wrapper = modal({ ...account(), proxy_id: originalProxy })
    await flushPromises()
    wrapper.findComponent(ProxySelector).vm.$emit('update:modelValue', selectedProxy)
    await wrapper.get('[data-testid="codex-cli-config"]').setValue('sandbox_mode = "read-only"')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccount).not.toHaveBeenCalled()
    expect(updateCLI).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('codexCli.account.saveProxyFirst')
    expect(showSuccess).not.toHaveBeenCalled()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="codex-cli-config"]').element.value).toBe('sandbox_mode = "read-only"')
  })

  it('saves a proxy independently and binds subsequent CLI edits to the reloaded owner', async () => {
    const nextOwner = 'd'.repeat(64)
    const savedAccount = { ...account(), proxy_id: 7, oauth_credential_owner: nextOwner }
    updateAccount.mockResolvedValue(savedAccount)
    const wrapper = modal()
    await flushPromises()
    wrapper.findComponent(ProxySelector).vm.$emit('update:modelValue', 7)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccount).toHaveBeenCalledTimes(1)
    expect(updateAccount.mock.calls[0][1]).toMatchObject({ proxy_id: 7 })
    expect(updateCLI).not.toHaveBeenCalled()
    expect(wrapper.emitted('close')).toHaveLength(1)

    await wrapper.setProps({ show: false })
    getCLI.mockResolvedValueOnce({ ...cliView(), expected_owner: nextOwner, oauth_credential_owner: nextOwner })
    updateCLI.mockResolvedValueOnce({ ...cliView(), expected_owner: nextOwner, oauth_credential_owner: nextOwner })
    await wrapper.setProps({ show: true, account: savedAccount })
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-config"]').setValue('sandbox_mode = "read-only"')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccount).toHaveBeenCalledTimes(2)
    expect(updateCLI).toHaveBeenCalledExactlyOnceWith(41, {
      expected_owner: nextOwner, config_toml: 'sandbox_mode = "read-only"',
    })
    expect(showError).not.toHaveBeenCalled()
    expect(wrapper.emitted('close')).toHaveLength(2)
  })

  it('rejects an empty replacement before persisting the general account', async () => {
    const wrapper = modal()
    await flushPromises()
    wrapper.findAllComponents(Select).find(component => component.props('id') === 'codex-cli-refresh_token-action-41')!
      .vm.$emit('update:modelValue', 'replace')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccount).not.toHaveBeenCalled()
    expect(updateCLI).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('codexCli.account.replacementRequired')
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('keeps CLI drafts and reports partial success if its save fails', async () => {
    updateCLI.mockRejectedValueOnce({ status: 409, message: 'credential-secret' })
    const wrapper = modal()
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-config"]').setValue('sandbox_mode = "read-only"')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccount).toHaveBeenCalledTimes(1)
    expect(updateCLI).toHaveBeenCalledTimes(1)
    expect(showError).toHaveBeenCalledWith('codexCli.account.partialSave')
    expect(showSuccess).not.toHaveBeenCalled()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="codex-cli-config"]').element.value).toBe('sandbox_mode = "read-only"')
    expect(wrapper.html()).not.toContain('credential-secret')
  })

  it('does not save CLI configuration when the account update fails', async () => {
    updateAccount.mockRejectedValueOnce(new Error('account update failed'))
    const wrapper = modal()
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-config"]').setValue('sandbox_mode = "read-only"')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateCLI).not.toHaveBeenCalled()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('does not submit an old CLI draft after the selected account changes during Save', async () => {
    let finish!: (account: Account) => void
    updateAccount.mockReturnValueOnce(new Promise<Account>(resolve => { finish = resolve }))
    const wrapper = modal()
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-config"]').setValue('old-account-draft')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    await wrapper.setProps({ account: account(42) })
    await flushPromises()
    finish(account())
    await flushPromises()
    expect(updateCLI).not.toHaveBeenCalled()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="codex-cli-config"]').element.value).toBe(defaultConfig)
  })
})
