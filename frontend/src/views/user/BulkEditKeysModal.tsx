import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { keysAPI } from '@/api'
import { Select, toast } from '@/components/ui'
import { DateTimePicker } from '@/components/auth/DateTimePicker'
import type { CatalogGroup } from '@/api/channels'
import type { ApiKey, Group, UpdateApiKeyRequest } from '@/types'
import { BaseDialog } from './BaseDialog'
import { GroupRateSummary } from './GroupRateSummary'

type SelectedKey = Pick<ApiKey, 'id' | 'name'>
type LimitField = 'quota' | 'rate_limit_5h' | 'rate_limit_1d' | 'rate_limit_7d'
type IPField = 'ip_whitelist' | 'ip_blacklist'
type EditableField = LimitField | IPField | 'group_id' | 'status' | 'expires_at'

const limitFields: Array<{ key: LimitField; label: string }> = [
  { key: 'quota', label: 'keys.quotaAmount' },
  { key: 'rate_limit_5h', label: 'keys.rateLimit5h' },
  { key: 'rate_limit_1d', label: 'keys.rateLimit1d' },
  { key: 'rate_limit_7d', label: 'keys.rateLimit7d' }
]

const ipFields: Array<{ key: IPField; label: string }> = [
  { key: 'ip_whitelist', label: 'keys.ipWhitelist' },
  { key: 'ip_blacklist', label: 'keys.ipBlacklist' }
]

const emptyEnabled = (): Record<EditableField, boolean> => ({
  group_id: false,
  status: false,
  quota: false,
  rate_limit_5h: false,
  rate_limit_1d: false,
  rate_limit_7d: false,
  expires_at: false,
  ip_whitelist: false,
  ip_blacklist: false
})

export interface BulkEditKeysModalProps {
  show: boolean
  selectedKeys: SelectedKey[]
  groups: Group[]
  quotedGroups?: CatalogGroup[]
  quotesLoading?: boolean
  quotesFailed?: boolean
  onClose?: () => void
  onUpdated?: (succeededIds: number[]) => void
  onRetryQuotes?: () => void
}

