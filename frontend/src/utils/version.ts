export function normalizeDisplayVersion(version: string | undefined | null): string {
  return (version || '').trim().replace(/^v+/i, '')
}

// OCI/Docker tags do not permit SemVer build metadata's `+` separator.
export function toDockerImageTag(version: string | undefined | null): string {
  const normalized = normalizeDisplayVersion(version)
  return normalized ? `v${normalized.replace(/\+/g, '-')}` : ''
}
