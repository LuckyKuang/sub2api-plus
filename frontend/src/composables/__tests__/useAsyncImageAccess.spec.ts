import { describe, expect, it } from 'vitest'
import { canViewAsyncImage, keyAllowsAsyncImage } from '../useAsyncImageAccess'
import type { ApiKey } from '@/types'

function apiKey(overrides: Partial<ApiKey>): ApiKey {
  return {
    id: 1,
    user_id: 1,
    key: 'sk-test',
    name: 'test',
    group_id: 1,
    status: 'active',
    ip_whitelist: [],
    ip_blacklist: [],
    last_used_at: null,
    last_used_ip: null,
    quota: 0,
    quota_used: 0,
    expires_at: null,
    created_at: '',
    updated_at: '',
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
  }
}

describe('async image access', () => {
  it('always lets administrators view the feature', () => {
    expect(canViewAsyncImage(true, false)).toBe(true)
  })

  it('requires an eligible API key for regular users', () => {
    expect(canViewAsyncImage(false, false)).toBe(false)
    expect(canViewAsyncImage(false, true)).toBe(true)
  })

  it('accepts only active OpenAI or Grok keys from image-enabled groups', () => {
    expect(keyAllowsAsyncImage(apiKey({ group: { platform: 'openai', allow_image_generation: true } } as Partial<ApiKey>))).toBe(true)
    expect(keyAllowsAsyncImage(apiKey({ group: { platform: 'grok', allow_image_generation: true } } as Partial<ApiKey>))).toBe(true)
    expect(keyAllowsAsyncImage(apiKey({ group: { platform: 'openai', allow_image_generation: false } } as Partial<ApiKey>))).toBe(false)
    expect(keyAllowsAsyncImage(apiKey({ status: 'inactive', group: { platform: 'openai', allow_image_generation: true } } as Partial<ApiKey>))).toBe(false)
  })
})
