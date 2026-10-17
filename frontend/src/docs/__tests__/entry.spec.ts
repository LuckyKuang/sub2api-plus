import { describe, expect, it } from 'vitest'
import { resolveDocsEntry } from '../entry'

describe('resolveDocsEntry', () => {
  it('prefers the in-app docs center when enabled', () => {
    expect(resolveDocsEntry({ docs_enabled: true, doc_url: 'https://docs.example.com' })).toEqual({
      visible: true,
      internal: true,
      href: '/docs',
    })
  })

  it('uses the embedded layout only for authenticated embedded navigation', () => {
    expect(
      resolveDocsEntry(
        { docs_enabled: true },
        { authenticated: true, embedded: true },
      ).href,
    ).toBe('/docs?embedded=1')
    expect(
      resolveDocsEntry(
        { docs_enabled: true },
        { authenticated: false, embedded: true },
      ).href,
    ).toBe('/docs')
  })

  it('falls back to a sanitized external documentation URL', () => {
    expect(resolveDocsEntry({ docs_enabled: false, doc_url: 'https://docs.example.com/guide' })).toEqual({
      visible: true,
      internal: false,
      href: 'https://docs.example.com/guide',
    })
  })

  it('rejects unsafe external documentation URLs', () => {
    expect(
      resolveDocsEntry({ docs_enabled: false, doc_url: 'javascript:alert(1)' }),
    ).toEqual({ visible: false, internal: false, href: '' })
  })

  it('hides the entry when neither feature nor external URL is configured', () => {
    expect(resolveDocsEntry({ docs_enabled: false, doc_url: '' })).toEqual({
      visible: false,
      internal: false,
      href: '',
    })
  })
})
