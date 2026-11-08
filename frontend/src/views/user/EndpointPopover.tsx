import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useClipboard } from './useClipboard'
import type { CustomEndpoint } from '@/types'

export interface EndpointPopoverProps {
  apiBaseUrl: string
  customEndpoints: CustomEndpoint[]
}

export function EndpointPopover({ apiBaseUrl, customEndpoints }: EndpointPopoverProps) {
  const { t } = useTranslation()
  const { copyToClipboard } = useClipboard()
  const [copiedEndpoint, setCopiedEndpoint] = useState<string | null>(null)
  const copiedResetTimer = useRef<number | undefined>(undefined)

  const allEndpoints: Array<{ name: string; endpoint: string; description: string; isDefault: boolean }> = []
  if (apiBaseUrl) {
    allEndpoints.push({ name: t('keys.endpoints.title'), endpoint: apiBaseUrl, description: '', isDefault: true })
  }
  for (const ep of customEndpoints) {
    allEndpoints.push({ ...ep, isDefault: false })
  }

  useEffect(() => {
    return () => {
      if (copiedResetTimer.current !== undefined) window.clearTimeout(copiedResetTimer.current)
    }
  }, [])

  async function copy(url: string) {
    const success = await copyToClipboard(url, t('keys.endpoints.copied'))
    if (!success) return

    setCopiedEndpoint(url)
    if (copiedResetTimer.current !== undefined) window.clearTimeout(copiedResetTimer.current)
    copiedResetTimer.current = window.setTimeout(() => {
      setCopiedEndpoint((current) => (current === url ? null : current))
    }, 1800)
  }

  function tooltipHint(endpoint: string): string {
    return copiedEndpoint === endpoint ? t('keys.endpoints.copiedHint') : t('keys.endpoints.clickToCopy')
  }

  function speedTestUrl(endpoint: string): string {
    return `https://www.tcptest.cn/http/${encodeURIComponent(endpoint)}`
  }

  if (allEndpoints.length === 0) return null

  return (
    <div className="flex flex-wrap gap-2">
      {allEndpoints.map((item, index) => (
        <div
          key={index}
          className="surface-inset flex items-center gap-1.5 rounded-lg border border-gray-200 px-2.5 py-1.5 text-xs transition-colors hover:border-primary-200 dark:border-dark-600 dark:hover:border-primary-700"
        >
          <span className="font-medium surface-muted">{item.name}</span>
          {item.isDefault && (
            <span className="rounded bg-primary-50/[0.15] px-1 py-px text-[10px] font-medium leading-tight text-primary-600 dark:bg-primary-900/30 dark:text-primary-400">
              {t('keys.endpoints.default')}
            </span>
          )}

          <span className="text-gray-300 dark:text-dark-500">|</span>

          <div className="group/endpoint relative flex items-center gap-1.5">
            <div className="surface-panel pointer-events-none absolute bottom-full left-1/2 z-20 mb-2 w-max max-w-[24rem] -translate-x-1/2 translate-y-1 rounded-xl border border-slate-200 px-3 py-2.5 text-left opacity-0 shadow-[0_14px_36px_-20px_rgba(15,23,42,0.35)] ring-1 ring-slate-200/80 transition-all duration-150 group-hover/endpoint:translate-y-0 group-hover/endpoint:opacity-100 group-focus-within/endpoint:translate-y-0 group-focus-within/endpoint:opacity-100 dark:border-slate-700 dark:ring-slate-700/70">
              {item.description && (
                <p className="max-w-[24rem] break-words text-xs leading-5 text-slate-600 dark:text-slate-200">
                  {item.description}
                </p>
              )}
              <p
                className={`flex items-center gap-1.5 text-[11px] leading-4 text-primary-600 dark:text-primary-300 ${
                  item.description ? 'mt-1.5' : ''
                }`}
              >
                <span className="h-1.5 w-1.5 rounded-full bg-primary-500 dark:bg-primary-300" />
                {tooltipHint(item.endpoint)}
              </p>
            </div>

            <code
              className="surface-muted cursor-pointer font-mono decoration-gray-400 decoration-dashed underline-offset-2 hover:text-primary-600 hover:underline focus:text-primary-600 focus:underline focus:outline-none dark:decoration-gray-500 dark:hover:text-primary-400 dark:focus:text-primary-400"
              role="button"
              tabIndex={0}
              onClick={() => void copy(item.endpoint)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                  event.preventDefault()
                  void copy(item.endpoint)
                }
              }}
            >
              {item.endpoint}
            </code>

            <button
              type="button"
              className={`surface-control rounded p-0.5 transition-colors ${
                copiedEndpoint === item.endpoint
                  ? 'text-emerald-500 dark:text-emerald-400'
                  : 'surface-muted hover:text-primary-500 dark:hover:text-primary-400'
              }`}
              aria-label={tooltipHint(item.endpoint)}
              onClick={() => void copy(item.endpoint)}
            >
              {copiedEndpoint === item.endpoint ? (
                <svg className="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2.2">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
                </svg>
              ) : (
                <svg className="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
                  />
                </svg>
              )}
            </button>

            <a
              href={speedTestUrl(item.endpoint)}
              target="_blank"
              rel="noopener noreferrer"
              className="surface-muted rounded p-0.5 transition-colors hover:text-amber-500 dark:hover:text-amber-400"
              title={t('keys.endpoints.speedTest')}
            >
              <svg className="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                <path strokeLinecap="round" strokeLinejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
              </svg>
            </a>
          </div>
        </div>
      ))}
    </div>
  )
}

export default EndpointPopover
