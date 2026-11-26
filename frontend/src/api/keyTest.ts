import { apiClient } from './client'
import { buildGatewayUrl } from './url'
import type { ApiKey } from '@/types'

export type KeyTestProtocol = 'responses' | 'chat' | 'messages' | 'gemini' | 'images'

export interface NativeAccessModel {
  name: string
  platform: string
  endpoints: string[]
  conversion_endpoints: string[]
  group_ids?: number[]
  is_image?: boolean
  image_group_ids?: number[]
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
  isImage?: boolean
  platform?: string
  groupId?: number
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
  | 'noImage'
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
  '/v1/images/generations': 'images',
}

const ENDPOINT_BY_PROTOCOL: Record<KeyTestProtocol, string> = {
  responses: '/v1/responses',
  chat: '/v1/chat/completions',
  messages: '/v1/messages',
  gemini: '/v1beta',
  images: '/v1/images/generations',
}

const PROTOCOL_PRIORITY: KeyTestProtocol[] = ['images', 'responses', 'chat', 'messages', 'gemini']

export const KEY_TEST_PROMPT = 'hi'
export const KEY_TEST_MAX_TOKENS = 128
export const KEY_TEST_TIMEOUT_MS = 60_000
export const KEY_TEST_IMAGE_TIMEOUT_MS = 300_000

