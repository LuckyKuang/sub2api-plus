import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/auth/Icon'
import { DateTimePicker } from '@/components/auth/DateTimePicker'
import { LocaleSwitcher } from '@/components/auth/LocaleSwitcher'
import { useProductAppearance } from '@/components/auth/useProductAppearance'
import { useAppStore } from '@/stores/app'
import { FeatureFlags, resolveFeatureFlag } from '@/utils/featureFlags'
import { buildGatewayUrl } from '@/api/client'
import { formatDateLocalInput } from '@/utils/format'
import { sanitizeUrl } from '@/utils/url'
import { formatTokenShare, sumTokenBuckets } from '@/utils/tokenShare'
import { cn } from '@/lib/utils'
import '@/views/authViews.css'

const GITHUB_URL = 'https://github.com/luckykuang/sub2api-plus'

type DateRangeKey = 'today' | '7d' | '30d' | 'custom'
type DailyUsageDays = 7 | 30 | 90

interface RingItem {
  title: string
  pct: number
  amount: string
  isBalance?: boolean
  iconType: 'clock' | 'calendar' | 'dollar'
  resetAt?: string | null
}

interface DetailRow {
  iconBg: string
  iconColor: string
  iconSvg: string
  label: string
  value: string
  valueClass: string
}

interface StatCell {
  label: string
  value: string
  share?: string | null
}

interface DailyUsageRow {
  date: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  cost: number
  actual_cost?: number
}

const CIRCUMFERENCE = 2 * Math.PI * 68
const RING_GRADIENTS = [
  { from: '#14b8a6', to: '#2dd4bf' },
  { from: '#6366F1', to: '#A5B4FC' },
  { from: '#10B981', to: '#6EE7B7' },
  { from: '#F59E0B', to: '#FCD34D' }
]

const ICON_SHIELD = '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>'
const ICON_CALENDAR =
  '<rect x="3" y="4" width="18" height="18" rx="2" ry="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/>'
const ICON_DOLLAR = '<line x1="12" y1="1" x2="12" y2="23"/><path d="M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"/>'
const ICON_CHECK = '<polyline points="20 6 9 17 4 12"/>'

function usd(value: number | null | undefined): string {
  if (value == null || value < 0) return '-'
  return '$' + Number(value).toFixed(2)
}

function fmtNum(val: number | null | undefined): string {
  if (val == null) return '-'
  return val.toLocaleString()
}

function getBrowserTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}

function getUsageColor(pct: number): string {
  if (pct > 90) return 'text-rose-500'
  if (pct > 70) return 'text-amber-500'
  return 'text-emerald-500'
}

