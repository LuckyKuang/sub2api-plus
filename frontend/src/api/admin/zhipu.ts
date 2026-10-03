import { apiClient } from '../client'

/**
 * Zhipu / GLM ZCode account-link API.
 *
 * The link is a server-side polling handshake: the panel opens a session, the
 * operator authorizes in a browser, and the server polls the platform. The poll
 * credential never reaches the panel, so only the opaque session handle is
 * carried here.
 */

/** Upstream estates. They differ in domain and in business-token semantics. */
export type ZhipuProvider = 'bigmodel' | 'zai'

/** Linked subscription kinds. The credential differs per plan. */
export type ZhipuPlanKind =
  | 'individual-coding-plan'
  | 'team-coding-plan'
  | 'start-plan'
  | 'off-peak'

export const zhipuProviders: ZhipuProvider[] = ['bigmodel', 'zai']
export const zhipuPlanKinds: ZhipuPlanKind[] = [
  'individual-coding-plan',
  'team-coding-plan',
  'start-plan',
  'off-peak'
]

export interface ZhipuOAuthCapabilities {
  enabled: boolean
  providers: ZhipuProvider[]
  plan_kinds: ZhipuPlanKind[]
  supported_plan_kinds: ZhipuPlanKind[]
  handshake_url: string
}

export interface ZhipuLinkSession {
  session_id: string
  provider: ZhipuProvider
  authorize_url: string
  expires_at: string
  interval_seconds: number
}

export interface ZhipuLinkUser {
  user_id: string
  user_name?: string
  user_email?: string
  avatar?: string
}

/** Token material returned once the authorization completes. */
export interface ZhipuLinkToken {
  provider: ZhipuProvider
  access_token: string
  refresh_token?: string
  zcode_jwt_token?: string
  user: ZhipuLinkUser
}

export interface ZhipuLinkPoll {
  pending: boolean
  ready?: ZhipuLinkToken
}

export interface ZhipuCreateAccountRequest {
  session_id: string
  plan_kind: ZhipuPlanKind
  team_organization?: string
  team_project?: string
  provider: ZhipuProvider
  access_token?: string
  refresh_token?: string
  zcode_jwt_token?: string
  name?: string
  concurrency?: number
  priority?: number
  proxy_id?: number
  group_ids?: number[]
}

const endpointPrefix = '/admin/zhipu/oauth'

export async function getZhipuOAuthCapabilities(): Promise<ZhipuOAuthCapabilities> {
  const { data } = await apiClient.get<ZhipuOAuthCapabilities>(`${endpointPrefix}/capabilities`)
  return data
}

export async function startZhipuLink(payload: { provider: ZhipuProvider; proxy_id?: number; account_id?: number }): Promise<ZhipuLinkSession> {
  const { data } = await apiClient.post<ZhipuLinkSession>(`${endpointPrefix}/start`, payload)
  return data
}

export async function pollZhipuLink(sessionId: string): Promise<ZhipuLinkPoll> {
  const { data } = await apiClient.post<ZhipuLinkPoll>(`${endpointPrefix}/poll`, { session_id: sessionId })
  return data
}

export async function exchangeZhipuLink(sessionId: string, callback: string, proxyId?: number): Promise<ZhipuLinkPoll> {
  const { data } = await apiClient.post<ZhipuLinkPoll>(`${endpointPrefix}/exchange-code`, {
    session_id: sessionId,
    callback,
    ...(proxyId ? { proxy_id: proxyId } : {})
  })
  return data
}

export async function createZhipuAccountFromLink(payload: ZhipuCreateAccountRequest): Promise<unknown> {
  const { data } = await apiClient.post(`${endpointPrefix}/create-from-oauth`, payload)
  return data
}

export const zhipuOAuthAPI = {
  getZhipuOAuthCapabilities,
  startZhipuLink,
  pollZhipuLink,
  exchangeZhipuLink,
  createZhipuAccountFromLink
}

export default zhipuOAuthAPI
