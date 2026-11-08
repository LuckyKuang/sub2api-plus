import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/Icon'
import { DetailContentHost } from '@/components/DetailContentHost'
import { useAppStore } from '@/stores/app'
import { opsAPI, type OpsErrorDetail } from '@/api/admin/ops'
import { formatDateTime } from '@/utils/format'
import { resolveUpstreamPayload } from '@/views/admin/ops/utils/errorDetailResponse'

export interface OpsErrorDetailModalProps {
  embedded?: boolean
  show: boolean
  errorId: number | null
  errorType?: 'request' | 'upstream'
  backToList?: boolean
  onClose?: () => void
  onBack?: () => void
}

type DiagnosticPayloadKey = 'client' | 'upstream_message' | 'upstream_detail' | 'upstream_events'

function meaningfulPayload(candidate: unknown): string {
  const value = String(candidate || '').trim()
  if (!value || value === '[]' || value === '{}' || value.toLowerCase() === 'null') return ''
  return value
}

function prettyJSON(raw?: string): string {
  if (!raw) return 'N/A'
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

function prettyObject(value: unknown): string {
  return JSON.stringify(value, null, 2)
}

function statusBadgeClass(code: number): string {
  if (code >= 500) {
    return 'bg-red-50/[0.15] text-red-700 ring-red-600/20 dark:bg-red-900/30 dark:text-red-400 dark:ring-red-500/30'
  }
  if (code === 429) {
    return 'bg-purple-50/[0.15] text-purple-700 ring-purple-600/20 dark:bg-purple-900/30 dark:text-purple-400 dark:ring-purple-500/30'
  }
  if (code >= 400) {
    return 'bg-amber-50/[0.15] text-amber-700 ring-amber-600/20 dark:bg-amber-900/30 dark:text-amber-400 dark:ring-amber-500/30'
  }
  return 'surface-inset surface-muted ring-gray-600/20 dark:surface-inset dark:ring-gray-500/30'
}

export function OpsErrorDetailModal({
  embedded = false,
  show,
  errorId,
  errorType,
  backToList = false,
  onClose,
  onBack
}: OpsErrorDetailModalProps) {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)

  const [loading, setLoading] = useState(false)
  const [detail, setDetail] = useState<OpsErrorDetail | null>(null)
  const [correlatedUpstream, setCorrelatedUpstream] = useState<OpsErrorDetail[]>([])
  const [correlatedUpstreamLoading, setCorrelatedUpstreamLoading] = useState(false)
  const [expandedUpstreamDetailIds, setExpandedUpstreamDetailIds] = useState<Set<number>>(new Set())
  const detailGeneration = useRef(0)

  const showUpstreamList = errorType === 'request'
  const requestId = detail?.request_id || detail?.client_request_id || ''

  const rootCauseMessage = useMemo(() => {
    const current = detail
    if (!current) return ''
    for (const candidate of [
      current.upstream_error_message,
      current.upstream_error_detail,
      current.message,
      current.error_body
    ]) {
      const value = meaningfulPayload(candidate)
      if (value) return value
    }
    return ''
  }, [detail])

  const diagnosticPayloadSections = useMemo(() => {
    const current = detail
    if (!current) return []
    const candidates: Array<{ key: DiagnosticPayloadKey; value: string }> = [
      { key: 'client', value: meaningfulPayload(current.error_body) },
      { key: 'upstream_message', value: meaningfulPayload(current.upstream_error_message) },
      { key: 'upstream_detail', value: meaningfulPayload(current.upstream_error_detail) },
      { key: 'upstream_events', value: meaningfulPayload(current.upstream_errors) }
    ]
    return candidates.filter(
      (section, index, all) => section.value && all.findIndex((candidate) => candidate.value === section.value) === index
    )
  }, [detail])

  const title = !errorId
    ? t('admin.ops.errorDetail.title')
    : t('admin.ops.errorDetail.titleWithId', { id: String(errorId) })
  const emptyText = t('admin.ops.errorDetail.noErrorSelected')

  const isUpstreamError = (d: OpsErrorDetail | null): boolean => {
    if (!d) return false
    const phase = String(d.phase || '').toLowerCase()
    const owner = String(d.error_owner || '').toLowerCase()
    return phase === 'upstream' && owner === 'provider'
  }

  const formatRequestTypeLabel = (type: number | null | undefined): string => {
    switch (type) {
      case 1:
        return t('admin.ops.errorDetail.requestTypeSync')
      case 2:
        return t('admin.ops.errorDetail.requestTypeStream')
      case 3:
        return t('admin.ops.errorDetail.requestTypeWs')
      default:
        return t('admin.ops.errorDetail.requestTypeUnknown')
    }
  }

  const hasModelMapping = (d: OpsErrorDetail | null): boolean => {
    if (!d) return false
    const requested = String(d.requested_model || '').trim()
    const upstream = String(d.upstream_model || '').trim()
    return !!requested && !!upstream && requested !== upstream
  }

  const displayModel = (d: OpsErrorDetail | null): string => {
    if (!d) return ''
    const upstream = String(d.upstream_model || '').trim()
    if (upstream) return upstream
    const requested = String(d.requested_model || '').trim()
    if (requested) return requested
    return String(d.model || '').trim()
  }

  const getUpstreamResponsePreview = (ev: OpsErrorDetail): string => {
    const upstreamPayload = resolveUpstreamPayload(ev)
    if (upstreamPayload) return upstreamPayload
    return String(ev.error_body || '').trim()
  }

  const toggleUpstreamDetail = (id: number) => {
    setExpandedUpstreamDetailIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  useEffect(() => {
    return () => {
      detailGeneration.current += 1
    }
  }, [])

  useEffect(() => {
    detailGeneration.current += 1
    const seq = detailGeneration.current
    setDetail(null)
    setCorrelatedUpstream([])
    if (!show) return

    if (typeof errorId === 'number' && errorId > 0) {
      setExpandedUpstreamDetailIds(new Set())
      void fetchDetail(seq, errorId)
      if (errorType === 'request') {
        void fetchCorrelatedUpstreamErrors(seq, errorId)
      } else {
        setCorrelatedUpstream([])
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [show, errorId, errorType])

  async function fetchDetail(seq: number, id: number) {
    setLoading(true)
    try {
      const kind = errorType || 'request'
      const d = kind === 'upstream' ? await opsAPI.getUpstreamErrorDetail(id) : await opsAPI.getRequestErrorDetail(id)
      if (seq === detailGeneration.current) setDetail(d)
    } catch (err: unknown) {
      if (seq === detailGeneration.current) {
        setDetail(null)
        showError((err as { message?: string })?.message || t('admin.ops.failedToLoadErrorDetail'))
      }
    } finally {
      if (seq === detailGeneration.current) setLoading(false)
    }
  }

  async function fetchCorrelatedUpstreamErrors(seq: number, requestErrorId: number) {
    setCorrelatedUpstreamLoading(true)
    try {
      const res = await opsAPI.listRequestErrorUpstreamErrors(
        requestErrorId,
        { page: 1, page_size: 100, view: 'all' },
        { include_detail: true }
      )
      if (seq === detailGeneration.current) setCorrelatedUpstream(res.items || [])
    } catch (err) {
      console.error('[OpsErrorDetailModal] Failed to load correlated upstream errors', err)
      if (seq === detailGeneration.current) setCorrelatedUpstream([])
    } finally {
      if (seq === detailGeneration.current) setCorrelatedUpstreamLoading(false)
    }
  }

  const close = () => {
    onClose?.()
  }

  const goBack = () => {
    onClose?.()
    onBack?.()
  }

  return (
    <DetailContentHost
      embedded={embedded}
      show={show}
      title={title}
      onClose={close}
      footer={
        backToList ? (
          <button
            type="button"
            className="btn btn-secondary"
            data-testid="error-detail-back-to-list"
            onClick={goBack}
          >
            {t('admin.ops.errorDetail.backToList')}
          </button>
        ) : undefined
      }
    >
      {loading && (
        <div className="flex items-center justify-center py-16">
          <div className="flex flex-col items-center gap-3">
            <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600" />
            <div className="text-sm font-medium surface-muted">{t('admin.ops.errorDetail.loading')}</div>
          </div>
        </div>
      )}

      {!loading && !detail && <div className="py-10 text-center text-sm surface-muted">{emptyText}</div>}

      {detail && (
        <div className="space-y-6">
          {/* Summary */}
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <div className="rounded-xl surface-inset p-4">
              <div className="text-xs font-bold uppercase tracking-wider surface-muted">
                {t('admin.ops.errorDetail.requestId')}
              </div>
              <div className="mt-1 break-all font-mono text-sm font-medium text-gray-900 dark:text-white">
                {requestId || '—'}
              </div>
            </div>

            {detail.async_task_id && (
              <div className="rounded-xl surface-inset p-4">
                <div className="text-xs font-bold uppercase tracking-wider surface-muted">
                  {t('admin.ops.errorDetail.asyncTaskId')}
                </div>
                <div className="mt-1 break-all font-mono text-sm font-medium text-gray-900 dark:text-white">
                  {detail.async_task_id}
                </div>
              </div>
            )}

            <div className="rounded-xl surface-inset p-4">
              <div className="text-xs font-bold uppercase tracking-wider surface-muted">
                {t('admin.ops.errorDetail.time')}
              </div>
              <div className="mt-1 text-sm font-medium text-gray-900 dark:text-white">
                {formatDateTime(detail.created_at)}
              </div>
            </div>

            <div className="rounded-xl surface-inset p-4">
              <div className="text-xs font-bold uppercase tracking-wider surface-muted">
                {isUpstreamError(detail) ? t('admin.ops.errorDetail.account') : t('admin.ops.errorDetail.user')}
              </div>
              <div className="mt-1 text-sm font-medium text-gray-900 dark:text-white">
                {isUpstreamError(detail)
                  ? detail.account_name || (detail.account_id != null ? String(detail.account_id) : '—')
                  : detail.user_email || (detail.user_id != null ? String(detail.user_id) : '—')}
              </div>
            </div>

            <div className="rounded-xl surface-inset p-4">
              <div className="text-xs font-bold uppercase tracking-wider surface-muted">
                {t('admin.ops.errorDetail.platform')}
              </div>
              <div className="mt-1 text-sm font-medium text-gray-900 dark:text-white">{detail.platform || '—'}</div>
            </div>

            <div className="rounded-xl surface-inset p-4">
              <div className="text-xs font-bold uppercase tracking-wider surface-muted">
                {t('admin.ops.errorDetail.group')}
              </div>
              <div className="mt-1 text-sm font-medium text-gray-900 dark:text-white">
                {detail.group_name || (detail.group_id != null ? String(detail.group_id) : '—')}
              </div>
            </div>

            <div className="rounded-xl surface-inset p-4">
              <div className="text-xs font-bold uppercase tracking-wider surface-muted">
                {t('admin.ops.errorDetail.model')}
              </div>
              <div className="mt-1 text-sm font-medium text-gray-900 dark:text-white">
                {hasModelMapping(detail) ? (
                  <>
                    <span className="font-mono">{detail.requested_model}</span>
                    <span className="mx-1 surface-muted">→</span>
                    <span className="font-mono text-primary-600 dark:text-primary-400">{detail.upstream_model}</span>
                  </>
                ) : (
                  displayModel(detail) || '—'
                )}
              </div>
            </div>

            <div className="rounded-xl surface-inset p-4">
              <div className="text-xs font-bold uppercase tracking-wider surface-muted">
                {t('admin.ops.errorDetail.inboundEndpoint')}
              </div>
              <div className="mt-1 break-all font-mono text-sm font-medium text-gray-900 dark:text-white">
                {detail.inbound_endpoint || '—'}
              </div>
            </div>

            <div className="rounded-xl surface-inset p-4">
              <div className="text-xs font-bold uppercase tracking-wider surface-muted">
                {t('admin.ops.errorDetail.upstreamEndpoint')}
              </div>
              <div className="mt-1 break-all font-mono text-sm font-medium text-gray-900 dark:text-white">
                {detail.upstream_endpoint || '—'}
              </div>
            </div>

            <div className="rounded-xl surface-inset p-4">
              <div className="text-xs font-bold uppercase tracking-wider surface-muted">
                {t('admin.ops.errorDetail.status')}
              </div>
              <div className="mt-1">
                <span
                  className={`inline-flex items-center rounded-lg px-2 py-1 text-xs font-black ring-1 ring-inset shadow-sm ${statusBadgeClass(
                    detail.status_code ?? 0
                  )}`}
                >
                  {detail.status_code}
                </span>
              </div>
            </div>

            <div className="rounded-xl surface-inset p-4">
              <div className="text-xs font-bold uppercase tracking-wider surface-muted">
                {t('admin.ops.errorDetail.upstreamStatus')}
              </div>
              <div className="mt-1">
                <span
                  className={`inline-flex items-center rounded-lg px-2 py-1 text-xs font-black ring-1 ring-inset shadow-sm ${statusBadgeClass(
                    detail.upstream_status_code ?? 0
                  )}`}
                >
                  {detail.upstream_status_code ?? '—'}
                </span>
              </div>
            </div>

            <div className="rounded-xl surface-inset p-4">
              <div className="text-xs font-bold uppercase tracking-wider surface-muted">
                {t('admin.ops.errorDetail.requestType')}
              </div>
              <div className="mt-1 text-sm font-medium text-gray-900 dark:text-white">
                {formatRequestTypeLabel(detail.request_type)}
              </div>
            </div>

            <div className="rounded-xl surface-inset p-4">
              <div className="text-xs font-bold uppercase tracking-wider surface-muted">
                {t('admin.ops.errorDetail.message')}
              </div>
              <div className="mt-1 break-words text-sm font-medium text-gray-900 dark:text-white" title={rootCauseMessage}>
                {rootCauseMessage || '—'}
              </div>
            </div>

            {detail.api_key_prefix && (
              <div className="rounded-xl surface-inset p-4">
                <div className="text-xs font-bold uppercase tracking-wider surface-muted">
                  {t('admin.ops.errorDetail.apiKeyPrefix')}
                </div>
                <div className="mt-1 font-mono text-sm font-medium text-gray-900 dark:text-white">
                  {detail.api_key_prefix}
                </div>
              </div>
            )}
          </div>

          {rootCauseMessage && (
            <div className="rounded-xl bg-amber-50/[0.15] p-6 dark:bg-amber-900/10">
              <h3 className="text-sm font-black uppercase tracking-wider text-amber-900 dark:text-amber-200">
                {t('admin.ops.errorDetail.rootCause')}
              </h3>
              <div className="mt-3 break-words text-sm font-medium text-amber-900 dark:text-amber-100">
                {rootCauseMessage}
              </div>
            </div>
          )}

          <div className="rounded-xl surface-inset p-6">
            <h3 className="text-sm font-black uppercase tracking-wider text-gray-900 dark:text-white">
              {t('admin.ops.errorDetail.diagnosticPayloads')}
            </h3>
            {diagnosticPayloadSections.length === 0 && (
              <div className="mt-4 text-sm surface-muted">{t('common.noData')}</div>
            )}
            {diagnosticPayloadSections.length > 0 && (
              <div className="mt-4 space-y-4">
                {diagnosticPayloadSections.map((section) => (
                  <div key={section.key}>
                    <div className="mb-2 text-xs font-bold uppercase tracking-wider surface-muted">
                      {t(`admin.ops.errorDetail.payloads.${section.key}`)}
                    </div>
                    <pre className="max-h-[520px] overflow-auto rounded-xl border border-gray-200 surface-inset p-4 text-xs text-gray-800 dark:border-dark-700 dark:text-gray-100">
                      <code>{prettyJSON(section.value)}</code>
                    </pre>
                  </div>
                ))}
              </div>
            )}
          </div>

          {/* Structured diagnostic vocabulary only; the backend rejects raw URLs,
              request headers, credentials, and arbitrary user-provided strings. */}
          {detail.routing_diagnostics && (
            <div className="rounded-xl surface-inset p-6">
              <h3 className="text-sm font-black uppercase tracking-wider text-gray-900 dark:text-white">
                {t('admin.ops.errorDetail.routingDiagnostics')}
              </h3>
              {detail.routing_diagnostics.selection_decision && (
                <dl className="mt-4 grid grid-cols-2 gap-2 text-sm">
                  <dt className="text-gray-500">{t('admin.ops.errorDetail.candidatePool')}</dt>
                  <dd data-testid="candidate-pool">
                    {detail.routing_diagnostics.candidate_pool ?? t('admin.ops.errorDetail.unknownDiagnostic')}
                  </dd>
                  <dt className="text-gray-500">{t('admin.ops.errorDetail.selectionReason')}</dt>
                  <dd data-testid="selection-reason">
                    {detail.routing_diagnostics.selection_reason || t('admin.ops.errorDetail.unknownDiagnostic')}
                  </dd>
                </dl>
              )}
              <pre className="mt-4 max-h-[360px] overflow-auto rounded-xl border border-gray-200 surface-inset p-4 text-xs text-gray-800 dark:border-dark-700 dark:text-gray-100">
                <code>{prettyObject(detail.routing_diagnostics)}</code>
              </pre>
            </div>
          )}

          {/* Upstream errors list (only for request errors) */}
          {showUpstreamList && (
            <div className="rounded-xl surface-inset p-6">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 className="text-sm font-black uppercase tracking-wider text-gray-900 dark:text-white">
                  {t('admin.ops.errorDetails.upstreamErrors')}
                </h3>
                {correlatedUpstreamLoading && <div className="text-xs surface-muted">{t('common.loading')}</div>}
              </div>

              {!correlatedUpstreamLoading && correlatedUpstream.length === 0 && (
                <div className="mt-3 text-sm surface-muted">{t('common.noData')}</div>
              )}

              {correlatedUpstream.length > 0 && (
                <div className="mt-4 space-y-3">
                  {correlatedUpstream.map((ev, idx) => (
                    <div
                      key={ev.id}
                      className="rounded-xl border border-gray-200 surface-inset p-4 dark:border-dark-700"
                    >
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <div className="text-xs font-black text-gray-900 dark:text-white">
                          #{idx + 1}
                          {ev.type && (
                            <span className="ml-2 rounded-md surface-inset px-2 py-0.5 font-mono text-[10px] font-bold surface-muted">
                              {ev.type}
                            </span>
                          )}
                        </div>
                        <div className="flex items-center gap-2">
                          <div className="font-mono text-xs surface-muted">{ev.status_code ?? '—'}</div>
                          <button
                            type="button"
                            className="inline-flex items-center gap-1.5 rounded-md px-1.5 py-1 text-[10px] font-bold text-primary-700 hover:bg-primary-50/25 disabled:cursor-not-allowed disabled:opacity-60 dark:text-primary-200 dark:hover:surface-hover surface-control"
                            disabled={!getUpstreamResponsePreview(ev)}
                            title={getUpstreamResponsePreview(ev) ? '' : t('common.noData')}
                            onClick={() => toggleUpstreamDetail(ev.id)}
                          >
                            <Icon
                              name={expandedUpstreamDetailIds.has(ev.id) ? 'chevronDown' : 'chevronRight'}
                              size="xs"
                              strokeWidth={2}
                            />
                            <span>
                              {expandedUpstreamDetailIds.has(ev.id)
                                ? t('admin.ops.errorDetail.responsePreview.collapse')
                                : t('admin.ops.errorDetail.responsePreview.expand')}
                            </span>
                          </button>
                        </div>
                      </div>

                      <div className="mt-3 grid grid-cols-1 gap-2 text-xs sm:grid-cols-2 surface-muted">
                        <div>
                          <span className="surface-muted">{t('admin.ops.errorDetail.upstreamEvent.status')}:</span>
                          <span className="ml-1 font-mono">{ev.status_code ?? '—'}</span>
                        </div>
                        <div>
                          <span className="surface-muted">{t('admin.ops.errorDetail.upstreamEvent.requestId')}:</span>
                          <span className="ml-1 font-mono">{ev.request_id || ev.client_request_id || '—'}</span>
                        </div>
                      </div>

                      {ev.message && (
                        <div className="mt-3 break-words text-sm font-medium text-gray-900 dark:text-white">
                          {ev.message}
                        </div>
                      )}

                      {expandedUpstreamDetailIds.has(ev.id) && (
                        <pre className="mt-3 max-h-[240px] overflow-auto rounded-xl border border-gray-200 surface-inset p-3 text-xs text-gray-800 dark:border-dark-700 dark:text-gray-100">
                          <code>{prettyJSON(getUpstreamResponsePreview(ev))}</code>
                        </pre>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </DetailContentHost>
  )
}

export default OpsErrorDetailModal
