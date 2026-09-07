import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ClientDisconnectEventsPanel from '../ClientDisconnectEventsPanel.vue'

const { listClientDisconnectEvents, searchUsers, searchApiKeys, showError } = vi.hoisted(() => ({
  listClientDisconnectEvents: vi.fn(),
  searchUsers: vi.fn(),
  searchApiKeys: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin/usage', () => ({
  adminUsageAPI: { listClientDisconnectEvents, searchUsers, searchApiKeys },
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

function eventResponse(requestId: string, enforce: boolean | undefined) {
  return {
    items: [{
      user_id: 7,
      user_email: 'owner@example.com',
      api_key_id: 11,
      api_key_name: 'Codex Desktop',
      request_id: requestId,
      session_id: 'codex-session-1',
      protocol: 'openai.responses',
      generation: 3,
      sequence: 9,
      outcome: 'client_disconnected' as const,
      completion_status: 'usage_missing' as const,
      usage_missing: true,
      consecutive_after: 10,
      threshold: 10,
      enforce,
      auto_banned: true,
      accepted_at: '2026-09-01T00:00:00Z',
      finalized_at: '2026-09-01T00:00:01Z',
    }],
    total: 1,
    page: 1,
    page_size: 20,
    pages: 1,
  }
}

describe('ClientDisconnectEventsPanel', () => {
  beforeEach(() => {
    listClientDisconnectEvents.mockReset()
    searchUsers.mockReset()
    searchApiKeys.mockReset()
    showError.mockReset()
    searchUsers.mockResolvedValue([{ id: 7, email: 'owner@example.com', deleted: false }])
    searchApiKeys.mockResolvedValue([{ id: 11, name: 'Codex Desktop', user_id: 7 }])
    listClientDisconnectEvents.mockResolvedValue(eventResponse('req-server-owned', true))
  })

  it('shows metadata-only missing-usage and auto-ban lifecycle results', async () => {
    const wrapper = mount(ClientDisconnectEventsPanel, {
      global: { stubs: { Icon: true, Pagination: true } },
    })
    await flushPromises()

    expect(listClientDisconnectEvents).toHaveBeenCalledWith(expect.objectContaining({ page: 1, page_size: 20 }))
    expect(wrapper.text()).toContain('owner@example.com')
    expect(wrapper.text()).toContain('Codex Desktop')
    expect(wrapper.text()).toContain('req-server-owned')
    expect(wrapper.text()).toContain('codex-session-1')
    expect(wrapper.text()).toContain('openai.responses')
    expect(wrapper.text()).toContain('admin.usage.disconnectEvents.usageMissing')
    expect(wrapper.text()).toContain('admin.usage.disconnectEvents.banned')
    const eventCells = wrapper.get('tbody tr').findAll('td')
    expect(eventCells[11]!.text()).toBe('10')
    expect(eventCells[12]!.text()).toBe('10')
    expect(showError).not.toHaveBeenCalled()
  })

  it('forwards validated event filters from the UI', async () => {
    vi.useFakeTimers()
    const wrapper = mount(ClientDisconnectEventsPanel, {
      global: { stubs: { Icon: true, Pagination: true } },
    })
    await flushPromises()
    listClientDisconnectEvents.mockClear()

    const userFilter = wrapper.get('[data-testid="disconnect-user-filter"]')
    await userFilter.trigger('focus')
    await userFilter.setValue('owner')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    await wrapper.findAll('button').find((button) => button.text().includes('owner@example.com'))!.trigger('click')
    await flushPromises()
    expect(listClientDisconnectEvents).toHaveBeenLastCalledWith(expect.objectContaining({ user_id: 7 }))

    await wrapper.get('[data-testid="disconnect-api-key-filter"]').trigger('focus')
    await flushPromises()
    await wrapper.findAll('button').find((button) => button.text().includes('Codex Desktop'))!.trigger('click')
    await wrapper.get('[data-testid="disconnect-request-filter"]').setValue('req-server-owned')
    await wrapper.get('[data-testid="disconnect-session-filter"]').setValue('codex-session-1')
    await wrapper.get('[data-testid="disconnect-protocol-filter"]').setValue('openai.responses')
    await wrapper.get('[data-testid="disconnect-accepted-from"]').setValue('2026-09-01T08:00')
    await wrapper.get('[data-testid="disconnect-accepted-to"]').setValue('2026-09-02T08:00')
    await wrapper.get('[data-testid="disconnect-finalized-from"]').setValue('2026-09-01T08:01')
    await wrapper.get('[data-testid="disconnect-finalized-to"]').setValue('2026-09-02T08:01')
    await wrapper.get('[data-testid="disconnect-outcome-filter"]').setValue('client_disconnected')
    await wrapper.get('[data-testid="disconnect-completion-filter"]').setValue('usage_missing')
    await wrapper.get('[data-testid="disconnect-usage-source-filter"]').setValue('partial')
    await wrapper.get('[data-testid="disconnect-usage-missing-filter"]').setValue('true')
    await wrapper.get('[data-testid="disconnect-enforce-filter"]').setValue('true')
    await wrapper.get('[data-testid="disconnect-auto-ban-filter"]').setValue('true')
    await flushPromises()

    expect(listClientDisconnectEvents).toHaveBeenLastCalledWith(expect.objectContaining({
      user_id: 7,
      api_key_id: 11,
      request_id: 'req-server-owned',
      session_id: 'codex-session-1',
      protocol: 'openai.responses',
      accepted_from: new Date('2026-09-01T08:00').toISOString(),
      accepted_to: new Date('2026-09-02T08:00').toISOString(),
      finalized_from: new Date('2026-09-01T08:01').toISOString(),
      finalized_to: new Date('2026-09-02T08:01').toISOString(),
      outcome: 'client_disconnected',
      completion_status: 'usage_missing',
      usage_source: 'partial',
      usage_missing: true,
      enforce: true,
      auto_banned: true,
      page: 1,
    }))

    listClientDisconnectEvents.mockClear()
    await userFilter.setValue('different-user')
    await flushPromises()
    expect(listClientDisconnectEvents).toHaveBeenLastCalledWith(expect.objectContaining({
      user_id: undefined,
      api_key_id: undefined,
    }))
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('keeps the newest filter response and renders unknown enforcement accurately', async () => {
    let resolveInitial!: (value: ReturnType<typeof eventResponse>) => void
    const initialResponse = new Promise<ReturnType<typeof eventResponse>>((resolve) => {
      resolveInitial = resolve
    })
    listClientDisconnectEvents.mockReset()
    listClientDisconnectEvents
      .mockReturnValueOnce(initialResponse)
      .mockResolvedValueOnce(eventResponse('newest-request', undefined))

    const wrapper = mount(ClientDisconnectEventsPanel, {
      global: { stubs: { Icon: true, Pagination: true } },
    })
    await wrapper.get('[data-testid="disconnect-outcome-filter"]').setValue('client_disconnected')
    await flushPromises()

    let eventCells = wrapper.get('tbody tr').findAll('td')
    expect(eventCells[4]!.text()).toBe('newest-request')
    expect(eventCells[13]!.text()).toBe('-')

    resolveInitial(eventResponse('stale-request', true))
    await flushPromises()
    eventCells = wrapper.get('tbody tr').findAll('td')
    expect(eventCells[4]!.text()).toBe('newest-request')
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
