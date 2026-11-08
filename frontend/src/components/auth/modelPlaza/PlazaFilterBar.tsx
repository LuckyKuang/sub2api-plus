import { useTranslation } from 'react-i18next'
import { Icon } from '../Icon'
import { PlatformIcon } from './PlatformIcon'
import { platformAccentColor } from '@/utils/platformColors'
import type { GroupPlatform } from '@/types'
import { cn } from '@/lib/utils'

export interface PlazaFilterBarProps {
  /** 数据中出现的平台(去重排序后)。 */
  platforms: string[]
  /** 全量分组(含平台与生效倍率),三个维度的置灰联动由此推导。 */
  groups: Array<{ id: number; name: string; platform: string; rate: number }>
  /** 全量生效倍率去重升序。 */
  rates: number[]
  platform: string
  groupId: number | 'all'
  rate: number | 'all'
  /** 模型名搜索词(纯前端过滤)。 */
  search: string
  className?: string
  onPlatformChange?: (value: string) => void
  onGroupIdChange?: (value: number | 'all') => void
  onRateChange?: (value: number | 'all') => void
  onSearchChange?: (value: string) => void
}

/**
 * 三个维度互为约束(faceted):某选项可点 ⟺ 在「其他两维」当前选择下仍有分组命中。
 * 「全部」永远可点,作为解除本维约束的出口;可点项组合恒有结果,无需选择修正。
 */
function platformEnabled(
  groups: PlazaFilterBarProps['groups'],
  platform: string,
  groupId: number | 'all',
  rate: number | 'all'
): boolean {
  return groups.some(
    (group) =>
      group.platform === platform &&
      (groupId === 'all' || group.id === groupId) &&
      (rate === 'all' || group.rate === rate)
  )
}

function groupEnabled(
  group: { platform: string; rate: number },
  platform: string,
  rate: number | 'all'
): boolean {
  return (platform === 'all' || group.platform === platform) && (rate === 'all' || group.rate === rate)
}

function rateEnabled(
  groups: PlazaFilterBarProps['groups'],
  target: number,
  platform: string,
  groupId: number | 'all'
): boolean {
  return groups.some(
    (group) =>
      group.rate === target &&
      (platform === 'all' || group.platform === platform) &&
      (groupId === 'all' || group.id === groupId)
  )
}

function chipClass(active: boolean): string {
  return active
    ? 'bg-gradient-to-r from-primary-500 to-primary-600 text-white shadow-sm shadow-primary-500/30'
    : 'surface-inset surface-muted ring-1 ring-inset ring-gray-200 enabled:hover:bg-gray-50 enabled:hover:text-gray-900 enabled:hover:ring-gray-300 dark:surface-inset dark:ring-dark-700 dark:enabled:hover:bg-dark-800 dark:enabled:hover:text-white'
}

