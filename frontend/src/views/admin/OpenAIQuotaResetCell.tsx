import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  refreshOpenAIQuota,
  resetOpenAIQuota,
  type OpenAIQuotaResetResult,
  type OpenAIQuotaUsage
} from '@/api/admin/accounts'
import type { Account } from '@/types'
import { ConfirmDialog } from './shared'

export interface OpenAIQuotaResetCellProps {
  account: Account
  className?: string
  /** Rendered before the action buttons so the parent's local query shares one row. */
  preActions?: React.ReactNode
  onAccountUpdated: (account: Account) => void
}

type AutoResetCreditState = NonNullable<NonNullable<Account['extra']>['codex_auto_reset_credit_state']>

export function OpenAIQuotaResetCell({ account, className, preActions, onAccountUpdated }: OpenAIQuotaResetCellProps) {
  const { t } = useTranslation()

  // Visible only for OpenAI OAuth accounts.
  const visible = account.platform === 'openai' && account.type === 'oauth'

  const [loading, setLoading] = useState(false)
  const [resetting, setResetting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [data, setData] = useState<OpenAIQuotaUsage | null>(null)
  const [cachedData, setCachedData] = useState<OpenAIQuotaUsage | null>(null)
  const [resetMessage, setResetMessage] = useState<string | null>(null)
  const [resetWarning, setResetWarning] = useState<string | null>(null)
  const [showResetConfirm, setShowResetConfirm] = useState(false)
  const [showResetCreditDetails, setShowResetCreditDetails] = useState(false)

  const validAutoResetStatuses = new Set(['checking', 'available', 'resetting', 'success', 'no_credit', 'failed'])
  const autoResetState: AutoResetCreditState | null = (() => {
    if (account.extra?.auto_reset_credit_enabled !== true) return null
    const state = account.extra?.codex_auto_reset_credit_state
    if (!state || typeof state !== 'object' || !validAutoResetStatuses.has(String(state.status))) return null
    return state
  })()
  const autoResetStateLabel = (() => {
    if (!autoResetState?.status) return ''
    const keyByStatus: Record<string, string> = {
      checking: 'checking',
      available: 'available',
      resetting: 'resetting',
      success: 'success',
      no_credit: 'noCredit',
      failed: 'failed'
    }
    return t(`admin.accounts.openaiQuotaReset.autoStatus.${keyByStatus[autoResetState.status]}`)
  })()
  const autoResetStateClass = (() => {
    switch (autoResetState?.status) {
      case 'available':
        return 'bg-blue-50/[0.15] text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
      case 'success':
        return 'bg-emerald-50/[0.15] text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
      case 'no_credit':
      case 'failed':
        return 'bg-red-50/[0.15] text-red-700 dark:bg-red-900/30 dark:text-red-300'
      case 'resetting':
        return 'bg-orange-50/[0.15] text-orange-700 dark:bg-orange-900/30 dark:text-orange-300'
      default:
        return 'surface-inset surface-muted dark:surface-inset'
    }
  })()

  // Rehydrate the card from the persisted snapshot. Credits that already expired
  // are dropped and the count is clamped to what remains: the snapshot has no
  // freshness signal, so an unfiltered read would offer to consume credits that no
  // longer exist. A snapshot claiming credits with no usable expiration left is
  // treated as absent, which keeps the reset button gated on a live query.
  const readCachedResetCredits = (target: Account): OpenAIQuotaUsage | null => {
    const cached = target.extra?.codex_reset_credit_snapshot
    if (!cached || typeof cached !== 'object' || Array.isArray(cached)) return null

    const { available_count: count, credits: rawCredits } = cached as {
      available_count?: unknown
      credits?: unknown
    }
    if (typeof count !== 'number' || !Number.isFinite(count)) return null

    const now = Date.now()
    const credits: { expires_at?: string }[] = []
    if (Array.isArray(rawCredits)) {
      for (const credit of rawCredits) {
        if (!credit || typeof credit !== 'object') continue
        const expiresAt = (credit as { expires_at?: unknown }).expires_at
        if (typeof expiresAt !== 'string' || expiresAt.trim() === '') continue
        const expiryTime = new Date(expiresAt).getTime()
        // Unparsable timestamps are kept: they are already rendered verbatim and
        // dropping them would silently understate the available count.
        if (!Number.isNaN(expiryTime) && expiryTime <= now) continue
        credits.push({ expires_at: expiresAt })
      }
    }
    const availableCount = Math.min(Math.max(count, 0), credits.length)
    // A snapshot that claimed credits but has none left is no longer informative;
    // report "unknown" so the operator re-queries instead of trusting it.
    if (count > 0 && availableCount <= 0) return null
    return {
      fetched_at: 0,
      rate_limit_reset_credits: {
        available_count: availableCount,
        credits
      }
    }
  }

  useEffect(() => {
    // Account row may be reused across paginated lists; reset local state.
    const cached = readCachedResetCredits(account)
    setCachedData(cached)
    setData(cached)
    setError(null)
    setResetMessage(null)
    setResetWarning(null)
    setLoading(false)
    setResetting(false)
    setShowResetConfirm(false)
    setShowResetCreditDetails(false)
  }, [account.id])

  // 影子账号的额度查询会 resolve 到母账号,但影子本身不支持重置(后端返回 409);
  // 重置必须在母账号上进行。前端据此禁用影子的重置入口(外审 F6)。
  const isShadow = account.parent_account_id != null

  const availableResetCount = data?.rate_limit_reset_credits?.available_count ?? 0
  // Prefer the live payload and fall back to the persisted snapshot only when the
  // live state is unknown, so the count and the expirations never come from two
  // different generations of the same data.
  const resetCreditExpirations = ((data ?? cachedData)?.rate_limit_reset_credits?.credits ?? [])
    .map((credit) => credit.expires_at?.trim() ?? '')
    .filter((expiresAt) => expiresAt.length > 0)
    .sort(compareResetCreditExpiry)
  const primaryResetCreditExpiry = resetCreditExpirations[0] ?? ''
  const hiddenResetCreditCount = Math.max(resetCreditExpirations.length - 1, 0)
  const canReset = availableResetCount > 0 && !isShadow

  useEffect(() => {
    if (hiddenResetCreditCount <= 0) setShowResetCreditDetails(false)
  }, [hiddenResetCreditCount])

  const resetCreditDetailsTitle = resetCreditExpirations
    .map((expiresAt) => formatResetCreditExpiry(expiresAt, 'full'))
    .join('\n')

  const resetCreditDetailsToggleLabel = showResetCreditDetails
    ? t('admin.accounts.openaiQuotaReset.collapseExpirations')
    : t('admin.accounts.openaiQuotaReset.expandExpirations', { count: hiddenResetCreditCount })

  const resetButtonTitle = (() => {
    if (isShadow) return t('admin.accounts.openaiQuotaReset.resetTooltipShadow')
    if (!data) return t('admin.accounts.openaiQuotaReset.resetTooltipNeedQuery')
    if (!canReset) return t('admin.accounts.openaiQuotaReset.resetTooltipNoCredits')
    return t('admin.accounts.openaiQuotaReset.resetTooltipReady')
  })()

  // "次数" button doubles as the upstream-query trigger and the count display.
  // Tooltip differs between "click to load" (no data yet) and "click to refresh".
  const countButtonTitle = !data
    ? t('admin.accounts.openaiQuotaReset.countTooltipLoad')
    : t('admin.accounts.openaiQuotaReset.countTooltipRefresh')

  const truncatedError = (() => {
    if (!error) return ''
    return error.length > 80 ? `${error.slice(0, 80)}…` : error
  })()

  const getResetCreditExpiryTime = (value: string): number => {
    const time = new Date(value).getTime()
    return Number.isNaN(time) ? Number.POSITIVE_INFINITY : time
  }

  function compareResetCreditExpiry(a: string, b: string): number {
    const diff = getResetCreditExpiryTime(a) - getResetCreditExpiryTime(b)
    if (diff !== 0) return diff
    return a.localeCompare(b)
  }

  function formatResetCreditExpiry(value: string, style: 'short' | 'full'): string {
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) return value

    const options: Intl.DateTimeFormatOptions = {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit'
    }
    if (style === 'full') {
      options.year = 'numeric'
    }

    return new Intl.DateTimeFormat(undefined, options).format(date)
  }

  const extractErrorMessage = (e: unknown): string => {
    // The project's axios response interceptor (api/client.ts) flattens server
    // errors into { status, code, message, reason, ... } and re-rejects them, so
    // the message lives at the top level rather than under .response.data. Fall
    // back to the raw axios shape for the cancellation/network branches that
    // bypass the flattening, and finally to the generic i18n string.
    const err = e as {
      message?: string
      reason?: string
      response?: { data?: { message?: string; error?: string } }
    }
    return (
      err?.message ||
      err?.reason ||
      err?.response?.data?.message ||
      err?.response?.data?.error ||
      t('common.error')
    )
  }

  const toggleResetCreditDetails = () => {
    if (hiddenResetCreditCount <= 0) return
    setShowResetCreditDetails((prev) => !prev)
  }

  const handleQuery = async () => {
    if (loading) return
    setLoading(true)
    setError(null)
    setResetMessage(null)
    setResetWarning(null)
    setShowResetCreditDetails(false)
    try {
      const result = await refreshOpenAIQuota(account.id)
      // The upstream read succeeded even when the snapshot write was rejected, so
      // the live count is always adopted. Only the persisted view is left alone,
      // which keeps the displayed expirations consistent with what is stored.
      setData(result)
      if (result.cache_persisted) {
        setCachedData(result)
      } else {
        setResetWarning(t('admin.accounts.openaiQuotaReset.refreshCachePersistFailed'))
      }
    } catch (e) {
      setError(extractErrorMessage(e))
    } finally {
      setLoading(false)
    }
  }

  const openResetConfirm = () => {
    if (resetting || loading) return
    if (!canReset) {
      setError(t('admin.accounts.openaiQuotaReset.noCreditsAvailable'))
      return
    }
    setShowResetConfirm(true)
  }

  const confirmReset = async () => {
    setShowResetConfirm(false)
    if (resetting) return
    if (!canReset) {
      setError(t('admin.accounts.openaiQuotaReset.noCreditsAvailable'))
      return
    }
    setResetting(true)
    setError(null)
    setResetMessage(null)
    setResetWarning(null)
    try {
      const result: OpenAIQuotaResetResult = await resetOpenAIQuota(account.id)
      setShowResetCreditDetails(false)
      if (result.cache_refreshed && result.quota) {
        setData(result.quota)
        setCachedData(result.quota)
      } else {
        // A credit was consumed but the post-reset count could not be read back.
        // Whatever we still hold is one generation stale, so report the count as
        // unknown instead of letting a second consumption start from stale data.
        setData(null)
      }
      if (result.account) onAccountUpdated(result.account)

      if (result.warning_code === 'reset_credit_cache_refresh_failed') {
        setResetWarning(t('admin.accounts.openaiQuotaReset.resetCacheRefreshFailed'))
      } else if (result.warning_code === 'account_state_recovery_failed') {
        setResetWarning(t('admin.accounts.openaiQuotaReset.resetAccountRecoveryFailed'))
      } else if (result.warning_code === 'account_state_refresh_failed') {
        setResetWarning(t('admin.accounts.openaiQuotaReset.resetAccountRefreshFailed'))
      } else {
        setResetMessage(t('admin.accounts.openaiQuotaReset.resetSuccess', { windows: result.windows_reset }))
      }
    } catch (e) {
      setError(extractErrorMessage(e))
    } finally {
      setResetting(false)
    }
  }

  if (!visible) return null

  return (
    <div className={cn('space-y-1', className)}>
      {/*
        Unified action row. Parents that already render their own "local query"
        affordance (e.g. AccountUsageCell's active-sampling refresh) pass it in
        via preActions so the user sees a single row of related
        buttons rather than two near-duplicate "查询" rows.

        The 5h / 7d window bars are deliberately NOT rendered here — the local
        active-sampling display (UsageProgressBar in AccountUsageCell) already
        owns that real estate. This cell is purely about the rate-limit reset
        credit: query its count, consume one if needed.
      */}
      <div className="flex flex-wrap items-center gap-1.5">
        {preActions}

        <button
          type="button"
          className="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium text-blue-600 transition-colors hover:bg-blue-50/25 disabled:cursor-not-allowed disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30 surface-control"
          disabled={loading || resetting}
          title={countButtonTitle}
          onClick={() => void handleQuery()}
        >
          <svg className={cn('h-2.5 w-2.5', loading && 'animate-spin')} fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          {t('admin.accounts.openaiQuotaReset.count')}
          {data && <> {availableResetCount}</>}
        </button>

        <button
          type="button"
          className="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium text-orange-600 transition-colors hover:bg-orange-50/25 disabled:cursor-not-allowed disabled:opacity-50 dark:text-orange-400 dark:hover:bg-orange-900/30 surface-control"
          disabled={resetting || loading || !canReset}
          title={resetButtonTitle}
          onClick={openResetConfirm}
        >
          <svg className={cn('h-2.5 w-2.5', resetting && 'animate-spin')} fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M20 12a8 8 0 11-2.343-5.657L20 8m0 0V4m0 4h-4" />
          </svg>
          {t('admin.accounts.openaiQuotaReset.reset')}
        </button>
      </div>

      {autoResetState && (
        <div className="flex flex-wrap items-center gap-1 text-[10px]" data-testid="auto-reset-credit-state">
          <span className={cn('inline-flex items-center rounded px-1.5 py-0.5 font-medium', autoResetStateClass)}>
            {autoResetStateLabel}
            {autoResetState.trigger_window && <span className="ml-1 tabular-nums">{autoResetState.trigger_window}</span>}
          </span>
          {autoResetState.checked_at && <span className="surface-muted">{formatResetCreditExpiry(autoResetState.checked_at, 'short')}</span>}
          {autoResetState.error_code && (
            <span className="max-w-full truncate text-red-600 dark:text-red-400" title={autoResetState.error_code}>
              {autoResetState.error_code}
            </span>
          )}
        </div>
      )}

      {primaryResetCreditExpiry && (
        <div className="space-y-1">
          <div className="flex flex-wrap items-center gap-1">
            <span
              className="inline-flex max-w-full items-center rounded surface-inset px-1.5 py-0.5 text-[10px] leading-4 tabular-nums surface-muted"
              title={t('admin.accounts.openaiQuotaReset.expiresAtFull', { time: formatResetCreditExpiry(primaryResetCreditExpiry, 'full') })}
            >
              {t('admin.accounts.openaiQuotaReset.expiresAt', { time: formatResetCreditExpiry(primaryResetCreditExpiry, 'short') })}
            </span>
            {hiddenResetCreditCount > 0 && (
              <button
                type="button"
                data-testid="reset-credit-expiry-toggle"
                className="inline-flex items-center rounded-full px-1.5 py-0.5 text-[10px] font-medium leading-4 transition-colors hover:surface-hover surface-control surface-muted"
                aria-expanded={showResetCreditDetails}
                aria-label={resetCreditDetailsToggleLabel}
                title={resetCreditDetailsTitle}
                onClick={toggleResetCreditDetails}
              >
                +{hiddenResetCreditCount}
              </button>
            )}
          </div>

          {showResetCreditDetails && resetCreditExpirations.length > 1 && (
            <div
              data-testid="reset-credit-expiry-details"
              className="inline-grid max-w-full gap-0.5 rounded border border-gray-200 surface-inset px-1.5 py-1 text-[10px] leading-4 shadow-sm dark:border-dark-700 surface-muted"
            >
              <span className="sr-only">{t('admin.accounts.openaiQuotaReset.expirationDetails')}</span>
              {resetCreditExpirations.map((expiresAt, index) => (
                <span
                  key={`${expiresAt}-${index}`}
                  className="flex min-w-0 items-center gap-1 tabular-nums"
                  title={t('admin.accounts.openaiQuotaReset.expiresAtFull', { time: formatResetCreditExpiry(expiresAt, 'full') })}
                >
                  <span className="h-1 w-1 shrink-0 rounded-full bg-gray-400 dark:bg-dark-500" />
                  <span className="truncate">{formatResetCreditExpiry(expiresAt, 'short')}</span>
                </span>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Error / success feedback */}
      {error && (
        <div className="text-[10px] text-red-600 dark:text-red-400" title={error}>
          {truncatedError}
        </div>
      )}
      {!error && resetWarning && <div className="text-[10px] text-amber-600 dark:text-amber-400">{resetWarning}</div>}
      {!error && !resetWarning && resetMessage && (
        <div className="text-[10px] text-emerald-600 dark:text-emerald-400">{resetMessage}</div>
      )}

      <ConfirmDialog
        open={showResetConfirm}
        title={t('admin.accounts.openaiQuotaReset.confirmTitle')}
        message={t('admin.accounts.openaiQuotaReset.confirmMessage', { count: availableResetCount })}
        confirmText={t('admin.accounts.openaiQuotaReset.reset')}
        cancelText={t('common.cancel')}
        danger
        onConfirm={() => void confirmReset()}
        onCancel={() => setShowResetConfirm(false)}
      />
    </div>
  )
}

export default OpenAIQuotaResetCell
