import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import type { Group } from '@/types'
import { AccountGroupBadge } from './AccountGroupBadge'
import { Icon } from './AccountIcon'

export interface AccountGroupsCellProps {
  groups: Group[] | null | undefined
  maxDisplay?: number
}

export function AccountGroupsCell({ groups, maxDisplay = 4 }: AccountGroupsCellProps) {
  const { t } = useTranslation()
  const moreButtonRef = useRef<HTMLButtonElement | null>(null)
  const [showPopover, setShowPopover] = useState(false)

  const displayGroups = useMemo(() => {
    if (!groups) return []
    if (groups.length <= maxDisplay) return groups
    return groups.slice(0, maxDisplay - 1)
  }, [groups, maxDisplay])

  const hiddenCount = useMemo(() => {
    if (!groups || groups.length <= maxDisplay) return 0
    return groups.length - (maxDisplay - 1)
  }, [groups, maxDisplay])

  const popoverStyle = useMemo(() => {
    if (!moreButtonRef.current) return {}
    const rect = moreButtonRef.current.getBoundingClientRect()
    const viewportHeight = window.innerHeight
    const viewportWidth = window.innerWidth
    let top = rect.bottom + 8
    let left = rect.left

    if (top + 280 > viewportHeight) top = Math.max(8, rect.top - 280)
    if (left + 384 > viewportWidth) left = Math.max(8, viewportWidth - 392)

    return { top: `${top}px`, left: `${left}px` }
    // Recompute whenever the popover opens.
  }, [showPopover])

  useEffect(() => {
    const handleKeydown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setShowPopover(false)
    }
    window.addEventListener('keydown', handleKeydown)
    return () => window.removeEventListener('keydown', handleKeydown)
  }, [])

  if (!groups || groups.length === 0) {
    return <span className="text-sm surface-muted">-</span>
  }

  return (
    <div className="relative w-56">
      <div data-test="group-summary" className="flex max-h-14 flex-wrap gap-1 overflow-hidden">
        {displayGroups.map((group) => (
          <AccountGroupBadge
            key={group.id}
            name={group.name}
            platform={group.platform}
            subscriptionType={group.subscription_type}
            rateMultiplier={group.rate_multiplier}
            showRate={false}
            title={group.name}
            className="max-w-24"
          />
        ))}
        {hiddenCount > 0 && (
          <button
            ref={moreButtonRef}
            type="button"
            className="inline-flex cursor-pointer items-center whitespace-nowrap rounded-md px-1.5 py-0.5 text-xs font-medium transition-colors hover:surface-hover dark:hover:bg-dark-500 surface-control surface-muted"
            aria-expanded={showPopover}
            aria-label={t('admin.accounts.groupCountTotal', { count: groups.length })}
            onClick={(event) => {
              event.stopPropagation()
              setShowPopover((prev) => !prev)
            }}
          >
            +{hiddenCount}
          </button>
        )}
      </div>

      {showPopover &&
        createPortal(
          <div
            className="fixed z-50 min-w-48 max-w-96 rounded-lg border border-gray-200 p-3 shadow-lg dark:border-dark-600 surface-panel"
            style={popoverStyle}
            role="dialog"
          >
            <div className="mb-2 flex items-center justify-between gap-3">
              <span className="text-xs font-medium surface-muted">
                {t('admin.accounts.groupCountTotal', { count: groups.length })}
              </span>
              <button
                type="button"
                className="rounded p-0.5 hover:surface-hover hover:text-gray-600 dark:hover:text-gray-300 surface-control surface-muted"
                aria-label={t('common.close')}
                onClick={() => setShowPopover(false)}
              >
                <Icon name="x" size="sm" />
              </button>
            </div>
            <div className="flex max-h-64 flex-wrap gap-1.5 overflow-y-auto">
              {groups.map((group) => (
                <AccountGroupBadge
                  key={group.id}
                  name={group.name}
                  platform={group.platform}
                  subscriptionType={group.subscription_type}
                  rateMultiplier={group.rate_multiplier}
                  showRate={false}
                  title={group.name}
                  truncate={false}
                  className="min-w-0 max-w-full"
                />
              ))}
            </div>
          </div>,
          document.body
        )}

      {showPopover && <div className="fixed inset-0 z-40" onClick={() => setShowPopover(false)} />}
    </div>
  )
}

export default AccountGroupsCell