export default function KeyUsageView() {
  const { t, i18n } = useTranslation()
  const appStore = useAppStore()

  useProductAppearance()

  const settings = appStore.cachedPublicSettings
  const subscriptionFeatureEnabled = resolveFeatureFlag(settings, FeatureFlags.subscription)

  // ==================== Site Settings (same as HomeView) ====================

  const siteName = settings?.site_name || appStore.siteName || 'Sub2API Plus'
  const siteLogo = sanitizeUrl(settings?.site_logo || appStore.siteLogo || '', {
    allowRelative: true,
    allowDataUrl: true
  })
  const docUrl = sanitizeUrl(settings?.doc_url || appStore.docUrl || '')

  // ==================== Theme (same as HomeView) ====================

  const [isDark, setIsDark] = useState(
    () => typeof document !== 'undefined' && document.documentElement.classList.contains('dark')
  )

  function toggleTheme() {
    const next = !isDark
    setIsDark(next)
    document.documentElement.classList.toggle('dark', next)
    localStorage.setItem('theme', next ? 'dark' : 'light')
  }

  const currentYear = new Date().getFullYear()

  // ==================== Key Query State ====================

  const [apiKey, setApiKey] = useState('')
  const [keyVisible, setKeyVisible] = useState(false)
  const [isQuerying, setIsQuerying] = useState(false)
  const [showResults, setShowResults] = useState(false)
  const [showLoading, setShowLoading] = useState(false)
  const [showDatePicker, setShowDatePicker] = useState(false)
  const [resultData, setResultData] = useState<Record<string, any> | null>(null)
  const [now, setNow] = useState(() => new Date())

  // ==================== Date Range State ====================

  const [currentRange, setCurrentRange] = useState<DateRangeKey>('today')
  const [customStartDate, setCustomStartDate] = useState('')
  const [customEndDate, setCustomEndDate] = useState('')
  const [dailyUsageDays, setDailyUsageDaysState] = useState<DailyUsageDays>(30)

  // Setters update these mirrors synchronously so a query triggered in the same
  // tick already sees the new range, like the Vue refs they replace.
  const currentRangeRef = useRef<DateRangeKey>('today')
  const customStartDateRef = useRef('')
  const customEndDateRef = useRef('')
  const dailyUsageDaysRef = useRef<DailyUsageDays>(30)

  function applyDateRange(key: DateRangeKey) {
    currentRangeRef.current = key
    setCurrentRange(key)
  }

  function applyCustomStartDate(value: string) {
    customStartDateRef.current = value
    setCustomStartDate(value)
  }

  function applyCustomEndDate(value: string) {
    customEndDateRef.current = value
    setCustomEndDate(value)
  }

  function applyDailyUsageDays(days: DailyUsageDays) {
    dailyUsageDaysRef.current = days
    setDailyUsageDaysState(days)
  }

  const dateRanges = useMemo(
    () => [
      { key: 'today' as const, label: t('keyUsage.dateRangeToday') },
      { key: '7d' as const, label: t('keyUsage.dateRange7d') },
      { key: '30d' as const, label: t('keyUsage.dateRange30d') },
      { key: 'custom' as const, label: t('keyUsage.dateRangeCustom') }
    ],
    [t]
  )

  const dailyUsageOptions = useMemo(
    () => [
      { value: 7 as const, label: t('keyUsage.dateRange7d') },
      { value: 30 as const, label: t('keyUsage.dateRange30d') },
      { value: 90 as const, label: t('keyUsage.dateRange90d') }
    ],
    [t]
  )

  function getDateParams(): string {
    const reference = new Date()
    const params = new URLSearchParams()
    const range = currentRangeRef.current

    if (range === 'custom') {
      if (customStartDateRef.current && customEndDateRef.current) {
        params.set('start_date', customStartDateRef.current)
        params.set('end_date', customEndDateRef.current)
      }
    } else {
      const end = formatDateLocalInput(reference)
      let start: string
      switch (range) {
        case 'today':
          start = end
          break
        case '7d':
          start = formatDateLocalInput(new Date(reference.getTime() - 7 * 86400000))
          break
        case '30d':
          start = formatDateLocalInput(new Date(reference.getTime() - 30 * 86400000))
          break
        default:
          start = formatDateLocalInput(new Date(reference.getTime() - 30 * 86400000))
      }
      params.set('start_date', start)
      params.set('end_date', end)
    }
    params.set('days', String(dailyUsageDaysRef.current))
    params.set('timezone', getBrowserTimezone())
    return params.toString()
  }

  // ==================== Ring Animation ====================

  const [ringAnimated, setRingAnimated] = useState(false)
  const [displayPcts, setDisplayPcts] = useState<number[]>([])
  const ringAnimatedRef = useRef(false)

  const ringTrackColor = isDark ? '#222222' : '#F0F0EE'

  function getRingOffset(ring: RingItem): number {
    if (!ringAnimated) return CIRCUMFERENCE
    if (ring.isBalance) return 0
    return CIRCUMFERENCE - (Math.min(ring.pct, 100) / 100) * CIRCUMFERENCE
  }

  function triggerRingAnimation(items: RingItem[]) {
    setRingAnimated(false)
    setDisplayPcts(items.map(() => 0))

    requestAnimationFrame(() => {
      setTimeout(() => {
        setRingAnimated(true)

        // Animate percentage numbers
        const duration = 1000
        const startTime = performance.now()
        const targets = items.map((item) => (item.isBalance ? 0 : item.pct))

        function tick() {
          const elapsed = performance.now() - startTime
          const progress = Math.min(elapsed / duration, 1)
          const ease = 1 - Math.pow(1 - progress, 3)
          setDisplayPcts(targets.map((target) => Math.round(ease * target)))
          if (progress < 1) requestAnimationFrame(tick)
        }
        requestAnimationFrame(tick)
      }, 50)
    })
  }

  // ==================== Computed Data ====================

  const statusInfo = useMemo(() => {
    const data = resultData
    if (!data) return null

    if (data.mode === 'quota_limited') {
      const isValid = data.isValid !== false
      const statusMap: Record<string, string> = {
        active: 'Active',
        quota_exhausted: 'Quota Exhausted',
        expired: 'Expired'
      }
      return {
        label: t('keyUsage.quotaMode'),
        statusText: statusMap[data.status] || data.status || 'Unknown',
        isActive: isValid && data.status === 'active'
      }
    }

    return {
      label: data.planName || t('keyUsage.walletBalance'),
      statusText: 'Active',
      isActive: true
    }
  }, [resultData, t])

  const ringItems = useMemo<RingItem[]>(() => {
    const data = resultData
    if (!data) return []

    const items: RingItem[] = []

    if (data.mode === 'quota_limited') {
      if (data.quota) {
        const pct = data.quota.limit > 0 ? Math.min(Math.round((data.quota.used / data.quota.limit) * 100), 100) : 0
        items.push({
          title: t('keyUsage.totalQuota'),
          pct,
          amount: `${usd(data.quota.used)} / ${usd(data.quota.limit)}`,
          iconType: 'dollar'
        })
      }
      if (data.rate_limits) {
        const windowLabels: Record<string, string> = {
          '5h': t('keyUsage.limit5h'),
          '1d': t('keyUsage.limitDaily'),
          '7d': t('keyUsage.limit7d')
        }
        const windowIcons: Record<string, 'clock' | 'calendar'> = {
          '5h': 'clock',
          '1d': 'calendar',
          '7d': 'calendar'
        }
        for (const rl of data.rate_limits) {
          const pct = rl.limit > 0 ? Math.min(Math.round((rl.used / rl.limit) * 100), 100) : 0
          items.push({
            title: windowLabels[rl.window] || rl.window,
            pct,
            amount: `${usd(rl.used)} / ${usd(rl.limit)}`,
            iconType: windowIcons[rl.window] || 'clock',
            resetAt: rl.reset_at
          })
        }
      }
    } else {
      if (data.subscription) {
        const sub = data.subscription
        const limits = [
          { label: t('keyUsage.limit5h'), usage: sub.five_hour_usage_usd, limit: sub.five_hour_limit_usd },
          { label: t('keyUsage.limitDaily'), usage: sub.daily_usage_usd, limit: sub.daily_limit_usd },
          { label: t('keyUsage.limitWeekly'), usage: sub.weekly_usage_usd, limit: sub.weekly_limit_usd },
          { label: t('keyUsage.limitMonthly'), usage: sub.monthly_usage_usd, limit: sub.monthly_limit_usd }
        ]
        for (const limit of limits) {
          if (limit.limit != null && limit.limit > 0) {
            const pct = Math.min(Math.round((limit.usage / limit.limit) * 100), 100)
            items.push({
              title: limit.label,
              pct,
              amount: `${usd(limit.usage)} / ${usd(limit.limit)}`,
              iconType: 'calendar'
            })
          }
        }
      }
      if (!data.subscription && data.balance != null) {
        items.push({
          title: t('keyUsage.walletBalance'),
          pct: 0,
          amount: usd(data.balance),
          isBalance: true,
          iconType: 'dollar'
        })
      }
    }

    return items
  }, [resultData, t])

  const ringGridClass = useMemo(() => {
    const len = ringItems.length
    if (len === 1) return 'grid grid-cols-1 max-w-md mx-auto gap-6'
    if (len === 2) return 'grid grid-cols-1 md:grid-cols-2 gap-6'
    return 'grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6'
  }, [ringItems.length])

  const detailRows = useMemo<DetailRow[]>(() => {
    const data = resultData
    if (!data) return []

    const rows: DetailRow[] = []

    if (data.mode === 'quota_limited') {
      if (data.quota) {
        const remainColor =
          data.quota.remaining <= 0
            ? 'text-rose-500'
            : data.quota.remaining < data.quota.limit * 0.1
              ? 'text-amber-500'
              : 'text-emerald-500'
        rows.push({
          iconBg: 'bg-emerald-500/10',
          iconColor: 'text-emerald-500',
          iconSvg: ICON_SHIELD,
          label: t('keyUsage.remainingQuota'),
          value: usd(data.quota.remaining),
          valueClass: remainColor
        })
      }
      if (data.expires_at) {
        const daysLeft = data.days_until_expiry
        let expiryStr = formatDate(data.expires_at)
        if (daysLeft != null) {
          expiryStr +=
            daysLeft > 0
              ? ` ${t('keyUsage.daysLeft', { days: daysLeft })}`
              : daysLeft === 0
                ? ` ${t('keyUsage.todayExpires')}`
                : ''
        }
        rows.push({
          iconBg: 'bg-amber-500/10',
          iconColor: 'text-amber-500',
          iconSvg: ICON_CALENDAR,
          label: t('keyUsage.expiresAt'),
          value: expiryStr,
          valueClass: ''
        })
      }
      if (data.rate_limits) {
        const windowMap: Record<string, string> = {
          '5h': '5H',
          '1d': i18n.language === 'zh' ? '日' : 'D',
          '7d': '7D'
        }
        for (const rl of data.rate_limits) {
          const pct = rl.limit > 0 ? (rl.used / rl.limit) * 100 : 0
          let valueStr = `${usd(rl.used)} / ${usd(rl.limit)}`
          const resetStr = formatResetTime(rl.reset_at)
          if (resetStr) {
            valueStr += ` (⟳ ${resetStr})`
          }
          rows.push({
            iconBg: 'bg-primary-500/10',
            iconColor: 'text-primary-500',
            iconSvg: ICON_DOLLAR,
            label: `${t('keyUsage.usedQuota')} (${windowMap[rl.window] || rl.window})`,
            value: valueStr,
            valueClass: getUsageColor(pct)
          })
        }
      }
    } else {
      rows.push({
        iconBg: 'bg-emerald-500/10',
        iconColor: 'text-emerald-500',
        iconSvg: ICON_CHECK,
        // 订阅功能关闭后这一行只会是「钱包余额」，标签改用不带「订阅」字样的「计费方式」。
        label: subscriptionFeatureEnabled ? t('keyUsage.subscriptionType') : t('keyUsage.billingType'),
        value: data.planName || t('keyUsage.walletBalance'),
        valueClass: ''
      })

      if (data.subscription) {
        const sub = data.subscription
        if (sub.five_hour_limit_usd > 0) {
          const pct = (sub.five_hour_usage_usd / sub.five_hour_limit_usd) * 100
          rows.push({
            iconBg: 'bg-violet-500/10',
            iconColor: 'text-violet-500',
            iconSvg: ICON_DOLLAR,
            label: `${t('keyUsage.usedQuota')} (${i18n.language === 'zh' ? '5小时' : '5h'})`,
            value: `${usd(sub.five_hour_usage_usd)} / ${usd(sub.five_hour_limit_usd)}`,
            valueClass: getUsageColor(pct)
          })
        }
        if (sub.daily_limit_usd > 0) {
          const pct = (sub.daily_usage_usd / sub.daily_limit_usd) * 100
          rows.push({
            iconBg: 'bg-primary-500/10',
            iconColor: 'text-primary-500',
            iconSvg: ICON_DOLLAR,
            label: `${t('keyUsage.usedQuota')} (${i18n.language === 'zh' ? '日' : 'D'})`,
            value: `${usd(sub.daily_usage_usd)} / ${usd(sub.daily_limit_usd)}`,
            valueClass: getUsageColor(pct)
          })
        }
        if (sub.weekly_limit_usd > 0) {
          const pct = (sub.weekly_usage_usd / sub.weekly_limit_usd) * 100
          rows.push({
            iconBg: 'bg-indigo-500/10',
            iconColor: 'text-indigo-500',
            iconSvg: ICON_DOLLAR,
            label: `${t('keyUsage.usedQuota')} (${i18n.language === 'zh' ? '周' : 'W'})`,
            value: `${usd(sub.weekly_usage_usd)} / ${usd(sub.weekly_limit_usd)}`,
            valueClass: getUsageColor(pct)
          })
        }
        if (sub.monthly_limit_usd > 0) {
          const pct = (sub.monthly_usage_usd / sub.monthly_limit_usd) * 100
          rows.push({
            iconBg: 'bg-emerald-500/10',
            iconColor: 'text-emerald-500',
            iconSvg: ICON_DOLLAR,
            label: `${t('keyUsage.usedQuota')} (${i18n.language === 'zh' ? '月' : 'M'})`,
            value: `${usd(sub.monthly_usage_usd)} / ${usd(sub.monthly_limit_usd)}`,
            valueClass: getUsageColor(pct)
          })
        }
        if (sub.expires_at) {
          rows.push({
            iconBg: 'bg-amber-500/10',
            iconColor: 'text-amber-500',
            iconSvg: ICON_CALENDAR,
            label: t('keyUsage.subscriptionExpires'),
            value: formatDate(sub.expires_at),
            valueClass: ''
          })
        }
      }

      const remainColor =
        data.remaining != null
          ? data.remaining <= 0
            ? 'text-rose-500'
            : data.remaining < 10
              ? 'text-amber-500'
              : 'text-emerald-500'
          : ''
      rows.push({
        iconBg: 'bg-emerald-500/10',
        iconColor: 'text-emerald-500',
        iconSvg: ICON_SHIELD,
        label: t('keyUsage.remainingQuota'),
        value: data.remaining != null ? usd(data.remaining) : '-',
        valueClass: remainColor
      })
    }

    return rows
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [resultData, t, i18n.language, subscriptionFeatureEnabled])

  const usageStatCells = useMemo<StatCell[]>(() => {
    const usage = resultData?.usage
    if (!usage) return []

    const today = usage.today || {}
    const total = usage.total || {}

    return [
      { label: t('keyUsage.todayRequests'), value: fmtNum(today.requests) },
      {
        label: t('keyUsage.todayInputTokens'),
        value: fmtNum(today.input_tokens),
        share: formatTokenShare(today.input_tokens, today.total_tokens)
      },
      {
        label: t('keyUsage.todayOutputTokens'),
        value: fmtNum(today.output_tokens),
        share: formatTokenShare(today.output_tokens, today.total_tokens)
      },
      { label: t('keyUsage.todayTokens'), value: fmtNum(today.total_tokens) },
      {
        label: t('keyUsage.todayCacheCreation'),
        value: fmtNum(today.cache_creation_tokens),
        share: formatTokenShare(today.cache_creation_tokens, today.total_tokens)
      },
      {
        label: t('keyUsage.todayCacheRead'),
        value: fmtNum(today.cache_read_tokens),
        share: formatTokenShare(today.cache_read_tokens, today.total_tokens)
      },
      { label: t('keyUsage.todayCost'), value: usd(today.actual_cost) },
      { label: t('keyUsage.rpmTpm'), value: `${usage.rpm || 0} / ${usage.tpm || 0}` },
      { label: t('keyUsage.totalRequests'), value: fmtNum(total.requests) },
      {
        label: t('keyUsage.totalInputTokens'),
        value: fmtNum(total.input_tokens),
        share: formatTokenShare(total.input_tokens, total.total_tokens)
      },
      {
        label: t('keyUsage.totalOutputTokens'),
        value: fmtNum(total.output_tokens),
        share: formatTokenShare(total.output_tokens, total.total_tokens)
      },
      { label: t('keyUsage.totalTokensLabel'), value: fmtNum(total.total_tokens) },
      {
        label: t('keyUsage.totalCacheCreation'),
        value: fmtNum(total.cache_creation_tokens),
        share: formatTokenShare(total.cache_creation_tokens, total.total_tokens)
      },
      {
        label: t('keyUsage.totalCacheRead'),
        value: fmtNum(total.cache_read_tokens),
        share: formatTokenShare(total.cache_read_tokens, total.total_tokens)
      },
      { label: t('keyUsage.totalCost'), value: usd(total.actual_cost) },
      {
        label: t('keyUsage.avgDuration'),
        value: usage.average_duration_ms ? `${Math.round(usage.average_duration_ms)} ms` : '-'
      }
    ]
  }, [resultData, t])

  const modelStats = useMemo<Array<Record<string, any>>>(() => resultData?.model_stats || [], [resultData])

  const dailyUsageRows = useMemo<DailyUsageRow[]>(() => {
    const rows = resultData?.daily_usage
    return Array.isArray(rows) ? rows : []
  }, [resultData])

  const showDailyUsage = Boolean(resultData && Array.isArray(resultData.daily_usage))

  function dailyTokenTotal(row: DailyUsageRow): number {
    return sumTokenBuckets(
      row.input_tokens,
      row.output_tokens,
      row.cache_read_tokens,
      row.cache_write_tokens
    )
  }

  function dailyTokenShare(row: DailyUsageRow, tokens: number | null | undefined): string | null {
    return formatTokenShare(tokens, dailyTokenTotal(row))
  }

  function modelTokenShare(
    tokens: number | null | undefined,
    totalTokens: number | null | undefined
  ): string | null {
    return formatTokenShare(tokens, totalTokens)
  }

  function totalTokenShare(tokens: number | null | undefined): string | null {
    return formatTokenShare(tokens, resultData?.usage?.total?.total_tokens)
  }

  // ==================== API Query ====================

  async function fetchUsage(key: string) {
    const dateParams = getDateParams()
    const url = buildGatewayUrl('/v1/usage') + (dateParams ? `?${dateParams}` : '')
    const res = await fetch(url, {
      headers: { Authorization: 'Bearer ' + key }
    })
    if (!res.ok) {
      const body = await res.json().catch(() => null)
      const msg = body?.error?.message || body?.message || `${t('keyUsage.queryFailed')} (${res.status})`
      throw new Error(msg)
    }
    return await res.json()
  }

  async function queryKey() {
    if (isQuerying) return
    const key = apiKey.trim()
    if (!key) {
      appStore.showInfo(t('keyUsage.enterApiKey'))
      return
    }

    setIsQuerying(true)
    setShowResults(true)
    setShowLoading(true)
    setResultData(null)

    try {
      const data = await fetchUsage(key)
      setResultData(data)
      setShowLoading(false)
      setShowDatePicker(true)

      // Trigger ring animations after DOM update
      requestAnimationFrame(() => {
        triggerRingAnimation(buildRingItems(data))
      })

      appStore.showSuccess(t('keyUsage.querySuccess'))
    } catch (err) {
      setShowResults(false)
      setShowLoading(false)
      appStore.showError((err as Error).message || t('keyUsage.queryFailedRetry'))
    } finally {
      setIsQuerying(false)
    }
  }

  function setDateRange(key: DateRangeKey) {
    applyDateRange(key)
    if (key !== 'custom') {
      void queryKey()
    }
  }

  function setDailyUsageDays(days: DailyUsageDays) {
    if (dailyUsageDaysRef.current === days) return
    applyDailyUsageDays(days)
    if (resultData && apiKey.trim()) {
      void queryKey()
    }
  }

  // ==================== Lifecycle ====================

  function initTheme() {
    const savedTheme = localStorage.getItem('theme')
    if (savedTheme === 'dark' || (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
      setIsDark(true)
      document.documentElement.classList.add('dark')
    }
  }

  function formatDate(iso: string | null | undefined): string {
    if (!iso) return '-'
    const date = new Date(iso)
    const loc = i18n.language === 'zh' ? 'zh-CN' : 'en-US'
    return date.toLocaleDateString(loc, { year: 'numeric', month: 'long', day: 'numeric' })
  }

  function formatResetTime(resetAt: string | null | undefined): string {
    if (!resetAt) return ''
    const diff = new Date(resetAt).getTime() - now.getTime()
    if (diff <= 0) return t('keyUsage.resetNow')
    const days = Math.floor(diff / 86400000)
    const hours = Math.floor((diff % 86400000) / 3600000)
    const mins = Math.floor((diff % 3600000) / 60000)
    if (days > 0) return `${days}d ${hours}h`
    if (hours > 0) return `${hours}h ${mins}m`
    return `${mins}m`
  }

  // Ring items are recomputed for the animation so the freshly loaded payload
  // drives the targets instead of the previous render's state.
  function buildRingItems(data: Record<string, any> | null): RingItem[] {
    if (!data) return []

    const items: RingItem[] = []

    if (data.mode === 'quota_limited') {
      if (data.quota) {
        const pct =
          data.quota.limit > 0 ? Math.min(Math.round((data.quota.used / data.quota.limit) * 100), 100) : 0
        items.push({
          title: t('keyUsage.totalQuota'),
          pct,
          amount: `${usd(data.quota.used)} / ${usd(data.quota.limit)}`,
          iconType: 'dollar'
        })
      }
      if (data.rate_limits) {
        const windowLabels: Record<string, string> = {
          '5h': t('keyUsage.limit5h'),
          '1d': t('keyUsage.limitDaily'),
          '7d': t('keyUsage.limit7d')
        }
        const windowIcons: Record<string, 'clock' | 'calendar'> = {
          '5h': 'clock',
          '1d': 'calendar',
          '7d': 'calendar'
        }
        for (const rl of data.rate_limits) {
          const pct = rl.limit > 0 ? Math.min(Math.round((rl.used / rl.limit) * 100), 100) : 0
          items.push({
            title: windowLabels[rl.window] || rl.window,
            pct,
            amount: `${usd(rl.used)} / ${usd(rl.limit)}`,
            iconType: windowIcons[rl.window] || 'clock',
            resetAt: rl.reset_at
          })
        }
      }
    } else {
      if (data.subscription) {
        const sub = data.subscription
        const limits = [
          { label: t('keyUsage.limit5h'), usage: sub.five_hour_usage_usd, limit: sub.five_hour_limit_usd },
          { label: t('keyUsage.limitDaily'), usage: sub.daily_usage_usd, limit: sub.daily_limit_usd },
          { label: t('keyUsage.limitWeekly'), usage: sub.weekly_usage_usd, limit: sub.weekly_limit_usd },
          { label: t('keyUsage.limitMonthly'), usage: sub.monthly_usage_usd, limit: sub.monthly_limit_usd }
        ]
        for (const limit of limits) {
          if (limit.limit != null && limit.limit > 0) {
            const pct = Math.min(Math.round((limit.usage / limit.limit) * 100), 100)
            items.push({
              title: limit.label,
              pct,
              amount: `${usd(limit.usage)} / ${usd(limit.limit)}`,
              iconType: 'calendar'
            })
          }
        }
      }
      if (!data.subscription && data.balance != null) {
        items.push({
          title: t('keyUsage.walletBalance'),
          pct: 0,
          amount: usd(data.balance),
          isBalance: true,
          iconType: 'dollar'
        })
      }
    }

    return items
  }

  useEffect(() => {
    initTheme()
    if (!appStore.publicSettingsLoaded) {
      void appStore.fetchPublicSettings()
    }
    const resetTimer = setInterval(() => setNow(new Date()), 60000)
    return () => clearInterval(resetTimer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    ringAnimatedRef.current = ringAnimated
  }, [ringAnimated])

  return (
    <div className="relative flex min-h-screen flex-col gap-[var(--shell-gap)] bg-canvas p-[var(--shell-gutter)] dark:surface-inset">
      {/* Header (same pattern as HomeView) */}
      <header className="shell-surface relative z-20 px-4 py-4 sm:px-6">
        <nav className="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-3">
          <Link to="/home" className="flex min-w-0 items-center gap-3">
            <div className="h-10 w-10 shrink-0 overflow-hidden rounded-xl shadow-md">
              <img src={siteLogo || '/logo.svg'} alt="Logo" className="h-full w-full object-contain" />
            </div>
            <span className="truncate text-lg font-semibold tracking-tight text-gray-900 dark:text-white">
              {siteName}
            </span>
          </Link>
          <div className="flex items-center gap-3">
            <LocaleSwitcher />
            {docUrl && (
              <a
                href={docUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="rounded-lg p-2 transition-colors hover:surface-hover hover:text-gray-700 dark:hover:text-white surface-muted"
                title={t('home.viewDocs')}
              >
                <Icon name="book" size="md" />
              </a>
            )}
            <button
              onClick={toggleTheme}
              className="theme-toggle rounded-lg p-2 transition-colors surface-control"
              title={isDark ? t('home.switchToLight') : t('home.switchToDark')}
            >
              <Icon name={isDark ? 'sun' : 'moon'} size="md" />
            </button>
          </div>
        </nav>
      </header>

      {/* Main Content */}
      <main className="mx-auto flex w-full min-w-0 max-w-5xl flex-1 flex-col gap-[var(--shell-gap)]">
        {/* Hero */}
        <div className="card px-4 py-6 text-center sm:p-8">
          <h1 className="text-3xl sm:text-4xl font-bold tracking-tight mb-3 text-gray-900 dark:text-white">
            {t('keyUsage.title')}
          </h1>
          <p className="text-base max-w-md mx-auto surface-muted">{t('keyUsage.subtitle')}</p>
        </div>

        {/* Input Section */}
        <div className="card mx-auto w-full min-w-0 max-w-xl p-4 sm:p-6">
          <div className="flex flex-col gap-3 sm:flex-row">
            <div className="relative min-w-0 flex-1">
              <div className="absolute left-4 top-1/2 -translate-y-1/2 surface-muted">
                <svg className="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <rect x="3" y="11" width="18" height="11" rx="2" ry="2" />
                  <path d="M7 11V7a5 5 0 0 1 10 0v4" />
                </svg>
              </div>
              <input
                value={apiKey}
                onChange={(event) => setApiKey(event.target.value)}
                type={keyVisible ? 'text' : 'password'}
                placeholder={t('keyUsage.placeholder')}
                className="input-ring w-full h-12 pl-12 pr-12 rounded-xl border border-gray-200 surface-inset text-sm text-gray-900 placeholder:text-gray-400 transition-all dark:border-dark-700 dark:text-white dark:placeholder:text-dark-500"
                onKeyDown={(event) => {
                  if (event.key === 'Enter') void queryKey()
                }}
              />
              <button
                onClick={() => setKeyVisible((value) => !value)}
                className="absolute right-4 top-1/2 -translate-y-1/2 hover:text-gray-700 dark:hover:text-white transition-colors surface-control surface-muted"
              >
                {!keyVisible ? (
                  <svg className="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24" />
                    <line x1="1" y1="1" x2="23" y2="23" />
                  </svg>
                ) : (
                  <svg className="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
                    <circle cx="12" cy="12" r="3" />
                  </svg>
                )}
              </button>
            </div>
            <button
              onClick={() => void queryKey()}
              disabled={isQuerying}
              className="h-12 px-7 rounded-xl bg-primary-500 hover:bg-primary-600 text-white font-medium text-sm transition-all active:scale-[0.97] flex items-center justify-center gap-2 whitespace-nowrap disabled:opacity-60"
            >
              {isQuerying ? (
                <svg className="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none">
                  <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" opacity="0.25" />
                  <path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
                </svg>
              ) : (
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                  <circle cx="11" cy="11" r="8" />
                  <line x1="21" y1="21" x2="16.65" y2="16.65" />
                </svg>
              )}
              {isQuerying ? t('keyUsage.querying') : t('keyUsage.query')}
            </button>
          </div>
          <p className="text-xs mt-3 text-center surface-muted">{t('keyUsage.privacyNote')}</p>

          {/* Date Range Picker */}
          {showDatePicker && (
            <div className="mt-4">
              <div className="flex flex-wrap items-center gap-2 justify-center">
                <span className="text-xs surface-muted">{t('keyUsage.dateRange')}</span>
                {dateRanges.map((range) => (
                  <button
                    key={range.key}
                    onClick={() => setDateRange(range.key)}
                    className={cn(
                      'text-xs px-3 py-1.5 rounded-lg border transition-all surface-control',
                      currentRange === range.key
                        ? 'bg-primary-500 text-white border-primary-500'
                        : 'border-gray-200 surface-field text-gray-700 dark:border-dark-700 dark:surface-field dark:text-dark-200 hover:border-primary-300 dark:hover:border-dark-600'
                    )}
                  >
                    {range.label}
                  </button>
                ))}
                {currentRange === 'custom' && (
                  <div className="flex max-w-full flex-wrap items-center justify-center gap-2">
                    <DateTimePicker
                      aria-label={t('dates.startDate')}
                      value={customStartDate}
                      type="date"
                      className="input-ring text-xs px-2 py-1.5 rounded-lg border border-gray-200 surface-inset text-gray-900 dark:border-dark-700 dark:text-white"
                      onValueChange={applyCustomStartDate}
                    />
                    <span className="text-xs surface-muted">-</span>
                    <DateTimePicker
                      aria-label={t('dates.endDate')}
                      value={customEndDate}
                      type="date"
                      className="input-ring text-xs px-2 py-1.5 rounded-lg border border-gray-200 surface-inset text-gray-900 dark:border-dark-700 dark:text-white"
                      onValueChange={applyCustomEndDate}
                    />
                    <button
                      onClick={() => void queryKey()}
                      className="text-xs px-3 py-1.5 rounded-lg bg-primary-500 text-white hover:bg-primary-600"
                    >
                      {t('keyUsage.apply')}
                    </button>
                  </div>
                )}
              </div>
            </div>
          )}
        </div>

        {/* Results Container */}
        {showResults && (
          <div>
            {/* Loading Skeleton */}
            {showLoading && (
              <div className="space-y-6">
                <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                  <div className="card p-5 sm:p-8">
                    <div className="skeleton h-5 w-24 mb-6" />
                    <div className="flex justify-center">
                      <div className="skeleton w-44 h-44 rounded-full" />
                    </div>
                  </div>
                  <div className="card p-5 sm:p-8">
                    <div className="skeleton h-5 w-24 mb-6" />
                    <div className="flex justify-center">
                      <div className="skeleton w-44 h-44 rounded-full" />
                    </div>
                  </div>
                </div>
                <div className="card p-5 sm:p-8">
                  <div className="skeleton h-5 w-32 mb-6" />
                  <div className="space-y-4">
                    <div className="skeleton h-4 w-full" />
                    <div className="skeleton h-4 w-3/4" />
                    <div className="skeleton h-4 w-5/6" />
                    <div className="skeleton h-4 w-2/3" />
                  </div>
                </div>
              </div>
            )}

            {/* Result Content */}
            {!showLoading && resultData && (
              <div className="space-y-6">
                {/* Status Badge */}
                {statusInfo && (
                  <div className="fade-up flex items-center justify-center mb-2">
                    <div className="inline-flex items-center gap-2 px-5 py-2.5 rounded-full border border-gray-200 surface-inset shadow-sm dark:border-dark-700">
                      <span
                        className={cn(
                          'console-data-mark w-2.5 h-2.5 rounded-full pulse-dot',
                          statusInfo.isActive ? 'bg-emerald-500' : 'bg-rose-500'
                        )}
                      />
                      <span className="text-sm font-medium text-gray-900 dark:text-white">{statusInfo.label}</span>
                      <span className="text-xs surface-muted">|</span>
                      <span className="text-xs surface-muted">{statusInfo.statusText}</span>
                    </div>
                  </div>
                )}

                {/* Ring Cards Grid */}
                {ringItems.length > 0 && (
                  <div className={ringGridClass}>
                    {ringItems.map((ring, index) => (
                      <div
                        key={index}
                        className={cn('card fade-up p-5 transition-all duration-300 hover:shadow-lg sm:p-8', `fade-up-delay-${Math.min(index + 1, 4)}`)}
                      >
                        <div className="flex items-center justify-between mb-6">
                          <h3 className="text-sm font-semibold uppercase tracking-wider surface-muted">{ring.title}</h3>
                          {ring.iconType === 'clock' ? (
                            <svg className="w-5 h-5 surface-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                              <circle cx="12" cy="12" r="10" />
                              <polyline points="12 6 12 12 16 14" />
                            </svg>
                          ) : ring.iconType === 'calendar' ? (
                            <svg className="w-5 h-5 surface-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                              <rect x="3" y="4" width="18" height="18" rx="2" ry="2" />
                              <line x1="16" y1="2" x2="16" y2="6" />
                              <line x1="8" y1="2" x2="8" y2="6" />
                              <line x1="3" y1="10" x2="21" y2="10" />
                            </svg>
                          ) : (
                            <svg className="w-5 h-5 surface-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                              <line x1="12" y1="1" x2="12" y2="23" />
                              <path d="M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6" />
                            </svg>
                          )}
                        </div>
                        <div className="flex justify-center">
                          <div className="relative">
                            <svg className="w-44 h-44" viewBox="0 0 160 160">
                              <circle cx="80" cy="80" r="68" fill="none" stroke={ringTrackColor} strokeWidth="10" />
                              <circle
                                className="progress-ring"
                                cx="80"
                                cy="80"
                                r="68"
                                fill="none"
                                stroke={`url(#ring-grad-${index})`}
                                strokeWidth="10"
                                strokeLinecap="round"
                                strokeDasharray={CIRCUMFERENCE.toFixed(2)}
                                strokeDashoffset={getRingOffset(ring)}
                              />
                              <defs>
                                <linearGradient id={`ring-grad-${index}`} x1="0%" y1="0%" x2="100%" y2="100%">
                                  <stop offset="0%" stopColor={RING_GRADIENTS[index % 4].from} />
                                  <stop offset="100%" stopColor={RING_GRADIENTS[index % 4].to} />
                                </linearGradient>
                              </defs>
                            </svg>
                            <div className="absolute inset-0 flex flex-col items-center justify-center">
                              {ring.isBalance ? (
                                <span
                                  className="text-2xl font-bold tabular-nums"
                                  style={{ color: RING_GRADIENTS[index % 4].from }}
                                >
                                  {ring.amount}
                                </span>
                              ) : (
                                <>
                                  <span className="text-3xl font-bold tabular-nums text-gray-900 dark:text-white">
                                    {displayPcts[index] ?? 0}%
                                  </span>
                                  <span className="text-xs mt-0.5 surface-muted">{t('keyUsage.used')}</span>
                                  <span
                                    className="text-sm font-semibold mt-1 tabular-nums"
                                    style={{ color: RING_GRADIENTS[index % 4].from }}
                                  >
                                    {ring.amount}
                                  </span>
                                  {ring.resetAt && formatResetTime(ring.resetAt) && (
                                    <p className="text-xs mt-0.5 tabular-nums surface-muted">
                                      ⟳ {formatResetTime(ring.resetAt)}
                                    </p>
                                  )}
                                </>
                              )}
                            </div>
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                )}

                {/* Detail Card */}
                {detailRows.length > 0 && (
                  <div className="card fade-up fade-up-delay-3 overflow-hidden">
                    <div className="px-8 py-5 border-b border-gray-200 dark:border-dark-700">
                      <h3 className="text-sm font-semibold uppercase tracking-wider surface-muted">
                        {t('keyUsage.detailInfo')}
                      </h3>
                    </div>
                    <div className="divide-y divide-gray-100 dark:divide-dark-800">
                      {detailRows.map((row, index) => (
                        <div key={index} className="px-8 py-4 flex items-center justify-between">
                          <div className="flex items-center gap-3">
                            <div className={cn('w-8 h-8 rounded-lg flex items-center justify-center', row.iconBg)}>
                              <svg
                                className={cn('w-4 h-4', row.iconColor)}
                                viewBox="0 0 24 24"
                                fill="none"
                                stroke="currentColor"
                                strokeWidth="2"
                                strokeLinecap="round"
                                strokeLinejoin="round"
                                dangerouslySetInnerHTML={{ __html: row.iconSvg }}
                              />
                            </div>
                            <span className="text-sm surface-muted">{row.label}</span>
                          </div>
                          <span className={cn('text-sm font-semibold tabular-nums', row.valueClass || 'text-gray-900 dark:text-white')}>
                            {row.value}
                          </span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {/* Usage Stats Card */}
                {usageStatCells.length > 0 && (
                  <div className="card fade-up fade-up-delay-3 overflow-hidden">
                    <div className="px-8 py-5 border-b border-gray-200 dark:border-dark-700">
                      <h3 className="text-sm font-semibold uppercase tracking-wider surface-muted">
                        {t('keyUsage.tokenStats')}
                      </h3>
                    </div>
                    <div className="grid grid-cols-2 md:grid-cols-4 gap-px surface-inset">
                      {usageStatCells.map((cell, index) => (
                        <div key={index} className="surface-inset px-6 py-4">
                          <div className="text-xs mb-1 surface-muted">{cell.label}</div>
                          <div className="text-sm font-semibold tabular-nums text-gray-900 dark:text-white">
                            {cell.value}{' '}
                            {cell.share && <span className="text-xs font-normal surface-muted">({cell.share})</span>}
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {/* Daily Usage Table */}
                {showDailyUsage && (
                  <div className="card fade-up fade-up-delay-4 overflow-hidden">
                    <div className="flex flex-col gap-3 px-8 py-5 border-b border-gray-200 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between">
                      <h3 className="text-sm font-semibold uppercase tracking-wider surface-muted">
                        {t('keyUsage.dailyDetail')}
                      </h3>
                      <div className="inline-flex rounded-lg border border-gray-200 surface-inset p-0.5 dark:border-dark-700">
                        {dailyUsageOptions.map((option) => (
                          <button
                            key={option.value}
                            onClick={() => setDailyUsageDays(option.value)}
                            className={cn(
                              'min-w-12 rounded-md px-3 py-1.5 text-xs font-medium transition-colors surface-control',
                              dailyUsageDays === option.value
                                ? 'bg-primary-500 text-white'
                                : 'text-gray-600 hover:surface-hover dark:text-dark-300 dark:hover:surface-hover'
                            )}
                          >
                            {option.label}
                          </button>
                        ))}
                      </div>
                    </div>
                    {dailyUsageRows.length > 0 ? (
                      <div className="record-table-scroll table-container overflow-x-auto">
                        <table className="record-table w-full">
                          <thead>
                            <tr className="border-b border-gray-200 surface-inset dark:border-dark-700">
                              <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider surface-muted">
                                {t('keyUsage.date')}
                              </th>
                              <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider surface-muted">
                                {t('keyUsage.requests')}
                              </th>
                              <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider surface-muted">
                                {t('keyUsage.inputTokens')}
                              </th>
                              <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider surface-muted">
                                {t('keyUsage.outputTokens')}
                              </th>
                              <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider surface-muted">
                                {t('keyUsage.cacheReadTokens')}
                              </th>
                              <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider surface-muted">
                                {t('keyUsage.cacheWriteTokens')}
                              </th>
                              <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider surface-muted">
                                {t('keyUsage.cost')}
                              </th>
                            </tr>
                          </thead>
                          <tbody>
                            {dailyUsageRows.map((row) => (
                              <tr
                                key={row.date}
                                className="border-b border-gray-100 last:border-b-0 dark:border-dark-800"
                              >
                                <td className="px-4 py-3 text-sm font-medium whitespace-nowrap text-gray-900 dark:text-white">
                                  {row.date}
                                </td>
                                <td className="px-4 py-3 text-sm tabular-nums text-right surface-muted">
                                  {fmtNum(row.requests)}
                                </td>
                                <td className="px-4 py-3 text-sm tabular-nums text-right surface-muted">
                                  {fmtNum(row.input_tokens)}{' '}
                                  {dailyTokenShare(row, row.input_tokens) && (
                                    <span className="text-xs surface-muted">({dailyTokenShare(row, row.input_tokens)})</span>
                                  )}
                                </td>
                                <td className="px-4 py-3 text-sm tabular-nums text-right surface-muted">
                                  {fmtNum(row.output_tokens)}{' '}
                                  {dailyTokenShare(row, row.output_tokens) && (
                                    <span className="text-xs surface-muted">({dailyTokenShare(row, row.output_tokens)})</span>
                                  )}
                                </td>
                                <td className="px-4 py-3 text-sm tabular-nums text-right surface-muted">
                                  {fmtNum(row.cache_read_tokens)}{' '}
                                  {dailyTokenShare(row, row.cache_read_tokens) && (
                                    <span className="text-xs surface-muted">({dailyTokenShare(row, row.cache_read_tokens)})</span>
                                  )}
                                </td>
                                <td className="px-4 py-3 text-sm tabular-nums text-right surface-muted">
                                  {fmtNum(row.cache_write_tokens)}{' '}
                                  {dailyTokenShare(row, row.cache_write_tokens) && (
                                    <span className="text-xs surface-muted">({dailyTokenShare(row, row.cache_write_tokens)})</span>
                                  )}
                                </td>
                                <td className="px-4 py-3 text-sm tabular-nums text-right font-medium text-gray-900 dark:text-white">
                                  {usd(row.actual_cost != null ? row.actual_cost : row.cost)}
                                </td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    ) : (
                      <div className="px-8 py-8 text-center text-sm surface-muted">{t('keyUsage.noDailyUsage')}</div>
                    )}
                  </div>
                )}

                {/* Model Stats Table */}
                {modelStats.length > 0 && (
                  <div className="card fade-up fade-up-delay-4 overflow-hidden">
                    <div className="px-8 py-5 border-b border-gray-200 dark:border-dark-700">
                      <h3 className="text-sm font-semibold uppercase tracking-wider surface-muted">
                        {t('keyUsage.modelStats')}
                      </h3>
                    </div>
                    <div className="record-table-scroll table-container overflow-x-auto">
                      <table className="record-table w-full">
                        <thead>
                          <tr className="border-b border-gray-200 surface-inset dark:border-dark-700">
                            <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider surface-muted">
                              {t('keyUsage.model')}
                            </th>
                            <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider surface-muted">
                              {t('keyUsage.requests')}
                            </th>
                            <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider surface-muted">
                              {t('keyUsage.inputTokens')}
                            </th>
                            <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider surface-muted">
                              {t('keyUsage.outputTokens')}
                            </th>
                            <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider surface-muted">
                              {t('keyUsage.cacheCreationTokens')}
                            </th>
                            <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider surface-muted">
                              {t('keyUsage.cacheReadTokens')}
                            </th>
                            <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider surface-muted">
                              {t('keyUsage.totalTokens')}
                            </th>
                            <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider surface-muted">
                              {t('keyUsage.cost')}
                            </th>
                          </tr>
                        </thead>
                        <tbody>
                          {modelStats.map((model, index) => (
                            <tr key={index} className="border-b border-gray-100 last:border-b-0 dark:border-dark-800">
                              <td className="px-4 py-3 text-sm font-medium whitespace-nowrap text-gray-900 dark:text-white">
                                {model.model || '-'}
                              </td>
                              <td className="px-4 py-3 text-sm tabular-nums text-right surface-muted">
                                {fmtNum(model.requests)}
                              </td>
                              <td className="px-4 py-3 text-sm tabular-nums text-right surface-muted">
                                {fmtNum(model.input_tokens)}{' '}
                                {modelTokenShare(model.input_tokens, model.total_tokens) && (
                                  <span className="text-xs surface-muted">
                                    ({modelTokenShare(model.input_tokens, model.total_tokens)})
                                  </span>
                                )}
                              </td>
                              <td className="px-4 py-3 text-sm tabular-nums text-right surface-muted">
                                {fmtNum(model.output_tokens)}{' '}
                                {modelTokenShare(model.output_tokens, model.total_tokens) && (
                                  <span className="text-xs surface-muted">
                                    ({modelTokenShare(model.output_tokens, model.total_tokens)})
                                  </span>
                                )}
                              </td>
                              <td className="px-4 py-3 text-sm tabular-nums text-right surface-muted">
                                {fmtNum(model.cache_creation_tokens)}{' '}
                                {modelTokenShare(model.cache_creation_tokens, model.total_tokens) && (
                                  <span className="text-xs surface-muted">
                                    ({modelTokenShare(model.cache_creation_tokens, model.total_tokens)})
                                  </span>
                                )}
                              </td>
                              <td className="px-4 py-3 text-sm tabular-nums text-right surface-muted">
                                {fmtNum(model.cache_read_tokens)}{' '}
                                {modelTokenShare(model.cache_read_tokens, model.total_tokens) && (
                                  <span className="text-xs surface-muted">
                                    ({modelTokenShare(model.cache_read_tokens, model.total_tokens)})
                                  </span>
                                )}
                              </td>
                              <td className="px-4 py-3 text-sm tabular-nums text-right surface-muted">
                                {fmtNum(model.total_tokens)}{' '}
                                {totalTokenShare(model.total_tokens) && (
                                  <span className="text-xs surface-muted">({totalTokenShare(model.total_tokens)})</span>
                                )}
                              </td>
                              <td className="px-4 py-3 text-sm tabular-nums text-right font-medium text-gray-900 dark:text-white">
                                {usd(model.actual_cost != null ? model.actual_cost : model.cost)}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </main>

      {/* Footer (same pattern as HomeView) */}
      <footer className="shell-surface relative z-10 px-4 py-6 sm:px-6">
        <div className="mx-auto flex max-w-6xl flex-col items-center justify-center gap-4 text-center sm:flex-row sm:text-left">
          <p className="text-sm surface-muted">
            &copy; {currentYear} {siteName}. {t('home.footer.allRightsReserved')}
          </p>
          <div className="flex items-center gap-4">
            {docUrl && (
              <a
                href={docUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="text-sm transition-colors hover:text-gray-700 dark:hover:text-white surface-muted"
              >
                {t('home.docs')}
              </a>
            )}
            <a
              href={GITHUB_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="text-sm transition-colors hover:text-gray-700 dark:hover:text-white surface-muted"
            >
              GitHub
            </a>
          </div>
        </div>
      </footer>
    </div>
  )
}
