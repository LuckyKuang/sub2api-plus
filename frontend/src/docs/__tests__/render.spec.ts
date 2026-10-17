import { describe, expect, it } from 'vitest'
import { interpolateDocMarkdown, renderContactInfo, renderDocMarkdown } from '../render'

describe('interpolateDocMarkdown', () => {
  it('replaces known placeholders and preserves unknown ones', () => {
    const result = interpolateDocMarkdown('{{baseUrl}}/v1 {{unknown}}', {
      baseUrl: 'https://api.example.com',
    })

    expect(result).toBe('https://api.example.com/v1 {{unknown}}')
  })

  it('allows whitespace inside placeholders', () => {
    expect(interpolateDocMarkdown('{{  host  }}', { host: 'api.example.com' })).toBe('api.example.com')
  })
})

describe('renderDocMarkdown', () => {
  it('renders headings with stable anchors and a table of contents', () => {
    const { html, toc } = renderDocMarkdown('# Title\n\n## Setup\n\n### Details\n', {})

    expect(html).toContain('id="title"')
    expect(html).toContain('id="setup"')
    expect(html).toContain('id="details"')
    expect(toc).toEqual([
      { id: 'title', text: 'Title', level: 1 },
      { id: 'setup', text: 'Setup', level: 2 },
      { id: 'details', text: 'Details', level: 3 },
    ])
  })

  it('keeps duplicate heading anchors unique', () => {
    const { toc } = renderDocMarkdown('## Setup\n\n## Setup\n', {})

    expect(toc.map(item => item.id)).toEqual(['setup', 'setup-1'])
  })

  it('sanitizes HTML while keeping documentation links and code blocks', () => {
    const { html } = renderDocMarkdown(
      '## Safe\n\n<script>alert(1)</script>\n\n[Guide](/docs/network)\n\n```bash\necho ok\n```\n',
      {},
    )

    expect(html).not.toContain('<script')
    expect(html).toContain('href="/docs/network"')
    expect(html).toContain('<pre>')
  })

  it('interpolates the runtime host inside code blocks', () => {
    const { html } = renderDocMarkdown('```text\nDOMAIN-SUFFIX,{{host}},DIRECT\n```', {
      host: 'api.example.com',
    })

    expect(html).toContain('DOMAIN-SUFFIX,api.example.com,DIRECT')
  })
})

describe('renderContactInfo', () => {
  it('escapes markup and linkifies URLs and email addresses', () => {
    const html = renderContactInfo('QQ: 123\nhttps://example.com/help\nhelp@example.com <b>x</b>')

    expect(html).toContain('https://example.com/help')
    expect(html).toContain('mailto:help@example.com')
    expect(html).not.toContain('<b>')
    expect(html).toContain('&lt;b&gt;')
  })
})
