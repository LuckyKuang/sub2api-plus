import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { adminAPI } from '@/api'
import type { CNProviderQuotaProbeResult } from '@/api/admin/cnProviders'
import type { Account } from '@/types'
import { cnQuotaCellVisible } from './credentialsBuilder'
import { UsageProgressBar } from './UsageProgressBar'

export interface CNProviderQuotaCellProps {
  account: Account
}

export function CNProviderQuotaCell({ account }: CNProviderQuotaCellProps) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [data, setData] = useState<CNProviderQuotaProbeResult | null>(null)

  const visible = cnQuotaCellVisible(account)

  // 后端周期任务/手动探测写入的 extra 快照键（<provider>_ 前缀，与后端
  // cnQuotaExtraUpdates 对齐）。页面加载即有数据，无需等待探测。
  const SNAPSHOT_STALE_MS = 15 * 60 * 1000

  // 自动探测去抖窗口与最近一次自动探测时间（模块级，跨实例共享）。
  const AUTO_PROBE_DEBOUNCE_MS = 5 * 60 * 1000
  const lastAutoProbeAt = new Map<number, number>()

  const readExtraNumber = (key: string, target: Account): number | null => {
    const v = (target.extra as Record<string, unknown> | undefined)?.[key]
    return typeof v === 'number' && Number.isFinite(v) ? v : null
  }

  const readExtraString = (key: string, target: Account): string => {
    const v = (target.extra as Record<string, unknown> | undefined)?.[key]
    return typeof v === 'string' ? v : ''
  }

  // 从持久化快照构造展示数据（缺少 5h/weekly 两档键时返回 null）。
  const snapshotDataFor = (target: Account): CNProviderQuotaProbeResult | null => {
    const platform = target.platform
    const tiers: NonNullable<CNProviderQuotaProbeResult['tiers']> = []
    for (const window of ['5h', 'weekly', 'monthly', 'monthly_code'] as const) {
      const unlimited = target.extra?.[`${platform}_${window}_unlimited`] === true
      const used = readExtraNumber(`${platform}_${window}_used_percent`, target)
      if (unlimited) {
        tiers.push({ window, unlimited: true, used_percent: 0 })
      } else if (used != null) {
        tiers.push({ window, used_percent: used, reset_at: readExtraString(`${platform}_${window}_reset_at`, target) || undefined })
      }
    }
    if (!tiers.length) return null
    return { success: true, tiers } as CNProviderQuotaProbeResult
  }

  // 快照是否过期（无更新时间或超过 staleness 窗口）→ 挂载时需要自动探测。
  const snapshotIsStaleFor = (target: Account): boolean => {
    const updatedAt = readExtraString(`${target.platform}_usage_updated_at`, target)
    if (!updatedAt) return true
    const ts = new Date(updatedAt).getTime()
    return Number.isNaN(ts) || Date.now() - ts > SNAPSHOT_STALE_MS
  }

  const handleProbe = async () => {
    if (loading) return
    setLoading(true)
    setError(null)
    try {
      const result = await adminAPI.cnProviders.queryQuota(account.id)
      // 失败时保留已渲染的快照条形图（仅显示错误行），成功才覆盖。
      if (result.success) {
        setData(result)
      } else {
        setError(result.error || t('common.error'))
      }
    } catch (e) {
      setError(extractErrorMessage(e))
    } finally {
      setLoading(false)
    }
  }

  const extractErrorMessage = (e: unknown): string => {
    const err = e as {
      message?: string
      reason?: string
      response?: { data?: { message?: string; error?: string } }
    }
    return err?.message || err?.reason || err?.response?.data?.message || err?.response?.data?.error || t('common.error')
  }

  const truncatedError = error ? (error.length > 80 ? `${error.slice(0, 80)}...` : error) : ''

  const windowLabel = (window: string) => {
    if (window === 'weekly') return t('admin.accounts.cnProviders.windowWeekly')
    if (window === 'monthly_code') return t('admin.accounts.cnProviders.windowMonthlyCode')
    if (window === 'monthly' && account.platform === 'kimi') return t('admin.accounts.cnProviders.windowMonthlyTotal')
    if (window === 'monthly') return t('admin.accounts.cnProviders.windowMonthly')
    return t('admin.accounts.cnProviders.window5h')
  }

  // 挂载/换行时：先用持久化快照渲染；快照缺失或过期再自动探测一次（失败显示错误，
  // 避免静默失败导致单元格空白无提示）。
  useEffect(() => {
    setData(snapshotDataFor(account))
    setError(null)
    setLoading(false)
    if (!snapshotIsStaleFor(account)) return
    // 模块级去抖：列表页每行一个实例，翻页/筛选/刷新会重复挂载；同一账号
    // 短时间内已自动探测过则跳过，避免对上游形成探测风暴。
    const last = lastAutoProbeAt.get(account.id) ?? 0
    if (Date.now() - last < AUTO_PROBE_DEBOUNCE_MS) return
    lastAutoProbeAt.set(account.id, Date.now())
    void handleProbe()
    // Snapshot inputs are stable per account row identity.
  }, [account.id])

  if (!visible) return null

  return (
    <div data-test="cn-provider-quota" className="min-w-[220px] space-y-1">
      {/* Tier rows: 5h + weekly utilization bars (snapshot renders on mount).
         复用账号页 UsageProgressBar：同阈值配色、同倒计时格式。 */}
      {data?.success && data.tiers?.length ? (
        <div className="space-y-1">
          {data.tiers.map((tier) =>
            tier.unlimited ? (
              <div key={tier.window} data-test="cn-provider-quota-unlimited" className="flex gap-2 text-xs surface-muted">
                <span>{windowLabel(tier.window)}</span>
                <span>{t('admin.accounts.cnProviders.unlimited')}</span>
              </div>
            ) : (
              <UsageProgressBar
                key={tier.window}
                data-test="cn-provider-quota-tier"
                label={windowLabel(tier.window)}
                color={tier.window === 'weekly' ? 'emerald' : 'indigo'}
                utilization={tier.used_percent}
                resetsAt={tier.reset_at}
              />
            )
          )}
        </div>
      ) : null}

      {/* Explicit refresh action (aligned with the OpenAI "Query" / Grok "Probe"
          buttons): a verb label tells users this chip is clickable. The previous
          noun label ("5h/weekly") read as a passive caption and users could not
          discover the manual refresh. */}
      <div className="flex flex-wrap items-center gap-1.5">
        <button
          type="button"
          data-test="cn-provider-quota-probe"
          className="inline-flex items-center gap-0.5 whitespace-nowrap rounded px-1.5 py-0.5 text-[10px] font-medium leading-4 text-blue-600 transition-colors hover:bg-blue-50/25 disabled:cursor-not-allowed disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30 surface-control"
          disabled={loading}
          title={t('admin.accounts.cnProviders.probeTooltip')}
          onClick={() => void handleProbe()}
        >
          <svg className={cn('h-2.5 w-2.5', loading && 'animate-spin')} fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          {t('admin.accounts.cnProviders.probe')}
        </button>
      </div>

      {error && (
        <div className="truncate text-[10px] leading-4 text-red-600 dark:text-red-400" title={error}>
          {truncatedError}
        </div>
      )}
    </div>
  )
}

export default CNProviderQuotaCell
