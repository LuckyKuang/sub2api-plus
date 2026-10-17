export type DocsLocale = 'zh' | 'en'
export type DocsGroupId = 'start' | 'clients' | 'troubleshooting'

export interface DocsText {
  zh: string
  en: string
}

export interface DocsGroup {
  id: DocsGroupId
  title: DocsText
}

export interface DocsSection {
  id: string
  group: DocsGroupId
  title: DocsText
  description: DocsText
}

export const docsGroups: DocsGroup[] = [
  { id: 'start', title: { zh: '开始', en: 'Getting started' } },
  { id: 'clients', title: { zh: '出站客户端', en: 'Outbound clients' } },
  { id: 'troubleshooting', title: { zh: '排障与兼容', en: 'Troubleshooting' } },
]

/**
 * The documentation is intentionally ordered by the outbound client families
 * declared by the gateway (`backend/internal/service/outbound_identity.go`),
 * not by arbitrary third-party GUI clients. MiniMax OAuth and API Key share
 * one article because they describe the same client with two account modes.
 */
export const docsSections: DocsSection[] = [
  {
    id: 'overview',
    group: 'start',
    title: { zh: '快速开始', en: 'Quick start' },
    description: {
      zh: '创建密钥、确认 Base URL，并发送第一个请求。',
      en: 'Create a key, confirm the base URL, and send your first request.',
    },
  },
  {
    id: 'codex',
    group: 'clients',
    title: { zh: 'Codex', en: 'Codex' },
    description: {
      zh: 'Codex CLI / Codex Desktop 的 Responses 接入。',
      en: 'Responses setup for Codex CLI and Codex Desktop.',
    },
  },
  {
    id: 'claude',
    group: 'clients',
    title: { zh: 'Claude Code', en: 'Claude Code' },
    description: {
      zh: 'Anthropic Messages 协议的 Claude Code 配置。',
      en: 'Claude Code configuration for the Anthropic Messages protocol.',
    },
  },
  {
    id: 'gemini',
    group: 'clients',
    title: { zh: 'Gemini CLI', en: 'Gemini CLI' },
    description: {
      zh: 'Gemini CLI 的环境变量与模型设置。',
      en: 'Gemini CLI environment variables and model settings.',
    },
  },
  {
    id: 'grok',
    group: 'clients',
    title: { zh: 'Grok CLI', en: 'Grok CLI' },
    description: {
      zh: 'Grok CLI 的 endpoints、认证与 Responses 配置。',
      en: 'Grok CLI endpoints, authentication, and Responses setup.',
    },
  },
  {
    id: 'antigravity',
    group: 'clients',
    title: { zh: 'Antigravity', en: 'Antigravity' },
    description: {
      zh: 'Antigravity 专用 Claude / Gemini 端点。',
      en: 'Dedicated Antigravity endpoints for Claude and Gemini.',
    },
  },
  {
    id: 'deepseek',
    group: 'clients',
    title: { zh: 'DeepSeek · DSH Desktop', en: 'DeepSeek · DSH Desktop' },
    description: {
      zh: 'DeepSeek / DSH Desktop 的端点与账号模式。',
      en: 'Endpoints and account modes for DeepSeek and DSH Desktop.',
    },
  },
  {
    id: 'minimax',
    group: 'clients',
    title: { zh: 'MiniMax Code', en: 'MiniMax Code' },
    description: {
      zh: 'MiniMax Code 的 OAuth 与 API Key 两种模式。',
      en: 'OAuth and API Key modes for MiniMax Code.',
    },
  },
  {
    id: 'kimi',
    group: 'clients',
    title: { zh: 'Kimi Code', en: 'Kimi Code' },
    description: {
      zh: 'Kimi Code 的端点、模型与设备声明。',
      en: 'Endpoints, models, and device declarations for Kimi Code.',
    },
  },
  {
    id: 'zcode',
    group: 'clients',
    title: { zh: 'GLM · ZCode', en: 'GLM · ZCode' },
    description: {
      zh: 'GLM / ZCode 的客户端连接与账号关联区别。',
      en: 'Client setup for GLM / ZCode and how it differs from account linking.',
    },
  },
  {
    id: 'stepfun',
    group: 'clients',
    title: { zh: 'StepFun · Step-Code', en: 'StepFun · Step-Code' },
    description: {
      zh: 'StepFun / Step-Code 的协议与端点。',
      en: 'Protocols and endpoints for StepFun / Step-Code.',
    },
  },
  {
    id: 'compatibility',
    group: 'troubleshooting',
    title: { zh: '兼容客户端', en: 'Compatible clients' },
    description: {
      zh: 'OpenCode、Cline、Command Code 与 TypeSafe / System One。',
      en: 'OpenCode, Cline, Command Code, and TypeSafe / System One.',
    },
  },
  {
    id: 'network',
    group: 'troubleshooting',
    title: { zh: '网络与代理', en: 'Network and proxy' },
    description: {
      zh: 'Clash / 代理分流与直连排查。',
      en: 'Clash / proxy routing and direct-connection troubleshooting.',
    },
  },
  {
    id: 'faq',
    group: 'troubleshooting',
    title: { zh: '常见问题', en: 'FAQ' },
    description: {
      zh: '401、404、模型不存在与配置不生效。',
      en: '401, 404, missing models, and configuration that does not take effect.',
    },
  },
]

export const defaultDocsSection = docsSections[0]

export function normalizeDocsLocale(locale: string | undefined | null): DocsLocale {
  return locale?.toLowerCase().startsWith('zh') ? 'zh' : 'en'
}

export function findDocsSection(id: string | undefined | null): DocsSection | undefined {
  if (!id) return undefined
  return docsSections.find(section => section.id === id)
}

export function docsSectionIndex(id: string): number {
  return docsSections.findIndex(section => section.id === id)
}

export function docsSectionHref(id: string): string {
  return `/docs/${id}`
}

const zhModules = import.meta.glob('./zh/*.md', {
  query: '?raw',
  import: 'default',
}) as Record<string, () => Promise<string>>
const enModules = import.meta.glob('./en/*.md', {
  query: '?raw',
  import: 'default',
}) as Record<string, () => Promise<string>>

const contentCache = new Map<string, Promise<string>>()

export function loadDocsMarkdown(locale: DocsLocale, id: string): Promise<string> {
  const cacheKey = `${locale}:${id}`
  const cached = contentCache.get(cacheKey)
  if (cached) return cached

  const modules = locale === 'zh' ? zhModules : enModules
  const loader = modules[`./${locale}/${id}.md`]
  if (!loader) {
    return Promise.reject(new Error(`Missing docs content: ${locale}/${id}.md`))
  }
  const request = loader().catch((error) => {
    contentCache.delete(cacheKey)
    throw error
  })
  contentCache.set(cacheKey, request)
  return request
}

/** Test/administration helper: the compiled content keys for a locale. */
export function docsContentKeys(locale: DocsLocale): string[] {
  return Object.keys(locale === 'zh' ? zhModules : enModules).sort()
}
