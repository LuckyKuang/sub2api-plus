import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { AlertTriangle, CheckCircle2, Info, X, XCircle } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from './Button'
import { Modal } from './Modal'

export type ToastType = 'success' | 'error' | 'info' | 'warning'

export interface ToastItem {
  id: string
  type: ToastType
  message: string
  duration?: number
}

const icons: Record<ToastType, React.ReactNode> = {
  success: <CheckCircle2 className="h-5 w-5 text-emerald-500" />,
  error: <XCircle className="h-5 w-5 text-red-500" />,
  warning: <AlertTriangle className="h-5 w-5 text-amber-500" />,
  info: <Info className="h-5 w-5 text-blue-500" />
}

const borderClass: Record<ToastType, string> = {
  success: 'border-emerald-500',
  error: 'border-red-500',
  warning: 'border-amber-500',
  info: 'border-blue-500'
}

let toastIdCounter = 0
const listeners = new Set<(toasts: ToastItem[]) => void>()
let toasts: ToastItem[] = []

function emit() {
  const snapshot = toasts
  for (const listener of listeners) listener(snapshot)
}

export function showToast(type: ToastType, message: string, duration = type === 'error' ? 5000 : 3000): string {
  const id = `toast-${++toastIdCounter}`
  toasts = [...toasts, { id, type, message, duration }]
  emit()
  if (duration !== undefined && duration > 0) {
    setTimeout(() => hideToast(id), duration)
  }
  return id
}

export function hideToast(id: string): void {
  toasts = toasts.filter((toast) => toast.id !== id)
  emit()
}

export const toast = {
  success: (message: string, duration?: number) => showToast('success', message, duration),
  error: (message: string, duration?: number) => showToast('error', message, duration),
  info: (message: string, duration?: number) => showToast('info', message, duration),
  warning: (message: string, duration?: number) => showToast('warning', message, duration)
}

function ToastRow({ item }: { item: ToastItem }) {
  return (
    <div
      className={cn(
        'surface-panel pointer-events-auto flex w-full max-w-sm items-start gap-3 rounded-xl border-l-4 px-4 py-3 shadow-lg',
        borderClass[item.type]
      )}
      role="status"
    >
      <div className="mt-0.5 shrink-0">{icons[item.type]}</div>
      <p className="min-w-0 flex-1 text-sm text-gray-800 dark:text-gray-100">{item.message}</p>
      <button
        type="button"
        onClick={() => hideToast(item.id)}
        aria-label="Dismiss"
        className="shrink-0 rounded p-0.5 text-gray-400 transition-colors hover:text-gray-600 dark:hover:text-gray-200"
      >
        <X className="h-4 w-4" />
      </button>
    </div>
  )
}

export function Toaster() {
  const [items, setItems] = useState<ToastItem[]>(toasts)

  useEffect(() => {
    listeners.add(setItems)
    setItems(toasts)
    return () => {
      listeners.delete(setItems)
    }
  }, [])

  if (items.length === 0) return null

  return createPortal(
    <div className="pointer-events-none fixed right-4 top-4 z-[100] flex w-full max-w-sm flex-col gap-2">
      {items.map((item) => (
        <ToastRow key={item.id} item={item} />
      ))}
    </div>,
    document.body
  )
}

export interface ConfirmDialogProps {
  open: boolean
  title?: string
  message?: React.ReactNode
  confirmText?: string
  cancelText?: string
  variant?: 'danger' | 'primary'
  onConfirm?: () => void
  onCancel?: () => void
}

export function ConfirmDialog({
  open,
  title,
  message,
  confirmText = 'Confirm',
  cancelText = 'Cancel',
  variant = 'primary',
  onConfirm,
  onCancel
}: ConfirmDialogProps) {
  return (
    <Modal
      open={open}
      onClose={onCancel}
      title={title}
      size="sm"
      footer={
        <>
          <Button variant="ghost" onClick={onCancel}>
            {cancelText}
          </Button>
          <Button variant={variant === 'danger' ? 'danger' : 'primary'} onClick={onConfirm}>
            {confirmText}
          </Button>
        </>
      }
    >
      <div className="text-sm text-gray-600 dark:text-gray-300">{message}</div>
    </Modal>
  )
}
