import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/components/DataTable'
import { ConfirmDialog, Select, toast } from '@/components/ui'
import { keysAPI } from '@/api'
import {
  AsyncImageDownloadValidationError,
  deleteAsyncImageTask,
  downloadAsyncImageZip,
  getAsyncImageTask,
  listAsyncImageModels,
  listAsyncImageTasks,
  preferredAsyncImageModel,
  saveAsyncImageBlob,
  submitAsyncImageEdit,
  submitAsyncImageGeneration,
  type AsyncImageTask
} from '@/api/asyncImage'
import { keyCanManageAsyncImage } from '@/composables/useAsyncImageAccess'
import { isGPTImage2, isGPTImage2ExperimentalSize, validateGPTImage2CustomSize } from '@/utils/asyncImageSize'
import type { ApiKey } from '@/types'
import { BaseDialog } from './BaseDialog'
import { Icon } from './icons'

const POLL_INTERVAL_MS = 3000
const PAGE_SIZE = 20
const MAX_EDIT_INPUT_IMAGES = 4
const MAX_EDIT_UPLOAD_FILE_BYTES = 20 * 1024 * 1024
const MAX_EDIT_UPLOAD_TOTAL_BYTES = 40 * 1024 * 1024
const supportedEditImageTypes = new Set(['image/png', 'image/jpeg', 'image/webp'])
const supportedMaskImageTypes = new Set(['image/png', 'image/webp'])

function keyAllowsAsyncImage(key: ApiKey): boolean {
  return (
    key.status === 'active' &&
    (key.group?.platform === 'openai' || key.group?.platform === 'grok') &&
    key.group?.allow_image_generation === true
  )
}

type AsyncImageMode = 'generation' | 'edit'

interface EditImageUpload {
  file: File
  url: string
}

interface AsyncImageForm {
  apiKeyId: number
  mode: AsyncImageMode
  model: string
  prompt: string
  size: string
  customWidth: number
  customHeight: number
  quality: string
  n: number
}

