import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { getStatusSnapshot, type ServiceStatus, type StatusPlatform, type StatusRange, type StatusSnapshot } from '@/api/channelMonitorV3'
import { Select } from '@/components/ui'
import { platformLabel, platformTextClass } from '@/utils/platformColors'
import { useAutoRefresh } from './useAutoRefresh'
import { BaseDialog } from './BaseDialog'
import { Icon } from './icons'
import { ProviderIcon } from './monitor/ProviderIcon'

const ranges: StatusRange[] = ['24h', '7d', '30d']
const legend: ServiceStatus[] = ['normal', 'degraded', 'partial', 'outage', 'unknown']

export default function ChannelStatusV3View() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language
  const [snapshot, setSnapshot] = useState<StatusSnapshot | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [tab, setTab] = useState<'platforms' | 'events'>('platforms')
  const [range, setRange] = useState<StatusRange>('24h')
  const [platform, setPlatform] = useState('')
  const [detailPlatform, setDetailPlatform] = useState('')
  const eventsTab = useRef<HTMLButtonElement>(null)

  const controller = useRef<AbortController | undefined>(undefined)

  const platforms = useMemo(
    () => (snapshot?.platforms || []).filter((item) => !platform || item.platform === platform),
    [snapshot, platform]
  )
  const incidents = useMemo(
    () => (snapshot?.incidents || []).filter((item) => !platform || item.platform === platform),
    [snapshot, platform]
  )
  const detail = useMemo(
    () => snapshot?.platforms.find((item) => item.platform === detailPlatform),
    [snapshot, detailPlatform]
  )

  // Use the documented joint-observation rules for each model, without exposing
  // the internal request-group or probe-monitor scopes in the user interface.
  function modelStatus(observations: StatusPlatform['models']): ServiceStatus {
    const statuses = observations.map((item) => item.status)
    const known = statuses.filter((status) => status !== 'unknown' && status !== 'insufficient')
    if (!known.length) return statuses.includes('insufficient') ? 'insufficient' : 'unknown'
    if (known.every((status) => status === 'outage')) return 'outage'
    if (known.some((status) => status === 'outage' || status === 'partial')) return 'partial'
    if (known.includes('degraded')) return 'degraded'
    return known.includes('recovering') ? 'recovering' : 'normal'
  }

  function observationReason(observations: StatusPlatform['models']): string {
    if (observations.some((item) => item.observation_reason === 'stale')) return 'stale'
    if (
      observations.some(
        (item) => item.observation_reason === 'observed' || !['unknown', 'insufficient'].includes(item.status)
      )
    ) {
      return 'observed'
    }
    return observations.some((item) => item.status === 'insufficient') ? 'insufficient' : 'no_observations'
  }

  const detailModels = useMemo(() => {
    const models = new Map<string, StatusPlatform['models']>()
    for (const observation of detail?.models || []) {
      const observations = models.get(observation.model) || []
      observations.push(observation)
      models.set(observation.model, observations)
    }
    return Array.from(models, ([model, observations]) => ({
      model,
      status: modelStatus(observations),
      lastObserved: observations
        .map((item) => item.last_observed_at)
        .filter((value): value is string => !!value)
        .sort((a, b) => Date.parse(a) - Date.parse(b))
        .at(-1),
      reason: observationReason(observations),
      phases: [
        ...new Set(
          observations
            .map((item) => item.incident_phase)
            .filter((value): value is NonNullable<typeof value> => !!value)
        )
      ],
      sources: (['request', 'probe'] as const).filter((source) =>
        observations.some((item) => (item.source_kind || 'request') === source)
      )
    })).sort((a, b) => a.model.localeCompare(b.model))
  }, [detail])

  const summary = useMemo(() => {
    const original = snapshot?.summary
    if (!platform || !original) return original!
    const status = platforms[0]?.status || 'unknown'
    return {
      status,
      normal: status === 'normal' ? 1 : 0,
      affected: ['partial', 'outage', 'degraded'].includes(status) ? 1 : 0,
      unknown: ['unknown', 'insufficient'].includes(status) ? 1 : 0,
      recovering: status === 'recovering' ? 1 : 0,
      active_events: incidents.filter((item) => !item.resolved_at).length
    }
  }, [snapshot, platform, platforms, incidents])

  const statusLabel = (status: ServiceStatus) => t(`channelMonitorV3.status.${status}`)
  const percent = (value: number | null) => (value == null ? '—' : `${(value * 100).toFixed(2)}%`)
  const latency = (value: number | null) => (value == null ? '—' : `≈ ${(value / 1000).toFixed(2)} s`)

  const time = (value?: string | null) =>
    value
      ? new Intl.DateTimeFormat(locale, {
          month: '2-digit',
          day: '2-digit',
          hour: '2-digit',
          minute: '2-digit',
          second: '2-digit'
        }).format(new Date(value))
      : '—'

  const load = useCallback(async () => {
    controller.current?.abort()
    const request = new AbortController()
    controller.current = request
    setLoading(true)
    try {
      const value = await getStatusSnapshot(range, request.signal)
      if (request.signal.aborted) return
      setSnapshot(value)
      if (platform && !value.platforms.some((item) => item.platform === platform)) setPlatform('')
      if (detailPlatform && !value.platforms.some((item) => item.platform === detailPlatform)) setDetailPlatform('')
      setError('')
    } catch {
      if (!request.signal.aborted) {
        setSnapshot(null)
        setError(t('channelMonitorV3.loadFailed'))
      }
    } finally {
      if (controller.current === request) setLoading(false)
    }
  }, [range, platform, detailPlatform, t])

  const showEvents = useCallback(
    async (filter = '') => {
      if (filter) setPlatform(filter)
      setDetailPlatform('')
      setTab('events')
      await Promise.resolve()
      eventsTab.current?.focus()
    },
    []
  )

  const openDetail = useCallback(
    async (name: string) => {
      setDetailPlatform(name)
      await load()
    },
    [load]
  )

  const autoRefresh = useAutoRefresh({
    storageKey: 'service-status-v3-refresh',
    intervals: [15],
    defaultInterval: 15,
    onRefresh: load,
    shouldPause: () => document.hidden || loading
  })

  useEffect(() => {
    void load()
    autoRefresh.setEnabled(true)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Replaces the Vue `watch(range, load)`.
  useEffect(() => {
    void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [range])

  useEffect(() => {
    const onVisibilityChange = () => {
      if (!document.hidden) void load()
    }
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () => {
      controller.current?.abort()
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="service-status workspace-page w-full">
      <header className="card workspace-fixed status-heading flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 dark:text-white">{t('channelMonitorV3.title')}</h1>
          <p className="mt-1 text-sm surface-muted">{t('channelMonitorV3.subtitle')}</p>
        </div>
        <div className="flex items-center gap-3 text-xs surface-muted">
          <span data-testid="current-through">
            {t('channelMonitorV3.observed')} {time(snapshot?.current_through)}
          </span>
          <button
            className="btn btn-secondary px-3 py-2"
            disabled={loading}
            aria-label={t('common.refresh')}
            onClick={() => void load()}
          >
            <Icon name="refresh" size="sm" className={loading ? 'animate-spin' : ''} />
          </button>
        </div>
      </header>

      {error && (
        <p
          className="rounded-xl border border-red-200 bg-red-50/[0.15] p-4 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300"
          role="alert"
        >
          {error}
        </p>
      )}
      {snapshot ? (
        <>
          <section
            className={`overview workspace-fixed flex gap-4 rounded-xl border p-5 status-${summary.status}`}
            aria-live="polite"
          >
            <span className="status-ink mt-0.5">
              <Icon name={summary.status === 'normal' ? 'check' : 'exclamationTriangle'} size="lg" />
            </span>
            <div className="min-w-0 flex-1">
              <h2 className="text-base font-semibold text-gray-900 dark:text-white">
                {t(
                  snapshot.monitoring_enabled
                    ? `channelMonitorV3.overview.${summary.status}`
                    : 'channelMonitorV3.monitoringDisabled'
                )}
              </h2>
              <p className="mt-1 text-xs leading-relaxed surface-muted">
                {t(
                  snapshot.monitoring_enabled
                    ? `channelMonitorV3.overviewDescription.${summary.status}`
                    : 'channelMonitorV3.monitoringDisabledHint'
                )}
              </p>
              <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs surface-muted">
                <span className="status-ink font-medium">
                  {t('channelMonitorV3.activeEvents', { count: summary.active_events })}
                </span>
                <span>{t('channelMonitorV3.counts', summary)}</span>
                <button
                  className="status-ink surface-control inline-flex items-center gap-1 font-medium"
                  data-testid="view-events"
                  onClick={() => void showEvents()}
                >
                  {t('channelMonitorV3.viewEvents')}
                  <Icon name="chevronRight" size="xs" />
                </button>
              </div>
            </div>
          </section>

          <div className="card workspace-fixed status-toolbar flex flex-wrap items-center justify-between gap-3">
            <div className="tabs" role="tablist" aria-label={t('channelMonitorV3.title')}>
              <button
                id="v3-platform-tab"
                className={`tab ${tab === 'platforms' ? 'tab-active' : ''}`}
                role="tab"
                aria-selected={tab === 'platforms'}
                aria-controls="v3-platform-panel"
                onClick={() => setTab('platforms')}
              >
                {t('channelMonitorV3.platforms')}
              </button>
              <button
                id="v3-events-tab"
                ref={eventsTab}
                className={`tab ${tab === 'events' ? 'tab-active' : ''}`}
                role="tab"
                aria-selected={tab === 'events'}
                aria-controls="v3-events-panel"
                onClick={() => setTab('events')}
              >
                {t('channelMonitorV3.events')} <span className="ml-1 text-xs">{summary.active_events || ''}</span>
              </button>
            </div>
            <div className="filter-bar">
              <div className="filter-field">
                <Select
                  value={platform}
                  triggerClass="text-xs"
                  ariaLabel={t('channelMonitorV3.filter')}
                  searchable={false}
                  options={[
                    { value: '', label: t('channelMonitorV3.allPlatforms') },
                    ...snapshot.platforms.map((item) => ({ value: item.platform, label: platformLabel(item.platform) }))
                  ]}
                  onValueChange={(value) => setPlatform(value === null ? '' : String(value))}
                />
              </div>
              <div className="segmented-control" role="group" aria-label={t('channelMonitorV3.range')}>
                {ranges.map((value) => (
                  <button
                    key={value}
                    type="button"
                    className="segmented-option"
                    aria-pressed={range === value}
                    onClick={() => setRange(value)}
                  >
                    {t(`channelMonitorV3.ranges.${value}`)}
                  </button>
                ))}
              </div>
            </div>
          </div>

          {tab === 'platforms' ? (
            <section
              id="v3-platform-panel"
              className="status-platform-panel"
              role="tabpanel"
              aria-labelledby="v3-platform-tab"
            >
              <div className="status-card-grid grid gap-4 md:grid-cols-2 xl:grid-cols-3" tabIndex={0}>
                {platforms.map((item) => (
                  <button
                    key={item.platform}
                    className="platform-card glass-surface p-5 text-left transition hover:border-gray-400 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:hover:border-dark-500"
                    onClick={() => void openDetail(item.platform)}
                  >
                    <div className="flex items-center justify-between gap-3">
                      <div className="flex items-center gap-2.5">
                        <span className="surface-inset surface-muted flex h-9 w-9 items-center justify-center rounded-lg">
                          <ProviderIcon provider={item.platform} size={20} />
                        </span>
                        <h3 className={`text-base font-semibold ${platformTextClass(item.platform)}`}>
                          {platformLabel(item.platform)}
                        </h3>
                      </div>
                      <span className={`status-badge status-${item.status}`}>{statusLabel(item.status)}</span>
                    </div>
                    <p className="mt-4 min-h-10 text-xs leading-5 surface-muted">
                      {t(`channelMonitorV3.description.${item.status}`)}
                    </p>
                    <div className="mt-4 grid grid-cols-2 gap-3">
                      <div>
                        <p className="text-xs surface-muted">{t('channelMonitorV3.successRate')}</p>
                        <p className="mt-1 font-mono text-xl font-medium text-gray-900 dark:text-white">
                          {percent(item.success_rate)}
                        </p>
                      </div>
                      <div>
                        <p className="text-xs surface-muted">{t('channelMonitorV3.ttft')}</p>
                        <p className="mt-1 font-mono text-xl font-medium text-gray-900 dark:text-white">
                          {latency(item.ttft_p50_ms)}
                        </p>
                      </div>
                    </div>
                    <div className="mt-5 flex h-6 items-end gap-1" aria-label={t('channelMonitorV3.history')}>
                      {item.timeline.map((point) => (
                        <span
                          key={point.at}
                          className={`history-bar flex-1 status-${point.status}`}
                          title={`${time(point.at)} · ${statusLabel(point.status)}`}
                        />
                      ))}
                    </div>
                    <div className="mt-2 flex justify-between text-[10px] surface-muted">
                      <span>{t(`channelMonitorV3.ranges.${range}`)}</span>
                      <span>{t('channelMonitorV3.now')}</span>
                    </div>
                    <div className="mt-4 flex items-center justify-between border-t border-gray-100 pt-3 text-xs surface-muted dark:border-dark-700">
                      <span>
                        {t('channelMonitorV3.lastRequest')} {time(item.last_request_at)}
                      </span>
                      <Icon name="chevronRight" size="sm" />
                    </div>
                  </button>
                ))}
              </div>
              {platforms.length === 0 && (
                <p className="py-12 text-center text-sm surface-muted">
                  {t(snapshot.monitoring_enabled ? 'channelMonitorV3.emptyPlatforms' : 'channelMonitorV3.monitoringDisabled')}
                </p>
              )}
            </section>
          ) : (
            <section id="v3-events-panel" className="status-events-panel" role="tabpanel" aria-labelledby="v3-events-tab">
              <div className="workspace-fixed flex justify-between text-xs surface-muted">
                <span>{t('channelMonitorV3.events')}</span>
                <span>{t('channelMonitorV3.recent30Days')}</span>
              </div>
              <div className="status-event-list workspace-scroll" tabIndex={0}>
                {incidents.map((event) => (
                  <details key={event.id} className="glass-surface" open={!event.resolved_at}>
                    <summary className="cursor-pointer p-5">
                      <span className="font-medium text-gray-900 dark:text-white">
                        {t(`channelMonitorV3.eventTitle.${event.severity}`, { platform: platformLabel(event.platform) })}
                      </span>
                      <span
                        className={`status-badge ml-3 status-${event.resolved_at ? 'normal' : event.severity}`}
                      >
                        {t(`channelMonitorV3.phase.${event.phase}`)}
                      </span>
                      <span className="mt-2 block text-xs surface-muted">
                        {time(event.started_at)} · {t('channelMonitorV3.autoDetected')}
                      </span>
                    </summary>
                    <div className="space-y-4 border-t border-gray-100 px-5 py-4 text-xs dark:border-dark-700">
                      <p className="leading-5 surface-muted">{t(`channelMonitorV3.phaseDescription.${event.phase}`)}</p>
                      <p className="surface-muted">
                        {t(
                          event.source_kind === 'probe'
                            ? 'featureExtensions.probeSource'
                            : 'featureExtensions.requestSource'
                        )}{' '}
                        · {event.model}
                      </p>
                      <ol className="space-y-3 border-l border-gray-200 pl-4 dark:border-dark-600">
                        {event.updates.map((update, i) => (
                          <li key={i}>
                            <span className="font-medium text-gray-800 dark:text-gray-200">
                              {t(`channelMonitorV3.phase.${update.phase}`)}
                            </span>
                            <span className="ml-3 surface-muted">{time(update.at)}</span>
                            <p className="mt-1 surface-muted">{t(`channelMonitorV3.phaseDescription.${update.phase}`)}</p>
                          </li>
                        ))}
                      </ol>
                    </div>
                  </details>
                ))}
                {incidents.length === 0 && (
                  <p className="py-12 text-center text-sm surface-muted">{t('channelMonitorV3.emptyEvents')}</p>
                )}
              </div>
            </section>
          )}
          <footer className="card workspace-fixed status-footer">
            {tab === 'platforms' && (
              <div className="flex flex-wrap gap-4 text-[11px] surface-muted">
                {legend.map((status) => (
                  <span key={status} className="inline-flex items-center gap-1.5">
                    <span className={`legend-dot status-${status}`} />
                    {statusLabel(status)}
                  </span>
                ))}
              </div>
            )}
            <p className="text-xs surface-muted">{t('channelMonitorV3.note')}</p>
          </footer>
        </>
      ) : loading ? (
        <p className="py-12 text-center text-sm surface-muted">{t('common.loading')}</p>
      ) : null}

      <BaseDialog
        show={!!detail}
        title={detail ? platformLabel(detail.platform) : ''}
        width="wide"
        onClose={() => setDetailPlatform('')}
        footer={
          <button type="button" className="btn btn-secondary" onClick={() => void showEvents(detailPlatform)}>
            {t('channelMonitorV3.viewEvents')}
          </button>
        }
      >
        {detail && (
          <>
            <div className="mb-5 flex flex-wrap items-center gap-3 text-xs surface-muted">
              <span className={`status-badge status-${detail.status}`}>{statusLabel(detail.status)}</span>
              {t('channelMonitorV3.lastRequest')} {time(detail.last_request_at)}
            </div>
            <p className="mb-5 text-sm surface-muted">{t(`channelMonitorV3.description.${detail.status}`)}</p>
            <h3 className="mb-3 text-sm font-medium text-gray-900 dark:text-white">{t('channelMonitorV3.modelStatus')}</h3>
            {snapshot?.current_window_start && snapshot?.current_window_end && (
              <p data-testid="current-window" className="mb-3 text-xs surface-muted">
                {t('channelMonitorV3.currentWindow')} {time(snapshot.current_window_start)} –{' '}
                {time(snapshot.current_window_end)}
              </p>
            )}
            <p className="mb-3 text-xs surface-muted">{t('channelMonitorV3.currentNote')}</p>
            <div className="record-table-scroll overflow-x-auto">
              <table className="record-table w-full text-left text-xs">
                <thead className="surface-muted">
                  <tr>
                    <th className="pb-3">{t('channelMonitorV3.model')}</th>
                    <th className="pb-3">{t('channelMonitorV3.recentStatus')}</th>
                    <th className="pb-3">{t('channelMonitorV3.lastRequest')}</th>
                    <th className="pb-3">{t('channelMonitorV3.eventProgress')}</th>
                  </tr>
                </thead>
                <tbody>
                  {detailModels.map((model) => (
                    <tr
                      key={model.model}
                      className="surface-muted border-t border-gray-100 dark:border-dark-700"
                    >
                      <td className="py-3 pr-3">
                        {model.model}
                        <span className="surface-muted mt-1 flex flex-wrap gap-x-2">
                          {model.sources.map((source) => (
                            <span key={source}>
                              {t(
                                source === 'probe'
                                  ? 'featureExtensions.probeSource'
                                  : 'featureExtensions.requestSource'
                              )}
                            </span>
                          ))}
                        </span>
                      </td>
                      <td className="py-3">
                        <span className={`status-badge status-${model.status}`}>{statusLabel(model.status)}</span>
                        {model.reason !== 'observed' && (
                          <span className="mt-1 block text-xs surface-muted">
                            {t(`channelMonitorV3.observation.${model.reason}`)}
                          </span>
                        )}
                      </td>
                      <td className="py-3 text-xs">{time(model.lastObserved)}</td>
                      <td className="py-3 text-xs">
                        {model.phases.length
                          ? model.phases.map((phase) => t(`channelMonitorV3.phase.${phase}`)).join(' · ')
                          : '—'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {detailModels.length === 0 && (
              <p className="py-6 text-sm surface-muted">{t('channelMonitorV3.emptyModels')}</p>
            )}
          </>
        )}
      </BaseDialog>
    </div>
  )
}
