import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Modal, Pagination, Select, toast } from '@/components/ui'
import { adminAPI } from '@/api/admin'
import { formatDateTime, formatDateTimeLocalInput, parseDateTimeLocalInput } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { AdminGroup, Announcement, AnnouncementTargeting } from '@/types'
import { AdminDataTable, type Column, type SortOrder } from '@/views/admin/AdminDataTable'
import { TablePageLayout } from '@/components/layout/TablePageLayout'
import { Icon } from '@/views/admin/AccountIcon'
import { ConfirmDialog, getPersistedPageSize } from '@/views/admin/shared'
import { AnnouncementPopup } from './parts/AnnouncementPopup'
import { AnnouncementReadStatusDialog } from './parts/AnnouncementReadStatusDialog'
import { AnnouncementTargetingEditor } from './parts/AnnouncementTargetingEditor'
import { DateTimePicker } from './parts/DateTimePicker'

interface AnnouncementForm {
  title: string
  content: string
  status: 'draft' | 'active' | 'archived'
  notify_mode: 'silent' | 'popup'
  starts_at_str: string
  ends_at_str: string
  targeting: AnnouncementTargeting
}

export default function AnnouncementsView() {
  const { t } = useTranslation()

  const [announcements, setAnnouncements] = useState<Announcement[]>([])
  const [loading, setLoading] = useState(false)
  const [statusFilter, setStatusFilter] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(getPersistedPageSize())
  const [total, setTotal] = useState(0)
  const [sortKey, setSortKey] = useState('created_at')
  const [sortOrder, setSortOrder] = useState<SortOrder>('desc')

  const [showEditDialog, setShowEditDialog] = useState(false)
  const [saving, setSaving] = useState(false)
  const [editing, setEditing] = useState<Announcement | null>(null)
  const [form, setForm] = useState<AnnouncementForm>({
    title: '',
    content: '',
    status: 'draft',
    notify_mode: 'silent',
    starts_at_str: '',
    ends_at_str: '',
    targeting: { any_of: [] }
  })
  const [subscriptionGroups, setSubscriptionGroups] = useState<AdminGroup[]>([])

  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [deleting, setDeleting] = useState<Announcement | null>(null)

  const [showReadStatusDialog, setShowReadStatusDialog] = useState(false)
  const [readStatusAnnouncementId, setReadStatusAnnouncementId] = useState<number | null>(null)
  const [previewAnnouncement, setPreviewAnnouncement] = useState<Announcement | null>(null)

  const currentController = useRef<AbortController | null>(null)
  const searchDebounceTimer = useRef<number | null>(null)
  const stateRef = useRef({ page, pageSize, statusFilter, searchQuery, sortKey, sortOrder })
  stateRef.current = { page, pageSize, statusFilter, searchQuery, sortKey, sortOrder }

  const statusFilterOptions = [
    { value: '', label: t('admin.announcements.allStatus') },
    { value: 'draft', label: t('admin.announcements.statusLabels.draft') },
    { value: 'active', label: t('admin.announcements.statusLabels.active') },
    { value: 'archived', label: t('admin.announcements.statusLabels.archived') }
  ]

  const statusOptions = [
    { value: 'draft', label: t('admin.announcements.statusLabels.draft') },
    { value: 'active', label: t('admin.announcements.statusLabels.active') },
    { value: 'archived', label: t('admin.announcements.statusLabels.archived') }
  ]

  const notifyModeOptions = [
    { value: 'silent', label: t('admin.announcements.notifyModeLabels.silent') },
    { value: 'popup', label: t('admin.announcements.notifyModeLabels.popup') }
  ]

  const columns = useMemo<Column[]>(
    () => [
      { key: 'title', label: t('admin.announcements.columns.title'), sortable: true },
      { key: 'status', label: t('admin.announcements.columns.status'), sortable: true },
      { key: 'notify_mode', label: t('admin.announcements.columns.notifyMode'), sortable: true },
      { key: 'targeting', label: t('admin.announcements.columns.targeting') },
      { key: 'timeRange', label: t('admin.announcements.columns.timeRange') },
      { key: 'created_at', label: t('admin.announcements.columns.createdAt'), sortable: true },
      { key: 'actions', label: t('admin.announcements.columns.actions') }
    ],
    [t]
  )

  const statusLabel = (status: string): string => {
    if (status === 'draft') return t('admin.announcements.statusLabels.draft')
    if (status === 'active') return t('admin.announcements.statusLabels.active')
    if (status === 'archived') return t('admin.announcements.statusLabels.archived')
    return status
  }

  const targetingSummary = (targeting: AnnouncementTargeting): string => {
    const anyOf = targeting?.any_of ?? []
    if (!anyOf || anyOf.length === 0) return t('admin.announcements.targetingSummaryAll')
    return t('admin.announcements.targetingSummaryCustom', { groups: anyOf.length })
  }

  async function loadAnnouncements() {
    const current = stateRef.current
    currentController.current?.abort()
    const requestController = new AbortController()
    currentController.current = requestController
    const { signal } = requestController

    try {
      setLoading(true)
      const res = await adminAPI.announcements.list(
        current.page,
        current.pageSize,
        {
          status: current.statusFilter || undefined,
          search: current.searchQuery || undefined,
          sort_by: current.sortKey,
          sort_order: current.sortOrder
        },
        { signal }
      )

      if (signal.aborted || currentController.current !== requestController) return

      setAnnouncements(res.items)
      setTotal(res.total)
      setPage(res.page)
      setPageSize(res.page_size)
    } catch (error) {
      const err = error as { name?: string; code?: string }
      if (
        signal.aborted ||
        currentController.current !== requestController ||
        err?.name === 'AbortError' ||
        err?.code === 'ERR_CANCELED'
      ) {
        return
      }
      console.error('Error loading announcements:', error)
      toast.error(extractApiErrorMessage(error, t('admin.announcements.failedToLoad')))
    } finally {
      if (currentController.current === requestController) {
        setLoading(false)
        currentController.current = null
      }
    }
  }

  async function loadSubscriptionGroups() {
    try {
      const all = await adminAPI.groups.getAll()
      setSubscriptionGroups((all || []).filter((group) => group.subscription_type === 'subscription'))
    } catch (error) {
      console.error('Error loading groups:', error)
      // not fatal
    }
  }

  useEffect(() => {
    void loadSubscriptionGroups()
    void loadAnnouncements()
    return () => {
      if (searchDebounceTimer.current) window.clearTimeout(searchDebounceTimer.current)
      currentController.current?.abort()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function resetForm() {
    setForm({
      title: '',
      content: '',
      status: 'draft',
      notify_mode: 'silent',
      starts_at_str: '',
      ends_at_str: '',
      targeting: { any_of: [] }
    })
  }

  function fillFormFromAnnouncement(announcement: Announcement) {
    setForm({
      title: announcement.title,
      content: announcement.content,
      status: announcement.status,
      notify_mode: announcement.notify_mode || 'silent',
      // Backend returns RFC3339 strings
      starts_at_str: announcement.starts_at
        ? formatDateTimeLocalInput(Math.floor(new Date(announcement.starts_at).getTime() / 1000))
        : '',
      ends_at_str: announcement.ends_at
        ? formatDateTimeLocalInput(Math.floor(new Date(announcement.ends_at).getTime() / 1000))
        : '',
      targeting: announcement.targeting ?? { any_of: [] }
    })
  }

  function buildCreatePayload() {
    const startsAt = parseDateTimeLocalInput(form.starts_at_str)
    const endsAt = parseDateTimeLocalInput(form.ends_at_str)

    return {
      title: form.title,
      content: form.content,
      status: form.status,
      notify_mode: form.notify_mode,
      targeting: form.targeting,
      starts_at: startsAt ?? undefined,
      ends_at: endsAt ?? undefined
    }
  }

  function buildUpdatePayload(original: Announcement) {
    const payload: Record<string, unknown> = {}

    if (form.title !== original.title) payload.title = form.title
    if (form.content !== original.content) payload.content = form.content
    if (form.status !== original.status) payload.status = form.status
    if (form.notify_mode !== (original.notify_mode || 'silent')) payload.notify_mode = form.notify_mode

    // starts_at / ends_at: distinguish unchanged vs clear(0) vs set
    const originalStarts = original.starts_at ? Math.floor(new Date(original.starts_at).getTime() / 1000) : null
    const originalEnds = original.ends_at ? Math.floor(new Date(original.ends_at).getTime() / 1000) : null

    const newStarts = parseDateTimeLocalInput(form.starts_at_str)
    const newEnds = parseDateTimeLocalInput(form.ends_at_str)

    if (newStarts !== originalStarts) {
      payload.starts_at = newStarts === null ? 0 : newStarts
    }
    if (newEnds !== originalEnds) {
      payload.ends_at = newEnds === null ? 0 : newEnds
    }

    // targeting: do shallow compare by JSON
    if (JSON.stringify(form.targeting ?? {}) !== JSON.stringify(original.targeting ?? {})) {
      payload.targeting = form.targeting
    }

    return payload
  }

  async function handleSave() {
    // Frontend validation for targeting (to avoid ANNOUNCEMENT_INVALID_TARGET)
    const anyOf = form.targeting?.any_of ?? []
    if (anyOf.length > 50) {
      toast.error(t('admin.announcements.failedToCreate'))
      return
    }
    for (const group of anyOf) {
      const allOf = group?.all_of ?? []
      if (allOf.length > 50) {
        toast.error(t('admin.announcements.failedToCreate'))
        return
      }
    }

    setSaving(true)
    try {
      if (!editing) {
        await adminAPI.announcements.create(buildCreatePayload())
        toast.success(t('common.success'))
        setShowEditDialog(false)
        await loadAnnouncements()
        return
      }

      const original = editing
      await adminAPI.announcements.update(original.id, buildUpdatePayload(original))
      toast.success(t('common.success'))
      setShowEditDialog(false)
      setEditing(null)
      await loadAnnouncements()
    } catch (error) {
      console.error('Failed to save announcement:', error)
      toast.error(
        extractApiErrorMessage(error, editing ? t('admin.announcements.failedToUpdate') : t('admin.announcements.failedToCreate'))
      )
    } finally {
      setSaving(false)
    }
  }

  async function confirmDelete() {
    if (!deleting) return

    try {
      await adminAPI.announcements.delete(deleting.id)
      toast.success(t('common.success'))
      setShowDeleteDialog(false)
      setDeleting(null)
      await loadAnnouncements()
    } catch (error) {
      console.error('Failed to delete announcement:', error)
      toast.error(extractApiErrorMessage(error, t('admin.announcements.failedToDelete')))
    }
  }

  return (
    <TablePageLayout
      filters={
        <div className="filter-bar">
          <div className="filter-field filter-field--search">
            <input
              value={searchQuery}
              type="text"
              placeholder={t('admin.announcements.searchAnnouncements')}
              className="input"
              onChange={(event) => {
                setSearchQuery(event.target.value)
                if (searchDebounceTimer.current) window.clearTimeout(searchDebounceTimer.current)
                searchDebounceTimer.current = window.setTimeout(() => {
                  setPage(1)
                  void loadAnnouncements()
                }, 300)
              }}
            />
          </div>
          <div className="filter-field">
            <Select
              value={statusFilter}
              options={statusFilterOptions}
              searchable={false}
              onValueChange={(next) => {
                setStatusFilter(String(next ?? ''))
                setPage(1)
                void loadAnnouncements()
              }}
            />
          </div>

          <div className="ml-auto flex w-full flex-wrap items-center justify-end gap-2 sm:w-auto">
            <Button
              variant="secondary"
              onClick={() => void loadAnnouncements()}
              disabled={loading}
              title={t('common.refresh')}
            >
              <Icon name="refresh" size="md" className={loading ? 'animate-spin' : ''} />
            </Button>
            <Button
              variant="primary"
              className="shrink-0 whitespace-nowrap"
              onClick={() => {
                setEditing(null)
                resetForm()
                setShowEditDialog(true)
              }}
            >
              <Icon name="plus" size="md" className="mr-1" />
              {t('admin.announcements.createAnnouncement')}
            </Button>
          </div>
        </div>
      }
      table={
        <AdminDataTable
          columns={columns}
          data={announcements}
          loading={loading}
          serverSideSort
          sortKey={sortKey}
          sortOrder={sortOrder}
          onSort={(key, order) => {
            setSortKey(key)
            setSortOrder(order)
            setPage(1)
            void loadAnnouncements()
          }}
          renderCell={(key, row, value) => {
            const announcement = row as Announcement
            if (key === 'title') {
              return (
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium text-gray-900 dark:text-white">{String(value ?? '')}</span>
                  </div>
                  <div className="mt-1 flex items-center gap-2 text-xs surface-muted">
                    <span>#{announcement.id}</span>
                    <span className="text-gray-300 dark:text-dark-700">·</span>
                    <span>{formatDateTime(announcement.created_at)}</span>
                  </div>
                </div>
              )
            }
            if (key === 'status') {
              const badge =
                value === 'active' ? 'badge-success' : value === 'draft' ? 'badge-gray' : 'badge-warning'
              return <span className={`badge ${badge}`}>{statusLabel(String(value ?? ''))}</span>
            }
            if (key === 'notify_mode') {
              return (
                <span className={announcement.notify_mode === 'popup' ? 'badge badge-warning' : 'badge badge-gray'}>
                  {announcement.notify_mode === 'popup'
                    ? t('admin.announcements.notifyModeLabels.popup')
                    : t('admin.announcements.notifyModeLabels.silent')}
                </span>
              )
            }
            if (key === 'targeting') {
              return <span className="text-sm surface-muted">{targetingSummary(announcement.targeting)}</span>
            }
            if (key === 'timeRange') {
              return (
                <div className="text-sm surface-muted">
                  <div>
                    <span className="font-medium">{t('admin.announcements.form.startsAt')}:</span>
                    <span className="ml-1">
                      {announcement.starts_at
                        ? formatDateTime(announcement.starts_at)
                        : t('admin.announcements.timeImmediate')}
                    </span>
                  </div>
                  <div className="mt-0.5">
                    <span className="font-medium">{t('admin.announcements.form.endsAt')}:</span>
                    <span className="ml-1">
                      {announcement.ends_at
                        ? formatDateTime(announcement.ends_at)
                        : t('admin.announcements.timeNever')}
                    </span>
                  </div>
                </div>
              )
            }
            if (key === 'created_at') {
              return <span className="text-sm surface-muted">{formatDateTime(value as string)}</span>
            }
            if (key === 'actions') {
              return (
                <div className="table-actions">
                  <button
                    type="button"
                    className="table-action"
                    title={t('admin.announcements.preview')}
                    onClick={() => setPreviewAnnouncement(announcement)}
                  >
                    <Icon name="eye" size="sm" />
                  </button>
                  <button
                    type="button"
                    className="table-action"
                    title={t('admin.announcements.readStatus')}
                    onClick={() => {
                      setReadStatusAnnouncementId(announcement.id)
                      setShowReadStatusDialog(true)
                    }}
                  >
                    <Icon name="chartBar" size="sm" />
                  </button>
                  <button
                    type="button"
                    className="table-action table-action-neutral"
                    title={t('common.edit')}
                    onClick={() => {
                      setEditing(announcement)
                      fillFormFromAnnouncement(announcement)
                      setShowEditDialog(true)
                    }}
                  >
                    <Icon name="edit" size="sm" />
                  </button>
                  <button
                    type="button"
                    className="table-action table-action-danger"
                    title={t('common.delete')}
                    onClick={() => {
                      setDeleting(announcement)
                      setShowDeleteDialog(true)
                    }}
                  >
                    <Icon name="trash" size="sm" />
                  </button>
                </div>
              )
            }
            return <span>{value == null ? '' : String(value)}</span>
          }}
          empty={
            <div className="flex flex-col items-center">
              <p className="text-lg font-medium text-gray-900 dark:text-gray-100">{t('empty.noData')}</p>
              <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {t('admin.announcements.createFirstAnnouncement')}
              </p>
              <Button
                variant="primary"
                className="mt-4"
                onClick={() => {
                  setEditing(null)
                  resetForm()
                  setShowEditDialog(true)
                }}
              >
                {t('admin.announcements.createAnnouncement')}
              </Button>
            </div>
          }
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
              void loadAnnouncements()
            }}
            onPageSizeChange={(size) => {
              setPageSize(size)
              setPage(1)
              void loadAnnouncements()
            }}
          />
        ) : undefined
      }
    >
      <Modal
        open={showEditDialog}
        onClose={() => {
          setShowEditDialog(false)
          setEditing(null)
        }}
        title={editing ? t('admin.announcements.editAnnouncement') : t('admin.announcements.createAnnouncement')}
        size="lg"
        footer={
          <div className="flex justify-end gap-3">
            <Button
              variant="secondary"
              onClick={() => {
                setShowEditDialog(false)
                setEditing(null)
              }}
            >
              {t('common.cancel')}
            </Button>
            <Button variant="primary" loading={saving} onClick={() => void handleSave()}>
              {saving ? t('common.saving') : t('common.save')}
            </Button>
          </div>
        }
      >
        <form
          id="announcement-form"
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            void handleSave()
          }}
        >
          <div>
            <label className="input-label">{t('admin.announcements.form.title')}</label>
            <input
              value={form.title}
              type="text"
              className="input"
              required
              onChange={(event) => setForm({ ...form, title: event.target.value })}
            />
          </div>

          <div>
            <label className="input-label">{t('admin.announcements.form.content')}</label>
            <textarea
              value={form.content}
              rows={6}
              className="input"
              required
              onChange={(event) => setForm({ ...form, content: event.target.value })}
            />
          </div>

          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div>
              <label className="input-label">{t('admin.announcements.form.status')}</label>
              <Select
                value={form.status}
                options={statusOptions}
                searchable={false}
                onValueChange={(next) => setForm({ ...form, status: next as AnnouncementForm['status'] })}
              />
            </div>
            <div>
              <label className="input-label">{t('admin.announcements.form.notifyMode')}</label>
              <Select
                value={form.notify_mode}
                options={notifyModeOptions}
                searchable={false}
                onValueChange={(next) => setForm({ ...form, notify_mode: next as AnnouncementForm['notify_mode'] })}
              />
              <p className="input-hint">{t('admin.announcements.form.notifyModeHint')}</p>
            </div>
          </div>

          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div>
              <label className="input-label">{t('admin.announcements.form.startsAt')}</label>
              <DateTimePicker
                ariaLabel={t('admin.announcements.form.startsAt')}
                type="datetime-local"
                max="9999-12-31T23:59"
                value={form.starts_at_str}
                onValueChange={(next) => setForm({ ...form, starts_at_str: next })}
              />
              <p className="input-hint">{t('admin.announcements.form.startsAtHint')}</p>
            </div>
            <div>
              <label className="input-label">{t('admin.announcements.form.endsAt')}</label>
              <DateTimePicker
                ariaLabel={t('admin.announcements.form.endsAt')}
                type="datetime-local"
                max="9999-12-31T23:59"
                value={form.ends_at_str}
                onValueChange={(next) => setForm({ ...form, ends_at_str: next })}
              />
              <p className="input-hint">{t('admin.announcements.form.endsAtHint')}</p>
            </div>
          </div>

          <AnnouncementTargetingEditor
            value={form.targeting}
            groups={subscriptionGroups}
            onValueChange={(next) => setForm({ ...form, targeting: next })}
          />
        </form>
      </Modal>

      <ConfirmDialog
        open={showDeleteDialog}
        title={t('admin.announcements.deleteAnnouncement')}
        message={t('admin.announcements.deleteConfirm')}
        confirmText={t('common.delete')}
        cancelText={t('common.cancel')}
        danger
        onConfirm={() => void confirmDelete()}
        onCancel={() => setShowDeleteDialog(false)}
      />

      <AnnouncementReadStatusDialog
        open={showReadStatusDialog}
        announcementId={readStatusAnnouncementId}
        onClose={() => setShowReadStatusDialog(false)}
      />

      <AnnouncementPopup announcement={previewAnnouncement} preview onClose={() => setPreviewAnnouncement(null)} />
    </TablePageLayout>
  )
}
