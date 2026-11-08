import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { adminAPI } from '@/api'
import type { CNProviderBalanceEntry, CNProviderBalanceResult } from '@/api/admin/cnProviders'
import type { Account } from '@/types'
import { platformTextClass } from '@/utils/platformColors'
import { cnBalanceCellVisible } from './credentialsBuilder'

export interface CNProviderBalanceCellProps {
  account: Account
}

export function CNProviderBalanceCell({ account }: CNProviderBalanceCellProps) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [data, setData] = useState<CNProviderBalanceResult | null>(null)

  // Kimi / DeepSeek payg API Key 与 DeepSeek 原生 OAuth 钱包分别使用各自余额入口。
  const visible = cnBalanceCellVisible(account)
  const nativeDeepSeek = account.platform === 'deepseek' && account.type === 'oauth'

  const extraKey = (suffix: string) => `${account.platform}_${suffix}`

  // 落库快照（后端周期探测/响应式写入 account.Extra）。
  const snapshotBalance = (() => {
    const v = account.extra?.[extraKey('balance')]
    return typeof v === 'number' ? v : null
  })()
  const snapshotCurrency = (() => {
    const v = account.extra?.[extraKey('balance_currency')]
    return typeof v === 'string' ? v : ''
  })()
  // 多币种快照（后端写 <platform>_balances：[{currency, balance}]，deepseek CNY+USD）。
  const snapshotBalances = ((): CNProviderBalanceEntry[] => {
    const v = account.extra?.[extraKey('balances')]
    if (!Array.isArray(v)) return []
    return v.flatMap((item): CNProviderBalanceEntry[] => {
      if (!item || typeof item !== 'object') return []
      const { currency, balance } = item as Record<string, unknown>
      if (typeof currency !== 'string' || typeof balance !== 'number') return []
      return [{ currency, balance }]
    })
  })()
  const snapshotBonusBalances = ((): CNProviderBalanceEntry[] => {
    const value = account.extra?.[extraKey('bonus_balances')]
    if (!Array.isArray(value)) return []
    return value.flatMap((item): CNProviderBalanceEntry[] => {
      if (!item || typeof item !== 'object') return []
      const { currency, balance } = item as Record<string, unknown>
      if (typeof currency !== 'string' || typeof balance !== 'number' || !Number.isFinite(balance)) return []
      return [{ currency, balance }]
    })
  })()
  const balanceLow = account.extra?.[extraKey('balance_low')] === true

  // 优先用探测结果，其次落库快照。多币种返回全部明细，否则主币种单条。
  const currentEntries = ((): CNProviderBalanceEntry[] => {
    if (data && data.success) {
      if (data.balances && data.balances.length > 0) return data.balances
      return [{ currency: data.currency || '', balance: data.balance }]
    }
    if (snapshotBalances.length > 0) return snapshotBalances
    if (snapshotBalance != null) {
      return [{ currency: snapshotCurrency, balance: snapshotBalance }]
    }
    return []
  })()

  const currentBonusEntries = ((): CNProviderBalanceEntry[] => {
    if (!nativeDeepSeek) return []
    if (data?.success) return data.bonus_balances ?? []
    return snapshotBonusBalances
  })()

  const formatEntry = (entry: CNProviderBalanceEntry): string => {
    const fixed = entry.balance >= 100 ? entry.balance.toFixed(0) : entry.balance.toFixed(2)
    return `${entry.currency || '¥'} ${fixed}`
  }

  const balanceLabel = (() => {
    if (currentEntries.length === 0) {
      // 修复:此前误引 admin.accounts.grokBalance(实际嵌套在 usageWindow 下),
      // 未命中时渲染原始 key。CN 供应商使用自己的占位键。
      return t('admin.accounts.cnProviders.balance')
    }
    const label = currentEntries.map(formatEntry).join(' · ')
    return nativeDeepSeek ? `${t('admin.accounts.cnProviders.normalBalance')}: ${label}` : label
  })()

  const bonusBalanceLabel = currentBonusEntries.map(formatEntry).join(' · ')

  const extractErrorMessage = (e: unknown): string => {
    const err = e as {
      message?: string
      reason?: string
      response?: { data?: { message?: string; error?: string } }
    }
    return err?.message || err?.reason || err?.response?.data?.message || err?.response?.data?.error || t('common.error')
  }

  const truncatedError = error ? (error.length > 80 ? `${error.slice(0, 80)}...` : error) : ''

  const handleProbe = async () => {
    if (loading) return
    setLoading(true)
    setError(null)
    try {
      const result = await adminAPI.cnProviders.queryBalance(account.id)
      // 失败时保留快照展示（仅显示错误行），成功才覆盖。
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

  useEffect(() => {
    setData(null)
    setError(null)
    setLoading(false)
  }, [account.id])

  if (!visible) return null

  return (
    <div className="space-y-1">
      {/* Balance value row: static display (snapshot or probe result) */}
      <div className="flex flex-wrap items-center gap-1.5">
        <span
          data-test="cn-provider-balance-value"
          className={cn('text-[10px] font-medium leading-4', platformTextClass(account.platform))}
          title={t('admin.accounts.cnProviders.balanceProbeTooltip')}
        >
          {balanceLabel}
        </span>

        {/* Low balance badge (reactive 402/429 marker or probe-detected) */}
        {balanceLow && (
          <span className="inline-flex items-center rounded bg-red-100/[0.15] px-1 py-0.5 text-[10px] font-medium text-red-700 dark:bg-red-900/30 dark:text-red-300">
            {t('admin.accounts.cnProviders.balanceLow')}
          </span>
        )}
      </div>

      {currentBonusEntries.length > 0 && (
        <div className={cn('text-[10px] font-medium leading-4', platformTextClass(account.platform))}>
          <span data-test="cn-provider-bonus-balance-value">
            {t('admin.accounts.cnProviders.bonusBalance')}: {bonusBalanceLabel}
          </span>
        </div>
      )}

      {/* Explicit refresh action (aligned with the OpenAI "Query" / Grok "Probe"
           buttons): the old chip doubled as the data display and a hidden click
           target, which users could not discover. The verb label makes the
           affordance explicit. */}
      <div className="flex flex-wrap items-center gap-1.5">
        <button
          type="button"
          data-test="cn-provider-balance-probe"
          className="inline-flex items-center gap-0.5 whitespace-nowrap rounded px-1.5 py-0.5 text-[10px] font-medium leading-4 text-blue-600 transition-colors hover:bg-blue-50/25 disabled:cursor-not-allowed disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30 surface-control"
          disabled={loading}
          title={t('admin.accounts.cnProviders.balanceProbeTooltip')}
          onClick={() => void handleProbe()}
        >
          <svg className={cn('h-2.5 w-2.5', loading && 'animate-spin')} fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          {t('admin.accounts.cnProviders.probe')}
        </button>
      </div>

      {error && (
        <div className="truncate text-[10px] text-red-600 dark:text-red-400" title={error}>
          {truncatedError}
        </div>
      )}
    </div>
  )
}

export default CNProviderBalanceCell