export default function AsyncImageView() {
  const { t } = useTranslation()
  const [apiKeys, setApiKeys] = useState<ApiKey[]>([])
  const [selectedApiKeyId, setSelectedApiKeyId] = useState(0)
  const [loadingKeys, setLoadingKeys] = useState(false)
  const [loadingModels, setLoadingModels] = useState(false)
  const [loadingTasks, setLoadingTasks] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [downloading, setDownloading] = useState(false)
  const [tasks, setTasks] = useState<AsyncImageTask[]>([])
  const [availableModels, setAvailableModels] = useState<string[]>([])
  const [hasMore, setHasMore] = useState(false)
  const [offset, setOffset] = useState(0)
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [selectedTask, setSelectedTask] = useState<AsyncImageTask | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<AsyncImageTask | null>(null)
  const [deletingTaskId, setDeletingTaskId] = useState('')
  const pollTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const modelRequestID = useRef(0)
  const taskListRequestID = useRef(0)

  const [filters, setFilters] = useState({ status: '' })
  const [form, setForm] = useState<AsyncImageForm>({
    apiKeyId: 0,
    mode: 'generation',
    model: '',
    prompt: '',
    size: '1024x1024',
    customWidth: 1024,
    customHeight: 1024,
    quality: 'auto',
    n: 1
  })
  const [editImages, setEditImages] = useState<EditImageUpload[]>([])
  const [editMask, setEditMask] = useState<EditImageUpload | null>(null)

  const eligibleKeys = useMemo(() => apiKeys.filter(keyAllowsAsyncImage), [apiKeys])
  const manageableKeys = useMemo(() => apiKeys.filter(keyCanManageAsyncImage), [apiKeys])

  const tasksRef = useRef(tasks)
  tasksRef.current = tasks
  const selectedTaskRef = useRef(selectedTask)
  selectedTaskRef.current = selectedTask
  const offsetRef = useRef(offset)
  offsetRef.current = offset

  const apiKeyFilterOptions = useMemo(
    () => [
      { value: 0, label: t('asyncImage.filters.selectKey') },
      ...manageableKeys.map((key) => ({ value: key.id, label: key.name || `API Key #${key.id}` }))
    ],
    [manageableKeys, t]
  )
  const statusFilterOptions = useMemo(
    () => [
      { value: '', label: t('asyncImage.filters.allStatuses') },
      { value: 'processing', label: t('asyncImage.status.processing') },
      { value: 'completed', label: t('asyncImage.status.completed') },
      { value: 'failed', label: t('asyncImage.status.failed') }
    ],
    [t]
  )

  const selectedApiKey = useMemo(
    () => manageableKeys.find((key) => key.id === selectedApiKeyId) || null,
    [manageableKeys, selectedApiKeyId]
  )
  const formApiKey = useMemo(
    () => eligibleKeys.find((key) => key.id === form.apiKeyId) || null,
    [eligibleKeys, form.apiKeyId]
  )
  const selectedTaskApiKey = selectedApiKey
  const selectedModelIsGPTImage2 = isGPTImage2(form.model)
  const usingCustomSize = selectedModelIsGPTImage2 && form.size === 'custom'
  const customSizeValidationCode = usingCustomSize ? validateGPTImage2CustomSize(form.customWidth, form.customHeight) : null
  const customSizeError = customSizeValidationCode ? t(`asyncImage.create.customSizeErrors.${customSizeValidationCode}`) : ''
  const customSizeExperimental =
    usingCustomSize && !customSizeValidationCode && isGPTImage2ExperimentalSize(form.customWidth, form.customHeight)
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1
  const deleteConfirmMessage = t('asyncImage.delete.confirm', { taskId: deleteTarget?.task_id || '' })

  const sizeOptions = useMemo(() => {
    if (!selectedModelIsGPTImage2) {
      return [
        { value: '1024x1024', label: '1024 x 1024' },
        { value: '1536x1024', label: '1536 x 1024' },
        { value: '1024x1536', label: '1024 x 1536' }
      ]
    }
    return [
      { value: 'auto', label: t('asyncImage.create.sizeAuto') },
      { value: '1024x1024', label: t('asyncImage.create.size1K') },
      { value: '1536x1024', label: t('asyncImage.create.sizeLandscape15K') },
      { value: '1024x1536', label: t('asyncImage.create.sizePortrait15K') },
      { value: '2048x2048', label: t('asyncImage.create.size2KSquare') },
      { value: '2048x1152', label: t('asyncImage.create.size2KWide') },
      { value: '2560x1440', label: t('asyncImage.create.sizeQHD') },
      { value: '3840x2160', label: t('asyncImage.create.size4KWide') },
      { value: '2160x3840', label: t('asyncImage.create.size4KPortrait') },
      { value: 'custom', label: t('asyncImage.create.sizeCustom') }
    ]
  }, [selectedModelIsGPTImage2, t])

  const columns = useMemo<Column[]>(
    () => [
      { key: 'prompt_preview', label: t('asyncImage.columns.prompt'), sortable: false, class: 'min-w-[280px] max-w-[360px]' },
      { key: 'model', label: t('asyncImage.columns.model'), sortable: false, class: 'w-44' },
      { key: 'request_type', label: t('asyncImage.columns.type'), sortable: false, class: 'w-24 text-center' },
      { key: 'status', label: t('asyncImage.columns.status'), sortable: false, class: 'w-28 text-center' },
      { key: 'result', label: t('asyncImage.columns.result'), sortable: false, class: 'w-[120px] text-center' },
      { key: 'created_at', label: t('asyncImage.columns.createdAt'), sortable: false, class: 'w-44' },
      { key: 'actions', label: t('asyncImage.columns.actions'), sortable: false, class: 'w-24 text-center' }
    ],
    [t]
  )

  function formatDate(timestamp: number) {
    return timestamp ? new Date(timestamp * 1000).toLocaleString() : '-'
  }

  function statusLabel(status: string) {
    const key = `asyncImage.status.${status}`
    const translated = t(key)
    return translated === key ? status : translated
  }

  function statusClass(status: string) {
    if (status === 'completed') return 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    if (status === 'failed') return 'bg-red-100/[0.15] text-red-700 dark:bg-red-900/30 dark:text-red-300'
    return 'bg-blue-100/[0.15] text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
  }

  function requestTypeLabel(requestType?: string) {
    return requestType === 'edit' ? t('asyncImage.requestTypes.edit') : t('asyncImage.requestTypes.generation')
  }

  function requestTypeClass(requestType?: string) {
    return requestType === 'edit'
      ? 'bg-violet-100/[0.15] text-violet-700 dark:bg-violet-900/30 dark:text-violet-300'
      : 'bg-cyan-100/[0.15] text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-300'
  }

  function taskImageUrls(task: AsyncImageTask): string[] {
    const urls = [task.image_url, ...(task.result?.data || []).map((item) => item.url)].filter((url): url is string =>
      Boolean(url)
    )
    return [...new Set(urls)]
  }

  function imageCountMismatch(task: AsyncImageTask): boolean {
    if (task.status !== 'completed') return false
    const requested = task.requested_images || 1
    const actual = task.actual_images ?? taskImageUrls(task).length
    return actual < requested
  }

  function errorMessage(error: unknown, fallback: string) {
    return (error as { message?: string })?.message || fallback
  }

  const loadApiKeys = useCallback(async () => {
    setLoadingKeys(true)
    try {
      const loadedKeys: ApiKey[] = []
      let page = 1
      while (true) {
        const response = await keysAPI.list(page, 100, { sort_by: 'created_at', sort_order: 'desc' })
        loadedKeys.push(...(response.items || []))
        const pages = Number.isFinite(response.pages) && response.pages > 0 ? response.pages : 1
        if (page >= pages || (response.items || []).length === 0) break
        page += 1
      }
      setApiKeys(loadedKeys)
      const manageable = loadedKeys.filter(keyCanManageAsyncImage)
      const allowed = loadedKeys.filter(keyAllowsAsyncImage)
      setSelectedApiKeyId((previous) =>
        manageable.some((key) => key.id === previous) ? previous : allowed[0]?.id || manageable[0]?.id || 0
      )
    } catch (error) {
      toast.error(errorMessage(error, t('asyncImage.errors.loadKeys')))
    } finally {
      setLoadingKeys(false)
    }
  }, [t])

  const stopPolling = useCallback(() => {
    if (pollTimer.current) {
      clearTimeout(pollTimer.current)
      pollTimer.current = null
    }
  }, [])

  const refreshProcessingTasks = useCallback(async () => {
    const key = selectedApiKey
    if (!key) return
    const active = tasksRef.current.filter((task) => task.status === 'processing')
    if (!active.length) return
    const refreshed = await Promise.allSettled(active.map((task) => getAsyncImageTask(key.key, task.task_id)))
    refreshed.forEach((result, index) => {
      if (result.status !== 'fulfilled') return
      const taskID = active[index].task_id
      setTasks((previous) => {
        const taskIndex = previous.findIndex((task) => task.task_id === taskID)
        if (taskIndex === -1) return previous
        const next = [...previous]
        next.splice(taskIndex, 1, result.value)
        return next
      })
      if (selectedTaskRef.current?.task_id === taskID) setSelectedTask(result.value)
    })
  }, [selectedApiKey])

  const schedulePolling = useCallback(() => {
    stopPolling()
    if (!selectedApiKey || !tasksRef.current.some((task) => task.status === 'processing')) return
    pollTimer.current = setTimeout(() => {
      void (async () => {
        await refreshProcessingTasks()
        schedulePolling()
      })()
    }, POLL_INTERVAL_MS)
  }, [refreshProcessingTasks, selectedApiKey, stopPolling])

  const loadTasks = useCallback(
    async (showError = true): Promise<boolean> => {
      const key = selectedApiKey
      const requestID = ++taskListRequestID.current
      stopPolling()
      if (!key) {
        setTasks([])
        setHasMore(false)
        setLoadingTasks(false)
        return true
      }
      setLoadingTasks(true)
      try {
        const response = await listAsyncImageTasks(key.key, {
          status: filters.status,
          limit: PAGE_SIZE,
          offset: offsetRef.current
        })
        if (requestID !== taskListRequestID.current) return false
        setTasks(response.data || [])
        setHasMore(response.has_more === true)
        return true
      } catch (error) {
        if (requestID !== taskListRequestID.current) return false
        setTasks([])
        setHasMore(false)
        if (showError) toast.error(errorMessage(error, t('asyncImage.errors.loadTasks')))
        return false
      } finally {
        if (requestID === taskListRequestID.current) {
          setLoadingTasks(false)
          schedulePolling()
        }
      }
    },
    [filters.status, schedulePolling, selectedApiKey, stopPolling, t]
  )

  const loadFormModels = useCallback(async () => {
    const key = formApiKey
    const requestID = ++modelRequestID.current
    setAvailableModels([])
    setForm((previous) => ({ ...previous, model: '' }))
    if (!key) return

    setLoadingModels(true)
    try {
      const models = await listAsyncImageModels(key.key)
      if (requestID !== modelRequestID.current) return
      setAvailableModels(models)
      const preferred = preferredAsyncImageModel(models)
      setForm((previous) => ({ ...previous, model: preferred }))
      onModelChanged(preferred)
    } catch (error) {
      if (requestID !== modelRequestID.current) return
      toast.error(errorMessage(error, t('asyncImage.errors.loadModels')))
    } finally {
      if (requestID === modelRequestID.current) setLoadingModels(false)
    }
  }, [formApiKey, t])

  function onModelChanged(model: string) {
    if (!isGPTImage2(model)) {
      setForm((previous) => ({
        ...previous,
        size: previous.size === 'custom' || previous.size === 'auto' ? '1024x1024' : previous.size
      }))
    }
  }

  function totalEditUploadBytes() {
    return editImages.reduce((total, image) => total + image.file.size, editMask?.file.size || 0)
  }

  const clearEditUploads = useCallback(() => {
    setEditImages([])
    setEditMask(null)
  }, [])

  useEffect(() => {
    const pending = [...editImages, ...(editMask ? [editMask] : [])]
    return () => {
      pending.forEach((upload) => URL.revokeObjectURL(upload.url))
    }
  }, [editImages, editMask])

  function uploadErrorFor(file: File, allowedTypes: Set<string>) {
    if (!allowedTypes.has(file.type)) return t('asyncImage.create.invalidUploadType')
    if (file.size > MAX_EDIT_UPLOAD_FILE_BYTES) return t('asyncImage.create.uploadFileTooLarge')
    return ''
  }

  function handleReferenceImageFiles(event: React.ChangeEvent<HTMLInputElement>) {
    const input = event.target
    const files = Array.from(input.files || [])
    input.value = ''
    if (!files.length) return
    if (editImages.length + files.length > MAX_EDIT_INPUT_IMAGES) {
      toast.error(t('asyncImage.create.referenceImageLimit', { count: MAX_EDIT_INPUT_IMAGES }))
      return
    }
    const invalid = files.find((file) => uploadErrorFor(file, supportedEditImageTypes))
    if (invalid) {
      toast.error(uploadErrorFor(invalid, supportedEditImageTypes))
      return
    }
    if (totalEditUploadBytes() + files.reduce((total, file) => total + file.size, 0) > MAX_EDIT_UPLOAD_TOTAL_BYTES) {
      toast.error(t('asyncImage.create.uploadTotalTooLarge'))
      return
    }
    setEditImages((previous) => [...previous, ...files.map((file) => ({ file, url: URL.createObjectURL(file) }))])
  }

  function handleMaskImageFile(event: React.ChangeEvent<HTMLInputElement>) {
    const input = event.target
    const file = input.files?.[0]
    input.value = ''
    if (!file) return
    const error = uploadErrorFor(file, supportedMaskImageTypes)
    if (error) {
      toast.error(error)
      return
    }
    if (totalEditUploadBytes() + file.size > MAX_EDIT_UPLOAD_TOTAL_BYTES) {
      toast.error(t('asyncImage.create.uploadTotalTooLarge'))
      return
    }
    setEditMask({ file, url: URL.createObjectURL(file) })
  }

  function removeReferenceImage(index: number) {
    setEditImages((previous) => {
      const next = [...previous]
      next.splice(index, 1)
      return next
    })
    if (index === 0 && editMask) setEditMask(null)
  }

  function removeMaskImage() {
    setEditMask(null)
  }

  const applyFilters = () => {
    setOffset(0)
    void loadTasks()
  }

  const selectApiKey = () => {
    setOffset(0)
    setSelectedTask(null)
    void loadTasks()
  }

  const confirmDeleteTask = async () => {
    const target = deleteTarget
    const key = selectedApiKey
    if (!target || !key || deletingTaskId) return

    setDeletingTaskId(target.task_id)
    try {
      await deleteAsyncImageTask(key.key, target.task_id)
      if (selectedTaskRef.current?.task_id === target.task_id) setSelectedTask(null)
      setDeleteTarget(null)
      toast.success(t('asyncImage.delete.success'))
      const loaded = await loadTasks()
      if (loaded && tasksRef.current.length === 0 && offsetRef.current > 0) {
        setOffset(Math.max(0, offsetRef.current - PAGE_SIZE))
        await loadTasks()
      }
    } catch (error) {
      const code = (error as { code?: string })?.code
      toast.error(
        t(code === 'IMAGE_TASK_DELETE_NOT_ALLOWED' ? 'asyncImage.errors.deleteNotAllowed' : 'asyncImage.errors.delete')
      )
    } finally {
      setDeletingTaskId('')
    }
  }

  const previousPage = () => {
    setOffset(Math.max(0, offset - PAGE_SIZE))
    void loadTasks()
  }

  const nextPage = () => {
    if (!hasMore) return
    setOffset(offset + PAGE_SIZE)
    void loadTasks()
  }

  const openCreateDialog = () => {
    setForm((previous) => ({
      ...previous,
      apiKeyId: eligibleKeys.some((key) => key.id === selectedApiKeyId) ? selectedApiKeyId : eligibleKeys[0]?.id || 0,
      mode: 'generation',
      size: '1024x1024',
      customWidth: 1024,
      customHeight: 1024,
      quality: 'auto'
    }))
    clearEditUploads()
    setShowCreateDialog(true)
    void loadFormModels()
  }

  const closeCreateDialog = () => {
    if (submitting) return
    setShowCreateDialog(false)
    clearEditUploads()
  }

  const downloadSelectedTask = async () => {
    const task = selectedTask
    const key = selectedTaskApiKey
    if (!task || !key || downloading || taskImageUrls(task).length === 0) return
    setDownloading(true)
    try {
      const blob = await downloadAsyncImageZip(key.key, task.task_id)
      saveAsyncImageBlob(blob, `${task.task_id}.zip`)
    } catch (error) {
      toast.error(
        error instanceof AsyncImageDownloadValidationError
          ? t('asyncImage.errors.invalidArchive')
          : errorMessage(error, t('asyncImage.errors.download'))
      )
    } finally {
      setDownloading(false)
    }
  }

  const submitTask = async () => {
    const key = formApiKey
    if (!key) {
      toast.error(t('asyncImage.create.noKeys'))
      return
    }
    if (!form.model.trim()) {
      toast.error(t('asyncImage.create.modelRequired'))
      return
    }
    if (!form.prompt.trim()) {
      toast.error(t('asyncImage.create.promptRequired'))
      return
    }
    if (form.mode === 'edit' && editImages.length === 0) {
      toast.error(t('asyncImage.create.referenceImageRequired'))
      return
    }
    if (customSizeError) {
      toast.error(customSizeError)
      return
    }
    const size = usingCustomSize ? `${form.customWidth}x${form.customHeight}` : form.size
    setSubmitting(true)
    try {
      const payload = {
        model: form.model.trim(),
        prompt: form.prompt.trim(),
        size,
        n: form.n,
        ...(selectedModelIsGPTImage2 ? { quality: form.quality } : {})
      }
      const created =
        form.mode === 'edit'
          ? await submitAsyncImageEdit(key.key, {
              ...payload,
              images: editImages.map((image) => image.file),
              ...(editMask ? { mask: editMask.file } : {})
            })
          : await submitAsyncImageGeneration(key.key, payload)
      setSelectedApiKeyId(key.id)
      setFilters({ status: '' })
      setOffset(0)
      setTasks((previous) => [created, ...previous.filter((task) => task.task_id !== created.task_id)].slice(0, PAGE_SIZE))
      setShowCreateDialog(false)
      setForm((previous) => ({ ...previous, prompt: '' }))
      clearEditUploads()
      toast.success(t('asyncImage.create.submitted'))
      schedulePolling()
    } catch (error) {
      toast.error(errorMessage(error, t('asyncImage.errors.submit')))
    } finally {
      setSubmitting(false)
    }
  }

  useEffect(() => {
    void (async () => {
      await loadApiKeys()
      await loadTasks()
    })()
    return () => {
      taskListRequestID.current += 1
      stopPolling()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Reload the task list whenever the selected key, status filter or offset changes.
  useEffect(() => {
    void loadTasks()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedApiKeyId, filters.status, offset])

  const renderCell = (key: string, row: AsyncImageTask): React.ReactNode => {
    switch (key) {
      case 'prompt_preview':
        return (
          <div className="max-w-[360px]">
            <p className="line-clamp-2 text-sm text-gray-800 dark:text-gray-100" title={row.prompt_preview || ''}>
              {row.prompt_preview || '-'}
            </p>
            <p className="mt-1 truncate font-mono text-xs surface-muted">{row.task_id}</p>
          </div>
        )
      case 'model':
        return (
          <span className="block max-w-[160px] truncate text-sm surface-muted" title={row.model || ''}>
            {row.model || '-'}
          </span>
        )
      case 'request_type':
        return <span className={`badge ${requestTypeClass(row.request_type)}`}>{requestTypeLabel(row.request_type)}</span>
      case 'status':
        return <span className={`badge ${statusClass(row.status)}`}>{statusLabel(row.status)}</span>
      case 'result': {
        const urls = taskImageUrls(row)
        if (urls.length) {
          return (
            <div className={`inline-grid gap-1 align-middle ${urls.length === 1 ? 'grid-cols-1' : 'grid-cols-2'}`}>
              {urls.map((url) => (
                <div
                  key={url}
                  className="surface-inset h-12 w-12 overflow-hidden rounded border border-gray-200 dark:border-dark-600"
                >
                  <img src={url} alt="" className="h-full w-full object-cover" loading="lazy" />
                </div>
              ))}
            </div>
          )
        }
        if (row.status === 'failed') {
          return <span className="text-xs text-red-600 dark:text-red-300">{row.error?.message || '-'}</span>
        }
        return <span className="text-xs surface-muted">-</span>
      }
      case 'created_at':
        return <span className="text-sm surface-muted">{formatDate(row.created_at)}</span>
      case 'actions':
        return (
          <div className="flex justify-center gap-1">
            <button
              type="button"
              className="icon-button surface-control"
              title={t('asyncImage.actions.view')}
              data-testid={`view-task-${row.task_id}`}
              onClick={() => setSelectedTask(row)}
            >
              <Icon name="eye" size="sm" />
            </button>
            {row.status === 'failed' && (
              <button
                type="button"
                className="icon-button icon-button-danger surface-control"
                disabled={deletingTaskId === row.task_id}
                title={t('asyncImage.actions.delete')}
                data-testid={`delete-task-${row.task_id}`}
                onClick={() => setDeleteTarget(row)}
              >
                <Icon
                  name={deletingTaskId === row.task_id ? 'refresh' : 'trash'}
                  size="sm"
                  className={deletingTaskId === row.task_id ? 'animate-spin' : ''}
                />
              </button>
            )}
          </div>
        )
      default:
        return ''
    }
  }

  return (
    <div className="workspace-page">
      <div className="card flex flex-col gap-3 p-4">
        <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
          <div className="filter-bar w-full xl:w-auto">
            <div className="filter-field">
              <Select
                value={selectedApiKeyId}
                options={apiKeyFilterOptions}
                disabled={loadingKeys}
                searchable="auto"
                ariaLabel={t('asyncImage.filters.apiKey')}
                data-testid="async-image-api-key-filter"
                onValueChange={(value) => {
                  setSelectedApiKeyId(Number(value))
                  selectApiKey()
                }}
              />
            </div>
            <div className="filter-field">
              <Select
                value={filters.status}
                options={statusFilterOptions}
                disabled={loadingTasks}
                searchable={false}
                ariaLabel={t('asyncImage.filters.allStatuses')}
                data-testid="async-image-status-filter"
                onValueChange={(value) => {
                  setFilters({ status: value === null ? '' : String(value) })
                  applyFilters()
                }}
              />
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2 xl:flex-shrink-0">
            <button
              type="button"
              className="btn btn-secondary"
              disabled={loadingTasks}
              title={t('asyncImage.actions.refresh')}
              onClick={() => void loadTasks()}
            >
              <Icon name="refresh" size="md" className={loadingTasks ? 'animate-spin' : ''} />
            </button>
            <button type="button" className="btn btn-primary" onClick={openCreateDialog}>
              <Icon name="plus" size="md" className="mr-2" />
              {t('asyncImage.actions.create')}
            </button>
          </div>
        </div>
        <p className="text-xs surface-muted">{t('asyncImage.list.keyHint')}</p>
      </div>

      <div className="card workspace-table-card workspace-shell">
        <DataTable
          className="workspace-table-viewport"
          columns={columns}
          data={tasks}
          loading={loadingTasks}
          rowKey="task_id"
          renderCell={(key, row) => renderCell(key, row as AsyncImageTask)}
          renderEmpty={() => (
            <div className="flex min-h-[260px] flex-col items-center justify-center px-6 py-10 text-center">
              <Icon name="sparkles" size="xl" className="mb-4 h-12 w-12 surface-muted" />
              <p className="text-lg font-medium text-gray-900 dark:text-gray-100">{t('asyncImage.list.empty')}</p>
              <p className="mt-1 max-w-md text-sm surface-muted">{t('asyncImage.list.emptyHint')}</p>
            </div>
          )}
        />
      </div>

      {(tasks.length > 0 || offset > 0) && (
        <div className="card flex flex-wrap items-center justify-end gap-3 px-4 py-3">
          <span className="mr-auto text-sm surface-muted">{t('asyncImage.list.page', { page: currentPage })}</span>
          <button type="button" className="btn btn-secondary btn-sm" disabled={offset === 0 || loadingTasks} onClick={previousPage}>
            {t('asyncImage.list.previous')}
          </button>
          <button type="button" className="btn btn-secondary btn-sm" disabled={!hasMore || loadingTasks} onClick={nextPage}>
            {t('asyncImage.list.next')}
          </button>
        </div>
      )}

      <BaseDialog show={showCreateDialog} title={t('asyncImage.create.title')} width="wide" onClose={closeCreateDialog}>
        <form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault()
            void submitTask()
          }}
        >
          {eligibleKeys.length === 0 ? (
            <div className="rounded border border-amber-200 bg-amber-50/[0.15] px-3 py-2 text-sm text-amber-800 dark:border-amber-800/60 dark:bg-amber-900/20 dark:text-amber-200">
              {t('asyncImage.create.noKeys')}
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="sm:col-span-2">
                <span className="input-label">{t('asyncImage.create.mode')}</span>
                <div className="mt-2 inline-flex gap-1 rounded-[var(--surface-radius-panel)] border border-gray-200 p-1 dark:border-dark-600">
                  <button
                    type="button"
                    className={`surface-control h-9 rounded-[var(--surface-radius-item)] px-4 text-sm font-medium transition-colors ${
                      form.mode === 'generation'
                        ? 'bg-primary-600 text-white'
                        : 'surface-field text-gray-700 hover:surface-hover dark:surface-field dark:text-gray-200 dark:hover:surface-hover'
                    }`}
                    disabled={submitting}
                    onClick={() => setForm((previous) => ({ ...previous, mode: 'generation' }))}
                  >
                    {t('asyncImage.create.generation')}
                  </button>
                  <button
                    type="button"
                    className={`surface-control h-9 rounded-[var(--surface-radius-item)] px-4 text-sm font-medium transition-colors ${
                      form.mode === 'edit'
                        ? 'bg-primary-600 text-white'
                        : 'surface-field text-gray-700 hover:surface-hover dark:surface-field dark:text-gray-200 dark:hover:surface-hover'
                    }`}
                    disabled={submitting}
                    onClick={() => setForm((previous) => ({ ...previous, mode: 'edit' }))}
                  >
                    {t('asyncImage.create.edit')}
                  </button>
                </div>
              </div>
              <label className="block">
                <span className="input-label">{t('asyncImage.create.apiKey')}</span>
                <Select
                  value={form.apiKeyId}
                  className="w-full"
                  disabled={submitting}
                  searchable={false}
                  options={eligibleKeys.map((key) => ({ value: key.id, label: key.name }))}
                  onValueChange={(value) => {
                    setForm((previous) => ({ ...previous, apiKeyId: Number(value) }))
                    void loadFormModels()
                  }}
                />
              </label>
              <label className="block">
                <span className="input-label">{t('asyncImage.create.model')}</span>
                <Select
                  value={form.model}
                  className="w-full"
                  disabled={loadingModels || availableModels.length === 0 || submitting}
                  searchable={false}
                  options={[
                    ...(loadingModels ? [{ value: '', label: t('asyncImage.create.modelsLoading'), disabled: true }] : []),
                    ...(!loadingModels && availableModels.length === 0
                      ? [{ value: '', label: t('asyncImage.create.noModels'), disabled: true }]
                      : []),
                    ...availableModels.map((model) => ({ value: model, label: model }))
                  ]}
                  onValueChange={(value) => {
                    const next = value === null ? '' : String(value)
                    setForm((previous) => ({ ...previous, model: next }))
                    onModelChanged(next)
                  }}
                />
              </label>
              <label className="block sm:col-span-2">
                <span className="input-label">{t('asyncImage.create.prompt')}</span>
                <textarea
                  value={form.prompt}
                  onChange={(event) => setForm((previous) => ({ ...previous, prompt: event.target.value }))}
                  className="input min-h-32 w-full resize-y"
                  placeholder={t('asyncImage.create.promptPlaceholder')}
                />
              </label>
              {form.mode === 'edit' && (
                <div className="space-y-4 sm:col-span-2">
                  <div>
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <div>
                        <span className="input-label">{t('asyncImage.create.referenceImages')}</span>
                        <p className="mt-1 text-xs surface-muted">{t('asyncImage.create.referenceImagesHint')}</p>
                      </div>
                      <label
                        className={`btn btn-secondary btn-sm cursor-pointer ${
                          editImages.length >= MAX_EDIT_INPUT_IMAGES ? 'pointer-events-none opacity-60' : ''
                        }`}
                      >
                        <Icon name="upload" size="sm" className="mr-1.5" />
                        {t('asyncImage.create.addReferenceImages')}
                        <input
                          type="file"
                          accept="image/png,image/jpeg,image/webp"
                          multiple
                          className="hidden"
                          disabled={submitting || editImages.length >= MAX_EDIT_INPUT_IMAGES}
                          onChange={handleReferenceImageFiles}
                        />
                      </label>
                    </div>
                    {editImages.length > 0 && (
                      <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
                        {editImages.map((image, index) => (
                          <div
                            key={image.url}
                            className="surface-inset relative overflow-hidden rounded border border-gray-200 dark:border-dark-600"
                          >
                            <img src={image.url} alt={image.file.name} className="aspect-square h-full w-full object-cover" />
                            <button
                              type="button"
                              className="surface-control absolute right-1 top-1 inline-flex h-6 w-6 items-center justify-center rounded bg-black/60 text-white hover:bg-red-600"
                              title={t('asyncImage.create.removeReferenceImage')}
                              disabled={submitting}
                              onClick={() => removeReferenceImage(index)}
                            >
                              <Icon name="x" size="xs" />
                            </button>
                            <p className="truncate px-2 py-1.5 text-xs surface-muted" title={image.file.name}>
                              {image.file.name}
                            </p>
                          </div>
                        ))}
                      </div>
                    )}
                    {!editImages.length && (
                      <p className="mt-3 text-sm text-amber-700 dark:text-amber-300">
                        {t('asyncImage.create.referenceImageRequired')}
                      </p>
                    )}
                  </div>
                  <div className="border-t border-gray-100 pt-4 dark:border-dark-700">
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <div>
                        <span className="input-label">{t('asyncImage.create.maskImage')}</span>
                        <p className="mt-1 text-xs surface-muted">{t('asyncImage.create.maskImageHint')}</p>
                      </div>
                      <label
                        className={`btn btn-secondary btn-sm cursor-pointer ${
                          !editImages.length || editMask ? 'pointer-events-none opacity-60' : ''
                        }`}
                      >
                        <Icon name="upload" size="sm" className="mr-1.5" />
                        {t('asyncImage.create.addMaskImage')}
                        <input
                          type="file"
                          accept="image/png,image/webp"
                          className="hidden"
                          disabled={submitting || !editImages.length || !!editMask}
                          onChange={handleMaskImageFile}
                        />
                      </label>
                    </div>
                    {editMask && (
                      <div className="surface-inset mt-3 flex max-w-sm items-center gap-3 rounded border border-gray-200 p-2 dark:border-dark-600">
                        <img src={editMask.url} alt={editMask.file.name} className="h-14 w-14 rounded object-cover" />
                        <p className="min-w-0 flex-1 truncate text-sm surface-muted" title={editMask.file.name}>
                          {editMask.file.name}
                        </p>
                        <button
                          type="button"
                          className="icon-button surface-control"
                          title={t('asyncImage.create.removeMaskImage')}
                          disabled={submitting}
                          onClick={removeMaskImage}
                        >
                          <Icon name="x" size="xs" />
                        </button>
                      </div>
                    )}
                  </div>
                </div>
              )}
              <label className="block">
                <span className="input-label">{t('asyncImage.create.size')}</span>
                <Select
                  value={form.size}
                  className="w-full"
                  searchable={false}
                  options={sizeOptions.map((option) => ({ value: option.value, label: option.label }))}
                  onValueChange={(value) => setForm((previous) => ({ ...previous, size: String(value) }))}
                />
              </label>
              {usingCustomSize && (
                <div className="grid grid-cols-1 gap-4 sm:col-span-2 sm:grid-cols-2">
                  <label className="block">
                    <span className="input-label">{t('asyncImage.create.width')}</span>
                    <input
                      value={form.customWidth}
                      onChange={(event) => setForm((previous) => ({ ...previous, customWidth: Number(event.target.value) }))}
                      type="number"
                      min="16"
                      max="3840"
                      step="16"
                      className="input w-full"
                    />
                  </label>
                  <label className="block">
                    <span className="input-label">{t('asyncImage.create.height')}</span>
                    <input
                      value={form.customHeight}
                      onChange={(event) => setForm((previous) => ({ ...previous, customHeight: Number(event.target.value) }))}
                      type="number"
                      min="16"
                      max="3840"
                      step="16"
                      className="input w-full"
                    />
                  </label>
                  {customSizeError ? (
                    <p className="text-xs text-red-600 sm:col-span-2 dark:text-red-300">{customSizeError}</p>
                  ) : customSizeExperimental ? (
                    <p className="text-xs text-amber-700 sm:col-span-2 dark:text-amber-300">
                      {t('asyncImage.create.experimentalSizeHint')}
                    </p>
                  ) : null}
                </div>
              )}
              {selectedModelIsGPTImage2 && (
                <label className="block">
                  <span className="input-label">{t('asyncImage.create.quality')}</span>
                  <Select
                    value={form.quality}
                    className="w-full"
                    searchable={false}
                    options={[
                      { value: 'auto', label: t('asyncImage.create.qualityAuto') },
                      { value: 'low', label: t('asyncImage.create.qualityLow') },
                      { value: 'medium', label: t('asyncImage.create.qualityMedium') },
                      { value: 'high', label: t('asyncImage.create.qualityHigh') }
                    ]}
                    onValueChange={(value) => setForm((previous) => ({ ...previous, quality: String(value) }))}
                  />
                </label>
              )}
              <label className="block">
                <span className="input-label">{t('asyncImage.create.count')}</span>
                <Select
                  value={form.n}
                  className="w-full"
                  searchable={false}
                  options={[
                    { value: 1, label: '1' },
                    { value: 2, label: '2' },
                    { value: 3, label: '3' },
                    { value: 4, label: '4' }
                  ]}
                  onValueChange={(value) => setForm((previous) => ({ ...previous, n: Number(value) }))}
                />
              </label>
            </div>
          )}
          <div className="flex justify-end gap-2 border-t border-gray-200 pt-4 dark:border-dark-700">
            <button type="button" className="btn btn-secondary" onClick={closeCreateDialog}>
              {t('common.cancel')}
            </button>
            <button
              type="submit"
              className="btn btn-primary"
              disabled={
                submitting ||
                eligibleKeys.length === 0 ||
                loadingModels ||
                !form.model ||
                (form.mode === 'edit' && editImages.length === 0)
              }
            >
              {submitting && <Icon name="refresh" size="sm" className="mr-2 animate-spin" />}
              {submitting ? t('common.submitting') : t('asyncImage.actions.submit')}
            </button>
          </div>
        </form>
      </BaseDialog>

      <BaseDialog
        show={!!selectedTask}
        title={t('asyncImage.detail.title')}
        width="extra-wide"
        onClose={() => setSelectedTask(null)}
      >
        {selectedTask && (
          <div className="space-y-5">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-3 lg:grid-cols-6">
              <div>
                <p className="text-xs surface-muted">{t('asyncImage.columns.status')}</p>
                <span className={`mt-1 inline-flex badge ${statusClass(selectedTask.status)}`}>
                  {statusLabel(selectedTask.status)}
                </span>
              </div>
              <div>
                <p className="text-xs surface-muted">{t('asyncImage.create.model')}</p>
                <p className="mt-1 truncate text-sm font-medium text-gray-900 dark:text-white">
                  {selectedTask.model || '-'}
                </p>
              </div>
              <div>
                <p className="text-xs surface-muted">{t('asyncImage.columns.type')}</p>
                <span className={`mt-1 inline-flex badge ${requestTypeClass(selectedTask.request_type)}`}>
                  {requestTypeLabel(selectedTask.request_type)}
                </span>
              </div>
              <div>
                <p className="text-xs surface-muted">{t('asyncImage.detail.taskId')}</p>
                <p className="mt-1 truncate font-mono text-xs surface-muted" title={selectedTask.task_id}>
                  {selectedTask.task_id}
                </p>
              </div>
              <div>
                <p className="text-xs surface-muted">{t('asyncImage.detail.requestedImages')}</p>
                <p className="mt-1 text-sm font-medium text-gray-900 dark:text-white">
                  {selectedTask.requested_images || 1}
                </p>
              </div>
              <div>
                <p className="text-xs surface-muted">{t('asyncImage.detail.actualImages')}</p>
                <p className="mt-1 text-sm font-medium text-gray-900 dark:text-white">
                  {selectedTask.actual_images ?? taskImageUrls(selectedTask).length}
                </p>
              </div>
            </div>
            <div>
              <p className="text-xs surface-muted">{t('asyncImage.detail.prompt')}</p>
              <p className="mt-1 whitespace-pre-wrap text-sm text-gray-800 dark:text-gray-100">
                {selectedTask.prompt_preview || '-'}
              </p>
            </div>
            {selectedTask.status === 'failed' ? (
              <div className="rounded border border-red-200 bg-red-50/[0.15] px-3 py-2 text-sm text-red-800 dark:border-red-900/70 dark:bg-red-950/30 dark:text-red-200">
                <span className="font-medium">{t('asyncImage.detail.error')}: </span>
                {selectedTask.error?.message || '-'}
              </div>
            ) : selectedTask.status === 'processing' ? (
              <div className="rounded border border-blue-200 bg-blue-50/[0.15] px-3 py-2 text-sm text-blue-800 dark:border-blue-900/70 dark:bg-blue-950/30 dark:text-blue-200">
                {t('asyncImage.detail.pending')}
              </div>
            ) : taskImageUrls(selectedTask).length ? (
              <div className="space-y-3">
                {imageCountMismatch(selectedTask) && (
                  <div className="rounded border border-amber-200 bg-amber-50/[0.15] px-3 py-2 text-sm text-amber-800 dark:border-amber-900/70 dark:bg-amber-950/30 dark:text-amber-200">
                    {t('asyncImage.detail.countMismatch', {
                      requested: selectedTask.requested_images || 1,
                      actual: selectedTask.actual_images ?? taskImageUrls(selectedTask).length
                    })}
                  </div>
                )}
                <div className="flex justify-end">
                  <button
                    type="button"
                    className="btn btn-secondary btn-sm"
                    disabled={downloading || !selectedTaskApiKey}
                    onClick={() => void downloadSelectedTask()}
                  >
                    <Icon
                      name={downloading ? 'refresh' : 'download'}
                      size="sm"
                      className={`mr-1.5 ${downloading ? 'animate-spin' : ''}`}
                    />
                    {downloading ? t('common.submitting') : t('asyncImage.actions.downloadAll')}
                  </button>
                </div>
                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                  {taskImageUrls(selectedTask).map((url) => (
                    <div
                      key={url}
                      className="surface-inset overflow-hidden rounded border border-gray-200 dark:border-dark-600"
                    >
                      <img src={url} alt="" className="aspect-square h-full w-full object-cover" loading="lazy" />
                    </div>
                  ))}
                </div>
              </div>
            ) : (
              <p className="text-sm surface-muted">{t('asyncImage.detail.noImage')}</p>
            )}
          </div>
        )}
      </BaseDialog>

      <ConfirmDialog
        open={!!deleteTarget}
        title={t('asyncImage.delete.title')}
        message={deleteConfirmMessage}
        confirmText={t('common.delete')}
        cancelText={t('common.cancel')}
        variant="danger"
        onConfirm={() => void confirmDeleteTask()}
        onCancel={() => setDeleteTarget(null)}
      />
    </div>
  )
}
