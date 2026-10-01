import { apiClient } from './client'

export type ServiceStatus = 'normal' | 'degraded' | 'partial' | 'outage' | 'recovering' | 'unknown' | 'insufficient'
export type StatusRange = '24h' | '7d' | '30d'
export type IncidentPhase = 'detected' | 'ongoing' | 'recovering' | 'awaiting_data' | 'resolved'
export interface StatusConfig {
  version: number
  minimum_samples: number
  warning_error_rate: number
  outage_error_rate: number
  warning_ttft_ms: number
  abnormal_windows: number
  recovery_windows: number
}
export interface StatusPlatform {
  platform: string
  status: ServiceStatus
  success_rate: number | null
  ttft_p50_ms: number | null
  last_request_at: string | null
  timeline: Array<{ at: string; status: ServiceStatus }>
  models: Array<{ group_id: number; group_name: string; model: string; status: ServiceStatus }>
}
export interface StatusIncident {
  id: string
  platform: string
  group_id: number
  group_name: string
  model: string
  severity: ServiceStatus
  phase: IncidentPhase
  started_at: string
  updated_at: string
  resolved_at: string | null
  updates: Array<{ phase: IncidentPhase; severity: ServiceStatus; at: string }>
}
export interface StatusSnapshot {
  computed_at: string
  data_through: string | null
  summary: { status: ServiceStatus; normal: number; affected: number; unknown: number; recovering: number; active_events: number }
  platforms: StatusPlatform[]
  incidents: StatusIncident[]
}

export async function getStatusSnapshot(range: StatusRange, signal?: AbortSignal) {
  const { data } = await apiClient.get<StatusSnapshot>('/channel-monitor-v3/snapshot', { params: { range }, signal })
  return data
}
export async function getStatusConfig() {
  const { data } = await apiClient.get<StatusConfig>('/admin/channel-monitor-v3/config')
  return data
}
export async function updateStatusConfig(config: StatusConfig) {
  const { data } = await apiClient.put<StatusConfig>('/admin/channel-monitor-v3/config', config)
  return data
}
