import type { ApiKey, Group } from '@/types'

export interface AdminAPIKeySummary {
  id: number
  user_id: number
  name: string
  group_id: number | null
  status: ApiKey['status']
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
