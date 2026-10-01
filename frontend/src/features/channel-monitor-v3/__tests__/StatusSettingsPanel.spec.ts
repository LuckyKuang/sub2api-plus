import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import StatusSettingsPanel from '../StatusSettingsPanel.vue'
import { getStatusConfig, updateStatusConfig } from '@/api/channelMonitorV3'

const app = vi.hoisted(() => ({ showError: vi.fn(), showSuccess: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => app }))
vi.mock('@/api/channelMonitorV3', () => ({ getStatusConfig: vi.fn(), updateStatusConfig: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const config = { version: 3, minimum_samples: 5, warning_error_rate: .05, outage_error_rate: .9, warning_ttft_ms: 5000, abnormal_windows: 2, recovery_windows: 3 }
describe('V3 configuration', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(getStatusConfig).mockImplementation(async () => ({ ...config }))
    vi.mocked(updateStatusConfig).mockResolvedValue({ ...config, version: 4 })
  })
  it('saves the loaded version and uses the returned version for subsequent edits', async () => {
    const wrapper = mount(StatusSettingsPanel); await flushPromises()
    await wrapper.find('input').setValue('8')
    await wrapper.find('form').trigger('submit'); await flushPromises()
    expect(updateStatusConfig).toHaveBeenCalledWith({ ...config, minimum_samples: 8 })
    await wrapper.find('form').trigger('submit'); await flushPromises()
    expect(updateStatusConfig).toHaveBeenLastCalledWith({ ...config, version: 4 })
    expect(app.showSuccess).toHaveBeenCalled()
    wrapper.unmount()
  })
  it('preserves the draft after a conflict and allows explicit reload', async () => {
    const wrapper = mount(StatusSettingsPanel); await flushPromises()
    vi.mocked(updateStatusConfig).mockRejectedValue({ status: 409, message: 'configuration changed' })
    await wrapper.find('input').setValue('9')
    await wrapper.find('form').trigger('submit'); await flushPromises()
    expect(app.showError).toHaveBeenCalled()
    expect((wrapper.find('input').element as HTMLInputElement).value).toBe('9')
    await wrapper.find('button[type="button"]').trigger('click'); await flushPromises()
    expect((wrapper.find('input').element as HTMLInputElement).value).toBe('5')
    wrapper.unmount()
  })
})
