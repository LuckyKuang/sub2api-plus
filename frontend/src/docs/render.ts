import DOMPurify from 'dompurify'
import { marked } from 'marked'

marked.setOptions({
  gfm: true,
  breaks: false,
})

export interface DocsTocItem {
  id: string
  text: string
  level: number
}

export interface RenderedDocsMarkdown {
  html: string
  toc: DocsTocItem[]
}

const PLACEHOLDER = /\{\{\s*([a-zA-Z][a-zA-Z0-9]*)\s*\}\}/g

/**
 * Replace `{{variable}}` placeholders before Markdown parsing. Values come from
 * trusted server settings (API base URL, site name), so they are not escaped
 * here; the rendered HTML is sanitized afterwards.
 */
export function interpolateDocMarkdown(
  source: string,
  variables: Record<string, string>,
): string {
  return source.replace(PLACEHOLDER, (match, name: string) => {
    const value = variables[name]
    return value === undefined ? match : value
  })
}

function slugifyHeading(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^\w\u4e00-\u9fff]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

/**
 * Render one documentation article. Heading IDs and the table of contents are
 * derived from the rendered HTML so every heading gets a stable anchor.
 */
export function renderDocMarkdown(
  source: string,
  variables: Record<string, string>,
): RenderedDocsMarkdown {
  const interpolated = interpolateDocMarkdown(source, variables)
  const parsed = marked.parse(interpolated) as string
  const sanitized = DOMPurify.sanitize(parsed, {
    ADD_ATTR: ['target', 'rel'],
  })

  const toc: DocsTocItem[] = []
  const usedIds = new Set<string>()
  let headingIndex = 0
  const html = sanitized.replace(
    /<(h[1-4])[^>]*>(.*?)<\/h[1-4]>/gi,
    (_match, tag: string, content: string) => {
      const level = Number(tag[1])
      const text = content.replace(/<[^>]+>/g, '').trim()
      const base = slugifyHeading(text) || `heading-${headingIndex}`
      let id = base
      if (usedIds.has(id)) {
        id = `${base}-${headingIndex}`
      }
      usedIds.add(id)
      headingIndex += 1
      toc.push({ id, text, level })
      return `<${tag} id="${id}">${content}</${tag}>`
    },
  )

  return { html, toc }
}

/** Turn plain text into a safe, linkified contact block. */
export function renderContactInfo(value: string): string {
  const escaped = value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')

  return escaped
    .replace(
      /\bhttps?:\/\/[^\s<]+/gi,
      url => `<a href="${url}" target="_blank" rel="noopener noreferrer">${url}</a>`,
    )
    .replace(
      /\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b/gi,
      email => `<a href="mailto:${email}">${email}</a>`,
    )
}
