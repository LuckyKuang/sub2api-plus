import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Icon } from './Icon'
import { Modal } from './ui/Modal'

export interface DetailWorkspaceProps {
  show: boolean
  title: string
  workspace?: boolean
  children?: React.ReactNode
  detail?: React.ReactNode
  onClose?: () => void
}

export function DetailWorkspace({
  show,
  title,
  workspace = false,
  children,
  detail,
  onClose
}: DetailWorkspaceProps) {
  const { t } = useTranslation()
  const root = useRef<HTMLDivElement | null>(null)
  const panel = useRef<HTMLElement | null>(null)
  const [docked, setDocked] = useState(false)
  const origin = useRef<HTMLElement | null>(null)
  useEffect(() => {
    if (show) origin.current = document.activeElement as HTMLElement | null
    else {
      const frame = requestAnimationFrame(() => {
        if (origin.current?.isConnected) origin.current.focus({ preventScroll: true })
      })
      return () => cancelAnimationFrame(frame)
    }
  }, [show])

  useEffect(() => {
    if (show && docked) {
      const frame = requestAnimationFrame(() => panel.current?.focus({ preventScroll: true }))
      return () => cancelAnimationFrame(frame)
    }
  }, [show, docked])

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (show && docked && event.key === 'Escape' && !document.querySelector('[aria-modal="true"]')) {
        onClose?.()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [show, docked, onClose])

  useEffect(() => {
    const element = root.current
    if (!element || typeof ResizeObserver === 'undefined') return
    // Fits the 1,044px admin table + 440px detail + 24px gap, including card insets.
    const observer = new ResizeObserver(([entry]) => {
      setDocked(entry.contentRect.width >= 1520)
    })
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  return (
    <div
      ref={root}
      className={cn('detail-workspace', show && docked && 'detail-workspace-open', workspace && 'detail-workspace-contained')}
    >
      <div className="detail-workspace-content min-w-0">{children}</div>
      {show &&
        docked && (
          <aside
            ref={panel}
            tabIndex={-1}
            className="detail-workspace-panel card focus:outline-none"
            aria-label={title}
          >
            <div className="detail-workspace-header flex items-center justify-between gap-3 border-b border-gray-200 p-4 dark:border-dark-700">
              <h2 className="font-semibold">{title}</h2>
              <button
                type="button"
                className="btn btn-ghost btn-icon"
                aria-label={t('inspection.close')}
                onClick={() => onClose?.()}
              >
                <Icon name="x" size="md" />
              </button>
            </div>
            <div className="detail-workspace-detail min-h-0 overflow-y-auto overscroll-contain p-4">
              {detail}
            </div>
          </aside>
        )}
      <Modal
        open={show && !docked}
        title={title}
        variant="detail"
        onClose={() => onClose?.()}
      >
        {detail}
      </Modal>
    </div>
  )
}
