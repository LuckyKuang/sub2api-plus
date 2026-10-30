import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import CodexCLICreateSettings from '../CodexCLICreateSettings.vue'

const { getRuntime } = vi.hoisted(() => ({ getRuntime: vi.fn() }))
vi.mock('@/api/admin/codexCliRuntime', () => ({ getCodexCLIRuntime: getRuntime }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const wrappers: VueWrapper[] = []
function settings() {
  const wrapper = mount(CodexCLICreateSettings, {
    props: { active: true, enabled: false, configToml: '' },
  })
  wrappers.push(wrapper)
  return wrapper
}

// Requirements: a new OAuth account saves CLI preferences in its creation write;
// global bridge permission gates visibility and runtime readiness gates enablement.
describe('Codex CLI account creation settings', () => {
  beforeEach(() => {
    getRuntime.mockReset().mockResolvedValue({ execution_available: true, bridge_enabled: true, default_config_toml: 'sandbox_mode = "danger-full-access"\napproval_policy = "never"\ncli_auth_credentials_store = "file"\n' })
  })
  afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

  it.each(['off', 'unknown', 'failed'])('hides the section when global permission is %s', async state => {
    if (state === 'off') getRuntime.mockResolvedValue({ execution_available: true, bridge_enabled: false })
    else if (state === 'unknown') getRuntime.mockResolvedValue({ execution_available: true })
    else getRuntime.mockRejectedValue(new Error('private-runtime-diagnostic'))
    const wrapper = settings()
    expect(wrapper.find('[data-testid="codex-cli-create-settings"]').exists()).toBe(false)
    expect(wrapper.vm.prepareCreate()).toBeNull()
    await flushPromises()
    expect(wrapper.find('[data-testid="codex-cli-create-settings"]').exists()).toBe(false)
    expect(wrapper.vm.prepareCreate()).toBeNull()
    expect(wrapper.html()).not.toContain('private-runtime-diagnostic')
  })

  it('reads global permission again on reopening and never submits the hidden draft', async () => {
    const wrapper = settings()
    await flushPromises()
    await wrapper.setProps({ enabled: true, configToml: 'sandbox_mode = "read-only"' })
    expect(wrapper.vm.prepareCreate()).toEqual({ enabled: true, config_toml: 'sandbox_mode = "read-only"' })
    await wrapper.setProps({ active: false })
    expect(wrapper.vm.prepareCreate()).toBeNull()
    getRuntime.mockResolvedValueOnce({ execution_available: true, bridge_enabled: false })
    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(getRuntime).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-testid="codex-cli-create-settings"]').exists()).toBe(false)
    expect(wrapper.vm.prepareCreate()).toBeNull()
    expect(wrapper.props('configToml')).toBe('sandbox_mode = "read-only"')
    expect(wrapper.emitted('update:configToml')).toBeUndefined()
    expect(wrapper.emitted('update:enabled')).toBeUndefined()
  })

  it('preserves a nonempty custom draft through enable toggles and reopening', async () => {
    const custom = 'sandbox_mode = "workspace-write"\nmodel = "gpt-6.1-sol"\n'
    const wrapper = mount(CodexCLICreateSettings, { props: { active: true, enabled: false, configToml: custom } })
    wrappers.push(wrapper)
    await flushPromises()
    for (const enabled of [true, false, true]) {
      await wrapper.setProps({ enabled })
      expect(wrapper.get<HTMLTextAreaElement>('[data-testid="codex-cli-create-config"]').element.value).toBe(custom)
    }
    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(getRuntime).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('initialize:configToml')).toBeUndefined()
    expect(wrapper.emitted('update:configToml')).toBeUndefined()
    expect(wrapper.vm.prepareCreate()).toEqual({ enabled: true, config_toml: custom })
  })

  it('shows controls only after enabled permission resolves', async () => {
    let resolve!: (value: unknown) => void
    getRuntime.mockReturnValueOnce(new Promise(done => { resolve = done }))
    const wrapper = settings()
    expect(wrapper.find('[data-testid="codex-cli-create-settings"]').exists()).toBe(false)
    resolve({ execution_available: true, bridge_enabled: true, default_config_toml: 'sandbox_mode = "danger-full-access"\napproval_policy = "never"\ncli_auth_credentials_store = "file"\n' })
    await flushPromises()
    expect(wrapper.find('[data-testid="codex-cli-create-settings"]').exists()).toBe(true)
    expect(wrapper.emitted('initialize:configToml')).toEqual([['sandbox_mode = "danger-full-access"\napproval_policy = "never"\ncli_auth_credentials_store = "file"\n']])
    await wrapper.get('[data-testid="codex-cli-create-mode"]').trigger('click')
    expect(wrapper.emitted('update:enabled')).toEqual([[true]])
  })

  it('keeps configuration local and does not expose another credentials form', async () => {
    const wrapper = settings()
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-create-config"]').setValue('sandbox_mode = "workspace-write"')
    expect(wrapper.emitted('update:configToml')).toEqual([['sandbox_mode = "workspace-write"']])
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
  })

  it('ignores a readiness response received after the creation form closes', async () => {
    let resolve!: (value: unknown) => void
    getRuntime.mockReturnValueOnce(new Promise(done => { resolve = done }))
    const wrapper = settings()
    await wrapper.setProps({ active: false })
    resolve({ execution_available: true, bridge_enabled: true, default_config_toml: 'sandbox_mode = "danger-full-access"\napproval_policy = "never"\ncli_auth_credentials_store = "file"\n' })
    await flushPromises()
    expect(wrapper.find('[data-testid="codex-cli-create-settings"]').exists()).toBe(false)
  })
})
