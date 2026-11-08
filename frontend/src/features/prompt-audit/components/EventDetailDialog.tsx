import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Modal } from '@/components/ui'
import type { PromptAuditEvent, PromptIssueSummary } from '../types'
import { SCANNER_CATALOG } from '../viewModel'

export interface EventDetailDialogProps {
  show: boolean
  event: PromptAuditEvent | null
  loading: boolean
  onClose: () => void
}

const TABS = ['summary', 'risks', 'technical'] as const
type EventDetailTab = (typeof TABS)[number]

const DECISIONS = new Set(['pass', 'flag', 'critical'])
const ACTIONS = new Set(['Allow', 'Warn', 'Block'])
const RISK_LEVELS = new Set(['low', 'medium', 'high', 'critical'])

function tabId(tab: EventDetailTab): string {
  return `prompt-audit-event-tab-${tab}`
}

function panelId(tab: EventDetailTab): string {
  return `prompt-audit-event-panel-${tab}`
}

export function EventDetailDialog({ show, event, loading, onClose }: EventDetailDialogProps) {
  const { t } = useTranslation()
  const [activeTab, setActiveTab] = useState<EventDetailTab>('summary')
  const activeEventId = event?.id

  useEffect(() => {
    setActiveTab('summary')
  }, [activeEventId])

  function moveTab(offset: number) {
    const current = TABS.indexOf(activeTab)
    const next = TABS[(current + offset + TABS.length) % TABS.length]!
    setActiveTab(next)
    requestAnimationFrame(() => document.getElementById(tabId(next))?.focus())
  }

  function displayPrompt(value: PromptAuditEvent): string {
    return value.snapshot.full_prompt || value.snapshot.redacted_preview || '—'
  }

  function formatDecisionAction(decision: string, action: string): string {
    const decisionLabel = DECISIONS.has(decision) ? t(`admin.promptAudit.decisions.${decision}`) : decision
    const actionLabel = ACTIONS.has(action) ? t(`admin.promptAudit.actions.${action}`) : action
    return `${decisionLabel} · ${actionLabel}`
  }

  function translateCategory(category: string): string {
    return SCANNER_CATALOG.some((scanner) => scanner.id === category)
      ? t(`admin.promptAudit.scanners.${category}`)
      : category
  }

  function formatCategories(categories: string[]): string {
    if (!categories.length) return '—'
    return categories.map(translateCategory).join(', ')
  }

  function formatMode(mode: string): string {
    const key = `admin.promptAudit.mode.${mode}`
    const label = t(key)
    return label === key ? mode || '—' : label
  }

  function formatDuration(value?: number | null): string {
    if (value == null) return '—'
    if (value < 1000) return `${value} ms`
    return `${(value / 1000).toFixed(value < 10000 ? 2 : 1)} s`
  }

  function formatNumber(value: number): string {
    return new Intl.NumberFormat().format(value)
  }

  function translateEvidence(value: string): string {
    const byId = SCANNER_CATALOG.find((scanner) => scanner.id === value)
    if (byId) return t(`admin.promptAudit.scanners.${byId.id}`)
    const byLabel = SCANNER_CATALOG.find((scanner) => scanner.label === value)
    if (byLabel) return t(`admin.promptAudit.scanners.${byLabel.id}`)
    return value
  }

  function formatGuardReturn(value: PromptAuditEvent): string {
    const evidence: Record<string, string> = {}
    for (const [key, item] of Object.entries(value.scanner_evidence || {})) {
      evidence[key] = translateEvidence(item)
    }
    return JSON.stringify(
      {
        decision: DECISIONS.has(value.decision)
          ? t(`admin.promptAudit.decisions.${value.decision}`)
          : value.decision,
        risk_level: RISK_LEVELS.has(value.risk_level)
          ? t(`admin.promptAudit.riskLevels.${value.risk_level}`)
          : value.risk_level,
        action: ACTIONS.has(value.action) ? t(`admin.promptAudit.actions.${value.action}`) : value.action,
        categories: value.categories.map(translateCategory),
        matched_scanners: value.matched_scanners.map(translateCategory),
        scanner_scores: value.scanner_scores,
        scanner_evidence: evidence,
        scanner_backend: value.scanner_backend,
        scanner_version: value.scanner_version,
        guard_endpoint_id: value.guard_endpoint_id,
        chunk_total: value.chunk_total,
        matched_chunk_index: value.matched_chunk_index ?? null,
        input_limit: value.input_limit ?? null,
        queue_delay_ms: value.queue_delay_ms ?? null,
        latency_ms: value.latency_ms
      },
      null,
      2
    )
  }

  function issueTitle(issue: PromptIssueSummary): string {
    return translateCategory(issue.category || issue.scanner_id) || issue.title
  }

  function issueDescription(issue: PromptIssueSummary): string {
    const category = issue.category || issue.scanner_id
    const key = `admin.promptAudit.scannerDescriptions.${category}`
    const label = t(key)
    return label === key ? issue.description : label
  }

  function issueSeverity(issue: PromptIssueSummary): string {
    return RISK_LEVELS.has(issue.severity)
      ? t(`admin.promptAudit.riskLevels.${issue.severity}`)
      : issue.severity_label || issue.severity
  }

  function issueAction(issue: PromptIssueSummary): string {
    return ACTIONS.has(issue.action) ? t(`admin.promptAudit.actions.${issue.action}`) : issue.action_label || issue.action
  }

  return (
    <Modal
      open={show}
      onClose={onClose}
      title={t('admin.promptAudit.events.detailTitle')}
      size="full"
    >
      {loading && (
        <div className="py-12 text-center text-sm surface-muted" aria-busy="true">
          {t('common.loading')}
        </div>
      )}

      {!loading && event && (
        <div className="flex flex-col">
          <div className="pb-3">
            <div className="card-tabs" role="tablist">
              {TABS.map((tab) => (
                <button
                  key={tab}
                  id={tabId(tab)}
                  type="button"
                  role="tab"
                  aria-selected={activeTab === tab}
                  aria-controls={panelId(tab)}
                  tabIndex={activeTab === tab ? 0 : -1}
                  className="card-tab"
                  onClick={() => setActiveTab(tab)}
                  onKeyDown={(keyEvent) => {
                    if (keyEvent.key === 'ArrowLeft') {
                      keyEvent.preventDefault()
                      moveTab(-1)
                    } else if (keyEvent.key === 'ArrowRight') {
                      keyEvent.preventDefault()
                      moveTab(1)
                    }
                  }}
                >
                  {t(`admin.promptAudit.events.tabs.${tab}`)}
                </button>
              ))}
            </div>
          </div>

          {event.snapshot.full_prompt_truncated && (
            <div
              role="alert"
              className="mt-4 rounded-md border border-amber-200 bg-amber-50/[0.15] px-3 py-2 text-sm text-amber-900 dark:border-amber-900/70 dark:bg-amber-950/30 dark:text-amber-200"
              data-test="prompt-truncated-warning"
            >
              {t('admin.promptAudit.events.promptTruncatedWarning')}
            </div>
          )}

          {/* Fixed panel height so switching tabs does not resize the dialog */}
          <div className="mt-5 h-[min(62vh,36rem)] overflow-y-auto" data-test="event-detail-tab-panel">
            <div
              id={panelId('summary')}
              className="grid gap-5 lg:grid-cols-2"
              style={{ display: activeTab === 'summary' ? undefined : 'none' }}
              role="tabpanel"
              aria-labelledby={tabId('summary')}
            >
              <div>
                <h4 className="text-sm font-medium text-gray-900 dark:text-white">
                  {t('admin.promptAudit.events.promptFull')}
                </h4>
                <pre
                  className="mt-2 max-h-[min(46vh,26rem)] overflow-auto whitespace-pre-wrap break-words rounded-lg surface-inset p-4 text-sm surface-muted"
                  data-test="summary-prompt-full"
                >
                  {displayPrompt(event)}
                </pre>
              </div>
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
                <dt className="text-gray-500">{t('admin.promptAudit.events.decision')}</dt>
                <dd className="font-medium text-gray-900 dark:text-white">
                  {formatDecisionAction(event.decision, event.action)}
                </dd>
                <dt className="text-gray-500">{t('admin.promptAudit.events.user')}</dt>
                <dd>{event.snapshot.username || '—'}</dd>
                <dt className="text-gray-500">{t('admin.promptAudit.events.email')}</dt>
                <dd>{event.snapshot.user_email || '—'}</dd>
                <dt className="text-gray-500">{t('admin.promptAudit.events.apiKey')}</dt>
                <dd>{event.snapshot.api_key_name || '—'}</dd>
                <dt className="text-gray-500">{t('admin.promptAudit.events.clientIp')}</dt>
                <dd className="font-mono">{event.snapshot.client_ip || '—'}</dd>
                <dt className="text-gray-500">{t('admin.promptAudit.events.group')}</dt>
                <dd>{event.snapshot.group_name || '—'}</dd>
                <dt className="text-gray-500">{t('admin.promptAudit.events.model')}</dt>
                <dd>{event.snapshot.model || '—'}</dd>
                <dt className="text-gray-500">{t('admin.promptAudit.events.executionMode')}</dt>
                <dd>{formatMode(event.execution_mode)}</dd>
                <dt className="text-gray-500">{t('admin.promptAudit.events.promptSize')}</dt>
                <dd>
                  {t('admin.promptAudit.events.promptSizeValue', {
                    chars: formatNumber(event.snapshot.prompt_length),
                    messages: formatNumber(event.snapshot.message_count)
                  })}
                </dd>
                <dt className="text-gray-500">{t('admin.promptAudit.events.categories')}</dt>
                <dd>{formatCategories(event.categories)}</dd>
              </dl>
            </div>

            <div
              id={panelId('risks')}
              className="space-y-5"
              style={{ display: activeTab === 'risks' ? undefined : 'none' }}
              role="tabpanel"
              aria-labelledby={tabId('risks')}
            >
              <div className="grid gap-4 lg:grid-cols-2">
                <section data-test="risk-prompt-preview">
                  <h4 className="text-sm font-medium text-gray-900 dark:text-white">
                    {t('admin.promptAudit.events.promptFull')}
                  </h4>
                  <p className="mt-1 text-xs surface-muted">{t('admin.promptAudit.events.promptFullHint')}</p>
                  <pre
                    className="mt-2 h-[min(46vh,26rem)] overflow-auto whitespace-pre-wrap break-words rounded-lg surface-inset p-4 text-sm surface-muted"
                    data-test="risk-prompt-full"
                  >
                    {displayPrompt(event)}
                  </pre>
                </section>
                <section data-test="risk-guard-return">
                  <h4 className="text-sm font-medium text-gray-900 dark:text-white">
                    {t('admin.promptAudit.events.guardReturn')}
                  </h4>
                  <p className="mt-1 text-xs surface-muted">{t('admin.promptAudit.events.guardReturnHint')}</p>
                  <pre className="mt-2 h-[min(46vh,26rem)] overflow-auto whitespace-pre-wrap break-words rounded-lg surface-inset p-4 font-mono text-xs surface-muted">
                    {formatGuardReturn(event)}
                  </pre>
                </section>
              </div>

              <div className="space-y-3">
                <h4 className="text-sm font-medium text-gray-900 dark:text-white">
                  {t('admin.promptAudit.events.riskSummaries')}
                </h4>
                {event.issue_summaries.map((issue) => (
                  <article
                    key={`${issue.scanner_id}-${issue.code}`}
                    className="rounded-xl border border-red-400 p-4"
                    data-test="risk-issue"
                  >
                    <div className="flex flex-wrap items-center gap-2">
                      <h5 className="font-medium text-gray-900 dark:text-white">{issueTitle(issue)}</h5>
                      <span className="text-xs text-red-600 dark:text-red-300">
                        {issueSeverity(issue)} · {issueAction(issue)}
                      </span>
                    </div>
                    <p className="mt-1 text-sm surface-muted">{issueDescription(issue)}</p>
                    <dl className="mt-2 grid gap-1 text-xs surface-muted sm:grid-cols-2">
                      <div>
                        <dt className="inline text-gray-400">{t('admin.promptAudit.events.categories')} · </dt>
                        <dd className="inline">{translateCategory(issue.category || issue.scanner_id)}</dd>
                      </div>
                      <div>
                        <dt className="inline text-gray-400">{t('admin.promptAudit.events.score')} · </dt>
                        <dd className="inline">{issue.score}</dd>
                      </div>
                      <div className="sm:col-span-2">
                        <dt className="inline text-gray-400">{t('admin.promptAudit.events.evidence')} · </dt>
                        <dd className="inline break-words">
                          {issue.evidence ? translateEvidence(issue.evidence) : '—'}
                        </dd>
                      </div>
                    </dl>
                  </article>
                ))}
                {event.issue_summaries.length === 0 && (
                  <p className="py-6 text-center text-sm surface-muted">{t('admin.promptAudit.events.noRisks')}</p>
                )}
              </div>
            </div>

            <dl
              id={panelId('technical')}
              className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm"
              style={{ display: activeTab === 'technical' ? undefined : 'none' }}
              role="tabpanel"
              aria-labelledby={tabId('technical')}
            >
              <dt className="text-gray-500">{t('admin.promptAudit.events.requestId')}</dt>
              <dd className="break-all font-mono">{event.snapshot.request_id || '—'}</dd>
              <dt className="text-gray-500">{t('admin.promptAudit.events.promptHash')}</dt>
              <dd className="break-all font-mono">{event.snapshot.prompt_hash}</dd>
              <dt className="text-gray-500">{t('admin.promptAudit.events.technical.scanner')}</dt>
              <dd>
                {event.scanner_backend} · {event.scanner_version}
              </dd>
              <dt className="text-gray-500">{t('admin.promptAudit.events.technical.policy')}</dt>
              <dd>
                {event.policy_id} · v{event.policy_version}
              </dd>
              <dt className="text-gray-500">{t('admin.promptAudit.events.technical.guardEndpoint')}</dt>
              <dd>{event.guard_endpoint_id}</dd>
              <dt className="text-gray-500">{t('admin.promptAudit.events.technical.config')}</dt>
              <dd>v{event.config_version}</dd>
              <dt className="text-gray-500">{t('admin.promptAudit.events.technical.chunks')}</dt>
              <dd>{event.chunk_total}</dd>
              <dt className="text-gray-500">{t('admin.promptAudit.events.technical.matchedChunk')}</dt>
              <dd>{event.matched_chunk_index ?? '—'}</dd>
              <dt className="text-gray-500">{t('admin.promptAudit.events.technical.inputLimit')}</dt>
              <dd>{event.input_limit == null ? '—' : formatNumber(event.input_limit)}</dd>
              <dt className="text-gray-500">{t('admin.promptAudit.events.queueDelay')}</dt>
              <dd>{formatDuration(event.queue_delay_ms)}</dd>
              <dt className="text-gray-500">{t('admin.promptAudit.events.technical.latency')}</dt>
              <dd>{formatDuration(event.latency_ms)}</dd>
              <dt className="text-gray-500">{t('admin.promptAudit.events.executionMode')}</dt>
              <dd>{formatMode(event.execution_mode)}</dd>
              <dt className="text-gray-500">{t('admin.promptAudit.events.clientIp')}</dt>
              <dd className="font-mono">{event.snapshot.client_ip || '—'}</dd>
              <dt className="text-gray-500">{t('admin.promptAudit.events.promptSize')}</dt>
              <dd>
                {t('admin.promptAudit.events.promptSizeValue', {
                  chars: formatNumber(event.snapshot.prompt_length),
                  messages: formatNumber(event.snapshot.message_count)
                })}
              </dd>
              <dt className="text-gray-500">{t('admin.promptAudit.events.contentStatus')}</dt>
              <dd>
                {event.snapshot.full_prompt_truncated
                  ? t('admin.promptAudit.events.contentTruncated')
                  : t('admin.promptAudit.events.contentComplete')}
              </dd>
              <dt className="text-gray-500">{t('admin.promptAudit.events.stage')}</dt>
              <dd>{event.snapshot.stage || 'http'}</dd>
              <dt className="text-gray-500">{t('admin.promptAudit.events.technical.protocol')}</dt>
              <dd>
                {event.snapshot.protocol} · {event.snapshot.endpoint}
              </dd>
            </dl>
          </div>
        </div>
      )}
    </Modal>
  )
}

export default EventDetailDialog
