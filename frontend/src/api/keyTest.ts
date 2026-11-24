import { apiClient } from './client'
import type { ApiKey } from '@/types'

export type KeyTestProtocol = 'responses' | 'chat' | 'messages' | 'gemini'

export interface NativeAccessModel {
  name: string
  platform: string
  endpoints: string[]
  conversion_endpoints: string[]
  group_ids?: number[]
}

export interface NativeAccessPayload {
  endpoints: string[]
  conversion_endpoints: string[]
  models: NativeAccessModel[]
}

export interface TestCandidate {
  id: string
  model: string
  protocol: KeyTestProtocol
  endpoint: string
  pathPrefix: string
}

export type KeyTestFailureReason =
  | 'auth'
  | 'access'
  | 'quota'
  | 'rate'
  | 'model'
  | 'upstream'
  | 'invalidRequest'
  | 'policy'
  | 'clientRestriction'
  | 'network'
  | 'timeout'
  | 'noText'
  | 'unknown'

export interface KeyTestRequest {
  url: string
  headers: Record<string, string>
  body: string
}

const PROTOCOL_BY_ENDPOINT: Record<string, KeyTestProtocol> = {
  '/v1/responses': 'responses',
  '/v1/chat/completions': 'chat',
  '/v1/messages': 'messages',
  '/v1beta': 'gemini',
}

const ENDPOINT_BY_PROTOCOL: Record<KeyTestProtocol, string> = {
  responses: '/v1/responses',
  chat: '/v1/chat/completions',
  messages: '/v1/messages',
  gemini: '/v1beta',
}

const PROTOCOL_PRIORITY: KeyTestProtocol[] = ['responses', 'chat', 'messages', 'gemini']

export const KEY_TEST_PROMPT = 'Reply with OK.'
export const KEY_TEST_MAX_TOKENS = 128
export const KEY_TEST_TIMEOUT_MS = 60_000

export function normalizeGatewayRoot(baseUrl: string): string {
  const fallback = typeof window !== 'undefined' ? window.location.origin : ''
  return (baseUrl || fallback)
    .trim()
    .replace(/\/+$/, '')
    .replace(/\/(?:v1|v1beta)$/i, '')
    .replace(/\/+$/, '')
}

export function isSupportedTestEndpoint(endpoint: string): boolean {
  return Object.prototype.hasOwnProperty.call(PROTOCOL_BY_ENDPOINT, endpoint)
}

export function accessPathForKey(key: ApiKey): string | null {
  const mode = key.group_binding_mode || 'single'
  if (mode === 'all' || mode === 'custom') {
    return `/keys/${key.id}/native-access`
  }
  if (key.group_id == null) return null
  return `/groups/${key.group_id}/native-access`
}

export async function fetchNativeAccess(path: string, signal?: AbortSignal): Promise<NativeAccessPayload> {
  const { data } = await apiClient.get<NativeAccessPayload>(path, { signal })
  if (!data || !Array.isArray(data.models)) {
    throw new Error('Invalid native access response')
  }
  return {
    endpoints: Array.isArray(data.endpoints) ? data.endpoints : [],
    conversion_endpoints: Array.isArray(data.conversion_endpoints) ? data.conversion_endpoints : [],
    models: data.models,
  }
}

/**
 * Build one candidate per public model name. Native mode prefers the first
 * supported endpoint in Responses > Chat > Messages > Gemini order; conversion
 * mode admits only models that support the key's configured default endpoint.
 */
