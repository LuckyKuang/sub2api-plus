import i18next from '@/i18n'
import type { RouteMeta } from './routes'

/**
 * 统一生成页面标题，避免多处写入 document.title 产生覆盖冲突。
 * 优先使用 titleKey 通过 i18n 翻译，fallback 到静态 routeTitle。
 */
export function resolveDocumentTitle(routeTitle: unknown, siteName?: string, titleKey?: string): string {
  const normalizedSiteName = typeof siteName === 'string' && siteName.trim() ? siteName.trim() : 'Sub2API Plus'

  if (typeof titleKey === 'string' && titleKey.trim()) {
    const translated = i18next.t(titleKey)
    if (translated && translated !== titleKey) {
      return `${translated} - ${normalizedSiteName}`
    }
  }

  if (typeof routeTitle === 'string' && routeTitle.trim()) {
    return `${routeTitle.trim()} - ${normalizedSiteName}`
  }

  return normalizedSiteName
}

export interface RouteTitleOptions {
  billingMode?: 'recharge_only' | 'subscription_only'
}

export interface RouteMetaKeys {
  titleKey?: string
  descriptionKey?: string
}

export const PURCHASE_ROUTE_PATH = '/purchase'

export interface RouteLike {
  pathname: string
  handle?: unknown
  params?: Record<string, string | undefined>
}

function readMeta(route: RouteLike): RouteMeta {
  return (route.handle as RouteMeta | undefined) ?? {}
}

/**
 * 解析路由的 i18n 标题/描述 key。绝大多数路由直接取 meta；
 * /purchase 随站点计费模式切换文案，AppHeader 与 document.title 共用这一处判断。
 */
export function resolveRouteMetaKeys(route: RouteLike, options: RouteTitleOptions = {}): RouteMetaKeys {
  if (route.pathname === PURCHASE_ROUTE_PATH) {
    if (options.billingMode === 'recharge_only') {
      return { titleKey: 'nav.recharge', descriptionKey: 'purchase.rechargeDescription' }
    }
    if (options.billingMode === 'subscription_only') {
      return { titleKey: 'nav.subscribe', descriptionKey: 'purchase.subscriptionDescription' }
    }
  }
  const meta = readMeta(route)
  return {
    titleKey: typeof meta.titleKey === 'string' ? meta.titleKey : undefined,
    descriptionKey: typeof meta.descriptionKey === 'string' ? meta.descriptionKey : undefined
  }
}

export function resolveRouteDocumentTitle(
  route: RouteLike,
  siteName: string | undefined,
  customMenuItems: Array<{ id: string; label: string }> = [],
  options: RouteTitleOptions = {}
): string {
  const id = route.params?.id ?? ''
  const isCustomPage = route.pathname.startsWith('/custom/')
  const menuItem = isCustomPage && id ? customMenuItems.find((item) => item.id === id) : undefined
  const menuTitle = menuItem?.label.trim()
  const { titleKey } = resolveRouteMetaKeys(route, options)

  return resolveDocumentTitle(menuTitle || readMeta(route).title, siteName, menuTitle ? undefined : titleKey)
}

let titleSiteName = 'Sub2API Plus'
let titleCustomMenuItems: Array<{ id: string; label: string }> = []
let titleBillingMode: RouteTitleOptions['billingMode']

export function configureDocumentTitle(
  siteName: string | undefined,
  customMenuItems: Array<{ id: string; label: string }> = [],
  billingMode?: RouteTitleOptions['billingMode']
): void {
  titleSiteName = siteName ?? 'Sub2API Plus'
  titleCustomMenuItems = customMenuItems
  titleBillingMode = billingMode
}

/** Re-apply the document title after a locale switch. */
export function refreshDocumentTitle(): void {
  if (typeof document === 'undefined') return
  document.title = resolveRouteDocumentTitle(
    { pathname: window.location.pathname, params: readCustomPageId(window.location.pathname) },
    titleSiteName,
    titleCustomMenuItems,
    { billingMode: titleBillingMode }
  )
}

function readCustomPageId(pathname: string): Record<string, string | undefined> {
  const match = /^\/custom\/([^/]+)/.exec(pathname)
  return match ? { id: decodeURIComponent(match[1]) } : {}
}
