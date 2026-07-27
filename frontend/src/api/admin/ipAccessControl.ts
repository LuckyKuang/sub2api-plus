import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export type IPAccessRuleKind = 'manual_block' | 'auto_block' | 'allow'
export type IPAccessRuleStatus = 'active' | 'released' | 'expired'

export interface IPAccessControlSettings {
  enforcement_enabled: boolean
  login_failure_auto_block_enabled: boolean
  login_failure_threshold: number
  login_failure_window_minutes: number
  login_failure_block_minutes: number
}

export type TrustedProxyConfigurationState =
  | 'configured'
  | 'not_configured'
  | 'empty'
  | 'invalid'

export interface TrustedProxyStatus {
  configuration_state: TrustedProxyConfigurationState
  trusted_proxies: string[]
  client_ip: string
  direct_peer_ip: string
  direct_peer_trusted: boolean
  trusted_proxy_applied: boolean
  forwarded_headers: string[]
  identity_source: 'direct' | 'trusted_forwarded' | ''
  safe_for_enforcement: boolean
  failure_reason?: string
  legacy_forwarded_mode: boolean
  emergency_allowlist_configured: boolean
  emergency_allowlist_count: number
  automatic_blocking_ready: boolean
  manual_blocking_ready: boolean
}

export interface IPAccessRule {
  id: number
  ip_or_cidr: string
  rule_kind: IPAccessRuleKind
  status: IPAccessRuleStatus
  reason: string
  failure_count: number
  first_failed_at?: string
  last_failed_at?: string
  blocked_at?: string
  expires_at?: string
  last_seen_at?: string
  hit_count: number
  created_at: string
  updated_at: string
}

export interface IPAccessRuleQuery {
  page?: number
  page_size?: number
  status?: IPAccessRuleStatus
  query?: string
}

export interface IPLoginFailureState {
  normalized_ip: string
  failure_count: number
  window_started_at: string
  last_failed_at: string
  window_expires_at: string
  currently_blocked: boolean
  auto_block_rule_id?: number
}

export interface IPLoginFailureStateQuery {
  page?: number
  page_size?: number
  query?: string
}

export interface CreateIPAccessRuleRequest {
  ip_or_cidr: string
  rule_kind: Extract<IPAccessRuleKind, 'manual_block' | 'allow'>
  reason?: string
  expires_at?: string
}

async function getSettings(): Promise<IPAccessControlSettings> {
  const { data } = await apiClient.get('/admin/ip-access-control/settings')
  return data
}

async function getTrustedProxyStatus(): Promise<TrustedProxyStatus> {
  const { data } = await apiClient.get('/admin/ip-access-control/trusted-proxy-status')
  return data
}

async function updateSettings(settings: IPAccessControlSettings): Promise<IPAccessControlSettings> {
  const { data } = await apiClient.put('/admin/ip-access-control/settings', settings)
  return data
}

async function listRules(params: IPAccessRuleQuery): Promise<PaginatedResponse<IPAccessRule>> {
  const { data } = await apiClient.get('/admin/ip-access-control/rules', { params })
  return data
}

async function listFailureStates(
  params: IPLoginFailureStateQuery
): Promise<PaginatedResponse<IPLoginFailureState>> {
  const { data } = await apiClient.get('/admin/ip-access-control/failure-states', { params })
  return data
}

async function createRule(payload: CreateIPAccessRuleRequest): Promise<IPAccessRule> {
  const { data } = await apiClient.post('/admin/ip-access-control/rules', payload)
  return data
}

async function releaseRuleAndReset(id: number): Promise<IPAccessRule> {
  const { data } = await apiClient.post(`/admin/ip-access-control/rules/${id}/release-reset`)
  return data
}

async function resetFailureState(ip: string): Promise<{ ip: string }> {
  const { data } = await apiClient.post('/admin/ip-access-control/failure-state/reset', { ip })
  return data
}

const ipAccessControlAPI = {
  getSettings,
  getTrustedProxyStatus,
  updateSettings,
  listFailureStates,
  listRules,
  createRule,
  releaseRuleAndReset,
  resetFailureState
}

export default ipAccessControlAPI
