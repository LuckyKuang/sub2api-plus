import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui'
import type { PromptAuditRuntime } from '../types'

export interface RuntimeOverviewProps {
  runtime: PromptAuditRuntime | null
  loading: boolean
  error: string
  onRefresh: () => void
}

function dependencyLabel(status: string, t: (key: string) => string): string {
  if (status === 'ok') return t('admin.promptAudit.runtime.dependencyStatus.ok')
  if (status === 'error') return t('admin.promptAudit.runtime.dependencyStatus.error')
  return status
}

function statusDot(status: string): string {
  if (status === 'running') return 'bg-emerald-500'
  if (status === 'disabled') return 'bg-gray-400'
  if (status === 'degraded') return 'bg-amber-500'
  return 'bg-red-500'
}

export function RuntimeOverview({ runtime, loading, error, onRefresh }: RuntimeOverviewProps) {
  const { t, i18n } = useTranslation()

  const formatDate = (value: string): string =>
    new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(value))

  const statusItems = useMemo(() => {
    if (!runtime) return []
    return [
      {
        label: t('admin.promptAudit.runtime.process'),
        value: t(`admin.promptAudit.status.${runtime.process_status}`),
        dot: statusDot(runtime.process_status)
      },
      { label: t('admin.promptAudit.runtime.mode'), value: t(`admin.promptAudit.mode.${runtime.effective_mode}`) },
      {
        label: t('admin.promptAudit.runtime.version'),
        value: `${runtime.active_config_version} / ${runtime.expected_config_version}`
      },
      { label: t('admin.promptAudit.runtime.workers'), value: `${runtime.worker_active} / ${runtime.worker_total}` },
      { label: t('admin.promptAudit.runtime.queue'), value: `${runtime.queue.active} / ${runtime.queue_capacity}` },
      {
        label: t('admin.promptAudit.runtime.dependencies'),
        value: t('admin.promptAudit.runtime.dependencySummary', {
          database: dependencyLabel(runtime.database_status, t),
          redis: dependencyLabel(runtime.redis_status, t)
        })
      }
    ]
  }, [runtime, t])

  const guardMetricItems = useMemo(() => {
    const metrics = runtime?.guard_metrics
    if (!metrics) return []
    return [
      { label: t('admin.promptAudit.metrics.total'), value: metrics.total },
      { label: t('admin.promptAudit.metrics.allowed'), value: metrics.allowed },
      { label: t('admin.promptAudit.metrics.flagged'), value: metrics.flagged },
      { label: t('admin.promptAudit.metrics.blocked'), value: metrics.blocked },
      { label: t('admin.promptAudit.metrics.unavailable'), value: metrics.unavailable },
      { label: t('admin.promptAudit.metrics.timeouts'), value: metrics.timeouts },
      { label: t('admin.promptAudit.metrics.failovers'), value: metrics.failovers },
      { label: 'P95', value: metrics.latency_p95_ms != null ? `${metrics.latency_p95_ms} ms` : '—' }
    ]
  }, [runtime, t])

  const extractionMetricItems = useMemo(() => {
    if (!runtime) return []
    return [
      {
        key: 'attempted',
        label: t('admin.promptAudit.metrics.extractionAttempted'),
        value: runtime.extraction_attempted,
        valueClass: 'text-gray-900 dark:text-white'
      },
      {
        key: 'succeeded',
        label: t('admin.promptAudit.metrics.extractionSucceeded'),
        value: runtime.extraction_succeeded,
        valueClass: 'text-emerald-700 dark:text-emerald-300'
      },
      {
        key: 'empty',
        label: t('admin.promptAudit.metrics.extractionEmpty'),
        value: runtime.extraction_empty,
        valueClass: 'text-gray-900 dark:text-white'
      },
      {
        key: 'failed',
        label: t('admin.promptAudit.metrics.extractionFailed'),
        value: runtime.extraction_failed,
        valueClass: runtime.extraction_failed > 0 ? 'text-red-700 dark:text-red-300' : 'text-gray-900 dark:text-white'
      }
    ]
  }, [runtime, t])

  return (
    <section
      aria-labelledby="prompt-runtime-title"
      className="border-b border-gray-200 py-6 dark:border-dark-700/60"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 id="prompt-runtime-title" className="text-base font-semibold text-gray-950 dark:text-white">
            {t('admin.promptAudit.runtime.title')}
          </h2>
          <p className="mt-1 text-sm surface-muted">{t('admin.promptAudit.runtime.description')}</p>
        </div>
        <Button variant="secondary" size="sm" disabled={loading} onClick={onRefresh}>
          {t('admin.promptAudit.actions.refresh')}
        </Button>
      </div>

      {error && (
        <div
          role="alert"
          className="mt-5 rounded-lg bg-red-50/[0.15] px-4 py-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300"
        >
          {error}
        </div>
      )}

      {!error && loading && !runtime && (
        <div className="mt-5 grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6" aria-busy="true">
          {Array.from({ length: 6 }).map((_, index) => (
            <div key={index} className="h-16 animate-pulse rounded-xl bg-gray-100 dark:bg-dark-800" />
          ))}
        </div>
      )}

      {!error && runtime && (
        <>
          <dl className="mt-5 grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
            {statusItems.map((item) => (
              <div
                key={item.label}
                className="rounded-xl border border-gray-100 surface-inset px-3 py-3 dark:border-dark-700/60"
              >
                <dt className="text-xs surface-muted">{item.label}</dt>
                <dd className="mt-1.5 flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white">
                  {item.dot && (
                    <span className={`console-data-mark h-2 w-2 shrink-0 rounded-full ${item.dot}`} />
                  )}
                  <span className="min-w-0 truncate">{item.value}</span>
                </dd>
              </div>
            ))}
          </dl>

          <div className="mt-4 grid gap-3 lg:grid-cols-[minmax(0,1.4fr)_minmax(220px,0.6fr)]">
            <div className="rounded-xl border border-gray-100 px-4 py-3 dark:border-dark-700/60 dark:surface-inset">
              <h3 className="text-sm font-medium text-gray-900 dark:text-white">
                {t('admin.promptAudit.runtime.guardMetrics')}
              </h3>
              <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
                {guardMetricItems.map((metric) => (
                  <div key={metric.label} className="rounded-lg surface-inset px-2.5 py-2">
                    <p className="text-[11px] surface-muted">{metric.label}</p>
                    <p className="mt-0.5 text-sm font-semibold tabular-nums text-gray-900 dark:text-white">
                      {metric.value}
                    </p>
                  </div>
                ))}
              </div>
              <div data-test="extraction-metrics" className="mt-4 border-t border-gray-100 pt-3 dark:border-dark-700/60">
                <h4 className="text-xs font-medium surface-muted">
                  {t('admin.promptAudit.runtime.extractionMetrics')}
                </h4>
                <dl className="mt-2 grid grid-cols-2 gap-x-4 gap-y-2 sm:grid-cols-4">
                  {extractionMetricItems.map((metric) => (
                    <div key={metric.key} data-test={`extraction-metric-${metric.key}`}>
                      <dt className="text-[11px] surface-muted">{metric.label}</dt>
                      <dd className={`mt-0.5 text-sm font-semibold tabular-nums ${metric.valueClass}`}>
                        {metric.value}
                      </dd>
                    </div>
                  ))}
                </dl>
              </div>
              <p className="mt-3 text-xs leading-5 surface-muted">
                {t('admin.promptAudit.runtime.queueBreakdown', {
                  queued: runtime.queue.queued,
                  processing: runtime.queue.processing,
                  retry: runtime.queue.retry,
                  done: runtime.queue.done,
                  failed: runtime.queue.failed
                })}
                <span className="mx-1.5 text-gray-300 dark:text-dark-600">·</span>
                {t('admin.promptAudit.runtime.deliveryTotals', {
                  enqueued: runtime.enqueued_total,
                  dropped: runtime.dropped_total,
                  processed: runtime.processed_total,
                  failed: runtime.failed_total
                })}
              </p>
            </div>
            <div className="rounded-xl border border-gray-100 px-4 py-3 dark:border-dark-700/60 dark:surface-inset">
              <h3 className="text-sm font-medium text-gray-900 dark:text-white">
                {t('admin.promptAudit.runtime.latest')}
              </h3>
              <dl className="mt-3 space-y-3 text-sm">
                <div>
                  <dt className="text-xs surface-muted">{t('admin.promptAudit.runtime.lastProcessed')}</dt>
                  <dd className="mt-1 surface-muted">
                    {runtime.last_processed_at ? formatDate(runtime.last_processed_at) : t('admin.promptAudit.common.never')}
                  </dd>
                </div>
                <div>
                  <dt className="text-xs surface-muted">{t('admin.promptAudit.runtime.lastError')}</dt>
                  {runtime.last_error_code ? (
                    <dd className="mt-1 break-words text-red-600 dark:text-red-300">
                      {runtime.last_error_at && (
                        <span className="block text-xs surface-muted">{formatDate(runtime.last_error_at)}</span>
                      )}
                      {runtime.last_error_code}
                      {runtime.last_error_message && <span> · {runtime.last_error_message}</span>}
                    </dd>
                  ) : (
                    <dd className="mt-1 surface-muted">{t('admin.promptAudit.common.never')}</dd>
                  )}
                </div>
              </dl>
              {Object.keys(runtime.endpoints).length > 0 && (
                <div className="mt-3 flex flex-wrap gap-2">
                  {Object.entries(runtime.endpoints).map(([id, probe]) => (
                    <span
                      key={id}
                      className={`rounded-md px-2 py-1 text-xs ${
                        probe.ok
                          ? 'bg-emerald-50/[0.15] text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300'
                          : 'bg-red-50/[0.15] text-red-700 dark:bg-red-950/40 dark:text-red-300'
                      }`}
                    >
                      {id} · {probe.status} · {probe.latency_ms} ms
                    </span>
                  ))}
                </div>
              )}
            </div>
          </div>
        </>
      )}
    </section>
  )
}

export default RuntimeOverview
