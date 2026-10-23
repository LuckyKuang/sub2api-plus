import { buildApiUrl } from '@/api/url'

export function resolveConsoleBackgroundURL(url: string): string {
  if (/^\/api\/v1\/settings\/console-background\/[a-f0-9]{64}$/.test(url)) return buildApiUrl(url)
  if (url.startsWith('blob:' + location.origin + '/')) return url
  return ''
}
