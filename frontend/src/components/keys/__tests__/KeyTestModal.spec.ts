import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

import KeyTestModal from '../KeyTestModal.vue'
import { setAdminSupportContext } from '@/utils/adminSupportContext'
import { KEY_TEST_PROMPT, KEY_TEST_TIMEOUT_MS } from '@/api/keyTest'
import type { ApiKey, Group } from '@/types'
import en from '@/i18n/locales/en'

const { getNativeAccess } = vi.hoisted(() => ({ getNativeAccess: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { get: getNativeAccess } }))

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

const group = (overrides: Partial<Group> = {}): Group => ({
  id: 1,
  name: 'OpenAI',
  description: null,
  platform: 'openai',
  rate_multiplier: 1,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'standard',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  five_hour_limit_usd: null,
  long_context_pricing_enabled: false,
  allow_image_generation: false,
  allow_batch_image_generation: false,
  image_count_billing: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  batch_image_discount_multiplier: 1,
  batch_image_hold_multiplier: 1,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  video_rate_independent: false,
  video_rate_multiplier: 1,
  video_price_480p: null,
  video_price_720p: null,
  video_price_1080p: null,
  web_search_price_per_call: null,
  search_price_per_1k: null,
  audio_realtime_price_per_min: null,
  audio_tts_price_per_million_chars: null,
  audio_stt_price_per_hour: null,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  ...overrides,
})

const makeKey = (overrides: Partial<ApiKey> = {}): ApiKey => ({
  id: 7,
  user_id: 1,
  key: 'sk-test-key',
  name: 'test-key',
  group_id: 1,
  group: group(),
  group_binding_mode: 'single',
  status: 'active',
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  last_used_ip: null,
  quota: 0,
  quota_used: 0,
  expires_at: null,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  current_concurrency: 0,
  rate_limit_5h: 0,
  rate_limit_1d: 0,
  rate_limit_7d: 0,
  usage_5h: 0,
  usage_1d: 0,
  usage_7d: 0,
  window_5h_start: null,
  window_1d_start: null,
  window_7d_start: null,
  reset_5h_at: null,
  reset_1d_at: null,
  reset_7d_at: null,
  ...overrides,
})

const access = (models: Array<Record<string, unknown>>) => ({
  data: {
    endpoints: [],
    conversion_endpoints: [],
    models: models.map(model => ({
      name: 'model',
      platform: 'openai',
      endpoints: [],
      conversion_endpoints: [],
      ...model,
    })),
  },
})

const gatewayResponse = (
  body: unknown,
  status = 200,
  headers: Record<string, string> = {},
) => ({
  ok: status >= 200 && status < 300,
  status,
  headers: {
    get: (name: string) => {
      const key = Object.keys(headers).find(candidate => candidate.toLowerCase() === name.toLowerCase())
      return key ? headers[key] : null
    },
  },
  text: async () => typeof body === 'string' ? body : JSON.stringify(body),
}) as unknown as Response

const fetchMock = vi.fn()
const BaseDialogStub = {
  props: ['show', 'title', 'width'],
  emits: ['close'],
  template: '<div v-if="show" role="dialog"><slot /><slot name="footer" /></div>',
}
const SelectStub = {
  name: 'Select',
  props: ['modelValue', 'options', 'disabled', 'placeholder'],
  emits: ['update:modelValue'],
  template: '<select :value="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>',
}
const IconStub = { props: ['name', 'size'], template: '<span />' }

const mountModal = async (key: ApiKey = makeKey()) => {
  const wrapper = mount(KeyTestModal, {
    props: { show: true, apiKey: key, baseUrl: 'https://api.example.com' },
    global: {
      plugins: [i18n],
      stubs: { BaseDialog: BaseDialogStub, Select: SelectStub, Icon: IconStub },
    },
  })
  await flushPromises()
  return wrapper
}

const startButton = (wrapper: VueWrapper) => wrapper.get('[data-testid="key-test-start"]')

beforeEach(() => {
  getNativeAccess.mockReset()
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
  setAdminSupportContext(null)
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  setAdminSupportContext(null)
})

