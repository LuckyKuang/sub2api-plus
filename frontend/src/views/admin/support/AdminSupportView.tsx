import { lazy, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMatches, useParams } from 'react-router-dom'
import { LoadingState } from '@/components/ui'
import { useAdminSupportViewStore } from '@/stores/adminSupportView'
import { parseAdminSupportTargetId, type AdminSupportResource } from '@/utils/adminSupport'
import type { RouteMeta } from '@/router/routes'

const pages: Record<AdminSupportResource, React.LazyExoticComponent<React.ComponentType>> = {
  overview: lazy(() => import('@/views/user/DashboardView')),
  'api-keys': lazy(() => import('@/views/user/KeysView')),
  'async-images': lazy(() => import('@/views/user/AsyncImageView')),
  'batch-images': lazy(() => import('@/views/user/BatchImageGuideView')),
  usage: lazy(() => import('@/views/user/UsageView')),
  channels: lazy(() => import('@/views/user/AvailableChannelsView')),
  'channel-status': lazy(() => import('@/views/user/ChannelStatusView')),
  subscriptions: lazy(() => import('@/views/user/SubscriptionsView')),
  orders: lazy(() => import('@/views/user/UserOrdersView')),
  profile: lazy(() => import('@/views/user/ProfileView')),
  purchase: lazy(() => import('@/views/user/PaymentView')),
  redeem: lazy(() => import('@/views/user/RedeemView')),
  affiliate: lazy(() => import('@/views/user/AffiliateView')),
  custom: lazy(() => import('@/views/user/CustomPageView'))
}

/**
 * Admin assistance host: renders the requested end-user screen against the
 * selected target. All writes are blocked by `utils/adminSupportContext`.
 */
export default function AdminSupportView() {
  const { t } = useTranslation()
  const params = useParams()
  const matches = useMatches()
  const loadTarget = useAdminSupportViewStore((s) => s.loadTarget)
  const target = useAdminSupportViewStore((s) => s.target)

  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const userId = useMemo(() => parseAdminSupportTargetId(params.user_id), [params.user_id])
  const resource = ((matches.at(-1)?.handle as RouteMeta | undefined)?.adminSupportResource ?? 'overview') as AdminSupportResource
  const resolvedTarget = target && target.id === userId ? target : null

  useEffect(() => {
    let cancelled = false
    const id = userId
    if (!id || (target && target.id === id)) return

    setLoading(true)
    setError('')
    loadTarget(id)
      .catch((err: unknown) => {
        if (!cancelled) setError((err as Error)?.message || t('admin.support.loadFailed'))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [userId])

  if (resolvedTarget && !loading) {
    const Page = pages[resource] ?? pages.overview
    return <Page key={`${userId}:${resource}:${params.id ?? ''}`} />
  }

  return (
    <div className="card workspace-shell flex min-h-64 flex-col items-center justify-center gap-4 p-6">
      {loading ? (
        <LoadingState />
      ) : (
        <>
          <p className="text-red-600 dark:text-red-400">{error || t('admin.support.loadFailed')}</p>
          <button type="button" className="btn btn-secondary" onClick={() => {
            setLoading(true)
            setError('')
            const id = userId
            if (!id) {
              setLoading(false)
              return
            }
            loadTarget(id)
              .catch((err: unknown) => setError((err as Error)?.message || t('admin.support.loadFailed')))
              .finally(() => setLoading(false))
          }}>
            {t('admin.support.retry')}
          </button>
        </>
      )}
    </div>
  )
}
