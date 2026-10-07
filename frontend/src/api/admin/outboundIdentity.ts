import { apiClient } from '../client'

export type IdentityPreset = 'codex' | 'claude' | 'gemini' | 'grok' | 'antigravity' | 'deepseek' | 'minimax' | 'minimax_apikey' | 'kimi' | 'zcode'
export interface IdentitySelection {
  preset: IdentityPreset | ''
  user_agent?: string
  version?: string
  /** DeepSeek/MiniMax timezone metadata; never an additional wire header. */
  timezone?: string
  /** DeepSeek UI language (zh-CN or en-US), rendered only on account-service calls. */
  language?: string
  /** Configured values for the preset's `runtime` declarations only. */
  headers?: Record<string, string>
}
export interface ResolvedIdentity {
  preset: IdentityPreset
  user_agent: string
  originator: string
  version: string
  source: string
  timezone?: string
  /** DeepSeek UI language (zh-CN or en-US), rendered only on account-service calls. */
  language?: string
  headers: Record<string, string>
}
/**
 * One provider-defined identity header a preset renders. `derived` follows the
 * resolved client triple and `pinned` is a fixed provider declaration; neither
 * accepts a configured value. `runtime` describes the host the official client
 * resolves at run time, so the settings and an account selection may set it.
 */
export type IdentityDeclarationClass = 'derived' | 'pinned' | 'runtime'
export interface IdentityDeclaration {
  name: string
  class: IdentityDeclarationClass
  editable: boolean
  builtin: string
  value: string
}
export interface PresetDeclarations {
  preset: IdentityPreset
  headers: IdentityDeclaration[]
}
export interface OutboundIdentitySettings {
  profiles: Partial<Record<IdentityPreset, IdentitySelection>>
  defaults: Record<string, IdentityPreset>
  /** Persisted runtime declarations per preset, keyed by header name. */
  runtime?: Partial<Record<IdentityPreset, Record<string, string>>>
}
export interface OutboundIdentityView {
  settings: OutboundIdentitySettings
  presets: ResolvedIdentity[]
  effective: ResolvedIdentity[]
  control_plane?: ResolvedIdentity[]
  wire_profiles?: (ResolvedIdentity & { protocol: string })[]
  declarations: PresetDeclarations[]
}
export const identityPresets: IdentityPreset[] = ['codex', 'claude', 'gemini', 'grok', 'antigravity', 'deepseek', 'minimax', 'minimax_apikey', 'kimi', 'zcode']
export const identityNames: Record<IdentityPreset, string> = {
  codex: 'Codex', claude: 'Claude Code', gemini: 'Gemini CLI', grok: 'Grok', antigravity: 'Antigravity', deepseek: 'DeepSeek · DSH Desktop', minimax: 'MiniMax Code · OAuth', minimax_apikey: 'MiniMax Code · API Key', kimi: 'Kimi Code', zcode: 'GLM · ZCode'
}
// Mirrors the backend's enumerated versionless client families
// (versionlessOutboundUserAgents in internal/service/outbound_identity.go). The
// official MiniMax client publishes the bare product token with no version
// segment, so these presets expose no client-version control and reject one.
export const versionlessIdentityPresets: IdentityPreset[] = ['minimax']
export async function getOutboundIdentity(): Promise<OutboundIdentityView> {
  return (await apiClient.get<OutboundIdentityView>('/admin/settings/outbound-identity')).data
}
export async function updateOutboundIdentity(settings: OutboundIdentitySettings): Promise<OutboundIdentityView> {
  return (await apiClient.put<OutboundIdentityView>('/admin/settings/outbound-identity', settings)).data
}
export async function previewOutboundIdentity(platform: string, type: string, selection?: IdentitySelection, userAgent?: string): Promise<ResolvedIdentity> {
  return (await apiClient.post<ResolvedIdentity>('/admin/settings/outbound-identity/preview', { platform, type, selection, user_agent: userAgent })).data
}

export const identityEnvironmentHeaders = ['X-Msh-Device-Model', 'X-Msh-Os-Version', 'X-Platform', 'X-Os-Category', 'X-Os-Version']
export const hasIdentityTimezone = (preset: string) => preset === 'deepseek' || preset === 'minimax' || preset === 'minimax_apikey'
