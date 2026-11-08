import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth'
import { cn } from '@/lib/utils'
import type { Group, GroupPlatform } from '@/types'
import { Icon } from './Icon'
import { GroupBadge } from './badges'

export interface GroupSelectorProps {
  value: number[]
  groups: Array<Group & { account_count?: number }>
  platform?: GroupPlatform // Optional platform filter
  mixedScheduling?: boolean // For antigravity accounts: allow anthropic/gemini groups
  searchable?: boolean | 'auto'
  className?: string
  onValueChange?: (value: number[]) => void
}

export function GroupSelector({
  value,
  groups,
  platform,
  mixedScheduling = false,
  searchable = 'auto',
  className,
  onValueChange
}: GroupSelectorProps) {
  const { t } = useTranslation()
  const [searchText, setSearchText] = useState('')
  const isSimpleMode = useAuthStore((s) => s.isSimpleMode())

  const isSearchable = searchable === 'auto' ? groups.length > 5 : searchable

  // Filter groups by platform if specified
  const filteredGroups = useMemo(() => {
    let result = isSimpleMode ? groups.filter((g) => g.platform !== 'composite') : groups
    if (platform) {
      // antigravity 账户启用混合调度后，可选择 anthropic/gemini 分组
      if (platform === 'antigravity' && mixedScheduling) {
        result = result.filter(
          (g) => g.platform === 'antigravity' || g.platform === 'anthropic' || g.platform === 'gemini' || g.platform === 'composite'
        )
      } else {
        // 默认：只能选择同 platform 的分组；composite 分组可接收任意具体平台账号
        result = result.filter((g) => g.platform === platform || g.platform === 'composite')
      }
    }
    if (isSearchable && searchText) {
      const q = searchText.toLowerCase()
      result = result.filter(
        (g) => g.name.toLowerCase().includes(q) || g.description?.toLowerCase().includes(q)
      )
    }
    return result
  }, [groups, isSimpleMode, platform, mixedScheduling, isSearchable, searchText])

  useEffect(() => {
    if (!isSimpleMode || groups.length === 0) return
    const visibleIDs = new Set(
      groups.filter((group) => group.platform !== 'composite').map((group) => group.id)
    )
    const cleaned = value.filter((id) => visibleIDs.has(id))
    if (cleaned.length !== value.length) onValueChange?.(cleaned)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isSimpleMode, groups, value])

  const handleChange = (groupId: number, checked: boolean) => {
    const newValue = checked ? [...value, groupId] : value.filter((id) => id !== groupId)
    onValueChange?.(newValue)
  }

  return (
    <div className={className}>
      <label className="input-label">
        {t('admin.users.groups')}
        <span className="font-normal surface-muted">{t('common.selectedCount', { count: value.length })}</span>
      </label>
      {isSearchable && (
        <div className="flex items-center gap-2 rounded-t-lg border border-b-0 border-gray-200 surface-inset px-3 py-2 dark:border-dark-600">
          <Icon name="search" size="sm" className="shrink-0 text-gray-400" />
          <input
            value={searchText}
            onChange={(event) => setSearchText(event.target.value)}
            type="text"
            placeholder={t('common.searchPlaceholder')}
            className="flex-1 bg-transparent text-sm text-gray-900 placeholder:text-gray-400 focus:outline-none dark:text-gray-100 dark:placeholder:text-dark-400"
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
              onChange={(event) => handleChange(group.id, event.target.checked)}
              className="h-3.5 w-3.5 shrink-0 rounded border-gray-300 text-primary-500 focus:ring-primary-500 dark:border-dark-500"
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
          <div className="col-span-2 py-2 text-center text-sm surface-muted">
            {t('common.noGroupsAvailable')}
          </div>
        )}
      </div>
    </div>
  )
}
