import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { RefreshCw } from 'lucide-react'
import { adminAPI } from '@/api/admin'
import type { AdminUser, Group, UserAttributeDefinition, UserAttributeValuesMap } from '@/types'
import type { BalanceHistoryItem } from '@/api/admin/users'
import type { AdminAPIKeySummary } from '@/api/admin/apiKeySummary'
import { platformQuotaPlatforms } from '@/api/admin/users'
import { formatDateTime } from '@/utils/format'
import { useAppStore } from '@/stores/app'
import { Button, Input, Modal, Select, TextArea } from '@/components/ui'
import { GroupBadge } from './GroupBadge'
import { GroupOptionItem } from './GroupOptionItem'
import { TotpStepUpDialog, useClipboard, useStepUp, isStepUpBlocked, isStepUpCancelled, stepUpBlockReason } from './stepUp'

// ---------------------------------------------------------------------------
// UserAttributeForm
// ---------------------------------------------------------------------------

interface UserAttributeFormProps {
  userId?: number
  value: UserAttributeValuesMap
  onValueChange: (value: UserAttributeValuesMap) => void
}

export function UserAttributeForm({ userId, value, onValueChange }: UserAttributeFormProps) {
  const [loading, setLoading] = useState(false)
  const [attributes, setAttributes] = useState<UserAttributeDefinition[]>([])
  const [localValues, setLocalValues] = useState<UserAttributeValuesMap>({})

  useEffect(() => {
    const loadAttributes = async () => {
      setLoading(true)
      try {
        setAttributes(await adminAPI.userAttributes.listEnabledDefinitions())
      } catch (error) {
        console.error('Failed to load attributes:', error)
      } finally {
        setLoading(false)
      }
    }
    void loadAttributes()
  }, [])

  useEffect(() => {
    const loadUserValues = async () => {
      if (!userId) {
        setLocalValues({})
        return
      }
      try {
        const values = await adminAPI.userAttributes.getUserAttributeValues(userId)
        const valuesMap: UserAttributeValuesMap = {}
        values.forEach((v) => {
          valuesMap[v.attribute_id] = v.value
        })
        setLocalValues({ ...valuesMap })
        onValueChange({ ...valuesMap })
      } catch (error) {
        console.error('Failed to load user attribute values:', error)
      }
    }
    void loadUserValues()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [userId])

  useEffect(() => {
    if (value && Object.keys(value).length > 0) {
      setLocalValues({ ...value })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(value)])

  const emitChange = (next: UserAttributeValuesMap) => {
    setLocalValues(next)
    onValueChange({ ...next })
  }

  const isOptionSelected = (attrId: number, optionValue: string): boolean => {
    const raw = localValues[attrId]
    if (!raw) return false
    try {
      const arr = JSON.parse(raw)
      return Array.isArray(arr) && arr.includes(optionValue)
    } catch {
      return false
    }
  }

  const toggleMultiSelectOption = (attrId: number, optionValue: string) => {
    let arr: string[] = []
    const raw = localValues[attrId]
    if (raw) {
      try {
        arr = JSON.parse(raw)
        if (!Array.isArray(arr)) arr = []
      } catch {
        arr = []
      }
    }

    const index = arr.indexOf(optionValue)
    if (index > -1) {
      arr.splice(index, 1)
    } else {
      arr.push(optionValue)
    }

    emitChange({ ...localValues, [attrId]: JSON.stringify(arr) })
  }

  if (attributes.length > 0) {
    return (
      <div className="space-y-4">
        {attributes.map((attr) => (
          <div key={attr.id}>
            <label className="input-label">
              {attr.name}
              {attr.required && <span className="text-red-500">*</span>}
            </label>

            {attr.type === 'text' || attr.type === 'email' || attr.type === 'url' ? (
              <Input
                type={attr.type === 'text' ? 'text' : attr.type}
                required={attr.required}
                placeholder={attr.placeholder}
                value={localValues[attr.id] ?? ''}
                onValueChange={(next) => emitChange({ ...localValues, [attr.id]: next })}
              />
            ) : attr.type === 'number' ? (
              <Input
                type="number"
                required={attr.required}
                placeholder={attr.placeholder}
                min={attr.validation?.min}
                max={attr.validation?.max}
                value={localValues[attr.id] ?? ''}
                onValueChange={(next) => emitChange({ ...localValues, [attr.id]: next })}
              />
            ) : attr.type === 'date' ? (
              <Input
                type="date"
                aria-label={attr.name}
                required={attr.required}
                value={localValues[attr.id] ?? ''}
                onValueChange={(next) => emitChange({ ...localValues, [attr.id]: next })}
              />
            ) : attr.type === 'textarea' ? (
              <TextArea
                required={attr.required}
                placeholder={attr.placeholder}
                rows={3}
                value={localValues[attr.id] ?? ''}
                onValueChange={(next) => emitChange({ ...localValues, [attr.id]: next })}
              />
            ) : attr.type === 'select' ? (
              <Select
                value={localValues[attr.id] ?? ''}
                options={attr.options || []}
                onValueChange={(next) => emitChange({ ...localValues, [attr.id]: String(next ?? '') })}
              />
            ) : attr.type === 'multi_select' ? (
              <div className="space-y-2">
                {(attr.options || []).map((opt) => (
                  <label key={opt.value} className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      value={opt.value}
                      checked={isOptionSelected(attr.id, opt.value)}
                      onChange={() => toggleMultiSelectOption(attr.id, opt.value)}
                      className="h-4 w-4 rounded border-gray-300 text-primary-600"
                    />
                    <span className="text-sm surface-muted">{opt.label}</span>
                  </label>
                ))}
              </div>
            ) : null}

            {attr.description && <p className="input-hint">{attr.description}</p>}
          </div>
        ))}
      </div>
    )
  }

  if (loading) {
    return (
      <div className="flex justify-center py-4">
        <svg className="h-5 w-5 animate-spin text-gray-400" fill="none" viewBox="0 0 24 24">
          <circle opacity="0.25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
          <path opacity="0.75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
        </svg>
      </div>
    )
  }

  return null
}

// ---------------------------------------------------------------------------
// UserCreateModal
// ---------------------------------------------------------------------------

const RANDOM_PASSWORD_CHARS = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%^&*'

function generateRandomPassword(): string {
  let p = ''
  for (let i = 0; i < 16; i += 1) {
    p += RANDOM_PASSWORD_CHARS.charAt(Math.floor(Math.random() * RANDOM_PASSWORD_CHARS.length))
  }
  return p
}

interface CreateFormState {
  email: string
  password: string
  username: string
  notes: string
  role: 'user' | 'admin'
  balance: string
  concurrency: number
  rpm_limit: number
}

const EMPTY_CREATE_FORM: CreateFormState = {
  email: '',
  password: '',
  username: '',
  notes: '',
  role: 'user',
  balance: '',
  concurrency: 1,
  rpm_limit: 0
}

export function UserCreateModal({
  show,
  onClose,
  onSuccess
}: {
  show: boolean
  onClose: () => void
  onSuccess: () => void
}) {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)
  const [form, setForm] = useState<CreateFormState>(EMPTY_CREATE_FORM)
  const [loading, setLoading] = useState(false)
  const stepUp = useStepUp()

  useEffect(() => {
    if (show) setForm(EMPTY_CREATE_FORM)
  }, [show])

  const submit = async () => {
    if (loading) return
    setLoading(true)
    try {
      const { balance: rawBalance, ...rest } = { ...form }
      const balance = String(rawBalance).trim()
      const payload: typeof rest & { balance?: number } = { ...rest }
      if (balance !== '') {
        payload.balance = Number(balance)
      }
      // 创建管理员属敏感操作：后端返回 STEP_UP_REQUIRED 时弹 TOTP 验证并重试
      await stepUp.run(() => adminAPI.users.create(payload))
      showSuccess(t('admin.users.userCreated'))
      onSuccess()
      onClose()
    } catch (e) {
      if (isStepUpCancelled(e)) {
        // 用户主动取消二次验证：静默返回，表单保持打开。
      } else if (isStepUpBlocked(e)) {
        showError(
          stepUpBlockReason(e) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
            ? t('stepUp.adminApiKeyForbidden')
            : t('stepUp.notEnabled')
        )
      } else {
        showError((e as { message?: string })?.message || t('admin.users.failedToCreate'))
      }
    } finally {
      setLoading(false)
    }
  }

  return (
    <>
      <Modal
        open={show}
        onClose={onClose}
        title={t('admin.users.createUser')}
        footer={
          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" loading={loading} onClick={submit}>
              {loading ? t('admin.users.creating') : t('common.create')}
            </Button>
          </div>
        }
      >
        <div className="space-y-5">
          <div>
            <label className="input-label">{t('admin.users.email')}</label>
            <Input
              type="email"
              required
              value={form.email}
              onValueChange={(value) => setForm((prev) => ({ ...prev, email: value }))}
              placeholder={t('admin.users.enterEmail')}
            />
          </div>
          <div>
            <label className="input-label">{t('admin.users.password')}</label>
            <div className="flex gap-2">
              <div className="relative flex-1">
                <Input
                  type="text"
                  required
                  value={form.password}
                  onValueChange={(value) => setForm((prev) => ({ ...prev, password: value }))}
                  placeholder={t('admin.users.enterPassword')}
                />
              </div>
              <Button variant="secondary" className="px-3" onClick={() => setForm((prev) => ({ ...prev, password: generateRandomPassword() }))}>
                <RefreshCw className="h-4 w-4" />
              </Button>
            </div>
          </div>
          <div>
            <label className="input-label">{t('admin.users.username')}</label>
            <Input
              type="text"
              value={form.username}
              onValueChange={(value) => setForm((prev) => ({ ...prev, username: value }))}
              placeholder={t('admin.users.enterUsername')}
            />
          </div>
          <div>
            <label className="input-label">{t('admin.users.form.roleLabel')}</label>
            <Select
              searchable={false}
              value={form.role}
              options={[
                { value: 'user', label: t('admin.users.roles.user') },
                { value: 'admin', label: t('admin.users.roles.admin') }
              ]}
              onValueChange={(value) => setForm((prev) => ({ ...prev, role: String(value) as 'user' | 'admin' }))}
            />
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div>
              <label className="input-label">{t('admin.users.columns.balance')}</label>
              <Input
                type="number"
                step="any"
                value={form.balance}
                onValueChange={(value) => setForm((prev) => ({ ...prev, balance: value }))}
              />
            </div>
            <div>
              <label className="input-label">{t('admin.users.columns.concurrency')}</label>
              <Input
                type="number"
                value={form.concurrency}
                onValueChange={(value) => setForm((prev) => ({ ...prev, concurrency: Number(value) }))}
              />
            </div>
          </div>
          <div>
            <label className="input-label">{t('admin.users.form.rpmLimit')}</label>
            <Input
              type="number"
              min="0"
              step="1"
              value={form.rpm_limit}
              onValueChange={(value) => setForm((prev) => ({ ...prev, rpm_limit: Number(value) }))}
              placeholder={t('admin.users.form.rpmLimitPlaceholder')}
            />
            <p className="input-hint">{t('admin.users.form.rpmLimitHint')}</p>
          </div>
        </div>
      </Modal>
      <TotpStepUpDialog controller={stepUp} />
    </>
  )
}

// ---------------------------------------------------------------------------
// UserEditModal
// ---------------------------------------------------------------------------

interface EditFormState {
  email: string
  password: string
  username: string
  notes: string
  role: AdminUser['role']
  concurrency: number
  rpm_limit: number
  customAttributes: UserAttributeValuesMap
}

export function UserEditModal({
  show,
  user,
  onClose,
  onSuccess
}: {
  show: boolean
  user: AdminUser | null
  onClose: () => void
  onSuccess: () => void
}) {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)
  const { copied, copyToClipboard } = useClipboard()
  const stepUp = useStepUp()
  const [submitting, setSubmitting] = useState(false)
  const [form, setForm] = useState<EditFormState>({
    email: '',
    password: '',
    username: '',
    notes: '',
    role: 'user',
    concurrency: 1,
    rpm_limit: 0,
    customAttributes: {}
  })

  useEffect(() => {
    if (!user) return
    setForm({
      email: user.email,
      password: '',
      username: user.username || '',
      notes: user.notes || '',
      role: user.role || 'user',
      concurrency: user.concurrency,
      rpm_limit: user.rpm_limit ?? 0,
      customAttributes: {}
    })
  }, [user])

  const roleOptions = useMemo(
    () => [
      { value: 'user', label: t('admin.users.roles.user') },
      { value: 'admin', label: t('admin.users.roles.admin') }
    ],
    [t]
  )

  const handleUpdateUser = async () => {
    if (!user) return
    if (!form.email.trim()) {
      showError(t('admin.users.emailRequired'))
      return
    }
    // 0 = 不限制，与网关 (AcquireUserSlot: maxConcurrency <= 0) 和批量改限额一致
    if (!Number.isInteger(form.concurrency) || form.concurrency < 0) {
      showError(t('admin.users.concurrencyNonNegative'))
      return
    }
    const userId = user.id
    setSubmitting(true)
    try {
      const data: Record<string, unknown> = {
        email: form.email,
        username: form.username,
        notes: form.notes,
        role: form.role,
        concurrency: form.concurrency,
        rpm_limit: form.rpm_limit
      }
      if (form.password.trim()) data.password = form.password.trim()
      // 提升为管理员属敏感操作：后端返回 STEP_UP_REQUIRED 时弹 TOTP 验证并重试
      await stepUp.run(() => adminAPI.users.update(userId, data))
      if (Object.keys(form.customAttributes).length > 0) {
        await adminAPI.userAttributes.updateUserAttributeValues(userId, form.customAttributes)
      }
      showSuccess(t('admin.users.userUpdated'))
      onSuccess()
      onClose()
    } catch (e) {
      if (isStepUpCancelled(e)) {
        // 用户主动取消二次验证：静默返回，表单保持打开。
      } else if (isStepUpBlocked(e)) {
        showError(
          stepUpBlockReason(e) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
            ? t('stepUp.adminApiKeyForbidden')
            : t('stepUp.notEnabled')
        )
      } else {
        showError((e as { message?: string })?.message || t('admin.users.failedToUpdate'))
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <>
      <Modal
        open={show}
        onClose={onClose}
        title={t('admin.users.editUser')}
        footer={
          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" loading={submitting} onClick={handleUpdateUser}>
              {submitting ? t('admin.users.updating') : t('common.update')}
            </Button>
          </div>
        }
      >
        {user && (
          <div className="space-y-5">
            <div>
              <label className="input-label">{t('admin.users.email')}</label>
              <Input
                type="email"
                value={form.email}
                onValueChange={(value) => setForm((prev) => ({ ...prev, email: value }))}
              />
            </div>
            <div>
              <label className="input-label">{t('admin.users.password')}</label>
              <div className="flex gap-2">
                <div className="relative flex-1">
                  <Input
                    type="text"
                    value={form.password}
                    onValueChange={(value) => setForm((prev) => ({ ...prev, password: value }))}
                    placeholder={t('admin.users.enterNewPassword')}
                    suffix={
                      form.password ? (
                        <button
                          type="button"
                          onClick={() => {
                            if (form.password) {
                              void copyToClipboard(form.password, t('admin.users.passwordCopied'))
                            }
                          }}
                          className={`rounded-lg p-1 transition-colors surface-control ${copied ? 'text-green-500' : 'text-gray-400'}`}
                        >
                          {copied ? (
                            <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="2"><path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" /></svg>
                          ) : (
                            <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="1.5"><path strokeLinecap="round" strokeLinejoin="round" d="M15.666 3.888A2.25 2.25 0 0013.5 2.25h-3c-1.03 0-1.9.693-2.166 1.638m7.332 0c.055.194.084.4.084.612v0a.75.75 0 01-.75.75H9a.75.75 0 01-.75-.75v0c0-.212.03-.418.084-.612m7.332 0c.646.049 1.288.11 1.927.184 1.1.128 1.907 1.077 1.907 2.185V19.5a2.25 2.25 0 01-2.25 2.25H6.75A2.25 2.25 0 014.5 19.5V6.257c0-1.108.806-2.057 1.907-2.185a48.208 48.208 0 011.927-.184" /></svg>
                          )}
                        </button>
                      ) : null
                    }
                  />
                </div>
                <Button variant="secondary" className="px-3" onClick={() => setForm((prev) => ({ ...prev, password: generateRandomPassword() }))}>
                  <RefreshCw className="h-4 w-4" />
                </Button>
              </div>
            </div>
            <div>
              <label className="input-label">{t('admin.users.username')}</label>
              <Input
                type="text"
                value={form.username}
                onValueChange={(value) => setForm((prev) => ({ ...prev, username: value }))}
              />
            </div>
            <div>
              <label className="input-label">{t('admin.users.form.roleLabel')}</label>
              <Select searchable={false} value={form.role} options={roleOptions} onValueChange={(value) => setForm((prev) => ({ ...prev, role: String(value) as AdminUser['role'] }))} />
            </div>
            <div>
              <label className="input-label">{t('admin.users.notes')}</label>
              <TextArea rows={3} value={form.notes} onValueChange={(value) => setForm((prev) => ({ ...prev, notes: value }))} />
            </div>
            <div>
              <label className="input-label">{t('admin.users.columns.concurrency')}</label>
              <Input
                type="number"
                min="0"
                step="1"
                value={form.concurrency}
                onValueChange={(value) => setForm((prev) => ({ ...prev, concurrency: Number(value) }))}
                placeholder={t('admin.users.form.concurrencyPlaceholder')}
                data-test="concurrency-input"
              />
              <p className="input-hint">{t('admin.users.form.concurrencyHint')}</p>
            </div>
            <div>
              <label className="input-label">{t('admin.users.form.rpmLimit')}</label>
              <Input
                type="number"
                min="0"
                step="1"
                value={form.rpm_limit}
                onValueChange={(value) => setForm((prev) => ({ ...prev, rpm_limit: Number(value) }))}
                placeholder={t('admin.users.form.rpmLimitPlaceholder')}
              />
              <p className="input-hint">{t('admin.users.form.rpmLimitHint')}</p>
            </div>
            <UserAttributeForm
              userId={user.id}
              value={form.customAttributes}
              onValueChange={(customAttributes) => setForm((prev) => ({ ...prev, customAttributes }))}
            />
          </div>
        )}
      </Modal>
      <TotpStepUpDialog controller={stepUp} />
    </>
  )
}

// ---------------------------------------------------------------------------
// BulkEditUserModal
// ---------------------------------------------------------------------------

const MAX_BATCH_USER_IDS = 500

function parseLimit(value: string | number): number | null | undefined {
  const trimmed = String(value).trim()
  if (!trimmed) return undefined
  const parsed = Number(trimmed)
  if (!Number.isInteger(parsed) || parsed < 0) return null
  return parsed
}

export function BulkEditUserModal({
  show,
  selectedIds,
  onClose,
  onSuccess
}: {
  show: boolean
  selectedIds: number[]
  onClose: () => void
  onSuccess: () => void
}) {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)

  const [enableConcurrency, setEnableConcurrency] = useState(false)
  const [enableRPMLimit, setEnableRPMLimit] = useState(false)
  const [concurrencyValue, setConcurrencyValue] = useState<string | number>('')
  const [rpmLimitValue, setRpmLimitValue] = useState<string | number>('')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (show) {
      setEnableConcurrency(false)
      setEnableRPMLimit(false)
      setConcurrencyValue('')
      setRpmLimitValue('')
      setSubmitting(false)
    }
  }, [show])

  const parsedConcurrency = enableConcurrency ? parseLimit(concurrencyValue) : undefined
  const parsedRPMLimit = enableRPMLimit ? parseLimit(rpmLimitValue) : undefined
  const hasInvalidValue = parsedConcurrency === null || parsedRPMLimit === null
  const hasUpdate =
    (parsedConcurrency !== undefined && parsedConcurrency !== null) ||
    (parsedRPMLimit !== undefined && parsedRPMLimit !== null)
  const selectionTooLarge = selectedIds.length > MAX_BATCH_USER_IDS
  const canSubmit =
    selectedIds.length > 0 && !selectionTooLarge && hasUpdate && !hasInvalidValue && !submitting

  const handleSubmit = async () => {
    if (!canSubmit) return

    const request: Record<string, unknown> = { user_ids: [...selectedIds], all: false }
    const fields: string[] = []
    if (parsedConcurrency !== undefined && parsedConcurrency !== null) {
      request.concurrency = parsedConcurrency
      fields.push(t('admin.users.bulkLimits.concurrencyValue', { value: parsedConcurrency }))
    }
    if (parsedRPMLimit !== undefined && parsedRPMLimit !== null) {
      request.rpm_limit = parsedRPMLimit
      fields.push(
        parsedRPMLimit === 0
          ? t('admin.users.bulkLimits.rpmUnlimitedValue')
          : t('admin.users.bulkLimits.rpmValue', { value: parsedRPMLimit })
      )
    }

    const confirmed = window.confirm(
      t('admin.users.bulkLimits.confirm', {
        count: selectedIds.length,
        fields: fields.join(', ')
      })
    )
    if (!confirmed) return

    setSubmitting(true)
    try {
      const result = await adminAPI.users.batchUpdateLimits(request as never)
      showSuccess(t('admin.users.bulkLimits.success', { count: result.affected }))
      onSuccess()
      onClose()
    } catch (error) {
      showError((error as { message?: string })?.message || t('admin.users.bulkLimits.failed'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open={show}
      onClose={onClose}
      title={t('admin.users.bulkLimits.title')}
      footer={
        <div className="flex justify-end gap-3">
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" disabled={!canSubmit} loading={submitting} onClick={handleSubmit} data-test="submit">
            {submitting ? t('admin.users.bulkLimits.applying') : t('admin.users.bulkLimits.apply')}
          </Button>
        </div>
      }
    >
      <div className="space-y-5">
        <p className="text-sm font-medium surface-muted">
          {t('admin.users.bulkLimits.selectedCount', { count: selectedIds.length })}
        </p>

        <div className="divide-y divide-gray-200 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700">
          <div className="space-y-3 py-4">
            <div className="flex items-center justify-between gap-4">
              <label className="input-label mb-0">{t('admin.users.columns.concurrency')}</label>
              <input
                type="checkbox"
                checked={enableConcurrency}
                onChange={(event) => setEnableConcurrency(event.target.checked)}
                aria-label={t('admin.users.bulkLimits.enableConcurrency')}
                data-test="enable-concurrency"
                className="h-4 w-4 rounded border-gray-300 text-primary-600"
              />
            </div>
            {enableConcurrency && (
              <Input
                type="number"
                min="0"
                step="1"
                value={concurrencyValue}
                onValueChange={setConcurrencyValue}
                data-test="concurrency-input"
              />
            )}
          </div>

          <div className="space-y-3 py-4">
            <div className="flex items-center justify-between gap-4">
              <label className="input-label mb-0">{t('admin.users.form.rpmLimit')}</label>
              <input
                type="checkbox"
                checked={enableRPMLimit}
                onChange={(event) => setEnableRPMLimit(event.target.checked)}
                aria-label={t('admin.users.bulkLimits.enableRPMLimit')}
                data-test="enable-rpm-limit"
                className="h-4 w-4 rounded border-gray-300 text-primary-600"
              />
            </div>
            {enableRPMLimit && (
              <div>
                <Input
                  type="number"
                  min="0"
                  step="1"
                  value={rpmLimitValue}
                  onValueChange={setRpmLimitValue}
                  data-test="rpm-limit-input"
                />
                {parsedRPMLimit === 0 && <p className="input-hint">{t('admin.users.bulkLimits.unlimited')}</p>}
              </div>
            )}
          </div>
        </div>

        {hasInvalidValue && (
          <p className="text-sm text-red-600 dark:text-red-400">{t('admin.users.bulkLimits.nonNegativeInteger')}</p>
        )}
        {selectionTooLarge && (
          <p className="text-sm text-red-600 dark:text-red-400">
            {t('admin.users.bulkLimits.selectionLimit', { max: MAX_BATCH_USER_IDS })}
          </p>
        )}
      </div>
    </Modal>
  )
}

// ---------------------------------------------------------------------------
// UserPlatformQuotaModal
// ---------------------------------------------------------------------------

type PlatformQuotaWindow = 'daily' | 'weekly' | 'monthly'

interface QuotaRow {
  platform: string
  daily_limit_usd: number | null
  weekly_limit_usd: number | null
  monthly_limit_usd: number | null
  daily_usage_usd: number
  weekly_usage_usd: number
  monthly_usage_usd: number
}

function configuredPlatforms(items: Array<{ daily_limit_usd: number | null; weekly_limit_usd: number | null; monthly_limit_usd: number | null; platform: string }>): Set<string> {
  const out = new Set<string>()
  for (const it of items) {
    if (it.daily_limit_usd != null || it.weekly_limit_usd != null || it.monthly_limit_usd != null) {
      out.add(it.platform)
    }
  }
  return out
}

function emptyRow(p: string): QuotaRow {
  return {
    platform: p,
    daily_limit_usd: null,
    weekly_limit_usd: null,
    monthly_limit_usd: null,
    daily_usage_usd: 0,
    weekly_usage_usd: 0,
    monthly_usage_usd: 0
  }
}

function normalizeQuotas(items: Array<Record<string, unknown>>): QuotaRow[] {
  const byPlatform = new Map<string, Record<string, unknown>>()
  for (const it of items) byPlatform.set(String(it.platform), it)
  return platformQuotaPlatforms().map((p) => {
    const it = byPlatform.get(p)
    if (!it) return emptyRow(p)
    return {
      platform: p,
      daily_limit_usd: (it.daily_limit_usd as number | null) ?? null,
      weekly_limit_usd: (it.weekly_limit_usd as number | null) ?? null,
      monthly_limit_usd: (it.monthly_limit_usd as number | null) ?? null,
      daily_usage_usd: (it.daily_usage_usd as number) ?? 0,
      weekly_usage_usd: (it.weekly_usage_usd as number) ?? 0,
      monthly_usage_usd: (it.monthly_usage_usd as number) ?? 0
    }
  })
}

function formatUsage(n: number): string {
  if (n == null || Number.isNaN(n)) return '-'
  return n.toFixed(2)
}

// 仅在合法输入下返回数字：null/undefined/NaN/±Inf/负数 → null（视为"无限额"）。
function normalizeLimit(v: number | null | undefined): number | null {
  if (v === null || v === undefined) return null
  if (typeof v === 'number' && Number.isFinite(v) && v >= 0) return v
  return null
}

export function UserPlatformQuotaModal({
  show,
  user,
  onClose,
  onSuccess
}: {
  show: boolean
  user: AdminUser | null
  onClose: () => void
  onSuccess: () => void
}) {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)

  const [loading, setLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [resetting, setResetting] = useState<Record<string, boolean>>({})
  const [quotas, setQuotas] = useState<QuotaRow[]>([])
  // 已保存且至少配置了一档限额的平台。只有这些平台在后端有配额记录，重置用量窗口才有对象。
  const [savedConfigured, setSavedConfigured] = useState<Set<string>>(new Set())

  const hasActiveSubscription = user?.subscriptions?.some((s) => s.status === 'active') ?? false

  useEffect(() => {
    const load = async () => {
      if (!user) return
      setLoading(true)
      try {
        const data = await adminAPI.users.getPlatformQuotas(user.id)
        const items = (data.platform_quotas ?? []) as unknown as Array<Record<string, unknown>>
        setQuotas(normalizeQuotas(items))
        setSavedConfigured(configuredPlatforms(items as never))
      } catch {
        showError(t('admin.users.platformQuota.loadFailed'))
        setQuotas(platformQuotaPlatforms().map(emptyRow))
        setSavedConfigured(new Set())
      } finally {
        setLoading(false)
      }
    }
    if (show && user) void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [show, user])

  const onClearAll = () => {
    // 二次确认：一键清空全部平台的 daily/weekly/monthly 限额属于高风险批量操作，
    // 误点后所有平台变为"无限额"，且本地无 undo 机制（需要逐个手动重填或取消保存）。
    const confirmed = window.confirm(t('admin.users.platformQuota.clearAllConfirm'))
    if (!confirmed) return
    setQuotas((prev) =>
      prev.map((row) => ({ ...row, daily_limit_usd: null, weekly_limit_usd: null, monthly_limit_usd: null }))
    )
  }

  const onSave = async () => {
    if (!user) return
    // 拒绝非法数值，避免 normalizeLimit 将其静默转换为 null（无限额）。
    const invalid: string[] = []
    for (const row of quotas) {
      for (const win of ['daily', 'weekly', 'monthly'] as const) {
        const v = row[`${win}_limit_usd`]
        if (typeof v === 'number' && (!Number.isFinite(v) || v < 0)) {
          invalid.push(`${row.platform}.${win}`)
        }
      }
    }
    if (invalid.length > 0) {
      showError(t('admin.users.platformQuota.invalidNumber', { fields: invalid.join(', ') }))
      return
    }

    setSubmitting(true)
    try {
      const payload = quotas.map((r) => ({
        platform: r.platform,
        daily_limit_usd: normalizeLimit(r.daily_limit_usd),
        weekly_limit_usd: normalizeLimit(r.weekly_limit_usd),
        monthly_limit_usd: normalizeLimit(r.monthly_limit_usd)
      }))
      await adminAPI.users.updatePlatformQuotas(user.id, payload)
      showSuccess(t('admin.users.platformQuota.updateSuccess'))
      onSuccess()
      onClose()
    } catch (e) {
      showError((e as { message?: string })?.message || t('admin.users.platformQuota.updateFailed'))
    } finally {
      setSubmitting(false)
    }
  }

  const onReset = async (platform: string, quotaWindow: PlatformQuotaWindow) => {
    if (!user) return
    const windowLabel = t(
      `admin.users.platformQuota.window${quotaWindow.charAt(0).toUpperCase() + quotaWindow.slice(1)}`
    )
    const confirmed = window.confirm(
      t('admin.users.platformQuota.reset.confirm', { platform, window: windowLabel })
    )
    if (!confirmed) return
    const key = `${platform}.${quotaWindow}`
    setResetting((prev) => ({ ...prev, [key]: true }))
    try {
      const data = await adminAPI.users.resetPlatformQuotaWindow(user.id, platform, quotaWindow)
      const items = (data.platform_quotas ?? []) as unknown as Array<Record<string, unknown>>
      setQuotas(normalizeQuotas(items))
      setSavedConfigured(configuredPlatforms(items as never))
      showSuccess(t('admin.users.platformQuota.reset.success', { platform, window: windowLabel }))
    } catch (e) {
      showError((e as { message?: string })?.message || t('admin.users.platformQuota.reset.failed'))
    } finally {
      setResetting((prev) => ({ ...prev, [key]: false }))
    }
  }

  return (
    <Modal
      open={show}
      onClose={onClose}
      title={t('admin.users.platformQuota.title')}
      size="xl"
      footer={
        <div className="flex justify-end gap-3">
          <Button variant="secondary" onClick={onClose}>
            {t('admin.users.platformQuota.cancel')}
          </Button>
          <Button variant="primary" disabled={submitting || loading} onClick={onSave}>
            {submitting ? t('admin.users.platformQuota.saving') : t('admin.users.platformQuota.save')}
          </Button>
        </div>
      }
    >
      {user && (
        <div className="space-y-4">
          {hasActiveSubscription && (
            <div className="rounded-xl border border-amber-200 bg-amber-50/[0.15] px-4 py-3 text-sm text-amber-700 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200">
              {t('admin.users.platformQuota.subscriptionWarning')}
            </div>
          )}
          <p className="text-sm surface-muted">{t('admin.users.platformQuota.subtitle', { email: user.email })}</p>
          {loading ? (
            <div className="py-10 text-center surface-muted">{t('common.loading')}</div>
          ) : (
            <div className="record-table-scroll record-table-dialog-scroll table-container overflow-x-auto">
              <table className="record-table min-w-full text-sm">
                <thead>
                  <tr className="border-b border-gray-200 dark:border-dark-700 surface-muted">
                    <th className="px-3 py-2 text-left font-medium">{t('admin.users.platformQuota.columns.platform')}</th>
                    <th className="px-3 py-2 text-left font-medium">{t('admin.users.platformQuota.columns.daily')}</th>
                    <th className="px-3 py-2 text-left font-medium">{t('admin.users.platformQuota.columns.weekly')}</th>
                    <th className="px-3 py-2 text-left font-medium">{t('admin.users.platformQuota.columns.monthly')}</th>
                    <th className="px-3 py-2 text-left font-medium">{t('admin.users.platformQuota.columns.usage')}</th>
                  </tr>
                </thead>
                <tbody>
                  {quotas.map((row) => (
                    <tr key={row.platform} className="border-b border-gray-100 dark:border-dark-800">
                      <td className="px-3 py-2 font-mono text-gray-900 dark:text-white">{row.platform}</td>
                      {(['daily', 'weekly', 'monthly'] as const).map((win) => (
                        <td key={win} className="px-3 py-2">
                          <div className="flex items-center gap-1">
                            <Input
                              type="number"
                              min="0"
                              step="0.01"
                              className="w-24"
                              value={row[`${win}_limit_usd`] ?? ''}
                              onValueChange={(value) =>
                                setQuotas((prev) =>
                                  prev.map((entry) =>
                                    entry.platform === row.platform
                                      ? { ...entry, [`${win}_limit_usd`]: value === '' ? null : Number(value) }
                                      : entry
                                  )
                                )
                              }
                              placeholder={t('admin.users.platformQuota.placeholder')}
                            />
                            <button
                              type="button"
                              className="text-xs hover:text-amber-500 disabled:opacity-50 surface-control surface-muted"
                              disabled={!!resetting[`${row.platform}.${win}`] || !savedConfigured.has(row.platform)}
                              title={t(
                                savedConfigured.has(row.platform)
                                  ? 'admin.users.platformQuota.reset.button'
                                  : 'admin.users.platformQuota.reset.unavailable'
                              )}
                              onClick={() => onReset(row.platform, win)}
                            >
                              ↻
                            </button>
                          </div>
                        </td>
                      ))}
                      <td className="px-3 py-2 text-xs surface-muted">
                        {formatUsage(row.daily_usage_usd)} / {formatUsage(row.weekly_usage_usd)} / {formatUsage(row.monthly_usage_usd)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <p className="mt-3 text-xs surface-muted">{t('admin.users.platformQuota.hint')}</p>
              <div className="mt-3">
                <Button variant="secondary" size="sm" onClick={onClearAll}>
                  {t('admin.users.platformQuota.clearAll')}
                </Button>
              </div>
            </div>
          )}
        </div>
      )}
    </Modal>
  )
}

// ---------------------------------------------------------------------------
// UserApiKeysModal
// ---------------------------------------------------------------------------

const DROPDOWN_HEIGHT = 272 // max-h-64 = 16rem = 256px + padding
const DROPDOWN_GAP = 4

export function UserApiKeysModal({
  show,
  user,
  onClose
}: {
  show: boolean
  user: AdminUser | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)

  const [apiKeys, setApiKeys] = useState<AdminAPIKeySummary[]>([])
  const [allGroups, setAllGroups] = useState<Group[]>([])
  const [loading, setLoading] = useState(false)
  const [updatingKeyIds, setUpdatingKeyIds] = useState<Set<number>>(new Set())
  const [groupSelectorKeyId, setGroupSelectorKeyId] = useState<number | null>(null)
  const [dropdownPosition, setDropdownPosition] = useState<{ top: number; left: number } | null>(null)
  const dropdownRef = useRef<HTMLDivElement>(null)
  const groupButtonRefs = useRef<Map<number, HTMLElement>>(new Map())

  const selectedKeyForGroup =
    groupSelectorKeyId === null ? null : apiKeys.find((k) => k.id === groupSelectorKeyId) || null

  useEffect(() => {
    if (!show || !user) {
      setGroupSelectorKeyId(null)
      setDropdownPosition(null)
      return
    }
    groupButtonRefs.current.clear()
    const load = async () => {
      setLoading(true)
      try {
        const res = await adminAPI.users.getUserApiKeys(user.id)
        setApiKeys(res.items || [])
      } catch (error) {
        console.error('Failed to load API keys:', error)
      } finally {
        setLoading(false)
      }
    }
    const loadGroups = async () => {
      try {
        setAllGroups(await adminAPI.groups.getAll())
      } catch (error) {
        console.error('Failed to load groups:', error)
      }
    }
    void load()
    void loadGroups()
  }, [show, user])

  const openGroupSelector = (key: AdminAPIKeySummary) => {
    if (groupSelectorKeyId === key.id) {
      closeGroupSelector()
      return
    }
    const buttonEl = groupButtonRefs.current.get(key.id)
    if (buttonEl) {
      const rect = buttonEl.getBoundingClientRect()
      const spaceBelow = window.innerHeight - rect.bottom
      const openUpward = spaceBelow < DROPDOWN_HEIGHT && rect.top > spaceBelow
      setDropdownPosition({
        top: openUpward ? rect.top - DROPDOWN_HEIGHT - DROPDOWN_GAP : rect.bottom + DROPDOWN_GAP,
        left: rect.left
      })
    }
    setGroupSelectorKeyId(key.id)
  }

  const closeGroupSelector = () => {
    setGroupSelectorKeyId(null)
    setDropdownPosition(null)
  }

  const changeGroup = async (key: AdminAPIKeySummary, newGroupId: number | null) => {
    closeGroupSelector()
    if (updatingKeyIds.has(key.id) || key.group_id === newGroupId || (!key.group_id && newGroupId === null)) return

    setUpdatingKeyIds((prev) => new Set(prev).add(key.id))
    try {
      const result = await adminAPI.apiKeys.updateApiKeyGroup(key.id, newGroupId)
      setApiKeys((prev) => prev.map((entry) => (entry.id === key.id ? result.api_key : entry)))
      if (result.auto_granted_group_access && result.granted_group_name) {
        showSuccess(t('admin.users.groupChangedWithGrant', { group: result.granted_group_name }))
      } else {
        showSuccess(t('admin.users.groupChangedSuccess'))
      }
    } catch (error) {
      showError((error as { message?: string })?.message || t('admin.users.groupChangeFailed'))
    } finally {
      setUpdatingKeyIds((prev) => {
        const next = new Set(prev)
        next.delete(key.id)
        return next
      })
    }
  }

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && groupSelectorKeyId !== null) {
        event.stopPropagation()
        closeGroupSelector()
      }
    }
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as HTMLElement
      if (dropdownRef.current && !dropdownRef.current.contains(target)) {
        for (const el of groupButtonRefs.current.values()) {
          if (el.contains(target)) return
        }
        closeGroupSelector()
      }
    }
    document.addEventListener('click', handleClickOutside)
    document.addEventListener('keydown', handleKeyDown, true)
    return () => {
      document.removeEventListener('click', handleClickOutside)
      document.removeEventListener('keydown', handleKeyDown, true)
    }
  }, [groupSelectorKeyId])

  return (
    <>
      <Modal open={show} onClose={onClose} title={t('admin.users.userApiKeys')} size="xl">
        {user && (
          <div className="space-y-4">
            <div className="flex items-center gap-3 rounded-xl surface-inset p-4">
              <div className="flex h-10 w-10 items-center justify-center rounded-full bg-primary-100/[0.15] dark:bg-primary-900/30">
                <span className="text-lg font-medium text-primary-700 dark:text-primary-300">
                  {user.email.charAt(0).toUpperCase()}
                </span>
              </div>
              <div>
                <p className="font-medium text-gray-900 dark:text-white">{user.email}</p>
                <p className="text-sm surface-muted">{user.username}</p>
              </div>
            </div>
            {loading ? (
              <div className="flex justify-center py-8">
                <svg className="h-8 w-8 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
                  <circle opacity="0.25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                  <path opacity="0.75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
                </svg>
              </div>
            ) : apiKeys.length === 0 ? (
              <div className="py-8 text-center">
                <p className="text-sm surface-muted">{t('admin.users.noApiKeys')}</p>
              </div>
            ) : (
              <div className="max-h-96 space-y-3 overflow-y-auto" onScroll={closeGroupSelector}>
                {apiKeys.map((key) => (
                  <div key={key.id} className="rounded-xl border border-gray-200 surface-inset p-4 dark:border-dark-600">
                    <div className="flex items-start justify-between">
                      <div className="min-w-0 flex-1">
                        <div className="mb-1 flex items-center gap-2">
                          <span className="font-medium text-gray-900 dark:text-white">{key.name}</span>
                          <span className={`badge text-xs ${key.status === 'active' ? 'badge-success' : 'badge-danger'}`}>
                            {t('keys.status.' + key.status)}
                          </span>
                        </div>
                        <p className="text-xs surface-muted">
                          #{key.id} · {t('admin.support.apiKeyConfidentiality')}
                        </p>
                      </div>
                    </div>
                    <div className="mt-3 flex flex-wrap gap-4 text-xs surface-muted">
                      <div className="flex items-center gap-1">
                        <span>{t('admin.users.group')}:</span>
                        <button
                          ref={(element) => {
                            if (element) groupButtonRefs.current.set(key.id, element)
                            else groupButtonRefs.current.delete(key.id)
                          }}
                          onClick={() => openGroupSelector(key)}
                          disabled={updatingKeyIds.has(key.id)}
                          className="-mx-1 -my-0.5 flex cursor-pointer items-center gap-1 rounded-md px-1 py-0.5 transition-colors hover:surface-hover surface-control"
                        >
                          {key.group_id && key.group ? (
                            <GroupBadge
                              name={key.group.name}
                              platform={key.group.platform}
                              subscriptionType={key.group.subscription_type}
                              rateMultiplier={key.group.rate_multiplier}
                              peakRateEnabled={key.group.peak_rate_enabled}
                              peakStart={key.group.peak_start}
                              peakEnd={key.group.peak_end}
                              peakRateMultiplier={key.group.peak_rate_multiplier}
                            />
                          ) : (
                            <span className="italic surface-muted">{t('admin.users.none')}</span>
                          )}
                          {updatingKeyIds.has(key.id) ? (
                            <svg className="h-3 w-3 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
                              <circle opacity="0.25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                              <path opacity="0.75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
                            </svg>
                          ) : (
                            <svg className="h-3 w-3 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="2"><path strokeLinecap="round" strokeLinejoin="round" d="M8.25 15L12 18.75 15.75 15m-7.5-6L12 5.25 15.75 9" /></svg>
                          )}
                        </button>
                      </div>
                      <div className="flex items-center gap-1">
                        <span>
                          {t('admin.users.columns.created')}: {formatDateTime(key.created_at)}
                        </span>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </Modal>

      {groupSelectorKeyId !== null && dropdownPosition &&
        createPortal(
          <div
            ref={dropdownRef}
            className="animate-in fade-in slide-in-from-top-2 fixed z-[100000020] w-64 overflow-hidden rounded-xl shadow-lg ring-1 ring-black/5 duration-200 dark:ring-white/10 surface-panel"
            style={{ top: `${dropdownPosition.top}px`, left: `${dropdownPosition.left}px` }}
          >
            <div className="max-h-64 overflow-y-auto p-1.5">
              <button
                onClick={() => selectedKeyForGroup && changeGroup(selectedKeyForGroup, null)}
                className={`flex w-full items-center rounded-lg px-3 py-2 text-sm transition-colors ${
                  !selectedKeyForGroup?.group_id
                    ? 'bg-primary-50/[0.15] dark:bg-primary-900/20'
                    : 'hover:surface-hover dark:hover:surface-hover'
                }`}
              >
                <span className="italic surface-muted">{t('admin.users.none')}</span>
                {!selectedKeyForGroup?.group_id && (
                  <svg className="ml-auto h-4 w-4 shrink-0 text-primary-600 dark:text-primary-400" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="2">
                    <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
                  </svg>
                )}
              </button>
              {allGroups.map((group) => (
                <button
                  key={group.id}
                  onClick={() => selectedKeyForGroup && changeGroup(selectedKeyForGroup, group.id)}
                  className={`flex w-full items-center justify-between rounded-lg px-3 py-2 text-sm transition-colors ${
                    selectedKeyForGroup?.group_id === group.id
                      ? 'bg-primary-50/[0.15] dark:bg-primary-900/20'
                      : 'hover:surface-hover dark:hover:surface-hover'
                  }`}
                >
                  <GroupOptionItem
                    name={group.name}
                    platform={group.platform}
                    subscriptionType={group.subscription_type}
                    rateMultiplier={group.rate_multiplier}
                    peakRateEnabled={group.peak_rate_enabled}
                    peakStart={group.peak_start}
                    peakEnd={group.peak_end}
                    peakRateMultiplier={group.peak_rate_multiplier}
                    description={group.description}
                    selected={selectedKeyForGroup?.group_id === group.id}
                  />
                </button>
              ))}
            </div>
          </div>,
          document.body
        )}
    </>
  )
}

// ---------------------------------------------------------------------------
// UserAllowedGroupsModal
// ---------------------------------------------------------------------------

interface GroupRateConfig {
  groupId: number
  groupName: string
  platform: Group['platform']
  isExclusive: boolean
  defaultRate: number
  customRate: number | null
  isSelected: boolean
}

export function UserAllowedGroupsModal({
  show,
  user,
  onClose,
  onSuccess
}: {
  show: boolean
  user: AdminUser | null
  onClose: () => void
  onSuccess: () => void
}) {
  const { t } = useTranslation()
  const showSuccess = useAppStore((state) => state.showSuccess)

  const [groups, setGroups] = useState<Group[]>([])
  const [groupConfigs, setGroupConfigs] = useState<GroupRateConfig[]>([])
  const originalGroupRates = useRef<Record<number, number>>({})
  const [loading, setLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [restrictPublicGroups, setRestrictPublicGroups] = useState(false)

  const exclusiveGroups = groups.filter((g) => g.is_exclusive)
  const publicGroups = groups.filter((g) => !g.is_exclusive)
  const exclusiveGroupConfigs = groupConfigs.filter((c) => c.isExclusive)
  const publicGroupConfigs = groupConfigs.filter((c) => !c.isExclusive)

  useEffect(() => {
    const load = async () => {
      if (!user) return
      setLoading(true)
      try {
        const res = await adminAPI.groups.list(1, 1000)
        // 只显示标准类型且活跃的分组
        const visible = res.items.filter((g) => g.subscription_type === 'standard' && g.status === 'active')
        setGroups(visible)

        const userAllowedGroups = user.allowed_groups || []
        const userGroupRates = user.group_rates || {}
        const restrict = user.restrict_public_groups ?? false
        setRestrictPublicGroups(restrict)

        // 保存原始专属倍率，用于检测删除操作
        originalGroupRates.current = { ...userGroupRates }

        setGroupConfigs(
          visible.map((g) => ({
            groupId: g.id,
            groupName: g.name,
            platform: g.platform,
            isExclusive: g.is_exclusive,
            defaultRate: g.rate_multiplier,
            customRate: userGroupRates[g.id] ?? null,
            // 专属分组：检查是否在 allowed_groups 中
            // 公开分组：未开启限制时恒可用；开启后同样以 allowed_groups 为准
            isSelected: g.is_exclusive || restrict ? userAllowedGroups.includes(g.id) : true
          }))
        )
      } catch (error) {
        console.error('Failed to load groups:', error)
      } finally {
        setLoading(false)
      }
    }
    if (show && user) void load()
  }, [show, user])

  const toggleExclusiveGroup = (groupId: number) => {
    setGroupConfigs((prev) =>
      prev.map((c) => (c.groupId === groupId && c.isExclusive ? { ...c, isSelected: !c.isSelected } : c))
    )
  }

  const togglePublicGroup = (groupId: number) => {
    setGroupConfigs((prev) =>
      prev.map((c) => (c.groupId === groupId && !c.isExclusive ? { ...c, isSelected: !c.isSelected } : c))
    )
  }

  // 关闭限制时把公开分组全部勾回，避免保存出一份"限制已关但只勾了两个"的误导状态。
  const toggleRestrictPublicGroups = () => {
    const next = !restrictPublicGroups
    setRestrictPublicGroups(next)
    if (!next) {
      setGroupConfigs((prev) => prev.map((c) => (c.isExclusive ? c : { ...c, isSelected: true })))
    }
  }

  const updateCustomRate = (groupId: number, value: string) => {
    setGroupConfigs((prev) =>
      prev.map((c) => {
        if (c.groupId !== groupId) return c
        if (value === '' || value === null || value === undefined) return { ...c, customRate: null }
        const numValue = parseFloat(value)
        return { ...c, customRate: isNaN(numValue) ? null : numValue }
      })
    )
  }

  const handleSave = async () => {
    if (!user) return
    setSubmitting(true)

    try {
      // 构建 allowed_groups：专属分组中被勾选的，以及开启限制后被勾选的公开分组。
      // 未开启限制时不写入公开分组，保持该表"额外授予"的原有语义。
      const allowedGroups = groupConfigs
        .filter((c) => c.isSelected && (c.isExclusive || restrictPublicGroups))
        .map((c) => c.groupId)

      // 构建 group_rates
      // - 有新专属倍率: 设置为该值
      // - 原本有专属倍率但现在被清空: 设置为 null（表示删除）
      const groupRates: Record<number, number | null> = {}
      for (const c of groupConfigs) {
        const hadOriginalRate = originalGroupRates.current[c.groupId] !== undefined

        if (c.customRate !== null) {
          groupRates[c.groupId] = c.customRate
        } else if (hadOriginalRate) {
          groupRates[c.groupId] = null
        }
      }

      await adminAPI.users.update(user.id, {
        allowed_groups: allowedGroups,
        restrict_public_groups: restrictPublicGroups,
        group_rates: Object.keys(groupRates).length > 0 ? groupRates : undefined
      } as never)

      showSuccess(t('admin.users.groupConfigUpdated'))
      onSuccess()
      onClose()
    } catch (error) {
      console.error('Failed to update user group config:', error)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open={show}
      onClose={onClose}
      title={t('admin.users.groupConfig')}
      size="xl"
      footer={
        <div className="flex justify-end gap-3">
          <Button variant="secondary" className="px-5" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" className="px-6" loading={submitting} onClick={handleSave}>
            {submitting ? t('common.saving') : t('common.save')}
          </Button>
        </div>
      }
    >
      {user && (
        <div className="space-y-6">
          <div className="flex items-center gap-4 rounded-2xl bg-gradient-to-r from-primary-50 to-primary-100 p-5 dark:from-primary-900/30 dark:to-primary-800/20">
            <div className="flex h-14 w-14 items-center justify-center rounded-full surface-inset shadow-sm">
              <span className="text-2xl font-semibold text-primary-600 dark:text-primary-400">
                {user.email.charAt(0).toUpperCase()}
              </span>
            </div>
            <div className="flex-1">
              <p className="text-lg font-semibold text-gray-900 dark:text-white">{user.email}</p>
              <p className="mt-1 text-sm surface-muted">{t('admin.users.groupConfigHint', { email: user.email })}</p>
            </div>
          </div>

          {loading ? (
            <div className="flex justify-center py-12">
              <svg className="h-10 w-10 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
                <circle opacity="0.25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                <path opacity="0.75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
              </svg>
            </div>
          ) : (
            <div className="space-y-6">
              {exclusiveGroups.length > 0 && (
                <div>
                  <div className="mb-3 flex items-center gap-2">
                    <div className="console-data-mark h-1.5 w-1.5 rounded-full bg-purple-500" />
                    <h4 className="text-sm font-semibold surface-muted">{t('admin.users.exclusiveGroups')}</h4>
                    <span className="text-xs surface-muted">
                      ({exclusiveGroupConfigs.filter((c) => c.isSelected).length}/{exclusiveGroupConfigs.length})
                    </span>
                  </div>
                  <div className="grid gap-3">
                    {exclusiveGroupConfigs.map((config) => (
                      <div
                        key={config.groupId}
                        className={`group relative overflow-hidden rounded-xl border-2 p-4 transition-all duration-200 ${
                          config.isSelected
                            ? 'border-primary-400 bg-primary-50/50 shadow-sm dark:border-primary-500 dark:bg-primary-900/20'
                            : 'border-gray-200 surface-inset hover:border-gray-300 dark:border-dark-600 dark:surface-inset dark:hover:border-dark-500'
                        }`}
                      >
                        <div className="flex items-center gap-4">
                          <div className="flex-shrink-0">
                            <label className="relative flex h-6 w-6 cursor-pointer items-center justify-center">
                              <input
                                type="checkbox"
                                checked={config.isSelected}
                                onChange={() => toggleExclusiveGroup(config.groupId)}
                                className="peer sr-only"
                              />
                              <div className="h-5 w-5 rounded-md border-2 border-gray-300 transition-all peer-checked:border-primary-500 peer-checked:bg-primary-500 dark:border-dark-500 peer-checked:dark:border-primary-500">
                                {config.isSelected && (
                                  <svg className="h-full w-full text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="3">
                                    <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
                                  </svg>
                                )}
                              </div>
                            </label>
                          </div>

                          <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-2">
                              <span className="text-base font-semibold text-gray-900 dark:text-white">{config.groupName}</span>
                              <span className="inline-flex items-center rounded-full bg-purple-100/[0.15] px-2 py-0.5 text-xs font-medium text-purple-700 dark:bg-purple-900/40 dark:text-purple-300">
                                {t('admin.groups.exclusive')}
                              </span>
                            </div>
                            <div className="mt-1.5 flex items-center gap-3 text-sm">
                              <span className="inline-flex items-center gap-1 surface-muted">
                                <span>{config.platform}</span>
                              </span>
                              <span className="text-gray-300 dark:text-dark-500">•</span>
                              <span className="surface-muted">
                                {t('admin.users.defaultRate')}: <span className="font-medium surface-muted">{config.defaultRate}x</span>
                              </span>
                            </div>
                          </div>

                          <div className="flex flex-shrink-0 items-center gap-3">
                            <label className="text-sm font-medium surface-muted">{t('admin.users.customRate')}</label>
                            <Input
                              type="number"
                              step="0.001"
                              min="0.001"
                              value={config.customRate ?? ''}
                              onValueChange={(value) => updateCustomRate(config.groupId, value)}
                              placeholder={String(config.defaultRate)}
                              className="hide-spinner w-24"
                            />
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {publicGroups.length > 0 && (
                <div>
                  <div className="mb-3 flex flex-wrap items-center gap-2">
                    <div className="console-data-mark h-1.5 w-1.5 rounded-full bg-green-500" />
                    <h4 className="text-sm font-semibold surface-muted">
                      {restrictPublicGroups ? t('admin.users.publicGroupsRestricted') : t('admin.users.publicGroups')}
                    </h4>
                    <span className="text-xs surface-muted">({publicGroupConfigs.length})</span>
                    <label className="ml-auto flex cursor-pointer items-center gap-2 text-sm surface-muted">
                      <input
                        type="checkbox"
                        checked={restrictPublicGroups}
                        onChange={toggleRestrictPublicGroups}
                        className="h-4 w-4 cursor-pointer rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
                      />
                      {t('admin.users.restrictPublicGroups')}
                    </label>
                  </div>
                  <p className="mb-3 text-xs surface-muted">{t('admin.users.restrictPublicGroupsHint')}</p>
                  <div className="grid gap-3">
                    {publicGroupConfigs.map((config) => (
                      <div
                        key={config.groupId}
                        className="relative overflow-hidden rounded-xl border-2 border-green-200 bg-green-50/50 p-4 dark:border-green-800/50 dark:bg-green-900/10"
                      >
                        <div className="flex items-center gap-4">
                          <div className="flex-shrink-0">
                            {restrictPublicGroups ? (
                              <input
                                type="checkbox"
                                checked={config.isSelected}
                                onChange={() => togglePublicGroup(config.groupId)}
                                className="h-5 w-5 cursor-pointer rounded-md border-2 border-green-400 text-green-600 focus:ring-green-500 dark:border-green-600"
                              />
                            ) : (
                              <div className="flex h-5 w-5 items-center justify-center rounded-md border-2 border-green-400 bg-green-500 dark:border-green-600 dark:bg-green-600">
                                <svg className="h-full w-full text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="3">
                                  <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
                                </svg>
                              </div>
                            )}
                          </div>

                          <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-2">
                              <span className="text-base font-semibold text-gray-900 dark:text-white">{config.groupName}</span>
                            </div>
                            <div className="mt-1.5 flex items-center gap-3 text-sm">
                              <span className="inline-flex items-center gap-1 surface-muted">
                                <span>{config.platform}</span>
                              </span>
                              <span className="text-gray-300 dark:text-dark-500">•</span>
                              <span className="surface-muted">
                                {t('admin.users.defaultRate')}: <span className="font-medium surface-muted">{config.defaultRate}x</span>
                              </span>
                            </div>
                          </div>

                          <div className="flex flex-shrink-0 items-center gap-3">
                            <label className="text-sm font-medium surface-muted">{t('admin.users.customRate')}</label>
                            <Input
                              type="number"
                              step="0.001"
                              min="0.001"
                              value={config.customRate ?? ''}
                              onValueChange={(value) => updateCustomRate(config.groupId, value)}
                              placeholder={String(config.defaultRate)}
                              className="hide-spinner w-24"
                            />
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {groups.length === 0 && (
                <div className="flex flex-col items-center justify-center py-12 text-center">
                  <div className="mb-4 flex h-16 w-16 items-center justify-center rounded-full surface-inset">
                    <svg className="h-8 w-8 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
                    </svg>
                  </div>
                  <p className="surface-muted">{t('common.noGroupsAvailable')}</p>
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </Modal>
  )
}

// ---------------------------------------------------------------------------
// UserBalanceModal
// ---------------------------------------------------------------------------

// 格式化余额：显示完整精度，去除尾部多余的0
function formatBalance(value: number): string {
  if (value === 0) return '0.00'
  // 最多保留8位小数，去除尾部的0
  const formatted = value.toFixed(8).replace(/\.?0+$/, '')
  // 确保至少有2位小数
  const parts = formatted.split('.')
  if (parts.length === 1) return formatted + '.00'
  if (parts[1].length === 1) return formatted + '0'
  return formatted
}

export function UserBalanceModal({
  show,
  user,
  operation,
  onClose,
  onSuccess
}: {
  show: boolean
  user: AdminUser | null
  operation: 'add' | 'subtract'
  onClose: () => void
  onSuccess: () => void
}) {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)
  const [submitting, setSubmitting] = useState(false)
  const [form, setForm] = useState({ amount: 0, notes: '' })

  useEffect(() => {
    if (show) setForm({ amount: 0, notes: '' })
  }, [show])

  const calculateNewBalance = () => {
    if (!user) return 0
    const result = operation === 'add' ? user.balance + form.amount : user.balance - form.amount
    // 避免浮点数精度问题导致的 -0.00 显示
    return Math.abs(result) < 1e-10 ? 0 : result
  }

  const handleBalanceSubmit = async () => {
    if (!user) return
    if (!form.amount || form.amount <= 0) {
      showError(t('admin.users.amountRequired'))
      return
    }
    // 退款时验证金额不超过实际余额
    if (operation === 'subtract' && form.amount > user.balance) {
      showError(t('admin.users.insufficientBalance'))
      return
    }
    setSubmitting(true)
    try {
      await adminAPI.users.updateBalance(user.id, form.amount, operation, form.notes)
      showSuccess(t('common.success'))
      onSuccess()
      onClose()
    } catch (e) {
      console.error('Failed to update balance:', e)
      showError((e as { message?: string })?.message || t('common.error'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open={show}
      onClose={onClose}
      title={operation === 'add' ? t('admin.users.deposit') : t('admin.users.withdraw')}
      size="sm"
      footer={
        <div className="flex justify-end gap-3">
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button
            variant={operation === 'add' ? 'success' : 'danger'}
            disabled={submitting || !form.amount}
            loading={submitting}
            onClick={handleBalanceSubmit}
          >
            {submitting ? t('common.saving') : t('common.confirm')}
          </Button>
        </div>
      }
    >
      {user && (
        <div className="space-y-5">
          <div className="flex items-center gap-3 rounded-xl surface-inset p-4">
            <div className="flex h-10 w-10 items-center justify-center rounded-full bg-primary-100/[0.15]">
              <span className="text-lg font-medium text-primary-700">{user.email.charAt(0).toUpperCase()}</span>
            </div>
            <div className="flex-1">
              <p className="font-medium text-gray-900 dark:text-gray-100">{user.email}</p>
              <p className="text-sm surface-muted">
                {t('admin.users.currentBalance')}: ${formatBalance(user.balance)}
              </p>
            </div>
          </div>
          <div>
            <label className="input-label">
              {operation === 'add' ? t('admin.users.depositAmount') : t('admin.users.withdrawAmount')}
            </label>
            <div className="relative flex gap-2">
              <div className="relative flex-1">
                <div className="absolute left-3 top-1/2 -translate-y-1/2 font-medium surface-muted">$</div>
                <Input
                  type="number"
                  step="any"
                  min="0"
                  required
                  className="pl-8"
                  value={form.amount}
                  onValueChange={(value) => setForm((prev) => ({ ...prev, amount: Number(value) }))}
                />
              </div>
              {operation === 'subtract' && (
                <Button variant="secondary" className="whitespace-nowrap" onClick={() => setForm((prev) => ({ ...prev, amount: user.balance }))}>
                  {t('admin.users.withdrawAll')}
                </Button>
              )}
            </div>
          </div>
          <div>
            <label className="input-label">{t('admin.users.notes')}</label>
            <TextArea rows={3} value={form.notes} onValueChange={(value) => setForm((prev) => ({ ...prev, notes: value }))} />
          </div>
          {form.amount > 0 && (
            <div className="rounded-xl border border-blue-200 bg-blue-50/[0.15] p-4 dark:border-blue-800 dark:bg-blue-950">
              <div className="flex items-center justify-between text-sm">
                <span className="surface-muted">{t('admin.users.newBalance')}:</span>
                <span className="font-bold text-gray-900 dark:text-gray-100">${formatBalance(calculateNewBalance())}</span>
              </div>
            </div>
          )}
        </div>
      )}
    </Modal>
  )
}

// ---------------------------------------------------------------------------
// GroupReplaceModal
// ---------------------------------------------------------------------------

export function GroupReplaceModal({
  show,
  user,
  oldGroup,
  allGroups,
  onClose,
  onSuccess
}: {
  show: boolean
  user: AdminUser | null
  oldGroup: { id: number; name: string } | null
  allGroups: Group[]
  onClose: () => void
  onSuccess: () => void
}) {
  const { t } = useTranslation()
  const showSuccess = useAppStore((state) => state.showSuccess)
  const [selectedGroupId, setSelectedGroupId] = useState<number | null>(null)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (show) setSelectedGroupId(null)
  }, [show])

  // 可选的专属标准分组（排除当前 oldGroup）
  const availableGroups = oldGroup
    ? allGroups.filter(
        (g) => g.status === 'active' && g.is_exclusive && g.subscription_type === 'standard' && g.id !== oldGroup.id
      )
    : []

  const handleReplace = async () => {
    if (!user || !oldGroup || !selectedGroupId) return
    setSubmitting(true)

    try {
      const result = await adminAPI.users.replaceGroup(user.id, oldGroup.id, selectedGroupId)
      showSuccess(t('admin.users.replaceGroupSuccess', { count: result.migrated_keys }))
      onSuccess()
      onClose()
    } catch (error) {
      console.error('Failed to replace group:', error)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open={show}
      onClose={onClose}
      title={t('admin.users.replaceGroupTitle')}
      size="sm"
      footer={
        <div className="flex justify-end gap-3">
          <Button variant="secondary" className="px-5" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" className="px-6" disabled={!selectedGroupId || submitting} loading={submitting} onClick={handleReplace}>
            {submitting ? t('common.saving') : t('admin.users.replaceGroupConfirm')}
          </Button>
        </div>
      }
    >
      {oldGroup && (
        <div className="space-y-4">
          <p className="text-sm surface-muted">{t('admin.users.replaceGroupHint', { old: oldGroup.name })}</p>

          <div className="rounded-lg border border-gray-200 surface-inset p-3 dark:border-dark-600">
            <div className="flex items-center gap-2">
              <svg className="h-4 w-4 text-purple-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                <path strokeLinecap="round" strokeLinejoin="round" d="M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z" />
              </svg>
              <span className="font-medium text-gray-900 dark:text-white">{oldGroup.name}</span>
              <svg className="ml-auto h-4 w-4 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                <path strokeLinecap="round" strokeLinejoin="round" d="M13.5 4.5L21 12m0 0l-7.5 7.5M21 12H3" />
              </svg>
              <span className="font-medium text-primary-600 dark:text-primary-400">
                {availableGroups.find((g) => g.id === selectedGroupId)?.name ?? '?'}
              </span>
            </div>
          </div>

          {availableGroups.length > 0 ? (
            <div className="max-h-64 space-y-2 overflow-y-auto">
              {availableGroups.map((group) => (
                <label
                  key={group.id}
                  className={`flex cursor-pointer items-center gap-3 rounded-lg border-2 p-3 transition-all ${
                    selectedGroupId === group.id
                      ? 'border-primary-400 bg-primary-50/50 dark:border-primary-500 dark:bg-primary-900/20'
                      : 'border-gray-200 hover:border-gray-300 dark:border-dark-600 dark:hover:border-dark-500'
                  }`}
                >
                  <input type="radio" value={group.id} checked={selectedGroupId === group.id} onChange={() => setSelectedGroupId(group.id)} className="sr-only" />
                  <div
                    className={`flex h-5 w-5 items-center justify-center rounded-full border-2 transition-all ${
                      selectedGroupId === group.id ? 'border-primary-500 bg-primary-500' : 'border-gray-300 dark:border-dark-500'
                    }`}
                  >
                    {selectedGroupId === group.id && <div className="console-data-mark h-2 w-2 rounded-full bg-white" />}
                  </div>
                  <div className="flex-1">
                    <span className="font-medium text-gray-900 dark:text-white">{group.name}</span>
                    <span className="ml-2 text-xs surface-muted">{group.platform}</span>
                  </div>
                </label>
              ))}
            </div>
          ) : (
            <div className="py-6 text-center text-sm surface-muted">{t('admin.users.noOtherGroups')}</div>
          )}
        </div>
      )}
    </Modal>
  )
}

// ---------------------------------------------------------------------------
// UserBalanceHistoryModal
// ---------------------------------------------------------------------------

const HISTORY_PAGE_SIZE = 15

function isAdminType(type: string): boolean {
  return type === 'admin_balance' || type === 'admin_concurrency'
}

function isBalanceType(type: string): boolean {
  return type === 'balance' || type === 'admin_balance' || type === 'affiliate_balance'
}

function isSubscriptionType(type: string): boolean {
  return type === 'subscription'
}

export function UserBalanceHistoryModal({
  show,
  user,
  hideActions,
  onClose,
  onDeposit,
  onWithdraw
}: {
  show: boolean
  user: AdminUser | null
  hideActions?: boolean
  onClose: () => void
  onDeposit: () => void
  onWithdraw: () => void
}) {
  const { t } = useTranslation()
  const [history, setHistory] = useState<BalanceHistoryItem[]>([])
  const [loading, setLoading] = useState(false)
  const [currentPage, setCurrentPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [totalRecharged, setTotalRecharged] = useState(0)
  const [typeFilter, setTypeFilter] = useState('')

  const totalPages = Math.ceil(total / HISTORY_PAGE_SIZE) || 1

  const typeOptions = useMemo(
    () => [
      { value: '', label: t('admin.users.allTypes') },
      { value: 'balance', label: t('admin.users.typeBalance') },
      { value: 'affiliate_balance', label: t('admin.users.typeAffiliateBalance') },
      { value: 'admin_balance', label: t('admin.users.typeAdminBalance') },
      { value: 'concurrency', label: t('admin.users.typeConcurrency') },
      { value: 'admin_concurrency', label: t('admin.users.typeAdminConcurrency') },
      { value: 'subscription', label: t('admin.users.typeSubscription') }
    ],
    [t]
  )

  useEffect(() => {
    if (show && user) {
      setTypeFilter('')
      void loadHistory(1)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [show, user])

  const loadHistory = async (page: number) => {
    if (!user) return
    setLoading(true)
    setCurrentPage(page)
    try {
      const res = await adminAPI.users.getUserBalanceHistory(user.id, page, HISTORY_PAGE_SIZE, typeFilter || undefined)
      setHistory(res.items || [])
      setTotal(res.total || 0)
      setTotalRecharged(res.total_recharged || 0)
    } catch (error) {
      console.error('Failed to load balance history:', error)
    } finally {
      setLoading(false)
    }
  }

  const getIconName = (item: BalanceHistoryItem) => {
    if (isBalanceType(item.type)) return 'dollar'
    if (isSubscriptionType(item.type)) return 'badge'
    return 'bolt'
  }

  const getIconBg = (item: BalanceHistoryItem) => {
    if (isBalanceType(item.type)) {
      return item.value >= 0 ? 'bg-emerald-100/[0.15] dark:bg-emerald-900/30' : 'bg-red-100/[0.15] dark:bg-red-900/30'
    }
    if (isSubscriptionType(item.type)) return 'bg-purple-100/[0.15] dark:bg-purple-900/30'
    return item.value >= 0 ? 'bg-blue-100/[0.15] dark:bg-blue-900/30' : 'bg-orange-100/[0.15] dark:bg-orange-900/30'
  }

  const getIconColor = (item: BalanceHistoryItem) => {
    if (isBalanceType(item.type)) {
      return item.value >= 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400'
    }
    if (isSubscriptionType(item.type)) return 'text-purple-600 dark:text-purple-400'
    return item.value >= 0 ? 'text-blue-600 dark:text-blue-400' : 'text-orange-600 dark:text-orange-400'
  }

  const getValueColor = (item: BalanceHistoryItem) => {
    if (isBalanceType(item.type)) {
      return item.value >= 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400'
    }
    if (isSubscriptionType(item.type)) return 'text-purple-600 dark:text-purple-400'
    return item.value >= 0 ? 'text-blue-600 dark:text-blue-400' : 'text-orange-600 dark:text-orange-400'
  }

  const getItemTitle = (item: BalanceHistoryItem) => {
    switch (item.type) {
      case 'balance':
        return t('redeem.balanceAddedRedeem')
      case 'affiliate_balance':
        return t('redeem.balanceAddedAffiliate')
      case 'admin_balance':
        return item.value >= 0 ? t('redeem.balanceAddedAdmin') : t('redeem.balanceDeductedAdmin')
      case 'concurrency':
        return t('redeem.concurrencyAddedRedeem')
      case 'admin_concurrency':
        return item.value >= 0 ? t('redeem.concurrencyAddedAdmin') : t('redeem.concurrencyReducedAdmin')
      case 'subscription':
        return t('redeem.subscriptionAssigned')
      default:
        return t('common.unknown')
    }
  }

  const formatValue = (item: BalanceHistoryItem) => {
    if (isBalanceType(item.type)) {
      const sign = item.value >= 0 ? '+' : ''
      return `${sign}$${item.value.toFixed(2)}`
    }
    if (isSubscriptionType(item.type)) {
      const days = item.validity_days || Math.round(item.value)
      const groupName = item.group?.name || ''
      return groupName ? `${days}d - ${groupName}` : `${days}d`
    }
    const sign = item.value >= 0 ? '+' : ''
    return `${sign}${item.value}`
  }

  return (
    <Modal open={show} onClose={onClose} title={t('admin.users.balanceHistoryTitle')} size="xl" closeOnOverlayClick>
      {user && (
        <div className="space-y-4">
          <div className="rounded-xl surface-inset p-4">
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-full bg-primary-100/[0.15] dark:bg-primary-900/30">
                <span className="text-lg font-medium text-primary-700 dark:text-primary-300">{user.email.charAt(0).toUpperCase()}</span>
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <p className="truncate font-medium text-gray-900 dark:text-white">{user.email}</p>
                  {user.deleted_at && (
                    <span className="flex-shrink-0 inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-rose-100/[0.15] text-rose-600 ring-1 ring-inset ring-rose-200 dark:bg-rose-500/20 dark:text-rose-400 dark:ring-rose-500/30">
                      {t('admin.usage.userDeletedBadge')}
                    </span>
                  )}
                  {user.username && (
                    <span className="flex-shrink-0 rounded bg-primary-50/[0.15] px-1.5 py-0.5 text-xs text-primary-600 dark:bg-primary-900/20 dark:text-primary-400">
                      {user.username}
                    </span>
                  )}
                </div>
                <p className="text-xs surface-muted">
                  {t('admin.users.createdAt')}: {formatDateTime(user.created_at)}
                </p>
              </div>
              <div className="flex-shrink-0 text-right">
                <p className="text-xs surface-muted">{t('admin.users.currentBalance')}</p>
                <p className="text-xl font-bold text-gray-900 dark:text-white">${user.balance?.toFixed(2) || '0.00'}</p>
              </div>
            </div>
            <div className="mt-2.5 flex items-center justify-between border-t border-gray-200/60 pt-2.5 dark:border-dark-600/60">
              <p className="min-w-0 flex-1 truncate text-xs surface-muted" title={user.notes || ''}>
                {user.notes ? `${t('admin.users.notes')}: ${user.notes}` : '\u00A0'}
              </p>
              <p className="ml-4 flex-shrink-0 text-xs surface-muted">
                {t('admin.users.totalRecharged')}: <span className="font-semibold text-emerald-600 dark:text-emerald-400">${totalRecharged.toFixed(2)}</span>
              </p>
            </div>
          </div>

          <div className="flex items-center gap-3">
            <Select className="w-56" value={typeFilter} options={typeOptions} onValueChange={(value) => { setTypeFilter(String(value ?? '')); void loadHistory(1) }} />
            {!hideActions && (
              <button
                onClick={onDeposit}
                className="flex items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm transition-colors hover:surface-hover dark:border-dark-600 surface-control surface-muted"
              >
                <svg className="h-4 w-4 text-emerald-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M12 4.5v15m7.5-7.5h-15" />
                </svg>
                {t('admin.users.deposit')}
              </button>
            )}
            {!hideActions && (
              <button
                onClick={onWithdraw}
                className="flex items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm transition-colors hover:surface-hover dark:border-dark-600 surface-control surface-muted"
              >
                <svg className="h-4 w-4 text-amber-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M20 12H4" />
                </svg>
                {t('admin.users.withdraw')}
              </button>
            )}
          </div>

          {loading ? (
            <div className="flex justify-center py-8">
              <svg className="h-8 w-8 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
                <circle opacity="0.25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                <path opacity="0.75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
              </svg>
            </div>
          ) : history.length === 0 ? (
            <div className="py-8 text-center">
              <p className="text-sm surface-muted">{t('admin.users.noBalanceHistory')}</p>
            </div>
          ) : (
            <div className="max-h-[28rem] space-y-3 overflow-y-auto">
              {history.map((item) => (
                <div key={item.id} className="rounded-xl border border-gray-200 surface-inset p-4 dark:border-dark-600">
                  <div className="flex items-start justify-between">
                    <div className="flex items-start gap-3">
                      <div className={`flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-lg ${getIconBg(item)}`}>
                        <span className={getIconColor(item)}>{historyIconGlyph(getIconName(item))}</span>
                      </div>
                      <div>
                        <p className="text-sm font-medium text-gray-900 dark:text-white">{getItemTitle(item)}</p>
                        {item.notes && (
                          <p className="mt-0.5 text-xs surface-muted" title={item.notes}>
                            {item.notes.length > 60 ? item.notes.substring(0, 55) + '...' : item.notes}
                          </p>
                        )}
                        <p className="mt-0.5 text-xs surface-muted">{formatDateTime(item.used_at || item.created_at)}</p>
                      </div>
                    </div>
                    <div className="text-right">
                      <p className={`text-sm font-semibold ${getValueColor(item)}`}>{formatValue(item)}</p>
                      {isAdminType(item.type) ? (
                        <p className="text-xs surface-muted">{t('redeem.adminAdjustment')}</p>
                      ) : (
                        <p className="font-mono text-xs surface-muted">{item.code.slice(0, 8)}...</p>
                      )}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}

          {totalPages > 1 && (
            <div className="pagination-card card flex items-center justify-center gap-2 px-4 py-3">
              <Button variant="secondary" size="sm" disabled={currentPage <= 1} onClick={() => loadHistory(currentPage - 1)}>
                {t('pagination.previous')}
              </Button>
              <span className="text-sm surface-muted">
                {currentPage} / {totalPages}
              </span>
              <Button variant="secondary" size="sm" disabled={currentPage >= totalPages} onClick={() => loadHistory(currentPage + 1)}>
                {t('pagination.next')}
              </Button>
            </div>
          )}
        </div>
      )}
    </Modal>
  )
}

function historyIconGlyph(name: string) {
  if (name === 'dollar') {
    return (
      <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
        <path strokeLinecap="round" strokeLinejoin="round" d="M12 6v12m-3-2.818l.879.659c1.171.879 3.07.879 4.242 0 1.172-.879 1.172-2.303 0-3.182C13.536 12.219 12.768 12 12 12c-.725 0-1.45-.22-2.003-.659-1.106-.879-1.106-2.303 0-3.182s2.9-.879 4.006 0l.415.33M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
      </svg>
    )
  }
  if (name === 'badge') {
    return (
      <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
        <path strokeLinecap="round" strokeLinejoin="round" d="M9 12.75L11.25 15 15 9.75M21 12c0 1.268-.63 2.39-1.593 3.068a3.745 3.745 0 01-1.043 3.296 3.745 3.745 0 01-3.296 1.043A3.745 3.745 0 0112 21c-1.268 0-2.39-.63-3.068-1.593a3.746 3.746 0 01-3.296-1.043 3.745 3.745 0 01-1.043-3.296A3.745 3.745 0 013 12c0-1.268.63-2.39 1.593-3.068a3.745 3.745 0 011.043-3.296 3.746 3.746 0 013.296-1.043A3.746 3.746 0 0112 3c1.268 0 2.39.63 3.068 1.593a3.746 3.746 0 013.296 1.043 3.746 3.746 0 011.043 3.296A3.746 3.746 0 0121 12z" />
      </svg>
    )
  }
  return (
    <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
      <path strokeLinecap="round" strokeLinejoin="round" d="M3.75 13.5l10.5-11.25L12 10.5h8.25L9.75 21.75 12 13.5H3.75z" />
    </svg>
  )
}