function isImageModel(model: string, platform?: string): boolean {
  const name = model.trim().toLowerCase().replace(/^models\//, '')
  const gpt = name.startsWith('gpt-image-')
  const grok = name === 'grok-imagine' || name === 'grok-imagine-edit' || name.startsWith('grok-imagine-image')
  const gemini = name.startsWith('gemini-') && (name.endsWith('-image') || name.includes('-image-'))
  if (platform === 'openai') return gpt || grok || gemini || name.includes('nano-banana')
  if (platform === 'grok') return grok
  if (platform === 'gemini' || platform === 'antigravity') return gemini
  return !platform && (gpt || grok || gemini || name.includes('nano-banana'))
}

export function isImageTestCandidate(candidate: TestCandidate): boolean {
  return candidate.protocol === 'images' || (candidate.isImage ?? isImageModel(candidate.model, candidate.platform))
}

export function normalizeGatewayRoot(baseUrl: string): string {
  // An explicit public gateway wins. Without one, resolve the gateway origin
  // from the configured API backend origin, not from the console's origin.
  const explicit = (baseUrl || '').trim()
  const fallback = buildGatewayUrl('/')
  return (explicit || fallback)
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
 * supported endpoint in Images > Responses > Chat > Messages > Gemini order; conversion
 * mode admits only models that support the key's configured default endpoint.
 */
export function buildCandidates(
  models: NativeAccessModel[],
  options: { conversionEndpoint?: string; pathPrefix?: string } = {},
): TestCandidate[] {
  const conversionEndpoint = options.conversionEndpoint || ''
  const pathPrefix = (options.pathPrefix || '').replace(/\/+$/, '')
  const endpointSets = new Map<string, Set<string>>()
  type EndpointOwner = { platform: string; groupId?: number; isImage?: boolean }
  const endpointOwners = new Map<string, Map<string, EndpointOwner>>()

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
      const owners = endpointOwners.get(name) || new Map<string, EndpointOwner>()
      for (const endpoint of supported) {
        if (owners.has(endpoint)) continue
        const groups = model.is_image === true && model.image_group_ids?.length
          ? model.image_group_ids
          : model.group_ids
        const groupId = groups?.find(id => Number.isSafeInteger(id) && id > 0)
        owners.set(endpoint, {
          platform: model.platform,
          ...(groupId ? { groupId } : {}),
          isImage: model.is_image ?? isImageModel(name, model.platform),
        })
      }
      endpointOwners.set(name, owners)
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
    const owner = endpointOwners.get(name)?.get(endpoint)
    const image = endpoint === '/v1/images/generations' || (owner?.isImage ?? isImageModel(name))
    candidates.push({
      id: name,
      model: name,
      protocol: PROTOCOL_BY_ENDPOINT[endpoint],
      endpoint,
      pathPrefix,
      ...(image
        ? { ...owner, isImage: true }
        : isImageModel(name) ? { isImage: false } : {}),
    })
  }

  return candidates.sort((left, right) => {
    const imagePriority = Number(isImageTestCandidate(left)) - Number(isImageTestCandidate(right))
    if (imagePriority !== 0) return imagePriority
    const priority = PROTOCOL_PRIORITY.indexOf(left.protocol) - PROTOCOL_PRIORITY.indexOf(right.protocol)
    return priority !== 0 ? priority : left.model.localeCompare(right.model)
  })
}

export function buildTestRequest(
  baseUrl: string,
  apiKey: string,
  candidate: TestCandidate,
  prompt: string = KEY_TEST_PROMPT,
): KeyTestRequest {
  const root = normalizeGatewayRoot(baseUrl)
  const prefix = candidate.pathPrefix || ''
  const headers = {
    Accept: 'application/json',
    'Content-Type': 'application/json',
    Authorization: `Bearer ${apiKey}`,
    ...(candidate.groupId ? { 'X-Composite-Group-ID': String(candidate.groupId) } : {}),
  }
  switch (candidate.protocol) {
    case 'responses':
      return {
        url: `${root}${prefix}/v1/responses`,
        headers,
        body: JSON.stringify({
          model: candidate.model,
          input: prompt,
          stream: false,
          ...(isImageTestCandidate(candidate)
            ? { tools: [{ type: 'image_generation' }] }
            : { max_output_tokens: KEY_TEST_MAX_TOKENS }),
        }),
      }
    case 'chat':
      return {
        url: `${root}${prefix}/v1/chat/completions`,
        headers,
        body: JSON.stringify({
          model: candidate.model,
          messages: [{ role: 'user', content: prompt }],
          stream: false,
          ...(!isImageTestCandidate(candidate) ? { max_tokens: KEY_TEST_MAX_TOKENS } : {}),
        }),
      }
    case 'messages':
      return {
        url: `${root}${prefix}/v1/messages`,
        headers,
        body: JSON.stringify({
          model: candidate.model,
          max_tokens: KEY_TEST_MAX_TOKENS,
          messages: [{ role: 'user', content: prompt }],
          stream: false,
        }),
      }
    case 'gemini': {
      const model = candidate.model.replace(/^models\//, '')
      return {
        url: `${root}${prefix}/v1beta/models/${encodeURIComponent(model)}:generateContent`,
        headers,
        body: JSON.stringify({
          contents: [{ role: 'user', parts: [{ text: prompt }] }],
          generationConfig: isImageTestCandidate(candidate)
            ? { responseModalities: ['TEXT', 'IMAGE'], imageConfig: { aspectRatio: '1:1' } }
            : { maxOutputTokens: KEY_TEST_MAX_TOKENS },
        }),
      }
    }
    case 'images':
      return {
        url: `${root}${prefix}/v1/images/generations`,
        headers,
        body: JSON.stringify({
          model: candidate.model,
          prompt,
          n: 1,
          ...(candidate.platform === 'grok' || candidate.model.toLowerCase().startsWith('grok-imagine')
            ? { response_format: 'b64_json' }
            : {}),
        }),
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
    case 'images':
      return ''
  }
}

const IMAGE_MIME_TYPES = new Set(['image/png', 'image/jpeg', 'image/webp', 'image/gif'])

function imageMime(value: unknown): string {
  const format = nonEmptyString(value).trim().toLowerCase()
  const mime = format.includes('/') ? format : `image/${format === 'jpg' ? 'jpeg' : format}`
  return IMAGE_MIME_TYPES.has(mime) ? mime : ''
}

function base64Image(data: unknown, mimeType: unknown = 'image/png'): string {
  const encoded = nonEmptyString(data).trim()
  const mime = imageMime(mimeType)
  if (!mime || !encoded || encoded.length % 4 !== 0 || !/^[A-Za-z0-9+/]+={0,2}$/.test(encoded)) return ''
  return `data:${mime};base64,${encoded}`
}

function imageUrl(value: unknown): string {
  const url = nonEmptyString(value).trim()
  const data = /^data:(image\/(?:png|jpeg|webp|gif));base64,([A-Za-z0-9+/]+={0,2})$/i.exec(url)
  if (data) return base64Image(data[2], data[1])
  try {
    const parsed = new URL(url)
    return (parsed.protocol === 'https:' || parsed.protocol === 'http:') && !parsed.username && !parsed.password ? parsed.href : ''
  } catch {
    return ''
  }
}

/** Extract only rendered image outputs, excluding Gemini thought parts and tool arguments. */
export function extractOutputImages(protocol: KeyTestProtocol, payload: unknown): string[] {
  const root = asRecord(payload)
  if (!root) return []
  const images = new Set<string>()
  const addUrl = (value: unknown) => { const url = imageUrl(value); if (url) images.add(url) }
  const addBase64 = (value: unknown, mime: unknown = 'image/png') => {
    const url = base64Image(value, mime)
    if (url) images.add(url)
  }
  switch (protocol) {
    case 'images':
      if (Array.isArray(root.data)) {
        for (const item of root.data) {
          const entry = asRecord(item)
          if (!entry) continue
          const inline = base64Image(entry.b64_json, entry.mime_type || entry.output_format || root.output_format || 'image/png')
          if (inline) images.add(inline)
          else addUrl(entry.url)
        }
      }
      break
    case 'responses':
      if (Array.isArray(root.output)) {
        for (const item of root.output) {
          const entry = asRecord(item)
          if (entry?.type === 'image_generation_call' && (!entry.status || entry.status === 'completed')) {
            addBase64(entry.result, entry.output_format || root.output_format || 'image/png')
          }
        }
      }
      break
    case 'gemini':
      if (Array.isArray(root.candidates)) {
        for (const candidate of root.candidates) {
          const parts = asRecord(asRecord(candidate)?.content)?.parts
          if (!Array.isArray(parts)) continue
          for (const item of parts) {
            const part = asRecord(item)
            if (!part || part.thought === true) continue
            const inline = asRecord(part.inlineData || part.inline_data)
            if (inline) addBase64(inline.data, inline.mimeType || inline.mime_type)
          }
        }
      }
      break
    case 'chat':
      if (Array.isArray(root.choices)) {
        for (const choice of root.choices) {
          const content = asRecord(asRecord(choice)?.message)?.content
          if (!Array.isArray(content)) continue
          for (const item of content) {
            const part = asRecord(item)
            if (part?.type === 'image_url') addUrl(asRecord(part.image_url)?.url || part.image_url)
          }
        }
      }
      break
    case 'messages':
      if (Array.isArray(root.content)) {
        for (const item of root.content) {
          const part = asRecord(item)
          const source = asRecord(part?.source)
          if (part?.type !== 'image' || !source) continue
          if (source.type === 'base64') addBase64(source.data, source.media_type)
          if (source.type === 'url') addUrl(source.url)
        }
      }
      break
  }
  return [...images]
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
