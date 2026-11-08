import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { status as fetchChannelMonitorDetail, type UserMonitorDetail } from '@/api/channelMonitor'
import { toast } from '@/components/ui'
import { extractApiErrorMessage } from '@/utils/apiError'
import { BaseDialog } from '../BaseDialog'
import { useChannelMonitorFormat } from '../useChannelMonitorFormat'

export interface MonitorDetailDialogProps {
  show: boolean
  monitorId: number | null
  title: string
  onClose?: () => void
}

export function MonitorDetailDialog({ show, monitorId, title, onClose }: MonitorDetailDialogProps) {
  const { t } = useTranslation()
  const { statusLabel, statusBadgeClass, formatLatency, formatPercent, formatMonitorModel } = useChannelMonitorFormat()
  const [detail, setDetail] = useState<UserMonitorDetail | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    let disposed = false
    if (!show) {
      setDetail(null)
      return
    }
    if (monitorId == null) return

    setDetail(null)
    setLoading(true)
    void (async () => {
      try {
        const result = await fetchChannelMonitorDetail(monitorId)
        if (!disposed) setDetail(result)
      } catch (err: unknown) {
        toast.error(extractApiErrorMessage(err, t('channelStatus.detailLoadError')))
      } finally {
        if (!disposed) setLoading(false)
      }
    })()

    return () => {
      disposed = true
    }
  }, [show, monitorId, t])

  return (
    <BaseDialog
      show={show}
      title={title}
      width="wide"
      onClose={onClose}
      footer={
        <div className="flex justify-end">
          <button type="button" className="btn btn-secondary" onClick={onClose}>
            {t('channelStatus.closeDetail')}
          </button>
        </div>
      }
    >
      {loading ? (
        <div className="py-8 text-center text-sm surface-muted">{t('common.loading')}</div>
      ) : !detail ? (
        <div className="py-8 text-center text-sm surface-muted">{t('channelStatus.detailLoadError')}</div>
      ) : (
        <div className="record-table-scroll record-table-dialog-scroll table-container overflow-x-auto">
          <table className="record-table table">
            <thead className="border-b border-gray-200 dark:border-dark-700">
              <tr className="text-xs uppercase tracking-wider surface-muted">
                <th>{t('channelStatus.detailColumns.model')}</th>
                <th>{t('channelStatus.detailColumns.latestStatus')}</th>
                <th>{t('monitorCommon.latestFirstText')}</th>
                <th>{t('monitorCommon.latestFirstByte')}</th>
                <th>{t('usage.latestTotalDuration')}</th>
                <th>{t('channelStatus.detailColumns.availability7d')}</th>
                <th>{t('channelStatus.detailColumns.availability15d')}</th>
                <th>{t('channelStatus.detailColumns.availability30d')}</th>
                <th>{t('monitorCommon.avgFirstText7d')}</th>
                <th>{t('monitorCommon.avgFirstByte7d')}</th>
                <th>{t('usage.avgTotalDuration7d')}</th>
              </tr>
            </thead>
            <tbody>
              {detail.models.map((m) => (
                <tr key={m.model} className="border-b border-gray-100 dark:border-dark-800">
                  <td className="font-medium text-gray-900 dark:text-gray-100">{formatMonitorModel(m.model)}</td>
                  <td>
                    <span
                      className={`inline-flex items-center rounded-full px-2 py-0.5 text-[11px] ${statusBadgeClass(m.latest_status)}`}
                    >
                      {statusLabel(m.latest_status)}
                    </span>
                  </td>
                  <td>{formatLatency(m.latest_first_text_ms)}</td>
                  <td>{formatLatency(m.latest_first_byte_ms)}</td>
                  <td>{formatLatency(m.latest_latency_ms)}</td>
                  <td>{formatPercent(m.availability_7d)}</td>
                  <td>{formatPercent(m.availability_15d)}</td>
                  <td>{formatPercent(m.availability_30d)}</td>
                  <td title={t('monitorCommon.timingHint')}>
                    {formatLatency(m.avg_first_text_7d_ms)}
                    <span className="block text-xs surface-muted">
                      {t('monitorCommon.textSamples', { n: m.first_text_samples_7d ?? 0 })}
                    </span>
                  </td>
                  <td title={t('monitorCommon.timingHint')}>
                    {formatLatency(m.avg_first_byte_7d_ms)}
                    <span className="block text-xs surface-muted">
                      {t('usage.firstByteSamples', { n: m.first_byte_samples_7d ?? 0 })}
                    </span>
                  </td>
                  <td>{formatLatency(m.avg_latency_7d_ms)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </BaseDialog>
  )
}

export default MonitorDetailDialog
