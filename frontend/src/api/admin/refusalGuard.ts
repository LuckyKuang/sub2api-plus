import { apiClient } from '../client'

export interface RefusalRule {
  id: string
  name: string
  enabled: boolean
  status_codes: number[]
  error_codes: string[]
  error_types: string[]
  message_keywords: string[]
}
export interface RefusalConfig {
  enabled: boolean
  user_threshold: number
  count_window_seconds: number
  user_cooldown_seconds: number
  session_block_ttl_seconds: number
  rules: RefusalRule[]
}
export interface RefusalUser {
  user_id: number
  email: string
  count: number
  count_ttl_seconds: number
  cooldown_until: string
  cooldown_remaining_seconds: number
}
export interface RefusalSession {
  block_key: string
  user_id: number
  api_key_id: number
  session_id: string
  request_id: string
  rule_id: string
  model: string
  created_at: string
  remaining_seconds: number
}
const base = '/admin/risk-control/refusal-guard'
export const refusalGuardAPI = {
  async getConfig(): Promise<RefusalConfig> { const { data } = await apiClient.get<{ data: RefusalConfig }>(`${base}/config`); return data.data },
  async updateConfig(config: RefusalConfig): Promise<RefusalConfig> { const { data } = await apiClient.put<{ data: RefusalConfig }>(`${base}/config`, config); return data.data },
  async listUsers(): Promise<RefusalUser[]> { const { data } = await apiClient.get<{ data: RefusalUser[] }>(`${base}/users`); return data.data },
  async clearUser(id: number): Promise<void> { await apiClient.delete(`${base}/users/${id}`) },
  async listSessions(page = 1, userID?: number): Promise<{ data: RefusalSession[]; total: number }> {
    const { data } = await apiClient.get<{ data: RefusalSession[]; total: number }>(`${base}/sessions`, { params: { page, user_id: userID } }); return data
  },
  async clearSession(blockKey: string): Promise<void> { await apiClient.delete(`${base}/sessions`, { data: { block_key: blockKey } }) },
  async clearAll(): Promise<void> { await apiClient.delete(`${base}/all`) },
}
export default refusalGuardAPI
