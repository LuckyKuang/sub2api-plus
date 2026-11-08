import { ExportProgressDialog } from '@/components/ExportProgressDialog'

export interface UsageExportProgressProps {
  show: boolean
  progress: number
  current: number
  total: number
  estimatedTime: string
  onCancel?: () => void
}

export function UsageExportProgress({ show, progress, current, total, estimatedTime, onCancel }: UsageExportProgressProps) {
  return (
    <ExportProgressDialog
      show={show}
      progress={progress}
      current={current}
      total={total}
      estimatedTime={estimatedTime}
      onCancel={onCancel}
    />
  )
}

export default UsageExportProgress
