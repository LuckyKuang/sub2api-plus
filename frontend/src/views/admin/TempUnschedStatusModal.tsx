import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { adminAPI } from '@/api'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
import type { Account, TempUnschedulableStatus } from '@/types'
import { Modal } from '@/components/ui'

export interface TempUnschedStatusModalProps {
  show: boolean
  account: Account | null
  onClose: () => void
  onReset: (account: Account) => void
}

export function TempUnschedStatusModal({ show, account, onClose, onReset }: TempUnschedStatusModalProps) {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)

  const [loading, setLoading] = useState(false)
  const [resetting, setResetting] = useState(false)
  const [status, setStatus] = useState<TempUnschedulableStatus | null>(null)

  const state = status?.state || null

  const isActive = (() => {
    if (!status?.active || !state) return false
    return state.until_unix * 1000 > Date.now()
  })()

  const ruleIndexDisplay = (() => {
    if (!state || !state.matched_keyword || state.rule_index < 0) return '-'
    return state.rule_index + 1
  })()

  const hasThresholdEvidence = (state?.trigger_count || 0) > 1

  const triggerEvidenceText = (() => {
    const count = state?.trigger_count || 0
    const threshold = state?.trigger_threshold || 0
    const minutes = state?.trigger_window_minutes || 0
    if (threshold > 0 && minutes > 0) {
      return t('admin.accounts.tempUnschedulable.multipleErrorTrigger', { count, threshold, minutes })
    }
    if (threshold > 0) {
      return t('admin.accounts.tempUnschedulable.multipleErrorTriggerNoWindow', { count, threshold })
    }
    if (minutes > 0) {
      return t('admin.accounts.tempUnschedulable.multipleErrorCountInWindow', { count, minutes })
    }
    return t('admin.accounts.tempUnschedulable.multipleErrorCount', { count })
  })()

  const triggeredAtText = state?.triggered_at_unix
    ? formatDateTime(new Date(state.triggered_at_unix * 1000))
    : '-'

  const untilText = state?.until_unix ? formatDateTime(new Date(state.until_unix * 1000)) : '-'

  const remainingText = (() => {
    if (!state) return '-'
    const remainingMs = state.until_unix * 1000 - Date.now()
    if (remainingMs <= 0) {
      return t('admin.accounts.tempUnschedulable.expired')
    }
    const minutes = Math.ceil(remainingMs / 60000)
    if (minutes < 60) {
      return t('admin.accounts.tempUnschedulable.remainingMinutes', { minutes })
    }
    const hours = Math.floor(minutes / 60)
    const rest = minutes % 60
    if (rest === 0) {
      return t('admin.accounts.tempUnschedulable.remainingHours', { hours })
    }
    return t('admin.accounts.tempUnschedulable.remainingHoursMinutes', { hours, minutes: rest })
  })()

  useEffect(() => {
    if (!show || !account) {
      setStatus(null)
      return
    }
    const loadStatus = async () => {
      setLoading(true)
      try {
        setStatus(await adminAPI.accounts.getTempUnschedulableStatus(account.id))
      } catch (error) {
        showError((error as { message?: string })?.message || t('admin.accounts.tempUnschedulable.failedToLoad'))
        setStatus(null)
      } finally {
        setLoading(false)
      }
    }
    void loadStatus()
  }, [show, account])

  const handleClose = () => {
    onClose()
  }

  const handleReset = async () => {
    if (!account) return
    setResetting(true)
    try {
      const updated = await adminAPI.accounts.recoverState(account.id)
      showSuccess(t('admin.accounts.recoverStateSuccess'))
      onReset(updated)
      handleClose()
    } catch (error) {
      showError((error as { message?: string })?.message || t('admin.accounts.recoverStateFailed'))
    } finally {
      setResetting(false)
    }
  }

  return (
    <Modal open={show} onClose={handleClose} title={t('admin.accounts.tempUnschedulable.statusTitle')} size="lg">
      <div className="space-y-4">
        {loading ? (
          <div className="flex items-center justify-center py-8">
            <svg className="h-6 w-6 animate-spin text-gray-400" fill="none" viewBox="0 0 24 24">
              <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
              <path
                className="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              />
            </svg>
          </div>
        ) : !isActive ? (
          <div className="rounded-lg border border-gray-200 p-4 text-sm dark:border-dark-600 surface-muted">
            {t('admin.accounts.tempUnschedulable.notActive')}
          </div>
        ) : (
          <div className="space-y-4">
            <div className="rounded-lg border border-emerald-200 bg-emerald-50/[0.15] p-3 text-sm text-emerald-800 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-300">
              {t('admin.accounts.recoverStateHint')}
            </div>

            <div className="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
              <p className="text-xs surface-muted">{t('admin.accounts.tempUnschedulable.accountName')}</p>
              <p className="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">{account?.name || '-'}</p>
            </div>

            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
                <p className="text-xs surface-muted">{t('admin.accounts.tempUnschedulable.triggeredAt')}</p>
                <p className="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">{triggeredAtText}</p>
              </div>
              <div className="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
                <p className="text-xs surface-muted">{t('admin.accounts.tempUnschedulable.until')}</p>
                <p className="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">{untilText}</p>
              </div>
              <div className="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
                <p className="text-xs surface-muted">{t('admin.accounts.tempUnschedulable.remaining')}</p>
                <p className="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">{remainingText}</p>
              </div>
              <div className="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
                <p className="text-xs surface-muted">{t('admin.accounts.tempUnschedulable.errorCode')}</p>
                <p className="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">{state?.status_code || '-'}</p>
              </div>
              <div className="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
                <p className="text-xs surface-muted">{t('admin.accounts.tempUnschedulable.matchedKeyword')}</p>
                <p className="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">{state?.matched_keyword || '-'}</p>
              </div>
              <div className="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
                <p className="text-xs surface-muted">{t('admin.accounts.tempUnschedulable.ruleOrder')}</p>
                <p className="mt-1 text-sm font-medium text-gray-900 dark:text-gray-100">{ruleIndexDisplay}</p>
              </div>
            </div>

            <div className="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
              <p className="text-xs surface-muted">{t('admin.accounts.tempUnschedulable.errorMessage')}</p>
              <div className="mt-2 rounded surface-inset p-2 text-xs surface-muted">{state?.error_message || '-'}</div>
            </div>

            {hasThresholdEvidence && (
              <div
                className="rounded-lg border border-blue-200 bg-blue-50/[0.15] p-3 text-sm text-blue-800 dark:border-blue-500/30 dark:bg-blue-500/10 dark:text-blue-300"
                data-testid="temp-unsched-trigger-evidence"
              >
                {triggerEvidenceText}
              </div>
            )}
          </div>
        )}
      </div>

      <div className="flex justify-end gap-3">
        <button type="button" className="btn btn-secondary" onClick={handleClose}>
          {t('common.close')}
        </button>
        <button type="button" className="btn btn-primary" disabled={!isActive || resetting} onClick={() => void handleReset()}>
          {resetting && (
            <svg className="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
              <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
              <path
                className="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              />
            </svg>
          )}
          {t('admin.accounts.recoverState')}
        </button>
      </div>
    </Modal>
  )
}

export default TempUnschedStatusModal
