import { useTranslation } from 'react-i18next'
import type { PlatformUsage } from '@/api/admin/dashboard'
import type { PlatformQuotaItem } from '@/api/admin/users'
import { listPlatforms } from '@/utils/platformCatalog'

export function UserConcurrencyCell({ current, max }: { current: number; max: number }) {
  let statusClass: string
  if (current >= max && max > 0) {
    statusClass = 'bg-red-100/[0.15] text-red-700 dark:bg-red-900/30 dark:text-red-400'
  } else if (current > 0) {
    statusClass = 'bg-yellow-100/[0.15] text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
  } else {
    statusClass = 'surface-inset surface-muted dark:surface-inset'
  }

  return (
    <div className="flex items-center">
      <span
        className={`inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium ${statusClass}`}
      >
        <svg className="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
          <path strokeLinecap="round" strokeLinejoin="round" d="M3.75 6A2.25 2.25 0 016 3.75h2.25A2.25 2.25 0 0110.5 6v2.25a2.25 2.25 0 01-2.25 2.25H6a2.25 2.25 0 01-2.25-2.25V6zM3.75 15.75A2.25 2.25 0 016 13.5h2.25a2.25 2.25 0 012.25 2.25V18a2.25 2.25 0 01-2.25 2.25H6A2.25 2.25 0 013.75 18v-2.25zM13.5 6a2.25 2.25 0 012.25-2.25H18a2.25 2.25 0 0120.25 6v2.25A2.25 2.25 0 0118 10.5h-2.25a2.25 2.25 0 01-2.25-2.25V6zM13.5 15.75a2.25 2.25 0 012.25-2.25H18a2.25 2.25 0 0122.25 2.25V18A2.25 2.25 0 0118 20.25h-2.25A2.25 2.25 0 0113.5 18v-2.25z" />
        </svg>
        <span className="font-mono">{current}</span>
        <span className="surface-muted">/</span>
        <span className="font-mono">{max}</span>
      </span>
    </div>
  )
}

export function PlatformCostCell({ usage }: { usage?: PlatformUsage }) {
  const { t } = useTranslation()
  if (!usage) return <span className="text-sm surface-muted">—</span>
  return (
    <div className="text-sm">
      <div className="flex items-center gap-1.5">
        <span className="surface-muted">{t('admin.users.today')}:</span>
        <span className="font-medium text-gray-900 dark:text-white">${usage.today_actual_cost.toFixed(4)}</span>
      </div>
      <div className="mt-0.5 flex items-center gap-1.5">
        <span className="surface-muted">{t('admin.users.total')}:</span>
        <span className="font-medium text-gray-900 dark:text-white">${usage.total_actual_cost.toFixed(4)}</span>
      </div>
    </div>
  )
}

// 按平台清单顺序展示；未登记的平台排在最后。
function platformRank(platform: string): number {
  const index = listPlatforms().findIndex((spec) => spec.id === platform)
  return index === -1 ? Number.MAX_SAFE_INTEGER : index
}

// 去尾零、最多 2 位小数：100→"100"，90.5→"90.5"，0.42→"0.42"
function fmtUsd(n: number): string {
  if (n == null || Number.isNaN(n)) return '0'
  return String(Math.round(n * 100) / 100)
}

function fmtLimit(n: number | null): string {
  return n == null ? '—' : fmtUsd(n)
}

export function UserPlatformQuotaCell({ quotas }: { quotas?: PlatformQuotaItem[] }) {
  const { t } = useTranslation()

  if (quotas === undefined) return <span className="text-xs surface-muted">…</span>

  // 仅展示「至少一档限额非空」的平台（配额列，非用量列）
  const configured = quotas
    .filter((q) => q.daily_limit_usd != null || q.weekly_limit_usd != null || q.monthly_limit_usd != null)
    .slice()
    .sort((a, b) => platformRank(a.platform) - platformRank(b.platform))

  if (configured.length === 0) {
    return <span className="text-xs surface-muted">{t('admin.users.platformQuota.cellNotConfigured')}</span>
  }

  return (
    <div className="space-y-0.5 text-xs">
      {configured.map((row) => (
        <div key={row.platform} className="flex items-center gap-2 whitespace-nowrap">
          <span className="w-20 shrink-0 font-mono surface-muted">{row.platform}</span>
          <span className="surface-muted">
            {t('admin.users.platformQuota.windowDaily')}
            <span className="text-gray-900 dark:text-white">
              {fmtUsd(row.daily_usage_usd)}/{fmtLimit(row.daily_limit_usd)}
            </span>
          </span>
          <span className="surface-muted">
            {t('admin.users.platformQuota.windowWeekly')}
            <span className="text-gray-900 dark:text-white">
              {fmtUsd(row.weekly_usage_usd)}/{fmtLimit(row.weekly_limit_usd)}
            </span>
          </span>
          <span className="surface-muted">
            {t('admin.users.platformQuota.windowMonthly')}
            <span className="text-gray-900 dark:text-white">
              {fmtUsd(row.monthly_usage_usd)}/{fmtLimit(row.monthly_limit_usd)}
            </span>
          </span>
        </div>
      ))}
    </div>
  )
}