describe('KeyTestModal capability discovery', () => {
  it('opens the selected row, loads candidates, and sends no inference', async () => {
    getNativeAccess.mockResolvedValue(access([{ name: 'gpt-5', endpoints: ['/v1/responses'] }]))
    const wrapper = await mountModal()

    expect(getNativeAccess).toHaveBeenCalledWith('/groups/1/native-access', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="key-test-summary"]').text()).toContain('test-key')
    expect(wrapper.get('[data-testid="key-test-scope"]').text()).toBe('Group: OpenAI')
    expect(wrapper.get('[data-testid="key-test-endpoint"]').text()).toContain('/v1/responses')
    expect(startButton(wrapper).attributes()).not.toHaveProperty('disabled')
    wrapper.unmount()
  })

  it('uses the aggregate key capability endpoint and sends no group header', async () => {
    getNativeAccess.mockResolvedValue(access([{ name: 'gpt-5', endpoints: ['/v1/responses'] }]))
    const wrapper = await mountModal(makeKey({ group_id: null, group: undefined, group_binding_mode: 'all' }))
    fetchMock.mockResolvedValue(gatewayResponse({ output_text: 'OK' }))
    await startButton(wrapper).trigger('click')
    await flushPromises()

    expect(getNativeAccess).toHaveBeenCalledWith('/keys/7/native-access', expect.anything())
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(Object.keys(init.headers as Record<string, string>)).not.toContain('X-Composite-Group-ID')
    expect(wrapper.get('[data-testid="key-test-result"]').text()).toContain('Test passed')
    wrapper.unmount()
  })

  it('shows an empty scope for a custom aggregate key without members', async () => {
    getNativeAccess.mockResolvedValue(access([]))
    const wrapper = await mountModal(makeKey({ group_id: null, group: undefined, group_binding_mode: 'custom', aggregate_group_ids: [] }))
    expect(wrapper.get('[data-testid="key-test-no-capability"]').text()).toBe('This aggregate key currently has no available models.')
    expect(startButton(wrapper).attributes()).toHaveProperty('disabled')
    wrapper.unmount()
  })

  it('reports a capability load failure without judging the key invalid and retries', async () => {
    getNativeAccess.mockRejectedValueOnce(new Error('temporary'))
    const wrapper = await mountModal()
    expect(wrapper.get('[data-testid="key-test-load-error"]').text()).toContain('This does not mean the key is invalid')
    expect(startButton(wrapper).attributes()).toHaveProperty('disabled')

    getNativeAccess.mockResolvedValueOnce(access([{ name: 'gpt-5', endpoints: ['/v1/responses'] }]))
    await wrapper.get('[data-testid="key-test-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="key-test-load-error"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="key-test-endpoint"]').text()).toContain('/v1/responses')
    wrapper.unmount()
  })

  it('limits Composite conversion candidates to the configured default endpoint', async () => {
    getNativeAccess.mockResolvedValue(access([
      { name: 'unconverted', endpoints: ['/v1/responses'], conversion_endpoints: ['/v1/responses'] },
      { name: 'converted', endpoints: ['/v1/responses'], conversion_endpoints: ['/v1/messages'] },
    ]))
    const wrapper = await mountModal(makeKey({
      group_id: 4,
      group: group({ id: 4, platform: 'composite' }),
      conversion_enabled: true,
      default_endpoint: '/v1/messages',
    }))
    expect(wrapper.findAll('option').map(option => option.text())).toEqual(['converted'])
    fetchMock.mockResolvedValue(gatewayResponse({ content: [{ type: 'text', text: 'OK' }] }))
    await startButton(wrapper).trigger('click')
    await flushPromises()
    const [url] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('https://api.example.com/v1/messages')
    wrapper.unmount()
  })

  it('uses native endpoints for an unconverted Composite key', async () => {
    getNativeAccess.mockResolvedValue(access([{ name: 'gpt-5', endpoints: ['/v1/responses'], conversion_endpoints: ['/v1/messages'] }]))
    const wrapper = await mountModal(makeKey({
      group_id: 4,
      group: group({ id: 4, platform: 'composite' }),
      conversion_enabled: false,
      default_endpoint: '/v1/messages',
    }))
    expect(wrapper.get('[data-testid="key-test-endpoint"]').text()).toContain('/v1/responses')
    wrapper.unmount()
  })

  it('uses the Antigravity platform path prefix for a single group', async () => {
    getNativeAccess.mockResolvedValue(access([{ name: 'claude-sonnet', endpoints: ['/v1/messages'] }]))
    const wrapper = await mountModal(makeKey({
      group_id: 6,
      group: group({ id: 6, name: 'Antigravity', platform: 'antigravity' }),
    }))
    expect(wrapper.get('[data-testid="key-test-endpoint"]').text()).toContain('/antigravity/v1/messages')
    fetchMock.mockResolvedValue(gatewayResponse({ content: [{ type: 'text', text: 'OK' }] }))
    await startButton(wrapper).trigger('click')
    await flushPromises()
    expect((fetchMock.mock.calls[0] as [string, RequestInit])[0])
      .toBe('https://api.example.com/antigravity/v1/messages')
    expect(wrapper.get('[data-testid="key-test-result"]').text()).toContain('Test passed')
    wrapper.unmount()
  })
})

