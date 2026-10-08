import { apiClient } from '../client'

export type CNOAuthPlatform = 'deepseek' | 'kimi' | 'minimax' | 'stepfun'
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
}
export async function cnOAuthRequest(
  platform: CNOAuthPlatform,
  action: 'start' | 'poll' | 'exchange' | 'cancel' | 'complete',
  input: Record<string, unknown>
): Promise<CNOAuthSession> {
  const { data } = await apiClient.post<CNOAuthSession>(`/admin/cn/oauth/${platform}/${action}`, input)
  return data
}
