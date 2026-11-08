import { cn } from '@/lib/utils'

export interface TriggerTooltipProps {
  content: React.ReactNode
  widthClass?: string
  children: React.ReactNode
}

/**
 * Hover tooltip wrapping a custom trigger (Vue HelpTooltip #trigger slot
 * equivalent): the panel follows the trigger and stays inside the viewport.
 */
export function TriggerTooltip({ content, widthClass, children }: TriggerTooltipProps) {
  return (
    <span className="group/help relative inline-flex">
      {children}
      <span
        className={cn(
          'pointer-events-none absolute left-0 top-full z-[100] mt-1 hidden rounded-[var(--surface-radius-panel)] px-3 py-2 text-xs text-white opacity-0 shadow-xl transition-opacity duration-200 group-hover/help:block group-hover/help:opacity-100 surface-panel',
          widthClass
        )}
      >
        {content}
      </span>
    </span>
  )
}

export default TriggerTooltip
