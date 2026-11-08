import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/components/DataTable'
import { Modal, SearchInput, Select, toast } from '@/components/ui'
import { keysAPI } from '@/api'
import {
  cancelBatchImageJob,
  deleteBatchImageJobRecord,
  downloadBatchImageZip,
  getBatchImageItemContent,
  getBatchImageJob,
  listBatchImageJobs,
  listBatchImageItems,
  listBatchImageModels,
  saveBlob,
  submitBatchImageJob,
  type BatchImageItem,
  type BatchImageJob,
  type BatchImageJobsListOptions,
  type BatchImageReferenceImage,
  type BatchImageSubmitItem
} from '@/api/batchImage'
import type { ApiKey } from '@/types'
import { Icon } from './icons'
import { useClipboard } from './useClipboard'
import { getPersistedPageSize, setPersistedPageSize } from './useUsageSnapshot'

type BatchImageJobRow = Pick<
  BatchImageJob,
  | 'id'
  | 'task_name'
  | 'parent_batch_id'
  | 'status'
  | 'model'
  | 'provider'
  | 'item_count'
  | 'success_count'
  | 'fail_count'
  | 'estimated_cost'
  | 'hold_amount'
  | 'actual_cost'
  | 'created_at'
  | 'downloaded_at'
> & {
  api_key_id: number
  api_key_name: string
  child_count: number
  is_child?: boolean
}

type BatchImageDetailItem = BatchImageItem & {
  batch_id: string
  source_task_name: string
}

type PromptRow = {
  localId: string
  custom_id: string
  prompt: string
  output_count: number
  reference_images: BatchImageReferenceImage[]
}

type ReferenceImageDraft = BatchImageReferenceImage & {
  name: string
  size: number
}

const TERMINAL_STATUSES = new Set(['completed', 'failed', 'cancelled', 'output_deleted'])
const BATCH_IMAGE_MAX_OUTPUTS_PER_ITEM = 4
const BATCH_IMAGE_MAX_OUTPUTS_PER_JOB = 200
const outputCountOptions = Array.from({ length: BATCH_IMAGE_MAX_OUTPUTS_PER_ITEM }, (_, index) => index + 1)
const batchPageSizeOptions = [20, 50, 100].map((size) => ({ value: size, label: String(size) }))

function defaultTaskName(timestamp?: number): string {
  const date = timestamp ? new Date(timestamp * 1000) : new Date()
  return date.toLocaleString()
}

function formatDate(timestamp: number): string {
  if (!timestamp) return ''
  return new Date(timestamp * 1000).toLocaleString()
}

function formatMoney(value: number | null | undefined): string {
  if (value === null || value === undefined || Number.isNaN(Number(value))) return '$0.00'
  return `$${Number(value).toFixed(2)}`
}

function readFileAsBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(reader.error || new Error('Failed to read file'))
    reader.onload = () => {
      const result = String(reader.result || '')
      resolve(result.includes(',') ? result.slice(result.indexOf(',') + 1) : result)
    }
    reader.readAsDataURL(file)
  })
}

