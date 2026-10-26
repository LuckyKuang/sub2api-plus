import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import AccountsView from '../AccountsView.vue'
import type { Account } from '@/types'

const api = vi.hoisted(() => ({ list: vi.fn(), listWithEtag: vi.fn(), getBatchTodayStats: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: api, proxies: { getAll: vi.fn().mockResolvedValue([]) }, groups: { getAllIncludingInactive: vi.fn().mockResolvedValue([]) } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), showInfo: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ token: 'test-token', isSimpleMode: true }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

const Table = { props: ['data'], template: '<div>{{ data.map(row => row.name).join(",") }}</div>' }
const base = (): Account => ({ id: 81, platform: 'grok', type: 'oauth', name: 'Existing login', credentials: {},
  credentials_status: { has_access_token: true, has_refresh_token: true }, oauth_credential_owner: 'a'.repeat(64),
  proxy_id: 31, proxy: { id: 31, name: 'Saved route', host: 'original.example', port: 8080, protocol: 'http', status: 'active' },
  status: 'active', schedulable: true, concurrency: 1, priority: 1, group_ids: [], updated_at: '2026-10-25T10:00:00Z',
} as Account)
function open() {
  return shallowMount(AccountsView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
    DataTable: Table,
    AccountTableActions: { template: '<div><slot name="after" /></div>' },
  } } })
}
beforeEach(() => {
  vi.useFakeTimers()
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  vi.clearAllMocks()
  localStorage.clear()
  localStorage.setItem('account-auto-refresh', JSON.stringify({ enabled: true, interval_seconds: 5 }))
  api.list.mockResolvedValue({ items: [base()], total: 1, pages: 1 })
  api.getBatchTodayStats.mockResolvedValue({ stats: {} })
})
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

describe('account list automatic refresh credential-owner propagation', () => {
  it.each(['redacted credential owner', 'same proxy endpoint'])('%s reaches table consumers even when account updated_at and credential-presence flags are unchanged', async boundary => {
    const next = { ...base(), oauth_credential_owner: 'b'.repeat(64) }
    if (boundary === 'same proxy endpoint') next.proxy = { ...next.proxy!, host: 'repaired.example' }
    api.listWithEtag.mockResolvedValue({ notModified: false, etag: 'next-owner', data: { items: [next], total: 1, pages: 1 } })
    const view = open()
    await flushPromises()
    const original = view.getComponent(Table).props('data')[0] as Account
    expect(original.oauth_credential_owner).toBe('a'.repeat(64))
    await vi.advanceTimersByTimeAsync(6000)
    await flushPromises()
    expect(api.listWithEtag).toHaveBeenCalledOnce()
    const refreshed = view.getComponent(Table).props('data')[0] as Account
    expect(refreshed.oauth_credential_owner).toBe('b'.repeat(64))
    expect(refreshed.updated_at).toBe('2026-10-25T10:00:00Z')
    expect(refreshed.credentials_status).toEqual({ has_access_token: true, has_refresh_token: true })
    expect(refreshed.proxy?.host).toBe(boundary === 'same proxy endpoint' ? 'repaired.example' : 'original.example')
    expect(refreshed).not.toBe(original)
    expect(api.list).toHaveBeenCalledOnce()
  })

  it('preserves the current owner when only account-observation freshness changes', async () => {
    api.listWithEtag.mockResolvedValue({ notModified: false, etag: 'fresh-observation', data: { items: [{ ...base(), extra: { quota_fetched_at: 1999999999 } }], total: 1, pages: 1 } })
    const view = open()
    await flushPromises()
    const original = view.getComponent(Table).props('data')[0]
    await vi.advanceTimersByTimeAsync(6000)
    await flushPromises()
    expect(api.listWithEtag).toHaveBeenCalledOnce()
    expect(view.getComponent(Table).props('data')[0]).toBe(original)
    expect((view.getComponent(Table).props('data')[0] as Account).oauth_credential_owner).toBe('a'.repeat(64))
  })
})
