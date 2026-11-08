import { useEffect, useRef } from 'react'
import { createPortal } from 'react-dom'
import { cn } from '@/lib/utils'
import { Icon } from '@/components/Icon'

export interface ModalProps {
  open: boolean
  onClose?: () => void
  title?: React.ReactNode
  children?: React.ReactNode
  footer?: React.ReactNode
  size?: 'sm' | 'md' | 'lg' | 'xl' | 'full'
  /** Render as a right-side detail panel instead of a centred dialog. */
  variant?: 'dialog' | 'detail'
  showClose?: boolean
  closeOnOverlayClick?: boolean
  className?: string
}

/** Mirrors the Vue BaseDialog width guidance; the responsive steps matter
    because `w-full` below `sm` is what keeps narrow forms usable. */
const sizeClass = {
  sm: 'max-w-md',
  md: 'max-w-lg',
  lg: 'w-full sm:max-w-2xl md:max-w-3xl lg:max-w-4xl',
  xl: 'w-full sm:max-w-3xl md:max-w-4xl lg:max-w-5xl xl:max-w-6xl',
  full: 'w-full sm:max-w-4xl md:max-w-5xl lg:max-w-6xl xl:max-w-7xl'
} as const

export function Modal({
  open,
  onClose,
  title,
  children,
  footer,
  size = 'md',
  variant = 'dialog',
  showClose = true,
  closeOnOverlayClick = false,
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
          className
        )}
      >
        {(title || showClose) && (
          <div className="modal-header">
            {title && <h3 className="modal-title">{title}</h3>}
            <div className="flex shrink-0 items-center gap-2">
              {showClose && onClose && (
                <button
                  type="button"
                  onClick={onClose}
                  aria-label="Close"
                  className="-mr-2 shrink-0 rounded-xl p-2 transition-colors hover:surface-hover hover:text-gray-600 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/30 focus-visible:ring-offset-2 dark:hover:text-dark-300 dark:focus-visible:ring-offset-dark-900 surface-control surface-muted"
                >
                  <Icon name="x" size="md" />
                </button>
              )}
            </div>
          </div>
        )}
        <div className="modal-body">{children}</div>
        {footer && <div className="modal-footer">{footer}</div>}
      </div>
    </div>,
    document.body
  )
}
