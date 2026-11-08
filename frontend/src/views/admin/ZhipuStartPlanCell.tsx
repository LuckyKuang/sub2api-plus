import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { adminAPI } from '@/api'
import type { ZhipuStartPlanEntitlement } from '@/api/admin/cnProviders'
import type { Account } from '@/types'

export interface ZhipuStartPlanCellProps {
  account: Account
}

export function ZhipuStartPlanCell({ account }: ZhipuStartPlanCellProps) {
  const { t } = useTranslation()
  const [data, setData] = useState<ZhipuStartPlanEntitlement | null>(null)
  const [loading, setLoading] = useState(false)
  const [failed, setFailed] = useState(false)
  const [now, setNow] = useState(() => Date.now())
  const generationRef = useRef(0)

  const visible =
    account.platform === 'zhipu' &&
    account.type === 'oauth' &&
    account.credentials?.plan_kind === 'start-plan' &&
    ['bigmodel', 'zai'].includes(String(account.credentials?.oauth_provider))

  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [])

  // The admin API supplies an opaque grant revision because credentials are
  // redacted. Same-ID relinks and route edits invalidate replies.
  const ownerKey = JSON.stringify([
    account.id,
    account.platform,
    account.type,
    account.oauth_credential_owner,
    account.credentials,
    account.proxy_id,
    account.proxy
  ])

  const state = (() => {
    if (!data) return 'unknown'
    const age = Math.max(0, Math.floor(now / 1000) - data.observed_at)
    const remoteNow = data.server_time + age
    if (data.expires_at && remoteNow >= data.expires_at) return 'unavailable'
    if (age > 15 * 60) return 'unknown'
    // Reaching an effective date cannot promote a cached pending result.
    return data.state
  })()

  const formatTime = (seconds: number) => new Date(seconds * 1000).toLocaleString()

  const probe = async () => {
    if (!visible || loading) return
    const requestGeneration = generationRef.current
    const requestOwner = ownerKey
    setLoading(true)
    setFailed(false)
    try {
      const result = await adminAPI.cnProviders.queryQuota(account.id)
      if (requestGeneration !== generationRef.current || requestOwner !== ownerKey) return
      if (result.success && result.persisted && result.start_plan) setData(result.start_plan)
      else setFailed(true)
    } catch {
      if (requestGeneration === generationRef.current && requestOwner === ownerKey) setFailed(true)
    } finally {
      if (requestGeneration === generationRef.current && requestOwner === ownerKey) setLoading(false)
    }
  }

  useEffect(() => {
    generationRef.current += 1
    setData(null)
    setFailed(false)
    setLoading(false)
    void probe()
    // Re-probe whenever the credential owner / row identity changes.
  }, [ownerKey])

  useEffect(
    () => () => {
      generationRef.current += 1
    },
    []
  )

  if (!visible) return null

  return (
    <div data-test="zhipu-start-plan" className="min-w-[180px] space-y-1 text-xs">
      <div className="flex flex-wrap items-center gap-1.5">
        <span>{t('admin.accounts.cnProviders.startPlan.title')}</span>
        <span data-test="start-plan-state" className="surface-muted">
          {t(`admin.accounts.cnProviders.startPlan.${state}`)}
        </span>
        <button
          type="button"
          data-test="start-plan-probe"
          className="rounded px-1.5 py-0.5 text-[10px] text-blue-600 disabled:opacity-50 dark:text-blue-400 surface-control"
          disabled={loading}
          onClick={() => void probe()}
        >
          {t('admin.accounts.cnProviders.probe')}
        </button>
      </div>
      {state === 'available' && (
        <div data-test="start-plan-models" className="surface-muted">
          {!data?.models_known
            ? t('admin.accounts.cnProviders.startPlan.modelsUnknown')
            : data.models.length
              ? data.models.join(', ')
              : t('admin.accounts.cnProviders.startPlan.modelsEmpty')}
        </div>
      )}
      {(data?.reason === 'auth_failed' || data?.reason === 'reauthorization_required') && (
        <div data-test="start-plan-reauthorize" className="text-amber-600 dark:text-amber-400">
          {t('admin.accounts.cnProviders.startPlan.reauthorize')}
        </div>
      )}
      {state === 'pending' && data?.effective_at && (
        <div data-test="start-plan-effective" className="surface-muted">
          {t('admin.accounts.cnProviders.startPlan.effectiveAt', { time: formatTime(data.effective_at) })}
        </div>
      )}
      {data?.expires_at && (
        <div data-test="start-plan-expiry" className="surface-muted">
          {t('admin.accounts.cnProviders.startPlan.expiresAt', { time: formatTime(data.expires_at) })}
        </div>
      )}
      {failed && (
        <div data-test="start-plan-error" className="text-[10px] text-red-600 dark:text-red-400">
          {t('admin.accounts.cnProviders.startPlan.queryFailed')}
        </div>
      )}
    </div>
  )
}

export default ZhipuStartPlanCell
