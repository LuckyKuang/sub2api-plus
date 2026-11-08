import { useRef } from 'react'
import { cn } from '@/lib/utils'

export interface WorkspaceTabItem<T extends string> {
  value: T
  label: string
  panelId?: string
  disabled?: boolean
}

export interface WorkspaceTabsProps<T extends string> {
  value: T
  tabs: WorkspaceTabItem<T>[]
  label: string
  className?: string
  onValueChange: (value: T) => void
}

export function WorkspaceTabs<T extends string>({
  value,
  tabs,
  label,
  className,
  onValueChange,
}: WorkspaceTabsProps<T>) {
  const navigation = useRef<HTMLDivElement | null>(null)

  function navigate(event: React.KeyboardEvent<HTMLButtonElement>, tabValue: T) {
    if (event.altKey || event.ctrlKey || event.metaKey) return
    const enabled = tabs.filter(tab => !tab.disabled)
    const index = enabled.findIndex(tab => tab.value === tabValue)
    let next: number
    switch (event.key) {
      case 'ArrowRight': next = (index + 1) % enabled.length; break
      case 'ArrowLeft': next = (index - 1 + enabled.length) % enabled.length; break
      case 'Home': next = 0; break
      case 'End': next = enabled.length - 1; break
      default: return
    }
    if (!enabled.length) return
    event.preventDefault()
    onValueChange(enabled[next].value)
    navigation.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')[next]?.focus()
  }

  return (
    <div
      ref={navigation}
      className={cn('card workspace-tabs workspace-fixed', className)}
      role="tablist"
      aria-label={label}
    >
      {tabs.map((tab) => (
        <button
          key={tab.value}
          type="button"
          role="tab"
          className="workspace-tab"
          aria-selected={value === tab.value}
          aria-controls={tab.panelId}
          disabled={tab.disabled}
          tabIndex={value === tab.value ? 0 : -1}
          onClick={() => onValueChange(tab.value)}
          onKeyDown={(event) => navigate(event, tab.value)}
        >{tab.label}</button>
      ))}
    </div>
  )
}

export default WorkspaceTabs