export function buildCandidates(
  models: NativeAccessModel[],
  options: { conversionEndpoint?: string; pathPrefix?: string } = {},
): TestCandidate[] {
  const conversionEndpoint = options.conversionEndpoint || ''
  const pathPrefix = (options.pathPrefix || '').replace(/\/+$/, '')
  const endpointSets = new Map<string, Set<string>>()

  for (const model of models) {
    const name = String(model?.name || '').trim()
    if (!name) continue
    const endpoints = conversionEndpoint
      ? (model.conversion_endpoints || [])
      : (model.endpoints || [])
    if (!conversionEndpoint) {
      const supported = endpoints.filter(isSupportedTestEndpoint)
      if (!supported.length) continue
      const set = endpointSets.get(name) || new Set<string>()
      supported.forEach(endpoint => set.add(endpoint))
      endpointSets.set(name, set)
      continue
    }
    if (!isSupportedTestEndpoint(conversionEndpoint) || !endpoints.includes(conversionEndpoint)) continue
    endpointSets.set(name, new Set([conversionEndpoint]))
  }

  const candidates: TestCandidate[] = []
  for (const [name, endpoints] of endpointSets) {
    const endpoint = conversionEndpoint
      ? conversionEndpoint
      : PROTOCOL_PRIORITY.map(protocol => ENDPOINT_BY_PROTOCOL[protocol]).find(value => endpoints.has(value))
    if (!endpoint) continue
    candidates.push({
      id: name,
      model: name,
      protocol: PROTOCOL_BY_ENDPOINT[endpoint],
      endpoint,
      pathPrefix,
    })
  }

  return candidates.sort((left, right) => {
    const priority = PROTOCOL_PRIORITY.indexOf(left.protocol) - PROTOCOL_PRIORITY.indexOf(right.protocol)
    return priority !== 0 ? priority : left.model.localeCompare(right.model)
  })
}

export function buildTestRequest(
  baseUrl: string,
  apiKey: string,
  candidate: TestCandidate,
): KeyTestRequest {
  const root = normalizeGatewayRoot(baseUrl)
  const prefix = candidate.pathPrefix || ''
  const headers = {
    Accept: 'application/json',
    'Content-Type': 'application/json',
    Authorization: `Bearer ${apiKey}`,
  }
  switch (candidate.protocol) {
    case 'responses':
      return {
        url: `${root}${prefix}/v1/responses`,
        headers,
        body: JSON.stringify({
          model: candidate.model,
          input: KEY_TEST_PROMPT,
          stream: false,
          max_output_tokens: KEY_TEST_MAX_TOKENS,
        }),
      }
    case 'chat':
      return {
        url: `${root}${prefix}/v1/chat/completions`,
        headers,
        body: JSON.stringify({
          model: candidate.model,
          messages: [{ role: 'user', content: KEY_TEST_PROMPT }],
          stream: false,
          max_tokens: KEY_TEST_MAX_TOKENS,
        }),
      }
    case 'messages':
      return {
        url: `${root}${prefix}/v1/messages`,
        headers,
        body: JSON.stringify({
          model: candidate.model,
          max_tokens: KEY_TEST_MAX_TOKENS,
          messages: [{ role: 'user', content: KEY_TEST_PROMPT }],
          stream: false,
        }),
      }
    case 'gemini': {
      const model = candidate.model.replace(/^models\//, '')
      return {
        url: `${root}${prefix}/v1beta/models/${encodeURIComponent(model)}:generateContent`,
        headers,
        body: JSON.stringify({
          contents: [{ role: 'user', parts: [{ text: KEY_TEST_PROMPT }] }],
          generationConfig: { maxOutputTokens: KEY_TEST_MAX_TOKENS },
        }),
      }
    }
  }
}

type UnknownRecord = Record<string, unknown>

function asRecord(value: unknown): UnknownRecord | null {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as UnknownRecord : null
}

function nonEmptyString(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function responseTextFromOutput(output: unknown): string {
  if (!Array.isArray(output)) return ''
  const parts: string[] = []
  for (const item of output) {
    const entry = asRecord(item)
    if (!entry) continue
    if (entry.type === 'output_text') {
      parts.push(nonEmptyString(entry.text))
      continue
    }
    if (entry.type !== 'message' || !Array.isArray(entry.content)) continue
    for (const part of entry.content) {
      const block = asRecord(part)
      if (!block) continue
      if ((block.type === 'output_text' || block.type === 'text')) {
        parts.push(nonEmptyString(block.text))
      }
    }
  }
  return parts.join('')
}

function chatMessageText(message: unknown): string {
  const entry = asRecord(message)
  if (!entry) return ''
  const content = entry.content
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) return ''
  return content.map(part => {
    const block = asRecord(part)
    if (!block) return ''
    return block.type === 'text' || block.type === 'output_text' ? nonEmptyString(block.text) : ''
  }).join('')
}

function messagesText(content: unknown): string {
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) return ''
  return content.map(part => {
    const block = asRecord(part)
    return block && block.type === 'text' ? nonEmptyString(block.text) : ''
  }).join('')
}

