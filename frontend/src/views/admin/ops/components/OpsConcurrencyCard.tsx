import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { opsAPI, type OpsConcurrencyStatsResponse } from '@/api/admin/ops'
import { toast } from '@/components/ui'

export interface OpsConcurrencyCardProps {
  platformFilter?: string
  groupIdFilter?: number | null
  refreshToken?: number
}

function loadClass(percentage: number): string {
  if (percentage >= 90) return 'bg-rose-500'
  if (percentage >= 70) return 'bg-yellow-500'
  return 'bg-emerald-500'
}

/** Live concurrency snapshot for platforms, groups and accounts. */
export function OpsConcurrencyCard({ platformFilter = '', groupIdFilter = null, refreshToken = 0 }: OpsConcurrencyCardProps) {
  const { t } = useTranslation()
  const [stats, setStats] = useState<OpsConcurrencyStatsResponse | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    void (async () => {
      try {
        const res = await opsAPI.getConcurrencyStats(platformFilter, groupIdFilter)
        if (!cancelled) setStats(res)
      } catch (error) {
        if (!cancelled) toast.error((error as { message?: string })?.message || t('common.error'))
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [platformFilter, groupIdFilter, refreshToken, t])

  const platforms = Object.values(stats?.platform ?? {})
  const groups = Object.values(stats?.group ?? {})

  return (
    <div className="glass-surface flex h-full flex-col p-6 ring-1 ring-gray-900/5 dark:ring-dark-700">
      <div className="mb-4 flex items-center justify-between">
        <h3 className="text-sm font-bold text-gray-900 dark:text-white">{t('admin.ops.concurrency')}</h3>
        {loading && <span className="text-xs text-gray-400">{t('common.loading')}</span>}
      </div>

      {!stats?.enabled ? (
        <div className="flex flex-1 items-center justify-center text-sm surface-muted">
          {t('admin.ops.concurrencyDisabled')}
        </div>
      ) : (
        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto">
          {platforms.map((entry) => (
            <div key={entry.platform} className="space-y-1">
              <div className="flex items-center justify-between text-xs">
                <span className="font-semibold uppercase surface-muted">{entry.platform}</span>
                <span className="font-bold text-gray-900 dark:text-white">
                  {entry.current_in_use} / {entry.max_capacity}
                </span>
              </div>
              <div className="h-2 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
                <div
                  className={`h-full ${loadClass(entry.load_percentage)}`}
                  style={{ width: `${Math.min(100, Math.max(0, entry.load_percentage))}%` }}
                />
              </div>
              {entry.waiting_in_queue > 0 && (
                <p className="text-[10px] surface-muted">
                  {t('admin.ops.waiting')}: {entry.waiting_in_queue}
                </p>
              )}
            </div>
          ))}

          {groups.slice(0, 12).map((entry) => (
            <div key={`${entry.group_id}`} className="space-y-1">
              <div className="flex items-center justify-between text-xs">
                <span className="truncate surface-muted">{entry.group_name}</span>
                <span className="font-bold text-gray-900 dark:text-white">
                  {entry.current_in_use} / {entry.max_capacity}
                </span>
              </div>
              <div className="h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
                <div
                  className={`h-full ${loadClass(entry.load_percentage)}`}
                  style={{ width: `${Math.min(100, Math.max(0, entry.load_percentage))}%` }}
                />
              </div>
            </div>
          ))}

          {platforms.length === 0 && groups.length === 0 && (
            <div className="py-8 text-center text-sm surface-muted">{t('common.noData')}</div>
          )}
        </div>
      )}
    </div>
  )
}

export default OpsConcurrencyCard
