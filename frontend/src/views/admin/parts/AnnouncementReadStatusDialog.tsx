import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Modal, Pagination } from '@/components/ui'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
import type { AnnouncementUserReadStatus } from '@/types'
import { getPersistedPageSize } from '@/views/admin/shared'
import { Icon } from '@/views/admin/AccountIcon'
import { AdminDataTable, type Column, type SortOrder } from '@/views/admin/AdminDataTable'

export interface AnnouncementReadStatusDialogProps {
  open: boolean
  announcementId: number | null
  onClose: () => void
}

/** Read-status dialog ported from the Vue AnnouncementReadStatusDialog. */
export function AnnouncementReadStatusDialog({ open, announcementId, onClose }: AnnouncementReadStatusDialogProps) {
  const { t } = useTranslation()
  const showError = useAppStore((s) => s.showError)

  const [loading, setLoading] = useState(false)
  const [search, setSearch] = useState('')
  const [items, setItems] = useState<AnnouncementUserReadStatus[]>([])
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(getPersistedPageSize())
  const [total, setTotal] = useState(0)
  const [sortKey, setSortKey] = useState('email')
  const [sortOrder, setSortOrder] = useState<SortOrder>('asc')

  const currentController = useRef<AbortController | null>(null)
  const searchDebounceTimer = useRef<number | null>(null)
  const stateRef = useRef({ open, announcementId, page, pageSize, search, sortKey, sortOrder })
  stateRef.current = { open, announcementId, page, pageSize, search, sortKey, sortOrder }

  const columns: Column[] = [
    { key: 'email', label: t('common.email'), sortable: true },
    { key: 'username', label: t('admin.users.columns.username'), sortable: true },
    { key: 'balance', label: t('common.balance'), sortable: true },
    { key: 'eligible', label: t('admin.announcements.eligible') },
    { key: 'read_at', label: t('admin.announcements.readAt') }
  ]

  function resetDialogState() {
    setLoading(false)
    setSearch('')
    setItems([])
    setPage(1)
    setTotal(0)
    setSortKey('email')
    setSortOrder('asc')
  }

  function cancelPendingLoad(resetState = false) {
    if (searchDebounceTimer.current) {
      window.clearTimeout(searchDebounceTimer.current)
      searchDebounceTimer.current = null
    }
    currentController.current?.abort()
    currentController.current = null
    if (resetState) resetDialogState()
  }

  async function load() {
    const current = stateRef.current
    if (!current.open || !current.announcementId) return

    currentController.current?.abort()
    const requestController = new AbortController()
    currentController.current = requestController
    const { signal } = requestController

    try {
      setLoading(true)
      const res = await adminAPI.announcements.getReadStatus(
        current.announcementId,
        current.page,
        current.pageSize,
        {
          search: current.search,
          sort_by: current.sortKey,
          sort_order: current.sortOrder
        },
        { signal }
      )

      if (signal.aborted || currentController.current !== requestController) return

      setItems(res.items)
      setTotal(res.total)
      setPage(res.page)
      setPageSize(res.page_size)
    } catch (error) {
      const err = error as { name?: string; code?: string; response?: { data?: { detail?: string } } }
      if (
        signal.aborted ||
        currentController.current !== requestController ||
        err?.name === 'AbortError' ||
        err?.code === 'ERR_CANCELED'
      ) {
        return
      }
      console.error('Failed to load read status:', error)
      showError(err.response?.data?.detail || t('admin.announcements.failedToLoadReadStatus'))
    } finally {
      if (currentController.current === requestController) {
        setLoading(false)
        currentController.current = null
      }
    }
  }

  useEffect(() => {
    if (!open) {
      cancelPendingLoad(true)
      return
    }
    setPage(1)
    void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, announcementId])

  useEffect(() => () => cancelPendingLoad(), [])

  function handleSearch(value: string) {
    setSearch(value)
    if (searchDebounceTimer.current) window.clearTimeout(searchDebounceTimer.current)
    searchDebounceTimer.current = window.setTimeout(() => {
      setPage(1)
      void load()
    }, 300)
  }

  function handleClose() {
    cancelPendingLoad(true)
    onClose()
  }

  return (
    <Modal
      open={open}
      onClose={handleClose}
      title={t('admin.announcements.readStatus')}
      size="xl"
      footer={
        <div className="flex justify-end">
          <Button variant="secondary" onClick={handleClose}>
            {t('common.close')}
          </Button>
        </div>
      }
    >
      <div className="space-y-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex-1">
            <input
              value={search}
              type="text"
              className="input"
              placeholder={t('admin.announcements.searchUsers')}
              onChange={(event) => handleSearch(event.target.value)}
            />
          </div>
          <Button variant="secondary" onClick={() => void load()} disabled={loading} title={t('common.refresh')}>
            <Icon name="refresh" size="md" className={loading ? 'animate-spin' : ''} />
          </Button>
        </div>

        <AdminDataTable
          columns={columns}
          data={items}
          loading={loading}
          serverSideSort
          sortKey={sortKey}
          sortOrder={sortOrder}
          onSort={(key, order) => {
            setSortKey(key)
            setSortOrder(order)
            setPage(1)
            void load()
          }}
          renderCell={(key, row, value) => {
            if (key === 'email') {
              return <span className="font-medium text-gray-900 dark:text-white">{String(value ?? '')}</span>
            }
            if (key === 'balance') {
              return (
                <span className="font-medium text-gray-900 dark:text-white">${Number(value ?? 0).toFixed(2)}</span>
              )
            }
            if (key === 'eligible') {
              return (
                <span className={value ? 'badge badge-success' : 'badge badge-gray'}>
                  {value ? t('admin.announcements.eligible') : t('common.no')}
                </span>
              )
            }
            if (key === 'read_at') {
              return (
                <span className="text-sm surface-muted">
                  {value ? formatDateTime(value as string) : t('admin.announcements.unread')}
                </span>
              )
            }
            return <span>{value == null ? '' : String(value)}</span>
          }}
        />

        {total > 0 && (
          <Pagination
            page={page}
            total={total}
            pageSize={pageSize}
            onPageChange={(next) => {
              setPage(next)
              void load()
            }}
            onPageSizeChange={(size) => {
              setPageSize(size)
              setPage(1)
              void load()
            }}
          />
        )}
      </div>
    </Modal>
  )
}

export default AnnouncementReadStatusDialog
