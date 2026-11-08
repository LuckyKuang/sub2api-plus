import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

export type MonitorWindow = '7d' | '15d' | '30d'
export type OverallStatus = 'operational' | 'degraded'

export interface AutoRefreshButtonProps {
  enabled: boolean
  fetching?: boolean
  paused?: boolean
  intervalSeconds: number
  countdown: number
  intervals: readonly number[]
  intervalOnly?: boolean
  onEnabledChange?: (value: boolean) => void
  onIntervalChange?: (value: number) => void
}

export function AutoRefreshButton({
  enabled,
  fetching = false,
  paused = false,
  intervalSeconds,
  countdown,
  intervals,
  intervalOnly = false,
  onEnabledChange,
  onIntervalChange
}: AutoRefreshButtonProps) {
  const { t } = useTranslation()
  const [showDropdown, setShowDropdown] = useState(false)
  const dropdownRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setShowDropdown(false)
      }
    }
    document.addEventListener('click', handleClickOutside)
    return () => document.removeEventListener('click', handleClickOutside)
  }, [])

  const label = fetching
    ? t('inspection.updating')
    : paused && enabled
      ? t('inspection.paused')
      : enabled
        ? t('common.autoRefresh.countdown', { seconds: countdown })
        : intervalOnly
          ? t('common.autoRefresh.seconds', { n: intervalSeconds })
          : t('common.autoRefresh.title')

  return (
    <div className="relative" ref={dropdownRef}>
      <button
        onClick={() => setShowDropdown((value) => !value)}
        className="surface-control surface-muted inline-flex items-center gap-1.5 rounded-lg border border-gray-200 px-2.5 py-1.5 text-xs font-medium shadow-sm transition-colors hover:surface-hover dark:border-dark-600"
        title={t('common.autoRefresh.title')}
      >
        <svg
          className={`h-3.5 w-3.5 ${fetching ? 'animate-spin' : ''}`}
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 20 20"
          fill="currentColor"
        >
          <path
            fillRule="evenodd"
            d="M15.312 11.424a5.5 5.5 0 01-9.201 2.466l-.312-.311h2.433a.75.75 0 000-1.5H4.598a.75.75 0 00-.75.75v3.634a.75.75 0 001.5 0v-2.033l.312.312a7 7 0 0011.712-3.138.75.75 0 00-1.449-.39zm-10.624-2.848a5.5 5.5 0 019.201-2.466l.312.311H11.768a.75.75 0 000 1.5h3.634a.75.75 0 00.75-.75V3.537a.75.75 0 00-1.5 0v2.034l-.312-.312A7 7 0 002.628 8.397a.75.75 0 001.449.39z"
            clipRule="evenodd"
          />
        </svg>
        <span>{label}</span>
      </button>

      {showDropdown && (
        <div className="surface-panel absolute right-0 z-20 mt-1 w-44 rounded-lg border border-gray-200 shadow-lg dark:border-dark-600">
          <div className="p-1.5">
            {!intervalOnly && (
              <>
                <button
                  onClick={() => onEnabledChange?.(!enabled)}
                  className="surface-control surface-muted flex w-full items-center justify-between rounded-md px-3 py-1.5 text-sm hover:surface-hover"
                >
                  <span>{t('common.autoRefresh.enable')}</span>
                  {enabled && (
                    <svg className="h-4 w-4 text-primary-500" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor">
                      <path
                        fillRule="evenodd"
                        d="M16.704 4.153a.75.75 0 01.143 1.052l-8 10.5a.75.75 0 01-1.127.075l-4.5-4.5a.75.75 0 011.06-1.06l3.894 3.893 7.48-9.817a.75.75 0 011.05-.143z"
                        clipRule="evenodd"
                      />
                    </svg>
                  )}
                </button>
                <div className="my-1 border-t border-gray-100 dark:border-dark-700" />
              </>
            )}
            {intervals.map((sec) => (
              <button
                key={sec}
                onClick={() => onIntervalChange?.(sec)}
                className="surface-control surface-muted flex w-full items-center justify-between rounded-md px-3 py-1.5 text-sm hover:surface-hover"
              >
                <span>{t('common.autoRefresh.seconds', { n: sec })}</span>
                {intervalSeconds === sec && (
                  <svg className="h-4 w-4 text-primary-500" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor">
                    <path
                      fillRule="evenodd"
                      d="M16.704 4.153a.75.75 0 01.143 1.052l-8 10.5a.75.75 0 01-1.127.075l-4.5-4.5a.75.75 0 011.06-1.06l3.894 3.893 7.48-9.817a.75.75 0 011.05-.143z"
                      clipRule="evenodd"
                    />
                  </svg>
                )}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

export default AutoRefreshButton
