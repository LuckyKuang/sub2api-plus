import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { Account } from '@/types'
import type { CodexCLIAccountView } from '@/api/admin/codexCli'
import Select from '@/components/common/Select.vue'
import CodexCLIEditor from '../CodexCLIEditor.vue'

const { getAccount, updateAccount, getRuntime } = vi.hoisted(() => ({ getAccount: vi.fn(), updateAccount: vi.fn(), getRuntime: vi.fn() }))
vi.mock('@/api/admin/codexCli', () => ({ getCodexCLIAccount: getAccount, updateCodexCLIAccount: updateAccount }))
vi.mock('@/api/admin/codexCliRuntime', () => ({ getCodexCLIRuntime: getRuntime }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const defaultConfig = 'sandbox_mode = "danger-full-access"\napproval_policy = "never"\ncli_auth_credentials_store = "file"\n'
const ownerA = 'a'.repeat(64)
const ownerB = 'b'.repeat(64)
function account(id = 24, overrides: Partial<Account> = {}): Account {
  return {
    id, platform: 'openai', type: 'oauth', parent_account_id: null,
    credentials: {}, oauth_credential_owner: id === 24 ? ownerA : ownerB,
    ...overrides,
  } as Account
}
function view(overrides: Partial<CodexCLIAccountView> = {}): CodexCLIAccountView {
  return {
    enabled: false,
    config_toml: defaultConfig,
    auth: { auth_mode: 'chatgpt', tokens: { account_id: 'workspace-a' } },
    auth_status: { has_access_token: true, has_refresh_token: true, has_id_token: false },
    expected_owner: ownerA,
    oauth_credential_owner: ownerA,
    availability: { available: true, code: 'exec_task_only', reason: 'task mode' },
    ...overrides,
  }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const wrappers: VueWrapper[] = []
function editor(overrides: Partial<Account> = {}) {
  const wrapper = mount(CodexCLIEditor, { props: { account: account(24, overrides) } })
  wrappers.push(wrapper)
  return wrapper
}
function choose(wrapper: VueWrapper, name: string, value: string) {
  const select = wrapper.findAllComponents(Select).find(component =>
    component.props('id') === `codex-cli-${name}-action-${wrapper.props('account').id}`)
  expect(select, `shared Select for ${name}`).toBeDefined()
  select!.vm.$emit('update:modelValue', value)
}

// Requirements: edits are local until account Save, secrets never round-trip,
// and old requests must not update a different account or authorization owner.
describe('Codex CLI account editor', () => {
  beforeEach(() => {
    getAccount.mockReset().mockResolvedValue(view())
    getRuntime.mockReset().mockResolvedValue({ execution_available: false })
    updateAccount.mockReset().mockResolvedValue(view({ expected_owner: ownerB }))
  })
  afterEach(() => {
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  })

  it('shows defaults and disables task mode until the pinned runtime is ready', async () => {
    const wrapper = editor()
    await flushPromises()
    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="codex-cli-config"]').element.value).toBe(defaultConfig)
    expect(wrapper.get('[data-testid="codex-cli-access_token-status"]').text()).toBe('codexCli.account.configured')
    expect(wrapper.get('[data-testid="codex-cli-id_token-status"]').text()).toBe('codexCli.account.notConfigured')
    expect(wrapper.get('[data-testid="codex-cli-mode"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="codex-cli-mode"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.text()).toContain('/v1/codex-cli/responses')
    expect(wrapper.text()).toContain('codexCli.account.taskModeHint')
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    expect(wrapper.vm.prepareSave()).toBeNull()
    expect(updateAccount).not.toHaveBeenCalled()
  })

  it('enables only the separate task mode when the runtime is ready', async () => {
    getRuntime.mockResolvedValueOnce({ execution_available: true })
    const wrapper = editor()
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-mode"]').trigger('click')
    expect(wrapper.vm.prepareSave()!.payload).toEqual({ expected_owner: ownerA, config_toml: defaultConfig, enabled: true })
    expect(updateAccount).not.toHaveBeenCalled()
  })

  it.each(['missing', 'check-failed'])('keeps task mode disabled for runtime %s', async state => {
    if (state === 'check-failed') getRuntime.mockRejectedValueOnce(new Error('never-show-runtime-secret'))
    const wrapper = editor()
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-cli-mode"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('codexCli.account.runtimeUnavailable')
    expect(wrapper.html()).not.toContain('never-show-runtime-secret')
    await wrapper.get('[data-testid="codex-cli-config"]').setValue('editable-config')
    expect(wrapper.vm.prepareSave()!.payload).toEqual({ expected_owner: ownerA, config_toml: 'editable-config' })
  })

  it('allows turning a saved task mode off while its runtime is missing', async () => {
    getAccount.mockResolvedValueOnce(view({ enabled: true }))
    const wrapper = editor()
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-cli-mode"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="codex-cli-mode"]').trigger('click')
    expect(wrapper.vm.prepareSave()!.payload).toEqual({ expected_owner: ownerA, config_toml: defaultConfig, enabled: false })
  })

  it.each([
    { platform: 'anthropic' }, { type: 'apikey' }, { type: 'setup-token' }, { parent_account_id: 2 },
    { credentials: { auth_mode: 'agentIdentity' } },
    { credentials: { auth_mode: 'personalAccessToken' } },
    { credentials: { openai_auth_mode: 'personal_access_token' } },
  ])('does not load or render unsupported account %j', async overrides => {
    const wrapper = editor(overrides)
    await flushPromises()
    expect(wrapper.find('[data-testid="codex-cli-editor"]').exists()).toBe(false)
    expect(getAccount).not.toHaveBeenCalled()
    expect(wrapper.vm.prepareSave()).toBeNull()
  })

  it('keeps config edits local and includes the original owner when explicitly saved', async () => {
    const wrapper = editor()
    await flushPromises()
    const modified = 'sandbox_mode = "read-only"\napproval_policy = "never"\n'
    await wrapper.get('[data-testid="codex-cli-config"]').setValue(modified)
    expect(updateAccount).not.toHaveBeenCalled()
    const prepared = wrapper.vm.prepareSave()!
    expect(prepared.payload).toEqual({ expected_owner: ownerA, config_toml: modified })
    await wrapper.vm.save(prepared)
    expect(updateAccount).toHaveBeenCalledExactlyOnceWith(24, { expected_owner: ownerA, config_toml: modified })
    expect(wrapper.vm.prepareSave()).toBeNull()
  })

  it('sends an empty config unchanged to request server defaults', async () => {
    const wrapper = editor()
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-config"]').setValue('')
    expect(wrapper.vm.prepareSave()!.payload).toEqual({ expected_owner: ownerA, config_toml: '' })
  })

  it('separates secret preservation, replacement and explicit clearing', async () => {
    const wrapper = editor()
    await flushPromises()
    choose(wrapper, 'access_token', 'replace')
    choose(wrapper, 'id_token', 'clear')
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-access_token-value"]').setValue('new-access')
    expect(wrapper.get('[data-testid="codex-cli-access_token-value"]').attributes('type')).toBe('password')
    const prepared = wrapper.vm.prepareSave()!
    expect(prepared.payload).toEqual({
      expected_owner: ownerA,
      config_toml: defaultConfig,
      auth: { auth_mode: 'chatgpt', tokens: { access_token: 'new-access', id_token: null } },
    })
    expect(prepared.payload.auth!.tokens).not.toHaveProperty('refresh_token')
    await wrapper.vm.save(prepared)
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
  })

  it('drops a replacement when the administrator switches back to Keep', async () => {
    const wrapper = editor()
    await flushPromises()
    choose(wrapper, 'refresh_token', 'replace')
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-refresh_token-value"]').setValue('draft-secret')
    choose(wrapper, 'refresh_token', 'keep')
    await flushPromises()
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    expect(wrapper.vm.prepareSave()).toBeNull()
  })

  it('rejects blank replacement before saving either resource', async () => {
    const wrapper = editor()
    await flushPromises()
    choose(wrapper, 'access_token', 'replace')
    await flushPromises()
    expect(() => wrapper.vm.prepareSave()).toThrow('codexCli.account.replacementRequired')
    expect(updateAccount).not.toHaveBeenCalled()
    await nextTick()
    expect(wrapper.get('[role="alert"]').text()).toBe('codexCli.account.replacementRequired')
  })

  it('submits workspace changes for server validation without adopting a new owner', async () => {
    const wrapper = editor()
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-workspace"]').setValue('workspace-other')
    expect(wrapper.vm.prepareSave()!.payload.auth).toEqual({ auth_mode: 'chatgpt', tokens: { account_id: 'workspace-other' } })
    updateAccount.mockRejectedValueOnce({ status: 409, message: 'sensitive-server-detail' })
    await expect(wrapper.vm.save(wrapper.vm.prepareSave()!)).rejects.toThrow('codexCli.account.saveFailed')
    expect(wrapper.html()).not.toContain('sensitive-server-detail')
    expect(wrapper.vm.prepareSave()!.payload.expected_owner).toBe(ownerA)
    expect(getAccount).toHaveBeenCalledTimes(1)
    expect(wrapper.get<HTMLInputElement>('[data-testid="codex-cli-workspace"]').element.value).toBe('workspace-other')
  })

  it('shows a safe load failure, retries, and does not save during either read', async () => {
    getAccount.mockRejectedValueOnce(new Error('secret=never-show'))
    const wrapper = editor()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('codexCli.account.loadFailed')
    expect(wrapper.html()).not.toContain('never-show')
    expect(wrapper.vm.prepareSave()).toBeNull()
    await wrapper.get('button.btn-secondary').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-cli-config"]').exists()).toBe(true)
    expect(updateAccount).not.toHaveBeenCalled()
  })

  it('discards drafts on cancel and reloads the saved state when reopened', async () => {
    const wrapper = editor()
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-config"]').setValue('draft config')
    choose(wrapper, 'access_token', 'replace')
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-access_token-value"]').setValue('discard-me')
    await wrapper.setProps({ active: false })
    expect(wrapper.vm.prepareSave()).toBeNull()
    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="codex-cli-config"]').element.value).toBe(defaultConfig)
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    expect(updateAccount).not.toHaveBeenCalled()
  })

  it.each(['resolve', 'reject'] as const)('ignores a late %s from a previous account', async outcome => {
    const old = deferred<CodexCLIAccountView>()
    getAccount.mockReturnValueOnce(old.promise)
    const wrapper = editor()
    const oldSignal = getAccount.mock.calls[0][1].signal as AbortSignal
    getAccount.mockResolvedValueOnce(view({ expected_owner: ownerB, config_toml: 'account-b-config' }))
    await wrapper.setProps({ account: account(25) })
    await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    if (outcome === 'resolve') old.resolve(view({ config_toml: 'account-a-stale' }))
    else old.reject(new Error('account-a-secret'))
    await flushPromises()
    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="codex-cli-config"]').element.value).toBe('account-b-config')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.html()).not.toContain('account-a-secret')
  })

  it('revokes a prepared save when authorization changes under the same account ID', async () => {
    const wrapper = editor()
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-config"]').setValue('draft')
    const prepared = wrapper.vm.prepareSave()!
    await wrapper.setProps({ account: account(24, { oauth_credential_owner: ownerB }) })
    await flushPromises()
    await expect(wrapper.vm.save(prepared)).rejects.toThrow('codexCli.account.ownerChanged')
    expect(updateAccount).not.toHaveBeenCalled()
  })

  it('does not replace another account with a late save result', async () => {
    const pending = deferred<CodexCLIAccountView>()
    const wrapper = editor()
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-config"]').setValue('a-draft')
    updateAccount.mockReturnValueOnce(pending.promise)
    const saving = wrapper.vm.save(wrapper.vm.prepareSave()!)
    getAccount.mockResolvedValueOnce(view({ expected_owner: ownerB, config_toml: 'b-saved' }))
    await wrapper.setProps({ account: account(25) })
    await flushPromises()
    pending.resolve(view({ config_toml: 'a-late-result' }))
    await saving
    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="codex-cli-config"]').element.value).toBe('b-saved')
  })
})
