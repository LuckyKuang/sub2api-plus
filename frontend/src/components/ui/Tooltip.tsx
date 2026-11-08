import { useId, useState } from 'react'
import { HelpCircle } from 'lucide-react'
import { cn } from '@/lib/utils'

export interface HelpTooltipProps {
  content: React.ReactNode
  title?: React.ReactNode
  className?: string
  iconClassName?: string
}

export function HelpTooltip({ content, title, className, iconClassName }: HelpTooltipProps) {
  const [open, setOpen] = useState(false)
  const id = useId()

  return (
    <span className={cn('relative inline-flex', className)}>
      <button
        type="button"
        aria-label="Help"
        aria-describedby={open ? id : undefined}
        onClick={() => setOpen((value) => !value)}
        onMouseEnter={() => setOpen(true)}
        onMouseLeave={() => setOpen(false)}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        className="text-gray-400 transition-colors hover:text-primary-500"
      >
        <HelpCircle className={cn('h-4 w-4', iconClassName)} />
      </button>
      {open && (
        <span
          id={id}
          role="tooltip"
          className="surface-panel pointer-events-none absolute left-1/2 top-full z-[70] mt-2 w-64 -translate-x-1/2 rounded-xl border px-3 py-2 text-xs leading-relaxed text-gray-600 shadow-lg dark:text-gray-300"
        >
          {title && <span className="mb-1 block text-sm font-semibold text-gray-900 dark:text-white">{title}</span>}
          {content}
        </span>
      )}
    </span>
  )
}

export interface TitleTooltipProps {
  content: React.ReactNode
  children: React.ReactNode
  className?: string
  maxWidth?: number
}

/** Truncates its child with an ellipsis and reveals the full text on hover. */
export function TitleTooltip({ content, children, className, maxWidth = 320 }: TitleTooltipProps) {
  const id = useId()
  const [open, setOpen] = useState(false)

  return (
    <span
      className={cn('relative inline-block min-w-0 max-w-full', className)}
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
    >
      <span className="block truncate">{children}</span>
      {open && content && (
        <span
          id={id}
          role="tooltip"
          style={{ maxWidth }}
          className="surface-panel pointer-events-none absolute left-0 top-full z-[70] mt-1 break-words rounded-lg border px-2.5 py-1.5 text-xs leading-relaxed text-gray-600 shadow-lg dark:text-gray-300"
        >
          {content}
        </span>
      )}
    </span>
  )
}
