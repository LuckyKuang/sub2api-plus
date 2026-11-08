import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth'
import type { Group, GroupPlatform } from '@/types'
import { Icon } from '@/views/admin/AccountIcon'
import { GroupBadge } from './GroupBadge'

export interface GroupSelectorProps {
  value: number[]
  groups: (Group & { account_count?: number })[]
  platform?: GroupPlatform
  mixedScheduling?: boolean
  searchable?: boolean | 'auto'
  onValueChange: (value: number[]) => void
}

/** Multi-select group picker ported from the Vue GroupSelector. */
export function GroupSelector({
  value,
  groups,
  platform,
  mixedScheduling = false,
  searchable = 'auto',
  onValueChange
}: GroupSelectorProps) {
  const { t } = useTranslation()
  const isSimpleMode = useAuthStore((s) => s.isSimpleMode())
  const [searchText, setSearchText] = useState('')

  const isSearchable = searchable === 'auto' ? groups.length > 5 : searchable

  const filteredGroups = useMemo(() => {
    let result = isSimpleMode ? groups.filter((g) => g.platform !== 'composite') : groups
    if (platform) {
      if (platform === 'antigravity' && mixedScheduling) {
        result = result.filter(
          (g) =>
            g.platform === 'antigravity' ||
            g.platform === 'anthropic' ||
            g.platform === 'gemini' ||
            g.platform === 'composite'
        )
      } else {
        result = result.filter((g) => g.platform === platform || g.platform === 'composite')
      }
    }
    if (isSearchable && searchText) {
      const q = searchText.toLowerCase()
      result = result.filter((g) => g.name.toLowerCase().includes(q) || g.description?.toLowerCase().includes(q))
    }
    return result
  }, [groups, isSimpleMode, platform, mixedScheduling, isSearchable, searchText])

  // Simple mode hides composite groups; drop already-selected composite ids.
  const cleaned = useMemo(() => {
    if (!isSimpleMode || groups.length === 0) return value
    const visibleIDs = new Set(groups.filter((group) => group.platform !== 'composite').map((group) => group.id))
    return value.filter((id) => visibleIDs.has(id))
  }, [isSimpleMode, groups, value])

  if (cleaned.length !== value.length) {
    onValueChange(cleaned)
  }

  const handleChange = (groupId: number, checked: boolean) => {
    onValueChange(checked ? [...value, groupId] : value.filter((id) => id !== groupId))
  }

  return (
    <div>
      <label className="input-label">
        {t('admin.users.groups')}
        <span className="font-normal surface-muted">{t('common.selectedCount', { count: value.length })}</span>
      </label>
      {isSearchable && (
        <div className="flex items-center gap-2 rounded-t-lg border border-b-0 border-gray-200 surface-inset px-3 py-2 dark:border-dark-600">
          <Icon name="search" size="sm" className="shrink-0 text-gray-400" />
          <input
            value={searchText}
            type="text"
            placeholder={t('common.searchPlaceholder')}
            className="flex-1 bg-transparent text-sm text-gray-900 placeholder:text-gray-400 focus:outline-none dark:text-gray-100 dark:placeholder:text-dark-400"
            onChange={(event) => setSearchText(event.target.value)}
          />
        </div>
      )}
      <div
        className={cn(
          'grid max-h-32 grid-cols-2 gap-1 overflow-y-auto p-2',
          isSearchable
            ? 'rounded-b-lg border border-t-0 border-gray-200 surface-inset dark:border-dark-600 dark:surface-inset'
            : 'rounded-lg border border-gray-200 surface-inset dark:border-dark-600 dark:surface-inset'
        )}
      >
        {filteredGroups.map((group) => (
          <label
            key={group.id}
            className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 transition-colors hover:surface-hover"
            title={
              group.rate_multiplier == null
                ? group.name
                : t('admin.groups.rateAndAccounts', {
                    rate: group.rate_multiplier,
                    count: group.account_count || 0
                  })
            }
          >
            <input
              type="checkbox"
              value={group.id}
              checked={value.includes(group.id)}
              className="h-3.5 w-3.5 shrink-0 rounded border-gray-300 text-primary-500 focus:ring-primary-500 dark:border-dark-500"
              onChange={(event) => handleChange(group.id, event.target.checked)}
            />
            <GroupBadge
              name={group.name}
              platform={group.platform}
              subscriptionType={group.subscription_type || undefined}
              rateMultiplier={group.rate_multiplier == null ? undefined : group.rate_multiplier}
              className="min-w-0 flex-1"
            />
            <span className="shrink-0 text-xs surface-muted">{group.account_count || 0}</span>
          </label>
        ))}
        {filteredGroups.length === 0 && (
          <div className="col-span-2 py-2 text-center text-sm surface-muted">{t('common.noGroupsAvailable')}</div>
        )}
      </div>
    </div>
  )
}

export default GroupSelector
