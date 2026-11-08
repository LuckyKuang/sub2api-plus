import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { Icon } from '../Icon'
import { PlazaFilterBar } from './PlazaFilterBar'
import { PlazaGroupSection } from './PlazaGroupSection'
import { useAuthStore } from '@/stores/auth'
import type { ModelPlazaGroup, ModelPlazaResponse } from '@/api/modelPlaza'
import { cn } from '@/lib/utils'

marked.setOptions({
  breaks: true,
  gfm: true
})

export interface ModelPlazaContentProps {
  response: ModelPlazaResponse | null
  loading: boolean
  error?: boolean
  /** 后台内嵌形态(AppLayout 内):隐藏页头。 */
  embedded?: boolean
}

/** 生效倍率 = 用户专属倍率 ?? 分组默认倍率。 */
function effectiveRate(group: ModelPlazaGroup): number {
  return group.user_rate_multiplier ?? group.rate_multiplier
}

export function ModelPlazaContent({ response, loading, error = false, embedded = false }: ModelPlazaContentProps) {
  const { t } = useTranslation()
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated())

  const [selectedPlatform, setSelectedPlatform] = useState<string>('all')
  const [selectedGroupId, setSelectedGroupId] = useState<number | 'all'>('all')
  const [selectedRate, setSelectedRate] = useState<number | 'all'>('all')
  const [searchQuery, setSearchQuery] = useState('')

  const searchActive = searchQuery.trim() !== ''

  const descriptionHtml = useMemo(() => {
    const markdown = response?.description?.trim()
    if (!markdown) return ''
    return DOMPurify.sanitize(marked.parse(markdown) as string)
  }, [response])

  const platforms = useMemo(
    () => [...new Set((response?.groups ?? []).map((group) => group.platform).filter(Boolean))].sort(),
    [response]
  )

  const groupOptions = useMemo(
    () =>
      (response?.groups ?? []).map((group) => ({
        id: group.id,
        name: group.name,
        platform: group.platform,
        rate: effectiveRate(group)
      })),
    [response]
  )

  /** 全量生效倍率;当前组合下不可用的项由 FilterBar 置灰而非隐藏。 */
  const rates = useMemo(
    () => [...new Set((response?.groups ?? []).map(effectiveRate))].sort((a, b) => a - b),
    [response]
  )

  /** 数据刷新后选中的倍率可能不复存在,重置为全部。 */
  useEffect(() => {
    if (selectedRate !== 'all' && !rates.includes(selectedRate)) {
      setSelectedRate('all')
    }
  }, [rates, selectedRate])

  const filteredGroups = useMemo(() => {
    let groups = response?.groups ?? []
    if (selectedPlatform !== 'all') {
      groups = groups.filter((group) => group.platform === selectedPlatform)
    }
    if (selectedGroupId !== 'all') {
      groups = groups.filter((group) => group.id === selectedGroupId)
    }
    if (selectedRate !== 'all') {
      groups = groups.filter((group) => effectiveRate(group) === selectedRate)
    }
    // 模型名搜索:分组内只留命中的模型,整组无命中则隐藏该分组。
    const query = searchQuery.trim().toLowerCase()
    if (query) {
      groups = groups
        .map((group) => ({ ...group, models: group.models.filter((model) => model.name.toLowerCase().includes(query)) }))
        .filter((group) => group.models.length > 0)
    }
    // 专属倍率会改变生效值,不能只依赖后端按默认倍率的排序。
    return [...groups].sort((a, b) => effectiveRate(a) - effectiveRate(b) || a.name.localeCompare(b.name))
  }, [response, searchQuery, selectedGroupId, selectedPlatform, selectedRate])

  return (
    <div className="flex min-w-0 flex-col gap-[var(--shell-gap)]">
      {/* 页头(独立形态下展示标题;后台形态 AppHeader 已有页面标题) */}
      {!embedded && (
        <div className="card px-5 py-5 sm:px-6">
          <h1 className="text-2xl font-bold tracking-tight text-gray-900 dark:text-white sm:text-3xl">
            {t('modelPlaza.title')}
          </h1>
          <p className="mt-1.5 text-sm surface-muted">{t('modelPlaza.description')}</p>
        </div>
      )}

      {/* 全局价格说明(管理员配置,Markdown) */}
      {descriptionHtml && (
        <div
          className={cn(
            'glass-surface plaza-description border px-5 py-4 text-sm shadow-card dark:border-dark-700/50',
            embedded && 'workspace-shell'
          )}
          dangerouslySetInnerHTML={{ __html: descriptionHtml }}
        />
      )}

      {/* 未登录提示 */}
      {!isAuthenticated && (
        <p className={cn('card flex items-center gap-1.5 px-5 py-3 text-xs surface-muted', embedded && 'workspace-shell')}>
          <Icon name="infoCircle" size="xs" className="h-3.5 w-3.5" />
          {t('modelPlaza.anonymousHint')}
        </p>
      )}

      {/* 加载/错误/空 */}
      {loading ? (
        <div className={cn('card flex min-h-[240px] items-center justify-center', embedded && 'workspace-shell')}>
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-primary-600/25 border-t-primary-600 dark:border-primary-400/25 dark:border-t-primary-400" />
        </div>
      ) : error ? (
        <div
          className={cn(
            'card border-red-200 px-5 py-8 text-center text-sm text-red-600 dark:border-red-500/30 dark:text-red-300',
            embedded && 'workspace-shell'
          )}
        >
          {t('modelPlaza.loadFailed')}
        </div>
      ) : (
        <>
          {/* 筛选区:平台 → 分组 → 倍率 */}
          <PlazaFilterBar
            className={cn(embedded && 'workspace-shell')}
            platforms={platforms}
            groups={groupOptions}
            rates={rates}
            platform={selectedPlatform}
            groupId={selectedGroupId}
            rate={selectedRate}
            search={searchQuery}
            onPlatformChange={setSelectedPlatform}
            onGroupIdChange={setSelectedGroupId}
            onRateChange={setSelectedRate}
            onSearchChange={setSearchQuery}
          />

          {/* 分组分节的模型清单(默认按生效倍率升序) */}
          {filteredGroups.length > 0 ? (
            <div className="flex flex-col gap-[var(--shell-gap)]">
              {filteredGroups.map((group) => (
                <PlazaGroupSection key={group.id} group={group} />
              ))}
            </div>
          ) : (
            <div
              className={cn(
                'card border-dashed border-gray-300 px-5 py-12 text-center text-sm dark:border-dark-600 surface-muted',
                embedded && 'workspace-shell'
              )}
            >
              {searchActive ? t('modelPlaza.noSearchResult') : t('modelPlaza.empty')}
            </div>
          )}
        </>
      )}
    </div>
  )
}

export default ModelPlazaContent
