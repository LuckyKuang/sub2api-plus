import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, ConfirmDialog, Toggle } from '@/components/ui'
import { useAppStore } from '@/stores/app'
import { extractApiErrorCode, extractApiErrorMessage } from '@/utils/apiError'
import promptAuditAPI from './api'
import { RuntimeOverview } from './components/RuntimeOverview'
import { EndpointPool } from './components/EndpointPool'
import { PolicyPanel } from './components/PolicyPanel'
import { EventWorkspace } from './components/EventWorkspace'
import { EventDetailDialog } from './components/EventDetailDialog'
import { FilterDeleteDialog } from './components/FilterDeleteDialog'
import type {
  PromptAuditDraft,
  PromptAuditEndpointDraft,
  PromptAuditEvent,
  PromptAuditGroup,
  PromptAuditRuntime,
  PromptDeletePreview,
  PromptEventFilters,
  PromptEventPage,
  PromptLoadErrors,
  PromptProbeResult
} from './types'
import { buildUpdateRequest, cloneData, configToDraft, draftFingerprint, emptyEventFilters } from './viewModel'

type PromptAuditPageTab = 'config' | 'events'

interface SaveToggleProps {
  label: string
  modelValue: boolean
  disabled?: boolean
  dataTest?: string
  onUpdateModelValue: (value: boolean) => void
}

function SaveToggle({ label, modelValue, disabled = false, dataTest, onUpdateModelValue }: SaveToggleProps) {
  return (
    <label className={`flex items-center gap-2.5 text-sm ${disabled ? 'cursor-not-allowed' : 'cursor-pointer'}`}>
      <Toggle
        checked={modelValue}
        disabled={disabled}
        ariaLabel={label}
        dataTest={dataTest}
        onCheckedChange={(value) => onUpdateModelValue(value)}
      />
      <span className={`select-none surface-muted ${disabled ? 'opacity-50' : ''}`}>{label}</span>
    </label>
  )
}

