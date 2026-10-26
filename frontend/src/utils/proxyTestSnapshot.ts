import type { Proxy } from '@/types'

// Connectivity/quality observations belong to the saved route that was tested.
// Include updated_at because the server may redact a rotated password.
// This value stays local; never render or log its credential-bearing contents.
export function proxyTestSnapshot(proxy: Proxy): string {
  return JSON.stringify([
    proxy.id, proxy.protocol, proxy.host, proxy.port, proxy.username, proxy.password,
    proxy.status, proxy.expires_at, proxy.fallback_mode, proxy.backup_proxy_id,
    proxy.updated_at
  ])
}
