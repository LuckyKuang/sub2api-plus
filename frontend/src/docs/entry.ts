import { sanitizeUrl } from '@/utils/url'
import type { PublicSettings } from '@/types'

export interface DocsEntry {
  /** Whether any documentation entry should render. */
  visible: boolean
  /** True when the entry opens the in-app docs center. */
  internal: boolean
  /** Router path or sanitized external URL. */
  href: string
}

export interface DocsEntryOptions {
  authenticated?: boolean
  /** Use the embedded panel layout when the visitor is signed in. */
  embedded?: boolean
  /** External documentation link configured in Site Settings. */
  externalUrl?: string
}

/**
 * Resolve the single documentation entry used by the header, home page and
 * key-usage page. The in-app docs center wins whenever it is enabled; the
 * legacy `doc_url` remains a fallback and is surfaced inside the docs footer.
 *
 * The in-app entry stays visible for anonymous visitors even when the docs
 * center requires login so the route guard can redirect them to the login page
 * with a return path. This mirrors the Model Plaza entry.
 */
export function resolveDocsEntry(
  settings: Partial<PublicSettings> | null | undefined,
  options: DocsEntryOptions = {},
): DocsEntry {
  const externalUrl = sanitizeUrl(options.externalUrl ?? settings?.doc_url ?? '')
  const enabled = settings?.docs_enabled === true
  if (enabled) {
    const embedded = options.authenticated === true && options.embedded === true
    return {
      visible: true,
      internal: true,
      href: embedded ? '/docs?embedded=1' : '/docs',
    }
  }
  if (externalUrl) {
    return { visible: true, internal: false, href: externalUrl }
  }
  return { visible: false, internal: false, href: '' }
}
