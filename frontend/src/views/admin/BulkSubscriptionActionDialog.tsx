import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Modal } from '@/components/ui'
import { adminAPI } from '@/api/admin'
import type {
  SubscriptionBulkAction,
  SubscriptionBulkActionRequest,
  SubscriptionBulkActionResult
} from '@/api/admin/subscriptions'
import type { UserSubscription } from '@/types'
import {
  completeBulkSubscriptionOperation,
  prepareBulkSubscriptionOperation,
  type BulkSubscriptionOperation
} from './bulkSubscriptionOperation'

const QUOTA_WINDOWS = ['daily', 'weekly', 'monthly'] as const

interface BulkTarget {
  id: number
  email?: string
  group?: string
  groupId: number
}

export interface BulkSubscriptionActionDialogProps {
  action: SubscriptionBulkAction
  subscriptions: UserSubscription[]
  onClose: () => void
  onCompleted: (result: SubscriptionBulkActionResult) => void
}

export function BulkSubscriptionActionDialog({
  action,
  subscriptions,
  onClose,
  onCompleted
}: BulkSubscriptionActionDialogProps) {
  const { t } = useTranslation()
  const [days, setDays] = useState<number | string>(30)
  const [windows, setWindows] = useState({ daily: true, weekly: true, monthly: true })
  const [submitting, setSubmitting] = useState(false)
  const [requestError, setRequestError] = useState('')
  const [result, setResult] = useState<SubscriptionBulkActionResult | null>(null)
  const [pendingOperation, setPendingOperation] = useState<BulkSubscriptionOperation | null>(null)
  const [targets, setTargets] = useState<BulkTarget[]>(() =>
    subscriptions.map((subscription) => ({
      id: subscription.id,
      email: subscription.user?.email,
      group: subscription.group?.name,
      groupId: subscription.group_id
    }))
  )
  const [submittedAction, setSubmittedAction] = useState<SubscriptionBulkAction | null>(null)

  const currentAction = submittedAction ?? action
  const parametersLocked = submitting || !!pendingOperation || !!result
  const canClose = !submitting
  const failedResults = result?.results.filter((item) => !item.success) ?? []

  const parametersLockedRef = useRef(parametersLocked)
  parametersLockedRef.current = parametersLocked

  useEffect(() => {
    if (parametersLockedRef.current) return
    setTargets(
      subscriptions.map((subscription) => ({
        id: subscription.id,
        email: subscription.user?.email,
        group: subscription.group?.name,
        groupId: subscription.group_id
      }))
    )
  }, [subscriptions])

  const validationError = (() => {
    if (targets.length === 0) return t('admin.subscriptions.bulk.selectionRequired')
    if (targets.length > 100) return t('admin.subscriptions.bulk.selectionLimit')
    if (currentAction === 'extend') {
      const value = Number(days)
      if (!Number.isInteger(value) || value === 0 || Math.abs(value) > 36500) {
        return t('admin.subscriptions.bulk.invalidDays')
      }
    }
    if (currentAction === 'reset_quota' && !QUOTA_WINDOWS.some((window) => windows[window])) {
      return t('admin.subscriptions.bulk.selectWindow')
    }
    return ''
  })()

  function handleClose() {
    if (canClose) onClose()
  }

  function failedTargetLabel(id: number) {
    const target = targets.find((item) => item.id === id)
    return target?.email ? `${target.email} · #${id}` : `#${id}`
  }

  async function submit() {
    if (submitting || result) return
    let operation = pendingOperation
    if (!operation) {
      if (validationError) return
      const request: SubscriptionBulkActionRequest = {
        subscription_ids: targets.map((subscription) => subscription.id),
        action
      }
      if (request.action === 'extend') request.days = Number(days)
      if (request.action === 'reset_quota') Object.assign(request, { ...windows })
      operation = prepareBulkSubscriptionOperation(request)
      setPendingOperation(operation)
      setSubmittedAction(request.action)
    }

    const pending = operation
    setSubmitting(true)
    setRequestError('')
    try {
      const outcome = await adminAPI.subscriptions.bulkAction(pending.request, pending.key)
      completeBulkSubscriptionOperation(pending)
      setPendingOperation(null)
      setResult(outcome)
      onCompleted(outcome)
    } catch (error) {
      const failure = error as {
        status?: number
        message?: string
        response?: { status?: number; data?: { message?: string } }
      }
      setRequestError(
        failure?.message || failure?.response?.data?.message || t('admin.subscriptions.bulk.requestFailed')
      )
      const status = failure?.status ?? failure?.response?.status
      // A timeout, server error or in-progress conflict may arrive after writes.
      // Retry the frozen payload and key until a definitive result is available.
      if (!pending.outcomeUncertain && status && status >= 400 && status < 500 && status !== 408 && status !== 409) {
        completeBulkSubscriptionOperation(pending)
        setPendingOperation(null)
        setSubmittedAction(null)
      } else {
        pending.outcomeUncertain = true
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={canClose ? handleClose : undefined}
      title={t(`admin.subscriptions.bulk.${currentAction}`)}
      size="md"
      showClose={canClose}
      footer={
        <div className="flex justify-end gap-3">
          <Button variant="secondary" disabled={!canClose} onClick={handleClose}>
            {result ? t('common.close') : t('common.cancel')}
          </Button>
          {!result && (
            <Button
              variant={currentAction === 'revoke' ? 'danger' : 'primary'}
              type="submit"
              form="bulk-subscription-action-form"
              loading={submitting}
              disabled={submitting || (!pendingOperation && !!validationError)}
            >
              {submitting
                ? t('common.processing')
                : pendingOperation
                  ? t('admin.subscriptions.bulk.retry')
                  : t('admin.subscriptions.bulk.confirm')}
            </Button>
          )}
        </div>
      }
    >
      <form
        id="bulk-subscription-action-form"
        className="space-y-4"
        noValidate
        onSubmit={(event) => {
          event.preventDefault()
          void submit()
        }}
      >
        <div>
          <p className="mb-2 text-sm font-medium text-gray-900 dark:text-gray-100">
            {t('admin.subscriptions.bulk.confirmTargets', { count: targets.length })}
          </p>
          <ul className="max-h-48 divide-y divide-gray-100 overflow-y-auto rounded-lg border border-gray-200 dark:divide-dark-700 dark:border-dark-600">
            {targets.map((subscription) => (
              <li key={subscription.id} className="px-3 py-2 text-sm">
                <div className="break-all text-gray-900 dark:text-gray-100">
                  {subscription.email || `#${subscription.id}`}
                </div>
                <div className="break-words text-xs surface-muted">
                  {subscription.group || t('admin.subscriptions.bulk.groupFallback', { id: subscription.groupId })}
                  {subscription.email && <span className="ml-2">#{subscription.id}</span>}
                </div>
              </li>
            ))}
          </ul>
        </div>

        <fieldset disabled={parametersLocked} className="space-y-3 disabled:opacity-70">
          {currentAction === 'extend' && (
            <div>
              <label htmlFor="bulk-subscription-days" className="input-label">
                {t('admin.subscriptions.form.adjustDays')}
              </label>
              <input
                id="bulk-subscription-days"
                value={days}
                type="number"
                min={-36500}
                max={36500}
                step={1}
                className="input"
                disabled={parametersLocked}
                placeholder={t('admin.subscriptions.adjustDaysPlaceholder')}
                aria-describedby="bulk-subscription-days-hint"
                onChange={(event) => setDays(event.target.value)}
              />
              <p id="bulk-subscription-days-hint" className="mt-1 text-xs surface-muted">
                {t('admin.subscriptions.bulk.extendHint')}
              </p>
            </div>
          )}

          {currentAction === 'reset_quota' && (
            <>
              <legend className="text-sm font-medium surface-muted">
                {t('admin.subscriptions.bulk.resetWindows')}
              </legend>
              <div className="flex flex-wrap gap-5">
                {QUOTA_WINDOWS.map((window) => (
                  <label key={window} className="flex items-center gap-2 text-sm surface-muted">
                    <input
                      name={window}
                      type="checkbox"
                      className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                      checked={windows[window]}
                      disabled={parametersLocked}
                      onChange={(event) =>
                        setWindows((previous) => ({ ...previous, [window]: event.target.checked }))
                      }
                    />
                    {t(`admin.subscriptions.${window}`)}
                  </label>
                ))}
              </div>
              <p className="text-xs surface-muted">{t('admin.subscriptions.bulk.resetHint')}</p>
            </>
          )}

          {currentAction !== 'extend' && currentAction !== 'reset_quota' && (
            <p className="rounded-lg bg-amber-50/[0.15] p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
              {t(`admin.subscriptions.bulk.${currentAction}Hint`)}
            </p>
          )}
        </fieldset>

        {validationError && !parametersLocked && (
          <p role="alert" className="text-sm text-red-600 dark:text-red-400">
            {validationError}
          </p>
        )}

        {requestError && (
          <div
            role="alert"
            className="space-y-2 rounded-lg bg-red-50/[0.15] p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300"
          >
            <p>{requestError}</p>
            {pendingOperation && <p>{t('admin.subscriptions.bulk.retryHint')}</p>}
          </div>
        )}

        {result && (
          <div aria-live="polite" className="space-y-3">
            <p className="rounded-lg surface-inset p-3 text-sm font-medium text-gray-900 dark:text-gray-100">
              {t('admin.subscriptions.bulk.result', {
                success: result.success_count,
                failed: result.failed_count
              })}
            </p>
            {failedResults.length > 0 && (
              <ul className="max-h-48 space-y-2 overflow-y-auto">
                {failedResults.map((item) => (
                  <li
                    key={item.subscription_id}
                    className="rounded-lg bg-red-50/[0.15] p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300"
                  >
                    <p className="break-all font-medium">{failedTargetLabel(item.subscription_id)}</p>
                    <p className="mt-1 break-words">{item.error || t('admin.subscriptions.bulk.itemFailed')}</p>
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </form>
    </Modal>
  )
}

export default BulkSubscriptionActionDialog