export default function PromptAuditView() {
  const { t, i18n } = useTranslation()
  const showSuccess = useAppStore((state) => state.showSuccess)
  const showError = useAppStore((state) => state.showError)

  const [activeTab, setActiveTab] = useState<PromptAuditPageTab>('events')
  const [serverConfig, setServerConfig] = useState<PromptAuditDraft | null>(null)
  const [draft, setDraft] = useState<PromptAuditDraft | null>(null)
  const [runtime, setRuntime] = useState<PromptAuditRuntime | null>(null)
  const [groups, setGroups] = useState<PromptAuditGroup[]>([])
  const [eventsState, setEventsState] = useState<PromptEventPage>({
    items: [],
    total: 0,
    page: 1,
    page_size: 20,
    pages: 0
  })
  // The event page mirrors into a ref so every loader reads the page it belongs
  // to, even when a page change and its reload happen in the same tick.
  const eventsRef = useRef(eventsState)
  eventsRef.current = eventsState
  const events = eventsState
  const setEvents = (next: PromptEventPage | ((previous: PromptEventPage) => PromptEventPage)) => {
    const resolved =
      typeof next === 'function' ? next(eventsRef.current) : next
    eventsRef.current = resolved
    setEventsState(resolved)
  }
  const [filters, setFilters] = useState<PromptEventFilters>(() => emptyEventFilters())
  const appliedFilters = useRef<PromptEventFilters>(emptyEventFilters())
  const [selectedEventIds, setSelectedEventIds] = useState<number[]>([])
  const [activeEvent, setActiveEvent] = useState<PromptAuditEvent | null>(null)
  const [showEventDetail, setShowEventDetail] = useState(false)
  const [probeResults, setProbeResults] = useState<Record<string, PromptProbeResult>>({})
  const [probingIds, setProbingIds] = useState<string[]>([])
  const probingIdsRef = useRef(probingIds)
  probingIdsRef.current = probingIds
  const [showFilterDelete, setShowFilterDelete] = useState(false)
  const [deletePreview, setDeletePreview] = useState<PromptDeletePreview | null>(null)
  const deletePreviewFilters = useRef<PromptEventFilters | null>(null)
  const [showBlockingConfirmation, setShowBlockingConfirmation] = useState(false)
  const [deleteRequest, setDeleteRequest] = useState<{ mode: '' | 'single' | 'batch'; ids: number[] }>({
    mode: '',
    ids: []
  })
  const [loading, setLoading] = useState({
    config: false,
    runtime: false,
    groups: false,
    events: false,
    saving: false,
    detail: false,
    deleting: false,
    previewing: false
  })
  const [loadErrors, setLoadErrors] = useState<PromptLoadErrors>({
    config: '',
    runtime: '',
    groups: '',
    events: ''
  })

  const dirty = draftFingerprint(draft) !== draftFingerprint(serverConfig)

  const errorMessage = useCallback(
    (error: unknown, fallbackKey: string): string => {
      const code = extractApiErrorCode(error)
      if (code) {
        const key = `admin.promptAudit.errors.${code}`
        const translated = t(key)
        if (translated !== key) return translated
      }
      return extractApiErrorMessage(error, t(fallbackKey))
    },
    [t]
  )

  const loadConfig = useCallback(async () => {
    setLoading((previous) => ({ ...previous, config: true }))
    setLoadErrors((previous) => ({ ...previous, config: '' }))
    try {
      const config = await promptAuditAPI.getConfig()
      setServerConfig(configToDraft(config))
      setDraft(configToDraft(config))
    } catch (error) {
      setLoadErrors((previous) => ({ ...previous, config: errorMessage(error, 'admin.promptAudit.errors.loadConfig') }))
    } finally {
      setLoading((previous) => ({ ...previous, config: false }))
    }
  }, [errorMessage])

  const loadRuntime = useCallback(async () => {
    setLoading((previous) => ({ ...previous, runtime: true }))
    setLoadErrors((previous) => ({ ...previous, runtime: '' }))
    try {
      setRuntime(await promptAuditAPI.getRuntime())
    } catch (error) {
      setLoadErrors((previous) => ({ ...previous, runtime: errorMessage(error, 'admin.promptAudit.errors.loadRuntime') }))
    } finally {
      setLoading((previous) => ({ ...previous, runtime: false }))
    }
  }, [errorMessage])

  const loadGroups = useCallback(async () => {
    setLoading((previous) => ({ ...previous, groups: true }))
    setLoadErrors((previous) => ({ ...previous, groups: '' }))
    try {
      setGroups(await promptAuditAPI.listGroups())
    } catch (error) {
      setLoadErrors((previous) => ({ ...previous, groups: errorMessage(error, 'admin.promptAudit.errors.loadGroups') }))
    } finally {
      setLoading((previous) => ({ ...previous, groups: false }))
    }
  }, [errorMessage])

  const loadEvents = useCallback(async () => {
    setLoading((previous) => ({ ...previous, events: true }))
    setLoadErrors((previous) => ({ ...previous, events: '' }))
    try {
      const result = await promptAuditAPI.listEvents(
        appliedFilters.current,
        eventsRef.current.page,
        eventsRef.current.page_size
      )
      setEvents(result)
      setSelectedEventIds([])
    } catch (error) {
      setLoadErrors((previous) => ({
        ...previous,
        events: errorMessage(error, 'admin.promptAudit.errors.loadEvents')
      }))
    } finally {
      setLoading((previous) => ({ ...previous, events: false }))
    }
  }, [errorMessage])

  useEffect(() => {
    void Promise.allSettled([loadConfig(), loadRuntime(), loadGroups(), loadEvents()])
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function replaceDraft(value: PromptAuditDraft) {
    setDraft(cloneData(value))
  }

  function updateEndpoints(value: PromptAuditEndpointDraft[]) {
    if (!draft) return
    replaceDraft({ ...draft, endpoints: value })
  }

  function setEnabled(value: boolean) {
    if (!draft) return
    replaceDraft({ ...draft, enabled: value, blocking_enabled: value ? draft.blocking_enabled : false })
  }

  function setBlocking(value: boolean) {
    if (!draft || !draft.enabled) return
    if (value && !draft.blocking_enabled) {
      setShowBlockingConfirmation(true)
      return
    }
    replaceDraft({ ...draft, blocking_enabled: value })
  }

  function confirmBlocking() {
    setShowBlockingConfirmation(false)
    if (draft) replaceDraft({ ...draft, blocking_enabled: true })
  }

  function resetDraft() {
    if (serverConfig) setDraft(cloneData(serverConfig))
  }

  async function saveConfig() {
    if (!draft || !dirty) return
    setLoading((previous) => ({ ...previous, saving: true }))
    try {
      const saved = await promptAuditAPI.updateConfig(buildUpdateRequest(draft))
      setServerConfig(configToDraft(saved))
      setDraft(configToDraft(saved))
      showSuccess(t('admin.promptAudit.messages.saved'))
      await loadRuntime()
    } catch (error) {
      const code = extractApiErrorCode(error)
      showError(
        errorMessage(
          error,
          code === 'prompt_audit_config_conflict'
            ? 'admin.promptAudit.errors.prompt_audit_config_conflict'
            : 'admin.promptAudit.errors.saveConfig'
        )
      )
    } finally {
      setLoading((previous) => ({ ...previous, saving: false }))
    }
  }

  async function runProbe(endpoint: PromptAuditEndpointDraft) {
    if (probingIdsRef.current.includes(endpoint.id)) return
    probingIdsRef.current = [...probingIdsRef.current, endpoint.id]
    setProbingIds(probingIdsRef.current)
    try {
      const result = await promptAuditAPI.probeEndpoint(endpoint)
      setProbeResults((previous) => ({ ...previous, [endpoint.id]: result }))
      if (result.ok) showSuccess(t('admin.promptAudit.messages.probeSucceeded'))
      else showError(`${result.error_code || result.status}: ${result.message}`)
    } catch (error) {
      showError(errorMessage(error, 'admin.promptAudit.errors.probe'))
    } finally {
      probingIdsRef.current = probingIdsRef.current.filter((id) => id !== endpoint.id)
      setProbingIds(probingIdsRef.current)
    }
  }

  function handleFiltersChanged(value: PromptEventFilters) {
    setFilters(cloneData(value))
    clearDeletePreview()
  }

  function applyEventFilters(value: PromptEventFilters) {
    setFilters(cloneData(value))
    appliedFilters.current = cloneData(value)
    setEvents((previous) => ({ ...previous, page: 1 }))
    clearDeletePreview()
    void loadEvents()
  }

  function changePage(value: number) {
    setEvents((previous) => ({ ...previous, page: value }))
    void loadEvents()
  }

  function changePageSize(value: number) {
    setEvents((previous) => ({ ...previous, page_size: value, page: 1 }))
    void loadEvents()
  }

  async function openEvent(id: number) {
    setShowEventDetail(true)
    setLoading((previous) => ({ ...previous, detail: true }))
    setActiveEvent(null)
    try {
      setActiveEvent(await promptAuditAPI.getEvent(id))
    } catch (error) {
      showError(errorMessage(error, 'admin.promptAudit.errors.loadDetail'))
      setShowEventDetail(false)
    } finally {
      setLoading((previous) => ({ ...previous, detail: false }))
    }
  }

  function closeEventDetail() {
    setShowEventDetail(false)
    setActiveEvent(null)
  }

  function requestSingleDelete(id: number) {
    setDeleteRequest({ mode: 'single', ids: [id] })
  }

  function requestBatchDelete() {
    if (selectedEventIds.length) setDeleteRequest({ mode: 'batch', ids: [...selectedEventIds] })
  }

  function clearDeleteRequest() {
    setDeleteRequest({ mode: '', ids: [] })
  }

  async function confirmIDDelete() {
    const mode = deleteRequest.mode
    const ids = [...deleteRequest.ids]
    clearDeleteRequest()
    if (!mode || ids.length === 0) return
    setLoading((previous) => ({ ...previous, deleting: true }))
    try {
      const result =
        mode === 'single'
          ? await promptAuditAPI.deleteEvent(ids[0])
          : await promptAuditAPI.batchDeleteEvents(ids)
      showSuccess(t('admin.promptAudit.messages.deleted', { count: result.deleted_events }))
      await Promise.allSettled([loadEvents(), loadRuntime()])
    } catch (error) {
      showError(errorMessage(error, 'admin.promptAudit.errors.delete'))
    } finally {
      setLoading((previous) => ({ ...previous, deleting: false }))
    }
  }

  function clearDeletePreview() {
    setDeletePreview(null)
    deletePreviewFilters.current = null
  }

  function requestFilterDeletePreview() {
    clearDeletePreview()
    setShowFilterDelete(true)
  }

  function closeFilterDelete() {
    setShowFilterDelete(false)
    clearDeletePreview()
  }

  async function runFilterDeletePreview(value: PromptEventFilters) {
    setLoading((previous) => ({ ...previous, previewing: true }))
    try {
      setDeletePreview(await promptAuditAPI.previewDelete(value))
      deletePreviewFilters.current = cloneData(value)
    } catch (error) {
      clearDeletePreview()
      showError(errorMessage(error, 'admin.promptAudit.errors.previewDelete'))
    } finally {
      setLoading((previous) => ({ ...previous, previewing: false }))
    }
  }

  async function confirmFilterDelete(filtersValue?: PromptEventFilters) {
    if (loading.deleting) return
    setLoading((previous) => ({ ...previous, deleting: true }))
    try {
      let preview = deletePreview
      let previewFilters = deletePreviewFilters.current ? cloneData(deletePreviewFilters.current) : null
      // One-click path: no fresh preview (never requested, or cleared by a
      // criteria change) — mint the confirmation token on the fly from the
      // criteria the dialog just emitted, then delete in the same action.
      if ((!preview || !previewFilters) && filtersValue) {
        preview = await promptAuditAPI.previewDelete(filtersValue)
        previewFilters = cloneData(filtersValue)
      }
      if (!preview || !previewFilters) return
      const result = await promptAuditAPI.deleteEventsByFilter(previewFilters, preview)
      closeFilterDelete()
      showSuccess(t('admin.promptAudit.messages.deleted', { count: result.deleted_events }))
      await Promise.allSettled([loadEvents(), loadRuntime()])
    } catch (error) {
      clearDeletePreview()
      showError(errorMessage(error, 'admin.promptAudit.errors.deleteConfirmation'))
    } finally {
      setLoading((previous) => ({ ...previous, deleting: false }))
    }
  }

  function formatDate(value: string): string {
    return new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'medium' }).format(
      new Date(value)
    )
  }

  const pageTabs = [
    { id: 'events' as const, label: t('admin.promptAudit.tabs.events') },
    { id: 'config' as const, label: t('admin.promptAudit.tabs.config') }
  ]

  return (
    <div className="workspace-page">
      <header className="prompt-page-heading card workspace-fixed grid grid-cols-1 items-center gap-4 px-6 py-4 md:grid-cols-[minmax(0,1fr)_auto]">
        <div className="min-w-0">
          <p className="text-xs font-semibold uppercase tracking-[0.16em] text-primary-600 dark:text-primary-400">
            {t('nav.securityAudit')}
          </p>
          <h1 className="mt-1 text-2xl font-semibold tracking-tight text-gray-950 dark:text-white">
            {t('admin.promptAudit.title')}
          </h1>
          <p className="mt-2 max-w-3xl text-sm surface-muted">{t('admin.promptAudit.description')}</p>
        </div>
        {draft && (
          <div className="text-right text-xs surface-muted">
            <p>{t('admin.promptAudit.configVersion', { version: draft.config_version })}</p>
            {draft.updated_at && <p className="mt-1">{formatDate(draft.updated_at)}</p>}
          </div>
        )}
        {/* Tabs live inside the heading card so the page keeps one surface. */}
        <div className="col-span-full w-full">
          <div
            className="workspace-tabs workspace-tabs--embedded workspace-fixed"
            role="tablist"
            aria-label={t('admin.promptAudit.title')}
          >
            {pageTabs.map((tab) => (
              <button
                key={tab.id}
                type="button"
                role="tab"
                className="workspace-tab"
                aria-selected={activeTab === tab.id}
                data-test={`tab-${tab.id}`}
                onClick={() => setActiveTab(tab.id)}
              >
                {tab.label}
              </button>
            ))}
          </div>
        </div>
      </header>

      {loadErrors.config && !draft && (
        <div
          role="alert"
          className="rounded-xl border border-red-200 bg-red-50/[0.15] p-5 dark:border-red-900 dark:bg-red-950/30"
        >
          <p className="text-sm text-red-700 dark:text-red-300">{loadErrors.config}</p>
          <button type="button" className="btn btn-secondary btn-sm mt-3" onClick={() => void loadConfig()}>
            {t('admin.promptAudit.actions.retry')}
          </button>
        </div>
      )}

      {!(loadErrors.config && !draft) && (
        <main className="card workspace-shell workspace-body workspace-clip px-4 sm:px-6">
          <div
            className="workspace-scroll"
            data-test="tab-panel-config"
            style={{ display: activeTab === 'config' ? undefined : 'none' }}
          >
            <RuntimeOverview
              runtime={runtime}
              loading={loading.runtime}
              error={loadErrors.runtime}
              onRefresh={() => void loadRuntime()}
            />

            {draft && (
              <>
                <EndpointPool
                  endpoints={draft.endpoints}
                  probeResults={probeResults}
                  probingIds={probingIds}
                  onUpdateEndpoints={updateEndpoints}
                  onProbe={(endpoint) => void runProbe(endpoint)}
                />
                {loadErrors.groups && (
                  <div
                    role="alert"
                    className="mt-5 rounded-lg bg-amber-50/[0.15] px-4 py-3 text-sm text-amber-800 dark:bg-amber-950/30 dark:text-amber-200"
                  >
                    {loadErrors.groups}
                  </div>
                )}
                <PolicyPanel draft={draft} groups={groups} onUpdateDraft={replaceDraft} />
              </>
            )}
          </div>

          <div
            className="workspace-body workspace-clip"
            data-test="tab-panel-events"
            style={{ display: activeTab === 'events' ? undefined : 'none' }}
          >
            {draft?.enabled && !draft.store_pass_events && (
              <div
                data-test="pass-events-disabled-notice"
                role="status"
                className="prompt-events-notice workspace-fixed mt-6 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-amber-200 bg-amber-50/[0.15] px-4 py-3 text-sm text-amber-900 dark:border-amber-900/70 dark:bg-amber-950/30 dark:text-amber-200"
              >
                <span>{t('admin.promptAudit.events.passEventsDisabled')}</span>
                <button type="button" className="btn btn-secondary btn-sm" onClick={() => setActiveTab('config')}>
                  {t('admin.promptAudit.events.openConfiguration')}
                </button>
              </div>
            )}
            <EventWorkspace
              events={events.items}
              total={events.total}
              page={events.page}
              pageSize={events.page_size}
              filters={filters}
              selectedIds={selectedEventIds}
              loading={loading.events}
              error={loadErrors.events}
              onFiltersChange={handleFiltersChanged}
              onSearch={applyEventFilters}
              onSelection={setSelectedEventIds}
              onPage={changePage}
              onPageSize={changePageSize}
              onView={(id) => void openEvent(id)}
              onDelete={requestSingleDelete}
              onBatchDelete={requestBatchDelete}
              onPreviewDelete={requestFilterDeletePreview}
            />
          </div>
        </main>
      )}

      {draft && activeTab === 'config' && (
        <div className="card workspace-fixed px-4 py-3" data-test="prompt-save-bar">
          <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
            <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
              <SaveToggle
                label={t('admin.promptAudit.saveBar.enabled')}
                modelValue={draft.enabled}
                dataTest="enabled-toggle"
                onUpdateModelValue={setEnabled}
              />
              <SaveToggle
                label={t('admin.promptAudit.saveBar.blocking')}
                modelValue={draft.blocking_enabled}
                disabled={!draft.enabled}
                dataTest="blocking-toggle"
                onUpdateModelValue={setBlocking}
              />
              <SaveToggle
                label={t('admin.promptAudit.saveBar.storePass')}
                modelValue={draft.store_pass_events}
                dataTest="store-pass-toggle"
                onUpdateModelValue={(value) => replaceDraft({ ...draft, store_pass_events: value })}
              />
            </div>
            <div className="flex items-center gap-3">
              <span
                className={`text-sm ${dirty ? 'text-amber-700 dark:text-amber-300' : 'surface-muted'}`}
              >
                {dirty ? t('admin.promptAudit.saveBar.dirty') : t('admin.promptAudit.saveBar.synced')}
              </span>
              <Button variant="secondary" disabled={!dirty || loading.saving} onClick={resetDraft}>
                {t('common.reset')}
              </Button>
              <Button
                variant="primary"
                disabled={!dirty || loading.saving}
                data-test="save-config"
                onClick={() => void saveConfig()}
              >
                {loading.saving ? t('common.saving') : t('common.save')}
              </Button>
            </div>
          </div>
        </div>
      )}

      <ConfirmDialog
        open={showBlockingConfirmation}
        title={t('admin.promptAudit.blockingConfirm.title')}
        message={t('admin.promptAudit.blockingConfirm.message')}
        confirmText={t('admin.promptAudit.blockingConfirm.confirm')}
        variant="danger"
        onConfirm={confirmBlocking}
        onCancel={() => setShowBlockingConfirmation(false)}
      />
      <ConfirmDialog
        open={deleteRequest.mode !== ''}
        title={t('admin.promptAudit.events.deleteConfirmTitle')}
        message={t('admin.promptAudit.events.deleteConfirmMessage', { count: deleteRequest.ids.length })}
        confirmText={t('common.delete')}
        variant="danger"
        onConfirm={() => void confirmIDDelete()}
        onCancel={clearDeleteRequest}
      />
      <FilterDeleteDialog
        show={showFilterDelete}
        initialFilters={filters}
        preview={deletePreview}
        previewing={loading.previewing}
        deleting={loading.deleting}
        onClose={closeFilterDelete}
        onPreview={(value) => void runFilterDeletePreview(value)}
        onConfirm={(value) => void confirmFilterDelete(value)}
        onCriteriaChange={clearDeletePreview}
      />
      <EventDetailDialog
        show={showEventDetail}
        event={activeEvent}
        loading={loading.detail}
        onClose={closeEventDetail}
      />
    </div>
  )
}
