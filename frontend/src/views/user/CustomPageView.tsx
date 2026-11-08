import { useEffect, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { useAppStore } from '@/stores/app'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import { buildApiUrl } from '@/api/client'
import { buildEmbeddedUrl, detectTheme } from '@/utils/embedded-url'
import { Icon } from './icons'
import { useUserView } from './useUserView'

interface TocItem {
  id: string
  text: string
  level: number
}

export default function CustomPageView() {
  const { id } = useParams<{ id: string }>()
  const { t, i18n } = useTranslation()
  const appStore = useAppStore()
  const authStore = useUserView()
  const adminSettingsStore = useAdminSettingsStore()

  const [loading, setLoading] = useState(false)
  const [pageTheme, setPageTheme] = useState<'light' | 'dark'>('light')
  const [renderedHtml, setRenderedHtml] = useState('')
  const markdownContainer = useRef<HTMLDivElement>(null)
  const [tocItems, setTocItems] = useState<TocItem[]>([])
  const [tocVisible, setTocVisible] = useState(
    () => typeof window === 'undefined' || window.innerWidth > 768
  )
  const [activeHeadingId, setActiveHeadingId] = useState('')
  const themeObserver = useRef<MutationObserver | null>(null)
  const markdownRequestVersion = useRef(0)

  const embedShell = useRef<HTMLDivElement>(null)
  const openButton = useRef<HTMLAnchorElement>(null)
  const [openButtonPosition, setButtonPosition] = useState<{ x: number; y: number } | null>(null)
  const buttonDrag = useRef<{ pointerId: number; startX: number; startY: number; x: number; y: number } | null>(null)
  const suppressButtonClick = useRef(false)

  function setOpenButtonPosition(x: number, y: number): void {
    const shell = embedShell.current
    const button = openButton.current
    if (!shell || !button) return
    setButtonPosition({
      x: Math.max(0, Math.min(x, shell.clientWidth - button.offsetWidth)),
      y: Math.max(0, Math.min(y, shell.clientHeight - button.offsetHeight))
    })
  }

  function startButtonDrag(event: React.PointerEvent<HTMLAnchorElement>) {
    if (event.button !== 0 || !event.isPrimary || !openButton.current) return
    const button = openButton.current
    suppressButtonClick.current = false
    buttonDrag.current = {
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      x: button.offsetLeft,
      y: button.offsetTop
    }
    // Capture keeps receiving moves when the pointer crosses the embedded iframe.
    button.setPointerCapture(event.pointerId)
  }

  function moveButtonDrag(event: React.PointerEvent<HTMLAnchorElement>) {
    const drag = buttonDrag.current
    if (!drag || event.pointerId !== drag.pointerId) return
    const dx = event.clientX - drag.startX
    const dy = event.clientY - drag.startY
    if (!suppressButtonClick.current && Math.hypot(dx, dy) < 4) return
    suppressButtonClick.current = true
    setOpenButtonPosition(drag.x + dx, drag.y + dy)
    event.preventDefault()
  }

  function endButtonDrag(event: React.PointerEvent<HTMLAnchorElement>) {
    const drag = buttonDrag.current
    if (!drag || event.pointerId !== drag.pointerId) return
    buttonDrag.current = null
    if (openButton.current?.hasPointerCapture(event.pointerId)) {
      openButton.current.releasePointerCapture(event.pointerId)
    }
  }

  function handleOpenButtonClick(event: React.MouseEvent<HTMLAnchorElement>) {
    if (suppressButtonClick.current && event.detail !== 0) event.preventDefault()
    suppressButtonClick.current = false
  }

  // Keep the dragged button inside the shell when either side resizes.
  useEffect(() => {
    const shell = embedShell.current
    const button = openButton.current
    if (!shell && !button) return
    const observer = new ResizeObserver(() => {
      if (openButtonPosition) setOpenButtonPosition(openButtonPosition.x, openButtonPosition.y)
    })
    if (shell) observer.observe(shell)
    if (button) observer.observe(button)
    return () => observer.disconnect()
  }, [openButtonPosition])

  const menuItem = (() => {
    const publicItems = appStore.cachedPublicSettings?.custom_menu_items ?? []
    const found = publicItems.find((item) => item.id === id) ?? null
    if (found) return found
    if (authStore.isAdmin) {
      return adminSettingsStore.customMenuItems.find((item) => item.id === id) ?? null
    }
    return null
  })()

  const markdownSlug = (() => {
    const item = menuItem
    if (!item) return ''
    if (item.page_slug) return item.page_slug
    if (item.url?.startsWith('md:')) return item.url.slice(3)
    return ''
  })()

  const isMarkdownMode = !!markdownSlug

  const embeddedUrl = (() => {
    if (!menuItem || isMarkdownMode) return ''
    return buildEmbeddedUrl(menuItem.url, authStore.user?.id, authStore.token, pageTheme, i18n.language)
  })()

  const isValidUrl = (() => {
    if (isMarkdownMode) return false
    const url = embeddedUrl
    return url.startsWith('http://') || url.startsWith('https://')
  })()

  function generateHeadingId(text: string, index: number): string {
    const base = text
      .toLowerCase()
      .replace(/[^\w一-鿿]+/g, '-')
      .replace(/^-+|-+$/g, '')
    return base ? `${base}-${index}` : `heading-${index}`
  }

  function isRelativeMarkdownAsset(src: string): boolean {
    const trimmed = src.trim()
    if (!trimmed || /^[a-z][a-z0-9+.-]*:/i.test(trimmed) || trimmed.startsWith('//') || trimmed.startsWith('/')) {
      return false
    }
    const [pathPart] = trimmed.split(/([?#].*)/, 2)
    return pathPart
      .split('/')
      .filter((part) => part && part !== '.')
      .every((part) => part !== '..' && !part.includes('\\'))
  }

  function buildPageImageUrl(slug: string, src: string): string {
    const trimmed = src.trim()
    const [pathPart, suffix = ''] = trimmed.split(/([?#].*)/, 2)
    const encodedPath = pathPart
      .split('/')
      .filter((part) => part && part !== '.')
      .map((part) => encodeURIComponent(part))
      .join('/')
    return buildApiUrl(`/pages/${encodeURIComponent(slug)}/images/${encodedPath}${suffix}`)
  }

  async function fetchAndRenderMarkdown(slug: string) {
    const version = ++markdownRequestVersion.current
    setLoading(true)
    setTocItems([])
    setActiveHeadingId('')
    try {
      const resp = await fetch(buildApiUrl(`/pages/${encodeURIComponent(slug)}`), {
        headers: authStore.token ? { Authorization: `Bearer ${authStore.token}` } : {}
      })
      if (version !== markdownRequestVersion.current) return
      if (!resp.ok) {
        setRenderedHtml(`<p class="text-red-500">${t('common.pageNotFound')}</p>`)
        return
      }
      let raw = await resp.text()
      if (version !== markdownRequestVersion.current) return

      raw = raw.replace(
        /!\[([^\]]*)\]\(([^)]+)\)/g,
        (match, alt, src) => (isRelativeMarkdownAsset(src) ? `![${alt}](${buildPageImageUrl(slug, src)})` : match)
      )

      const html = marked.parse(raw) as string
      const sanitized = DOMPurify.sanitize(html, {
        ADD_TAGS: ['iframe'],
        ADD_ATTR: ['allowfullscreen', 'frameborder', 'src']
      })

      // Inject IDs into headings and build TOC
      const toc: TocItem[] = []
      let headingIndex = 0
      const withIds = sanitized.replace(/<(h[1-4])[^>]*>(.*?)<\/h[1-4]>/gi, (_, tag: string, content: string) => {
        const level = parseInt(tag[1])
        const text = content.replace(/<[^>]+>/g, '').trim()
        const id = generateHeadingId(text, headingIndex++)
        toc.push({ id, text, level })
        return `<${tag} id="${id}">${content}</${tag}>`
      })

      setRenderedHtml(withIds)
      setTocItems(toc)
    } catch {
      if (version === markdownRequestVersion.current) {
        setRenderedHtml('<p class="text-red-500">Failed to load page</p>')
      }
    } finally {
      if (version === markdownRequestVersion.current) {
        setLoading(false)
        injectCopyButtons()
      }
    }
  }

  function scrollToHeading(id: string) {
    const container = markdownContainer.current
    if (!container) return
    const el = container.querySelector(`#${CSS.escape(id)}`)
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'start' })
      setActiveHeadingId(id)
      if (window.innerWidth <= 640) setTocVisible(false)
    }
  }

  const scrollRafId = useRef(0)
  const tocItemsRef = useRef(tocItems)
  tocItemsRef.current = tocItems

  function onContentScroll() {
    if (scrollRafId.current) return
    scrollRafId.current = requestAnimationFrame(() => {
      scrollRafId.current = 0
      const container = markdownContainer.current
      if (!container || tocItemsRef.current.length === 0) return

      const containerRect = container.getBoundingClientRect()
      let current = ''

      for (const item of tocItemsRef.current) {
        const el = container.querySelector(`#${CSS.escape(item.id)}`) as HTMLElement | null
        if (el) {
          const elRect = el.getBoundingClientRect()
          if (elRect.top - containerRect.top <= 100) current = item.id
        }
      }
      setActiveHeadingId(current)
    })
  }

  function injectCopyButtons() {
    const container = markdownContainer.current
    if (!container) return

    container.querySelectorAll('pre').forEach((pre) => {
      if (pre.querySelector('.copy-btn')) return
      const btn = document.createElement('button')
      btn.className = 'copy-btn'
      btn.textContent = t('customPage.copyCode')
      btn.addEventListener('click', () => {
        void (async () => {
          const code = pre.querySelector('code')?.textContent ?? pre.textContent ?? ''
          try {
            await navigator.clipboard.writeText(code)
            btn.textContent = t('customPage.copiedCode')
            setTimeout(() => {
              btn.textContent = t('customPage.copyCode')
            }, 2000)
          } catch {
            btn.textContent = t('customPage.copyCodeFailed')
            setTimeout(() => {
              btn.textContent = t('customPage.copyCode')
            }, 2000)
          }
        })()
      })
      pre.style.position = 'relative'
      pre.appendChild(btn)
    })
  }

  // Replaces the Vue `watch(markdownSlug, …, { immediate: true })`.
  useEffect(() => {
    if (markdownSlug) {
      void fetchAndRenderMarkdown(markdownSlug)
    } else {
      markdownRequestVersion.current += 1
      setLoading(false)
      setRenderedHtml('')
      setTocItems([])
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [markdownSlug])

  useEffect(() => {
    setPageTheme(detectTheme())

    if (typeof document !== 'undefined') {
      themeObserver.current = new MutationObserver(() => {
        setPageTheme(detectTheme())
      })
      themeObserver.current.observe(document.documentElement, {
        attributes: true,
        attributeFilter: ['class']
      })
    }

    let disposed = false
    if (!useAppStore.getState().publicSettingsLoaded) {
      setLoading(true)
      void useAppStore
        .getState()
        .fetchPublicSettings()
        .finally(() => {
          if (!disposed) setLoading(false)
        })
    }

    return () => {
      disposed = true
      markdownRequestVersion.current += 1
      if (themeObserver.current) {
        themeObserver.current.disconnect()
        themeObserver.current = null
      }
    }
  }, [])

  return (
    <div className="custom-page-layout">
      <div className="card workspace-shell min-h-0 flex-1 overflow-hidden">
        {loading ? (
          <div className="flex h-full items-center justify-center py-12">
            <div className="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent" />
          </div>
        ) : !menuItem ? (
          <div className="flex h-full items-center justify-center p-10 text-center">
            <div className="max-w-md">
              <div className="surface-inset mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full">
                <Icon name="link" size="lg" className="text-gray-400" />
              </div>
              <h3 className="text-lg font-semibold text-gray-900 dark:text-white">{t('customPage.notFoundTitle')}</h3>
              <p className="mt-2 text-sm surface-muted">{t('customPage.notFoundDesc')}</p>
            </div>
          </div>
        ) : isMarkdownMode ? (
          <div className="relative flex h-full overflow-hidden">
            {tocVisible && (
              <aside className="toc-sidebar">
                <div className="toc-header">
                  <span className="toc-title">{t('customPage.tableOfContents')}</span>
                  <button type="button" className="toc-close-btn surface-control" onClick={() => setTocVisible(false)}>
                    <svg
                      xmlns="http://www.w3.org/2000/svg"
                      width="16"
                      height="16"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    >
                      <path d="M15 18l-6-6 6-6" />
                    </svg>
                  </button>
                </div>
                <nav className="toc-nav">
                  {tocItems.map((item) => (
                    <a
                      key={item.id}
                      href={`#${item.id}`}
                      className={`toc-item toc-level-${item.level} ${activeHeadingId === item.id ? 'toc-active' : ''}`}
                      onClick={(event) => {
                        event.preventDefault()
                        scrollToHeading(item.id)
                      }}
                    >
                      {item.text}
                    </a>
                  ))}
                </nav>
              </aside>
            )}

            {!tocVisible && tocItems.length > 0 && (
              <button
                type="button"
                className="toc-toggle-btn surface-control"
                onClick={() => setTocVisible(true)}
              >
                <svg
                  xmlns="http://www.w3.org/2000/svg"
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <path d="M3 12h18M3 6h18M3 18h18" />
                </svg>
                <span className="ml-1 text-xs">{t('customPage.tableOfContents')}</span>
              </button>
            )}

            <div
              ref={markdownContainer}
              className="markdown-page-content flex-1 h-full overflow-auto p-6 md:p-10"
              dangerouslySetInnerHTML={{ __html: renderedHtml }}
              onScroll={onContentScroll}
            />
          </div>
        ) : !isValidUrl ? (
          <div className="flex h-full items-center justify-center p-10 text-center">
            <div className="max-w-md">
              <div className="surface-inset mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full">
                <Icon name="link" size="lg" className="text-gray-400" />
              </div>
              <h3 className="text-lg font-semibold text-gray-900 dark:text-white">
                {t('customPage.notConfiguredTitle')}
              </h3>
              <p className="mt-2 text-sm surface-muted">{t('customPage.notConfiguredDesc')}</p>
            </div>
          </div>
        ) : (
          <div ref={embedShell} className="custom-embed-shell">
            {!menuItem.hide_open_button && (
              <a
                ref={openButton}
                href={embeddedUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="btn btn-secondary btn-sm custom-open-fab"
                style={
                  openButtonPosition
                    ? { left: `${openButtonPosition.x}px`, top: `${openButtonPosition.y}px`, right: 'auto' }
                    : undefined
                }
                onPointerDown={startButtonDrag}
                onPointerMove={moveButtonDrag}
                onPointerUp={endButtonDrag}
                onPointerCancel={endButtonDrag}
                onLostPointerCapture={endButtonDrag}
                onDragStart={(event) => event.preventDefault()}
                onClick={handleOpenButtonClick}
              >
                <Icon name="externalLink" size="sm" className="mr-1.5" strokeWidth={2} />
                {t('customPage.openInNewTab')}
              </a>
            )}
            <iframe src={embeddedUrl} className="custom-embed-frame" allowFullScreen />
          </div>
        )}
      </div>
    </div>
  )
}
