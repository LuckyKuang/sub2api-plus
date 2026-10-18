import { describe, expect, it } from 'vitest'
import {
  docsGroups,
  docsSections,
  loadDocsMarkdown,
  type DocsLocale,
} from '../manifest'

const zhContent = import.meta.glob('../zh/*.md', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>
const enContent = import.meta.glob('../en/*.md', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

function contentKeys(content: Record<string, string>): string[] {
  return Object.keys(content)
    .map(path => path.split('/').pop()?.replace(/\.md$/, '') ?? '')
    .sort()
}

function hasOsVariant(content: string, os: string): boolean {
  return content.includes(`os=${os}`) || content.includes(`class="docs-os-line" data-os="${os}"`)
}

function sectionIds(): string[] {
  return docsSections.map(section => section.id).sort()
}

describe('docs manifest', () => {
  it('has unique section IDs and known groups', () => {
    expect(new Set(docsSections.map(section => section.id)).size).toBe(docsSections.length)
    const groupIds = new Set(docsGroups.map(group => group.id))
    for (const section of docsSections) {
      expect(groupIds.has(section.group)).toBe(true)
      for (const locale of ['zh', 'en'] as DocsLocale[]) {
        expect(section.title[locale].trim()).not.toBe('')
        expect(section.description[locale].trim()).not.toBe('')
      }
    }
  })

  it('ships exactly one Chinese and one English file per section', () => {
    expect(contentKeys(zhContent)).toEqual(sectionIds())
    expect(contentKeys(enContent)).toEqual(sectionIds())
  })

  it('covers every outbound client family and merges the two MiniMax modes', () => {
    const ids = sectionIds()
    for (const preset of [
      'codex',
      'claude',
      'gemini',
      'grok',
      'antigravity',
      'deepseek',
      'minimax',
      'kimi',
      'zcode',
      'stepfun',
    ]) {
      expect(ids).toContain(preset)
    }
    expect(ids).not.toContain('minimax_apikey')
    expect(ids).toContain('compatibility')
    expect(ids).toContain('ccswitch')
    expect(ids).toContain('network')
  })

  it('loads the same section from both locale bundles', async () => {
    const zh = await loadDocsMarkdown('zh', 'overview')
    const en = await loadDocsMarkdown('en', 'overview')

    expect(zh).toContain('# 快速开始')
    expect(en).toContain('# Quick start')
  })

  it('rejects unknown sections instead of rendering an empty page', async () => {
    await expect(loadDocsMarkdown('zh', 'missing')).rejects.toThrow('Missing docs content')
  })

  it('gives every client article all three operating-system variants', () => {
    for (const id of ['overview', 'codex', 'claude', 'gemini', 'grok', 'antigravity', 'deepseek', 'minimax', 'kimi', 'stepfun']) {
      for (const os of ['unix', 'cmd', 'powershell']) {
        expect(hasOsVariant(zhContent[`../zh/${id}.md`], os)).toBe(true)
        expect(hasOsVariant(enContent[`../en/${id}.md`], os)).toBe(true)
      }
    }
  })

  it('documents ZCode as a graphical client without shell-specific configuration', () => {
    expect(zhContent['../zh/zcode.md']).toContain('图形客户端')
    expect(enContent['../en/zcode.md']).toContain('graphical client')
    expect(zhContent['../zh/zcode.md']).toContain('Model Providers')
    expect(enContent['../en/zcode.md']).toContain('Model Providers')
  })

  it('links every client guide to its official documentation', () => {
    const officialLinks: Record<string, string> = {
      codex: 'developers.openai.com/codex/config-reference',
      claude: 'code.claude.com/docs/en/env-vars',
      gemini: 'github.com/google-gemini/gemini-cli/blob/main/docs/reference/configuration.md',
      grok: 'docs.x.ai/build/settings/reference',
      antigravity: 'antigravity.google/docs/cli/using',
      deepseek: 'deepseek-harness.github.io/deepseek-harness/',
      minimax: 'agent.minimax.io/docs/cli/configuration',
      kimi: 'moonshotai.github.io/kimi-code/en/configuration/providers',
      zcode: 'docs.z.ai/devpack/tool/zcode',
      stepfun: 'platform.stepfun.com/docs/zh/step-code/configuration/models',
    }
    for (const [id, link] of Object.entries(officialLinks)) {
      expect(zhContent[`../zh/${id}.md`]).toContain(link)
      expect(enContent[`../en/${id}.md`]).toContain(link)
    }
  })

  it('uses the OpenAI provider name in the Codex guide for remote compaction', () => {
    for (const content of [zhContent['../zh/codex.md'], enContent['../en/codex.md']]) {
      expect(content).toContain('model_provider = "OpenAI"')
      expect(content).toContain('[model_providers.OpenAI]')
      expect(content).toContain('name = "OpenAI"')
      expect(content).not.toContain('model_providers.sub2api')
    }
  })

  it('starts every article with a level-one heading', () => {
    for (const content of [zhContent, enContent]) {
      for (const source of Object.values(content)) {
        expect(source.trimStart()).toMatch(/^# /)
      }
    }
  })

  it('only uses documented runtime placeholders', () => {
    const allowed = new Set([
      'baseUrl',
      'openaiBaseUrl',
      'host',
      'siteName',
      'keysUrl',
      'modelPlazaUrl',
    ])
    for (const content of [zhContent, enContent]) {
      for (const source of Object.values(content)) {
        for (const match of source.matchAll(/\{\{\s*([a-zA-Z][a-zA-Z0-9]*)\s*\}\}/g)) {
          expect(allowed.has(match[1])).toBe(true)
        }
      }
    }
  })

  it('never hard-codes the upstream documentation domain', () => {
    for (const content of [zhContent, enContent]) {
      for (const source of Object.values(content)) {
        expect(source.toLowerCase()).not.toContain('heiyucode.com')
      }
    }
  })

  it('derives the Clash rule from the runtime host', () => {
    expect(zhContent['../zh/network.md']).toContain('DOMAIN-SUFFIX,{{host}},DIRECT')
    expect(enContent['../en/network.md']).toContain('DOMAIN-SUFFIX,{{host}},DIRECT')
  })
})
