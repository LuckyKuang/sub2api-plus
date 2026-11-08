import { cn } from '@/lib/utils'

export interface TabItem {
  value: string
  label: React.ReactNode
  badge?: React.ReactNode
  disabled?: boolean
}

export interface TabsProps {
  items: TabItem[]
  value?: string
  onValueChange?: (value: string) => void
  className?: string
}

export function Tabs({ items, value, onValueChange, className }: TabsProps) {
  return (
    <div className={cn('tabs', className)} role="tablist">
      {items.map((item) => {
        const active = item.value === value
        return (
          <button
            key={item.value}
            type="button"
            role="tab"
            aria-selected={active}
            disabled={item.disabled}
            onClick={() => onValueChange?.(item.value)}
            className={cn('tab', active && 'tab-active', item.disabled && 'cursor-not-allowed opacity-50')}
          >
            {item.label}
            {item.badge !== undefined && item.badge !== null && (
              <span className="ml-1.5 text-xs opacity-70">{item.badge}</span>
            )}
          </button>
        )
      })}
    </div>
  )
}
