import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { opsAPI, type AlertEvent } from '@/api/admin/ops'
import { Button, Select } from '@/components/ui'
import { toast } from '@/components/ui'
import { formatDateTime } from '@/utils/format'
import { getSeverityClass, truncateMessage } from '../utils/opsFormatters'

export interface OpsAlertEventsCardProps {
  workspace?: boolean
}

/** Alert event feed with severity filter and status resolution. */
export function OpsAlertEventsCard({ workspace = false }: OpsAlertEventsCardProps) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<AlertEvent[]>([])
  const [limit, setLimit] = useState(50)
  const [severity, setSeverity] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    void (async () => {
      try {
        const res = await opsAPI.listAlertEvents({
          limit,
          severity: severity || undefined
        })
        if (cancelled) return
        setRows(res || [])
      } catch (error) {
        if (!cancelled) toast.error((error as { message?: string })?.message || t('common.error'))
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [limit, severity, t])

  const severityOptions = [
    { value: '', label: t('admin.ops.allSeverities') },
    { value: 'P0', label: 'P0' },
    { value: 'P1', label: 'P1' },
    { value: 'P2', label: 'P2' },
    { value: 'P3', label: 'P3' }
  ]

  async function resolve(event: AlertEvent) {
    try {
      await opsAPI.updateAlertEventStatus(event.id, 'manual_resolved')
      setRows((previous) =>
        previous.map((entry) => (entry.id === event.id ? { ...entry, status: 'manual_resolved' } : entry))
      )
      toast.success(t('common.success'))
    } catch (error) {
      toast.error((error as { message?: string })?.message || t('common.error'))
    }
  }

  const statusClass = (status: string): string => {
    if (status === 'firing') return 'badge-danger'
    if (status === 'resolved' || status === 'manual_resolved') return 'badge-success'
    return 'badge-gray'
  }

  return (
    <div className={`card ${workspace ? 'workspace-shell flex flex-col' : ''}`}>
      <div className="flex flex-wrap items-center gap-2 border-b border-gray-100 p-4 dark:border-dark-700">
        <Select
          value={severity}
          searchable={false}
          className="w-32"
          options={severityOptions}
          onValueChange={(next) => setSeverity(String(next ?? ''))}
        />
        {loading && <span className="text-xs text-gray-400">{t('common.loading')}</span>}
      </div>

      <div className={workspace ? 'min-h-0 flex-1 overflow-auto' : ''}>
        <table className="record-table w-full min-w-max text-xs">
          <thead className="table-header surface-inset">
            <tr>
              <th className="px-3 py-2 text-left">{t('common.status')}</th>
              <th className="px-3 py-2 text-left">{t('admin.ops.severity')}</th>
              <th className="px-3 py-2 text-left">{t('admin.ops.alertTitle')}</th>
              <th className="px-3 py-2 text-left">{t('payment.orders.createdAt')}</th>
              <th className="px-3 py-2 text-right">{t('common.actions')}</th>
            </tr>
          </thead>
          <tbody className="table-body divide-y divide-gray-200 dark:divide-dark-700">
            {rows.map((row) => (
              <tr key={row.id} className="table-data-row">
                <td className="px-3 py-2">
                  <span className={`badge ${statusClass(row.status)}`}>{row.status}</span>
                </td>
                <td className="px-3 py-2">
                  <span className={`rounded px-2 py-0.5 text-xs font-medium ${getSeverityClass(row.severity)}`}>
                    {String(row.severity || '')}
                  </span>
                </td>
                <td className="px-3 py-2 text-gray-900 dark:text-gray-100" title={row.title || row.description || ''}>
                  {truncateMessage(row.title || row.description || '')}
                </td>
                <td className="whitespace-nowrap px-3 py-2 surface-muted">{formatDateTime(row.fired_at)}</td>
                <td className="px-3 py-2 text-right">
                  {row.status === 'firing' && (
                    <Button variant="secondary" size="xs" onClick={() => void resolve(row)}>
                      {t('admin.ops.resolve')}
                    </Button>
                  )}
                </td>
              </tr>
            ))}
            {rows.length === 0 && !loading && (
              <tr>
                <td colSpan={5} className="empty-state-cell">
                  <div className="empty-state-fill surface-muted">
                    <p className="text-sm">{t('common.noData')}</p>
                  </div>
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      <div className="flex items-center justify-end gap-2 border-t border-gray-100 p-3 dark:border-dark-700">
        <span className="text-xs surface-muted">{t('admin.ops.eventCount', { count: rows.length })}</span>
        <Button variant="secondary" size="xs" onClick={() => setLimit((previous) => previous + 50)}>
          {t('common.loadMore')}
        </Button>
      </div>
    </div>
  )
}

export default OpsAlertEventsCard
