import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import RefusalGuardPanel from '../RefusalGuardPanel.vue'

const api = vi.hoisted(() => ({
  getConfig: vi.fn(), updateConfig: vi.fn(), listUsers: vi.fn(), clearUser: vi.fn(),
  listSessions: vi.fn(), clearSession: vi.fn(), clearAll: vi.fn(),
}))
vi.mock('@/api/admin/refusalGuard', () => ({ refusalGuardAPI: api }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const config = () => ({
  enabled: false, user_threshold: 3, count_window_seconds: 300, user_cooldown_seconds: 300,
  session_block_ttl_seconds: 2592000, rules: [{ id: 'policy', name: 'Policy', enabled: true,
    status_codes: [400], error_codes: ['refusal_guard'], error_types: [], message_keywords: [] }],
})
beforeEach(() => {
  vi.clearAllMocks()
  api.getConfig.mockResolvedValue(config())
  api.updateConfig.mockImplementation(async (value) => value)
  api.listUsers.mockResolvedValue([{ user_id: 42, email: '', count: 1, count_ttl_seconds: 300, cooldown_until: '', cooldown_remaining_seconds: 0 }])
  api.listSessions.mockResolvedValue({ data: [], total: 0 })
  api.clearUser.mockResolvedValue(undefined)
  vi.spyOn(window, 'confirm').mockReturnValue(true)
})
describe('RefusalGuardPanel', () => {
  it('loads configuration and state', async () => {
    const wrapper = mount(RefusalGuardPanel)
    await flushPromises()
    expect(api.getConfig).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('#42')
  })
  it('saves its own configuration including TTL days', async () => {
    const wrapper = mount(RefusalGuardPanel)
    await flushPromises()
    await wrapper.find('button.btn-primary').trigger('click')
    await flushPromises()
    expect(api.updateConfig).toHaveBeenCalledWith(expect.objectContaining({ session_block_ttl_seconds: 2592000 }))
  })
  it('clears a user after confirmation', async () => {
    const wrapper = mount(RefusalGuardPanel)
    await flushPromises()
    const clearButton = wrapper.findAll('button').find(button => button.text().trim() === 'admin.riskControl.refusal.clear')
    expect(clearButton).toBeDefined()
    await clearButton!.trigger('click')
    await flushPromises()
    expect(api.clearUser).toHaveBeenCalledWith(42)
  })
})
