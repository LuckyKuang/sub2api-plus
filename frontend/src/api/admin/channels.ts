/**
 * Admin Channels API endpoints
 * Handles channel management for administrators
 */

import { apiClient } from '../client'
import type { BillingMode, ChannelStatus, BillingModelSource } from '@/constants/channel'

export type { BillingMode } from '@/constants/channel'

export interface PricingInterval {
  id?: number
  min_tokens: number
  max_tokens: number | null
  tier_label: string
  input_price: number | null
  output_price: number | null
  cache_write_price: number | null
  cache_write_1h_price?: number | null
  cache_read_price: number | null
  input_multiplier: number | null
  output_multiplier: number | null
  cache_write_multiplier: number | null
  cache_read_multiplier: number | null
  per_request_price: number | null
  sort_order: number
}

export interface ChannelTimePricingPeriod {
  start_time: string
  end_time: string
  multiplier: number
}

export interface ChannelTimePricing {
  timezone: string
  weekdays_only?: boolean
  periods: ChannelTimePricingPeriod[]
}

export interface ChannelModelPricing {
  id?: number
  platform: string
  models: string[]
  billing_mode: BillingMode
  input_price: number | null
  output_price: number | null
  cache_write_price: number | null
  cache_write_1h_price?: number | null
  cache_read_price: number | null
  fast_multiplier?: number | null
  flex_multiplier?: number | null
  reasoning_effort_multipliers?: Record<string, number> | null
  image_input_price: number | null
  image_output_price: number | null
  per_request_price: number | null
  intervals: PricingInterval[]
  time_pricing: ChannelTimePricing | null
}

export interface AccountStatsPricingRule {
  id?: number
  name: string
  group_ids: number[]
  account_ids: number[]
  pricing: ChannelModelPricing[]
}

export interface Channel {
  id: number
  name: string
  description: string
  status: ChannelStatus
  billing_model_source: BillingModelSource
  restrict_models: boolean
  features_config?: Record<string, unknown>
  group_ids: number[]
  model_pricing: ChannelModelPricing[]
  model_mapping: Record<string, Record<string, string>> // platform → {src→dst}
  apply_pricing_to_account_stats: boolean
  account_stats_pricing_rules: AccountStatsPricingRule[]
  created_at: string
  updated_at: string
}

export interface CreateChannelRequest {
  name: string
  description?: string
  group_ids?: number[]
  model_pricing?: ChannelModelPricing[]
  model_mapping?: Record<string, Record<string, string>>
  billing_model_source?: string
  restrict_models?: boolean
  features_config?: Record<string, unknown>
  apply_pricing_to_account_stats?: boolean
  account_stats_pricing_rules?: AccountStatsPricingRule[]
}

export interface UpdateChannelRequest {
  name?: string
  description?: string
  status?: string
  group_ids?: number[]
  model_pricing?: ChannelModelPricing[]
  model_mapping?: Record<string, Record<string, string>>
  billing_model_source?: string
  restrict_models?: boolean
  features_config?: Record<string, unknown>
  apply_pricing_to_account_stats?: boolean
  account_stats_pricing_rules?: AccountStatsPricingRule[]
}

interface PaginatedResponse<T> {
  items: T[]
  total: number
}

/**
 * List channels with pagination
 */
export async function list(
  page: number = 1,
  pageSize: number = 20,
  filters?: {
    status?: string
    search?: string
    sort_by?: string
    sort_order?: 'asc' | 'desc'
  },
  options?: { signal?: AbortSignal }
): Promise<PaginatedResponse<Channel>> {
  const { data } = await apiClient.get<PaginatedResponse<Channel>>('/admin/channels', {
    params: {
      page,
      page_size: pageSize,
      ...filters
    },
    signal: options?.signal
  })
  return data
}

/**
 * Get channel by ID
 */
export async function getById(id: number): Promise<Channel> {
  const { data } = await apiClient.get<Channel>(`/admin/channels/${id}`)
  return data
}

/**
 * Create a new channel
 */
