import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Modal, Toggle } from '@/components/ui'
import type { PromptAuditEndpointDraft, PromptProbeResult } from '../types'
import {
  cloneData,
  createDefaultEndpoint,
  MAX_GUARD_INPUT_LIMIT,
  MAX_GUARD_TIMEOUT_MS,
  MIN_GUARD_INPUT_LIMIT,
  MIN_GUARD_TIMEOUT_MS
} from '../viewModel'

export interface EndpointPoolProps {
  endpoints: PromptAuditEndpointDraft[]
  probeResults: Record<string, PromptProbeResult>
  probingIds: string[]
  onUpdateEndpoints: (value: PromptAuditEndpointDraft[]) => void
  onProbe: (endpoint: PromptAuditEndpointDraft) => void
}

function hasCredential(endpoint: PromptAuditEndpointDraft): boolean {
  return Boolean(endpoint.token.trim() || (endpoint.has_token && !endpoint.clear_token))
}

function credentialInvalid(endpoint: PromptAuditEndpointDraft): boolean {
  return endpoint.token_status === 'invalid' && !endpoint.token.trim() && !endpoint.clear_token
}

export function EndpointPool({ endpoints, probeResults, probingIds, onUpdateEndpoints, onProbe }: EndpointPoolProps) {
  const { t } = useTranslation()
  const [editing, setEditing] = useState<PromptAuditEndpointDraft | null>(null)
  const [editingIndex, setEditingIndex] = useState(-1)
  const editorForm = useRef<HTMLFormElement | null>(null)

  function openCreate() {
    setEditingIndex(-1)
    setEditing(createDefaultEndpoint(endpoints.length + 1))
  }

  function openEdit(endpoint: PromptAuditEndpointDraft) {
    setEditingIndex(endpoints.findIndex((item) => item.id === endpoint.id))
    setEditing(cloneData(endpoint))
  }

  function closeEditor() {
    setEditing(null)
    setEditingIndex(-1)
  }

  function saveEditor() {
    if (!editorForm.current?.reportValidity()) return
    if (!editing?.id.trim() || !editing.name.trim() || !editing.base_url.trim()) return
    const next = endpoints.map((item) => cloneData(item))
    const value = cloneData(editing)
    if (value.token.trim()) value.clear_token = false
    if (editingIndex < 0) next.push(value)
    else next.splice(editingIndex, 1, value)
    onUpdateEndpoints(next)
    closeEditor()
  }

  function formatNumber(value: number): string {
    return new Intl.NumberFormat().format(value)
  }

  function toggleEndpoint(id: string) {
    onUpdateEndpoints(
      endpoints.map((item) => (item.id === id ? { ...item, enabled: !item.enabled } : cloneData(item)))
    )
  }

  function removeEndpoint(endpoint: PromptAuditEndpointDraft) {
    if (!window.confirm(t('admin.promptAudit.pool.deleteConfirm', { name: endpoint.name }))) return
    onUpdateEndpoints(endpoints.filter((item) => item.id !== endpoint.id).map((item) => cloneData(item)))
  }

  const patchEditing = (value: Partial<PromptAuditEndpointDraft>) =>
    setEditing((previous) => (previous ? { ...previous, ...value } : previous))

  return (
    <section aria-labelledby="prompt-pool-title" className="border-b border-gray-200 py-6 dark:border-dark-700/60">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 id="prompt-pool-title" className="text-base font-semibold text-gray-950 dark:text-white">
            {t('admin.promptAudit.pool.title')}
          </h2>
          <p className="mt-1 text-sm surface-muted">{t('admin.promptAudit.pool.description')}</p>
        </div>
        <Button variant="primary" size="sm" data-test="add-endpoint" onClick={openCreate}>
          {t('admin.promptAudit.pool.add')}
        </Button>
      </div>

      {endpoints.length === 0 ? (
        <div className="mt-5 rounded-xl border border-dashed border-gray-300 px-5 py-10 text-center text-sm dark:border-dark-600 dark:surface-inset surface-muted">
          {t('admin.promptAudit.pool.empty')}
        </div>
      ) : (
        <div className="mt-5 overflow-hidden rounded-xl border border-gray-200 surface-inset dark:border-dark-700/60">
          <div className="hidden grid-cols-[minmax(260px,1.45fr)_minmax(210px,1fr)_minmax(190px,.8fr)_minmax(230px,1.15fr)_auto] gap-5 border-x border-b border-x-transparent border-b-gray-200 surface-inset px-7 py-2.5 text-[11px] font-semibold uppercase tracking-[0.08em] dark:border-b-dark-700/60 xl:grid surface-muted">
            <span>{t('admin.promptAudit.pool.node')}</span>
            <span>{t('admin.promptAudit.pool.model')}</span>
            <span>{t('admin.promptAudit.pool.limits')}</span>
            <span>{t('admin.promptAudit.pool.credential')}</span>
            <span className="text-right">{t('admin.promptAudit.common.actions')}</span>
          </div>

          <div className="space-y-2 p-2">
            {endpoints.map((endpoint) => (
              <article
                key={endpoint.id}
                data-test={`endpoint-${endpoint.id}`}
                className="group grid gap-4 rounded-xl border border-gray-200 px-4 py-4 transition-[background-color,border-color] duration-200 hover:border-primary-500 hover:surface-hover dark:border-dark-700 sm:px-5 xl:grid-cols-[minmax(260px,1.45fr)_minmax(210px,1fr)_minmax(190px,.8fr)_minmax(230px,1.15fr)_auto] xl:items-center xl:gap-5"
              >
                <div className="flex min-w-0 items-center gap-3">
                  <Toggle
                    checked={!!endpoint.enabled}
                    onCheckedChange={() => toggleEndpoint(endpoint.id)}
                    aria-label={t('admin.promptAudit.pool.toggleNode', { name: endpoint.name })}
                  />
                  <div className="min-w-0">
                    <div className="flex min-w-0 items-center gap-2">
                      <p className="truncate font-semibold text-gray-950 dark:text-white">{endpoint.name}</p>
                      <span
                        className={`console-data-mark h-1.5 w-1.5 shrink-0 rounded-full ${
                          endpoint.enabled ? 'bg-emerald-500' : 'bg-gray-300 dark:bg-dark-500'
                        }`}
                        aria-hidden="true"
                      />
                    </div>
                    <p className="mt-0.5 truncate font-mono text-[11px] surface-muted" title={endpoint.base_url}>
                      {endpoint.base_url}
                    </p>
                  </div>
                </div>

                <div className="min-w-0 xl:block">
                  <p className="mb-1 text-[10px] font-semibold uppercase tracking-wider xl:hidden surface-muted">
                    {t('admin.promptAudit.pool.model')}
                  </p>
                  <p className="truncate text-sm font-medium surface-muted" title={endpoint.model}>
                    {endpoint.model}
                  </p>
                </div>

                <div>
                  <p className="mb-1 text-[10px] font-semibold uppercase tracking-wider xl:hidden surface-muted">
                    {t('admin.promptAudit.pool.limits')}
                  </p>
                  <div className="flex flex-wrap gap-1.5 text-xs surface-muted">
                    <span className="rounded-md surface-inset px-2 py-1 tabular-nums">{endpoint.timeout_ms} ms</span>
                    <span className="rounded-md surface-inset px-2 py-1 tabular-nums">{endpoint.input_limit} chars</span>
                  </div>
                </div>

                <div className="min-w-0">
                  <p className="mb-1 text-[10px] font-semibold uppercase tracking-wider xl:hidden surface-muted">
                    {t('admin.promptAudit.pool.credential')}
                  </p>
                  <div
                    className={`flex items-center gap-1.5 text-xs font-medium ${
                      credentialInvalid(endpoint)
                        ? 'text-red-600 dark:text-red-300'
                        : hasCredential(endpoint)
                          ? 'text-emerald-700 dark:text-emerald-300'
                          : 'surface-muted'
                    }`}
                  >
                    <span
                      className={`console-data-mark h-1.5 w-1.5 rounded-full ${
                        credentialInvalid(endpoint)
                          ? 'bg-red-500'
                          : hasCredential(endpoint)
                            ? 'bg-emerald-500'
                            : 'bg-gray-300 dark:bg-dark-500'
                      }`}
                      aria-hidden="true"
                    />
                    {credentialInvalid(endpoint)
                      ? t('admin.promptAudit.pool.invalid')
                      : hasCredential(endpoint)
                        ? t('admin.promptAudit.pool.configured')
                        : t('admin.promptAudit.pool.missing')}
                  </div>
                  {probingIds.includes(endpoint.id) && (
                    <p className="mt-1.5 text-xs text-primary-600 dark:text-primary-300">
                      {t('admin.promptAudit.pool.probeProgress')}
                    </p>
                  )}
                  {probeResults[endpoint.id] && (
                    <p
                      className={`mt-1.5 line-clamp-2 text-xs leading-5 ${
                        probeResults[endpoint.id].ok
                          ? 'text-emerald-600 dark:text-emerald-300'
                          : 'text-red-600 dark:text-red-300'
                      }`}
                    >
                      {t('admin.promptAudit.pool.probeResult', {
                        status: probeResults[endpoint.id].status,
                        http: probeResults[endpoint.id].http_status || '—',
                        latency: probeResults[endpoint.id].latency_ms
                      })}
                      · {probeResults[endpoint.id].message}
                    </p>
                  )}
                </div>

                <div className="flex flex-wrap items-center justify-end gap-1 border-t border-gray-100 pt-3 dark:border-dark-800 xl:flex-nowrap xl:border-0 xl:pt-0">
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={probingIds.includes(endpoint.id)}
                    onClick={() => onProbe(endpoint)}
                  >
                    {probingIds.includes(endpoint.id)
                      ? t('admin.promptAudit.pool.probing')
                      : t('admin.promptAudit.pool.probe')}
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => openEdit(endpoint)}>
                    {t('common.edit')}
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-red-600 hover:bg-red-50/25 dark:text-red-300 dark:hover:bg-red-950/30"
                    onClick={() => removeEndpoint(endpoint)}
                  >
                    {t('common.delete')}
                  </Button>
                </div>
              </article>
            ))}
          </div>
        </div>
      )}

      <Modal
        open={Boolean(editing)}
        onClose={closeEditor}
        title={editingIndex < 0 ? t('admin.promptAudit.pool.add') : t('admin.promptAudit.pool.edit')}
        size="xl"
        footer={
          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={closeEditor}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" data-test="save-endpoint" onClick={saveEditor}>
              {t('common.save')}
            </Button>
          </div>
        }
      >
        {editing && (
          <form ref={editorForm} className="grid gap-4 sm:grid-cols-2" onSubmit={(event) => {
            event.preventDefault()
            saveEditor()
          }}>
            <label className="space-y-1 text-sm surface-muted">
              <span>{t('admin.promptAudit.pool.name')}</span>
              <input
                value={editing.name}
                className="input w-full"
                required
                aria-label={t('admin.promptAudit.pool.name')}
                onChange={(event) => patchEditing({ name: event.target.value })}
              />
            </label>
            <label className="space-y-1 text-sm surface-muted">
              <span>{t('admin.promptAudit.pool.id')}</span>
              <input
                value={editing.id}
                className="input w-full"
                required
                disabled={editingIndex >= 0}
                aria-label={t('admin.promptAudit.pool.id')}
                onChange={(event) => patchEditing({ id: event.target.value })}
              />
            </label>
            <label className="space-y-1 sm:col-span-2 surface-muted">
              <span>{t('admin.promptAudit.pool.baseUrl')}</span>
              <input
                value={editing.base_url}
                className="input w-full"
                required
                inputMode="url"
                aria-label={t('admin.promptAudit.pool.baseUrl')}
                onChange={(event) => patchEditing({ base_url: event.target.value })}
              />
            </label>
            <label className="space-y-1 sm:col-span-2 surface-muted">
              <span>{t('admin.promptAudit.pool.apiKey')}</span>
              <input
                value={editing.token}
                className="input w-full"
                type="password"
                autoComplete="new-password"
                placeholder={
                  editing.has_token
                    ? editing.token_status === 'invalid'
                      ? t('admin.promptAudit.pool.reenterSecret')
                      : t('admin.promptAudit.pool.keepSecret')
                    : ''
                }
                aria-label={t('admin.promptAudit.pool.apiKey')}
                onChange={(event) => patchEditing({ token: event.target.value })}
              />
              <span className="block text-xs surface-muted">{t('admin.promptAudit.pool.secretHint')}</span>
            </label>
            {editing.has_token && (
              <label className="flex items-center gap-2 text-sm text-red-600 dark:text-red-300 sm:col-span-2">
                <input
                  type="checkbox"
                  checked={editing.clear_token}
                  aria-label={t('admin.promptAudit.pool.clearSecret')}
                  onChange={(event) => patchEditing({ clear_token: event.target.checked })}
                />
                {t('admin.promptAudit.pool.clearSecret')}
              </label>
            )}
            <label className="space-y-1 sm:col-span-2 surface-muted">
              <span>{t('admin.promptAudit.pool.model')}</span>
              <input
                value={editing.model}
                className="input w-full"
                aria-label={t('admin.promptAudit.pool.model')}
                onChange={(event) => patchEditing({ model: event.target.value })}
              />
            </label>
            <label className="space-y-1 surface-muted">
              <span>{t('admin.promptAudit.pool.timeout')}</span>
              <input
                value={editing.timeout_ms}
                type="number"
                min={MIN_GUARD_TIMEOUT_MS}
                max={MAX_GUARD_TIMEOUT_MS}
                step={1}
                className="input w-full"
                required
                aria-label={t('admin.promptAudit.pool.timeout')}
                onChange={(event) => patchEditing({ timeout_ms: Number(event.target.value) })}
              />
              <span className="block text-xs surface-muted">
                {t('admin.promptAudit.pool.timeoutRange', {
                  min: formatNumber(MIN_GUARD_TIMEOUT_MS),
                  max: formatNumber(MAX_GUARD_TIMEOUT_MS)
                })}
              </span>
            </label>
            <label className="space-y-1 surface-muted">
              <span>{t('admin.promptAudit.pool.inputLimit')}</span>
              <input
                value={editing.input_limit}
                type="number"
                min={MIN_GUARD_INPUT_LIMIT}
                max={MAX_GUARD_INPUT_LIMIT}
                step={1}
                className="input w-full"
                required
                aria-label={t('admin.promptAudit.pool.inputLimit')}
                onChange={(event) => patchEditing({ input_limit: Number(event.target.value) })}
              />
              <span className="block text-xs surface-muted">
                {t('admin.promptAudit.pool.inputLimitRange', {
                  min: formatNumber(MIN_GUARD_INPUT_LIMIT),
                  max: formatNumber(MAX_GUARD_INPUT_LIMIT)
                })}
              </span>
            </label>
            <p className="text-xs leading-5 sm:col-span-2 surface-muted">
              {t('admin.promptAudit.pool.inputLimitBehavior')}
            </p>
          </form>
        )}
      </Modal>
    </section>
  )
}

export default EndpointPool
