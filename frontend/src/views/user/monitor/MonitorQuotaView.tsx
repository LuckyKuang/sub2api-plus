import { useTranslation } from 'react-i18next'
import type { MonitorQuotaSnapshot, MonitorQuotaTier } from '@/api/admin/channelMonitor'
import { UsageProgressBar } from '../UsageProgressBar'

/**
 * 配额快照渲染（管理端监控列表/运行结果 + 用户端监控卡片共用）。
 * 用量条直接复用账号页的 UsageProgressBar（与 CNProviderQuotaCell、
 * AccountUsageCell 同一组件：同阈值配色、同倒计时格式）；tier 的
 * Window/Label 是后端约定的机器 token，已知 token 走 i18n，未知 token
 * 原样展示（前向兼容）。
 */
export interface MonitorQuotaViewProps {
  snapshot?: MonitorQuotaSnapshot | null
}

type TierColor = 'indigo' | 'emerald' | 'purple' | 'amber'

interface QuotaTierRow {
  key: string
  label: string
  title: string
  color: TierColor
  tier: MonitorQuotaTier
}

// 已知的 window/label 机器 token → i18n key（monitorCommon.quota.*）。
const windowI18nKeys: Record<string, string> = {
  '5h': 'monitorCommon.quota.windows.5h',
  '7d': 'monitorCommon.quota.windows.7d',
  '7d-sonnet': 'monitorCommon.quota.windows.7dSonnet',
  '7d-fable': 'monitorCommon.quota.windows.7dFable',
  weekly: 'monitorCommon.quota.windows.weekly',
  daily: 'monitorCommon.quota.windows.daily',
  '30d': 'monitorCommon.quota.windows.30d',
  total: 'monitorCommon.quota.windows.total'
}

const labelI18nKeys: Record<string, string> = {
  requests: 'monitorCommon.quota.labels.requests',
  tokens: 'monitorCommon.quota.labels.tokens',
  shared: 'monitorCommon.quota.labels.shared',
  pro: 'monitorCommon.quota.labels.pro',
  flash: 'monitorCommon.quota.labels.flash'
}

// tier 配色按数组顺序轮转（UsageProgressBar 支持的色板）。
const tierColors: TierColor[] = ['indigo', 'emerald', 'purple', 'amber']

export function MonitorQuotaView({ snapshot }: MonitorQuotaViewProps) {
  const { t, i18n } = useTranslation()

  function windowLabel(window: string): string {
    const key = windowI18nKeys[window]
    return key && i18n.exists(key) ? t(key) : window
  }

  function tierLabel(tier: MonitorQuotaTier): string {
    const window = windowLabel(tier.window)
    if (!tier.label) return window
    const labelKey = labelI18nKeys[tier.label]
    const label = labelKey && i18n.exists(labelKey) ? t(labelKey) : tier.label
    return `${label}/${window}`
  }

  if (!snapshot) return null

  const tierRows: QuotaTierRow[] = (snapshot.tiers || []).map((tier, idx) => ({
    key: `${tier.window}-${tier.label || ''}-${idx}`,
    label: tierLabel(tier),
    title: tierLabel(tier),
    color: tierColors[idx % tierColors.length],
    tier
  }))

  const balanceRows = (() => {
    if (snapshot.balances?.length) return snapshot.balances
    if (snapshot.balance != null) {
      return [{ currency: snapshot.currency || '?', balance: snapshot.balance }]
    }
    return []
  })()

  const error = snapshot.error || t('monitorCommon.quota.unavailable')
  const truncatedError = error.length > 48 ? `${error.slice(0, 48)}…` : error

  return (
    <div className="space-y-1" data-testid="monitor-quota-view">
      {snapshot.plan_level && (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="rounded surface-inset px-1.5 py-0.5 text-[10px] font-medium surface-muted">
            {snapshot.plan_level}
          </span>
        </div>
      )}

      {snapshot.success && tierRows.length > 0 && (
        <div className="space-y-1">
          {tierRows.map((row) =>
            row.tier.unlimited ? (
              <div key={row.key} data-testid="monitor-quota-unlimited" className="flex gap-2 text-xs surface-muted">
                <span>{row.label}</span>
                <span>{t('admin.accounts.cnProviders.unlimited')}</span>
              </div>
            ) : (
              <UsageProgressBar
                key={row.key}
                dataTestId="monitor-quota-tier"
                label={row.label}
                title={row.title}
                labelWidth="auto"
                color={row.color}
                utilization={row.tier.used_percent}
                resetsAt={row.tier.reset_at ?? null}
              />
            )
          )}
        </div>
      )}

      {snapshot.success && balanceRows.length > 0 && (
        <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[10px]">
          {balanceRows.map((b) => (
            <span
              key={b.currency}
              className={['font-medium', b.balance <= 0 ? 'text-red-600 dark:text-red-400' : 'surface-muted'].join(' ')}
            >
              {b.balance.toFixed(2)} {b.currency}
            </span>
          ))}
        </div>
      )}

      {!snapshot.success && (
        <div className="truncate text-[10px] text-red-600 dark:text-red-400" title={snapshot.error} data-testid="monitor-quota-error">
          {truncatedError}
        </div>
      )}
    </div>
  )
}

export default MonitorQuotaView
