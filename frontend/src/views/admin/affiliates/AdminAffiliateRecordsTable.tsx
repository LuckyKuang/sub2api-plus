import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Modal, Pagination } from '@/components/ui'
import { toast } from '@/components/ui'
import { affiliatesAPI, type ListAffiliateRecordsParams } from '@/api/admin/affiliates'
import type { AffiliateInviteRecord, AffiliateRebateRecord, AffiliateTransferRecord, AffiliateUserOverview } from '@/api/admin/affiliates'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { formatDateTime as formatDisplayDateTime } from '@/utils/format'
import { AdminDataTable, type Column, type SortOrder } from '@/views/admin/AdminDataTable'
import { AdminTablePageLayout } from '@/views/admin/AdminTablePageLayout'
import { Icon } from '@/views/admin/AccountIcon'
import { DateTimePicker } from '../parts/DateTimePicker'
import { OrderStatusBadge } from '../parts/OrderStatusBadge'

type RecordType = 'invites' | 'rebates' | 'transfers'
type AffiliateRecord = AffiliateInviteRecord | AffiliateRebateRecord | AffiliateTransferRecord

export interface AdminAffiliateRecordsTableProps {
  type: RecordType
}

function formatAmount(value: number | null | undefined): string {
  return Number(value || 0).toFixed(2)
}

function formatPercent(value: number | null | undefined): string {
  const rounded = Math.round(Number(value || 0) * 100) / 100
  return `${Number.isInteger(rounded) ? rounded.toString() : rounded.toString()}%`
}

function userTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone
  } catch {
    return 'UTC'
  }
}