export function BulkEditKeysModal({
  show,
  selectedKeys,
  groups,
  quotedGroups,
  quotesLoading = false,
  quotesFailed = false,
  onClose,
  onUpdated,
  onRetryQuotes
}: BulkEditKeysModalProps) {
  const { t } = useTranslation()
  const quoteFor = (id: number) => quotedGroups?.find((group) => group.id === id)

  const [submitting, setSubmitting] = useState(false)
  const [pendingKeys, setPendingKeys] = useState<SelectedKey[]>([])
  const [failures, setFailures] = useState<Array<{ id: number; name: string; message: string }>>([])
  const [enabled, setEnabled] = useState<Record<EditableField, boolean>>(emptyEnabled)
  const [groupId, setGroupId] = useState<number | null>(null)
  const [status, setStatus] = useState<'active' | 'inactive'>('active')
  const [limits, setLimits] = useState<Record<LimitField, string | number>>({
    quota: '',
    rate_limit_5h: '',
    rate_limit_1d: '',
    rate_limit_7d: ''
  })
  const [ipLists, setIpLists] = useState<Record<IPField, string>>({ ip_whitelist: '', ip_blacklist: '' })
  const [neverExpires, setNeverExpires] = useState(false)
  const [expirationDate, setExpirationDate] = useState('')

  useEffect(() => {
    if (!show) return
    setPendingKeys(selectedKeys.map(({ id, name }) => ({ id, name })))
    setFailures([])
    setEnabled(emptyEnabled())
    setLimits({ quota: '', rate_limit_5h: '', rate_limit_1d: '', rate_limit_7d: '' })
    setIpLists({ ip_whitelist: '', ip_blacklist: '' })
    setGroupId(null)
    setStatus('active')
    setNeverExpires(false)
    setExpirationDate('')
  }, [show, selectedKeys])

  const groupOptions = groups.map((group) => ({ value: group.id, label: group.name }))
  const statusOptions = [
    { value: 'active', label: t('keys.enable') },
    { value: 'inactive', label: t('keys.disable') }
  ]

  const validationError = (() => {
    if (enabled.group_id && !groups.some((group) => group.id === groupId)) {
      return t('keys.groupRequired')
    }
    for (const { key } of limitFields) {
      if (!enabled[key]) continue
      const value = String(limits[key]).trim()
      if (!value || !Number.isFinite(Number(value)) || Number(value) < 0) {
        return t('keys.bulkEdit.invalidLimit')
      }
    }
    if (enabled.expires_at && !neverExpires && !Number.isFinite(Date.parse(expirationDate))) {
      return t('keys.bulkEdit.invalidExpiration')
    }
    return ''
  })()

  const canSubmit = pendingKeys.length > 0 && Object.values(enabled).some(Boolean) && !validationError && !submitting

  const close = () => {
    if (!submitting) onClose?.()
  }

  const errorMessage = (error: unknown): string => {
    const message = (error as { message?: unknown } | null)?.message
    return typeof message === 'string' && message ? message : t('keys.failedToSave')
  }

  const submit = async () => {
    if (!canSubmit) return
    const updates: UpdateApiKeyRequest = {}
    if (enabled.group_id) updates.group_id = groupId
    if (enabled.status) updates.status = status
    for (const { key } of limitFields) {
      if (enabled[key]) updates[key] = Number(limits[key])
    }
    for (const { key } of ipFields) {
      if (enabled[key]) updates[key] = ipLists[key].split('\n').map((ip) => ip.trim()).filter(Boolean)
    }
    if (enabled.expires_at) {
      updates.expires_at = neverExpires ? '' : new Date(expirationDate).toISOString()
    }

    setSubmitting(true)
    try {
      const result = await keysAPI.bulkUpdate(
        pendingKeys.map((key) => key.id),
        updates
      )
      setFailures(
        result.failures.map(({ id, error }) => ({
          id,
          name: pendingKeys.find((key) => key.id === id)?.name ?? '',
          message: errorMessage(error)
        }))
      )
      const failedIds = new Set(result.failures.map(({ id }) => id))
      setPendingKeys((previous) => previous.filter((key) => failedIds.has(key.id)))
      if (result.succeededIds.length) onUpdated?.(result.succeededIds)
      if (result.failures.length) {
        toast.error(
          t('keys.bulkEdit.partialFailure', { success: result.succeededIds.length, failed: result.failures.length })
        )
      } else {
        toast.success(t('keys.bulkEdit.success', { count: result.succeededIds.length }))
        onClose?.()
      }
    } catch (error) {
      toast.error(errorMessage(error))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <BaseDialog
      show={show}
      title={t('keys.bulkEdit.title')}
      width="normal"
      closeOnEscape={!submitting}
      showCloseButton={!submitting}
      onClose={close}
      footer={
        <>
          <button type="button" className="btn btn-secondary" disabled={submitting} onClick={close}>
            {t('common.cancel')}
          </button>
          <button
            type="submit"
            form="bulk-edit-keys-form"
            className="btn btn-primary"
            disabled={!canSubmit}
            data-test="submit"
          >
            {submitting ? t('keys.saving') : t('keys.bulkEdit.apply', { count: pendingKeys.length })}
          </button>
        </>
      }
    >
      <form
        id="bulk-edit-keys-form"
        className="space-y-5"
        onSubmit={(event) => {
          event.preventDefault()
          void submit()
        }}
      >
        <div className="space-y-1 text-sm">
          <p className="font-medium text-gray-900 dark:text-white">{t('keys.bulkEdit.selectedCount', { count: pendingKeys.length })}</p>
          <p className="surface-muted">{t('keys.bulkEdit.hint')}</p>
        </div>

        <fieldset disabled={submitting} className="space-y-5">
          <div className="space-y-2">
            <label className="flex items-center gap-2 text-sm font-medium">
              <input
                checked={enabled.group_id}
                onChange={(event) => setEnabled((previous) => ({ ...previous, group_id: event.target.checked }))}
                type="checkbox"
                className="checkbox"
                data-test="enable-group"
              />
              {t('keys.groupLabel')}
            </label>
            {enabled.group_id && (
              <Select
                value={groupId}
                options={groupOptions}
                placeholder={t('keys.selectGroup')}
                disabled={submitting}
                ariaLabel={t('keys.groupLabel')}
                searchable
                data-test="group-input"
                onValueChange={(value) => setGroupId(value === null ? null : Number(value))}
                renderOption={(option) => (
                  <div className="min-w-0 space-y-1">
                    <strong>{String(option.label)}</strong>
                    <GroupRateSummary group={quoteFor(Number(option.value))} loading={quotesLoading} failed={quotesFailed} compact />
                  </div>
                )}
              />
            )}
            {enabled.group_id && groupId && (
              <GroupRateSummary group={quoteFor(groupId)} loading={quotesLoading} failed={quotesFailed} onRetry={onRetryQuotes} />
            )}
          </div>

          <div className="space-y-2">
            <label className="flex items-center gap-2 text-sm font-medium">
              <input
                checked={enabled.status}
                onChange={(event) => setEnabled((previous) => ({ ...previous, status: event.target.checked }))}
                type="checkbox"
                className="checkbox"
                data-test="enable-status"
              />
              {t('keys.statusLabel')}
            </label>
            {enabled.status && (
              <Select
                value={status}
                options={statusOptions}
                disabled={submitting}
                ariaLabel={t('keys.statusLabel')}
                data-test="status-input"
                onValueChange={(value) => setStatus(value === 'inactive' ? 'inactive' : 'active')}
              />
            )}
          </div>

          {limitFields.map((field) => (
            <div key={field.key} className="space-y-2">
              <label className="flex items-center gap-2 text-sm font-medium">
                <input
                  checked={enabled[field.key]}
                  onChange={(event) => setEnabled((previous) => ({ ...previous, [field.key]: event.target.checked }))}
                  type="checkbox"
                  className="checkbox"
                  data-test={`enable-${field.key}`}
                />
                {t(field.label)}
              </label>
              {enabled[field.key] && (
                <div>
                  <input
                    value={limits[field.key]}
                    onChange={(event) => setLimits((previous) => ({ ...previous, [field.key]: event.target.value }))}
                    type="number"
                    min="0"
                    step="any"
                    required
                    className="input"
                    aria-label={t(field.label)}
                    data-test={`${field.key}-input`}
                  />
                  <p className="input-hint">{t('keys.bulkEdit.limitHint')}</p>
                </div>
              )}
            </div>
          ))}

          <div className="space-y-2">
            <label className="flex items-center gap-2 text-sm font-medium">
              <input
                checked={enabled.expires_at}
                onChange={(event) => setEnabled((previous) => ({ ...previous, expires_at: event.target.checked }))}
                type="checkbox"
                className="checkbox"
                data-test="enable-expiration"
              />
              {t('keys.expiration')}
            </label>
            {enabled.expires_at && (
              <div className="space-y-2">
                <label className="flex items-center gap-2 text-sm">
                  <input
                    checked={neverExpires}
                    onChange={(event) => setNeverExpires(event.target.checked)}
                    type="checkbox"
                    className="checkbox"
                    data-test="never-expires"
                  />
                  {t('keys.noExpiration')}
                </label>
                {!neverExpires && (
                  <DateTimePicker
                    value={expirationDate}
                    onValueChange={setExpirationDate}
                    type="datetime-local"
                    required
                    className="input"
                    aria-label={t('keys.expirationDate')}
                  />
                )}
              </div>
            )}
          </div>

          {ipFields.map((field) => (
            <div key={field.key} className="space-y-2">
              <label className="flex items-center gap-2 text-sm font-medium">
                <input
                  checked={enabled[field.key]}
                  onChange={(event) => setEnabled((previous) => ({ ...previous, [field.key]: event.target.checked }))}
                  type="checkbox"
                  className="checkbox"
                  data-test={`enable-${field.key}`}
                />
                {t(field.label)}
              </label>
              {enabled[field.key] && (
                <div>
                  <textarea
                    value={ipLists[field.key]}
                    onChange={(event) => setIpLists((previous) => ({ ...previous, [field.key]: event.target.value }))}
                    rows={3}
                    className="input font-mono text-sm"
                    aria-label={t(field.label)}
                    data-test={`${field.key}-input`}
                  />
                  <p className="input-hint">{t('keys.bulkEdit.ipHint')}</p>
                </div>
              )}
            </div>
          ))}
        </fieldset>

        {validationError && (
          <p role="alert" className="text-sm text-red-600 dark:text-red-400">
            {validationError}
          </p>
        )}
        {failures.length > 0 && (
          <div role="alert" className="space-y-2 text-sm text-red-600 dark:text-red-400">
            <p>{t('keys.bulkEdit.failureHint')}</p>
            <ul className="max-h-40 space-y-1 overflow-y-auto">
              {failures.map((failure) => (
                <li key={failure.id} className="break-words">
                  #{failure.id} {failure.name}: {failure.message}
                </li>
              ))}
            </ul>
          </div>
        )}
      </form>
    </BaseDialog>
  )
}

export default BulkEditKeysModal
