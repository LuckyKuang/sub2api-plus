import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { CodexCLIRuntimeStatus } from '@/api/admin/codexCliRuntime'
import CodexCLIRuntimeSettings from '../CodexCLIRuntimeSettings.vue'

const { getRuntime, installRuntime, saveBridge } = vi.hoisted(() => ({ getRuntime: vi.fn(), installRuntime: vi.fn(), saveBridge: vi.fn() }))
vi.mock('@/api/admin/codexCliRuntime', () => ({ getCodexCLIRuntime: getRuntime, installCodexCLI: installRuntime, updateCodexCLIBridge: saveBridge }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({
  t: (key: string, params?: { version?: string }) => params?.version ? `${key} ${params.version}` : key,
}) }))

function status(overrides: Partial<CodexCLIRuntimeStatus> = {}): CodexCLIRuntimeStatus {
  return {
    target_version: '0.165.0', installed_version: '', path: '', source: '', source_verified: false,
    platform: 'linux-arm64', installed: false, version_match: false, can_install: true,
    execution_available: false, bridge_enabled: false, unavailable_code: 'not_installed', message: 'safe server description',
    mode: 'exec_task', transparent_proxy_available: false, ...overrides,
  }
}
function readyStatus(): CodexCLIRuntimeStatus {
  return status({ installed: true, installed_version: '0.165.0', source: 'managed',
    path: '/app/data/codex-cli/0.165.0/codex', source_verified: true, version_match: true,
    execution_available: true, unavailable_code: '',
  })
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(yes => { resolve = yes })
  return { promise, resolve }
}
const wrappers: VueWrapper[] = []
function settings() {
  const wrapper = mount(CodexCLIRuntimeSettings)
  wrappers.push(wrapper)
  return wrapper
}

