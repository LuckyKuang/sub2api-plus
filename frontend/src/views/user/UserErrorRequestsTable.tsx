import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/components/DataTable'
import { DetailWorkspace } from '@/components/DetailWorkspace'
import { EmptyState, Pagination } from '@/components/ui'
import { IpGeoCell } from '@/components/IpGeoCell'
import { IpGeoBatchToolbar } from '@/components/IpGeoBatchToolbar'
import { formatDateTime } from '@/utils/format'
import {
  mapErrorSortKey,
  numericRequestTypeKind,
  requestTypeBadgeClass,
  requestTypeLabelKey,
  statusCodeBadgeClass
} from '@/utils/errorBadges'
import type { UserErrorRequest } from '@/types'
import { UserErrorDetailModal } from './UserErrorDetailModal'

export interface UserErrorRequestsTableProps {
  rows: UserErrorRequest[]
  total: number
  loading: boolean
  page: number
  pageSize: number
  workspace?: boolean
  /** 列设置:仅显示这些 key 的列;不传则全显(key 须与 allColumns 一致) */
  visibleColumnKeys?: string[]
  onPageChange?: (page: number) => void
  onPageSizeChange?: (pageSize: number) => void
  onIpGeoBatchFailed?: () => void
  onSort?: (sortBy: string, sortOrder: 'asc' | 'desc') => void
}

const compactWidths: Record<string, number> = {
  created_at: 124,
  key_name: 140,
  model: 160,
  category: 100,
  status: 90,
  message: 240
}

