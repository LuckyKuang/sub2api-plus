import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { opsAPI, type OpsSystemLog } from '@/api/admin/ops'
import { Pagination, Select } from '@/components/ui'
import { toast } from '@/components/ui'
import { formatDateTime } from '@/utils/format'
import { truncateMessage } from '../utils/opsFormatters'

export interface OpsSystemLogTableProps {
  platformFilter?: string
  refreshToken?: number
  className?: string
}

/** Runtime system log viewer with level filters and pagination. */
export function OpsSystemLogTable({ platformFilter = '', refreshToken = 0, className }: OpsSystemLogTableProps) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<OpsSystemLog[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [level, setLevel] = useState('')
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    void (async () => {
      try {
        const res = await opsAPI.listSystemLogs({
          page,
          page_size: pageSize,
          platform: platformFilter || undefined,
          level: level || undefined,
          q: query.trim() || undefined,
          time_range: '24h'
        })
        if (cancelled) return
        setRows(res.items || [])
        setTotal(res.total || 0)
      } catch (error) {
        if (!cancelled) toast.error((error as { message?: string })?.message || t('common.error'))
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [page, pageSize, level, query, platformFilter, refreshToken, t])

  const levelOptions = [
    { value: '', label: t('admin.ops.allLevels') },
    { value: 'info', label: 'info' },
    { value: 'warn', label: 'warn' },
    { value: 'error', label: 'error' },
    { value: 'debug', label: 'debug' }
  ]

  const levelClass = (value: string): string => {
    if (value === 'error' || value === 'fatal') return 'badge-danger'
    if (value === 'warn' || value === 'warning') return 'badge-warning'
    if (value === 'debug') return 'badge-gray'
    return 'badge-primary'
  }

  return (
    <div className={`card workspace-shell flex flex-col ${className ?? ''}`}>
      <div className="flex flex-wrap items-center gap-2 border-b border-gray-100 p-4 dark:border-dark-700">
        <input
          className="input max-w-xs"
          placeholder={t('common.search')}
          value={query}
          onChange={(event) => {
            setQuery(event.target.value)
            setPage(1)
          }}
        />
        <Select
          value={level}
          searchable={false}
          className="w-32"
          options={levelOptions}
          onValueChange={(next) => {
            setLevel(String(next ?? ''))
            setPage(1)
          }}
        />
        {loading && <span className="text-xs text-gray-400">{t('common.loading')}</span>}
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        <table className="record-table w-full min-w-max text-xs">
          <thead className="table-header surface-inset">
            <tr>
              <th className="px-3 py-2 text-left">{t('payment.orders.createdAt')}</th>
              <th className="px-3 py-2 text-left">{t('admin.ops.level')}</th>
              <th className="px-3 py-2 text-left">{t('admin.ops.component')}</th>
              <th className="px-3 py-2 text-left">{t('admin.ops.message')}</th>
              <th className="px-3 py-2 text-left">{t('admin.ops.host')}</th>
            </tr>
          </thead>
          <tbody className="table-body divide-y divide-gray-200 dark:divide-dark-700">
            {rows.map((row) => (
              <tr key={row.id} className="table-data-row">
                <td className="whitespace-nowrap px-3 py-2 surface-muted">{formatDateTime(row.created_at)}</td>
                <td className="px-3 py-2">
                  <span className={`badge ${levelClass(row.level)}`}>{row.level}</span>
                </td>
                <td className="whitespace-nowrap px-3 py-2 surface-muted">{row.component}</td>
                <td className="px-3 py-2 text-gray-900 dark:text-gray-100" title={row.message}>
                  {truncateMessage(row.message)}
                </td>
                <td className="whitespace-nowrap px-3 py-2 surface-muted">{row.host}</td>
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

      <div className="border-t border-gray-100 p-3 dark:border-dark-700">
        {total > 0 && (
          <Pagination
            page={page}
            total={total}
            pageSize={pageSize}
            onPageChange={setPage}
            onPageSizeChange={(size) => {
              setPageSize(size)
              setPage(1)
            }}
          />
        )}
      </div>
    </div>
  )
}

export default OpsSystemLogTable
