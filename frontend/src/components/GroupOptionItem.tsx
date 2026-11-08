import { useTranslation } from 'react-i18next'
import { useAppStore } from '@/stores/app'
import { cn } from '@/lib/utils'
import type { GroupPlatform, SubscriptionType } from '@/types'
import { formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import { GroupBadge } from './badges'

export interface GroupOptionItemProps {
  name: string
  platform: GroupPlatform
  subscriptionType?: SubscriptionType
  rateMultiplier?: number
  userRateMultiplier?: number | null
  peakRateEnabled?: boolean
  peakStart?: string
  peakEnd?: string
  peakRateMultiplier?: number
  description?: string | null
  selected?: boolean
  showCheckmark?: boolean
  rates?: React.ReactNode
}

export function GroupOptionItem({
  name,
  platform,
  subscriptionType = 'standard',
  rateMultiplier,
  userRateMultiplier = null,
  peakRateEnabled = false,
  peakStart,
  peakEnd,
  peakRateMultiplier,
  description,
  selected = false,
  showCheckmark = true,
  rates
}: GroupOptionItemProps) {
  const { t } = useTranslation()
  const appStore = useAppStore()

  // Whether user has a custom rate different from default
  const hasCustomRate =
    userRateMultiplier !== null &&
    userRateMultiplier !== undefined &&
    rateMultiplier !== undefined &&
    userRateMultiplier !== rateMultiplier

  const hasPeakRate = Boolean(peakRateEnabled && peakStart && peakEnd)

  const peakRateText = formatPeakRateWindow(
    {
      peak_rate_enabled: peakRateEnabled,
      peak_start: peakStart,
      peak_end: peakEnd,
      peak_rate_multiplier: peakRateMultiplier
    },
    serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset)
  )

  const peakRateTitle = t('common.peakRateTooltip', { window: peakRateText })

  // Rate pill color matches platform badge color
  const ratePillClass = (() => {
    switch (platform) {
      case 'anthropic':
        return 'bg-amber-50/[0.15] text-amber-700 dark:bg-amber-900/20 dark:text-amber-400'
      case 'openai':
        return 'bg-green-50/[0.15] text-green-700 dark:bg-green-900/20 dark:text-green-400'
      case 'gemini':
        return 'bg-sky-50/[0.15] text-sky-700 dark:bg-sky-900/20 dark:text-sky-400'
      default:
        // antigravity and others
        return 'bg-violet-50/[0.15] text-violet-700 dark:bg-violet-900/20 dark:text-violet-400'
    }
  })()

  return (
    <div className="flex min-w-0 flex-1 items-start justify-between gap-3">
      {/* Left: name + description */}
      <div className="flex min-w-0 flex-1 flex-col items-start" title={description || undefined}>
        {/* Row 1: platform badge (name bold) */}
        <GroupBadge
          name={name}
          platform={platform}
          subscriptionType={subscriptionType}
          showRate={false}
          className="groupOptionItemBadge"
        />
        {/* Row 2: description with top spacing */}
        {description && (
          <span className="mt-1.5 w-full whitespace-pre-line [overflow-wrap:anywhere] text-left text-xs leading-relaxed line-clamp-3 surface-muted">
            {description}
          </span>
        )}
        {rates && <div className="mt-1.5 w-full whitespace-normal text-left">{rates}</div>}
      </div>

      {/* Right: rate pill + checkmark (vertically centered to first row) */}
      <div className="flex shrink-0 items-center gap-2 pt-0.5">
        <div className="flex shrink-0 flex-col items-end gap-1">
          {/* Rate pill (platform color) */}
          {!rates && rateMultiplier !== undefined && (
            <span
              className={cn(
                'inline-flex items-center whitespace-nowrap rounded-full px-3 py-1 text-xs font-semibold',
                ratePillClass
              )}
            >
              {hasCustomRate ? (
                <>
                  <span className="mr-1 line-through opacity-50">{rateMultiplier}x</span>
                  <span className="font-bold">{userRateMultiplier}x</span>
                </>
              ) : (
                <>
                  {rateMultiplier}x {t('admin.groups.rateLabel')}
                </>
              )}
            </span>
          )}
          {!rates && hasPeakRate && (
            <span
              className="inline-flex items-center whitespace-nowrap rounded-full bg-amber-50/[0.15] px-3 py-1 text-xs font-semibold text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
              title={peakRateTitle}
            >
              {peakRateText}
            </span>
          )}
        </div>
        {/* Checkmark */}
        {showCheckmark && selected && (
          <svg
            className="h-4 w-4 shrink-0 text-primary-600 dark:text-primary-400"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
            strokeWidth="2"
            aria-hidden="true"
          >
            <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
          </svg>
        )}
      </div>
    </div>
  )
}