// Requirements: the application release pins one CLI version; merely finding
// `codex --version` does not establish official provenance or task readiness.
describe('Codex CLI runtime settings', () => {
  beforeEach(() => {
    getRuntime.mockReset().mockResolvedValue(status())
    installRuntime.mockReset().mockResolvedValue(readyStatus())
    saveBridge.mockReset().mockImplementation(async (enabled: boolean) => ({ ...readyStatus(), bridge_enabled: enabled }))
  })
  afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

  it('checks installation without automatically installing or checking a latest release', async () => {
    const wrapper = settings()
    await flushPromises()
    expect(getRuntime).toHaveBeenCalledTimes(1)
    expect(installRuntime).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="codex-cli-target-version"]').text()).toBe('0.165.0')
    expect(wrapper.get('[data-testid="codex-cli-installed-version"]').text()).toBe('codexCliRuntime.notInstalled')
    expect(wrapper.get('[data-testid="codex-cli-runtime-readiness"]').text()).toBe('codexCliRuntime.notReady')
    expect(wrapper.get('[data-testid="codex-cli-runtime-install"]').text()).toContain('0.165.0')
    expect(wrapper.find('input').exists()).toBe(false)
    expect(wrapper.text()).toContain('codexCliRuntime.taskHint')
    expect(wrapper.text()).toContain('codexCliRuntime.permissionsHint')
    expect(wrapper.get('[data-testid="codex-cli-bridge-toggle"]').attributes('disabled')).toBeDefined()
    expect(saveBridge).not.toHaveBeenCalled()
  })

  it('installs only after an explicit click and displays verified readiness', async () => {
    const wrapper = settings()
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-runtime-install"]').trigger('click')
    await flushPromises()
    expect(installRuntime).toHaveBeenCalledExactlyOnceWith()
    expect(wrapper.get('[data-testid="codex-cli-installed-version"]').text()).toBe('0.165.0')
    expect(wrapper.get('[data-testid="codex-cli-runtime-source"]').text()).toBe('codexCliRuntime.managed')
    expect(wrapper.get('[data-testid="codex-cli-runtime-path"]').text()).toBe('/app/data/codex-cli/0.165.0/codex')
    expect(wrapper.get('[data-testid="codex-cli-runtime-readiness"]').text()).toBe('codexCliRuntime.ready')
    expect(wrapper.get('[data-testid="codex-cli-runtime-install"]').attributes('disabled')).toBeDefined()
  })

  it.each(['0.164.0', '0.166.0', '0.165.0-alpha.1'])('requires switching %s to the exact release target', async version => {
    getRuntime.mockResolvedValueOnce(status({ installed: true, installed_version: version, source: 'system', path: '/usr/bin/codex', unavailable_code: 'version_mismatch' }))
    const wrapper = settings()
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-cli-installed-version"]').text()).toBe(version)
    expect(wrapper.get('[data-testid="codex-cli-target-version"]').text()).toBe('0.165.0')
    expect(wrapper.get('[data-testid="codex-cli-runtime-reason"]').text()).toBe('codexCliRuntime.versionMismatch')
    expect(wrapper.get('[data-testid="codex-cli-runtime-install"]').attributes('disabled')).toBeUndefined()
    expect(installRuntime).not.toHaveBeenCalled()
  })

  it('does not call a version-matching system CLI ready without source verification', async () => {
    getRuntime.mockResolvedValueOnce(status({ installed: true, installed_version: '0.165.0', version_match: true,
      source: 'system', path: '/usr/local/bin/codex', unavailable_code: 'source_unverified',
    }))
    const wrapper = settings()
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-cli-runtime-source"]').text()).toBe('codexCliRuntime.system')
    expect(wrapper.get('[data-testid="codex-cli-runtime-readiness"]').text()).toBe('codexCliRuntime.notReady')
    expect(wrapper.get('[data-testid="codex-cli-runtime-reason"]').text()).toBe('codexCliRuntime.sourceUnverified')
  })

  it('shows a detected unverified installation without claiming it is missing or executing it to read its version', async () => {
    getRuntime.mockResolvedValueOnce(status({ installed: true, installed_version: '',
      source: 'system', path: '/usr/local/bin/codex', unavailable_code: 'source_unverified',
    }))
    const wrapper = settings()
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-cli-installed-version"]').text()).toBe('codexCliRuntime.versionUnavailable')
    expect(wrapper.get('[data-testid="codex-cli-runtime-source"]').text()).toBe('codexCliRuntime.system')
    expect(wrapper.get('[data-testid="codex-cli-runtime-path"]').text()).toBe('/usr/local/bin/codex')
    expect(wrapper.get('[data-testid="codex-cli-runtime-readiness"]').text()).toBe('codexCliRuntime.notReady')
    expect(wrapper.get('[data-testid="codex-cli-runtime-reason"]').text()).toBe('codexCliRuntime.sourceUnverified')
    expect(installRuntime).not.toHaveBeenCalled()
  })

  it('allows a system CLI with the exact reviewed version and verified official binary', async () => {
    getRuntime.mockResolvedValueOnce({ ...readyStatus(), source: 'system', path: '/usr/local/bin/codex' })
    const wrapper = settings()
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-cli-runtime-source"]').text()).toBe('codexCliRuntime.system')
    expect(wrapper.get('[data-testid="codex-cli-runtime-readiness"]').text()).toBe('codexCliRuntime.ready')
    expect(wrapper.get('[data-testid="codex-cli-runtime-install"]').attributes('disabled')).toBeDefined()
    expect(installRuntime).not.toHaveBeenCalled()
  })

  it.each(['source_verified', 'version_match', 'installed'] as const)(
    'does not infer readiness from execution_available when %s is false', async field => {
      getRuntime.mockResolvedValueOnce({ ...readyStatus(), [field]: false })
      const wrapper = settings()
      await flushPromises()
      expect(wrapper.get('[data-testid="codex-cli-runtime-readiness"]').text()).toBe('codexCliRuntime.notReady')
    },
  )

  it('disables installation on unsupported platforms', async () => {
    getRuntime.mockResolvedValueOnce(status({ platform: 'unsupported', can_install: false, unavailable_code: 'unsupported_platform' }))
    const wrapper = settings()
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-cli-runtime-install"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="codex-cli-runtime-reason"]').text()).toBe('codexCliRuntime.unsupportedPlatform')
    await wrapper.get('[data-testid="codex-cli-runtime-install"]').trigger('click')
    expect(installRuntime).not.toHaveBeenCalled()
  })

  it.each([
    ['host_configuration_conflict', 'codexCliRuntime.hostConfigurationConflict'],
    ['host_configuration_check_failed', 'codexCliRuntime.hostConfigurationCheckFailed'],
  ])('keeps a verified CLI unavailable when %s blocks isolated tasks', async (code, reason) => {
    getRuntime.mockResolvedValueOnce({ ...readyStatus(), execution_available: false,
      can_install: false, unavailable_code: code })
    const wrapper = settings()
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-cli-installed-version"]').text()).toBe('0.165.0')
    expect(wrapper.get('[data-testid="codex-cli-runtime-readiness"]').text()).toBe('codexCliRuntime.notReady')
    expect(wrapper.get('[data-testid="codex-cli-runtime-reason"]').text()).toBe(reason)
    expect(wrapper.get('[data-testid="codex-cli-runtime-install"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="codex-cli-runtime-install"]').trigger('click')
    expect(installRuntime).not.toHaveBeenCalled()
  })

  it('prevents duplicate detection and installation while a check is pending', async () => {
    const pending = deferred<CodexCLIRuntimeStatus>()
    getRuntime.mockReturnValueOnce(pending.promise)
    const wrapper = settings()
    await nextTick()
    expect(wrapper.get('[data-testid="codex-cli-runtime-check"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="codex-cli-runtime-install"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="codex-cli-runtime-check"]').trigger('click')
    await wrapper.get('[data-testid="codex-cli-runtime-install"]').trigger('click')
    expect(getRuntime).toHaveBeenCalledTimes(1)
    expect(installRuntime).not.toHaveBeenCalled()
    pending.resolve(status())
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-cli-runtime-check"]').attributes('disabled')).toBeUndefined()
  })

  it('prevents duplicate installation and concurrent checks while installing', async () => {
    const pending = deferred<CodexCLIRuntimeStatus>()
    installRuntime.mockReturnValueOnce(pending.promise)
    const wrapper = settings()
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-runtime-install"]').trigger('click')
    await wrapper.get('[data-testid="codex-cli-runtime-install"]').trigger('click')
    await wrapper.get('[data-testid="codex-cli-runtime-check"]').trigger('click')
    expect(installRuntime).toHaveBeenCalledTimes(1)
    expect(getRuntime).toHaveBeenCalledTimes(1)
    pending.resolve(readyStatus())
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-cli-runtime-readiness"]').text()).toBe('codexCliRuntime.ready')
  })

  it('reports a safe detection failure and permits retry without installation', async () => {
    getRuntime.mockRejectedValueOnce(new Error('sensitive-server-detail'))
    const wrapper = settings()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('codexCliRuntime.checkFailed')
    expect(wrapper.html()).not.toContain('sensitive-server-detail')
    expect(wrapper.get('[data-testid="codex-cli-runtime-install"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="codex-cli-runtime-check"]').trigger('click')
    await flushPromises()
    expect(getRuntime).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(installRuntime).not.toHaveBeenCalled()
  })

  it('keeps old installation information after a failed update and retries explicitly', async () => {
    getRuntime.mockResolvedValueOnce(status({ installed: true, installed_version: '0.164.0', source: 'managed', path: '/old/codex', unavailable_code: 'version_mismatch' }))
    installRuntime.mockRejectedValueOnce(new Error('download-secret'))
    const wrapper = settings()
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-runtime-install"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('codexCliRuntime.installFailed')
    expect(wrapper.get('[data-testid="codex-cli-installed-version"]').text()).toBe('0.164.0')
    expect(wrapper.get('[data-testid="codex-cli-runtime-path"]').text()).toBe('/old/codex')
    expect(wrapper.html()).not.toContain('download-secret')
    await wrapper.get('[data-testid="codex-cli-runtime-install"]').trigger('click')
    await flushPromises()
    expect(installRuntime).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-testid="codex-cli-runtime-readiness"]').text()).toBe('codexCliRuntime.ready')
  })

  it('does not report success when an install response is still unverified', async () => {
    installRuntime.mockResolvedValueOnce(status({ installed: true, installed_version: '0.165.0', version_match: true, unavailable_code: 'source_unverified' }))
    const wrapper = settings()
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-runtime-install"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('codexCliRuntime.installFailed')
    expect(wrapper.text()).not.toContain('codexCliRuntime.installedSuccessfully')
    expect(wrapper.get('[data-testid="codex-cli-runtime-readiness"]').text()).toBe('codexCliRuntime.notReady')
  })

  // Requirement: installing/detecting an official CLI never grants permission
  // to bridge traffic. Only an explicit saved global choice may do so.
  it('keeps the bridge disabled after installation and saves a separate explicit opt-in', async () => {
    const wrapper = settings()
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-runtime-install"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-cli-bridge-toggle"]').attributes('aria-checked')).toBe('false')
    expect(saveBridge).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="codex-cli-bridge-toggle"]').trigger('click')
    expect(saveBridge).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="codex-cli-bridge-save"]').trigger('click')
    await flushPromises()
    expect(saveBridge).toHaveBeenCalledExactlyOnceWith(true)
    expect(wrapper.get('[data-testid="codex-cli-bridge-save"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('codexCliRuntime.bridgeSaved')
  })

  it('can disable an enabled bridge even when the runtime is no longer available', async () => {
    getRuntime.mockResolvedValueOnce(status({ bridge_enabled: true }))
    saveBridge.mockResolvedValueOnce(status({ bridge_enabled: false }))
    const wrapper = settings()
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-cli-bridge-toggle"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="codex-cli-bridge-toggle"]').trigger('click')
    await wrapper.get('[data-testid="codex-cli-bridge-save"]').trigger('click')
    await flushPromises()
    expect(saveBridge).toHaveBeenCalledExactlyOnceWith(false)
    expect(wrapper.get('[data-testid="codex-cli-bridge-toggle"]').attributes('aria-checked')).toBe('false')
    expect(installRuntime).not.toHaveBeenCalled()
  })

  it.each(['source_verified', 'version_match', 'installed', 'execution_available'] as const)(
    'prevents enabling the bridge when %s is false', async field => {
      getRuntime.mockResolvedValueOnce({ ...readyStatus(), [field]: false })
      const wrapper = settings()
      await flushPromises()
      expect(wrapper.get('[data-testid="codex-cli-bridge-toggle"]').attributes('disabled')).toBeDefined()
      await wrapper.get('[data-testid="codex-cli-bridge-toggle"]').trigger('click')
      await wrapper.get('[data-testid="codex-cli-bridge-save"]').trigger('click')
      expect(saveBridge).not.toHaveBeenCalled()
    },
  )

  it('retains a failed bridge draft for explicit retry and never displays private errors', async () => {
    getRuntime.mockResolvedValueOnce(readyStatus())
    saveBridge.mockRejectedValueOnce(new Error('private-auth-token'))
    const wrapper = settings()
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-bridge-toggle"]').trigger('click')
    await wrapper.get('[data-testid="codex-cli-bridge-save"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('codexCliRuntime.bridgeSaveFailed')
    expect(wrapper.html()).not.toContain('private-auth-token')
    expect(wrapper.text()).not.toContain('codexCliRuntime.bridgeSaved')
    expect(wrapper.get('[data-testid="codex-cli-bridge-toggle"]').attributes('aria-checked')).toBe('true')
    await wrapper.get('[data-testid="codex-cli-bridge-save"]').trigger('click')
    await flushPromises()
    expect(saveBridge).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('serializes global saves with runtime checks and ignores a late save after unmount', async () => {
    getRuntime.mockResolvedValueOnce(readyStatus())
    const pending = deferred<CodexCLIRuntimeStatus>()
    saveBridge.mockReturnValueOnce(pending.promise)
    const wrapper = settings()
    await flushPromises()
    await wrapper.get('[data-testid="codex-cli-bridge-toggle"]').trigger('click')
    await wrapper.get('[data-testid="codex-cli-bridge-save"]').trigger('click')
    expect(wrapper.get('[data-testid="codex-cli-runtime-check"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="codex-cli-bridge-save"]').trigger('click')
    await wrapper.get('[data-testid="codex-cli-runtime-check"]').trigger('click')
    expect(saveBridge).toHaveBeenCalledTimes(1)
    expect(getRuntime).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    pending.resolve({ ...readyStatus(), bridge_enabled: true })
    await flushPromises()
    expect(saveBridge).toHaveBeenCalledTimes(1)
  })
})
