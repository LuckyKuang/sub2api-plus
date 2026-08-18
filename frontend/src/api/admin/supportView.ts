import { apiClient } from '../client'
import type { Group, PaginatedResponse } from '@/types'
import type { UserSubscription } from '@/types'
import type { UserAvailableChannel } from '@/api/channels'
import type { UserMonitorDetail, UserMonitorView } from '@/api/channelMonitor'
import type { PaymentOrder } from '@/types/payment'

export interface AdminSupportUser {
  id: number
  email: string
  username: string
  role: 'admin' | 'user'
  balance: number
  frozen_balance: number
  concurrency: number
  rpm_limit: number
  status: 'active' | 'disabled'
  allowed_groups: number[] | null
  last_active_at?: string | null
  created_at: string
  updated_at: string
}

// Credential-free by construction: this model intentionally has no key,
// secret, token, authorization, export payload, or complete IP-list field.
export interface AdminSupportAPIKey {
  id: number
  user_id: number
  name: string
  group_id: number | null
  status: 'active' | 'inactive' | 'quota_exhausted' | 'expired'
  has_ip_whitelist: boolean
  ip_whitelist_size: number
  has_ip_blacklist: boolean
  ip_blacklist_size: number
  last_used_at: string | null
  last_used_ip: string | null
  quota: number
  quota_used: number
  expires_at: string | null
  created_at: string
  updated_at: string
  current_concurrency: number
  rate_limit_5h: number
  rate_limit_1d: number
  rate_limit_7d: number
  usage_5h: number
  usage_1d: number
  usage_7d: number
  window_5h_start: string | null
  window_1d_start: string | null
  window_7d_start: string | null
  reset_5h_at: string | null
  reset_1d_at: string | null
  reset_7d_at: string | null
  group?: Group
}

export interface AdminSupportImageTask {
  id: string
  task_id: string
  object: string
  api_key_id: number
  request_type?: string
  model?: string
  prompt_preview?: string
  status: 'processing' | 'completed' | 'failed'
  http_status?: number
  image_url?: string
  result?: unknown
  error?: unknown
  created_at: number
  completed_at?: number | null
  expires_at: number
}

export interface AdminSupportImageTaskList {
  items: AdminSupportImageTask[]
  has_more: boolean
}

export interface AdminSupportUsage {
  period?: string
  total_requests: number
  total_cost: number
  total_actual_cost?: number
  total_tokens: number
  avg_duration_ms?: number
}

export interface AdminSupportChannels {
  items: UserAvailableChannel[]
  group_rates: Record<number, number>
}

export interface AdminSupportChannelStatus {
  items: UserMonitorView[]
}

export async function getProfile(userId: number): Promise<AdminSupportUser> {
  const { data } = await apiClient.get<AdminSupportUser>(`/admin/support/users/${userId}`)
  return data
}

export async function listAPIKeys(
  userId: number,
  page = 1,
  pageSize = 20
): Promise<PaginatedResponse<AdminSupportAPIKey>> {
  const { data } = await apiClient.get<PaginatedResponse<AdminSupportAPIKey>>(
    `/admin/support/users/${userId}/api-keys`,
    { params: { page, page_size: pageSize, sort_by: 'created_at', sort_order: 'desc' } }
  )
  return data
}

export async function listAsyncImages(
  userId: number,
  params: { status?: string; limit?: number; offset?: number } = {}
): Promise<AdminSupportImageTaskList> {
  const { data } = await apiClient.get<AdminSupportImageTaskList>(
    `/admin/support/users/${userId}/async-images`,
    { params }
  )
  return data
}

export async function getAsyncImage(userId: number, taskId: string): Promise<AdminSupportImageTask> {
  const { data } = await apiClient.get<AdminSupportImageTask>(
    `/admin/support/users/${userId}/async-images/${encodeURIComponent(taskId)}`
  )
  return data
}

export async function getUsage(userId: number, period = 'month'): Promise<AdminSupportUsage> {
  const { data } = await apiClient.get<AdminSupportUsage>(`/admin/support/users/${userId}/usage`, {
    params: { period }
  })
  return data
}

export async function listChannels(userId: number): Promise<AdminSupportChannels> {
  const { data } = await apiClient.get<AdminSupportChannels>(
    `/admin/support/users/${userId}/channels`
  )
  return data
}

export async function listChannelStatus(userId: number): Promise<AdminSupportChannelStatus> {
  const { data } = await apiClient.get<AdminSupportChannelStatus>(
    `/admin/support/users/${userId}/channel-status`
  )
  return data
}

export async function getChannelStatus(
  userId: number,
  monitorId: number
): Promise<UserMonitorDetail> {
  const { data } = await apiClient.get<UserMonitorDetail>(
    `/admin/support/users/${userId}/channel-status/${monitorId}`
  )
  return data
}

export async function listSubscriptions(userId: number): Promise<UserSubscription[]> {
  const { data } = await apiClient.get<UserSubscription[]>(
    `/admin/support/users/${userId}/subscriptions`
  )
  return data
}

export async function listOrders(
  userId: number,
  params: { page?: number; page_size?: number; status?: string } = {}
): Promise<PaginatedResponse<PaymentOrder>> {
  const { data } = await apiClient.get<PaginatedResponse<PaymentOrder>>(
    `/admin/support/users/${userId}/orders`,
    { params }
  )
  return data
}

export async function getOrder(userId: number, orderId: number): Promise<PaymentOrder> {
  const { data } = await apiClient.get<PaymentOrder>(
    `/admin/support/users/${userId}/orders/${orderId}`
  )
  return data
}
