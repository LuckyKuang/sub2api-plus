import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  getClaudeResetCredits,
  redeemClaudeResetCredit,
  type ClaudeResetCredit,
  type ClaudeResetCredits,
  type ClaudeResetOutcome
} from '@/api/admin/claudeResetCredits'
import type { Account } from '@/types'
import { ConfirmDialog } from './shared'

export interface ClaudeResetCreditsCellProps {
  account: Account
  className?: string
  /** Rendered before the action buttons so the parent's local query shares one row. */
  preActions?: React.ReactNode
  onRedeemed: (outcome: ClaudeResetOutcome) => void
}

export function ClaudeResetCreditsCell({ account, className, preActions, onRedeemed }: ClaudeResetCreditsCellProps) {
  const { t } = useTranslation()
  const [status, setStatus] = useState<ClaudeResetCredits | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(false)
  const [redeeming, setRedeeming] = useState(false)
  const [showRedeemConfirm, setShowRedeemConfirm] = useState(false)
  const [redeemFeedback, setRedeemFeedback] = useState<{ kind: 'success' | 'warning' | 'error'; text: string } | null>(null)
  // One key per operator confirmation. It survives a failed request so retrying the
  // same confirmation replays server-side instead of claiming a second reset.
  const pendingKeyRef = useRef<string | null>(null)
  const generationRef = useRef(0)

  const visible = account.platform === 'anthropic' && account.type === 'oauth'

  useEffect(() => {
    generationRef.current += 1
    setStatus(null)
    setLoading(false)
    setError(false)
    setRedeeming(false)
    setShowRedeemConfirm(false)
    setRedeemFeedback(null)
    pendingKeyRef.current = null
  }, [account.id, account.platform, account.type])

  // 与 OpenAIQuotaResetCell 的到期时间格式保持一致
  const formatTime = (value: string, style: 'short' | 'full'): string => {
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) return value
    const options: Intl.DateTimeFormatOptions = { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }
    if (style === 'full') options.year = 'numeric'
    return new Intl.DateTimeFormat(undefined, options).format(date)
  }

  // 与 Codex 的「次数」一致：显示持有的剩余次数，而不是此刻可兑换的次数
  const totalResets = (status?.credits ?? []).reduce((sum, c) => sum + c.resets_left, 0)

  // 优先展示下一张会被使用的券（仅它可能 redeemable），否则取最早到期的一张
  const primaryCredit: ClaudeResetCredit | null = (() => {
    const credits = status?.credits ?? []
    return credits.find((c) => c.redeemable) ?? [...credits].sort((a, b) => expiryMs(a.expires_at) - expiryMs(b.expires_at))[0] ?? null
  })()

  // 缺失或无法解析的到期时间排在最后
  function expiryMs(value?: string): number {
    const ms = value ? new Date(value).getTime() : NaN
    return Number.isNaN(ms) ? Number.POSITIVE_INFINITY : ms
  }

  // 仅在冷却时间仍在未来时提示（后端已清理过期值，这里防御性再判断一次）
  const cooldownActive = (() => {
    const until = status?.cooldown_until
    return !!until && new Date(until).getTime() > Date.now()
  })()

  const creditTitle = (() => {
    const credit = primaryCredit
    if (!credit) return ''
    const lines = [credit.label]
    if (credit.expires_at) lines.push(t('admin.accounts.claudeResetCredits.expiresAtFull', { time: formatTime(credit.expires_at, 'full') }))
    if (credit.clears.length) lines.push(t('admin.accounts.claudeResetCredits.clears', { windows: windowLabels(credit.clears) }))
    if (credit.use_requires_limit) lines.push(t('admin.accounts.claudeResetCredits.requiresLimit'))
    return lines.join('\n')
  })()

  // 已知窗口显示为可读名称，未知窗口原样显示
  const windowKeys: Record<string, string> = {
    five_hour: 'fiveHour',
    seven_day: 'sevenDay',
    seven_day_overage_included: 'sevenDayOverage'
  }
  function windowLabels(windows?: string[]): string {
    return (windows ?? []).map((w) => (windowKeys[w] ? t(`admin.accounts.claudeResetCredits.windows.${windowKeys[w]}`) : w)).join(', ')
  }

  const countButtonTitle = !status
    ? t('admin.accounts.claudeResetCredits.countTooltipLoad')
    : [
        t('admin.accounts.claudeResetCredits.countTooltipRefresh'),
        t('admin.accounts.claudeResetCredits.fetched', { time: formatTime(status.fetched_at, 'full') })
      ].join('\n')

  const canRedeem = (status?.available_count ?? 0) > 0

  const redeemButtonTitle = (() => {
    if (!status) return t('admin.accounts.claudeResetCredits.resetTooltipNeedQuery')
    if (!canRedeem) return t('admin.accounts.claudeResetCredits.resetTooltipNone')
    return t('admin.accounts.claudeResetCredits.resetTooltipReady')
  })()

  const confirmMessage = (() => {
    const next = status?.credits.find((c) => c.redeemable)
    return t('admin.accounts.claudeResetCredits.confirmMessage', {
      windows: windowLabels(next?.clears) || '—',
      count: Math.max(totalResets - 1, 0)
    })
  })()

  const feedbackClass = (() => {
    switch (redeemFeedback?.kind) {
      case 'success':
        return 'text-emerald-600 dark:text-emerald-400'
      case 'warning':
        return 'text-amber-600 dark:text-amber-400'
      default:
        return 'text-red-600 dark:text-red-400'
    }
  })()

  function newOperationKey(accountID: number): string {
    const id = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
    return `claude-reset-${accountID}-${id}`
  }

  function openRedeemConfirm() {
    if (redeeming || loading || !canRedeem) return
    setShowRedeemConfirm(true)
  }

  function outcomeFeedback(result: ClaudeResetOutcome): { kind: 'success' | 'warning' | 'error'; text: string } {
    const key = 'admin.accounts.claudeResetCredits.outcome'
    switch (result.outcome) {
      case 'reset':
        return { kind: 'success', text: t(`${key}.reset`, { windows: windowLabels(result.cleared) || '—' }) }
      case 'already_used':
        return { kind: 'warning', text: t(`${key}.alreadyUsed`) }
      case 'cooldown':
        return {
          kind: 'warning',
          text: result.cooldown_until
            ? t(`${key}.cooldownUntil`, { time: formatTime(result.cooldown_until, 'short') })
            : t(`${key}.cooldown`)
        }
      case 'not_limited':
        return { kind: 'warning', text: t(`${key}.notLimited`) }
      case 'ineligible':
        return { kind: 'error', text: t(`${key}.ineligible`) }
      default:
        if (result.reason === 'upstream_unavailable') return { kind: 'warning', text: t(`${key}.unavailable`) }
        return { kind: 'warning', text: t(`${key}.unknown`) }
    }
  }

  // Refusals issued before any claim was sent: the next attempt is a new confirmation.
  const preClaimRefusals = new Set(['CLAUDE_RESET_BUSY', 'CLAUDE_RESET_NOT_AVAILABLE', 'CLAUDE_RESET_UNRESOLVED', 'CLAUDE_RESET_UPSTREAM_UNAVAILABLE'])

  function errorText(e: unknown): string {
    const err = e as { reason?: string; message?: string }
    switch (err?.reason) {
      case 'CLAUDE_RESET_UNRESOLVED': return t('admin.accounts.claudeResetCredits.outcome.unknown')
      case 'CLAUDE_RESET_BUSY': return t('admin.accounts.claudeResetCredits.outcome.busy')
      case 'CLAUDE_RESET_NOT_AVAILABLE': return t('admin.accounts.claudeResetCredits.outcome.notAvailable')
      case 'CLAUDE_RESET_UPSTREAM_UNAVAILABLE': return t('admin.accounts.claudeResetCredits.outcome.unavailable')
      case 'IDEMPOTENCY_IN_PROGRESS': return t('admin.accounts.claudeResetCredits.outcome.inProgress')
      case 'IDEMPOTENCY_RETRY_BACKOFF': return t('admin.accounts.claudeResetCredits.outcome.retryBackoff')
      default: return err?.message || t('admin.accounts.claudeResetCredits.outcome.failed')
    }
  }

  const refresh = useCallback(async () => {
    if (loading || redeeming) return
    const current = ++generationRef.current
    setLoading(true)
    setError(false)
    try {
      const result = await getClaudeResetCredits(account.id)
      if (current === generationRef.current) setStatus(result)
    } catch {
      if (current === generationRef.current) {
        setError(true)
        setStatus(null)
      }
    } finally {
      if (current === generationRef.current) setLoading(false)
    }
  }, [account.id, loading, redeeming])

  async function confirmRedeem() {
    setShowRedeemConfirm(false)
    if (redeeming || loading || !canRedeem) return
    const accountID = account.id
    const current = generationRef.current
    pendingKeyRef.current ??= newOperationKey(accountID)
    setRedeeming(true)
    setRedeemFeedback(null)
    try {
      const result = await redeemClaudeResetCredit(accountID, pendingKeyRef.current)
      if (current !== generationRef.current) return
      // A definite server answer ends this confirmation; the next one gets a new key.
      pendingKeyRef.current = null
      setRedeemFeedback(outcomeFeedback(result))
      // The previous availability snapshot predates this claim. An uncertain
      // result requires a deliberate status query before offering another reset.
      setStatus(null)
      setRedeeming(false)
      onRedeemed(result)
      if (result.outcome !== 'unknown') await refresh()
    } catch (e) {
      if (current !== generationRef.current) return
      // Transport or unknown errors keep the key so a retry replays server-side.
      if (preClaimRefusals.has((e as { reason?: string })?.reason ?? '')) {
        pendingKeyRef.current = null
        setStatus(null)
      }
      setRedeemFeedback({ kind: 'error', text: errorText(e) })
    } finally {
      if (current === generationRef.current) setRedeeming(false)
    }
  }

  if (!visible) return null

  return (
    <div className={cn('space-y-1', className)}>
      {/*
        Same action row layout as OpenAIQuotaResetCell: the parent's local
        "查询" button is passed in via preActions so related buttons share one
        row. The reset count only shows for Anthropic OAuth accounts; the slot always
        renders. The count button is read-only; only the orange reset button,
        after a query shows a redeemable credit and the operator confirms,
        consumes one reset.
      */}
      <div className="flex flex-wrap items-center gap-1.5">
        {preActions}

        <button
          type="button"
          data-testid="claude-reset-count"
          className="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium text-blue-600 transition-colors hover:bg-blue-50/25 disabled:cursor-not-allowed disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30 surface-control"
          disabled={loading || redeeming}
          title={countButtonTitle}
          onClick={() => void refresh()}
        >
          <svg className={cn('h-2.5 w-2.5', loading && 'animate-spin')} fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          {t('admin.accounts.claudeResetCredits.count')}
          {status && <span className="ml-0.5 tabular-nums">{totalResets}</span>}
        </button>

        <button
          type="button"
          data-testid="claude-reset-redeem"
          className="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium text-orange-600 transition-colors hover:bg-orange-50/25 disabled:cursor-not-allowed disabled:opacity-50 dark:text-orange-400 dark:hover:bg-orange-900/30 surface-control"
          disabled={redeeming || loading || !canRedeem}
          title={redeemButtonTitle}
          onClick={openRedeemConfirm}
        >
          <svg className={cn('h-2.5 w-2.5', redeeming && 'animate-spin')} fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M20 12a8 8 0 11-2.343-5.657L20 8m0 0V4m0 4h-4" />
          </svg>
          {t('admin.accounts.claudeResetCredits.reset')}
        </button>
      </div>

      {status && (primaryCredit || !status.eligible || cooldownActive) && (
        <div className="flex flex-wrap items-center gap-1">
          {primaryCredit?.expires_at && (
            <span
              data-testid="claude-reset-expiry"
              className="inline-flex max-w-full items-center rounded surface-inset px-1.5 py-0.5 text-[10px] leading-4 tabular-nums surface-muted"
              title={creditTitle}
            >
              {t('admin.accounts.claudeResetCredits.expiresAt', { time: formatTime(primaryCredit.expires_at, 'short') })}
            </span>
          )}
          {status.eligible && totalResets > 0 && status.available_count === 0 && (
            <span data-testid="claude-reset-not-usable" className="text-[10px] surface-muted">
              {t('admin.accounts.claudeResetCredits.notUsableNow')}
            </span>
          )}
          {!status.eligible && (
            <span className="text-[10px] text-amber-600 dark:text-amber-400">
              {t('admin.accounts.claudeResetCredits.ineligible')}
            </span>
          )}
          {cooldownActive && (
            <span data-testid="claude-reset-cooldown" className="text-[10px] text-amber-600 dark:text-amber-400">
              {t('admin.accounts.claudeResetCredits.cooldown', { time: formatTime(status.cooldown_until!, 'short') })}
            </span>
          )}
        </div>
      )}

      {error && (
        <div role="alert" className="text-[10px] text-red-600 dark:text-red-400">
          {t('admin.accounts.claudeResetCredits.error')}
        </div>
      )}
      {redeemFeedback && (
        <div
          data-testid="claude-reset-feedback"
          role={redeemFeedback.kind === 'success' ? 'status' : 'alert'}
          className={cn('text-[10px]', feedbackClass)}
          title={redeemFeedback.text}
        >
          {redeemFeedback.text}
        </div>
      )}

      <ConfirmDialog
        open={showRedeemConfirm}
        title={t('admin.accounts.claudeResetCredits.confirmTitle')}
        message={confirmMessage}
        confirmText={t('admin.accounts.claudeResetCredits.reset')}
        cancelText={t('common.cancel')}
        danger
        onConfirm={() => void confirmRedeem()}
        onCancel={() => setShowRedeemConfirm(false)}
      />
    </div>
  )
}

export default ClaudeResetCreditsCell