interface BreakdownRow {
  platform: string
  today_actual_cost: number
  total_actual_cost: number
  isOther?: boolean
}

// 与 UserDashboardStats 保持一致：把"总值 - 各平台之和"的差作为"其他"行展示，
// 避免 tooltip 内各平台费用加总与列首总值对不上。
const OTHER_THRESHOLD = 0.0001

const PLATFORM_LABELS: Record<string, string> = {
  anthropic: 'Claude',
  openai: 'OpenAI',
  gemini: 'Gemini',
  antigravity: 'Antigravity',
  grok: 'Grok',
  kimi: 'Kimi',
  zhipu: 'Zhipu GLM',
  deepseek: 'DeepSeek',
  minimax: 'MiniMax',
  stepfun: 'StepFun'
}

function platformLabel(platform: string): string {
  return PLATFORM_LABELS[platform] ?? platform
}

export function PlatformUsageBreakdown({
  today,
  total,
  byPlatform
}: {
  today: number
  total: number
  byPlatform?: PlatformUsage[]
}) {
  const { t } = useTranslation()

  const sortedBreakdown: BreakdownRow[] = [...(byPlatform ?? [])]
    .sort((a, b) => b.total_actual_cost - a.total_actual_cost)
    .map((p) => ({ ...p }))

  const sumTotal = sortedBreakdown.reduce((s, r) => s + r.total_actual_cost, 0)
  const sumToday = sortedBreakdown.reduce((s, r) => s + r.today_actual_cost, 0)
  const diffTotal = Math.max(0, total - sumTotal)
  const diffToday = Math.max(0, today - sumToday)
  if (diffTotal > OTHER_THRESHOLD || diffToday > OTHER_THRESHOLD) {
    sortedBreakdown.push({
      platform: '__other__',
      today_actual_cost: diffToday,
      total_actual_cost: diffTotal,
      isOther: true
    })
  }

  const hasBreakdown = sortedBreakdown.length > 0

  return (
    <div className="group/usage relative text-sm">
      <div className="flex items-center gap-1.5">
        <span className="surface-muted">{t('admin.users.today')}:</span>
        <span className="font-medium text-gray-900 dark:text-white">${today.toFixed(4)}</span>
        {hasBreakdown && <InfoCircleGlyph />}
      </div>
      <div className="mt-0.5 flex items-center gap-1.5">
        <span className="surface-muted">{t('admin.users.total')}:</span>
        <span className="font-medium text-gray-900 dark:text-white">${total.toFixed(4)}</span>
      </div>

      {hasBreakdown && (
        <div className="pointer-events-none absolute left-full top-0 z-50 ml-2 min-w-[220px] whitespace-nowrap rounded-md px-3 py-2 text-xs text-white opacity-0 shadow-xl transition-opacity duration-100 group-hover/usage:opacity-100 surface-panel">
          <div className="mb-1.5 flex items-center justify-between gap-3 border-b border-white/10 pb-1 text-[11px] opacity-80">
            <span>{t('admin.users.platformBreakdown')}</span>
            <span className="font-mono">
              {t('admin.users.today')} / {t('admin.users.total')}
            </span>
          </div>
          {sortedBreakdown.map((item) => (
            <div
              key={item.platform}
              className={`flex items-center justify-between gap-3 py-0.5 ${item.isOther ? 'opacity-70 italic' : ''}`}
            >
              <span className="capitalize">
                {item.isOther ? t('admin.users.platformOther') : platformLabel(item.platform)}
              </span>
              <span className="font-mono">
                ${item.today_actual_cost.toFixed(4)}
                <span className="opacity-50">/</span>${item.total_actual_cost.toFixed(4)}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function InfoCircleGlyph() {
  return (
    <svg className="h-3 w-3 surface-muted" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
      <path strokeLinecap="round" strokeLinejoin="round" d="M11.25 11.25l.041-.02a.75.75 0 011.063.852l-.708 2.836a.75.75 0 001.063.853l.041-.021M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9-3.75h.008v.008H12V8.25z" />
    </svg>
  )
}
