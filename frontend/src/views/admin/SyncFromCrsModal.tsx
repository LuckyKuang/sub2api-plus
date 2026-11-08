import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { adminAPI } from '@/api'
import { useAppStore } from '@/stores/app'
import type { PreviewFromCRSResult } from '@/api/admin/accounts'
import { Modal } from '@/components/ui'

export interface SyncFromCrsModalProps {
  show: boolean
  onClose: () => void
  onSynced: () => void
}

type Step = 'input' | 'preview' | 'result'

export function SyncFromCrsModal({ show, onClose, onSynced }: SyncFromCrsModalProps) {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)

  const [currentStep, setCurrentStep] = useState<Step>('input')
  const [previewing, setPreviewing] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [previewResult, setPreviewResult] = useState<PreviewFromCRSResult | null>(null)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [result, setResult] = useState<Awaited<ReturnType<typeof adminAPI.accounts.syncFromCrs>> | null>(null)

  const [form, setForm] = useState({
    base_url: '',
    username: '',
    password: '',
    sync_proxies: true
  })

  useEffect(() => {
    if (!show) return
    setCurrentStep('input')
    setPreviewResult(null)
    setSelectedIds(new Set())
    setResult(null)
    setForm({ base_url: '', username: '', password: '', sync_proxies: true })
  }, [show])

  const hasNewButNoneSelected = previewResult
    ? previewResult.new_accounts.length > 0 && selectedIds.size === 0
    : false

  const errorItems = result?.items
    ? result.items.filter((i) => i.action === 'failed' || (i.action === 'skipped' && i.error !== 'not selected'))
    : []

  const handleClose = () => {
    if (syncing || previewing) return
    onClose()
  }

  const handleBack = () => {
    setCurrentStep('input')
    setPreviewResult(null)
    setSelectedIds(new Set())
  }

  const selectAll = () => {
    if (!previewResult) return
    setSelectedIds(new Set(previewResult.new_accounts.map((a) => a.crs_account_id)))
  }

  const selectNone = () => {
    setSelectedIds(new Set())
  }

  const toggleSelect = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const handlePreview = async () => {
    if (!form.base_url.trim() || !form.username.trim() || !form.password.trim()) {
      showError(t('admin.accounts.syncMissingFields'))
      return
    }

    setPreviewing(true)
    try {
      const res = await adminAPI.accounts.previewFromCrs({
        base_url: form.base_url.trim(),
        username: form.username.trim(),
        password: form.password
      })
      setPreviewResult(res)
      // Auto-select all new accounts
      setSelectedIds(new Set(res.new_accounts.map((a) => a.crs_account_id)))
      setCurrentStep('preview')
    } catch (error) {
      showError((error as { message?: string })?.message || t('admin.accounts.crsPreviewFailed'))
    } finally {
      setPreviewing(false)
    }
  }

  const handleSync = async () => {
    if (!form.base_url.trim() || !form.username.trim() || !form.password.trim()) {
      showError(t('admin.accounts.syncMissingFields'))
      return
    }

    setSyncing(true)
    try {
      const res = await adminAPI.accounts.syncFromCrs({
        base_url: form.base_url.trim(),
        username: form.username.trim(),
        password: form.password,
        sync_proxies: form.sync_proxies,
        selected_account_ids: [...selectedIds]
      })
      setResult(res)
      setCurrentStep('result')

      if (res.failed > 0) {
        showError(t('admin.accounts.syncCompletedWithErrors', res))
      } else {
        showSuccess(t('admin.accounts.syncCompleted', res))
      }
      onSynced()
    } catch (error) {
      showError((error as { message?: string })?.message || t('admin.accounts.syncFailed'))
    } finally {
      setSyncing(false)
    }
  }

  return (
    <Modal open={show} onClose={handleClose} title={t('admin.accounts.syncFromCrsTitle')} size="lg">
      {/* Step 1: Input credentials */}
      {currentStep === 'input' ? (
        <form
          id="sync-from-crs-form"
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            void handlePreview()
          }}
        >
          <div className="text-sm surface-muted">{t('admin.accounts.syncFromCrsDesc')}</div>
          <div className="rounded-lg surface-inset p-3 text-xs surface-muted">
            {t('admin.accounts.crsUpdateBehaviorNote')}
          </div>
          <div className="rounded-lg border border-amber-200 bg-amber-50/[0.15] p-3 text-xs text-amber-600 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-400">
            {t('admin.accounts.crsVersionRequirement')}
          </div>

          <div className="grid grid-cols-1 gap-4">
            <div>
              <label htmlFor="crs-base-url" className="input-label">
                {t('admin.accounts.crsBaseUrl')}
              </label>
              <input
                id="crs-base-url"
                value={form.base_url}
                onChange={(event) => setForm((prev) => ({ ...prev, base_url: event.target.value }))}
                type="text"
                className="input"
                required
                placeholder={t('admin.accounts.crsBaseUrlPlaceholder')}
              />
            </div>

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div>
                <label htmlFor="crs-username" className="input-label">
                  {t('admin.accounts.crsUsername')}
                </label>
                <input
                  id="crs-username"
                  value={form.username}
                  onChange={(event) => setForm((prev) => ({ ...prev, username: event.target.value }))}
                  type="text"
                  className="input"
                  required
                  autoComplete="username"
                />
              </div>
              <div>
                <label htmlFor="crs-password" className="input-label">
                  {t('admin.accounts.crsPassword')}
                </label>
                <input
                  id="crs-password"
                  value={form.password}
                  onChange={(event) => setForm((prev) => ({ ...prev, password: event.target.value }))}
                  type="password"
                  className="input"
                  required
                  autoComplete="current-password"
                />
              </div>
            </div>

            <label className="flex items-center gap-2 text-sm surface-muted">
              <input
                checked={form.sync_proxies}
                onChange={(event) => setForm((prev) => ({ ...prev, sync_proxies: event.target.checked }))}
                type="checkbox"
                className="rounded border-gray-300 dark:border-dark-600"
              />
              {t('admin.accounts.syncProxies')}
            </label>
          </div>
        </form>
      ) : null}

      {/* Step 2: Preview & select */}
      {currentStep === 'preview' && previewResult ? (
        <div className="space-y-4">
          {/* Existing accounts (read-only info) */}
          {previewResult.existing_accounts.length > 0 && (
            <div className="rounded-lg surface-inset p-3">
              <div className="mb-2 text-sm font-medium surface-muted">
                {t('admin.accounts.crsExistingAccounts')}
                <span className="ml-1 text-xs surface-muted">({previewResult.existing_accounts.length})</span>
              </div>
              <div className="max-h-32 overflow-auto text-xs surface-muted">
                {previewResult.existing_accounts.map((acc) => (
                  <div key={acc.crs_account_id} className="flex items-center gap-2 py-0.5">
                    <span className="inline-block rounded bg-blue-100/[0.15] px-1.5 py-0.5 text-[10px] font-medium text-blue-700 dark:bg-blue-900/30 dark:text-blue-400">
                      {acc.platform} / {acc.type}
                    </span>
                    <span className="truncate">{acc.name}</span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* New accounts (selectable) */}
          {previewResult.new_accounts.length > 0 && (
            <div>
              <div className="mb-2 flex items-center justify-between">
                <div className="text-sm font-medium text-gray-900 dark:text-white">
                  {t('admin.accounts.crsNewAccounts')}
                  <span className="ml-1 text-xs surface-muted">({previewResult.new_accounts.length})</span>
                </div>
                <div className="flex gap-2">
                  <button
                    type="button"
                    className="text-xs text-blue-600 hover:text-blue-700 dark:text-blue-400 surface-control"
                    onClick={selectAll}
                  >
                    {t('admin.accounts.crsSelectAll')}
                  </button>
                  <button
                    type="button"
                    className="text-xs hover:text-gray-600 surface-control surface-muted"
                    onClick={selectNone}
                  >
                    {t('admin.accounts.crsSelectNone')}
                  </button>
                </div>
              </div>
              <div className="max-h-48 overflow-auto rounded-lg border border-gray-200 p-2 dark:border-dark-600">
                {previewResult.new_accounts.map((acc) => (
                  <label
                    key={acc.crs_account_id}
                    className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 hover:surface-hover"
                  >
                    <input
                      type="checkbox"
                      checked={selectedIds.has(acc.crs_account_id)}
                      className="rounded border-gray-300 dark:border-dark-600"
                      onChange={() => toggleSelect(acc.crs_account_id)}
                    />
                    <span className="inline-block rounded bg-green-100/[0.15] px-1.5 py-0.5 text-[10px] font-medium text-green-700 dark:bg-green-900/30 dark:text-green-400">
                      {acc.platform} / {acc.type}
                    </span>
                    <span className="truncate text-sm surface-muted">{acc.name}</span>
                  </label>
                ))}
              </div>
              <div className="mt-1 text-xs surface-muted">{t('admin.accounts.crsSelectedCount', { count: selectedIds.size })}</div>
            </div>
          )}

          {/* Sync options summary */}
          <div className="flex items-center gap-2 text-xs surface-muted">
            <span>{t('admin.accounts.syncProxies')}:</span>
            <span className={form.sync_proxies ? 'text-green-600 dark:text-green-400' : 'surface-muted'}>
              {form.sync_proxies ? t('common.yes') : t('common.no')}
            </span>
          </div>

          {/* No new accounts */}
          {previewResult.new_accounts.length === 0 && (
            <div className="rounded-lg surface-inset p-4 text-center text-sm surface-muted">
              {t('admin.accounts.crsNoNewAccounts')}
              {previewResult.existing_accounts.length > 0 && (
                <span>{t('admin.accounts.crsWillUpdate', { count: previewResult.existing_accounts.length })}</span>
              )}
            </div>
          )}
        </div>
      ) : null}

      {/* Step 3: Result */}
      {currentStep === 'result' && result ? (
        <div className="space-y-4">
          <div className="space-y-2 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
            <div className="text-sm font-medium text-gray-900 dark:text-white">{t('admin.accounts.syncResult')}</div>
            <div className="text-sm surface-muted">{t('admin.accounts.syncResultSummary', result)}</div>

            {errorItems.length > 0 && (
              <div className="mt-2">
                <div className="text-sm font-medium text-red-600 dark:text-red-400">{t('admin.accounts.syncErrors')}</div>
                <div className="mt-2 max-h-48 overflow-auto rounded-lg surface-inset p-3 font-mono text-xs">
                  {errorItems.map((item, idx) => (
                    <div key={idx} className="whitespace-pre-wrap">
                      {item.kind} {item.crs_account_id} — {item.action}
                      {item.error ? `: ${item.error}` : ''}
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>
      ) : null}

      <div className="flex justify-end gap-3">
        {/* Step 1: Input */}
        {currentStep === 'input' && (
          <>
            <button className="btn btn-secondary" type="button" disabled={previewing} onClick={handleClose}>
              {t('common.cancel')}
            </button>
            <button className="btn btn-primary" type="submit" form="sync-from-crs-form" disabled={previewing}>
              {previewing ? t('admin.accounts.crsPreviewing') : t('admin.accounts.crsPreview')}
            </button>
          </>
        )}

        {/* Step 2: Preview */}
        {currentStep === 'preview' && (
          <>
            <button className="btn btn-secondary" type="button" disabled={syncing} onClick={handleBack}>
              {t('admin.accounts.crsBack')}
            </button>
            <button
              className="btn btn-primary"
              type="button"
              disabled={syncing || hasNewButNoneSelected}
              onClick={() => void handleSync()}
            >
              {syncing ? t('admin.accounts.syncing') : t('admin.accounts.syncNow')}
            </button>
          </>
        )}

        {/* Step 3: Result */}
        {currentStep === 'result' && (
          <button className="btn btn-secondary" type="button" onClick={handleClose}>
            {t('common.close')}
          </button>
        )}
      </div>
    </Modal>
  )
}

export default SyncFromCrsModal
