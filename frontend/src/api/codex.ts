import { supportImageFetch } from '@/utils/adminSupportContext'
import { buildApiUrl } from './url'

export interface CodexModelsManifestResult {
  content: string
  modelCount: number
  responseBytes: number
}

const DEFAULT_CODEX_CLIENT_VERSION = '0.158.0'

function normalizeCodexBaseUrl(baseUrl: string): string {
  const fallback = typeof window !== 'undefined' ? window.location.origin : ''
  const value = (baseUrl || fallback).trim().replace(/\/+$/, '')
  if (!value) return '/v1'
  return /\/v1$/i.test(value) ? value : `${value}/v1`
}

export function buildCodexModelsManifestUrl(
  baseUrl: string,
  clientVersion = DEFAULT_CODEX_CLIENT_VERSION
): string {
  const params = new URLSearchParams({ client_version: clientVersion })
  return `${buildCodexModelCatalogUrl(baseUrl)}?${params.toString()}`
}

export function buildCodexModelCatalogUrl(baseUrl: string): string {
  return `${normalizeCodexBaseUrl(baseUrl)}/models`
}

function isCodexModelsManifest(value: unknown): value is { models: unknown[] } {
  return typeof value === 'object' && value !== null && Array.isArray((value as { models?: unknown }).models)
}

export async function fetchCodexModelsManifest(
  baseUrl: string,
  apiKey: string,
  signal?: AbortSignal
): Promise<CodexModelsManifestResult> {
  const manifestURL = buildCodexModelsManifestUrl(baseUrl)
  const query = manifestURL.slice(manifestURL.indexOf('?'))
  const response = await supportImageFetch(`/v1/models${query}`, apiKey, {
    method: 'GET',
    headers: {
      Accept: 'application/json',
      Authorization: `Bearer ${apiKey}`
    },
    cache: 'no-store',
    signal
  }, () => manifestURL, buildApiUrl)

  if (!response.ok) {
    throw new Error(`Codex models request failed with status ${response.status}`)
  }

  const text = await response.text()
  const payload: unknown = JSON.parse(text)
  if (!isCodexModelsManifest(payload)) {
    throw new Error('Codex models response is not a valid manifest')
  }

  return {
    content: JSON.stringify(payload, null, 2),
    modelCount: payload.models.length,
    responseBytes: new TextEncoder().encode(text).byteLength
  }
}
