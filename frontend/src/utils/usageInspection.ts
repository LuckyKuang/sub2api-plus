import type { UsageLog } from '@/types'

export function usageCompletion(row: Pick<UsageLog, 'completion_status' | 'is_complete'>): string {
  if (row.completion_status === 'client_disconnected') return 'client_disconnected'
  if (row.is_complete === false || row.completion_status === 'incomplete') return 'incomplete'
  if (row.completion_status === 'completed') return 'completed'
  return 'unknown'
}

export function usageMoney(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '—'
  // Do not round a non-zero amount down to free. Detail keeps useful precision.
  return `$${value === 0 ? '0.00' : Math.abs(value) < 0.000001 ? value.toPrecision(3) : value.toFixed(8).replace(/0+$/, '').replace(/\.$/, '')}`
}

export function usageMilliseconds(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value) || value < 0) return '—'
  return value < 1000 ? `${value} ms` : `${(value / 1000).toFixed(3)} s`
}
