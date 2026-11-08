import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import type { Account, GeminiCredentials } from '@/types'

export interface AccountQuotaInfoProps {
  account: Account
}

export function AccountQuotaInfo({ account }: AccountQuotaInfoProps) {
  const { t } = useTranslation()
  const [now, setNow] = useState(() => new Date())

  const creds = account.credentials as GeminiCredentials | undefined

  // 是否为 Code Assist OAuth
  // 判断逻辑与后端保持一致：project_id 存在即为 Code Assist
  const isCodeAssist = creds?.oauth_type === 'code_assist' || (!creds?.oauth_type && !!creds?.project_id)

  // 是否为 Google One OAuth
  const isGoogleOne = creds?.oauth_type === 'google_one'

  // 是否应该显示配额信息
  const shouldShowQuota = account.platform === 'gemini'

  // Tier 标签文本
  const tierLabel = (() => {
    if (isCodeAssist) {
      const tier = (creds?.tier_id || '').toString().trim().toLowerCase()
      if (tier === 'gcp_enterprise') return 'GCP Enterprise'
      if (tier === 'gcp_standard') return 'GCP Standard'
      // Backward compatibility
      const upper = (creds?.tier_id || '').toString().trim().toUpperCase()
      if (upper.includes('ULTRA') || upper.includes('ENTERPRISE')) return 'GCP Enterprise'
      if (upper) return `GCP ${upper}`
      return 'GCP'
    }

    if (isGoogleOne) {
      const tier = (creds?.tier_id || '').toString().trim().toLowerCase()
      if (tier === 'google_ai_ultra') return 'Google AI Ultra'
      if (tier === 'google_ai_pro') return 'Google AI Pro'
      if (tier === 'google_one_free') return 'Google One Free'
      // Backward compatibility
      const upper = (creds?.tier_id || '').toString().trim().toUpperCase()
      if (upper === 'AI_PREMIUM') return 'Google AI Pro'
      if (upper === 'GOOGLE_ONE_UNLIMITED') return 'Google AI Ultra'
      if (upper) return `Google One ${upper}`
      return 'Google One'
    }

    // API Key: 显示 AI Studio
    const tier = (creds?.tier_id || '').toString().trim().toLowerCase()
    if (tier === 'aistudio_paid') return 'AI Studio Pay-as-you-go'
    if (tier === 'aistudio_free') return 'AI Studio Free Tier'
    return 'AI Studio'
  })()

  // Tier Badge 样式（统一样式）
  const tierBadgeClass = (() => {
    if (isCodeAssist) {
      const tier = (creds?.tier_id || '').toString().trim().toLowerCase()
      if (tier === 'gcp_enterprise') return 'bg-purple-100/[0.15] text-purple-600 dark:bg-purple-900/40 dark:text-purple-300'
      if (tier === 'gcp_standard') return 'bg-blue-100/[0.15] text-blue-600 dark:bg-blue-900/40 dark:text-blue-300'
      // Backward compatibility
      const upper = (creds?.tier_id || '').toString().trim().toUpperCase()
      if (upper.includes('ULTRA') || upper.includes('ENTERPRISE')) return 'bg-purple-100/[0.15] text-purple-600 dark:bg-purple-900/40 dark:text-purple-300'
      return 'bg-blue-100/[0.15] text-blue-600 dark:bg-blue-900/40 dark:text-blue-300'
    }

    if (isGoogleOne) {
      const tier = (creds?.tier_id || '').toString().trim().toLowerCase()
      if (tier === 'google_ai_ultra') return 'bg-purple-100/[0.15] text-purple-600 dark:bg-purple-900/40 dark:text-purple-300'
      if (tier === 'google_ai_pro') return 'bg-blue-100/[0.15] text-blue-600 dark:bg-blue-900/40 dark:text-blue-300'
      if (tier === 'google_one_free') return 'surface-inset surface-muted dark:surface-inset'
      // Backward compatibility
      const upper = (creds?.tier_id || '').toString().trim().toUpperCase()
      if (upper === 'GOOGLE_ONE_UNLIMITED') return 'bg-purple-100/[0.15] text-purple-600 dark:bg-purple-900/40 dark:text-purple-300'
      if (upper === 'AI_PREMIUM') return 'bg-blue-100/[0.15] text-blue-600 dark:bg-blue-900/40 dark:text-blue-300'
      return 'surface-inset surface-muted dark:surface-inset'
    }

    // AI Studio 默认样式：蓝色
    const tier = (creds?.tier_id || '').toString().trim().toLowerCase()
    if (tier === 'aistudio_paid') return 'bg-blue-100/[0.15] text-blue-600 dark:bg-blue-900/40 dark:text-blue-300'
    if (tier === 'aistudio_free') return 'surface-inset surface-muted dark:surface-inset'
    return 'bg-blue-100/[0.15] text-blue-600 dark:bg-blue-900/40 dark:text-blue-300'
  })()

  // 是否限流
  const isRateLimited = useMemo(() => {
    if (!account.rate_limit_reset_at) return false
    const resetTime = Date.parse(account.rate_limit_reset_at)
    // 防护：如果日期解析失败（NaN），则认为未限流
    if (Number.isNaN(resetTime)) return false
    return resetTime > now.getTime()
  }, [account.rate_limit_reset_at, now])

  // 倒计时文本
  const resetCountdown = useMemo(() => {
    if (!account.rate_limit_reset_at) return ''
    const resetTime = Date.parse(account.rate_limit_reset_at)
    // 防护：如果日期解析失败，显示 "-"
    if (Number.isNaN(resetTime)) return '-'

    const diffMs = resetTime - now.getTime()
    if (diffMs <= 0) return t('admin.accounts.gemini.rateLimit.now')

    const diffSeconds = Math.floor(diffMs / 1000)
    const diffMinutes = Math.floor(diffSeconds / 60)
    const diffHours = Math.floor(diffMinutes / 60)

    if (diffMinutes < 1) return `${diffSeconds}s`
    if (diffHours < 1) {
      const secs = diffSeconds % 60
      return `${diffMinutes}m ${secs}s`
    }
    const mins = diffMinutes % 60
    return `${diffHours}h ${mins}m`
  }, [account.rate_limit_reset_at, now, t])

  // 是否紧急（< 1分钟）
  const isUrgent = useMemo(() => {
    if (!account.rate_limit_reset_at) return false
    const resetTime = Date.parse(account.rate_limit_reset_at)
    // 防护：如果日期解析失败，返回 false
    if (Number.isNaN(resetTime)) return false

    const diffMs = resetTime - now.getTime()
    return diffMs > 0 && diffMs < 60000
  }, [account.rate_limit_reset_at, now])

  // 监听限流状态，动态启动/停止定时器
  useEffect(() => {
    if (!isRateLimited) return
    // 进入限流状态，启动定时器
    const timer = setInterval(() => setNow(new Date()), 1000)
    return () => clearInterval(timer)
  }, [isRateLimited])

  if (!shouldShowQuota) return null

  return (
    <div>
      {/* First line: Platform + Tier Badge */}
      <div className="mb-1 flex items-center gap-1">
        <span className={cn('badge text-xs px-2 py-0.5 rounded font-medium', tierBadgeClass)}>{tierLabel}</span>
      </div>

      {/* Usage status: unlimited flow or rate limit */}
      <div className="text-xs surface-muted">
        {!isRateLimited ? (
          <span>{t('admin.accounts.gemini.rateLimit.unlimited')}</span>
        ) : (
          <span
            className={cn(
              'font-medium',
              isUrgent ? 'text-red-600 dark:text-red-400 animate-pulse' : 'text-amber-600 dark:text-amber-400'
            )}
          >
            {t('admin.accounts.gemini.rateLimit.limited', { time: resetCountdown })}
          </span>
        )}
      </div>
    </div>
  )
}

export default AccountQuotaInfo
