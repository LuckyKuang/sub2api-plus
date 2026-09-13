import { apiClient } from '../client'

export type IdentityPreset = 'codex' | 'claude' | 'gemini' | 'grok' | 'antigravity'
export interface IdentitySelection {
  preset: IdentityPreset | ''
  user_agent?: string
  version?: string
}
export interface ResolvedIdentity {
  preset: IdentityPreset
  user_agent: string
  originator: string
  version: string
  source: string
  headers: Record<string, string>
}
export interface OutboundIdentitySettings {
  profiles: Partial<Record<IdentityPreset, IdentitySelection>>
  defaults: Record<string, IdentityPreset>
}
export interface OutboundIdentityView {
  settings: OutboundIdentitySettings
  presets: ResolvedIdentity[]
  effective: ResolvedIdentity[]
}
export const identityPresets: IdentityPreset[] = ['codex', 'claude', 'gemini', 'grok', 'antigravity']
export const identityNames: Record<IdentityPreset, string> = {
  codex: 'Codex', claude: 'Claude Code', gemini: 'Gemini CLI', grok: 'Grok', antigravity: 'Antigravity'
}
export async function getOutboundIdentity(): Promise<OutboundIdentityView> {
  return (await apiClient.get<OutboundIdentityView>('/admin/settings/outbound-identity')).data
}
export async function updateOutboundIdentity(settings: OutboundIdentitySettings): Promise<OutboundIdentityView> {
  return (await apiClient.put<OutboundIdentityView>('/admin/settings/outbound-identity', settings)).data
}
export async function previewOutboundIdentity(platform: string, type: string, selection?: IdentitySelection, userAgent?: string): Promise<ResolvedIdentity> {
  return (await apiClient.post<ResolvedIdentity>('/admin/settings/outbound-identity/preview', { platform, type, selection, user_agent: userAgent })).data
}
