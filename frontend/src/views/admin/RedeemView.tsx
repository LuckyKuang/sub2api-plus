import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Modal, Pagination, Select, TextArea } from '@/components/ui'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { formatDateTime, getBrowserTimeZone, parseDateTimeLocalInput } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'
import type {
  BatchUpdateRedeemCodeFields,
  Group,
  GroupPlatform,
  RedeemCode,
  RedeemCodeType,
  SubscriptionPlan,
  SubscriptionType
} from '@/types'
import { AdminDataTable, type Column, type SortOrder } from '@/views/admin/AdminDataTable'
import { AdminTablePageLayout } from '@/views/admin/AdminTablePageLayout'
import { ConfirmDialog, getPersistedPageSize } from '@/views/admin/shared'
import { Icon } from '@/views/admin/AccountIcon'
import { DateTimePicker } from './parts/DateTimePicker'
import { GroupBadge } from './GroupBadge'
import { GroupOptionItem } from './GroupOptionItem'

type RedeemCodeExpiryOption = 'never' | '1' | '3' | '7' | 'custom'

interface GroupOption {
  value: number
  label: string
  description: string | null
  platform: GroupPlatform
  subscriptionType: SubscriptionType
  rate: number
}

interface GenerateFormState {
  type: RedeemCodeType
  generate_order: boolean
  order_currency: string
  order_plan_id: number | null
  value: string
  count: string
  group_id: number | null
  validity_days: string
  expiry_option: RedeemCodeExpiryOption
  custom_expiry_days: string
}

const INITIAL_GENERATE_FORM: GenerateFormState = {
  type: 'balance',
  generate_order: false,
  order_currency: 'CNY',
  order_plan_id: null,
  value: '10',
  count: '1',
  group_id: null,
  validity_days: '30',
  expiry_option: 'never',
  custom_expiry_days: '7'
}

interface BatchUpdateFormState {
  update_status: boolean
  status: 'unused' | 'disabled'
  update_expires_at: boolean
  expires_mode: 'clear' | 'custom'
  expires_at_local: string
  update_notes: boolean
  notes: string
  update_group_id: boolean
  group_id: number | null
}

interface RedeemQueryOverrides {
  page?: number
  pageSize?: number
  type?: string
  status?: string
  search?: string
  sortKey?: string
  sortOrder?: SortOrder
}