export function PlazaFilterBar({
  platforms,
  groups,
  rates,
  platform,
  groupId,
  rate,
  search,
  className,
  onPlatformChange,
  onGroupIdChange,
  onRateChange,
  onSearchChange
}: PlazaFilterBarProps) {
  const { t } = useTranslation()

  return (
    <div className={cn('card space-y-3 p-4 sm:p-5', className)}>
      {/* 一级:平台 */}
      <div className="flex items-start gap-2">
        <span className="w-10 shrink-0 pt-2 text-xs font-semibold uppercase tracking-wider surface-muted">
          {t('modelPlaza.filters.platformLabel')}
        </span>
        <div className="flex flex-wrap items-center gap-2">
          {['all', ...platforms].map((candidate) => (
            <button
              key={`platform-${candidate}`}
              type="button"
              className={cn(
                'inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition disabled:cursor-not-allowed disabled:opacity-40 disabled:grayscale surface-control',
                candidate === 'all'
                  ? chipClass(platform === 'all')
                  : platform === candidate
                    ? 'chip-tinted-active'
                    : 'chip-tinted'
              )}
              style={candidate === 'all' ? undefined : { '--chip-accent': platformAccentColor(candidate) } as React.CSSProperties}
              disabled={candidate !== 'all' && !platformEnabled(groups, candidate, groupId, rate)}
              onClick={() => onPlatformChange?.(candidate)}
            >
              {candidate !== 'all' && <PlatformIcon platform={candidate as GroupPlatform} size="xs" />}
              {candidate === 'all' ? t('modelPlaza.filters.all') : candidate}
            </button>
          ))}
        </div>
      </div>

      {/* 二级:分组(按所属平台着色,当前组合下无结果的置灰) */}
      <div className="flex items-start gap-2">
        <span className="w-10 shrink-0 pt-2 text-xs font-semibold uppercase tracking-wider surface-muted">
          {t('modelPlaza.filters.groupLabel')}
        </span>
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            className={cn('rounded-lg px-3 py-1.5 text-sm font-medium transition surface-control', chipClass(groupId === 'all'))}
            onClick={() => onGroupIdChange?.('all')}
          >
            {t('modelPlaza.filters.all')}
          </button>
          {groups.map((group) => (
            <button
              key={`group-${group.id}`}
              type="button"
              className={cn(
                'rounded-lg px-3 py-1.5 text-sm font-medium transition disabled:cursor-not-allowed disabled:opacity-40 disabled:grayscale surface-control',
                groupId === group.id ? 'chip-tinted-active' : 'chip-tinted'
              )}
              style={{ '--chip-accent': platformAccentColor(group.platform) } as React.CSSProperties}
              disabled={!groupEnabled(group, platform, rate)}
              onClick={() => onGroupIdChange?.(group.id)}
            >
              {group.name}
            </button>
          ))}
        </div>
      </div>

      {/* 三级:倍率(当前组合下不存在的置灰) */}
      <div className="flex items-start gap-2">
        <span className="w-10 shrink-0 pt-2 text-xs font-semibold uppercase tracking-wider surface-muted">
          {t('modelPlaza.filters.rateLabel')}
        </span>
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            className={cn('rounded-lg px-3 py-1.5 text-sm font-medium transition surface-control', chipClass(rate === 'all'))}
            onClick={() => onRateChange?.('all')}
          >
            {t('modelPlaza.filters.all')}
          </button>
          {rates.map((candidate) => (
            <button
              key={`rate-${candidate}`}
              type="button"
              className={cn(
                'rounded-lg px-3 py-1.5 font-mono text-sm font-medium transition disabled:cursor-not-allowed disabled:opacity-40 disabled:grayscale surface-control',
                chipClass(rate === candidate)
              )}
              disabled={!rateEnabled(groups, candidate, platform, groupId)}
              onClick={() => onRateChange?.(candidate)}
            >
              {candidate}x
            </button>
          ))}
        </div>
      </div>

      {/* 四级:模型名搜索(纯前端过滤) */}
      <div className="flex flex-wrap items-start gap-2">
        <span className="w-10 shrink-0 pt-2 text-xs font-semibold uppercase tracking-wider surface-muted">
          {t('modelPlaza.filters.modelLabel')}
        </span>
        <div className="relative w-full sm:w-72">
          <Icon name="search" size="sm" className="absolute left-3 top-1/2 -translate-y-1/2 surface-muted" />
          <input
            value={search}
            type="text"
            placeholder={t('modelPlaza.filters.searchPlaceholder')}
            className="input rounded-lg py-1.5 pl-9 pr-9"
            onChange={(event) => onSearchChange?.(event.target.value)}
          />
          {search && (
            <button
              type="button"
              className="absolute right-2.5 top-1/2 -translate-y-1/2 transition-colors hover:text-gray-600 dark:hover:text-gray-300 surface-control surface-muted"
              onClick={() => onSearchChange?.('')}
            >
              <Icon name="x" size="xs" className="h-3.5 w-3.5" />
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

export default PlazaFilterBar
