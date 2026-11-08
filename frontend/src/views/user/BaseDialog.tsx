import { useEffect, useRef } from 'react'
import { Modal } from '@/components/ui'

export type DialogWidth = 'narrow' | 'normal' | 'wide' | 'extra-wide' | 'full'

export interface BaseDialogProps {
  show: boolean
  title: string
  width?: DialogWidth
  placement?: 'center' | 'right'
  closeOnEscape?: boolean
  closeOnClickOutside?: boolean
  showCloseButton?: boolean
  footer?: React.ReactNode
  children?: React.ReactNode
  onClose?: () => void
}

const widthClasses: Record<DialogWidth, string> = {
  narrow: 'max-w-md',
  normal: 'max-w-lg',
  wide: 'w-full sm:max-w-2xl md:max-w-3xl lg:max-w-4xl',
  'extra-wide': 'w-full sm:max-w-3xl md:max-w-4xl lg:max-w-5xl xl:max-w-6xl',
  full: 'w-full sm:max-w-4xl md:max-w-5xl lg:max-w-6xl xl:max-w-7xl'
}

export function BaseDialog({
  show,
  title,
  width = 'normal',
  placement = 'center',
  closeOnEscape = true,
  closeOnClickOutside = false,
  showCloseButton = true,
  footer,
  children,
  onClose
}: BaseDialogProps) {
  const bodyRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (show && bodyRef.current) bodyRef.current.scrollTop = 0
  }, [show])

  return (
    <Modal
      open={show}
      onClose={closeOnEscape ? onClose : undefined}
      title={title}
      size="md"
      variant={placement === 'right' ? 'detail' : 'dialog'}
      showClose={showCloseButton}
      closeOnOverlayClick={closeOnClickOutside}
      footer={footer}
      className={placement === 'right' ? undefined : widthClasses[width]}
    >
      <div ref={bodyRef}>{children}</div>
    </Modal>
  )
}

export default BaseDialog
