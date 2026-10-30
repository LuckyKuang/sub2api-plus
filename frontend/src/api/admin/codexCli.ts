import { apiClient } from '../client'

export type CodexCLISecretName = 'access_token' | 'refresh_token' | 'id_token'

export interface CodexCLIAccountView {
  enabled: boolean
  config_toml: string
  auth: {
    auth_mode: 'chatgpt' | 'apikey'
    tokens: { account_id?: string }
  }
  auth_status: Record<`has_${CodexCLISecretName}`, boolean>
  expected_owner: string
  oauth_credential_owner: string
  availability: {
    available: true
    code: 'http_bridge_and_tasks' | 'http_bridge_only'
    reason: string
  }
}

export interface CodexCLIAccountPatch {
  expected_owner: string
  enabled?: boolean
  config_toml: string
  auth?: {
    auth_mode?: 'chatgpt'
    tokens?: Partial<Record<CodexCLISecretName | 'account_id', string | null>>
  }
}

export async function getCodexCLIAccount(
  accountID: number,
  options?: { signal?: AbortSignal },
): Promise<CodexCLIAccountView> {
  const { data } = await apiClient.get<CodexCLIAccountView>(
    `/admin/accounts/${accountID}/codex-cli`,
    { signal: options?.signal },
  )
  return data
}

export async function updateCodexCLIAccount(
  accountID: number,
  patch: CodexCLIAccountPatch,
): Promise<CodexCLIAccountView> {
  if (!/^[0-9a-f]{64}$/.test(patch.expected_owner)) {
    throw new Error('Account ownership is unavailable; reopen the account and retry')
  }
  const { data } = await apiClient.put<CodexCLIAccountView>(
    `/admin/accounts/${accountID}/codex-cli`,
    patch,
  )
  return data
}
