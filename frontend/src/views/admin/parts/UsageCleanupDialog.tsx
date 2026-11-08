import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, ConfirmDialog, Modal, Pagination, toast } from '@/components/ui'
import { adminUsageAPI } from '@/api/admin/usage'
import type {
  AdminUsageQueryParams,
  CreateUsageCleanupTaskRequest,
  UsageCleanupTask
} from '@/api/admin/usage'
import { requestTypeToLegacyStream } from '@/utils/usageRequestType'
import { UsageFilters } from './UsageFilters'

export interface UsageCleanupDialogProps {
  show: boolean
  filters: AdminUsageQueryParams
  startDate: string
  endDate: string
  onClose?: () => void
}

function formatDateTime(value?: string | null): string {
  if (!value) return '--'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function getUserTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone
  } catch {
    return 'UTC'
  }
}

export function UsageCleanupDialog({ show, filters, startDate, endDate, onClose }: UsageCleanupDialogProps) {
  const { t } = useTranslation()

  const [localFilters, setLocalFilters] = useState<AdminUsageQueryParams>({})
  const [localStartDate, setLocalStartDate] = useState('')
  const [localEndDate, setLocalEndDate] = useState('')
  const localFiltersRef = useRef(localFilters)
  localFiltersRef.current = localFilters
  const localStartRef = useRef('')
  const localEndRef = useRef('')

  const [tasks, setTasks] = useState<UsageCleanupTask[]>([])
  const [tasksLoading, setTasksLoading] = useState(false)
  const [tasksPage, setTasksPage] = useState(1)
  const [tasksPageSize, setTasksPageSize] = useState(5)
  const [tasksTotal, setTasksTotal] = useState(0)
  const [submitting, setSubmitting] = useState(false)
  const [confirmVisible, setConfirmVisible] = useState(false)
  const [cancelConfirmVisible, setCancelConfirmVisible] = useState(false)
  const [, setCanceling] = useState(false)
  const [cancelTarget, setCancelTarget] = useState<UsageCleanupTask | null>(null)
  const pollTimer = useRef<ReturnType<typeof setInterval> | null>(null)
  const taskRequestId = useRef(0)

  const stopPolling = useCallback(() => {
    if (pollTimer.current !== null) {
      window.clearInterval(pollTimer.current)
      pollTimer.current = null
    }
  }, [])

  const loadTasks = useCallback(async () => {
    if (!show) return
    const requestId = ++taskRequestId.current
    setTasksLoading(true)
    try {
      const res = await adminUsageAPI.listCleanupTasks({ page: tasksPage, page_size: tasksPageSize })
      if (requestId !== taskRequestId.current) return
      setTasks(res.items || [])
      setTasksTotal(res.total || 0)
      if (res.page) setTasksPage(res.page)
      if (res.page_size) setTasksPageSize(res.page_size)
    } catch (error) {
      if (requestId !== taskRequestId.current) return
      console.error('Failed to load cleanup tasks:', error)
      toast.error(t('admin.usage.cleanup.loadFailed'))
    } finally {
      if (requestId === taskRequestId.current) setTasksLoading(false)
    }
  }, [show, tasksPage, tasksPageSize, t])

  const resetFilters = useCallback(() => {
    const next = { ...filters }
    localStartRef.current = startDate
    localEndRef.current = endDate
    next.start_date = startDate
    next.end_date = endDate
    setLocalFilters(next)
    setLocalStartDate(startDate)
    setLocalEndDate(endDate)
    setTasksPage(1)
    setTasksTotal(0)
  }, [filters, startDate, endDate])

  useEffect(() => {
    if (show) {
      resetFilters()
      void loadTasks()
      stopPolling()
      pollTimer.current = window.setInterval(() => {
        void loadTasks()
      }, 10000)
    } else {
      taskRequestId.current += 1
      stopPolling()
      setConfirmVisible(false)
      setCancelConfirmVisible(false)
      setCanceling(false)
      setCancelTarget(null)
      setSubmitting(false)
    }
    return () => {
      taskRequestId.current += 1
      stopPolling()
    }
  }, [show, resetFilters, loadTasks, stopPolling])

  const handleClose = () => {
    taskRequestId.current += 1
    stopPolling()
    setConfirmVisible(false)
    setCancelConfirmVisible(false)
    setCanceling(false)
    setCancelTarget(null)
    setSubmitting(false)
    onClose?.()
  }

  const statusLabel = (status: string): string => {
    const map: Record<string, string> = {
      pending: t('admin.usage.cleanup.status.pending'),
      running: t('admin.usage.cleanup.status.running'),
      succeeded: t('admin.usage.cleanup.status.succeeded'),
      failed: t('admin.usage.cleanup.status.failed'),
      canceled: t('admin.usage.cleanup.status.canceled')
    }
    return map[status] || status
  }

  const statusClass = (status: string): string => {
    const map: Record<string, string> = {
      pending: 'bg-amber-100/[0.15] text-amber-700 dark:bg-amber-500/20 dark:text-amber-200',
      running: 'bg-blue-100/[0.15] text-blue-700 dark:bg-blue-500/20 dark:text-blue-200',
      succeeded: 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-200',
      failed: 'bg-rose-100/[0.15] text-rose-700 dark:bg-rose-500/20 dark:text-rose-200',
      canceled: 'surface-inset surface-muted dark:surface-inset'
    }
    return map[status] || 'surface-inset surface-muted'
  }

  const formatRange = (task: UsageCleanupTask): string => {
    const start = formatDateTime(task.filters.start_time)
    const end = formatDateTime(task.filters.end_time)
    return `${start} ~ ${end}`
  }

  const handleTaskPageChange = (page: number) => {
    setTasksPage(page)
    void loadTasks()
  }

  const handleTaskPageSizeChange = (size: number) => {
    if (!Number.isFinite(size) || size <= 0) return
    setTasksPageSize(size)
    setTasksPage(1)
    void loadTasks()
  }

  const canCancel = (task: UsageCleanupTask): boolean =>
    task.status === 'pending' || task.status === 'running'

  const openCancelConfirm = (task: UsageCleanupTask) => {
    setCancelTarget(task)
    setCancelConfirmVisible(true)
  }

  const buildPayload = (): CreateUsageCleanupTaskRequest | null => {
    if (!localStartRef.current || !localEndRef.current) {
      toast.error(t('admin.usage.cleanup.missingRange'))
      return null
    }

    const current = localFiltersRef.current
    const payload: CreateUsageCleanupTaskRequest = {
      start_date: localStartRef.current,
      end_date: localEndRef.current,
      timezone: getUserTimezone()
    }

    if (current.user_id && current.user_id > 0) payload.user_id = current.user_id
    if (current.api_key_id && current.api_key_id > 0) payload.api_key_id = current.api_key_id
    if (current.account_id && current.account_id > 0) payload.account_id = current.account_id
    if (current.group_id && current.group_id > 0) payload.group_id = current.group_id
    if (current.model) payload.model = current.model
    if (current.request_type) {
      payload.request_type = current.request_type
      const legacyStream = requestTypeToLegacyStream(current.request_type)
      if (legacyStream !== null && legacyStream !== undefined) payload.stream = legacyStream
    } else if (current.stream !== null && current.stream !== undefined) {
      payload.stream = current.stream
    }
    if (current.billing_type !== null && current.billing_type !== undefined) {
      payload.billing_type = current.billing_type
    }

    return payload
  }

  const submitCleanup = async () => {
    const payload = buildPayload()
    if (!payload) {
      setConfirmVisible(false)
      return
    }
    setSubmitting(true)
    setConfirmVisible(false)
    try {
      await adminUsageAPI.createCleanupTask(payload)
      toast.success(t('admin.usage.cleanup.submitSuccess'))
      void loadTasks()
    } catch (error) {
      console.error('Failed to create cleanup task:', error)
      toast.error(t('admin.usage.cleanup.submitFailed'))
    } finally {
      setSubmitting(false)
    }
  }

  const cancelTask = async () => {
    const task = cancelTarget
    if (!task) {
      setCancelConfirmVisible(false)
      return
    }
    setCanceling(true)
    setCancelConfirmVisible(false)
    try {
      await adminUsageAPI.cancelCleanupTask(task.id)
      toast.success(t('admin.usage.cleanup.cancelSuccess'))
      void loadTasks()
    } catch (error) {
      console.error('Failed to cancel cleanup task:', error)
      toast.error(t('admin.usage.cleanup.cancelFailed'))
    } finally {
      setCanceling(false)
      setCancelTarget(null)
    }
  }

  return (
    <>
      <Modal
        open={show}
        onClose={handleClose}
        title={t('admin.usage.cleanup.title')}
        size="full"
        footer={
          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={handleClose}>
              {t('common.cancel')}
            </Button>
            <Button variant="danger" disabled={submitting} onClick={() => setConfirmVisible(true)}>
              {submitting ? t('admin.usage.cleanup.submitting') : t('admin.usage.cleanup.submit')}
            </Button>
          </div>
        }
      >
        <div className="space-y-4">
          <UsageFilters
            filters={localFilters}
            onFiltersChange={setLocalFilters}
            exporting={false}
            startDate={localStartDate}
            endDate={localEndDate}
            onStartDateChange={(value) => {
              localStartRef.current = value
              setLocalStartDate(value)
            }}
            onEndDateChange={(value) => {
              localEndRef.current = value
              setLocalEndDate(value)
            }}
            showActions={false}
            onChange={() => {}}
          />

          <div className="rounded-xl border border-amber-200 bg-amber-50/[0.15] px-4 py-3 text-sm text-amber-700 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200">
            {t('admin.usage.cleanup.warning')}
          </div>

          <div className="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
            <div className="flex items-center justify-between">
              <h4 className="text-sm font-semibold surface-muted">{t('admin.usage.cleanup.recentTasks')}</h4>
              <Button variant="ghost" size="sm" onClick={() => void loadTasks()}>
                {t('common.refresh')}
              </Button>
            </div>

            <div className="mt-3 space-y-2">
              {tasksLoading && <div className="text-sm surface-muted">{t('admin.usage.cleanup.loadingTasks')}</div>}
              {!tasksLoading && tasks.length === 0 && (
                <div className="text-sm surface-muted">{t('admin.usage.cleanup.noTasks')}</div>
              )}
              {!tasksLoading &&
                tasks.map((task) => (
                  <div
                    key={task.id}
                    className="flex flex-col gap-2 rounded-lg border border-gray-100 px-3 py-2 text-sm dark:border-dark-700 surface-muted"
                  >
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <div className="flex items-center gap-2">
                        <span className={`rounded-full px-2 py-0.5 text-xs font-semibold ${statusClass(task.status)}`}>
                          {statusLabel(task.status)}
                        </span>
                        <span className="text-xs surface-muted">#{task.id}</span>
                        {canCancel(task) && (
                          <Button
                            variant="ghost"
                            size="xs"
                            className="text-rose-600 hover:text-rose-700 dark:text-rose-300"
                            onClick={() => openCancelConfirm(task)}
                          >
                            {t('admin.usage.cleanup.cancel')}
                          </Button>
                        )}
                      </div>
                      <div className="text-xs surface-muted">{formatDateTime(task.created_at)}</div>
                    </div>
                    <div className="flex flex-wrap items-center gap-4 text-xs surface-muted">
                      <span>
                        {t('admin.usage.cleanup.range')}: {formatRange(task)}
                      </span>
                      <span>
                        {t('admin.usage.cleanup.deletedRows')}: {task.deleted_rows.toLocaleString()}
                      </span>
                    </div>
                    {task.error_message && <div className="text-xs text-rose-500">{task.error_message}</div>}
                  </div>
                ))}
            </div>

            {tasksTotal > tasksPageSize && (
              <Pagination
                className="mt-4"
                total={tasksTotal}
                page={tasksPage}
                pageSize={tasksPageSize}
                pageSizeOptions={[5]}
                onPageChange={handleTaskPageChange}
                onPageSizeChange={handleTaskPageSizeChange}
              />
            )}
          </div>
        </div>
      </Modal>

      <ConfirmDialog
        open={confirmVisible}
        title={t('admin.usage.cleanup.confirmTitle')}
        message={t('admin.usage.cleanup.confirmMessage')}
        confirmText={t('admin.usage.cleanup.confirmSubmit')}
        variant="danger"
        onConfirm={() => void submitCleanup()}
        onCancel={() => setConfirmVisible(false)}
      />

      <ConfirmDialog
        open={cancelConfirmVisible}
        title={t('admin.usage.cleanup.cancelConfirmTitle')}
        message={t('admin.usage.cleanup.cancelConfirmMessage')}
        confirmText={t('admin.usage.cleanup.cancelConfirm')}
        variant="danger"
        onConfirm={() => void cancelTask()}
        onCancel={() => setCancelConfirmVisible(false)}
      />
    </>
  )
}

export default UsageCleanupDialog