describe('KeyTestModal blocked states', () => {
  it.each([
    { name: 'disabled', key: makeKey({ status: 'inactive' }), expected: 'This key is disabled. Enable it before testing.' },
    { name: 'expired status', key: makeKey({ status: 'expired' }), expected: 'This key has expired.' },
    { name: 'expired date', key: makeKey({ expires_at: '2020-01-01T00:00:00Z' }), expected: 'This key has expired.' },
    { name: 'exhausted quota', key: makeKey({ quota: 10, quota_used: 10 }), expected: 'This key’s quota is exhausted.' },
    { name: 'ungrouped', key: makeKey({ group_id: null, group: undefined }), expected: 'This key is not bound to a group. Testing requires a group or aggregate scope.' },
    { name: 'dedicated client', key: makeKey({ group: group({ claude_code_only: true }) }), expected: 'This group requires a dedicated client, so the browser test is unavailable.' },
    { name: 'unsupported conversion', key: makeKey({ group: group({ platform: 'composite' }), conversion_enabled: true, default_endpoint: '/v1/systemone' }), expected: 'This key’s conversion endpoint is not supported by browser testing.' },
  ])('blocks $name without loading or sending inference', async ({ key, expected }) => {
    const wrapper = await mountModal(key)
    expect(wrapper.get('[data-testid="key-test-blocked"]').text()).toBe(expected)
    expect(getNativeAccess).not.toHaveBeenCalled()
    expect(fetchMock).not.toHaveBeenCalled()
    expect(startButton(wrapper).attributes()).toHaveProperty('disabled')
    wrapper.unmount()
  })

  it('blocks read-only assistance', async () => {
    setAdminSupportContext({ actorId: 1, userId: 42 })
    const wrapper = await mountModal()
    expect(wrapper.get('[data-testid="key-test-blocked"]').text()).toBe('Read-only assistance cannot send a test request.')
    expect(getNativeAccess).not.toHaveBeenCalled()
    expect(fetchMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('KeyTestModal keyboard contract', () => {
  it('closes on Escape through the shared dialog behavior', async () => {
    getNativeAccess.mockResolvedValue(access([{ name: 'gpt-5', endpoints: ['/v1/responses'] }]))
    const wrapper = mount(KeyTestModal, {
      attachTo: document.body,
      props: { show: true, apiKey: makeKey(), baseUrl: 'https://api.example.com' },
      global: {
        plugins: [i18n],
        stubs: { Select: SelectStub, Icon: IconStub, Teleport: true },
      },
    })
    await flushPromises()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await flushPromises()
    expect(wrapper.emitted('close')).toBeTruthy()
    wrapper.unmount()
  })
})

describe('KeyTestModal inference', () => {
  it('runs one Responses request with the row key and shows model plus latency', async () => {
    getNativeAccess.mockResolvedValue(access([{ name: 'gpt-5', endpoints: ['/v1/responses'] }]))
    fetchMock.mockResolvedValue(gatewayResponse({
      output: [{ type: 'message', role: 'assistant', content: [{ type: 'output_text', text: 'OK' }] }],
    }))
    const wrapper = await mountModal()
    await startButton(wrapper).trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('https://api.example.com/v1/responses')
    expect(url).not.toContain('sk-test-key')
    expect(init).toMatchObject({ method: 'POST', credentials: 'omit' })
    expect(init.headers).toEqual({
      Accept: 'application/json',
      'Content-Type': 'application/json',
      Authorization: 'Bearer sk-test-key',
    })
    expect(JSON.parse(String(init.body))).toMatchObject({
      model: 'gpt-5',
      input: KEY_TEST_PROMPT,
      stream: false,
    })
    expect(wrapper.get('[data-testid="key-test-result"]').text()).toContain('Test passed')
    expect(wrapper.get('[data-testid="key-test-success-detail"]').text()).toContain('gpt-5')
    expect(wrapper.get('[data-testid="key-test-success-detail"]').text()).toContain('ms')
    wrapper.unmount()
  })

  it('does not require the example word in the output', async () => {
    getNativeAccess.mockResolvedValue(access([{ name: 'gpt-5', endpoints: ['/v1/responses'] }]))
    fetchMock.mockResolvedValue(gatewayResponse({ output_text: 'Sure, the key works.' }))
    const wrapper = await mountModal()
    await startButton(wrapper).trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="key-test-result"]').text()).toContain('Test passed')
    wrapper.unmount()
  })

  it.each([
    {
      name: 'Responses',
      model: 'gpt-5',
      endpoint: '/v1/responses',
      url: 'https://api.example.com/v1/responses',
      response: { output: [{ type: 'message', role: 'assistant', content: [{ type: 'output_text', text: 'OK' }] }] },
    },
    {
      name: 'Chat Completions',
      model: 'gpt-4o',
      endpoint: '/v1/chat/completions',
      url: 'https://api.example.com/v1/chat/completions',
      response: { choices: [{ message: { role: 'assistant', content: 'OK' }, finish_reason: 'stop' }] },
    },
    {
      name: 'Messages',
      model: 'claude-sonnet',
      endpoint: '/v1/messages',
      url: 'https://api.example.com/v1/messages',
      response: { id: 'msg_1', type: 'message', role: 'assistant', content: [{ type: 'text', text: 'OK' }] },
    },
    {
      name: 'Gemini',
      model: 'gemini-2.5-pro',
      endpoint: '/v1beta',
      url: 'https://api.example.com/v1beta/models/gemini-2.5-pro:generateContent',
      response: { candidates: [{ content: { role: 'model', parts: [{ text: 'OK' }] }, finishReason: 'STOP' }] },
    },
  ])('accepts a real $name success shape', async ({ model, endpoint, url, response }) => {
    getNativeAccess.mockResolvedValue(access([{ name: model, endpoints: [endpoint] }]))
    fetchMock.mockResolvedValue(gatewayResponse(response))
    const wrapper = await mountModal()
    await startButton(wrapper).trigger('click')
    await flushPromises()

    expect((fetchMock.mock.calls[0] as [string, RequestInit])[0]).toBe(url)
    expect(wrapper.get('[data-testid="key-test-result"]').text()).toContain('Test passed')
    expect(wrapper.get('[data-testid="key-test-success-detail"]').text()).toContain(model)
    wrapper.unmount()
  })

  it.each([
    { name: 'Responses', model: 'gpt-5', endpoint: '/v1/responses', response: { output: [] } },
    { name: 'Chat Completions', model: 'gpt-4o', endpoint: '/v1/chat/completions', response: { choices: [{ message: { content: null, tool_calls: [{ id: 'tool' }] } }] } },
    { name: 'Messages', model: 'claude-sonnet', endpoint: '/v1/messages', response: { content: [{ type: 'thinking', thinking: 'private' }] } },
    { name: 'Gemini', model: 'gemini-2.5-pro', endpoint: '/v1beta', response: { candidates: [{ content: { parts: [{ thought: true, text: 'reasoning' }] }, finishReason: 'STOP' }] } },
  ])('rejects a $name response without output text', async ({ model, endpoint, response }) => {
    getNativeAccess.mockResolvedValue(access([{ name: model, endpoints: [endpoint] }]))
    fetchMock.mockResolvedValue(gatewayResponse(response))
    const wrapper = await mountModal()
    await startButton(wrapper).trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="key-test-result"]').text()).toContain('Test failed')
    expect(wrapper.get('[data-testid="key-test-failure-reason"]').text()).toBe('Unable to confirm a valid text response.')
    wrapper.unmount()
  })

  it('treats an error envelope with HTTP 200 as a failure', async () => {
    getNativeAccess.mockResolvedValue(access([{ name: 'gpt-5', endpoints: ['/v1/responses'] }]))
    fetchMock.mockResolvedValue(gatewayResponse({ error: { type: 'insufficient_quota' } }))
    const wrapper = await mountModal()
    await startButton(wrapper).trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="key-test-failure-reason"]').text()).toBe('Balance or quota is insufficient. Add balance or adjust the key limits.')
    wrapper.unmount()
  })

  it.each([
    { status: 401, body: { error: { type: 'authentication_error' } }, expected: 'The gateway rejected this key. Check that it is still enabled and valid.' },
    { status: 403, body: { error: { message: 'API key is not assigned to any group' } }, expected: 'This key cannot access the selected model or group.' },
    { status: 403, body: { error: { message: 'Insufficient account balance' } }, expected: 'Balance or quota is insufficient. Add balance or adjust the key limits.' },
    { status: 403, body: { error: { code: 'content_policy_violation' } }, expected: 'The gateway security audit rejected this test request. Contact the administrator if this was unexpected.' },
    { status: 403, body: { error: { message: 'This account only allows approved Codex client profiles' } }, expected: 'This model requires a dedicated client, so the browser test cannot confirm it.' },
    { status: 404, body: { error: { code: 'model_not_found' } }, expected: 'The selected model is unavailable for this key.' },
    { status: 429, body: { error: { type: 'rate_limit_error' } }, expected: 'The request was rate limited. Try again later.' },
    { status: 502, body: { error: { type: 'api_error' } }, expected: 'The upstream service or gateway is temporarily unavailable. Try again later.' },
    { status: 400, body: { error: { type: 'invalid_request_error' } }, expected: 'The gateway rejected the test request. Choose another model or contact the administrator.' },
  ])('maps HTTP $status to an actionable reason', async ({ status, body, expected }) => {
    getNativeAccess.mockResolvedValue(access([{ name: 'gpt-5', endpoints: ['/v1/responses'] }]))
    fetchMock.mockResolvedValue(gatewayResponse(body, status, { 'X-Request-ID': 'req-from-header' }))
    const wrapper = await mountModal()
    await startButton(wrapper).trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="key-test-failure-reason"]').text()).toBe(expected)
    expect(wrapper.get('[data-testid="key-test-request-id"]').text()).toContain('req-from-header')
    wrapper.unmount()
  })

  it('shows a request ID from the error payload when no header is readable', async () => {
    getNativeAccess.mockResolvedValue(access([{ name: 'gpt-5', endpoints: ['/v1/responses'] }]))
    fetchMock.mockResolvedValue(gatewayResponse({
      error: { type: 'api_error' },
      request_id: 'body-request-id',
    }, 500))
    const wrapper = await mountModal()
    await startButton(wrapper).trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="key-test-request-id"]').text()).toContain('body-request-id')
    wrapper.unmount()
  })

  it('reports a network failure as unable to confirm without a request ID', async () => {
    getNativeAccess.mockResolvedValue(access([{ name: 'gpt-5', endpoints: ['/v1/responses'] }]))
    fetchMock.mockRejectedValue(new TypeError('Failed to fetch'))
    const wrapper = await mountModal()
    await startButton(wrapper).trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="key-test-failure-reason"]').text()).toBe('Unable to confirm: the network request failed or the cross-origin response was unreadable.')
    expect(wrapper.find('[data-testid="key-test-request-id"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('reports a timeout as unable to confirm', async () => {
    getNativeAccess.mockResolvedValue(access([{ name: 'gpt-5', endpoints: ['/v1/responses'] }]))
    const wrapper = await mountModal()
    fetchMock.mockImplementation((_url: string, init: RequestInit) => new Promise((_resolve, reject) => {
      init.signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
    }))
    vi.useFakeTimers()
    await startButton(wrapper).trigger('click')
    await vi.advanceTimersByTimeAsync(KEY_TEST_TIMEOUT_MS)

    expect(wrapper.get('[data-testid="key-test-failure-reason"]').text()).toBe('Unable to confirm: the request timed out.')
    wrapper.unmount()
  })

  it('submits once when the start button is clicked repeatedly', async () => {
    getNativeAccess.mockResolvedValue(access([{ name: 'gpt-5', endpoints: ['/v1/responses'] }]))
    let resolveFetch!: (response: Response) => void
    fetchMock.mockReturnValue(new Promise(resolve => { resolveFetch = resolve }))
    const wrapper = await mountModal()
    await startButton(wrapper).trigger('click')
    await startButton(wrapper).trigger('click')
    expect(fetchMock).toHaveBeenCalledTimes(1)
    resolveFetch(gatewayResponse({ output_text: 'OK' }))
    await flushPromises()
    wrapper.unmount()
  })

  it('discards a late response after the dialog closes', async () => {
    getNativeAccess.mockResolvedValue(access([{ name: 'gpt-5', endpoints: ['/v1/responses'] }]))
    let resolveFetch!: (response: Response) => void
    fetchMock.mockReturnValue(new Promise(resolve => { resolveFetch = resolve }))
    const wrapper = await mountModal()
    await startButton(wrapper).trigger('click')
    await wrapper.setProps({ show: false })
    resolveFetch(gatewayResponse({ output_text: 'OK' }))
    await flushPromises()

    expect(wrapper.find('[data-testid="key-test-result"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Test passed')
    wrapper.unmount()
  })

  it('discards the previous row result when the dialog switches rows', async () => {
    getNativeAccess
      .mockResolvedValueOnce(access([{ name: 'gpt-5', endpoints: ['/v1/responses'] }]))
      .mockResolvedValueOnce(access([{ name: 'claude-sonnet', endpoints: ['/v1/messages'] }]))
    let resolveFirst!: (response: Response) => void
    fetchMock.mockReturnValue(new Promise(resolve => { resolveFirst = resolve }))
    const wrapper = await mountModal()
    await startButton(wrapper).trigger('click')
    await wrapper.setProps({ apiKey: makeKey({ id: 8, name: 'other-key', group_id: 2, group: group({ id: 2, name: 'Anthropic', platform: 'anthropic' }) }) })
    await flushPromises()
    resolveFirst(gatewayResponse({ output_text: 'OK' }))
    await flushPromises()

    expect(wrapper.find('[data-testid="key-test-result"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="key-test-summary"]').text()).toContain('other-key')
    expect(getNativeAccess).toHaveBeenLastCalledWith('/groups/2/native-access', expect.anything())
    wrapper.unmount()
  })

  it('clears the previous conclusion when the model changes', async () => {
    getNativeAccess.mockResolvedValue(access([
      { name: 'gpt-5', endpoints: ['/v1/responses'] },
      { name: 'claude-sonnet', endpoints: ['/v1/messages'] },
    ]))
    fetchMock.mockResolvedValue(gatewayResponse({ output_text: 'OK' }))
    const wrapper = await mountModal()
    await startButton(wrapper).trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="key-test-result"]').exists()).toBe(true)

    wrapper.getComponent(SelectStub).vm.$emit('update:modelValue', 'claude-sonnet')
    await flushPromises()
    expect(wrapper.find('[data-testid="key-test-result"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
