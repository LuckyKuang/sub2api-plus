import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { adminAPI } from '@/api'
import type { GrokQuotaProbeResult } from '@/api/admin/grok'
import type { Account } from '@/types'

export interface GrokQuotaProbeCellProps {
  account: Account
  /** When true, only show the probe button (+ errors). No duplicate weekly summary. */
  compact?: boolean
  onProbed: (result: GrokQuotaProbeResult) => void
}

export function GrokQuotaProbeCell({ account, compact = false, onProbed }: GrokQuotaProbeCellProps) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [data, setData] = useState<GrokQuotaProbeResult | null>(null)

  const visible = account.platform === 'grok' && account.type === 'oauth'

  useEffect(() => {
    setData(null)
    setError(null)
    setLoading(false)
  }, [account.id])

  const extractErrorMessage = (e: unknown): string => {
    const err = e as {
      message?: string
      reason?: string
      response?: { data?: { message?: string; error?: string } }
    }
    return err?.message || err?.reason || err?.response?.data?.message || err?.response?.data?.error || t('common.error')
  }

  const summary = (() => {
    if (compact || !data) return ''
    // Non-compact fallback (rarely used): brief weekly percent if present.
    const billing = data.billing
    if (billing?.period_type?.toLowerCase() === 'weekly' && billing.usage_percent != null) {
      return t('admin.accounts.usageWindow.grokWeeklyUsage', {
        percent: Math.round(Math.min(100, Math.max(0, billing.usage_percent)))
      })
    }
    return ''
  })()

  const truncatedError = (() => {
    if (!error) return ''
    return error.length > 80 ? `${error.slice(0, 80)}...` : error
  })()

  const handleProbe = async () => {
    if (loading) return
    setLoading(true)
    setError(null)
    try {
      const result = await adminAPI.grok.queryQuota(account.id)
      setData(result)
      setError(result.probe_error || null)
      onProbed(result)
    } catch (e) {
      setError(extractErrorMessage(e))
    } finally {
      setLoading(false)
    }
  }

  if (!visible) return null

  return (
    <div className="space-y-1">
      <div className="flex flex-wrap items-center gap-1.5">
        <button
          type="button"
          className="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium text-cyan-700 transition-colors hover:bg-cyan-50/25 disabled:cursor-not-allowed disabled:opacity-50 dark:text-cyan-300 dark:hover:bg-cyan-900/30 surface-control"
          disabled={loading}
          title={t('admin.accounts.usageWindow.grokProbeTooltip')}
          onClick={() => void handleProbe()}
        >
          <svg className={cn('h-2.5 w-2.5', loading && 'animate-spin')} fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          {t('admin.accounts.usageWindow.grokProbe')}
        </button>
      </div>

      {/* Compact mode: parent already shows 7d/30d/prepaid or 24h — only surface errors. */}
      {!compact && summary && <div className="text-[10px] surface-muted">{summary}</div>}
      {error && (
        <div className="truncate text-[10px] text-red-600 dark:text-red-400" title={error}>
          {truncatedError}
        </div>
      )}
    </div>
  )
}

export default GrokQuotaProbeCell
