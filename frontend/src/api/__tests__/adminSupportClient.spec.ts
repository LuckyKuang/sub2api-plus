import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import axios, { type InternalAxiosRequestConfig } from 'axios'
vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
import { apiClient } from '@/api/client'
import { setAdminSupportContext, supportImageFetch } from '@/utils/adminSupportContext'

const success = (config: InternalAxiosRequestConfig, data: unknown) => ({ config, data: { code: 0, data }, status: 200, statusText: 'OK', headers: {} })
describe('complete scoped assistance requests', () => {
  beforeEach(() => { localStorage.clear(); localStorage.setItem('auth_token', 'administrator-session'); setAdminSupportContext({ actorId: 1, userId: 42 }) })
  afterEach(() => { setAdminSupportContext(null); vi.unstubAllGlobals() })
  it('reads complete user keys using the administrator session and retains copy data', async () => {
    const keys = { items: [{ id: 7, user_id: 42, key: 'sk-example-target', ip_whitelist: ['192.0.2.0/24'] }] }
    const adapter = vi.fn(async (config: InternalAxiosRequestConfig) => success(config, keys))
    apiClient.defaults.adapter = adapter
    expect((await apiClient.get('/keys', { params: { search: 'example' } })).data).toEqual(keys)
    expect(adapter.mock.calls[0][0].url).toBe('/admin/support/users/42/keys')
    expect(adapter.mock.calls[0][0].headers.get('Authorization')).toBe('Bearer administrator-session')
    expect(localStorage.getItem('auth_token')).toBe('administrator-session')
    const fetchMock = vi.fn().mockResolvedValue(new Response('{}'))
    vi.stubGlobal('fetch', fetchMock)
    await supportImageFetch('/v1/images/tasks?limit=20', 'sk-example-target', {}, path => '/gateway'+path, path => '/api/v1'+path)
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/admin/support/users/42/images/tasks?limit=20&api_key_id=7')
    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe('Bearer administrator-session')
  })
  it.each(['post','put','patch','delete'] as const)('rejects %s changes before any network call', async method => {
    const adapter = vi.fn()
    apiClient.defaults.adapter = adapter
    await expect(apiClient.request({ method, url: '/keys/7', data: {} })).rejects.toMatchObject({ code: 'ADMIN_SUPPORT_READ_ONLY' })
    expect(adapter).not.toHaveBeenCalled()
  })
  it('preserves batch key usage reads as a GET query with target ownership', async () => {
    const adapter = vi.fn(async (config: InternalAxiosRequestConfig) => success(config, { stats: {} }))
    apiClient.defaults.adapter = adapter
    await apiClient.post('/usage/dashboard/api-keys-usage', { api_key_ids: [7,8] })
    const config = adapter.mock.calls[0][0]
    expect(config.method).toBe('get')
    expect(config.url).toBe('/admin/support/users/42/usage/dashboard/api-keys-usage?api_key_ids=7&api_key_ids=8')
    expect(config.data).toBeUndefined()
  })
  it('rejects stale responses when the target changes', async () => {
    let resolve!: (value: ReturnType<typeof success>) => void
    let config!: InternalAxiosRequestConfig
    apiClient.defaults.adapter = value => { config = value; return new Promise(done => { resolve = done }) }
    const pending = apiClient.get('/user/profile').catch(error => error)
    await Promise.resolve(); await Promise.resolve()
    setAdminSupportContext({ actorId: 1, userId: 43 })
    resolve(success(config, { id: 42 }))
    expect(axios.isCancel(await pending)).toBe(true)
  })
  it('rejects image writes and keys from a previous scope', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    await expect(supportImageFetch('/v1/images/batches', 'old-key', { method: 'POST' }, value => value, value => value)).rejects.toMatchObject({ code: 'ADMIN_SUPPORT_READ_ONLY' })
    await expect(supportImageFetch('/v1/images/tasks', 'old-key', {}, value => value, value => value)).rejects.toThrow('not in the current assistance view')
    expect(fetchMock).not.toHaveBeenCalled()
  })
  it('allows administrator logout while assisting a user', async () => {
    const adapter = vi.fn(async (config: InternalAxiosRequestConfig) => success(config, {}))
    apiClient.defaults.adapter = adapter
    await apiClient.post('/auth/logout')
    expect(adapter.mock.calls[0][0].url).toBe('/auth/logout')
    expect(adapter.mock.calls[0][0].headers.get('Authorization')).toBe('Bearer administrator-session')
  })
  it('routes the real-key Codex configuration catalog through authenticated support discovery', async () => {
    const key = 'sk-guide-fixture'
    apiClient.defaults.adapter = async config => success(config, { items: [{ id: 7, key }] })
    await apiClient.get('/keys')
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ models: [{ slug: 'gpt-fixture' }] })))
    vi.stubGlobal('fetch', fetchMock)
    const { fetchCodexModelsManifest } = await import('@/api/codex')
    expect((await fetchCodexModelsManifest('https://public-base.example.com', key)).modelCount).toBe(1)
    expect(fetchMock.mock.calls[0][0]).toContain('/admin/support/users/42/images/models?client_version=0.158.0&api_key_id=7')
    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe('Bearer administrator-session')
  })
})
