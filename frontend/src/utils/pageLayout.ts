import { selfPaths, supportResourceFromPath } from './adminSupport'

export type PageLayout = 'overview' | 'data' | 'form'

/** Resolve the user resource in support mode without borrowing admin layout. */
export function pageLayout(path: string): PageLayout {
  const supportResource = supportResourceFromPath(path)
  const resource = supportResource ? selfPaths[supportResource] : path
  if (/^\/(?:admin\/)?(?:dashboard|orders\/dashboard)$/.test(resource)) return 'overview'
  if (/^\/(?:profile|redeem|purchase|payment)(?:\/|$)/.test(resource)) return 'form'
  if (/^\/admin\/(?:settings|plugins)(?:\/|$)/.test(resource)) return 'overview'
  if (/^\/admin(?:\/|$)/.test(resource)) return 'data'
  if (/^\/(?:usage|keys|batch-image|async-image|available-channels|monitor|orders)(?:\/|$)/.test(resource)) return 'data'
  return 'overview'
}

