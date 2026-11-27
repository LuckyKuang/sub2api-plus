import { describe, expect, it } from 'vitest'
import {
  KEY_TEST_MAX_TOKENS,
  KEY_TEST_PROMPT,
  KEY_TEST_IMAGE_TIMEOUT_MS,
  accessPathForKey,
  buildCandidates,
  buildTestRequest,
  classifyFailure,
  extractOutputText,
  extractOutputImages,
  isImageTestCandidate,
  hasErrorEnvelope,
  normalizeGatewayRoot,
  requestIdFromPayload,
  type NativeAccessModel,
} from '../keyTest'
import type { ApiKey } from '@/types'

const model = (overrides: Partial<NativeAccessModel>): NativeAccessModel => ({
  name: 'model',
  platform: 'openai',
  endpoints: [],
  conversion_endpoints: [],
  ...overrides,
})

const apiKey = (overrides: Partial<ApiKey> = {}): ApiKey => ({
  id: 7,
  user_id: 1,
  key: 'sk-test',
  name: 'test',
  group_id: null,
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

describe('key test request helpers', () => {
  it('uses hi for text and allows five minutes for generated images', () => {
    expect(KEY_TEST_PROMPT).toBe('hi')
    expect(KEY_TEST_IMAGE_TIMEOUT_MS).toBe(300_000)
  })

  it('keeps a text model as the initial choice when image models are available', () => {
    expect(buildCandidates([
      model({ name: 'gpt-image-1', endpoints: ['/v1/images/generations'] }),
      model({ name: 'gpt-5.4', endpoints: ['/v1/responses'] }),
    ])[0]?.model).toBe('gpt-5.4')
  })

  it('includes advertised image routes and keeps their exact aggregate owner', () => {
    const candidates = buildCandidates([
      model({ name: 'shared-image', platform: 'gemini', endpoints: ['/v1beta'], group_ids: [1], is_image: true }),
      model({ name: 'shared-image', platform: 'openai', endpoints: ['/v1/images/generations'], group_ids: [2], is_image: true }),
      model({ name: 'gpt-5', endpoints: ['/v1/responses'], group_ids: [3] }),
    ])
    const image = candidates.find(candidate => candidate.model === 'shared-image')!
    expect(image).toMatchObject({ protocol: 'images', isImage: true, groupId: 2, platform: 'openai' })
    const request = buildTestRequest('https://api.example.com', 'sk-test', image, 'Draw an orange cat.')
    expect(request.url).toBe('https://api.example.com/v1/images/generations')
    expect(request.headers['X-Composite-Group-ID']).toBe('2')
    expect(JSON.parse(request.body)).toEqual({ model: 'shared-image', prompt: 'Draw an orange cat.', n: 1 })
    expect(buildTestRequest('', 'sk-test', candidates.find(candidate => candidate.model === 'gpt-5')!).headers)
      .not.toHaveProperty('X-Composite-Group-ID')
  })

  it('pins the image group when the same platform exposes text and images under one public model', () => {
    const candidate = buildCandidates([model({
      name: 'gpt-image-1', platform: 'openai',
      endpoints: ['/v1/responses', '/v1/images/generations'],
      group_ids: [1, 2], image_group_ids: [2], is_image: true,
    })])[0]!
    expect(candidate).toMatchObject({ protocol: 'images', groupId: 2 })
    expect(buildTestRequest('', 'sk-test', candidate, 'Draw a cat.').headers['X-Composite-Group-ID']).toBe('2')
  })

  it('never invents an image endpoint from a model name', () => {
    const candidate = buildCandidates([model({ name: 'gpt-image-1', endpoints: ['/v1/responses'] })])[0]!
    expect(candidate.endpoint).toBe('/v1/responses')
    expect(isImageTestCandidate(candidate)).toBe(true)
    expect(buildCandidates([model({ name: 'gpt-image-1', endpoints: [] })])).toEqual([])
    expect(buildCandidates([model({ name: 'gpt-image-1', endpoints: ['/v1/images/generations'] })], {
      conversionEndpoint: '/v1/messages',
    })).toEqual([])
  })

  it('uses resolved image metadata and provider ownership instead of guessing across platforms', () => {
    for (const modelEntry of [
      model({ name: 'gpt-image-1', platform: 'anthropic', endpoints: ['/v1/messages'] }),
      model({ name: 'gpt-image-1', platform: 'openai', endpoints: ['/v1/responses'], is_image: false }),
    ]) {
      expect(isImageTestCandidate(buildCandidates([modelEntry])[0]!)).toBe(false)
    }
  })

  it('sends native GPT, Grok and Gemini image requests without text token limits', () => {
    const prompt = 'Generate a cute orange cat astronaut sticker on a clean pastel background.'
    const images = buildCandidates([
      model({ name: 'gpt-image-1', endpoints: ['/v1/images/generations'] }),
      model({ name: 'public-art', platform: 'grok', endpoints: ['/v1/images/generations'], is_image: true }),
      model({ name: 'public-gemini-art', platform: 'gemini', endpoints: ['/v1beta'], is_image: true }),
    ])
    const gpt = JSON.parse(buildTestRequest('', 'sk-test', images.find(candidate => candidate.model === 'gpt-image-1')!, prompt).body)
    expect(gpt).toEqual({ model: 'gpt-image-1', prompt, n: 1 })
    const grok = JSON.parse(buildTestRequest('', 'sk-test', images.find(candidate => candidate.model === 'public-art')!, prompt).body)
    expect(grok).toEqual({ model: 'public-art', prompt, n: 1, response_format: 'b64_json' })
    const gemini = JSON.parse(buildTestRequest('', 'sk-test', images.find(candidate => candidate.model === 'public-gemini-art')!, prompt).body)
    expect(gemini).toEqual({
      contents: [{ role: 'user', parts: [{ text: prompt }] }],
      generationConfig: { responseModalities: ['TEXT', 'IMAGE'], imageConfig: { aspectRatio: '1:1' } },
    })
  })

  it('preserves a custom text prompt for every supported text protocol', () => {
    for (const [protocol, endpoint, field] of [
      ['responses', '/v1/responses', 'input'],
      ['chat', '/v1/chat/completions', 'messages'],
      ['messages', '/v1/messages', 'messages'],
      ['gemini', '/v1beta', 'contents'],
    ] as const) {
      const request = buildTestRequest('', 'sk-test', { id: 'text', model: 'text', protocol, endpoint, pathPrefix: '' }, 'custom prompt')
      expect(JSON.stringify(JSON.parse(request.body)[field])).toContain('custom prompt')
    }
  })

  it('extracts image-only success payloads in native Images, Responses and Gemini shapes', () => {
    expect(extractOutputImages('images', { data: [{ b64_json: 'QUJD' }] })).toEqual(['data:image/png;base64,QUJD'])
    expect(extractOutputImages('images', { output_format: 'webp', data: [{ b64_json: 'QUJD' }] })).toEqual(['data:image/webp;base64,QUJD'])
    expect(extractOutputImages('images', { data: [{ url: 'https://images.example/result.png' }] })).toEqual(['https://images.example/result.png'])
    expect(extractOutputImages('images', { data: [{ b64_json: 'invalid', url: 'https://images.example/result.png' }] }))
      .toEqual(['https://images.example/result.png'])
    expect(extractOutputImages('responses', { output: [{ type: 'image_generation_call', status: 'completed', result: 'QUJD' }] }))
      .toEqual(['data:image/png;base64,QUJD'])
    expect(extractOutputImages('gemini', { candidates: [{ content: { parts: [
      { inlineData: { mimeType: 'image/jpeg', data: 'QUJD' } },
      { inline_data: { mime_type: 'image/webp', data: 'REVG' } },
    ] } }] })).toEqual(['data:image/jpeg;base64,QUJD', 'data:image/webp;base64,REVG'])
  })

  it('rejects unsafe, malformed and private image outputs', () => {
    expect(extractOutputImages('images', { data: [
      { url: 'javascript:alert(1)' }, { url: 'file:///tmp/private.png' },
      { url: 'data:image/svg+xml;base64,QUJD' }, { b64_json: '<svg>' },
      { b64_json: 'Q' }, { b64_json: 'QQ=' }, { b64_json: 'QUJD', mime_type: 'image/svg+xml' },
      { url: 'https://user:secret@example.com/image.png' },
    ] })).toEqual([])
    expect(extractOutputImages('gemini', { candidates: [{ content: { parts: [
      { thought: true, inlineData: { mimeType: 'image/png', data: 'QUJD' } },
      { inlineData: { mimeType: 'text/html', data: 'QUJD' } },
    ] } }] })).toEqual([])
    expect(extractOutputImages('responses', { output: [
      { type: 'image_generation_call', status: 'in_progress', result: 'QUJD' },
      { type: 'function_call', arguments: { result: 'QUJD' } },
    ] })).toEqual([])
  })

  it('normalizes the public gateway root', () => {
    expect(normalizeGatewayRoot('https://api.example.com/')).toBe('https://api.example.com')
    expect(normalizeGatewayRoot('https://api.example.com/v1/')).toBe('https://api.example.com')
    expect(normalizeGatewayRoot('https://api.example.com/v1beta')).toBe('https://api.example.com')
  })

  it('selects the native-access endpoint for each key scope', () => {
    expect(accessPathForKey(apiKey({ group_id: 3 }))).toBe('/groups/3/native-access')
    expect(accessPathForKey(apiKey({ group_binding_mode: 'all' }))).toBe('/keys/7/native-access')
    expect(accessPathForKey(apiKey({ group_binding_mode: 'custom', aggregate_group_ids: [2] }))).toBe('/keys/7/native-access')
    expect(accessPathForKey(apiKey({ group_binding_mode: 'single' }))).toBeNull()
  })

  it('keeps one candidate per model and prefers native Responses', () => {
    const candidates = buildCandidates([
      model({ name: 'shared', platform: 'anthropic', endpoints: ['/v1/messages'] }),
      model({ name: 'shared', platform: 'openai', endpoints: ['/v1/chat/completions', '/v1/responses'] }),
      model({ name: 'gemini-pro', platform: 'gemini', endpoints: ['/v1beta'] }),
      model({ name: 'media-only', platform: 'openai', endpoints: [] }),
    ])
    expect(candidates).toEqual([
      { id: 'shared', model: 'shared', protocol: 'responses', endpoint: '/v1/responses', pathPrefix: '' },
      { id: 'gemini-pro', model: 'gemini-pro', protocol: 'gemini', endpoint: '/v1beta', pathPrefix: '' },
    ])
  })

  it('filters converted Composite candidates by the configured default endpoint', () => {
    const candidates = buildCandidates([
      model({ name: 'responses-only', conversion_endpoints: ['/v1/responses'] }),
      model({ name: 'messages-only', conversion_endpoints: ['/v1/messages'] }),
      model({ name: 'both', conversion_endpoints: ['/v1/responses', '/v1/messages'] }),
    ], { conversionEndpoint: '/v1/messages' })
    expect(candidates.map(candidate => candidate.model).sort()).toEqual(['both', 'messages-only'])
    expect(candidates.every(candidate => candidate.protocol === 'messages')).toBe(true)
  })

  it('applies a platform path prefix to the native gateway path', () => {
    const candidates = buildCandidates([
      model({ name: 'claude', platform: 'antigravity', endpoints: ['/v1/messages'] }),
      model({ name: 'gemini-2.5-pro', platform: 'antigravity', endpoints: ['/v1beta'] }),
    ], { pathPrefix: '/antigravity/' })
    expect(candidates[0]).toMatchObject({ endpoint: '/v1/messages', pathPrefix: '/antigravity' })
    expect(buildTestRequest('https://api.example.com', 'sk-test', candidates[0]!).url)
      .toBe('https://api.example.com/antigravity/v1/messages')
    expect(buildTestRequest('https://api.example.com', 'sk-test', candidates[1]!).url)
      .toBe('https://api.example.com/antigravity/v1beta/models/gemini-2.5-pro:generateContent')
  })

  it('builds the supported protocol request shapes', () => {
    const responses = buildTestRequest('https://api.example.com/v1', 'sk-test', {
      id: 'gpt-5', model: 'gpt-5', protocol: 'responses', endpoint: '/v1/responses', pathPrefix: '',
    })
    expect(responses.url).toBe('https://api.example.com/v1/responses')
    expect(responses.headers).toEqual({
      Accept: 'application/json',
      'Content-Type': 'application/json',
      Authorization: 'Bearer sk-test',
    })
    expect(JSON.parse(responses.body)).toEqual({
      model: 'gpt-5',
      input: KEY_TEST_PROMPT,
      stream: false,
      max_output_tokens: KEY_TEST_MAX_TOKENS,
    })

    const chat = buildTestRequest('https://api.example.com', 'sk-test', {
      id: 'gpt-4o', model: 'gpt-4o', protocol: 'chat', endpoint: '/v1/chat/completions', pathPrefix: '',
    })
    expect(chat.url).toBe('https://api.example.com/v1/chat/completions')
    expect(JSON.parse(chat.body)).toMatchObject({
      model: 'gpt-4o',
      messages: [{ role: 'user', content: KEY_TEST_PROMPT }],
      stream: false,
      max_tokens: KEY_TEST_MAX_TOKENS,
    })

    const messages = buildTestRequest('https://api.example.com', 'sk-test', {
      id: 'claude', model: 'claude', protocol: 'messages', endpoint: '/v1/messages', pathPrefix: '',
    })
    expect(messages.url).toBe('https://api.example.com/v1/messages')
    expect(JSON.parse(messages.body)).toMatchObject({
      model: 'claude',
      max_tokens: KEY_TEST_MAX_TOKENS,
      messages: [{ role: 'user', content: KEY_TEST_PROMPT }],
      stream: false,
    })

    const gemini = buildTestRequest('https://api.example.com/v1beta/', 'sk-test', {
      id: 'models/gemini-2.5-pro', model: 'models/gemini-2.5-pro', protocol: 'gemini', endpoint: '/v1beta', pathPrefix: '',
    })
    expect(gemini.url).toBe('https://api.example.com/v1beta/models/gemini-2.5-pro:generateContent')
    expect(JSON.parse(gemini.body)).toEqual({
      contents: [{ role: 'user', parts: [{ text: KEY_TEST_PROMPT }] }],
      generationConfig: { maxOutputTokens: KEY_TEST_MAX_TOKENS },
    })
  })

  it('extracts non-empty text from each protocol success shape', () => {
    expect(extractOutputText('responses', {
      output: [{ type: 'message', role: 'assistant', content: [{ type: 'output_text', text: 'OK' }] }],
    })).toBe('OK')
    expect(extractOutputText('responses', { output_text: 'OK' })).toBe('OK')
    expect(extractOutputText('chat', {
      choices: [{ message: { role: 'assistant', content: 'OK' }, finish_reason: 'stop' }],
    })).toBe('OK')
    expect(extractOutputText('chat', {
      choices: [{ message: { content: [{ type: 'text', text: 'O' }, { type: 'text', text: 'K' }] } }],
    })).toBe('OK')
    expect(extractOutputText('messages', {
      content: [{ type: 'text', text: 'OK' }],
    })).toBe('OK')
    expect(extractOutputText('gemini', {
      candidates: [{ content: { role: 'model', parts: [{ text: 'OK' }] }, finishReason: 'STOP' }],
    })).toBe('OK')
  })

  it('does not treat tool calls, thinking blocks, or empty candidates as text', () => {
    expect(extractOutputText('chat', {
      choices: [{ message: { content: null, tool_calls: [{ id: 'call' }] } }],
    })).toBe('')
    expect(extractOutputText('messages', {
      content: [{ type: 'thinking', thinking: 'private' }, { type: 'tool_use', id: 'tool' }],
    })).toBe('')
    expect(extractOutputText('gemini', {
      candidates: [{ content: { parts: [{ thought: true, text: 'reasoning' }] }, finishReason: 'STOP' }],
    })).toBe('')
    expect(extractOutputText('responses', { output: [] })).toBe('')
    expect(extractOutputText('chat', { choices: [] })).toBe('')
  })

  it('detects an error envelope even with HTTP 2xx', () => {
    expect(hasErrorEnvelope({ error: { type: 'insufficient_quota' } })).toBe(true)
    expect(hasErrorEnvelope({ type: 'error', error: { type: 'overloaded_error' } })).toBe(true)
    expect(hasErrorEnvelope({ error: null, output_text: 'OK' })).toBe(false)
    expect(hasErrorEnvelope({ output_text: 'OK' })).toBe(false)
  })

  it('classifies actionable gateway failures without echoing payload text', () => {
    expect(classifyFailure(401, { error: { type: 'authentication_error' } })).toBe('auth')
    expect(classifyFailure(403, { error: { message: 'API key is not assigned to any group' } })).toBe('access')
    expect(classifyFailure(403, { error: { message: 'Insufficient account balance' } })).toBe('quota')
    expect(classifyFailure(403, { error: { code: 'content_policy_violation' } })).toBe('policy')
    expect(classifyFailure(403, { error: { message: 'This account only allows approved Codex client profiles' } })).toBe('clientRestriction')
    expect(classifyFailure(402, { error: { message: 'Payment required' } })).toBe('quota')
    expect(classifyFailure(408, { error: { message: 'Request timed out' } })).toBe('timeout')
    expect(classifyFailure(429, { error: { type: 'rate_limit_error' } })).toBe('rate')
    expect(classifyFailure(404, { error: { code: 'model_not_found' } })).toBe('model')
    expect(classifyFailure(400, { error: { code: 'native_endpoint_unsupported' } })).toBe('model')
    expect(classifyFailure(400, { error: { type: 'invalid_request_error' } })).toBe('invalidRequest')
    expect(classifyFailure(502, { error: { type: 'api_error' } })).toBe('upstream')
    expect(classifyFailure(200, { error: { type: 'insufficient_quota' } })).toBe('quota')
  })

  it('reads a request ID from a response payload when available', () => {
    expect(requestIdFromPayload({ request_id: 'req-1' })).toBe('req-1')
    expect(requestIdFromPayload({ error: { request_id: 'req-2' } })).toBe('req-2')
    expect(requestIdFromPayload({ error: { message: 'no id' } })).toBe('')
  })
})