export default function BatchImageGuideView() {
  const { t, i18n } = useTranslation()
  const { copyToClipboard } = useClipboard()

  const [form, setForm] = useState({
    apiKeyId: 0,
    taskName: '',
    model: '',
    responseMimeType: 'image/png'
  })
  const [filters, setFilters] = useState({ taskName: '', apiKeyId: '', status: '', downloaded: '' })
  const [pagination, setPagination] = useState({
    page: 1,
    page_size: Math.min(getPersistedPageSize(20), 100),
    has_more: false
  })
  const [apiKeys, setApiKeys] = useState<ApiKey[]>([])
  const [loadingKeys, setLoadingKeys] = useState(false)
  const [loadingJobs, setLoadingJobs] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [cancelling, setCancelling] = useState(false)
  const [downloading, setDownloading] = useState(false)
  const [downloadingBatchId, setDownloadingBatchId] = useState('')
  const [retryingBatchId, setRetryingBatchId] = useState('')
  const [bulkDownloading, setBulkDownloading] = useState(false)
  const [bulkDeleting, setBulkDeleting] = useState(false)
  const [deletingBatchId, setDeletingBatchId] = useState('')
  const [loadingItems, setLoadingItems] = useState(false)
  const [loadingModels, setLoadingModels] = useState(false)
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [showGuideModal, setShowGuideModal] = useState(false)
  const [currentJob, setCurrentJob] = useState<BatchImageJob | null>(null)
  const [selectedBatchId, setSelectedBatchId] = useState('')
  const [selectedBatchApiKeyId, setSelectedBatchApiKeyId] = useState(0)
  const [items, setItems] = useState<BatchImageDetailItem[]>([])
  const [batchJobs, setBatchJobs] = useState<BatchImageJobRow[]>([])
  const [selectedJobIds, setSelectedJobIds] = useState<Set<string>>(new Set())
  const [expandedParentIds, setExpandedParentIds] = useState<Set<string>>(new Set())
  const [promptRows, setPromptRows] = useState<PromptRow[]>([])
  const [promptDraft, setPromptDraft] = useState('')
  const [customIdDraft, setCustomIdDraft] = useState('')
  const [outputCountDraft, setOutputCountDraft] = useState(1)
  const [referenceImageDrafts, setReferenceImageDrafts] = useState<ReferenceImageDraft[]>([])
  const [itemPreviewUrls, setItemPreviewUrls] = useState<Record<string, string>>({})
  const [previewLoadingIds, setPreviewLoadingIds] = useState<Set<string>>(new Set())
  const [previewErrorIds, setPreviewErrorIds] = useState<Set<string>>(new Set())
  const [previewImageItem, setPreviewImageItem] = useState<BatchImageItem | null>(null)
  const [availableBatchImageModels, setAvailableBatchImageModels] = useState<Array<{ value: string; label: string }>>([])
  const [modelLoadError, setModelLoadError] = useState('')
  const [openMoreJobId, setOpenMoreJobId] = useState('')
  const [moreMenuStyle, setMoreMenuStyle] = useState<Record<string, string>>({})

  const pollTimer = useRef<ReturnType<typeof setInterval> | null>(null)
  const modelRequestSeq = useRef(0)

  const filtersRef = useRef(filters)
  filtersRef.current = filters
  const paginationRef = useRef(pagination)
  paginationRef.current = pagination
  const batchJobsRef = useRef(batchJobs)
  batchJobsRef.current = batchJobs
  const selectedBatchIdRef = useRef(selectedBatchId)
  selectedBatchIdRef.current = selectedBatchId
  const selectedBatchApiKeyIdRef = useRef(selectedBatchApiKeyId)
  selectedBatchApiKeyIdRef.current = selectedBatchApiKeyId
  const formRef = useRef(form)
  formRef.current = form
  const itemPreviewUrlsRef = useRef(itemPreviewUrls)
  itemPreviewUrlsRef.current = itemPreviewUrls
  const previewLoadingIdsRef = useRef(previewLoadingIds)
  previewLoadingIdsRef.current = previewLoadingIds

  const geminiApiKeys = useMemo(
    () =>
      apiKeys.filter(
        (key) =>
          key.status === 'active' &&
          key.group?.platform === 'gemini' &&
          key.group?.allow_batch_image_generation === true
      ),
    [apiKeys]
  )

  const selectedApiKey = useMemo(
    () => geminiApiKeys.find((key) => key.id === Number(form.apiKeyId)) || null,
    [geminiApiKeys, form.apiKeyId]
  )

  const filteredApiKeys = useMemo(() => {
    const selectedFilterID = Number(filters.apiKeyId || 0)
    if (!selectedFilterID) return geminiApiKeys
    return geminiApiKeys.filter((key) => key.id === selectedFilterID)
  }, [geminiApiKeys, filters.apiKeyId])

  const apiKeyFilterOptions = useMemo(
    () => [
      { value: '', label: t('batchImage.filters.allApiKeys') },
      ...geminiApiKeys.map((key) => ({ value: String(key.id), label: key.name || `API Key #${key.id}` }))
    ],
    [geminiApiKeys, t]
  )

  const statusFilterOptions = useMemo(
    () => [
      { value: '', label: t('batchImage.filters.allStatuses') },
      { value: 'queued', label: t('batchImage.status.queued') },
      { value: 'running', label: t('batchImage.status.running') },
      { value: 'processing_results', label: t('batchImage.status.processingResults') },
      { value: 'settling', label: t('batchImage.status.settling') },
      { value: 'completed', label: t('batchImage.status.completed') },
      { value: 'failed', label: t('batchImage.status.failed') },
      { value: 'cancelled', label: t('batchImage.status.cancelled') },
      { value: 'output_deleted', label: t('batchImage.status.outputDeleted') }
    ],
    [t]
  )

  const downloadFilterOptions = useMemo(
    () => [
      { value: '', label: t('batchImage.filters.allDownloadStates') },
      { value: 'true', label: t('batchImage.filters.downloaded') },
      { value: 'false', label: t('batchImage.filters.notDownloaded') }
    ],
    [t]
  )

  const childrenByParent = useMemo(() => {
    const groups = new Map<string, BatchImageJobRow[]>()
    for (const job of batchJobs) {
      if (!job.parent_batch_id) continue
      const rows = groups.get(job.parent_batch_id) || []
      rows.push(job)
      groups.set(job.parent_batch_id, rows)
    }
    for (const rows of groups.values()) {
      rows.sort((a, b) => a.created_at - b.created_at)
    }
    return groups
  }, [batchJobs])

  const visibleBatchJobs = useMemo(() => {
    const rows: BatchImageJobRow[] = []
    for (const job of batchJobs.filter((item) => !item.parent_batch_id)) {
      rows.push(job)
      if (expandedParentIds.has(job.id)) {
        rows.push(...(childrenByParent.get(job.id) || []).map((child) => ({ ...child, is_child: true })))
      }
    }
    return rows
  }, [batchJobs, expandedParentIds, childrenByParent])

  const selectedRows = useMemo(() => batchJobs.filter((job) => selectedJobIds.has(job.id)), [batchJobs, selectedJobIds])

  const allVisibleSelected =
    visibleBatchJobs.length > 0 && visibleBatchJobs.every((job) => selectedJobIds.has(job.id))
  const someVisibleSelected =
    visibleBatchJobs.some((job) => selectedJobIds.has(job.id)) && !allVisibleSelected

  const canDownload = useCallback((job: Pick<BatchImageJob, 'status' | 'success_count'>) => {
    return job.status === 'completed' && job.success_count > 0
  }, [])

  const selectedDownloadableRows = useMemo(
    () => selectedRows.filter((job) => canDownload(job)),
    [selectedRows, canDownload]
  )

  const displayJob = useCallback(
    <T extends Pick<BatchImageJob, 'id' | 'parent_batch_id' | 'status' | 'item_count' | 'success_count' | 'fail_count' | 'estimated_cost' | 'hold_amount' | 'actual_cost'>>(
      job: T
    ): T => {
      if (job.parent_batch_id) return job
      const children = childrenByParent.get(job.id) || []
      if (!children.length) return job

      const childSuccess = children.reduce((sum, child) => sum + child.success_count, 0)
      const childEstimated = children.reduce((sum, child) => sum + child.estimated_cost, 0)
      const childHold = children.reduce((sum, child) => sum + child.hold_amount, 0)
      const childActual = children.reduce((sum, child) => sum + (child.actual_cost || 0), 0)
      const childActualReady = children.every((child) => child.actual_cost !== null)
      const successCount = Math.min(job.item_count, job.success_count + childSuccess)
      const failCount = Math.max(0, job.item_count - successCount)
      const actualCost =
        job.actual_cost === null ? (childActualReady ? childActual : null) : job.actual_cost + childActual

      return {
        ...job,
        success_count: successCount,
        fail_count: failCount,
        status: failCount === 0 && TERMINAL_STATUSES.has(job.status) ? 'completed' : job.status,
        estimated_cost: job.estimated_cost + childEstimated,
        hold_amount: job.hold_amount + childHold,
        actual_cost: actualCost
      }
    },
    [childrenByParent]
  )

  const canRetry = useCallback(
    (job: Pick<BatchImageJob, 'status' | 'fail_count'>) => {
      const display = 'id' in job ? displayJob(job as BatchImageJob) : job
      return TERMINAL_STATUSES.has(display.status) && display.fail_count > 0
    },
    [displayJob]
  )

  const canDeleteRecord = useCallback((job: Pick<BatchImageJob, 'status'>) => TERMINAL_STATUSES.has(job.status), [])
  const canCancel = useCallback((job: Pick<BatchImageJob, 'status'>) => !TERMINAL_STATUSES.has(job.status), [])

  const selectedModelReferenceLimit = useCallback((model: string) => {
    const normalized = String(model || '').toLowerCase()
    if (normalized.includes('pro-image')) return 14
    if (normalized.includes('flash-image')) return 3
    return 0
  }, [])

  const estimatedOutputCount = promptRows.reduce((sum, row) => sum + normalizeOutputCount(row.output_count), 0)

  const endpointBase = useMemo(() => {
    if (typeof window !== 'undefined') return window.location.origin.replace(/\/+$/, '')
    return '<你的 Sub2API Plus API 端点>'
  }, [])

  const agentInstruction = useMemo(
    () => `---
name: sub2api-batch-image
description: 当用户希望用 Gemini/Vertex 批量生成图片、批量跑提示词、下载批量生图结果、重试失败图片时使用。
---

你是 Codex 中的批量生图执行 Agent。用户不需要手动填写页面表单；你应从当前聊天、用户给的文件、目录或上下文中整理任务名称、prompt 列表和输出目录，只有缺少关键决策时才向用户提问。

默认端点：
${endpointBase}

你需要自己完成：
1. 从用户聊天或附件中提取 prompt。每条 prompt 保留完整文本，按顺序生成稳定 custom_id，例如 img_001、img_002。
2. 从用户要求或上下文推断任务名称；没有明确名称时用当前时间生成任务名。
3. 从用户要求或上下文推断输出目录；如果用户没有说保存到哪里，才询问用户。
4. 提交前必须先计算 expected_output_count = 所有 item 的 output_count 之和。单个批量任务硬性最多 200 张输出图；超过 200 张必须拆成多组任务，不能提交一个超大任务，也不能把参考图附件上限当成生成张数上限。
5. 如果用户提供参考图，把参考图按用途绑定到具体 item。参考图只是输入附件，不是输出图数量。模型单条限制必须按模型执行：Gemini 2.5 Flash Image 每条最多 3 张参考图；Gemini 3 Pro Image 每条最多 14 张参考图。不要把后端附件风控理解成 Pro 单条能力：按 output_count 展开后，所有 item 的参考图附件总数还有内部保护阈值 1000 个，inline base64 参考图解码后总量最多 128MB。这个 1000 只是服务器拒绝异常请求的保护阈值，不是推荐规模；参考图很多或总请求体较大时应主动拆分任务。
6. 参考图会按 output_count 重复消耗输入 token；大量任务、重复复用同一张参考图或参考图总体积较大时，优先使用 gs:// file_uri 或拆分成多组任务。
7. 选择 API Key 和模型：先获取当前可用的批量生图 Key/模型；如果用户指定模型且该 Key 支持，则使用用户指定模型；否则使用该 Key 可用模型中的默认/第一个。不要展示或询问内部 provider 名称。
8. 调用批量生图 API 提交、轮询、下载，不要求用户去页面里手填。

API 调用规范：
- 模型：GET ${joinEndpointPath(endpointBase, '/v1/images/batches/models')}
- 提交：POST ${joinEndpointPath(endpointBase, '/v1/images/batches')}
- 查询：GET ${joinEndpointPath(endpointBase, '/v1/images/batches/{id}')}
- 明细：GET ${joinEndpointPath(endpointBase, '/v1/images/batches/{id}/items')}
- 下载：GET ${joinEndpointPath(endpointBase, '/v1/images/batches/{id}/download')}
- 取消：POST ${joinEndpointPath(endpointBase, '/v1/images/batches/{id}/cancel')}

提交请求体：
{
  "model": "<按所选 Key 可用模型填写>",
  "task_name": "<从聊天推断；为空则用当前时间>",
  "image_size": "1K",
  "response_mime_type": "image/png",
  "items": [
    {
      "custom_id": "img_001",
      "prompt": "<第一条完整 prompt>",
      "output_count": 1,
      "reference_images": [
        {
          "id": "face",
          "type": "subject",
          "mime_type": "image/png",
          "data": "<base64，不含 data:image/png;base64, 前缀>"
        }
      ]
    }
  ]
}

必须遵守：
- 不要把 API Key 写入仓库、日志、提交记录或最终回复。
- 不要把参考图 base64 写入最终回复、日志或公开文件。恢复记录中只保存参考图文件名、用途、数量和请求 JSON 文件路径；若请求 JSON 文件包含 base64，应保存在用户指定输出目录且不要提交到仓库。
- output_count 表示同一 prompt 和参考图重复生成几张，默认 1，每条最多 4；这不是依赖 Gemini 单次请求返回多图，而是系统展开成多个真实任务项。提交前必须确认预计输出图总数不超过 200，超过就拆分成多组任务。绝不能因为参考图附件有更高的内部保护阈值，就提交会生成超过 200 张图的任务。
- 当前对用户的批量生图计费仍按成功输出图片数量结算，不单独对参考图加价。可以向用户说明：参考图会产生少量上游输入 token 和临时存储成本，且会随 output_count 重复计算；页面显示的冻结/结算金额按输出图片数量计算。
- 提交成功后，必须立刻在输出目录写入本地恢复记录，例如 batch-image-resume.json。不要在恢复记录里保存 API Key。
- 恢复记录至少包含：endpoint、task_name、batch_id、model、output_dir、request_file、submitted_at、last_status、status_url、items_url、download_url、prompt_count、expected_output_count，以及可用于失败重试的 custom_id 到 prompt 映射或请求 JSON 文件路径。
- 每次查询状态后更新恢复记录，写入 last_checked_at、last_status、成功数、失败数、实际扣费和失败摘要。会话中断或暂停后，下次必须能凭该文件继续查询、下载或重试。
- 不要高频轮询。首次查询等待约 20 到 30 秒；queued 状态每 60 到 120 秒查询一次；如果连续 3 次仍是 queued，就先停止主动查询，告诉用户任务仍在排队，并保留恢复记录，之后可继续其他任务或等待用户稍后让你恢复。
- running 状态每约 60 秒查询一次，服务器压力大或大批量任务时可以更久；processing_results 等接近完成的状态可每 20 到 45 秒查询一次。
- 任务完成后报告任务名、任务 id、成功数、失败数、实际扣费和保存路径。
- 只下载成功图片。部分失败时，先展示失败 custom_id、错误码、错误来源和简要原因。
- 重试只能重试失败项，不能重复提交已成功项。若历史任务没有保存失败项 prompt，必须告诉用户无法自动重试，并询问用户是否提供原 prompt。
- 取消任务前必须提醒：已被系统索引为成功的图片仍会按成功项结算扣费，其余冻结金额会释放。
- 图片预览按需加载；不要为了查看列表自动批量加载图片内容。`,
    [endpointBase]
  )

  const batchImageText = useCallback(
    (key: string) => t(`batchImage.messages.${key}`),
    [t]
  )

  const isZhLocale = String(i18n.language || '').toLowerCase().startsWith('zh')

  const batchImageErrorReference = useCallback(
    (error: unknown) => {
      const parts: string[] = []
      const e = error as { code?: string; requestId?: string; status?: string }
      const code = String(e?.code || '').trim()
      const requestId = String(e?.requestId || '').trim()
      const status = String(e?.status || '').trim()
      if (code) parts.push(t('batchImage.messages.errorCodeRef', { code }))
      if (requestId) parts.push(t('batchImage.messages.requestIdRef', { id: requestId }))
      if (!code && status) parts.push(t('batchImage.messages.httpStatusRef', { status }))
      return parts.length ? `（${parts.join(isZhLocale ? '，' : ', ')}）` : ''
    },
    [isZhLocale, t]
  )

  const batchImageErrorMessage = useCallback(
    (error: unknown, fallback: string) => {
      const e = error as { code?: string; message?: string; requestId?: string; status?: string }
      const code = String(e?.code || '').trim()
      const message = String(e?.message || '').trim()
      const reference = batchImageErrorReference(error)
      const adminError = (base: string) => `${base}${reference ? ` ${reference}` : ''} ${batchImageText('adminReference')}`
      if (code === 'API_KEY_REQUIRED' || code === '401') return batchImageText('authRequired')
      if (code === 'BATCH_IMAGE_NO_ACCOUNT_AVAILABLE' || /no compatible batch image account/i.test(message)) {
        return adminError(batchImageText('noCompatibleAccount'))
      }
      if (code === 'BATCH_IMAGE_UNSUPPORTED_PROVIDER' || /unsupported batch image provider/i.test(message)) {
        return adminError(batchImageText('unsupportedProvider'))
      }
      if (code === 'BATCH_IMAGE_VERTEX_GCS_BUCKET_MISSING' || code === 'VERTEX_MANAGED_GCS_BUCKET_MISSING') {
        return adminError(batchImageText('vertexGcsBucketMissing'))
      }
      if (
        code === 'BATCH_IMAGE_PROVIDER_SUBMIT_FAILED' ||
        code === 'BATCH_IMAGE_PROVIDER_MISSING_API_KEY' ||
        code === 'BATCH_IMAGE_PROVIDER_MISSING_SERVICE_ACCOUNT' ||
        code === 'BATCH_IMAGE_PROVIDER_UNSUPPORTED_ACCOUNT'
      ) {
        return adminError(batchImageText('providerSubmitFailed'))
      }
      if (code === 'BATCH_IMAGE_QUEUE_FAILED' || code === 'BATCH_IMAGE_QUEUE_NOT_CONFIGURED') {
        return adminError(batchImageText('queueFailed'))
      }
      if (code === 'BATCH_IMAGE_BILLING_HOLD_FAILED') return adminError(batchImageText('billingHoldFailed'))
      if (code === 'BATCH_IMAGE_GROUP_DISABLED') return batchImageText('groupDisabled')
      if (code === 'BATCH_IMAGE_SETTLEMENT_PRICING_MISSING') return adminError(batchImageText('pricingMissing'))
      if (code === 'BATCH_IMAGE_INSUFFICIENT_BALANCE') return batchImageText('insufficientBalance')
      if (code === 'BATCH_IMAGE_INVALID_MODEL') return batchImageText('invalidModel')
      if (code === 'BATCH_IMAGE_INVALID_ITEMS') return batchImageText('invalidItems')
      if (code === 'BATCH_IMAGE_DUPLICATE_CUSTOM_ID') return batchImageText('duplicateCustomId')
      if (code === 'BATCH_IMAGE_PROMPT_TOO_LONG') return batchImageText('promptTooLong')
      if (code === 'BATCH_IMAGE_INVALID_REFERENCE_IMAGE') return batchImageText('invalidReferenceImage')
      if (code === 'BATCH_IMAGE_TOO_MANY_REFERENCE_IMAGES') return batchImageText('tooManyReferenceImages')
      if (code === 'BATCH_IMAGE_REFERENCE_IMAGES_TOO_LARGE') return batchImageText('referenceImagesTooLarge')
      if (code === 'BATCH_IMAGE_TOO_MANY_OUTPUT_IMAGES') return batchImageText('tooManyOutputImages')
      if (code === 'BATCH_IMAGE_IDEMPOTENCY_CONFLICT') return batchImageText('idempotencyConflict')
      if (code === 'BATCH_IMAGE_NOT_READY') return batchImageText('notReady')
      if (code === 'BATCH_IMAGE_DOWNLOAD_LIMITED') return batchImageText('downloadLimited')
      if (code === 'BATCH_IMAGE_DOWNLOAD_TOO_LARGE') return batchImageText('downloadTooLarge')
      if (code === 'BATCH_IMAGE_DELETE_NOT_READY') return batchImageText('deleteNotReady')
      if (code === 'BATCH_IMAGE_DISABLED') return batchImageText('disabled')
      if (code === 'BATCH_IMAGE_OUTPUT_DELETED') return batchImageText('outputDeleted')
      if (code === 'BATCH_IMAGE_RESULT_MISSING') return batchImageText('resultMissing')
      if (code === 'BATCH_IMAGE_ITEM_FAILED') return batchImageText('itemFailed')
      if (code === 'BATCH_IMAGE_ITEM_IMAGE_INDEX_OUT_OF_RANGE') return batchImageText('itemImageIndexOutOfRange')
      return fallback
    },
    [batchImageErrorReference, batchImageText]
  )


  const addPromptRow = () => {
    const prompt = promptDraft.trim()
    if (!prompt) return
    const outputCount = normalizeOutputCount(outputCountDraft)
    const used = new Set(promptRows.map((row) => row.custom_id))
    const customID = uniqueCustomID(
      customIdDraft || `img_${String(promptRows.length + 1).padStart(3, '0')}`,
      used,
      promptRows.length
    )
    setPromptRows((previous) => [
      ...previous,
      {
        localId: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        custom_id: customID,
        prompt,
        output_count: outputCount,
        reference_images: referenceImageDrafts.map(({ name: _name, size: _size, ...ref }) => ref)
      }
    ])
    setPromptDraft('')
    setCustomIdDraft('')
    setOutputCountDraft(1)
    setReferenceImageDrafts([])
  }

  const removePromptRow = (index: number) => {
    setPromptRows((previous) => previous.filter((_, currentIndex) => currentIndex !== index))
  }

  const removeReferenceImageDraft = (index: number) => {
    setReferenceImageDrafts((previous) => previous.filter((_, currentIndex) => currentIndex !== index))
  }

  const handleReferenceImageFiles = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const input = event.target
    const files = Array.from(input.files || [])
    input.value = ''
    if (files.length === 0) return
    const limit = selectedModelReferenceLimit(formRef.current.model)
    if (limit <= 0) {
      toast.error(t('batchImage.create.modelNoReferenceImages'))
      return
    }
    const slots = Math.max(0, limit - referenceImageDrafts.length)
    if (slots <= 0) {
      toast.error(t('batchImage.create.refLimitReached', { limit }))
      return
    }
    const accepted = files.slice(0, slots)
    if (accepted.length < files.length) {
      toast.error(t('batchImage.create.refLimitExceededIgnored', { limit }))
    }
    const next: ReferenceImageDraft[] = []
    for (const file of accepted) {
      if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type)) {
        toast.error(t('batchImage.create.refFormatUnsupported'))
        continue
      }
      if (file.size > 10 * 1024 * 1024) {
        toast.error(t('batchImage.create.refFileTooLarge', { name: file.name }))
        continue
      }
      const data = await readFileAsBase64(file)
      next.push({ id: file.name, type: 'reference', mime_type: file.type, data, name: file.name, size: file.size })
    }
    setReferenceImageDrafts((previous) => [...previous, ...next])
  }

  const refreshDetail = async () => {
    await Promise.all([refreshSelected(), loadItems()])
  }

  const hasChildJobs = (batchId: string) => (childrenByParent.get(batchId) || []).length > 0

  const isChildDetailItem = (item: Pick<BatchImageDetailItem, 'batch_id'>) => {
    const rootBatchId = detailRootBatchId
    return Boolean(rootBatchId && item.batch_id && item.batch_id !== rootBatchId)
  }

  const isRecoveredOriginalFailure = (item: BatchImageDetailItem) =>
    Boolean(
      detailRootBatchId &&
        item.batch_id === detailRootBatchId &&
        item.status === 'failed' &&
        recoveredOriginalCustomIds.has(item.custom_id)
    )

  const itemStatusLabel = (status: string) => {
    const statusKeys: Record<string, string> = {
      pending: 'pending',
      succeeded: 'succeeded',
      success: 'succeeded',
      failed: 'failed',
      cancelled: 'cancelled'
    }
    const key = statusKeys[status]
    return key ? t(`batchImage.itemStatus.${key}`) : status
  }

  const itemDisplayStatusLabel = (item: BatchImageDetailItem) =>
    isRecoveredOriginalFailure(item) ? t('batchImage.itemStatus.recovered') : itemStatusLabel(item.status)

  const itemStatusBadgeClass = (status: string) => {
    if (status === 'succeeded' || status === 'success') return 'badge-success'
    if (status === 'failed' || status === 'cancelled') return 'badge-danger'
    return 'badge-primary'
  }

  const itemDisplayStatusBadgeClass = (item: BatchImageDetailItem) =>
    isRecoveredOriginalFailure(item) ? 'badge-gray' : itemStatusBadgeClass(item.status)

  const itemResultLabel = (item: BatchImageDetailItem) => {
    if (isRecoveredOriginalFailure(item)) return t('batchImage.itemResult.recoveredByRetry')
    if (item.error) return friendlyItemError(item.error)
    if (item.status === 'succeeded' || item.status === 'success') {
      return itemPreviewUrls[itemPreviewKey(item)]
        ? t('batchImage.itemResult.readyPreview')
        : t('batchImage.itemResult.readyDownload')
    }
    if (item.status === 'failed') return t('batchImage.itemResult.noUsableImage')
    if (item.status === 'cancelled') return t('batchImage.itemResult.cancelled')
    return t('batchImage.itemResult.waiting')
  }

  const itemResultClass = (item: BatchImageDetailItem) => {
    if (isRecoveredOriginalFailure(item)) {
      return 'surface-inset surface-muted ring-gray-200 dark:surface-inset dark:ring-dark-700'
    }
    if (item.error || item.status === 'failed' || item.status === 'cancelled') {
      return 'bg-red-50/[0.15] text-red-700 ring-red-100 dark:bg-red-950/30 dark:text-red-300 dark:ring-red-900/50'
    }
    if (item.status === 'succeeded' || item.status === 'success') {
      return 'bg-emerald-50/[0.15] text-emerald-700 ring-emerald-100 dark:bg-emerald-950/30 dark:text-emerald-300 dark:ring-emerald-900/50'
    }
    return 'surface-inset surface-muted ring-gray-200 dark:surface-inset dark:ring-dark-700'
  }

  const friendlyItemError = (error: BatchImageItem['error']) => {
    if (!error) return '-'
    if (error.code === 'EMPTY_IMAGE_OUTPUT') return t('batchImage.itemResult.emptyImageOutput')
    if (error.code === 'PROVIDER_ITEM_FAILED') return t('batchImage.itemResult.providerItemFailed')
    return error.message || error.code || '-'
  }

  const statusLabel = (
    jobOrStatus: string | Pick<BatchImageJob, 'status' | 'success_count' | 'fail_count'>
  ) => {
    const status = typeof jobOrStatus === 'string' ? jobOrStatus : jobOrStatus.status
    if (typeof jobOrStatus !== 'string' && status === 'completed' && jobOrStatus.fail_count > 0) {
      if (jobOrStatus.success_count > 0) return t('batchImage.status.partialSuccess')
      return t('batchImage.status.allFailed')
    }
    const statusKeys: Record<string, string> = {
      queued: 'queued',
      running: 'running',
      indexing: 'processingResults',
      processing_results: 'processingResults',
      settling: 'settling',
      completed: 'completed',
      failed: 'failed',
      cancelled: 'cancelled',
      output_deleted: 'outputDeleted'
    }
    const key = statusKeys[status]
    return key ? t(`batchImage.status.${key}`) : status
  }

  const statusBadgeClass = (
    jobOrStatus: string | Pick<BatchImageJob, 'status' | 'success_count' | 'fail_count'>
  ) => {
    const status = typeof jobOrStatus === 'string' ? jobOrStatus : jobOrStatus.status
    if (typeof jobOrStatus !== 'string' && status === 'completed' && jobOrStatus.fail_count > 0) {
      if (jobOrStatus.success_count > 0) return 'badge-warning'
      return 'badge-danger'
    }
    if (status === 'completed') return 'badge-success'
    if (status === 'failed' || status === 'cancelled') return 'badge-danger'
    if (status === 'output_deleted') return 'badge-gray'
    return 'badge-primary'
  }

  const terminalZeroCost = (job: Pick<BatchImageJob, 'status' | 'actual_cost'>) =>
    job.actual_cost === null && (job.status === 'failed' || job.status === 'cancelled')

  const costLabel = (job: Pick<BatchImageJob, 'status' | 'hold_amount' | 'actual_cost'>) => {
    if (job.actual_cost !== null) return formatMoney(job.actual_cost)
    if (terminalZeroCost(job)) return formatMoney(0)
    return t('batchImage.detail.holdCost', { amount: formatMoney(job.hold_amount) })
  }

  const loadApiKeys = useCallback(async () => {
    setLoadingKeys(true)
    try {
      const response = await keysAPI.list(1, 100, { status: 'active', sort_by: 'created_at', sort_order: 'desc' })
      setApiKeys(response.items || [])
      setForm((previous) => ({
        ...previous,
        apiKeyId: previous.apiKeyId || geminiApiKeys[0]?.id || 0
      }))
      setFilters((previous) => ({
        ...previous,
        apiKeyId:
          previous.apiKeyId && geminiApiKeys.some((key) => String(key.id) === previous.apiKeyId)
            ? previous.apiKeyId
            : ''
      }))
    } catch (error: unknown) {
      toast.error(batchImageErrorMessage(error, batchImageText('loadKeysFailed')))
    } finally {
      setLoadingKeys(false)
    }
  }, [batchImageErrorMessage, batchImageText, geminiApiKeys])

  const loadAvailableModels = useCallback(async () => {
    const key = selectedApiKey
    const requestID = ++modelRequestSeq.current
    setModelLoadError('')
    setAvailableBatchImageModels([])
    setForm((previous) => ({ ...previous, model: '' }))
    if (!key) return

    setLoadingModels(true)
    try {
      const result = await listBatchImageModels(key.key)
      if (requestID !== modelRequestSeq.current) return
      const seen = new Set<string>()
      const models = (result.data || [])
        .map((model) => String(model.id || '').trim())
        .filter((model) => {
          if (!model || seen.has(model)) return false
          seen.add(model)
          return true
        })
        .map((model) => ({ value: model, label: model }))
      setAvailableBatchImageModels(models)
      setForm((previous) => ({ ...previous, model: models[0]?.value || '' }))
    } catch (error: unknown) {
      if (requestID !== modelRequestSeq.current) return
      setModelLoadError(batchImageErrorMessage(error, batchImageText('loadModelsFailed')))
    } finally {
      if (requestID === modelRequestSeq.current) setLoadingModels(false)
    }
  }, [batchImageErrorMessage, batchImageText, selectedApiKey])

  const applyChildCounts = (rows: BatchImageJobRow[]) => {
    const counts = new Map<string, number>()
    for (const row of rows) {
      if (!row.parent_batch_id) continue
      counts.set(row.parent_batch_id, (counts.get(row.parent_batch_id) || 0) + 1)
    }
    return rows.map((row) => ({ ...row, child_count: counts.get(row.id) || 0 }))
  }

  const toJobRow = (job: BatchImageJob, key?: ApiKey | null): BatchImageJobRow => ({
    id: job.id,
    task_name: job.task_name || defaultTaskName(job.created_at),
    parent_batch_id: job.parent_batch_id || null,
    status: job.status,
    model: job.model,
    provider: job.provider,
    item_count: job.item_count,
    success_count: job.success_count,
    fail_count: job.fail_count,
    estimated_cost: job.estimated_cost,
    hold_amount: job.hold_amount,
    actual_cost: job.actual_cost,
    created_at: job.created_at,
    downloaded_at: job.downloaded_at,
    api_key_id: key?.id || 0,
    api_key_name: key?.name || '',
    child_count: 0
  })

  const listOptions = (): BatchImageJobsListOptions => {
    const options: BatchImageJobsListOptions = {
      limit: paginationRef.current.page_size,
      cursor: String((paginationRef.current.page - 1) * paginationRef.current.page_size)
    }
    if (filtersRef.current.taskName.trim()) options.taskName = filtersRef.current.taskName.trim()
    if (filtersRef.current.status) options.status = filtersRef.current.status
    if (filtersRef.current.downloaded) options.downloaded = filtersRef.current.downloaded
    return options
  }

  const loadBatchJobs = useCallback(async () => {
    const keys = filteredApiKeys
    if (!keys.length) {
      setBatchJobs([])
      setPagination((previous) => ({ ...previous, has_more: false }))
      return
    }
    setLoadingJobs(true)
    try {
      const options = listOptions()
      const results = await Promise.all(
        keys.map(async (key) => {
          const result = await listBatchImageJobs(key.key, options)
          return {
            hasMore: Boolean(result.has_more),
            rows: (result.data || []).map((job) => toJobRow(job, key))
          }
        })
      )
      setBatchJobs(
        applyChildCounts(
          results
            .flatMap((result) => result.rows)
            .sort((a, b) => b.created_at - a.created_at)
            .slice(0, paginationRef.current.page_size)
        )
      )
      setPagination((previous) => ({ ...previous, has_more: results.some((result) => result.hasMore) }))
    } catch (error: unknown) {
      toast.error(batchImageErrorMessage(error, batchImageText('loadJobsFailed')))
    } finally {
      setLoadingJobs(false)
    }
  }, [batchImageErrorMessage, batchImageText, filteredApiKeys])

  const upsertJob = useCallback((job: BatchImageJob) => {
    setBatchJobs((previous) => {
      const next = toJobRow(job)
      const index = previous.findIndex((item) => item.id === job.id)
      if (index >= 0) {
        const rows = [...previous]
        rows[index] = { ...next, is_child: rows[index].is_child }
        return applyChildCounts(rows)
      }
      return applyChildCounts([next, ...previous].slice(0, paginationRef.current.page_size))
    })
  }, [])

  const keyForSelectedBatch = useCallback((): ApiKey | null => {
    if (selectedBatchApiKeyIdRef.current) {
      const key = geminiApiKeys.find((item) => item.id === selectedBatchApiKeyIdRef.current)
      if (key) return key
    }
    return selectedApiKey
  }, [geminiApiKeys, selectedApiKey])

  const requireApiKey = useCallback((): ApiKey | null => {
    if (!selectedApiKey) {
      toast.error(batchImageText('selectApiKey'))
      return null
    }
    return selectedApiKey
  }, [batchImageText, selectedApiKey])

  const clearItemPreviews = useCallback(() => {
    setItemPreviewUrls((previous) => {
      Object.values(previous).forEach((url) => URL.revokeObjectURL(url))
      return {}
    })
    setPreviewErrorIds(new Set())
  }, [])

  const loadItems = useCallback(async () => {
    const batchId = selectedBatchIdRef.current || currentJob?.id || ''
    if (!batchId) return
    const key = keyForSelectedBatch() || requireApiKey()
    if (!key) return
    setLoadingItems(true)
    try {
      clearItemPreviews()
      const jobs = detailJobsForBatch(batchId)
      const results = await Promise.all(
        jobs.map(async (job) => {
          const result = await listBatchImageItems(key.key, job.id)
          return (result.data || []).map((item) => ({
            ...item,
            batch_id: job.id,
            source_task_name: detailSourceName(job, batchId)
          }))
        })
      )
      setItems(results.flat())
    } catch (error: unknown) {
      toast.error(batchImageErrorMessage(error, batchImageText('loadItemsFailed')))
    } finally {
      setLoadingItems(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [keyForSelectedBatch, requireApiKey])

  const detailJobsForBatch = (batchId: string): BatchImageJobRow[] => {
    const row = batchJobsRef.current.find((job) => job.id === batchId)
    const base =
      row || (currentJob && currentJob.id === batchId ? toJobRow(currentJob, keyForSelectedBatch() || selectedApiKey) : null)
    if (!base) return []
    if (base.parent_batch_id) return [base]
    return [base, ...(childrenByParent.get(base.id) || [])]
  }

  const detailSourceName = (job: Pick<BatchImageJobRow, 'id' | 'task_name' | 'parent_batch_id'>, rootBatchId: string) => {
    const name = job.task_name || job.id
    if (job.id === rootBatchId) return t('batchImage.detail.mainTask', { name })
    return t('batchImage.detail.childTask', { name })
  }

  const refreshSelected = useCallback(async () => {
    if (!selectedBatchIdRef.current) return
    const key = keyForSelectedBatch() || requireApiKey()
    if (!key) return
    setRefreshing(true)
    try {
      const job = await getBatchImageJob(key.key, selectedBatchIdRef.current)
      setCurrentJob(job)
      upsertJob(job)
      if (TERMINAL_STATUSES.has(job.status)) stopPolling()
    } catch (error: unknown) {
      toast.error(batchImageErrorMessage(error, batchImageText('refreshFailed')))
    } finally {
      setRefreshing(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [keyForSelectedBatch, requireApiKey])

  const stopPolling = useCallback(() => {
    if (pollTimer.current) {
      clearInterval(pollTimer.current)
      pollTimer.current = null
    }
  }, [])

  const startPolling = useCallback(() => {
    stopPolling()
    pollTimer.current = setInterval(() => {
      if (!currentJob || TERMINAL_STATUSES.has(currentJob.status)) {
        stopPolling()
        return
      }
      void refreshSelected()
    }, 8000)
  }, [currentJob, refreshSelected, stopPolling])

  const apiKeyForJob = (job: BatchImageJobRow | Pick<BatchImageJob, 'id'>): ApiKey | null => {
    if ('api_key_id' in job && job.api_key_id) {
      return geminiApiKeys.find((key) => key.id === job.api_key_id) || null
    }
    return selectedApiKey
  }

  const toggleJobSelection = (batchId: string, checked: boolean) => {
    setSelectedJobIds((previous) => {
      const next = new Set(previous)
      if (checked) next.add(batchId)
      else next.delete(batchId)
      return next
    })
  }

  const toggleAllVisible = (checked: boolean) => {
    setSelectedJobIds((previous) => {
      const next = new Set(previous)
      for (const job of visibleBatchJobs) {
        if (checked) next.add(job.id)
        else next.delete(job.id)
      }
      return next
    })
  }

  const applyFilters = () => {
    setPagination((previous) => ({ ...previous, page: 1 }))
    setSelectedJobIds(new Set())
    void loadBatchJobs()
  }

  const resetFilters = () => {
    setFilters({ taskName: '', apiKeyId: '', status: '', downloaded: '' })
    applyFilters()
  }

  const refreshPage = async () => {
    await loadApiKeys()
    await loadBatchJobs()
  }

  const handlePageChange = (page: number) => {
    if (page < 1 || page === paginationRef.current.page) return
    setPagination((previous) => ({ ...previous, page }))
    setSelectedJobIds(new Set())
    void loadBatchJobs()
  }

  const handlePageSizeChange = (value: string | number | boolean | null) => {
    if (value === null || typeof value === 'boolean') return
    const nextSize = Math.min(Math.max(Number(value) || 20, 1), 100)
    setPagination((previous) => ({ ...previous, page_size: nextSize, page: 1 }))
    setPersistedPageSize(nextSize)
    setSelectedJobIds(new Set())
    void loadBatchJobs()
  }

  const closeMoreMenu = () => setOpenMoreJobId('')

  const toggleMoreMenu = (job: BatchImageJobRow, event: React.MouseEvent) => {
    if (openMoreJobId === job.id) {
      closeMoreMenu()
      return
    }
    const trigger = event.currentTarget as HTMLElement | null
    const rect = trigger?.getBoundingClientRect()
    if (!rect) return
    const menuWidth = 176
    const margin = 8
    const left = Math.max(margin, Math.min(rect.right - menuWidth, window.innerWidth - menuWidth - margin))
    const top = Math.min(rect.bottom + margin, window.innerHeight - 96)
    setMoreMenuStyle({ left: `${left}px`, top: `${Math.max(margin, top)}px` })
    setOpenMoreJobId(job.id)
  }

  const closeDetail = () => {
    setCurrentJob(null)
    setSelectedBatchId('')
    setSelectedBatchApiKeyId(0)
    setItems([])
    clearItemPreviews()
    stopPolling()
  }

  const selectJob = (batchId: string) => {
    const row = batchJobsRef.current.find((job) => job.id === batchId)
    if (row?.api_key_id && geminiApiKeys.some((key) => key.id === row.api_key_id)) {
      setForm((previous) => ({ ...previous, apiKeyId: row.api_key_id }))
      setSelectedBatchApiKeyId(row.api_key_id)
    } else {
      setSelectedBatchApiKeyId(0)
    }
    setSelectedBatchId(batchId)
    setCurrentJob(null)
    setItems([])
    void refreshSelected()
    void loadItems()
  }

  const parsedItems = useMemo<BatchImageSubmitItem[]>(() => {
    const used = new Set<string>()
    return promptRows
      .map((row, index) => {
        const customID = uniqueCustomID(
          row.custom_id || `img_${String(index + 1).padStart(3, '0')}`,
          used,
          index
        )
        const item: BatchImageSubmitItem = { custom_id: customID, prompt: row.prompt.trim() }
        const outputCount = normalizeOutputCount(row.output_count)
        if (outputCount > 1) item.output_count = outputCount
        if (row.reference_images.length) item.reference_images = row.reference_images
        return item
      })
      .filter((item) => item.prompt)
  }, [promptRows])

  const validateForm = (): boolean => {
    if (!requireApiKey()) return false
    if (!form.model) {
      toast.error(
        availableBatchImageModels.length === 0 ? batchImageText('noModelsForKey') : batchImageText('selectModel')
      )
      return false
    }
    if (parsedItems.length === 0) {
      toast.error(batchImageText('promptRequired'))
      return false
    }
    if (estimatedOutputCount > BATCH_IMAGE_MAX_OUTPUTS_PER_JOB) {
      toast.error(batchImageText('tooManyOutputImages'))
      return false
    }
    const refLimit = selectedModelReferenceLimit(form.model)
    if (promptRows.some((row) => row.reference_images.length > refLimit)) {
      toast.error(batchImageText('tooManyReferenceImages'))
      return false
    }
    return true
  }

  const submitJob = async () => {
    if (submitting) return
    if (promptDraft.trim()) addPromptRow()
    if (!validateForm()) return
    const key = requireApiKey()
    if (!key) return
    setSubmitting(true)
    try {
      const job = await submitBatchImageJob(
        key.key,
        {
          model: form.model,
          task_name: form.taskName.trim() || defaultTaskName(),
          image_size: '1K',
          response_mime_type: form.responseMimeType,
          items: parsedItems
        },
        `sub2api-ui-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
      )
      setCurrentJob(job)
      setSelectedBatchId(job.id)
      setSelectedBatchApiKeyId(key.id)
      setItems([])
      upsertJob(job)
      setShowCreateModal(false)
      resetCreateDraft()
      toast.success(batchImageText('submitted'))
      void loadItems()
      startPolling()
    } catch (error: unknown) {
      toast.error(batchImageErrorMessage(error, batchImageText('submitFailed')))
    } finally {
      setSubmitting(false)
    }
  }

  const cancelSelected = async () => {
    if (!currentJob) return
    const key = keyForSelectedBatch() || requireApiKey()
    if (!key) return
    if (!window.confirm(batchImageText('cancelConfirm'))) return
    setCancelling(true)
    try {
      const job = await cancelBatchImageJob(key.key, currentJob.id)
      setCurrentJob(job)
      upsertJob(job)
      toast.success(batchImageText('cancelled'))
    } catch (error: unknown) {
      toast.error(batchImageErrorMessage(error, batchImageText('cancelFailed')))
    } finally {
      setCancelling(false)
    }
  }

  const downloadJob = async (job: BatchImageJobRow | Pick<BatchImageJob, 'id'>) => {
    if (downloading) return
    closeMoreMenu()
    const key = apiKeyForJob(job) || requireApiKey()
    if (!key) return
    setDownloading(true)
    setDownloadingBatchId(job.id)
    try {
      const blob = await downloadBatchImageZip(key.key, job.id)
      saveBlob(blob, `${job.id}.zip`)
      markJobDownloaded(job.id)
    } catch (error: unknown) {
      toast.error(batchImageErrorMessage(error, batchImageText('downloadFailed')))
    } finally {
      setDownloading(false)
      setDownloadingBatchId('')
    }
  }

  const downloadSelectedJobs = async () => {
    if (bulkDownloading || selectedDownloadableRows.length === 0) return
    setBulkDownloading(true)
    try {
      for (const row of selectedDownloadableRows) {
        const key = apiKeyForJob(row)
        if (!key) continue
        setDownloading(true)
        setDownloadingBatchId(row.id)
        const blob = await downloadBatchImageZip(key.key, row.id)
        saveBlob(blob, `${row.id}.zip`)
        markJobDownloaded(row.id)
      }
      toast.success(batchImageText('batchDownloadStarted'))
    } catch (error: unknown) {
      toast.error(batchImageErrorMessage(error, batchImageText('downloadFailed')))
    } finally {
      setBulkDownloading(false)
      setDownloading(false)
      setDownloadingBatchId('')
    }
  }

  const deleteJob = async (job: BatchImageJobRow) => {
    if (!canDeleteRecord(job) || deletingBatchId) return
    closeMoreMenu()
    const key = apiKeyForJob(job)
    if (!key) return
    if (!window.confirm(batchImageText('deleteConfirm'))) return
    setDeletingBatchId(job.id)
    try {
      await deleteBatchImageJobRecord(key.key, job.id)
      removeJobFromList(job.id)
      toast.success(batchImageText('deleted'))
    } catch (error: unknown) {
      toast.error(batchImageErrorMessage(error, batchImageText('deleteFailed')))
    } finally {
      setDeletingBatchId('')
    }
  }

  const deleteSelectedJobs = async () => {
    const rows = selectedRows.filter((job) => canDeleteRecord(job))
    if (bulkDeleting || rows.length === 0) return
    if (!window.confirm(batchImageText('deleteSelectedConfirm'))) return
    setBulkDeleting(true)
    try {
      for (const row of rows) {
        const key = apiKeyForJob(row)
        if (!key) continue
        setDeletingBatchId(row.id)
        await deleteBatchImageJobRecord(key.key, row.id)
        removeJobFromList(row.id)
      }
      toast.success(batchImageText('deleted'))
    } catch (error: unknown) {
      toast.error(batchImageErrorMessage(error, batchImageText('deleteFailed')))
    } finally {
      setBulkDeleting(false)
      setDeletingBatchId('')
    }
  }

  const markJobDownloaded = (batchId: string) => {
    const downloadedAt = Math.floor(Date.now() / 1000)
    setBatchJobs((previous) =>
      previous.map((job) => (job.id === batchId ? { ...job, downloaded_at: job.downloaded_at || downloadedAt } : job))
    )
    setCurrentJob((previous) =>
      previous && previous.id === batchId && !previous.downloaded_at ? { ...previous, downloaded_at: downloadedAt } : previous
    )
  }

  const removeJobFromList = (batchId: string) => {
    setBatchJobs((previous) => previous.filter((job) => job.id !== batchId))
    toggleJobSelection(batchId, false)
    if (currentJob?.id === batchId) closeDetail()
  }

  const retryFailedJob = async (job: BatchImageJobRow | BatchImageJob) => {
    if (!canRetry(job) || retryingBatchId) return
    closeMoreMenu()
    const key = apiKeyForJob(job) || keyForSelectedBatch() || requireApiKey()
    if (!key) return
    setRetryingBatchId(job.id)
    try {
      const sourceItems =
        selectedBatchIdRef.current === job.id && items.length > 0 ? items : (await listBatchImageItems(key.key, job.id)).data || []
      const failedItems = sourceItems
        .filter((item) => item.status === 'failed')
        .map((item) => ({ custom_id: retryCustomID(item.custom_id), prompt: String(item.prompt_preview || '').trim() }))
        .filter((item) => item.prompt)
      if (failedItems.length === 0) {
        toast.error(batchImageText('retryMissingPrompts'))
        return
      }
      const retryJob = await submitBatchImageJob(
        key.key,
        {
          model: job.model,
          task_name: `${job.task_name || defaultTaskName()} ${t('batchImage.messages.retryTaskNameSuffix')}`,
          parent_batch_id: job.parent_batch_id || job.id,
          provider: job.provider,
          image_size: '1K',
          response_mime_type: form.responseMimeType,
          items: failedItems
        },
        `sub2api-ui-retry-${job.id}-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
      )
      setCurrentJob(retryJob)
      setSelectedBatchId(retryJob.id)
      setSelectedBatchApiKeyId(key.id)
      setItems([])
      upsertJob(retryJob)
      if (retryJob.parent_batch_id) {
        setExpandedParentIds((previous) => new Set([...previous, retryJob.parent_batch_id as string]))
      }
      toast.success(batchImageText('retrySubmitted'))
      void loadItems()
      startPolling()
    } catch (error: unknown) {
      toast.error(batchImageErrorMessage(error, batchImageText('retryFailed')))
    } finally {
      setRetryingBatchId('')
    }
  }

  const itemPreviewKey = (item: Pick<BatchImageItem, 'batch_id' | 'custom_id'>) =>
    previewCacheKey(
      item.batch_id || selectedBatchIdRef.current || currentJob?.id || '',
      item.custom_id,
      0
    )

  const loadItemPreview = async (item: BatchImageItem) => {
    const batchId = item.batch_id || selectedBatchIdRef.current || currentJob?.id || ''
    const previewKey = itemPreviewKey(item)
    if (!batchId || !canLoadItemPreview(item) || itemPreviewUrlsRef.current[previewKey]) return
    const key = keyForSelectedBatch() || requireApiKey()
    if (!key) return
    setPreviewLoadingIds((previous) => new Set([...previous, previewKey]))
    try {
      setPreviewErrorIds((previous) => new Set([...previous].filter((id) => id !== previewKey)))
      const blob = await getBatchImageItemContent(key.key, batchId, item.custom_id, 0)
      setItemPreviewUrls((previous) => {
        if (previous[previewKey]) URL.revokeObjectURL(previous[previewKey])
        return { ...previous, [previewKey]: URL.createObjectURL(blob) }
      })
    } catch (error: unknown) {
      setPreviewErrorIds((previous) => new Set([...previous, previewKey]))
      console.error('[BatchImageGuideView] load preview failed:', error)
    } finally {
      setPreviewLoadingIds((previous) => {
        const next = new Set([...previous])
        next.delete(previewKey)
        return next
      })
    }
  }

  const closeImagePreview = () => setPreviewImageItem(null)

  const openCreateModal = () => {
    setShowCreateModal(true)
    if (!apiKeys.length) void loadApiKeys()
  }

  const closeCreateModal = () => {
    if (submitting) return
    setShowCreateModal(false)
    resetCreateDraft()
  }

  const resetCreateDraft = () => {
    setForm((previous) => ({ ...previous, taskName: '', responseMimeType: 'image/png' }))
    setPromptRows([])
    setPromptDraft('')
    setCustomIdDraft('')
    setOutputCountDraft(1)
    setReferenceImageDrafts([])
  }

  useEffect(() => {
    void (async () => {
      await loadApiKeys()
      await loadBatchJobs()
    })()
    return () => stopPolling()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    void loadBatchJobs()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filters.apiKeyId, filters.status, filters.downloaded, pagination.page, pagination.page_size])

  useEffect(() => {
    if (selectedApiKey) void loadAvailableModels()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedApiKey])

  useEffect(() => {
    const objectUrls = Object.values(itemPreviewUrls)
    return () => objectUrls.forEach((url) => URL.revokeObjectURL(url))
  }, [itemPreviewUrls])

  const columns = useMemo<Column[]>(
    () => [
      { key: 'select', label: '', sortable: false, class: 'w-12 text-center' },
      { key: 'id', label: t('batchImage.columns.taskName'), sortable: false, class: 'w-[240px] max-w-[240px]' },
      { key: 'model', label: t('batchImage.columns.model'), sortable: false, class: 'w-[180px] max-w-[180px] text-center' },
      { key: 'api_key_name', label: t('batchImage.columns.apiKey'), sortable: false, class: 'w-40 max-w-40 text-center' },
      { key: 'status', label: t('common.status'), sortable: false, class: 'w-28 text-center' },
      { key: 'counts', label: t('batchImage.columns.result'), sortable: false, class: 'w-32 text-center' },
      { key: 'cost', label: t('batchImage.columns.cost'), sortable: false, class: 'w-36 text-center' },
      { key: 'downloaded', label: t('batchImage.columns.downloadStatus'), sortable: false, class: 'w-40 text-center' },
      { key: 'actions', label: t('common.actions'), sortable: false, class: 'w-40 text-center' }
    ],
    [t]
  )

  const renderCell = (key: string, row: BatchImageJobRow): React.ReactNode => {
    const display = displayJob(row)
    switch (key) {
      case 'select':
        return (
          <input
            type="checkbox"
            className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
            checked={selectedJobIds.has(row.id)}
            onChange={(event) => toggleJobSelection(row.id, event.target.checked)}
            onClick={(event) => event.stopPropagation()}
          />
        )
      case 'id':
        return (
          <div className={`flex w-full min-w-0 max-w-[220px] items-start gap-1 ${row.is_child ? 'pl-6' : ''}`}>
            {row.child_count > 0 && !row.is_child ? (
              <button
                type="button"
                className="surface-control surface-muted mt-1 flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-md transition-colors hover:surface-hover hover:text-gray-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/30 dark:hover:text-white"
                title={expandedParentIds.has(row.id) ? t('batchImage.list.collapseChildren') : t('batchImage.list.expandChildren', { n: row.child_count })}
                onClick={(event) => {
                  event.stopPropagation()
                  toggleChildRows(row.id)
                }}
              >
                <Icon name={expandedParentIds.has(row.id) ? 'chevronDown' : 'chevronRight'} size="xs" />
              </button>
            ) : (
              <span className="w-6 flex-shrink-0" />
            )}
            <button
              type="button"
              className="surface-control min-w-0 flex-1 rounded-lg py-1 text-left transition-colors hover:surface-hover focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/30"
              onClick={() => selectJob(row.id)}
            >
              <span
                className={`flex min-w-0 items-center gap-2 text-sm font-medium ${
                  row.task_name ? 'text-gray-900 dark:text-white' : 'surface-muted'
                }`}
              >
                <span className="min-w-0 truncate">{row.task_name || defaultTaskName(row.created_at)}</span>
                {row.child_count > 0 && !row.is_child && (
                  <span className="surface-inset surface-muted flex-shrink-0 rounded-full px-2 py-0.5 text-xs font-normal">
                    {t('batchImage.list.childCount', { n: row.child_count })}
                  </span>
                )}
                {row.is_child && (
                  <span className="flex-shrink-0 rounded-full bg-amber-50/[0.15] px-2 py-0.5 text-xs font-normal text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
                    {t('batchImage.list.childBadge')}
                  </span>
                )}
              </span>
              <span className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs surface-muted">
                <span>{formatDate(row.created_at)}</span>
              </span>
            </button>
          </div>
        )
      case 'model':
        return (
          <div className="mx-auto max-w-[180px] text-center">
            <p className="truncate text-sm surface-muted" title={row.model}>
              {row.model}
            </p>
          </div>
        )
      case 'api_key_name':
        return (
          <span className="block truncate text-center text-sm surface-muted">
            {row.api_key_name || t('batchImage.list.keyNotRecorded')}
          </span>
        )
      case 'status':
        return (
          <div className="flex justify-center">
            <span className={`badge ${statusBadgeClass(display)}`}>{statusLabel(display)}</span>
          </div>
        )
      case 'counts':
        return (
          <div className="flex items-center justify-center gap-2 text-sm tabular-nums">
            <span className="text-emerald-600 dark:text-emerald-300">{display.success_count}</span>
            <span className="text-gray-300 dark:text-dark-500">/</span>
            <span className={display.fail_count > 0 ? 'text-red-600 dark:text-red-300' : 'surface-muted'}>
              {display.fail_count}
            </span>
            <span className="text-xs surface-muted">{t('batchImage.list.totalCount', { n: display.item_count })}</span>
          </div>
        )
      case 'cost':
        return <span className="block text-center text-sm surface-muted">{costLabel(display)}</span>
      case 'downloaded':
        return (
          <span
            className={`block text-center text-sm ${row.downloaded_at ? 'text-emerald-700 dark:text-emerald-300' : 'surface-muted'}`}
          >
            {row.downloaded_at ? formatDate(row.downloaded_at) : t('batchImage.list.notDownloaded')}
          </span>
        )
      case 'actions':
        return (
          <div className="table-actions justify-center">
            <button type="button" className="table-action" title={t('batchImage.actions.viewDetail')} onClick={() => selectJob(row.id)}>
              <Icon name="eye" size="sm" />
              <span className="text-xs">{t('common.view')}</span>
            </button>
            <button
              type="button"
              className="table-action"
              disabled={!canDownload(row) || downloading}
              title={t('batchImage.actions.downloadZip')}
              onClick={() => void downloadJob(row)}
            >
              <Icon
                name={isDownloadingJob(row.id) ? 'refresh' : 'download'}
                size="sm"
                className={isDownloadingJob(row.id) ? 'animate-spin' : ''}
              />
              <span className="text-xs">{t('batchImage.actions.download')}</span>
            </button>
            {(canRetry(row) || canDeleteRecord(row)) && (
              <button
                type="button"
                className={`table-action table-action-neutral ${openMoreJobId === row.id ? 'table-action-active' : ''}`}
                title={t('batchImage.actions.moreActions')}
                onClick={(event) => {
                  event.stopPropagation()
                  toggleMoreMenu(row, event)
                }}
              >
                <Icon name="more" size="sm" />
                <span className="text-xs">{t('common.more')}</span>
              </button>
            )}
          </div>
        )
      default:
        return ''
    }
  }

  const isDownloadingJob = (batchId: string) => downloading && downloadingBatchId === batchId

  const toggleChildRows = (batchId: string) => {
    setExpandedParentIds((previous) => {
      const next = new Set(previous)
      if (next.has(batchId)) next.delete(batchId)
      else next.add(batchId)
      return next
    })
  }

  const moreMenuJob = batchJobs.find((job) => job.id === openMoreJobId) || null

  const currentDisplayJob = currentJob ? displayJob(currentJob) : null

  const detailRootBatchId = currentJob?.parent_batch_id || selectedBatchId || currentJob?.id || ''

  const recoveredOriginalCustomIds = useMemo(() => {
    if (!detailRootBatchId) return new Set<string>()
    const ids = new Set<string>()
    for (const item of items) {
      if (!isChildDetailItem(item) || !isSuccessfulImageItem(item)) continue
      const sourceCustomID = retrySourceCustomID(item.custom_id)
      if (sourceCustomID) ids.add(sourceCustomID)
    }
    return ids
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [items, detailRootBatchId])

  return (
    <div className="workspace-page">
      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-3 2xl:flex-row 2xl:items-center 2xl:justify-between">
          <div className="filter-bar w-full 2xl:w-auto">
            <div className="filter-field filter-field--search">
              <SearchInput
                value={filters.taskName}
                placeholder={t('batchImage.filters.searchTaskName')}
                onValueChange={(value) => setFilters((previous) => ({ ...previous, taskName: value }))}
                onSearch={applyFilters}
              />
            </div>
            <div className="filter-field">
              <Select
                value={filters.apiKeyId}
                options={apiKeyFilterOptions}
                searchable={false}
                onValueChange={(value) => {
                  setFilters((previous) => ({ ...previous, apiKeyId: value === null ? '' : String(value) }))
                  applyFilters()
                }}
              />
            </div>
            <div className="filter-field">
              <Select
                value={filters.status}
                options={statusFilterOptions}
                searchable={false}
                onValueChange={(value) => {
                  setFilters((previous) => ({ ...previous, status: value === null ? '' : String(value) }))
                  applyFilters()
                }}
              />
            </div>
            <div className="filter-field">
              <Select
                value={filters.downloaded}
                options={downloadFilterOptions}
                searchable={false}
                onValueChange={(value) => {
                  setFilters((previous) => ({ ...previous, downloaded: value === null ? '' : String(value) }))
                  applyFilters()
                }}
              />
            </div>
          </div>
          <div className="flex flex-wrap items-center justify-start gap-2 sm:justify-end 2xl:flex-shrink-0">
            <button type="button" className="btn btn-secondary" disabled={loadingJobs} onClick={resetFilters}>
              {t('common.reset')}
            </button>
            <button
              type="button"
              className="btn btn-secondary"
              disabled={loadingKeys || loadingJobs}
              title={t('common.refresh')}
              onClick={() => void refreshPage()}
            >
              <Icon name="refresh" size="md" className={loadingKeys || loadingJobs ? 'animate-spin' : ''} />
            </button>
            <button type="button" className="btn btn-secondary" onClick={() => setShowGuideModal(true)}>
              <Icon name="book" size="md" className="mr-2" />
              {t('batchImage.actions.usageGuide')}
            </button>
            <button type="button" className="btn btn-primary" onClick={openCreateModal}>
              <Icon name="plus" size="md" className="mr-2" />
              {t('batchImage.actions.createJob')}
            </button>
          </div>
        </div>

        {selectedJobIds.size > 0 && (
          <div className="surface-inset flex flex-wrap items-center justify-between gap-3 rounded-lg border border-gray-200 px-3 py-2 shadow-sm dark:border-dark-700">
            <span className="text-sm surface-muted">
              {t('batchImage.list.selectedJobs', { count: selectedJobIds.size })}
            </span>
            <div className="flex flex-wrap items-center gap-2">
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                disabled={bulkDownloading || selectedDownloadableRows.length === 0}
                onClick={() => void downloadSelectedJobs()}
              >
                <Icon
                  name={bulkDownloading ? 'refresh' : 'download'}
                  size="sm"
                  className={`mr-1.5 ${bulkDownloading ? 'animate-spin' : ''}`}
                />
                {t('batchImage.actions.downloadSelected')}
              </button>
              <button
                type="button"
                className="btn btn-secondary btn-sm text-red-600 hover:text-red-700 dark:text-red-400"
                disabled={bulkDeleting}
                onClick={() => void deleteSelectedJobs()}
              >
                <Icon
                  name={bulkDeleting ? 'refresh' : 'trash'}
                  size="sm"
                  className={`mr-1.5 ${bulkDeleting ? 'animate-spin' : ''}`}
                />
                {t('batchImage.actions.deleteRecords')}
              </button>
            </div>
          </div>
        )}
      </div>

      <div className="card workspace-table-card workspace-shell">
        <DataTable
          className="workspace-table-viewport"
          columns={columns}
          data={visibleBatchJobs}
          loading={loadingKeys || loadingJobs}
          expandableActions={false}
          rowKey="id"
          selectable={false}
          renderCell={(key, row) => renderCell(key, row as BatchImageJobRow)}
          renderHeader={(key) =>
            key === 'select' ? (
              <input
                type="checkbox"
                className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                checked={allVisibleSelected}
                ref={(node) => {
                  if (node) node.indeterminate = someVisibleSelected
                }}
                onChange={(event) => toggleAllVisible(event.target.checked)}
              />
            ) : undefined
          }
          renderEmpty={() => (
            <div className="flex min-h-[260px] flex-col items-center justify-center py-6 md:min-h-[300px]">
              <Icon name="sparkles" size="xl" className="mb-4 h-12 w-12 surface-muted" />
              <p className="text-lg font-medium text-gray-900 dark:text-gray-100">{t('batchImage.list.empty')}</p>
              <p className="mt-1 text-sm surface-muted">{t('batchImage.list.emptyHint')}</p>
            </div>
          )}
        />
      </div>

      {visibleBatchJobs.length > 0 || pagination.page > 1 ? (
        <div className="batch-pagination pagination-card card flex flex-col gap-3 px-4 py-3 sm:px-6">
          <div className="flex flex-wrap items-center gap-3 text-sm surface-muted">
            <span>{t('batchImage.pagination.pageNumber', { page: pagination.page })}</span>
            <span>{t('batchImage.pagination.pageItems', { count: visibleBatchJobs.length })}</span>
            <div className="flex items-center gap-2">
              <span>{t('pagination.perPage')}</span>
              <Select
                value={pagination.page_size}
                options={batchPageSizeOptions}
                className="w-24"
                searchable={false}
                onValueChange={handlePageSizeChange}
              />
            </div>
          </div>
          <div className="flex items-center justify-end gap-2">
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              disabled={pagination.page <= 1 || loadingJobs}
              onClick={() => handlePageChange(pagination.page - 1)}
            >
              <Icon name="chevronLeft" size="sm" className="mr-1" />
              {t('pagination.previous')}
            </button>
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              disabled={!pagination.has_more || loadingJobs}
              onClick={() => handlePageChange(pagination.page + 1)}
            >
              {t('pagination.next')}
              <Icon name="chevronRight" size="sm" className="ml-1" />
            </button>
          </div>
        </div>
      ) : null}

      {openMoreJobId && moreMenuJob && (
        <div
          className="surface-panel fixed z-[9999] w-44 overflow-hidden p-1 text-sm shadow-lg ring-1 ring-black/5 dark:ring-white/10"
          style={moreMenuStyle}
          onClick={(event) => event.stopPropagation()}
        >
          {canRetry(moreMenuJob) && (
            <button
              type="button"
              className="surface-control surface-muted flex w-full items-center gap-2 rounded-[var(--surface-radius-item)] px-3 py-2 text-left transition-colors hover:bg-amber-50/25 hover:text-amber-700 disabled:opacity-60 dark:hover:bg-amber-900/20 dark:hover:text-amber-300"
              disabled={retryingBatchId === moreMenuJob.id}
              onClick={() => void retryFailedJob(moreMenuJob)}
            >
              <Icon name="refresh" size="sm" className={retryingBatchId === moreMenuJob.id ? 'animate-spin' : ''} />
              {t('batchImage.actions.retryFailedItems')}
            </button>
          )}
          {canDeleteRecord(moreMenuJob) && (
            <button
              type="button"
              className="surface-control flex w-full items-center gap-2 rounded-[var(--surface-radius-item)] px-3 py-2 text-left text-red-600 transition-colors hover:bg-red-50/25 disabled:opacity-60 dark:text-red-400 dark:hover:bg-red-900/20"
              disabled={deletingBatchId === moreMenuJob.id}
              onClick={() => void deleteJob(moreMenuJob)}
            >
              <Icon
                name={deletingBatchId === moreMenuJob.id ? 'refresh' : 'trash'}
                size="sm"
                className={deletingBatchId === moreMenuJob.id ? 'animate-spin' : ''}
              />
              {t('batchImage.actions.deleteRecords')}
            </button>
          )}
        </div>
      )}

      {/* Job detail */}
      <Modal
        open={!!currentJob}
        onClose={closeDetail}
        title={t('batchImage.detail.title')}
        size="xl"
        footer={
          <div className="flex flex-wrap justify-end gap-3">
            <button
              type="button"
              className="btn btn-secondary"
              disabled={!currentJob || !canCancel(currentJob) || cancelling}
              onClick={() => void cancelSelected()}
            >
              {cancelling && <Icon name="refresh" size="sm" className="mr-2 animate-spin" />}
              {t('batchImage.actions.cancelJob')}
            </button>
            {currentJob && currentDisplayJob && canRetry(currentDisplayJob) && (
              <button
                type="button"
                className="btn btn-secondary inline-flex min-w-[116px] items-center justify-center"
                disabled={retryingBatchId === currentJob.id}
                onClick={() => void retryFailedJob(currentJob)}
              >
                <Icon
                  name="refresh"
                  size="sm"
                  className={`mr-2 ${retryingBatchId === currentJob.id ? 'animate-spin' : ''}`}
                />
                {t('batchImage.actions.retryFailedItems')}
              </button>
            )}
            <button
              type="button"
              className="btn btn-primary inline-flex min-w-[112px] items-center justify-center"
              disabled={!currentJob || !canDownload(currentJob) || downloading}
              onClick={() => void (currentJob ? downloadJob(currentJob) : undefined)}
            >
              <Icon
                name={currentJob && isDownloadingJob(currentJob.id) ? 'refresh' : 'download'}
                size="sm"
                className={`mr-2 ${currentJob && isDownloadingJob(currentJob.id) ? 'animate-spin' : ''}`}
              />
              {t('batchImage.actions.downloadZip')}
            </button>
          </div>
        }
      >
        {currentJob && (
          <div className="space-y-4">
            <div className="surface-inset rounded-lg border border-gray-200 px-4 py-3 dark:border-dark-700">
              <div className="grid gap-x-6 gap-y-3 sm:grid-cols-2 lg:grid-cols-4">
                <div className="min-w-0 text-center">
                  <p className="text-xs surface-muted">{t('common.status')}</p>
                  <div className="mt-1 flex justify-center">
                    <span className={`badge whitespace-nowrap ${statusBadgeClass(currentDisplayJob || currentJob)}`}>
                      {statusLabel(currentDisplayJob || currentJob)}
                    </span>
                  </div>
                </div>
                <div className="min-w-0 text-center">
                  <p className="text-xs surface-muted">
                    {hasChildJobs(currentJob.id) ? t('batchImage.detail.aggregatedResult') : t('batchImage.detail.result')}
                  </p>
                  <p className="mt-1 flex items-center justify-center gap-2 font-medium tabular-nums">
                    <span className="text-emerald-600 dark:text-emerald-300">
                      {(currentDisplayJob || currentJob).success_count}
                    </span>
                    <span className="text-gray-300 dark:text-dark-500">/</span>
                    <span
                      className={
                        (currentDisplayJob || currentJob).fail_count > 0 ? 'text-red-600 dark:text-red-300' : 'surface-muted'
                      }
                    >
                      {(currentDisplayJob || currentJob).fail_count}
                    </span>
                  </p>
                </div>
                <div className="min-w-0 text-center">
                  <p className="text-xs surface-muted">{t('batchImage.detail.cost')}</p>
                  <p className="mt-1 truncate font-medium text-gray-900 dark:text-white">
                    {costLabel(currentDisplayJob || currentJob)}
                  </p>
                </div>
                <div className="min-w-0 text-center">
                  <p className="text-xs surface-muted">{t('batchImage.detail.downloadStatus')}</p>
                  <p className="mt-1 truncate font-medium text-gray-900 dark:text-white">
                    {currentJob.downloaded_at ? formatDate(currentJob.downloaded_at) : t('batchImage.list.notDownloaded')}
                  </p>
                </div>
              </div>
            </div>

            <div className="flex flex-wrap items-center justify-between gap-3">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white">{t('batchImage.detail.items')}</h3>
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                disabled={refreshing || loadingItems}
                onClick={() => void refreshDetail()}
              >
                <Icon
                  name="refresh"
                  size="sm"
                  className={`mr-1.5 ${refreshing || loadingItems ? 'animate-spin' : ''}`}
                />
                {t('common.refresh')}
              </button>
            </div>

            {items.length > 0 ? (
              <div className="record-table-scroll surface-inset overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
                <table className="record-table w-full min-w-[860px] table-fixed divide-y divide-gray-200 text-sm dark:divide-dark-700">
                  <colgroup>
                    <col className="w-[18%]" />
                    <col className="w-[34%]" />
                    <col className="w-[12%]" />
                    <col className="w-[10%]" />
                    <col className="w-[26%]" />
                  </colgroup>
                  <thead className="surface-inset">
                    <tr>
                      <th className="px-3 py-3 text-center text-sm font-medium surface-muted">
                        {t('batchImage.detail.customId')}
                      </th>
                      <th className="px-3 py-3 text-left text-sm font-medium surface-muted">
                        {t('batchImage.detail.prompt')}
                      </th>
                      <th className="px-3 py-3 text-center text-sm font-medium surface-muted">{t('common.status')}</th>
                      <th className="px-3 py-3 text-center text-sm font-medium surface-muted">
                        {t('batchImage.detail.preview')}
                      </th>
                      <th className="px-3 py-3 text-center text-sm font-medium surface-muted">
                        {t('batchImage.detail.result')}
                      </th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-gray-100 dark:divide-dark-700">
                    {items.map((item) => {
                      const previewKey = itemPreviewKey(item)
                      const recovered = isRecoveredOriginalFailure(item)
                      return (
                        <tr
                          key={previewKey}
                          className={`align-middle ${
                            recovered ? 'surface-inset surface-muted hover:surface-hover' : 'hover:surface-hover'
                          }`}
                        >
                          <td className="px-3 py-2.5 text-center">
                            <span
                              className={`block min-w-0 truncate font-mono text-sm ${
                                recovered ? 'surface-muted' : 'text-gray-900 dark:text-white'
                              }`}
                              title={item.custom_id}
                            >
                              {item.custom_id}
                            </span>
                          </td>
                          <td className="px-3 py-2.5 text-left surface-muted">
                            <div className="cursor-default truncate rounded px-1 text-sm leading-6" title={item.prompt_preview || '-'}>
                              {item.prompt_preview || '-'}
                            </div>
                          </td>
                          <td className="px-3 py-2.5 text-center">
                            <span
                              className={`badge max-w-full truncate whitespace-nowrap ${itemDisplayStatusBadgeClass(item)}`}
                              title={itemDisplayStatusLabel(item)}
                            >
                              {itemDisplayStatusLabel(item)}
                            </span>
                          </td>
                          <td className="px-3 py-2.5 text-center">
                            <div className="surface-inset mx-auto h-12 w-12 overflow-hidden rounded-md border border-gray-200 dark:border-dark-700">
                              {itemPreviewUrls[previewKey] && !previewErrorIds.has(previewKey) ? (
                                <button
                                  type="button"
                                  className="surface-control block h-full w-full overflow-hidden"
                                  title={t('batchImage.detail.previewZoom', { id: item.custom_id })}
                                  onClick={() => setPreviewImageItem(item)}
                                >
                                  <img src={itemPreviewUrls[previewKey]} className="h-full w-full object-cover" alt="" />
                                </button>
                              ) : canLoadItemPreview(item) ? (
                                <button
                                  type="button"
                                  className="surface-control surface-muted flex h-full w-full items-center justify-center transition-colors hover:surface-hover hover:text-primary-600 disabled:cursor-wait disabled:opacity-70"
                                  disabled={previewLoadingIds.has(previewKey)}
                                  title={
                                    previewErrorIds.has(previewKey)
                                      ? t('batchImage.detail.previewReload')
                                      : t('batchImage.detail.previewLoad')
                                  }
                                  onClick={() => void loadItemPreview(item)}
                                >
                                  <Icon
                                    name={previewLoadingIds.has(previewKey) ? 'refresh' : 'eye'}
                                    size="sm"
                                    className={previewLoadingIds.has(previewKey) ? 'animate-spin' : ''}
                                  />
                                </button>
                              ) : (
                                <div
                                  className="surface-muted flex h-full w-full items-center justify-center"
                                  title={
                                    item.image_count > 0 ? t('batchImage.detail.previewUnavailable') : t('batchImage.detail.noImage')
                                  }
                                >
                                  <Icon name="document" size="sm" />
                                </div>
                              )}
                            </div>
                          </td>
                          <td className="px-3 py-2.5 text-center">
                            <span
                              className={`inline-flex max-w-full items-center justify-center truncate rounded-md px-2.5 py-1 text-xs font-medium leading-5 ring-1 ring-inset ${itemResultClass(item)}`}
                              title={itemResultLabel(item)}
                            >
                              {itemResultLabel(item)}
                            </span>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="rounded-lg border border-dashed border-gray-200 py-10 text-center dark:border-dark-700">
                <Icon
                  name="refresh"
                  size="lg"
                  className={`mx-auto mb-3 text-gray-400 ${loadingItems ? 'animate-spin' : ''}`}
                />
                <p className="text-sm font-medium surface-muted">
                  {loadingItems ? t('batchImage.detail.loadingItems') : t('batchImage.detail.noItems')}
                </p>
                {!loadingItems && (
                  <p className="mt-1 text-sm surface-muted">{t('batchImage.detail.noItemsHint')}</p>
                )}
              </div>
            )}
          </div>
        )}
      </Modal>

      {/* Full image preview */}
      <Modal
        open={!!previewImageItem}
        onClose={closeImagePreview}
        title={previewImageItem?.custom_id || t('batchImage.imagePreview.title')}
        size="xl"
      >
        <div className="space-y-3">
          <div className="rounded-lg border border-amber-200 bg-amber-50/[0.15] px-3 py-2 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-100">
            {t('batchImage.imagePreview.notice')}
          </div>
          <div className="surface-inset flex min-h-[420px] items-center justify-center rounded-lg p-4">
            {previewImageItem && itemPreviewUrls[itemPreviewKey(previewImageItem)] && (
              <img
                src={itemPreviewUrls[itemPreviewKey(previewImageItem)]}
                className="max-h-[70vh] max-w-full rounded-md object-contain"
                alt={previewImageItem.custom_id || ''}
              />
            )}
          </div>
        </div>
      </Modal>

      {/* Create job */}
      <Modal open={showCreateModal} onClose={closeCreateModal} title={t('batchImage.create.title')} size="md"
        footer={
          <div className="flex flex-wrap justify-end gap-3">
            <button type="button" className="btn btn-secondary" disabled={submitting} onClick={closeCreateModal}>
              {t('common.cancel')}
            </button>
            <button
              type="button"
              className="btn btn-primary inline-flex min-w-[120px] justify-center"
              disabled={submitting || loadingModels || (parsedItems.length === 0 && !promptDraft.trim()) || !selectedApiKey || !form.model}
              onClick={() => void submitJob()}
            >
              {submitting && <Icon name="refresh" size="sm" className="mr-2 animate-spin" />}
              {submitting ? t('common.submitting') : t('batchImage.actions.submitJob')}
            </button>
          </div>
        }
      >
        <form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault()
            void submitJob()
          }}
        >
          <div className="grid gap-4 md:grid-cols-2">
            <div className="md:col-span-2">
              <label className="input-label">{t('batchImage.create.taskName')}</label>
              <input
                value={form.taskName}
                onChange={(event) => setForm((previous) => ({ ...previous, taskName: event.target.value }))}
                type="text"
                maxLength={255}
                className="input"
                placeholder={t('batchImage.create.taskNamePlaceholder')}
              />
            </div>

            <div className="md:col-span-2">
              <label className="input-label">{t('batchImage.create.apiKey')}</label>
              <Select
                value={form.apiKeyId}
                disabled={loadingKeys}
                searchable={false}
                options={[
                  {
                    value: 0,
                    label: loadingKeys ? t('batchImage.create.loadingKeys') : t('batchImage.create.selectKeyPlaceholder')
                  },
                  ...geminiApiKeys.map((key) => ({ value: key.id, label: `${key.name} · ${key.group?.name || 'Gemini'}` }))
                ]}
                onValueChange={(value) => {
                  setForm((previous) => ({ ...previous, apiKeyId: Number(value) }))
                }}
              />
              {!loadingKeys && geminiApiKeys.length === 0 && (
                <p className="input-hint text-amber-600 dark:text-amber-400">{t('batchImage.create.noKeysHint')}</p>
              )}
            </div>

            <div>
              <label className="input-label">{t('batchImage.create.model')}</label>
              <Select
                value={form.model}
                disabled={loadingModels || availableBatchImageModels.length === 0}
                searchable={false}
                options={[
                  ...(loadingModels ? [{ value: '', label: batchImageText('loadingModels') }] : []),
                  ...(!loadingModels && availableBatchImageModels.length === 0
                    ? [{ value: '', label: batchImageText('noModels') }]
                    : []),
                  ...availableBatchImageModels.map((model) => ({ value: model.value, label: model.label }))
                ]}
                onValueChange={(value) => setForm((previous) => ({ ...previous, model: String(value) }))}
              />
              {modelLoadError ? (
                <p className="input-hint text-amber-600 dark:text-amber-400">{modelLoadError}</p>
              ) : selectedApiKey && !loadingModels && availableBatchImageModels.length === 0 ? (
                <p className="input-hint text-amber-600 dark:text-amber-400">{batchImageText('noModelsHint')}</p>
              ) : null}
            </div>

            <div>
              <label className="input-label">{t('batchImage.create.imageSize')}</label>
              <div className="input surface-inset surface-muted flex items-center">1K</div>
              <p className="input-hint">{t('batchImage.create.imageSizeHint')}</p>
            </div>

            <div>
              <label className="input-label">{t('batchImage.create.outputFormat')}</label>
              <Select
                value={form.responseMimeType}
                searchable={false}
                options={[
                  { value: 'image/png', label: 'PNG' },
                  { value: 'image/jpeg', label: 'JPEG' },
                  { value: 'image/webp', label: 'WebP' }
                ]}
                onValueChange={(value) => setForm((previous) => ({ ...previous, responseMimeType: String(value) }))}
              />
            </div>

            <div>
              <label className="input-label">{t('batchImage.create.estimatedOutput')}</label>
              <div className="input surface-inset surface-muted flex items-center">
                {t('batchImage.create.estimatedOutputValue', { images: estimatedOutputCount, prompts: promptRows.length })}
              </div>
            </div>
          </div>

          <div className="space-y-3">
            <div className="flex items-center justify-between gap-3">
              <label className="input-label mb-0">{t('batchImage.detail.prompt')}</label>
              <span className="text-xs surface-muted">{t('batchImage.create.promptAdded', { count: promptRows.length })}</span>
            </div>
            <div className="rounded-lg border border-gray-200 p-3 dark:border-dark-700">
              <textarea
                value={promptDraft}
                onChange={(event) => setPromptDraft(event.target.value)}
                rows={3}
                className="h-[76px] w-full resize-y rounded-md border border-gray-300 px-3 py-2 text-sm leading-5 outline-none focus:border-primary-500 focus:ring-2 focus:ring-primary-100 dark:border-dark-600 dark:surface-inset dark:text-gray-100 dark:focus:ring-primary-900/40"
                placeholder={t('batchImage.create.promptPlaceholder')}
              />
              <div className="mt-2 grid gap-2 md:grid-cols-[minmax(0,1fr)_132px_132px_112px] md:items-center">
                <input
                  value={customIdDraft}
                  onChange={(event) => setCustomIdDraft(event.target.value)}
                  type="text"
                  maxLength={255}
                  className="input h-9 text-sm"
                  placeholder={t('batchImage.create.customIdPlaceholder')}
                />
                <Select
                  value={outputCountDraft}
                  searchable={false}
                  options={outputCountOptions.map((count) => ({
                    value: count,
                    label: t('batchImage.create.outputCountOption', { n: count })
                  }))}
                  onValueChange={(value) => setOutputCountDraft(Number(value))}
                />
                <label
                  className={`btn btn-secondary h-9 cursor-pointer justify-center text-sm ${
                    referenceImageDrafts.length >= selectedModelReferenceLimit(form.model)
                      ? 'pointer-events-none opacity-60'
                      : ''
                  }`}
                >
                  <Icon name="upload" size="sm" className="mr-1.5" />
                  {t('batchImage.create.referenceImage')}
                  <input
                    type="file"
                    accept="image/png,image/jpeg,image/webp"
                    multiple
                    className="hidden"
                    disabled={referenceImageDrafts.length >= selectedModelReferenceLimit(form.model)}
                    onChange={(event) => void handleReferenceImageFiles(event)}
                  />
                </label>
                <button
                  type="button"
                  className="btn btn-secondary h-9 justify-center whitespace-nowrap px-4 text-sm"
                  disabled={!promptDraft.trim()}
                  onClick={addPromptRow}
                >
                  <Icon name="plus" size="sm" className="mr-1.5" />
                  {t('common.add')}
                </button>
              </div>
              {referenceImageDrafts.length > 0 && (
                <div className="mt-3 flex flex-wrap gap-2">
                  {referenceImageDrafts.map((ref, refIndex) => (
                    <span
                      key={`${ref.name}-${refIndex}`}
                      className="surface-inset surface-muted inline-flex max-w-full items-center gap-1 rounded-md border border-gray-200 px-2 py-1 text-xs dark:border-dark-700"
                    >
                      <span className="max-w-[180px] truncate">{ref.name}</span>
                      <button
                        type="button"
                        className="surface-control surface-muted hover:text-red-600"
                        title={t('batchImage.create.removeReferenceImage')}
                        onClick={() => removeReferenceImageDraft(refIndex)}
                      >
                        <Icon name="x" size="xs" />
                      </button>
                    </span>
                  ))}
                </div>
              )}
              <p className="mt-2 text-xs surface-muted">
                {t('batchImage.create.limitsHint', {
                  maxPerItem: BATCH_IMAGE_MAX_OUTPUTS_PER_ITEM,
                  maxPerJob: BATCH_IMAGE_MAX_OUTPUTS_PER_JOB,
                  refLimit: selectedModelReferenceLimit(form.model)
                })}
              </p>
            </div>
            {promptRows.length > 0 ? (
              <div className="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-700">
                {promptRows.map((row, index) => (
                  <div
                    key={row.localId}
                    className="flex items-center gap-3 border-b border-gray-100 px-3 py-2 last:border-b-0 dark:border-dark-700"
                  >
                    <span className="w-20 flex-shrink-0 font-mono text-xs surface-muted">{row.custom_id}</span>
                    <p className="min-w-0 flex-1 truncate text-sm text-gray-800 dark:text-gray-100">{row.prompt}</p>
                    {row.output_count > 1 && <span className="flex-shrink-0 text-xs surface-muted">x{row.output_count}</span>}
                    {row.reference_images.length > 0 && (
                      <span className="flex-shrink-0 text-xs surface-muted">
                        {t('batchImage.create.referenceCount', { n: row.reference_images.length })}
                      </span>
                    )}
                    <button
                      type="button"
                      className="btn-ghost btn-icon surface-control flex-shrink-0 text-red-600 hover:bg-red-50/25 dark:text-red-400 dark:hover:bg-red-900/20"
                      title={t('common.delete')}
                      onClick={() => removePromptRow(index)}
                    >
                      <Icon name="trash" size="sm" />
                    </button>
                  </div>
                ))}
              </div>
            ) : (
              <div className="surface-muted rounded-lg border border-dashed border-gray-200 px-3 py-6 text-center text-sm dark:border-dark-700">
                {t('batchImage.create.noPrompts')}
              </div>
            )}
          </div>

          <div className="rounded-lg border border-amber-200 bg-amber-50/[0.15] p-3 text-sm leading-6 text-amber-900 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-100">
            {t('batchImage.create.cancelNotice')}
          </div>
          {submitting && (
            <div className="rounded-lg border border-sky-200 bg-sky-50/[0.15] p-3 text-sm leading-6 text-sky-800 dark:border-sky-800 dark:bg-sky-950/30 dark:text-sky-100">
              {t('batchImage.create.submittingNotice')}
            </div>
          )}
        </form>
      </Modal>

      {/* Usage guide */}
      <Modal
        open={showGuideModal}
        onClose={() => setShowGuideModal(false)}
        title={t('batchImage.guide.title')}
        size="md"
        footer={
          <div className="flex flex-wrap justify-end gap-3">
            <button type="button" className="btn btn-secondary" onClick={() => setShowGuideModal(false)}>
              {t('common.close')}
            </button>
            <button
              type="button"
              className="btn btn-primary"
              onClick={() => void copyToClipboard(agentInstruction, batchImageText('copiedInstruction'))}
            >
              <Icon name="copy" size="sm" className="mr-2" />
              {t('batchImage.actions.copyInstruction')}
            </button>
          </div>
        }
      >
        <div className="space-y-5">
          <section className="space-y-3">
            <h3 className="text-sm font-semibold text-gray-900 dark:text-white">{t('batchImage.guide.uiTitle')}</h3>
            <div className="surface-inset surface-muted rounded-lg border border-gray-200 p-3 text-sm leading-6 dark:border-dark-700">
              <p>{t('batchImage.guide.step1')}</p>
              <p>{t('batchImage.guide.step2')}</p>
              <p>{t('batchImage.guide.step3')}</p>
              <p>{t('batchImage.guide.step4')}</p>
            </div>
          </section>
          <section className="space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white">{t('batchImage.guide.skillTitle')}</h3>
              <p className="text-xs surface-muted">{t('batchImage.guide.skillDesc')}</p>
            </div>
            <textarea
              readOnly
              value={agentInstruction}
              className="surface-inset min-h-[420px] w-full resize-y rounded-md border border-gray-200 p-4 font-mono text-sm leading-6 text-gray-800 outline-none focus:border-primary-400 focus:ring-2 focus:ring-primary-100 dark:border-dark-600 dark:text-gray-100 dark:focus:border-primary-500 dark:focus:ring-primary-900/40"
            />
          </section>
        </div>
      </Modal>
    </div>
  )
}

function joinEndpointPath(base: string, path: string): string {
  return `${base.replace(/\/+$/, '')}/${path.replace(/^\/+/, '')}`
}

function previewCacheKey(batchId: string, customID: string, imageIndex = 0): string {
  return [batchId, customID, imageIndex].map((part) => encodeURIComponent(String(part))).join(':')
}

function uniqueCustomID(raw: string, used: Set<string>, index: number): string {
  const base = raw.replace(/[^\w.-]+/g, '_').replace(/^_+|_+$/g, '') || `img_${String(index + 1).padStart(3, '0')}`
  let candidate = base
  let suffix = 2
  while (used.has(candidate)) {
    candidate = `${base}_${suffix}`
    suffix += 1
  }
  used.add(candidate)
  return candidate
}

function normalizeOutputCount(value: unknown): number {
  const parsed = Math.floor(Number(value || 1))
  if (!Number.isFinite(parsed)) return 1
  return Math.min(BATCH_IMAGE_MAX_OUTPUTS_PER_ITEM, Math.max(1, parsed))
}

function retryCustomID(customID: string): string {
  const base = String(customID || 'item').replace(/[^\w.-]+/g, '_').replace(/^_+|_+$/g, '') || 'item'
  return `${base}_retry_${Date.now().toString(36)}`
}

function retrySourceCustomID(customID: string): string {
  return String(customID || '').replace(/(?:_retry_[a-z0-9]+)+$/i, '')
}

function canLoadItemPreview(item: BatchImageItem): boolean {
  return (item.status === 'succeeded' || item.status === 'success') && item.image_count > 0
}

function isSuccessfulImageItem(item: Pick<BatchImageItem, 'status' | 'image_count'>): boolean {
  return (item.status === 'succeeded' || item.status === 'success') && item.image_count > 0
}