/** Shared records table for the admin affiliate screens. */
export function AdminAffiliateRecordsTable({ type }: AdminAffiliateRecordsTableProps) {
  const { t } = useTranslation()

  const [records, setRecords] = useState<AffiliateRecord[]>([])
  const [loading, setLoading] = useState(false)
  const [search, setSearch] = useState('')
  const [startAt, setStartAt] = useState('')
  const [endAt, setEndAt] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize] = useState(20)
  const [total, setTotal] = useState(0)
  const [sortKey, setSortKey] = useState('created_at')
  const [sortOrder, setSortOrder] = useState<SortOrder>('desc')

  const [overviewDialog, setOverviewDialog] = useState(false)
  const [overviewLoading, setOverviewLoading] = useState(false)
  const [selectedOverview, setSelectedOverview] = useState<AffiliateUserOverview | null>(null)

  const debounceTimer = useRef<number | null>(null)
  const stateRef = useRef({ page, pageSize, search, startAt, endAt, sortKey, sortOrder })
  stateRef.current = { page, pageSize, search, startAt, endAt, sortKey, sortOrder }

  const sortStorageKey = `admin-affiliate-${type}-table-sort`

  const columns = useMemo<Column[]>(() => {
    if (type === 'invites') {
      return [
        { key: 'inviter', label: t('admin.affiliates.records.inviter'), sortable: true },
        { key: 'invitee', label: t('admin.affiliates.records.invitee'), sortable: true },
        { key: 'aff_code', label: t('admin.affiliates.records.affCode'), sortable: true },
        { key: 'total_rebate', label: t('admin.affiliates.records.totalRebate'), sortable: true },
        { key: 'created_at', label: t('admin.affiliates.records.invitedAt'), sortable: true }
      ]
    }
    if (type === 'rebates') {
      return [
        { key: 'order', label: t('admin.affiliates.records.order'), sortable: true },
        { key: 'inviter', label: t('admin.affiliates.records.inviter'), sortable: true },
        { key: 'invitee', label: t('admin.affiliates.records.invitee'), sortable: true },
        { key: 'order_amount', label: t('admin.affiliates.records.orderAmount'), sortable: true },
        { key: 'pay_amount', label: t('admin.affiliates.records.payAmount'), sortable: true },
        { key: 'rebate_amount', label: t('admin.affiliates.records.rebateAmount') },
        { key: 'payment_type', label: t('admin.affiliates.records.paymentType'), sortable: true },
        { key: 'order_status', label: t('admin.affiliates.records.orderStatus'), sortable: true },
        { key: 'created_at', label: t('admin.affiliates.records.rebatedAt'), sortable: true }
      ]
    }
    return [
      { key: 'user', label: t('admin.affiliates.records.user'), sortable: true },
      { key: 'amount', label: t('admin.affiliates.records.transferAmount'), sortable: true },
      { key: 'balance_after', label: t('admin.affiliates.records.balanceAfter'), sortable: true },
      { key: 'available_quota_after', label: t('admin.affiliates.records.availableQuotaAfter'), sortable: true },
      { key: 'frozen_quota_after', label: t('admin.affiliates.records.frozenQuotaAfter'), sortable: true },
      { key: 'history_quota_after', label: t('admin.affiliates.records.historyQuotaAfter'), sortable: true },
      { key: 'created_at', label: t('admin.affiliates.records.transferredAt'), sortable: true }
    ]
  }, [type, t])

  const loadInitialSortState = (): { sort_by: string; sort_order: SortOrder } => {
    const fallback = { sort_by: 'created_at', sort_order: 'desc' as SortOrder }
    try {
      const raw = localStorage.getItem(sortStorageKey)
      if (!raw) return fallback
      const parsed = JSON.parse(raw) as { key?: string; order?: string }
      const key = typeof parsed.key === 'string' ? parsed.key : ''
      if (!columns.some((column) => column.key === key && column.sortable)) return fallback
      return { sort_by: key, sort_order: parsed.order === 'asc' ? 'asc' : 'desc' }
    } catch {
      return fallback
    }
  }

  // Initial sort state is read once from storage, mirroring the Vue loader.
  const initialSort = useRef<{ sort_by: string; sort_order: SortOrder } | null>(null)
  if (initialSort.current === null) initialSort.current = loadInitialSortState()

  const buildParams = (): ListAffiliateRecordsParams => {
    const current = stateRef.current
    return {
      page: current.page,
      page_size: current.pageSize,
      search: current.search.trim() || undefined,
      start_at: current.startAt || undefined,
      end_at: current.endAt || undefined,
      sort_by: current.sortKey,
      sort_order: current.sortOrder,
      timezone: userTimezone()
    }
  }

  async function fetchRecords(params: ListAffiliateRecordsParams): Promise<{ items: AffiliateRecord[]; total: number }> {
    if (type === 'invites') return affiliatesAPI.listInviteRecords(params)
    if (type === 'rebates') return affiliatesAPI.listRebateRecords(params)
    return affiliatesAPI.listTransferRecords(params)
  }

  async function loadRecords() {
    setLoading(true)
    try {
      const res = await fetchRecords(buildParams())
      setRecords(res.items || [])
      setTotal(res.total || 0)
    } catch (error) {
      toast.error(extractI18nErrorMessage(error, t, 'admin.affiliates.errors', t('common.error')))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    setSortKey(initialSort.current!.sort_by)
    setSortOrder(initialSort.current!.sort_order)
    void loadRecords()
    return () => {
      if (debounceTimer.current) window.clearTimeout(debounceTimer.current)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function debounceLoad() {
    if (debounceTimer.current) window.clearTimeout(debounceTimer.current)
    debounceTimer.current = window.setTimeout(() => {
      setPage(1)
      void loadRecords()
    }, 300)
  }

  function reloadFromFirstPage() {
    setPage(1)
    void loadRecords()
  }

  function handleSort(key: string, order: SortOrder) {
    setSortKey(key)
    setSortOrder(order)
    setPage(1)
    try {
      localStorage.setItem(sortStorageKey, JSON.stringify({ key, order }))
    } catch {
      /* restricted storage */
    }
    void loadRecords()
  }

  async function openUserOverview(userId: number) {
    if (!userId) return
    setOverviewDialog(true)
    setOverviewLoading(true)
    setSelectedOverview(null)
    try {
      setSelectedOverview(await affiliatesAPI.getUserOverview(userId))
    } catch (error) {
      setOverviewDialog(false)
      toast.error(extractI18nErrorMessage(error, t, 'admin.affiliates.errors', t('common.error')))
    } finally {
      setOverviewLoading(false)
    }
  }

  const formatDateTime = (value: string | null | undefined): string => (value ? formatDisplayDateTime(value) : '-')

  const userCell = (id: number, email: string, username: string, clickable: boolean) => (
    <div className="space-y-0.5">
      <div className="font-mono text-sm text-gray-900 dark:text-white">#{id}</div>
      {clickable ? (
        <button
          type="button"
          className="max-w-56 truncate text-left text-sm font-medium text-primary-600 hover:text-primary-700 hover:underline dark:text-primary-400 dark:hover:text-primary-300"
          onClick={() => void openUserOverview(id)}
        >
          {email || '-'}
        </button>
      ) : (
        <div className="max-w-56 truncate text-sm surface-muted">{email || '-'}</div>
      )}
      <div className="max-w-56 truncate text-sm surface-muted">{username || '-'}</div>
    </div>
  )

  const amountText = (value: number | null | undefined, strong = false) => (
    <span
      className={
        strong
          ? 'text-sm font-semibold text-emerald-600 dark:text-emerald-400'
          : 'text-sm text-gray-900 dark:text-white'
      }
    >
      ${formatAmount(value)}
    </span>
  )

  const nullableAmountText = (value: number | null | undefined) =>
    value === null || value === undefined ? (
      <span className="text-sm surface-muted">-</span>
    ) : (
      amountText(value)
    )

  const overviewStat = (label: string, value: string, mono = false) => (
    <div className="rounded-lg border border-gray-100 surface-inset p-3 dark:border-dark-700 dark:surface-inset">
      <div className="text-sm surface-muted">{label}</div>
      <div className={mono ? 'mt-1 font-mono text-base font-semibold text-gray-900 dark:text-white' : 'mt-1 text-base font-semibold text-gray-900 dark:text-white'}>
        {value}
      </div>
    </div>
  )

  const renderCell = (key: string, row: AffiliateRecord, value: unknown) => {
    const record = row as unknown as Record<string, unknown>
    const num = (field: string): number => Number((record[field] as number | null | undefined) ?? 0)
    const str = (field: string): string => String(record[field] ?? '')

    if (key === 'inviter') {
      return userCell(num('inviter_id'), str('inviter_email'), str('inviter_username'), type !== 'transfers')
    }
    if (key === 'invitee') {
      return userCell(num('invitee_id'), str('invitee_email'), str('invitee_username'), type !== 'transfers')
    }
    if (key === 'user') {
      return userCell(num('user_id'), str('user_email'), str('username'), true)
    }
    if (key === 'aff_code') {
      return <span className="font-mono text-sm surface-muted">{str('aff_code') || '-'}</span>
    }
    if (key === 'order') {
      return (
        <div className="space-y-0.5">
          <div className="font-mono text-sm text-gray-900 dark:text-white">#{num('order_id')}</div>
          <div className="max-w-56 truncate text-sm surface-muted">{str('out_trade_no')}</div>
        </div>
      )
    }
    if (key === 'payment_type') {
      return t('payment.methods.' + str('payment_type'), str('payment_type') || '-')
    }
    if (key === 'order_status') {
      return <OrderStatusBadge status={str('order_status') as never} />
    }
    if (key === 'total_rebate' || key === 'order_amount') {
      return amountText(value as number)
    }
    if (key === 'pay_amount') {
      return <span className="text-sm text-gray-900 dark:text-white">¥{formatAmount(value as number)}</span>
    }
    if (key === 'rebate_amount' || key === 'amount') {
      return amountText(value as number, true)
    }
    if (
      key === 'balance_after' ||
      key === 'available_quota_after' ||
      key === 'frozen_quota_after' ||
      key === 'history_quota_after'
    ) {
      return nullableAmountText(value as number | null | undefined)
    }
    if (key === 'created_at') {
      return <span className="text-sm surface-muted">{formatDateTime(value as string)}</span>
    }
    return <span>{value == null ? '' : String(value)}</span>
  }

  return (
    <AdminTablePageLayout
      filters={
        <div className="filter-bar">
          <div className="filter-field filter-field--search relative">
            <Icon name="search" size="md" className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <input
              value={search}
              type="text"
              className="input pl-10"
              placeholder={t('admin.affiliates.records.searchPlaceholder')}
              onChange={(event) => {
                setSearch(event.target.value)
                debounceLoad()
              }}
            />
          </div>
          <div className="filter-field">
            <DateTimePicker
              ariaLabel={t('admin.affiliates.records.startAt')}
              type="date"
              value={startAt}
              onValueChange={(next) => {
                setStartAt(next)
                reloadFromFirstPage()
              }}
            />
          </div>
          <div className="filter-field">
            <DateTimePicker
              ariaLabel={t('admin.affiliates.records.endAt')}
              type="date"
              value={endAt}
              onValueChange={(next) => {
                setEndAt(next)
                reloadFromFirstPage()
              }}
            />
          </div>
          <Button variant="secondary" className="px-2 md:px-3" disabled={loading} title={t('common.refresh')} onClick={() => void loadRecords()}>
            <Icon name="refresh" size="md" className={loading ? 'animate-spin' : ''} />
          </Button>
        </div>
      }
      table={
        <AdminDataTable
          columns={columns}
          data={records}
          loading={loading}
          serverSideSort
          sortKey={sortKey}
          sortOrder={sortOrder}
          onSort={handleSort}
          renderCell={renderCell}
        />
      }
      pagination={
        total > 0 ? (
          <Pagination
            page={page}
            total={total}
            pageSize={pageSize}
            onPageChange={(next) => {
              setPage(next)
              void loadRecords()
            }}
            onPageSizeChange={() => {
              setPage(1)
              void loadRecords()
            }}
          />
        ) : undefined
      }
    >
      <Modal
        open={overviewDialog}
        onClose={() => setOverviewDialog(false)}
        title={t('admin.affiliates.overview.title')}
        size="md"
      >
        {overviewLoading ? (
          <div className="flex justify-center py-8">
            <div className="h-6 w-6 animate-spin rounded-full border-2 border-primary-500 border-t-transparent" />
          </div>
        ) : (
          selectedOverview && (
            <div className="space-y-4">
              <div className="rounded-lg border border-gray-100 surface-inset p-4 dark:border-dark-700">
                <div className="font-mono text-sm text-gray-900 dark:text-white">#{selectedOverview.user_id}</div>
                <div className="mt-1 text-sm font-medium text-gray-900 dark:text-white">{selectedOverview.email || '-'}</div>
                <div className="mt-0.5 text-sm surface-muted">{selectedOverview.username || '-'}</div>
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                {overviewStat(t('admin.affiliates.overview.affCode'), selectedOverview.aff_code || '-', true)}
                {overviewStat(t('admin.affiliates.overview.rebateRate'), formatPercent(selectedOverview.rebate_rate_percent))}
                {overviewStat(t('admin.affiliates.overview.invitedCount'), String(selectedOverview.invited_count))}
                {overviewStat(
                  t('admin.affiliates.overview.rebatedInviteeCount'),
                  String(selectedOverview.rebated_invitee_count)
                )}
                {overviewStat(t('admin.affiliates.overview.availableQuota'), '$' + formatAmount(selectedOverview.available_quota))}
                {overviewStat(t('admin.affiliates.overview.historyQuota'), '$' + formatAmount(selectedOverview.history_quota))}
              </div>
            </div>
          )
        )}
      </Modal>
    </AdminTablePageLayout>
  )
}

export default AdminAffiliateRecordsTable
