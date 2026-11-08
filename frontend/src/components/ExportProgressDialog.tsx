import { useTranslation } from 'react-i18next'
import { Modal } from './ui/Modal'

export interface ExportProgressDialogProps {
  show: boolean
  progress: number
  current: number
  total: number
  estimatedTime: string
  onCancel?: () => void
}

export function ExportProgressDialog({
  show,
  progress,
  current,
  total,
  estimatedTime,
  onCancel
}: ExportProgressDialogProps) {
  const { t } = useTranslation()

  const normalizedProgress = (() => {
    const value = Number.isFinite(progress) ? progress : 0
    return Math.min(100, Math.max(0, Math.round(value)))
  })()

  const handleCancel = () => {
    onCancel?.()
  }

  return (
    <Modal
      open={show}
      title={t('usage.exporting')}
      size="sm"
      showClose={false}
      closeOnOverlayClick={false}
      onClose={handleCancel}
      footer={
        <button
          onClick={handleCancel}
          type="button"
          className="rounded-md border border-gray-300 px-4 py-2 text-sm font-medium hover:surface-hover focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 dark:border-dark-600 dark:focus:ring-offset-dark-800 surface-control surface-muted"
        >
          {t('usage.cancelExport')}
        </button>
      }
    >
      <div className="space-y-4">
        <div className="text-sm surface-muted">{t('usage.exportingProgress')}</div>
        <div className="flex items-center justify-between text-sm surface-muted">
          <span>{t('usage.exportedCount', { current, total })}</span>
          <span className="font-medium text-gray-900 dark:text-white">{normalizedProgress}%</span>
        </div>
        <div className="h-2 w-full rounded-full bg-gray-200 dark:bg-dark-700">
          <div
            role="progressbar"
            aria-valuenow={normalizedProgress}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-label={`${t('usage.exportingProgress')}: ${normalizedProgress}%`}
            className="console-data-mark h-2 rounded-full bg-primary-600 transition-all"
            style={{ width: `${normalizedProgress}%` }}
          ></div>
        </div>
        {estimatedTime && (
          <div className="text-xs surface-muted" aria-live="polite" aria-atomic="true">
            {t('usage.estimatedTime', { time: estimatedTime })}
          </div>
        )}
      </div>
    </Modal>
  )
}