function geminiCandidateText(payload: UnknownRecord): string {
  if (!Array.isArray(payload.candidates)) return ''
  for (const candidate of payload.candidates) {
    const content = asRecord(asRecord(candidate)?.content)
    if (!content || !Array.isArray(content.parts)) continue
    const text = content.parts.map(part => {
      const block = asRecord(part)
      return block && block.thought !== true ? nonEmptyString(block.text) : ''
    }).join('')
    if (text.trim()) return text
  }
  return ''
}

export function extractOutputText(protocol: KeyTestProtocol, payload: unknown): string {
  const root = asRecord(payload)
  if (!root) return ''
  switch (protocol) {
    case 'responses': {
      const direct = nonEmptyString(root.output_text)
      return direct.trim() ? direct : responseTextFromOutput(root.output)
    }
    case 'chat': {
      if (!Array.isArray(root.choices)) return ''
      for (const choice of root.choices) {
        const entry = asRecord(choice)
        if (!entry) continue
        const text = chatMessageText(entry.message)
        if (text.trim()) return text
      }
      return ''
    }
    case 'messages':
      return messagesText(root.content)
    case 'gemini':
      return geminiCandidateText(root)
  }
}

export function hasErrorEnvelope(payload: unknown): boolean {
  const root = asRecord(payload)
  if (!root) return false
  if (root.type === 'error') return true
  const error = root.error
  return error !== null && error !== undefined
}

function errorTokens(payload: unknown): string[] {
  const root = asRecord(payload)
  if (!root) return []
  const tokens: string[] = []
  const collect = (value: unknown, depth = 0) => {
    if (depth > 2 || value === null || value === undefined) return
    if (typeof value === 'string') {
      tokens.push(value)
      return
    }
    if (typeof value === 'number') {
      tokens.push(String(value))
      return
    }
    const record = asRecord(value)
    if (!record) return
    for (const key of ['type', 'code', 'status', 'message', 'reason']) {
      collect(record[key], depth + 1)
    }
  }
  collect(root.error)
  collect(root.type)
  collect(root.code)
  collect(root.status)
  collect(root.message)
  return tokens.map(token => token.toLowerCase())
}

export function classifyFailure(status: number, payload: unknown): KeyTestFailureReason {
  const text = errorTokens(payload).join(' ')
  if (/content_policy_violation|session_blocked_by_content_policy|prompt_guard_blocked|security audit/.test(text)) return 'policy'
  if (/approved codex client|official clients|only allows/.test(text)) return 'clientRestriction'
  if (/not assigned to any group|group is not available|group unavailable|access denied/.test(text)) return 'access'
  if (status === 401 || /authentication|unauthenticated|invalid api key|invalid_api_key|api key is disabled|api key .*expired|已过期/.test(text)) return 'auth'
  if (status === 402 || /insufficient_quota|quota_exhausted|quota|balance|余额|额度|usage limit|credit/.test(text)) return 'quota'
  if (status === 403 || /permission|forbidden/.test(text)) return 'access'
  if (/native_endpoint_unsupported|composite_model_unresolved|composite_endpoint_required/.test(text)) return 'model'
  if (status === 404 || /model_not_found|model not found|model is not available|unavailable model/.test(text)) return 'model'
  if (status === 408 || /timed out|timeout/.test(text)) return 'timeout'
  if (status === 429 || /rate_limit|rate limit|resource_exhausted|too many requests/.test(text)) return 'rate'
  if (status >= 500 || /api_error|overloaded|internal|unavailable|bad gateway|server_error/.test(text)) return 'upstream'
  if (status === 400 || /invalid_request|invalid request/.test(text)) return 'invalidRequest'
  return 'unknown'
}

export function requestIdFromPayload(payload: unknown): string {
  const root = asRecord(payload)
  if (!root) return ''
  const direct = nonEmptyString(root.request_id).trim()
  if (direct) return direct
  const error = asRecord(root.error)
  return nonEmptyString(error?.request_id).trim()
}
