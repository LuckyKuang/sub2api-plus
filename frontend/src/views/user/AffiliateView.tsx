import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ArrowRight, Copy, DollarSign, Link as LinkIcon, Lock, RefreshCw } from 'lucide-react'
import { cn } from '@/lib/utils'
import { userAPI } from '@/api/user'
import type { UserAffiliateDetail } from '@/types'
import { useAppStore } from '@/stores/app'
import { useUserView } from './useUserView'
import { formatCurrency, formatDateTime } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'
import './affiliate.css'

interface AffiliateTip {
  title: string
  body: string
  icon: 'link' | 'dollar' | 'arrowRight' | 'lock'
}

export default function AffiliateView() {
  const { t } = useTranslation()
  const appStore = useAppStore()
  const authStore = useUserView()
  const settings = useAppStore((s) => s.cachedPublicSettings)

  const [loading, setLoading] = useState(true)
  const [transferring, setTransferring] = useState(false)
  const [detail, setDetail] = useState<UserAffiliateDetail | null>(null)
  const inviteePageSize = 10
  const [inviteePage, setInviteePage] = useState(1)
  const inviteeViewport = useRef<HTMLDivElement | null>(null)

  const inviteePageCount = Math.max(1, Math.ceil((detail?.invitees.length || 0) / inviteePageSize))
  const pagedInvitees = (detail?.invitees || []).slice((inviteePage - 1) * inviteePageSize, inviteePage * inviteePageSize)
  const visibleInviteePages = useMemo(() => {
    const total = inviteePageCount
    const candidates = new Set([1, total, inviteePage - 1, inviteePage, inviteePage + 1])
    if (total <= 7) for (let page = 1; page <= total; page++) candidates.add(page)
    const pages = [...candidates].filter(page => page >= 1 && page <= total).sort((a, b) => a - b)
    const result: Array<number | 'ellipsis'> = []
    for (const page of pages) {
      const previous = result[result.length - 1]
      if (typeof previous === 'number' && page - previous > 1) result.push('ellipsis')
      result.push(page)
    }
    return result
  }, [inviteePageCount, inviteePage])
  const affiliateTips: AffiliateTip[] = [
    { title: 'affiliate.tips.shareTitle', body: 'affiliate.tips.line1', icon: 'link' },
    { title: 'affiliate.tips.earnTitle', body: 'affiliate.tips.line2', icon: 'dollar' },
    { title: 'affiliate.tips.transferTitle', body: 'affiliate.tips.line3', icon: 'arrowRight' },
    { title: 'affiliate.tips.frozenTitle', body: 'affiliate.tips.line4', icon: 'lock' },
  ]

  async function changeInviteePage(page: number): Promise<void> {
    setInviteePage(Math.max(1, Math.min(inviteePageCount, page)))
    if (inviteeViewport.current) inviteeViewport.current.scrollTop = 0
  }

  useEffect(() => {
    if (inviteePage > inviteePageCount) void changeInviteePage(inviteePageCount)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [inviteePageCount])

  const inviteLink = detail
    ? (typeof window === 'undefined'
      ? `/register?aff=${encodeURIComponent(detail.aff_code)}`
      : `${window.location.origin}/register?aff=${encodeURIComponent(detail.aff_code)}`)
    : ''

  // Rebate rate is a percentage in the range [0, 100]; backend already clamps it.
  // We trim trailing zeros (e.g. 20.00 → "20", 12.50 → "12.5") for a cleaner UI.
  const formattedRebateRate = (() => {
    const v = detail?.effective_rebate_rate_percent ?? 0
    const rounded = Math.round(v * 100) / 100
    return Number.isInteger(rounded) ? String(rounded) : rounded.toString()
  })()

  function formatCount(value: number): string {
    return value.toLocaleString()
  }

  async function loadAffiliateDetail(silent = false): Promise<void> {
    // The affiliate program is opt-in; an explicit `false` never issues the request.
    if (settings != null && settings.affiliate_enabled !== true) {
      if (!silent) setLoading(false)
      return
    }
    if (!silent) {
      setLoading(true)
    }
    try {
      setDetail(await userAPI.getAffiliateDetail())
    } catch (error) {
      appStore.showError(extractApiErrorMessage(error, t('affiliate.loadFailed')))
    } finally {
      if (!silent) {
        setLoading(false)
      }
    }
  }

  useEffect(() => {
    void loadAffiliateDetail()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function copyToClipboard(text: string, successMessage?: string): Promise<boolean> {
    if (!text) return false

    let success = false
    if (navigator.clipboard && window.isSecureContext) {
      try {
        await navigator.clipboard.writeText(text)
        success = true
      } catch {
        success = fallbackCopy(text)
      }
    } else {
      success = fallbackCopy(text)
    }

    if (success) {
      appStore.showSuccess(successMessage || t('common.copiedToClipboard'))
    } else {
      appStore.showError(t('common.copyFailed'))
    }

    return success
  }

  // 降级方案：使用 textarea + execCommand
  // 使用 textarea 而非 input，以正确处理多行文本
  function fallbackCopy(text: string): boolean {
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.setAttribute('readonly', 'true')
    textarea.style.cssText = 'position:fixed;left:0;top:0;width:1px;height:1px;opacity:0;pointer-events:none'
    document.body.appendChild(textarea)
    textarea.focus({ preventScroll: true })
    textarea.select()
    textarea.setSelectionRange(0, textarea.value.length)
    try {
      return document.execCommand('copy')
    } catch {
      return false
    } finally {
      document.body.removeChild(textarea)
    }
  }

  async function copyCode(): Promise<void> {
    if (!detail?.aff_code) return
    await copyToClipboard(detail.aff_code, t('affiliate.codeCopied'))
  }

  async function copyInviteLink(): Promise<void> {
    if (!inviteLink) return
    await copyToClipboard(inviteLink, t('affiliate.linkCopied'))
  }

  async function transferQuota(): Promise<void> {
    if (!detail || detail.aff_quota <= 0 || transferring) return
    setTransferring(true)
    try {
      const resp = await userAPI.transferAffiliateQuota()
      appStore.showSuccess(t('affiliate.transfer.success', { amount: formatCurrency(resp.transferred_quota) }))
      await Promise.all([
        loadAffiliateDetail(true),
        authStore.refreshUser().catch(() => undefined),
      ])
    } catch (error) {
      appStore.showError(extractApiErrorMessage(error, t('affiliate.transferFailed')))
    } finally {
      setTransferring(false)
    }
  }

  if (loading) {
    return (
      <div className="workspace-page affiliate-workspace">
        <div className="flex justify-center py-12">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"></div>
        </div>
      </div>
    )
  }

  if (!detail) {
    return <div className="workspace-page affiliate-workspace" />
  }

  return (
    <div className="workspace-page affiliate-workspace">
      <div className="affiliate-stats workspace-fixed grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div className="card p-5">
          <p className="flex items-center gap-1.5 text-sm surface-muted">
            <DollarSign size={16} strokeWidth={1.5} className="text-primary-500" />
            {t('affiliate.stats.rebateRate')}
          </p>
          <p className="mt-2 text-2xl font-semibold text-primary-600 dark:text-primary-400">
            {formattedRebateRate}<span className="ml-0.5 text-base font-medium">%</span>
          </p>
          <p className="mt-1 text-xs surface-muted">{t('affiliate.stats.rebateRateHint')}</p>
        </div>
        <div className="card p-5">
          <p className="text-sm surface-muted">{t('affiliate.stats.invitedUsers')}</p>
          <p className="mt-2 text-2xl font-semibold">{formatCount(detail.aff_count)}</p>
        </div>
        <div className="card p-5">
          <p className="text-sm surface-muted">{t('affiliate.stats.availableQuota')}</p>
          <p className="mt-2 text-2xl font-semibold text-emerald-600 dark:text-emerald-400">{formatCurrency(detail.aff_quota)}</p>
        </div>
        <div className="card p-5">
          <p className="text-sm surface-muted">{t('affiliate.stats.totalQuota')}</p>
          <p className="mt-2 text-2xl font-semibold">{formatCurrency(detail.aff_history_quota)}</p>
          {detail.aff_frozen_quota > 0 && (
            <p className="mt-1 text-xs text-amber-600 dark:text-amber-400">
              {t('affiliate.stats.frozenQuota')}: {formatCurrency(detail.aff_frozen_quota)}
            </p>
          )}
        </div>
      </div>

      <div className="affiliate-top-row workspace-fixed">
        <section className="card affiliate-invitation">
          <h3 className="text-base font-semibold">{t('affiliate.title')}</h3>
          <p className="mt-1 text-sm surface-muted">{t('affiliate.description')}</p>
          <div className="mt-4 grid gap-4 md:grid-cols-2">
            <div className="min-w-0 space-y-2">
              <p className="text-sm font-medium surface-muted">{t('affiliate.yourCode')}</p>
              <div className="flex flex-col items-stretch gap-2 rounded-xl border border-gray-200 surface-inset px-3 py-2 dark:border-dark-700 sm:flex-row sm:items-center">
                <code className="min-w-0 break-all text-xs font-semibold sm:flex-1 sm:truncate">{detail.aff_code}</code>
                <button className="btn btn-secondary btn-sm w-full sm:w-auto sm:shrink-0" onClick={() => { void copyCode() }}>
                  <Copy size={16} strokeWidth={1.5} /><span>{t('affiliate.copyCode')}</span>
                </button>
              </div>
            </div>
            <div className="min-w-0 space-y-2">
              <p className="text-sm font-medium surface-muted">{t('affiliate.inviteLink')}</p>
              <div className="flex flex-col items-stretch gap-2 rounded-xl border border-gray-200 surface-inset px-3 py-2 dark:border-dark-700 sm:flex-row sm:items-center">
                <code className="min-w-0 break-all text-xs sm:flex-1 sm:truncate surface-muted">{inviteLink}</code>
                <button className="btn btn-secondary btn-sm w-full sm:w-auto sm:shrink-0" onClick={() => { void copyInviteLink() }}>
                  <Copy size={16} strokeWidth={1.5} /><span>{t('affiliate.copyLink')}</span>
                </button>
              </div>
            </div>
          </div>
        </section>

        <section className="card affiliate-transfer">
          <div className="flex flex-wrap items-center justify-between gap-5">
            <div className="min-w-0 flex-1">
              <h3 className="text-base font-semibold">{t('affiliate.transfer.title')}</h3>
              <p className="mt-1 text-sm surface-muted">{t('affiliate.transfer.description')}</p>
              <div className="affiliate-transfer-value">
                <strong>{formatCurrency(detail.aff_quota)}</strong>
                <span>{t('affiliate.stats.availableQuota')}</span>
              </div>
            </div>
            <button className="btn btn-primary" disabled={transferring || detail.aff_quota <= 0} onClick={() => { void transferQuota() }}>
              {transferring
                ? <RefreshCw size={16} strokeWidth={1.5} className="animate-spin" />
                : <DollarSign size={16} strokeWidth={1.5} />}
              <span>{transferring ? t('affiliate.transfer.transferring') : t('affiliate.transfer.button')}</span>
            </button>
          </div>
          {detail.aff_quota <= 0 && (
            <p className="mt-3 text-sm text-amber-600 dark:text-amber-400">{t('affiliate.transfer.empty')}</p>
          )}
        </section>
      </div>

      <section className="workspace-fixed affiliate-guide" aria-label={t('affiliate.tips.title')}>
        <h3 className="mb-3 text-sm font-semibold">{t('affiliate.tips.title')}</h3>
        <ul className="affiliate-guide-grid">
          {affiliateTips.map((tip, index) => (
            <li key={tip.body} className="card affiliate-guide-card">
              <div className="affiliate-guide-top">
                <span className="affiliate-guide-icon"><AffiliateTipIcon icon={tip.icon} /></span>
                <span className="affiliate-guide-step">{String(index + 1).padStart(2, '0')}</span>
              </div>
              <h4 className="text-sm font-semibold">{t(tip.title)}</h4>
              <p className="text-[13px] leading-relaxed surface-muted">{t(tip.body, { rate: `${formattedRebateRate}%` })}</p>
            </li>
          ))}
        </ul>
      </section>

      <section className="card workspace-table-card affiliate-invitees">
        <header className="affiliate-invitees-heading workspace-fixed">
          <h3 className="text-[15px] font-semibold">{t('affiliate.invitees.title')}</h3>
          <span className="text-xs surface-muted">{t('common.total')}: {detail.invitees.length}</span>
        </header>
        {detail.invitees.length === 0 ? (
          <div className="p-6 text-center text-sm surface-muted">{t('affiliate.invitees.empty')}</div>
        ) : (
          <div ref={inviteeViewport} className="record-table-scroll workspace-table-viewport affiliate-invitees-viewport">
            <table className="record-table w-full min-w-[560px] text-left text-sm">
              <thead>
                <tr className="border-b border-gray-200 dark:border-dark-700 surface-muted">
                  <th className="px-3 py-2 font-medium">{t('affiliate.invitees.columns.email')}</th>
                  <th className="px-3 py-2 font-medium">{t('affiliate.invitees.columns.username')}</th>
                  <th className="px-3 py-2 font-medium text-right">{t('affiliate.invitees.columns.rebate')}</th>
                  <th className="px-3 py-2 font-medium">{t('affiliate.invitees.columns.joinedAt')}</th>
                </tr>
              </thead>
              <tbody>
                {pagedInvitees.map(item => (
                  <tr key={item.user_id} className="border-b border-gray-100 last:border-b-0 dark:border-dark-800">
                    <td className="px-3 py-3">{item.email || '-'}</td>
                    <td className="px-3 py-3 surface-muted">{item.username || '-'}</td>
                    <td className="px-3 py-3 text-right font-medium text-emerald-600 dark:text-emerald-400">{formatCurrency(item.total_rebate)}</td>
                    <td className="px-3 py-3 surface-muted">{formatDateTime(item.created_at) || '-'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <nav className="card workspace-fixed affiliate-pagination" aria-label={t('affiliate.invitees.title')}>
        <div className="affiliate-pagination-info">
          <span>{t('common.total')}: {detail.invitees.length} {t('pagination.results')}</span>
          <span>{t('pagination.perPage')}: {inviteePageSize}</span>
        </div>
        <div className="affiliate-pagination-actions">
          <button className="btn btn-secondary" disabled={inviteePage <= 1} onClick={() => { void changeInviteePage(inviteePage - 1) }}>{t('pagination.previous')}</button>
          {visibleInviteePages.map((page, index) => (
            page === 'ellipsis'
              ? <span key={`ellipsis-${index}`} className="px-1 text-sm surface-muted" aria-hidden="true">…</span>
              : (
                <button
                  key={`${page}-${index}`}
                  type="button"
                  className={cn('affiliate-page-number surface-control')}
                  aria-label={t('pagination.goToPage', { page })}
                  aria-current={page === inviteePage ? 'page' : undefined}
                  onClick={() => { void changeInviteePage(page) }}
                >{page}</button>
              )
          ))}
          <button className="btn btn-secondary" disabled={inviteePage >= inviteePageCount} onClick={() => { void changeInviteePage(inviteePage + 1) }}>{t('pagination.next')}</button>
        </div>
      </nav>
    </div>
  )
}

function AffiliateTipIcon({ icon }: { icon: AffiliateTip['icon'] }) {
  if (icon === 'link') return <LinkIcon size={20} strokeWidth={1.5} />
  if (icon === 'dollar') return <DollarSign size={20} strokeWidth={1.5} />
  if (icon === 'arrowRight') return <ArrowRight size={20} strokeWidth={1.5} />
  return <Lock size={20} strokeWidth={1.5} />
}
