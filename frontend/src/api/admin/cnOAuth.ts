import { apiClient } from '../client'

export type CNOAuthPlatform = 'deepseek' | 'kimi' | 'minimax' | 'stepfun'
export function isCNOAuthPlatform(platform: string): platform is CNOAuthPlatform {
  return platform === 'deepseek' || platform === 'kimi' || platform === 'minimax' || platform === 'stepfun'
}
export interface CNOAuthSession {
  session_id: string
  authorize_url: string
  user_code?: string
  expires_at: string
  interval_seconds: number
  status: 'pending' | 'ready' | 'completed' | 'cancelled'
  account_id?: number
}
export interface CNOAuthAccountInput {
  name?: string
  concurrency?: number
  priority?: number
  group_ids?: number[]
  model_mapping?: Record<string, string>
}
export async function cnOAuthModels(sessionId: string): Promise<{ models: string[] }> {
  const { data } = await apiClient.post<{ models: string[] }>('/admin/cn/oauth/stepfun/models', { session_id: sessionId })
  return data
}
export async function cnOAuthRequest(
  platform: CNOAuthPlatform,
  action: 'start' | 'poll' | 'exchange' | 'cancel' | 'complete',
  input: Record<string, unknown>
): Promise<CNOAuthSession> {
  const { data } = await apiClient.post<CNOAuthSession>(`/admin/cn/oauth/${platform}/${action}`, input)
  return data
}
