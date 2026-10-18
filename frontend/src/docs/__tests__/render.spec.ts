import { describe, expect, it } from 'vitest'
import { extractOsCodeBlocks, hasOsCodeBlocks, interpolateDocMarkdown, renderContactInfo, renderDocMarkdown } from '../render'

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

describe('OS-tagged code blocks', () => {
  it('detects and wraps each operating-system variant', () => {
    const source = '```bash os=unix\nexport A=1\n```\n\n```bat os=cmd\nset A=1\n```\n\n```powershell os=powershell\n$env:A=\"1\"\n```'
    expect(hasOsCodeBlocks(source)).toBe(true)

    const wrapped = extractOsCodeBlocks(source)
    expect(wrapped).toContain('data-os="unix"')
    expect(wrapped).toContain('data-os="cmd"')
    expect(wrapped).toContain('data-os="powershell"')
    expect(wrapped).toContain('data-os-label="Windows CMD"')
  })

  it('leaves ordinary code fences untouched', () => {
    const source = '```json\n{"model":"YOUR_MODEL_ID"}\n```'
    expect(hasOsCodeBlocks(source)).toBe(false)
    expect(extractOsCodeBlocks(source)).toBe(source)
  })

  it('escapes OS block content and keeps the fence language', () => {
    const wrapped = extractOsCodeBlocks('```bash os=unix\necho "<key>"\n```')
    expect(wrapped).toContain('language-bash')
    expect(wrapped).toContain('&lt;key&gt;')
    expect(wrapped).not.toContain('<key>')
  })

  it('keeps OS-scoped prose lines and detects them as OS content', () => {
    const source = '<div class="docs-os-line" data-os="unix">macOS / Linux: <code>~/.codex/config.toml</code></div>'
    expect(hasOsCodeBlocks(source)).toBe(true)

    const { html } = renderDocMarkdown(source, {})
    expect(html).toContain('class="docs-os-line"')
    expect(html).toContain('data-os="unix"')
    expect(html).toContain('<code>~/.codex/config.toml</code>')
    expect(html).not.toContain('&lt;div')
    expect(html).not.toContain('<p><div')
  })

  it('renders OS-tagged blocks as pre elements with labels', () => {
    const { html } = renderDocMarkdown('```bash os=unix\nexport A=1\n```', {})
    expect(html).toContain('<pre class="docs-code" data-os="unix" data-os-label="macOS / Linux">')
    expect(html).not.toContain('<p><pre')
    expect(html).not.toContain('```')
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
