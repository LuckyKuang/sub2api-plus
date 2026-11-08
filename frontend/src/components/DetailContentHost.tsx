import type { ReactNode } from 'react'
import { Modal } from './ui/Modal'

export interface DetailContentHostProps {
  embedded?: boolean
  show: boolean
  title: string
  children?: ReactNode
  footer?: ReactNode
  onClose?: () => void
}

export function DetailContentHost({
  embedded = false,
  show,
  title,
  children,
  footer,
  onClose
}: DetailContentHostProps) {
  if (embedded && show) {
    return (
      <div className="min-w-0">
        {children}
        {footer}
      </div>
    )
  }
  if (embedded) return null
  return (
    <Modal open={show} title={title} footer={footer} onClose={onClose}>
      {children}
    </Modal>
  )
}
