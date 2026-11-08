import { useEffect, useId, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Pagination, Select } from '@/components/ui'
import { DateTimePicker } from '@/views/admin/parts/DateTimePicker'
import type { PromptAuditEvent, PromptEventFilters } from '../types'
import { cloneData, emptyEventFilters, SCANNER_CATALOG } from '../viewModel'

export interface EventWorkspaceProps {
  events: PromptAuditEvent[]
  total: number
  page: number
  pageSize: number
  filters: PromptEventFilters
  selectedIds: number[]
  loading: boolean
  error: string
  onFiltersChange: (value: PromptEventFilters) => void
  onSearch: (value: PromptEventFilters) => void
  onSelection: (value: number[]) => void
  onPage: (value: number) => void
  onPageSize: (value: number) => void
  onView: (id: number) => void
  onDelete: (id: number) => void
  onBatchDelete: () => void
  onPreviewDelete: () => void
}

interface FilterInputProps {
  value: string
  label: string
  type?: string
  onValueChange: (value: string) => void
}

function FilterInput({ value, label, type = 'text', onValueChange }: FilterInputProps) {
  return (
    <label className="filter-field text-xs surface-muted">
      <span>{label}</span>
      <input
        value={value}
        type={type}
        className="input mt-1 w-full"
        aria-label={label}
        onChange={(event) => onValueChange(event.target.value)}
      />
    </label>
  )
}

function CopyLine({ label, value, copyLabel }: { label: string; value: string; copyLabel: string }) {
  return (
    <div className="flex max-w-56 items-center gap-1 text-xs">
      <span className="w-16 flex-none surface-muted">{label}</span>
      <span className="min-w-0 flex-1 truncate text-gray-800 dark:text-dark-100">{value || '—'}</span>
      {value && (
        <button
          type="button"
          className="text-primary-600 hover:underline"
          aria-label={`${copyLabel} ${label}`}
          onClick={() => navigator.clipboard?.writeText(value)}
        >
          {copyLabel}
        </button>
      )}
    </div>
  )
}

const DECISIONS = new Set(['pass', 'flag', 'critical'])
const RISK_LEVELS = new Set(['low', 'medium', 'high', 'critical'])

