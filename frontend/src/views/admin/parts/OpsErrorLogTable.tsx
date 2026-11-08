import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/components/DataTable'
import { IpGeoCell } from '@/components/IpGeoCell'
import { IpGeoBatchToolbar } from '@/components/IpGeoBatchToolbar'
import { EmptyState, Pagination } from '@/components/ui'
import { Icon } from '@/components/Icon'
import { useAppStore } from '@/stores/app'
import type { OpsErrorLog } from '@/api/admin/ops'
import { formatDateTime } from '@/utils/format'
import { mapErrorCategory } from '@/utils/errorCategory'
import { mapErrorSortKey, statusCodeBadgeClass } from '@/utils/errorBadges'
import { getSeverityClass } from '@/views/admin/ops/utils/opsFormatters'

export interface OpsErrorLogTableProps {
  rows: OpsErrorLog[]
  total: number
  loading: boolean
  page: number
  pageSize: number
  /** 用户邮箱可点击(emit userClick),仅在有弹窗承接的使用方开启 */
  userClickable?: boolean
  /** 列设置:仅显示这些 key 的列;不传则全量 */
  visibleColumnKeys?: string[]
  /** 运维弹窗优先展示时间和响应内容 */
  summaryFirst?: boolean
  /** Keep filters and pagination outside the bounded table viewport. */
  workspace?: boolean
  compact?: boolean
  activeRowKey?: number
  onOpenErrorDetail?: (id: number) => void
  onPageChange?: (page: number) => void
  onPageSizeChange?: (pageSize: number) => void
  onIpGeoBatchFailed?: () => void
  onSort?: (sortBy: string, sortOrder: 'asc' | 'desc') => void
  onUserClick?: (userId: number, email?: string) => void
}

// 列序对齐管理端用量明细:身份(用户→Key→账号)→ 请求形态(平台→模型→端点→分组→类型)
// → 结果(状态→消息)→ 时间→UA→IP→操作
const ALL_COLUMNS: Column[] = [
  { key: 'user', label: 'admin.ops.errorLog.user' },
  { key: 'api_key', label: 'admin.ops.errorLog.apiKey' },
  { key: 'account', label: 'admin.ops.errorLog.account' },
  { key: 'platform', label: 'admin.ops.errorLog.platform' },
  { key: 'model', label: 'admin.ops.errorLog.model', sortable: true },
  { key: 'endpoint', label: 'admin.ops.errorLog.endpoint' },
  { key: 'group', label: 'admin.ops.errorLog.group' },
  { key: 'type', label: 'admin.ops.errorLog.type' },
  { key: 'category', label: 'usage.errors.category' },
  { key: 'status', label: 'admin.ops.errorLog.status', sortable: true },
  { key: 'message', label: 'admin.ops.errorLog.message' },
  { key: 'created_at', label: 'admin.ops.errorLog.time', sortable: true },
  { key: 'user_agent', label: 'usage.userAgent' },
  { key: 'client_ip', label: 'admin.ops.errorLog.ip' },
  { key: 'actions', label: 'admin.ops.errorLog.action' }
]

const compactWidths: Record<string, number> = {
  created_at: 124,
  user: 140,
  account: 140,
  model: 160,
  category: 100,
  status: 120,
  message: 240
}

function isUpstreamRow(log: OpsErrorLog): boolean {
  const phase = String(log.phase || '').toLowerCase()
  const owner = String(log.error_owner || '').toLowerCase()
  return phase === 'upstream' && owner === 'provider'
}

function hasModelMapping(log: OpsErrorLog): boolean {
  const requested = String(log.requested_model || '').trim()
  const upstream = String(log.upstream_model || '').trim()
  return !!requested && !!upstream && requested !== upstream
}

function displayModel(log: OpsErrorLog): string {
  const upstream = String(log.upstream_model || '').trim()
  if (upstream) return upstream
  const requested = String(log.requested_model || '').trim()
  if (requested) return requested
  return String(log.model || '').trim()
}

