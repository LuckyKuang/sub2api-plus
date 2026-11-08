import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { DetailContentHost } from '@/components/DetailContentHost'
import { getMyErrorDetail } from '@/api/usage'
import { formatDateTime } from '@/utils/format'
import { statusCodeBadgeClass } from '@/utils/errorBadges'
import type { UserErrorRequestDetail } from '@/types'

export interface UserErrorDetailModalProps {
  embedded?: boolean
  show: boolean
  errorId: number | null
  onClose?: () => void
}

export function UserErrorDetailModal({ embedded = false, show, errorId, onClose }: UserErrorDetailModalProps) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState(false)
  const [detail, setDetail] = useState<UserErrorRequestDetail | null>(null)
  const generation = useRef(0)

  useEffect(() => {
    generation.current += 1
    if (show && errorId != null) {
      void fetchDetail(errorId)
    } else if (!show) {
      setDetail(null)
      setLoadError(false)
    }
  }, [show, errorId])

  useEffect(() => {
    return () => {
      generation.current += 1
    }
  }, [])

  async function fetchDetail(id: number) {
    const seq = generation.current
    setLoading(true)
    setLoadError(false)
    setDetail(null)
    try {
      const result = await getMyErrorDetail(id)
      if (seq === generation.current) setDetail(result)
    } catch (e) {
      console.error('[UserErrorDetailModal] Failed to load error detail:', e)
      if (seq === generation.current) setLoadError(true)
    } finally {
      if (seq === generation.current) setLoading(false)
    }
  }

  return (
    <DetailContentHost embedded={embedded} show={show} title={t('usage.errors.detail.title')} onClose={onClose}>
      {loading ? (
        <div className="flex justify-center py-10">
          <svg className="h-7 w-7 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path
              className="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            />
          </svg>
        </div>
      ) : loadError ? (
        <div className="py-8 text-center text-sm text-red-500">{t('usage.errors.detail.loadFailed')}</div>
      ) : detail ? (
        <div className="space-y-4 text-sm">
          <div className="grid grid-cols-2 gap-x-6 gap-y-3">
            <div>
              <span className="font-medium surface-muted">{t('usage.errors.time')}</span>
              <p className="mt-0.5 text-gray-900 dark:text-dark-100">{formatDateTime(detail.created_at)}</p>
            </div>
            <div>
              <span className="font-medium surface-muted">{t('usage.errors.model')}</span>
              <p className="mt-0.5 text-gray-900 dark:text-dark-100">{detail.model || '-'}</p>
            </div>
            <div>
              <span className="font-medium surface-muted">{t('usage.errors.endpoint')}</span>
              <p className="mt-0.5 text-gray-900 dark:text-dark-100">{detail.inbound_endpoint || '-'}</p>
            </div>
            <div>
              <span className="font-medium surface-muted">{t('usage.errors.status')}</span>
              <p className="mt-0.5">
                <span className={`badge ${statusCodeBadgeClass(detail.status_code)}`}>{detail.status_code || '-'}</span>
              </p>
            </div>
            <div>
              <span className="font-medium surface-muted">{t('usage.errors.category')}</span>
              <p className="mt-0.5 text-gray-900 dark:text-dark-100">{t('usage.errors.categories.' + detail.category)}</p>
            </div>
            <div>
              <span className="font-medium surface-muted">{t('usage.errors.platform')}</span>
              <p className="mt-0.5 text-gray-900 dark:text-dark-100">{detail.platform || '-'}</p>
            </div>
            {/* Async task correlation is safe because this detail is already owner-scoped. */}
            {detail.async_task_id && (
              <div>
                <span className="font-medium surface-muted">{t('usage.errors.detail.asyncTaskId')}</span>
                <p className="mt-0.5 break-all font-mono text-gray-900 dark:text-dark-100">{detail.async_task_id}</p>
              </div>
            )}
            {detail.upstream_status_code != null && (
              <div>
                <span className="font-medium surface-muted">{t('usage.errors.detail.upstreamStatus')}</span>
                <p className="mt-0.5 text-gray-900 dark:text-dark-100">{detail.upstream_status_code}</p>
              </div>
            )}
          </div>

          {detail.message && (
            <div>
              <span className="font-medium surface-muted">{t('usage.errors.message')}</span>
              <p className="mt-0.5 break-all text-gray-900 dark:text-dark-100">{detail.message}</p>
            </div>
          )}

          {detail.error_body && (
            <div>
              <span className="font-medium surface-muted">{t('usage.errors.detail.responseBody')}</span>
              <pre className="surface-inset mt-1 max-h-[40vh] overflow-auto whitespace-pre-wrap break-all rounded-lg border border-gray-200 p-3 text-xs text-gray-800 dark:border-dark-700 dark:text-dark-200">
                {detail.error_body}
              </pre>
            </div>
          )}
        </div>
      ) : null}
    </DetailContentHost>
  )
}

export default UserErrorDetailModal
