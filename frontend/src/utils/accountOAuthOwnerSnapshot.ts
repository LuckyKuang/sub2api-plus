import type { Account } from '@/types'
import { proxyTestSnapshot } from './proxyTestSnapshot'

// This credential-bearing comparison stays local and must never be rendered,
// logged or used as a DOM key. Unrelated usage polling does not revoke a login.
export function accountOAuthOwnerSnapshot(account: Account | null): string {
  if (!account) return ''
  if (account.oauth_credential_owner) {
    // The server includes the credential, saved route and recovery state while
    // excluding names and observation timestamps that must not revoke a login.
    return JSON.stringify([account.id, account.platform, account.type, account.oauth_credential_owner])
  }
  return JSON.stringify([
    account.id, account.platform, account.type, account.credentials,
    account.proxy_id, account.proxy ? proxyTestSnapshot(account.proxy) : null,
  ])
}
