import { describe, expect, it, vi } from 'vitest'
import { resolveConsoleBackgroundURL } from '../consoleAppearance'

vi.mock('@/api/url', () => ({ buildApiUrl: (path: string) => 'https://configured-api.example' + path }))

describe('console background location', () => {
  it('uses the configured API origin for a published image', () => {
    const path = '/api/v1/settings/console-background/' + 'a'.repeat(64)
    expect(resolveConsoleBackgroundURL(path)).toBe('https://configured-api.example' + path)
  })
  it('accepts this page’s draft blobs and rejects arbitrary remote, data and executable URLs', () => {
    const blob = 'blob:' + location.origin + '/local-draft'
    expect(resolveConsoleBackgroundURL(blob)).toBe(blob)
    for (const invalid of ['', '//example.com/a', 'https://example.com/a', 'javascript:alert(1)', 'data:image/svg+xml;base64,eA==', 'blob:https://other.example/a']) expect(resolveConsoleBackgroundURL(invalid)).toBe('')
  })
})
