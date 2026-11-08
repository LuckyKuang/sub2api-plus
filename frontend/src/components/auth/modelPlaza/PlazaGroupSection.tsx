import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Icon } from '../Icon'
import { GroupBadge } from './GroupBadge'
import { PlazaModelPricingTable } from './PlazaModelPricingTable'
import { useAppStore } from '@/stores/app'
import { platformBorderStrongClass } from '@/utils/platformColors'
import { formatPeakRateWindow, hasPeakRate, serverTimezoneLabel } from '@/utils/peak-rate'
import type { ModelPlazaGroup } from '@/api/modelPlaza'
import type { SubscriptionType } from '@/types'
import { cn } from '@/lib/utils'

export interface PlazaGroupSectionProps {
  group: ModelPlazaGroup
  className?: string
}

export function PlazaGroupSection({ group, className }: PlazaGroupSectionProps) {
  const { t } = useTranslation()
  const serverUtcOffset = useAppStore((state) => state.cachedPublicSettings?.server_utc_offset)

  /** 高峰窗口描述(含倍率与服务器时区标注);分组未启用高峰为空串。 */
  const peakWindow = useMemo(() => {
    if (!hasPeakRate(group)) return ''
    return formatPeakRateWindow(group, serverTimezoneLabel(serverUtcOffset))
  }, [group, serverUtcOffset])

  const peakNote = peakWindow
    ? t('modelPlaza.detail.peakNote', { window: peakWindow, multiplier: group.peak_rate_multiplier })
    : ''

  /**
   * 分组关闭了长上下文阶梯、但组内有模型官方带阶梯时提示:实付列只展示基础档,
   * 官方阶梯仅供参考。字段缺失(旧后端)不提示。
   */
  const longContextNote =
    group.long_context_pricing_enabled !== false
      ? ''
      : group.models.some((model) => (model.official_pricing?.intervals?.length ?? 0) > 1)
        ? t('modelPlaza.detail.longContextDisabledNote')
        : ''

  return (
    <section className={cn('glass-surface overflow-hidden border shadow-card', platformBorderStrongClass(group.platform), className)}>
      {/* 分组头部:名称/平台/倍率徽章/专属/订阅徽章 + 描述 */}
      <header className="border-b border-gray-100 px-5 py-4 dark:border-dark-700/60">
        <div className="flex flex-wrap items-center gap-2">
          <GroupBadge
            name={group.name}
            platform={group.platform}
            subscriptionType={(group.subscription_type || 'standard') as SubscriptionType}
            rateMultiplier={group.rate_multiplier}
            userRateMultiplier={group.user_rate_multiplier ?? null}
            peakRateEnabled={group.peak_rate_enabled}
            peakStart={group.peak_start}
            peakEnd={group.peak_end}
            peakRateMultiplier={group.peak_rate_multiplier}
            alwaysShowRate
          />
          {group.is_exclusive && (
            <span className="inline-flex items-center gap-1 rounded-md bg-purple-50/[0.15] px-2 py-0.5 text-xs font-medium text-purple-600 dark:bg-purple-900/20 dark:text-purple-400">
              <Icon name="shield" size="xs" className="h-3 w-3" />
              {t('modelPlaza.badges.exclusive')}
            </span>
          )}
          {group.subscription_type === 'subscription' && (
            <span className="inline-flex items-center rounded-md bg-violet-50/[0.15] px-2 py-0.5 text-xs font-medium text-violet-600 dark:bg-violet-900/20 dark:text-violet-400">
              {t('modelPlaza.badges.subscription')}
            </span>
          )}
        </div>
        {group.description && <p className="mt-2 text-sm surface-muted">{group.description}</p>}
        {peakNote && (
          <p className="mt-1.5 inline-flex items-center gap-1 text-xs text-amber-600 dark:text-amber-400">
            <Icon name="clock" size="xs" className="h-3 w-3" />
            {peakNote}
          </p>
        )}
        {longContextNote && (
          <p className="mt-1.5 flex items-center gap-1 text-xs surface-muted">
            <Icon name="infoCircle" size="xs" className="h-3 w-3" />
            {longContextNote}
          </p>
        )}
      </header>

      {/* 模型价格表:整行(含 hover 底色/分区底色)顶到卡片边缘,左右留白由表格首列/末列的 padding 提供 */}
      <div>
        {group.models.length > 0 ? (
          <PlazaModelPricingTable
            models={group.models}
            platform={group.platform}
            rateMultiplier={group.rate_multiplier}
            userRateMultiplier={group.user_rate_multiplier ?? null}
            imageRateIndependent={group.image_rate_independent}
            imageRateMultiplier={group.image_rate_multiplier}
            videoRateIndependent={group.video_rate_independent}
            videoRateMultiplier={group.video_rate_multiplier}
            peakWindow={peakWindow}
            peakRateMultiplier={group.peak_rate_multiplier}
          />
        ) : (
          <p className="px-5 py-4 text-center text-sm surface-muted">{t('modelPlaza.detail.noModels')}</p>
        )}
      </div>
    </section>
  )
}

export default PlazaGroupSection
