/**
 * Shared helpers for the OAuth callback views (LinuxDo / OIDC / WeChat / DingTalk).
 * The provider-specific views own their state; these functions only carry the
 * provider-independent parsing, sanitizing and state-machine rules.
 */

import type { OAuthTokenResponse } from '@/api/auth'

export type PendingAccountAction = 'none' | 'choose_account_action' | 'create_account' | 'bind_login'

export interface PendingOAuthCompletionFields {
  access_token?: string
  refresh_token?: string
  expires_in?: number
  token_type?: string
  auth_result?: string
  redirect?: string
  error?: string
  step?: string
  status?: string
  state?: string
  intent?: string
  requires_2fa?: boolean
  temp_token?: string
  user_email_masked?: string
  adoption_required?: boolean
  suggested_display_name?: string
  suggested_avatar_url?: string
  pending_email?: string
  resolved_email?: string
  existing_account_email?: string
  compat_email?: string
  email?: string
  suggested_email?: string
  provider?: string
}

export interface AdoptionSuggestionFields {
  adoption_required?: boolean
  suggested_display_name?: string
  suggested_avatar_url?: string
}

/**
 * Each provider resolves the pending account action from a slightly different
 * alias set and reads the suggested email from a different field order, so the
 * aliases stay per-provider instead of being merged into one superset.
 */
export interface PendingOAuthStateAliases {
  emailFields: Array<keyof PendingOAuthCompletionFields>
  bindLoginStates: string[]
}

export const LINUXDO_STATE_ALIASES: PendingOAuthStateAliases = {
  emailFields: ['pending_email', 'existing_account_email', 'email', 'resolved_email', 'suggested_email'],
  bindLoginStates: [
    'bind_login_required',
    'bind_login',
    'existing_account',
    'existing_account_required',
    'existing_account_binding_required',
    'adopt_existing_user_by_email'
  ]
}

export const OIDC_STATE_ALIASES: PendingOAuthStateAliases = {
  emailFields: [
    'pending_email',
    'existing_account_email',
    'compat_email',
    'resolved_email',
    'email',
    'suggested_email'
  ],
  bindLoginStates: [
    'bind_login_required',
    'bind_login',
    'existing_account_binding_required',
    'existing_account_required',
    'adopt_existing_user_by_email'
  ]
}

export const DINGTALK_STATE_ALIASES: PendingOAuthStateAliases = {
  emailFields: ['pending_email', 'existing_account_email', 'resolved_email', 'suggested_email'],
  bindLoginStates: ['bind_login_required', 'bind_login', 'adopt_existing_user_by_email']
}

export const WECHAT_STATE_ALIASES: PendingOAuthStateAliases = {
  emailFields: ['pending_email', 'existing_account_email', 'resolved_email'],
  bindLoginStates: ['adopt_existing_user_by_email', 'bind_login_required', 'bind_login']
}

const CHOICE_STATES = [
  'choice',
  'choose_account_action_required',
  'choose_account_action',
  'choose_account',
  'choose'
]

const CREATE_ACCOUNT_STATES = ['email_required', 'create_account_required', 'create_account']

export function parseFragmentParams(): URLSearchParams {
  const raw = typeof window !== 'undefined' ? window.location.hash : ''
  const hash = raw.startsWith('#') ? raw.slice(1) : raw
  return new URLSearchParams(hash)
}

export function readLegacyFragmentLogin(params: URLSearchParams): OAuthTokenResponse | null {
  const accessToken = params.get('access_token')?.trim() || ''
  if (!accessToken) {
    return null
  }

  const completion: OAuthTokenResponse = {
    access_token: accessToken
  }
  const refreshToken = params.get('refresh_token')?.trim() || ''
  if (refreshToken) {
    completion.refresh_token = refreshToken
  }
  const expiresIn = Number.parseInt(params.get('expires_in')?.trim() || '', 10)
  if (Number.isFinite(expiresIn) && expiresIn > 0) {
    completion.expires_in = expiresIn
  }
  const tokenType = params.get('token_type')?.trim() || ''
  if (tokenType) {
    completion.token_type = tokenType
  }
  return completion
}

export function sanitizeRedirectPath(path: string | null | undefined): string {
  if (!path) return '/dashboard'
  if (!path.startsWith('/')) return '/dashboard'
  if (path.startsWith('//')) return '/dashboard'
  if (path.includes('://')) return '/dashboard'
  if (path.includes('\n') || path.includes('\r')) return '/dashboard'
  return path
}

export function serializeAdoptionDecision(decision: {
  adoptDisplayName?: boolean
  adoptAvatar?: boolean
}): Record<string, boolean> {
  const payload: Record<string, boolean> = {}
  if (typeof decision.adoptDisplayName === 'boolean') {
    payload.adopt_display_name = decision.adoptDisplayName
  }
  if (typeof decision.adoptAvatar === 'boolean') {
    payload.adopt_avatar = decision.adoptAvatar
  }
  return payload
}

export function hasSuggestedProfile(completion: AdoptionSuggestionFields): boolean {
  return Boolean(completion.suggested_display_name || completion.suggested_avatar_url)
}

export function normalizedPendingState(value: string | null | undefined): string {
  return value?.trim().toLowerCase() || ''
}

export function resolvePendingAccountAction(
  completion: { step?: string; status?: string; state?: string; error?: string; intent?: string },
  aliases: PendingOAuthStateAliases
): PendingAccountAction {
  const raw = normalizedPendingState(
    completion.step || completion.status || completion.state || completion.error || completion.intent
  )
  if (CHOICE_STATES.includes(raw)) {
    return 'choose_account_action'
  }
  if (CREATE_ACCOUNT_STATES.includes(raw)) {
    return 'create_account'
  }
  if (aliases.bindLoginStates.includes(raw)) {
    return 'bind_login'
  }
  return 'none'
}

export function extractPendingAccountEmail(
  completion: PendingOAuthCompletionFields,
  aliases: PendingOAuthStateAliases,
  fallbackEmail = ''
): string {
  for (const field of aliases.emailFields) {
    const value = completion[field]
    if (typeof value === 'string' && value.trim()) {
      return value.trim()
    }
  }
  return fallbackEmail.trim()
}

export function getRequestErrorMessage(error: unknown, fallback: string): string {
  const err = error as { message?: string; response?: { data?: { detail?: string; message?: string } } }
  return err.response?.data?.detail || err.response?.data?.message || err.message || fallback
}

export function isCreateAccountRecoveryError(error: unknown): boolean {
  const data = (
    error as {
      response?: {
        data?: {
          reason?: string
          error?: string
          code?: string
          step?: string
          intent?: string
        }
      }
    }
  ).response?.data
  const states = [data?.reason, data?.error, data?.code, data?.step, data?.intent]
    .map((value) => value?.trim().toLowerCase())
    .filter((value): value is string => Boolean(value))

  return states.includes('email_exists') ||
    states.includes('bind_login_required') ||
    states.includes('bind_login') ||
    states.includes('adopt_existing_user_by_email') ||
    states.includes('existing_account_required') ||
    states.includes('existing_account_binding_required')
}
