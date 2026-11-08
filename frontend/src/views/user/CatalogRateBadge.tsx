import { useTranslation } from 'react-i18next'
import type { CatalogGroup, CatalogOffer } from '@/api/channels'
import { effectiveRate, rateSource } from './catalogQuote'

export interface CatalogRateBadgeProps {
  group: CatalogGroup
  offer: CatalogOffer
}

export function CatalogRateBadge({ group, offer }: CatalogRateBadgeProps) {
  const { t } = useTranslation()
  const rate = effectiveRate(group, offer)
  const source = rateSource(group, offer)

  return (
    <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
      <strong
        className={cnRate(source)}
      >
        {Number.isFinite(rate) ? t(`availableChannels.catalog.rateLabels.${source}`, { rate }) : t('billingRates.unknown')}
      </strong>
      {source !== 'group' && (
        <span className="text-xs surface-muted">{t('availableChannels.catalog.defaultRate', { rate: group.rate_multiplier })}</span>
      )}
      {source === 'media' && group.user_rate_multiplier != null && (
        <span className="text-xs surface-muted">{t('availableChannels.catalog.overriddenPersonalRate', { rate: group.user_rate_multiplier })}</span>
      )}
      {offer.rate_quote && offer.rate_quote.rate_calculation !== 'override_v1' ? (
        <span className="text-xs surface-muted">
          {t('billingRates.policyFactors', { platform: offer.rate_quote.platform_multiplier, model: offer.rate_quote.model_multiplier })}
        </span>
      ) : offer.rate_quote && offer.rate_quote.source !== 'default' ? (
        <span className="text-xs surface-muted">
          {t(`billingRates.source.${offer.rate_quote.source}`)} ×{offer.rate_quote.policy_multiplier}
        </span>
      ) : null}
    </div>
  )
}

function cnRate(source: 'media' | 'personal' | 'group'): string {
  const base = 'catalog-effective-rate rounded px-1.5 py-0.5 text-xs font-semibold leading-4'
  if (source === 'personal') return `${base} bg-emerald-100/[0.15] text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-300`
  if (source === 'media') return `${base} bg-cyan-100/[0.15] text-cyan-800 dark:bg-cyan-900/30 dark:text-cyan-300`
  return `${base} bg-blue-100/[0.15] text-blue-800 dark:bg-blue-900/30 dark:text-blue-300`
}

export default CatalogRateBadge