export async function create(req: CreateChannelRequest): Promise<Channel> {
  const { data } = await apiClient.post<Channel>('/admin/channels', req)
  return data
}

/**
 * Update a channel
 */
export async function update(id: number, req: UpdateChannelRequest): Promise<Channel> {
  const { data } = await apiClient.put<Channel>(`/admin/channels/${id}`, req)
  return data
}

/**
 * Delete a channel
 */
export async function remove(id: number): Promise<void> {
  await apiClient.delete(`/admin/channels/${id}`)
}

/** 单模型参考价状态。priced 之外的值都不能被当成「已填价」使用。 */
export type ModelPricingStatus = 'priced' | 'manual_required' | 'unsupported_unit'

/** 参考价来源。proxy_reference 是代理价，不是供应商同型号公开价。 */
export type ModelPricingSource =
  | 'release_catalog'
  | 'builtin_fallback'
  | 'proxy_reference'
  | 'none'

/**
 * 缺价原因。稳定 key，用于 i18n 文案和排障，不要在前端比较自由文本。
 */
export type ModelPricingReasonCode =
  | 'exact_price_unavailable'
  | 'provider_price_unpublished'
  | 'unsupported_billing_dimension'
  | 'platform_model_unsupported'
  | 'catalog_unavailable'

/** 目录刷新状态。stale 表示沿用旧快照，不能当成「已是最新」。 */
export type PricingRefreshStatus = 'refreshed' | 'current' | 'stale' | 'disabled'

/**
 * 完整的渠道价卡，字段口径与后端 ChannelModelPricing 一致。
 * null = 源数据没有该字段（不是免费）；0 = 明确免费。两者必须区分。
 */
export interface ReferencePricingCard {
  platform: string
  models: string[]
  billing_mode: BillingMode
  input_price: number | null
  output_price: number | null
  cache_write_price: number | null
  cache_write_1h_price: number | null
  cache_read_price: number | null
  fast_multiplier: number | null
  flex_multiplier: number | null
  reasoning_effort_multipliers?: Record<string, number> | null
  image_input_price: number | null
  image_output_price: number | null
  per_request_price: number | null
  intervals: PricingInterval[]
}

/** 单个模型的参考价解析结果。 */
export interface ModelPricingReference {
  model: string
  /** 实际命中的型号：与 model 不同说明走了别名/归一，UI 要显示出来。 */
  matched_model: string
  platform: string
  status: ModelPricingStatus
  source: ModelPricingSource
  reason_code: ModelPricingReasonCode | ''
  /** 仅 status=priced 时非 null；不能把 null 当成 0 元。 */
  pricing: ReferencePricingCard | null
}

/**
 * 查询单个模型的渠道参考价。
 *
 * platform 与 model 都是必填：没有平台就无法判断价格是否属于该平台，
 * 早期版本因此把 Anthropic 的价返回给 OpenAI 渠道。
 */
export async function getModelDefaultPricing(
  platform: string,
  model: string,
): Promise<ModelPricingReference> {
  const { data } = await apiClient.get<ModelPricingReference>('/admin/channels/model-pricing', {
    params: { platform, model }
  })
  return data
}

export interface SyncPricingModelsResult {
  platform: string
  refresh_status: PricingRefreshStatus
  catalog_version: string
  last_updated?: string
  /** CATALOG_REFRESH_FAILED；空表示本次没有刷新告警。 */
  warning_code: string
  models: ModelPricingReference[]
}

/**
 * 刷新受信任的价格 Release 并同步该平台的支持模型。
 *
 * 走 POST：这一步会读远端 manifest、校验并原子换装目录，还要返回刷新状态，
 * 不是幂等的 GET。
 */
export async function syncPricingModels(platform: string): Promise<SyncPricingModelsResult> {
  const { data } = await apiClient.post<SyncPricingModelsResult>(
    '/admin/channels/pricing/sync-models',
    { platform }
  )
  return data
}

const channelsAPI = { list, getById, create, update, remove, getModelDefaultPricing, syncPricingModels }
export default channelsAPI