export function OpsErrorLogTable({
  rows,
  total,
  loading,
  page,
  pageSize,
  userClickable = false,
  visibleColumnKeys,
  summaryFirst = false,
  workspace = false,
  compact = false,
  activeRowKey,
  onOpenErrorDetail,
  onPageChange,
  onPageSizeChange,
  onIpGeoBatchFailed,
  onSort,
  onUserClick
}: OpsErrorLogTableProps) {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)
  const [copiedUserAgent, setCopiedUserAgent] = useState<string | null>(null)

  const columns = useMemo<Column[]>(() => {
    const visible = visibleColumnKeys ? ALL_COLUMNS.filter((c) => visibleColumnKeys.includes(c.key)) : ALL_COLUMNS
    const withLabels = visible.map((column) => ({ ...column, label: t(column.label) }))
    if (compact) {
      return [...withLabels.filter((column) => column.key === 'created_at'), ...withLabels.filter((column) => column.key !== 'created_at')]
    }
    if (!summaryFirst) return withLabels

    return [
      ...withLabels.filter((c) => c.key === 'created_at'),
      ...withLabels.filter((c) => c.key === 'message'),
      ...withLabels.filter((c) => c.key !== 'created_at' && c.key !== 'message')
    ]
  }, [visibleColumnKeys, compact, summaryFirst, t])

  const compactTableWidth = useMemo(
    () => columns.reduce((width, column) => width + (compactWidths[column.key] ?? 160), 0),
    [columns]
  )

  const copyUserAgent = async (userAgent: string) => {
    try {
      await navigator.clipboard.writeText(userAgent)
      setCopiedUserAgent(userAgent)
      showSuccess(t('admin.usage.userAgentCopied'))
      window.setTimeout(() => {
        setCopiedUserAgent((current) => (current === userAgent ? null : current))
      }, 2000)
    } catch {
      showError(t('common.copyFailed'))
    }
  }

  const formatRequestType = (type: number | null | undefined): string => {
    switch (type) {
      case 1:
        return t('admin.ops.errorLog.requestTypeSync')
      case 2:
        return t('admin.ops.errorLog.requestTypeStream')
      case 3:
        return t('admin.ops.errorLog.requestTypeWs')
      default:
        return ''
    }
  }

  // 徽章配色对齐用量明细(UsageTable)的 bg-X-100/text-X-800 体系
  const getTypeBadge = (log: OpsErrorLog): { label: string; className: string } => {
    const phase = String(log.phase || '').toLowerCase()
    const owner = String(log.error_owner || '').toLowerCase()

    if (isUpstreamRow(log)) {
      return {
        label: t('admin.ops.errorLog.typeUpstream'),
        className: 'bg-red-100/[0.15] text-red-800 dark:bg-red-900/25 dark:text-red-200'
      }
    }
    if (phase === 'request' && owner === 'client') {
      return {
        label: t('admin.ops.errorLog.typeRequest'),
        className: 'bg-amber-100/[0.15] text-amber-800 dark:bg-amber-900/25 dark:text-amber-200'
      }
    }
    if (phase === 'auth' && owner === 'client') {
      return {
        label: t('admin.ops.errorLog.typeAuth'),
        className: 'bg-blue-100/[0.15] text-blue-800 dark:bg-blue-900/25 dark:text-blue-200'
      }
    }
    if (phase === 'account_auth') {
      return {
        label: t('admin.ops.errorLog.typeAccountAuth'),
        className: 'bg-orange-100/[0.15] text-orange-800 dark:bg-orange-900/25 dark:text-orange-200'
      }
    }
    if (phase === 'routing' && owner === 'platform') {
      return {
        label: t('admin.ops.errorLog.typeRouting'),
        className: 'bg-purple-100/[0.15] text-purple-800 dark:bg-purple-900/25 dark:text-purple-200'
      }
    }
    if (phase === 'internal' && owner === 'platform') {
      return {
        label: t('admin.ops.errorLog.typeInternal'),
        className: 'surface-inset text-gray-800 dark:surface-inset dark:text-gray-200'
      }
    }

    const fallback = phase || owner || t('common.unknown')
    return { label: fallback, className: 'surface-inset text-gray-800 dark:surface-inset dark:text-gray-200' }
  }

  const formatSmartMessage = (msg: string): string => {
    if (!msg) return ''

    if (msg.startsWith('{') || msg.startsWith('[')) {
      try {
        const obj = JSON.parse(msg)
        if (obj?.error?.message) return String(obj.error.message)
        if (obj?.message) return String(obj.message)
        if (obj?.detail) return String(obj.detail)
        if (typeof obj === 'object') return JSON.stringify(obj).substring(0, 150)
      } catch {
        // ignore parse error
      }
    }

    if (msg.includes('context deadline exceeded')) return t('admin.ops.errorLog.commonErrors.contextDeadlineExceeded')
    if (msg.includes('connection refused')) return t('admin.ops.errorLog.commonErrors.connectionRefused')
    if (msg.toLowerCase().includes('rate limit')) return t('admin.ops.errorLog.commonErrors.rateLimit')

    return msg.length > 200 ? `${msg.substring(0, 200)}...` : msg
  }

  const renderCell = (key: string, row: OpsErrorLog, value: unknown): React.ReactNode => {
    switch (key) {
      case 'created_at':
        return compact ? (
          <button
            type="button"
            className="text-left text-sm text-primary-600 underline decoration-dotted surface-control"
            aria-label={t('inspection.view')}
            onClick={(event) => {
              event.stopPropagation()
              onOpenErrorDetail?.(row.id)
            }}
          >
            {formatDateTime(row.created_at)}
          </button>
        ) : (
          <span className="text-sm surface-muted" title={row.request_id || row.client_request_id}>
            {formatDateTime(row.created_at)}
          </span>
        )
      case 'type': {
        const badge = getTypeBadge(row)
        return (
          <span className={`inline-flex items-center rounded px-2 py-0.5 text-xs font-medium ${badge.className}`}>
            {badge.label}
          </span>
        )
      }
      case 'endpoint':
        return (
          <div className="max-w-[320px] space-y-1 text-xs">
            <div className="break-all surface-muted">
              <span className="font-medium surface-muted">{t('usage.inbound')}:</span>
              <span className="ml-1">{row.inbound_endpoint?.trim() || '-'}</span>
            </div>
            {row.upstream_endpoint && (
              <div className="break-all surface-muted">
                <span className="font-medium surface-muted">{t('usage.upstream')}:</span>
                <span className="ml-1">{row.upstream_endpoint?.trim() || '-'}</span>
              </div>
            )}
          </div>
        )
      case 'platform':
        return <span className="text-sm text-gray-900 dark:text-white">{row.platform || '-'}</span>
      case 'model':
        if (hasModelMapping(row)) {
          return (
            <div className="space-y-0.5 text-xs">
              <div className="break-all font-medium text-gray-900 dark:text-white">{row.requested_model}</div>
              <div className="break-all surface-muted">
                <span className="mr-0.5">↳</span>
                {row.upstream_model}
              </div>
            </div>
          )
        }
        if (displayModel(row)) {
          return <span className="text-sm font-medium text-gray-900 dark:text-white">{displayModel(row)}</span>
        }
        return <span className="text-sm surface-muted">-</span>
      case 'group':
        return row.group_id ? (
          <span
            className="inline-flex items-center rounded bg-indigo-100/[0.15] px-2 py-0.5 text-xs font-medium text-indigo-800 dark:bg-indigo-900/25 dark:text-indigo-200"
            title={`${t('admin.ops.errorLog.id')} ${row.group_id}`}
          >
            {row.group_name || `#${row.group_id}`}
          </span>
        ) : (
          <span className="text-sm surface-muted">-</span>
        )
      case 'user':
        if (!row.user_id) return <span className="text-sm surface-muted">-</span>
        return (
          <div className={`text-sm ${compact ? 'max-w-[180px]' : ''}`}>
            {userClickable && row.user_email ? (
              <button
                className="max-w-full truncate font-medium text-primary-600 underline decoration-dashed underline-offset-2 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300 surface-control"
                title={`${row.user_email} · ${t('admin.usage.clickToViewBalance')}`}
                onClick={(event) => {
                  event.stopPropagation()
                  onUserClick?.(row.user_id as number, row.user_email)
                }}
              >
                {row.user_email}
              </button>
            ) : (
              <span
                className={`font-medium text-gray-900 dark:text-white ${compact ? 'block truncate' : ''}`}
                title={row.user_email}
              >
                {row.user_email || `#${row.user_id}`}
              </span>
            )}
            {!compact && <span className="ml-1 surface-muted">#{row.user_id}</span>}
            {compact && !columns.some((column) => column.key === 'api_key') && (
              <div className="truncate text-xs surface-muted" title={row.api_key_name}>
                {row.api_key_name || (row.api_key_id ? `#${row.api_key_id}` : '—')}
                {row.api_key_deleted && <> · {t('admin.ops.errorLog.keyDeletedBadge')}</>}
              </div>
            )}
          </div>
        )
      case 'api_key':
        return row.api_key_id || row.api_key_name ? (
          <div className="text-sm">
            <span className="text-gray-900 dark:text-white">{row.api_key_name || `#${row.api_key_id}`}</span>
            {row.api_key_deleted && (
              <span className="ml-1 inline-flex items-center rounded bg-rose-100/[0.15] px-1 py-px text-[10px] font-medium leading-tight text-rose-600 ring-1 ring-inset ring-rose-200 dark:bg-rose-500/20 dark:text-rose-400 dark:ring-rose-500/30">
                {t('admin.ops.errorLog.keyDeletedBadge')}
              </span>
            )}
          </div>
        ) : (
          <span className="text-sm surface-muted">-</span>
        )
      case 'account':
        return (
          <>
            {row.account_id ? (
              <span
                className={`text-sm text-gray-900 dark:text-white ${compact ? 'block max-w-[160px] truncate' : ''}`}
                title={`${row.account_name || ''} · ${t('admin.ops.errorLog.accountId')} ${row.account_id}`}
              >
                {row.account_name || `#${row.account_id}`}
              </span>
            ) : (
              <span className="text-sm surface-muted">-</span>
            )}
            {compact && !columns.some((column) => column.key === 'group') && (
              <div className="max-w-[160px] truncate text-xs surface-muted" title={row.group_name}>
                {row.group_name || (row.group_id ? `#${row.group_id}` : '—')}
              </div>
            )}
          </>
        )
      case 'category':
        return (
          <span className="text-sm text-gray-900 dark:text-white">
            {t(`usage.errors.categories.${mapErrorCategory(row.phase, row.type)}`)}
          </span>
        )
      case 'status':
        return (
          <div className="flex items-center gap-1.5">
            <span
              className={`inline-flex items-center rounded px-2 py-0.5 text-xs font-medium ${statusCodeBadgeClass(row.status_code)}`}
            >
              {row.status_code}
            </span>
            {row.severity && (
              <span className={`rounded px-1.5 py-0.5 text-[10px] font-medium ${getSeverityClass(row.severity)}`}>
                {row.severity}
              </span>
            )}
            {row.request_type != null && row.request_type > 0 && (
              <span className="inline-flex items-center rounded surface-inset px-2 py-0.5 text-xs font-medium text-gray-800 dark:text-gray-200">
                {formatRequestType(row.request_type)}
              </span>
            )}
          </div>
        )
      case 'message':
        return row.message ? (
          <span className="block max-w-[280px] truncate text-sm surface-muted" title={row.message}>
            {formatSmartMessage(row.message) || '-'}
          </span>
        ) : (
          <span className="text-sm surface-muted">-</span>
        )
      case 'user_agent':
        return row.user_agent ? (
          <div
            className="flex max-w-[320px] items-center gap-1.5"
            onClick={(event) => event.stopPropagation()}
          >
            <span className="truncate text-sm surface-muted" title={row.user_agent}>
              {row.user_agent}
            </span>
            <button
              type="button"
              className={`shrink-0 rounded p-0.5 transition-colors hover:surface-hover hover:text-gray-600 dark:hover:text-gray-300 surface-control surface-muted ${
                copiedUserAgent === row.user_agent ? 'text-green-500 hover:text-green-500' : ''
              }`}
              title={copiedUserAgent === row.user_agent ? t('keys.copied') : t('keys.copyToClipboard')}
              onClick={() => void copyUserAgent(row.user_agent as string)}
            >
              <Icon name={copiedUserAgent === row.user_agent ? 'check' : 'copy'} size="sm" className="h-3.5 w-3.5" />
            </button>
          </div>
        ) : (
          <span className="text-sm surface-muted">-</span>
        )
      case 'client_ip':
        return (
          <div onClick={(event) => event.stopPropagation()}>
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
      case 'actions':
        return (
          <button
            type="button"
            className="rounded p-1 transition-colors hover:surface-hover hover:text-primary-600 dark:hover:text-primary-400 surface-control surface-muted"
            title={t('admin.ops.errorLog.details')}
            onClick={(event) => {
              event.stopPropagation()
              onOpenErrorDetail?.(row.id)
            }}
          >
            <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth="2"
                d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
              />
            </svg>
          </button>
        )
      default:
        return value === null || value === undefined ? '' : String(value)
    }
  }

  return (
    <div
      className={`flex h-full min-h-0 flex-col ${compact ? 'error-compact' : ''} ${
        workspace ? 'workspace-body gap-4' : ''
      }`}
      style={compact ? ({ '--error-table-width': `${compactTableWidth}px` } as React.CSSProperties) : undefined}
    >
      <div
        className={`card flex min-h-0 flex-1 flex-col overflow-hidden ${
          workspace ? 'workspace-table-card workspace-shell' : ''
        }`}
      >
        <IpGeoBatchToolbar ips={rows.map((row) => row.client_ip)} onFailed={onIpGeoBatchFailed} />

        <DataTable
          className={workspace ? 'workspace-table-viewport' : undefined}
          columns={columns}
          data={rows}
          loading={loading}
          activeRowKey={activeRowKey ?? null}
          clickableRows
          serverSideSort
          defaultSortKey="created_at"
          defaultSortOrder="desc"
          onSort={(key, order) => {
            onSort?.(mapErrorSortKey(key), order)
          }}
          onRowClick={(row) => onOpenErrorDetail?.((row as OpsErrorLog).id)}
          renderCell={(key, row, cellValue) => renderCell(key, row as OpsErrorLog, cellValue)}
          renderEmpty={() => <EmptyState title={t('admin.ops.errorLog.noErrors')} />}
        />
      </div>

      <div className={`flex-shrink-0 ${workspace ? 'workspace-fixed' : ''}`}>
        {(workspace || total > 0) && (
          <Pagination
            total={total}
            page={page}
            pageSize={pageSize}
            onPageChange={onPageChange}
            onPageSizeChange={onPageSizeChange}
          />
        )}
      </div>
    </div>
  )
}

export default OpsErrorLogTable
