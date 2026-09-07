import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ClientDisconnectEventsPanel from '../ClientDisconnectEventsPanel.vue'

const { listClientDisconnectEvents, showError } = vi.hoisted(() => ({
  listClientDisconnectEvents: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin/usage', () => ({
  adminUsageAPI: { listClientDisconnectEvents },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError }),
}))

vi.mock('@/utils/apiError', () => ({
  extractApiErrorMessage: (_error: unknown, fallback: string) => fallback,
}))

vi.mock('@/utils/format', () => ({
  formatDateTime: (value: string) => value,
}))

vi.mock('@/composables/usePersistedPageSize', () => ({
  getPersistedPageSize: () => 20,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

describe('ClientDisconnectEventsPanel', () => {
  beforeEach(() => {
    listClientDisconnectEvents.mockReset()
    showError.mockReset()
    listClientDisconnectEvents.mockResolvedValue({
      items: [{
        user_id: 7,
        api_key_id: 11,
        request_id: 'req-server-owned',
        protocol: 'openai.responses',
        generation: 3,
        sequence: 9,
        outcome: 'client_disconnected',
        completion_status: 'usage_missing',
        usage_missing: true,
        consecutive_after: 10,
        threshold: 10,
        enforce: true,
        auto_banned: true,
        accepted_at: '2026-09-01T00:00:00Z',
        finalized_at: '2026-09-01T00:00:01Z',
      }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
  })

  it('shows metadata-only missing-usage and auto-ban lifecycle results', async () => {
    const wrapper = mount(ClientDisconnectEventsPanel, {
      global: { stubs: { Icon: true, Pagination: true } },
    })
    await flushPromises()

    expect(listClientDisconnectEvents).toHaveBeenCalledWith(expect.objectContaining({ page: 1, page_size: 20 }))
    expect(wrapper.text()).toContain('req-server-owned')
    expect(wrapper.text()).toContain('openai.responses')
    expect(wrapper.text()).toContain('admin.usage.disconnectEvents.usageMissing')
    expect(wrapper.text()).toContain('admin.usage.disconnectEvents.banned')
    expect(wrapper.text()).toContain('10 / 10')
    expect(showError).not.toHaveBeenCalled()
  })

  it('forwards validated event filters from the UI', async () => {
    const wrapper = mount(ClientDisconnectEventsPanel, {
      global: { stubs: { Icon: true, Pagination: true } },
    })
    await flushPromises()
    listClientDisconnectEvents.mockClear()

    const numberInputs = wrapper.findAll('input[type="number"]')
    await numberInputs[0]!.setValue('7')
    await numberInputs[1]!.setValue('11')
    const selects = wrapper.findAll('select')
    await selects[0]!.setValue('client_disconnected')
    await selects[1]!.setValue('usage_missing')
    await selects[2]!.setValue('true')
    await selects[3]!.setValue('true')
    await flushPromises()

    expect(listClientDisconnectEvents).toHaveBeenLastCalledWith(expect.objectContaining({
      user_id: 7,
      api_key_id: 11,
      outcome: 'client_disconnected',
      completion_status: 'usage_missing',
      usage_missing: true,
      auto_banned: true,
      page: 1,
    }))
  })
})