export function EventWorkspace({
  events,
  total,
  page,
  pageSize,
  filters,
  selectedIds,
  loading,
  error,
  onFiltersChange,
  onSearch,
  onSelection,
  onPage,
  onPageSize,
  onView,
  onDelete,
  onBatchDelete,
  onPreviewDelete
}: EventWorkspaceProps) {
  const { t, i18n } = useTranslation()
  const filterFormId = `prompt-event-filters-${useId()}`
  const [localFilters, setLocalFilters] = useState<PromptEventFilters>(() => cloneData(filters))

  useEffect(() => {
    setLocalFilters(cloneData(filters))
  }, [filters])

  const allSelected = events.length > 0 && events.every((event) => selectedIds.includes(event.id))

  const formatDate = (value: string): string =>
    new Intl.DateTimeFormat(i18n.language, { dateStyle: 'short', timeStyle: 'medium' }).format(new Date(value))

  function formatDuration(value?: number | null): string {
    if (value == null) return '—'
    if (value < 1000) return `${value} ms`
    return `${(value / 1000).toFixed(value < 10000 ? 2 : 1)} s`
  }

  function formatMode(mode?: string): string {
    if (!mode) return '—'
    const key = `admin.promptAudit.mode.${mode}`
    const label = t(key)
    return label === key ? mode : label
  }

  function decisionClass(decision: string): string {
    if (decision === 'critical')
      return 'bg-red-100/[0.15] text-red-700 dark:bg-red-950/50 dark:text-red-300'
    if (decision === 'flag') return 'bg-amber-100/[0.15] text-amber-700 dark:bg-amber-950/50 dark:text-amber-300'
    return 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-950/50 dark:text-emerald-300'
  }

  function translateDecision(decision: string): string {
    return DECISIONS.has(decision) ? t(`admin.promptAudit.decisions.${decision}`) : decision
  }

  function translateRiskLevel(riskLevel: string): string {
    return RISK_LEVELS.has(riskLevel) ? t(`admin.promptAudit.riskLevels.${riskLevel}`) : riskLevel
  }

  function translateCategory(category: string): string {
    return SCANNER_CATALOG.some((scanner) => scanner.id === category)
      ? t(`admin.promptAudit.scanners.${category}`)
      : category
  }

  function formatDecisionRisk(decision: string, riskLevel: string): string {
    return `${translateDecision(decision)} · ${translateRiskLevel(riskLevel)}`
  }

  function formatCategories(categories: string[]): string {
    if (!categories.length) return '—'
    return categories.map(translateCategory).join(', ')
  }

  function filtersChanged(override?: Partial<PromptEventFilters>) {
    onFiltersChange(cloneData({ ...localFilters, ...override }))
  }

  function applyFilters(override?: Partial<PromptEventFilters>) {
    const value = cloneData({ ...localFilters, ...override })
    setLocalFilters(value)
    onFiltersChange(value)
    onSearch(value)
  }

  function resetFilters() {
    applyFilters(emptyEventFilters())
  }

  function filterByIP(value: string) {
    applyFilters({ client_ip: value })
  }

  function toggleOne(id: number) {
    const selected = new Set(selectedIds)
    if (selected.has(id)) selected.delete(id)
    else selected.add(id)
    onSelection([...selected])
  }

  function toggleAll() {
    onSelection(allSelected ? [] : events.map((event) => event.id))
  }

  const decisionOptions = useMemo(
    () => [
      { value: '', label: t('common.all') },
      { value: 'pass', label: t('admin.promptAudit.decisions.pass') },
      { value: 'flag', label: t('admin.promptAudit.decisions.flag') },
      { value: 'critical', label: t('admin.promptAudit.decisions.critical') }
    ],
    [t]
  )

  const riskOptions = useMemo(
    () => [
      { value: '', label: t('common.all') },
      { value: 'low', label: t('admin.promptAudit.riskLevels.low') },
      { value: 'medium', label: t('admin.promptAudit.riskLevels.medium') },
      { value: 'high', label: t('admin.promptAudit.riskLevels.high') },
      { value: 'critical', label: t('admin.promptAudit.riskLevels.critical') }
    ],
    [t]
  )

  return (
    <section
      aria-labelledby="prompt-events-title"
      className="event-workspace workspace-body workspace-clip pt-4"
    >
      <div className="event-heading workspace-fixed">
        <div className="min-w-0">
          <h2 id="prompt-events-title" className="text-base font-semibold text-gray-950 dark:text-white">
            {t('admin.promptAudit.events.title')}
          </h2>
          <p className="mt-1 text-sm surface-muted">{t('admin.promptAudit.events.description')}</p>
        </div>
      </div>

      <form
        id={filterFormId}
        className="event-filters workspace-fixed filter-bar"
        onSubmit={(event) => {
          event.preventDefault()
          applyFilters()
        }}
      >
        <label className="filter-field text-xs surface-muted">
          <span>{t('admin.promptAudit.events.decision')}</span>
          <Select
            value={localFilters.decision}
            className="mt-1 w-full"
            ariaLabel={t('admin.promptAudit.events.decision')}
            searchable={false}
            options={decisionOptions}
            onValueChange={(next) => {
              const value = String(next ?? '')
              setLocalFilters((previous) => ({ ...previous, decision: value }))
              filtersChanged({ decision: value })
            }}
          />
        </label>
        <label className="filter-field text-xs surface-muted">
          <span>{t('admin.promptAudit.events.risk')}</span>
          <Select
            value={localFilters.risk_level}
            className="mt-1 w-full"
            ariaLabel={t('admin.promptAudit.events.risk')}
            searchable={false}
            options={riskOptions}
            onValueChange={(next) => {
              const value = String(next ?? '')
              setLocalFilters((previous) => ({ ...previous, risk_level: value }))
              filtersChanged({ risk_level: value })
            }}
          />
        </label>
        <FilterInput
          value={localFilters.endpoint}
          label={t('admin.promptAudit.events.endpoint')}
          onValueChange={(value) => {
            setLocalFilters((previous) => ({ ...previous, endpoint: value }))
            filtersChanged({ endpoint: value })
          }}
        />
        <FilterInput
          value={localFilters.client_ip}
          label={t('admin.promptAudit.events.clientIp')}
          onValueChange={(value) => {
            setLocalFilters((previous) => ({ ...previous, client_ip: value }))
            filtersChanged({ client_ip: value })
          }}
        />
        <FilterInput
          value={localFilters.group_id}
          label={t('admin.promptAudit.events.groupId')}
          type="number"
          onValueChange={(value) => {
            setLocalFilters((previous) => ({ ...previous, group_id: value }))
            filtersChanged({ group_id: value })
          }}
        />
        <FilterInput
          value={localFilters.user_id}
          label={t('admin.promptAudit.events.userId')}
          type="number"
          onValueChange={(value) => {
            setLocalFilters((previous) => ({ ...previous, user_id: value }))
            filtersChanged({ user_id: value })
          }}
        />
        <FilterInput
          value={localFilters.api_key_id}
          label={t('admin.promptAudit.events.apiKeyId')}
          type="number"
          onValueChange={(value) => {
            setLocalFilters((previous) => ({ ...previous, api_key_id: value }))
            filtersChanged({ api_key_id: value })
          }}
        />
        <FilterInput
          value={localFilters.request_id}
          label={t('admin.promptAudit.events.requestId')}
          onValueChange={(value) => {
            setLocalFilters((previous) => ({ ...previous, request_id: value }))
            filtersChanged({ request_id: value })
          }}
        />
        <FilterInput
          value={localFilters.prompt_hash}
          label={t('admin.promptAudit.events.promptHash')}
          onValueChange={(value) => {
            setLocalFilters((previous) => ({ ...previous, prompt_hash: value }))
            filtersChanged({ prompt_hash: value })
          }}
        />
        <FilterInput
          value={localFilters.keyword}
          label={t('admin.promptAudit.events.keyword')}
          onValueChange={(value) => {
            setLocalFilters((previous) => ({ ...previous, keyword: value }))
            filtersChanged({ keyword: value })
          }}
        />
        <label className="filter-field text-xs surface-muted">
          <span>{t('admin.promptAudit.events.startAt')}</span>
          <DateTimePicker
            value={localFilters.start_at}
            type="datetime-local"
            className="input mt-1 w-full"
            ariaLabel={t('admin.promptAudit.events.startAt')}
            onValueChange={(value) => {
              setLocalFilters((previous) => ({ ...previous, start_at: value }))
              filtersChanged({ start_at: value })
            }}
          />
        </label>
        <label className="filter-field text-xs surface-muted">
          <span>{t('admin.promptAudit.events.endAt')}</span>
          <DateTimePicker
            value={localFilters.end_at}
            type="datetime-local"
            className="input mt-1 w-full"
            ariaLabel={t('admin.promptAudit.events.endAt')}
            onValueChange={(value) => {
              setLocalFilters((previous) => ({ ...previous, end_at: value }))
              filtersChanged({ end_at: value })
            }}
          />
        </label>
        <div className="event-actions filter-actions">
          <Button type="submit" form={filterFormId} variant="primary" size="sm" data-test="event-search">
            {t('common.search')}
          </Button>
          <Button type="button" variant="ghost" size="sm" data-test="event-reset" onClick={resetFilters}>
            {t('common.reset')}
          </Button>
          <Button
            type="button"
            variant="secondary"
            size="sm"
            disabled={selectedIds.length === 0}
            onClick={onBatchDelete}
          >
            {t('admin.promptAudit.events.deleteSelected', { count: selectedIds.length })}
          </Button>
          <Button type="button" variant="danger" size="sm" data-test="filter-delete" onClick={onPreviewDelete}>
            {t('admin.promptAudit.events.deleteByFilter')}
          </Button>
        </div>
      </form>

      {error && (
        <div
          role="alert"
          className="workspace-fixed rounded-lg bg-red-50/[0.15] px-4 py-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300"
        >
          {error}
        </div>
      )}

      <div
        className="record-table-scroll workspace-table-viewport overflow-x-auto rounded-xl border border-gray-200 dark:border-dark-700/60"
        data-test="event-table-scroll"
      >
        <table className="record-table min-w-[1420px] w-full text-left text-sm">
          <thead className="surface-inset text-xs uppercase tracking-wide surface-muted">
            <tr>
              <th className="w-10 px-3 py-3">
                <input
                  type="checkbox"
                  checked={allSelected}
                  aria-label={t('admin.promptAudit.events.selectAll')}
                  onChange={toggleAll}
                />
              </th>
              <th className="px-3 py-3 font-medium">{t('admin.promptAudit.events.time')}</th>
              <th className="px-3 py-3 font-medium">{t('admin.promptAudit.events.clientIp')}</th>
              <th className="px-3 py-3 font-medium">{t('admin.promptAudit.events.identity')}</th>
              <th className="px-3 py-3 font-medium">{t('admin.promptAudit.events.group')}</th>
              <th className="px-3 py-3 font-medium">{t('admin.promptAudit.events.route')}</th>
              <th className="px-3 py-3 font-medium">{t('admin.promptAudit.events.result')}</th>
              <th className="px-3 py-3 font-medium">{t('admin.promptAudit.events.durations')}</th>
              <th className="px-3 py-3 font-medium">{t('admin.promptAudit.events.preview')}</th>
              <th className="px-3 py-3 text-right font-medium">{t('admin.promptAudit.common.actions')}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100 dark:divide-dark-700">
            {loading && (
              <tr>
                <td colSpan={10} className="px-4 py-12 text-center surface-muted" aria-busy="true">
                  {t('common.loading')}
                </td>
              </tr>
            )}
            {!loading && events.length === 0 && (
              <tr>
                <td colSpan={10} className="px-4 py-12 text-center surface-muted">
                  {t('admin.promptAudit.events.empty')}
                </td>
              </tr>
            )}
            {!loading &&
              events.map((event) => (
                <tr key={event.id} data-test={`event-${event.id}`} className="align-top hover:surface-hover">
                  <td className="px-3 py-3">
                    <input
                      type="checkbox"
                      checked={selectedIds.includes(event.id)}
                      aria-label={t('admin.promptAudit.events.selectEvent', { id: event.id })}
                      onChange={() => toggleOne(event.id)}
                    />
                  </td>
                  <td className="whitespace-nowrap px-3 py-3 text-xs surface-muted">{formatDate(event.created_at)}</td>
                  <td className="px-3 py-3">
                    {event.snapshot.client_ip ? (
                      <button
                        type="button"
                        className="font-mono text-xs text-primary-600 hover:underline dark:text-primary-300 surface-control"
                        title={t('admin.promptAudit.events.filterByIp', { ip: event.snapshot.client_ip })}
                        aria-label={t('admin.promptAudit.events.filterByIp', { ip: event.snapshot.client_ip })}
                        onClick={() => filterByIP(event.snapshot.client_ip)}
                      >
                        {event.snapshot.client_ip}
                      </button>
                    ) : (
                      <span>—</span>
                    )}
                  </td>
                  <td className="px-3 py-3">
                    <CopyLine label={t('admin.promptAudit.events.user')} value={event.snapshot.username} copyLabel={t('common.copy')} />
                    <CopyLine
                      label={t('admin.promptAudit.events.email')}
                      value={event.snapshot.user_email}
                      copyLabel={t('common.copy')}
                    />
                    <CopyLine
                      label={t('admin.promptAudit.events.apiKey')}
                      value={event.snapshot.api_key_name}
                      copyLabel={t('common.copy')}
                    />
                  </td>
                  <td className="px-3 py-3 surface-muted">{event.snapshot.group_name || '—'}</td>
                  <td className="px-3 py-3">
                    <p className="font-medium text-gray-900 dark:text-white">{event.snapshot.endpoint}</p>
                    <p className="mt-1 text-xs surface-muted">
                      {event.snapshot.model} · {event.snapshot.protocol} · {event.snapshot.stage || 'http'}
                    </p>
                  </td>
                  <td className="px-3 py-3">
                    <span
                      className={`rounded-full px-2 py-0.5 text-xs font-medium ${decisionClass(event.decision)}`}
                    >
                      {formatDecisionRisk(event.decision, event.risk_level)}
                    </span>
                    <p
                      className="mt-2 max-w-48 truncate text-xs surface-muted"
                      title={formatCategories(event.categories)}
                    >
                      {formatCategories(event.categories)}
                    </p>
                  </td>
                  <td className="whitespace-nowrap px-3 py-3 text-xs surface-muted">
                    <p>
                      {t('admin.promptAudit.events.queueDelay')} · {formatDuration(event.queue_delay_ms)}
                    </p>
                    <p className="mt-1">
                      {t('admin.promptAudit.events.auditLatency')} · {formatDuration(event.latency_ms)}
                    </p>
                    <p className="mt-1 surface-muted">{formatMode(event.execution_mode)}</p>
                  </td>
                  <td className="max-w-xs px-3 py-3">
                    <p className="line-clamp-2 break-words surface-muted">{event.snapshot.redacted_preview || '—'}</p>
                  </td>
                  <td className="whitespace-nowrap px-3 py-3 text-right">
                    <Button type="button" variant="ghost" size="sm" onClick={() => onView(event.id)}>
                      {t('common.view')}
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="text-red-600"
                      onClick={() => onDelete(event.id)}
                    >
                      {t('common.delete')}
                    </Button>
                  </td>
                </tr>
              ))}
          </tbody>
        </table>
      </div>

      <Pagination
        className="event-pagination workspace-fixed"
        total={total}
        page={page}
        pageSize={pageSize}
        onPageChange={onPage}
        onPageSizeChange={onPageSize}
      />
    </section>
  )
}

export default EventWorkspace
