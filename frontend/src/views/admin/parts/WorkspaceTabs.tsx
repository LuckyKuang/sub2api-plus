import { useRef } from 'react'
import { cn } from '@/lib/utils'

export interface WorkspaceTab {
  value: string
  label: string
  panelId?: string
  disabled?: boolean
}

export interface WorkspaceTabsProps {
  value: string
  tabs: WorkspaceTab[]
  label: string
  embedded?: boolean
  onValueChange: (value: string) => void
}

export function WorkspaceTabs({ value, tabs, label, embedded = false, onValueChange }: WorkspaceTabsProps) {
  const navigationRef = useRef<HTMLDivElement>(null)

  function navigate(event: React.KeyboardEvent, tabValue: string) {
    if (event.altKey || event.ctrlKey || event.metaKey) return
    const enabled = tabs.filter((tab) => !tab.disabled)
    const index = enabled.findIndex((tab) => tab.value === tabValue)
    let next: number
    switch (event.key) {
      case 'ArrowRight':
        next = (index + 1) % enabled.length
        break
      case 'ArrowLeft':
        next = (index - 1 + enabled.length) % enabled.length
        break
      case 'Home':
        next = 0
        break
      case 'End':
        next = enabled.length - 1
        break
      default:
        return
    }
    if (!enabled.length) return
    event.preventDefault()
    onValueChange(enabled[next].value)
    requestAnimationFrame(() => {
      navigationRef.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')[next]?.focus()
    })
  }

  return (
    <div
      ref={navigationRef}
      className={cn(
        'workspace-tabs workspace-fixed',
        embedded ? 'workspace-tabs--embedded' : 'card'
      )}
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
        >
          {tab.label}
        </button>
      ))}
    </div>
  )
}

export default WorkspaceTabs
