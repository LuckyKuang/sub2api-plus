import { apiClient } from './client'
import type { UsageRequestType, UsageLog } from '@/types'
export interface LiveRequest {
  id: string; version: number; user_id: number; api_key_id: number; key_name: string; group_name?: string; account_id?: number
  model: string; endpoint: string; request_id: string; request_type: UsageRequestType; turn?: number
  status: 'processing' | 'transmitting' | 'finished' | 'failed' | 'rejected' | 'cancelled' | 'completed' | 'client_disconnected' | 'incomplete' | 'unknown'
  started_at: string; updated_at: string; finished_at?: string; elapsed_ms: number
  duration_ms?: number; input_tokens?: number; output_tokens?: number; image_output_tokens?: number; audio_output_tokens?: number
  first_byte_ms?: number; first_token_ms?: number; first_output_kind?: UsageLog['first_output_kind']
  reason?: string; http_status?: number; actual_cost?: number
  cache_read_tokens?: number; cache_creation_tokens?: number; user_agent?: string; ip_address?: string
  reasoning_effort?: string; timing_version?: number; native_compaction_v2?: boolean; is_complete?: boolean
  first_output_ms?: number; last_token_ms?: number; image_count?: number; image_size?: string
  video_count?: number; video_resolution?: string; video_duration_seconds?: number; billing_mode?: string
}
export interface LiveRequestSnapshot { items: LiveRequest[]; server_now: string; window_seconds: number; limit: number; truncated: boolean; poll_after_seconds: number }
export async function getLiveRequests(mode: 'user' | 'admin', signal: AbortSignal): Promise<LiveRequestSnapshot> {
  const { data } = await apiClient.get<LiveRequestSnapshot>(mode === 'admin' ? '/admin/usage/live' : '/usage/live', { signal })
  return data
}
