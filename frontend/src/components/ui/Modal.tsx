import { useEffect, useRef } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import { cn } from '@/lib/utils'

export interface ModalProps {
  open: boolean
  onClose?: () => void
  title?: React.ReactNode
  description?: React.ReactNode
  children?: React.ReactNode
  footer?: React.ReactNode
  size?: 'sm' | 'md' | 'lg' | 'xl' | 'full'
  /** Render as a right-side detail panel instead of a centred dialog. */
  variant?: 'dialog' | 'detail'
  showClose?: boolean
  closeOnOverlayClick?: boolean
  className?: string
}

const sizeClass = {
  sm: 'max-w-sm',
  md: 'max-w-lg',
  lg: 'max-w-2xl',
  xl: 'max-w-4xl',
  full: 'max-w-6xl'
} as const

export function Modal({
  open,
  onClose,
  title,
  description,
  children,
  footer,
  size = 'md',
  variant = 'dialog',
  showClose = true,
  closeOnOverlayClick = true,
  className
}: ModalProps) {
  const contentRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && onClose) onClose()
    }
    document.addEventListener('keydown', onKeyDown)
    document.body.classList.add('modal-open')
    return () => {
      document.removeEventListener('keydown', onKeyDown)
      document.body.classList.remove('modal-open')
    }
  }, [open, onClose])

  if (!open) return null

  const isDetail = variant === 'detail'

  return createPortal(
    <div
      className={cn(isDetail ? 'detail-dialog-wrap fixed inset-0 z-50' : 'modal-overlay')}
      onMouseDown={(event) => {
        if (closeOnOverlayClick && event.target === event.currentTarget) onClose?.()
      }}
    >
      <div
        ref={contentRef}
        role="dialog"
        aria-modal="true"
        className={cn(
          isDetail ? 'detail-dialog modal-content' : 'modal-content',
          !isDetail && sizeClass[size],
          'surface-panel mx-auto',
          className
        )}
      >
        {(title || showClose) && (
          <div className="modal-header">
            <div className="min-w-0">
              {title && <h2 className="modal-title">{title}</h2>}
              {description && <p className="mt-1 text-sm text-gray-500 dark:text-dark-400">{description}</p>}
            </div>
            {showClose && onClose && (
              <button
                type="button"
                onClick={onClose}
                aria-label="Close"
                className="shrink-0 rounded-lg p-1.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-dark-800 dark:hover:text-gray-200"
              >
                <X className="h-5 w-5" />
              </button>
            )}
          </div>
        )}
        <div className="modal-body">{children}</div>
        {footer && <div className="modal-footer">{footer}</div>}
      </div>
    </div>,
    document.body
  )
}
