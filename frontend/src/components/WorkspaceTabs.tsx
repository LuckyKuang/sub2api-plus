import { useRef } from 'react'

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
  /** Render inside a parent card instead of as its own card. */
  embedded?: boolean
  onValueChange?: (value: string) => void
}

export function WorkspaceTabs({
  value,
  tabs,
  label,
  embedded = false,
  onValueChange
}: WorkspaceTabsProps) {
  const navigation = useRef<HTMLDivElement | null>(null)

  const navigate = (event: React.KeyboardEvent, tabValue: string) => {
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
    onValueChange?.(enabled[next].value)
    requestAnimationFrame(() => {
      navigation.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')[next]?.focus()
    })
  }

  return (
    <div
      ref={navigation}
      className={
        embedded
          ? 'workspace-tabs workspace-tabs--embedded workspace-fixed'
          : 'card workspace-tabs workspace-fixed'
      }
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
          onClick={() => onValueChange?.(tab.value)}
          onKeyDown={(event) => navigate(event, tab.value)}
        >
          {tab.label}
        </button>
      ))}
    </div>
  )
}