export function UserErrorRequestsTable({
  rows,
  total,
  loading,
  page,
  pageSize,
  workspace = false,
  visibleColumnKeys,
  onPageChange,
  onPageSizeChange,
  onIpGeoBatchFailed,
  onSort
}: UserErrorRequestsTableProps) {
  const { t } = useTranslation()
  const [showDetail, setShowDetail] = useState(false)
  const [selectedId, setSelectedId] = useState<number | null>(null)

  const allColumns = useMemo<Column[]>(
    () => [
      { key: 'created_at', label: t('usage.errors.time'), sortable: true },
      { key: 'key_name', label: t('usage.errors.keyName') },
      { key: 'model', label: t('usage.errors.model'), sortable: true },
      { key: 'endpoint', label: t('usage.errors.endpoint') },
      { key: 'client_ip', label: 'IP' },
      { key: 'group', label: t('admin.usage.group') },
      { key: 'type', label: t('usage.type') },
      { key: 'platform', label: t('usage.errors.platform') },
      { key: 'category', label: t('usage.errors.category') },
      { key: 'status', label: t('usage.errors.status'), sortable: true },
      { key: 'message', label: t('usage.errors.message') },
      { key: 'user_agent', label: t('usage.userAgent') }
    ],
    [t]
  )

  const columns = useMemo(
    () => (visibleColumnKeys ? allColumns.filter((c) => visibleColumnKeys.includes(c.key)) : allColumns),
    [allColumns, visibleColumnKeys]
  )

  const compactTableWidth = columns.reduce((width, column) => width + (compactWidths[column.key] ?? 160), 0)

  function requestTypeBadge(row: UserErrorRequest): { label: string; className: string } | null {
    const kind = numericRequestTypeKind(row.request_type, row.stream)
    if (!kind) return null
    return { label: t(requestTypeLabelKey(kind)), className: requestTypeBadgeClass(kind) }
  }

  function openDetail(id: number) {
    setSelectedId(id)
    setShowDetail(true)
  }

  const renderCell = (key: string, row: UserErrorRequest, value: unknown): React.ReactNode => {
    switch (key) {
      case 'model':
        return row.model ? (
          <span className="text-sm font-medium text-gray-900 dark:text-white">{row.model}</span>
        ) : (
          <span className="text-sm surface-muted">-</span>
        )
      case 'key_name':
        return (
          <div className="text-sm">
            <span className="block max-w-[160px] truncate text-gray-900 dark:text-white" title={row.key_name}>
              {row.key_name || '-'}
            </span>
            {row.key_deleted && (
              <span className="ml-1 inline-flex items-center rounded bg-rose-100/[0.15] px-1 py-px text-[10px] font-medium leading-tight text-rose-600 ring-1 ring-inset ring-rose-200 dark:bg-rose-500/20 dark:text-rose-400 dark:ring-rose-500/30">
                {t('usage.errors.keyDeleted')}
              </span>
            )}
            {!columns.some((column) => column.key === 'group') && (
              <span className="block max-w-[160px] truncate text-xs surface-muted" title={row.group_name}>
                {row.group_name || '—'}
              </span>
            )}
          </div>
        )
      case 'endpoint':
        return (
          <div className="max-w-[320px] text-xs">
            <div className="break-all surface-muted">
              <span className="font-medium surface-muted">{t('usage.inbound')}:</span>
              <span className="ml-1">{row.inbound_endpoint?.trim() || '-'}</span>
            </div>
          </div>
        )
      case 'status':
        return (
          <span
            className={`inline-flex items-center rounded px-2 py-0.5 text-xs font-medium ${statusCodeBadgeClass(row.status_code)}`}
          >
            {row.status_code || '-'}
          </span>
        )
      case 'category':
        return <span className="text-sm text-gray-900 dark:text-white">{t('usage.errors.categories.' + row.category)}</span>
      case 'message':
        return row.message ? (
          <span className="block max-w-[280px] truncate text-sm surface-muted" title={row.message}>
            {row.message}
          </span>
        ) : (
          <span className="text-sm surface-muted">-</span>
        )
      case 'group':
        return row.group_name ? (
          <span className="inline-flex items-center rounded bg-indigo-100/[0.15] px-2 py-0.5 text-xs font-medium text-indigo-800 dark:bg-indigo-900/25 dark:text-indigo-200">
            {row.group_name}
          </span>
        ) : (
          <span className="text-sm surface-muted">-</span>
        )
      case 'type': {
        const badge = requestTypeBadge(row)
        return badge ? (
          <span className={`inline-flex items-center rounded px-2 py-0.5 text-xs font-medium ${badge.className}`}>
            {badge.label}
          </span>
        ) : (
          <span className="text-sm surface-muted">-</span>
        )
      }
      case 'platform':
        return <span className="text-sm text-gray-900 dark:text-white">{row.platform || '-'}</span>
      case 'client_ip':
        return (
          <div
            onClick={(event) => event.stopPropagation()}
            onKeyDown={(event) => event.stopPropagation()}
            role="presentation"
          >
            {row.client_ip ? (
              <div>
                <span className="font-mono text-sm surface-muted">{row.client_ip}</span>
                <IpGeoCell ip={row.client_ip} />
              </div>
            ) : (
              <span className="text-sm surface-muted">-</span>
            )}
          </div>
        )
      case 'created_at':
        return (
          <button
            type="button"
            className="surface-control text-left text-sm text-primary-600 underline decoration-dotted"
            aria-label={t('inspection.view')}
            onClick={(event) => {
              event.stopPropagation()
              openDetail(row.id)
            }}
          >
            {formatDateTime(row.created_at)}
          </button>
        )
      case 'user_agent':
        return row.user_agent ? (
          <span className="block max-w-[320px] truncate text-sm surface-muted" title={row.user_agent}>
            {row.user_agent}
          </span>
        ) : (
          <span className="text-sm surface-muted">-</span>
        )
      default:
        return value === null || value === undefined ? '' : String(value)
    }
  }

  return (
    <DetailWorkspace
      workspace={workspace}
      show={showDetail}
      title={t('usage.errors.detail.title')}
      onClose={() => setShowDetail(false)}
      detail={<UserErrorDetailModal embedded show={showDetail} errorId={selectedId} onClose={() => setShowDetail(false)} />}
    >
      <div
        className="error-compact flex min-h-0 flex-1 flex-col gap-4"
        style={{ ['--error-table-width' as string]: `${compactTableWidth}px` }}
      >
        <div className={`card flex min-h-0 flex-1 flex-col overflow-hidden ${workspace ? 'workspace-shell' : ''}`}>
          <IpGeoBatchToolbar ips={rows.map((r) => r.client_ip)} onFailed={onIpGeoBatchFailed} />

          <DataTable
            className={workspace ? 'workspace-table-viewport' : undefined}
            columns={columns}
            data={rows}
            loading={loading}
            activeRowKey={showDetail ? selectedId : undefined}
            clickableRows
            serverSideSort
            defaultSortKey="created_at"
            defaultSortOrder="desc"
            onSort={(key, order) => onSort?.(mapErrorSortKey(key), order)}
            onRowClick={(row) => openDetail((row as UserErrorRequest).id)}
            renderCell={(key, row, cellValue) => renderCell(key, row as UserErrorRequest, cellValue)}
            renderEmpty={() => <EmptyState title={t('usage.errors.empty')} />}
          />
        </div>

        <div className="workspace-fixed flex-shrink-0">
          <Pagination page={page} pageSize={pageSize} total={total} onPageChange={onPageChange} onPageSizeChange={onPageSizeChange} />
        </div>
      </div>
    </DetailWorkspace>
  )
}

export default UserErrorRequestsTable
