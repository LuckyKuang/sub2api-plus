import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { proxiesAPI } from '@/api/admin/proxies'
import type { Proxy } from '@/types'
import { proxyTestSnapshot } from '@/utils/proxyTestSnapshot'
import { Icon } from './Icon'

interface ProxyTestResult {
  success: boolean
  message: string
  latency_ms?: number
  ip_address?: string
  city?: string
  region?: string
  country?: string
}

export interface ProxySelectorProps {
  value: number | null
  proxies: Proxy[]
  disabled?: boolean
  className?: string
  onValueChange?: (value: number | null) => void
}

export function ProxySelector({
  value,
  proxies,
  disabled = false,
  className,
  onValueChange
}: ProxySelectorProps) {
  const { t } = useTranslation()

  const [isOpen, setIsOpen] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const containerRef = useRef<HTMLDivElement | null>(null)
  const searchInputRef = useRef<HTMLInputElement | null>(null)

  // Test state
  const [testResults, setTestResults] = useState<Record<number, ProxyTestResult>>({})
  const [testingProxyIds, setTestingProxyIds] = useState<Set<number>>(new Set())
  const [batchTesting, setBatchTesting] = useState(false)
  const probeOwners = useRef(new Map<number, object>())
  const proxiesRef = useRef(proxies)
  proxiesRef.current = proxies
  const previousSnapshotsRef = useRef<string[] | null>(null)

  useEffect(() => {
    const current = proxies.map(proxyTestSnapshot)
    const previous = previousSnapshotsRef.current
    previousSnapshotsRef.current = current
    if (!previous) return
    setTestResults((results) => {
      const next: Record<number, ProxyTestResult> = {}
      for (const [id, result] of Object.entries(results)) {
        const index = proxies.findIndex((proxy) => proxy.id === Number(id))
        if (index < 0 || !previous.includes(current[index])) continue
        next[Number(id)] = result
      }
      return next
    })
    for (const [id] of probeOwners.current) {
      const index = proxies.findIndex((proxy) => proxy.id === id)
      if (index < 0 || !previous.includes(current[index])) {
        probeOwners.current.delete(id)
        setTestingProxyIds((ids) => {
          if (!ids.has(id)) return ids
          const next = new Set(ids)
          next.delete(id)
          return next
        })
      }
    }
  }, [proxies])

  const selectedProxy = useMemo(() => {
    if (value === null) return null
    return proxies.find((p) => p.id === value) || null
  }, [proxies, value])

  const selectedLabel = (() => {
    if (!selectedProxy) {
      return t('admin.accounts.noProxy')
    }
    const proxy = selectedProxy
    return `${proxy.name} (${proxy.protocol}://${proxy.host}:${proxy.port})`
  })()

  const filteredProxies = useMemo(() => {
    if (!searchQuery) {
      return proxies
    }
    const query = searchQuery.toLowerCase()
    return proxies.filter((proxy) => {
      const name = proxy.name.toLowerCase()
      const host = proxy.host.toLowerCase()
      return name.includes(query) || host.includes(query)
    })
  }, [proxies, searchQuery])

  useEffect(() => {
    if (disabled) {
      setIsOpen(false)
      setSearchQuery('')
    }
  }, [disabled])

  const toggle = () => {
    if (disabled) return
    setIsOpen((open) => {
      const next = !open
      if (next) {
        requestAnimationFrame(() => searchInputRef.current?.focus())
      }
      return next
    })
  }

  const selectOption = (next: number | null) => {
    onValueChange?.(next)
    setIsOpen(false)
    setSearchQuery('')
  }

  const handleTestProxy = async (proxy: Proxy) => {
    if (testingProxyIds.has(proxy.id)) return

    const snapshot = proxyTestSnapshot(proxy)
    const owner = {}
    probeOwners.current.set(proxy.id, owner)
    const isCurrent = () =>
      probeOwners.current.get(proxy.id) === owner &&
      proxiesRef.current.some(
        (current) => current.id === proxy.id && proxyTestSnapshot(current) === snapshot
      )
    setTestingProxyIds((ids) => new Set(ids).add(proxy.id))
    try {
      const result = await proxiesAPI.testProxy(proxy.id)
      if (!isCurrent()) return
      setTestResults((results) => ({ ...results, [proxy.id]: result }))
    } catch (error: unknown) {
      if (!isCurrent()) return
      const err = error as { response?: { data?: { detail?: string } }; message?: string }
      setTestResults((results) => ({
        ...results,
        [proxy.id]: {
          success: false,
          message: err.response?.data?.detail || 'Test failed'
        }
      }))
    } finally {
      if (probeOwners.current.get(proxy.id) === owner) {
        probeOwners.current.delete(proxy.id)
        setTestingProxyIds((ids) => {
          const next = new Set(ids)
          next.delete(proxy.id)
          return next
        })
      }
    }
  }

  const handleBatchTest = async () => {
    if (batchTesting || proxies.length === 0) return

    setBatchTesting(true)

    // Test all proxies in parallel
    await Promise.all(proxies.map(handleTestProxy))
    setBatchTesting(false)
  }

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setIsOpen(false)
        setSearchQuery('')
      }
    }
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && isOpen) {
        setIsOpen(false)
        setSearchQuery('')
      }
    }
    document.addEventListener('click', handleClickOutside)
    document.addEventListener('keydown', handleEscape)
    return () => {
      document.removeEventListener('click', handleClickOutside)
      document.removeEventListener('keydown', handleEscape)
    }
  }, [isOpen])

  return (
    <div className={cn('proxy-selector relative', className)} ref={containerRef}>
      <button
        type="button"
        onClick={toggle}
        disabled={disabled}
        className={cn('select-trigger', isOpen && 'select-trigger-open', disabled && 'select-trigger-disabled')}
      >
        <span className="select-value">{selectedLabel}</span>
        <span className="select-icon">
          <Icon
            name="chevronDown"
            size="md"
            className={cn('transition-transform duration-200', isOpen && 'rotate-180')}
          />
        </span>
      </button>

      {isOpen && (
        <div className="select-dropdown surface-panel">
          {/* Search and Batch Test Header */}
          <div className="select-header">
            <div className="select-search">
              <Icon name="search" size="sm" className="text-gray-400" />
              <input
                ref={searchInputRef}
                value={searchQuery}
                onChange={(event) => setSearchQuery(event.target.value)}
                type="text"
                placeholder={t('admin.proxies.searchProxies')}
                className="select-search-input"
                onClick={(event) => event.stopPropagation()}
              />
            </div>
            {proxies.length > 0 && (
              <button
                type="button"
                onClick={(event) => {
                  event.stopPropagation()
                  void handleBatchTest()
                }}
                disabled={batchTesting}
                className="batch-test-btn surface-control"
                title={t('admin.proxies.batchTest')}
              >
                {batchTesting ? (
                  <svg className="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24" aria-hidden="true">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                    <path
                      className="opacity-75"
                      fill="currentColor"
                      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                    ></path>
                  </svg>
                ) : (
                  <Icon name="play" size="sm" />
                )}
              </button>
            )}
          </div>

          {/* Options list */}
          <div className="select-options">
            {/* No Proxy option */}
            <div
              onClick={() => selectOption(null)}
              className={cn('select-option', value === null && 'select-option-selected')}
            >
              <span className="select-option-label">{t('admin.accounts.noProxy')}</span>
              {value === null && <Icon name="check" size="sm" className="text-primary-500" />}
            </div>

            {/* Proxy options */}
            {filteredProxies.map((proxy) => (
              <div
                key={proxy.id}
                onClick={() => selectOption(proxy.id)}
                className={cn('select-option', value === proxy.id && 'select-option-selected')}
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium">{proxy.name}</span>
                    {/* Account count badge */}
                    {proxy.account_count !== undefined && (
                      <span className="inline-flex flex-shrink-0 items-center rounded surface-inset px-1.5 py-0.5 text-xs surface-muted">
                        {proxy.account_count}
                      </span>
                    )}
                    {/* Test result badges */}
                    {testResults[proxy.id] && (
                      <>
                        {testResults[proxy.id].success ? (
                          <span className="inline-flex flex-shrink-0 items-center gap-1 rounded bg-emerald-100/[0.15] px-1.5 py-0.5 text-xs text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400">
                            {testResults[proxy.id].country && <span>{testResults[proxy.id].country}</span>}
                            {testResults[proxy.id].latency_ms && (
                              <span>{testResults[proxy.id].latency_ms}ms</span>
                            )}
                          </span>
                        ) : (
                          <span className="inline-flex flex-shrink-0 items-center rounded bg-red-100/[0.15] px-1.5 py-0.5 text-xs text-red-700 dark:bg-red-900/30 dark:text-red-400">
                            {t('admin.proxies.testFailed')}
                          </span>
                        )}
                      </>
                    )}
                  </div>
                  <div className="truncate text-xs surface-muted">
                    {proxy.protocol}://{proxy.host}:{proxy.port}
                  </div>
                </div>

                {/* Individual test button */}
                <button
                  type="button"
                  onClick={(event) => {
                    event.stopPropagation()
                    void handleTestProxy(proxy)
                  }}
                  disabled={testingProxyIds.has(proxy.id)}
                  className="test-btn surface-control"
                  title={t('admin.proxies.testConnection')}
                >
                  {testingProxyIds.has(proxy.id) ? (
                    <svg className="h-3.5 w-3.5 animate-spin" fill="none" viewBox="0 0 24 24" aria-hidden="true">
                      <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                      <path
                        className="opacity-75"
                        fill="currentColor"
                        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                      ></path>
                    </svg>
                  ) : (
                    <Icon name="play" size="xs" />
                  )}
                </button>

                {value === proxy.id && (
                  <Icon name="check" size="sm" className="flex-shrink-0 text-primary-500" />
                )}
              </div>
            ))}

            {/* Empty state */}
            {filteredProxies.length === 0 && searchQuery && (
              <div className="select-empty">{t('common.noOptionsFound')}</div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
