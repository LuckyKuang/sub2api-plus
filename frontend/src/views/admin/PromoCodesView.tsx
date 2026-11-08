import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Modal, Pagination, Select, toast } from '@/components/ui'
import { adminAPI } from '@/api/admin'
import { formatDateTime, formatDateTimeLocalInput } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'
import { useClipboard } from '@/views/user/useClipboard'
import type { PromoCode, PromoCodeUsage } from '@/types'
import { AdminDataTable, type Column, type SortOrder } from '@/views/admin/AdminDataTable'
import { TablePageLayout } from '@/components/layout/TablePageLayout'
import { Icon } from '@/views/admin/AccountIcon'
import { ConfirmDialog, getPersistedPageSize } from '@/views/admin/shared'
import { DateTimePicker } from './parts/DateTimePicker'

interface CreateFormState {
  code: string
  bonus_amount: number
  max_uses: number
  expires_at_str: string
  notes: string
}

interface EditFormState {
  code: string
  bonus_amount: number
  max_uses: number
  status: 'active' | 'disabled'
  expires_at_str: string
  notes: string
}

export default function PromoCodesView() {
  const { t } = useTranslation()
  const { copyToClipboard: clipboardCopy } = useClipboard()

  const [codes, setCodes] = useState<PromoCode[]>([])
  const [loading, setLoading] = useState(false)
  const [creating, setCreating] = useState(false)
  const [updating, setUpdating] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [copiedCode, setCopiedCode] = useState<string | null>(null)
  const [statusFilter, setStatusFilter] = useState('')

  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(getPersistedPageSize())
  const [total, setTotal] = useState(0)
  const [sortKey, setSortKey] = useState('created_at')
  const [sortOrder, setSortOrder] = useState<SortOrder>('desc')

  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [showEditDialog, setShowEditDialog] = useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [showUsagesDialog, setShowUsagesDialog] = useState(false)

  const [editingCode, setEditingCode] = useState<PromoCode | null>(null)
  const [deletingCode, setDeletingCode] = useState<PromoCode | null>(null)

  const [usages, setUsages] = useState<PromoCodeUsage[]>([])
  const [usagesLoading, setUsagesLoading] = useState(false)
  const [currentViewingCode, setCurrentViewingCode] = useState<PromoCode | null>(null)
  const [usagesPage, setUsagesPage] = useState(1)
  const [usagesPageSize, setUsagesPageSize] = useState(20)
  const [usagesTotal, setUsagesTotal] = useState(0)

  const [createForm, setCreateForm] = useState<CreateFormState>({
    code: '',
    bonus_amount: 1,
    max_uses: 0,
    expires_at_str: '',
    notes: ''
  })

  const [editForm, setEditForm] = useState<EditFormState>({
    code: '',
    bonus_amount: 0,
    max_uses: 0,
    status: 'active',
    expires_at_str: '',
    notes: ''
  })

  const abortController = useRef<AbortController | null>(null)
  const searchTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const copiedTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const stateRef = useRef({ page, pageSize, statusFilter, searchQuery, sortKey, sortOrder })
  stateRef.current = { page, pageSize, statusFilter, searchQuery, sortKey, sortOrder }

  const filterStatusOptions = [
    { value: '', label: t('admin.promo.allStatus') },
    { value: 'active', label: t('admin.promo.statusActive') },
    { value: 'disabled', label: t('admin.promo.statusDisabled') }
  ]

  const statusOptions = [
    { value: 'active', label: t('admin.promo.statusActive') },
    { value: 'disabled', label: t('admin.promo.statusDisabled') }
  ]

  const columns = useMemo<Column[]>(
    () => [
      { key: 'code', label: t('admin.promo.columns.code') },
      { key: 'bonus_amount', label: t('admin.promo.columns.bonusAmount'), sortable: true },
      { key: 'usage', label: t('admin.promo.columns.usage') },
      { key: 'status', label: t('admin.promo.columns.status'), sortable: true },
      { key: 'expires_at', label: t('admin.promo.columns.expiresAt'), sortable: true },
      { key: 'created_at', label: t('admin.promo.columns.createdAt'), sortable: true },
      { key: 'actions', label: t('admin.promo.columns.actions') }
    ],
    [t]
  )

  const getStatusClass = (status: string, row: PromoCode): string => {
    if (row.expires_at && new Date(row.expires_at) < new Date()) {
      return 'badge-danger'
    }
    if (row.max_uses > 0 && row.used_count >= row.max_uses) {
      return 'badge-gray'
    }
    return status === 'active' ? 'badge-success' : 'badge-gray'
  }

  const getStatusLabel = (status: string, row: PromoCode): string => {
    if (row.expires_at && new Date(row.expires_at) < new Date()) {
      return t('admin.promo.statusExpired')
    }
    if (row.max_uses > 0 && row.used_count >= row.max_uses) {
      return t('admin.promo.statusMaxUsed')
    }
    return status === 'active' ? t('admin.promo.statusActive') : t('admin.promo.statusDisabled')
  }

  const loadCodes = async () => {
    const current = stateRef.current
    if (abortController.current) abortController.current.abort()
    const currentController = new AbortController()
    abortController.current = currentController
    setLoading(true)

    try {
      const response = await adminAPI.promo.list(
        current.page,
        current.pageSize,
        {
          status: current.statusFilter || undefined,
          search: current.searchQuery || undefined,
          sort_by: current.sortKey,
          sort_order: current.sortOrder
        },
        { signal: currentController.signal }
      )
      if (currentController.signal.aborted || abortController.current !== currentController) return

      setCodes(response.items)
      setTotal(response.total)
    } catch (error) {
      const err = error as { name?: string; code?: string }
      if (
        currentController.signal.aborted ||
        abortController.current !== currentController ||
        err?.name === 'AbortError' ||
        err?.code === 'ERR_CANCELED'
      ) {
        return
      }
      toast.error(t('admin.promo.failedToLoad'))
      console.error('Error loading promo codes:', error)
    } finally {
      if (abortController.current === currentController) {
        setLoading(false)
        abortController.current = null
      }
    }
  }

  useEffect(() => {
    void loadCodes()
    return () => {
      if (searchTimer.current) clearTimeout(searchTimer.current)
      abortController.current?.abort()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function handleSearch(value: string) {
    setSearchQuery(value)
    if (searchTimer.current) clearTimeout(searchTimer.current)
    searchTimer.current = setTimeout(() => {
      setPage(1)
      void loadCodes()
    }, 300)
  }

  async function copyToClipboard(text: string) {
    const success = await clipboardCopy(text, t('admin.promo.copied'))
    if (success) {
      setCopiedCode(text)
      if (copiedTimer.current) clearTimeout(copiedTimer.current)
      copiedTimer.current = setTimeout(() => setCopiedCode(null), 2000)
    }
  }

  async function handleCreate() {
    setCreating(true)
    try {
      await adminAPI.promo.create({
        code: createForm.code || undefined,
        bonus_amount: createForm.bonus_amount,
        max_uses: createForm.max_uses,
        expires_at: createForm.expires_at_str
          ? Math.floor(new Date(createForm.expires_at_str).getTime() / 1000)
          : undefined,
        notes: createForm.notes || undefined
      })
      toast.success(t('admin.promo.codeCreated'))
      setShowCreateDialog(false)
      resetCreateForm()
      void loadCodes()
    } catch (error) {
      toast.error(extractApiErrorMessage(error, t('admin.promo.failedToCreate')))
    } finally {
      setCreating(false)
    }
  }

  function resetCreateForm() {
    setCreateForm({ code: '', bonus_amount: 1, max_uses: 0, expires_at_str: '', notes: '' })
  }

  function handleEdit(code: PromoCode) {
    setEditingCode(code)
    setEditForm({
      code: code.code,
      bonus_amount: code.bonus_amount,
      max_uses: code.max_uses,
      status: code.status,
      expires_at_str: code.expires_at
        ? formatDateTimeLocalInput(Math.floor(new Date(code.expires_at).getTime() / 1000))
        : '',
      notes: code.notes || ''
    })
    setShowEditDialog(true)
  }

  function closeEditDialog() {
    setShowEditDialog(false)
    setEditingCode(null)
  }

  async function handleUpdate() {
    if (!editingCode) return

    setUpdating(true)
    try {
      await adminAPI.promo.update(editingCode.id, {
        code: editForm.code,
        bonus_amount: editForm.bonus_amount,
        max_uses: editForm.max_uses,
        status: editForm.status,
        expires_at: editForm.expires_at_str ? Math.floor(new Date(editForm.expires_at_str).getTime() / 1000) : 0,
        notes: editForm.notes
      })
      toast.success(t('admin.promo.codeUpdated'))
      closeEditDialog()
      void loadCodes()
    } catch (error) {
      toast.error(extractApiErrorMessage(error, t('admin.promo.failedToUpdate')))
    } finally {
      setUpdating(false)
    }
  }

  async function copyRegisterLink(code: PromoCode) {
    const baseUrl = window.location.origin
    const registerLink = `${baseUrl}/register?promo=${encodeURIComponent(code.code)}`

    try {
      await navigator.clipboard.writeText(registerLink)
      toast.success(t('admin.promo.registerLinkCopied'))
    } catch {
      // Fallback for older browsers
      const textArea = document.createElement('textarea')
      textArea.value = registerLink
      document.body.appendChild(textArea)
      textArea.select()
      document.execCommand('copy')
      document.body.removeChild(textArea)
      toast.success(t('admin.promo.registerLinkCopied'))
    }
  }

  function handleDelete(code: PromoCode) {
    setDeletingCode(code)
    setShowDeleteDialog(true)
  }

  async function confirmDelete() {
    if (!deletingCode) return

    try {
      await adminAPI.promo.delete(deletingCode.id)
      toast.success(t('admin.promo.codeDeleted'))
      setShowDeleteDialog(false)
      setDeletingCode(null)
      void loadCodes()
    } catch (error) {
      toast.error(extractApiErrorMessage(error, t('admin.promo.failedToDelete')))
    }
  }

  async function handleViewUsages(code: PromoCode) {
    setCurrentViewingCode(code)
    setShowUsagesDialog(true)
    setUsagesPage(1)
    await loadUsages(code, 1)
  }

  async function loadUsages(codeOverride?: PromoCode, pageOverride?: number) {
    const target = codeOverride ?? currentViewingCode
    const targetPage = pageOverride ?? usagesPage
    if (!target) return
    setUsagesLoading(true)
    setUsages([])

    try {
      const response = await adminAPI.promo.getUsages(target.id, targetPage, usagesPageSize)
      setUsages(response.items)
      setUsagesTotal(response.total)
    } catch (error) {
      toast.error(extractApiErrorMessage(error, t('admin.promo.failedToLoadUsages')))
    } finally {
      setUsagesLoading(false)
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
              placeholder={t('admin.promo.searchCodes')}
              className="input"
              onChange={(event) => handleSearch(event.target.value)}
            />
          </div>
          <div className="filter-field">
            <Select
              value={statusFilter}
              options={filterStatusOptions}
              searchable={false}
              onValueChange={(next) => {
                setStatusFilter(String(next ?? ''))
                void loadCodes()
              }}
            />
          </div>

          <div className="ml-auto flex w-full flex-wrap items-center justify-end gap-2 sm:w-auto">
            <Button variant="secondary" onClick={() => void loadCodes()} disabled={loading} title={t('common.refresh')}>
              <Icon name="refresh" size="md" className={loading ? 'animate-spin' : ''} />
            </Button>
            <Button variant="primary" className="shrink-0 whitespace-nowrap" onClick={() => setShowCreateDialog(true)}>
              <Icon name="plus" size="md" className="mr-1" />
              {t('admin.promo.createCode')}
            </Button>
          </div>
        </div>
      }
      table={
        <AdminDataTable
          columns={columns}
          data={codes}
          loading={loading}
          serverSideSort
          sortKey={sortKey}
          sortOrder={sortOrder}
          onSort={(key, order) => {
            setSortKey(key)
            setSortOrder(order)
            setPage(1)
            void loadCodes()
          }}
          renderCell={(key, row, value) => {
            const code = row as PromoCode
            if (key === 'code') {
              return (
                <div className="flex items-center space-x-2">
                  <code className="font-mono text-sm text-gray-900 dark:text-gray-100">{String(value ?? '')}</code>
                  <button
                    type="button"
                    className={`flex items-center transition-colors ${
                      copiedCode === value
                        ? 'text-green-500'
                        : 'text-gray-400 hover:text-gray-600 dark:hover:text-gray-300'
                    }`}
                    title={copiedCode === value ? t('admin.promo.copied') : t('keys.copyToClipboard')}
                    onClick={() => void copyToClipboard(String(value ?? ''))}
                  >
                    {copiedCode !== value ? (
                      <Icon name="copy" size="sm" strokeWidth={2} />
                    ) : (
                      <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M5 13l4 4L19 7" />
                      </svg>
                    )}
                  </button>
                </div>
              )
            }
            if (key === 'bonus_amount') {
              return (
                <span className="text-sm font-medium text-gray-900 dark:text-white">
                  ${Number(value ?? 0).toFixed(2)}
                </span>
              )
            }
            if (key === 'usage') {
              return (
                <span className="text-sm surface-muted">
                  {code.used_count} / {code.max_uses === 0 ? '∞' : code.max_uses}
                </span>
              )
            }
            if (key === 'status') {
              return <span className={`badge ${getStatusClass(String(value ?? ''), code)}`}>{getStatusLabel(String(value ?? ''), code)}</span>
            }
            if (key === 'expires_at') {
              return (
                <span className="text-sm surface-muted">
                  {value ? formatDateTime(value as string) : t('admin.promo.neverExpires')}
                </span>
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
                    title={t('admin.promo.copyRegisterLink')}
                    onClick={() => void copyRegisterLink(code)}
                  >
                    <Icon name="link" size="sm" />
                  </button>
                  <button
                    type="button"
                    className="table-action"
                    title={t('admin.promo.viewUsages')}
                    onClick={() => void handleViewUsages(code)}
                  >
                    <Icon name="eye" size="sm" />
                  </button>
                  <button
                    type="button"
                    className="table-action table-action-neutral"
                    title={t('common.edit')}
                    onClick={() => handleEdit(code)}
                  >
                    <Icon name="edit" size="sm" />
                  </button>
                  <button
                    type="button"
                    className="table-action table-action-danger"
                    title={t('common.delete')}
                    onClick={() => handleDelete(code)}
                  >
                    <Icon name="trash" size="sm" />
                  </button>
                </div>
              )
            }
            return <span>{value == null ? '' : String(value)}</span>
          }}
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
              void loadCodes()
            }}
            onPageSizeChange={(size) => {
              setPageSize(size)
              setPage(1)
              void loadCodes()
            }}
          />
        ) : undefined
      }
    >
      <Modal
        open={showCreateDialog}
        onClose={() => setShowCreateDialog(false)}
        title={t('admin.promo.createCode')}
        size="md"
        footer={
          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={() => setShowCreateDialog(false)}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" loading={creating} onClick={() => void handleCreate()}>
              {creating ? t('common.creating') : t('common.create')}
            </Button>
          </div>
        }
      >
        <form
          id="create-promo-form"
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            void handleCreate()
          }}
        >
          <div>
            <label className="input-label">
              {t('admin.promo.code')}
              <span className="ml-1 text-xs font-normal surface-muted">({t('admin.promo.autoGenerate')})</span>
            </label>
            <input
              value={createForm.code}
              type="text"
              className="input font-mono uppercase"
              placeholder={t('admin.promo.codePlaceholder')}
              onChange={(event) => setCreateForm({ ...createForm, code: event.target.value })}
            />
          </div>
          <div>
            <label className="input-label">{t('admin.promo.bonusAmount')}</label>
            <input
              value={createForm.bonus_amount}
              type="number"
              step="0.01"
              min="0"
              required
              className="input"
              onChange={(event) => setCreateForm({ ...createForm, bonus_amount: Number(event.target.value) })}
            />
          </div>
          <div>
            <label className="input-label">
              {t('admin.promo.maxUses')}
              <span className="ml-1 text-xs font-normal surface-muted">({t('admin.promo.zeroUnlimited')})</span>
            </label>
            <input
              value={createForm.max_uses}
              type="number"
              min="0"
              className="input"
              onChange={(event) => setCreateForm({ ...createForm, max_uses: Number(event.target.value) })}
            />
          </div>
          <div>
            <label className="input-label">
              {t('admin.promo.expiresAt')}
              <span className="ml-1 text-xs font-normal surface-muted">({t('common.optional')})</span>
            </label>
            <DateTimePicker
              ariaLabel={t('admin.promo.expiresAt')}
              type="datetime-local"
              className="input"
              value={createForm.expires_at_str}
              onValueChange={(next) => setCreateForm({ ...createForm, expires_at_str: next })}
            />
          </div>
          <div>
            <label className="input-label">
              {t('admin.promo.notes')}
              <span className="ml-1 text-xs font-normal surface-muted">({t('common.optional')})</span>
            </label>
            <textarea
              value={createForm.notes}
              rows={2}
              className="input"
              placeholder={t('admin.promo.notesPlaceholder')}
              onChange={(event) => setCreateForm({ ...createForm, notes: event.target.value })}
            />
          </div>
        </form>
      </Modal>

      <Modal
        open={showEditDialog}
        onClose={closeEditDialog}
        title={t('admin.promo.editCode')}
        size="md"
        footer={
          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={closeEditDialog}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" loading={updating} onClick={() => void handleUpdate()}>
              {updating ? t('common.saving') : t('common.save')}
            </Button>
          </div>
        }
      >
        <form
          id="edit-promo-form"
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            void handleUpdate()
          }}
        >
          <div>
            <label className="input-label">{t('admin.promo.code')}</label>
            <input
              value={editForm.code}
              type="text"
              className="input font-mono uppercase"
              onChange={(event) => setEditForm({ ...editForm, code: event.target.value })}
            />
          </div>
          <div>
            <label className="input-label">{t('admin.promo.bonusAmount')}</label>
            <input
              value={editForm.bonus_amount}
              type="number"
              step="0.01"
              min="0"
              required
              className="input"
              onChange={(event) => setEditForm({ ...editForm, bonus_amount: Number(event.target.value) })}
            />
          </div>
          <div>
            <label className="input-label">
              {t('admin.promo.maxUses')}
              <span className="ml-1 text-xs font-normal surface-muted">({t('admin.promo.zeroUnlimited')})</span>
            </label>
            <input
              value={editForm.max_uses}
              type="number"
              min="0"
              className="input"
              onChange={(event) => setEditForm({ ...editForm, max_uses: Number(event.target.value) })}
            />
          </div>
          <div>
            <label className="input-label">{t('admin.promo.status')}</label>
            <Select
              value={editForm.status}
              options={statusOptions}
              searchable={false}
              onValueChange={(next) => setEditForm({ ...editForm, status: next as EditFormState['status'] })}
            />
          </div>
          <div>
            <label className="input-label">
              {t('admin.promo.expiresAt')}
              <span className="ml-1 text-xs font-normal surface-muted">({t('common.optional')})</span>
            </label>
            <DateTimePicker
              ariaLabel={t('admin.promo.expiresAt')}
              type="datetime-local"
              className="input"
              value={editForm.expires_at_str}
              onValueChange={(next) => setEditForm({ ...editForm, expires_at_str: next })}
            />
          </div>
          <div>
            <label className="input-label">
              {t('admin.promo.notes')}
              <span className="ml-1 text-xs font-normal surface-muted">({t('common.optional')})</span>
            </label>
            <textarea
              value={editForm.notes}
              rows={2}
              className="input"
              onChange={(event) => setEditForm({ ...editForm, notes: event.target.value })}
            />
          </div>
        </form>
      </Modal>

      <Modal
        open={showUsagesDialog}
        onClose={() => setShowUsagesDialog(false)}
        title={t('admin.promo.usageRecords')}
        size="lg"
        footer={
          <div className="flex justify-end">
            <Button variant="secondary" onClick={() => setShowUsagesDialog(false)}>
              {t('common.close')}
            </Button>
          </div>
        }
      >
        {usagesLoading ? (
          <div className="flex items-center justify-center py-8">
            <Icon name="refresh" size="lg" className="animate-spin text-gray-400" />
          </div>
        ) : usages.length === 0 ? (
          <div className="py-8 text-center surface-muted">{t('admin.promo.noUsages')}</div>
        ) : (
          <div className="space-y-3">
            {usages.map((usage) => (
              <div
                key={usage.id}
                className="flex items-center justify-between rounded-lg border border-gray-200 p-3 dark:border-dark-600"
              >
                <div className="flex items-center gap-3">
                  <div className="flex h-8 w-8 items-center justify-center rounded-full bg-green-100/[0.15] dark:bg-green-900/30">
                    <Icon name="user" size="sm" className="text-green-600 dark:text-green-400" />
                  </div>
                  <div>
                    <p className="text-sm font-medium text-gray-900 dark:text-white">
                      {usage.user?.email || t('admin.promo.userPrefix', { id: usage.user_id })}
                    </p>
                    <p className="text-xs surface-muted">{formatDateTime(usage.used_at)}</p>
                  </div>
                </div>
                <div className="text-right">
                  <span className="text-sm font-medium text-green-600 dark:text-green-400">
                    +${usage.bonus_amount.toFixed(2)}
                  </span>
                </div>
              </div>
            ))}
            {usagesTotal > usagesPageSize && (
              <div className="mt-4">
                <Pagination
                  page={usagesPage}
                  total={usagesTotal}
                  pageSize={usagesPageSize}
                  onPageChange={(next) => {
                    setUsagesPage(next)
                    void loadUsages(undefined, next)
                  }}
                  onPageSizeChange={(size) => {
                    setUsagesPageSize(size)
                    setUsagesPage(1)
                    void loadUsages(undefined, 1)
                  }}
                />
              </div>
            )}
          </div>
        )}
      </Modal>

      <ConfirmDialog
        open={showDeleteDialog}
        title={t('admin.promo.deleteCode')}
        message={t('admin.promo.deleteCodeConfirm')}
        confirmText={t('common.delete')}
        cancelText={t('common.cancel')}
        danger
        onConfirm={() => void confirmDelete()}
        onCancel={() => setShowDeleteDialog(false)}
      />
    </TablePageLayout>
  )
}