function toDatetimeLocalInputValue(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

function resetBatchUpdateForm(): BatchUpdateFormState {
  return {
    update_status: false,
    status: 'disabled',
    update_expires_at: false,
    expires_mode: 'clear',
    expires_at_local: toDatetimeLocalInputValue(new Date(Date.now() + 24 * 60 * 60 * 1000)),
    update_notes: false,
    notes: '',
    update_group_id: false,
    group_id: null
  }
}

export default function RedeemView() {
  const { t } = useTranslation()
  const browserTimeZone = getBrowserTimeZone()
  const showSuccess = useAppStore((state) => state.showSuccess)
  const showError = useAppStore((state) => state.showError)
  const showInfo = useAppStore((state) => state.showInfo)
  const { copyToClipboard: clipboardCopy } = useClipboard()

  const [codes, setCodes] = useState<RedeemCode[]>([])
  const [loading, setLoading] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [typeFilter, setTypeFilter] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(getPersistedPageSize())
  const [total, setTotal] = useState(0)
  const [sortKey, setSortKey] = useState('id')
  const [sortOrder, setSortOrder] = useState<SortOrder>('desc')

  const [selectedCodeKeys, setSelectedCodeKeys] = useState<Array<string | number>>([])
  const selectedCodeIds = selectedCodeKeys.map((key) => Number(key)).filter((id) => Number.isFinite(id))
  const selectedCount = selectedCodeIds.length

  const [showGenerateDialog, setShowGenerateDialog] = useState(false)
  const [showResultDialog, setShowResultDialog] = useState(false)
  const [generatedCodes, setGeneratedCodes] = useState<RedeemCode[]>([])
  const [copiedAll, setCopiedAll] = useState(false)
  const [generateForm, setGenerateForm] = useState<GenerateFormState>(INITIAL_GENERATE_FORM)
  const [generating, setGenerating] = useState(false)

  const [subscriptionGroups, setSubscriptionGroups] = useState<Group[]>([])
  const [orderPlans, setOrderPlans] = useState<SubscriptionPlan[]>([])

  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [deletingCode, setDeletingCode] = useState<RedeemCode | null>(null)
  const [showDeleteUnusedDialog, setShowDeleteUnusedDialog] = useState(false)
  const [copiedCode, setCopiedCode] = useState<string | null>(null)

  const [showBatchUpdateDialog, setShowBatchUpdateDialog] = useState(false)
  const [batchUpdateForm, setBatchUpdateForm] = useState<BatchUpdateFormState>(resetBatchUpdateForm)
  const [batchUpdating, setBatchUpdating] = useState(false)

  const disposedRef = useRef(false)
  const abortControllerRef = useRef<AbortController | null>(null)
  const searchTimerRef = useRef<number | null>(null)
  const copiedTimerRef = useRef<number | null>(null)
  const copiedAllTimerRef = useRef<number | null>(null)

  useEffect(() => {
    disposedRef.current = false
    return () => {
      disposedRef.current = true
      if (searchTimerRef.current) window.clearTimeout(searchTimerRef.current)
      if (copiedTimerRef.current) window.clearTimeout(copiedTimerRef.current)
      if (copiedAllTimerRef.current) window.clearTimeout(copiedAllTimerRef.current)
      abortControllerRef.current?.abort()
    }
  }, [])

  // 订阅类型分组选项
  const subscriptionGroupOptions = useMemo<GroupOption[]>(
    () =>
      subscriptionGroups
        .filter((group) => group.subscription_type === 'subscription')
        .map((group) => ({
          value: group.id,
          label: group.name,
          description: group.description,
          platform: group.platform,
          subscriptionType: group.subscription_type,
          rate: group.rate_multiplier
        })),
    [subscriptionGroups]
  )

  const batchGroupOptions = useMemo(
    () => [{ value: null, label: t('admin.redeem.clearGroup') }, ...subscriptionGroupOptions],
    [subscriptionGroupOptions, t]
  )

  const generatedCodesText = generatedCodes.map((code) => code.code).join('\n')

  const textareaHeight = useMemo(() => {
    const lineCount = generatedCodes.length
    const lineHeight = 24 // approximate line height in px
    const padding = 24 // top + bottom padding
    const minHeight = 60
    const maxHeight = 240
    const calculatedHeight = Math.min(Math.max(lineCount * lineHeight + padding, minHeight), maxHeight)
    return `${calculatedHeight}px`
  }, [generatedCodes.length])

  const columns = useMemo<Column[]>(
    () => [
      { key: 'code', label: t('admin.redeem.columns.code') },
      { key: 'type', label: t('admin.redeem.columns.type'), sortable: true },
      { key: 'value', label: t('admin.redeem.columns.value'), sortable: true },
      { key: 'status', label: t('admin.redeem.columns.status'), sortable: true },
      { key: 'used_by', label: t('admin.redeem.columns.usedBy') },
      { key: 'used_at', label: t('admin.redeem.columns.usedAt'), sortable: true },
      { key: 'expires_at', label: t('admin.redeem.columns.expiresAt'), sortable: true },
      { key: 'actions', label: t('admin.redeem.columns.actions') }
    ],
    [t]
  )

  const typeOptions = useMemo(
    () => [
      { value: 'balance', label: t('admin.redeem.balance') },
      { value: 'concurrency', label: t('admin.redeem.concurrency') },
      { value: 'subscription', label: t('admin.redeem.subscription') },
      { value: 'invitation', label: t('admin.redeem.invitation') }
    ],
    [t]
  )

  const filterTypeOptions = useMemo(
    () => [
      { value: '', label: t('admin.redeem.allTypes') },
      { value: 'balance', label: t('admin.redeem.balance') },
      { value: 'concurrency', label: t('admin.redeem.concurrency') },
      { value: 'subscription', label: t('admin.redeem.subscription') },
      { value: 'invitation', label: t('admin.redeem.invitation') }
    ],
    [t]
  )

  const filterStatusOptions = useMemo(
    () => [
      { value: '', label: t('admin.redeem.allStatus') },
      { value: 'unused', label: t('admin.redeem.unused') },
      { value: 'used', label: t('admin.redeem.used') },
      { value: 'expired', label: t('admin.redeem.status.expired') },
      { value: 'disabled', label: t('admin.redeem.status.disabled') }
    ],
    [t]
  )

  const batchStatusOptions = useMemo(
    () => [
      { value: 'unused', label: t('admin.redeem.status.unused') },
      { value: 'disabled', label: t('admin.redeem.status.disabled') }
    ],
    [t]
  )

  const batchExpiryModeOptions = useMemo(
    () => [
      { value: 'clear', label: t('admin.redeem.neverExpires') },
      { value: 'custom', label: t('admin.redeem.customExpiry') }
    ],
    [t]
  )

  const redeemCodeExpiryOptions = useMemo<{ value: RedeemCodeExpiryOption; label: string }[]>(
    () => [
      { value: 'never', label: t('admin.redeem.neverExpires') },
      { value: '1', label: t('admin.redeem.expiryPresetDays', { days: 1 }) },
      { value: '3', label: t('admin.redeem.expiryPresetDays', { days: 3 }) },
      { value: '7', label: t('admin.redeem.expiryPresetDays', { days: 7 }) },
      { value: 'custom', label: t('admin.redeem.customExpiry') }
    ],
    [t]
  )

  const orderPlanOptions = useMemo(
    () =>
      orderPlans
        .filter((plan) => !plan.currency || ['CNY', 'USD'].includes(plan.currency.toUpperCase()))
        .map((plan) => ({ value: plan.id, label: `${plan.name} · ${plan.price} ${plan.currency || 'CNY'}` })),
    [orderPlans]
  )

  const selectedOrderPlan = orderPlans.find((plan) => plan.id === Number(generateForm.order_plan_id))

  const buildRedeemQueryFilters = (overrides: RedeemQueryOverrides = {}) => {
    const type = overrides.type ?? typeFilter
    const status = overrides.status ?? statusFilter
    const search = overrides.search ?? searchQuery
    return {
      type: (type || undefined) as RedeemCodeType | undefined,
      status: (status || undefined) as 'used' | 'expired' | 'unused' | 'disabled' | undefined,
      search: search || undefined,
      sort_by: overrides.sortKey ?? sortKey,
      sort_order: overrides.sortOrder ?? sortOrder
    }
  }

  const isAbortError = (error: unknown) => {
    if (!error || typeof error !== 'object') return false
    const maybeError = error as { name?: string; code?: string }
    return maybeError.name === 'AbortError' || maybeError.code === 'ERR_CANCELED'
  }

  const loadCodes = async (overrides: RedeemQueryOverrides = {}) => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort()
    }
    const currentController = new AbortController()
    abortControllerRef.current = currentController
    setLoading(true)
    try {
      const response = await adminAPI.redeem.list(
        overrides.page ?? page,
        overrides.pageSize ?? pageSize,
        buildRedeemQueryFilters(overrides),
        { signal: currentController.signal }
      )
      if (currentController.signal.aborted) {
        return
      }
      setCodes(response.items)
      setTotal(response.total)
    } catch (error) {
      if (
        currentController.signal.aborted ||
        isAbortError(error)
      ) {
        return
      }
      showError(t('admin.redeem.failedToLoad'))
      console.error('Error loading redeem codes:', error)
    } finally {
      if (abortControllerRef.current === currentController && !currentController.signal.aborted) {
        setLoading(false)
        abortControllerRef.current = null
      }
    }
  }

  const loadSubscriptionGroups = async () => {
    try {
      setSubscriptionGroups(await adminAPI.groups.getAll())
    } catch (error) {
      console.error('Error loading subscription groups:', error)
    }
  }

  useEffect(() => {
    void loadCodes()
    void loadSubscriptionGroups()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (!showGenerateDialog) return
    void (async () => {
      try {
        const { data } = await adminAPI.payment.getPlans()
        setOrderPlans(data)
      } catch {
        setOrderPlans([])
      }
    })()
  }, [showGenerateDialog])

  const handleSearch = (value: string) => {
    setSearchQuery(value)
    if (searchTimerRef.current) window.clearTimeout(searchTimerRef.current)
    searchTimerRef.current = window.setTimeout(() => {
      setPage(1)
      void loadCodes({ search: value, page: 1 })
    }, 300)
  }

  const handleFilterChange = (overrides: RedeemQueryOverrides = {}) => {
    setPage(1)
    void loadCodes({ ...overrides, page: 1 })
  }

  const handlePageChange = (next: number) => {
    setPage(next)
    void loadCodes({ page: next })
  }

  const handlePageSizeChange = (next: number) => {
    setPageSize(next)
    setPage(1)
    void loadCodes({ pageSize: next, page: 1 })
  }

  const handleSort = (key: string, order: SortOrder) => {
    setSortKey(key)
    setSortOrder(order)
    setPage(1)
    void loadCodes({ sortKey: key, sortOrder: order, page: 1 })
  }

  const clearSelectedCodes = () => setSelectedCodeKeys([])

  const getRedeemCodeExpiresInDays = (): number | null | undefined => {
    if (generateForm.expiry_option === 'never') {
      return undefined
    }
    if (generateForm.expiry_option === 'custom') {
      const days = Number(generateForm.custom_expiry_days)
      if (!Number.isFinite(days) || days < 1) {
        return null
      }
      return Math.floor(days)
    }
    return Number(generateForm.expiry_option)
  }

  const applyGenerateType = (nextType: RedeemCodeType) => {
    setGenerateForm((previous) => {
      const next: GenerateFormState = {
        ...previous,
        type: nextType,
        // 邀请码类型时自动设置 value 为 0
        value: nextType === 'invitation' ? '0' : previous.value === '0' ? '10' : previous.value
      }
      if (nextType !== 'balance' && nextType !== 'subscription') {
        next.generate_order = false
      }
      next.order_plan_id = null
      return next
    })
  }

  const handleGenerateCodes = async () => {
    // 订阅类型必须选择分组
    if (generateForm.type === 'subscription' && !generateForm.generate_order && !generateForm.group_id) {
      showError(t('admin.redeem.groupRequired'))
      return
    }

    if (generateForm.generate_order && generateForm.type === 'subscription' && !generateForm.order_plan_id) {
      showError(t('featureExtensions.selectPlan'))
      return
    }
    const expiresInDays = getRedeemCodeExpiresInDays()
    if (expiresInDays === null) {
      showError(t('admin.redeem.expiryDaysRequired'))
      return
    }

    setGenerating(true)
    try {
      const result = await adminAPI.redeem.generate(
        Number(generateForm.count),
        generateForm.type,
        Number(generateForm.value),
        generateForm.type === 'subscription' && !generateForm.generate_order ? generateForm.group_id : undefined,
        generateForm.type === 'subscription' && !generateForm.generate_order ? Number(generateForm.validity_days) : undefined,
        expiresInDays,
        {
          generate_order: generateForm.generate_order,
          order_currency: generateForm.order_currency,
          order_plan_id: generateForm.order_plan_id == null ? null : Number(generateForm.order_plan_id)
        }
      )
      setShowGenerateDialog(false)
      setGeneratedCodes(result)
      setShowResultDialog(true)
      // 重置表单
      setGenerateForm((previous) => ({
        ...previous,
        generate_order: false,
        order_currency: 'CNY',
        order_plan_id: null,
        group_id: null,
        validity_days: '30',
        expiry_option: 'never',
        custom_expiry_days: '7'
      }))
      void loadCodes()
    } catch (error) {
      showError(extractApiErrorMessage(error, t('admin.redeem.failedToGenerate')))
      console.error('Error generating codes:', error)
    } finally {
      setGenerating(false)
    }
  }

  const copyToClipboard = async (text: string) => {
    const success = await clipboardCopy(text, t('admin.redeem.copied'))
    if (success) {
      setCopiedCode(text)
      if (copiedTimerRef.current) window.clearTimeout(copiedTimerRef.current)
      copiedTimerRef.current = window.setTimeout(() => setCopiedCode(null), 2000)
    }
  }

  const copyGeneratedCodes = async () => {
    const success = await clipboardCopy(generatedCodesText, t('admin.redeem.copied'))
    if (success) {
      setCopiedAll(true)
      if (copiedAllTimerRef.current) window.clearTimeout(copiedAllTimerRef.current)
      copiedAllTimerRef.current = window.setTimeout(() => setCopiedAll(false), 2000)
    }
  }

  const downloadGeneratedCodes = () => {
    const blob = new Blob([generatedCodesText], { type: 'text/plain' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `redeem-codes-${new Date().toISOString().split('T')[0]}.txt`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    window.URL.revokeObjectURL(url)
  }

  const handleExportCodes = async () => {
    try {
      const blob = await adminAPI.redeem.exportCodes(buildRedeemQueryFilters())

      // Create download link
      const url = window.URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `redeem-codes-${new Date().toISOString().split('T')[0]}.csv`
      document.body.appendChild(link)
      link.click()
      document.body.removeChild(link)
      window.URL.revokeObjectURL(url)

      showSuccess(t('admin.redeem.codesExported'))
    } catch (error) {
      showError(extractApiErrorMessage(error, t('admin.redeem.failedToExport')))
      console.error('Error exporting codes:', error)
    }
  }

  const handleDelete = (code: RedeemCode) => {
    setDeletingCode(code)
    setShowDeleteDialog(true)
  }

  const confirmDelete = async () => {
    if (!deletingCode) return

    try {
      await adminAPI.redeem.delete(deletingCode.id)
      showSuccess(t('admin.redeem.codeDeleted'))
      setShowDeleteDialog(false)
      setDeletingCode(null)
      void loadCodes()
    } catch (error) {
      showError(extractApiErrorMessage(error, t('admin.redeem.failedToDelete')))
      console.error('Error deleting code:', error)
    }
  }

  const confirmDeleteUnused = async () => {
    try {
      // Get all unused codes and delete them
      const unusedCodesResponse = await adminAPI.redeem.list(1, 1000, { status: 'unused' })
      const unusedCodeIds = unusedCodesResponse.items.map((code) => code.id)

      if (unusedCodeIds.length === 0) {
        showInfo(t('admin.redeem.noUnusedCodes'))
        setShowDeleteUnusedDialog(false)
        return
      }

      const result = await adminAPI.redeem.batchDelete(unusedCodeIds)
      showSuccess(t('admin.redeem.codesDeleted', { count: result.deleted }))
      setShowDeleteUnusedDialog(false)
      void loadCodes()
    } catch (error) {
      showError(extractApiErrorMessage(error, t('admin.redeem.failedToDeleteUnused')))
      console.error('Error deleting unused codes:', error)
    }
  }

  const openBatchUpdateDialog = () => {
    if (selectedCount === 0) {
      showInfo(t('admin.redeem.selectCodesFirst'))
      return
    }
    setBatchUpdateForm(resetBatchUpdateForm())
    setShowBatchUpdateDialog(true)
  }

  const buildBatchUpdateFields = (): BatchUpdateRedeemCodeFields | null => {
    const fields: BatchUpdateRedeemCodeFields = {}

    if (batchUpdateForm.update_status) {
      fields.status = batchUpdateForm.status
    }
    if (batchUpdateForm.update_expires_at) {
      if (batchUpdateForm.expires_mode === 'clear') {
        fields.expires_at = null
      } else {
        const expiresAt = parseDateTimeLocalInput(batchUpdateForm.expires_at_local)
        if (expiresAt === null) {
          showError(t('admin.redeem.expiryDateRequired'))
          return null
        }
        fields.expires_at = new Date(expiresAt * 1000).toISOString()
      }
    }
    if (batchUpdateForm.update_notes) {
      fields.notes = batchUpdateForm.notes
    }
    if (batchUpdateForm.update_group_id) {
      fields.group_id = batchUpdateForm.group_id == null ? null : Number(batchUpdateForm.group_id)
    }

    return Object.keys(fields).length > 0 ? fields : null
  }

  const handleBatchUpdate = async () => {
    const ids = [...selectedCodeIds]
    if (ids.length === 0) {
      showInfo(t('admin.redeem.selectCodesFirst'))
      return
    }

    const hasSelectedFields =
      batchUpdateForm.update_status ||
      batchUpdateForm.update_expires_at ||
      batchUpdateForm.update_notes ||
      batchUpdateForm.update_group_id
    if (!hasSelectedFields) {
      showError(t('admin.redeem.noBatchFieldsSelected'))
      return
    }

    const fields = buildBatchUpdateFields()
    if (!fields) {
      return
    }

    setBatchUpdating(true)
    try {
      const result = await adminAPI.redeem.batchUpdate(ids, fields)
      showSuccess(t('admin.redeem.batchUpdateSuccess', { count: result.updated }))
      setShowBatchUpdateDialog(false)
      clearSelectedCodes()
      void loadCodes()
    } catch (error) {
      showError(extractApiErrorMessage(error, t('admin.redeem.failedToBatchUpdate')))
      console.error('Error batch updating codes:', error)
    } finally {
      setBatchUpdating(false)
    }
  }

  return (
    <>
      <AdminTablePageLayout
        filters={
          <div className="filter-bar">
            <div className="filter-field filter-field--search">
              <input
                value={searchQuery}
                type="text"
                placeholder={t('admin.redeem.searchCodes')}
                className="input"
                onChange={(event) => handleSearch(event.target.value)}
              />
            </div>
            <div className="filter-field">
              <Select
                value={typeFilter}
                options={filterTypeOptions}
                searchable={false}
                onValueChange={(next) => {
                  setTypeFilter(String(next ?? ''))
                  handleFilterChange({ type: String(next ?? '') })
                }}
              />
            </div>
            <div className="filter-field">
              <Select
                value={statusFilter}
                options={filterStatusOptions}
                searchable={false}
                onValueChange={(next) => {
                  setStatusFilter(String(next ?? ''))
                  handleFilterChange({ status: String(next ?? '') })
                }}
              />
            </div>

            <div className="filter-actions">
              <Button onClick={() => void loadCodes()} disabled={loading} title={t('common.refresh')}>
                <Icon name="refresh" size="md" className={loading ? 'animate-spin' : ''} />
              </Button>
              <Button onClick={() => void handleExportCodes()}>{t('admin.redeem.exportCsv')}</Button>
              <Button
                data-test="batch-update-open"
                onClick={openBatchUpdateDialog}
                disabled={selectedCount === 0 || batchUpdating}
              >
                <Icon name="edit" size="md" className="mr-2" />
                {t('admin.redeem.batchUpdate')}
              </Button>
              <Button variant="primary" onClick={() => setShowGenerateDialog(true)}>
                {t('admin.redeem.generateCodes')}
              </Button>
            </div>
          </div>
        }
      table={
        <AdminDataTable<RedeemCode>
          columns={columns}
          data={codes}
          loading={loading}
          rowKey="id"
          selectable
          selectedKeys={selectedCodeKeys}
          serverSideSort
          sortKey={sortKey}
          sortOrder={sortOrder}
          onSort={handleSort}
          onSelectedKeysChange={setSelectedCodeKeys}
          renderCell={(key, row, value) => {
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
                    title={copiedCode === value ? t('admin.redeem.copied') : t('keys.copyToClipboard')}
                    onClick={() => void copyToClipboard(String(value ?? ''))}
                  >
                    {copiedCode !== value ? (
                      <Icon name="copy" size="sm" strokeWidth={2} />
                    ) : (
                      <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M5 13l4 4L19 7" />
                      </svg>
                    )}
                  </button>
                </div>
              )
            }
            if (key === 'type') {
              const badge =
                value === 'balance' ? 'badge-success' : value === 'subscription' ? 'badge-warning' : 'badge-primary'
              return <span className={`badge ${badge}`}>{t('admin.redeem.types.' + String(value ?? ''))}</span>
            }
            if (key === 'value') {
              return (
                <div>
                  {row.generate_order && (
                    <p className="mb-1 text-xs text-primary-600">
                      {t('featureExtensions.redeemOrder')} · {row.order_amount} {row.order_currency}
                      {row.order_plan_name && <> · {row.order_plan_name}</>}
                    </p>
                  )}
                  <span className="text-sm font-medium text-gray-900 dark:text-white">
                    {row.type === 'balance' ? (
                      `$${(value as number).toFixed(2)}`
                    ) : row.type === 'subscription' ? (
                      <>
                        {row.validity_days || 30} {t('admin.redeem.days')}
                        {row.group && <span className="ml-1 text-xs surface-muted">({row.group.name})</span>}
                      </>
                    ) : (
                      String(value ?? '')
                    )}
                  </span>
                </div>
              )
            }
            if (key === 'status') {
              const badge =
                value === 'unused' ? 'badge-success' : value === 'used' ? 'badge-gray' : 'badge-danger'
              return <span className={`badge ${badge}`}>{t('admin.redeem.status.' + String(value ?? ''))}</span>
            }
            if (key === 'used_by') {
              return (
                <span className="text-sm surface-muted">
                  {row.user?.email || (value ? t('admin.redeem.userPrefix', { id: value }) : '-')}
                </span>
              )
            }
            if (key === 'used_at') {
              return <span className="text-sm surface-muted">{value ? formatDateTime(value as string) : '-'}</span>
            }
            if (key === 'expires_at') {
              return (
                <span
                  className={`text-sm ${row.status === 'expired' ? 'text-red-600 dark:text-red-400' : 'surface-muted'}`}
                >
                  {value ? formatDateTime(value as string) : t('admin.redeem.neverExpires')}
                </span>
              )
            }
            if (key === 'actions') {
              return (
                <div className="table-actions">
                  {row.status === 'unused' ? (
                    <button
                      type="button"
                      onClick={() => handleDelete(row)}
                      className="table-action table-action-danger"
                    >
                      <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                        <path
                          strokeLinecap="round"
                          strokeLinejoin="round"
                          strokeWidth="2"
                          d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                        />
                      </svg>
                      <span className="text-xs">{t('common.delete')}</span>
                    </button>
                  ) : (
                    <span className="surface-muted">-</span>
                  )}
                </div>
              )
            }
            return <span>{value == null ? '' : String(value)}</span>
          }}
        />
      }
      pagination={
        <>
          {selectedCount > 0 && (
            <div className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-lg bg-primary-50/[0.15] p-3 dark:bg-primary-900/20">
              <span className="text-sm font-medium text-primary-900 dark:text-primary-100">
                {t('admin.redeem.selectedCount', { count: selectedCount })}
              </span>
              <div className="flex flex-wrap items-center gap-2">
                <button
                  type="button"
                  className="text-xs font-medium text-primary-700 hover:text-primary-800 dark:text-primary-300 dark:hover:text-primary-200 surface-control"
                  onClick={clearSelectedCodes}
                >
                  {t('admin.redeem.clearSelection')}
                </button>
                <Button variant="primary" size="sm" onClick={openBatchUpdateDialog}>
                  {t('admin.redeem.batchUpdate')}
                </Button>
              </div>
            </div>
          )}

          {total > 0 && (
            <Pagination
              page={page}
              total={total}
              pageSize={pageSize}
              onPageChange={handlePageChange}
              onPageSizeChange={handlePageSizeChange}
            />
          )}

          {statusFilter === 'unused' && (
            <div className="flex justify-end">
              <Button variant="danger" onClick={() => setShowDeleteUnusedDialog(true)}>
                {t('admin.redeem.deleteAllUnused')}
              </Button>
            </div>
          )}
        </>
      }
      />
      <ConfirmDialog
        open={showDeleteDialog}
        title={t('admin.redeem.deleteCode')}
        message={t('admin.redeem.deleteCodeConfirm')}
        confirmText={t('common.delete')}
        cancelText={t('common.cancel')}
        danger
        onConfirm={() => void confirmDelete()}
        onCancel={() => setShowDeleteDialog(false)}
      />

      <ConfirmDialog
        open={showDeleteUnusedDialog}
        title={t('admin.redeem.deleteAllUnused')}
        message={t('admin.redeem.deleteAllUnusedConfirm')}
        confirmText={t('admin.redeem.deleteAll')}
        cancelText={t('common.cancel')}
        danger
        onConfirm={() => void confirmDeleteUnused()}
        onCancel={() => setShowDeleteUnusedDialog(false)}
      />

      <Modal
        open={showGenerateDialog}
        onClose={() => setShowGenerateDialog(false)}
        title={t('admin.redeem.generateCodesTitle')}
        size="sm"
        footer={
          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={() => setShowGenerateDialog(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" form="generate-redeem-codes-form" variant="primary" disabled={generating}>
              {generating ? t('admin.redeem.generating') : t('admin.redeem.generate')}
            </Button>
          </div>
        }
      >
        <form
          id="generate-redeem-codes-form"
          onSubmit={(event) => {
            event.preventDefault()
            void handleGenerateCodes()
          }}
          className="space-y-4"
        >
          <div>
            <label className="input-label">{t('admin.redeem.codeType')}</label>
            <Select
              value={generateForm.type}
              options={typeOptions}
              searchable={false}
              onValueChange={(next) => applyGenerateType(String(next) as RedeemCodeType)}
            />
          </div>
          {(generateForm.type === 'balance' || generateForm.type === 'subscription') && (
            <div>
              <label className="input-label">{t('featureExtensions.generateOrder')}</label>
              <Select
                value={generateForm.generate_order}
                options={[
                  { value: false, label: t('featureExtensions.noOrder') },
                  { value: true, label: t('featureExtensions.withOrder') }
                ]}
                searchable={false}
                onValueChange={(next) =>
                  setGenerateForm((previous) => ({ ...previous, generate_order: next === true }))
                }
              />
            </div>
          )}
          {generateForm.generate_order && generateForm.type === 'balance' && (
            <div>
              <label className="input-label">{t('featureExtensions.orderCurrency')}</label>
              <Select
                value={generateForm.order_currency}
                options={[
                  { value: 'CNY', label: 'CNY' },
                  { value: 'USD', label: 'USD' }
                ]}
                searchable={false}
                onValueChange={(next) =>
                  setGenerateForm((previous) => ({ ...previous, order_currency: String(next) }))
                }
              />
              <p className="mt-1 text-xs surface-muted">{t('featureExtensions.noExchange')}</p>
            </div>
          )}
          {generateForm.generate_order && generateForm.type === 'subscription' && (
            <div>
              <label className="input-label">{t('featureExtensions.orderPlan')}</label>
              <Select
                value={generateForm.order_plan_id}
                options={orderPlanOptions}
                searchable={false}
                placeholder={t('featureExtensions.selectPlan')}
                onValueChange={(next) =>
                  setGenerateForm((previous) => ({ ...previous, order_plan_id: next == null ? null : Number(next) }))
                }
              />
              {selectedOrderPlan && (
                <p className="mt-1 text-xs surface-muted">
                  {t('featureExtensions.planSnapshot', {
                    name: selectedOrderPlan.name,
                    price: selectedOrderPlan.price,
                    currency: selectedOrderPlan.currency || 'CNY'
                  })}
                </p>
              )}
            </div>
          )}
          {/* 余额/并发类型：显示数值输入 */}
          {generateForm.type !== 'subscription' && generateForm.type !== 'invitation' && (
            <div>
              <label className="input-label">
                {generateForm.type === 'balance' ? t('admin.redeem.amount') : t('admin.redeem.columns.value')}
              </label>
              <input
                value={generateForm.value}
                type="number"
                step={generateForm.type === 'balance' ? '0.01' : '1'}
                min={generateForm.type === 'balance' ? '0.01' : '1'}
                className="input"
                onChange={(event) => setGenerateForm((previous) => ({ ...previous, value: event.target.value }))}
              />
            </div>
          )}
          {/* 邀请码类型：显示提示信息 */}
          {generateForm.type === 'invitation' && (
            <div className="rounded-lg bg-blue-50/[0.15] p-3 dark:bg-blue-900/20">
              <p className="text-sm text-blue-700 dark:text-blue-300">{t('admin.redeem.invitationHint')}</p>
            </div>
          )}
          {/* 订阅类型：显示分组选择和有效天数 */}
          {generateForm.type === 'subscription' && !generateForm.generate_order && (
            <>
              <div>
                <label className="input-label">{t('admin.redeem.selectGroup')}</label>
                <Select
                  value={generateForm.group_id}
                  options={subscriptionGroupOptions}
                  placeholder={t('admin.redeem.selectGroupPlaceholder')}
                  renderSelected={(option) => {
                    const groupOption = option as GroupOption | null
                    return groupOption ? (
                      <GroupBadge
                        name={groupOption.label}
                        platform={groupOption.platform}
                        subscriptionType={groupOption.subscriptionType}
                        rateMultiplier={groupOption.rate}
                      />
                    ) : (
                      <span className="surface-muted">{t('admin.redeem.selectGroupPlaceholder')}</span>
                    )
                  }}
                  renderOption={(option) => {
                    const groupOption = option as unknown as GroupOption
                    return (
                      <GroupOptionItem
                        name={groupOption.label}
                        platform={groupOption.platform}
                        subscriptionType={groupOption.subscriptionType}
                        rateMultiplier={groupOption.rate}
                        description={groupOption.description}
                      />
                    )
                  }}
                  onValueChange={(next) =>
                    setGenerateForm((previous) => ({ ...previous, group_id: next == null ? null : Number(next) }))
                  }
                />
              </div>
              <div>
                <label className="input-label">{t('admin.redeem.validityDays')}</label>
                <input
                  value={generateForm.validity_days}
                  type="number"
                  min="1"
                  max="36500"
                  className="input"
                  onChange={(event) => setGenerateForm((previous) => ({ ...previous, validity_days: event.target.value }))}
                />
              </div>
            </>
          )}
          <div>
            <label className="input-label">{t('admin.redeem.codeExpiry')}</label>
            <div className="flex flex-wrap gap-2">
              {redeemCodeExpiryOptions.map((option) => (
                <button
                  key={option.value}
                  type="button"
                  className={`flex-1 whitespace-nowrap rounded-lg border px-3 py-2 text-sm transition-colors ${
                    generateForm.expiry_option === option.value
                      ? 'border-primary-500 bg-primary-50/[0.15] text-primary-700 dark:border-primary-400 dark:bg-primary-900/20 dark:text-primary-300'
                      : 'border-gray-200 text-gray-700 hover:surface-hover dark:border-dark-600 dark:text-gray-300 dark:hover:surface-hover'
                  }`}
                  onClick={() => setGenerateForm((previous) => ({ ...previous, expiry_option: option.value }))}
                >
                  {option.label}
                </button>
              ))}
            </div>
            {generateForm.expiry_option === 'custom' && (
              <input
                value={generateForm.custom_expiry_days}
                type="number"
                min="1"
                max="3650"
                className="input mt-2"
                placeholder={t('admin.redeem.customExpiryDays')}
                onChange={(event) =>
                  setGenerateForm((previous) => ({ ...previous, custom_expiry_days: event.target.value }))
                }
              />
            )}
          </div>
          <div>
            <label className="input-label">{t('admin.redeem.count')}</label>
            <input
              value={generateForm.count}
              type="number"
              min="1"
              max="100"
              className="input"
              onChange={(event) => setGenerateForm((previous) => ({ ...previous, count: event.target.value }))}
            />
          </div>
        </form>
      </Modal>

      <Modal
        open={showBatchUpdateDialog}
        onClose={() => setShowBatchUpdateDialog(false)}
        title={t('admin.redeem.batchUpdateTitle')}
        closeOnOverlayClick={false}
        footer={
          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={() => setShowBatchUpdateDialog(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              data-test="batch-update-submit"
              type="submit"
              form="batch-update-redeem-codes-form"
              variant="primary"
              disabled={batchUpdating}
            >
              {batchUpdating ? t('common.submitting') : t('admin.redeem.batchUpdate')}
            </Button>
          </div>
        }
      >
        <p className="mb-4 text-sm surface-muted">{t('admin.redeem.selectedCount', { count: selectedCount })}</p>

        <form
          id="batch-update-redeem-codes-form"
          data-test="batch-update-form"
          onSubmit={(event) => {
            event.preventDefault()
            void handleBatchUpdate()
          }}
          className="space-y-4"
        >
          <div className="space-y-2">
            <label className="flex items-center gap-2 text-sm font-medium surface-muted">
              <input
                data-test="batch-field-status"
                type="checkbox"
                className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                checked={batchUpdateForm.update_status}
                onChange={(event) =>
                  setBatchUpdateForm((previous) => ({ ...previous, update_status: event.target.checked }))
                }
              />
              {t('admin.redeem.batchFields.status')}
            </label>
            {batchUpdateForm.update_status && (
              <Select
                value={batchUpdateForm.status}
                options={batchStatusOptions}
                searchable={false}
                onValueChange={(next) =>
                  setBatchUpdateForm((previous) => ({ ...previous, status: String(next) as 'unused' | 'disabled' }))
                }
              />
            )}
          </div>

          <div className="space-y-2">
            <label className="flex items-center gap-2 text-sm font-medium surface-muted">
              <input
                type="checkbox"
                className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                checked={batchUpdateForm.update_expires_at}
                onChange={(event) =>
                  setBatchUpdateForm((previous) => ({ ...previous, update_expires_at: event.target.checked }))
                }
              />
              {t('admin.redeem.batchFields.expiresAt')}
            </label>
            {batchUpdateForm.update_expires_at && (
              <>
                <Select
                  value={batchUpdateForm.expires_mode}
                  options={batchExpiryModeOptions}
                  searchable={false}
                  onValueChange={(next) =>
                    setBatchUpdateForm((previous) => ({ ...previous, expires_mode: String(next) as 'clear' | 'custom' }))
                  }
                />
                {batchUpdateForm.expires_mode === 'custom' && (
                  <>
                    <DateTimePicker
                      ariaLabel={t('admin.redeem.batchFields.expiresAt')}
                      value={batchUpdateForm.expires_at_local}
                      type="datetime-local"
                      className="input"
                      onValueChange={(next) =>
                        setBatchUpdateForm((previous) => ({ ...previous, expires_at_local: next }))
                      }
                    />
                    <p className="input-hint">{t('admin.redeem.localTimeZoneHint', { timezone: browserTimeZone })}</p>
                  </>
                )}
              </>
            )}
          </div>

          <div className="space-y-2">
            <label className="flex items-center gap-2 text-sm font-medium surface-muted">
              <input
                data-test="batch-field-notes"
                type="checkbox"
                className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                checked={batchUpdateForm.update_notes}
                onChange={(event) =>
                  setBatchUpdateForm((previous) => ({ ...previous, update_notes: event.target.checked }))
                }
              />
              {t('admin.redeem.batchFields.notes')}
            </label>
            {batchUpdateForm.update_notes && (
              <TextArea
                data-test="batch-notes-input"
                value={batchUpdateForm.notes}
                rows={3}
                placeholder={t('admin.redeem.batchNotesPlaceholder')}
                onValueChange={(next) => setBatchUpdateForm((previous) => ({ ...previous, notes: next }))}
              />
            )}
          </div>

          <div className="space-y-2">
            <label className="flex items-center gap-2 text-sm font-medium surface-muted">
              <input
                type="checkbox"
                className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                checked={batchUpdateForm.update_group_id}
                onChange={(event) =>
                  setBatchUpdateForm((previous) => ({ ...previous, update_group_id: event.target.checked }))
                }
              />
              {t('admin.redeem.batchFields.group')}
            </label>
            {batchUpdateForm.update_group_id && (
              <Select
                value={batchUpdateForm.group_id}
                options={batchGroupOptions}
                placeholder={t('admin.redeem.selectGroupPlaceholder')}
                onValueChange={(next) =>
                  setBatchUpdateForm((previous) => ({ ...previous, group_id: next == null ? null : Number(next) }))
                }
              />
            )}
          </div>
        </form>
      </Modal>

      <Modal
        open={showResultDialog}
        onClose={() => {
          setShowResultDialog(false)
          setGeneratedCodes([])
          setCopiedAll(false)
        }}
        title={t('admin.redeem.generatedSuccessfully')}
        footer={
          <div className="flex justify-end gap-2">
            <Button variant={copiedAll ? 'success' : 'secondary'} onClick={() => void copyGeneratedCodes()}>
              {copiedAll ? (
                <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M5 13l4 4L19 7" />
                </svg>
              ) : (
                <Icon name="copy" size="sm" strokeWidth={2} />
              )}
              {copiedAll ? t('admin.redeem.copied') : t('admin.redeem.copyAll')}
            </Button>
            <Button variant="primary" onClick={downloadGeneratedCodes}>
              <Icon name="download" size="sm" strokeWidth={2} />
              {t('admin.redeem.download')}
            </Button>
          </div>
        }
      >
        <div className="mb-4 flex items-center gap-3">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-green-100/[0.15] dark:bg-green-900/30">
            <Icon name="check" size="md" className="text-green-600 dark:text-green-400" />
          </div>
          <p className="text-sm surface-muted">{t('admin.redeem.codesCreated', { count: generatedCodes.length })}</p>
        </div>
        <div className="relative">
          <TextArea
            readOnly
            value={generatedCodesText}
            style={{ height: textareaHeight }}
            className="w-full resize-none rounded-lg border border-gray-200 surface-inset p-3 font-mono text-sm text-gray-800 focus:outline-none dark:border-dark-600 dark:text-gray-200"
          />
        </div>
      </Modal>
    </>
  )
}
