import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Modal, Select } from '@/components/ui'
import { DateTimePicker } from '@/views/admin/parts/DateTimePicker'
import type { PromptDeletePreview, PromptEventFilters } from '../types'
import {
  DELETE_RANGE_PRESETS,
  cloneData,
  emptyEventFilters,
  hasExplicitDeleteRange,
  resolveDeleteRangeFilters,
  type DeleteRangePreset
} from '../viewModel'

export interface FilterDeleteDialogProps {
  show: boolean
  initialFilters: PromptEventFilters
  preview: PromptDeletePreview | null
  previewing: boolean
  deleting: boolean
  onClose: () => void
  onPreview: (value: PromptEventFilters) => void
  onConfirm: (value: PromptEventFilters) => void
  onCriteriaChange: () => void
}

export function FilterDeleteDialog({
  show,
  initialFilters,
  preview,
  previewing,
  deleting,
  onClose,
  onPreview,
  onConfirm,
  onCriteriaChange
}: FilterDeleteDialogProps) {
  const { t, i18n } = useTranslation()
  const [preset, setPreset] = useState<DeleteRangePreset>('7d')
  const [local, setLocal] = useState<PromptEventFilters>(() => emptyEventFilters())
  const initialFiltersRef = useRef(initialFilters)
  initialFiltersRef.current = initialFilters

  useEffect(() => {
    if (!show) return
    const initial = cloneData(initialFiltersRef.current)
    // Only inherit an explicit list-filter range; otherwise default to the
    // seven-day preset so a careless click can never target everything.
    setPreset(hasExplicitDeleteRange(initial) ? 'custom' : '7d')
    setLocal(initial)
  }, [show])

  const canPreview = preset !== 'custom' || hasExplicitDeleteRange(local)

  // One-click flow: a valid criteria selection is enough to confirm — the parent
  // mints the server-side confirmation token on the fly. The button stays
  // disabled only when the range is invalid, work is in flight, or a fresh
  // preview already proved there is nothing to delete.
  const confirmDisabled = !canPreview || previewing || deleting || (preview !== null && preview.matched_count === 0)
  const confirmDisabledReason = (() => {
    if (previewing || deleting) return ''
    if (!canPreview) return 'admin.promptAudit.events.filterDeleteConfirmInvalidRange'
    if (preview && preview.matched_count === 0) return 'admin.promptAudit.events.filterDeleteConfirmNoMatches'
    return ''
  })()

  function criteriaChanged() {
    onCriteriaChange()
  }

  function requestPreview() {
    if (!canPreview) return
    onPreview(resolveDeleteRangeFilters(local, preset))
  }

  function requestConfirm() {
    if (confirmDisabled) return
    onConfirm(resolveDeleteRangeFilters(local, preset))
  }

  function formatDate(value: string): string {
    return new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(value))
  }

  const decisionOptions = [
    { value: '', label: t('common.all') },
    { value: 'pass', label: t('admin.promptAudit.decisions.pass') },
    { value: 'flag', label: t('admin.promptAudit.decisions.flag') },
    { value: 'critical', label: t('admin.promptAudit.decisions.critical') }
  ]

  const riskOptions = [
    { value: '', label: t('common.all') },
    { value: 'low', label: t('admin.promptAudit.riskLevels.low') },
    { value: 'medium', label: t('admin.promptAudit.riskLevels.medium') },
    { value: 'high', label: t('admin.promptAudit.riskLevels.high') },
    { value: 'critical', label: t('admin.promptAudit.riskLevels.critical') }
  ]

  return (
    <Modal
      open={show}
      onClose={onClose}
      title={t('admin.promptAudit.events.filterDeleteDialogTitle')}
      size="xl"
      footer={
        <div className="flex flex-wrap items-center justify-end gap-3">
          {confirmDisabledReason && (
            <p className="mr-auto text-xs surface-muted" data-test="confirm-disabled-reason">
              {t(confirmDisabledReason)}
            </p>
          )}
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button
            variant="secondary"
            disabled={!canPreview || previewing || deleting}
            data-test="run-delete-preview"
            onClick={requestPreview}
          >
            {previewing
              ? t('admin.promptAudit.events.filterDeletePreviewing')
              : t('admin.promptAudit.events.filterDeletePreviewAction')}
          </Button>
          <Button
            variant="danger"
            disabled={confirmDisabled}
            title={confirmDisabledReason ? t(confirmDisabledReason) : undefined}
            data-test="confirm-filter-delete"
            onClick={requestConfirm}
          >
            {deleting ? t('common.submitting') : t('admin.promptAudit.events.confirmFilterDelete')}
          </Button>
        </div>
      }
    >
      <div className="space-y-5 text-sm">
        <p className="surface-muted">{t('admin.promptAudit.events.filterDeleteDialogDesc')}</p>

        <fieldset>
          <legend className="text-xs font-medium surface-muted">{t('admin.promptAudit.events.filterTimeRange')}</legend>
          <div
            className="mt-2 flex flex-wrap gap-2"
            role="radiogroup"
            aria-label={t('admin.promptAudit.events.filterTimeRange')}
          >
            {DELETE_RANGE_PRESETS.map((option) => (
              <label key={option.id} className="cursor-pointer">
                <input
                  type="radio"
                  name="prompt-delete-range"
                  value={option.id}
                  checked={preset === option.id}
                  className="peer sr-only"
                  data-test={`range-preset-${option.id}`}
                  onChange={() => {
                    setPreset(option.id)
                    criteriaChanged()
                  }}
                />
                <span className="inline-flex items-center rounded-full border border-gray-200 px-3 py-1.5 text-xs font-medium transition-colors peer-checked:border-red-500 peer-checked:bg-red-50 peer-checked:text-red-700 peer-focus-visible:ring-2 peer-focus-visible:ring-red-500/30 dark:border-dark-600 dark:peer-checked:bg-red-950/40 dark:peer-checked:text-red-300 surface-muted">
                  {t(`admin.promptAudit.events.timePresets.${option.id}`)}
                </span>
              </label>
            ))}
          </div>
          <p className="mt-2 text-xs surface-muted">{t('admin.promptAudit.events.filterTimeRangeHint')}</p>
          {preset === 'custom' && (
            <div className="mt-3 grid gap-3 sm:grid-cols-2" data-test="custom-range">
              <label className="text-xs surface-muted">
                <span>{t('admin.promptAudit.events.startAt')}</span>
                <DateTimePicker
                  value={local.start_at}
                  type="datetime-local"
                  className="input mt-1 w-full"
                  ariaLabel={t('admin.promptAudit.events.startAt')}
                  onValueChange={(value) => {
                    setLocal((previous) => ({ ...previous, start_at: value }))
                    criteriaChanged()
                  }}
                />
              </label>
              <label className="text-xs surface-muted">
                <span>{t('admin.promptAudit.events.endAt')}</span>
                <DateTimePicker
                  value={local.end_at}
                  type="datetime-local"
                  className="input mt-1 w-full"
                  ariaLabel={t('admin.promptAudit.events.endAt')}
                  onValueChange={(value) => {
                    setLocal((previous) => ({ ...previous, end_at: value }))
                    criteriaChanged()
                  }}
                />
              </label>
              {!canPreview && (
                <p className="text-xs text-red-600 dark:text-red-400 sm:col-span-2">
                  {t('admin.promptAudit.events.customRangeInvalid')}
                </p>
              )}
            </div>
          )}
        </fieldset>

        <div className="grid gap-3 sm:grid-cols-2">
          <label className="text-xs surface-muted">
            <span>{t('admin.promptAudit.events.decision')}</span>
            <Select
              value={local.decision}
              className="mt-1 w-full"
              ariaLabel={t('admin.promptAudit.events.decision')}
              searchable={false}
              options={decisionOptions}
              onValueChange={(next) => {
                const value = String(next ?? '')
                setLocal((previous) => ({ ...previous, decision: value }))
                criteriaChanged()
              }}
            />
          </label>
          <label className="text-xs surface-muted">
            <span>{t('admin.promptAudit.events.risk')}</span>
            <Select
              value={local.risk_level}
              className="mt-1 w-full"
              ariaLabel={t('admin.promptAudit.events.risk')}
              searchable={false}
              options={riskOptions}
              onValueChange={(next) => {
                const value = String(next ?? '')
                setLocal((previous) => ({ ...previous, risk_level: value }))
                criteriaChanged()
              }}
            />
          </label>
        </div>

        <details
          className="rounded-xl border border-gray-200 px-4 py-3 dark:border-dark-700/60"
          data-test="more-conditions"
        >
          <summary className="cursor-pointer select-none text-xs font-medium surface-muted">
            {t('admin.promptAudit.events.moreConditions')}
          </summary>
          <div className="mt-3 grid gap-3 sm:grid-cols-2">
            <label className="text-xs surface-muted">
              <span>{t('admin.promptAudit.events.endpoint')}</span>
              <input
                value={local.endpoint}
                type="text"
                className="input mt-1 w-full"
                aria-label={t('admin.promptAudit.events.endpoint')}
                onChange={(event) => {
                  setLocal((previous) => ({ ...previous, endpoint: event.target.value }))
                  criteriaChanged()
                }}
              />
            </label>
            <label className="text-xs surface-muted">
              <span>{t('admin.promptAudit.events.keyword')}</span>
              <input
                value={local.keyword}
                type="text"
                className="input mt-1 w-full"
                aria-label={t('admin.promptAudit.events.keyword')}
                onChange={(event) => {
                  setLocal((previous) => ({ ...previous, keyword: event.target.value }))
                  criteriaChanged()
                }}
              />
            </label>
            <label className="text-xs surface-muted">
              <span>{t('admin.promptAudit.events.groupId')}</span>
              <input
                value={local.group_id}
                type="number"
                className="input mt-1 w-full"
                aria-label={t('admin.promptAudit.events.groupId')}
                onChange={(event) => {
                  setLocal((previous) => ({ ...previous, group_id: event.target.value }))
                  criteriaChanged()
                }}
              />
            </label>
            <label className="text-xs surface-muted">
              <span>{t('admin.promptAudit.events.userId')}</span>
              <input
                value={local.user_id}
                type="number"
                className="input mt-1 w-full"
                aria-label={t('admin.promptAudit.events.userId')}
                onChange={(event) => {
                  setLocal((previous) => ({ ...previous, user_id: event.target.value }))
                  criteriaChanged()
                }}
              />
            </label>
          </div>
        </details>

        {preview ? (
          <div
            className="rounded-xl border border-red-200 bg-red-50/60 px-4 py-3 dark:border-red-900/60 dark:bg-red-950/20"
            data-test="delete-preview-result"
          >
            <p className="text-sm font-semibold text-red-700 dark:text-red-300">
              {t('admin.promptAudit.events.filterDeleteCount', { count: preview.matched_count })}
            </p>
            <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs surface-muted">
              <dt>{t('admin.promptAudit.events.snapshotMax')}</dt>
              <dd>{preview.snapshot_max_id}</dd>
              <dt>Filter SHA-256</dt>
              <dd className="break-all font-mono">{preview.filter_hash}</dd>
              <dt>{t('admin.promptAudit.events.expiresAt')}</dt>
              <dd>{formatDate(preview.expires_at)}</dd>
            </dl>
            <p className="mt-2 rounded-lg bg-amber-50/[0.15] px-3 py-2 text-xs text-amber-800 dark:bg-amber-950/30 dark:text-amber-200">
              {t('admin.promptAudit.events.filterDeleteWarning')}
            </p>
          </div>
        ) : (
          <p
            className="rounded-xl border border-dashed border-gray-300 px-4 py-3 text-xs dark:border-dark-600 surface-muted"
            data-test="delete-preview-empty"
          >
            {t('admin.promptAudit.events.filterDeleteNeedPreview')}
          </p>
        )}
      </div>
    </Modal>
  )
}

export default FilterDeleteDialog
