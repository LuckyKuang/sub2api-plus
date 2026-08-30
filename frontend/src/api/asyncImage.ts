import { buildGatewayUrl } from './client'

export type AsyncImageTaskStatus = 'processing' | 'completed' | 'failed' | string

export interface AsyncImageTask {
  id: string
  task_id: string
  object: string
  request_type?: 'generation' | 'edit' | string
  model?: string
  prompt_preview?: string
  requested_images?: number
  actual_images?: number
  status: AsyncImageTaskStatus
  http_status?: number
  image_url?: string
  result?: {
    created?: number
    data?: Array<{ url?: string }>
  }
  error?: {
    type?: string
    code?: string
    message?: string
  }
  created_at: number
  completed_at?: number
  expires_at: number
}

export interface AsyncImageTaskListResponse {
  object: string
  data: AsyncImageTask[]
  has_more: boolean
}

export interface AsyncImageListOptions {
  status?: string
  limit?: number
  offset?: number
}

export interface AsyncImageGenerationRequest {
  model: string
  prompt: string
  size?: string
  n?: number
  quality?: string
  background?: string
}

export interface AsyncImageEditRequest extends AsyncImageGenerationRequest {
  images: File[]
  mask?: File
}

export class AsyncImageDownloadValidationError extends Error {
  constructor() {
    super('async_image_download_invalid_archive')
    this.name = 'AsyncImageDownloadValidationError'
  }
}

interface GatewayModel {
  id?: unknown
}

async function parseAsyncImageError(response: Response): Promise<Error> {
  try {
    const body = await response.json()
    const error = new Error(body?.error?.message || body?.message || response.statusText)
    ;(error as Error & { status?: number; code?: string }).status = response.status
    ;(error as Error & { status?: number; code?: string }).code = body?.error?.code || body?.error?.type
    return error
  } catch {
    const error = new Error(response.statusText || `HTTP ${response.status}`)
    ;(error as Error & { status?: number }).status = response.status
    return error
  }
}

function authHeaders(apiKey: string, extra?: HeadersInit): HeadersInit {
  return { Authorization: `Bearer ${apiKey}`, ...extra }
}

export function preferredAsyncImageModel(models: string[]): string {
  return models.includes('gpt-image-2') ? 'gpt-image-2' : models[0] || ''
}

export async function listAsyncImageModels(apiKey: string): Promise<string[]> {
  const response = await fetch(buildGatewayUrl('/v1/models'), { headers: authHeaders(apiKey) })
  if (!response.ok) throw await parseAsyncImageError(response)

  const body = await response.json() as { data?: Array<GatewayModel | string> }
  const seen = new Set<string>()
  const models: string[] = []
  for (const entry of body.data || []) {
    const model = typeof entry === 'string' ? entry : entry?.id
    if (typeof model !== 'string') continue
    const id = model.trim()
    if (!id || seen.has(id)) continue
    seen.add(id)
    models.push(id)
  }
  return models
}

export async function submitAsyncImageGeneration(apiKey: string, payload: AsyncImageGenerationRequest): Promise<AsyncImageTask> {
  const response = await fetch(buildGatewayUrl('/v1/images/generations/async'), {
    method: 'POST',
    headers: authHeaders(apiKey, { 'Content-Type': 'application/json' }),
    body: JSON.stringify(payload),
  })
  if (!response.ok) throw await parseAsyncImageError(response)
  return response.json()
}

export async function submitAsyncImageEdit(apiKey: string, payload: AsyncImageEditRequest): Promise<AsyncImageTask> {
  const form = new FormData()
  form.set('model', payload.model)
  form.set('prompt', payload.prompt)
  if (payload.size) form.set('size', payload.size)
  if (payload.n) form.set('n', String(payload.n))
  if (payload.quality) form.set('quality', payload.quality)
  if (payload.background) form.set('background', payload.background)
  payload.images.forEach(image => form.append('image[]', image, image.name))
  if (payload.mask) form.append('mask', payload.mask, payload.mask.name)

  const response = await fetch(buildGatewayUrl('/v1/images/edits/async'), {
    method: 'POST',
    // Do not set Content-Type here. The browser supplies the multipart boundary.
    headers: authHeaders(apiKey),
    body: form,
  })
  if (!response.ok) throw await parseAsyncImageError(response)
  return response.json()
}

export async function listAsyncImageTasks(apiKey: string, options: AsyncImageListOptions = {}): Promise<AsyncImageTaskListResponse> {
  const query = new URLSearchParams()
  query.set('limit', String(options.limit || 20))
  query.set('offset', String(options.offset || 0))
  if (options.status) query.set('status', options.status)
  const response = await fetch(buildGatewayUrl(`/v1/images/tasks?${query.toString()}`), { headers: authHeaders(apiKey) })
  if (!response.ok) throw await parseAsyncImageError(response)
  return response.json()
}

export async function getAsyncImageTask(apiKey: string, taskID: string): Promise<AsyncImageTask> {
  const response = await fetch(buildGatewayUrl(`/v1/images/tasks/${encodeURIComponent(taskID)}`), { headers: authHeaders(apiKey) })
  if (!response.ok) throw await parseAsyncImageError(response)
  return response.json()
}

export async function deleteAsyncImageTask(apiKey: string, taskID: string): Promise<void> {
  const response = await fetch(buildGatewayUrl(`/v1/images/tasks/${encodeURIComponent(taskID)}`), {
    method: 'DELETE',
    headers: authHeaders(apiKey),
  })
  if (!response.ok) throw await parseAsyncImageError(response)
}

export async function downloadAsyncImageZip(apiKey: string, taskID: string): Promise<Blob> {
  const response = await fetch(buildGatewayUrl(`/v1/images/tasks/${encodeURIComponent(taskID)}/download`), {
    headers: authHeaders(apiKey),
  })
  if (!response.ok) throw await parseAsyncImageError(response)
  const contentType = response.headers.get('content-type') || ''
  const blob = await response.blob()
  if (!isZipContentType(contentType) || !(await hasZipFileHeader(blob))) {
    throw new AsyncImageDownloadValidationError()
  }
  return new Blob([blob], { type: 'application/zip' })
}

export function saveAsyncImageBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}

function isZipContentType(contentType: string): boolean {
  const mediaType = contentType.split(';', 1)[0]?.trim().toLowerCase()
  return mediaType === 'application/zip' || mediaType === 'application/x-zip-compressed' || mediaType === 'application/octet-stream'
}

async function hasZipFileHeader(blob: Blob): Promise<boolean> {
  if (blob.size < 4) return false
  const bytes = new Uint8Array(await readBlobAsArrayBuffer(blob.slice(0, 4)))
  return bytes[0] === 0x50 && bytes[1] === 0x4b && bytes[2] === 0x03 && bytes[3] === 0x04
}

function readBlobAsArrayBuffer(blob: Blob): Promise<ArrayBuffer> {
  if (typeof blob.arrayBuffer === 'function') return blob.arrayBuffer()
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(reader.error || new Error('failed to read download archive'))
    reader.onload = () => resolve(reader.result as ArrayBuffer)
    reader.readAsArrayBuffer(blob)
  })
}
