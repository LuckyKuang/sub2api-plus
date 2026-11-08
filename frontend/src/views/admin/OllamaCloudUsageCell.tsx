import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { adminAPI } from '@/api'
import type { Account, OllamaCloudUsageState } from '@/types'
import { UsageProgressBar } from './UsageProgressBar'

export interface OllamaCloudUsageCellProps {
  account: Account
  onUpdated: (state: OllamaCloudUsageState) => void
}

export function OllamaCloudUsageCell({ account, onUpdated }: OllamaCloudUsageCellProps) {
  const { t } = useTranslation()
  const [state, setState] = useState(account.ollama_cloud_usage)
  const [refreshing, setRefreshing] = useState(false)
  const snapshot = state?.snapshot

  useEffect(() => {
    setState(account.ollama_cloud_usage)
  }, [account.ollama_cloud_usage])

  const refreshUsage = async () => {
    if (refreshing) return
    setRefreshing(true)
    try {
      const next = await adminAPI.accounts.refreshOllamaCloudUsage(account.id)
      setState(next)
      onUpdated(next)
    } catch (error) {
      console.error('Failed to refresh Ollama Cloud usage:', error)
    } finally {
      setRefreshing(false)
    }
  }

  if (!state?.eligible) return <span className="text-sm surface-muted">-</span>

  return (
    <div className="min-w-0 max-w-full space-y-1" data-testid="ollama-cloud-usage-cell">
      {snapshot?.data?.five_hour && (
        <UsageProgressBar
          label="5h"
          utilization={snapshot.data.five_hour.used_percent}
          resetsAt={snapshot.data.five_hour.reset_at}
          color="indigo"
          data-testid="ollama-cloud-five-hour"
        />
      )}
      {snapshot?.data?.seven_day && (
        <UsageProgressBar
          label="7d"
          utilization={snapshot.data.seven_day.used_percent}
          resetsAt={snapshot.data.seven_day.reset_at}
          color="emerald"
          data-testid="ollama-cloud-seven-day"
        />
      )}
      {state.configured && (
        <div className="flex items-center pt-0.5">
          <button
            type="button"
            className="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium text-blue-600 transition-colors hover:bg-blue-50/25 disabled:cursor-not-allowed disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30 surface-control"
            disabled={refreshing}
            data-testid="ollama-cloud-usage-query"
            onClick={() => void refreshUsage()}
          >
            <svg className={cn('h-2.5 w-2.5', refreshing && 'animate-spin')} fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            {t('admin.accounts.usageWindow.activeQuery')}
          </button>
        </div>
      )}
    </div>
  )
}

export default OllamaCloudUsageCell
