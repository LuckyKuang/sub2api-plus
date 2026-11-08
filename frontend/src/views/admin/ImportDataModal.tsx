import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { adminAPI } from '@/api'
import { useAppStore } from '@/stores/app'
import type { AdminDataImportResult, AdminDataPayload } from '@/types'
import { Modal } from '@/components/ui'

export interface ImportDataModalProps {
  show: boolean
  onClose: () => void
  onImported: () => void
}

const SUPPORTED_DATA_TYPES = ['sub2api-data', 'sub2api-bundle']
const SUPPORTED_DATA_VERSION = 1

export function ImportDataModal({ show, onClose, onImported }: ImportDataModalProps) {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showWarning = useAppStore((state) => state.showWarning)
  const showSuccess = useAppStore((state) => state.showSuccess)

  const [importing, setImporting] = useState(false)
  const [files, setFiles] = useState<File[]>([])
  const [dragDepth, setDragDepth] = useState(0)
  const [hasCreatedData, setHasCreatedData] = useState(false)
  const [result, setResult] = useState<AdminDataImportResult | null>(null)
  const fileInput = useRef<HTMLInputElement | null>(null)

  const dragActive = dragDepth > 0

  const selectedFilesLabel = (() => {
    if (files.length === 0) return ''
    if (files.length === 1) return files[0]?.name || ''
    return t('admin.accounts.selectedCount', { count: files.length })
  })()
  const fileListTitle = files.map((item) => item.name).join(', ')

  const warningItems = result?.warnings || []
  const errorItems = result?.errors || []

  useEffect(() => {
    if (!show) return
    setFiles([])
    setDragDepth(0)
    setHasCreatedData(false)
    setResult(null)
    if (fileInput.current) fileInput.current.value = ''
  }, [show])

  const openFilePicker = () => {
    fileInput.current?.click()
  }

  const handleFileChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    setSelectedFiles(event.target.files)
    event.target.value = ''
  }

  const handleClose = () => {
    if (importing) return
    if (hasCreatedData) {
      setHasCreatedData(false)
      onImported()
    }
    onClose()
  }

  const isJsonFile = (sourceFile: File) => {
    const name = sourceFile.name.toLowerCase()
    return name.endsWith('.json') || sourceFile.type === 'application/json'
  }

  const setSelectedFiles = (sourceFiles: FileList | File[] | null | undefined) => {
    if (importing) return
    const incoming = Array.from(sourceFiles || [])
    const picked = incoming.filter(isJsonFile)
    if (!picked.length) {
      showError(t('admin.accounts.dataImportSelectFile'))
      return
    }
    if (picked.length < incoming.length) {
      showWarning(t('admin.accounts.dataImportIgnoredFiles', { count: incoming.length - picked.length }))
    }
    setFiles(picked)
    setResult(null)
  }

  const handleDragEnter = () => {
    if (importing) return
    setDragDepth((prev) => prev + 1)
  }

  const handleDragLeave = () => {
    setDragDepth((prev) => Math.max(0, prev - 1))
  }

  const handleDrop = (event: React.DragEvent) => {
    setDragDepth(0)
    if (importing) return
    setSelectedFiles(event.dataTransfer?.files)
  }

  const readFileAsText = async (sourceFile: File): Promise<string> => {
    if (typeof sourceFile.text === 'function') {
      return sourceFile.text()
    }

    if (typeof sourceFile.arrayBuffer === 'function') {
      const buffer = await sourceFile.arrayBuffer()
      return new TextDecoder().decode(buffer)
    }

    return await new Promise<string>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(String(reader.result ?? ''))
      reader.onerror = () => reject(reader.error || new Error('Failed to read file'))
      reader.readAsText(sourceFile)
    })
  }

  // 与后端 validateDataHeader 对齐:合并前逐文件校验,避免坏文件混入合并 payload 后
  // 报错无法定位来源,或绕过后端本会对单文件做的 type/version 检查。
  const isValidDataPayload = (payload: unknown): payload is AdminDataPayload => {
    if (!payload || typeof payload !== 'object' || Array.isArray(payload)) return false
    const candidate = payload as Record<string, unknown>
    if (
      candidate.type !== undefined &&
      candidate.type !== '' &&
      !SUPPORTED_DATA_TYPES.includes(candidate.type as string)
    ) {
      return false
    }
    if (candidate.version !== undefined && candidate.version !== 0 && candidate.version !== SUPPORTED_DATA_VERSION) {
      return false
    }
    return Array.isArray(candidate.proxies) && Array.isArray(candidate.accounts)
  }

  const mergeDataPayloads = (payloads: AdminDataPayload[]): AdminDataPayload => {
    const [firstPayload] = payloads
    if (payloads.length === 1 && firstPayload) return firstPayload

    return {
      type: payloads.find((item) => typeof item.type === 'string')?.type,
      version: payloads.find((item) => typeof item.version === 'number')?.version,
      exported_at: new Date().toISOString(),
      proxies: payloads.flatMap((item) => item.proxies),
      accounts: payloads.flatMap((item) => item.accounts),
      skipped_shadows: payloads.reduce((sum, item) => {
        const count = Number(item.skipped_shadows || 0)
        return Number.isFinite(count) ? sum + count : sum
      }, 0)
    }
  }

  const handleImport = async () => {
    if (files.length === 0) {
      showError(t('admin.accounts.dataImportSelectFile'))
      return
    }

    setImporting(true)
    try {
      const dataPayloads: AdminDataPayload[] = []
      for (const sourceFile of files) {
        let parsed: unknown
        try {
          parsed = JSON.parse(await readFileAsText(sourceFile))
        } catch {
          showError(t('admin.accounts.dataImportParseFailedFile', { name: sourceFile.name }))
          return
        }
        if (!isValidDataPayload(parsed)) {
          showError(t('admin.accounts.dataImportInvalidFile', { name: sourceFile.name }))
          return
        }
        dataPayloads.push(parsed)
      }
      const dataPayload = mergeDataPayloads(dataPayloads)

      const res = await adminAPI.accounts.importData({
        data: dataPayload,
        skip_default_group_bind: true
      })

      setResult(res)

      const msgParams: Record<string, unknown> = {
        account_created: res.account_created,
        account_failed: res.account_failed,
        proxy_created: res.proxy_created,
        proxy_reused: res.proxy_reused,
        proxy_failed: res.proxy_failed
      }
      if (res.account_failed > 0 || res.proxy_failed > 0) {
        // 部分成功也创建了数据;弹窗关闭时通过 imported 通知父组件刷新列表
        if (res.account_created > 0 || res.proxy_created > 0) {
          setHasCreatedData(true)
        }
        showError(t('admin.accounts.dataImportCompletedWithErrors', msgParams))
      } else {
        showSuccess(t('admin.accounts.dataImportSuccess', msgParams))
        onImported()
      }
    } catch (error) {
      showError((error as { message?: string })?.message || t('admin.accounts.dataImportFailed'))
    } finally {
      setImporting(false)
    }
  }

  return (
    <Modal open={show} onClose={handleClose} title={t('admin.accounts.dataImportTitle')} size="lg">
      <form
        id="import-data-form"
        className="space-y-4"
        onSubmit={(event) => {
          event.preventDefault()
          void handleImport()
        }}
      >
        <div className="text-sm surface-muted">{t('admin.accounts.dataImportHint')}</div>
        <div className="rounded-lg border border-amber-200 bg-amber-50/[0.15] p-3 text-xs text-amber-600 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-400">
          {t('admin.accounts.dataImportWarning')}
        </div>

        <div>
          <label className="input-label">{t('admin.accounts.dataImportFile')}</label>
          <div
            className={cn(
              'flex items-center justify-between gap-3 rounded-lg border border-dashed px-4 py-3 transition-colors',
              dragActive
                ? 'border-primary-400 bg-primary-50/70 dark:border-primary-500 dark:bg-primary-900/20'
                : 'border-gray-300 surface-inset dark:border-dark-600 dark:surface-inset'
            )}
            onDragEnter={(event) => {
              event.preventDefault()
              handleDragEnter()
            }}
            onDragOver={(event) => event.preventDefault()}
            onDragLeave={(event) => {
              event.preventDefault()
              handleDragLeave()
            }}
            onDrop={(event) => {
              event.preventDefault()
              handleDrop(event)
            }}
          >
            <div className="min-w-0">
              <div className="truncate text-sm surface-muted" title={fileListTitle}>
                {selectedFilesLabel || t('admin.accounts.dataImportSelectFile')}
              </div>
              <div className="text-xs surface-muted">
                JSON (.json)
                {files.length > 1 && <> · {fileListTitle}</>}
              </div>
            </div>
            <button type="button" className="btn btn-secondary shrink-0" onClick={openFilePicker}>
              {t('common.chooseFile')}
            </button>
          </div>
          <input
            ref={fileInput}
            type="file"
            className="hidden"
            accept="application/json,.json"
            multiple
            onChange={handleFileChange}
          />
        </div>

        {result && (
          <div className="space-y-2 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
            <div className="text-sm font-medium text-gray-900 dark:text-white">{t('admin.accounts.dataImportResult')}</div>
            <div className="text-sm surface-muted">
              {t('admin.accounts.dataImportResultSummary', { ...result } as Record<string, unknown>)}
            </div>

            {warningItems.length > 0 && (
              <div className="mt-2">
                <div className="text-sm font-medium text-amber-600 dark:text-amber-400">
                  {t('admin.accounts.dataImportWarnings')}
                </div>
                <div className="mt-2 max-h-48 overflow-auto rounded-lg bg-amber-50/[0.15] p-3 font-mono text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
                  {warningItems.map((item, idx) => (
                    <div key={idx} className="whitespace-pre-wrap">
                      {item.kind} {item.name || item.proxy_key || '-'} — {item.message}
                    </div>
                  ))}
                </div>
              </div>
            )}

            {errorItems.length > 0 && (
              <div className="mt-2">
                <div className="text-sm font-medium text-red-600 dark:text-red-400">{t('admin.accounts.dataImportErrors')}</div>
                <div className="mt-2 max-h-48 overflow-auto rounded-lg surface-inset p-3 font-mono text-xs">
                  {errorItems.map((item, idx) => (
                    <div key={idx} className="whitespace-pre-wrap">
                      {item.kind} {item.name || item.proxy_key || '-'} — {item.message}
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}
      </form>

      <div className="flex justify-end gap-3">
        <button className="btn btn-secondary" type="button" disabled={importing} onClick={handleClose}>
          {t('common.cancel')}
        </button>
        <button className="btn btn-primary" type="submit" form="import-data-form" disabled={importing}>
          {importing ? t('admin.accounts.dataImporting') : t('admin.accounts.dataImportButton')}
        </button>
      </div>
    </Modal>
  )
}

export default ImportDataModal
